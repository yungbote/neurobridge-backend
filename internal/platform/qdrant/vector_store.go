package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

const (
	payloadNamespaceKey = "_nb_namespace"
	payloadVectorIDKey  = "_nb_vector_id"
	maxErrorBodyBytes   = 1024
)

var pointIDNamespaceUUID = uuid.MustParse("0f1705d1-2c3f-4e40-b2f4-f855f7d3c8e8")

type vectorStore struct {
	log      *logger.Logger
	cfg      Config
	baseURL  string
	nsPrefix string
	distance string
	http     *http.Client
}

type qdrantEnvelope struct {
	Result json.RawMessage `json:"result"`
	Status json.RawMessage `json:"status"`
	Time   float64         `json:"time"`
}

type qdrantSearchResultItem struct {
	ID      json.RawMessage `json:"id"`
	Score   float64         `json:"score"`
	Payload map[string]any  `json:"payload"`
}

func NewVectorStore(log *logger.Logger, cfg Config) (pinecone.VectorStore, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	if err := ValidateConfig(cfg, true); err != nil {
		return nil, err
	}

	s := &vectorStore{
		log:      log.With("service", "QdrantVectorStore"),
		cfg:      cfg,
		baseURL:  strings.TrimRight(cfg.URL, "/"),
		nsPrefix: strings.TrimSpace(cfg.NamespacePrefix),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if err := s.verifyReady(context.Background()); err != nil {
		return nil, err
	}

	log.Info(
		"Qdrant vector store selected",
		"provider", "qdrant",
		"url", s.baseURL,
		"collection", cfg.Collection,
		"namespace_prefix", s.nsPrefix,
		"vector_dim", cfg.VectorDim,
		"distance", s.distance,
	)
	return s, nil
}

func (s *vectorStore) Upsert(ctx context.Context, namespace string, vectors []pinecone.Vector) error {
	if s == nil {
		return nil
	}
	const op = "upsert"
	if len(vectors) == 0 {
		return nil
	}

	qualifiedNS := s.qualifyNamespace(namespace)
	points := make([]map[string]any, 0, len(vectors))
	for _, v := range vectors {
		vectorID := strings.TrimSpace(v.ID)
		if vectorID == "" {
			return opErr(op, OperationErrorValidation, "vector id is required", nil)
		}
		if len(v.Values) == 0 {
			return opErr(op, OperationErrorValidation, fmt.Sprintf("vector %q has empty values", vectorID), nil)
		}
		if s.cfg.VectorDim > 0 && len(v.Values) != s.cfg.VectorDim {
			return opErr(
				op,
				OperationErrorValidation,
				fmt.Sprintf(
					"vector %q dimension mismatch: expected=%d got=%d",
					vectorID,
					s.cfg.VectorDim,
					len(v.Values),
				),
				nil,
			)
		}
		payload := clonePayload(v.Metadata)
		payload[payloadNamespaceKey] = qualifiedNS
		payload[payloadVectorIDKey] = vectorID
		points = append(points, map[string]any{
			"id":      s.pointID(qualifiedNS, vectorID),
			"vector":  v.Values,
			"payload": payload,
		})
	}

	req := map[string]any{"points": points}
	if err := s.doJSON(ctx, op, http.MethodPut, s.collectionPath("/points?wait=true"), req, nil); err != nil {
		return err
	}
	return nil
}

func (s *vectorStore) QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("vector store unavailable")
	}
	matches, err := s.QueryMatches(ctx, namespace, q, topK, filter)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		id := strings.TrimSpace(m.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *vectorStore) QueryMatches(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]pinecone.VectorMatch, error) {
	if s == nil {
		return nil, fmt.Errorf("vector store unavailable")
	}
	const op = "query"
	if len(q) == 0 {
		return nil, opErr(op, OperationErrorValidation, "query vector required", nil)
	}
	if s.cfg.VectorDim > 0 && len(q) != s.cfg.VectorDim {
		return nil, opErr(
			op,
			OperationErrorValidation,
			fmt.Sprintf("query vector dimension mismatch: expected=%d got=%d", s.cfg.VectorDim, len(q)),
			nil,
		)
	}
	if topK <= 0 {
		topK = 10
	}

	qualifiedNS := s.qualifyNamespace(namespace)
	qdrantFilter, err := s.translateQueryFilter(qualifiedNS, filter)
	if err != nil {
		var opErrTyped *OperationError
		if errors.As(err, &opErrTyped) && opErrTyped.Code == OperationErrorUnsupportedFilter {
			s.log.Warn("qdrant query filter unsupported", "namespace", qualifiedNS, "error", err)
		}
		return nil, err
	}

	req := map[string]any{
		"vector":       q,
		"limit":        topK,
		"with_payload": true,
		"with_vector":  false,
		"filter":       qdrantFilter,
	}
	var rawResults []qdrantSearchResultItem
	if err := s.doJSON(
		ctx,
		op,
		http.MethodPost,
		s.collectionPath("/points/search"),
		req,
		&rawResults,
	); err != nil {
		return nil, err
	}

	out := make([]pinecone.VectorMatch, 0, len(rawResults))
	for _, item := range rawResults {
		id := s.extractVectorID(item, qualifiedNS)
		if id == "" {
			continue
		}
		out = append(out, pinecone.VectorMatch{
			ID:    id,
			Score: s.normalizeScore(item.Score),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

func (s *vectorStore) DeleteIDs(ctx context.Context, namespace string, ids []string) error {
	if s == nil {
		return nil
	}
	const op = "delete"
	if len(ids) == 0 {
		return nil
	}

	qualifiedNS := s.qualifyNamespace(namespace)
	pointIDs := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		vectorID := strings.TrimSpace(id)
		if vectorID == "" {
			continue
		}
		pointID := s.pointID(qualifiedNS, vectorID)
		if _, exists := seen[pointID]; exists {
			continue
		}
		seen[pointID] = struct{}{}
		pointIDs = append(pointIDs, pointID)
	}
	if len(pointIDs) == 0 {
		return nil
	}

	req := map[string]any{"points": pointIDs}
	if err := s.doJSON(
		ctx,
		op,
		http.MethodPost,
		s.collectionPath("/points/delete?wait=true"),
		req,
		nil,
	); err != nil {
		return err
	}
	return nil
}

func (s *vectorStore) verifyReady(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("qdrant vector store not initialized")
	}
	const op = "bootstrap_verify"

	readyReq, err := http.NewRequestWithContext(ctxutil.Default(ctx), http.MethodGet, s.baseURL+"/readyz", nil)
	if err != nil {
		return opErr(op, OperationErrorTransportFailed, "build ready request failed", err)
	}
	readyResp, err := s.http.Do(readyReq)
	if err != nil {
		return classifyHTTPCallError(op, "qdrant ready check failed", err)
	}
	_ = readyResp.Body.Close()
	if readyResp.StatusCode < 200 || readyResp.StatusCode >= 300 {
		return &OperationError{
			Code:       OperationErrorQueryFailed,
			Operation:  op,
			StatusCode: readyResp.StatusCode,
			Message:    fmt.Sprintf("qdrant ready check returned status=%d", readyResp.StatusCode),
		}
	}

	var result struct {
		Config struct {
			Params struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			} `json:"params"`
		} `json:"config"`
	}
	if err := s.doJSON(
		ctx,
		op,
		http.MethodGet,
		s.collectionPath(""),
		nil,
		&result,
	); err != nil {
		return err
	}

	size := result.Config.Params.Vectors.Size
	if size != 0 && size != s.cfg.VectorDim {
		return &OperationError{
			Code:      OperationErrorValidation,
			Operation: op,
			Message: fmt.Sprintf(
				"qdrant collection %q vector size mismatch: expected=%d actual=%d",
				s.cfg.Collection,
				s.cfg.VectorDim,
				size,
			),
		}
	}
	s.distance = strings.TrimSpace(result.Config.Params.Vectors.Distance)
	return nil
}

func (s *vectorStore) doJSON(ctx context.Context, op, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(in); err != nil {
			return opErr(op, OperationErrorEncodeFailed, "encode request failed", err)
		}
		body = &buf
	}

	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), method, s.baseURL+path, body)
	if err != nil {
		return opErr(op, OperationErrorTransportFailed, "build request failed", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return classifyHTTPCallError(op, "qdrant request failed", err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 10*maxErrorBodyBytes))
	if readErr != nil {
		return opErr(op, OperationErrorDecodeFailed, "read response failed", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &OperationError{
			Code:       OperationErrorQueryFailed,
			Operation:  op,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("qdrant http status=%d body=%q", resp.StatusCode, truncateBody(raw)),
		}
	}

	var envelope qdrantEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return opErr(op, OperationErrorDecodeFailed, "decode qdrant envelope failed", err)
	}
	if statusErr := parseEnvelopeStatus(envelope.Status); statusErr != "" {
		return &OperationError{
			Code:       OperationErrorQueryFailed,
			Operation:  op,
			StatusCode: resp.StatusCode,
			Message:    statusErr,
		}
	}

	if out == nil {
		return nil
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return opErr(op, OperationErrorDecodeFailed, "decode qdrant result failed", err)
	}
	return nil
}

func classifyHTTPCallError(op, message string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return opErr(op, OperationErrorTimeout, message, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return opErr(op, OperationErrorTimeout, message, err)
	}
	return opErr(op, OperationErrorTransportFailed, message, err)
}

func parseEnvelopeStatus(raw json.RawMessage) string {
	status := strings.TrimSpace(string(raw))
	if status == "" || status == "null" {
		return ""
	}

	var statusString string
	if err := json.Unmarshal(raw, &statusString); err == nil {
		if strings.EqualFold(statusString, "ok") {
			return ""
		}
		return fmt.Sprintf("qdrant status=%q", statusString)
	}

	var statusObject struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &statusObject); err == nil {
		if strings.TrimSpace(statusObject.Error) != "" {
			return strings.TrimSpace(statusObject.Error)
		}
	}

	return fmt.Sprintf("qdrant status=%s", status)
}

func truncateBody(raw []byte) string {
	if len(raw) <= maxErrorBodyBytes {
		return string(raw)
	}
	return string(raw[:maxErrorBodyBytes]) + "..."
}

func clonePayload(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *vectorStore) qualifyNamespace(namespace string) string {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return s.nsPrefix
	}
	return s.nsPrefix + ":" + ns
}

func (s *vectorStore) pointID(qualifiedNS, vectorID string) string {
	deterministic := uuid.NewSHA1(pointIDNamespaceUUID, []byte(qualifiedNS+"|"+vectorID))
	return deterministic.String()
}

func (s *vectorStore) collectionPath(suffix string) string {
	path := "/collections/" + s.cfg.Collection
	if strings.TrimSpace(suffix) == "" {
		return path
	}
	return path + suffix
}

func (s *vectorStore) translateQueryFilter(qualifiedNS string, filter map[string]any) (map[string]any, error) {
	base := translatedFilter{
		Must: []any{
			qdrantMatchCondition(payloadNamespaceKey, qualifiedNS),
		},
	}
	if len(filter) == 0 {
		return base.asMap(), nil
	}

	translated, err := translateFilterMap(filter)
	if err != nil {
		return nil, err
	}
	mergeTranslatedFilters(&base, translated)
	return base.asMap(), nil
}

func (s *vectorStore) extractVectorID(item qdrantSearchResultItem, qualifiedNS string) string {
	if payloadID, ok := item.Payload[payloadVectorIDKey].(string); ok {
		id := strings.TrimSpace(payloadID)
		if id != "" {
			return id
		}
	}
	decodedID := decodePointID(item.ID)
	if decodedID != "" {
		// Conservative fallback: only return raw point ID if payload ID is absent.
		// This should be rare because adapter always writes payloadVectorIDKey.
		return decodedID
	}
	return ""
}

func decodePointID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var idString string
	if err := json.Unmarshal(raw, &idString); err == nil {
		return strings.TrimSpace(idString)
	}
	var idNumber int64
	if err := json.Unmarshal(raw, &idNumber); err == nil {
		return fmt.Sprintf("%d", idNumber)
	}
	return strings.TrimSpace(string(raw))
}

func (s *vectorStore) normalizeScore(score float64) float64 {
	switch strings.ToLower(strings.TrimSpace(s.distance)) {
	case "euclid", "manhattan":
		if score < 0 {
			score = -score
		}
		return 1.0 / (1.0 + score)
	default:
		return score
	}
}

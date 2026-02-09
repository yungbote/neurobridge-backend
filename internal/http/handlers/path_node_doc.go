package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

// GET /api/path-nodes/:id/doc
func (h *PathHandler) GetPathNodeDoc(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("GetPathNodeDoc failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("GetPathNodeDoc failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("GetPathNodeDoc failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	var baseDoc content.NodeDocV1
	if err := json.Unmarshal(docRow.DocJSON, &baseDoc); err != nil {
		response.RespondError(c, http.StatusInternalServerError, "doc_invalid_json", err)
		return
	}

	baseContentHash := docRow.ContentHash
	if withIDs, changed := content.EnsureNodeDocBlockIDs(baseDoc); changed {
		baseDoc = withIDs
		if rawDoc, err := json.Marshal(baseDoc); err == nil {
			if canon, cErr := content.CanonicalizeJSON(rawDoc); cErr == nil {
				now := time.Now().UTC()
				contentHash := content.HashBytes(canon)
				updated := &types.LearningNodeDoc{
					ID:            docRow.ID,
					UserID:        docRow.UserID,
					PathID:        docRow.PathID,
					PathNodeID:    docRow.PathNodeID,
					SchemaVersion: docRow.SchemaVersion,
					DocJSON:       datatypes.JSON(canon),
					DocText:       docRow.DocText,
					ContentHash:   contentHash,
					SourcesHash:   docRow.SourcesHash,
					CreatedAt:     docRow.CreatedAt,
					UpdatedAt:     now,
				}
				_ = h.nodeDocs.Upsert(dbctx.Context{Ctx: c.Request.Context()}, updated)
				baseContentHash = contentHash
			}
		}
	}

	variantRow, variantDoc, variantContentHash, variantReady := h.loadDocVariant(c, rd.UserID, nodeID)

	policyMode := docgen.DocVariantPolicyMode()
	rolloutPct := docgen.DocVariantRolloutPct()
	eligible := rolloutEligible(rd.UserID, rolloutPct)
	safe := true
	if policyMode == "active" && docgen.DocVariantRequireSafe() {
		safe = docVariantPolicySafe(c.Request.Context(), h.policyEval)
	}

	servedDoc := baseDoc
	servedVariant := false

	exposureDoc := baseDoc
	exposureContentHash := baseContentHash
	exposureKind := "base"
	exposurePolicyVersion := docgen.DocPolicyVersion()
	exposureVariantKind := "base"
	var exposureVariantID *uuid.UUID
	baseDocID := docRow.ID
	if variantRow != nil && variantRow.BaseDocID != nil && *variantRow.BaseDocID != uuid.Nil {
		baseDocID = *variantRow.BaseDocID
	}

	candidateMeta := map[string]any{
		"policy_mode":      policyMode,
		"rollout_pct":      rolloutPct,
		"rollout_eligible": eligible,
		"safe_required":    docgen.DocVariantRequireSafe(),
		"safe_to_activate": safe,
	}

	if variantReady {
		candidatePolicyVersion := strings.TrimSpace(variantRow.PolicyVersion)
		candidateVariantKind := strings.TrimSpace(variantRow.VariantKind)
		candidateVariantID := variantRow.ID
		candidateMeta["candidate_variant_id"] = variantRow.ID.String()
		candidateMeta["candidate_variant_kind"] = candidateVariantKind
		candidateMeta["candidate_policy_version"] = candidatePolicyVersion
		candidateMeta["candidate_status"] = strings.TrimSpace(variantRow.Status)

		switch policyMode {
		case "active":
			if eligible && safe {
				servedDoc = variantDoc
				servedVariant = true
				exposureDoc = variantDoc
				exposureContentHash = variantContentHash
				exposureKind = "served"
			} else if !eligible {
				exposureKind = "holdback"
			} else if !safe {
				exposureKind = "rollback"
			}
		case "shadow":
			if eligible {
				exposureDoc = variantDoc
				exposureContentHash = variantContentHash
				exposureKind = "shadow"
			}
		default:
			// off: keep base exposure
		}
		if exposureKind == "served" || exposureKind == "shadow" {
			exposureVariantKind = candidateVariantKind
			exposurePolicyVersion = candidatePolicyVersion
			id := candidateVariantID
			exposureVariantID = &id
		}
	}
	candidateMeta["served_variant"] = servedVariant
	candidateMeta["exposure_kind"] = exposureKind

	// Generated figures are stored in a private bucket; rewrite figure URLs to a protected streaming endpoint.
	// This avoids mixed public/private bucket configs and prevents stale/signed URLs from breaking the UI.
	if withAssetURLs, changed := h.rewriteNodeDocFigureAssetURLs(servedDoc, nodeID); changed {
		servedDoc = withAssetURLs
	}

	var prereqGate *types.PrereqGateDecision
	var gateEvidence prereqGateEvidence
	if h.prereqGates != nil {
		if row, err := h.prereqGates.GetLatestByUserAndNode(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, nodeID); err == nil && row != nil {
			prereqGate = row
			if len(row.EvidenceJSON) > 0 && string(row.EvidenceJSON) != "null" {
				_ = json.Unmarshal(row.EvidenceJSON, &gateEvidence)
			}
		}
	}

	if prereqGate != nil && strings.EqualFold(prereqGate.Decision, "blocked") && strings.EqualFold(prereqGate.GateMode, "hard") {
		c.JSON(http.StatusConflict, gin.H{
			"error":       response.APIError{Message: "prereq_gate_blocked", Code: "prereq_gate_blocked"},
			"prereq_gate": prereqGate,
			"evidence":    gateEvidence,
		})
		return
	}

	if h.docVariantExposure != nil {
		h.logDocVariantExposure(
			c,
			rd,
			nodeID,
			node.PathID,
			exposureDoc,
			baseDocID,
			exposureVariantID,
			exposureVariantKind,
			exposurePolicyVersion,
			exposureKind,
			exposureContentHash,
			candidateMeta,
		)
	}

	if prereqGate != nil {
		if patched, changed := injectPrereqGateCallout(servedDoc, gateEvidence); changed {
			servedDoc = patched
		}
	}

	response.RespondOK(c, gin.H{
		"doc":         servedDoc,
		"prereq_gate": prereqGate,
	})
}

type docVariantBaseline struct {
	ConceptID            string  `json:"concept_id,omitempty"`
	ConceptKey           string  `json:"concept_key,omitempty"`
	Mastery              float64 `json:"mastery,omitempty"`
	Confidence           float64 `json:"confidence,omitempty"`
	EpistemicUncertainty float64 `json:"epistemic_uncertainty,omitempty"`
	AleatoricUncertainty float64 `json:"aleatoric_uncertainty,omitempty"`
}

func (h *PathHandler) loadDocVariant(c *gin.Context, userID, nodeID uuid.UUID) (*types.LearningNodeDocVariant, content.NodeDocV1, string, bool) {
	empty := content.NodeDocV1{}
	if h == nil || h.docVariants == nil || userID == uuid.Nil || nodeID == uuid.Nil {
		return nil, empty, "", false
	}
	row, err := h.docVariants.GetLatestByUserAndNode(dbctx.Context{Ctx: c.Request.Context()}, userID, nodeID)
	if err != nil || row == nil {
		if err != nil && h.log != nil {
			h.log.Warn("GetPathNodeDoc failed (load variant)", "error", err, "path_node_id", nodeID)
		}
		return nil, empty, "", false
	}
	if strings.ToLower(strings.TrimSpace(row.Status)) != "active" {
		return nil, empty, "", false
	}
	if row.ExpiresAt != nil && !row.ExpiresAt.IsZero() && time.Now().After(*row.ExpiresAt) {
		return nil, empty, "", false
	}
	if len(row.DocJSON) == 0 || string(row.DocJSON) == "null" {
		return nil, empty, "", false
	}

	var doc content.NodeDocV1
	if err := json.Unmarshal(row.DocJSON, &doc); err != nil {
		return nil, empty, "", false
	}
	contentHash := row.ContentHash

	if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
		doc = withIDs
		if rawDoc, err := json.Marshal(doc); err == nil {
			if canon, cErr := content.CanonicalizeJSON(rawDoc); cErr == nil {
				now := time.Now().UTC()
				contentHash = content.HashBytes(canon)
				updated := &types.LearningNodeDocVariant{
					ID:              row.ID,
					UserID:          row.UserID,
					PathID:          row.PathID,
					PathNodeID:      row.PathNodeID,
					BaseDocID:       row.BaseDocID,
					VariantKind:     row.VariantKind,
					PolicyVersion:   row.PolicyVersion,
					SchemaVersion:   row.SchemaVersion,
					SnapshotID:      row.SnapshotID,
					RetrievalPackID: row.RetrievalPackID,
					TraceID:         row.TraceID,
					DocJSON:         datatypes.JSON(canon),
					DocText:         row.DocText,
					ContentHash:     contentHash,
					SourcesHash:     row.SourcesHash,
					Status:          row.Status,
					ExpiresAt:       row.ExpiresAt,
					CreatedAt:       row.CreatedAt,
					UpdatedAt:       now,
				}
				_ = h.docVariants.Upsert(dbctx.Context{Ctx: c.Request.Context()}, updated)
			}
		}
	}

	return row, doc, contentHash, true
}

func (h *PathHandler) logDocVariantExposure(
	c *gin.Context,
	rd *ctxutil.RequestData,
	nodeID uuid.UUID,
	pathID uuid.UUID,
	doc content.NodeDocV1,
	baseDocID uuid.UUID,
	variantID *uuid.UUID,
	variantKind string,
	policyVersion string,
	exposureKind string,
	contentHash string,
	metadata map[string]any,
) {
	if h == nil || h.docVariantExposure == nil || rd == nil || rd.UserID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return
	}
	ctx := c.Request.Context()
	conceptKeys := normalizeConceptKeys(extractDocConceptKeys(doc))
	conceptIDs, idToKey := h.resolveConceptIDs(ctx, pathID, conceptKeys)
	baseline := h.buildConceptBaseline(ctx, rd.UserID, conceptIDs, idToKey)

	exposure := &types.DocVariantExposure{
		ID:            uuid.New(),
		UserID:        rd.UserID,
		PathID:        pathID,
		PathNodeID:    nodeID,
		BaseDocID:     nil,
		VariantID:     nil,
		VariantKind:   strings.TrimSpace(variantKind),
		PolicyVersion: strings.TrimSpace(policyVersion),
		SchemaVersion: 1,
		ExposureKind:  strings.TrimSpace(exposureKind),
		Source:        "api",
		ContentHash:   strings.TrimSpace(contentHash),
	}
	if baseDocID != uuid.Nil {
		exposure.BaseDocID = &baseDocID
	}
	if variantID != nil && *variantID != uuid.Nil {
		exposure.VariantID = variantID
	}
	if rd.SessionID != uuid.Nil {
		exposure.SessionID = &rd.SessionID
	}
	if td := ctxutil.GetTraceData(ctx); td != nil {
		exposure.TraceID = strings.TrimSpace(td.TraceID)
		exposure.RequestID = strings.TrimSpace(td.RequestID)
	}

	if len(conceptKeys) > 0 {
		if b, err := json.Marshal(conceptKeys); err == nil {
			exposure.ConceptKeys = datatypes.JSON(b)
		}
	}
	if len(conceptIDs) > 0 {
		if b, err := json.Marshal(uuidStrings(conceptIDs)); err == nil {
			exposure.ConceptIDs = datatypes.JSON(b)
		}
	}
	if len(baseline) > 0 {
		if b, err := json.Marshal(baseline); err == nil {
			exposure.BaselineJSON = datatypes.JSON(b)
		}
	}
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			exposure.Metadata = datatypes.JSON(b)
		}
	}
	_ = h.docVariantExposure.Create(dbctx.Context{Ctx: ctx}, exposure)
}

func (h *PathHandler) resolveConceptIDs(ctx context.Context, pathID uuid.UUID, keys []string) ([]uuid.UUID, map[uuid.UUID]string) {
	out := []uuid.UUID{}
	idToKey := map[uuid.UUID]string{}
	if h == nil || h.concepts == nil || pathID == uuid.Nil || len(keys) == 0 {
		return out, idToKey
	}
	rows, err := h.concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil || len(rows) == 0 {
		return out, idToKey
	}
	byKey := map[string]uuid.UUID{}
	for _, c := range rows {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		key := normalizeConceptKeyDoc(c.Key)
		if key == "" {
			continue
		}
		id := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			id = *c.CanonicalConceptID
		}
		if id != uuid.Nil {
			byKey[key] = id
		}
	}
	seen := map[uuid.UUID]bool{}
	for _, key := range keys {
		if id, ok := byKey[normalizeConceptKeyDoc(key)]; ok && id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
			idToKey[id] = normalizeConceptKeyDoc(key)
		}
	}
	return out, idToKey
}

func (h *PathHandler) buildConceptBaseline(ctx context.Context, userID uuid.UUID, conceptIDs []uuid.UUID, idToKey map[uuid.UUID]string) []docVariantBaseline {
	out := []docVariantBaseline{}
	if h == nil || h.conceptState == nil || userID == uuid.Nil || len(conceptIDs) == 0 {
		return out
	}
	rows, err := h.conceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, userID, conceptIDs)
	if err != nil {
		return out
	}
	for _, st := range rows {
		if st == nil || st.ConceptID == uuid.Nil {
			continue
		}
		out = append(out, docVariantBaseline{
			ConceptID:            st.ConceptID.String(),
			ConceptKey:           idToKey[st.ConceptID],
			Mastery:              st.Mastery,
			Confidence:           st.Confidence,
			EpistemicUncertainty: st.EpistemicUncertainty,
			AleatoricUncertainty: st.AleatoricUncertainty,
		})
	}
	return out
}

func docVariantPolicySafe(ctx context.Context, evals repos.PolicyEvalSnapshotRepo) bool {
	if evals == nil {
		return false
	}
	snap, err := evals.GetLatestByKey(dbctx.Context{Ctx: ctx}, docgen.DocVariantPolicyKey())
	if err != nil || snap == nil {
		return false
	}
	return docVariantSnapshotSafe(snap)
}

func docVariantSnapshotSafe(snap *types.PolicyEvalSnapshot) bool {
	if snap == nil {
		return false
	}
	if snap.Samples < docgen.DocVariantSafeMinSamples() {
		return false
	}
	if snap.IPS < docgen.DocVariantSafeMinIPS() {
		return false
	}
	if snap.Lift < docgen.DocVariantSafeMinLift() {
		return false
	}
	return true
}

func rolloutEligible(userID uuid.UUID, pct float64) bool {
	if pct >= 1.0 {
		return true
	}
	if pct <= 0 || userID == uuid.Nil {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID.String()))
	val := float64(h.Sum32()%10000) / 10000.0
	return val < pct
}

func extractDocConceptKeys(doc content.NodeDocV1) []string {
	keys := make([]string, 0, len(doc.ConceptKeys))
	keys = append(keys, doc.ConceptKeys...)
	for _, block := range doc.Blocks {
		if block == nil {
			continue
		}
		keys = append(keys, stringSliceFromAny(block["concept_keys"])...)
		payload, ok := block["payload"].(map[string]any)
		if !ok || payload == nil {
			continue
		}
		keys = append(keys, stringSliceFromAny(payload["concept_keys"])...)
		if cards, ok := payload["cards"].([]any); ok {
			for _, card := range cards {
				if m, ok := card.(map[string]any); ok {
					keys = append(keys, stringSliceFromAny(m["concept_keys"])...)
				}
			}
		}
		if questions, ok := payload["questions"].([]any); ok {
			for _, q := range questions {
				if m, ok := q.(map[string]any); ok {
					keys = append(keys, stringSliceFromAny(m["concept_keys"])...)
				}
			}
		}
	}
	return keys
}

func normalizeConceptKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = normalizeConceptKeyDoc(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normalizeConceptKeyDoc(k string) string {
	return strings.ToLower(strings.TrimSpace(k))
}

// GET /api/path-nodes/:id/assets/view?key=...
func (h *PathHandler) ViewPathNodeAsset(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.pathNodes == nil || h.path == nil {
		response.RespondError(c, http.StatusInternalServerError, "path_repo_missing", nil)
		return
	}
	if h.bucket == nil {
		response.RespondError(c, http.StatusInternalServerError, "bucket_unavailable", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}
	storageKey := strings.TrimSpace(c.Query("key"))
	if storageKey == "" {
		response.RespondError(c, http.StatusBadRequest, "missing_storage_key", nil)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	// Prevent arbitrary bucket reads: only allow generated node figure assets for this node.
	allowedPrefix := fmt.Sprintf("generated/node_figures/%s/%s/", node.PathID.String(), node.ID.String())
	if !strings.HasPrefix(storageKey, allowedPrefix) {
		response.RespondError(c, http.StatusNotFound, "asset_not_found", nil)
		return
	}

	ctx := c.Request.Context()
	attrs, err := h.bucket.GetObjectAttrs(ctx, gcp.BucketCategoryMaterial, storageKey)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (GetObjectAttrs)", "error", err, "storage_key", storageKey)
		response.RespondError(c, http.StatusNotFound, "asset_not_found", err)
		return
	}

	contentType := resolveContentType("", attrs.ContentType, storageKey, storageKey)
	disposition := buildContentDisposition(storageKey, c.Query("download") != "")
	size := attrs.Size
	rangeHeader := c.GetHeader("Range")

	if rangeHeader != "" && size > 0 {
		rng, ok, rErr := parseByteRangeHeader(rangeHeader, size)
		if rErr != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			response.RespondError(c, http.StatusRequestedRangeNotSatisfiable, "invalid_range", rErr)
			return
		}
		if ok {
			reader, err := h.bucket.OpenRangeReader(ctx, gcp.BucketCategoryMaterial, storageKey, rng.start, rng.end-rng.start+1)
			if err != nil {
				h.log.Error("ViewPathNodeAsset failed (OpenRangeReader)", "error", err, "storage_key", storageKey)
				response.RespondError(c, http.StatusInternalServerError, "stream_failed", err)
				return
			}
			defer reader.Close()
			headers := map[string]string{
				"Content-Range":       fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, size),
				"Accept-Ranges":       "bytes",
				"Content-Disposition": disposition,
			}
			c.DataFromReader(http.StatusPartialContent, rng.end-rng.start+1, contentType, reader, headers)
			return
		}
	}

	reader, err := h.bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, storageKey)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (DownloadFile)", "error", err, "storage_key", storageKey)
		response.RespondError(c, http.StatusInternalServerError, "stream_failed", err)
		return
	}
	defer reader.Close()
	contentLength := size
	if contentLength <= 0 {
		contentLength = -1
	}
	headers := map[string]string{
		"Accept-Ranges":       "bytes",
		"Content-Disposition": disposition,
	}
	c.DataFromReader(http.StatusOK, contentLength, contentType, reader, headers)
}

func (h *PathHandler) rewriteNodeDocFigureAssetURLs(doc content.NodeDocV1, nodeID uuid.UUID) (content.NodeDocV1, bool) {
	if h == nil || h.bucket == nil || nodeID == uuid.Nil || len(doc.Blocks) == 0 {
		return doc, false
	}

	changed := false
	base := fmt.Sprintf("/api/path-nodes/%s/assets/view?key=", nodeID.String())

	for i := range doc.Blocks {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(b["type"]))) != "figure" {
			continue
		}
		assetAny, ok := b["asset"]
		if !ok || assetAny == nil {
			continue
		}
		asset, ok := assetAny.(map[string]any)
		if !ok || asset == nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(stringFromAny(asset["source"])))
		if source == "external" {
			continue
		}
		storageKey := strings.TrimSpace(stringFromAny(asset["storage_key"]))
		if storageKey == "" {
			continue
		}
		wantURL := base + url.QueryEscape(storageKey)
		if strings.TrimSpace(stringFromAny(asset["url"])) == wantURL {
			continue
		}
		asset["url"] = wantURL
		b["asset"] = asset
		changed = true
	}

	return doc, changed
}

type prereqGateEvidence struct {
	Status                string   `json:"status"`
	Decision              string   `json:"decision"`
	Mode                  string   `json:"mode"`
	Reason                string   `json:"reason"`
	Score                 float64  `json:"score"`
	WeakConcepts          []string `json:"weak_concepts"`
	UncertainConcepts     []string `json:"uncertain_concepts"`
	MisconceptionConcepts []string `json:"misconception_concepts"`
	DueReviewConcepts     []string `json:"due_review_concepts"`
	FrameBridgeFrom       string   `json:"frame_bridge_from"`
	FrameBridgeTo         string   `json:"frame_bridge_to"`
	FrameBridgeMD         string   `json:"frame_bridge_md"`
	EscalationAction      string   `json:"escalation_action"`
	EscalationReason      string   `json:"escalation_reason"`
}

func injectPrereqGateCallout(doc content.NodeDocV1, evidence prereqGateEvidence) (content.NodeDocV1, bool) {
	if len(doc.Blocks) == 0 {
		return doc, false
	}
	weak := normalizeKeyList(evidence.WeakConcepts)
	uncertain := normalizeKeyList(evidence.UncertainConcepts)
	miscons := normalizeKeyList(evidence.MisconceptionConcepts)
	due := normalizeKeyList(evidence.DueReviewConcepts)
	frameMD := strings.TrimSpace(evidence.FrameBridgeMD)
	if frameMD == "" && evidence.FrameBridgeFrom != "" && evidence.FrameBridgeTo != "" {
		frameMD = fmt.Sprintf("Try reframing from **%s** to **%s** and restate the rule in the new frame.", evidence.FrameBridgeFrom, evidence.FrameBridgeTo)
	}
	needsPrereq := len(weak) > 0 || len(miscons) > 0 || len(due) > 0 || len(uncertain) > 0
	needsFrame := frameMD != ""
	needsEscalation := strings.TrimSpace(evidence.EscalationAction) != ""
	if !needsPrereq && !needsFrame && !needsEscalation {
		return doc, false
	}

	blocksToInsert := []map[string]any{}
	if needsPrereq {
		variant := "note"
		title := "Prerequisite check"
		if strings.EqualFold(evidence.Status, "not_ready") {
			variant = "warning"
			title = "Prerequisites need attention"
		} else if len(due) > 0 {
			variant = "info"
			title = "Quick prereq refresher"
		}

		parts := []string{
			"Before moving on, take a moment to shore up prerequisites.",
		}
		if len(weak) > 0 {
			parts = append(parts, "Weak prerequisites:\n- "+strings.Join(weak, "\n- "))
		}
		if len(uncertain) > 0 {
			parts = append(parts, "Uncertain prerequisites:\n- "+strings.Join(uncertain, "\n- "))
		}
		if len(miscons) > 0 {
			parts = append(parts, "Active misconceptions to correct:\n- "+strings.Join(miscons, "\n- "))
		}
		if len(due) > 0 {
			parts = append(parts, "Spaced review due:\n- "+strings.Join(due, "\n- "))
		}
		parts = append(parts, "Use the quick checks and flashcards, or revisit prerequisite sections if anything feels shaky.")
		md := strings.Join(parts, "\n\n")

		blocksToInsert = append(blocksToInsert, map[string]any{
			"id":      uuid.New().String(),
			"type":    "callout",
			"variant": variant,
			"title":   title,
			"md":      md,
		})
	}

	if needsFrame {
		blocksToInsert = append(blocksToInsert, map[string]any{
			"id":      uuid.New().String(),
			"type":    "callout",
			"variant": "info",
			"title":   "Try a different frame",
			"md":      frameMD,
		})
	}

	if needsEscalation {
		action := strings.ToLower(strings.TrimSpace(evidence.EscalationAction))
		parts := []string{
			"We noticed repeated friction on prerequisites. Try a different support path:",
		}
		switch action {
		case "alternate_modality":
			parts = append(parts, "- Switch modality: try a short video, diagram, or interactive activity.")
			parts = append(parts, "- Then retry the quick checks.")
		case "guided_recap":
			parts = append(parts, "- Take the guided recap.")
			parts = append(parts, "- Then retry the quick checks.")
		default:
			parts = append(parts, "- Take a short recap or ask for a worked example.")
		}
		escalationMD := strings.Join(parts, "\n")
		blocksToInsert = append(blocksToInsert, map[string]any{
			"id":      uuid.New().String(),
			"type":    "callout",
			"variant": "warning",
			"title":   "Need a different approach",
			"md":      escalationMD,
		})
	}

	insertAt := 0
	for i, b := range doc.Blocks {
		if b == nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		if typ == "prerequisites" {
			insertAt = i + 1
			break
		}
		if typ == "objectives" {
			insertAt = i + 1
		}
	}
	blocks := append([]map[string]any{}, doc.Blocks...)
	if len(blocksToInsert) > 0 {
		if insertAt >= len(blocks) {
			blocks = append(blocks, blocksToInsert...)
		} else {
			blocks = append(blocks[:insertAt], append(blocksToInsert, blocks[insertAt:]...)...)
		}
	}
	doc.Blocks = blocks
	return doc, len(blocksToInsert) > 0
}

func normalizeKeyList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, k := range in {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type DocPatchSelection struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type DocPatchRequest struct {
	BlockID        string             `json:"block_id"`
	BlockIndex     *int               `json:"block_index"`
	Action         string             `json:"action"`
	Instruction    string             `json:"instruction"`
	CitationPolicy string             `json:"citation_policy"`
	Selection      *DocPatchSelection `json:"selection"`
}

// POST /api/path-nodes/:id/doc/patch
func (h *PathHandler) EnqueuePathNodeDocPatch(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.jobSvc == nil {
		response.RespondError(c, http.StatusInternalServerError, "job_service_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	var req DocPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_json", err)
		return
	}

	blockID := strings.TrimSpace(req.BlockID)
	blockIndex := -1
	if req.BlockIndex != nil {
		blockIndex = *req.BlockIndex
	}
	if blockID == "" && blockIndex < 0 {
		response.RespondError(c, http.StatusBadRequest, "missing_block_target", nil)
		return
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "rewrite"
	}
	if action != "rewrite" && action != "regen_media" {
		response.RespondError(c, http.StatusBadRequest, "invalid_action", nil)
		return
	}

	policy := strings.ToLower(strings.TrimSpace(req.CitationPolicy))
	if policy == "" {
		policy = "reuse_only"
	}
	if policy != "reuse_only" && policy != "allow_new" {
		response.RespondError(c, http.StatusBadRequest, "invalid_citation_policy", nil)
		return
	}

	payload := map[string]any{
		"path_node_id":    nodeID.String(),
		"action":          action,
		"instruction":     strings.TrimSpace(req.Instruction),
		"citation_policy": policy,
	}
	if blockID != "" {
		payload["block_id"] = blockID
	}
	if blockIndex >= 0 {
		payload["block_index"] = blockIndex
	}
	if req.Selection != nil {
		payload["selection"] = map[string]any{
			"text":  strings.TrimSpace(req.Selection.Text),
			"start": req.Selection.Start,
			"end":   req.Selection.End,
		}
	}

	entityID := nodeID
	job, err := h.jobSvc.Enqueue(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, "node_doc_patch", "path_node", &entityID, payload)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (enqueue)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "enqueue_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"job_id": job.ID})
}

// GET /api/path-nodes/:id/doc/revisions
func (h *PathHandler) ListPathNodeDocRevisions(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.docRevisions == nil {
		response.RespondError(c, http.StatusInternalServerError, "revision_repo_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	includeDocs := strings.EqualFold(strings.TrimSpace(c.Query("include_docs")), "true") || c.Query("include_docs") == "1"

	rows, err := h.docRevisions.ListByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID, limit)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load revisions)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_revisions_failed", err)
		return
	}
	if !includeDocs {
		for _, r := range rows {
			if r != nil {
				r.BeforeJSON = nil
				r.AfterJSON = nil
			}
		}
	}

	response.RespondOK(c, gin.H{"revisions": rows})
}

// GET /api/path-nodes/:id/doc/materials
func (h *PathHandler) ListPathNodeDocMaterials(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.chunks == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	chunkIDs := dedupeUUIDsLocal(extractChunkIDsFromNodeDocJSON(docRow.DocJSON))
	if len(chunkIDs) == 0 {
		response.RespondOK(c, gin.H{"files": []any{}, "chunk_ids": []any{}, "chunk_ids_by_file": gin.H{}})
		return
	}

	chunks, err := h.chunks.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, chunkIDs)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load chunks)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_chunks_failed", err)
		return
	}

	fileIDSet := map[uuid.UUID]bool{}
	chunkIDsByFile := map[string][]string{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		fileIDSet[ch.MaterialFileID] = true
		chunkIDsByFile[ch.MaterialFileID.String()] = append(chunkIDsByFile[ch.MaterialFileID.String()], ch.ID.String())
	}

	fileIDs := make([]uuid.UUID, 0, len(fileIDSet))
	for id := range fileIDSet {
		fileIDs = append(fileIDs, id)
	}

	files, err := h.materialFiles.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, fileIDs)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load files)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
		return
	}

	response.RespondOK(c, gin.H{
		"files":             files,
		"chunk_ids":         uuidStrings(chunkIDs),
		"chunk_ids_by_file": chunkIDsByFile,
	})
}

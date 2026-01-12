package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content/schema"
	"github.com/yungbote/neurobridge-backend/internal/platform/apierr"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type DrillSpec struct {
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	Reason         string `json:"reason,omitempty"`
	SuggestedCount int    `json:"suggested_count,omitempty"`
}

type GeneratePathNodeDrillInput struct {
	UserID     uuid.UUID
	PathNodeID uuid.UUID
	Kind       string
	Count      int
}

func (u Usecases) ListPathNodeDrills(ctx context.Context, userID uuid.UUID, nodeID uuid.UUID) ([]DrillSpec, error) {
	if userID == uuid.Nil {
		return nil, apierr.New(http.StatusUnauthorized, "unauthorized", nil)
	}
	if nodeID == uuid.Nil {
		return nil, apierr.New(http.StatusBadRequest, "invalid_path_node_id", fmt.Errorf("missing path_node_id"))
	}
	if u.deps.PathNodes == nil || u.deps.Path == nil {
		return nil, apierr.New(http.StatusInternalServerError, "path_repo_missing", fmt.Errorf("missing deps"))
	}

	node, err := u.deps.PathNodes.GetByID(dbctx.Context{Ctx: ctx}, nodeID)
	if err != nil {
		return nil, apierr.New(http.StatusInternalServerError, "load_node_failed", err)
	}
	if node == nil || node.PathID == uuid.Nil {
		return nil, apierr.New(http.StatusNotFound, "node_not_found", nil)
	}
	pathRow, err := u.deps.Path.GetByID(dbctx.Context{Ctx: ctx}, node.PathID)
	if err != nil {
		return nil, apierr.New(http.StatusInternalServerError, "load_path_failed", err)
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != userID {
		return nil, apierr.New(http.StatusNotFound, "path_not_found", nil)
	}

	// v0: static recs (later: use node metadata + user profile + mastery)
	return []DrillSpec{
		{Kind: "flashcards", Label: "Flashcards", Reason: "Memorize key terms and definitions.", SuggestedCount: 12},
		{Kind: "quiz", Label: "Quick quiz", Reason: "Check understanding with grounded MCQs.", SuggestedCount: 8},
	}, nil
}

func (u Usecases) GeneratePathNodeDrill(ctx context.Context, in GeneratePathNodeDrillInput) (json.RawMessage, error) {
	if in.UserID == uuid.Nil {
		return nil, apierr.New(http.StatusUnauthorized, "unauthorized", nil)
	}
	if in.PathNodeID == uuid.Nil {
		return nil, apierr.New(http.StatusBadRequest, "invalid_path_node_id", fmt.Errorf("missing path_node_id"))
	}

	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		return nil, apierr.New(http.StatusBadRequest, "missing_kind", nil)
	}
	if kind != "quiz" && kind != "flashcards" {
		return nil, apierr.New(http.StatusBadRequest, "unsupported_kind", fmt.Errorf("unsupported kind %q", kind))
	}

	if u.deps.PathNodes == nil || u.deps.Path == nil {
		return nil, apierr.New(http.StatusInternalServerError, "path_repo_missing", fmt.Errorf("missing deps"))
	}
	if u.deps.AI == nil || u.deps.Chunks == nil || u.deps.Drills == nil {
		return nil, apierr.New(http.StatusInternalServerError, "drill_generator_not_configured", fmt.Errorf("missing deps"))
	}

	node, err := u.deps.PathNodes.GetByID(dbctx.Context{Ctx: ctx}, in.PathNodeID)
	if err != nil {
		return nil, apierr.New(http.StatusInternalServerError, "load_node_failed", err)
	}
	if node == nil || node.PathID == uuid.Nil {
		return nil, apierr.New(http.StatusNotFound, "node_not_found", nil)
	}

	pathRow, err := u.deps.Path.GetByID(dbctx.Context{Ctx: ctx}, node.PathID)
	if err != nil {
		return nil, apierr.New(http.StatusInternalServerError, "load_path_failed", err)
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != in.UserID {
		return nil, apierr.New(http.StatusNotFound, "path_not_found", nil)
	}

	// Require that node content exists (drills are grounded in node content).
	var docRow *types.LearningNodeDoc
	if u.deps.NodeDocs != nil {
		docRow, _ = u.deps.NodeDocs.GetByPathNodeID(dbctx.Context{Ctx: ctx}, node.ID)
	}

	raw, err := u.generateDrillV1(ctx, in.UserID, node, docRow, kind, in.Count)
	if err != nil {
		return nil, apierr.New(http.StatusInternalServerError, "generate_drill_failed", err)
	}
	return raw, nil
}

func (u Usecases) generateDrillV1(ctx context.Context, userID uuid.UUID, node *types.PathNode, docRow *types.LearningNodeDoc, kind string, count int) (json.RawMessage, error) {
	if u.deps.AI == nil || u.deps.Chunks == nil || u.deps.Drills == nil {
		return nil, fmt.Errorf("drill generator not configured")
	}
	if node == nil || node.ID == uuid.Nil || node.PathID == uuid.Nil {
		return nil, fmt.Errorf("missing node")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "flashcards" && kind != "quiz" {
		return nil, fmt.Errorf("unsupported kind %q", kind)
	}

	// Defaults and bounds.
	switch kind {
	case "flashcards":
		if count <= 0 {
			count = 12
		}
		if count < 6 {
			count = 6
		}
		if count > 24 {
			count = 24
		}
	case "quiz":
		if count <= 0 {
			count = 8
		}
		if count < 4 {
			count = 4
		}
		if count > 12 {
			count = 12
		}
	}

	// Prefer doc-backed evidence; fallback to legacy node content markdown if doc missing.
	sourcesHash := ""
	var evidenceChunkIDs []uuid.UUID
	if docRow != nil && len(docRow.DocJSON) > 0 && string(docRow.DocJSON) != "null" {
		sourcesHash = strings.TrimSpace(docRow.SourcesHash)
		evidenceChunkIDs = extractChunkIDsFromNodeDocJSON(docRow.DocJSON)
	}

	if len(evidenceChunkIDs) == 0 {
		// Legacy fallback: derive evidence from node.ContentJSON citations.
		if len(node.ContentJSON) == 0 || string(node.ContentJSON) == "null" || strings.TrimSpace(string(node.ContentJSON)) == "" {
			return nil, fmt.Errorf("node content not ready")
		}
		_, citationsCSV := contentJSONToMarkdownAndCitations([]byte(node.ContentJSON))
		for _, s := range strings.Split(citationsCSV, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
				evidenceChunkIDs = append(evidenceChunkIDs, id)
			}
		}
		evidenceChunkIDs = dedupeUUIDs(evidenceChunkIDs)
		sourcesHash = content.HashSources("legacy_node_content", 1, uuidStrings(evidenceChunkIDs))
	}

	if len(evidenceChunkIDs) == 0 {
		return nil, fmt.Errorf("no evidence chunks available")
	}

	if sourcesHash == "" {
		sourcesHash = content.HashSources("unknown_sources", 1, uuidStrings(evidenceChunkIDs))
	}

	// Cache lookup.
	if cached, err := u.deps.Drills.GetByKey(dbctx.Context{Ctx: ctx}, userID, node.ID, kind, count, sourcesHash); err == nil && cached != nil && len(cached.PayloadJSON) > 0 && string(cached.PayloadJSON) != "null" {
		if json.Valid(cached.PayloadJSON) {
			return json.RawMessage(cached.PayloadJSON), nil
		}
	}

	// Load chunks and build excerpts.
	// Keep prompt small/deterministic.
	const maxEvidence = 18
	if len(evidenceChunkIDs) > maxEvidence {
		evidenceChunkIDs = evidenceChunkIDs[:maxEvidence]
	}
	chunks, err := u.deps.Chunks.GetByIDs(dbctx.Context{Ctx: ctx}, evidenceChunkIDs)
	if err != nil {
		return nil, err
	}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	allowed := map[string]bool{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID] = ch
		allowed[ch.ID.String()] = true
	}

	excerpts := buildChunkExcerpts(chunkByID, evidenceChunkIDs, 16, 700)
	if strings.TrimSpace(excerpts) == "" {
		return nil, fmt.Errorf("empty evidence excerpts")
	}

	schemaMap, err := schema.DrillPayloadV1()
	if err != nil {
		return nil, err
	}

	system := `
You generate supplemental drills for studying, grounded ONLY in the provided excerpts.
Hard rules:
- Return ONLY valid JSON matching the schema.
- No learner-facing meta (no "Plan", no check-ins, no preference questions).
- Every item must include non-empty citations that reference ONLY provided chunk_ids.
`

	var user string
	switch kind {
	case "flashcards":
		user = fmt.Sprintf(`
DRILL_KIND: flashcards
TARGET_CARD_COUNT: %d
NODE_TITLE: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

Task:
- Output kind="flashcards"
- Produce exactly TARGET_CARD_COUNT cards (or as close as possible if constrained by excerpts).
- Cards should be atomic and test a single idea.
- Keep fronts short; backs can be 1-4 sentences.
- citations must reference provided chunk_ids only.
- Set questions=[].
Return JSON only.`, count, node.Title, excerpts)
	case "quiz":
		user = fmt.Sprintf(`
DRILL_KIND: quiz
TARGET_QUESTION_COUNT: %d
NODE_TITLE: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

Task:
- Output kind="quiz"
- Produce exactly TARGET_QUESTION_COUNT MCQs.
- Each question must have 4 options with stable ids like "a","b","c","d".
- answer_id must match one of the option ids.
- explanation_md should justify using the excerpts (no new facts).
- citations must reference provided chunk_ids only.
- Set cards=[].
Return JSON only.`, count, node.Title, excerpts)
	}

	var lastErrs []string
	for attempt := 1; attempt <= 3; attempt++ {
		feedback := ""
		if len(lastErrs) > 0 {
			feedback = "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrs, "\n- ")
		}

		start := time.Now()
		obj, genErr := u.deps.AI.GenerateJSON(ctx, system, user+feedback, "drill_payload_v1", schemaMap)
		latency := int(time.Since(start).Milliseconds())
		if genErr != nil {
			lastErrs = []string{"generate_failed: " + genErr.Error()}
			u.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, lastErrs, nil)
			continue
		}

		raw, _ := json.Marshal(obj)
		var payload content.DrillPayloadV1
		if err := json.Unmarshal(raw, &payload); err != nil {
			lastErrs = []string{"schema_unmarshal_failed"}
			u.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, lastErrs, nil)
			continue
		}

		// Best-effort scrub for occasional meta phrasing that can slip into learner-facing drill text.
		if scrubbed, phrases := content.ScrubDrillPayloadV1(payload); len(phrases) > 0 {
			payload = scrubbed
		}

		minCount := count
		maxCount := count
		if kind == "flashcards" {
			minCount = count - 2
			maxCount = count + 2
			if minCount < 6 {
				minCount = 6
			}
			if maxCount > 24 {
				maxCount = 24
			}
		} else if kind == "quiz" {
			minCount = count
			maxCount = count
		}

		errs, qm := content.ValidateDrillPayloadV1(payload, allowed, kind, minCount, maxCount)
		if len(errs) > 0 {
			lastErrs = errs
			u.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, errs, qm)
			continue
		}

		// Persist the scrubbed-and-validated payload (not the raw model output bytes).
		raw, _ = json.Marshal(payload)
		canon, err := content.CanonicalizeJSON(raw)
		if err != nil {
			return nil, err
		}
		contentHash := content.HashBytes(canon)

		row := &types.LearningDrillInstance{
			ID:            uuid.New(),
			UserID:        userID,
			PathID:        node.PathID,
			PathNodeID:    node.ID,
			Kind:          kind,
			Count:         count,
			SourcesHash:   sourcesHash,
			SchemaVersion: 1,
			PayloadJSON:   datatypes.JSON(canon),
			ContentHash:   contentHash,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		_ = u.deps.Drills.Upsert(dbctx.Context{Ctx: ctx}, row)
		u.recordGenRun(ctx, "drill", &row.ID, userID, node.PathID, node.ID, "succeeded", "drill_v1@1:"+kind, attempt, latency, nil, map[string]any{
			"content_hash": contentHash,
			"sources_hash": sourcesHash,
		})

		return json.RawMessage(canon), nil
	}

	return nil, fmt.Errorf("drill generation failed after retries: %v", lastErrs)
}

func (u Usecases) recordGenRun(ctx context.Context, artifactType string, artifactID *uuid.UUID, userID uuid.UUID, pathID uuid.UUID, pathNodeID uuid.UUID, status string, promptVersion string, attempt int, latencyMS int, validationErrors []string, qualityMetrics map[string]any) {
	if u.deps.GenRuns == nil {
		return
	}
	ve := datatypes.JSON([]byte(`null`))
	if len(validationErrors) > 0 {
		b, _ := json.Marshal(validationErrors)
		ve = datatypes.JSON(b)
	}
	qm := datatypes.JSON([]byte(`null`))
	if qualityMetrics != nil {
		b, _ := json.Marshal(qualityMetrics)
		qm = datatypes.JSON(b)
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "unknown"
	}
	_, _ = u.deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{{
		ID:               uuid.New(),
		ArtifactType:     artifactType,
		ArtifactID:       artifactID,
		UserID:           userID,
		PathID:           pathID,
		PathNodeID:       pathNodeID,
		Status:           status,
		Model:            model,
		PromptVersion:    promptVersion,
		Attempt:          attempt,
		LatencyMS:        latencyMS,
		TokensIn:         0,
		TokensOut:        0,
		ValidationErrors: ve,
		QualityMetrics:   qm,
		CreatedAt:        time.Now().UTC(),
	}})
}

func buildChunkExcerpts(byID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxLines int, maxChars int) string {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}
	var b strings.Builder
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := byID[id]
		if ch == nil {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		if len(txt) > maxChars {
			txt = txt[:maxChars] + "..."
		}
		b.WriteString("[chunk_id=")
		b.WriteString(id.String())
		b.WriteString("] ")
		b.WriteString(txt)
		b.WriteString("\n")
		n++
		if n >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func extractChunkIDsFromNodeDocJSON(raw datatypes.JSON) []uuid.UUID {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	blocks, _ := obj["blocks"].([]any)
	out := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]bool{}
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		for _, c := range stringSliceFromAny(extractChunkIDsFromCitations(m["citations"])) {
			if id, err := uuid.Parse(strings.TrimSpace(c)); err == nil && id != uuid.Nil && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func extractChunkIDsFromCitations(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func dedupeUUIDs(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func uuidStrings(in []uuid.UUID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if id != uuid.Nil {
			out = append(out, id.String())
		}
	}
	return out
}

func contentJSONToMarkdownAndCitations(raw []byte) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", ""
	}

	// citations is optional in ContentJSONSchema but is expected in generated outputs.
	citations := []string{}
	if v, ok := obj["citations"]; ok {
		citations = append(citations, stringSliceFromAny(v)...)
	}

	blocksAny, _ := obj["blocks"].([]any)
	var b strings.Builder
	for _, rawBlock := range blocksAny {
		m, ok := rawBlock.(map[string]any)
		if !ok || m == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(stringFromAny(m["kind"])))
		content := strings.TrimSpace(stringFromAny(m["content_md"]))
		items := stringSliceFromAny(m["items"])
		assetRefs := stringSliceFromAny(m["asset_refs"])

		switch kind {
		case "heading":
			if content != "" {
				b.WriteString("## ")
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "paragraph", "callout":
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "bullets":
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				b.WriteString("- ")
				b.WriteString(it)
				b.WriteString("\n")
			}
			if len(items) > 0 {
				b.WriteString("\n")
			}
		case "steps":
			n := 0
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				n++
				b.WriteString(fmt.Sprintf("%d. %s\n", n, it))
			}
			if n > 0 {
				b.WriteString("\n")
			}
		case "divider":
			b.WriteString("\n---\n\n")
		case "image":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[image: %s]\n\n", assetRefs[0]))
			}
		case "video_embed":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[video: %s]\n\n", assetRefs[0]))
			}
		default:
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		}
	}

	md := strings.TrimSpace(b.String())
	csv := strings.Join(dedupeStrings(citations), ", ")
	return md, csv
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(v)
	}
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s := strings.TrimSpace(stringFromAny(it))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

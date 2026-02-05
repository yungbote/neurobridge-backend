package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/apierr"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type quickCheckAttemptRequest struct {
	Action        string     `json:"action"` // submit|hint
	Answer        string     `json:"answer"`
	ClientEventID string     `json:"client_event_id"`
	OccurredAt    *time.Time `json:"occurred_at,omitempty"`
	LatencyMS     int        `json:"latency_ms,omitempty"`
	Confidence    float64    `json:"confidence,omitempty"`
}

// POST /api/path-nodes/:id/quick-checks/:block_id/attempt
func (h *PathHandler) AttemptPathNodeQuickCheck(c *gin.Context) {
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
	blockID := strings.TrimSpace(c.Param("block_id"))
	if blockID == "" {
		response.RespondError(c, http.StatusBadRequest, "missing_block_id", nil)
		return
	}

	// Keep the payload small; answers are short.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<15)

	var req quickCheckAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.RespondError(c, http.StatusBadRequest, "invalid_body", err)
		return
	}

	out, err := h.learning.QuickCheckAttempt(c.Request.Context(), learningmod.QuickCheckAttemptInput{
		UserID:     rd.UserID,
		PathNodeID: nodeID,
		BlockID:    blockID,
		Action:     req.Action,
		Answer:     req.Answer,
	})
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) {
			response.RespondError(c, ae.Status, ae.Code, ae.Err)
			return
		}
		response.RespondError(c, http.StatusInternalServerError, "quick_check_failed", err)
		return
	}

	h.bestEffortIngestQuickCheckEvent(c, rd.UserID, nodeID, blockID, req, out)

	response.RespondOK(c, gin.H{"result": out})
}

func (h *PathHandler) bestEffortIngestQuickCheckEvent(
	c *gin.Context,
	userID uuid.UUID,
	nodeID uuid.UUID,
	blockID string,
	req quickCheckAttemptRequest,
	out learningmod.QuickCheckAttemptOutput,
) {
	if h == nil || c == nil || userID == uuid.Nil || nodeID == uuid.Nil {
		return
	}
	if h.events == nil {
		return
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "submit"
	}

	typ := ""
	data := map[string]any{
		"question_id": blockID,
		"block_id":    blockID,
		"kind":        "quick_check",
	}

	switch action {
	case "hint":
		typ = types.EventHintUsed
	case "submit":
		typ = types.EventQuestionAnswered
		data["is_correct"] = out.IsCorrect
		data["grader_confidence"] = out.Confidence
		if req.LatencyMS > 0 {
			data["latency_ms"] = req.LatencyMS
		}
		if req.Confidence > 0 {
			data["confidence"] = req.Confidence
		}
		ans := strings.TrimSpace(req.Answer)
		if ans != "" {
			sum := sha256.Sum256([]byte(ans))
			data["answer_sha256"] = hex.EncodeToString(sum[:])
			data["answer_len"] = len([]rune(ans))
		}
	default:
		return
	}

	pathID := ""
	conceptIDs := []string{}
	canonicalByKey := map[string]string{}
	conceptWeights := map[string]float64{}
	nodeDifficultyLabel := ""
	nodeDifficultyScore := 0.0
	if h.pathNodes != nil {
		if node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID); err == nil && node != nil && node.PathID != uuid.Nil {
			pathID = node.PathID.String()

			var meta map[string]any
			if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
				_ = json.Unmarshal(node.Metadata, &meta)
			}
			nodeDifficultyLabel = difficultyLabelFromMeta(meta)
			nodeDifficultyScore = difficultyToTheta(nodeDifficultyLabel)

			// Best-effort: attach node concept IDs so quick_check attempts update the user knowledge graph.
			if h.concepts != nil && meta != nil {
				rawKeys, ok := meta["concept_keys"]
				if !ok || rawKeys == nil {
					rawKeys = meta["conceptKeys"]
				}
				keys := make([]string, 0, 16)
				switch t := rawKeys.(type) {
				case []string:
					keys = append(keys, t...)
				case []any:
					for _, x := range t {
						keys = append(keys, strings.TrimSpace(fmt.Sprint(x)))
					}
				default:
					// ignore
				}
				seen := map[string]bool{}
				norm := make([]string, 0, len(keys))
				for _, k := range keys {
					k = strings.TrimSpace(strings.ToLower(k))
					if k == "" || seen[k] {
						continue
					}
					seen[k] = true
					norm = append(norm, k)
				}
				if len(norm) > 0 {
					if rows, err := h.concepts.GetByScopeAndKeys(dbctx.Context{Ctx: c.Request.Context()}, "path", &node.PathID, norm); err == nil {
						seenIDs := map[string]bool{}
						for _, cc := range rows {
							if cc == nil || cc.ID == uuid.Nil {
								continue
							}
							key := strings.TrimSpace(strings.ToLower(cc.Key))
							if key == "" {
								continue
							}
							id := cc.ID
							if cc.CanonicalConceptID != nil && *cc.CanonicalConceptID != uuid.Nil {
								id = *cc.CanonicalConceptID
							}
							s := id.String()
							if s != "" {
								canonicalByKey[key] = s
							}
							if s != "" && !seenIDs[s] {
								seenIDs[s] = true
								conceptIDs = append(conceptIDs, s)
							}
						}
					}
				}

				if len(canonicalByKey) > 0 {
					rawWeights, ok := meta["concept_weights"]
					if !ok || rawWeights == nil {
						rawWeights = meta["conceptWeights"]
					}
					weightsByKey := map[string]float64{}
					switch t := rawWeights.(type) {
					case map[string]float64:
						for k, v := range t {
							weightsByKey[strings.TrimSpace(strings.ToLower(k))] = v
						}
					case map[string]any:
						for k, v := range t {
							weightsByKey[strings.TrimSpace(strings.ToLower(k))] = floatFromAny(v, 0)
						}
					}
					if len(weightsByKey) > 0 {
						total := 0.0
						for key, w := range weightsByKey {
							if w <= 0 {
								continue
							}
							if id := canonicalByKey[key]; id != "" {
								conceptWeights[id] += w
								total += w
							}
						}
						if total > 0 {
							for id, w := range conceptWeights {
								conceptWeights[id] = w / total
							}
						} else {
							conceptWeights = map[string]float64{}
						}
					}
				}
			}
		}
	}

	itemType := strings.ToLower(strings.TrimSpace(out.QuestionType))
	optionsCount := out.OptionsCount
	itemGuess := 0.18
	if optionsCount > 1 {
		itemGuess = 1.0 / float64(optionsCount)
	} else if itemType == "true_false" {
		itemGuess = 0.5
	}
	if itemGuess < 0.02 {
		itemGuess = 0.02
	}
	itemDisc := 1.0
	switch itemType {
	case "freeform":
		itemDisc = 1.15
	case "true_false":
		itemDisc = 0.75
	case "mcq":
		itemDisc = 0.95
	}
	itemDifficulty := nodeDifficultyScore
	switch itemType {
	case "freeform":
		itemDifficulty += 0.15
	case "true_false":
		itemDifficulty -= 0.10
	}
	itemDifficulty = clampRange(itemDifficulty, -2.5, 2.5)

	input := services.EventInput{
		ClientEventID: strings.TrimSpace(req.ClientEventID),
		Type:          typ,
		OccurredAt:    req.OccurredAt,
		PathID:        pathID,
		PathNodeID:    nodeID.String(),
		ConceptIDs:    conceptIDs,
		Data:          data,
	}
	if len(conceptWeights) > 0 {
		data["concept_weights"] = conceptWeights
	}
	if blockID != "" {
		data["testlet_id"] = fmt.Sprintf("quick_check:%s:%s", nodeID.String(), blockID)
		data["testlet_type"] = "quick_check"
	}

	data["item_type"] = itemType
	data["item_options"] = optionsCount
	data["item_guess"] = itemGuess
	data["item_discrimination"] = itemDisc
	data["item_difficulty"] = itemDifficulty
	if nodeDifficultyLabel != "" {
		data["node_difficulty_label"] = nodeDifficultyLabel
	}
	if !math.IsNaN(nodeDifficultyScore) && nodeDifficultyScore != 0 {
		data["node_difficulty"] = nodeDifficultyScore
	}

	dbc := dbctx.Context{Ctx: c.Request.Context()}
	if _, err := h.events.Ingest(dbc, []services.EventInput{input}); err != nil {
		if h.log != nil {
			h.log.Warn("QuickCheck event ingest failed (continuing)", "error", err, "type", typ)
		}
		return
	}
	if h.jobSvc != nil {
		_, _, _ = h.jobSvc.EnqueueUserModelUpdateIfNeeded(dbc, userID, typ)
		_, _, _ = h.jobSvc.EnqueueRuntimeUpdateIfNeeded(dbc, userID, typ)
	}
}

func difficultyLabelFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	raw := meta["difficulty"]
	if raw == nil {
		raw = meta["sig_difficulty"]
	}
	if raw == nil {
		raw = meta["path_difficulty"]
	}
	return strings.TrimSpace(strings.ToLower(fmt.Sprint(raw)))
}

func difficultyToTheta(label string) float64 {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "intro", "introductory", "beginner":
		return -0.9
	case "foundation", "basic":
		return -0.5
	case "intermediate", "standard":
		return 0.0
	case "advanced":
		return 0.6
	case "expert", "research":
		return 1.1
	default:
		if f, err := strconv.ParseFloat(label, 64); err == nil && !math.IsNaN(f) {
			return clampRange(f, -2.5, 2.5)
		}
	}
	return 0.0
}

func clampRange(v float64, lo float64, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func floatFromAny(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case uint:
		return float64(t)
	case uint64:
		return float64(t)
	case uint32:
		return float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
	}
	return def
}

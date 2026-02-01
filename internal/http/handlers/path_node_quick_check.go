package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	if h.pathNodes != nil {
		if node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID); err == nil && node != nil && node.PathID != uuid.Nil {
			pathID = node.PathID.String()

			// Best-effort: attach node concept IDs so quick_check attempts update the user knowledge graph.
			if h.concepts != nil && len(node.Metadata) > 0 && string(node.Metadata) != "null" {
				var meta map[string]any
				if json.Unmarshal(node.Metadata, &meta) == nil && meta != nil {
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
								id := cc.ID
								if cc.CanonicalConceptID != nil && *cc.CanonicalConceptID != uuid.Nil {
									id = *cc.CanonicalConceptID
								}
								s := id.String()
								if s != "" && !seenIDs[s] {
									seenIDs[s] = true
									conceptIDs = append(conceptIDs, s)
								}
							}
						}
					}
				}
			}
		}
	}

	input := services.EventInput{
		ClientEventID: strings.TrimSpace(req.ClientEventID),
		Type:          typ,
		OccurredAt:    req.OccurredAt,
		PathID:        pathID,
		PathNodeID:    nodeID.String(),
		ConceptIDs:    conceptIDs,
		Data:          data,
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

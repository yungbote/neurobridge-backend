package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type EventHandler struct {
	events services.EventService
	jobs   services.JobService
	index  repos.UserLibraryIndexRepo
	path   repos.PathRepo
}

func NewEventHandler(events services.EventService, jobs services.JobService, index repos.UserLibraryIndexRepo, path repos.PathRepo) *EventHandler {
	return &EventHandler{events: events, jobs: jobs, index: index, path: path}
}

type ingestEventsRequest struct {
	Events []services.EventInput `json:"events"`
}

func isMeaningfulEventType(t string) bool {
	switch t {
	case
		// lifecycle / progress
		"activity_started",
		"activity_completed",
		"activity_abandoned",
		// assessment
		"quiz_started",
		"question_answered",
		"quiz_completed",
		// help
		"hint_used",
		"explanation_opened",
		// explicit feedback
		"style_feedback",
		"feedback_thumbs_up",
		"feedback_thumbs_down",
		"feedback_too_easy",
		"feedback_too_hard",
		"feedback_confusing",
		"feedback_loved_diagram",
		"feedback_want_examples",
		// summarized engagement (NOT raw scroll spam unless you aggregate)
		"scroll_depth",
		"block_viewed":
		return true
	default:
		return false
	}
}

func (h *EventHandler) Ingest(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_body", err)
		return
	}
	if len(raw) == 0 {
		response.RespondError(c, http.StatusBadRequest, "empty_body", nil)
		return
	}
	var inputs []services.EventInput
	var env ingestEventsRequest
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Events) > 0 {
		inputs = env.Events
	} else {
		var arr []services.EventInput
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			response.RespondError(c, http.StatusBadRequest, "invalid_json", err2)
			return
		}
		inputs = arr
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	n, err := h.events.Ingest(dbc, inputs)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "event_ingest_failed", err)
		return
	}
	meaningful := false
	trigger := ""
	for _, ev := range inputs {
		if isMeaningfulEventType(ev.Type) {
			meaningful = true
			trigger = ev.Type
			break
		}
	}
	enqueued := false
	enqueuedJobs := map[string]int{}
	if meaningful && h.jobs != nil {
		_, ok, _ := h.jobs.EnqueueUserModelUpdateIfNeeded(dbc, rd.UserID, trigger)
		enqueued = ok
		if ok {
			enqueuedJobs["user_model_update"]++
		}
		_, ok, _ = h.jobs.EnqueueRuntimeUpdateIfNeeded(dbc, rd.UserID, trigger)
		enqueued = enqueued || ok
		if ok {
			enqueuedJobs["runtime_update"]++
		}

		// Enqueue additional derived-state refresh jobs (best-effort).
		//
		// NOTE: Some refresh jobs are global per-user (cursor-based). We dedupe them by entity=("user", userID)
		// inside JobService to avoid double-counting.
		if h.jobs != nil {
			pathIDs := map[uuid.UUID]bool{}
			for _, ev := range inputs {
				pidStr := strings.TrimSpace(ev.PathID)
				if pidStr == "" {
					continue
				}
				if pid, err := uuid.Parse(pidStr); err == nil && pid != uuid.Nil {
					pathIDs[pid] = true
				}
			}

			pathToSet := map[uuid.UUID]uuid.UUID{}
			setIDs := make([]uuid.UUID, 0, len(pathIDs))
			seenSet := map[uuid.UUID]bool{}
			for pid := range pathIDs {
				setID := uuid.Nil
				if h.index != nil {
					if idx, err := h.index.GetByUserAndPathID(dbc, rd.UserID, pid); err == nil && idx != nil && idx.MaterialSetID != uuid.Nil {
						setID = idx.MaterialSetID
					}
				}
				if setID == uuid.Nil && h.path != nil {
					if row, err := h.path.GetByID(dbc, pid); err == nil && row != nil && row.UserID != nil && *row.UserID == rd.UserID && row.MaterialSetID != nil && *row.MaterialSetID != uuid.Nil {
						setID = *row.MaterialSetID
					}
				}
				if setID == uuid.Nil {
					continue
				}
				pathToSet[pid] = setID
				if !seenSet[setID] {
					seenSet[setID] = true
					setIDs = append(setIDs, setID)
				}
			}

			// Pick any set ID for global per-user refresh jobs (they scan by user cursor, not by set).
			var anySetID uuid.UUID
			if len(setIDs) > 0 {
				anySetID = setIDs[0]
			}

			if anySetID != uuid.Nil {
				if _, ok, _ := h.jobs.EnqueueProgressionCompactIfNeeded(dbc, rd.UserID, anySetID, trigger); ok {
					enqueuedJobs["progression_compact"]++
				}
				if _, ok, _ := h.jobs.EnqueueVariantStatsRefreshIfNeeded(dbc, rd.UserID, anySetID, trigger); ok {
					enqueuedJobs["variant_stats_refresh"]++
				}
			}

			// Path-scoped refreshes (subpath-aware): enqueue per path_id so programs with subpaths work.
			for pid, setID := range pathToSet {
				if pid == uuid.Nil || setID == uuid.Nil {
					continue
				}
				if _, ok, _ := h.jobs.EnqueueCompletedUnitRefreshForPathIfNeeded(dbc, rd.UserID, pid, setID, trigger); ok {
					enqueuedJobs["completed_unit_refresh"]++
				}
				if _, ok, _ := h.jobs.EnqueuePriorsRefreshForPathIfNeeded(dbc, rd.UserID, pid, setID, trigger); ok {
					enqueuedJobs["priors_refresh"]++
				}
			}
		}
	}
	response.RespondOK(c, gin.H{
		"ok":       true,
		"ingested": n,
		"enqueued": enqueued,
		"jobs":     enqueuedJobs,
	})
}

package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type EventHandler struct {
	events services.EventService
	jobs   services.JobService
}

func NewEventHandler(events services.EventService, jobs services.JobService) *EventHandler {
	return &EventHandler{events: events, jobs: jobs}
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
	rd := requestdata.GetRequestData(c.Request.Context())
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
	n, err := h.events.Ingest(c.Request.Context(), nil, inputs)
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
	if meaningful {
		_, ok, _ := h.jobs.EnqueueUserModelUpdateIfNeeded(c.Request.Context(), nil, rd.UserID, trigger)
		enqueued = ok
	}
	response.RespondOK(c, gin.H{
		"ok":       true,
		"ingested": n,
		"enqueued": enqueued,
	})
}











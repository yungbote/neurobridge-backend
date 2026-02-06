package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/personalization"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

var eventTypeRe = regexp.MustCompile(`^[a-z0-9_\.]{3,64}$`)

var requiredEventKeys = map[string][]string{
	personalization.EventBlockRead:              {"block_id"},
	personalization.EventBlockViewed:            {"block_id"},
	personalization.EventQuestionAnswered:       {"question_id", "is_correct"},
	personalization.EventRuntimePromptCompleted: {"prompt_id"},
	personalization.EventRuntimePromptDismissed: {"prompt_id"},
	personalization.EventExperimentExposure:        {"experiment", "variant"},
	personalization.EventExperimentGuardrailBreach: {"experiment", "guardrail"},
	personalization.EventEngagementFunnelStep:      {"funnel", "step"},
	personalization.EventCostTelemetry:             {"category", "amount_usd"},
	personalization.EventSecurityEvent:             {"event"},
}

type EventInput struct {
	ClientEventID   string         `json:"client_event_id"`
	Type            string         `json:"type"`
	OccurredAt      *time.Time     `json:"occurred_at,omitempty"`
	PathID          string         `json:"path_id,omitempty"`
	PathNodeID      string         `json:"path_node_id,omitempty"`
	ActivityID      string         `json:"activity_id,omitempty"`
	ActivityVariant string         `json:"activity_variant,omitempty"`
	Modality        string         `json:"modality,omitempty"`
	ConceptIDs      []string       `json:"concept_ids,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
}

type EventService interface {
	Ingest(dbc dbctx.Context, inputs []EventInput) (int, error)
}

type eventService struct {
	db   *gorm.DB
	log  *logger.Logger
	repo repos.UserEventRepo
}

func NewEventService(db *gorm.DB, baseLog *logger.Logger, repo repos.UserEventRepo) EventService {
	return &eventService{
		db:   db,
		log:  baseLog.With("service", "EventService"),
		repo: repo,
	}
}

func (s *eventService) Ingest(dbc dbctx.Context, inputs []EventInput) (int, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return 0, fmt.Errorf("not authenticated")
	}
	if len(inputs) == 0 {
		return 0, nil
	}
	if len(inputs) > 200 {
		return 0, fmt.Errorf("too many events (max 200)")
	}
	now := time.Now().UTC()
	rows := make([]*types.UserEvent, 0, len(inputs))
	metrics := observability.Current()
	for i := range inputs {
		in := inputs[i]

		typ := strings.TrimSpace(strings.ToLower(in.Type))
		if !eventTypeRe.MatchString(typ) {
			return 0, fmt.Errorf("invalid event type at index %d", i)
		}

		occurred := now
		if in.OccurredAt != nil && !in.OccurredAt.IsZero() {
			occurred = in.OccurredAt.UTC()
		}

		clientID := strings.TrimSpace(in.ClientEventID)
		if clientID == "" {
			if s.log != nil {
				s.log.Warn("event ingest: missing client_event_id; generating fallback", "type", typ)
			}
			clientID = uuid.New().String()
		}

		var (
			pathID     *uuid.UUID
			pathNodeID *uuid.UUID
			activityID *uuid.UUID
			conceptID  *uuid.UUID
		)

		if v := strings.TrimSpace(in.PathID); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				pathID = &id
			}
		}
		if v := strings.TrimSpace(in.PathNodeID); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				pathNodeID = &id
			}
		}
		if v := strings.TrimSpace(in.ActivityID); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				activityID = &id
			}
		}
		if len(in.ConceptIDs) == 1 {
			if id, err := uuid.Parse(strings.TrimSpace(in.ConceptIDs[0])); err == nil {
				conceptID = &id
			}
		}
		data := map[string]any{}
		for k, v := range in.Data {
			data[k] = v
		}
		if len(in.ConceptIDs) > 0 {
			data["concept_ids"] = in.ConceptIDs
		}
		if strings.TrimSpace(in.ActivityVariant) != "" {
			data["activity_variant"] = strings.TrimSpace(in.ActivityVariant)
		}
		if strings.TrimSpace(in.Modality) != "" {
			data["modality"] = strings.TrimSpace(in.Modality)
		}
		if reqKeys, ok := requiredEventKeys[typ]; ok && len(reqKeys) > 0 {
			missing := make([]string, 0, len(reqKeys))
			for _, key := range reqKeys {
				if _, ok := data[key]; !ok {
					missing = append(missing, key)
				}
			}
			if len(missing) > 0 {
				observability.ReportDataQualityMissingKeys(dbc.Ctx, s.log, "user_event_ingest", missing, map[string]any{
					"event_type": typ,
				})
			}
		}
		if metrics != nil {
			switch typ {
			case personalization.EventClientPerf:
				kind := strings.TrimSpace(fmt.Sprint(data["kind"]))
				name := strings.TrimSpace(fmt.Sprint(data["name"]))
				ms := floatFromAny(data["duration_ms"])
				if ms <= 0 {
					ms = floatFromAny(data["value"])
				}
				if ms > 0 {
					metrics.ObserveClientPerf(kind, name, ms/1000)
				}
			case personalization.EventClientError:
				kind := strings.TrimSpace(fmt.Sprint(data["kind"]))
				metrics.IncClientError(kind)
			case personalization.EventExperimentExposure:
				experiment := stringFromAny(data["experiment"])
				variant := stringFromAny(data["variant"])
				source := stringFromAny(data["source"])
				metrics.IncExperimentExposure(experiment, variant, source)
			case personalization.EventExperimentGuardrailBreach:
				experiment := stringFromAny(data["experiment"])
				guardrail := stringFromAny(data["guardrail"])
				metrics.IncExperimentGuardrail(experiment, guardrail)
			case personalization.EventEngagementFunnelStep:
				funnel := stringFromAny(data["funnel"])
				step := stringFromAny(data["step"])
				metrics.IncEngagementFunnelStep(funnel, step)
			case personalization.EventCostTelemetry:
				category := stringFromAny(data["category"])
				source := stringFromAny(data["source"])
				amount := floatFromAny(data["amount_usd"])
				metrics.AddCost(category, source, amount)
			case personalization.EventSecurityEvent:
				event := stringFromAny(data["event"])
				metrics.IncSecurityEvent(event)
			}
		}
		b, _ := json.Marshal(data)
		rows = append(rows, &types.UserEvent{
			ID:              uuid.New(),
			UserID:          rd.UserID,
			ClientEventID:   clientID,
			OccurredAt:      occurred,
			SessionID:       rd.SessionID,
			PathID:          pathID,
			PathNodeID:      pathNodeID,
			ActivityID:      activityID,
			ActivityVariant: strings.TrimSpace(in.ActivityVariant),
			Modality:        strings.TrimSpace(in.Modality),
			ConceptID:       conceptID,
			Type:            typ,
			Data:            datatypes.JSON(b),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	n, err := s.repo.CreateIgnoreDuplicates(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, rows)
	if err != nil {
		s.log.Warn("event ingest failed", "error", err)
		return 0, err
	}
	return n, nil
}

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

var eventTypeRe = regexp.MustCompile(`^[a-z0-9_\.]{3,64}$`)

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
	Ingest(ctx context.Context, tx *gorm.DB, inputs []EventInput) (int, error)
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

func (s *eventService) Ingest(ctx context.Context, tx *gorm.DB, inputs []EventInput) (int, error) {
	rd := requestdata.GetRequestData(ctx)
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
		b, _ := json.Marshal(data)
		rows = append(rows, &types.UserEvent{
			ID:             uuid.New(),
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
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	n, err := s.repo.CreateIgnoreDuplicates(ctx, transaction, rows)
	if err != nil {
		s.log.Warn("event ingest failed", "error", err)
		return 0, err
	}
	return n, nil
}











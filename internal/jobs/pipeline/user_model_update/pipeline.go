package user_model_update

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *UserModelUpdatePipeline) Run(jobCtx *runtime.Context) error {
	if jobCtx == nil || jobCtx.Job == nil {
		return nil
	}

	userID, err := p.resolveUserID(jobCtx)
	if err != nil {
		jobCtx.Fail("validate", err)
		return nil
	}

	const consumer = "user_model_update"
	const batchSize = 500

	jobCtx.Progress("process", 5, "Loading cursor")

	var processed int
	var hasMore bool

	err = p.db.WithContext(jobCtx.Ctx).Transaction(func(tx *gorm.DB) error {
		cur, cErr := p.cursorRepo.Get(jobCtx.Ctx, tx, userID, consumer)
		if cErr != nil && cErr != gorm.ErrRecordNotFound {
			return cErr
		}
		if cur == nil || cur.ID == uuid.Nil {
			cur = &types.UserEventCursor{
				ID:       uuid.New(),
				UserID:   userID,
				Consumer: consumer,
				UpdatedAt: time.Now().UTC(),
			}
		}

		events, eErr := p.userEventRepo.ListAfterCursor(jobCtx.Ctx, tx, userID, cur.LastCreatedAt, cur.LastEventID, batchSize+1)
		if eErr != nil {
			return eErr
		}

		if len(events) == 0 {
			cur.UpdatedAt = time.Now().UTC()
			return p.cursorRepo.Upsert(jobCtx.Ctx, tx, cur)
		}

		if len(events) > batchSize {
			hasMore = true
			events = events[:batchSize]
		}

		jobCtx.Progress("process", 25, fmt.Sprintf("Processing %d events", len(events)))

		if err := p.applyEvents(jobCtx.Ctx, tx, userID, events); err != nil {
			return err
		}

		last := events[len(events)-1]
		tm := last.CreatedAt.UTC()
		id := last.ID
		cur.LastCreatedAt = &tm
		cur.LastEventID = &id
		cur.UpdatedAt = time.Now().UTC()

		if err := p.cursorRepo.Upsert(jobCtx.Ctx, tx, cur); err != nil {
			return err
		}

		processed = len(events)
		return nil
	})

	if err != nil {
		jobCtx.Fail("process", err)
		return nil
	}

	if processed == 0 {
		jobCtx.Succeed("noop", map[string]any{"processed": 0})
		return nil
	}

	jobCtx.Progress("process", 90, "Done")
	jobCtx.Succeed("done", map[string]any{
		"user_id":   userID.String(),
		"processed": processed,
		"has_more":  hasMore,
	})

	if hasMore {
		_ = p.enqueueFollowup(jobCtx.Ctx, userID)
	}

	return nil
}

func (p *UserModelUpdatePipeline) resolveUserID(jobCtx *runtime.Context) (uuid.UUID, error) {
	if id, ok := jobCtx.PayloadUUID("user_id"); ok && id != uuid.Nil {
		return id, nil
	}
	if jobCtx.Job.EntityID != nil && *jobCtx.Job.EntityID != uuid.Nil {
		return *jobCtx.Job.EntityID, nil
	}
	return uuid.Nil, fmt.Errorf("missing user_id")
}

func (p *UserModelUpdatePipeline) enqueueFollowup(ctx context.Context, userID uuid.UUID) error {
	if p.jobRunRepo == nil {
		return nil
	}

	entityID := userID
	// avoid infinite duplicates
	exists, err := p.jobRunRepo.ExistsRunnable(ctx, nil, userID, "user_model_update", "user", &entityID)
	if err != nil || exists {
		return err
	}

	now := time.Now().UTC()
	job := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: userID,
		JobType:     "user_model_update",
		EntityType:  "user",
		EntityID:    &entityID,
		Status:      "queued",
		Stage:       "queued",
		Progress:    0,
		Attempts:    0,
		Payload:     datatypes.JSON([]byte(fmt.Sprintf(`{"user_id":"%s"}`, userID.String()))),
		Result:      datatypes.JSON([]byte(`{}`)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err = p.jobRunRepo.Create(ctx, nil, []*types.JobRun{job})
	return err
}











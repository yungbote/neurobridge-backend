package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type JobService interface {
	Enqueue(dbc dbctx.Context, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID, payload map[string]any) (*types.JobRun, error)
	EnqueueDebouncedUserModelUpdate(dbc dbctx.Context, userID uuid.UUID) (*types.JobRun, bool, error)
	EnqueueUserModelUpdateIfNeeded(dbc dbctx.Context, ownerUserID uuid.UUID, trigger string) (*types.JobRun, bool, error)
	GetByIDForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error)
	GetLatestForEntityForRequestUser(dbc dbctx.Context, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error)
	CancelForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error)
	RestartForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error)
}

type jobService struct {
	db     *gorm.DB
	log    *logger.Logger
	repo   repos.JobRunRepo
	notify JobNotifier
}

func NewJobService(db *gorm.DB, baseLog *logger.Logger, repo repos.JobRunRepo, notify JobNotifier) JobService {
	return &jobService{
		db:     db,
		log:    baseLog.With("service", "JobService"),
		repo:   repo,
		notify: notify,
	}
}

func (s *jobService) Enqueue(dbc dbctx.Context, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID, payload map[string]any) (*types.JobRun, error) {
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	if jobType == "" {
		return nil, fmt.Errorf("missing job_type")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	var payloadJSON datatypes.JSON
	if payload != nil {
		b, _ := json.Marshal(payload)
		payloadJSON = datatypes.JSON(b)
	} else {
		payloadJSON = datatypes.JSON([]byte(`{}`))
	}
	now := time.Now()
	job := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     jobType,
		EntityType:  entityType,
		EntityID:    entityID,
		Status:      "queued",
		Stage:       "queued",
		Progress:    0,
		Attempts:    0,
		Message:     "Queued",
		Payload:     payloadJSON,
		Result:      datatypes.JSON([]byte(`{}`)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.repo.Create(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, []*types.JobRun{job}); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	// Notify immediately (request-time)
	s.notify.JobCreated(ownerUserID, job)
	return job, nil
}

func (s *jobService) EnqueueDebouncedUserModelUpdate(dbc dbctx.Context, userID uuid.UUID) (*types.JobRun, bool, error) {
	if userID == uuid.Nil {
		return nil, false, fmt.Errorf("missing user_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	// If a user_model_update job is already queued/running for this user, do nothing.
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}
	has, err := s.repo.HasRunnableForEntity(repoCtx, userID, "user", userID, "user_model_update")
	if err != nil {
		return nil, false, err
	}
	if has {
		return nil, false, nil
	}

	payload := map[string]any{
		"user_id": userID.String(),
	}
	entityID := userID
	job, err := s.Enqueue(repoCtx, userID, "user_model_update", "user", &entityID, payload)
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (s *jobService) EnqueueUserModelUpdateIfNeeded(dbc dbctx.Context, ownerUserID uuid.UUID, trigger string) (*types.JobRun, bool, error) {
	if ownerUserID == uuid.Nil {
		return nil, false, fmt.Errorf("missing owner_user_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	entityID := ownerUserID
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}
	exists, err := s.repo.ExistsRunnable(repoCtx, ownerUserID, "user_model_update", "user", &entityID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		return nil, false, nil
	}

	payload := map[string]any{
		"trigger": trigger,
	}
	job, err := s.Enqueue(repoCtx, ownerUserID, "user_model_update", "user", &entityID, payload)
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (s *jobService) GetByIDForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if jobID == uuid.Nil {
		return nil, fmt.Errorf("missing job id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	rows, err := s.repo.GetByIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, []uuid.UUID{jobID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, fmt.Errorf("job not found")
	}
	if rows[0].OwnerUserID != rd.UserID {
		return nil, fmt.Errorf("job not found")
	}
	return rows[0], nil
}

func (s *jobService) GetLatestForEntityForRequestUser(dbc dbctx.Context, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if entityType == "" || entityID == uuid.Nil || jobType == "" {
		return nil, fmt.Errorf("missing entity/job info")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	return s.repo.GetLatestByEntity(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, rd.UserID, entityType, entityID, jobType)
}

func (s *jobService) CancelForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if jobID == uuid.Nil {
		return nil, fmt.Errorf("missing job id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	var updated *types.JobRun
	shouldNotify := false

	err := transaction.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		inner := dbctx.Context{Ctx: dbc.Ctx, Tx: txx}
		job, err := s.GetByIDForRequestUser(inner, jobID)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("job not found")
		}

		status := strings.ToLower(strings.TrimSpace(job.Status))
		if status == "succeeded" || status == "failed" || status == "canceled" {
			updated = job
			return nil
		}

		now := time.Now().UTC()
		if err := s.repo.UpdateFields(inner, jobID, map[string]interface{}{
			"status":       "canceled",
			"message":      "Canceled",
			"locked_at":    nil,
			"heartbeat_at": now,
			"updated_at":   now,
		}); err != nil {
			return err
		}

		job.Status = "canceled"
		job.Message = "Canceled"
		job.LockedAt = nil
		job.HeartbeatAt = &now
		job.UpdatedAt = now
		updated = job
		shouldNotify = true

		// Best-effort: if this is a learning_build root job, cancel any child stage jobs.
		if strings.EqualFold(strings.TrimSpace(job.JobType), "learning_build") {
			childIDs := extractLearningBuildChildJobIDs(job.Result)
			for _, cid := range childIDs {
				if cid == uuid.Nil {
					continue
				}
				// Only cancel jobs that haven't already completed.
				if err := txx.WithContext(dbc.Ctx).
					Model(&types.JobRun{}).
					Where("id = ? AND status NOT IN ?", cid, []string{"succeeded", "failed", "canceled"}).
					Updates(map[string]interface{}{
						"status":       "canceled",
						"locked_at":    nil,
						"heartbeat_at": now,
						"updated_at":   now,
					}).Error; err != nil {
					// don't fail cancel for partial child cancellation
					s.log.Warn("Cancel child job failed", "job_id", jobID, "child_job_id", cid, "error", err)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if shouldNotify && s.notify != nil && updated != nil {
		s.notify.JobCanceled(rd.UserID, updated)
	}
	return updated, nil
}

func (s *jobService) RestartForRequestUser(dbc dbctx.Context, jobID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if jobID == uuid.Nil {
		return nil, fmt.Errorf("missing job id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	var updated *types.JobRun
	shouldNotify := false

	err := transaction.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		inner := dbctx.Context{Ctx: dbc.Ctx, Tx: txx}
		job, err := s.GetByIDForRequestUser(inner, jobID)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("job not found")
		}

		status := strings.ToLower(strings.TrimSpace(job.Status))
		if status != "canceled" && status != "failed" {
			return fmt.Errorf("job not restartable")
		}

		now := time.Now().UTC()
		nextResult := job.Result
		if strings.EqualFold(strings.TrimSpace(job.JobType), "learning_build") {
			nextResult = resetLearningBuildStateForRestart(nextResult)
		}

		if err := s.repo.UpdateFields(inner, jobID, map[string]interface{}{
			"status":        "queued",
			"stage":         "queued",
			"progress":      0,
			"message":       "Restarting…",
			"error":         "",
			"last_error_at": nil,
			"result":        nextResult,
			"locked_at":     nil,
			"heartbeat_at":  now,
			"updated_at":    now,
		}); err != nil {
			return err
		}

		job.Status = "queued"
		job.Stage = "queued"
		job.Progress = 0
		job.Message = "Restarting…"
		job.Error = ""
		job.LastErrorAt = nil
		job.Result = nextResult
		job.LockedAt = nil
		job.HeartbeatAt = &now
		job.UpdatedAt = now

		updated = job
		shouldNotify = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if shouldNotify && s.notify != nil && updated != nil {
		s.notify.JobRestarted(rd.UserID, updated)
	}
	return updated, nil
}

func extractLearningBuildChildJobIDs(result datatypes.JSON) []uuid.UUID {
	if len(result) == 0 || string(result) == "null" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil {
		return nil
	}
	rawStages, ok := obj["stages"]
	if !ok || rawStages == nil {
		return nil
	}
	stageMap, ok := rawStages.(map[string]any)
	if !ok || len(stageMap) == 0 {
		return nil
	}

	seen := make(map[uuid.UUID]bool, len(stageMap))
	out := make([]uuid.UUID, 0, len(stageMap))
	for _, v := range stageMap {
		m, ok := v.(map[string]any)
		if !ok || m == nil {
			continue
		}
		idStr := strings.TrimSpace(fmt.Sprint(m["child_job_id"]))
		if idStr == "" {
			continue
		}
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func resetLearningBuildStateForRestart(result datatypes.JSON) datatypes.JSON {
	if len(result) == 0 || string(result) == "null" {
		return result
	}
	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil {
		return result
	}

	// Avoid honoring a previous wait window.
	obj["wait_until"] = nil
	obj["last_progress"] = 0

	rawStages, ok := obj["stages"]
	if ok && rawStages != nil {
		if stageMap, ok := rawStages.(map[string]any); ok {
			for _, v := range stageMap {
				m, ok := v.(map[string]any)
				if !ok || m == nil {
					continue
				}
				st := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["status"])))
				if st == "succeeded" {
					continue
				}
				m["status"] = "pending"
				delete(m, "child_job_id")
				delete(m, "child_job_status")
				delete(m, "last_error")
				delete(m, "started_at")
				delete(m, "finished_at")
				delete(m, "child_result")
			}
		}
	}

	b, err := json.Marshal(obj)
	if err != nil {
		return result
	}
	return datatypes.JSON(b)
}

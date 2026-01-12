package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

const (
	SagaStatusRunning       = "running"
	SagaStatusSucceeded     = "succeeded"
	SagaStatusFailed        = "failed"
	SagaStatusCompensating  = "compensating"
	SagaStatusCompensated   = "compensated"
	SagaActionStatusPending = "pending"
	SagaActionStatusDone    = "done"
	SagaActionStatusFailed  = "failed"
)

const (
	SagaActionKindGCSDeleteKey      = "gcs_delete_key"
	SagaActionKindGCSDeletePrefix   = "gcs_delete_prefix"
	SagaActionKindPineconeDeleteIDs = "pinecone_delete_ids"
)

type SagaService interface {
	CreateOrGetSaga(ctx context.Context, ownerUserID uuid.UUID, rootJobID uuid.UUID) (uuid.UUID, error)
	AppendAction(dbc dbctx.Context, sagaID uuid.UUID, kind string, payload map[string]any) error
	Compensate(ctx context.Context, sagaID uuid.UUID) error
	MarkSagaStatus(ctx context.Context, sagaID uuid.UUID, status string) error
}

type sagaService struct {
	db      *gorm.DB
	log     *logger.Logger
	runs    repos.SagaRunRepo
	actions repos.SagaActionRepo

	bucket gcp.BucketService
	vec    pinecone.VectorStore
}

func NewSagaService(
	db *gorm.DB,
	baseLog *logger.Logger,
	runs repos.SagaRunRepo,
	actions repos.SagaActionRepo,
	bucket gcp.BucketService,
	vec pinecone.VectorStore,
) SagaService {
	return &sagaService{
		db:      db,
		log:     baseLog.With("service", "SagaService"),
		runs:    runs,
		actions: actions,
		bucket:  bucket,
		vec:     vec,
	}
}

func (s *sagaService) CreateOrGetSaga(ctx context.Context, ownerUserID uuid.UUID, rootJobID uuid.UUID) (uuid.UUID, error) {
	if s == nil || s.db == nil || s.runs == nil {
		return uuid.Nil, fmt.Errorf("saga service not configured")
	}
	if ownerUserID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("missing owner_user_id")
	}
	if rootJobID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("missing root_job_id")
	}

	var sagaID uuid.UUID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		existing, err := s.runs.GetByRootJobID(dbc, rootJobID)
		if err != nil {
			return err
		}
		if existing != nil && existing.ID != uuid.Nil {
			sagaID = existing.ID
			return nil
		}
		now := time.Now().UTC()
		row := &types.SagaRun{
			ID:          uuid.New(),
			OwnerUserID: ownerUserID,
			RootJobID:   rootJobID,
			Status:      SagaStatusRunning,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := s.runs.Create(dbc, []*types.SagaRun{row}); err != nil {
			return err
		}
		sagaID = row.ID
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return sagaID, nil
}

// AppendAction must be called with a non-nil tx so actions are committed atomically
// with the canonical stage state.
func (s *sagaService) AppendAction(dbc dbctx.Context, sagaID uuid.UUID, kind string, payload map[string]any) error {
	if s == nil || s.runs == nil || s.actions == nil {
		return fmt.Errorf("saga service not configured")
	}
	if dbc.Tx == nil {
		return fmt.Errorf("AppendAction requires a db transaction")
	}
	if sagaID == uuid.Nil {
		return fmt.Errorf("missing saga_id")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return fmt.Errorf("missing saga action kind")
	}

	// Serialize seq assignment by locking saga_run.
	sr, err := s.runs.LockByID(dbc, sagaID)
	if err != nil {
		return err
	}
	if sr == nil {
		return fmt.Errorf("saga_run not found: %s", sagaID.String())
	}

	maxSeq, err := s.actions.GetMaxSeq(dbc, sagaID)
	if err != nil {
		return err
	}

	raw, _ := json.Marshal(payload)
	now := time.Now().UTC()
	row := &types.SagaAction{
		ID:        uuid.New(),
		SagaID:    sagaID,
		Seq:       maxSeq + 1,
		Kind:      kind,
		Payload:   datatypes.JSON(raw),
		Status:    SagaActionStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = s.actions.Create(dbc, []*types.SagaAction{row})
	return err
}

func (s *sagaService) MarkSagaStatus(ctx context.Context, sagaID uuid.UUID, status string) error {
	if s == nil || s.runs == nil {
		return fmt.Errorf("saga service not configured")
	}
	if sagaID == uuid.Nil {
		return fmt.Errorf("missing saga_id")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("missing saga status")
	}
	return s.runs.UpdateFields(dbctx.Context{Ctx: ctx}, sagaID, map[string]interface{}{"status": status})
}

func (s *sagaService) Compensate(ctx context.Context, sagaID uuid.UUID) error {
	if s == nil || s.actions == nil {
		return fmt.Errorf("saga service not configured")
	}
	if sagaID == uuid.Nil {
		return fmt.Errorf("missing saga_id")
	}

	_ = s.MarkSagaStatus(ctx, sagaID, SagaStatusCompensating)

	actions, err := s.actions.ListBySagaIDDesc(dbctx.Context{Ctx: ctx}, sagaID)
	if err != nil {
		return err
	}

	for _, a := range actions {
		if a == nil || a.ID == uuid.Nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Status), SagaActionStatusDone) {
			continue
		}

		execErr := s.executeAction(ctx, a)
		nextStatus := SagaActionStatusDone
		if execErr != nil {
			nextStatus = SagaActionStatusFailed
			if s.log != nil {
				s.log.Warn("saga action compensate failed",
					"saga_id", sagaID.String(),
					"action_id", a.ID.String(),
					"kind", a.Kind,
					"seq", a.Seq,
					"err", execErr.Error(),
				)
			}
		}
		_ = s.actions.UpdateFields(dbctx.Context{Ctx: ctx}, a.ID, map[string]interface{}{"status": nextStatus})
	}

	_ = s.MarkSagaStatus(ctx, sagaID, SagaStatusCompensated)
	return nil
}

func (s *sagaService) executeAction(ctx context.Context, a *types.SagaAction) error {
	if a == nil {
		return nil
	}
	kind := strings.TrimSpace(a.Kind)
	if kind == "" {
		return nil
	}
	switch kind {
	case SagaActionKindGCSDeleteKey:
		if s.bucket == nil {
			return fmt.Errorf("bucket service unavailable")
		}
		var p struct {
			Category string `json:"category"`
			Key      string `json:"key"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		cat, err := parseBucketCategory(p.Category)
		if err != nil {
			return err
		}
		key := strings.TrimSpace(p.Key)
		if key == "" {
			return nil
		}
		err = s.bucket.DeleteFile(dbctx.Context{Ctx: ctx}, cat, key)
		if isNotFoundErr(err) {
			return nil
		}
		return err

	case SagaActionKindGCSDeletePrefix:
		if s.bucket == nil {
			return fmt.Errorf("bucket service unavailable")
		}
		var p struct {
			Category string `json:"category"`
			Prefix   string `json:"prefix"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		cat, err := parseBucketCategory(p.Category)
		if err != nil {
			return err
		}
		prefix := strings.TrimSpace(p.Prefix)
		if prefix == "" {
			return nil
		}
		return s.bucket.DeletePrefix(ctx, cat, prefix)

	case SagaActionKindPineconeDeleteIDs:
		if s.vec == nil {
			return fmt.Errorf("pinecone vector store unavailable")
		}
		var p struct {
			Namespace string   `json:"namespace"`
			IDs       []string `json:"ids"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		ns := strings.TrimSpace(p.Namespace)
		if ns == "" || len(p.IDs) == 0 {
			return nil
		}
		return s.vec.DeleteIDs(ctx, ns, p.IDs)

	default:
		return fmt.Errorf("unknown saga action kind: %s", kind)
	}
}

func parseBucketCategory(category string) (gcp.BucketCategory, error) {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case string(gcp.BucketCategoryMaterial):
		return gcp.BucketCategoryMaterial, nil
	case string(gcp.BucketCategoryAvatar):
		return gcp.BucketCategoryAvatar, nil
	default:
		return "", fmt.Errorf("unknown bucket category: %q", category)
	}
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "not found") || strings.Contains(s, "doesn't exist") || strings.Contains(s, "does not exist") {
		return true
	}
	return false
}

func SagaStagingPrefix(sagaID uuid.UUID) string {
	if sagaID == uuid.Nil {
		return ""
	}
	return fmt.Sprintf("staging/saga/%s/", sagaID.String())
}

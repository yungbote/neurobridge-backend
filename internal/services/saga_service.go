package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/observability"
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
	SagaActionKindGCSDeleteKey    = "gcs_delete_key"
	SagaActionKindGCSDeletePrefix = "gcs_delete_prefix"
	SagaActionKindVectorDeleteIDs = "vector_delete_ids"
	// Legacy action kind retained for backward compatibility with historical rows and dashboards.
	SagaActionKindPineconeDeleteIDs = "pinecone_delete_ids"
)

type SagaService interface {
	CreateOrGetSaga(ctx context.Context, ownerUserID uuid.UUID, rootJobID uuid.UUID) (uuid.UUID, error)
	AppendAction(dbc dbctx.Context, sagaID uuid.UUID, kind string, payload map[string]any) error
	Compensate(ctx context.Context, sagaID uuid.UUID) error
	MarkSagaStatus(ctx context.Context, sagaID uuid.UUID, status string) error
}

type sagaService struct {
	db        *gorm.DB
	log       *logger.Logger
	runs      repos.SagaRunRepo
	actions   repos.SagaActionRepo
	aggregate domainagg.SagaAggregate

	bucket gcp.BucketService
	vec    pinecone.VectorStore

	vectorProvider string
}

func NewSagaService(
	db *gorm.DB,
	baseLog *logger.Logger,
	runs repos.SagaRunRepo,
	actions repos.SagaActionRepo,
	aggregate domainagg.SagaAggregate,
	bucket gcp.BucketService,
	vec pinecone.VectorStore,
	vectorProvider string,
) SagaService {
	return &sagaService{
		db:        db,
		log:       baseLog.With("service", "SagaService"),
		runs:      runs,
		actions:   actions,
		aggregate: aggregate,
		bucket:    bucket,
		vec:       vec,

		vectorProvider: strings.TrimSpace(strings.ToLower(vectorProvider)),
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

func (s *sagaService) AppendAction(dbc dbctx.Context, sagaID uuid.UUID, kind string, payload map[string]any) error {
	if s == nil || s.aggregate == nil {
		return fmt.Errorf("saga service not configured")
	}
	if sagaID == uuid.Nil {
		return fmt.Errorf("missing saga_id")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return fmt.Errorf("missing saga action kind")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal saga payload: %w", err)
	}
	_, err = s.aggregate.AppendAction(dbc.Ctx, domainagg.AppendSagaActionInput{
		SagaID:     sagaID,
		Kind:       kind,
		Payload:    raw,
		AppendedAt: time.Now().UTC(),
	})
	return err
}

func (s *sagaService) MarkSagaStatus(ctx context.Context, sagaID uuid.UUID, status string) error {
	if s == nil {
		return fmt.Errorf("saga service not configured")
	}
	if s.aggregate == nil {
		return fmt.Errorf("saga aggregate not configured")
	}
	if sagaID == uuid.Nil {
		return fmt.Errorf("missing saga_id")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("missing saga status")
	}
	_, err := s.aggregate.TransitionStatus(ctx, domainagg.TransitionSagaStatusInput{
		SagaID:       sagaID,
		ToStatus:     status,
		TransitionAt: time.Now().UTC(),
	})
	return err
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
				logKind, actionKind, legacyKind := sagaActionKindForLog(a.Kind)
				s.log.Warn("saga action compensate failed",
					"saga_id", sagaID.String(),
					"action_id", a.ID.String(),
					"kind", logKind, // legacy field preserved for dashboard compatibility
					"action_kind", actionKind,
					"action_kind_raw", strings.TrimSpace(a.Kind),
					"action_kind_legacy", legacyKind,
					"vector_provider", s.vectorProvider,
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
	switch canonicalSagaActionKind(kind) {
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

	case SagaActionKindVectorDeleteIDs:
		if s.vec == nil {
			s.observeVectorStoreOperation("delete_ids", "error", 0)
			return fmt.Errorf("vector store unavailable (provider=%s)", s.vectorProvider)
		}
		var p struct {
			Namespace string   `json:"namespace"`
			IDs       []string `json:"ids"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		ns := strings.TrimSpace(p.Namespace)
		if ns == "" || len(p.IDs) == 0 {
			s.observeVectorStoreOperation("delete_ids", "skipped", 0)
			return nil
		}
		return s.vec.DeleteIDs(ctx, ns, p.IDs)

	default:
		return fmt.Errorf("unknown saga action kind: %s", kind)
	}
}

func canonicalSagaActionKind(kind string) string {
	k := strings.TrimSpace(strings.ToLower(kind))
	switch k {
	case SagaActionKindPineconeDeleteIDs, SagaActionKindVectorDeleteIDs:
		return SagaActionKindVectorDeleteIDs
	default:
		return k
	}
}

func sagaActionKindForLog(rawKind string) (kind string, actionKind string, legacyKind string) {
	raw := strings.TrimSpace(strings.ToLower(rawKind))
	actionKind = canonicalSagaActionKind(raw)
	if actionKind == SagaActionKindVectorDeleteIDs {
		// Keep existing `kind` filterable by legacy pinecone label for existing dashboards.
		return SagaActionKindPineconeDeleteIDs, actionKind, SagaActionKindPineconeDeleteIDs
	}
	if raw == "" {
		return "unknown", actionKind, ""
	}
	return raw, actionKind, ""
}

func (s *sagaService) observeVectorStoreOperation(operation, status string, dur time.Duration) {
	metrics := observability.Current()
	if metrics == nil {
		return
	}
	metrics.ObserveVectorStoreOperation(s.vectorProvider, operation, status, dur)
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

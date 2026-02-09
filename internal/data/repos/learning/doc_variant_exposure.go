package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DocVariantExposureRepo interface {
	Create(dbc dbctx.Context, row *types.DocVariantExposure) error
	ListUnevaluatedByUser(dbc dbctx.Context, userID uuid.UUID, pathID *uuid.UUID, cutoff time.Time, limit int) ([]*types.DocVariantExposure, error)
}

type docVariantExposureRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocVariantExposureRepo(db *gorm.DB, baseLog *logger.Logger) DocVariantExposureRepo {
	return &docVariantExposureRepo{db: db, log: baseLog.With("repo", "DocVariantExposureRepo")}
}

func (r *docVariantExposureRepo) Create(dbc dbctx.Context, row *types.DocVariantExposure) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	row.PolicyVersion = strings.TrimSpace(row.PolicyVersion)
	if row.PolicyVersion == "" {
		row.PolicyVersion = "base"
	}
	row.VariantKind = strings.TrimSpace(row.VariantKind)
	if row.VariantKind == "" {
		row.VariantKind = "base"
	}
	row.ExposureKind = strings.TrimSpace(row.ExposureKind)
	if row.ExposureKind == "" {
		row.ExposureKind = "base"
	}
	row.Source = strings.TrimSpace(row.Source)
	if row.Source == "" {
		row.Source = "api"
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *docVariantExposureRepo) ListUnevaluatedByUser(dbc dbctx.Context, userID uuid.UUID, pathID *uuid.UUID, cutoff time.Time, limit int) ([]*types.DocVariantExposure, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.DocVariantExposure{}
	if userID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 200
	}
	q := t.WithContext(dbc.Ctx).
		Model(&types.DocVariantExposure{}).
		Joins("LEFT JOIN doc_variant_outcome o ON o.exposure_id = doc_variant_exposure.id").
		Where("doc_variant_exposure.user_id = ?", userID).
		Where("o.id IS NULL")
	if pathID != nil && *pathID != uuid.Nil {
		q = q.Where("doc_variant_exposure.path_id = ?", *pathID)
	}
	if !cutoff.IsZero() {
		q = q.Where("doc_variant_exposure.created_at <= ?", cutoff)
	}
	if err := q.Order("doc_variant_exposure.created_at ASC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

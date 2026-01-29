package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type LearningArtifactRepo interface {
	GetByKey(dbc dbctx.Context, ownerUserID uuid.UUID, materialSetID uuid.UUID, pathID uuid.UUID, artifactType string) (*types.LearningArtifact, error)
	Upsert(dbc dbctx.Context, row *types.LearningArtifact) error
}

type learningArtifactRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningArtifactRepo(db *gorm.DB, baseLog *logger.Logger) LearningArtifactRepo {
	return &learningArtifactRepo{db: db, log: baseLog.With("repo", "LearningArtifactRepo")}
}

func (r *learningArtifactRepo) GetByKey(dbc dbctx.Context, ownerUserID uuid.UUID, materialSetID uuid.UUID, pathID uuid.UUID, artifactType string) (*types.LearningArtifact, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if ownerUserID == uuid.Nil || materialSetID == uuid.Nil || strings.TrimSpace(artifactType) == "" {
		return nil, nil
	}
	var row types.LearningArtifact
	if err := t.WithContext(dbc.Ctx).
		Where("owner_user_id = ? AND material_set_id = ? AND path_id = ? AND artifact_type = ?", ownerUserID, materialSetID, pathID, strings.TrimSpace(artifactType)).
		First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *learningArtifactRepo) Upsert(dbc dbctx.Context, row *types.LearningArtifact) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.OwnerUserID == uuid.Nil || row.MaterialSetID == uuid.Nil || strings.TrimSpace(row.ArtifactType) == "" {
		return nil
	}
	if row.PathID == uuid.Nil {
		row.PathID = uuid.Nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "owner_user_id"},
				{Name: "material_set_id"},
				{Name: "path_id"},
				{Name: "artifact_type"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"input_hash",
				"version",
				"metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}

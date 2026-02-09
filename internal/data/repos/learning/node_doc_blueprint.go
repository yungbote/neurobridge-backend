package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type LearningNodeDocBlueprintRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocBlueprint, error)
	GetByNodeAndVersion(dbc dbctx.Context, pathNodeID uuid.UUID, version string) (*types.LearningNodeDocBlueprint, error)
	GetLatestByNode(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.LearningNodeDocBlueprint, error)
	Upsert(dbc dbctx.Context, row *types.LearningNodeDocBlueprint) error
}

type learningNodeDocBlueprintRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeDocBlueprintRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocBlueprintRepo {
	return &learningNodeDocBlueprintRepo{db: db, log: baseLog.With("repo", "LearningNodeDocBlueprintRepo")}
}

func (r *learningNodeDocBlueprintRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocBlueprint, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.LearningNodeDocBlueprint
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *learningNodeDocBlueprintRepo) GetByNodeAndVersion(dbc dbctx.Context, pathNodeID uuid.UUID, version string) (*types.LearningNodeDocBlueprint, error) {
	if pathNodeID == uuid.Nil || version == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.LearningNodeDocBlueprint
	if err := t.WithContext(dbc.Ctx).
		First(&out, "path_node_id = ? AND blueprint_version = ?", pathNodeID, version).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *learningNodeDocBlueprintRepo) GetLatestByNode(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.LearningNodeDocBlueprint, error) {
	if pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.LearningNodeDocBlueprint
	if err := t.WithContext(dbc.Ctx).
		Where("path_node_id = ?", pathNodeID).
		Order("created_at DESC").
		Limit(1).
		Find(&out).Error; err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}

func (r *learningNodeDocBlueprintRepo) Upsert(dbc dbctx.Context, row *types.LearningNodeDocBlueprint) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_node_id"}, {Name: "blueprint_version"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"schema_version",
				"blueprint_json",
			}),
		}).
		Create(row).Error
}

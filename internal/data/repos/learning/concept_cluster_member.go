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

type ConceptClusterMemberRepo interface {
	Create(dbc dbctx.Context, rows []*types.ConceptClusterMember) ([]*types.ConceptClusterMember, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptClusterMember) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptClusterMember, error)
	GetByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) ([]*types.ConceptClusterMember, error)
	GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptClusterMember, error)

	Upsert(dbc dbctx.Context, row *types.ConceptClusterMember) error

	SoftDeleteByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) error
	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
}

type conceptClusterMemberRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptClusterMemberRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterMemberRepo {
	return &conceptClusterMemberRepo{db: db, log: baseLog.With("repo", "ConceptClusterMemberRepo")}
}

func (r *conceptClusterMemberRepo) Create(dbc dbctx.Context, rows []*types.ConceptClusterMember) ([]*types.ConceptClusterMember, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptClusterMember{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptClusterMemberRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptClusterMember) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "cluster_id"}, {Name: "concept_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *conceptClusterMemberRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) GetByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(clusterIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("cluster_id IN ?", clusterIDs).
		Order("cluster_id ASC, weight DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, weight DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) Upsert(dbc dbctx.Context, row *types.ConceptClusterMember) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ClusterID == uuid.Nil || row.ConceptID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "cluster_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"weight",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *conceptClusterMemberRepo) SoftDeleteByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(clusterIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("cluster_id IN ?", clusterIDs).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) FullDeleteByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(clusterIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("cluster_id IN ?", clusterIDs).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptClusterMember{}).Error
}

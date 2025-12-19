package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ConceptClusterMemberRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptClusterMember) ([]*types.ConceptClusterMember, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptClusterMember) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptClusterMember, error)
	GetByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) ([]*types.ConceptClusterMember, error)
	GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptClusterMember, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptClusterMember) error

	SoftDeleteByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) error
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type conceptClusterMemberRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptClusterMemberRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterMemberRepo {
	return &conceptClusterMemberRepo{db: db, log: baseLog.With("repo", "ConceptClusterMemberRepo")}
}

func (r *conceptClusterMemberRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptClusterMember) ([]*types.ConceptClusterMember, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptClusterMember{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptClusterMemberRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptClusterMember) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
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

func (r *conceptClusterMemberRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) GetByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(clusterIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("cluster_id IN ?", clusterIDs).
		Order("cluster_id ASC, weight DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptClusterMember, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptClusterMember
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, weight DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterMemberRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptClusterMember) error {
	t := tx
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

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "cluster_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"weight",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *conceptClusterMemberRepo) SoftDeleteByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(clusterIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("cluster_id IN ?", clusterIDs).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) FullDeleteByClusterIDs(ctx context.Context, tx *gorm.DB, clusterIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(clusterIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("cluster_id IN ?", clusterIDs).Delete(&types.ConceptClusterMember{}).Error
}

func (r *conceptClusterMemberRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptClusterMember{}).Error
}











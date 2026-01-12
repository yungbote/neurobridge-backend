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

type LibraryTaxonomyMembershipRepo interface {
	GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyMembership, error)
	GetByUserFacetAndPathIDs(dbc dbctx.Context, userID uuid.UUID, facet string, pathIDs []uuid.UUID) ([]*types.LibraryTaxonomyMembership, error)
	GetByUserFacetAndNodeIDs(dbc dbctx.Context, userID uuid.UUID, facet string, nodeIDs []uuid.UUID) ([]*types.LibraryTaxonomyMembership, error)
	UpsertMany(dbc dbctx.Context, rows []*types.LibraryTaxonomyMembership) error
	SoftDeleteByPathIDs(dbc dbctx.Context, userID uuid.UUID, facet string, pathIDs []uuid.UUID) error
}

type libraryTaxonomyMembershipRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryTaxonomyMembershipRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyMembershipRepo {
	return &libraryTaxonomyMembershipRepo{db: db, log: baseLog.With("repo", "LibraryTaxonomyMembershipRepo")}
}

func (r *libraryTaxonomyMembershipRepo) GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyMembership, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	var out []*types.LibraryTaxonomyMembership
	if userID == uuid.Nil || facet == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ?", userID, facet).
		Order("weight DESC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryTaxonomyMembershipRepo) GetByUserFacetAndPathIDs(dbc dbctx.Context, userID uuid.UUID, facet string, pathIDs []uuid.UUID) ([]*types.LibraryTaxonomyMembership, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	var out []*types.LibraryTaxonomyMembership
	if userID == uuid.Nil || facet == "" || len(pathIDs) == 0 {
		return out, nil
	}
	return out, t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ? AND path_id IN ?", userID, facet, pathIDs).
		Order("weight DESC, updated_at DESC").
		Find(&out).Error
}

func (r *libraryTaxonomyMembershipRepo) GetByUserFacetAndNodeIDs(dbc dbctx.Context, userID uuid.UUID, facet string, nodeIDs []uuid.UUID) ([]*types.LibraryTaxonomyMembership, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	var out []*types.LibraryTaxonomyMembership
	if userID == uuid.Nil || facet == "" || len(nodeIDs) == 0 {
		return out, nil
	}
	return out, t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ? AND node_id IN ?", userID, facet, nodeIDs).
		Order("weight DESC, updated_at DESC").
		Find(&out).Error
}

func (r *libraryTaxonomyMembershipRepo) UpsertMany(dbc dbctx.Context, rows []*types.LibraryTaxonomyMembership) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]*types.LibraryTaxonomyMembership, 0, len(rows))
	for _, m := range rows {
		if m == nil || m.UserID == uuid.Nil || strings.TrimSpace(m.Facet) == "" || m.PathID == uuid.Nil || m.NodeID == uuid.Nil {
			continue
		}
		if m.ID == uuid.Nil {
			m.ID = uuid.New()
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
		m.UpdatedAt = now
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_id"}, {Name: "node_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"user_id":     gorm.Expr("EXCLUDED.user_id"),
				"facet":       gorm.Expr("EXCLUDED.facet"),
				"weight":      gorm.Expr("EXCLUDED.weight"),
				"assigned_by": gorm.Expr("EXCLUDED.assigned_by"),
				"metadata":    gorm.Expr("EXCLUDED.metadata"),
				"version":     gorm.Expr("EXCLUDED.version"),
				"updated_at":  now,
				"deleted_at":  nil,
			}),
		}).
		Create(&out).Error
}

func (r *libraryTaxonomyMembershipRepo) SoftDeleteByPathIDs(dbc dbctx.Context, userID uuid.UUID, facet string, pathIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	if userID == uuid.Nil || facet == "" || len(pathIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ? AND path_id IN ?", userID, facet, pathIDs).
		Delete(&types.LibraryTaxonomyMembership{}).Error
}

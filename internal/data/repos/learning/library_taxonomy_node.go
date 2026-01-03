package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LibraryTaxonomyNodeRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LibraryTaxonomyNode, error)
	GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyNode, error)
	GetByUserFacetKeys(dbc dbctx.Context, userID uuid.UUID, facet string, keys []string) ([]*types.LibraryTaxonomyNode, error)
	Upsert(dbc dbctx.Context, node *types.LibraryTaxonomyNode) error
	UpsertMany(dbc dbctx.Context, nodes []*types.LibraryTaxonomyNode) error
}

type libraryTaxonomyNodeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryTaxonomyNodeRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyNodeRepo {
	return &libraryTaxonomyNodeRepo{db: db, log: baseLog.With("repo", "LibraryTaxonomyNodeRepo")}
}

func (r *libraryTaxonomyNodeRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LibraryTaxonomyNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil, nil
	}
	var row types.LibraryTaxonomyNode
	if err := t.WithContext(dbc.Ctx).Where("id = ?", id).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *libraryTaxonomyNodeRepo) GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	var out []*types.LibraryTaxonomyNode
	if userID == uuid.Nil || facet == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ?", userID, facet).
		Order("kind ASC, name ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryTaxonomyNodeRepo) GetByUserFacetKeys(dbc dbctx.Context, userID uuid.UUID, facet string, keys []string) ([]*types.LibraryTaxonomyNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	out := make([]*types.LibraryTaxonomyNode, 0)
	if userID == uuid.Nil || facet == "" || len(keys) == 0 {
		return out, nil
	}
	clean := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		clean = append(clean, k)
	}
	if len(clean) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ? AND key IN ?", userID, facet, clean).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryTaxonomyNodeRepo) Upsert(dbc dbctx.Context, node *types.LibraryTaxonomyNode) error {
	return r.UpsertMany(dbc, []*types.LibraryTaxonomyNode{node})
}

func (r *libraryTaxonomyNodeRepo) UpsertMany(dbc dbctx.Context, nodes []*types.LibraryTaxonomyNode) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(nodes) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]*types.LibraryTaxonomyNode, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.UserID == uuid.Nil || strings.TrimSpace(n.Facet) == "" || strings.TrimSpace(n.Key) == "" {
			continue
		}
		if n.ID == uuid.Nil {
			n.ID = uuid.New()
		}
		if n.CreatedAt.IsZero() {
			n.CreatedAt = now
		}
		n.UpdatedAt = now
		rows = append(rows, n)
	}
	if len(rows) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "facet"}, {Name: "key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"kind":        gorm.Expr("EXCLUDED.kind"),
				"name":        gorm.Expr("EXCLUDED.name"),
				"description": gorm.Expr("EXCLUDED.description"),
				"embedding":   gorm.Expr("EXCLUDED.embedding"),
				"stats":       gorm.Expr("EXCLUDED.stats"),
				"version":     gorm.Expr("EXCLUDED.version"),
				"updated_at":  now,
				"deleted_at":  nil,
			}),
		}).
		Create(&rows).Error
}


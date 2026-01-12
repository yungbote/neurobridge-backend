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

type LibraryTaxonomyEdgeRepo interface {
	GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyEdge, error)
	GetByUserFacetKind(dbc dbctx.Context, userID uuid.UUID, facet string, kind string) ([]*types.LibraryTaxonomyEdge, error)
	UpsertMany(dbc dbctx.Context, edges []*types.LibraryTaxonomyEdge) error
}

type libraryTaxonomyEdgeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryTaxonomyEdgeRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyEdgeRepo {
	return &libraryTaxonomyEdgeRepo{db: db, log: baseLog.With("repo", "LibraryTaxonomyEdgeRepo")}
}

func (r *libraryTaxonomyEdgeRepo) GetByUserFacet(dbc dbctx.Context, userID uuid.UUID, facet string) ([]*types.LibraryTaxonomyEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	var out []*types.LibraryTaxonomyEdge
	if userID == uuid.Nil || facet == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ?", userID, facet).
		Order("kind ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryTaxonomyEdgeRepo) GetByUserFacetKind(dbc dbctx.Context, userID uuid.UUID, facet string, kind string) ([]*types.LibraryTaxonomyEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	facet = strings.TrimSpace(facet)
	kind = strings.TrimSpace(kind)
	var out []*types.LibraryTaxonomyEdge
	if userID == uuid.Nil || facet == "" || kind == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND facet = ? AND kind = ?", userID, facet, kind).
		Order("created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryTaxonomyEdgeRepo) UpsertMany(dbc dbctx.Context, edges []*types.LibraryTaxonomyEdge) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(edges) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]*types.LibraryTaxonomyEdge, 0, len(edges))
	for _, e := range edges {
		if e == nil || e.UserID == uuid.Nil || strings.TrimSpace(e.Facet) == "" || strings.TrimSpace(e.Kind) == "" || e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
			continue
		}
		if e.ID == uuid.Nil {
			e.ID = uuid.New()
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = now
		}
		e.UpdatedAt = now
		rows = append(rows, e)
	}
	if len(rows) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "kind"}, {Name: "from_node_id"}, {Name: "to_node_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"user_id":    gorm.Expr("EXCLUDED.user_id"),
				"facet":      gorm.Expr("EXCLUDED.facet"),
				"weight":     gorm.Expr("EXCLUDED.weight"),
				"metadata":   gorm.Expr("EXCLUDED.metadata"),
				"version":    gorm.Expr("EXCLUDED.version"),
				"updated_at": now,
				"deleted_at": nil,
			}),
		}).
		Create(&rows).Error
}

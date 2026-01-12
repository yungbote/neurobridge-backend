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

type LibraryPathEmbeddingRepo interface {
	GetByUserAndPathIDs(dbc dbctx.Context, userID uuid.UUID, pathIDs []uuid.UUID) ([]*types.LibraryPathEmbedding, error)
	UpsertMany(dbc dbctx.Context, rows []*types.LibraryPathEmbedding) error
}

type libraryPathEmbeddingRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryPathEmbeddingRepo(db *gorm.DB, baseLog *logger.Logger) LibraryPathEmbeddingRepo {
	return &libraryPathEmbeddingRepo{db: db, log: baseLog.With("repo", "LibraryPathEmbeddingRepo")}
}

func (r *libraryPathEmbeddingRepo) GetByUserAndPathIDs(dbc dbctx.Context, userID uuid.UUID, pathIDs []uuid.UUID) ([]*types.LibraryPathEmbedding, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LibraryPathEmbedding
	if userID == uuid.Nil || len(pathIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_id IN ?", userID, pathIDs).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *libraryPathEmbeddingRepo) UpsertMany(dbc dbctx.Context, rows []*types.LibraryPathEmbedding) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]*types.LibraryPathEmbedding, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.Model == "" || row.SourcesHash == "" {
			continue
		}
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		row.UpdatedAt = now
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "path_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"model":        gorm.Expr("EXCLUDED.model"),
				"embedding":    gorm.Expr("EXCLUDED.embedding"),
				"sources_hash": gorm.Expr("EXCLUDED.sources_hash"),
				"updated_at":   now,
				"deleted_at":   nil,
			}),
		}).
		Create(&out).Error
}

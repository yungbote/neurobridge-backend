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

type UserTestletStateRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserTestletState) error
	ListByUserAndTestletIDs(dbc dbctx.Context, userID uuid.UUID, ids []string) ([]*types.UserTestletState, error)
}

type userTestletStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserTestletStateRepo(db *gorm.DB, baseLog *logger.Logger) UserTestletStateRepo {
	return &userTestletStateRepo{db: db, log: baseLog.With("repo", "UserTestletStateRepo")}
}

func (r *userTestletStateRepo) Upsert(dbc dbctx.Context, row *types.UserTestletState) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || strings.TrimSpace(row.TestletID) == "" {
		return nil
	}
	row.UpdatedAt = time.Now().UTC()
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "testlet_id"},
				{Name: "testlet_type"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"attempts",
				"correct",
				"beta_a",
				"beta_b",
				"ema",
				"last_seen_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userTestletStateRepo) ListByUserAndTestletIDs(dbc dbctx.Context, userID uuid.UUID, ids []string) ([]*types.UserTestletState, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.UserTestletState{}
	if userID == uuid.Nil || len(ids) == 0 {
		return out, nil
	}
	clean := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND testlet_id IN ?", userID, clean).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

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

type ItemCalibrationRepo interface {
	Upsert(dbc dbctx.Context, row *types.ItemCalibration) error
	ListByItemIDs(dbc dbctx.Context, itemIDs []string) ([]*types.ItemCalibration, error)
}

type itemCalibrationRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewItemCalibrationRepo(db *gorm.DB, baseLog *logger.Logger) ItemCalibrationRepo {
	return &itemCalibrationRepo{db: db, log: baseLog.With("repo", "ItemCalibrationRepo")}
}

func (r *itemCalibrationRepo) Upsert(dbc dbctx.Context, row *types.ItemCalibration) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	row.ItemID = strings.TrimSpace(row.ItemID)
	row.ItemType = strings.TrimSpace(row.ItemType)
	if row.ItemID == "" {
		return nil
	}
	if row.ItemType == "" {
		row.ItemType = "question"
	}

	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "item_id"}, {Name: "item_type"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"concept_id",
				"difficulty",
				"discrimination",
				"guess",
				"slip",
				"count",
				"correct",
				"last_event_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *itemCalibrationRepo) ListByItemIDs(dbc dbctx.Context, itemIDs []string) ([]*types.ItemCalibration, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.ItemCalibration{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	clean := make([]string, 0, len(itemIDs))
	seen := map[string]bool{}
	for _, id := range itemIDs {
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
		Where("item_id IN ?", clean).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

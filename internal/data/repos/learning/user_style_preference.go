package learning

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserStylePreferenceRepo interface {
	// reward in [-1..+1]; if binary != nil we also update Beta(a,b)
	UpsertEMA(dbc dbctx.Context, userID uuid.UUID, conceptID *uuid.UUID, modality, variant string, reward float64, binary *bool) error
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserStylePreference, error)
	ListGlobalByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.UserStylePreference, error)
}

type userStylePreferenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) UserStylePreferenceRepo {
	return &userStylePreferenceRepo{
		db:  db,
		log: baseLog.With("repo", "UserStylePreferenceRepo"),
	}
}

func clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}

func (r *userStylePreferenceRepo) UpsertEMA(dbc dbctx.Context, userID uuid.UUID, conceptID *uuid.UUID, modality, variant string, reward float64, binary *bool) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil
	}

	modality = strings.TrimSpace(modality)
	variant = strings.TrimSpace(variant)
	if modality == "" {
		return nil
	}
	if variant == "" {
		variant = "default"
	}

	reward = clamp(reward, -1, 1)
	now := time.Now().UTC()

	var row types.UserStylePreference
	err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IS NOT DISTINCT FROM ? AND modality = ? AND variant = ?",
			userID, conceptID, modality, variant,
		).
		First(&row).Error

	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if row.ID == uuid.Nil {
		row.ID = uuid.New()
		row.UserID = userID
		row.ConceptID = conceptID
		row.Modality = modality
		row.Variant = variant
		row.EMA = 0
		row.N = 0
		row.A = 1
		row.B = 1
	}

	n := row.N + 1
	alpha := 2.0 / float64(n+1)
	if alpha > 0.25 {
		alpha = 0.25
	}
	row.EMA = row.EMA + alpha*(reward-row.EMA)
	row.N = n

	if binary != nil {
		if *binary {
			row.A += 1
		} else {
			row.B += 1
		}
	}

	row.LastObservedAt = &now
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "concept_id"},
				{Name: "modality"},
				{Name: "variant"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"ema", "n", "a", "b", "last_observed_at", "updated_at",
			}),
		}).
		Create(&row).Error
}

func (r *userStylePreferenceRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserStylePreference, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.UserStylePreference{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IN ?", userID, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userStylePreferenceRepo) ListGlobalByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.UserStylePreference, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.UserStylePreference{}
	if userID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IS NULL", userID).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

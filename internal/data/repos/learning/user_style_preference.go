package learning

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserStylePreferenceRepo interface {
	// reward in [-1..+1]; if binary != nil we also update Beta(a,b)
	UpsertEMA(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID *uuid.UUID, modality, variant string, reward float64, binary *bool) error
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

func (r *userStylePreferenceRepo) UpsertEMA(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID *uuid.UUID, modality, variant string, reward float64, binary *bool) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil
	}
	modality = stringsTrim(modality)
	variant = stringsTrim(variant)
	if modality == "" {
		return nil
	}
	if variant == "" {
		variant = "default"
	}

	reward = clamp(reward, -1, 1)
	now := time.Now().UTC()

	// Load existing (simple & safe)
	var row types.UserStylePreference
	err := t.WithContext(ctx).
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
	row.UpdatedAt = now

	if binary != nil {
		if *binary {
			row.A += 1
		} else {
			row.B += 1
		}
	}

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "concept_id"},
				{Name: "modality"},
				{Name: "variant"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"ema", "n", "a", "b", "updated_at"}),
		}).
		Create(&row).Error
}

func stringsTrim(s string) string {
	// avoid adding strings import in multiple files if you prefer; keep local
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\n' || last == '\t' || last == '\r' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}

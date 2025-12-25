package learning

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LearningDocGenerationRunRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.LearningDocGenerationRun) ([]*types.LearningDocGenerationRun, error)
}

type learningDocGenerationRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningDocGenerationRunRepo(db *gorm.DB, baseLog *logger.Logger) LearningDocGenerationRunRepo {
	return &learningDocGenerationRunRepo{db: db, log: baseLog.With("repo", "LearningDocGenerationRunRepo")}
}

func (r *learningDocGenerationRunRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.LearningDocGenerationRun) ([]*types.LearningDocGenerationRun, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.LearningDocGenerationRun{}, nil
	}
	// Ensure IDs for inserts.
	for _, row := range rows {
		if row != nil && row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

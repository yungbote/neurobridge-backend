package learning

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LearningDocGenerationRunRepo interface {
	Create(dbc dbctx.Context, rows []*types.LearningDocGenerationRun) ([]*types.LearningDocGenerationRun, error)
}

type learningDocGenerationRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningDocGenerationRunRepo(db *gorm.DB, baseLog *logger.Logger) LearningDocGenerationRunRepo {
	return &learningDocGenerationRunRepo{db: db, log: baseLog.With("repo", "LearningDocGenerationRunRepo")}
}

func (r *learningDocGenerationRunRepo) Create(dbc dbctx.Context, rows []*types.LearningDocGenerationRun) ([]*types.LearningDocGenerationRun, error) {
	t := dbc.Tx
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
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

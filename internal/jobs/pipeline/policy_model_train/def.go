package policy_model_train

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db     *gorm.DB
	log    *logger.Logger
	traces repos.DecisionTraceRepo
	models repos.ModelSnapshotRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	traces repos.DecisionTraceRepo,
	models repos.ModelSnapshotRepo,
) *Pipeline {
	return &Pipeline{
		db:     db,
		log:    baseLog.With("job", "policy_model_train"),
		traces: traces,
		models: models,
	}
}

func (p *Pipeline) Type() string { return "policy_model_train" }

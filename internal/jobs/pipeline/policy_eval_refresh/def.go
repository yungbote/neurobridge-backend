package policy_eval_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db     *gorm.DB
	log    *logger.Logger
	traces repos.DecisionTraceRepo
	evals  repos.PolicyEvalSnapshotRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	traces repos.DecisionTraceRepo,
	evals repos.PolicyEvalSnapshotRepo,
) *Pipeline {
	return &Pipeline{
		db:     db,
		log:    baseLog.With("job", "policy_eval_refresh"),
		traces: traces,
		evals:  evals,
	}
}

func (p *Pipeline) Type() string { return "policy_eval_refresh" }

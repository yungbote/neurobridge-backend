package saga_cleanup

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db     *gorm.DB
	log    *logger.Logger
	sagas  repos.SagaRunRepo
	saga   services.SagaService
	bucket gcp.BucketService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	sagas repos.SagaRunRepo,
	saga services.SagaService,
	bucket gcp.BucketService,
) *Pipeline {
	return &Pipeline{
		db:     db,
		log:    baseLog.With("job", "saga_cleanup"),
		sagas:  sagas,
		saga:   saga,
		bucket: bucket,
	}
}

func (p *Pipeline) Type() string { return "saga_cleanup" }

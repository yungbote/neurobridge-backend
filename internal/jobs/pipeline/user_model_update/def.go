package user_model_update

import (
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
)

type UserModelUpdatePipeline struct {
	db								*gorm.DB
	log								*logger.Logger
	userEventRepo			repos.UserEventRepo
	cursorRepo				repos.UserEventCursorRepo
	conceptStateRepo	repos.UserConceptStateRepo
	stylePrefRepo			repos.UserStylePreferenceRepo
	jobRunRepo				repos.JobRunRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	userEventRepo repos.UserEventRepo,
	cursorRepo repos.UserEventCursorRepo,
	conceptStateRepo repos.UserConceptStateRepo,
	stylePrefRepo repos.UserStylePreferenceRepo,
	jobRunRepo repos.JobRunRepo,
) *UserModelUpdatePipeline {
	return &UserModelUpdatePipeline{
		db:      db,
		log:     baseLog.With("job", "user_model_update"),
		userEventRepo:   userEventRepo,
		cursorRepo:      cursorRepo,
		conceptStateRepo: conceptStateRepo,
		stylePrefRepo:   stylePrefRepo,
		jobRunRepo: jobRunRepo,
	}
}

func (p *UserModelUpdatePipeline) Type() string { return "user_model_update" }











package repos

import (
	"github.com/yungbote/neurobridge-backend/internal/data/repos/auth"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/jobs"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/learning"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/materials"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/user"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type UserRepo = user.UserRepo
type UserTokenRepo = auth.UserTokenRepo

type AssetRepo = materials.AssetRepo
type MaterialSetRepo = materials.MaterialSetRepo
type MaterialFileRepo = materials.MaterialFileRepo
type MaterialChunkRepo = materials.MaterialChunkRepo
type MaterialAssetRepo = materials.MaterialAssetRepo

type CourseRepo = learning.CourseRepo
type CourseModuleRepo = learning.CourseModuleRepo
type CourseConceptRepo = learning.CourseConceptRepo
type CourseTagRepo = learning.CourseTagRepo
type CourseBlueprintRepo = learning.CourseBlueprintRepo

type LessonRepo = learning.LessonRepo
type LessonVariantRepo = learning.LessonVariantRepo
type LessonCitationRepo = learning.LessonCitationRepo
type LessonConceptRepo = learning.LessonConceptRepo
type LessonAssetRepo = learning.LessonAssetRepo
type LessonProgressRepo = learning.LessonProgressRepo

type QuizQuestionRepo = learning.QuizQuestionRepo
type QuizAttemptRepo = learning.QuizAttemptRepo

type LearningProfileRepo = learning.LearningProfileRepo
type TopicMasteryRepo = learning.TopicMasteryRepo
type TopicStylePreferenceRepo = learning.TopicStylePreferenceRepo
type UserConceptStateRepo = learning.UserConceptStateRepo
type UserStylePreferenceRepo = learning.UserStylePreferenceRepo
type UserEventRepo = learning.UserEventRepo
type UserEventCursorRepo = learning.UserEventCursorRepo

type ConceptRepo = learning.ConceptRepo
type ActivityRepo = learning.ActivityRepo
type ActivityVariantRepo = learning.ActivityVariantRepo
type ActivityConceptRepo = learning.ActivityConceptRepo
type ActivityCitationRepo = learning.ActivityCitationRepo

type PathRepo = learning.PathRepo
type PathNodeRepo = learning.PathNodeRepo
type PathNodeActivityRepo = learning.PathNodeActivityRepo

type JobRunRepo = jobs.JobRunRepo

func NewUserRepo(db *gorm.DB, baseLog *logger.Logger) UserRepo { return user.NewUserRepo(db, baseLog) }
func NewUserTokenRepo(db *gorm.DB, baseLog *logger.Logger) UserTokenRepo {
	return auth.NewUserTokenRepo(db, baseLog)
}

func NewAssetRepo(db *gorm.DB, baseLog *logger.Logger) AssetRepo {
	return materials.NewAssetRepo(db, baseLog)
}
func NewMaterialSetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetRepo {
	return materials.NewMaterialSetRepo(db, baseLog)
}
func NewMaterialFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileRepo {
	return materials.NewMaterialFileRepo(db, baseLog)
}
func NewMaterialChunkRepo(db *gorm.DB, baseLog *logger.Logger) MaterialChunkRepo {
	return materials.NewMaterialChunkRepo(db, baseLog)
}
func NewMaterialAssetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialAssetRepo {
	return materials.NewMaterialAssetRepo(db, baseLog)
}

func NewCourseRepo(db *gorm.DB, baseLog *logger.Logger) CourseRepo {
	return learning.NewCourseRepo(db, baseLog)
}
func NewCourseModuleRepo(db *gorm.DB, baseLog *logger.Logger) CourseModuleRepo {
	return learning.NewCourseModuleRepo(db, baseLog)
}
func NewCourseConceptRepo(db *gorm.DB, baseLog *logger.Logger) CourseConceptRepo {
	return learning.NewCourseConceptRepo(db, baseLog)
}
func NewCourseTagRepo(db *gorm.DB, baseLog *logger.Logger) CourseTagRepo {
	return learning.NewCourseTagRepo(db, baseLog)
}
func NewCourseBlueprintRepo(db *gorm.DB, baseLog *logger.Logger) CourseBlueprintRepo {
	return learning.NewCourseBlueprintRepo(db, baseLog)
}

func NewLessonRepo(db *gorm.DB, baseLog *logger.Logger) LessonRepo {
	return learning.NewLessonRepo(db, baseLog)
}
func NewLessonVariantRepo(db *gorm.DB, baseLog *logger.Logger) LessonVariantRepo {
	return learning.NewLessonVariantRepo(db, baseLog)
}
func NewLessonCitationRepo(db *gorm.DB, baseLog *logger.Logger) LessonCitationRepo {
	return learning.NewLessonCitationRepo(db, baseLog)
}
func NewLessonConceptRepo(db *gorm.DB, baseLog *logger.Logger) LessonConceptRepo {
	return learning.NewLessonConceptRepo(db, baseLog)
}
func NewLessonAssetRepo(db *gorm.DB, baseLog *logger.Logger) LessonAssetRepo {
	return learning.NewLessonAssetRepo(db, baseLog)
}
func NewLessonProgressRepo(db *gorm.DB, baseLog *logger.Logger) LessonProgressRepo {
	return learning.NewLessonProgressRepo(db, baseLog)
}

func NewQuizQuestionRepo(db *gorm.DB, baseLog *logger.Logger) QuizQuestionRepo {
	return learning.NewQuizQuestionRepo(db, baseLog)
}
func NewQuizAttemptRepo(db *gorm.DB, baseLog *logger.Logger) QuizAttemptRepo {
	return learning.NewQuizAttemptRepo(db, baseLog)
}

func NewLearningProfileRepo(db *gorm.DB, baseLog *logger.Logger) LearningProfileRepo {
	return learning.NewLearningProfileRepo(db, baseLog)
}
func NewTopicMasteryRepo(db *gorm.DB, baseLog *logger.Logger) TopicMasteryRepo {
	return learning.NewTopicMasteryRepo(db, baseLog)
}
func NewTopicStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) TopicStylePreferenceRepo {
	return learning.NewTopicStylePreferenceRepo(db, baseLog)
}
func NewUserConceptStateRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptStateRepo {
	return learning.NewUserConceptStateRepo(db, baseLog)
}
func NewUserStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) UserStylePreferenceRepo {
	return learning.NewUserStylePreferenceRepo(db, baseLog)
}
func NewUserEventRepo(db *gorm.DB, baseLog *logger.Logger) UserEventRepo {
	return learning.NewUserEventRepo(db, baseLog)
}
func NewUserEventCursorRepo(db *gorm.DB, baseLog *logger.Logger) UserEventCursorRepo {
	return learning.NewUserEventCursorRepo(db, baseLog)
}

func NewConceptRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepo {
	return learning.NewConceptRepo(db, baseLog)
}
func NewActivityRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRepo {
	return learning.NewActivityRepo(db, baseLog)
}
func NewActivityVariantRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantRepo {
	return learning.NewActivityVariantRepo(db, baseLog)
}
func NewActivityConceptRepo(db *gorm.DB, baseLog *logger.Logger) ActivityConceptRepo {
	return learning.NewActivityConceptRepo(db, baseLog)
}
func NewActivityCitationRepo(db *gorm.DB, baseLog *logger.Logger) ActivityCitationRepo {
	return learning.NewActivityCitationRepo(db, baseLog)
}

func NewPathRepo(db *gorm.DB, baseLog *logger.Logger) PathRepo {
	return learning.NewPathRepo(db, baseLog)
}
func NewPathNodeRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeRepo {
	return learning.NewPathNodeRepo(db, baseLog)
}
func NewPathNodeActivityRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeActivityRepo {
	return learning.NewPathNodeActivityRepo(db, baseLog)
}

func NewJobRunRepo(db *gorm.DB, baseLog *logger.Logger) JobRunRepo {
	return jobs.NewJobRunRepo(db, baseLog)
}

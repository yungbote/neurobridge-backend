package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserEventRepo interface {
	Create(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) ([]*types.UserEvent, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) (int, error)

	ListAfterCursor(ctx context.Context, tx *gorm.DB, userID uuid.UUID, afterCreatedAt *time.Time, afterID *uuid.UUID, limit int) ([]*types.UserEvent, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserEvent, error)
	GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.UserEvent, error)
	GetByUserAndCourseID(ctx context.Context, tx *gorm.DB, userID, courseID uuid.UUID) ([]*types.UserEvent, error)

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type userEventRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserEventRepo(db *gorm.DB, baseLog *logger.Logger) UserEventRepo {
	return &userEventRepo{db: db, log: baseLog.With("repo", "UserEventRepo")}
}

func (r *userEventRepo) Create(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) ([]*types.UserEvent, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(events) == 0 {
		return []*types.UserEvent{}, nil
	}
	if err := t.WithContext(ctx).Create(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (r *userEventRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(events) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "client_event_id"}},
			DoNothing: true,
		}).
		Create(&events)

	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *userEventRepo) ListAfterCursor(ctx context.Context, tx *gorm.DB, userID uuid.UUID, afterCreatedAt *time.Time, afterID *uuid.UUID, limit int) ([]*types.UserEvent, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return []*types.UserEvent{}, nil
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	q := t.WithContext(ctx).Model(&types.UserEvent{}).Where("user_id = ?", userID)

	// tie-safe cursor: (created_at, id)
	if afterCreatedAt != nil {
		id := uuid.Nil
		if afterID != nil {
			id = *afterID
		}
		q = q.Where("(created_at > ?) OR (created_at = ? AND id > ?)", *afterCreatedAt, *afterCreatedAt, id)
	}

	var out []*types.UserEvent
	if err := q.Order("created_at ASC, id ASC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userEventRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserEvent, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserEvent
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userEventRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.UserEvent, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserEvent
	if userID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userEventRepo) GetByUserAndCourseID(ctx context.Context, tx *gorm.DB, userID, courseID uuid.UUID) ([]*types.UserEvent, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserEvent
	if userID == uuid.Nil || courseID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id = ? AND course_id = ?", userID, courseID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userEventRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.UserEvent{}).Error
}

func (r *userEventRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.UserEvent{}).Error
}

// -------------------- Cursor repo (per consumer) --------------------

type UserEventCursorRepo interface {
	Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, consumer string) (*types.UserEventCursor, error)
	Upsert(ctx context.Context, tx *gorm.DB, row *types.UserEventCursor) error
}

type userEventCursorRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserEventCursorRepo(db *gorm.DB, baseLog *logger.Logger) UserEventCursorRepo {
	return &userEventCursorRepo{
		db:  db,
		log: baseLog.With("repo", "UserEventCursorRepo"),
	}
}

func (r *userEventCursorRepo) Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, consumer string) (*types.UserEventCursor, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || consumer == "" {
		return nil, nil
	}

	var row types.UserEventCursor
	err := t.WithContext(ctx).
		Where("user_id = ? AND consumer = ?", userID, consumer).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *userEventCursorRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.UserEventCursor) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.Consumer == "" {
		return nil
	}

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "consumer"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"last_created_at",
				"last_event_id",
				"updated_at",
			}),
		}).
		Create(row).Error
}

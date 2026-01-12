package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type UserSessionStateRepo interface {
	Ensure(dbc dbctx.Context, userID, sessionID uuid.UUID) error
	GetBySessionID(dbc dbctx.Context, sessionID uuid.UUID) (*types.UserSessionState, error)
	UpdateFields(dbc dbctx.Context, sessionID uuid.UUID, updates map[string]any) error
}

type userSessionStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserSessionStateRepo(db *gorm.DB, baseLog *logger.Logger) UserSessionStateRepo {
	return &userSessionStateRepo{
		db:  db,
		log: baseLog.With("repo", "UserSessionStateRepo"),
	}
}

func (r *userSessionStateRepo) Ensure(dbc dbctx.Context, userID, sessionID uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return nil
	}

	now := time.Now().UTC()
	row := &types.UserSessionState{
		SessionID:  sessionID,
		UserID:     userID,
		LastSeenAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error
}

func (r *userSessionStateRepo) GetBySessionID(dbc dbctx.Context, sessionID uuid.UUID) (*types.UserSessionState, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if sessionID == uuid.Nil {
		return nil, nil
	}
	var row types.UserSessionState
	if err := t.WithContext(dbc.Ctx).
		Where("session_id = ?", sessionID).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.SessionID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userSessionStateRepo) UpdateFields(dbc dbctx.Context, sessionID uuid.UUID, updates map[string]any) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if sessionID == uuid.Nil {
		return nil
	}
	if updates == nil {
		updates = map[string]any{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}
	return t.WithContext(dbc.Ctx).
		Model(&types.UserSessionState{}).
		Where("session_id = ?", sessionID).
		Updates(updates).Error
}

package user

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type UserGazeBlockStatRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserGazeBlockStat) error
	GetByUserSessionBlock(dbc dbctx.Context, userID, sessionID uuid.UUID, blockID string) (*types.UserGazeBlockStat, error)
}

type userGazeBlockStatRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserGazeBlockStatRepo(db *gorm.DB, baseLog *logger.Logger) UserGazeBlockStatRepo {
	return &userGazeBlockStatRepo{db: db, log: baseLog.With("repo", "UserGazeBlockStatRepo")}
}

func (r *userGazeBlockStatRepo) Upsert(dbc dbctx.Context, row *types.UserGazeBlockStat) error {
	if row == nil {
		return nil
	}
	return r.db.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "session_id"}, {Name: "block_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"path_id", "path_node_id", "fixation_ms", "fixation_count", "read_credit", "last_seen_at", "metadata", "updated_at"}),
		}).
		Create(row).Error
}

func (r *userGazeBlockStatRepo) GetByUserSessionBlock(dbc dbctx.Context, userID, sessionID uuid.UUID, blockID string) (*types.UserGazeBlockStat, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil || blockID == "" {
		return nil, nil
	}
	var row types.UserGazeBlockStat
	err := r.db.WithContext(dbc.Ctx).
		Where("user_id = ? AND session_id = ? AND block_id = ?", userID, sessionID, blockID).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

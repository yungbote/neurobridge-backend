package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type PathRunRepo interface {
	GetByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID) (*types.PathRun, error)
	Upsert(dbc dbctx.Context, row *types.PathRun) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]any) error
}

type NodeRunRepo interface {
	GetByUserAndNodeID(dbc dbctx.Context, userID uuid.UUID, nodeID uuid.UUID) (*types.NodeRun, error)
	Upsert(dbc dbctx.Context, row *types.NodeRun) error
}

type ActivityRunRepo interface {
	GetByUserAndActivityID(dbc dbctx.Context, userID uuid.UUID, activityID uuid.UUID) (*types.ActivityRun, error)
	Upsert(dbc dbctx.Context, row *types.ActivityRun) error
}

type PathRunTransitionRepo interface {
	Create(dbc dbctx.Context, row *types.PathRunTransition) error
	ExistsByEventID(dbc dbctx.Context, userID uuid.UUID, eventID uuid.UUID) (bool, error)
}

type pathRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

type nodeRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

type activityRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

type pathRunTransitionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathRunRepo(db *gorm.DB, baseLog *logger.Logger) PathRunRepo {
	return &pathRunRepo{db: db, log: baseLog.With("repo", "PathRunRepo")}
}

func NewNodeRunRepo(db *gorm.DB, baseLog *logger.Logger) NodeRunRepo {
	return &nodeRunRepo{db: db, log: baseLog.With("repo", "NodeRunRepo")}
}

func NewActivityRunRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRunRepo {
	return &activityRunRepo{db: db, log: baseLog.With("repo", "ActivityRunRepo")}
}

func NewPathRunTransitionRepo(db *gorm.DB, baseLog *logger.Logger) PathRunTransitionRepo {
	return &pathRunTransitionRepo{db: db, log: baseLog.With("repo", "PathRunTransitionRepo")}
}

func (r *pathRunRepo) GetByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID) (*types.PathRun, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || pathID == uuid.Nil {
		return nil, nil
	}
	var row types.PathRun
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_id = ?", userID, pathID).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *pathRunRepo) Upsert(dbc dbctx.Context, row *types.PathRun) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "path_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"state",
				"active_node_id",
				"active_activity_id",
				"strategy",
				"metadata",
				"last_event_id",
				"last_event_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *pathRunRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]any) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil || len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now().UTC()
	return t.WithContext(dbc.Ctx).Model(&types.PathRun{}).Where("id = ?", id).Updates(updates).Error
}

func (r *nodeRunRepo) GetByUserAndNodeID(dbc dbctx.Context, userID uuid.UUID, nodeID uuid.UUID) (*types.NodeRun, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || nodeID == uuid.Nil {
		return nil, nil
	}
	var row types.NodeRun
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND node_id = ?", userID, nodeID).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *nodeRunRepo) Upsert(dbc dbctx.Context, row *types.NodeRun) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.NodeID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "node_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"state",
				"path_id",
				"started_at",
				"completed_at",
				"last_seen_at",
				"attempt_count",
				"last_score",
				"metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityRunRepo) GetByUserAndActivityID(dbc dbctx.Context, userID uuid.UUID, activityID uuid.UUID) (*types.ActivityRun, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || activityID == uuid.Nil {
		return nil, nil
	}
	var row types.ActivityRun
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND activity_id = ?", userID, activityID).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *activityRunRepo) Upsert(dbc dbctx.Context, row *types.ActivityRun) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.ActivityID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "activity_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"state",
				"path_id",
				"node_id",
				"attempts",
				"last_score",
				"last_attempt_at",
				"completed_at",
				"metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *pathRunTransitionRepo) Create(dbc dbctx.Context, row *types.PathRunTransition) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.EventID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.OccurredAt.IsZero() {
		row.OccurredAt = time.Now().UTC()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_id"}},
			DoNothing: true,
		}).
		Create(row).Error
}

func (r *pathRunTransitionRepo) ExistsByEventID(dbc dbctx.Context, userID uuid.UUID, eventID uuid.UUID) (bool, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || eventID == uuid.Nil {
		return false, nil
	}
	var count int64
	if err := t.WithContext(dbc.Ctx).
		Model(&types.PathRunTransition{}).
		Where("user_id = ? AND event_id = ?", userID, eventID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

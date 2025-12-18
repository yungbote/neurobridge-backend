package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestUserEventRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewUserEventRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "usereventrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)

	now := time.Now().UTC()
	e1 := &types.UserEvent{
		ID:            uuid.New(),
		UserID:        u.ID,
		ClientEventID: "evt-1",
		OccurredAt:    now.Add(-10 * time.Second),
		SessionID:     uuid.New(),
		Type:          "session_started",
		Data:          datatypes.JSON([]byte("{}")),
		CreatedAt:     now.Add(-10 * time.Second),
		UpdatedAt:     now.Add(-10 * time.Second),
	}
	e2 := &types.UserEvent{
		ID:            uuid.New(),
		UserID:        u.ID,
		ClientEventID: "evt-2",
		OccurredAt:    now.Add(-5 * time.Second),
		SessionID:     uuid.New(),
		CourseID:      testutil.PtrUUID(course.ID),
		Type:          "path_opened",
		Data:          datatypes.JSON([]byte("{}")),
		CreatedAt:     now.Add(-5 * time.Second),
		UpdatedAt:     now.Add(-5 * time.Second),
	}
	if _, err := repo.Create(ctx, tx, []*types.UserEvent{e1, e2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.UserEvent{ID: uuid.New(), UserID: u.ID, ClientEventID: "evt-1", OccurredAt: now, SessionID: uuid.New(), Type: "dup", Data: datatypes.JSON([]byte("{}"))}
	e3 := &types.UserEvent{ID: uuid.New(), UserID: u.ID, ClientEventID: "evt-3", OccurredAt: now, SessionID: uuid.New(), Type: "x", Data: datatypes.JSON([]byte("{}"))}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.UserEvent{dup, e3}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{e1.ID, e2.ID, e3.ID}); err != nil || len(rows) != 3 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserID(ctx, tx, u.ID); err != nil || len(rows) != 3 {
		t.Fatalf("GetByUserID: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserAndCourseID(ctx, tx, u.ID, course.ID); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserAndCourseID: err=%v len=%d", err, len(rows))
	}

	after := now.Add(-6 * time.Second)
	list, err := repo.ListAfterCursor(ctx, tx, u.ID, &after, nil, 10)
	if err != nil {
		t.Fatalf("ListAfterCursor: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("ListAfterCursor: expected non-empty")
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{e1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	e4 := &types.UserEvent{
		ID:            uuid.New(),
		UserID:        u.ID,
		ClientEventID: "evt-4",
		OccurredAt:    now,
		SessionID:     uuid.New(),
		Type:          "y",
		Data:          datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.UserEvent{e4}); err != nil {
		t.Fatalf("seed e4: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{e4.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

package learning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/gorm"
)

func TestUserEventCursorRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewUserEventCursorRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "usereventcursorrepo@example.com")

	_, err := repo.Get(ctx, tx, u.ID, "missing")
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Get(missing): expected ErrRecordNotFound, got %v", err)
	}

	now := time.Now().UTC()
	row := &types.UserEventCursor{
		ID:            uuid.New(),
		UserID:        u.ID,
		Consumer:      "consumer-a",
		LastCreatedAt: &now,
		LastEventID:   testutil.PtrUUID(uuid.New()),
		UpdatedAt:     now,
	}
	if err := repo.Upsert(ctx, tx, row); err != nil {
		t.Fatalf("Upsert(create): %v", err)
	}

	got, err := repo.Get(ctx, tx, u.ID, "consumer-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.Consumer != "consumer-a" {
		t.Fatalf("Get: unexpected: %+v", got)
	}

	now2 := now.Add(1 * time.Minute)
	row.LastCreatedAt = &now2
	if err := repo.Upsert(ctx, tx, row); err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}
	got2, err := repo.Get(ctx, tx, u.ID, "consumer-a")
	if err != nil {
		t.Fatalf("Get(after update): %v", err)
	}
	if got2.LastCreatedAt == nil || !got2.LastCreatedAt.Equal(now2) {
		t.Fatalf("expected LastCreatedAt=%v got %+v", now2, got2.LastCreatedAt)
	}
}

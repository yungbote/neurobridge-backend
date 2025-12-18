package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestUserConceptStateRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewUserConceptStateRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "userconceptstaterepo@example.com")
	scopeID := uuid.New()
	concept := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, Key: "c1", Name: "C1"}
	if err := tx.WithContext(ctx).Create(concept).Error; err != nil {
		t.Fatalf("seed concept: %v", err)
	}

	if got, err := repo.Get(ctx, tx, u.ID, concept.ID); err != nil || got != nil {
		t.Fatalf("Get(missing): got=%v err=%v", got, err)
	}

	ls := time.Now().Add(-1 * time.Hour).UTC()
	if err := repo.UpsertDelta(ctx, tx, u.ID, concept.ID, 0.2, 0.3, &ls); err != nil {
		t.Fatalf("UpsertDelta: %v", err)
	}
	got, err := repo.Get(ctx, tx, u.ID, concept.ID)
	if err != nil || got == nil || got.Mastery != 0.2 {
		t.Fatalf("Get(after upsert): got=%v err=%v", got, err)
	}

	if err := repo.UpsertDelta(ctx, tx, u.ID, concept.ID, 0.5, 0.6, &ls); err != nil {
		t.Fatalf("UpsertDelta(update): %v", err)
	}
	got2, _ := repo.Get(ctx, tx, u.ID, concept.ID)
	if got2 == nil || got2.Mastery != 0.5 || got2.Confidence != 0.6 {
		t.Fatalf("UpsertDelta verify update: got=%v", got2)
	}
}

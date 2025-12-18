package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestTopicStylePreferenceRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewTopicStylePreferenceRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "topicstyleprefrepo@example.com")

	row := &types.TopicStylePreference{
		ID:     uuid.Nil,
		UserID: u.ID,
		Topic:  "math",
		Style:  "diagram",
		Score:  0.1,
		N:      1,
	}
	created, err := repo.Create(ctx, tx, []*types.TopicStylePreference{row})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created) != 1 || created[0].ID == uuid.Nil {
		t.Fatalf("Create: unexpected: %+v", created)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{row.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByID(ctx, tx, row.ID); err != nil || got == nil || got.ID != row.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if got, err := repo.Get(ctx, tx, u.ID, " math ", " diagram "); err != nil || got == nil {
		t.Fatalf("Get: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDAndTopics(ctx, tx, u.ID, []string{"math"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDAndTopics: err=%v len=%d", err, len(rows))
	}

	if err := repo.UpsertEMA(ctx, tx, u.ID, "math", "diagram", 1.0); err != nil {
		t.Fatalf("UpsertEMA: %v", err)
	}
	afterEMA, _ := repo.Get(ctx, tx, u.ID, "math", "diagram")
	if afterEMA == nil || afterEMA.N < 2 {
		t.Fatalf("UpsertEMA verify: %+v", afterEMA)
	}

	up := &types.TopicStylePreference{ID: uuid.New(), UserID: u.ID, Topic: "math", Style: "diagram", Score: 0.9, N: 10}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.UpdateFields(ctx, tx, row.ID, map[string]interface{}{"score": 0.2}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{row.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	row2 := &types.TopicStylePreference{ID: uuid.New(), UserID: u.ID, Topic: "science", Style: "text", Score: 0.1, N: 1}
	if _, err := repo.Create(ctx, tx, []*types.TopicStylePreference{row2}); err != nil {
		t.Fatalf("seed row2: %v", err)
	}
	if err := repo.SoftDeleteByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("SoftDeleteByUserIDs: %v", err)
	}

	row3 := &types.TopicStylePreference{ID: uuid.New(), UserID: u.ID, Topic: "history", Style: "diagram", Score: 0.1, N: 1}
	if _, err := repo.Create(ctx, tx, []*types.TopicStylePreference{row3}); err != nil {
		t.Fatalf("seed row3: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{row3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	row4 := &types.TopicStylePreference{ID: uuid.New(), UserID: u.ID, Topic: "geo", Style: "diagram", Score: 0.1, N: 1}
	if _, err := repo.Create(ctx, tx, []*types.TopicStylePreference{row4}); err != nil {
		t.Fatalf("seed row4: %v", err)
	}
	if err := repo.FullDeleteByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("FullDeleteByUserIDs: %v", err)
	}
}

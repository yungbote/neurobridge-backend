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

func TestTopicMasteryRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewTopicMasteryRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "topicmasteryrepo@example.com")

	tm := &types.TopicMastery{
		ID:         uuid.New(),
		UserID:     u.ID,
		Topic:      "math",
		Mastery:    0.5,
		Metadata:   datatypes.JSON([]byte("{}")),
		LastUpdate: time.Now().UTC(),
	}
	if _, err := repo.Create(ctx, tx, []*types.TopicMastery{tm}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{tm.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDAndTopics(ctx, tx, u.ID, []string{"math"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDAndTopics: err=%v len=%d", err, len(rows))
	}

	tm.Mastery = 0.7
	if err := repo.Update(ctx, tx, tm); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{tm.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	tm2 := &types.TopicMastery{
		ID:         uuid.New(),
		UserID:     u.ID,
		Topic:      "science",
		Mastery:    0.2,
		Metadata:   datatypes.JSON([]byte("{}")),
		LastUpdate: time.Now().UTC(),
	}
	if _, err := repo.Create(ctx, tx, []*types.TopicMastery{tm2}); err != nil {
		t.Fatalf("seed tm2: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{tm2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestLearningProfileRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLearningProfileRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "learningprofilerepo@example.com")

	lp := &types.LearningProfile{
		ID:            uuid.New(),
		UserID:        u.ID,
		Diagnoses:     datatypes.JSON([]byte("[]")),
		Accomodations: datatypes.JSON([]byte("[]")),
		Constraints:   datatypes.JSON([]byte("[]")),
		Preferences:   datatypes.JSON([]byte("[]")),
		Notes:         "n1",
	}
	if _, err := repo.Create(ctx, tx, []*types.LearningProfile{lp}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{lp.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}

	lp.Notes = "n2"
	if err := repo.Update(ctx, tx, lp); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{lp.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	lp2 := &types.LearningProfile{
		ID:            uuid.New(),
		UserID:        u.ID,
		Diagnoses:     datatypes.JSON([]byte("[]")),
		Accomodations: datatypes.JSON([]byte("[]")),
		Constraints:   datatypes.JSON([]byte("[]")),
		Preferences:   datatypes.JSON([]byte("[]")),
		Notes:         "n3",
	}
	if _, err := repo.Create(ctx, tx, []*types.LearningProfile{lp2}); err != nil {
		t.Fatalf("seed lp2: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{lp2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

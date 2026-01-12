package materials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestMaterialSetRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewMaterialSetRepo(db, testutil.Logger(t))

	u := &types.User{
		ID:        uuid.New(),
		Email:     "materialsetrepo@example.com",
		Password:  "pw",
		FirstName: "A",
		LastName:  "B",
	}
	if err := tx.WithContext(ctx).Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ms := &types.MaterialSet{
		ID:     uuid.New(),
		UserID: u.ID,
		Title:  "set",
		Status: "pending",
	}
	if _, err := repo.Create(dbc, []*types.MaterialSet{ms}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{ms.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(dbc, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByStatus(dbc, []string{"pending"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByStatus: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(dbc, []uuid.UUID{ms.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{ms.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	ms2 := &types.MaterialSet{
		ID:     uuid.New(),
		UserID: u.ID,
		Title:  "set2",
		Status: "pending",
	}
	if _, err := repo.Create(dbc, []*types.MaterialSet{ms2}); err != nil {
		t.Fatalf("seed for FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByIDs(dbc, []uuid.UUID{ms2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{ms2.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after FullDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}
}

package materials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestMaterialFileRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewMaterialFileRepo(db, testutil.Logger(t))

	u := &types.User{
		ID:        uuid.New(),
		Email:     "materialfilerepo@example.com",
		Password:  "pw",
		FirstName: "A",
		LastName:  "B",
	}
	if err := tx.WithContext(ctx).Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ms := &types.MaterialSet{ID: uuid.New(), UserID: u.ID, Title: "set", Status: "pending"}
	if err := tx.WithContext(ctx).Create(ms).Error; err != nil {
		t.Fatalf("seed material set: %v", err)
	}

	mf := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		OriginalName:  "file.pdf",
		StorageKey:    "gs://bucket/file.pdf",
		Status:        "uploaded",
	}
	if _, err := repo.Create(dbc, []*types.MaterialFile{mf}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{mf.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialSetIDs(dbc, []uuid.UUID{ms.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialSetIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialSetID(dbc, ms.ID); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialSetID: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(dbc, []uuid.UUID{mf.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{mf.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	mf2 := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		OriginalName:  "file2.pdf",
		StorageKey:    "gs://bucket/file2.pdf",
		Status:        "uploaded",
	}
	if _, err := repo.Create(dbc, []*types.MaterialFile{mf2}); err != nil {
		t.Fatalf("seed mf2: %v", err)
	}
	if err := repo.SoftDeleteByMaterialSetIDs(dbc, []uuid.UUID{ms.ID}); err != nil {
		t.Fatalf("SoftDeleteByMaterialSetIDs: %v", err)
	}

	mf3 := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		OriginalName:  "file3.pdf",
		StorageKey:    "gs://bucket/file3.pdf",
		Status:        "uploaded",
	}
	if _, err := repo.Create(dbc, []*types.MaterialFile{mf3}); err != nil {
		t.Fatalf("seed mf3: %v", err)
	}
	if err := repo.FullDeleteByIDs(dbc, []uuid.UUID{mf3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	mf4 := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		OriginalName:  "file4.pdf",
		StorageKey:    "gs://bucket/file4.pdf",
		Status:        "uploaded",
	}
	if _, err := repo.Create(dbc, []*types.MaterialFile{mf4}); err != nil {
		t.Fatalf("seed mf4: %v", err)
	}
	if err := repo.FullDeleteByMaterialSetIDs(dbc, []uuid.UUID{ms.ID}); err != nil {
		t.Fatalf("FullDeleteByMaterialSetIDs: %v", err)
	}
}

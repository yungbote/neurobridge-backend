package materials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"gorm.io/datatypes"
)

func TestMaterialAssetRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewMaterialAssetRepo(db, testutil.Logger(t))

	u := &types.User{ID: uuid.New(), Email: "materialassetrepo@example.com", Password: "pw", FirstName: "A", LastName: "B"}
	if err := tx.WithContext(ctx).Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ms := &types.MaterialSet{ID: uuid.New(), UserID: u.ID, Title: "set", Status: "pending"}
	if err := tx.WithContext(ctx).Create(ms).Error; err != nil {
		t.Fatalf("seed set: %v", err)
	}
	mf := &types.MaterialFile{ID: uuid.New(), MaterialSetID: ms.ID, OriginalName: "file.pdf", StorageKey: "key", Status: "uploaded"}
	if err := tx.WithContext(ctx).Create(mf).Error; err != nil {
		t.Fatalf("seed file: %v", err)
	}

	a1 := &types.MaterialAsset{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Kind:           "original",
		StorageKey:     "asset/original",
		URL:            "https://example.com/original",
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	a2 := &types.MaterialAsset{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Kind:           "pdf_page",
		StorageKey:     "asset/page1",
		URL:            "https://example.com/page1",
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(dbc, []*types.MaterialAsset{a1, a2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(dbc, a1.ID); err != nil || got == nil || got.ID != a1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{a1.ID, a2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialFileIDs(dbc, []uuid.UUID{mf.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByMaterialFileIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByStorageKeys(dbc, []string{a1.StorageKey}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByStorageKeys: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByKinds(dbc, []string{"pdf_page"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByKinds: err=%v len=%d", err, len(rows))
	}

	a1.URL = "https://example.com/original2"
	if err := repo.Update(dbc, a1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(dbc, a2.ID, map[string]interface{}{"url": "https://example.com/page1b"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(dbc, []uuid.UUID{a1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{a1.ID, a2.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByMaterialFileIDs(dbc, []uuid.UUID{mf.ID}); err != nil {
		t.Fatalf("SoftDeleteByMaterialFileIDs: %v", err)
	}
	if rows, err := repo.GetByMaterialFileIDs(dbc, []uuid.UUID{mf.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByMaterialFileIDs GetByMaterialFileIDs: err=%v len=%d", err, len(rows))
	}

	// Full deletes
	b1 := &types.MaterialAsset{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Kind:           "frame",
		StorageKey:     "asset/frame1",
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(dbc, []*types.MaterialAsset{b1}); err != nil {
		t.Fatalf("seed b1: %v", err)
	}
	if err := repo.FullDeleteByIDs(dbc, []uuid.UUID{b1.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	b2 := &types.MaterialAsset{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Kind:           "audio",
		StorageKey:     "asset/audio1",
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(dbc, []*types.MaterialAsset{b2}); err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if err := repo.FullDeleteByMaterialFileIDs(dbc, []uuid.UUID{mf.ID}); err != nil {
		t.Fatalf("FullDeleteByMaterialFileIDs: %v", err)
	}
}

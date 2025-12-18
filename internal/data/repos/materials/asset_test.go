package materials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestAssetRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewAssetRepo(db, testutil.Logger(t))

	ownerType := "test_owner"
	owner1 := uuid.New()
	owner2 := uuid.New()

	a1 := &types.Asset{
		ID:         uuid.New(),
		Kind:       "image",
		StorageKey: "asset/key/1",
		OwnerType:  ownerType,
		OwnerID:    owner1,
		URL:        "https://example.com/1",
	}
	a2 := &types.Asset{
		ID:         uuid.New(),
		Kind:       "video",
		StorageKey: "asset/key/2",
		OwnerType:  ownerType,
		OwnerID:    owner1,
		URL:        "https://example.com/2",
	}
	a3 := &types.Asset{
		ID:         uuid.New(),
		Kind:       "image",
		StorageKey: "asset/key/3",
		OwnerType:  ownerType,
		OwnerID:    owner2,
		URL:        "https://example.com/3",
	}

	if _, err := repo.Create(ctx, tx, []*types.Asset{a1, a2, a3}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(ctx, tx, a1.ID); err != nil || got == nil || got.ID != a1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if got, err := repo.GetByIDs(ctx, tx, []uuid.UUID{a1.ID, a2.ID, a3.ID}); err != nil || len(got) != 3 {
		t.Fatalf("GetByIDs: len=%d err=%v", len(got), err)
	}
	if got, err := repo.GetByOwner(ctx, tx, ownerType, owner1); err != nil || len(got) != 2 {
		t.Fatalf("GetByOwner: len=%d err=%v", len(got), err)
	}
	if got, err := repo.GetByOwnerIDs(ctx, tx, ownerType, []uuid.UUID{owner1, owner2}); err != nil || len(got) != 3 {
		t.Fatalf("GetByOwnerIDs: len=%d err=%v", len(got), err)
	}
	if got, err := repo.GetByStorageKeys(ctx, tx, []string{a1.StorageKey, a3.StorageKey}); err != nil || len(got) != 2 {
		t.Fatalf("GetByStorageKeys: len=%d err=%v", len(got), err)
	}
	if got, err := repo.GetByKinds(ctx, tx, []string{"image"}); err != nil || len(got) != 2 {
		t.Fatalf("GetByKinds: len=%d err=%v", len(got), err)
	}

	a1.URL = "https://example.com/1b"
	if err := repo.Update(ctx, tx, a1); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := repo.UpdateFields(ctx, tx, a2.ID, map[string]interface{}{"url": "https://example.com/2b"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}
	if got, err := repo.GetByID(ctx, tx, a2.ID); err != nil || got == nil || got.URL != "https://example.com/2b" {
		t.Fatalf("UpdateFields verify: got=%v err=%v", got, err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{a3.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByOwnerIDs(ctx, tx, ownerType, []uuid.UUID{owner2}); err != nil {
		t.Fatalf("SoftDeleteByOwnerIDs: %v", err)
	}
	if got, err := repo.GetByIDs(ctx, tx, []uuid.UUID{a1.ID, a2.ID, a3.ID}); err != nil || len(got) != 2 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: len=%d err=%v", len(got), err)
	}

	if err := repo.SoftDeleteByOwner(ctx, tx, ownerType, owner1); err != nil {
		t.Fatalf("SoftDeleteByOwner: %v", err)
	}
	if got, err := repo.GetByOwnerIDs(ctx, tx, ownerType, []uuid.UUID{owner1, owner2}); err != nil || len(got) != 0 {
		t.Fatalf("after SoftDeleteByOwner GetByOwnerIDs: len=%d err=%v", len(got), err)
	}

	// Re-seed and test FullDelete variants.
	b1 := &types.Asset{
		ID:         uuid.New(),
		Kind:       "image",
		StorageKey: "asset/key/full/1",
		OwnerType:  ownerType,
		OwnerID:    owner1,
	}
	b2 := &types.Asset{
		ID:         uuid.New(),
		Kind:       "image",
		StorageKey: "asset/key/full/2",
		OwnerType:  ownerType,
		OwnerID:    owner2,
	}
	if _, err := repo.Create(ctx, tx, []*types.Asset{b1, b2}); err != nil {
		t.Fatalf("seed for full delete: %v", err)
	}

	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{b1.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	if err := repo.FullDeleteByOwner(ctx, tx, ownerType, owner2); err != nil {
		t.Fatalf("FullDeleteByOwner: %v", err)
	}

	// No-op but covers method.
	if err := repo.FullDeleteByOwnerIDs(ctx, tx, ownerType, []uuid.UUID{owner1, owner2}); err != nil {
		t.Fatalf("FullDeleteByOwnerIDs: %v", err)
	}
}

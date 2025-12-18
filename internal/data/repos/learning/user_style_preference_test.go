package learning

import (
	"context"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestUserStylePreferenceRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewUserStylePreferenceRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "userstyleprefrepo@example.com")

	binaryTrue := true
	binaryFalse := false

	if err := repo.UpsertEMA(ctx, tx, u.ID, nil, " diagram ", "", 1.0, &binaryTrue); err != nil {
		t.Fatalf("UpsertEMA(first): %v", err)
	}
	if err := repo.UpsertEMA(ctx, tx, u.ID, nil, "diagram", "", -1.0, &binaryFalse); err != nil {
		t.Fatalf("UpsertEMA(second): %v", err)
	}

	var row types.UserStylePreference
	if err := tx.WithContext(ctx).
		Where("user_id = ? AND concept_id IS NULL AND modality = ? AND variant = ?", u.ID, "diagram", "default").
		First(&row).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if row.N < 2 {
		t.Fatalf("expected N>=2, got %d", row.N)
	}
	if row.A < 2 || row.B < 2 {
		t.Fatalf("expected A/B to be incremented, got A=%v B=%v", row.A, row.B)
	}
}

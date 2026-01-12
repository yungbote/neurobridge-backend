package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestUserTokenRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewUserTokenRepo(db, testutil.Logger(t))

	u := &types.User{
		ID:        uuid.New(),
		Email:     "usertokenrepo@example.com",
		Password:  "pw",
		FirstName: "A",
		LastName:  "B",
	}
	if err := tx.WithContext(ctx).Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	makeToken := func(access, refresh string) *types.UserToken {
		return &types.UserToken{
			ID:           uuid.New(),
			UserID:       u.ID,
			AccessToken:  access,
			RefreshToken: refresh,
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		}
	}

	t1 := makeToken("access-1", "refresh-1")
	if _, err := repo.Create(dbc, []*types.UserToken{t1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{t1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUsers(dbc, []*types.User{u}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUsers: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(dbc, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByAccessTokens(dbc, []string{t1.AccessToken}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByAccessTokens: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByRefreshTokens(dbc, []string{t1.RefreshToken}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByRefreshTokens: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByTokens(dbc, []*types.UserToken{t1}); err != nil {
		t.Fatalf("SoftDeleteByTokens: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{t1.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByTokens GetByIDs: err=%v len=%d", err, len(rows))
	}

	t2 := makeToken("access-2", "refresh-2")
	if _, err := repo.Create(dbc, []*types.UserToken{t2}); err != nil {
		t.Fatalf("seed token2: %v", err)
	}
	if err := repo.SoftDeleteByIDs(dbc, []uuid.UUID{t2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	t3 := makeToken("access-3", "refresh-3")
	if _, err := repo.Create(dbc, []*types.UserToken{t3}); err != nil {
		t.Fatalf("seed token3: %v", err)
	}
	if err := repo.SoftDeleteByUserIDs(dbc, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("SoftDeleteByUserIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{t3.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByUserIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	t4 := makeToken("access-4", "refresh-4")
	if _, err := repo.Create(dbc, []*types.UserToken{t4}); err != nil {
		t.Fatalf("seed token4: %v", err)
	}
	if err := repo.FullDeleteByTokens(dbc, []*types.UserToken{t4}); err != nil {
		t.Fatalf("FullDeleteByTokens: %v", err)
	}

	t5 := makeToken("access-5", "refresh-5")
	if _, err := repo.Create(dbc, []*types.UserToken{t5}); err != nil {
		t.Fatalf("seed token5: %v", err)
	}
	if err := repo.FullDeleteByIDs(dbc, []uuid.UUID{t5.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	t6 := makeToken("access-6", "refresh-6")
	if _, err := repo.Create(dbc, []*types.UserToken{t6}); err != nil {
		t.Fatalf("seed token6: %v", err)
	}
	if err := repo.FullDeleteByUserIDs(dbc, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("FullDeleteByUserIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{t6.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after FullDeleteByUserIDs GetByIDs: err=%v len=%d", err, len(rows))
	}
}

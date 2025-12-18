package user

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestUserRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	repo := NewUserRepo(db, testutil.Logger(t))
	ctx := context.Background()

	created, err := repo.Create(ctx, tx, []*types.User{
		{
			ID:        uuid.New(),
			Email:     "userrepo@example.com",
			Password:  "pw",
			FirstName: "A",
			LastName:  "B",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("Create: expected 1 user, got %d", len(created))
	}

	gotByIDs, err := repo.GetByIDs(ctx, tx, []uuid.UUID{created[0].ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(gotByIDs) != 1 || gotByIDs[0].ID != created[0].ID {
		t.Fatalf("GetByIDs: unexpected result: %+v", gotByIDs)
	}

	gotByEmails, err := repo.GetByEmails(ctx, tx, []string{created[0].Email})
	if err != nil {
		t.Fatalf("GetByEmails: %v", err)
	}
	if len(gotByEmails) != 1 || gotByEmails[0].Email != created[0].Email {
		t.Fatalf("GetByEmails: unexpected result: %+v", gotByEmails)
	}

	exists, err := repo.EmailExists(ctx, tx, created[0].Email)
	if err != nil {
		t.Fatalf("EmailExists: %v", err)
	}
	if !exists {
		t.Fatalf("EmailExists: expected true")
	}

	exists, err = repo.EmailExists(ctx, tx, "does-not-exist@example.com")
	if err != nil {
		t.Fatalf("EmailExists (missing): %v", err)
	}
	if exists {
		t.Fatalf("EmailExists (missing): expected false")
	}
}

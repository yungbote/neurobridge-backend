package materials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/datatypes"
)

func TestMaterialChunkRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewMaterialChunkRepo(db, testutil.Logger(t))

	u := &types.User{
		ID:        uuid.New(),
		Email:     "materialchunkrepo@example.com",
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
	mf := &types.MaterialFile{ID: uuid.New(), MaterialSetID: ms.ID, OriginalName: "file.pdf", StorageKey: "key", Status: "uploaded"}
	if err := tx.WithContext(ctx).Create(mf).Error; err != nil {
		t.Fatalf("seed material file: %v", err)
	}

	c1 := &types.MaterialChunk{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Index:          0,
		Text:           "chunk-0",
		Embedding:      datatypes.JSON([]byte("[]")),
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	c2 := &types.MaterialChunk{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Index:          1,
		Text:           "chunk-1",
		Embedding:      datatypes.JSON([]byte("[]")),
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(dbc, []*types.MaterialChunk{c1, c2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByMaterialFileIDs(dbc, []uuid.UUID{mf.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByMaterialFileIDs: err=%v len=%d", err, len(rows))
	}

	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{c1.ID, c2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
}

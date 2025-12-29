package testutil

import (
	"testing"
	"time"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func SeedUser(tb testing.TB, dbc dbctx.Context, email string) *types.User {
	tb.Helper()
	u := &types.User{
		ID:        uuid.New(),
		Email:     email,
		Password:  "pw",
		FirstName: "A",
		LastName:  "B",
	}
	if err := dbc.Tx.WithContext(dbc.Ctx).Create(u).Error; err != nil {
		tb.Fatalf("seed user: %v", err)
	}
	return u
}

func SeedMaterialSet(tb testing.TB, dbc dbctx.Context, userID uuid.UUID) *types.MaterialSet {
	tb.Helper()
	ms := &types.MaterialSet{
		ID:     uuid.New(),
		UserID: userID,
		Title:  "set",
		Status: "pending",
	}
	if err := dbc.Tx.WithContext(dbc.Ctx).Create(ms).Error; err != nil {
		tb.Fatalf("seed material set: %v", err)
	}
	return ms
}

func SeedMaterialFile(tb testing.TB, dbc dbctx.Context, setID uuid.UUID, storageKey string) *types.MaterialFile {
	tb.Helper()
	mf := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: setID,
		OriginalName:  "file.pdf",
		StorageKey:    storageKey,
		Status:        "uploaded",
		AIType:        "",
		AITopics:      datatypes.JSON([]byte("[]")),
	}
	if err := dbc.Tx.WithContext(dbc.Ctx).Create(mf).Error; err != nil {
		tb.Fatalf("seed material file: %v", err)
	}
	return mf
}

func SeedMaterialChunk(tb testing.TB, dbc dbctx.Context, fileID uuid.UUID, index int) *types.MaterialChunk {
	tb.Helper()
	c := &types.MaterialChunk{
		ID:             uuid.New(),
		MaterialFileID: fileID,
		Index:          index,
		Text:           "chunk",
		Embedding:      datatypes.JSON([]byte("[]")),
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if err := dbc.Tx.WithContext(dbc.Ctx).Create(c).Error; err != nil {
		tb.Fatalf("seed material chunk: %v", err)
	}
	return c
}

func PtrUUID(v uuid.UUID) *uuid.UUID { return &v }

func PtrTime(v time.Time) *time.Time { return &v }

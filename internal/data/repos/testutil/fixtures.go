package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func SeedUser(tb testing.TB, ctx context.Context, tx *gorm.DB, email string) *types.User {
	tb.Helper()
	u := &types.User{
		ID:        uuid.New(),
		Email:     email,
		Password:  "pw",
		FirstName: "A",
		LastName:  "B",
	}
	if err := tx.WithContext(ctx).Create(u).Error; err != nil {
		tb.Fatalf("seed user: %v", err)
	}
	return u
}

func SeedMaterialSet(tb testing.TB, ctx context.Context, tx *gorm.DB, userID uuid.UUID) *types.MaterialSet {
	tb.Helper()
	ms := &types.MaterialSet{
		ID:     uuid.New(),
		UserID: userID,
		Title:  "set",
		Status: "pending",
	}
	if err := tx.WithContext(ctx).Create(ms).Error; err != nil {
		tb.Fatalf("seed material set: %v", err)
	}
	return ms
}

func SeedMaterialFile(tb testing.TB, ctx context.Context, tx *gorm.DB, setID uuid.UUID, storageKey string) *types.MaterialFile {
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
	if err := tx.WithContext(ctx).Create(mf).Error; err != nil {
		tb.Fatalf("seed material file: %v", err)
	}
	return mf
}

func SeedMaterialChunk(tb testing.TB, ctx context.Context, tx *gorm.DB, fileID uuid.UUID, index int) *types.MaterialChunk {
	tb.Helper()
	c := &types.MaterialChunk{
		ID:             uuid.New(),
		MaterialFileID: fileID,
		Index:          index,
		Text:           "chunk",
		Embedding:      datatypes.JSON([]byte("[]")),
		Metadata:       datatypes.JSON([]byte("{}")),
	}
	if err := tx.WithContext(ctx).Create(c).Error; err != nil {
		tb.Fatalf("seed material chunk: %v", err)
	}
	return c
}

func SeedCourse(tb testing.TB, ctx context.Context, tx *gorm.DB, userID uuid.UUID, materialSetID *uuid.UUID) *types.Course {
	tb.Helper()
	c := &types.Course{
		ID:            uuid.New(),
		UserID:        userID,
		MaterialSetID: materialSetID,
		Title:         "course",
		Status:        "draft",
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if err := tx.WithContext(ctx).Create(c).Error; err != nil {
		tb.Fatalf("seed course: %v", err)
	}
	return c
}

func SeedCourseModule(tb testing.TB, ctx context.Context, tx *gorm.DB, courseID uuid.UUID, index int) *types.CourseModule {
	tb.Helper()
	m := &types.CourseModule{
		ID:        uuid.New(),
		CourseID:  courseID,
		Index:     index,
		Title:     "module",
		Metadata:  datatypes.JSON([]byte("{}")),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := tx.WithContext(ctx).Create(m).Error; err != nil {
		tb.Fatalf("seed course module: %v", err)
	}
	return m
}

func SeedLesson(tb testing.TB, ctx context.Context, tx *gorm.DB, moduleID uuid.UUID, index int) *types.Lesson {
	tb.Helper()
	l := &types.Lesson{
		ID:          uuid.New(),
		ModuleID:    moduleID,
		Index:       index,
		Title:       "lesson",
		Kind:        "reading",
		ContentMD:   "content",
		SummaryMD:   "summary",
		ContentJSON: datatypes.JSON([]byte("{}")),
		Metadata:    datatypes.JSON([]byte("{}")),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := tx.WithContext(ctx).Create(l).Error; err != nil {
		tb.Fatalf("seed lesson: %v", err)
	}
	return l
}

func PtrUUID(v uuid.UUID) *uuid.UUID { return &v }

func PtrTime(v time.Time) *time.Time { return &v }

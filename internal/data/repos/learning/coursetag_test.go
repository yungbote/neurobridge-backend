package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestCourseTagRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewCourseTagRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "coursetagrepo@example.com")
	c1 := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	c2 := testutil.SeedCourse(t, ctx, tx, u.ID, nil)

	ct1 := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "a"}
	ct2 := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "b"}
	ct3 := &types.CourseTag{ID: uuid.New(), CourseID: c2.ID, Tag: "a"}
	if _, err := repo.Create(ctx, tx, []*types.CourseTag{ct1, ct2, ct3}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// CreateIgnoreDuplicates should insert only new rows.
	dup := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "a"}
	ct4 := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "c"}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.CourseTag{dup, ct4}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{ct1.ID, ct2.ID, ct3.ID, ct4.ID}); err != nil || len(rows) != 4 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByID(ctx, tx, ct1.ID); err != nil || got == nil || got.ID != ct1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByCourseIDs(ctx, tx, []uuid.UUID{c1.ID}); err != nil || len(rows) != 3 {
		t.Fatalf("GetByCourseIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseID(ctx, tx, c1.ID); err != nil || len(rows) != 3 {
		t.Fatalf("GetByCourseID: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByTags(ctx, tx, []string{"a"}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByTags: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseIDAndTags(ctx, tx, c1.ID, []string{"a", "b"}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByCourseIDAndTags: err=%v len=%d", err, len(rows))
	}

	if err := repo.UpdateFields(ctx, tx, ct2.ID, map[string]interface{}{"tag": "b2"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByCourseIDAndTags(ctx, tx, c1.ID, []string{"b2"}); err != nil {
		t.Fatalf("SoftDeleteByCourseIDAndTags: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{ct4.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	if err := repo.SoftDeleteByCourseIDs(ctx, tx, []uuid.UUID{c1.ID}); err != nil {
		t.Fatalf("SoftDeleteByCourseIDs: %v", err)
	}
	if rows, err := repo.GetByCourseID(ctx, tx, c1.ID); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByCourseIDs GetByCourseID: err=%v len=%d", err, len(rows))
	}

	// Full delete variants
	ct5 := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "x"}
	ct6 := &types.CourseTag{ID: uuid.New(), CourseID: c1.ID, Tag: "y"}
	if _, err := repo.Create(ctx, tx, []*types.CourseTag{ct5, ct6}); err != nil {
		t.Fatalf("seed for full delete: %v", err)
	}

	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{ct5.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByCourseIDAndTags(ctx, tx, c1.ID, []string{"y"}); err != nil {
		t.Fatalf("FullDeleteByCourseIDAndTags: %v", err)
	}

	ct7 := &types.CourseTag{ID: uuid.New(), CourseID: c2.ID, Tag: "z"}
	if _, err := repo.Create(ctx, tx, []*types.CourseTag{ct7}); err != nil {
		t.Fatalf("seed ct7: %v", err)
	}
	if err := repo.FullDeleteByCourseIDs(ctx, tx, []uuid.UUID{c2.ID}); err != nil {
		t.Fatalf("FullDeleteByCourseIDs: %v", err)
	}
}

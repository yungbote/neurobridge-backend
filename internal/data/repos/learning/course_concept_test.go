package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestCourseConceptRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewCourseConceptRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "courseconceptrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)

	root := &types.CourseConcept{
		ID:       uuid.New(),
		CourseID: course.ID,
		Key:      "root",
		Name:     "Root",
		Depth:    0,
	}
	child := &types.CourseConcept{
		ID:       uuid.New(),
		CourseID: course.ID,
		ParentID: testutil.PtrUUID(root.ID),
		Key:      "child",
		Name:     "Child",
		Depth:    1,
	}
	if _, err := repo.Create(ctx, tx, []*types.CourseConcept{root, child}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{root.ID, child.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByID(ctx, tx, root.ID); err != nil || got == nil || got.ID != root.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByCourseIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseAndKeys(ctx, tx, course.ID, []string{"root"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByCourseAndKeys: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByCourseAndKey(ctx, tx, course.ID, "child"); err != nil || got == nil || got.ID != child.ID {
		t.Fatalf("GetByCourseAndKey: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByCourseAndParent(ctx, tx, course.ID, nil); err != nil || len(rows) != 1 {
		t.Fatalf("GetByCourseAndParent (nil): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseAndParent(ctx, tx, course.ID, testutil.PtrUUID(root.ID)); err != nil || len(rows) != 1 {
		t.Fatalf("GetByCourseAndParent (root): err=%v len=%d", err, len(rows))
	}

	up := &types.CourseConcept{
		ID:       uuid.New(),
		CourseID: course.ID,
		Key:      "upsert",
		Name:     "Upsert1",
	}
	if err := repo.UpsertByCourseAndKey(ctx, tx, up); err != nil {
		t.Fatalf("UpsertByCourseAndKey (create): %v", err)
	}
	upGot, err := repo.GetByCourseAndKey(ctx, tx, course.ID, "upsert")
	if err != nil || upGot == nil || upGot.Name != "Upsert1" {
		t.Fatalf("UpsertByCourseAndKey verify create: got=%v err=%v", upGot, err)
	}
	upGot.Name = "Upsert2"
	if err := repo.UpsertByCourseAndKey(ctx, tx, upGot); err != nil {
		t.Fatalf("UpsertByCourseAndKey (update): %v", err)
	}
	upGot2, _ := repo.GetByCourseAndKey(ctx, tx, course.ID, "upsert")
	if upGot2 == nil || upGot2.Name != "Upsert2" {
		t.Fatalf("UpsertByCourseAndKey verify update: got=%v", upGot2)
	}

	root.Name = "Root2"
	if err := repo.Update(ctx, tx, root); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rootGot, _ := repo.GetByCourseAndKey(ctx, tx, course.ID, "root")
	if rootGot == nil || rootGot.Name != "Root2" {
		t.Fatalf("Update verify: got=%v", rootGot)
	}

	if err := repo.UpdateFields(ctx, tx, child.ID, map[string]interface{}{"name": "Child2"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}
	childGot, _ := repo.GetByCourseAndKey(ctx, tx, course.ID, "child")
	if childGot == nil || childGot.Name != "Child2" {
		t.Fatalf("UpdateFields verify: got=%v", childGot)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{root.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{root.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil {
		t.Fatalf("SoftDeleteByCourseIDs: %v", err)
	}
	if rows, err := repo.GetByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByCourseIDs GetByCourseIDs: err=%v len=%d", err, len(rows))
	}

	fd := &types.CourseConcept{ID: uuid.New(), CourseID: course.ID, Key: "fd", Name: "FD"}
	if _, err := repo.Create(ctx, tx, []*types.CourseConcept{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	fd2 := &types.CourseConcept{ID: uuid.New(), CourseID: course.ID, Key: "fd2", Name: "FD2"}
	if _, err := repo.Create(ctx, tx, []*types.CourseConcept{fd2}); err != nil {
		t.Fatalf("seed fd2: %v", err)
	}
	if err := repo.FullDeleteByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil {
		t.Fatalf("FullDeleteByCourseIDs: %v", err)
	}
}

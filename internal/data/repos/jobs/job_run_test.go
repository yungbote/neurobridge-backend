package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestJobRunRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewJobRunRepo(db, testutil.Logger(t))

	now := time.Now().UTC()
	ownerUserID := uuid.New()

	queued := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "test_job",
		EntityType:  "course",
		EntityID:    ptrUUID(uuid.New()),
		Status:      "queued",
		Stage:       "queued",
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
		CreatedAt:   now.Add(-3 * time.Hour),
		UpdatedAt:   now.Add(-3 * time.Hour),
	}
	failed := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "test_job",
		EntityType:  "course",
		EntityID:    ptrUUID(uuid.New()),
		Status:      "failed",
		Stage:       "failed",
		Attempts:    0,
		LastErrorAt: ptrTime(now.Add(-2 * time.Hour)),
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-2 * time.Hour),
	}
	staleRunning := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "test_job",
		EntityType:  "course",
		EntityID:    ptrUUID(uuid.New()),
		Status:      "running",
		Stage:       "running",
		Attempts:    0,
		HeartbeatAt: ptrTime(now.Add(-10 * time.Hour)),
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
		CreatedAt:   now.Add(-1 * time.Hour),
		UpdatedAt:   now.Add(-1 * time.Hour),
	}

	created, err := repo.Create(ctx, tx, []*types.JobRun{queued, failed, staleRunning})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("Create: expected 3, got %d", len(created))
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{queued.ID, failed.ID, staleRunning.ID}); err != nil || len(rows) != 3 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}

	// GetLatestByEntity
	entityType := "lesson"
	entityID := uuid.New()
	older := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "build",
		EntityType:  entityType,
		EntityID:    &entityID,
		Status:      "queued",
		Stage:       "queued",
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
		CreatedAt:   now.Add(-5 * time.Hour),
		UpdatedAt:   now.Add(-5 * time.Hour),
	}
	newer := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "build",
		EntityType:  entityType,
		EntityID:    &entityID,
		Status:      "queued",
		Stage:       "queued",
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
		CreatedAt:   now.Add(-4 * time.Hour),
		UpdatedAt:   now.Add(-4 * time.Hour),
	}
	if _, err := repo.Create(ctx, tx, []*types.JobRun{older, newer}); err != nil {
		t.Fatalf("seed latest: %v", err)
	}
	latest, err := repo.GetLatestByEntity(ctx, tx, ownerUserID, entityType, entityID, "build")
	if err != nil {
		t.Fatalf("GetLatestByEntity: %v", err)
	}
	if latest == nil || latest.ID != newer.ID {
		t.Fatalf("GetLatestByEntity: expected %v got %v", newer.ID, latest)
	}

	// ClaimNextRunnable should walk the runnable set in created_at ASC order.
	claim1, err := repo.ClaimNextRunnable(ctx, tx, 3, 1*time.Hour, 1*time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunnable #1: %v", err)
	}
	if claim1 == nil || claim1.ID != queued.ID {
		t.Fatalf("ClaimNextRunnable #1: expected %v got %v", queued.ID, claim1)
	}

	claim2, err := repo.ClaimNextRunnable(ctx, tx, 3, 1*time.Hour, 1*time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunnable #2: %v", err)
	}
	if claim2 == nil || claim2.ID != failed.ID {
		t.Fatalf("ClaimNextRunnable #2: expected %v got %v", failed.ID, claim2)
	}

	claim3, err := repo.ClaimNextRunnable(ctx, tx, 3, 1*time.Hour, 1*time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunnable #3: %v", err)
	}
	if claim3 == nil || claim3.ID != staleRunning.ID {
		t.Fatalf("ClaimNextRunnable #3: expected %v got %v", staleRunning.ID, claim3)
	}

	claim4, err := repo.ClaimNextRunnable(ctx, tx, 3, 1*time.Hour, 1*time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunnable #4: %v", err)
	}
	if claim4 != nil {
		t.Fatalf("ClaimNextRunnable #4: expected nil, got %v", claim4)
	}

	// UpdateFields
	if err := repo.UpdateFields(ctx, tx, queued.ID, map[string]interface{}{"status": "failed", "stage": "error"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	// Heartbeat
	if err := repo.Heartbeat(ctx, tx, failed.ID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// HasRunnableForEntity / ExistsRunnable
	rEntityType := "course"
	rEntityID := uuid.New()
	runnable := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     "rebuild",
		EntityType:  rEntityType,
		EntityID:    &rEntityID,
		Status:      "queued",
		Stage:       "queued",
		Payload:     datatypes.JSON([]byte("{}")),
		Result:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.JobRun{runnable}); err != nil {
		t.Fatalf("seed runnable: %v", err)
	}

	has, err := repo.HasRunnableForEntity(ctx, tx, ownerUserID, rEntityType, rEntityID, "rebuild")
	if err != nil {
		t.Fatalf("HasRunnableForEntity: %v", err)
	}
	if !has {
		t.Fatalf("HasRunnableForEntity: expected true")
	}

	exists, err := repo.ExistsRunnable(ctx, tx, ownerUserID, "rebuild", "", nil)
	if err != nil {
		t.Fatalf("ExistsRunnable: %v", err)
	}
	if !exists {
		t.Fatalf("ExistsRunnable: expected true")
	}

	exists, err = repo.ExistsRunnable(ctx, tx, ownerUserID, "rebuild", rEntityType, &rEntityID)
	if err != nil {
		t.Fatalf("ExistsRunnable (scoped): %v", err)
	}
	if !exists {
		t.Fatalf("ExistsRunnable (scoped): expected true")
	}

	exists, err = repo.ExistsRunnable(ctx, tx, ownerUserID, "other", rEntityType, &rEntityID)
	if err != nil {
		t.Fatalf("ExistsRunnable (other): %v", err)
	}
	if exists {
		t.Fatalf("ExistsRunnable (other): expected false")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func ptrUUID(u uuid.UUID) *uuid.UUID { return &u }

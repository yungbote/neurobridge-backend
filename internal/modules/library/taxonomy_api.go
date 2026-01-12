package library

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/platform/apierr"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
)

func (u Usecases) GetTaxonomySnapshot(ctx context.Context, userID uuid.UUID) (snapshot any, enqueuedRefine bool, err error) {
	if userID == uuid.Nil {
		return nil, false, apierr.New(http.StatusUnauthorized, "unauthorized", nil)
	}
	if u.deps.Snapshots == nil || u.deps.TaxNodes == nil || u.deps.TaxEdges == nil || u.deps.Membership == nil || u.deps.State == nil {
		return nil, false, apierr.New(http.StatusInternalServerError, "library_not_configured", nil)
	}

	snap, err := u.deps.Snapshots.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	if err != nil {
		return nil, false, apierr.New(http.StatusInternalServerError, "load_snapshot_failed", err)
	}

	// If missing, build a snapshot synchronously (no AI calls required; embeddings are optional).
	if snap == nil || len(snap.SnapshotJSON) == 0 || string(snap.SnapshotJSON) == "null" {
		_ = u.BuildAndPersistTaxonomySnapshot(ctx, userID)
		snap, _ = u.deps.Snapshots.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	}

	if snap != nil && len(snap.SnapshotJSON) > 0 && string(snap.SnapshotJSON) != "null" {
		_ = json.Unmarshal(snap.SnapshotJSON, &snapshot)
	}

	// Optional: enqueue refine if thresholds are crossed and no refine job is already runnable.
	enqueuedRefine = false
	if u.deps.Jobs != nil && u.deps.JobRuns != nil {
		state, err := u.deps.State.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
		if err == nil {
			refineNewPathsThreshold := envutil.Int("LIBRARY_TAXONOMY_REFINE_NEW_PATHS_THRESHOLD", 5)
			if refineNewPathsThreshold < 1 {
				refineNewPathsThreshold = 1
			}
			refineUnsortedThreshold := envutil.Int("LIBRARY_TAXONOMY_REFINE_UNSORTED_THRESHOLD", 3)
			if refineUnsortedThreshold < 1 {
				refineUnsortedThreshold = 1
			}

			locked := state != nil && state.RefineLockUntil != nil && state.RefineLockUntil.After(time.Now().UTC())
			should := state == nil || state.LastRefinedAt == nil
			if !should && state != nil {
				should = state.NewPathsSinceRefine >= refineNewPathsThreshold || state.PendingUnsortedPaths >= refineUnsortedThreshold
			}
			if should && !locked {
				entityID := userID
				exists, err := u.deps.JobRuns.ExistsRunnable(dbctx.Context{Ctx: ctx}, userID, "library_taxonomy_refine", "user", &entityID)
				if err == nil && !exists {
					if _, err := u.deps.Jobs.Enqueue(dbctx.Context{Ctx: ctx}, userID, "library_taxonomy_refine", "user", &entityID, map[string]any{"user_id": userID.String()}); err == nil {
						enqueuedRefine = true
					}
				}
			}
		}
	}

	return snapshot, enqueuedRefine, nil
}

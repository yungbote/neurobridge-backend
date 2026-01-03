package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	libsteps "github.com/yungbote/neurobridge-backend/internal/jobs/library/steps"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/envutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type LibraryHandler struct {
	log *logger.Logger

	jobSvc services.JobService
	jobRun repos.JobRunRepo

	nodes      repos.LibraryTaxonomyNodeRepo
	edges      repos.LibraryTaxonomyEdgeRepo
	membership repos.LibraryTaxonomyMembershipRepo
	state      repos.LibraryTaxonomyStateRepo
	snapshots  repos.LibraryTaxonomySnapshotRepo
}

func NewLibraryHandler(
	log *logger.Logger,
	jobSvc services.JobService,
	jobRun repos.JobRunRepo,
	nodes repos.LibraryTaxonomyNodeRepo,
	edges repos.LibraryTaxonomyEdgeRepo,
	membership repos.LibraryTaxonomyMembershipRepo,
	state repos.LibraryTaxonomyStateRepo,
	snapshots repos.LibraryTaxonomySnapshotRepo,
) *LibraryHandler {
	return &LibraryHandler{
		log:        log.With("handler", "LibraryHandler"),
		jobSvc:     jobSvc,
		jobRun:     jobRun,
		nodes:      nodes,
		edges:      edges,
		membership: membership,
		state:      state,
		snapshots:  snapshots,
	}
}

// GET /api/library/taxonomy
func (h *LibraryHandler) GetTaxonomySnapshot(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h == nil || h.snapshots == nil || h.nodes == nil || h.edges == nil || h.membership == nil || h.state == nil {
		response.RespondError(c, http.StatusInternalServerError, "library_not_configured", nil)
		return
	}

	userID := rd.UserID
	ctx := c.Request.Context()

	snap, err := h.snapshots.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	if err != nil {
		h.log.Error("GetTaxonomySnapshot failed (load snapshot)", "error", err, "user_id", userID.String())
		response.RespondError(c, http.StatusInternalServerError, "load_snapshot_failed", err)
		return
	}

	// If missing, build a snapshot synchronously (no AI calls).
	if snap == nil || len(snap.SnapshotJSON) == 0 || string(snap.SnapshotJSON) == "null" {
		_ = libsteps.BuildAndPersistLibraryTaxonomySnapshot(ctx, libsteps.LibraryTaxonomyRouteDeps{
			TaxNodes:   h.nodes,
			TaxEdges:   h.edges,
			Membership: h.membership,
			State:      h.state,
			Snapshots:  h.snapshots,
		}, userID)
		snap, _ = h.snapshots.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	}

	var snapshotAny any
	if snap != nil && len(snap.SnapshotJSON) > 0 && string(snap.SnapshotJSON) != "null" {
		_ = json.Unmarshal(snap.SnapshotJSON, &snapshotAny)
	}

	// Optional: enqueue refine if thresholds are crossed and no refine job is already runnable.
	enqueuedRefine := false
	if h.jobSvc != nil && h.jobRun != nil {
		state, err := h.state.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
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
				exists, err := h.jobRun.ExistsRunnable(dbctx.Context{Ctx: ctx}, userID, "library_taxonomy_refine", "user", &entityID)
				if err == nil && !exists {
					if _, err := h.jobSvc.Enqueue(dbctx.Context{Ctx: ctx}, userID, "library_taxonomy_refine", "user", &entityID, map[string]any{"user_id": userID.String()}); err == nil {
						enqueuedRefine = true
					}
				}
			}
		}
	}

	response.RespondOK(c, gin.H{
		"snapshot":        snapshotAny,
		"enqueued_refine": enqueuedRefine,
	})
}

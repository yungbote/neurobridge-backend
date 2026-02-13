package library_taxonomy_route

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	librarymod "github.com/yungbote/neurobridge-backend/internal/modules/library"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.ai == nil || p.jobs == nil || p.jobRun == nil || p.path == nil || p.pathNodes == nil || p.clusters == nil || p.taxNodes == nil || p.taxEdges == nil || p.membership == nil || p.state == nil || p.snapshots == nil || p.pathVectors == nil {
		jc.Fail("validate", fmt.Errorf("library_taxonomy_route: pipeline not configured"))
		return nil
	}

	pathID, ok := jc.PayloadUUID("path_id")
	if !ok || pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}

	jc.Progress("route", 10, "Organizing path in your library")

	out, err := librarymod.New(librarymod.UsecasesDeps{
		DB:          p.db,
		Log:         p.log,
		AI:          p.ai,
		Graph:       p.graph,
		Path:        p.path,
		PathNodes:   p.pathNodes,
		Clusters:    p.clusters,
		TaxNodes:    p.taxNodes,
		TaxEdges:    p.taxEdges,
		Membership:  p.membership,
		State:       p.state,
		Snapshots:   p.snapshots,
		PathVectors: p.pathVectors,
	}).LibraryTaxonomyRoute(jc.Ctx, librarymod.LibraryTaxonomyRouteInput{PathID: pathID})
	if err != nil {
		jc.Fail("route", err)
		return nil
	}

	enqueuedRefine := false
	if out.ShouldEnqueueRefine && out.UserID != uuid.Nil {
		entityID := out.UserID
		exists, err := p.jobRun.ExistsRunnable(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, out.UserID, "library_taxonomy_refine", "user", &entityID)
		if err == nil && !exists {
			payload := map[string]any{"user_id": out.UserID.String()}
			if _, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, out.UserID, "library_taxonomy_refine", "user", &entityID, payload); err == nil {
				enqueuedRefine = true
			}
		}
	}

	meta := map[string]any{
		"job_run_id":               jc.Job.ID.String(),
		"owner_user_id":            jc.Job.OwnerUserID.String(),
		"user_id":                  out.UserID.String(),
		"path_id":                  out.PathID.String(),
		"facets_processed":         out.FacetsProcessed,
		"nodes_created":            out.NodesCreated,
		"edges_upserted":           out.EdgesUpserted,
		"members_upserted":         out.MembersUpserted,
		"assigned_to_inbox_facets": out.AssignedToInboxFacets,
		"should_enqueue_refine":    out.ShouldEnqueueRefine,
		"enqueued_refine":          enqueuedRefine,
	}
	inputs := map[string]any{
		"path_id": pathID.String(),
	}
	chosen := map[string]any{
		"nodes_created":  out.NodesCreated,
		"edges_upserted": out.EdgesUpserted,
	}
	userID := jc.Job.OwnerUserID
	_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		PathID:        &out.PathID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      true,
		RequireTrace:  true,
	})
	if traceErr != nil {
		jc.Fail("structural_trace", traceErr)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"user_id":                  out.UserID.String(),
		"path_id":                  out.PathID.String(),
		"facets_processed":         out.FacetsProcessed,
		"nodes_created":            out.NodesCreated,
		"edges_upserted":           out.EdgesUpserted,
		"members_upserted":         out.MembersUpserted,
		"assigned_to_inbox_facets": out.AssignedToInboxFacets,
		"should_enqueue_refine":    out.ShouldEnqueueRefine,
		"enqueued_refine":          enqueuedRefine,
	})
	return nil
}

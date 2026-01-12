package library_taxonomy_refine

import (
	"fmt"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	librarymod "github.com/yungbote/neurobridge-backend/internal/modules/library"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.ai == nil || p.path == nil || p.pathNodes == nil || p.clusters == nil || p.taxNodes == nil || p.taxEdges == nil || p.membership == nil || p.state == nil || p.snapshots == nil || p.pathVectors == nil {
		jc.Fail("validate", fmt.Errorf("library_taxonomy_refine: pipeline not configured"))
		return nil
	}

	userID, ok := jc.PayloadUUID("user_id")
	if !ok || userID == uuid.Nil {
		// Default to owner.
		userID = jc.Job.OwnerUserID
	}
	if userID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing user_id"))
		return nil
	}

	jc.Progress("refine", 10, "Refining library taxonomy")

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
	}).LibraryTaxonomyRefine(jc.Ctx, librarymod.LibraryTaxonomyRefineInput{UserID: userID})
	if err != nil {
		jc.Fail("refine", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"user_id":          out.UserID.String(),
		"facets_processed": out.FacetsProcessed,
		"paths_considered": out.PathsConsidered,
		"paths_reassigned": out.PathsReassigned,
		"nodes_created":    out.NodesCreated,
		"edges_upserted":   out.EdgesUpserted,
		"members_upserted": out.MembersUpserted,
		"skipped":          out.Skipped,
	})
	return nil
}

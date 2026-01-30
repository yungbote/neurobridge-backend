package realize_activities

import (
	"fmt"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing saga_id"))
		return nil
	}
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("node_content", 2, "Writing node content")
	nodeOut, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		Path:             p.path,
		PathNodes:        p.nodes,
		Files:            p.files,
		Chunks:           p.chunks,
		UserProfile:      p.profile,
		TeachingPatterns: p.patterns,
		AI:               p.ai,
		Vec:              p.vec,
		Bucket:           p.bucket,
		Bootstrap:        p.bootstrap,
	}).NodeContentBuild(jc.Ctx, learningmod.NodeContentBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("node_content", err)
		return nil
	}

	jc.Progress("activities", 10, "Generating activities")
	actOut, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                 p.db,
		Log:                p.log,
		Path:               p.path,
		PathNodes:          p.nodes,
		PathNodeActivities: p.nodeActivities,
		Activities:         p.activities,
		Variants:           p.variants,
		ActivityConcepts:   p.activityConcepts,
		ActivityCitations:  p.activityCites,
		Concepts:           p.concepts,
		ConceptState:       p.mastery,
		ConceptModel:       p.model,
		MisconRepo:         p.miscon,
		Files:              p.files,
		Chunks:             p.chunks,
		UserProfile:        p.profile,
		TeachingPatterns:   p.patterns,
		Graph:              p.graph,
		AI:                 p.ai,
		Vec:                p.vec,
		Saga:               p.saga,
		Bootstrap:          p.bootstrap,
	}).RealizeActivities(jc.Ctx, learningmod.RealizeActivitiesInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("activities", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         actOut.PathID.String(),
		"nodes_written":   nodeOut.NodesWritten,
		"nodes_existing":  nodeOut.NodesExisting,
		"activities_made": actOut.ActivitiesMade,
		"variants_made":   actOut.VariantsMade,
	})
	return nil
}

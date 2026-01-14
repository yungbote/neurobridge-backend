package node_videos_plan_build

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
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("videos_plan", 2, "Planning videos")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Videos:    p.videos,
		GenRuns:   p.genRuns,
		Files:     p.files,
		Chunks:    p.chunks,
		AI:        p.ai,
		Vec:       p.vec,
		Bootstrap: p.bootstrap,
	}).NodeVideosPlanBuild(jc.Ctx, learningmod.NodeVideosPlanBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("videos_plan", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"nodes_planned":   out.NodesPlanned,
		"nodes_skipped":   out.NodesSkipped,
		"videos_planned":  out.VideosPlanned,
		"videos_existing": out.VideosExisting,
	})
	return nil
}

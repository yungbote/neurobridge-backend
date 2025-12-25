package node_videos_render

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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

	jc.Progress("videos_render", 2, "Rendering videos")
	out, err := steps.NodeVideosRender(jc.Ctx, steps.NodeVideosRenderDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Videos:    p.videos,
		Assets:    p.assets,
		GenRuns:   p.genRuns,
		AI:        p.ai,
		Bucket:    p.bucket,
		Bootstrap: p.bootstrap,
	}, steps.NodeVideosRenderInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("videos_render", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"videos_rendered": out.VideosRendered,
		"videos_existing": out.VideosExisting,
		"videos_failed":   out.VideosFailed,
	})
	return nil
}

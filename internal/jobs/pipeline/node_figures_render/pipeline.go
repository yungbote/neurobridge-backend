package node_figures_render

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

	jc.Progress("figures_render", 2, "Rendering figures")
	out, err := steps.NodeFiguresRender(jc.Ctx, steps.NodeFiguresRenderDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Figures:   p.figures,
		Assets:    p.assets,
		GenRuns:   p.genRuns,
		AI:        p.ai,
		Bucket:    p.bucket,
		Bootstrap: p.bootstrap,
	}, steps.NodeFiguresRenderInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("figures_render", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"figures_rendered": out.FiguresRendered,
		"figures_existing": out.FiguresExisting,
		"figures_failed":   out.FiguresFailed,
	})
	return nil
}

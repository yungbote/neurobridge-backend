package node_figures_plan_build

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

	jc.Progress("figures_plan", 2, "Planning figures")
	out, err := steps.NodeFiguresPlanBuild(jc.Ctx, steps.NodeFiguresPlanBuildDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Figures:   p.figures,
		GenRuns:   p.genRuns,
		Files:     p.files,
		Chunks:    p.chunks,
		AI:        p.ai,
		Vec:       p.vec,
		Bootstrap: p.bootstrap,
	}, steps.NodeFiguresPlanBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("figures_plan", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"nodes_planned":    out.NodesPlanned,
		"nodes_skipped":    out.NodesSkipped,
		"figures_planned":  out.FiguresPlanned,
		"figures_existing": out.FiguresExisting,
	})
	return nil
}

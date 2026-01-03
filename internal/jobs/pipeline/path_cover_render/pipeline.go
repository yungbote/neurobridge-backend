package path_cover_render

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
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

	jc.Progress("path_cover_render", 2, "Generating path avatar")
	pathID, err := p.bootstrap.EnsurePath(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, setID)
	if err != nil {
		if p.log != nil {
			p.log.Warn("path_cover_render bootstrap failed", "error", err, "material_set_id", setID.String())
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"skipped":         true,
		})
		return nil
	}

	out, err := steps.PathCoverRender(jc.Ctx, steps.PathCoverRenderDeps{
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Avatar:    p.avatar,
	}, steps.PathCoverRenderInput{PathID: pathID})
	if err != nil {
		if p.log != nil {
			p.log.Warn("path_cover_render failed", "error", err, "path_id", pathID.String())
		}
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"path_id":         pathID.String(),
		"generated":       out.Generated,
		"existing":        out.Existing,
		"url":             out.URL,
	})
	return nil
}

package path_grouping_refine

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
	if p == nil || p.db == nil || p.log == nil || p.path == nil || p.files == nil || p.fileSigs == nil {
		jc.Fail("validate", fmt.Errorf("path_grouping_refine: pipeline not configured"))
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	pathID, _ := jc.PayloadUUID("path_id")
	if pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}

	jc.Progress("refine", 2, "Refining path grouping")

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:       p.db,
		Log:      p.log,
		Path:     p.path,
		Files:    p.files,
		FileSigs: p.fileSigs,
	}).PathGroupingRefine(jc.Ctx, learningmod.PathGroupingRefineInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("refine", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"path_id":           pathID.String(),
		"status":            out.Status,
		"paths_before":      out.PathsBefore,
		"paths_after":       out.PathsAfter,
		"files_considered":  out.FilesConsidered,
		"confidence":        out.Confidence,
	})
	return nil
}

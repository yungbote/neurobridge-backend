package pipelines

import (
	"fmt"
	"github.com/google/uuid"
)

func (p *CourseBuildPipeline) loadAndValidate(buildCtx *buildContext) error {
	if buildCtx == nil || buildCtx.jobCtx == nil || buildCtx.jobCtx.Job == nil {
		return nil
	}
	// Required Payload
	materialSetID, ok := buildCtx.jobCtx.PayloadUUID("material_set_id")
	if !ok || materialSetID == uuid.Nil {
		return fmt.Errorf("missing material_set_id")
	}
	courseID, ok := buildCtx.jobCtx.PayloadUUID("course_id")
	if !ok || courseID == uuid.Nil {
		return fmt.Errorf("missing course_id")
	}
	buildCtx.materialSetID = materialSetID
	buildCtx.courseID = courseID
	// Load Course
	rows, err := p.courseRepo.GetByIDs(buildCtx.ctx, nil, []uuid.UUID{courseID})
	if err != nil || len(rows) == 0 || rows[0] == nil {
		return fmt.Errorf("course not found: %v", err)
	}
	buildCtx.course = rows[0]
	// Load Files
	files, err := p.materialFileRepo.GetByMaterialSetID(buildCtx.ctx, nil, materialSetID)
	if err != nil {
		return fmt.Errorf("load files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no material files found")
	}
	buildCtx.files = files
	ids := make([]uuid.UUID, 0, len(files))
	for _, mf := range files {
		if mf != nil {
			ids = append(ids, mf.ID)
		}
	}
	buildCtx.fileIDs = ids
	return nil
}











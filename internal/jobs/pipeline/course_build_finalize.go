package pipelines

import (
	"encoding/json"
	"fmt"
	"time"
	"gorm.io/datatypes"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *CourseBuildPipeline) stageFinalize(bc *buildContext) error {
	if bc == nil || bc.course == nil {
		return nil
	}
	// Finalize Course Metadata Status to Ready
	var currentMeta map[string]any
	_ = json.Unmarshal(bc.course.Metadata, &currentMeta)
	if currentMeta == nil {
		currentMeta = map[string]any{}
	}
	currentMeta["status"] = "ready"
	if err := p.db.WithContext(bc.ctx).Model(&types.Course{}).
		Where("id = ?", bc.courseID).
		Updates(map[string]any{
			"metadata":				datatypes.JSON(mustJSON(currentMeta)),
			"updated_at":			time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("update course ready status: %w", err)
	}
	// Update in-memory snapshot + push final course update
	bc.course.Metadata = datatypes.JSON(mustJSON(currentMeta))
	p.snapshot(bc)
	return nil
}











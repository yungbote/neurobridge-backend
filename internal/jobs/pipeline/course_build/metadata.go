package course_build

import (
	"fmt"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
	"strings"
	"time"
)

func (p *CourseBuildPipeline) stageMetadata(buildCtx *buildContext) error {
	if buildCtx == nil || buildCtx.course == nil {
		return nil
	}
	p.progress(buildCtx, "metadata", 50, "Ensuring course metadata exists")
	isPlaceholder := strings.TrimSpace(buildCtx.course.Title) == "" || strings.Contains(buildCtx.course.Title, "Generating course")
	if !isPlaceholder {
		return nil
	}
	metaSchema := courseMetadataSchema()
	metaObj, err := p.ai.GenerateJSON(buildCtx.ctx,
		"You generate concise, high quality course metadata from user-provided learning materials.",
		fmt.Sprintf(
			"Materials (truncated):\n%s\n\nReturn course metadata.\n\nRules:\n"+
				"- tags MUST be single words only (no spaces).\n"+
				"- tags MUST be lowercase and contain only letters/numbers.\n"+
				"- short_title: <= 64 chars.\n"+
				"- short_description: <= 140 chars.\n"+
				"- title: <= 120 chars.\n"+
				"- description: 2-10 sentences.\n",
			buildCtx.combined,
		),
		"course_metadata",
		metaSchema,
	)
	if err != nil {
		return err
	}
	shortTitle := clampString(fmt.Sprint(metaObj["short_title"]), 64)
	shortDesc := clampString(fmt.Sprint(metaObj["short_description"]), 140)
	longTitle := clampString(fmt.Sprint(metaObj["title"]), 120)
	longDesc := strings.TrimSpace(fmt.Sprint(metaObj["description"]))
	subject := strings.TrimSpace(fmt.Sprint(metaObj["subject"]))
	level := strings.TrimSpace(fmt.Sprint(metaObj["level"]))
	tags := normalizeTags(metaObj["tags"], 12)
	meta := map[string]any{
		"status":           "generating",
		"tags":             tags,
		"long_title":       longTitle,
		"long_description": longDesc,
	}
	if err := p.db.WithContext(buildCtx.ctx).Model(&types.Course{}).
		Where("id = ?", buildCtx.courseID).
		Updates(map[string]any{
			"title":       shortTitle,
			"description": shortDesc,
			"subject":     subject,
			"level":       level,
			"metadata":    datatypes.JSON(mustJSON(meta)),
			"updated_at":  time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("update course: %w", err)
	}
	// Update in memory snapshot + notify
	buildCtx.course.Title = shortTitle
	buildCtx.course.Description = shortDesc
	buildCtx.course.Subject = subject
	buildCtx.course.Level = level
	buildCtx.course.Metadata = datatypes.JSON(mustJSON(meta))
	p.snapshot(buildCtx)
	return nil
}

func courseMetadataSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"short_title":       map[string]any{"type": "string"},
			"short_description": map[string]any{"type": "string"},
			"title":             map[string]any{"type": "string"},
			"description":       map[string]any{"type": "string"},
			"subject":           map[string]any{"type": "string"},
			"level":             map[string]any{"type": "string"},
			"tags":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"short_title", "short_description", "title", "description", "subject", "level", "tags"},
		"additionalProperties": false,
	}
}

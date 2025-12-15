package pipelines

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *CourseBuildPipeline) stageBlueprint(buildCtx *buildContext) error {
	if buildCtx == nil || buildCtx.course == nil {
		return nil
	}

	p.progress(buildCtx, "blueprint", 60, "Ensuring course blueprint exists")

	// FIX: repo method is GetByCourseIDs (not GetByCourseID)
	existingModules, _ := p.moduleRepo.GetByCourseIDs(buildCtx.ctx, nil, []uuid.UUID{buildCtx.courseID})
	if len(existingModules) > 0 {
		return nil
	}

	blueprintSchema := courseBlueprintSchema()

	blueprintObj, err := p.ai.GenerateJSON(
		buildCtx.ctx,
		"You design structured, coherent course outlines to cover all of the material from a given set of learning materials.",
		fmt.Sprintf(
			"Course title: %s\nSubject: %s\nLevel: %s\nMaterials (truncated):\n%s\n\nCreate a course blueprint with both a number of modules and lessons per module dependent on how much it takes to cover the entirety of the material presented in the learning material. Keep titles specific and make sure to cover all content.",
			buildCtx.course.Title, buildCtx.course.Subject, buildCtx.course.Level, buildCtx.combined,
		),
		"course_blueprint",
		blueprintSchema,
	)
	if err != nil {
		return err
	}

	// Persist blueprint JSON
	blueprintJSON, _ := json.Marshal(blueprintObj)
	now := time.Now()

	cb := &types.CourseBlueprint{
		ID:            uuid.New(),
		MaterialSetID: buildCtx.materialSetID,
		UserID:        buildCtx.userID,
		BlueprintJSON: datatypes.JSON(blueprintJSON),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := p.blueprintRepo.Create(buildCtx.ctx, nil, []*types.CourseBlueprint{cb}); err != nil {
		return fmt.Errorf("save blueprint: %w", err)
	}

	// Parse modules
	modsAny, ok := blueprintObj["modules"].([]any)
	if !ok || len(modsAny) == 0 {
		return fmt.Errorf("blueprint modules missing or wrong type")
	}

	// Create CourseModule rows first
	modules := make([]*types.CourseModule, 0, len(modsAny))
	for i, m := range modsAny {
		mm, ok := m.(map[string]any)
		if !ok {
			return fmt.Errorf("blueprint module %d wrong type", i)
		}
		modules = append(modules, &types.CourseModule{
			ID:          uuid.New(),
			CourseID:    buildCtx.courseID,
			Index:       i,
			Title:       strings.TrimSpace(fmt.Sprint(mm["title"])),
			Description: strings.TrimSpace(fmt.Sprint(mm["description"])),
			Metadata:    datatypes.JSON([]byte(`{}`)),
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if _, err := p.moduleRepo.Create(buildCtx.ctx, nil, modules); err != nil {
		return fmt.Errorf("create modules: %w", err)
	}

	// Create Lesson rows referencing created modules
	lessons := make([]*types.Lesson, 0)

	for mi, m := range modsAny {
		mm, ok := m.(map[string]any)
		if !ok {
			return fmt.Errorf("blueprint module %d wrong type", mi)
		}

		lsAny, ok := mm["lessons"].([]any)
		if !ok {
			return fmt.Errorf("blueprint lessons missing or wrong type for module %d", mi)
		}

		for li, lraw := range lsAny {
			lm, ok := lraw.(map[string]any)
			if !ok {
				return fmt.Errorf("lesson %d in module %d wrong type", li, mi)
			}

			topics := toStringSlice(lm["topics"])
			est := intFromAny(lm["estimated_minutes"], 10)

			lessons = append(lessons, &types.Lesson{
				ID:               uuid.New(),
				ModuleID:         modules[mi].ID,
				Index:            li,
				Title:            strings.TrimSpace(fmt.Sprint(lm["title"])),
				Kind:             "reading",
				EstimatedMinutes: est,
				Metadata:         datatypes.JSON(mustJSON(map[string]any{"topics": topics})),
				CreatedAt:        now,
				UpdatedAt:        now,
			})
		}
	}

	if _, err := p.lessonRepo.Create(buildCtx.ctx, nil, lessons); err != nil {
		return fmt.Errorf("create lessons: %w", err)
	}

	return nil
}

func courseBlueprintSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"lessons": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"title":             map[string]any{"type": "string"},
									"topics":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									"estimated_minutes": map[string]any{"type": "integer"},
								},
								"required":             []string{"title", "topics", "estimated_minutes"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"title", "description", "lessons"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"modules"},
		"additionalProperties": false,
	}
}











package pipelines

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"gorm.io/datatypes"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *CourseBuildPipeline) stageLessonsAndQuizzes(bc *buildContext) error {
	if bc == nil || bc.course == nil {
		return nil
	}
	p.progress(bc, "lessons", 75, "Generating missing lessons/quizzes")
	// Load Modules/Lessons
	modules, err := p.moduleRepo.GetByCourseIDs(bc.ctx, nil, []uuid.UUID{bc.courseID})
	if err != nil || len(modules) == 0 {
		return fmt.Errorf("no modules found")
	}
	moduleIDs := make([]uuid.UUID, 0, len(modules))
	for _, m := range modules {
		if m != nil {
			moduleIDs = append(moduleIDs, m.ID)
		}
	}
	lessons, err := p.lessonRepo.GetByModuleIDs(bc.ctx, nil, moduleIDs)
	if err != nil || len(lessons) == 0 {
		return fmt.Errorf("no lessons found")
	}
	// Build Vector Index From Chunks
	chunkVectors := make([]chunkWithVec, 0, len(bc.chunks))
	for _, ch := range bc.chunks {
		if ch == nil {
			continue
		}
		vec, ok := parseEmbedding(ch.Embedding)
		if !ok {
			continue
		}
		chunkVectors = append(chunkVectors, chunkWithVec{Chunk: ch, Vec: vec})
	}
	if len(chunkVectors) == 0 {
		return fmt.Errorf("no embeddings available for retrieval")
	}
	lessonIDs := make([]uuid.UUID, 0, len(lessons))
	for _, l := range lessons {
		if l != nil {
			lessonIDs = append(lessonIDs, l.ID)
		}
	}
	existingQuestions, _ := p.quizRepo.GetByLessonIDs(bc.ctx, nil, lessonIDs)
	hasQuiz := map[uuid.UUID]bool{}
	for _, q := range existingQuestions {
		if q != nil {
			hasQuiz[q.LessonID] = true
		}
	}
	total := len(lessons)
	done := 0
	for _, l := range lessons {
		if l == nil {
			continue
		}
		// old idempotency: if ContentMD exists, skip
		if strings.TrimSpace(l.ContentMD) != "" {
			done++
			continue
		}
		topics := extractTopicsFromLessonMetadata(l.Metadata)
		query := l.Title + " " + strings.Join(topics, " ")
		qVecs, err := p.ai.Embed(bc.ctx, []string{query})
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}
		top := topKChunks(chunkVectors, qVecs[0], 10)
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString("You MUST ground the lesson in the provided excerpts.\nExcerpts:\n")
		for _, t := range top {
			ctxBuilder.WriteString(fmt.Sprintf("[chunk_id=%s] %s\n", t.Chunk.ID.String(), truncate(t.Chunk.Text, 800)))
		}
		lessonSchema := lessonContentSchemaOld()
		out, err := p.ai.GenerateJSON(bc.ctx,
			"You write clear lessons full lessons and include citations as chunk_id strings from the provided excerpts. Never cite anything not in excerpts.",
			fmt.Sprintf(
				"Lesson title: %s\nTopics: %s\n\n%s\n\nWrite two versions: summary and full lesson. Return citations as a list of chunk_id strings you actually used.",
				l.Title, strings.Join(topics, ", "), ctxBuilder.String(),
			),
			"lesson_content",
			lessonSchema,
		)
		if err != nil {
			return err
		}
		summary := fmt.Sprint(out["concise_md"])
		step := fmt.Sprint(out["step_by_step_md"])
		citations := toStringSlice(out["citations"])
		est := intFromAny(out["estimated_minutes"], l.EstimatedMinutes)
		meta := map[string]any{
			"topics":			topics,
			"variants":		map[string]any{
				"concise_md":				summary,
				"step_by_step_md":	step,
			},
			"citations":	citations,
		}
		if err := p.db.WithContext(bc.ctx).Model(&types.Lesson{}).
			Where("id = ?", l.ID).
			Updates(map[string]any{
				"content_md":						summary,
				"estimated_minutes":		est,
				"metadata":							datatypes.JSON(mustJSON(meta)),
				"updated_at":						time.Now(),
			}).Error; err != nil {
			return fmt.Errorf("update lesson: %w", err)
		}
		// Quiz Generation (old behavior)
		if !hasQuiz[l.ID] {
			quizSchema := lessonQuizSchemaOld()
			quizOut, err := p.ai.GenerateJSON(bc.ctx,
				"You generate fair quiz questions based strictly on the material in the lesson. Use MCQ only.",
				fmt.Sprintf("Lesson:\n%s\n\nGenerate Any the appropriate amount of multiple-choice questions with an appropriate amount of options each dependent on the material for the lesson.", summary),
				"lesson_quiz",
				quizSchema,
			)
			if err != nil {
				return err
			}
			qsAny, ok := quizOut["questions"].([]any)
			if !ok {
				return fmt.Errorf("quiz questions missing or wrong type")
			}
			qs := make([]*types.QuizQuestion, 0, len(qsAny))
			for qi, qraw := range qsAny {
				qm := qraw.(map[string]any)
				opts := toStringSlice(qm["options"])
				correct := intFromAny(qm["correct_index"], 0)
				optsJSON, _ := json.Marshal(opts)
				correctJSON, _ := json.Marshal(map[string]any{"index": correct})
				qs = append(qs, &types.QuizQuestion{
					ID:									uuid.New(),
					LessonID:						l.ID,
					Index:							qi,
					Type:								"mcq",
					PromptMD:						fmt.Sprint(qm["prompt_md"]),
					Options:						datatypes.JSON(optsJSON),
					CorrectAnswer:			datatypes.JSON(correctJSON),
					ExplanationMD:			fmt.Sprint(qm["explanation_md"]),
					Metadata:						datatypes.JSON(mustJSON(map[string]any{"topics": topics, "citations": citations})),
					CreatedAt:					time.Now(),
					UpdatedAt:					time.Now(),
				})
			}
			if _, err := p.quizRepo.Create(bc.ctx, nil, qs); err != nil {
				return fmt.Errorf("create quiz: %w", err)
			}
		}
		done++
		pct := 75 + int(float64(done)/float64(max(1, total))*20.0)
		p.progress(bc, "lessons", pct, fmt.Sprintf("Generated %d/%d lessons", done, total))
	}
	return nil
}


func lessonContentSchemaOld() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"concise_md":        map[string]any{"type": "string"},
			"step_by_step_md":   map[string]any{"type": "string"},
			"citations":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"estimated_minutes": map[string]any{"type": "integer"},
		},
		"required":             []string{"concise_md", "step_by_step_md", "citations", "estimated_minutes"},
		"additionalProperties": false,
	}
}

func lessonQuizSchemaOld() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":           map[string]any{"type": "string"},
						"prompt_md":      map[string]any{"type": "string"},
						"options":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"correct_index":  map[string]any{"type": "integer"},
						"explanation_md": map[string]any{"type": "string"},
					},
					"required":             []string{"type", "prompt_md", "options", "correct_index", "explanation_md"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"questions"},
		"additionalProperties": false,
	}
}











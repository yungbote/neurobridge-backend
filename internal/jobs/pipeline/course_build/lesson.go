package course_build

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
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

	// Build a concept lookup map (optional)
	conceptLookup := map[string]conceptNode{}
	{
		var meta map[string]any
		_ = json.Unmarshal(bc.course.Metadata, &meta)
		if v, ok := meta["concept_map"]; ok {
			b, _ := json.Marshal(v)
			var cm conceptMap
			if err := json.Unmarshal(b, &cm); err == nil {
				for _, c := range cm.Concepts {
					if strings.TrimSpace(c.ID) != "" {
						conceptLookup[c.ID] = c
					}
				}
			}
		}
	}

	total := len(lessons)
	done := 0

	// Pinecone namespace
	ns := fmt.Sprintf("chunks:material_set:%s", bc.materialSetID.String())

	for _, l := range lessons {
		if l == nil {
			continue
		}

		// idempotency: if ContentMD exists, skip
		if strings.TrimSpace(l.ContentMD) != "" {
			done++
			continue
		}

		// Prefer concept_ids (v2), fallback to topics (old)
		conceptIDs := extractStringArrayFromLessonMetadata(l.Metadata, "concept_ids")
		topics := extractTopicsFromLessonMetadata(l.Metadata)

		// Build query: title + (concept names/summaries) + topics
		var qParts []string
		qParts = append(qParts, l.Title)
		for _, cid := range conceptIDs {
			if cn, ok := conceptLookup[cid]; ok {
				qParts = append(qParts, cn.Name)
				if cn.Summary != "" {
					qParts = append(qParts, cn.Summary)
				}
			} else {
				qParts = append(qParts, cid)
			}
		}
		if len(topics) > 0 {
			qParts = append(qParts, strings.Join(topics, " "))
		}
		query := strings.Join(qParts, " ")

		qVecs, err := p.ai.Embed(bc.ctx, []string{query})
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}
		qVec := qVecs[0]

		// Retrieve top chunks (pinecone first, fallback local cosine)
		var topChunks []*types.MaterialChunk

		if p.vectorStore != nil {
			ids, qErr := p.vectorStore.QueryIDs(bc.ctx, ns, qVec, 12, map[string]any{
				"type": "chunk",
			})
			if qErr != nil {
				p.log.Warn("pinecone query failed; falling back to local retrieval", "err", qErr.Error())
			} else if len(ids) > 0 {
				uids := make([]uuid.UUID, 0, len(ids))
				for _, s := range ids {
					id, err := uuid.Parse(strings.TrimSpace(s))
					if err == nil && id != uuid.Nil {
						uids = append(uids, id)
					}
				}

				rows, err := p.chunkRepo.GetByIDs(bc.ctx, nil, uids)
				if err == nil && len(rows) > 0 {
					byID := map[uuid.UUID]*types.MaterialChunk{}
					for _, r := range rows {
						if r != nil {
							byID[r.ID] = r
						}
					}
					// preserve pinecone rank order
					for _, s := range ids {
						id, err := uuid.Parse(strings.TrimSpace(s))
						if err != nil {
							continue
						}
						if ch := byID[id]; ch != nil {
							topChunks = append(topChunks, ch)
						}
					}
				}
			}
		}

		if len(topChunks) == 0 {
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
				return fmt.Errorf("no embeddings available for retrieval (pinecone empty and local missing)")
			}

			top := topKChunks(chunkVectors, qVec, 12)
			for _, t := range top {
				if t.Chunk != nil {
					topChunks = append(topChunks, t.Chunk)
				}
			}
		}

		if len(topChunks) == 0 {
			return fmt.Errorf("retrieval returned 0 chunks")
		}

		var ctxBuilder strings.Builder
		ctxBuilder.WriteString("You MUST ground the lesson in the provided excerpts.\nExcerpts:\n")
		for _, ch := range topChunks {
			if ch == nil {
				continue
			}
			ctxBuilder.WriteString(fmt.Sprintf(
				"[chunk_id=%s] %s\n",
				ch.ID.String(),
				truncate(ch.Text, 900),
			))
		}

		lessonSchema := lessonContentSchemaOld()
		out, err := p.ai.GenerateJSON(
			bc.ctx,
			"You are a medical educator writing polished, high-quality lesson content.\n\n"+
				"Rules:\n"+
				"- Use ONLY the excerpts as factual grounding.\n"+
				"- Do NOT include any frontend/UI/source code.\n"+
				"- Produce a real lesson with structure: headings, subheadings, clinical reasoning where appropriate.\n"+
				"- citations MUST be chunk_id strings used.\n",
			fmt.Sprintf(
				"Lesson title: %s\nConcept IDs: %s\nTopics: %s\n\n%s\n\n"+
					"Write two versions:\n"+
					"1) concise_md: short high-signal summary\n"+
					"2) step_by_step_md: full structured lesson with headings and sections\n"+
					"Return citations as a list of chunk_id strings you actually used.\n",
				l.Title,
				strings.Join(conceptIDs, ", "),
				strings.Join(topics, ", "),
				ctxBuilder.String(),
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
			"topics":      topics,
			"concept_ids": conceptIDs,
			"variants": map[string]any{
				"concise_md":      summary,
				"step_by_step_md": step,
			},
			"citations": citations,
		}

		// IMPORTANT: store FULL lesson in content_md
		if err := p.db.WithContext(bc.ctx).Model(&types.Lesson{}).
			Where("id = ?", l.ID).
			Updates(map[string]any{
				"content_md":        step,
				"estimated_minutes": est,
				"metadata":          datatypes.JSON(mustJSON(meta)),
				"updated_at":        time.Now(),
			}).Error; err != nil {
			return fmt.Errorf("update lesson: %w", err)
		}

		// Quiz Generation (existing behavior preserved)
		if !hasQuiz[l.ID] {
			quizSchema := lessonQuizSchemaOld()
			quizOut, err := p.ai.GenerateJSON(
				bc.ctx,
				"You generate fair quiz questions based strictly on the material in the lesson. Use MCQ only.",
				fmt.Sprintf(
					"Lesson:\n%s\n\nGenerate the appropriate number of multiple-choice questions with an appropriate number of options each dependent on the material for the lesson.",
					summary,
				),
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

			now := time.Now()
			qs := make([]*types.QuizQuestion, 0, len(qsAny))
			for qi, qraw := range qsAny {
				qm := qraw.(map[string]any)
				opts := toStringSlice(qm["options"])
				correct := intFromAny(qm["correct_index"], 0)
				optsJSON, _ := json.Marshal(opts)
				correctJSON, _ := json.Marshal(map[string]any{"index": correct})

				qs = append(qs, &types.QuizQuestion{
					ID:            uuid.New(),
					LessonID:      l.ID,
					Index:         qi,
					Type:          "mcq",
					PromptMD:      fmt.Sprint(qm["prompt_md"]),
					Options:       datatypes.JSON(optsJSON),
					CorrectAnswer: datatypes.JSON(correctJSON),
					ExplanationMD: fmt.Sprint(qm["explanation_md"]),
					Metadata:      datatypes.JSON(mustJSON(map[string]any{"topics": topics, "citations": citations})),
					CreatedAt:     now,
					UpdatedAt:     now,
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

func extractStringArrayFromLessonMetadata(js datatypes.JSON, key string) []string {
	if len(js) == 0 {
		return []string{}
	}
	var m map[string]any
	if err := json.Unmarshal(js, &m); err != nil {
		return []string{}
	}
	v, ok := m[key]
	if !ok {
		return []string{}
	}
	return toStringSlice(v)
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

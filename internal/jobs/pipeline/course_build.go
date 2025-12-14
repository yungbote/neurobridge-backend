package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/jobs"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type CourseBuildPipeline struct {
	db  *gorm.DB
	log *logger.Logger
	courseRepo      repos.CourseRepo
	materialFileRepo repos.MaterialFileRepo
	moduleRepo      repos.CourseModuleRepo
	lessonRepo      repos.LessonRepo
	quizRepo        repos.QuizQuestionRepo
	blueprintRepo   repos.CourseBlueprintRepo
	chunkRepo       repos.MaterialChunkRepo
	bucket services.BucketService
	ai     services.OpenAIClient
}

func NewCourseBuildPipeline(
	db *gorm.DB,
	baseLog *logger.Logger,
	courseRepo repos.CourseRepo,
	materialFileRepo repos.MaterialFileRepo,
	moduleRepo repos.CourseModuleRepo,
	lessonRepo repos.LessonRepo,
	quizRepo repos.QuizQuestionRepo,
	blueprintRepo repos.CourseBlueprintRepo,
	chunkRepo repos.MaterialChunkRepo,
	bucket services.BucketService,
	ai services.OpenAIClient,
) *CourseBuildPipeline {
	return &CourseBuildPipeline{
		db: db,
		log: baseLog.With("job", "course_build"),
		courseRepo: courseRepo,
		materialFileRepo: materialFileRepo,
		moduleRepo: moduleRepo,
		lessonRepo: lessonRepo,
		quizRepo: quizRepo,
		blueprintRepo: blueprintRepo,
		chunkRepo: chunkRepo,
		bucket: bucket,
		ai: ai,
	}
}

func (p *CourseBuildPipeline) Type() string { return "course_build" }

func (p *CourseBuildPipeline) Run(jc *jobs.Context) error {
	ctx := jc.Ctx
	if jc.Job == nil {
		return nil
	}
	// Required payload
	materialSetID, ok := jc.PayloadUUID("material_set_id")
	if !ok || materialSetID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	courseID, ok := jc.PayloadUUID("course_id")
	if !ok || courseID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing course_id"))
		return nil
	}
	// 0) Load files for material set
	files, err := p.materialFileRepo.GetByMaterialSetID(ctx, nil, materialSetID)
	if err != nil {
		jc.Fail("ingest", fmt.Errorf("load files: %w", err))
		return nil
	}
	if len(files) == 0 {
		jc.Fail("ingest", fmt.Errorf("no material files found"))
		return nil
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, mf := range files {
		if mf != nil {
			fileIDs = append(fileIDs, mf.ID)
		}
	}
	// 1) INGEST (idempotent): ensure chunks exist per file
	jc.Progress("ingest", 5, "Ensuring extracted chunks exist")
	existingChunks, err := p.chunkRepo.GetByMaterialFileIDs(ctx, nil, fileIDs)
	if err != nil {
		jc.Fail("ingest", fmt.Errorf("load existing chunks: %w", err))
		return nil
	}
	hasChunks := map[uuid.UUID]bool{}
	for _, ch := range existingChunks {
		if ch != nil && ch.MaterialFileID != uuid.Nil {
			hasChunks[ch.MaterialFileID] = true
		}
	}
	for i, mf := range files {
		if mf == nil || hasChunks[mf.ID] {
			continue
		}
		rc, err := p.bucket.DownloadFile(ctx, services.BucketCategoryMaterial, mf.StorageKey)
		if err != nil {
			jc.Fail("ingest", fmt.Errorf("download file %s: %w", mf.ID, err))
			return nil
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			jc.Fail("ingest", fmt.Errorf("read file %s: %w", mf.OriginalName, readErr))
			return nil
		}
		text, err := services.ExtractText(mf.OriginalName, mf.MimeType, data)
		if err != nil {
			jc.Fail("ingest", fmt.Errorf("extract text %s: %w", mf.OriginalName, err))
			return nil
		}
		chunks := chunkText(mf.ID, text, 1200, 200)
		if _, err := p.chunkRepo.Create(ctx, nil, chunks); err != nil {
			jc.Fail("ingest", fmt.Errorf("create chunks: %w", err))
			return nil
		}
		jc.Progress("ingest", 5+int(float64(i+1)/float64(len(files))*20.0), fmt.Sprintf("Extracted %s", mf.OriginalName))
	}
	// Reload chunks now that we ensured they exist
	chunks, err := p.chunkRepo.GetByMaterialFileIDs(ctx, nil, fileIDs)
	if err != nil {
		jc.Fail("ingest", fmt.Errorf("load chunks after ingest: %w", err))
		return nil
	}
	if len(chunks) == 0 {
		jc.Fail("ingest", fmt.Errorf("no chunks available after ingest"))
		return nil
	}
	// 2) EMBED (idempotent)
	jc.Progress("embed", 30, "Embedding missing chunks")
	missing := make([]*types.MaterialChunk, 0)
	for _, ch := range chunks {
		if ch != nil && len(ch.Embedding) == 0 {
			missing = append(missing, ch)
		}
	}
	const batchSize = 64
	for start := 0; start < len(missing); start += batchSize {
		end := start + batchSize
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]
		inputs := make([]string, len(batch))
		for i, ch := range batch {
			inputs[i] = ch.Text
		}
		vecs, err := p.ai.Embed(ctx, inputs)
		if err != nil {
			jc.Fail("embed", fmt.Errorf("embed: %w", err))
			return nil
		}
		for i, ch := range batch {
			b, _ := json.Marshal(vecs[i])
			if err := p.db.WithContext(ctx).Model(&types.MaterialChunk{}).
				Where("id = ?", ch.ID).
				Updates(map[string]any{
					"embedding":  datatypes.JSON(b),
					"updated_at": time.Now(),
				}).Error; err != nil {
				jc.Fail("embed", fmt.Errorf("update chunk embedding: %w", err))
				return nil
			}
			ch.Embedding = datatypes.JSON(b)
		}
		jc.Progress("embed", 30+int(float64(end)/float64(max(1, len(missing)))*15.0), "Embedded chunk batch")
	}
	combined := buildCombinedFromChunks(chunks, 20000)
	if combined == "" {
		jc.Fail("metadata", fmt.Errorf("no combined materials text available"))
		return nil
	}
	// 3) METADATA (idempotent): fill course if placeholder
	jc.Progress("metadata", 50, "Ensuring course metadata exists")
	courseRows, err := p.courseRepo.GetByIDs(ctx, nil, []uuid.UUID{courseID})
	if err != nil || len(courseRows) == 0 || courseRows[0] == nil {
		jc.Fail("metadata", fmt.Errorf("load course failed: %v", err))
		return nil
	}
	course := courseRows[0]
	isPlaceholder := strings.TrimSpace(course.Title) == "" || strings.Contains(course.Title, "Generating course")
	if isPlaceholder {
		metaSchema := map[string]any{
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
			"required": []string{"short_title", "short_description", "title", "description", "subject", "level", "tags"},
			"additionalProperties": false,
		}
		metaObj, err := p.ai.GenerateJSON(ctx,
			"You generate concise, high-quality course metadata from user-provided learning materials.",
			fmt.Sprintf(
				"Materials (truncated):\n%s\n\nReturn course metadata.\n\nRules:\n"+
					"- tags MUST be single words only (no spaces).\n"+
					"- tags must be lowercase and contain only letters/numbers.\n"+
					"- short_title: <= 64 chars.\n"+
					"- short_description: <= 140 chars.\n"+
					"- title: <= 120 chars.\n"+
					"- description: 2-4 sentences.\n",
				combined,
			),
			"course_metadata",
			metaSchema,
		)
		if err != nil {
			jc.Fail("metadata", err)
			return nil
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
		if err := p.db.WithContext(ctx).Model(&types.Course{}).
			Where("id = ?", courseID).
			Updates(map[string]any{
				"title":       shortTitle,
				"description": shortDesc,
				"subject":     subject,
				"level":       level,
				"metadata":    datatypes.JSON(mustJSON(meta)),
				"updated_at":  time.Now(),
			}).Error; err != nil {
			jc.Fail("metadata", fmt.Errorf("update course: %w", err))
			return nil
		}
		course.Title = shortTitle
		course.Description = shortDesc
		course.Subject = subject
		course.Level = level
		course.Metadata = datatypes.JSON(mustJSON(meta))
	}
	// 4) BLUEPRINT (idempotent)
	jc.Progress("blueprint", 60, "Ensuring course blueprint exists")
	existingModules, _ := p.moduleRepo.GetByCourseIDs(ctx, nil, []uuid.UUID{courseID})
	if len(existingModules) == 0 {
		blueprintSchema := map[string]any{
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
									"required": []string{"title", "topics", "estimated_minutes"},
									"additionalProperties": false,
								},
							},
						},
						"required": []string{"title", "description", "lessons"},
						"additionalProperties": false,
					},
				},
			},
			"required": []string{"modules"},
			"additionalProperties": false,
		}
		blueprintObj, err := p.ai.GenerateJSON(ctx,
			"You design structured, coherent course outlines from learning materials.",
			fmt.Sprintf(
				"Course title: %s\nSubject: %s\nLevel: %s\n\nMaterials (truncated):\n%s\n\nCreate a course blueprint with 3-6 modules and 2-6 lessons per module. Keep titles specific.",
				course.Title, course.Subject, course.Level, combined,
			),
			"course_blueprint",
			blueprintSchema,
		)
		if err != nil {
			jc.Fail("blueprint", err)
			return nil
		}
		blueprintJSON, _ := json.Marshal(blueprintObj)
		cb := &types.CourseBlueprint{
			ID:            uuid.New(),
			MaterialSetID: materialSetID,
			UserID:        jc.Job.OwnerUserID,
			BlueprintJSON: datatypes.JSON(blueprintJSON),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if _, err := p.blueprintRepo.Create(ctx, nil, []*types.CourseBlueprint{cb}); err != nil {
			jc.Fail("blueprint", fmt.Errorf("save blueprint: %w", err))
			return nil
		}
		modsAny, ok := blueprintObj["modules"].([]any)
		if !ok {
			jc.Fail("blueprint", fmt.Errorf("blueprint modules missing or wrong type"))
			return nil
		}
		now := time.Now()
		modules := make([]*types.CourseModule, 0, len(modsAny))
		for i, m := range modsAny {
			mm := m.(map[string]any)
			modules = append(modules, &types.CourseModule{
				ID:          uuid.New(),
				CourseID:    courseID,
				Index:       i,
				Title:       fmt.Sprint(mm["title"]),
				Description: fmt.Sprint(mm["description"]),
				Metadata:    datatypes.JSON([]byte(`{}`)),
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
		if _, err := p.moduleRepo.Create(ctx, nil, modules); err != nil {
			jc.Fail("blueprint", fmt.Errorf("create modules: %w", err))
			return nil
		}
		lessons := make([]*types.Lesson, 0)
		for mi, m := range modsAny {
			mm := m.(map[string]any)
			ls := mm["lessons"].([]any)
			for li, lraw := range ls {
				lm := lraw.(map[string]any)
				topics := toStringSlice(lm["topics"])
				est := intFromAny(lm["estimated_minutes"], 10)

				lessons = append(lessons, &types.Lesson{
					ID:               uuid.New(),
					ModuleID:         modules[mi].ID,
					Index:            li,
					Title:            fmt.Sprint(lm["title"]),
					Kind:             "reading",
					EstimatedMinutes: est,
					Metadata:         datatypes.JSON(mustJSON(map[string]any{"topics": topics})),
					CreatedAt:        now,
					UpdatedAt:        now,
				})
			}
		}
		if _, err := p.lessonRepo.Create(ctx, nil, lessons); err != nil {
			jc.Fail("blueprint", fmt.Errorf("create lessons: %w", err))
			return nil
		}
	}
	// 5) LESSONS + QUIZZES (idempotent)
	jc.Progress("lessons", 75, "Generating missing lessons/quizzes")
	modules, err := p.moduleRepo.GetByCourseIDs(ctx, nil, []uuid.UUID{courseID})
	if err != nil || len(modules) == 0 {
		jc.Fail("lessons", fmt.Errorf("no modules found"))
		return nil
	}
	moduleIDs := make([]uuid.UUID, 0, len(modules))
	for _, m := range modules {
		if m != nil {
			moduleIDs = append(moduleIDs, m.ID)
		}
	}
	lessons, err := p.lessonRepo.GetByModuleIDs(ctx, nil, moduleIDs)
	if err != nil || len(lessons) == 0 {
		jc.Fail("lessons", fmt.Errorf("no lessons found"))
		return nil
	}
	chunkVectors := make([]chunkWithVec, 0, len(chunks))
	for _, ch := range chunks {
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
		jc.Fail("lessons", fmt.Errorf("no embeddings available for retrieval"))
		return nil
	}
	lessonIDs := make([]uuid.UUID, 0, len(lessons))
	for _, l := range lessons {
		if l != nil {
			lessonIDs = append(lessonIDs, l.ID)
		}
	}
	existingQuestions, _ := p.quizRepo.GetByLessonIDs(ctx, nil, lessonIDs)
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
		if strings.TrimSpace(l.ContentMD) != "" {
			done++
			continue
		}
		topics := extractTopicsFromLessonMetadata(l.Metadata)
		query := l.Title + " " + strings.Join(topics, " ")
		qVecs, err := p.ai.Embed(ctx, []string{query})
		if err != nil {
			jc.Fail("lessons", fmt.Errorf("embed query: %w", err))
			return nil
		}
		top := topKChunks(chunkVectors, qVecs[0], 10)
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString("You MUST ground the lesson in the provided excerpts.\nExcerpts:\n")
		for _, t := range top {
			ctxBuilder.WriteString(fmt.Sprintf("[chunk_id=%s] %s\n", t.Chunk.ID.String(), truncate(t.Chunk.Text, 800)))
		}
		lessonSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"concise_md":        map[string]any{"type": "string"},
				"step_by_step_md":   map[string]any{"type": "string"},
				"citations":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"estimated_minutes": map[string]any{"type": "integer"},
			},
			"required": []string{"concise_md", "step_by_step_md", "citations", "estimated_minutes"},
			"additionalProperties": false,
		}
		out, err := p.ai.GenerateJSON(ctx,
			"You write clear lessons and include citations as chunk_id strings from the provided excerpts. Never cite anything not in excerpts.",
			fmt.Sprintf(
				"Lesson title: %s\nTopics: %s\n\n%s\n\nWrite two versions: concise and step-by-step. Return citations as a list of chunk_id strings you actually used.",
				l.Title, strings.Join(topics, ", "), ctxBuilder.String(),
			),
			"lesson_content",
			lessonSchema,
		)
		if err != nil {
			jc.Fail("lessons", err)
			return nil
		}
		concise := fmt.Sprint(out["concise_md"])
		step := fmt.Sprint(out["step_by_step_md"])
		citations := toStringSlice(out["citations"])
		est := intFromAny(out["estimated_minutes"], l.EstimatedMinutes)
		meta := map[string]any{
			"topics": topics,
			"variants": map[string]any{
				"concise_md":      concise,
				"step_by_step_md": step,
			},
			"citations": citations,
		}
		if err := p.db.WithContext(ctx).Model(&types.Lesson{}).
			Where("id = ?", l.ID).
			Updates(map[string]any{
				"content_md":        concise,
				"estimated_minutes": est,
				"metadata":          datatypes.JSON(mustJSON(meta)),
				"updated_at":        time.Now(),
			}).Error; err != nil {
			jc.Fail("lessons", fmt.Errorf("update lesson: %w", err))
			return nil
		}
		if !hasQuiz[l.ID] {
			quizSchema := map[string]any{
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
							"required": []string{"type", "prompt_md", "options", "correct_index", "explanation_md"},
							"additionalProperties": false,
						},
					},
				},
				"required": []string{"questions"},
				"additionalProperties": false,
			}
			quizOut, err := p.ai.GenerateJSON(ctx,
				"You generate fair quiz questions based strictly on the lesson. Use MCQ only.",
				fmt.Sprintf("Lesson:\n%s\n\nGenerate 5 multiple-choice questions with 4 options each.", concise),
				"lesson_quiz",
				quizSchema,
			)
			if err != nil {
				jc.Fail("quizzes", err)
				return nil
			}
			qsAny, ok := quizOut["questions"].([]any)
			if !ok {
				jc.Fail("quizzes", fmt.Errorf("quiz questions missing or wrong type"))
				return nil
			}
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
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				})
			}
			if _, err := p.quizRepo.Create(ctx, nil, qs); err != nil {
				jc.Fail("quizzes", fmt.Errorf("create quiz: %w", err))
				return nil
			}
		}
		done++
		jc.Progress("lessons", 75+int(float64(done)/float64(max(1, total))*20.0), fmt.Sprintf("Generated %d/%d lessons", done, total))
	}
	// Finalize course metadata status to ready
	var currentMeta map[string]any
	_ = json.Unmarshal(course.Metadata, &currentMeta)
	if currentMeta == nil {
		currentMeta = map[string]any{}
	}
	currentMeta["status"] = "ready"

	if err := p.db.WithContext(ctx).Model(&types.Course{}).
		Where("id = ?", courseID).
		Updates(map[string]any{
			"metadata":   datatypes.JSON(mustJSON(currentMeta)),
			"updated_at": time.Now(),
		}).Error; err != nil {
		jc.Fail("done", fmt.Errorf("update course ready status: %w", err))
		return nil
	}

	jc.Succeed("done", map[string]any{
		"course_id":       courseID.String(),
		"material_set_id": materialSetID.String(),
	})

	return nil
}

// ---------------- helpers ----------------

func buildCombinedFromChunks(chunks []*types.MaterialChunk, maxLen int) string {
	var b strings.Builder
	for _, ch := range chunks {
		if ch == nil || strings.TrimSpace(ch.Text) == "" {
			continue
		}
		if b.Len() >= maxLen {
			break
		}
		s := ch.Text
		if b.Len()+len(s)+2 > maxLen {
			s = s[:max(0, maxLen-b.Len()-2)]
		}
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func normalizeOneWordTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")

	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		}
	}
	return string(out)
}

func normalizeTags(v any, maxN int) []string {
	raw := toStringSlice(v)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		tt := normalizeOneWordTag(t)
		if tt == "" || seen[tt] {
			continue
		}
		seen[tt] = true
		out = append(out, tt)
		if maxN > 0 && len(out) >= maxN {
			break
		}
	}
	return out
}

func clampString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return strings.TrimSpace(s[:maxLen])
}

func extractTopicsFromLessonMetadata(js datatypes.JSON) []string {
	if len(js) == 0 {
		return []string{}
	}
	var m map[string]any
	if err := json.Unmarshal(js, &m); err != nil {
		return []string{}
	}
	v, ok := m["topics"]
	if !ok {
		return []string{}
	}
	return toStringSlice(v)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func chunkText(fileID uuid.UUID, text string, chunkSize int, overlap int) []*types.MaterialChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return []*types.MaterialChunk{}
	}
	if chunkSize < 200 {
		chunkSize = 200
	}
	if overlap < 0 {
		overlap = 0
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	out := []*types.MaterialChunk{}
	idx := 0
	for start := 0; start < len(text); start += step {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		piece := strings.TrimSpace(text[start:end])
		if piece != "" {
			out = append(out, &types.MaterialChunk{
				ID:             uuid.New(),
				MaterialFileID: fileID,
				Index:          idx,
				Text:           piece,
				Metadata:       datatypes.JSON(mustJSON(map[string]any{"start": start, "end": end})),
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			})
			idx++
		}
		if end == len(text) {
			break
		}
	}
	return out
}

type chunkWithVec struct {
	Chunk *types.MaterialChunk
	Vec   []float32
}

func parseEmbedding(js datatypes.JSON) ([]float32, bool) {
	if len(js) == 0 {
		return nil, false
	}
	var v []float32
	if err := json.Unmarshal(js, &v); err != nil {
		var f64 []float64
		if err2 := json.Unmarshal(js, &f64); err2 != nil {
			return nil, false
		}
		v = make([]float32, len(f64))
		for i := range f64 {
			v[i] = float32(f64[i])
		}
	}
	if len(v) == 0 {
		return nil, false
	}
	return v, true
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return -1
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return -1
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func topKChunks(chunks []chunkWithVec, q []float32, k int) []chunkWithVec {
	type scored struct {
		c chunkWithVec
		s float64
	}
	arr := make([]scored, 0, len(chunks))
	for _, ch := range chunks {
		arr = append(arr, scored{c: ch, s: cosine(ch.Vec, q)})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	if k > len(arr) {
		k = len(arr)
	}
	out := make([]chunkWithVec, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, arr[i].c)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func toStringSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	a, ok := v.([]any)
	if !ok {
		if ss, ok2 := v.([]string); ok2 {
			return ss
		}
		return []string{}
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}











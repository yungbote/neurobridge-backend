package services

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

  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/sse"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type CourseGenerationService interface {
  EnqueueFromMaterialSet(ctx context.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.Course, *types.CourseGenerationRun, error)
  StartWorker(ctx context.Context)
}

type courseGenerationService struct {
  db  *gorm.DB
  log *logger.Logger

  sseHub *sse.SSEHub

  courseRepo       repos.CourseRepo
  materialSetRepo  repos.MaterialSetRepo
  materialFileRepo repos.MaterialFileRepo
  moduleRepo       repos.CourseModuleRepo
  lessonRepo       repos.LessonRepo
  quizRepo         repos.QuizQuestionRepo
  blueprintRepo    repos.CourseBlueprintRepo
  chunkRepo        repos.MaterialChunkRepo
  runRepo          repos.CourseGenerationRunRepo

  bucket BucketService
  ai     OpenAIClient
}

func NewCourseGenerationService(
  db *gorm.DB,
  baseLog *logger.Logger,
  sseHub *sse.SSEHub,
  courseRepo repos.CourseRepo,
  materialSetRepo repos.MaterialSetRepo,
  materialFileRepo repos.MaterialFileRepo,
  moduleRepo repos.CourseModuleRepo,
  lessonRepo repos.LessonRepo,
  quizRepo repos.QuizQuestionRepo,
  blueprintRepo repos.CourseBlueprintRepo,
  chunkRepo repos.MaterialChunkRepo,
  runRepo repos.CourseGenerationRunRepo,
  bucket BucketService,
  ai OpenAIClient,
) CourseGenerationService {
  return &courseGenerationService{
    db:               db,
    log:              baseLog.With("service", "CourseGenerationService"),
    sseHub:           sseHub,
    courseRepo:       courseRepo,
    materialSetRepo:  materialSetRepo,
    materialFileRepo: materialFileRepo,
    moduleRepo:       moduleRepo,
    lessonRepo:       lessonRepo,
    quizRepo:         quizRepo,
    blueprintRepo:    blueprintRepo,
    chunkRepo:        chunkRepo,
    runRepo:          runRepo,
    bucket:           bucket,
    ai:               ai,
  }
}

func (cgs *courseGenerationService) EnqueueFromMaterialSet(ctx context.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.Course, *types.CourseGenerationRun, error) {
  var course *types.Course
  var run *types.CourseGenerationRun

  err := cgs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    sets, err := cgs.materialSetRepo.GetByIDs(ctx, tx, []uuid.UUID{materialSetID})
    if err != nil {
      return fmt.Errorf("load material set: %w", err)
    }
    if len(sets) == 0 || sets[0] == nil || sets[0].UserID != userID {
      return fmt.Errorf("material set not found or not owned by user")
    }

    now := time.Now()
    course = &types.Course{
      ID:            uuid.New(),
      UserID:        userID,
      MaterialSetID: &materialSetID,
      Title:         "Generating course…",
      Description:   "We’re analyzing your files and building your course.",
      Metadata:      datatypes.JSON(mustJSON(map[string]any{"status": "generating"})),
      CreatedAt:     now,
      UpdatedAt:     now,
    }
    if _, err := cgs.courseRepo.Create(ctx, tx, []*types.Course{course}); err != nil {
      return fmt.Errorf("create course: %w", err)
    }

    run = &types.CourseGenerationRun{
      ID:            uuid.New(),
      UserID:        userID,
      MaterialSetID: materialSetID,
      CourseID:      course.ID,
      Status:        "queued",
      Stage:         "ingest",
      Progress:      0,
      Attempts:      0,
      Metadata:      datatypes.JSON([]byte(`{}`)),
      CreatedAt:     now,
      UpdatedAt:     now,
    }
    if _, err := cgs.runRepo.Create(ctx, tx, []*types.CourseGenerationRun{run}); err != nil {
      return fmt.Errorf("create generation run: %w", err)
    }

    if ssd := ssedata.GetSSEData(ctx); ssd != nil {
      ssd.AppendMessage(sse.SSEMessage{
        Channel: userID.String(),
        Event:   sse.SSEEventUserCourseCreated,
        Data:    map[string]any{"course": course, "run": run},
      })
    }

    return nil
  })
  if err != nil {
    return nil, nil, err
  }
  return course, run, nil
}

func (cgs *courseGenerationService) StartWorker(ctx context.Context) {
  go func() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    // Worker policy
    const maxAttempts = 5
    retryDelay := 30 * time.Second
    staleRunning := 2 * time.Minute

    for {
      select {
      case <-ctx.Done():
        return
      case <-ticker.C:
        run, err := cgs.runRepo.ClaimNextRunnable(ctx, cgs.db, maxAttempts, retryDelay, staleRunning)
        if err != nil {
          cgs.log.Warn("ClaimNextRunnable failed", "error", err)
          continue
        }
        if run == nil {
          continue
        }
        cgs.processRun(ctx, run)
      }
    }
  }()
}

func (cgs *courseGenerationService) processRun(ctx context.Context, run *types.CourseGenerationRun) {
  userID := run.UserID
  runID := run.ID

  fail := func(stage string, err error) {
    now := time.Now()
    _ = cgs.runRepo.UpdateFields(ctx, nil, runID, map[string]any{
      "status":        "failed",
      "stage":         stage,
      "error":         err.Error(),
      "last_error_at": now,
      "locked_at":     nil,
      "updated_at":    now,
    })
    cgs.broadcast(userID, "CourseGenerationFailed", map[string]any{
      "run_id": runID,
      "stage":  stage,
      "error":  err.Error(),
    })
  }

  progress := func(stage string, pct int, msg string) {
    now := time.Now()
    _ = cgs.runRepo.UpdateFields(ctx, nil, runID, map[string]any{
      "stage":        stage,
      "progress":     pct,
      "heartbeat_at": now,
      "updated_at":   now,
    })
    cgs.broadcast(userID, "CourseGenerationProgress", map[string]any{
      "run_id":   runID,
      "stage":    stage,
      "progress": pct,
      "message":  msg,
    })
  }

  // Load files for this material set
  files, err := cgs.materialFileRepo.GetByMaterialSetID(ctx, nil, run.MaterialSetID)
  if err != nil {
    fail("ingest", fmt.Errorf("load files: %w", err))
    return
  }
  if len(files) == 0 {
    fail("ingest", fmt.Errorf("no material files found"))
    return
  }

  fileIDs := make([]uuid.UUID, 0, len(files))
  for _, mf := range files {
    if mf != nil {
      fileIDs = append(fileIDs, mf.ID)
    }
  }

  // 1) INGEST (idempotent): if chunks already exist for a file, do not re-download.
  progress("ingest", 5, "Ensuring extracted chunks exist")
  existingChunks, err := cgs.chunkRepo.GetByMaterialFileIDs(ctx, nil, fileIDs)
  if err != nil {
    fail("ingest", fmt.Errorf("load existing chunks: %w", err))
    return
  }
  hasChunks := map[uuid.UUID]bool{}
  for _, ch := range existingChunks {
    if ch != nil && ch.MaterialFileID != uuid.Nil {
      hasChunks[ch.MaterialFileID] = true
    }
  }

  // Only download/extract for files without chunks.
  for i, mf := range files {
    if mf == nil {
      continue
    }
    if hasChunks[mf.ID] {
      continue
    }

    rc, err := cgs.bucket.DownloadFile(ctx, BucketCategoryMaterial, mf.StorageKey)
    if err != nil {
      fail("ingest", fmt.Errorf("download file %s: %w", mf.ID, err))
      return
    }

    data, readErr := io.ReadAll(rc)
    _ = rc.Close()
    if readErr != nil {
      fail("ingest", fmt.Errorf("read file %s: %w", mf.OriginalName, readErr))
      return
    }

    text, err := ExtractText(mf.OriginalName, mf.MimeType, data)
    if err != nil {
      fail("ingest", fmt.Errorf("extract text %s: %w", mf.OriginalName, err))
      return
    }

    chunks := chunkText(mf.ID, text, 1200, 200)
    if _, err := cgs.chunkRepo.Create(ctx, nil, chunks); err != nil {
      fail("ingest", fmt.Errorf("create chunks: %w", err))
      return
    }

    progress("ingest", 5+int(float64(i+1)/float64(len(files))*20.0), fmt.Sprintf("Extracted %s", mf.OriginalName))
  }

  // Reload chunks now that we ensured they exist
  chunks, err := cgs.chunkRepo.GetByMaterialFileIDs(ctx, nil, fileIDs)
  if err != nil {
    fail("ingest", fmt.Errorf("load chunks after ingest: %w", err))
    return
  }
  if len(chunks) == 0 {
    fail("ingest", fmt.Errorf("no chunks available after ingest"))
    return
  }

  // 2) EMBED (idempotent): embed only chunks missing embeddings
  progress("embed", 30, "Embedding missing chunks")
  missing := make([]*types.MaterialChunk, 0)
  for _, ch := range chunks {
    if ch == nil {
      continue
    }
    if len(ch.Embedding) == 0 {
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

    vecs, err := cgs.ai.Embed(ctx, inputs)
    if err != nil {
      fail("embed", fmt.Errorf("embed: %w", err))
      return
    }

    for i, ch := range batch {
      b, _ := json.Marshal(vecs[i])
      if err := cgs.db.WithContext(ctx).Model(&types.MaterialChunk{}).
        Where("id = ?", ch.ID).
        Updates(map[string]any{
          "embedding":  datatypes.JSON(b),
          "updated_at": time.Now(),
        }).Error; err != nil {
        fail("embed", fmt.Errorf("update chunk embedding: %w", err))
        return
      }
      ch.Embedding = datatypes.JSON(b)
    }

    progress("embed", 30+int(float64(end)/float64(max(1, len(missing)))*15.0), "Embedded chunk batch")
  }

  // Materials context should come from chunks (canonical signals), not raw downloads.
  combined := buildCombinedFromChunks(chunks, 20000)
  if combined == "" {
    fail("metadata", fmt.Errorf("no combined materials text available"))
    return
  }

  // 3) METADATA (idempotent): only if course is still placeholder
  progress("metadata", 50, "Ensuring course metadata exists")
  courseRows, err := cgs.courseRepo.GetByIDs(ctx, nil, []uuid.UUID{run.CourseID})
  if err != nil || len(courseRows) == 0 || courseRows[0] == nil {
    fail("metadata", fmt.Errorf("load course failed: %v", err))
    return
  }
  course := courseRows[0]
  isPlaceholder := strings.TrimSpace(course.Title) == "" || strings.Contains(course.Title, "Generating course")

  if isPlaceholder {
    metaSchema := map[string]any{
      "type": "object",
      "properties": map[string]any{
        "title":       map[string]any{"type": "string"},
        "description": map[string]any{"type": "string"},
        "subject":     map[string]any{"type": "string"},
        "level":       map[string]any{"type": "string"},
        "tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
      },
      "required":             []string{"title", "description", "subject", "level", "tags"},
      "additionalProperties": false,
    }

    metaObj, err := cgs.ai.GenerateJSON(ctx,
      "You generate concise, high-quality course metadata from user-provided learning materials.",
      fmt.Sprintf("Materials (truncated):\n%s\n\nReturn course metadata.", combined),
      "course_metadata",
      metaSchema,
    )
    if err != nil {
      fail("metadata", err)
      return
    }

    title := fmt.Sprint(metaObj["title"])
    desc := fmt.Sprint(metaObj["description"])
    subject := fmt.Sprint(metaObj["subject"])
    level := fmt.Sprint(metaObj["level"])
    tags := metaObj["tags"]

    meta := map[string]any{
      "status": "generating",
      "tags":   tags,
    }

    if err := cgs.db.WithContext(ctx).Model(&types.Course{}).
      Where("id = ?", run.CourseID).
      Updates(map[string]any{
        "title":       title,
        "description": desc,
        "subject":     subject,
        "level":       level,
        "metadata":    datatypes.JSON(mustJSON(meta)),
        "updated_at":  time.Now(),
      }).Error; err != nil {
      fail("metadata", fmt.Errorf("update course: %w", err))
      return
    }

    course.Title = title
    course.Description = desc
    course.Subject = subject
    course.Level = level
    course.Metadata = datatypes.JSON(mustJSON(meta))

    // NEW: push updated course to frontend immediately (title/subject/level change)
    cgs.broadcast(userID, sse.SSEEventUserCourseCreated, map[string]any{
      "course": course,
      "run":    run,
    })
  }

  // 4) BLUEPRINT (idempotent): skip if blueprint exists OR modules exist.
  progress("blueprint", 60, "Ensuring course blueprint exists")
  existingModules, _ := cgs.moduleRepo.GetByCourseIDs(ctx, nil, []uuid.UUID{run.CourseID})
  if len(existingModules) == 0 {
    // check blueprint table for this material set + user
    bps, _ := cgs.blueprintRepo.GetByMaterialSetIDs(ctx, nil, []uuid.UUID{run.MaterialSetID})
    hasBP := false
    for _, bp := range bps {
      if bp != nil && bp.UserID == userID {
        hasBP = true
        break
      }
    }

    if !hasBP {
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

      blueprintObj, err := cgs.ai.GenerateJSON(ctx,
        "You design structured, coherent course outlines from learning materials.",
        fmt.Sprintf(
          "Course title: %s\nSubject: %s\nLevel: %s\n\nMaterials (truncated):\n%s\n\nCreate a course blueprint with 3-6 modules and 2-6 lessons per module. Keep titles specific.",
          course.Title, course.Subject, course.Level, combined,
        ),
        "course_blueprint",
        blueprintSchema,
      )
      if err != nil {
        fail("blueprint", err)
        return
      }

      blueprintJSON, _ := json.Marshal(blueprintObj)
      cb := &types.CourseBlueprint{
        ID:            uuid.New(),
        MaterialSetID: run.MaterialSetID,
        UserID:        userID,
        BlueprintJSON: datatypes.JSON(blueprintJSON),
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
      }
      if _, err := cgs.blueprintRepo.Create(ctx, nil, []*types.CourseBlueprint{cb}); err != nil {
        fail("blueprint", fmt.Errorf("save blueprint: %w", err))
        return
      }

      modsAny, ok := blueprintObj["modules"].([]any)
      if !ok {
        fail("blueprint", fmt.Errorf("blueprint modules missing or wrong type"))
        return
      }

      now := time.Now()
      modules := make([]*types.CourseModule, 0, len(modsAny))
      for i, m := range modsAny {
        mm := m.(map[string]any)
        modules = append(modules, &types.CourseModule{
          ID:          uuid.New(),
          CourseID:    run.CourseID,
          Index:       i,
          Title:       fmt.Sprint(mm["title"]),
          Description: fmt.Sprint(mm["description"]),
          Metadata:    datatypes.JSON([]byte(`{}`)),
          CreatedAt:   now,
          UpdatedAt:   now,
        })
      }
      if _, err := cgs.moduleRepo.Create(ctx, nil, modules); err != nil {
        fail("blueprint", fmt.Errorf("create modules: %w", err))
        return
      }

      lessons := make([]*types.Lesson, 0)
      for mi, m := range modsAny {
        mm := m.(map[string]any)
        ls := mm["lessons"].([]any)
        for li, lraw := range ls {
          lm := lraw.(map[string]any)
          lid := uuid.New()
          topics := toStringSlice(lm["topics"])
          est := intFromAny(lm["estimated_minutes"], 10)

          lessons = append(lessons, &types.Lesson{
            ID:               lid,
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

      if _, err := cgs.lessonRepo.Create(ctx, nil, lessons); err != nil {
        fail("blueprint", fmt.Errorf("create lessons: %w", err))
        return
      }
    }
  }

  // 5) LESSONS + QUIZZES (idempotent): generate only missing lesson content / missing quizzes
  progress("lessons", 75, "Generating missing lessons/quizzes")

  modules, err := cgs.moduleRepo.GetByCourseIDs(ctx, nil, []uuid.UUID{run.CourseID})
  if err != nil || len(modules) == 0 {
    fail("lessons", fmt.Errorf("no modules found for course after blueprint"))
    return
  }
  moduleIDs := make([]uuid.UUID, 0, len(modules))
  for _, m := range modules {
    if m != nil {
      moduleIDs = append(moduleIDs, m.ID)
    }
  }

  lessons, err := cgs.lessonRepo.GetByModuleIDs(ctx, nil, moduleIDs)
  if err != nil {
    fail("lessons", fmt.Errorf("load lessons: %w", err))
    return
  }
  if len(lessons) == 0 {
    fail("lessons", fmt.Errorf("no lessons found"))
    return
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
    fail("lessons", fmt.Errorf("no embeddings available for retrieval"))
    return
  }

  lessonIDs := make([]uuid.UUID, 0, len(lessons))
  for _, l := range lessons {
    if l != nil {
      lessonIDs = append(lessonIDs, l.ID)
    }
  }
  existingQuestions, _ := cgs.quizRepo.GetByLessonIDs(ctx, nil, lessonIDs)
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

    q := l.Title + " " + strings.Join(topics, " ")
    qVecs, err := cgs.ai.Embed(ctx, []string{q})
    if err != nil {
      fail("lessons", fmt.Errorf("embed query: %w", err))
      return
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
        "concise_md":         map[string]any{"type": "string"},
        "step_by_step_md":    map[string]any{"type": "string"},
        "citations":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
        "estimated_minutes":  map[string]any{"type": "integer"},
      },
      "required":             []string{"concise_md", "step_by_step_md", "citations", "estimated_minutes"},
      "additionalProperties": false,
    }

    out, err := cgs.ai.GenerateJSON(ctx,
      "You write clear lessons and include citations as chunk_id strings from the provided excerpts. Never cite anything not in excerpts.",
      fmt.Sprintf(
        "Lesson title: %s\nTopics: %s\n\n%s\n\nWrite two versions: concise and step-by-step. Return citations as a list of chunk_id strings you actually used.",
        l.Title, strings.Join(topics, ", "), ctxBuilder.String(),
      ),
      "lesson_content",
      lessonSchema,
    )
    if err != nil {
      fail("lessons", err)
      return
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

    if err := cgs.db.WithContext(ctx).Model(&types.Lesson{}).
      Where("id = ?", l.ID).
      Updates(map[string]any{
        "content_md":        concise,
        "estimated_minutes": est,
        "metadata":          datatypes.JSON(mustJSON(meta)),
        "updated_at":        time.Now(),
      }).Error; err != nil {
      fail("lessons", fmt.Errorf("update lesson: %w", err))
      return
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
              "required":             []string{"type", "prompt_md", "options", "correct_index", "explanation_md"},
              "additionalProperties": false,
            },
          },
        },
        "required":             []string{"questions"},
        "additionalProperties": false,
      }

      quizOut, err := cgs.ai.GenerateJSON(ctx,
        "You generate fair quiz questions based strictly on the lesson. Use MCQ only.",
        fmt.Sprintf("Lesson:\n%s\n\nGenerate 5 multiple-choice questions with 4 options each.", concise),
        "lesson_quiz",
        quizSchema,
      )
      if err != nil {
        fail("quizzes", err)
        return
      }

      qsAny, ok := quizOut["questions"].([]any)
      if !ok {
        fail("quizzes", fmt.Errorf("quiz questions missing or wrong type"))
        return
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

      if _, err := cgs.quizRepo.Create(ctx, nil, qs); err != nil {
        fail("quizzes", fmt.Errorf("create quiz: %w", err))
        return
      }
    }

    done++
    progress("lessons", 75+int(float64(done)/float64(max(1, total))*20.0), fmt.Sprintf("Generated %d/%d lessons", done, total))
  }

  // finalize run
  now := time.Now()
  _ = cgs.runRepo.UpdateFields(ctx, nil, runID, map[string]any{
    "status":       "succeeded",
    "stage":        "done",
    "progress":     100,
    "error":        "",
    "locked_at":    nil,
    "heartbeat_at": now,
    "updated_at":   now,
  })

  // keep metadata object and set status ready (do not overwrite tags)
  var currentMeta map[string]any
  _ = json.Unmarshal(course.Metadata, &currentMeta)
  if currentMeta == nil {
    currentMeta = map[string]any{}
  }
  currentMeta["status"] = "ready"

  if err := cgs.db.WithContext(ctx).Model(&types.Course{}).
    Where("id = ?", run.CourseID).
    Updates(map[string]any{
      "metadata":   datatypes.JSON(mustJSON(currentMeta)),
      "updated_at": time.Now(),
    }).Error; err != nil {
    fail("done", fmt.Errorf("update course ready status: %w", err))
    return
  }

  // NEW: push final course update (status ready)
  course.Metadata = datatypes.JSON(mustJSON(currentMeta))
  cgs.broadcast(userID, sse.SSEEventUserCourseCreated, map[string]any{
    "course": course,
    "run":    run,
  })

  cgs.broadcast(userID, "CourseGenerationDone", map[string]any{
    "run_id":    runID,
    "course_id": run.CourseID,
  })
}

func (cgs *courseGenerationService) broadcast(userID uuid.UUID, event sse.SSEEvent, data any) {
  cgs.sseHub.Broadcast(sse.SSEMessage{
    Channel: userID.String(),
    Event:   event,
    Data:    data,
  })
}

// ---- helpers ----

func buildCombinedFromChunks(chunks []*types.MaterialChunk, maxLen int) string {
  var b strings.Builder
  for _, ch := range chunks {
    if ch == nil {
      continue
    }
    if strings.TrimSpace(ch.Text) == "" {
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

// --- your existing helpers below (unchanged) ---

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
    s := cosine(ch.Vec, q)
    arr = append(arr, scored{c: ch, s: s})
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











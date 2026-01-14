package learning_build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	orchestrator "github.com/yungbote/neurobridge-backend/internal/jobs/orchestrator"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

var stageOrder = []string{
	"web_resources_seed",
	"ingest_chunks",
	// Ask clarifying questions early; downstream graph/planning uses intake context.
	"path_intake",
	// For multi-goal uploads, optionally split into a program path + subpaths.
	"path_structure_dispatch",
	"embed_chunks",
	"material_set_summarize",
	"user_profile_refresh",
	"teaching_patterns_seed",
	"concept_graph_build",
	// Post-concept structure refinement across sibling subpaths (best-effort, non-destructive).
	"path_structure_refine",
	"material_kg_build",
	"concept_cluster_build",
	"chain_signature_build",
	"path_plan_build",
	"path_cover_render",
	"node_figures_plan_build",
	"node_figures_render",
	"node_videos_plan_build",
	"node_videos_render",
	"node_doc_build",
	"realize_activities",
	"coverage_coherence_audit",
	"progression_compact",
	"variant_stats_refresh",
	"priors_refresh",
	"completed_unit_refresh",
}

var programStageOrder = []string{
	"web_resources_seed",
	"ingest_chunks",
	"path_intake",
	"path_structure_dispatch",
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.jobs == nil || p.path == nil || p.saga == nil || p.bootstrap == nil {
		jc.Fail("validate", fmt.Errorf("learning_build: pipeline not configured"))
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}

	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		id, err := p.saga.CreateOrGetSaga(jc.Ctx, jc.Job.OwnerUserID, jc.Job.ID)
		if err != nil {
			jc.Fail("saga", err)
			return nil
		}
		sagaID = id
	}

	// Prefer an explicit path_id when provided (enables subpath builds).
	pathID, _ := jc.PayloadUUID("path_id")
	if pathID != uuid.Nil {
		if p.path != nil {
			row, err := p.path.GetByID(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, pathID)
			if err != nil {
				jc.Fail("bootstrap", err)
				return nil
			}
			if row == nil || row.ID == uuid.Nil || row.UserID == nil || *row.UserID != jc.Job.OwnerUserID {
				jc.Fail("bootstrap", fmt.Errorf("path not found"))
				return nil
			}
		}
	} else {
		id, err := p.bootstrap.EnsurePath(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, setID)
		if err != nil {
			jc.Fail("bootstrap", err)
			return nil
		}
		pathID = id
	}

	threadID, _ := jc.PayloadUUID("thread_id")

	st := loadState(jc.Job.Result)
	st.MaterialSetID = setID.String()
	st.SagaID = sagaID.String()
	st.PathID = pathID.String()

	// Determine orchestration mode (persist in state for resumability).
	if strings.TrimSpace(st.Mode) == "" {
		st.Mode = resolveMode(jc)
	}

	var runErr error
	switch strings.ToLower(strings.TrimSpace(st.Mode)) {
	case "inline":
		runErr = p.runInline(jc, st, setID, sagaID, pathID)
	default:
		runErr = p.runChild(jc, st, setID, sagaID, pathID, threadID)
	}

	if strings.EqualFold(strings.TrimSpace(jc.Job.Status), "failed") {
		p.maybeAppendPathBuildFailedMessage(jc, pathID)
	}

	return runErr
}

func resolveMode(jc *jobrt.Context) string {
	if jc == nil {
		return "child"
	}
	// Payload override (useful for dev).
	if v, ok := jc.Payload()["mode"]; ok {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		if s == "inline" {
			return "inline"
		}
	}
	// Env override.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("LEARNING_BUILD_MODE")), "inline") {
		return "inline"
	}
	return "child"
}

func (p *Pipeline) runChild(jc *jobrt.Context, st *state, setID, sagaID, pathID, threadID uuid.UUID) error {
	if p.isCanceled(jc) {
		return nil
	}

	mode := "child"
	if st != nil && strings.TrimSpace(st.Mode) != "" {
		mode = strings.ToLower(strings.TrimSpace(st.Mode))
	}

	engine := orchestrator.NewDAGEngine(p.jobs)
	engine.MinPollInterval = p.minPoll
	engine.MaxPollInterval = p.maxPoll
	engine.ChildMaxWait = p.childMaxWait
	engine.ChildStaleRunning = p.childStaleRunning
	engine.ResultEncoder = orchestrator.EncodeFlatState
	engine.IsCanceled = func(ctx *jobrt.Context) bool {
		return p.isCanceled(ctx)
	}
	engine.OnFail = func(ctx *jobrt.Context, st *orchestrator.OrchestratorState, stage string, jobStage string, err error) {
		if ctx == nil || p == nil || p.saga == nil || sagaID == uuid.Nil {
			return
		}
		if strings.TrimSpace(jobStage) == "finalize" {
			return
		}
		_ = p.saga.MarkSagaStatus(ctx.Ctx, sagaID, services.SagaStatusFailed)
		_ = p.saga.Compensate(ctx.Ctx, sagaID)
	}
	engine.OnSuccess = func(ctx *jobrt.Context, st *orchestrator.OrchestratorState) error {
		if ctx == nil || p == nil || p.db == nil || p.path == nil || pathID == uuid.Nil {
			return nil
		}
		p.maybeGeneratePathCover(ctx, pathID)
		readyAt := time.Now().UTC()
		if err := p.db.WithContext(ctx.Ctx).Transaction(func(tx *gorm.DB) error {
			return p.path.UpdateFields(dbctx.Context{Ctx: ctx.Ctx, Tx: tx}, pathID, map[string]interface{}{
				"status":   "ready",
				"job_id":   nil,
				"ready_at": readyAt,
			})
		}); err != nil {
			return err
		}
		if p.saga != nil && sagaID != uuid.Nil {
			_ = p.saga.MarkSagaStatus(ctx.Ctx, sagaID, services.SagaStatusSucceeded)
		}
		p.enqueueChatPathIndex(ctx, pathID)
		p.enqueueNodeAvatarRender(ctx, setID, pathID)
		p.enqueueLibraryTaxonomyRoute(ctx, pathID)
		p.maybeAppendPathBuildReadyMessage(ctx, setID, pathID)
		return nil
	}

	init := func(st *orchestrator.OrchestratorState) {
		if st == nil {
			return
		}
		if st.Meta == nil {
			st.Meta = map[string]any{}
		}
		st.Meta["material_set_id"] = setID.String()
		st.Meta["saga_id"] = sagaID.String()
		st.Meta["path_id"] = pathID.String()
		if threadID != uuid.Nil {
			st.Meta["thread_id"] = threadID.String()
		}
		st.Meta["mode"] = mode
	}

	finalResult := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
		"mode":            mode,
	}
	if threadID != uuid.Nil {
		finalResult["thread_id"] = threadID.String()
	}
	order := stageOrder
	if p.shouldStopAfterDispatch(jc.Ctx, pathID) {
		order = programStageOrder
	}
	stages := buildChildStagesForNames(order, setID, sagaID, pathID, threadID)
	return engine.Run(jc, stages, finalResult, init)
}

func (p *Pipeline) runInline(jc *jobrt.Context, st *state, setID, sagaID, pathID uuid.UUID) error {
	if p.inline == nil {
		jc.Fail("validate", fmt.Errorf("inline mode requested but inline deps are not configured"))
		return nil
	}

	uc := learningmod.New(learningmod.UsecasesDeps{
		DB:      p.db,
		Log:     p.log,
		Extract: p.inline.Extract,

		AI:    p.inline.AI,
		Vec:   p.inline.Vec,
		Graph: p.inline.Graph,

		Bucket: p.inline.Bucket,
		Avatar: p.inline.Avatar,

		Files:     p.inline.Files,
		Chunks:    p.inline.Chunks,
		Summaries: p.inline.Summaries,

		Path:               p.inline.Path,
		PathNodes:          p.inline.PathNodes,
		PathNodeActivities: p.inline.PathNodeActivities,

		Concepts: p.inline.Concepts,
		Evidence: p.inline.Evidence,
		Edges:    p.inline.Edges,

		Clusters: p.inline.Clusters,
		Members:  p.inline.Members,

		ChainSignatures: p.inline.ChainSignatures,

		StylePrefs:  p.inline.StylePrefs,
		ProgEvents:  p.inline.UserProgressionEvents,
		Prefs:       p.inline.UserPrefs,
		UserProfile: p.inline.UserProfile,

		TeachingPatterns: p.inline.TeachingPatterns,

		NodeDocs: p.inline.NodeDocs,
		Figures:  p.inline.NodeFigures,
		Videos:   p.inline.NodeVideos,
		GenRuns:  p.inline.DocGenRuns,

		Assets: p.inline.Assets,

		Activities:        p.inline.Activities,
		Variants:          p.inline.Variants,
		ActivityConcepts:  p.inline.ActivityConcepts,
		ActivityCitations: p.inline.ActivityCitations,

		UserEvents:       p.inline.UserEvents,
		UserEventCursors: p.inline.UserEventCursors,
		VariantStats:     p.inline.VariantStats,

		ChainPriors:  p.inline.ChainPriors,
		CohortPriors: p.inline.CohortPriors,

		CompletedUnits: p.inline.CompletedUnits,
		ConceptState:   p.inline.ConceptState,

		Threads:  p.threads,
		Messages: p.messages,
		Notify:   p.chatNotif,

		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	})

	total := len(stageOrder)
	for i, stageName := range stageOrder {
		if p.isCanceled(jc) {
			return nil
		}

		ss := st.ensureStage(stageName)
		if ss.Status == stageStatusSucceeded {
			continue
		}

		progress := st.setProgress(progressForStage(i, total))
		jc.Progress(stageName, progress, "Running "+stageName+" inline")

		var stageErr error
		switch stageName {
		case "web_resources_seed":
			_, stageErr = uc.WebResourcesSeed(jc.Ctx, learningmod.WebResourcesSeedInput{
				OwnerUserID:   jc.Job.OwnerUserID,
				MaterialSetID: setID,
				SagaID:        sagaID,
				PathID:        pathID,
				Prompt: func() string {
					if v, ok := jc.Payload()["prompt"]; ok && v != nil {
						return fmt.Sprint(v)
					}
					return ""
				}(),
			})
		case "ingest_chunks":
			_, stageErr = uc.IngestChunks(jc.Ctx, learningmod.IngestChunksInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "embed_chunks":
			_, stageErr = uc.EmbedChunks(jc.Ctx, learningmod.EmbedChunksInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "material_set_summarize":
			_, stageErr = uc.MaterialSetSummarize(jc.Ctx, learningmod.MaterialSetSummarizeInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "concept_graph_build":
			_, stageErr = uc.ConceptGraphBuild(jc.Ctx, learningmod.ConceptGraphBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "material_kg_build":
			_, stageErr = uc.MaterialKGBuild(jc.Ctx, learningmod.MaterialKGBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "concept_cluster_build":
			_, stageErr = uc.ConceptClusterBuild(jc.Ctx, learningmod.ConceptClusterBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "chain_signature_build":
			_, stageErr = uc.ChainSignatureBuild(jc.Ctx, learningmod.ChainSignatureBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "user_profile_refresh":
			_, stageErr = uc.UserProfileRefresh(jc.Ctx, learningmod.UserProfileRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "teaching_patterns_seed":
			_, stageErr = uc.TeachingPatternsSeed(jc.Ctx, learningmod.TeachingPatternsSeedInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "path_intake":
			_, stageErr = uc.PathIntake(jc.Ctx, learningmod.PathIntakeInput{
				OwnerUserID:   jc.Job.OwnerUserID,
				MaterialSetID: setID,
				SagaID:        sagaID,
				PathID:        pathID,
				ThreadID: func() uuid.UUID {
					tid, _ := jc.PayloadUUID("thread_id")
					return tid
				}(),
				JobID:       jc.Job.ID,
				WaitForUser: false,
			})
		case "path_structure_dispatch":
			// Inline mode is a dev-only execution path. The production dispatch logic runs in child mode.
			// Treat as a no-op so inline builds remain functional.
			stageErr = nil
		case "path_plan_build":
			_, stageErr = uc.PathPlanBuild(jc.Ctx, learningmod.PathPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "path_cover_render":
			_, err := uc.PathCoverRender(jc.Ctx, learningmod.PathCoverRenderInput{PathID: pathID})
			if err != nil && p.log != nil {
				p.log.Warn("path_cover_render failed", "error", err, "path_id", pathID.String())
			}
			stageErr = nil
		case "node_avatar_render":
			_, err := uc.NodeAvatarRender(jc.Ctx, learningmod.NodeAvatarRenderInput{PathID: pathID})
			if err != nil && p.log != nil {
				p.log.Warn("node_avatar_render failed", "error", err, "path_id", pathID.String())
			}
			stageErr = nil
		case "node_figures_plan_build":
			_, stageErr = uc.NodeFiguresPlanBuild(jc.Ctx, learningmod.NodeFiguresPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "node_figures_render":
			_, stageErr = uc.NodeFiguresRender(jc.Ctx, learningmod.NodeFiguresRenderInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "node_videos_plan_build":
			_, stageErr = uc.NodeVideosPlanBuild(jc.Ctx, learningmod.NodeVideosPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "node_videos_render":
			_, stageErr = uc.NodeVideosRender(jc.Ctx, learningmod.NodeVideosRenderInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "node_doc_build":
			_, stageErr = uc.NodeDocBuild(jc.Ctx, learningmod.NodeDocBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "realize_activities":
			_, stageErr = uc.NodeContentBuild(jc.Ctx, learningmod.NodeContentBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, PathID: pathID})
			if stageErr == nil {
				_, stageErr = uc.RealizeActivities(jc.Ctx, learningmod.RealizeActivitiesInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
			}
		case "coverage_coherence_audit":
			_, stageErr = uc.CoverageCoherenceAudit(jc.Ctx, learningmod.CoverageCoherenceAuditInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "progression_compact":
			_, stageErr = uc.ProgressionCompact(jc.Ctx, learningmod.ProgressionCompactInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "variant_stats_refresh":
			_, stageErr = uc.VariantStatsRefresh(jc.Ctx, learningmod.VariantStatsRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "priors_refresh":
			_, stageErr = uc.PriorsRefresh(jc.Ctx, learningmod.PriorsRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		case "completed_unit_refresh":
			_, stageErr = uc.CompletedUnitRefresh(jc.Ctx, learningmod.CompletedUnitRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID, PathID: pathID})
		default:
			stageErr = fmt.Errorf("unknown stage %q", stageName)
		}

		if stageErr != nil {
			ss.Status = stageStatusFailed
			ss.LastError = stageErr.Error()
			now := time.Now().UTC()
			ss.FinishedAt = &now
			_ = p.saveState(jc, nil, st)
			return p.failAndCompensate(jc, st, sagaID, stageName, stageErr)
		}

		ss.Status = stageStatusSucceeded
		now := time.Now().UTC()
		ss.FinishedAt = &now
		_ = p.saveState(jc, nil, st)
	}

	// All stages succeeded.
	p.maybeGeneratePathCover(jc, pathID)
	readyAt := time.Now().UTC()
	if err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		return p.path.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: tx}, pathID, map[string]interface{}{
			"status":   "ready",
			"job_id":   nil,
			"ready_at": readyAt,
		})
	}); err != nil {
		jc.Fail("finalize", err)
		return nil
	}

	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusSucceeded)

	// Best-effort: project canonical path artifacts into chat_doc (ScopePath) for retrieval.
	p.enqueueChatPathIndex(jc, pathID)
	p.enqueueNodeAvatarRender(jc, setID, pathID)
	p.enqueueLibraryTaxonomyRoute(jc, pathID)
	p.maybeAppendPathBuildReadyMessage(jc, setID, pathID)

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
		"mode":            st.Mode,
		"stages":          st.Stages,
	})
	return nil
}

func (p *Pipeline) failAndCompensate(jc *jobrt.Context, st *state, sagaID uuid.UUID, stage string, err error) error {
	if st != nil {
		ss := st.ensureStage(stage)
		ss.Status = stageStatusFailed
		if err != nil {
			ss.LastError = err.Error()
		}
		now := time.Now().UTC()
		ss.FinishedAt = &now
	}
	_ = p.saveState(jc, nil, st)
	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusFailed)
	_ = p.saga.Compensate(jc.Ctx, sagaID)
	jc.Fail(stage, err)
	return nil
}

func (p *Pipeline) failAndCompensateWithStage(jc *jobrt.Context, st *state, sagaID uuid.UUID, stateStage string, jobStage string, err error) error {
	if st != nil {
		ss := st.ensureStage(stateStage)
		ss.Status = stageStatusFailed
		if err != nil {
			ss.LastError = err.Error()
		}
		now := time.Now().UTC()
		ss.FinishedAt = &now
	}
	_ = p.saveState(jc, nil, st)
	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusFailed)
	_ = p.saga.Compensate(jc.Ctx, sagaID)
	jc.Fail(jobStage, err)
	return nil
}

func (p *Pipeline) maybeGeneratePathCover(jc *jobrt.Context, pathID uuid.UUID) {
	if p == nil || jc == nil || pathID == uuid.Nil || p.inline == nil {
		return
	}
	if p.inline.Path == nil || p.inline.PathNodes == nil || p.inline.Avatar == nil {
		return
	}
	_, err := learningmod.New(learningmod.UsecasesDeps{
		Log:       p.log,
		Path:      p.inline.Path,
		PathNodes: p.inline.PathNodes,
		Avatar:    p.inline.Avatar,
	}).PathCoverRender(jc.Ctx, learningmod.PathCoverRenderInput{PathID: pathID})
	if err != nil && p.log != nil {
		p.log.Warn("path_cover_render failed", "error", err, "path_id", pathID.String())
	}
}

func (p *Pipeline) enqueueChatPathIndex(jc *jobrt.Context, pathID uuid.UUID) {
	if p == nil || p.jobs == nil || jc == nil || jc.Job == nil || pathID == uuid.Nil {
		return
	}
	payload := map[string]any{"path_id": pathID.String()}
	entityID := pathID
	if _, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, "chat_path_index", "path", &entityID, payload); err != nil {
		p.log.Warn("Failed to enqueue chat_path_index", "error", err, "path_id", pathID.String())
	}
}

func (p *Pipeline) enqueueLibraryTaxonomyRoute(jc *jobrt.Context, pathID uuid.UUID) {
	if p == nil || p.jobs == nil || jc == nil || jc.Job == nil || pathID == uuid.Nil {
		return
	}
	payload := map[string]any{"path_id": pathID.String()}
	entityID := pathID
	if _, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, "library_taxonomy_route", "path", &entityID, payload); err != nil {
		p.log.Warn("Failed to enqueue library_taxonomy_route", "error", err, "path_id", pathID.String())
	}
}

func (p *Pipeline) enqueueNodeAvatarRender(jc *jobrt.Context, setID uuid.UUID, pathID uuid.UUID) {
	if p == nil || p.jobs == nil || jc == nil || jc.Job == nil || setID == uuid.Nil {
		return
	}
	payload := map[string]any{
		"material_set_id": setID.String(),
	}
	if pathID != uuid.Nil {
		payload["path_id"] = pathID.String()
	}
	entityID := setID
	if _, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, "node_avatar_render", "material_set", &entityID, payload); err != nil {
		p.log.Warn("Failed to enqueue node_avatar_render", "error", err, "material_set_id", setID.String())
	}
}

func (p *Pipeline) saveState(jc *jobrt.Context, tx *gorm.DB, st *state) error {
	if jc == nil || jc.Job == nil || jc.Repo == nil || st == nil {
		return nil
	}
	b, _ := json.Marshal(st)
	if err := jc.Repo.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: tx}, jc.Job.ID, map[string]interface{}{"result": datatypes.JSON(b)}); err != nil {
		return err
	}
	jc.Job.Result = b
	return nil
}

func (p *Pipeline) yield(jc *jobrt.Context, st *state, stage string, progress int) error {
	if jc == nil || jc.Job == nil || jc.Repo == nil {
		return nil
	}
	now := time.Now()
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99
	}
	progress = st.setProgress(progress)
	_ = p.saveState(jc, nil, st)
	_, err := jc.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.ID, []string{"canceled"}, map[string]interface{}{
		"status":       "queued",
		"stage":        stage,
		"progress":     progress,
		"locked_at":    nil,
		"heartbeat_at": now,
	})
	return err
}

func progressForStage(i, total int) int {
	if total <= 0 {
		return 0
	}
	pct := int(float64(i) / float64(total) * 100)
	if pct < 0 {
		return 0
	}
	if pct > 99 {
		return 99
	}
	return pct
}

func ptrTime(t time.Time) *time.Time { return &t }

func clampDuration(d, min, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

func (p *Pipeline) isCanceled(jc *jobrt.Context) bool {
	if jc == nil || jc.Job == nil || jc.Repo == nil {
		return false
	}
	rows, err := jc.Repo.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{jc.Job.ID})
	if err != nil || len(rows) == 0 || rows[0] == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(rows[0].Status), "canceled")
}

func (p *Pipeline) shouldStopAfterDispatch(ctx context.Context, pathID uuid.UUID) bool {
	if p == nil || p.path == nil || p.db == nil || ctx == nil || pathID == uuid.Nil {
		return false
	}
	row, err := p.path.GetByID(dbctx.Context{Ctx: ctx, Tx: p.db}, pathID)
	if err != nil || row == nil || row.ID == uuid.Nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(row.Kind), "program") {
		return true
	}
	if len(row.Metadata) == 0 || strings.TrimSpace(string(row.Metadata)) == "" || strings.TrimSpace(string(row.Metadata)) == "null" {
		return false
	}
	meta := map[string]any{}
	if err := json.Unmarshal(row.Metadata, &meta); err != nil {
		return false
	}
	intake, _ := meta["intake"].(map[string]any)
	if intake == nil {
		return false
	}
	ps, _ := intake["path_structure"].(map[string]any)
	if ps == nil {
		return false
	}
	selected := strings.ToLower(strings.TrimSpace(fmt.Sprint(ps["selected_mode"])))
	recommended := strings.ToLower(strings.TrimSpace(fmt.Sprint(ps["recommended_mode"])))
	if selected == "" || selected == "unspecified" {
		selected = recommended
	}
	if selected != "program_with_subpaths" {
		return false
	}
	ma, _ := intake["material_alignment"].(map[string]any)
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(ma["mode"])))
	tracks, _ := intake["tracks"].([]any)
	multiGoal := mode == "multi_goal" || len(tracks) > 1
	return multiGoal
}

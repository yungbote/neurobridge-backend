package learning_build

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	orchestrator "github.com/yungbote/neurobridge-backend/internal/jobs/orchestrator"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

var stageOrder = []string{
	"ingest_chunks",
	"embed_chunks",
	"material_set_summarize",
	"concept_graph_build",
	"concept_cluster_build",
	"chain_signature_build",
	"user_profile_refresh",
	"teaching_patterns_seed",
	"path_plan_build",
	"path_cover_render",
	"node_avatar_render",
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

	pathID, err := p.bootstrap.EnsurePath(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, setID)
	if err != nil {
		jc.Fail("bootstrap", err)
		return nil
	}

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
		runErr = p.runChild(jc, st, setID, sagaID, pathID)
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

func (p *Pipeline) runChild(jc *jobrt.Context, st *state, setID, sagaID, pathID uuid.UUID) error {
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
				"status": "ready",
				"job_id": nil,
				"ready_at": readyAt,
			})
		}); err != nil {
			return err
		}
		if p.saga != nil && sagaID != uuid.Nil {
			_ = p.saga.MarkSagaStatus(ctx.Ctx, sagaID, services.SagaStatusSucceeded)
		}
		p.enqueueChatPathIndex(ctx, pathID)
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
		st.Meta["mode"] = mode
	}

	finalResult := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
		"mode":            mode,
	}
	stages := buildChildStages(setID, sagaID)
	return engine.Run(jc, stages, finalResult, init)
}

func (p *Pipeline) runInline(jc *jobrt.Context, st *state, setID, sagaID, pathID uuid.UUID) error {
	if p.inline == nil {
		jc.Fail("validate", fmt.Errorf("inline mode requested but inline deps are not configured"))
		return nil
	}

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
		case "ingest_chunks":
			_, stageErr = steps.IngestChunks(jc.Ctx, steps.IngestChunksDeps{
				DB:        p.db,
				Log:       p.log,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				Extract:   p.inline.Extract,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.IngestChunksInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "embed_chunks":
			_, stageErr = steps.EmbedChunks(jc.Ctx, steps.EmbedChunksDeps{
				DB:        p.db,
				Log:       p.log,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.EmbedChunksInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "material_set_summarize":
			_, stageErr = steps.MaterialSetSummarize(jc.Ctx, steps.MaterialSetSummarizeDeps{
				DB:        p.db,
				Log:       p.log,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				Summaries: p.inline.Summaries,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.MaterialSetSummarizeInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "concept_graph_build":
			_, stageErr = steps.ConceptGraphBuild(jc.Ctx, steps.ConceptGraphBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				Concepts:  p.inline.Concepts,
				Evidence:  p.inline.Evidence,
				Edges:     p.inline.Edges,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.ConceptGraphBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "concept_cluster_build":
			_, stageErr = steps.ConceptClusterBuild(jc.Ctx, steps.ConceptClusterBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Concepts:  p.inline.Concepts,
				Clusters:  p.inline.Clusters,
				Members:   p.inline.Members,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.ConceptClusterBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "chain_signature_build":
			_, stageErr = steps.ChainSignatureBuild(jc.Ctx, steps.ChainSignatureBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Concepts:  p.inline.Concepts,
				Clusters:  p.inline.Clusters,
				Members:   p.inline.Members,
				Edges:     p.inline.Edges,
				Chains:    p.inline.ChainSignatures,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Saga:      p.saga,
				Bootstrap: p.bootstrap,
			}, steps.ChainSignatureBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "user_profile_refresh":
			_, stageErr = steps.UserProfileRefresh(jc.Ctx, steps.UserProfileRefreshDeps{
				DB:          p.db,
				Log:         p.log,
				StylePrefs:  p.inline.StylePrefs,
				ProgEvents:  p.inline.UserProgressionEvents,
				UserProfile: p.inline.UserProfile,
				AI:          p.inline.AI,
				Vec:         p.inline.Vec,
				Saga:        p.saga,
				Bootstrap:   p.bootstrap,
			}, steps.UserProfileRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "teaching_patterns_seed":
			_, stageErr = steps.TeachingPatternsSeed(jc.Ctx, steps.TeachingPatternsSeedDeps{
				DB:          p.db,
				Log:         p.log,
				Patterns:    p.inline.TeachingPatterns,
				UserProfile: p.inline.UserProfile,
				AI:          p.inline.AI,
				Vec:         p.inline.Vec,
				Saga:        p.saga,
				Bootstrap:   p.bootstrap,
			}, steps.TeachingPatternsSeedInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "path_plan_build":
			_, stageErr = steps.PathPlanBuild(jc.Ctx, steps.PathPlanBuildDeps{
				DB:          p.db,
				Log:         p.log,
				Path:        p.inline.Path,
				PathNodes:   p.inline.PathNodes,
				Concepts:    p.inline.Concepts,
				Edges:       p.inline.Edges,
				Summaries:   p.inline.Summaries,
				UserProfile: p.inline.UserProfile,
				AI:          p.inline.AI,
				Bootstrap:   p.bootstrap,
			}, steps.PathPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "path_cover_render":
			_, err := steps.PathCoverRender(jc.Ctx, steps.PathCoverRenderDeps{
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Avatar:    p.inline.Avatar,
			}, steps.PathCoverRenderInput{PathID: pathID})
			if err != nil && p.log != nil {
				p.log.Warn("path_cover_render failed", "error", err, "path_id", pathID.String())
			}
			stageErr = nil
		case "node_avatar_render":
			_, err := steps.NodeAvatarRender(jc.Ctx, steps.NodeAvatarRenderDeps{
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Avatar:    p.inline.Avatar,
			}, steps.NodeAvatarRenderInput{PathID: pathID})
			if err != nil && p.log != nil {
				p.log.Warn("node_avatar_render failed", "error", err, "path_id", pathID.String())
			}
			stageErr = nil
		case "node_figures_plan_build":
			_, stageErr = steps.NodeFiguresPlanBuild(jc.Ctx, steps.NodeFiguresPlanBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Figures:   p.inline.NodeFigures,
				GenRuns:   p.inline.DocGenRuns,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Bootstrap: p.bootstrap,
			}, steps.NodeFiguresPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "node_figures_render":
			_, stageErr = steps.NodeFiguresRender(jc.Ctx, steps.NodeFiguresRenderDeps{
				DB:        p.db,
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Figures:   p.inline.NodeFigures,
				Assets:    p.inline.Assets,
				GenRuns:   p.inline.DocGenRuns,
				AI:        p.inline.AI,
				Bucket:    p.inline.Bucket,
				Bootstrap: p.bootstrap,
			}, steps.NodeFiguresRenderInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "node_videos_plan_build":
			_, stageErr = steps.NodeVideosPlanBuild(jc.Ctx, steps.NodeVideosPlanBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Videos:    p.inline.NodeVideos,
				GenRuns:   p.inline.DocGenRuns,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Bootstrap: p.bootstrap,
			}, steps.NodeVideosPlanBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "node_videos_render":
			_, stageErr = steps.NodeVideosRender(jc.Ctx, steps.NodeVideosRenderDeps{
				DB:        p.db,
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				Videos:    p.inline.NodeVideos,
				Assets:    p.inline.Assets,
				GenRuns:   p.inline.DocGenRuns,
				AI:        p.inline.AI,
				Bucket:    p.inline.Bucket,
				Bootstrap: p.bootstrap,
			}, steps.NodeVideosRenderInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "node_doc_build":
			_, stageErr = steps.NodeDocBuild(jc.Ctx, steps.NodeDocBuildDeps{
				DB:        p.db,
				Log:       p.log,
				Path:      p.inline.Path,
				PathNodes: p.inline.PathNodes,
				NodeDocs:  p.inline.NodeDocs,
				Figures:   p.inline.NodeFigures,
				Videos:    p.inline.NodeVideos,
				GenRuns:   p.inline.DocGenRuns,
				Files:     p.inline.Files,
				Chunks:    p.inline.Chunks,
				AI:        p.inline.AI,
				Vec:       p.inline.Vec,
				Bucket:    p.inline.Bucket,
				Bootstrap: p.bootstrap,
			}, steps.NodeDocBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "realize_activities":
			_, stageErr = steps.NodeContentBuild(jc.Ctx, steps.NodeContentBuildDeps{
				DB:          p.db,
				Log:         p.log,
				Path:        p.inline.Path,
				PathNodes:   p.inline.PathNodes,
				Files:       p.inline.Files,
				Chunks:      p.inline.Chunks,
				UserProfile: p.inline.UserProfile,
				AI:          p.inline.AI,
				Vec:         p.inline.Vec,
				Bucket:      p.inline.Bucket,
				Bootstrap:   p.bootstrap,
			}, steps.NodeContentBuildInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID})
		case "coverage_coherence_audit":
			_, stageErr = steps.CoverageCoherenceAudit(jc.Ctx, steps.CoverageCoherenceAuditDeps{
				DB:         p.db,
				Log:        p.log,
				Path:       p.inline.Path,
				PathNodes:  p.inline.PathNodes,
				Concepts:   p.inline.Concepts,
				Activities: p.inline.Activities,
				Variants:   p.inline.Variants,
				AI:         p.inline.AI,
				Bootstrap:  p.bootstrap,
			}, steps.CoverageCoherenceAuditInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "progression_compact":
			_, stageErr = steps.ProgressionCompact(jc.Ctx, steps.ProgressionCompactDeps{
				DB:        p.db,
				Log:       p.log,
				Events:    p.inline.UserEvents,
				Cursors:   p.inline.UserEventCursors,
				Progress:  p.inline.UserProgressionEvents,
				Bootstrap: p.bootstrap,
			}, steps.ProgressionCompactInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "variant_stats_refresh":
			_, stageErr = steps.VariantStatsRefresh(jc.Ctx, steps.VariantStatsRefreshDeps{
				DB:        p.db,
				Log:       p.log,
				Events:    p.inline.UserEvents,
				Cursors:   p.inline.UserEventCursors,
				Variants:  p.inline.Variants,
				Stats:     p.inline.VariantStats,
				Bootstrap: p.bootstrap,
			}, steps.VariantStatsRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "priors_refresh":
			_, stageErr = steps.PriorsRefresh(jc.Ctx, steps.PriorsRefreshDeps{
				DB:           p.db,
				Log:          p.log,
				Activities:   p.inline.Activities,
				Variants:     p.inline.Variants,
				VariantStats: p.inline.VariantStats,
				Chains:       p.inline.ChainSignatures,
				Concepts:     p.inline.Concepts,
				ActConcepts:  p.inline.ActivityConcepts,
				ChainPriors:  p.inline.ChainPriors,
				CohortPriors: p.inline.CohortPriors,
				Bootstrap:    p.bootstrap,
			}, steps.PriorsRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
		case "completed_unit_refresh":
			_, stageErr = steps.CompletedUnitRefresh(jc.Ctx, steps.CompletedUnitRefreshDeps{
				DB:        p.db,
				Log:       p.log,
				Completed: p.inline.CompletedUnits,
				Progress:  p.inline.UserProgressionEvents,
				Concepts:  p.inline.Concepts,
				Act:       p.inline.Activities,
				ActCon:    p.inline.ActivityConcepts,
				Chains:    p.inline.ChainSignatures,
				Mastery:   p.inline.ConceptState,
				Bootstrap: p.bootstrap,
			}, steps.CompletedUnitRefreshInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
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
			"status": "ready",
			"job_id": nil,
			"ready_at": readyAt,
		})
	}); err != nil {
		jc.Fail("finalize", err)
		return nil
	}

	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusSucceeded)

	// Best-effort: project canonical path artifacts into chat_doc (ScopePath) for retrieval.
	p.enqueueChatPathIndex(jc, pathID)
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
	_, err := steps.PathCoverRender(jc.Ctx, steps.PathCoverRenderDeps{
		Log:       p.log,
		Path:      p.inline.Path,
		PathNodes: p.inline.PathNodes,
		Avatar:    p.inline.Avatar,
	}, steps.PathCoverRenderInput{PathID: pathID})
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

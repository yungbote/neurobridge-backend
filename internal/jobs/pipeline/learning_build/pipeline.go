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
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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

	pathID, err := p.bootstrap.EnsurePath(jc.Ctx, nil, jc.Job.OwnerUserID, setID)
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

	switch strings.ToLower(strings.TrimSpace(st.Mode)) {
	case "inline":
		return p.runInline(jc, st, setID, sagaID, pathID)
	default:
		return p.runChild(jc, st, setID, sagaID, pathID)
	}
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
	// If we're in a scheduled wait, sleep a bit to reduce polling pressure.
	if st.WaitUntil != nil && time.Now().Before(*st.WaitUntil) {
		sleep := clampDuration(st.WaitUntil.Sub(time.Now()), p.minPoll, p.maxPoll)
		if sleep > 0 {
			time.Sleep(sleep)
		}
		st.WaitUntil = nil
	}

	total := len(stageOrder)
	for i, jobType := range stageOrder {
		ss := st.ensureStage(jobType)
		if ss.Status == stageStatusSucceeded {
			continue
		}

		// Enqueue the next stage if needed.
		if strings.TrimSpace(ss.ChildJobID) == "" {
			progress := st.setProgress(progressForStage(i, total))
			jc.Progress(jobType, progress, "Enqueuing "+jobType)

			payload := map[string]any{
				"material_set_id": setID.String(),
				"saga_id":         sagaID.String(),
			}
			entityID := setID
			now := time.Now().UTC()

			err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
				child, err := p.jobs.Enqueue(jc.Ctx, tx, jc.Job.OwnerUserID, jobType, "material_set", &entityID, payload)
				if err != nil {
					return err
				}
				ss.Status = stageStatusWaitingChild
				ss.ChildJobID = child.ID.String()
				ss.ChildJobStatus = child.Status
				if ss.StartedAt == nil {
					ss.StartedAt = &now
				}
				st.WaitUntil = ptrTime(now.Add(p.minPoll))

				// Persist state in the same transaction as child job creation to avoid duplicate enqueues.
				b, _ := json.Marshal(st)
				return jc.Repo.UpdateFields(jc.Ctx, tx, jc.Job.ID, map[string]interface{}{
					"result": datatypes.JSON(b),
				})
			})
			if err != nil {
				jc.Fail(jobType, err)
				return nil
			}

			// Update in-memory copy for subsequent logic this run.
			jc.Job.Result, _ = json.Marshal(st)
			return p.yield(jc, st, "waiting_child_"+jobType, progress)
		}

		// Poll child job.
		childID, err := uuid.Parse(ss.ChildJobID)
		if err != nil || childID == uuid.Nil {
			return p.failAndCompensate(jc, st, sagaID, jobType, fmt.Errorf("invalid child_job_id %q", ss.ChildJobID))
		}

		rows, err := jc.Repo.GetByIDs(jc.Ctx, nil, []uuid.UUID{childID})
		if err != nil || len(rows) == 0 || rows[0] == nil {
			return p.failAndCompensate(jc, st, sagaID, jobType, fmt.Errorf("load child job %s: %w", childID.String(), err))
		}
		child := rows[0]
		ss.ChildJobStatus = child.Status
		if ss.StartedAt == nil {
			t := child.CreatedAt.UTC()
			ss.StartedAt = &t
			_ = p.saveState(jc, nil, st)
		}

		// Hard stop: if a child stage takes too long, fail the root saga so we don't wait forever.
		if ss.StartedAt != nil && p.childMaxWait > 0 && time.Since(*ss.StartedAt) > p.childMaxWait {
			now := time.Now().UTC()
			_ = jc.Repo.UpdateFields(jc.Ctx, nil, childID, map[string]interface{}{
				"status":        "failed",
				"stage":         "timeout",
				"error":         fmt.Sprintf("timed out after %s waiting for parent stage %s", p.childMaxWait.String(), jobType),
				"last_error_at": now,
				"locked_at":     nil,
				"updated_at":    now,
			})
			return p.failAndCompensateWithStage(
				jc,
				st,
				sagaID,
				jobType,
				"timeout_"+jobType,
				fmt.Errorf("learning_build: child stage %s timed out after %s (child_job_id=%s)", jobType, p.childMaxWait.String(), childID.String()),
			)
		}

		// If the child is "running" but hasn't heartbeated recently, treat it as stuck.
		if p.childStaleRunning > 0 && strings.EqualFold(strings.TrimSpace(child.Status), "running") {
			stale := false
			if child.HeartbeatAt != nil && !child.HeartbeatAt.IsZero() {
				stale = time.Since(child.HeartbeatAt.UTC()) > p.childStaleRunning
			} else {
				stale = time.Since(child.CreatedAt.UTC()) > p.childStaleRunning
			}
			if stale {
				now := time.Now().UTC()
				_ = jc.Repo.UpdateFields(jc.Ctx, nil, childID, map[string]interface{}{
					"status":        "failed",
					"stage":         "stale_heartbeat",
					"error":         fmt.Sprintf("stale heartbeat (> %s) while running; treated as stuck by learning_build", p.childStaleRunning.String()),
					"last_error_at": now,
					"locked_at":     nil,
					"updated_at":    now,
				})
				return p.failAndCompensateWithStage(
					jc,
					st,
					sagaID,
					jobType,
					"stale_"+jobType,
					fmt.Errorf("learning_build: child stage %s has stale heartbeat (> %s) (child_job_id=%s)", jobType, p.childStaleRunning.String(), childID.String()),
				)
			}
		}

		switch child.Status {
		case "succeeded":
			now := time.Now().UTC()
			ss.Status = stageStatusSucceeded
			ss.FinishedAt = &now
			if len(child.Result) > 0 && string(child.Result) != "null" {
				var obj any
				_ = json.Unmarshal(child.Result, &obj)
				ss.ChildResult = obj
			}

			_ = p.saveState(jc, nil, st)
			continue

		case "failed":
			errMsg := strings.TrimSpace(child.Error)
			if errMsg == "" {
				errMsg = "child job failed"
			}
			return p.failAndCompensate(jc, st, sagaID, jobType, fmt.Errorf("%s: %s", jobType, errMsg))

		default:
			// Still running/queued; yield.
			progress := st.setProgress(progressForStage(i, total))
			st.WaitUntil = ptrTime(time.Now().Add(p.minPoll))
			_ = p.saveState(jc, nil, st)
			return p.yield(jc, st, "waiting_child_"+jobType, progress)
		}
	}

	// All stages succeeded.
	if err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		return p.path.UpdateFields(jc.Ctx, tx, pathID, map[string]interface{}{
			"status": "ready",
		})
	}); err != nil {
		jc.Fail("finalize", err)
		return nil
	}

	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusSucceeded)
	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
		"mode":            st.Mode,
		"stages":          st.Stages,
	})
	return nil
}

func (p *Pipeline) runInline(jc *jobrt.Context, st *state, setID, sagaID, pathID uuid.UUID) error {
	if p.inline == nil {
		jc.Fail("validate", fmt.Errorf("inline mode requested but inline deps are not configured"))
		return nil
	}

	total := len(stageOrder)
	for i, stageName := range stageOrder {
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
		case "realize_activities":
			_, stageErr = steps.RealizeActivities(jc.Ctx, steps.RealizeActivitiesDeps{
				DB:                 p.db,
				Log:                p.log,
				Path:               p.inline.Path,
				PathNodes:          p.inline.PathNodes,
				PathNodeActivities: p.inline.PathNodeActivities,
				Activities:         p.inline.Activities,
				Variants:           p.inline.Variants,
				ActivityConcepts:   p.inline.ActivityConcepts,
				ActivityCitations:  p.inline.ActivityCitations,
				Concepts:           p.inline.Concepts,
				Files:              p.inline.Files,
				Chunks:             p.inline.Chunks,
				UserProfile:        p.inline.UserProfile,
				AI:                 p.inline.AI,
				Vec:                p.inline.Vec,
				Saga:               p.saga,
				Bootstrap:          p.bootstrap,
			}, steps.RealizeActivitiesInput{OwnerUserID: jc.Job.OwnerUserID, MaterialSetID: setID, SagaID: sagaID})
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
	if err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		return p.path.UpdateFields(jc.Ctx, tx, pathID, map[string]interface{}{"status": "ready"})
	}); err != nil {
		jc.Fail("finalize", err)
		return nil
	}

	_ = p.saga.MarkSagaStatus(jc.Ctx, sagaID, services.SagaStatusSucceeded)
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

func (p *Pipeline) saveState(jc *jobrt.Context, tx *gorm.DB, st *state) error {
	if jc == nil || jc.Job == nil || jc.Repo == nil || st == nil {
		return nil
	}
	b, _ := json.Marshal(st)
	if err := jc.Repo.UpdateFields(jc.Ctx, tx, jc.Job.ID, map[string]interface{}{"result": datatypes.JSON(b)}); err != nil {
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
	return jc.Repo.UpdateFields(jc.Ctx, nil, jc.Job.ID, map[string]interface{}{
		"status":       "queued",
		"stage":        stage,
		"progress":     progress,
		"locked_at":    nil,
		"heartbeat_at": now,
	})
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

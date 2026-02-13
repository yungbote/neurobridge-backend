package material_signal_build

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing saga_id"))
		return nil
	}
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("material_signal", 2, "Building material signals")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:           p.db,
		Log:          p.log,
		Files:        p.files,
		FileSigs:     p.fileSigs,
		FileSections: p.fileSections,
		Chunks:       p.chunks,
		Concepts:     p.concepts,
		MaterialSets: p.materialSets,
		AI:           p.ai,
		Bootstrap:    p.bootstrap,
		Artifacts:    p.artifacts,
	}).MaterialSignalBuild(jc.Ctx, learningmod.MaterialSignalBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("material_signal", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":               jc.Job.ID.String(),
		"owner_user_id":            jc.Job.OwnerUserID.String(),
		"material_set_id":          setID.String(),
		"path_id":                  out.PathID.String(),
		"files_total":              out.FilesTotal,
		"intents_upserted":         out.IntentsUpserted,
		"chunk_signals_upserted":   out.ChunkSignalsUpserted,
		"set_coverage_upserted":    out.SetCoverageUpserted,
		"set_edges_upserted":       out.SetEdgesUpserted,
		"chunk_links_upserted":     out.ChunkLinksUpserted,
		"global_edges_upserted":    out.GlobalEdgesUpserted,
		"global_coverage_upserted": out.GlobalCoverageUpserted,
		"emergent_upserted":        out.EmergentUpserted,
		"skipped":                  out.Skipped,
		"cache_hit":                out.CacheHit,
	}
	inputs := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
	}
	chosen := map[string]any{
		"chunk_signals_upserted": out.ChunkSignalsUpserted,
		"set_edges_upserted":     out.SetEdgesUpserted,
	}
	userID := jc.Job.OwnerUserID
	if _, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		PathID:        &out.PathID,
		MaterialSetID: &setID,
		SagaID:        &sagaID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      true,
		RequireTrace:  true,
	}); traceErr != nil {
		jc.Fail("structural_trace", traceErr)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":          setID.String(),
		"saga_id":                  sagaID.String(),
		"path_id":                  out.PathID.String(),
		"files_total":              out.FilesTotal,
		"intents_upserted":         out.IntentsUpserted,
		"chunk_signals_upserted":   out.ChunkSignalsUpserted,
		"set_coverage_upserted":    out.SetCoverageUpserted,
		"set_edges_upserted":       out.SetEdgesUpserted,
		"chunk_links_upserted":     out.ChunkLinksUpserted,
		"global_edges_upserted":    out.GlobalEdgesUpserted,
		"global_coverage_upserted": out.GlobalCoverageUpserted,
		"emergent_upserted":        out.EmergentUpserted,
		"skipped":                  out.Skipped,
		"cache_hit":                out.CacheHit,
		"trace":                    out.Trace,
	})
	return nil
}

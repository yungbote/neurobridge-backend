package file_signature_build

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
	if p == nil || p.db == nil || p.log == nil || p.files == nil || p.fileSigs == nil || p.fileSections == nil || p.chunks == nil || p.ai == nil || p.saga == nil || p.bootstrap == nil {
		jc.Fail("validate", fmt.Errorf("file_signature_build: pipeline not configured"))
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		if p.saga == nil {
			jc.Fail("validate", fmt.Errorf("missing saga_id"))
			return nil
		}
		id, err := p.saga.CreateOrGetSaga(jc.Ctx, jc.Job.OwnerUserID, jc.Job.ID)
		if err != nil {
			jc.Fail("saga", err)
			return nil
		}
		sagaID = id
	}
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("build", 3, "Building file signatures")

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:           p.db,
		Log:          p.log,
		Files:        p.files,
		FileSigs:     p.fileSigs,
		FileSections: p.fileSections,
		Chunks:       p.chunks,
		AI:           p.ai,
		Vec:          p.vec,
		Saga:         p.saga,
		Bootstrap:    p.bootstrap,
		Artifacts:    p.artifacts,
	}).FileSignatureBuild(jc.Ctx, learningmod.FileSignatureBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("build", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":          jc.Job.ID.String(),
		"owner_user_id":       jc.Job.OwnerUserID.String(),
		"material_set_id":     setID.String(),
		"path_id":             out.PathID.String(),
		"files_total":         out.FilesTotal,
		"files_processed":     out.FilesProcessed,
		"signatures_upserted": out.SignaturesUpserted,
		"signatures_skipped":  out.SignaturesSkipped,
		"sections_upserted":   out.SectionsUpserted,
		"intents_upserted":    out.IntentsUpserted,
		"intents_skipped":     out.IntentsSkipped,
		"cache_hit":           out.CacheHit,
	}
	inputs := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
	}
	chosen := map[string]any{
		"signatures_upserted": out.SignaturesUpserted,
		"sections_upserted":   out.SectionsUpserted,
		"intents_upserted":    out.IntentsUpserted,
	}
	userID := jc.Job.OwnerUserID
	_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
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
	})
	if traceErr != nil {
		jc.Fail("structural_trace", traceErr)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":     setID.String(),
		"saga_id":             sagaID.String(),
		"path_id":             out.PathID.String(),
		"files_total":         out.FilesTotal,
		"files_processed":     out.FilesProcessed,
		"signatures_upserted": out.SignaturesUpserted,
		"signatures_skipped":  out.SignaturesSkipped,
		"sections_upserted":   out.SectionsUpserted,
		"intents_upserted":    out.IntentsUpserted,
		"intents_skipped":     out.IntentsSkipped,
		"cache_hit":           out.CacheHit,
	})
	return nil
}

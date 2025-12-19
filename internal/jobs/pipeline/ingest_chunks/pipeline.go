package ingest_chunks

import (
	"fmt"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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

	jc.Progress("ingest", 2, "Ensuring chunks exist")
	out, err := steps.IngestChunks(jc.Ctx, steps.IngestChunksDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		Extract:   p.extract,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}, steps.IngestChunksInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("ingest", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":       setID.String(),
		"saga_id":               sagaID.String(),
		"path_id":               out.PathID.String(),
		"files_total":           out.FilesTotal,
		"files_processed":       out.FilesProcessed,
		"files_already_chunked": out.FilesAlreadyChunked,
	})
	return nil
}

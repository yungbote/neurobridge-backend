package embed_chunks

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

	jc.Progress("embed", 2, "Embedding chunks")
	out, err := steps.EmbedChunks(jc.Ctx, steps.EmbedChunksDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		AI:        p.ai,
		Vec:       p.vec,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}, steps.EmbedChunksInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("embed", err)
		return nil
	}
	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"chunks_total":     out.ChunksTotal,
		"chunks_embedded":  out.ChunksEmbedded,
		"pinecone_upserts": out.PineconeUpserts,
		"pinecone_skipped": out.PineconeSkipped,
	})
	return nil
}

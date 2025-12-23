package chat_path_index

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/chat/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	pathID, ok := jc.PayloadUUID("path_id")
	if !ok || pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}

	jc.Progress("index", 10, "Indexing path docs for chat retrieval")
	out, err := steps.IndexPathDocsForChat(jc.Ctx, steps.PathIndexDeps{
		DB:         p.db,
		Log:        p.log,
		AI:         p.ai,
		Vec:        p.vec,
		Docs:       p.docs,
		Path:       p.path,
		PathNodes:  p.pathNodes,
		NodeActs:   p.nodeActs,
		Activities: p.activities,
		Concepts:   p.concepts,
	}, steps.PathIndexInput{
		UserID: jc.Job.OwnerUserID,
		PathID: pathID,
	})
	if err != nil {
		jc.Fail("index", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"path_id":         pathID.String(),
		"docs_upserted":   out.DocsUpserted,
		"vector_upserted": out.VectorUpserted,
	})
	return nil
}

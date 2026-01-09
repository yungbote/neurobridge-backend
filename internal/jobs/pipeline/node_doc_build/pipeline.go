package node_doc_build

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
	sagaID, _ := jc.PayloadUUID("saga_id")

	jc.Progress("docs", 2, "Writing unit docs")
	out, err := steps.NodeDocBuild(jc.Ctx, steps.NodeDocBuildDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		NodeDocs:  p.docs,
		Figures:   p.figures,
		Videos:    p.videos,
		GenRuns:   p.genRuns,
		Files:     p.files,
		Chunks:    p.chunks,
		UserProfile:      p.userProf,
		TeachingPatterns: p.patterns,
		AI:        p.ai,
		Vec:       p.vec,
		Bucket:    p.bucket,
		Bootstrap: p.bootstrap,
	}, steps.NodeDocBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("docs", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"docs_written":    out.DocsWritten,
		"docs_existing":   out.DocsExisting,
	})
	return nil
}

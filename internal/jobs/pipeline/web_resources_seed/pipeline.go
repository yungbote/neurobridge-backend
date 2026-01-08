package web_resources_seed

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

	prompt := ""
	if v, ok := jc.Payload()["prompt"]; ok && v != nil {
		prompt = fmt.Sprint(v)
	}

	jc.Progress("seed", 2, "Seeding learning materials")
	out, err := steps.WebResourcesSeed(jc.Ctx, steps.WebResourcesSeedDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Path:      p.path,
		Bucket:    p.bucket,
		AI:        p.ai,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}, steps.WebResourcesSeedInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		Prompt:        prompt,
	})
	if err != nil {
		jc.Fail("seed", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"saga_id":           sagaID.String(),
		"path_id":           out.PathID.String(),
		"skipped":           out.Skipped,
		"files_created":     out.FilesCreated,
		"resources_planned": out.ResourcesPlanned,
		"resources_fetched": out.ResourcesFetched,
	})
	return nil
}

package saga_cleanup

import (
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}

	jc.Progress("cleanup", 2, "Cleaning up old sagas")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:     p.db,
		Log:    p.log,
		Sagas:  p.sagas,
		Saga:   p.saga,
		Bucket: p.bucket,
	}).SagaCleanup(jc.Ctx, learningmod.SagaCleanupInput{
		OwnerUserID: jc.Job.OwnerUserID,
	})
	if err != nil {
		jc.Fail("cleanup", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"sagas_scanned":    out.SagasScanned,
		"prefixes_deleted": out.PrefixesDeleted,
	})
	return nil
}

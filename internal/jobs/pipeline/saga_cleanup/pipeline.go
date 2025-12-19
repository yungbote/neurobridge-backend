package saga_cleanup

import (
	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}

	jc.Progress("cleanup", 2, "Cleaning up old sagas")
	out, err := steps.SagaCleanup(jc.Ctx, steps.SagaCleanupDeps{
		DB:      p.db,
		Log:     p.log,
		Sagas:   p.sagas,
		SagaSvc: p.saga,
		Bucket:  p.bucket,
	}, steps.SagaCleanupInput{
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

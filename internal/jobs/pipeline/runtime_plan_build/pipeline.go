package runtime_plan_build

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

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
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")

	force := false
	if v, ok := jc.Payload()["force"]; ok && v != nil {
		switch t := v.(type) {
		case bool:
			force = t
		default:
			s := strings.TrimSpace(fmt.Sprint(v))
			if strings.EqualFold(s, "true") || s == "1" || strings.EqualFold(s, "yes") {
				force = true
			}
		}
	}
	model := ""
	if v, ok := jc.Payload()["model"]; ok && v != nil {
		model = strings.TrimSpace(fmt.Sprint(v))
	}

	jc.Progress("runtime_plan_build", 2, "Building runtime learning plan")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:          p.db,
		Log:         p.log,
		Path:        p.path,
		PathNodes:   p.nodes,
		NodeDocs:    p.nodeDocs,
		Summaries:   p.summaries,
		UserProfile: p.userProf,
		ProgEvents:  p.progEvents,
		AI:          p.ai,
		Bootstrap:   p.bootstrap,
	}).RuntimePlanBuild(jc.Ctx, learningmod.RuntimePlanBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		Force:         force,
		Model:         model,
	})
	if err != nil {
		jc.Fail("runtime_plan_build", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"nodes":           out.Nodes,
	})
	return nil
}

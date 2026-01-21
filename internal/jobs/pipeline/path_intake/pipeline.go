package path_intake

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	runtime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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
	threadID, _ := jc.PayloadUUID("thread_id")

	jc.Progress("intake", 2, "Reviewing your materials")

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		Summaries: p.summaries,
		Path:      p.path,
		Prefs:     p.prefs,
		Threads:   p.threads,
		Messages:  p.messages,
		AI:        p.ai,
		Notify:    p.notify,
		Bootstrap: p.bootstrap,
	}).PathIntake(jc.Ctx, learningmod.PathIntakeInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		ThreadID:      threadID,
		JobID:         jc.Job.ID,
		WaitForUser:   true,
	})
	if err != nil {
		jc.Fail("intake", err)
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// WAITPOINT HANDLING (THIS IS THE IMPORTANT PART)
	// ─────────────────────────────────────────────────────────────

	if strings.EqualFold(strings.TrimSpace(out.Status), "waiting_user") {

		// Build FILES_JSON for the waitpoint interpreter
		filesForWaitpoint := []map[string]any{}
		if p.files != nil {
			rows, _ := p.files.GetByMaterialSetIDs(
				dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB},
				[]uuid.UUID{setID},
			)
			for _, f := range rows {
				if f == nil || f.ID == uuid.Nil {
					continue
				}
				filesForWaitpoint = append(filesForWaitpoint, map[string]any{
					"file_id":       f.ID.String(),
					"original_name": strings.TrimSpace(f.OriginalName),
					"mime_type":     strings.TrimSpace(f.MimeType),
					"size_bytes":    f.SizeBytes,
				})
			}
		}

		spec := runtime.WaitpointSpec{
			Version:  1,
			Kind:     "path_intake.structure_v1",
			Step:     "path_intake",
			Blocking: true,
			ThreadID: out.ThreadID.String(),
			MinSeq: func() int64 {
				if out.Meta == nil {
					return 0
				}
				switch v := out.Meta["question_seq"].(type) {
				case int64:
					return v
				case int:
					return int64(v)
				case float64:
					return int64(v)
				default:
					return 0
				}
			}(),
		}

		state := runtime.WaitpointState{
			Version: 1,
			Phase:   "awaiting_choice",
		}

		data := map[string]any{
			"material_set_id": setID.String(),
			"saga_id":         sagaID.String(),
			"path_id":         out.PathID.String(),
			"thread_id":       out.ThreadID.String(),
			"intake":          out.Intake,
			"meta":            out.Meta,
			"files":           filesForWaitpoint,
		}

		jc.WaitForUser(
			"waiting_user",
			3,
			"Waiting for your response…",
			spec,
			state,
			data,
		)
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// NORMAL SUCCESS PATH
	// ─────────────────────────────────────────────────────────────

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"thread_id":       out.ThreadID.String(),
		"intake":          out.Intake,
	})
	return nil
}











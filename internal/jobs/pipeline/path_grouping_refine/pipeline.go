package path_grouping_refine

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	runtime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	waitcfg "github.com/yungbote/neurobridge-backend/internal/waitpoint/configs"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.path == nil || p.files == nil || p.fileSigs == nil {
		jc.Fail("validate", fmt.Errorf("path_grouping_refine: pipeline not configured"))
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	pathID, _ := jc.PayloadUUID("path_id")
	if pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}
	threadID, _ := jc.PayloadUUID("thread_id")

	jc.Progress("refine", 2, "Refining path grouping")

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:       p.db,
		Log:      p.log,
		Path:     p.path,
		Files:    p.files,
		FileSigs: p.fileSigs,
		Prefs:    p.prefs,
		Threads:  p.threads,
		Messages: p.messages,
		Notify:   p.notify,
	}).PathGroupingRefine(jc.Ctx, learningmod.PathGroupingRefineInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		PathID:        pathID,
		ThreadID:      threadID,
		JobID:         jc.Job.ID,
		WaitForUser:   true,
	})
	if err != nil {
		jc.Fail("refine", err)
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(out.Status), "waiting_user") {
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
			Kind:     waitcfg.PathGroupingRefineKind,
			Step:     "path_grouping_refine",
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

		var options any
		if out.Meta != nil {
			options = out.Meta["options"]
		}
		data := map[string]any{
			"material_set_id": setID.String(),
			"path_id":         pathID.String(),
			"thread_id":       out.ThreadID.String(),
			"intake":          out.Intake,
			"options":         options,
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

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"path_id":           pathID.String(),
		"status":            out.Status,
		"paths_before":      out.PathsBefore,
		"paths_after":       out.PathsAfter,
		"files_considered":  out.FilesConsidered,
		"confidence":        out.Confidence,
	})
	return nil
}

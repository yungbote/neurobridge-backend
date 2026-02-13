package path_grouping_refine

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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

	stageCfg := stageConfig(jc.Payload())
	waitForUser := true
	confirmExternally := false
	if stageCfg != nil {
		if v, ok := stageCfg["wait_for_user"]; ok {
			waitForUser = boolFromAny(v)
		}
		if boolFromAny(stageCfg["waitpoint_external"]) || boolFromAny(stageCfg["confirm_externally"]) {
			confirmExternally = true
			waitForUser = false
		}
	}

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
		OwnerUserID:       jc.Job.OwnerUserID,
		MaterialSetID:     setID,
		PathID:            pathID,
		ThreadID:          threadID,
		JobID:             jc.Job.ID,
		WaitForUser:       waitForUser,
		ConfirmExternally: confirmExternally,
	})
	if err != nil {
		jc.Fail("refine", err)
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(out.Status), "waiting_user") {
		meta := map[string]any{
			"job_run_id":         jc.Job.ID.String(),
			"owner_user_id":      jc.Job.OwnerUserID.String(),
			"material_set_id":    setID.String(),
			"path_id":            out.PathID.String(),
			"status":             out.Status,
			"paths_before":       out.PathsBefore,
			"paths_after":        out.PathsAfter,
			"files_considered":   out.FilesConsidered,
			"confidence":         out.Confidence,
			"needs_confirmation": out.NeedsConfirmation,
		}
		inputs := map[string]any{
			"material_set_id":    setID.String(),
			"path_id":            pathID.String(),
			"thread_id":          threadID.String(),
			"wait_for_user":      waitForUser,
			"confirm_externally": confirmExternally,
		}
		chosen := map[string]any{
			"status": out.Status,
		}
		userID := jc.Job.OwnerUserID
		_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
			DecisionType:  p.Type(),
			DecisionPhase: "build",
			DecisionMode:  "deterministic",
			UserID:        &userID,
			PathID:        &out.PathID,
			MaterialSetID: &setID,
			Inputs:        inputs,
			Chosen:        chosen,
			Metadata:      meta,
			Payload:       jc.Payload(),
			Validate:      false,
			RequireTrace:  true,
		})
		if traceErr != nil {
			jc.Fail("structural_trace", traceErr)
			return nil
		}

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

		spec := jobrt.WaitpointSpec{
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

		if cfg := jobrt.StageWaitpointConfig(jc.Payload()); cfg != nil {
			spec = jobrt.ApplyWaitpointConfig(spec, cfg)
		}

		state := jobrt.WaitpointState{
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
		if cfg := jobrt.StageWaitpointConfig(jc.Payload()); cfg != nil {
			data["waitpoint_config"] = cfg
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

	var options any
	if out.Meta != nil {
		options = out.Meta["options"]
	}
	meta := map[string]any{
		"job_run_id":         jc.Job.ID.String(),
		"owner_user_id":      jc.Job.OwnerUserID.String(),
		"material_set_id":    setID.String(),
		"path_id":            out.PathID.String(),
		"status":             out.Status,
		"paths_before":       out.PathsBefore,
		"paths_after":        out.PathsAfter,
		"files_considered":   out.FilesConsidered,
		"confidence":         out.Confidence,
		"needs_confirmation": out.NeedsConfirmation,
	}
	inputs := map[string]any{
		"material_set_id":    setID.String(),
		"path_id":            pathID.String(),
		"thread_id":          threadID.String(),
		"wait_for_user":      waitForUser,
		"confirm_externally": confirmExternally,
	}
	chosen := map[string]any{
		"status":       out.Status,
		"paths_before": out.PathsBefore,
		"paths_after":  out.PathsAfter,
	}
	userID := jc.Job.OwnerUserID
	_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		PathID:        &out.PathID,
		MaterialSetID: &setID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      true,
		RequireTrace:  true,
	})
	if traceErr != nil {
		jc.Fail("invariant_validation", traceErr)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":    setID.String(),
		"path_id":            pathID.String(),
		"status":             out.Status,
		"paths_before":       out.PathsBefore,
		"paths_after":        out.PathsAfter,
		"files_considered":   out.FilesConsidered,
		"confidence":         out.Confidence,
		"meta":               out.Meta,
		"intake":             out.Intake,
		"needs_confirmation": out.NeedsConfirmation,
		"prompt":             out.Prompt,
		"options":            options,
	})
	return nil
}

func stageConfig(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	raw, ok := payload["stage_config"]
	if !ok || raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return nil
}

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	default:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	}
}

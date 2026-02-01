package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type toolExecResult struct {
	Text        string
	Metadata    map[string]any
	EnqueuedIDs []uuid.UUID
}

type toolSpec struct {
	ToolName     string
	JobType      string
	EntityType   string
	Requires     []string
	Group        string
	DeferIfBuild bool
}

var chatToolRegistry = map[string]toolSpec{
	"learning_build": {
		ToolName:   "learning_build",
		JobType:    "learning_build",
		EntityType: "material_set",
		Requires:   []string{"material_set_id"},
		Group:      "build",
	},
	"learning_build_progressive": {
		ToolName:   "learning_build_progressive",
		JobType:    "learning_build_progressive",
		EntityType: "material_set",
		Requires:   []string{"material_set_id"},
		Group:      "build",
	},
	"chat_rebuild": {
		ToolName:     "chat_rebuild",
		JobType:      "chat_rebuild",
		EntityType:   "chat_thread",
		Requires:     []string{"thread_id"},
		Group:        "chat",
		DeferIfBuild: true,
	},
	"chat_path_index": {
		ToolName:     "chat_path_index",
		JobType:      "chat_path_index",
		EntityType:   "path",
		Requires:     []string{"path_id"},
		Group:        "chat",
		DeferIfBuild: true,
	},
}

type toolExecSummary struct {
	ToolName string         `json:"tool_name"`
	Message  string         `json:"message,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type toolSkipSummary struct {
	ToolName string `json:"tool_name"`
	Reason   string `json:"reason"`
}

func executeChatToolCalls(ctx context.Context, deps RespondDeps, thread *types.ChatThread, calls []chatToolCall) (toolExecResult, error) {
	out := toolExecResult{Metadata: map[string]any{}}
	if deps.Jobs == nil || deps.JobRuns == nil || thread == nil || thread.ID == uuid.Nil {
		out.Text = "I can help with that, but tool execution is not available right now."
		return out, nil
	}
	if len(calls) == 0 {
		out.Text = "I didn’t detect a specific pipeline to run. What should I trigger?"
		return out, nil
	}

	maxCalls := resolveChatToolMaxCalls()
	ordered := pickToolCalls(calls)
	executed := []toolExecSummary{}
	skipped := []toolSkipSummary{}
	usedGroups := map[string]bool{}
	buildStarted := false
	var missingArgs []string
	var missingTool string

	for _, call := range ordered {
		if len(executed) >= maxCalls {
			skipped = append(skipped, toolSkipSummary{ToolName: call.ToolName, Reason: "max_calls"})
			continue
		}
		spec, ok := chatToolRegistry[strings.ToLower(strings.TrimSpace(call.ToolName))]
		if !ok {
			skipped = append(skipped, toolSkipSummary{ToolName: call.ToolName, Reason: "unsupported"})
			continue
		}
		if spec.Group != "" && usedGroups[spec.Group] {
			skipped = append(skipped, toolSkipSummary{ToolName: call.ToolName, Reason: "group_conflict"})
			continue
		}
		if buildStarted && spec.DeferIfBuild {
			skipped = append(skipped, toolSkipSummary{ToolName: call.ToolName, Reason: "deferred_by_build"})
			continue
		}

		res, execOK, skipReason, missing := executeSingleToolCall(ctx, deps, thread, call, spec)
		if len(missing) > 0 && missingArgs == nil {
			missingArgs = missing
			missingTool = spec.ToolName
		}
		if !execOK {
			if skipReason == "" {
				skipReason = "skipped"
			}
			skipped = append(skipped, toolSkipSummary{ToolName: call.ToolName, Reason: skipReason})
			continue
		}
		if spec.Group != "" {
			usedGroups[spec.Group] = true
		}
		if spec.Group == "build" {
			buildStarted = true
		}
		executed = append(executed, toolExecSummary{
			ToolName: spec.ToolName,
			Message:  res.Text,
			Metadata: res.Metadata,
		})
		out.EnqueuedIDs = append(out.EnqueuedIDs, res.EnqueuedIDs...)
	}

	if len(executed) == 0 {
		if len(missingArgs) > 0 {
			out.Text = fmt.Sprintf("I can do that, but I need: %s.", strings.Join(missingArgs, ", "))
			out.Metadata["tool_error"] = "missing_args"
			out.Metadata["missing_args"] = missingArgs
			out.Metadata["tool_name"] = missingTool
			return out, nil
		}
		out.Text = "I can’t run that pipeline yet. Please try a supported action."
		out.Metadata["tool_error"] = "unsupported_tool"
		if len(skipped) > 0 {
			out.Metadata["skipped"] = skipped
		}
		return out, nil
	}

	if len(executed) == 1 {
		out.Text = executed[0].Message
		for k, v := range executed[0].Metadata {
			out.Metadata[k] = v
		}
	} else {
		lines := make([]string, 0, len(executed))
		for _, ex := range executed {
			if ex.Message != "" {
				lines = append(lines, ex.Message)
			} else {
				lines = append(lines, fmt.Sprintf("Started %s.", ex.ToolName))
			}
		}
		out.Text = strings.Join(lines, "\n")
	}

	out.Metadata["executed"] = executed
	if len(skipped) > 0 {
		out.Metadata["skipped"] = skipped
	}
	return out, nil
}

func pickToolCalls(calls []chatToolCall) []chatToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := append([]chatToolCall{}, calls...)
	// Stable sort by confidence (desc).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Confidence > out[i].Confidence {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func missingRequiredArgs(req []string, resolved map[string]uuid.UUID) []string {
	out := []string{}
	for _, r := range req {
		key := strings.ToLower(strings.TrimSpace(r))
		if resolved[key] == uuid.Nil {
			out = append(out, r)
		}
	}
	return out
}

func parseUUIDFromAny(v any) uuid.UUID {
	switch t := v.(type) {
	case string:
		id, _ := uuid.Parse(strings.TrimSpace(t))
		return id
	case []byte:
		id, _ := uuid.Parse(strings.TrimSpace(string(t)))
		return id
	default:
		return uuid.Nil
	}
}

func payloadUUID(raw datatypes.JSON, key string) uuid.UUID {
	if len(raw) == 0 {
		return uuid.Nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return uuid.Nil
	}
	v, ok := m[key]
	if !ok {
		return uuid.Nil
	}
	return parseUUIDFromAny(v)
}

func resolveChatToolMaxCalls() int {
	n := 2
	if v := strings.TrimSpace(os.Getenv("CHAT_TOOL_MAX_CALLS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

func executeSingleToolCall(ctx context.Context, deps RespondDeps, thread *types.ChatThread, call chatToolCall, spec toolSpec) (toolExecResult, bool, string, []string) {
	out := toolExecResult{Metadata: map[string]any{}}
	resolved := map[string]uuid.UUID{}
	for k, v := range call.Arguments {
		if id := parseUUIDFromAny(v); id != uuid.Nil {
			resolved[strings.ToLower(strings.TrimSpace(k))] = id
		}
	}

	if thread.ID != uuid.Nil {
		resolved["thread_id"] = thread.ID
	}

	if thread.PathID != nil && *thread.PathID != uuid.Nil {
		if _, ok := resolved["path_id"]; !ok {
			resolved["path_id"] = *thread.PathID
		}
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	if _, ok := resolved["material_set_id"]; !ok {
		if pathID, ok := resolved["path_id"]; ok && deps.Path != nil {
			if row, err := deps.Path.GetByID(dbc, pathID); err == nil && row != nil && row.MaterialSetID != nil {
				resolved["material_set_id"] = *row.MaterialSetID
			}
		}
	}

	if _, ok := resolved["material_set_id"]; !ok {
		if thread.JobID != nil && *thread.JobID != uuid.Nil {
			if jrRows, err := deps.JobRuns.GetByIDs(dbc, []uuid.UUID{*thread.JobID}); err == nil && len(jrRows) > 0 && jrRows[0] != nil {
				id := payloadUUID(jrRows[0].Payload, "material_set_id")
				if id != uuid.Nil {
					resolved["material_set_id"] = id
				}
				if resolved["path_id"] == uuid.Nil {
					pid := payloadUUID(jrRows[0].Payload, "path_id")
					if pid != uuid.Nil {
						resolved["path_id"] = pid
					}
				}
			}
		}
	}

	missing := missingRequiredArgs(spec.Requires, resolved)
	if len(missing) > 0 {
		return out, false, "missing_args", missing
	}

	payload := map[string]any{}
	switch spec.ToolName {
	case "learning_build", "learning_build_progressive":
		payload["material_set_id"] = resolved["material_set_id"].String()
		if pid, ok := resolved["path_id"]; ok && pid != uuid.Nil {
			payload["path_id"] = pid.String()
		}
		if tid, ok := resolved["thread_id"]; ok && tid != uuid.Nil {
			payload["thread_id"] = tid.String()
		}
	case "chat_rebuild":
		payload["thread_id"] = resolved["thread_id"].String()
	case "chat_path_index":
		payload["path_id"] = resolved["path_id"].String()
	}

	entityID := resolved[spec.EntityType+"_id"]
	if entityID == uuid.Nil {
		entityID = resolved[spec.EntityType]
	}

	if entityID != uuid.Nil {
		if has, _ := deps.JobRuns.HasRunnableForEntity(dbc, thread.UserID, spec.EntityType, entityID, spec.JobType); has {
			out.Text = fmt.Sprintf("A %s job is already running. I’ll post updates here.", spec.JobType)
			out.Metadata["tool_name"] = spec.ToolName
			out.Metadata["already_running"] = true
			return out, true, "", nil
		}
	}

	job, err := deps.Jobs.Enqueue(dbc, thread.UserID, spec.JobType, spec.EntityType, &entityID, payload)
	if err != nil || job == nil || job.ID == uuid.Nil {
		out.Text = "I tried to start that pipeline, but it failed to enqueue."
		if err != nil {
			out.Metadata["tool_error"] = err.Error()
		}
		out.Metadata["tool_name"] = spec.ToolName
		return out, false, "enqueue_failed", nil
	}

	out.EnqueuedIDs = append(out.EnqueuedIDs, job.ID)
	out.Metadata["tool_name"] = spec.ToolName
	out.Metadata["job_id"] = job.ID.String()

	if spec.ToolName == "learning_build" || spec.ToolName == "learning_build_progressive" {
		if deps.Threads != nil {
			_ = deps.Threads.UpdateFields(dbc, thread.ID, map[string]any{
				"job_id": job.ID,
			})
		}
		if deps.Path != nil {
			if pid, ok := resolved["path_id"]; ok && pid != uuid.Nil {
				_ = deps.Path.UpdateFields(dbc, pid, map[string]any{
					"job_id": job.ID,
				})
			}
		}
	}

	out.Text = fmt.Sprintf("Started %s. I’ll post updates here.", spec.JobType)
	return out, true, "", nil
}

package configs

import (
	"encoding/json"
	"fmt"
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/waitpoint"
)

const PathIntakeStructureKind = "path_intake.structure_v1"

func PathIntakeStructureConfig() waitpoint.Config {
	return waitpoint.Config{
		Kind: PathIntakeStructureKind,

		BuildClassifierPrompt: func(ic *waitpoint.InterpreterContext) (string, string, string, map[string]any, error) {
			if ic == nil || ic.Envelope == nil || ic.UserMessage == nil {
				return "", "", "", nil, fmt.Errorf("missing interpreter context")
			}
			data := ic.Envelope.Data
			filesJSON := "(none)"
			if v, ok := data["files"]; ok {
				if b, err := json.Marshal(v); err == nil {
					filesJSON = string(b)
				}
			}
			pathsJSON := "(none)"
			if v, ok := data["intake"]; ok {
				if intake, ok := v.(map[string]any); ok && intake != nil {
					payload := map[string]any{
						"primary_path_id": strings.TrimSpace(fmt.Sprint(intake["primary_path_id"])),
						"paths":           intake["paths"],
					}
					if b, err := json.Marshal(payload); err == nil && len(b) > 0 {
						pathsJSON = string(b)
					}
				}
			}
			excerpt := buildMessageExcerpt(ic.Messages, 24)
			system := strings.TrimSpace(strings.Join([]string{
				"ROLE: Waitpoint classifier for path grouping intake.",
				"TASK: Classify the user's latest message about the proposed grouping.",
				"OUTPUT: Return ONLY JSON matching the schema (no extra keys).",
				"CONTEXT: The user is responding to a proposed grouping of uploaded files into learning paths.",
				"",
				"Classify the message into exactly one case:",
				"- no_commit: the user has not committed to any decision yet (still discussing, asking questions, or unrelated info)",
				"- ambiguous_commit: the user seems to commit but it's unclear whether they want to confirm or change the grouping",
				"- committed: the user clearly confirms the grouping or clearly requests a different grouping",
				"",
				"For committed decisions, also determine commit_type:",
				"- 'confirm': user accepts the proposed grouping as-is",
				"- 'change': user clearly asks to regroup files, split/merge paths, or move files between paths",
				"",
				"Rules:",
				"- If the user is asking questions or discussing, classify as no_commit",
				"- If the user gives a short confirmation like 'ok', 'sure', 'yes' without clear context, classify as ambiguous_commit",
				"- If the user says 'change grouping' but doesn't describe how, classify as ambiguous_commit",
				"",
			}, "\n"))

			user := strings.TrimSpace(strings.Join([]string{
				"CONVERSATION_EXCERPT:",
				excerpt,
				"",
				"PATHS_JSON (proposed grouping):",
				pathsJSON,
				"",
				"FILES_JSON:",
				filesJSON,
				"",
				"LATEST_USER_MESSAGE:",
				strings.TrimSpace(ic.UserMessage.Content),
			}, "\n"))

			schema := map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"case": map[string]any{
						"type": "string",
						"enum": []any{"no_commit", "ambiguous_commit", "committed"},
					},
					"commit_type": map[string]any{
						"type": "string",
						"enum": []any{"confirm", "change", "unspecified"},
					},
					"confidence": map[string]any{"type": "number"},
					"reason":     map[string]any{"type": "string"},
					"clarifying_prompt": map[string]any{
						"type": "string",
					},
				},
				"required": []string{
					"case",
					"commit_type",
					"confidence",
					"reason",
					"clarifying_prompt",
				},
			}

			return system, user, "path_intake_paths_v1", schema, nil
		},

		Reduce: func(ic *waitpoint.InterpreterContext, cr waitpoint.ClassifierResult) (waitpoint.Decision, error) {
			switch cr.Case {

			case waitpoint.CaseNotCommit:
				return waitpoint.Decision{
					Kind:               waitpoint.DecisionContinueChat,
					EnqueueChatRespond: true,
				}, nil

			case waitpoint.CaseAmbiguousCommit:
				return waitpoint.Decision{
					Kind: waitpoint.DecisionAskClarify,
					AssistantMessage: defaultString(cr.ClarifyPrompt,
						"Do you want me to proceed with the proposed paths, or should I regroup the files? If regroup, tell me how you want them arranged."),
					Selection: map[string]any{
						"pending_guess": cr.CommitType,
					},
				}, nil

			case waitpoint.CaseCommitted:
				commitType := strings.ToLower(strings.TrimSpace(cr.CommitType))
				if commitType == "" || commitType == "unspecified" {
					// Fallback: treat as ambiguous
					return waitpoint.Decision{
						Kind: waitpoint.DecisionAskClarify,
						AssistantMessage: defaultString(cr.ClarifyPrompt,
							"Do you want me to proceed with the proposed paths, or should I regroup the files? If regroup, tell me how you want them arranged."),
					}, nil
				}

				confirmMsg := "Got it — I'll proceed with these paths and continue."
				if commitType == "change" {
					confirmMsg = "Got it — I'll regroup the files based on your notes and continue."
				}

				return waitpoint.Decision{
					Kind:           waitpoint.DecisionConfirmResume,
					ConfirmMessage: confirmMsg,
					Selection: map[string]any{
						"commit_type": commitType,
					},
				}, nil

			default:
				return waitpoint.Decision{
					Kind:               waitpoint.DecisionContinueChat,
					EnqueueChatRespond: true,
				}, nil
			}
		},

		ApplySelection: func(ic *waitpoint.InterpreterContext, selection map[string]any) error {
			// Applied in waitpoint_interpret pipeline where PathRepo is available
			return nil
		},
	}
}

func buildMessageExcerpt(msgs []*types.ChatMessage, max int) string {
	if len(msgs) == 0 {
		return "(none)"
	}
	if max <= 0 {
		max = 20
	}
	if len(msgs) > max {
		msgs = msgs[len(msgs)-max:]
	}
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		txt := strings.TrimSpace(m.Content)
		if txt == "" {
			continue
		}
		if len(txt) > 800 {
			txt = txt[:800] + "…"
		}
		b.WriteString(strings.ToLower(m.Role))
		b.WriteString(": ")
		b.WriteString(txt)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func defaultString(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fallback
}

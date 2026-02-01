package configs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/waitpoint"
)

const PathGroupingRefineKind = "path_grouping_refine.structure_v1"

func PathGroupingRefineConfig() waitpoint.Config {
	return waitpoint.Config{
		Kind: PathGroupingRefineKind,

		BuildClassifierPrompt: func(ic *waitpoint.InterpreterContext) (string, string, string, map[string]any, error) {
			if ic == nil || ic.Envelope == nil || ic.UserMessage == nil {
				return "", "", "", nil, fmt.Errorf("missing interpreter context")
			}
			data := ic.Envelope.Data
			optionsJSON := "(none)"
			if v, ok := data["options"]; ok {
				if b, err := json.Marshal(v); err == nil {
					optionsJSON = string(b)
				}
			}

			excerpt := buildMessageExcerpt(ic.Messages, 20)
			system := strings.TrimSpace(strings.Join([]string{
				"ROLE: Waitpoint classifier for grouping refinement.",
				"TASK: Determine which grouping option the user chose (if any).",
				"OUTPUT: Return ONLY JSON matching the schema (no extra keys).",
				"CONTEXT: The user is choosing between two grouping options for uploaded files.",
				"",
				"Classify the message into exactly one case:",
				"- no_commit: user is asking questions or not choosing yet",
				"- ambiguous_commit: user seems to choose but it's unclear which option",
				"- committed: user clearly chose an option",
				"",
				"When committed, select the option id from OPTIONS_JSON (field: id).",
				"If the user replies with a number (e.g., 1 or 2), map it to the option with matching choice field.",
			}, "\n"))

			user := strings.TrimSpace(strings.Join([]string{
				"CONVERSATION_EXCERPT:",
				excerpt,
				"",
				"OPTIONS_JSON:",
				optionsJSON,
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
					"selected_mode": map[string]any{"type": "string"},
					"confidence":    map[string]any{"type": "number"},
					"reason":        map[string]any{"type": "string"},
					"clarifying_prompt": map[string]any{
						"type": "string",
					},
				},
				"required": []string{"case", "selected_mode", "confidence", "reason", "clarifying_prompt"},
			}

			return system, user, "path_grouping_refine_choice_v1", schema, nil
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
						"Do you want to keep the current grouping (reply 1) or use the refined grouping (reply 2)?"),
				}, nil
			case waitpoint.CaseCommitted:
				selected := strings.TrimSpace(cr.Selected)
				if selected == "" {
					return waitpoint.Decision{
						Kind: waitpoint.DecisionAskClarify,
						AssistantMessage: defaultString(cr.ClarifyPrompt,
							"Do you want to keep the current grouping (reply 1) or use the refined grouping (reply 2)?"),
					}, nil
				}

				opt := findGroupingOption(ic.Envelope.Data, selected)
				if opt == nil {
					return waitpoint.Decision{
						Kind: waitpoint.DecisionAskClarify,
						AssistantMessage: defaultString(cr.ClarifyPrompt,
							"Do you want to keep the current grouping (reply 1) or use the refined grouping (reply 2)?"),
					}, nil
				}

				selection := map[string]any{
					"paths":                    opt.Paths,
					"paths_confirmed":          true,
					"paths_confirmation_type":  "confirm",
					"paths_refined":            true,
					"paths_refined_mode":       strings.TrimSpace(opt.ID),
					"grouping_prefer_single":   opt.PreferSingle,
				}

				return waitpoint.Decision{
					Kind:           waitpoint.DecisionConfirmResume,
					ConfirmMessage: "Got it — I'll proceed with that grouping.",
					Selection:      selection,
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

type groupingOption struct {
	ID           string
	Paths        any
	PreferSingle bool
}

func findGroupingOption(data map[string]any, id string) *groupingOption {
	if data == nil {
		return nil
	}
	raw, ok := data["options"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		optID := strings.TrimSpace(fmt.Sprint(m["id"]))
		choice := strings.TrimSpace(fmt.Sprint(m["choice"]))
		if optID == "" {
			continue
		}
		if !(strings.EqualFold(optID, id) || strings.EqualFold(choice, id)) {
			continue
		}
		return &groupingOption{
			ID:           optID,
			Paths:        m["paths"],
			PreferSingle: boolFromAny(m["prefer_single_path"]),
		}
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
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

package configs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/waitpoint"
)

const YAMLIntentKind = "yaml_intent_v1"

// YAMLIntentConfig defines a generic YAML-driven waitpoint interpreter.
// It expects waitpoint_config in the envelope data or child payload:
//
//	waitpoint:
//	  kind: yaml_intent_v1
//	  prompt: "..."
//	  clarify_prompt: "..."
//	  intents:
//	    - id: confirm
//	      label: Confirm
//	      description: "proceed with current grouping"
//	      action: confirm_resume
//	      selection: {commit_type: confirm}
//	      confirm_message: "..."
//	    - id: regroup
//	      label: Regroup
//	      description: "change grouping"
//	      action: confirm_resume
//	      selection: {commit_type: change}
func YAMLIntentConfig() waitpoint.Config {
	return waitpoint.Config{
		Kind: YAMLIntentKind,
		BuildClassifierPrompt: func(ic *waitpoint.InterpreterContext) (string, string, string, map[string]any, error) {
			cfg := waitpointConfigFromEnvelope(ic)
			if cfg == nil {
				return "", "", "", nil, fmt.Errorf("missing waitpoint_config for yaml_intent")
			}
			intents := intentsFromConfig(cfg)
			if len(intents) == 0 {
				return "", "", "", nil, fmt.Errorf("yaml_intent requires intents")
			}

			system := strings.TrimSpace(strings.Join([]string{
				"ROLE: Waitpoint intent classifier.",
				"TASK: Classify the user's latest message into exactly one intent from the list.",
				"OUTPUT: Return ONLY JSON matching the schema (no extra keys).",
				"ASSUME: The user is responding to the waitpoint prompt right now.",
				"Always set selected_mode to the intent id that best matches the user's message, even for no_commit.",
				"If the user gives a short affirmation like 'yes', 'ok', 'sure', or 'confirm',",
				"treat it as a committed choice for the default confirm intent when one exists.",
				"If the user expresses regroup/change keywords (split/merge/move/regroup/refine),",
				"treat it as a committed choice for the change/refine intent when one exists.",
				"Prefer matching an intent over no_commit when the message is brief but affirmative.",
				"When the user replies with a number (e.g., 1 or 2), map it to the most likely intent.",
				"If the user has not committed to any decision, output case=no_commit.",
				"If the user seems to commit but it's unclear which intent, output case=ambiguous_commit.",
				"If the user clearly matches an intent, output case=committed and set selected_mode to the intent id.",
			}, "\n"))

			prompt := strings.TrimSpace(stringFromAny(cfg["prompt"]))
			if prompt == "" {
				prompt = "(none)"
			}

			intentsJSON := "(none)"
			if b, err := json.Marshal(intents); err == nil {
				intentsJSON = string(b)
			}

			excerpt := "(none)"
			if ic != nil {
				excerpt = buildMessageExcerpt(ic.Messages, 18)
			}
			latest := ""
			if ic != nil && ic.UserMessage != nil {
				latest = strings.TrimSpace(ic.UserMessage.Content)
			}
			if latest == "" {
				latest = "(empty)"
			}

			user := strings.TrimSpace(strings.Join([]string{
				"CONVERSATION_EXCERPT:",
				excerpt,
				"",
				"WAITPOINT_PROMPT:",
				prompt,
				"",
				"INTENTS:",
				intentsJSON,
				"",
				"LATEST_USER_MESSAGE:",
				latest,
			}, "\n"))

			schema := map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"case": map[string]any{
						"type": "string",
						"enum": []any{"no_commit", "ambiguous_commit", "committed"},
					},
					"selected_mode": map[string]any{
						"type": "string",
						"enum": intentIDs(intents),
					},
					"confidence": map[string]any{"type": "number"},
					"reason":     map[string]any{"type": "string"},
					"clarifying_prompt": map[string]any{
						"type": "string",
					},
				},
				"required": []string{"case", "selected_mode", "confidence", "reason", "clarifying_prompt"},
			}

			return system, user, "waitpoint_yaml_intent_v1", schema, nil
		},
		Reduce: func(ic *waitpoint.InterpreterContext, cr waitpoint.ClassifierResult) (waitpoint.Decision, error) {
			cfg := waitpointConfigFromEnvelope(ic)
			intents := intentsFromConfig(cfg)

			switch cr.Case {
			case waitpoint.CaseNotCommit:
				return waitpoint.Decision{Kind: waitpoint.DecisionContinueChat, EnqueueChatRespond: true}, nil
			case waitpoint.CaseAmbiguousCommit:
				clarify := strings.TrimSpace(defaultString(cr.ClarifyPrompt, stringFromAny(cfg["clarify_prompt"])))
				if clarify == "" {
					clarify = "Please clarify your choice so I can proceed."
				}
				return waitpoint.Decision{Kind: waitpoint.DecisionAskClarify, AssistantMessage: clarify}, nil
			case waitpoint.CaseCommitted:
				intentID := strings.TrimSpace(cr.Selected)
				if intentID == "" {
					intentID = strings.TrimSpace(cr.Structure)
				}
				if intentID == "" {
					return waitpoint.Decision{Kind: waitpoint.DecisionAskClarify, AssistantMessage: "Please clarify your choice so I can proceed."}, nil
				}
				intent := intentByID(intents, intentID)
				if intent == nil {
					return waitpoint.Decision{Kind: waitpoint.DecisionAskClarify, AssistantMessage: "Please clarify your choice so I can proceed."}, nil
				}
				action := strings.ToLower(strings.TrimSpace(stringFromAny(intent["action"])))
				switch action {
				case "continue_chat":
					return waitpoint.Decision{Kind: waitpoint.DecisionContinueChat, EnqueueChatRespond: true}, nil
				case "ask_clarify":
					clarify := strings.TrimSpace(defaultString(stringFromAny(intent["clarify_prompt"]), stringFromAny(cfg["clarify_prompt"])))
					if clarify == "" {
						clarify = "Please clarify your choice so I can proceed."
					}
					return waitpoint.Decision{Kind: waitpoint.DecisionAskClarify, AssistantMessage: clarify}, nil
				default:
					confirmMsg := strings.TrimSpace(stringFromAny(intent["confirm_message"]))
					selection := copySelection(intent["selection"])
					selection["intent_id"] = intentID
					return waitpoint.Decision{Kind: waitpoint.DecisionConfirmResume, ConfirmMessage: confirmMsg, Selection: selection}, nil
				}
			default:
				return waitpoint.Decision{Kind: waitpoint.DecisionContinueChat, EnqueueChatRespond: true}, nil
			}
		},
		ApplySelection: func(ic *waitpoint.InterpreterContext, selection map[string]any) error {
			return nil
		},
	}
}

func waitpointConfigFromEnvelope(ic *waitpoint.InterpreterContext) map[string]any {
	if ic == nil || ic.Envelope == nil {
		return nil
	}
	if ic.Envelope.Data != nil {
		if v, ok := ic.Envelope.Data["waitpoint_config"]; ok {
			if cfg, ok := v.(map[string]any); ok {
				return cfg
			}
		}
	}
	if ic.ChildJob != nil && ic.ChildJob.Payload != nil {
		payload := map[string]any{}
		if err := json.Unmarshal(ic.ChildJob.Payload, &payload); err == nil {
			return runtime.StageWaitpointConfig(payload)
		}
	}
	return nil
}

func intentsFromConfig(cfg map[string]any) []map[string]any {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["intents"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["id"]))
		if id == "" {
			continue
		}
		m["id"] = id
		out = append(out, m)
	}
	return out
}

func intentIDs(intents []map[string]any) []any {
	if len(intents) == 0 {
		return []any{}
	}
	ids := make([]any, 0, len(intents))
	for _, it := range intents {
		id := strings.TrimSpace(stringFromAny(it["id"]))
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func intentByID(intents []map[string]any, id string) map[string]any {
	for _, it := range intents {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(it["id"])), strings.TrimSpace(id)) {
			return it
		}
	}
	return nil
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func copySelection(v any) map[string]any {
	src, _ := v.(map[string]any)
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src)+1)
	for k, val := range src {
		out[k] = val
	}
	return out
}

package runtime

import "strings"

// StageWaitpointConfig reads a waitpoint config from the current job payload.
// Expect payload: {"stage_config": {"waitpoint": { ... } } }
func StageWaitpointConfig(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	raw, ok := payload["stage_config"]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return nil
	}
	wp, _ := m["waitpoint"].(map[string]any)
	return wp
}

// WaitpointKindFromConfig extracts a kind string from a waitpoint config.
func WaitpointKindFromConfig(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	if v := stringFromAny(cfg["kind"]); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

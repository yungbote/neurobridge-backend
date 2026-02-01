package promptstyle

import "strings"

const marker = "NEUROBRIDGE_PROMPT_STYLE_V1"

// ApplySystem prepends a concise, structured guidance block to system prompts.
// It is intentionally minimal to avoid changing task semantics while improving output quality.
func ApplySystem(system string, mode string) string {
	base := strings.TrimSpace(system)
	if base == "" {
		return base
	}
	if strings.Contains(base, marker) {
		return base
	}
	mode = strings.ToLower(strings.TrimSpace(mode))

	taskSummary := ""
	for _, line := range strings.Split(base, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			taskSummary = trimmed
			break
		}
	}

	var b strings.Builder
	b.WriteString(marker)
	b.WriteString("\nYou are a careful assistant for Neurobridge.")
	if taskSummary != "" {
		b.WriteString("\nTask summary: " + taskSummary)
	}
	b.WriteString("\nFollow the system and user instructions precisely.")
	b.WriteString("\nIf an output format or schema is specified, output only that format.")
	b.WriteString("\nDo not add analysis or extra commentary.")
	b.WriteString("\nUse provided inputs as grounding; do not invent facts or citations.")
	b.WriteString("\nIf information is missing, say so or use conservative defaults.")
	if mode == "json" {
		b.WriteString("\nReturn a single JSON object that conforms to the schema and contains no extra keys.")
	} else {
		b.WriteString("\nBe concise and structured when helpful.")
	}
	b.WriteString("\n---\n")
	b.WriteString(base)
	return strings.TrimSpace(b.String())
}

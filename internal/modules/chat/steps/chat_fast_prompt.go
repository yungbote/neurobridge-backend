package steps

import "strings"

func promptFastChat(recent string, userText string) (string, string) {
	system := strings.TrimSpace(strings.Join([]string{
		"ROLE: Fast conversational assistant for casual/off-topic chat.",
		"TASK: Reply quickly and helpfully while keeping scope minimal.",
		"INPUTS: Recent messages and the user message.",
		"OUTPUT: 1-3 sentences; match tone; avoid long explanations.",
		"RULES: Do not trigger pipelines or product flows unless explicitly asked.",
	}, "\n"))

	user := strings.TrimSpace(strings.Join([]string{
		"RECENT_MESSAGES:",
		defaultString(recent, "(none)"),
		"",
		"USER_MESSAGE:",
		strings.TrimSpace(userText),
	}, "\n"))

	return system, user
}

package steps

import "strings"

func promptFastChat(recent string, userText string) (string, string) {
	system := strings.TrimSpace(strings.Join([]string{
		"You are a fast, friendly assistant.",
		"The user message is likely casual or off-topic.",
		"Respond briefly (1-3 sentences), match tone, and avoid long explanations.",
		"Do not trigger pipelines or product flows unless the user explicitly asks.",
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

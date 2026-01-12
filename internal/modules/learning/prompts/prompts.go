package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type Prompt struct {
	Name       string
	Version    int
	System     string
	User       string
	SchemaName string
	Schema     map[string]any
}

func (p Prompt) Fingerprint() string {
	h := sha256.Sum256([]byte(
		strings.TrimSpace(p.Name) + "|" +
			itoa(p.Version) + "|" +
			strings.TrimSpace(p.System) + "|" +
			strings.TrimSpace(p.User),
	))
	return hex.EncodeToString(h[:])
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [32]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + (i % 10))
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

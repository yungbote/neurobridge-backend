package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func chunkByChars(s string, n int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if n <= 0 {
		return []string{s}
	}
	r := []rune(s)
	if len(r) <= n {
		return []string{s}
	}
	out := make([]string, 0, (len(r)/n)+1)
	for i := 0; i < len(r); i += n {
		end := i + n
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}

func trimToChars(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" || n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// crude token estimate (~4 chars/token English)
func estimateTokens(s string) int {
	r := []rune(s)
	return int(math.Ceil(float64(len(r)) / 4.0))
}

func trimToTokens(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if estimateTokens(s) <= n {
		return s
	}
	return trimToChars(s, n*4)
}

func formatRecent(msgs []*types.ChatMessage, max int) string {
	if len(msgs) == 0 {
		return ""
	}
	// msgs may be DESC; normalize to ASC for readability.
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Seq < msgs[j].Seq })
	if max > 0 && len(msgs) > max {
		msgs = msgs[len(msgs)-max:]
	}
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		b.WriteString("[" + itoa64(m.Seq) + "] " + strings.TrimSpace(m.Role) + ": " + trimToChars(content, 800) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func formatWindow(msgs []*types.ChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Seq < msgs[j].Seq })
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		b.WriteString("[" + itoa64(m.Seq) + "] " + strings.TrimSpace(m.Role) + ":\n" + content + "\n\n")
	}
	return strings.TrimSpace(b.String())
}

func itoa64(i int64) string {
	if i == 0 {
		return "0"
	}
	sign := ""
	if i < 0 {
		sign = "-"
		i = -i
	}
	var b [64]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + (i % 10))
		i /= 10
	}
	return sign + string(b[pos:])
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func deterministicUUID(s string) uuid.UUID {
	h := sha256.Sum256([]byte(s))
	id, err := uuid.FromBytes(h[:16])
	if err != nil {
		return uuid.New()
	}
	return id
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

func nowUTC() time.Time { return time.Now().UTC() }

func nonNilEmb(v []float32) []float32 {
	if v == nil {
		return []float32{}
	}
	return v
}

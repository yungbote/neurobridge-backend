package course_build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

func readAll(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildCombinedFromChunks(chunks []*types.MaterialChunk, maxLen int) string {
	var b strings.Builder
	for _, ch := range chunks {
		if ch == nil || strings.TrimSpace(ch.Text) == "" {
			continue
		}
		if b.Len() >= maxLen {
			break
		}
		s := ch.Text
		if b.Len()+len(s)+2 > maxLen {
			s = s[:max(0, maxLen-b.Len()-2)]
		}
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func normalizeOneWordTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")

	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		}
	}
	return string(out)
}

func normalizeTags(v any, maxN int) []string {
	raw := toStringSlice(v)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		tt := normalizeOneWordTag(t)
		if tt == "" || seen[tt] {
			continue
		}
		seen[tt] = true
		out = append(out, tt)
		if maxN > 0 && len(out) >= maxN {
			break
		}
	}
	return out
}

func clampString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return strings.TrimSpace(s[:maxLen])
}

func extractTopicsFromLessonMetadata(js datatypes.JSON) []string {
	if len(js) == 0 {
		return []string{}
	}
	var m map[string]any
	if err := json.Unmarshal(js, &m); err != nil {
		return []string{}
	}
	v, ok := m["topics"]
	if !ok {
		return []string{}
	}
	return toStringSlice(v)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func chunkText(fileID uuid.UUID, text string, chunkSize int, overlap int) []*types.MaterialChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return []*types.MaterialChunk{}
	}
	if chunkSize < 200 {
		chunkSize = 200
	}
	if overlap < 0 {
		overlap = 0
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	out := []*types.MaterialChunk{}
	idx := 0
	now := time.Now()
	for start := 0; start < len(text); start += step {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		piece := strings.TrimSpace(text[start:end])
		if piece != "" {
			out = append(out, &types.MaterialChunk{
				ID:             uuid.New(),
				MaterialFileID: fileID,
				Index:          idx,
				Text:           piece,
				Metadata:       datatypes.JSON(mustJSON(map[string]any{"start": start, "end": end})),
				CreatedAt:      now,
				UpdatedAt:      now,
			})
			idx++
		}
		if end == len(text) {
			break
		}
	}
	return out
}

type chunkWithVec struct {
	Chunk *types.MaterialChunk
	Vec   []float32
}

func parseEmbedding(js datatypes.JSON) ([]float32, bool) {
	if len(js) == 0 {
		return nil, false
	}
	var v []float32
	if err := json.Unmarshal(js, &v); err != nil {
		var f64 []float64
		if err2 := json.Unmarshal(js, &f64); err2 != nil {
			return nil, false
		}
		v = make([]float32, len(f64))
		for i := range f64 {
			v[i] = float32(f64[i])
		}
	}
	if len(v) == 0 {
		return nil, false
	}
	return v, true
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return -1
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return -1
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func topKChunks(chunks []chunkWithVec, q []float32, k int) []chunkWithVec {
	type scored struct {
		c chunkWithVec
		s float64
	}
	arr := make([]scored, 0, len(chunks))
	for _, ch := range chunks {
		arr = append(arr, scored{c: ch, s: cosine(ch.Vec, q)})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	if k > len(arr) {
		k = len(arr)
	}
	out := make([]chunkWithVec, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, arr[i].c)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func toStringSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	a, ok := v.([]any)
	if !ok {
		if ss, ok2 := v.([]string); ok2 {
			return ss
		}
		return []string{}
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}

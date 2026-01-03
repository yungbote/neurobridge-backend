package steps

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func mustJSON(v any) datatypes.JSON {
	b, _ := json.Marshal(v)
	return datatypes.JSON(b)
}

func stringFromAny(v any) string {
	return strings.TrimSpace(fmt.Sprint(v))
}

func chunkMetadataKind(ch *types.MaterialChunk) string {
	if ch == nil || len(ch.Metadata) == 0 || strings.TrimSpace(string(ch.Metadata)) == "" || strings.TrimSpace(string(ch.Metadata)) == "null" {
		return ""
	}
	var meta map[string]any
	if err := json.Unmarshal(ch.Metadata, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(stringFromAny(meta["kind"]))
}

func isUnextractableChunk(ch *types.MaterialChunk) bool {
	if strings.EqualFold(chunkMetadataKind(ch), "unextractable") {
		return true
	}
	// Fallback for legacy rows missing metadata.kind.
	txt := strings.ToLower(strings.TrimSpace(ch.Text))
	return strings.HasPrefix(txt, "no extractable ")
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		out := make([]string, 0, len(ss))
		for _, s := range ss {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	arr, ok := v.([]any)
	if !ok {
		s := strings.TrimSpace(stringFromAny(v))
		if s == "" {
			return nil
		}
		return []string{s}
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s := strings.TrimSpace(stringFromAny(x))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func uuidSliceFromStrings(ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err == nil && id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func embeddingMissing(emb datatypes.JSON) bool {
	if len(emb) == 0 {
		return true
	}
	s := strings.TrimSpace(string(emb))
	return s == "" || s == "null" || s == "[]"
}

func decodeEmbedding(emb datatypes.JSON) ([]float32, bool) {
	if embeddingMissing(emb) {
		return nil, false
	}
	var out []float32
	if err := json.Unmarshal(emb, &out); err == nil && len(out) > 0 {
		return out, true
	}
	var tmp []float64
	if err := json.Unmarshal(emb, &tmp); err != nil || len(tmp) == 0 {
		return nil, false
	}
	out = make([]float32, 0, len(tmp))
	for _, f := range tmp {
		out = append(out, float32(f))
	}
	return out, true
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil || i <= 0 {
		return def
	}
	return i
}

func envIntAllowZero(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na <= 0 || nb <= 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func shorten(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func stratifiedChunkExcerpts(chunks []*types.MaterialChunk, perFile int, maxChars int) string {
	if perFile <= 0 {
		perFile = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}

	byFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		byFile[ch.MaterialFileID] = append(byFile[ch.MaterialFileID], ch)
	}

	fileIDs := make([]uuid.UUID, 0, len(byFile))
	for fid := range byFile {
		fileIDs = append(fileIDs, fid)
	}
	sort.Slice(fileIDs, func(i, j int) bool { return fileIDs[i].String() < fileIDs[j].String() })

	var b strings.Builder
	for _, fid := range fileIDs {
		arr := byFile[fid]
		sort.Slice(arr, func(i, j int) bool { return arr[i].Index < arr[j].Index })
		n := len(arr)
		if n == 0 {
			continue
		}
		k := perFile
		if k > n {
			k = n
		}
		step := float64(n) / float64(k)
		for i := 0; i < k; i++ {
			idx := int(float64(i) * step)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			ch := arr[idx]
			txt := shorten(ch.Text, maxChars)
			if txt == "" {
				continue
			}
			b.WriteString("[chunk_id=")
			b.WriteString(ch.ID.String())
			b.WriteString("] ")
			b.WriteString(txt)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

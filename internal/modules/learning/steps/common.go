package steps

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

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

func fallbackConceptKeysForNode(title, goal string, concepts []*types.Concept, max int) []string {
	if max <= 0 {
		max = 8
	}
	if max > 25 {
		max = 25
	}
	if len(concepts) == 0 {
		return nil
	}
	nodeText := normalizeForMatchString(title + " " + goal)
	if nodeText == "" {
		return nil
	}
	nodeTokens := tokenSetForMatch(nodeText)

	type scored struct {
		Key       string
		Score     int
		SortIndex int
	}
	scoredArr := make([]scored, 0, len(concepts))
	for _, c := range concepts {
		if c == nil {
			continue
		}
		key := strings.TrimSpace(c.Key)
		if key == "" {
			continue
		}
		keyText := normalizeForMatchString(strings.ReplaceAll(key, "_", " "))
		nameText := normalizeForMatchString(c.Name)

		score := 0
		if keyText != "" && strings.Contains(" "+nodeText+" ", " "+keyText+" ") {
			score += 3
		}
		if nameText != "" && strings.Contains(" "+nodeText+" ", " "+nameText+" ") {
			score += 6
		}

		for t := range tokenSetForMatch(keyText + " " + nameText) {
			if nodeTokens[t] {
				score++
			}
		}

		scoredArr = append(scoredArr, scored{Key: key, Score: score, SortIndex: c.SortIndex})
	}
	if len(scoredArr) == 0 {
		return nil
	}

	sort.Slice(scoredArr, func(i, j int) bool {
		if scoredArr[i].Score != scoredArr[j].Score {
			return scoredArr[i].Score > scoredArr[j].Score
		}
		if scoredArr[i].SortIndex != scoredArr[j].SortIndex {
			return scoredArr[i].SortIndex > scoredArr[j].SortIndex
		}
		return scoredArr[i].Key < scoredArr[j].Key
	})

	out := make([]string, 0, max)
	for _, s := range scoredArr {
		if len(out) >= max {
			break
		}
		if s.Score <= 0 {
			break
		}
		out = append(out, s.Key)
	}
	if len(out) > 0 {
		return dedupeStrings(out)
	}

	// No lexical matches; fall back to the most "important" concepts for the path.
	for _, s := range scoredArr {
		if len(out) >= max {
			break
		}
		out = append(out, s.Key)
	}
	return dedupeStrings(out)
}

func normalizeForMatchString(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	needSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			needSpace = true
			continue
		}
		if needSpace {
			b.WriteByte(' ')
			needSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func tokenSetForMatch(s string) map[string]bool {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	raw := strings.Fields(b.String())
	out := make(map[string]bool, len(raw))
	for _, tok := range raw {
		if len(tok) < 2 {
			continue
		}
		out[tok] = true
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

func stratifiedChunkExcerptsWithLimits(chunks []*types.MaterialChunk, perFile int, maxChars int, maxLines int, maxTotalChars int) string {
	txt, _ := stratifiedChunkExcerptsWithLimitsAndIDs(chunks, perFile, maxChars, maxLines, maxTotalChars)
	return txt
}

func stratifiedChunkExcerptsWithLimitsAndIDs(chunks []*types.MaterialChunk, perFile int, maxChars int, maxLines int, maxTotalChars int) (string, []uuid.UUID) {
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
	linesUsed := 0
	ids := make([]uuid.UUID, 0)

outer:
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
		if maxLines > 0 {
			remaining := maxLines - linesUsed
			if remaining <= 0 {
				break
			}
			if k > remaining {
				k = remaining
			}
		}
		if k <= 0 {
			break
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
			line := fmt.Sprintf("[chunk_id=%s] %s\n", ch.ID.String(), txt)
			if maxTotalChars > 0 && b.Len()+len(line) > maxTotalChars {
				break outer
			}
			b.WriteString(line)
			ids = append(ids, ch.ID)
			linesUsed++
			if maxLines > 0 && linesUsed >= maxLines {
				break outer
			}
		}
		b.WriteString("\n")
		if maxTotalChars > 0 && b.Len() >= maxTotalChars {
			break
		}
	}

	return strings.TrimSpace(b.String()), ids
}

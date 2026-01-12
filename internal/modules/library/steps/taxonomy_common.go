package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// We currently only run AI-driven taxonomy for the "topic" facet.
// Other "facets" like status/workflow/views are handled as stable, non-AI UI filters.
var defaultTaxonomyFacets = []string{"topic"}

func normalizeFacet(f string) string {
	return strings.ToLower(strings.TrimSpace(f))
}

func stableStrings(vals []string) []string {
	out := make([]string, 0, len(vals))
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(strings.TrimSpace(p)))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func parseFloat32ArrayJSON(raw []byte) ([]float32, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var tmp []float64
	if err := json.Unmarshal(raw, &tmp); err != nil || len(tmp) == 0 {
		return nil, false
	}
	out := make([]float32, 0, len(tmp))
	for _, f := range tmp {
		out = append(out, float32(f))
	}
	return out, true
}

func meanVector(vs [][]float32) ([]float32, bool) {
	if len(vs) == 0 {
		return nil, false
	}
	var dim int
	for _, v := range vs {
		if len(v) > 0 {
			dim = len(v)
			break
		}
	}
	if dim == 0 {
		return nil, false
	}
	sum := make([]float64, dim)
	n := 0
	for _, v := range vs {
		if len(v) != dim {
			continue
		}
		for i := 0; i < dim; i++ {
			sum[i] += float64(v[i])
		}
		n++
	}
	if n == 0 {
		return nil, false
	}
	out := make([]float32, dim)
	for i := 0; i < dim; i++ {
		out[i] = float32(sum[i] / float64(n))
	}
	return out, true
}

func cosineSimilarity(a, b []float32) float64 {
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

func toJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func titleCaseFacet(facet string) string {
	facet = normalizeFacet(facet)
	switch facet {
	case "topic":
		return "Topics"
	case "skill":
		return "Skills"
	case "context":
		return "Contexts"
	default:
		if facet == "" {
			return "Library"
		}
		return strings.ToUpper(facet[:1]) + facet[1:]
	}
}

func mustUUIDString(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty id")
	}
	// We keep ids as strings in prompts/JSON; validate they look like UUIDs.
	if len(s) < 32 {
		return "", fmt.Errorf("invalid id")
	}
	return s, nil
}

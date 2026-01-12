package keys

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// ChainEdge is a minimal portable edge for chain_key hashing
type ChainEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // prereq/related
}

// ChainKey computes a deterministic key from concept keys + edges.
// If edges are unknown, pass nil and the key becomes set-based (still deterministic).
func ChainKey(conceptKeys []string, edges []ChainEdge) string {
	keys := normalizeKeys(conceptKeys)
	sort.Strings(keys)
	// Normalize edges
	es := make([]ChainEdge, 0, len(edges))
	for _, e := range edges {
		f := strings.TrimSpace(strings.ToLower(e.From))
		t := strings.TrimSpace(strings.ToLower(e.To))
		ty := strings.TrimSpace(strings.ToLower(e.Type))
		if f == "" || t == "" {
			continue
		}
		if ty == "" {
			ty = "prereq"
		}
		es = append(es, ChainEdge{From: f, To: t, Type: ty})
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].From != es[j].From {
			return es[i].From < es[j].From
		}
		if es[i].To != es[j].To {
			return es[i].To < es[j].To
		}
		return es[i].Type < es[j].Type
	})
	payload := map[string]any{
		"concept_keys": keys,
		"edges":        es,
		"v":            1,
	}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:32]
}

// RepresentationKey computes a deterministic key from a representation tuple.
// representation should already be "small JSON" (no large content), e.g., modaility/variant/diagram_type/study_cycle etc.
func RepresentationKey(representation map[string]any) string {
	if representation == nil {
		representation = map[string]any{}
	}
	normalized := normalizeJSON(representation)
	b, _ := json.Marshal(normalized)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:32]
}

func normalizeKeys(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		k := strings.TrimSpace(strings.ToLower(s))
		if k == "" {
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func normalizeJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		ks := make([]string, 0, len(t))
		for k := range t {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		out := map[string]any{}
		for _, k := range ks {
			out[k] = normalizeJSON(t[k])
		}
		return out
	case []any:
		arr := make([]any, 0, len(t))
		for _, x := range t {
			arr = append(arr, normalizeJSON(x))
		}
		// Do not sort arbitrary arrays; only sort arrays of strings if clearly strings.
		if allStrings(arr) {
			ss := make([]string, 0, len(arr))
			for _, x := range arr {
				ss = append(ss, x.(string))
			}
			sort.Strings(ss)
			out := make([]any, 0, len(ss))
			for _, s := range ss {
				out = append(out, s)
			}
			return out
		}
		return arr
	case string:
		return strings.TrimSpace(t)
	default:
		return v
	}
}

func allStrings(a []any) bool {
	for _, x := range a {
		_, ok := x.(string)
		if !ok {
			return false
		}
	}
	return true
}

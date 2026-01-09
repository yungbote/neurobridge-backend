package content

import (
	"encoding/json"
	"sort"
	"strings"
)

// CitedChunkIDsFromNodeDocV1 returns unique cited chunk IDs (as strings) found in block citations.
// It is used for deterministic coverage accounting across generated docs.
func CitedChunkIDsFromNodeDocV1(doc NodeDocV1) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, b := range doc.Blocks {
		if b == nil {
			continue
		}
		arr, ok := b["citations"].([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		for _, x := range arr {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func CitedChunkIDsFromNodeDocJSON(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var doc NodeDocV1
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return CitedChunkIDsFromNodeDocV1(doc)
}

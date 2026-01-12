package content

import (
	"strings"

	"github.com/google/uuid"
)

// EnsureNodeDocBlockIDs assigns stable IDs to any blocks missing them.
// Returns the updated doc and whether any changes were made.
func EnsureNodeDocBlockIDs(doc NodeDocV1) (NodeDocV1, bool) {
	changed := false
	seen := map[string]bool{}

	for i := range doc.Blocks {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		id := strings.TrimSpace(stringFromAny(b["id"]))
		if id == "" || seen[id] {
			if t == "" {
				t = "block"
			}
			id = t + "_" + uuid.New().String()
			b["id"] = id
			doc.Blocks[i] = b
			changed = true
		}
		seen[id] = true
	}

	return doc, changed
}

package steps

import (
	"encoding/json"
	"strings"
)

type supportPointer struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	OccurredAt string  `json:"occurred_at,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

func loadSupportPointers(raw []byte) []supportPointer {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var out []supportPointer
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func addSupportPointer(list []supportPointer, ptr supportPointer, max int) ([]supportPointer, bool) {
	if ptr.SourceType == "" || ptr.SourceID == "" {
		return list, false
	}
	for _, it := range list {
		if it.SourceType == ptr.SourceType && it.SourceID == ptr.SourceID {
			return list, false
		}
	}
	list = append(list, ptr)
	if max > 0 && len(list) > max {
		list = list[len(list)-max:]
	}
	return list, true
}

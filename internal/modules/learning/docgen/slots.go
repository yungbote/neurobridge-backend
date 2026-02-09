package docgen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

// SlotFillsFromDoc computes slot fill evidence for a generated doc.
func SlotFillsFromDoc(doc content.NodeDocV1, blueprint DocBlueprintV1) []DocSlotFill {
	if len(blueprint.OptionalSlots) == 0 || len(doc.Blocks) == 0 {
		return nil
	}

	slotByID := map[string]DocOptionalSlot{}
	order := make([]string, 0, len(blueprint.OptionalSlots))
	for _, slot := range blueprint.OptionalSlots {
		id := normalizeSlotID(slot.SlotID)
		if id == "" {
			continue
		}
		if _, ok := slotByID[id]; ok {
			continue
		}
		slot.SlotID = id
		slot.AllowedBlockKinds = normalizeBlockKinds(slot.AllowedBlockKinds)
		slot.ConceptKeys = normalizeConceptKeys(slot.ConceptKeys)
		slotByID[id] = slot
		order = append(order, id)
	}
	if len(order) == 0 {
		return nil
	}

	fills := map[string]*DocSlotFill{}
	for _, id := range order {
		slot := slotByID[id]
		fills[id] = &DocSlotFill{
			SlotID:            id,
			Purpose:           strings.TrimSpace(slot.Purpose),
			MinBlocks:         slot.MinBlocks,
			MaxBlocks:         slot.MaxBlocks,
			AllowedBlockKinds: append([]string{}, slot.AllowedBlockKinds...),
			ConceptKeys:       append([]string{}, slot.ConceptKeys...),
		}
	}

	for _, block := range doc.Blocks {
		blockID := strings.TrimSpace(blockStringFromAny(block["id"]))
		if blockID == "" {
			continue
		}
		slotID := slotIDFromBlockID(blockID)
		if slotID == "" {
			continue
		}
		fill := fills[slotID]
		if fill == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(blockStringFromAny(block["type"])))
		fill.BlockIDs = append(fill.BlockIDs, blockID)
		fill.BlockKinds = append(fill.BlockKinds, kind)
		fill.FilledBlocks++
	}

	sort.Strings(order)
	out := make([]DocSlotFill, 0, len(order))
	for _, id := range order {
		if fill := fills[id]; fill != nil {
			out = append(out, *fill)
		}
	}
	return out
}

func slotIDFromBlockID(id string) string {
	raw := strings.TrimSpace(id)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "slot_") {
		return ""
	}
	if len(raw) <= len("slot_") {
		return ""
	}
	rest := raw[len("slot_"):]
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	if idx := strings.LastIndex(rest, "_"); idx > 0 && idx < len(rest)-1 {
		if _, err := strconv.Atoi(rest[idx+1:]); err == nil {
			rest = rest[:idx]
		}
	}
	return normalizeSlotID(rest)
}

func normalizeSlotID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.ReplaceAll(id, " ", "_")
	return strings.Trim(id, "_")
}

func normalizeBlockKinds(kinds []string) []string {
	if len(kinds) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk == "" || seen[kk] {
			continue
		}
		seen[kk] = true
		out = append(out, kk)
	}
	sort.Strings(out)
	return out
}

func normalizeConceptKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk == "" || seen[kk] {
			continue
		}
		seen[kk] = true
		out = append(out, kk)
	}
	sort.Strings(out)
	return out
}

func blockStringFromAny(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}

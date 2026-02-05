package content

import (
	"fmt"
	"strings"
)

// NodeDocGenOrderIssues validates that every block referenced in the order list exists
// and every generated block is referenced by order (no orphans).
func NodeDocGenOrderIssues(gen NodeDocGenV1) ([]string, map[string]any) {
	errs := make([]string, 0)
	metrics := map[string]any{}

	type idSet map[string]bool
	idsByKind := map[string]idSet{}
	allIDs := map[string]bool{}

	addID := func(kind, id string) {
		kind = strings.ToLower(strings.TrimSpace(kind))
		id = strings.TrimSpace(id)
		if kind == "" || id == "" {
			return
		}
		set := idsByKind[kind]
		if set == nil {
			set = idSet{}
			idsByKind[kind] = set
		}
		set[id] = true
		allIDs[id] = true
	}

	for _, b := range gen.Headings {
		addID("heading", b.ID)
	}
	for _, b := range gen.Paragraphs {
		addID("paragraph", b.ID)
	}
	for _, b := range gen.Callouts {
		addID("callout", b.ID)
	}
	for _, b := range gen.Codes {
		addID("code", b.ID)
	}
	for _, b := range gen.Figures {
		addID("figure", b.ID)
	}
	for _, b := range gen.Videos {
		addID("video", b.ID)
	}
	for _, b := range gen.Diagrams {
		addID("diagram", b.ID)
	}
	for _, b := range gen.Tables {
		addID("table", b.ID)
	}
	for _, b := range gen.Equations {
		addID("equation", b.ID)
	}
	for _, b := range gen.QuickChecks {
		addID("quick_check", b.ID)
	}
	for _, b := range gen.Flashcards {
		addID("flashcard", b.ID)
	}
	for _, b := range gen.Dividers {
		addID("divider", b.ID)
	}
	for _, b := range gen.Objectives {
		addID("objectives", b.ID)
	}
	for _, b := range gen.Prerequisites {
		addID("prerequisites", b.ID)
	}
	for _, b := range gen.KeyTakeaways {
		addID("key_takeaways", b.ID)
	}
	for _, b := range gen.Glossary {
		addID("glossary", b.ID)
	}
	for _, b := range gen.CommonMistakes {
		addID("common_mistakes", b.ID)
	}
	for _, b := range gen.Misconceptions {
		addID("misconceptions", b.ID)
	}
	for _, b := range gen.EdgeCases {
		addID("edge_cases", b.ID)
	}
	for _, b := range gen.Heuristics {
		addID("heuristics", b.ID)
	}
	for _, b := range gen.Steps {
		addID("steps", b.ID)
	}
	for _, b := range gen.Checklist {
		addID("checklist", b.ID)
	}
	for _, b := range gen.FAQ {
		addID("faq", b.ID)
	}
	for _, b := range gen.Intuition {
		addID("intuition", b.ID)
	}
	for _, b := range gen.MentalModel {
		addID("mental_model", b.ID)
	}
	for _, b := range gen.WhyItMatters {
		addID("why_it_matters", b.ID)
	}
	for _, b := range gen.Connections {
		addID("connections", b.ID)
	}

	seenOrder := map[string]bool{}
	refByKind := map[string]int{}
	orderIndex := map[string]int{}
	for idx, item := range gen.Order {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		id := strings.TrimSpace(item.ID)
		if kind == "" || id == "" {
			errs = append(errs, "order contains empty kind/id")
			continue
		}
		key := kind + ":" + id
		if seenOrder[key] {
			errs = append(errs, fmt.Sprintf("order contains duplicate reference %s", key))
			continue
		}
		seenOrder[key] = true
		if _, ok := orderIndex[id]; !ok {
			orderIndex[id] = idx
		}
		if idsByKind[kind] == nil || !idsByKind[kind][id] {
			errs = append(errs, fmt.Sprintf("order references missing block %s", key))
			continue
		}
		refByKind[kind]++
	}

	missing := 0
	for kind, set := range idsByKind {
		for id := range set {
			key := kind + ":" + id
			if !seenOrder[key] {
				errs = append(errs, fmt.Sprintf("order missing block %s", key))
				missing++
			}
		}
	}

	metrics["order_items"] = len(gen.Order)
	metrics["order_missing_blocks"] = missing
	metrics["order_ref_counts"] = refByKind

	// Validate trigger_after_block_ids references (best-effort, non-fatal).
	missingTriggers := 0
	lateTriggers := 0
	checkTriggers := func(kind string, id string, triggers []string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		parentIdx := -1
		if idx, ok := orderIndex[id]; ok {
			parentIdx = idx
		}
		for _, t := range triggers {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if !allIDs[t] {
				errs = append(errs, fmt.Sprintf("%s %s trigger_after_block_ids missing %s", kind, id, t))
				missingTriggers++
				continue
			}
			if parentIdx >= 0 {
				if trgIdx, ok := orderIndex[t]; ok && trgIdx > parentIdx {
					errs = append(errs, fmt.Sprintf("%s %s trigger_after_block_ids occurs after block %s", kind, id, t))
					lateTriggers++
				}
			}
		}
	}
	for _, qc := range gen.QuickChecks {
		checkTriggers("quick_check", qc.ID, qc.TriggerAfterBlockIDs)
	}
	for _, fc := range gen.Flashcards {
		checkTriggers("flashcard", fc.ID, fc.TriggerAfterBlockIDs)
	}
	if missingTriggers > 0 {
		metrics["trigger_missing_ids"] = missingTriggers
	}
	if lateTriggers > 0 {
		metrics["trigger_late_ids"] = lateTriggers
	}
	return errs, metrics
}

// RepairNodeDocGenOrder normalizes order references and fills missing block IDs.
// It ensures all blocks are referenced exactly once and moves obvious intro/outro
// blocks to more sensible positions (objectives/prereqs early, takeaways/refs late).
func RepairNodeDocGenOrder(gen NodeDocGenV1) (NodeDocGenV1, map[string]any) {
	metrics := map[string]any{}

	type idSeq struct {
		IDs []string
	}
	idsByKind := map[string]*idSeq{}
	ensureID := func(kind string, id *string, idx int, seen map[string]bool) bool {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind == "" || id == nil {
			return false
		}
		raw := strings.TrimSpace(*id)
		if raw != "" && !seen[raw] {
			seen[raw] = true
			return false
		}
		// Fill or de-dupe.
		prefix := kind
		switch kind {
		case "heading":
			prefix = "h"
		case "paragraph":
			prefix = "p"
		case "callout":
			prefix = "c"
		case "code":
			prefix = "code"
		case "figure":
			prefix = "fig"
		case "video":
			prefix = "vid"
		case "diagram":
			prefix = "diag"
		case "table":
			prefix = "tbl"
		case "equation":
			prefix = "eq"
		case "quick_check":
			prefix = "qc"
		case "flashcard":
			prefix = "fc"
		case "divider":
			prefix = "div"
		case "objectives":
			prefix = "obj"
		case "prerequisites":
			prefix = "pre"
		case "key_takeaways":
			prefix = "kt"
		case "glossary":
			prefix = "gl"
		case "common_mistakes":
			prefix = "cm"
		case "misconceptions":
			prefix = "mis"
		case "edge_cases":
			prefix = "ec"
		case "heuristics":
			prefix = "heu"
		case "steps":
			prefix = "st"
		case "checklist":
			prefix = "cl"
		case "faq":
			prefix = "faq"
		case "intuition":
			prefix = "int"
		case "mental_model":
			prefix = "mm"
		case "why_it_matters":
			prefix = "wim"
		case "connections":
			prefix = "con"
		}
		seq := idx + 1
		for {
			candidate := fmt.Sprintf("%s%d", prefix, seq)
			if !seen[candidate] {
				*id = candidate
				seen[candidate] = true
				return true
			}
			seq++
		}
	}

	record := func(kind string, id string) {
		kind = strings.ToLower(strings.TrimSpace(kind))
		id = strings.TrimSpace(id)
		if kind == "" || id == "" {
			return
		}
		seq := idsByKind[kind]
		if seq == nil {
			seq = &idSeq{}
			idsByKind[kind] = seq
		}
		seq.IDs = append(seq.IDs, id)
	}

	idsFilled := 0
	{
		seen := map[string]bool{}
		for i := range gen.Headings {
			if ensureID("heading", &gen.Headings[i].ID, i, seen) {
				idsFilled++
			}
			record("heading", gen.Headings[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Paragraphs {
			if ensureID("paragraph", &gen.Paragraphs[i].ID, i, seen) {
				idsFilled++
			}
			record("paragraph", gen.Paragraphs[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Callouts {
			if ensureID("callout", &gen.Callouts[i].ID, i, seen) {
				idsFilled++
			}
			record("callout", gen.Callouts[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Codes {
			if ensureID("code", &gen.Codes[i].ID, i, seen) {
				idsFilled++
			}
			record("code", gen.Codes[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Figures {
			if ensureID("figure", &gen.Figures[i].ID, i, seen) {
				idsFilled++
			}
			record("figure", gen.Figures[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Videos {
			if ensureID("video", &gen.Videos[i].ID, i, seen) {
				idsFilled++
			}
			record("video", gen.Videos[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Diagrams {
			if ensureID("diagram", &gen.Diagrams[i].ID, i, seen) {
				idsFilled++
			}
			record("diagram", gen.Diagrams[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Tables {
			if ensureID("table", &gen.Tables[i].ID, i, seen) {
				idsFilled++
			}
			record("table", gen.Tables[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Equations {
			if ensureID("equation", &gen.Equations[i].ID, i, seen) {
				idsFilled++
			}
			record("equation", gen.Equations[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.QuickChecks {
			if ensureID("quick_check", &gen.QuickChecks[i].ID, i, seen) {
				idsFilled++
			}
			record("quick_check", gen.QuickChecks[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Flashcards {
			if ensureID("flashcard", &gen.Flashcards[i].ID, i, seen) {
				idsFilled++
			}
			record("flashcard", gen.Flashcards[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Dividers {
			if ensureID("divider", &gen.Dividers[i].ID, i, seen) {
				idsFilled++
			}
			record("divider", gen.Dividers[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Objectives {
			if ensureID("objectives", &gen.Objectives[i].ID, i, seen) {
				idsFilled++
			}
			record("objectives", gen.Objectives[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Prerequisites {
			if ensureID("prerequisites", &gen.Prerequisites[i].ID, i, seen) {
				idsFilled++
			}
			record("prerequisites", gen.Prerequisites[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.KeyTakeaways {
			if ensureID("key_takeaways", &gen.KeyTakeaways[i].ID, i, seen) {
				idsFilled++
			}
			record("key_takeaways", gen.KeyTakeaways[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Glossary {
			if ensureID("glossary", &gen.Glossary[i].ID, i, seen) {
				idsFilled++
			}
			record("glossary", gen.Glossary[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.CommonMistakes {
			if ensureID("common_mistakes", &gen.CommonMistakes[i].ID, i, seen) {
				idsFilled++
			}
			record("common_mistakes", gen.CommonMistakes[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Misconceptions {
			if ensureID("misconceptions", &gen.Misconceptions[i].ID, i, seen) {
				idsFilled++
			}
			record("misconceptions", gen.Misconceptions[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.EdgeCases {
			if ensureID("edge_cases", &gen.EdgeCases[i].ID, i, seen) {
				idsFilled++
			}
			record("edge_cases", gen.EdgeCases[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Heuristics {
			if ensureID("heuristics", &gen.Heuristics[i].ID, i, seen) {
				idsFilled++
			}
			record("heuristics", gen.Heuristics[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Steps {
			if ensureID("steps", &gen.Steps[i].ID, i, seen) {
				idsFilled++
			}
			record("steps", gen.Steps[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Checklist {
			if ensureID("checklist", &gen.Checklist[i].ID, i, seen) {
				idsFilled++
			}
			record("checklist", gen.Checklist[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.FAQ {
			if ensureID("faq", &gen.FAQ[i].ID, i, seen) {
				idsFilled++
			}
			record("faq", gen.FAQ[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Intuition {
			if ensureID("intuition", &gen.Intuition[i].ID, i, seen) {
				idsFilled++
			}
			record("intuition", gen.Intuition[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.MentalModel {
			if ensureID("mental_model", &gen.MentalModel[i].ID, i, seen) {
				idsFilled++
			}
			record("mental_model", gen.MentalModel[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.WhyItMatters {
			if ensureID("why_it_matters", &gen.WhyItMatters[i].ID, i, seen) {
				idsFilled++
			}
			record("why_it_matters", gen.WhyItMatters[i].ID)
		}
	}
	{
		seen := map[string]bool{}
		for i := range gen.Connections {
			if ensureID("connections", &gen.Connections[i].ID, i, seen) {
				idsFilled++
			}
			record("connections", gen.Connections[i].ID)
		}
	}

	if idsFilled > 0 {
		metrics["ids_filled"] = idsFilled
	}

	exists := func(kind, id string) bool {
		seq := idsByKind[kind]
		if seq == nil {
			return false
		}
		for _, v := range seq.IDs {
			if v == id {
				return true
			}
		}
		return false
	}

	order := make([]NodeDocGenOrderItemV1, 0, len(gen.Order))
	seenOrder := map[string]bool{}
	invalidDropped := 0
	dupDropped := 0
	for _, item := range gen.Order {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		id := strings.TrimSpace(item.ID)
		if kind == "" || id == "" || !exists(kind, id) {
			invalidDropped++
			continue
		}
		key := kind + ":" + id
		if seenOrder[key] {
			dupDropped++
			continue
		}
		seenOrder[key] = true
		order = append(order, NodeDocGenOrderItemV1{Kind: kind, ID: id})
	}

	kindOrder := []string{
		"prerequisites", "objectives",
		"heading", "paragraph", "callout", "code", "figure", "video", "diagram", "table", "equation",
		"steps", "checklist", "quick_check", "flashcard", "intuition", "mental_model", "why_it_matters",
		"common_mistakes", "misconceptions", "edge_cases", "heuristics",
		"faq", "glossary", "key_takeaways", "connections", "divider",
	}
	missingAdded := 0
	for _, kind := range kindOrder {
		seq := idsByKind[kind]
		if seq == nil {
			continue
		}
		for _, id := range seq.IDs {
			key := kind + ":" + id
			if seenOrder[key] {
				continue
			}
			seenOrder[key] = true
			order = append(order, NodeDocGenOrderItemV1{Kind: kind, ID: id})
			missingAdded++
		}
	}

	startKinds := map[string]bool{
		"prerequisites": true,
		"objectives":    true,
	}
	endKinds := map[string]bool{
		"key_takeaways":   true,
		"glossary":        true,
		"faq":             true,
		"common_mistakes": true,
		"misconceptions":  true,
		"edge_cases":      true,
		"heuristics":      true,
		"checklist":       true,
		"connections":     true,
	}
	start := make([]NodeDocGenOrderItemV1, 0)
	middle := make([]NodeDocGenOrderItemV1, 0)
	end := make([]NodeDocGenOrderItemV1, 0)
	for _, item := range order {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		if startKinds[kind] {
			start = append(start, item)
			continue
		}
		if endKinds[kind] {
			end = append(end, item)
			continue
		}
		middle = append(middle, item)
	}
	reordered := false
	if len(start) > 0 || len(end) > 0 {
		reordered = true
		order = append(start, append(middle, end...)...)
	}

	if invalidDropped > 0 {
		metrics["order_invalid_dropped"] = invalidDropped
	}
	if dupDropped > 0 {
		metrics["order_duplicates_dropped"] = dupDropped
	}
	if missingAdded > 0 {
		metrics["order_missing_added"] = missingAdded
	}
	if reordered {
		metrics["order_grouped"] = true
	}

	gen.Order = order
	return gen, metrics
}

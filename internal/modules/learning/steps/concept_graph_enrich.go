package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type sectionNode struct {
	ID       string
	FileID   uuid.UUID
	FileName string
	Path     string
	Title    string
	Depth    int
	ChunkIDs []uuid.UUID
	Summary  string
	Emb      []float32
}

type sectionEdge struct {
	FromID string
	ToID   string
	Score  float64
}

func buildCrossDocSectionGraph(
	ctx context.Context,
	deps ConceptGraphBuildDeps,
	files []*types.MaterialFile,
	chunks []*types.MaterialChunk,
	embByChunk map[uuid.UUID][]float32,
) (string, []sectionNode) {
	if len(files) == 0 || len(chunks) == 0 {
		return "", nil
	}
	fileNameByID := map[uuid.UUID]string{}
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		fileNameByID[f.ID] = strings.TrimSpace(f.OriginalName)
	}

	nodes := map[string]*sectionNode{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		meta := chunkMetaMap(ch)
		sectionPath := strings.TrimSpace(stringFromAny(meta["section_path"]))
		sectionTitle := strings.TrimSpace(stringFromAny(meta["section_title"]))
		depth := intFromAny(meta["section_depth"], 0)
		if sectionPath == "" && sectionTitle == "" {
			sectionTitle = "Document"
		}
		key := fmt.Sprintf("%s|%s", ch.MaterialFileID.String(), sectionPath)
		node := nodes[key]
		if node == nil {
			node = &sectionNode{
				ID:       key,
				FileID:   ch.MaterialFileID,
				FileName: fileNameByID[ch.MaterialFileID],
				Path:     sectionPath,
				Title:    sectionTitle,
				Depth:    depth,
				ChunkIDs: []uuid.UUID{},
			}
			nodes[key] = node
		}
		node.ChunkIDs = append(node.ChunkIDs, ch.ID)
		if node.Title == "" && sectionTitle != "" {
			node.Title = sectionTitle
		}
		if node.Depth == 0 && depth > 0 {
			node.Depth = depth
		}
	}
	sections := make([]sectionNode, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || len(n.ChunkIDs) == 0 {
			continue
		}
		sections = append(sections, *n)
	}
	if len(sections) == 0 {
		return "", nil
	}

	maxSections := envIntAllowZero("CONCEPT_GRAPH_SECTION_MAX", 80)
	if maxSections <= 0 {
		maxSections = 80
	}
	if len(sections) > maxSections {
		sort.Slice(sections, func(i, j int) bool { return len(sections[i].ChunkIDs) > len(sections[j].ChunkIDs) })
		sections = sections[:maxSections]
	}

	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch != nil && ch.ID != uuid.Nil {
			chunkByID[ch.ID] = ch
		}
	}

	for i := range sections {
		ids := make([]uuid.UUID, 0, len(sections[i].ChunkIDs))
		seen := map[uuid.UUID]bool{}
		for _, id := range sections[i].ChunkIDs {
			if id != uuid.Nil && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(a, b int) bool {
			ai := chunkByID[ids[a]]
			bi := chunkByID[ids[b]]
			if ai == nil || bi == nil {
				return ids[a].String() < ids[b].String()
			}
			return ai.Index < bi.Index
		})
		var sb strings.Builder
		for j := 0; j < len(ids) && j < 3; j++ {
			ch := chunkByID[ids[j]]
			if ch == nil {
				continue
			}
			txt := shorten(ch.Text, 600)
			if txt == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(txt)
		}
		sections[i].Summary = strings.TrimSpace(sb.String())
	}

	// Embeddings: prefer chunk embeddings when available; otherwise embed summaries.
	for i := range sections {
		var embs [][]float32
		for _, id := range sections[i].ChunkIDs {
			if v := embByChunk[id]; len(v) > 0 {
				embs = append(embs, v)
			}
		}
		if len(embs) >= 3 {
			sections[i].Emb = averageEmbeddings(embs)
		}
	}

	pending := make([]int, 0)
	docs := make([]string, 0)
	for i := range sections {
		if len(sections[i].Emb) > 0 {
			continue
		}
		if strings.TrimSpace(sections[i].Summary) == "" {
			continue
		}
		pending = append(pending, i)
		docs = append(docs, sections[i].Summary)
	}
	if len(pending) > 0 && deps.AI != nil {
		embs, err := deps.AI.Embed(ctx, docs)
		if err == nil && len(embs) == len(pending) {
			for i := range pending {
				sections[pending[i]].Emb = embs[i]
			}
		}
	}

	minScore := envFloatAllowZero("CONCEPT_GRAPH_SECTION_MIN_SCORE", 0.78)
	topK := envIntAllowZero("CONCEPT_GRAPH_SECTION_TOPK", 3)
	if topK <= 0 {
		topK = 3
	}
	edges := make([]sectionEdge, 0)
	for i := range sections {
		if len(sections[i].Emb) == 0 {
			continue
		}
		type scored struct {
			ID    string
			Score float64
		}
		cands := make([]scored, 0)
		for j := range sections {
			if i == j {
				continue
			}
			if sections[i].FileID == sections[j].FileID {
				continue
			}
			if len(sections[j].Emb) == 0 {
				continue
			}
			score := cosineSim(sections[i].Emb, sections[j].Emb)
			if score >= minScore {
				cands = append(cands, scored{ID: sections[j].ID, Score: score})
			}
		}
		sort.Slice(cands, func(a, b int) bool { return cands[a].Score > cands[b].Score })
		if len(cands) > topK {
			cands = cands[:topK]
		}
		for _, c := range cands {
			edges = append(edges, sectionEdge{FromID: sections[i].ID, ToID: c.ID, Score: c.Score})
		}
	}

	if len(edges) == 0 {
		return "", sections
	}
	sectionsOut := make([]map[string]any, 0, len(sections))
	for _, s := range sections {
		if s.ID == "" {
			continue
		}
		sectionsOut = append(sectionsOut, map[string]any{
			"id":        s.ID,
			"file_id":   s.FileID.String(),
			"file_name": s.FileName,
			"path":      s.Path,
			"title":     s.Title,
			"summary":   shorten(s.Summary, 800),
		})
	}
	edgesOut := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		edgesOut = append(edgesOut, map[string]any{
			"from_id": e.FromID,
			"to_id":   e.ToID,
			"score":   e.Score,
		})
	}
	payload := map[string]any{
		"sections": sectionsOut,
		"edges":    edgesOut,
	}
	b, _ := json.Marshal(payload)
	if len(b) == 0 || string(b) == "null" {
		return "", sections
	}
	return string(b), sections
}

func chunkMetaMap(ch *types.MaterialChunk) map[string]any {
	if ch == nil || len(ch.Metadata) == 0 || string(ch.Metadata) == "null" {
		return map[string]any{}
	}
	var meta map[string]any
	if err := json.Unmarshal(ch.Metadata, &meta); err != nil {
		return map[string]any{}
	}
	if meta == nil {
		meta = map[string]any{}
	}
	return meta
}

func buildConceptGraphExcerpts(chunks []*types.MaterialChunk, perFile int, maxChars int, maxLines int, maxTotalChars int) (string, []uuid.UUID) {
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
		if strings.TrimSpace(ch.Text) == "" {
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
			line := buildEnrichedChunkLine(ch, maxChars)
			if line == "" {
				continue
			}
			if maxTotalChars > 0 && b.Len()+len(line) > maxTotalChars {
				break outer
			}
			b.WriteString(line)
			b.WriteString("\n")
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

func buildEnrichedChunkLine(ch *types.MaterialChunk, maxChars int) string {
	if ch == nil || ch.ID == uuid.Nil {
		return ""
	}
	txt := shorten(ch.Text, maxChars)
	if txt == "" {
		return ""
	}
	meta := chunkMetaMap(ch)
	section := strings.TrimSpace(stringFromAny(meta["section_path"]))
	if section != "" {
		txt = fmt.Sprintf("[section=%s] %s", section, txt)
	}
	if v := meta["formula_latex"]; v != nil {
		if arr := stringSliceFromAny(v); len(arr) > 0 {
			txt = txt + " | formulas: " + strings.Join(arr, "; ")
		}
	}
	if v := meta["formula_symbolic"]; v != nil {
		if arr := stringSliceFromAny(v); len(arr) > 0 {
			txt = txt + " | symbolic: " + strings.Join(arr, "; ")
		}
	}
	if v := meta["table_json"]; v != nil {
		if b, err := json.Marshal(v); err == nil && len(b) > 0 {
			txt = txt + " | table: " + shorten(string(b), 180)
		}
	}
	return fmt.Sprintf("[chunk_id=%s] %s", ch.ID.String(), txt)
}

var formulaCandidateRE = regexp.MustCompile(`(?i)([a-z][a-z0-9_]*\s*[=<>≈≤≥]\s*[^;]+)|([∑∫√πµλΔΩαβγ]|\\frac|\\sqrt|\\sum|\\int)`)

func detectFormulaCandidates(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0)
	seen := map[string]bool{}
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if len(l) > 240 {
			l = shorten(l, 240)
		}
		if !formulaCandidateRE.MatchString(l) {
			continue
		}
		if seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

type formulaExtraction struct {
	Items []formulaExtractionItem `json:"items"`
}

type formulaExtractionItem struct {
	ChunkID  string             `json:"chunk_id"`
	Formulas []formulaExtracted `json:"formulas"`
}

type formulaExtracted struct {
	Raw      string `json:"raw"`
	Latex    string `json:"latex"`
	Symbolic string `json:"symbolic"`
	Notes    string `json:"notes"`
}

func extractFormulasAndPersist(ctx context.Context, deps ConceptGraphBuildDeps, chunks []*types.MaterialChunk, allowedChunkIDs map[string]bool) {
	if deps.AI == nil || deps.Chunks == nil || len(chunks) == 0 {
		return
	}
	maxChunks := envIntAllowZero("FORMULA_EXTRACT_MAX_CHUNKS", 60)
	if maxChunks <= 0 {
		maxChunks = 60
	}
	candidates := make([]map[string]any, 0)
	for _, ch := range chunks {
		if len(candidates) >= maxChunks {
			break
		}
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[ch.ID.String()] {
			continue
		}
		cands := detectFormulaCandidates(ch.Text)
		if len(cands) == 0 {
			continue
		}
		candidates = append(candidates, map[string]any{
			"chunk_id":   ch.ID.String(),
			"candidates": cands,
			"excerpt":    shorten(ch.Text, 800),
		})
	}
	if len(candidates) == 0 {
		return
	}
	b, _ := json.Marshal(map[string]any{"items": candidates})
	prompt, err := prompts.Build(prompts.PromptFormulaExtraction, prompts.Input{FormulaCandidatesJSON: string(b)})
	if err != nil {
		return
	}
	obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
	if err != nil {
		return
	}
	raw, _ := json.Marshal(obj)
	var out formulaExtraction
	if err := json.Unmarshal(raw, &out); err != nil {
		return
	}
	if len(out.Items) == 0 {
		return
	}
	chunkByID := map[string]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID.String()] = ch
	}
	for _, item := range out.Items {
		ch := chunkByID[strings.TrimSpace(item.ChunkID)]
		if ch == nil || len(item.Formulas) == 0 {
			continue
		}
		meta := chunkMetaMap(ch)
		latex := make([]string, 0)
		symbolic := make([]string, 0)
		for _, f := range item.Formulas {
			if strings.TrimSpace(f.Latex) != "" {
				latex = append(latex, strings.TrimSpace(f.Latex))
			}
			if strings.TrimSpace(f.Symbolic) != "" {
				symbolic = append(symbolic, strings.TrimSpace(f.Symbolic))
			}
		}
		if len(latex) == 0 && len(symbolic) == 0 {
			continue
		}
		meta["formula_latex"] = dedupeStrings(latex)
		meta["formula_symbolic"] = dedupeStrings(symbolic)
		metaJSON := datatypes.JSON(mustJSON(meta))
		_ = deps.Chunks.UpdateFields(dbctx.Context{Ctx: ctx}, ch.ID, map[string]interface{}{
			"metadata": metaJSON,
		})
		ch.Metadata = metaJSON
	}
}

type assumedKnowledge struct {
	Assumed []assumedKnowledgeItem `json:"assumed_concepts"`
	Notes   string                 `json:"notes"`
}

type assumedKnowledgeItem struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	Aliases    []string `json:"aliases"`
	Importance int      `json:"importance"`
	Citations  []string `json:"citations"`
	RequiredBy []string `json:"required_by"`
}

type conceptAlignment struct {
	Aliases []conceptAlias `json:"aliases"`
	Splits  []conceptSplit `json:"splits"`
}

type conceptAlias struct {
	CanonicalKey string   `json:"canonical_key"`
	AliasKeys    []string `json:"alias_keys"`
	Rationale    string   `json:"rationale"`
}

type conceptSplit struct {
	AmbiguousKey string           `json:"ambiguous_key"`
	Meanings     []conceptMeaning `json:"meanings"`
}

type conceptMeaning struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Aliases   []string `json:"aliases"`
	Citations []string `json:"citations"`
	Rationale string   `json:"rationale"`
}

func parseAssumedKnowledge(obj map[string]any) assumedKnowledge {
	if obj == nil {
		return assumedKnowledge{}
	}
	raw, _ := json.Marshal(obj)
	var out assumedKnowledge
	_ = json.Unmarshal(raw, &out)
	return out
}

func parseConceptAlignment(obj map[string]any) conceptAlignment {
	if obj == nil {
		return conceptAlignment{}
	}
	raw, _ := json.Marshal(obj)
	var out conceptAlignment
	_ = json.Unmarshal(raw, &out)
	return out
}

func applyConceptAlignment(
	concepts []conceptInvItem,
	alignment conceptAlignment,
	allowedChunkIDs map[string]bool,
	conceptMetaByKey map[string]map[string]any,
) []conceptInvItem {
	if len(concepts) == 0 {
		return concepts
	}
	byKey := map[string]conceptInvItem{}
	for _, c := range concepts {
		byKey[c.Key] = c
	}

	mergedFrom := map[string][]string{}
	for _, a := range alignment.Aliases {
		canon := normalizeConceptKey(strings.TrimSpace(a.CanonicalKey))
		if canon == "" {
			continue
		}
		base, ok := byKey[canon]
		if !ok {
			continue
		}
		for _, ak := range a.AliasKeys {
			ak = normalizeConceptKey(strings.TrimSpace(ak))
			if ak == "" || ak == canon {
				continue
			}
			if alias, ok := byKey[ak]; ok {
				base.Aliases = dedupeStrings(append(base.Aliases, alias.Aliases...))
				base.Aliases = dedupeStrings(append(base.Aliases, alias.Key, alias.Name))
				base.KeyPoints = dedupeStrings(append(base.KeyPoints, alias.KeyPoints...))
				base.Citations = dedupeStrings(append(base.Citations, alias.Citations...))
				if alias.Importance > base.Importance {
					base.Importance = alias.Importance
				}
				delete(byKey, ak)
				if conceptMetaByKey != nil {
					conceptMetaByKey[canon] = mergeConceptMeta(conceptMetaByKey[canon], conceptMetaByKey[ak])
					delete(conceptMetaByKey, ak)
				}
				mergedFrom[canon] = append(mergedFrom[canon], ak)
			}
		}
		byKey[canon] = base
	}
	for k, v := range mergedFrom {
		meta := conceptMetaByKey[k]
		if meta == nil {
			meta = map[string]any{}
		}
		meta["merged_from"] = dedupeStrings(v)
		conceptMetaByKey[k] = meta
	}

	for _, sp := range alignment.Splits {
		root := normalizeConceptKey(strings.TrimSpace(sp.AmbiguousKey))
		if root == "" {
			continue
		}
		orig, ok := byKey[root]
		if !ok {
			continue
		}
		delete(byKey, root)
		for i, m := range sp.Meanings {
			key := normalizeConceptKey(strings.TrimSpace(m.Key))
			if key == "" {
				key = fmt.Sprintf("%s_variant_%d", root, i+1)
			}
			if _, exists := byKey[key]; exists {
				key = fmt.Sprintf("%s_variant_%d", key, i+1)
			}
			newItem := conceptInvItem{
				Key:        key,
				Name:       strings.TrimSpace(m.Name),
				ParentKey:  orig.ParentKey,
				Depth:      orig.Depth,
				Summary:    strings.TrimSpace(m.Summary),
				KeyPoints:  nil,
				Aliases:    dedupeStrings(m.Aliases),
				Importance: orig.Importance,
				Citations:  dedupeStrings(filterChunkIDStrings(m.Citations, allowedChunkIDs)),
			}
			byKey[key] = newItem
			meta := conceptMetaByKey[key]
			if meta == nil {
				meta = map[string]any{}
			}
			meta["split_from"] = root
			meta["split_rationale"] = strings.TrimSpace(m.Rationale)
			conceptMetaByKey[key] = meta
		}
	}

	out := make([]conceptInvItem, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func averageEmbeddings(embs [][]float32) []float32 {
	if len(embs) == 0 {
		return nil
	}
	maxLen := 0
	for _, e := range embs {
		if len(e) > maxLen {
			maxLen = len(e)
		}
	}
	if maxLen == 0 {
		return nil
	}
	out := make([]float32, maxLen)
	counts := make([]float32, maxLen)
	for _, e := range embs {
		for i := 0; i < len(e) && i < maxLen; i++ {
			out[i] += e[i]
			counts[i]++
		}
	}
	for i := range out {
		if counts[i] > 0 {
			out[i] = out[i] / counts[i]
		}
	}
	return out
}

func mergeConceptMeta(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	if src == nil {
		return dst
	}
	if boolFromAny(src["assumed"]) {
		dst["assumed"] = true
	}
	if v := stringSliceFromAny(src["required_by"]); len(v) > 0 {
		dst["required_by"] = dedupeStrings(append(stringSliceFromAny(dst["required_by"]), v...))
	}
	if v := stringSliceFromAny(src["merged_from"]); len(v) > 0 {
		dst["merged_from"] = dedupeStrings(append(stringSliceFromAny(dst["merged_from"]), v...))
	}
	if v := stringSliceFromAny(src["split_from"]); len(v) > 0 {
		dst["split_from"] = dedupeStrings(append(stringSliceFromAny(dst["split_from"]), v...))
	}
	if v := strings.TrimSpace(stringFromAny(src["split_rationale"])); v != "" && strings.TrimSpace(stringFromAny(dst["split_rationale"])) == "" {
		dst["split_rationale"] = v
	}
	if v := strings.TrimSpace(stringFromAny(src["assumed_notes"])); v != "" && strings.TrimSpace(stringFromAny(dst["assumed_notes"])) == "" {
		dst["assumed_notes"] = v
	}
	return dst
}

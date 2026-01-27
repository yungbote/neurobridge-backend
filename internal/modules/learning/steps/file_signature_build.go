package steps

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type FileSignatureBuildDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Files        repos.MaterialFileRepo
	FileSigs     repos.MaterialFileSignatureRepo
	FileSections repos.MaterialFileSectionRepo
	Chunks       repos.MaterialChunkRepo
	AI           openai.Client
	Vec          pc.VectorStore
	Saga         services.SagaService
	Bootstrap    services.LearningBuildBootstrapService
}

type FileSignatureBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type FileSignatureBuildOutput struct {
	PathID             uuid.UUID `json:"path_id"`
	FilesTotal         int       `json:"files_total"`
	FilesProcessed     int       `json:"files_processed"`
	SignaturesUpserted int       `json:"signatures_upserted"`
	SectionsUpserted   int       `json:"sections_upserted"`
	SignaturesSkipped  int       `json:"signatures_skipped"`
	IntentsUpserted    int       `json:"intents_upserted"`
	IntentsSkipped     int       `json:"intents_skipped"`
}

func FileSignatureBuild(ctx context.Context, deps FileSignatureBuildDeps, in FileSignatureBuildInput) (FileSignatureBuildOutput, error) {
	out := FileSignatureBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.FileSigs == nil || deps.FileSections == nil || deps.Chunks == nil || deps.AI == nil || deps.Saga == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("file_signature_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("file_signature_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("file_signature_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("file_signature_build: missing saga_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.FilesTotal = len(files)
	if len(files) == 0 {
		return out, fmt.Errorf("file_signature_build: no files for set")
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}

	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	chunksByFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		chunksByFile[ch.MaterialFileID] = append(chunksByFile[ch.MaterialFileID], ch)
	}

	existing, err := deps.FileSigs.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	existingByFile := map[uuid.UUID]*types.MaterialFileSignature{}
	for _, row := range existing {
		if row == nil || row.MaterialFileID == uuid.Nil {
			continue
		}
		existingByFile[row.MaterialFileID] = row
	}
	existingIntents := map[uuid.UUID]*types.MaterialIntent{}
	var intentRows []*types.MaterialIntent
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialIntent{}).Where("material_file_id IN ?", fileIDs).Find(&intentRows).Error; err == nil {
		for _, row := range intentRows {
			if row == nil || row.MaterialFileID == uuid.Nil {
				continue
			}
			existingIntents[row.MaterialFileID] = row
		}
	}

	perFile := envIntAllowZero("FILE_SIGNATURE_EXCERPTS_PER_FILE", 10)
	if perFile <= 0 {
		perFile = 10
	}
	maxChars := envIntAllowZero("FILE_SIGNATURE_EXCERPT_MAX_CHARS", 800)
	if maxChars <= 0 {
		maxChars = 800
	}
	maxTotal := envIntAllowZero("FILE_SIGNATURE_EXCERPT_MAX_TOTAL_CHARS", 12000)
	if maxTotal <= 0 {
		maxTotal = 12000
	}
	maxSections := envIntAllowZero("FILE_SIGNATURE_MAX_SECTIONS", 60)
	if maxSections <= 0 {
		maxSections = 60
	}
	minTextChars := envIntAllowZero("FILE_SIGNATURE_MIN_TEXT_CHARS", 500)
	if minTextChars < 0 {
		minTextChars = 0
	}
	sectionEmbedBatch := envIntAllowZero("FILE_SIGNATURE_SECTION_EMBED_BATCH_SIZE", 64)
	if sectionEmbedBatch <= 0 {
		sectionEmbedBatch = 64
	}
	sectionEmbedConcurrency := envIntAllowZero("FILE_SIGNATURE_SECTION_EMBED_CONCURRENCY", 2)
	if sectionEmbedConcurrency <= 0 {
		sectionEmbedConcurrency = 1
	}

	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		chArr := chunksByFile[f.ID]
		if len(chArr) == 0 {
			continue
		}

		fingerprint := fileFingerprint(f, chArr)
		if row := existingByFile[f.ID]; row != nil && strings.TrimSpace(row.Fingerprint) == fingerprint && row.Version >= 2 {
			if existingIntents[f.ID] != nil {
				out.SignaturesSkipped++
				out.IntentsSkipped++
				continue
			}
		}

		excerpt := stratifiedChunkExcerptsWithLimits(chArr, perFile, maxChars, 0, maxTotal)
		if strings.TrimSpace(excerpt) == "" {
			continue
		}

		outlineHint := buildOutlineHint(f, chArr, maxSections)
		fileInfo := map[string]any{
			"file_id":        f.ID.String(),
			"original_name":  strings.TrimSpace(f.OriginalName),
			"mime_type":      strings.TrimSpace(f.MimeType),
			"size_bytes":     f.SizeBytes,
			"extracted_kind": strings.TrimSpace(f.ExtractedKind),
		}

		fileInfoJSON, _ := json.Marshal(fileInfo)
		outlineHintJSON, _ := json.Marshal(outlineHint)

		p, err := prompts.Build(prompts.PromptFileSignatureBuild, prompts.Input{
			Excerpts:        excerpt,
			FileInfoJSON:    string(fileInfoJSON),
			OutlineHintJSON: string(outlineHintJSON),
		})
		if err != nil {
			return out, err
		}

		obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
		if err != nil {
			return out, err
		}

		summary := strings.TrimSpace(stringFromAny(obj["summary_md"]))
		topics := dedupeStrings(stringSliceFromAny(obj["topics"]))
		conceptKeys := dedupeStrings(stringSliceFromAny(obj["concept_keys"]))
		difficulty := strings.TrimSpace(stringFromAny(obj["difficulty"]))
		domainTags := dedupeStrings(stringSliceFromAny(obj["domain_tags"]))
		citations := dedupeStrings(append(stringSliceFromAny(obj["citations"]), extractCitations(excerpt)...))
		outlineJSON := mapFromAny(obj["outline_json"])
		outlineConf := floatFromAny(obj["outline_confidence"], 0.4)
		lang := strings.TrimSpace(stringFromAny(obj["language"]))

		fromState := strings.TrimSpace(stringFromAny(obj["from_state"]))
		toState := strings.TrimSpace(stringFromAny(obj["to_state"]))
		coreThread := strings.TrimSpace(stringFromAny(obj["core_thread"]))
		destination := dedupeStrings(stringSliceFromAny(obj["destination_concepts"]))
		prereq := dedupeStrings(stringSliceFromAny(obj["prerequisite_concepts"]))
		assumed := dedupeStrings(stringSliceFromAny(obj["assumed_knowledge"]))
		intentNotes := dedupeStrings(stringSliceFromAny(obj["notes"]))

		quality := buildQualitySignals(chArr, excerpt, minTextChars)
		if q := mapFromAny(obj["quality"]); q != nil {
			quality["llm_quality"] = q
		}

		embDoc := summary
		if embDoc == "" {
			embDoc = strings.TrimSpace(strings.Join(topics, " "))
		}
		var summaryEmb []float32
		if strings.TrimSpace(embDoc) != "" {
			if vecs, err := deps.AI.Embed(ctx, []string{embDoc}); err == nil && len(vecs) > 0 {
				summaryEmb = vecs[0]
			}
		}

		now := time.Now().UTC()
		row := &types.MaterialFileSignature{
			ID:                uuid.New(),
			MaterialFileID:    f.ID,
			MaterialSetID:     in.MaterialSetID,
			Version:           2,
			Language:          lang,
			Quality:           datatypes.JSON(mustJSON(quality)),
			Difficulty:        difficulty,
			DomainTags:        datatypes.JSON(mustJSON(domainTags)),
			Topics:            datatypes.JSON(mustJSON(topics)),
			ConceptKeys:       datatypes.JSON(mustJSON(conceptKeys)),
			SummaryMD:         summary,
			SummaryEmbedding:  datatypes.JSON(mustJSON(summaryEmb)),
			OutlineJSON:       datatypes.JSON(mustJSON(outlineJSON)),
			OutlineConfidence: outlineConf,
			Citations:         datatypes.JSON(mustJSON(citations)),
			Fingerprint:       fingerprint,
			CreatedAt:         now,
			UpdatedAt:         now,
		}

		intent := &types.MaterialIntent{
			ID:                   uuid.New(),
			MaterialFileID:       f.ID,
			MaterialSetID:        in.MaterialSetID,
			FromState:            fromState,
			ToState:              toState,
			CoreThread:           coreThread,
			DestinationConcepts:  datatypes.JSON(mustJSON(destination)),
			PrerequisiteConcepts: datatypes.JSON(mustJSON(prereq)),
			AssumedKnowledge:     datatypes.JSON(mustJSON(assumed)),
			Metadata:             datatypes.JSON(mustJSON(map[string]any{"notes": intentNotes, "source": "file_signature_build"})),
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		if strings.TrimSpace(intent.FromState) == "" && strings.TrimSpace(intent.ToState) == "" &&
			strings.TrimSpace(intent.CoreThread) == "" && len(destination) == 0 && len(prereq) == 0 && len(assumed) == 0 {
			intent = fallbackMaterialIntent(f, row)
			intent.MaterialSetID = in.MaterialSetID
			intent.Metadata = datatypes.JSON(mustJSON(map[string]any{"notes": append(intentNotes, "fallback_intent"), "source": "file_signature_build"}))
		}

		if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			dbc := dbctx.Context{Ctx: ctx, Tx: tx}
			if err := deps.FileSigs.UpsertByMaterialFileID(dbc, row); err != nil {
				return err
			}
			if intent != nil {
				if err := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "material_file_id"}},
					DoUpdates: clause.AssignmentColumns([]string{
						"material_set_id",
						"from_state",
						"to_state",
						"core_thread",
						"destination_concepts",
						"prerequisite_concepts",
						"assumed_knowledge",
						"metadata",
						"updated_at",
					}),
				}).Create(intent).Error; err != nil {
					return err
				}
				out.IntentsUpserted++
			}

			sections := flattenOutlineSections(outlineJSON, maxSections)
			for i := range sections {
				sections[i].MaterialFileID = f.ID
				sections[i].SectionIndex = i + 1
				sections[i].CreatedAt = now
				sections[i].UpdatedAt = now
			}

			if len(sections) > 0 {
				if err := deps.FileSections.DeleteByMaterialFileID(dbc, f.ID); err != nil {
					return err
				}
				if err := attachSectionEmbeddings(ctx, deps.AI, sections, sectionEmbedBatch, sectionEmbedConcurrency); err != nil {
					return err
				}
				if err := deps.FileSections.BulkUpsert(dbc, sections); err != nil {
					return err
				}
				out.SectionsUpserted += len(sections)
			}
			return nil
		}); err != nil {
			return out, err
		}

		if deps.Vec != nil && len(summaryEmb) > 0 {
			ns := fmt.Sprintf("file_signatures:material_set:%s", in.MaterialSetID.String())
			_ = deps.Vec.Upsert(ctx, ns, []pc.Vector{{
				ID:     f.ID.String(),
				Values: summaryEmb,
				Metadata: map[string]any{
					"material_set_id": in.MaterialSetID.String(),
					"file_id":         f.ID.String(),
					"topics":          topics,
					"difficulty":      difficulty,
					"language":        lang,
				},
			}})
		}

		out.FilesProcessed++
		out.SignaturesUpserted++
	}

	return out, nil
}

func fileFingerprint(f *types.MaterialFile, chunks []*types.MaterialChunk) string {
	h := sha1.New()
	if f != nil {
		_, _ = h.Write([]byte(strings.TrimSpace(f.OriginalName)))
		_, _ = h.Write([]byte(fmt.Sprintf("|%d|%s", f.SizeBytes, strings.TrimSpace(f.MimeType))))
	}
	ids := make([]string, 0, len(chunks))
	totalChars := 0
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		ids = append(ids, ch.ID.String())
		totalChars += len(ch.Text)
	}
	sort.Strings(ids)
	for _, id := range ids {
		_, _ = h.Write([]byte("|" + id))
	}
	_, _ = h.Write([]byte(fmt.Sprintf("|len:%d", totalChars)))
	return hex.EncodeToString(h.Sum(nil))
}

func buildQualitySignals(chunks []*types.MaterialChunk, excerpt string, minTextChars int) map[string]any {
	totalChars := 0
	alpha := 0
	tableChunks := 0
	ocrChunks := 0
	transcriptChunks := 0
	noise := 0

	for _, ch := range chunks {
		if ch == nil {
			continue
		}
		txt := ch.Text
		totalChars += len(txt)
		for _, r := range txt {
			if unicode.IsLetter(r) {
				alpha++
			} else if unicode.IsSymbol(r) || unicode.IsPunct(r) {
				noise++
			}
		}
		if strings.Contains(string(ch.Metadata), `"kind":"table_text"`) {
			tableChunks++
		}
		if strings.Contains(string(ch.Metadata), `"kind":"ocr_text"`) {
			ocrChunks++
		}
		if strings.Contains(string(ch.Metadata), `"kind":"transcript"`) {
			transcriptChunks++
		}
	}

	alphaRatio := 0.0
	if totalChars > 0 {
		alphaRatio = float64(alpha) / float64(totalChars)
	}
	noiseRatio := 0.0
	if totalChars > 0 {
		noiseRatio = float64(noise) / float64(totalChars)
	}

	coverage := 0.5
	if len(strings.TrimSpace(excerpt)) > 0 && totalChars > 0 {
		coverage = clamp01(float64(len(excerpt)) / float64(totalChars))
	}

	lowSignal := false
	if minTextChars > 0 && totalChars > 0 && totalChars < minTextChars {
		lowSignal = true
	}

	return map[string]any{
		"text_chars":        totalChars,
		"alpha_ratio":       alphaRatio,
		"noise_ratio":       noiseRatio,
		"chunk_count":       len(chunks),
		"table_chunks":      tableChunks,
		"ocr_chunks":        ocrChunks,
		"transcript_chunks": transcriptChunks,
		"coverage":          coverage,
		"low_text_signal":   lowSignal,
	}
}

func buildOutlineHint(f *types.MaterialFile, chunks []*types.MaterialChunk, maxSections int) map[string]any {
	if f != nil {
		if hint := outlineHintFromDiagnostics(f.ExtractionDiagnostics, maxSections); hint != nil {
			return hint
		}
	}
	if f == nil {
		return nil
	}
	title := strings.TrimSpace(f.OriginalName)
	if title == "" {
		title = "Document"
	}
	headings := extractHeadingCandidates(chunks, maxSections)
	sections := make([]map[string]any, 0, len(headings))
	for i, h := range headings {
		sections = append(sections, map[string]any{
			"title":      h.Title,
			"path":       fmt.Sprintf("%d", i+1),
			"start_page": h.StartPage,
			"end_page":   h.EndPage,
			"start_sec":  h.StartSec,
			"end_sec":    h.EndSec,
			"children":   []map[string]any{},
		})
		if len(sections) >= maxSections {
			break
		}
	}
	return map[string]any{
		"title":    title,
		"sections": sections,
	}
}

func outlineHintFromDiagnostics(raw datatypes.JSON, maxSections int) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var diag map[string]any
	if err := json.Unmarshal(raw, &diag); err != nil {
		return nil
	}
	hint := mapFromAny(diag["outline_hint"])
	if hint == nil {
		return nil
	}
	sections := sliceAny(hint["sections"])
	if len(sections) == 0 {
		return nil
	}
	if maxSections > 0 && len(sections) > maxSections {
		sections = sections[:maxSections]
	}
	out := map[string]any{
		"title":    strings.TrimSpace(stringFromAny(hint["title"])),
		"sections": sections,
	}
	if v := strings.TrimSpace(stringFromAny(hint["source"])); v != "" {
		out["source"] = v
	}
	if v := floatFromAny(hint["confidence"], 0); v > 0 {
		out["confidence"] = v
	}
	return out
}

type headingCandidate struct {
	Title     string
	StartPage *int
	EndPage   *int
	StartSec  *float64
	EndSec    *float64
}

func extractHeadingCandidates(chunks []*types.MaterialChunk, maxSections int) []headingCandidate {
	if len(chunks) == 0 || maxSections <= 0 {
		return nil
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })
	seen := map[string]bool{}
	out := make([]headingCandidate, 0, maxSections)

	for _, ch := range chunks {
		if ch == nil || strings.TrimSpace(ch.Text) == "" {
			continue
		}
		lines := strings.Split(ch.Text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !looksLikeHeading(line) {
				continue
			}
			key := strings.ToLower(line)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, headingCandidate{
				Title:     line,
				StartPage: ch.Page,
				EndPage:   ch.Page,
				StartSec:  ch.StartSec,
				EndSec:    ch.EndSec,
			})
			if len(out) >= maxSections {
				return out
			}
		}
	}
	return out
}

func looksLikeHeading(line string) bool {
	if line == "" {
		return false
	}
	if len(line) < 4 || len(line) > 80 {
		return false
	}
	if strings.HasSuffix(line, ".") && len(line) < 10 {
		return false
	}
	upper := 0
	letters := 0
	for _, r := range line {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters == 0 {
		return false
	}
	if upper == letters {
		return true
	}
	if upper > 0 && float64(upper)/float64(letters) >= 0.6 {
		return true
	}
	if headingNumPrefix.MatchString(line) {
		return true
	}
	return false
}

var headingNumPrefix = regexp.MustCompile(`^(\d+(\.\d+)*|[IVX]+)\s+`)

func flattenOutlineSections(outline map[string]any, maxSections int) []*types.MaterialFileSection {
	if outline == nil || maxSections <= 0 {
		return nil
	}
	sectionsAny, ok := outline["sections"].([]any)
	if !ok || len(sectionsAny) == 0 {
		return nil
	}

	out := make([]*types.MaterialFileSection, 0, maxSections)
	for _, it := range sectionsAny {
		if len(out) >= maxSections {
			break
		}
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		path := strings.TrimSpace(stringFromAny(m["path"]))
		sp := intPtrFromAny(m["start_page"])
		ep := intPtrFromAny(m["end_page"])
		ss := floatPtrFromAny(m["start_sec"])
		es := floatPtrFromAny(m["end_sec"])

		out = append(out, &types.MaterialFileSection{
			Title:       title,
			Path:        path,
			StartPage:   sp,
			EndPage:     ep,
			StartSec:    ss,
			EndSec:      es,
			TextExcerpt: title,
			Metadata:    datatypes.JSON(mustJSON(map[string]any{"source": "outline"})),
		})
	}
	return out
}

func attachSectionEmbeddings(ctx context.Context, ai openai.Client, sections []*types.MaterialFileSection, batchSize int, concurrency int) error {
	if ai == nil || len(sections) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(sections)
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	type batch struct {
		start int
		items []*types.MaterialFileSection
	}
	batches := make([]batch, 0, (len(sections)+batchSize-1)/batchSize)
	for i := 0; i < len(sections); i += batchSize {
		end := i + batchSize
		if end > len(sections) {
			end = len(sections)
		}
		batches = append(batches, batch{start: i, items: sections[i:end]})
	}

	embedBatch := func(items []*types.MaterialFileSection) ([][]float32, error) {
		texts := make([]string, 0, len(items))
		for _, s := range items {
			if s == nil {
				texts = append(texts, "")
				continue
			}
			doc := strings.TrimSpace(s.TextExcerpt)
			if doc == "" {
				doc = strings.TrimSpace(s.Title)
			}
			texts = append(texts, doc)
		}
		return ai.Embed(ctx, texts)
	}

	if concurrency == 1 || len(batches) == 1 {
		for _, b := range batches {
			embs, err := embedBatch(b.items)
			if err != nil {
				return err
			}
			if len(embs) != len(b.items) {
				return fmt.Errorf("file_signature_build: section embedding count mismatch")
			}
			for i, s := range b.items {
				if s == nil {
					continue
				}
				s.Embedding = datatypes.JSON(mustJSON(embs[i]))
			}
		}
		return nil
	}

	jobs := make(chan batch)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	worker := func() {
		defer wg.Done()
		for b := range jobs {
			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				continue
			}
			mu.Unlock()
			embs, err := embedBatch(b.items)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				continue
			}
			if len(embs) != len(b.items) {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("file_signature_build: section embedding count mismatch")
				}
				mu.Unlock()
				continue
			}
			for i, s := range b.items {
				if s == nil {
					continue
				}
				s.Embedding = datatypes.JSON(mustJSON(embs[i]))
			}
		}
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker()
	}
	for _, b := range batches {
		jobs <- b
	}
	close(jobs)
	wg.Wait()

	return firstErr
}

func extractCitations(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	out := []string{}
	for _, m := range urlRegex.FindAllString(text, -1) {
		out = append(out, m)
	}
	for _, m := range doiRegex.FindAllString(text, -1) {
		out = append(out, m)
	}
	for _, m := range rfcRegex.FindAllString(text, -1) {
		out = append(out, m)
	}
	return dedupeStrings(out)
}

var (
	urlRegex = regexp.MustCompile(`https?://[^\s\)\]]+`)
	doiRegex = regexp.MustCompile(`\b10\.\d{4,9}/[^\s]+`)
	rfcRegex = regexp.MustCompile(`\bRFC\s?\d{3,5}\b`)
)

func intPtrFromAny(v any) *int {
	switch x := v.(type) {
	case int:
		if x == 0 {
			return nil
		}
		return &x
	case int64:
		if x == 0 {
			return nil
		}
		y := int(x)
		return &y
	case float64:
		if x == 0 {
			return nil
		}
		y := int(x)
		return &y
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return nil
		}
		if i, err := strconv.Atoi(s); err == nil {
			return &i
		}
	}
	return nil
}

func floatPtrFromAny(v any) *float64 {
	switch x := v.(type) {
	case float64:
		if x == 0 {
			return nil
		}
		return &x
	case float32:
		if x == 0 {
			return nil
		}
		y := float64(x)
		return &y
	case int:
		if x == 0 {
			return nil
		}
		y := float64(x)
		return &y
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return &f
		}
	}
	return nil
}

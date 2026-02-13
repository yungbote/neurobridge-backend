package steps

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type StructureExtractDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Threads      repos.ChatThreadRepo
	Messages     repos.ChatMessageRepo
	Turns        repos.ChatTurnRepo
	State        repos.ChatThreadStateRepo
	Concepts     repos.ConceptRepo
	ConceptModel repos.UserConceptModelRepo
	MisconRepo   repos.UserMisconceptionInstanceRepo
	Events       repos.UserEventRepo
	AI           openai.Client
}

type StructureExtractInput struct {
	UserID    uuid.UUID
	ThreadID  uuid.UUID
	MessageID uuid.UUID
}

type StructureExtractOutput struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	Processed int       `json:"processed"`
	MaxSeq    int64     `json:"max_seq"`
}

type structureExtractFrame struct {
	Frame      string  `json:"frame"`
	Confidence float64 `json:"confidence"`
}

type structureExtractUncertainty struct {
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
	LastSeenAt string  `json:"last_seen_at,omitempty"`
	Count      int     `json:"count,omitempty"`
}

type structureExtractConcept struct {
	CanonicalConceptID string  `json:"canonical_concept_id"`
	AttributionScore   float64 `json:"attribution_score"`
}

type structureExtractMisconception struct {
	PatternID   *string `json:"pattern_id,omitempty"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}

type misconceptionTaxon struct {
	PatternID   string `json:"pattern_id"`
	Description string `json:"description"`
	Source      string `json:"source,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type structureExtractPayload struct {
	ExtractorVersion  int                             `json:"extractor_version"`
	ConceptCandidates []structureExtractConcept       `json:"concept_candidates"`
	Frames            []structureExtractFrame         `json:"frames"`
	Misconceptions    []structureExtractMisconception `json:"misconceptions"`
	Uncertainty       []structureExtractUncertainty   `json:"uncertainty_regions"`
	Scope             string                          `json:"scope"`
	Polarity          string                          `json:"polarity"`
	Calibration       float64                         `json:"calibration"`
}

func StructureExtract(ctx context.Context, deps StructureExtractDeps, in StructureExtractInput) (StructureExtractOutput, error) {
	out := StructureExtractOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Threads == nil || deps.Messages == nil || deps.State == nil || deps.Concepts == nil || deps.ConceptModel == nil || deps.MisconRepo == nil || deps.AI == nil {
		return out, fmt.Errorf("structure_extract: missing deps")
	}
	if in.UserID == uuid.Nil {
		return out, fmt.Errorf("structure_extract: missing user_id")
	}
	if in.ThreadID == uuid.Nil {
		return out, fmt.Errorf("structure_extract: missing thread_id")
	}

	dbc := dbctx.Context{Ctx: ctx}
	threads, err := deps.Threads.GetByIDs(dbc, []uuid.UUID{in.ThreadID})
	if err != nil {
		return out, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].ID == uuid.Nil {
		return out, nil
	}
	thread := threads[0]
	out.ThreadID = thread.ID
	if thread.UserID != in.UserID {
		return out, fmt.Errorf("structure_extract: thread user mismatch")
	}
	if thread.PathID == nil || *thread.PathID == uuid.Nil {
		return out, nil
	}

	state, err := deps.State.GetOrCreate(dbc, thread.ID)
	if err != nil {
		return out, err
	}

	maxMessages := envIntAllowZero("STRUCTURE_EXTRACT_MAX_MESSAGES", 200)
	if maxMessages < 1 {
		maxMessages = 200
	}
	if maxMessages > 1000 {
		maxMessages = 1000
	}
	msgs, err := deps.Messages.ListSinceSeq(dbc, thread.ID, state.LastStructureSeq, maxMessages)
	if err != nil {
		return out, err
	}
	if len(msgs) == 0 {
		return out, nil
	}
	if in.MessageID != uuid.Nil {
		filtered := []*types.ChatMessage{}
		for _, m := range msgs {
			if m != nil && m.ID == in.MessageID {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			return out, nil
		}
		msgs = filtered
	}

	contextWindow := envIntAllowZero("STRUCTURE_EXTRACT_CONTEXT_WINDOW", 3)
	if contextWindow < 1 {
		contextWindow = 3
	}
	contextLimit := contextWindow*6 + 8
	if contextLimit < 12 {
		contextLimit = 12
	}
	if contextLimit > 200 {
		contextLimit = 200
	}
	recent, _ := deps.Messages.ListByThread(dbc, thread.ID, contextLimit)
	bySeq := map[int64]*types.ChatMessage{}
	for _, m := range recent {
		if m == nil || m.ID == uuid.Nil {
			continue
		}
		bySeq[m.Seq] = m
	}

	turnByMessageID := map[uuid.UUID]*types.ChatTurn{}
	lookupTurn := func(messageID uuid.UUID) *types.ChatTurn {
		if messageID == uuid.Nil || deps.Turns == nil {
			return nil
		}
		if cached, ok := turnByMessageID[messageID]; ok {
			return cached
		}
		turn, err := deps.Turns.GetByUserMessageID(dbc, in.UserID, thread.ID, messageID)
		if err != nil || turn == nil || turn.ID == uuid.Nil {
			turnByMessageID[messageID] = nil
			return nil
		}
		turnByMessageID[messageID] = turn
		return turn
	}

	concepts, err := deps.Concepts.GetByScope(dbc, "path", thread.PathID)
	if err != nil {
		return out, err
	}
	if len(concepts) == 0 {
		return out, nil
	}
	conceptIndex := buildStructureConceptIndex(concepts)
	pathConceptByCanonical := buildPathConceptIndex(concepts)

	useTaxonomy := envBool("STRUCTURE_EXTRACT_USE_TAXONOMY", true)
	taxonomyMax := envIntAllowZero("STRUCTURE_EXTRACT_TAXONOMY_MAX", 6)
	if taxonomyMax < 1 {
		taxonomyMax = 6
	}
	taxonomyByConcept := map[uuid.UUID][]misconceptionTaxon{}
	if useTaxonomy {
		canonicalIDs := collectCanonicalIDs(conceptIndex)
		if len(canonicalIDs) > 0 {
			rows, err := deps.Concepts.GetByIDs(dbc, canonicalIDs)
			if err == nil {
				for _, c := range rows {
					if c == nil || c.ID == uuid.Nil {
						continue
					}
					taxonomyByConcept[c.ID] = loadMisconceptionTaxonomy(c.Metadata)
				}
			}
		}
		// Fallback to path concept metadata when global concept is missing.
		for cid, pc := range pathConceptByCanonical {
			if _, ok := taxonomyByConcept[cid]; ok {
				continue
			}
			if pc == nil || pc.ID == uuid.Nil {
				continue
			}
			taxonomyByConcept[cid] = loadMisconceptionTaxonomy(pc.Metadata)
		}
	}

	minChars := envIntAllowZero("STRUCTURE_EXTRACT_MIN_CHARS", 200)
	if minChars < 40 {
		minChars = 40
	}
	maxConcepts := envIntAllowZero("STRUCTURE_EXTRACT_MAX_CONCEPTS", 80)
	if maxConcepts < 10 {
		maxConcepts = 10
	}
	if maxConcepts > 200 {
		maxConcepts = 200
	}
	maxFrames := envIntAllowZero("STRUCTURE_EXTRACT_MAX_FRAMES", 3)
	if maxFrames < 1 {
		maxFrames = 3
	}
	maxUnc := envIntAllowZero("STRUCTURE_EXTRACT_MAX_UNCERTAINTY", 3)
	if maxUnc < 1 {
		maxUnc = 3
	}
	emitClaims := envBool("STRUCTURE_EXTRACT_EMIT_CLAIM_EVENTS", true)
	claimMinScore := envFloatAllowZero("STRUCTURE_EXTRACT_CLAIM_MIN_SCORE", 0.35)
	if claimMinScore <= 0 {
		claimMinScore = 0.35
	}
	claimMax := envIntAllowZero("STRUCTURE_EXTRACT_CLAIM_MAX_CONCEPTS", 3)
	if claimMax < 1 {
		claimMax = 3
	}
	extractorVersion := envIntAllowZero("STRUCTURE_EXTRACT_VERSION", 1)
	if extractorVersion < 1 {
		extractorVersion = 1
	}

	var maxSeq int64
	processed := 0

	for _, msg := range msgs {
		if msg == nil || msg.ID == uuid.Nil {
			continue
		}
		if msg.Seq > maxSeq {
			maxSeq = msg.Seq
		}
		if strings.TrimSpace(strings.ToLower(msg.Role)) != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if !shouldExtractStructure(content, minChars) {
			continue
		}

		candidates := selectStructureCandidates(conceptIndex, content, maxConcepts)
		if len(candidates) == 0 {
			continue
		}

		window := buildStructureWindow(bySeq, msg.Seq, contextWindow)
		window = append(window, msg)
		windowText := formatStructureWindow(window)
		sys, usr := structureExtractPrompt(windowText, candidates, taxonomyByConcept, taxonomyMax, extractorVersion)

		obj, err := deps.AI.GenerateJSON(ctx, sys, usr, "structure_extract_v1", schemaStructureExtract())
		if err != nil {
			deps.Log.Warn("structure_extract: model error (skipping)", "error", err, "thread_id", thread.ID.String(), "msg_id", msg.ID.String())
			continue
		}

		payload := parseStructureExtract(obj)
		if len(payload.ConceptCandidates) == 0 {
			processed++
			continue
		}

		allowedConcepts := map[string]struct{}{}
		for _, c := range candidates {
			allowedConcepts[c.CanonicalConceptID.String()] = struct{}{}
		}

		// Apply updates per message in a short transaction.
		err = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			tdbc := dbctx.Context{Ctx: ctx, Tx: tx}

			// Resolve models in bulk.
			conceptIDs := []uuid.UUID{}
			primaryConcept := uuid.Nil
			primaryScore := -1.0
			for _, c := range payload.ConceptCandidates {
				id, err := uuid.Parse(strings.TrimSpace(c.CanonicalConceptID))
				if err != nil || id == uuid.Nil {
					continue
				}
				if _, ok := allowedConcepts[id.String()]; !ok {
					continue
				}
				conceptIDs = append(conceptIDs, id)
				if c.AttributionScore > primaryScore {
					primaryScore = c.AttributionScore
					primaryConcept = id
				}
			}
			conceptIDs = dedupeUUIDs(conceptIDs)
			if len(conceptIDs) == 0 {
				return nil
			}

			conceptRows, _ := deps.Concepts.GetByIDs(tdbc, conceptIDs)
			conceptByID := map[uuid.UUID]*types.Concept{}
			for _, c := range conceptRows {
				if c != nil && c.ID != uuid.Nil {
					conceptByID[c.ID] = c
				}
			}

			models, err := deps.ConceptModel.ListByUserAndConceptIDs(tdbc, in.UserID, conceptIDs)
			if err != nil {
				return err
			}
			modelByConcept := map[uuid.UUID]*types.UserConceptModel{}
			for _, m := range models {
				if m != nil && m.CanonicalConceptID != uuid.Nil {
					modelByConcept[m.CanonicalConceptID] = m
				}
			}
			existingMis, _ := deps.MisconRepo.ListActiveByUserAndConceptIDs(tdbc, in.UserID, conceptIDs)
			existingMisByKey := map[string]*types.UserMisconceptionInstance{}
			for _, m := range existingMis {
				if m == nil || m.CanonicalConceptID == uuid.Nil {
					continue
				}
				key := misconceptionKey(m.CanonicalConceptID, m.PatternID, m.Description)
				existingMisByKey[key] = m
			}

			supportID := msg.ID.String()
			seenAt := msg.CreatedAt
			type claimCandidate struct {
				score float64
				event *types.UserEvent
			}
			claimEvents := []claimCandidate{}
			misconMatchMin := envFloatAllowZero("STRUCTURE_EXTRACT_MISCONCEPT_MATCH_MIN", 0.78)
			if misconMatchMin <= 0 {
				misconMatchMin = 0.78
			}
			misconMaxPerConcept := envIntAllowZero("STRUCTURE_EXTRACT_MISCONCEPT_MAX_PER_CONCEPT", 50)
			if misconMaxPerConcept < 1 {
				misconMaxPerConcept = 50
			}
			conceptMetaUpdates := map[uuid.UUID]datatypes.JSON{}

			for _, c := range payload.ConceptCandidates {
				cid, err := uuid.Parse(strings.TrimSpace(c.CanonicalConceptID))
				if err != nil || cid == uuid.Nil {
					continue
				}
				if _, ok := allowedConcepts[cid.String()]; !ok {
					continue
				}
				model := ensureConceptModel(modelByConcept[cid], in.UserID, cid)

				ptr := supportPointer{
					SourceType: "chat_message",
					SourceID:   supportID,
					OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
					Confidence: clamp01(payload.Calibration),
				}
				if ptr.Confidence == 0 {
					ptr.Confidence = clamp01(c.AttributionScore)
				}
				modelSupport := loadSupportPointers([]byte(model.Support))
				modelSupport, added := addSupportPointer(modelSupport, ptr, 20)
				if !added {
					continue
				}
				model.Support = mustJSON(modelSupport)
				if model.ModelVersion <= 0 {
					model.ModelVersion = 1
				}
				if cid == primaryConcept {
					model.ActiveFrames = mustJSON(mergeStructureFrames(model.ActiveFrames, payload.Frames, maxFrames))
					model.Uncertainty = mustJSON(mergeStructureUncertainty(model.Uncertainty, payload.Uncertainty, seenAt, maxUnc))
				}
				ts := seenAt.UTC()
				model.LastStructuralAt = &ts

				if err := deps.ConceptModel.Upsert(tdbc, model); err != nil {
					return err
				}

				if cid == primaryConcept {
					// Map misconceptions to a stable taxonomy (and extend it if needed).
					if len(payload.Misconceptions) > 0 {
						var conceptForTax *types.Concept
						if existing := conceptByID[cid]; existing != nil {
							conceptForTax = existing
						} else if pc := pathConceptByCanonical[cid]; pc != nil {
							conceptForTax = pc
						}
						if conceptForTax != nil {
							tax := loadMisconceptionTaxonomy(conceptForTax.Metadata)
							changed := false
							for i := range payload.Misconceptions {
								desc := strings.TrimSpace(payload.Misconceptions[i].Description)
								if desc == "" {
									continue
								}
								if pid, ok := findMisconceptionMatch(desc, tax, misconMatchMin); ok {
									payload.Misconceptions[i].PatternID = &pid
									continue
								}
								if len(tax) >= misconMaxPerConcept {
									continue
								}
								pid := makeMisconceptionPatternID(cid, desc)
								payload.Misconceptions[i].PatternID = &pid
								tax = append(tax, misconceptionTaxon{
									PatternID:   pid,
									Description: desc,
									Source:      "chat_extract",
									CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
								})
								changed = true
							}
							if changed {
								conceptMetaUpdates[conceptForTax.ID] = upsertMisconceptionTaxonomy(conceptForTax.Metadata, tax)
							}
						}
					}
					signature := inferStructureMisconceptionSignature(payload.Polarity, payload.Scope)
					triggerContext := "message:" + msg.ID.String()
					for _, mc := range payload.Misconceptions {
						desc := strings.TrimSpace(mc.Description)
						if desc == "" {
							continue
						}
						conf := clamp01(mc.Confidence)
						if conf == 0 {
							conf = clamp01(ptr.Confidence)
						}
						pattern := mc.PatternID
						key := misconceptionKey(cid, pattern, desc)
						ex := existingMisByKey[key]
						firstSeen := seenAt.UTC()
						if ex != nil && ex.FirstSeenAt != nil {
							firstSeen = ex.FirstSeenAt.UTC()
						}
						misSupport := types.DecodeMisconceptionSupport(datatypes.JSON(nil))
						if ex != nil {
							misSupport = types.DecodeMisconceptionSupport(ex.Support)
						}
						if misSupport.SignatureType == "" || misSupport.SignatureType == "unknown" {
							misSupport.SignatureType = signature
						}
						misSupport = types.MergeMisconceptionSupportPointer(misSupport, types.MisconceptionSupportPointer{
							SourceType: "chat_message",
							SourceID:   supportID,
							OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
							Confidence: clamp01(ptr.Confidence),
						}, 20)
						if triggerContext != "" {
							misSupport = types.AddMisconceptionTriggerContext(misSupport, triggerContext, 12)
						}
						row := &types.UserMisconceptionInstance{
							UserID:             in.UserID,
							CanonicalConceptID: cid,
							PatternID:          pattern,
							Description:        desc,
							Status:             "active",
							Confidence:         conf,
							FirstSeenAt:        &firstSeen,
							LastSeenAt:         &ts,
							Support:            types.EncodeMisconceptionSupport(misSupport),
						}
						if ex != nil && ex.Confidence > conf {
							row.Confidence = ex.Confidence
						}
						if err := deps.MisconRepo.Upsert(tdbc, row); err != nil {
							return err
						}
					}
				}
				if emitClaims && deps.Events != nil && c.AttributionScore >= claimMinScore {
					polarity := strings.TrimSpace(strings.ToLower(payload.Polarity))
					scope := strings.TrimSpace(strings.ToLower(payload.Scope))
					isCorrect := false
					hasTruth := false
					isConfusion := false
					switch polarity {
					case "confident_right":
						hasTruth = true
						isCorrect = true
					case "confident_wrong":
						hasTruth = true
						isCorrect = false
					case "confusion":
						isConfusion = true
					}
					signal := clamp01(payload.Calibration)
					if signal == 0 {
						signal = clamp01(c.AttributionScore)
					}
					data := map[string]any{
						"concept_ids":       []string{cid.String()},
						"polarity":          polarity,
						"scope":             scope,
						"calibration":       clamp01(payload.Calibration),
						"attribution_score": clamp01(c.AttributionScore),
						"signal_strength":   signal,
						"thread_id":         thread.ID.String(),
						"message_id":        msg.ID.String(),
					}
					if turn := lookupTurn(msg.ID); turn != nil && turn.ID != uuid.Nil {
						data["chat_turn_id"] = turn.ID.String()
						data["retrieval_trace_id"] = turn.ID.String()
						if turn.AssistantMessageID != uuid.Nil {
							data["assistant_message_id"] = turn.AssistantMessageID.String()
						}
					}
					if hasTruth {
						data["is_correct"] = isCorrect
						data["has_truth"] = true
					}
					if isConfusion {
						data["is_confusion"] = true
					}
					if cid == primaryConcept && len(payload.Misconceptions) > 0 {
						ids := []string{}
						for _, mc := range payload.Misconceptions {
							if mc.PatternID != nil && strings.TrimSpace(*mc.PatternID) != "" {
								ids = append(ids, strings.TrimSpace(*mc.PatternID))
							}
						}
						if len(ids) > 0 {
							data["misconception_ids"] = dedupeStrings(ids)
						}
					}

					clientEventID := fmt.Sprintf("structure_claim:%s:%s:%s", msg.ID.String(), cid.String(), polarity)
					ev := &types.UserEvent{
						ID:            uuid.New(),
						UserID:        in.UserID,
						ClientEventID: clientEventID,
						OccurredAt:    seenAt,
						PathID:        thread.PathID,
						ConceptID:     &cid,
						Type:          types.EventConceptClaimEvaluated,
						Data:          datatypes.JSON(mustJSON(data)),
					}
					claimEvents = append(claimEvents, claimCandidate{score: c.AttributionScore, event: ev})
				}
			}
			if len(conceptMetaUpdates) > 0 {
				for id, meta := range conceptMetaUpdates {
					if id == uuid.Nil {
						continue
					}
					if err := deps.Concepts.UpdateFields(tdbc, id, map[string]interface{}{
						"metadata": meta,
					}); err != nil {
						return err
					}
				}
			}
			if emitClaims && deps.Events != nil && len(claimEvents) > 0 {
				sort.Slice(claimEvents, func(i, j int) bool {
					return claimEvents[i].score > claimEvents[j].score
				})
				if claimMax > 0 && len(claimEvents) > claimMax {
					claimEvents = claimEvents[:claimMax]
				}
				rows := make([]*types.UserEvent, 0, len(claimEvents))
				for _, c := range claimEvents {
					if c.event != nil {
						rows = append(rows, c.event)
					}
				}
				_, _ = deps.Events.CreateIgnoreDuplicates(tdbc, rows)
			}
			return nil
		})
		if err != nil {
			return out, err
		}
		processed++
	}

	if maxSeq > 0 {
		if err := deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_structure_seq": maxSeq,
		}); err != nil {
			return out, err
		}
		out.MaxSeq = maxSeq
	}
	out.Processed = processed
	return out, nil
}

type structureCandidate struct {
	CanonicalConceptID uuid.UUID
	Key                string
	Name               string
	Tokens             []string
	KeyTokens          []string
}

func buildStructureConceptIndex(concepts []*types.Concept) []structureCandidate {
	out := make([]structureCandidate, 0, len(concepts))
	seen := map[uuid.UUID]bool{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		if c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil {
			continue
		}
		cid := *c.CanonicalConceptID
		if cid == uuid.Nil || seen[cid] {
			continue
		}
		seen[cid] = true
		key := strings.TrimSpace(strings.ToLower(c.Key))
		name := strings.TrimSpace(c.Name)
		out = append(out, structureCandidate{
			CanonicalConceptID: cid,
			Key:                key,
			Name:               name,
			Tokens:             tokenizeConceptText(name),
			KeyTokens:          tokenizeConceptText(key),
		})
	}
	return out
}

func buildPathConceptIndex(concepts []*types.Concept) map[uuid.UUID]*types.Concept {
	out := map[uuid.UUID]*types.Concept{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil || c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil {
			continue
		}
		cid := *c.CanonicalConceptID
		prev := out[cid]
		if prev == nil {
			out[cid] = c
			continue
		}
		if len(prev.Metadata) == 0 && len(c.Metadata) > 0 {
			out[cid] = c
		}
	}
	return out
}

func collectCanonicalIDs(index []structureCandidate) []uuid.UUID {
	if len(index) == 0 {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(index))
	for _, c := range index {
		if c.CanonicalConceptID == uuid.Nil || seen[c.CanonicalConceptID] {
			continue
		}
		seen[c.CanonicalConceptID] = true
		out = append(out, c.CanonicalConceptID)
	}
	return out
}

func tokenizeConceptText(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			tok := cur.String()
			if len(tok) >= 3 {
				out = append(out, tok)
			}
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		tok := cur.String()
		if len(tok) >= 3 {
			out = append(out, tok)
		}
	}
	return dedupeStrings(out)
}

func inferStructureMisconceptionSignature(polarity, scope string) string {
	pol := strings.TrimSpace(strings.ToLower(polarity))
	sc := strings.TrimSpace(strings.ToLower(scope))
	switch pol {
	case "confusion":
		return "frame_error"
	case "confident_wrong":
		switch sc {
		case "explanation", "assertion":
			return "frame_error"
		case "question", "attempt":
			return "procedural_gap"
		default:
			return "frame_error"
		}
	}
	return "unknown"
}

func loadMisconceptionTaxonomy(raw datatypes.JSON) []misconceptionTaxon {
	if len(raw) == 0 {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	var meta map[string]any
	if json.Unmarshal(raw, &meta) != nil {
		return nil
	}
	payload, ok := meta["misconception_taxonomy"]
	if !ok {
		return nil
	}
	if m, ok := payload.(map[string]any); ok {
		if items, ok := m["items"]; ok {
			payload = items
		} else if items, ok := m["patterns"]; ok {
			payload = items
		}
	}
	list, ok := payload.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]misconceptionTaxon, 0, len(list))
	seenID := map[string]bool{}
	seenDesc := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pattern := strings.TrimSpace(asString(m["pattern_id"]))
		if pattern == "" {
			pattern = strings.TrimSpace(asString(m["id"]))
		}
		desc := strings.TrimSpace(asString(m["description"]))
		if desc == "" {
			desc = strings.TrimSpace(asString(m["text"]))
		}
		if pattern == "" || desc == "" {
			continue
		}
		if seenID[strings.ToLower(pattern)] {
			continue
		}
		norm := normalizeMisconceptionText(desc)
		if norm != "" && seenDesc[norm] {
			continue
		}
		seenID[strings.ToLower(pattern)] = true
		if norm != "" {
			seenDesc[norm] = true
		}
		out = append(out, misconceptionTaxon{
			PatternID:   pattern,
			Description: desc,
			Source:      strings.TrimSpace(asString(m["source"])),
			CreatedAt:   strings.TrimSpace(asString(m["created_at"])),
		})
	}
	return out
}

func upsertMisconceptionTaxonomy(raw datatypes.JSON, tax []misconceptionTaxon) datatypes.JSON {
	meta := map[string]any{}
	if len(raw) > 0 {
		text := strings.TrimSpace(string(raw))
		if text != "" && text != "null" {
			_ = json.Unmarshal(raw, &meta)
		}
	}
	if meta == nil {
		meta = map[string]any{}
	}
	clean := make([]misconceptionTaxon, 0, len(tax))
	seenID := map[string]bool{}
	seenDesc := map[string]bool{}
	for _, t := range tax {
		pattern := strings.TrimSpace(t.PatternID)
		desc := strings.TrimSpace(t.Description)
		if pattern == "" || desc == "" {
			continue
		}
		lowPattern := strings.ToLower(pattern)
		if seenID[lowPattern] {
			continue
		}
		norm := normalizeMisconceptionText(desc)
		if norm != "" && seenDesc[norm] {
			continue
		}
		seenID[lowPattern] = true
		if norm != "" {
			seenDesc[norm] = true
		}
		clean = append(clean, misconceptionTaxon{
			PatternID:   pattern,
			Description: desc,
			Source:      strings.TrimSpace(t.Source),
			CreatedAt:   strings.TrimSpace(t.CreatedAt),
		})
	}
	list := make([]map[string]any, 0, len(clean))
	for _, t := range clean {
		list = append(list, map[string]any{
			"pattern_id":  t.PatternID,
			"description": t.Description,
			"source":      t.Source,
			"created_at":  t.CreatedAt,
		})
	}
	meta["misconception_taxonomy"] = list
	return datatypes.JSON(mustJSON(meta))
}

func findMisconceptionMatch(desc string, tax []misconceptionTaxon, min float64) (string, bool) {
	desc = normalizeMisconceptionText(desc)
	if desc == "" || len(tax) == 0 {
		return "", false
	}
	descTokens := tokenizeMisconceptionText(desc)
	bestScore := 0.0
	bestID := ""
	for _, t := range tax {
		pattern := strings.TrimSpace(t.PatternID)
		if pattern == "" {
			continue
		}
		tdesc := normalizeMisconceptionText(t.Description)
		if tdesc == "" {
			continue
		}
		if desc == tdesc {
			return pattern, true
		}
		if strings.Contains(desc, tdesc) || strings.Contains(tdesc, desc) {
			if 0.95 >= min {
				return pattern, true
			}
		}
		score := jaccardSimilarity(descTokens, tokenizeMisconceptionText(tdesc))
		if score > bestScore {
			bestScore = score
			bestID = pattern
		}
	}
	if bestID == "" || bestScore < min {
		return "", false
	}
	return bestID, true
}

func makeMisconceptionPatternID(conceptID uuid.UUID, desc string) string {
	norm := normalizeMisconceptionText(desc)
	payload := conceptID.String() + "|" + norm
	sum := sha1.Sum([]byte(payload))
	return "mc_" + hex.EncodeToString(sum[:])[:12]
}

func normalizeMisconceptionText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func tokenizeMisconceptionText(s string) []string {
	norm := normalizeMisconceptionText(s)
	if norm == "" {
		return nil
	}
	parts := strings.Fields(norm)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) >= 3 {
			out = append(out, p)
		}
	}
	return dedupeStrings(out)
}

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	inter := 0
	union := len(set)
	for _, v := range b {
		if set[v] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func shouldExtractStructure(content string, minChars int) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) >= minChars {
		return true
	}
	lower := strings.ToLower(trimmed)
	if len(trimmed) >= minChars/2 {
		if strings.Contains(lower, "?") {
			return true
		}
		for _, kw := range []string{"why", "how", "what", "explain", "difference", "confused"} {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

func selectStructureCandidates(index []structureCandidate, content string, maxConcepts int) []structureCandidate {
	if len(index) == 0 {
		return nil
	}
	lower := strings.ToLower(content)
	type scored struct {
		structureCandidate
		score float64
	}
	scoredList := make([]scored, 0, len(index))
	for _, c := range index {
		score := 0.0
		if c.Key != "" {
			if strings.Contains(lower, c.Key) {
				score += 2.0
			}
			if alt := strings.ReplaceAll(c.Key, "_", " "); alt != c.Key && strings.Contains(lower, alt) {
				score += 1.2
			}
		}
		for _, tok := range c.Tokens {
			if len(tok) < 4 {
				continue
			}
			if strings.Contains(lower, tok) {
				score += 1.0
			}
		}
		for _, tok := range c.KeyTokens {
			if len(tok) < 4 {
				continue
			}
			if strings.Contains(lower, tok) {
				score += 1.0
			}
		}
		if score > 0 {
			scoredList = append(scoredList, scored{structureCandidate: c, score: score})
		}
	}
	if len(scoredList) == 0 {
		limit := maxConcepts
		if limit > len(index) {
			limit = len(index)
		}
		return index[:limit]
	}
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score == scoredList[j].score {
			return scoredList[i].Key < scoredList[j].Key
		}
		return scoredList[i].score > scoredList[j].score
	})
	if maxConcepts > 0 && len(scoredList) > maxConcepts {
		scoredList = scoredList[:maxConcepts]
	}
	out := make([]structureCandidate, 0, len(scoredList))
	for _, sc := range scoredList {
		out = append(out, sc.structureCandidate)
	}
	return out
}

func buildStructureWindow(bySeq map[int64]*types.ChatMessage, seq int64, window int) []*types.ChatMessage {
	if window <= 0 || len(bySeq) == 0 {
		return nil
	}
	out := make([]*types.ChatMessage, 0, window)
	for i := int64(1); i <= int64(window); i++ {
		if m := bySeq[seq-i]; m != nil {
			out = append(out, m)
		}
	}
	return out
}

func formatStructureWindow(msgs []*types.ChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Seq < msgs[j].Seq })
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(fmt.Sprint(m.Seq))
		b.WriteString("] ")
		b.WriteString(strings.TrimSpace(m.Role))
		b.WriteString(":\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func structureExtractPrompt(window string, candidates []structureCandidate, taxonomy map[uuid.UUID][]misconceptionTaxon, taxonomyMax int, version int) (string, string) {
	sys := `ROLE: Structural understanding extractor.
TASK: Extract structured signals about a user's understanding from a chat message.
OUTPUT: Return ONLY JSON matching the schema (no extra keys).
RULES: Use ONLY the provided candidate concepts (canonical_concept_id). If none apply, return empty arrays. Keep misconception descriptions concise (max 12 words). Do not invent IDs.`
	var b strings.Builder
	b.WriteString("Conversation window:\n")
	if strings.TrimSpace(window) == "" {
		b.WriteString("(no context)\n")
	} else {
		b.WriteString(window)
		b.WriteString("\n")
	}
	b.WriteString("\nCandidate concepts (canonical_concept_id | key | name):\n")
	for _, c := range candidates {
		b.WriteString("- ")
		b.WriteString(c.CanonicalConceptID.String())
		b.WriteString(" | ")
		b.WriteString(strings.TrimSpace(c.Key))
		b.WriteString(" | ")
		b.WriteString(strings.TrimSpace(c.Name))
		b.WriteString("\n")
	}
	if len(taxonomy) > 0 && taxonomyMax > 0 {
		b.WriteString("\nKnown misconception patterns (pattern_id | description):\n")
		for _, c := range candidates {
			tax := taxonomy[c.CanonicalConceptID]
			if len(tax) == 0 {
				continue
			}
			b.WriteString("- ")
			b.WriteString(c.CanonicalConceptID.String())
			b.WriteString(":\n")
			max := taxonomyMax
			if len(tax) < max {
				max = len(tax)
			}
			for i := 0; i < max; i++ {
				b.WriteString("  - ")
				b.WriteString(strings.TrimSpace(tax[i].PatternID))
				b.WriteString(" | ")
				b.WriteString(strings.TrimSpace(tax[i].Description))
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\nReturn JSON with extractor_version=")
	b.WriteString(fmt.Sprint(version))
	return sys, b.String()
}

func schemaStructureExtract() map[string]any {
	frames := []any{"definition", "procedural", "mechanistic", "probabilistic", "geometric", "intuitive", "boundary", "application"}
	uncertainty := []any{"definition_gap", "procedural_gap", "mechanistic_gap", "boundary_gap", "notation_gap"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"extractor_version": map[string]any{"type": "integer"},
			"concept_candidates": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"canonical_concept_id": map[string]any{"type": "string"},
						"attribution_score":    map[string]any{"type": "number"},
					},
					"required": []string{"canonical_concept_id", "attribution_score"},
				},
			},
			"frames": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"frame":      map[string]any{"type": "string", "enum": frames},
						"confidence": map[string]any{"type": "number"},
					},
					"required": []string{"frame", "confidence"},
				},
			},
			"misconceptions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern_id":  map[string]any{"type": []string{"string", "null"}},
						"description": map[string]any{"type": "string"},
						"confidence":  map[string]any{"type": "number"},
					},
					"required": []string{"description", "confidence"},
				},
			},
			"uncertainty_regions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":       map[string]any{"type": "string", "enum": uncertainty},
						"confidence": map[string]any{"type": "number"},
					},
					"required": []string{"kind", "confidence"},
				},
			},
			"scope":       map[string]any{"type": "string", "enum": []any{"question", "explanation", "assertion", "attempt"}},
			"polarity":    map[string]any{"type": "string", "enum": []any{"confusion", "confident_wrong", "confident_right", "neutral"}},
			"calibration": map[string]any{"type": "number"},
		},
		"required": []string{
			"extractor_version",
			"concept_candidates",
			"frames",
			"misconceptions",
			"uncertainty_regions",
			"scope",
			"polarity",
			"calibration",
		},
	}
}

func parseStructureExtract(obj map[string]any) structureExtractPayload {
	out := structureExtractPayload{}
	if obj == nil {
		return out
	}
	b, _ := json.Marshal(obj)
	_ = json.Unmarshal(b, &out)
	out.Calibration = clamp01(out.Calibration)
	return out
}

func mergeStructureFrames(raw datatypes.JSON, incoming []structureExtractFrame, max int) []structureExtractFrame {
	existing := []structureExtractFrame{}
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "" && strings.TrimSpace(string(raw)) != "null" {
		_ = json.Unmarshal(raw, &existing)
	}
	byFrame := map[string]structureExtractFrame{}
	for _, f := range existing {
		k := normalizeFrameName(f.Frame)
		if k == "" {
			continue
		}
		f.Frame = k
		f.Confidence = clamp01(f.Confidence)
		byFrame[k] = f
	}
	for _, f := range incoming {
		k := normalizeFrameName(f.Frame)
		if k == "" {
			continue
		}
		conf := clamp01(f.Confidence)
		if cur, ok := byFrame[k]; ok {
			if conf > cur.Confidence {
				cur.Confidence = conf
				byFrame[k] = cur
			}
		} else {
			byFrame[k] = structureExtractFrame{Frame: k, Confidence: conf}
		}
	}
	out := make([]structureExtractFrame, 0, len(byFrame))
	for _, v := range byFrame {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func mergeStructureUncertainty(raw datatypes.JSON, incoming []structureExtractUncertainty, seenAt time.Time, max int) []structureExtractUncertainty {
	existing := []structureExtractUncertainty{}
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "" && strings.TrimSpace(string(raw)) != "null" {
		_ = json.Unmarshal(raw, &existing)
	}
	byKind := map[string]structureExtractUncertainty{}
	for _, u := range existing {
		k := normalizeUncertaintyKind(u.Kind)
		if k == "" {
			continue
		}
		u.Kind = k
		u.Confidence = clamp01(u.Confidence)
		byKind[k] = u
	}
	for _, u := range incoming {
		k := normalizeUncertaintyKind(u.Kind)
		if k == "" {
			continue
		}
		conf := clamp01(u.Confidence)
		item := byKind[k]
		if conf > item.Confidence {
			item.Confidence = conf
		}
		item.Count++
		item.Kind = k
		item.LastSeenAt = seenAt.UTC().Format(time.RFC3339Nano)
		byKind[k] = item
	}
	out := make([]structureExtractUncertainty, 0, len(byKind))
	for _, v := range byKind {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func normalizeFrameName(frame string) string {
	f := strings.ToLower(strings.TrimSpace(frame))
	switch f {
	case "definition", "procedural", "mechanistic", "probabilistic", "geometric", "intuitive", "boundary", "application":
		return f
	}
	if strings.Contains(f, "definition") {
		return "definition"
	}
	if strings.Contains(f, "procedure") || strings.Contains(f, "process") {
		return "procedural"
	}
	if strings.Contains(f, "mechan") || strings.Contains(f, "causal") {
		return "mechanistic"
	}
	if strings.Contains(f, "prob") || strings.Contains(f, "stat") {
		return "probabilistic"
	}
	if strings.Contains(f, "geom") || strings.Contains(f, "intuit") {
		return "intuitive"
	}
	if strings.Contains(f, "bound") || strings.Contains(f, "limit") {
		return "boundary"
	}
	if strings.Contains(f, "apply") || strings.Contains(f, "transfer") {
		return "application"
	}
	return ""
}

func normalizeUncertaintyKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "definition_gap", "procedural_gap", "mechanistic_gap", "boundary_gap", "notation_gap":
		return k
	}
	if strings.Contains(k, "definition") {
		return "definition_gap"
	}
	if strings.Contains(k, "procedure") || strings.Contains(k, "process") {
		return "procedural_gap"
	}
	if strings.Contains(k, "mechan") || strings.Contains(k, "causal") {
		return "mechanistic_gap"
	}
	if strings.Contains(k, "bound") || strings.Contains(k, "limit") {
		return "boundary_gap"
	}
	if strings.Contains(k, "notation") || strings.Contains(k, "symbol") {
		return "notation_gap"
	}
	return ""
}

func misconceptionKey(conceptID uuid.UUID, pattern *string, description string) string {
	p := ""
	if pattern != nil {
		p = strings.TrimSpace(*pattern)
	}
	return conceptID.String() + "|" + strings.ToLower(strings.TrimSpace(p)) + "|" + strings.ToLower(strings.TrimSpace(description))
}

func ensureConceptModel(prev *types.UserConceptModel, userID uuid.UUID, conceptID uuid.UUID) *types.UserConceptModel {
	if prev != nil {
		return prev
	}
	return &types.UserConceptModel{
		ID:                 uuid.New(),
		UserID:             userID,
		CanonicalConceptID: conceptID,
		ModelVersion:       1,
	}
}

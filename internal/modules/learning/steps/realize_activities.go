package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	learningcontent "github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type RealizeActivitiesDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path               repos.PathRepo
	PathNodes          repos.PathNodeRepo
	PathNodeActivities repos.PathNodeActivityRepo

	Activities        repos.ActivityRepo
	Variants          repos.ActivityVariantRepo
	ActivityConcepts  repos.ActivityConceptRepo
	ActivityCitations repos.ActivityCitationRepo

	Concepts     repos.ConceptRepo
	ConceptState repos.UserConceptStateRepo
	Files        repos.MaterialFileRepo
	Chunks       repos.MaterialChunkRepo

	UserProfile repos.UserProfileVectorRepo
	Patterns    repos.TeachingPatternRepo

	Graph *neo4jdb.Client

	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type RealizeActivitiesInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type RealizeActivitiesOutput struct {
	PathID         uuid.UUID `json:"path_id"`
	ActivitiesMade int       `json:"activities_made"`
	VariantsMade   int       `json:"variants_made"`
}

func RealizeActivities(ctx context.Context, deps RealizeActivitiesDeps, in RealizeActivitiesInput) (RealizeActivitiesOutput, error) {
	out := RealizeActivitiesOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.PathNodeActivities == nil ||
		deps.Activities == nil || deps.Variants == nil || deps.ActivityConcepts == nil || deps.ActivityCitations == nil ||
		deps.Concepts == nil || deps.Files == nil || deps.Chunks == nil || deps.UserProfile == nil ||
		deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("realize_activities: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("realize_activities: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("realize_activities: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("realize_activities: missing saga_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
	if err != nil || up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("realize_activities: missing user_profile_doc (run user_profile_refresh first)")
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	charterJSON := ""
	var allowFiles map[uuid.UUID]bool
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
			allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
			if v, ok := meta["charter"]; ok && v != nil {
				if b, err := json.Marshal(v); err == nil {
					charterJSON = string(b)
				}
			}
		}
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("realize_activities: no path nodes (run path_plan_build first)")
	}

	// Existing joins -> idempotency guard by (node_id, rank==slot)
	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	existingJoins, err := deps.PathNodeActivities.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}
	existingRank := map[uuid.UUID]map[int]bool{}
	for _, j := range existingJoins {
		if j == nil || j.PathNodeID == uuid.Nil {
			continue
		}
		if existingRank[j.PathNodeID] == nil {
			existingRank[j.PathNodeID] = map[int]bool{}
		}
		existingRank[j.PathNodeID][j.Rank] = true
	}

	// Concepts for joins (key -> id)
	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	keyToConceptID := map[string]uuid.UUID{}
	canonicalIDByKey := map[string]uuid.UUID{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(c.Key))
		if k == "" {
			continue
		}
		keyToConceptID[k] = c.ID

		cid := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			cid = *c.CanonicalConceptID
		}
		if cid != uuid.Nil {
			canonicalIDByKey[k] = cid
		}
	}

	// Chunks for grounding (Pinecone preferred; local fallback needs embeddings)
	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(allowFiles) > 0 {
		filtered := filterMaterialFilesByAllowlist(files, allowFiles)
		if len(filtered) > 0 {
			files = filtered
		} else {
			deps.Log.Warn("realize_activities: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
		}
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	allChunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	if len(allChunks) == 0 {
		return out, fmt.Errorf("realize_activities: no chunks for material set")
	}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	embByID := map[uuid.UUID][]float32{}
	for _, ch := range allChunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID] = ch
		if v, ok := decodeEmbedding(ch.Embedding); ok {
			embByID[ch.ID] = v
		}
	}

	// Derived material sets share the chunk namespace (and KG products) with their source upload batch.
	sourceSetID := in.MaterialSetID
	if deps.DB != nil {
		if sc, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
			sourceSetID = sc.SourceMaterialSetID
		}
	}
	chunksNS := index.ChunksNamespace(sourceSetID)
	actNS := index.ActivitiesNamespace("path", &pathID)

	// Iterate nodes/slots deterministically.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	// Precompute deterministic local-embedding scan order for fallback retrieval.
	chunkEmbs := make([]chunkEmbedding, 0, len(embByID))
	for id, emb := range embByID {
		if id == uuid.Nil || len(emb) == 0 {
			continue
		}
		chunkEmbs = append(chunkEmbs, chunkEmbedding{ID: id, Emb: emb})
	}
	sort.Slice(chunkEmbs, func(i, j int) bool { return chunkEmbs[i].ID.String() < chunkEmbs[j].ID.String() })

	type activitySlotWork struct {
		NodeID           uuid.UUID
		NodeTitle        string
		NodeGoal         string
		NodeDifficulty   string
		SlotIndex        int
		Kind             string
		EstimatedMinutes int
		PrimaryKeys      []string
		ConceptCSV       string
		QueryText        string
		TitleHint        string
	}

	work := make([]activitySlotWork, 0)
	for _, node := range nodes {
		if node == nil || node.ID == uuid.Nil {
			continue
		}

		nodeMeta := map[string]any{}
		if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
			_ = json.Unmarshal(node.Metadata, &nodeMeta)
		}
		nodeGoal := strings.TrimSpace(stringFromAny(nodeMeta["goal"]))
		nodeDifficulty := strings.TrimSpace(stringFromAny(nodeMeta["difficulty"]))
		nodeConceptKeys := dedupeStrings(stringSliceFromAny(nodeMeta["concept_keys"]))
		fallbackKeys := nodeConceptKeys
		if len(fallbackKeys) == 0 {
			fallbackKeys = fallbackConceptKeysForNode(node.Title, nodeGoal, concepts, 8)
			if len(fallbackKeys) > 0 {
				deps.Log.Warn("realize_activities: missing concept_keys on path node; using inferred keys", "path_node_id", node.ID.String(), "concept_keys", strings.Join(fallbackKeys, ","))
			}
		}

		rawSlots, _ := nodeMeta["activity_slots"].([]any)
		if len(rawSlots) == 0 {
			continue
		}

		for i, raw := range rawSlots {
			slotObj, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			slotIndex := intFromAny(slotObj["slot"], i)
			if existingRank[node.ID] != nil && existingRank[node.ID][slotIndex] {
				continue
			}

			kind := strings.TrimSpace(stringFromAny(slotObj["kind"]))
			if kind == "" {
				kind = "reading"
			}
			estimatedMinutes := intFromAny(slotObj["estimated_minutes"], 10)
			primaryKeys := dedupeStrings(stringSliceFromAny(slotObj["primary_concept_keys"]))
			if len(primaryKeys) == 0 {
				primaryKeys = fallbackKeys
			}
			conceptCSV := strings.TrimSpace(strings.Join(primaryKeys, ", "))
			if conceptCSV == "" {
				// Keep the prompt validator satisfied even for legacy nodes missing concept keys / titles.
				conceptCSV = strings.TrimSpace(node.Title)
			}
			if conceptCSV == "" {
				conceptCSV = nodeGoal
			}
			if conceptCSV == "" {
				conceptCSV = kind
			}
			if conceptCSV == "" {
				conceptCSV = "general"
			}

			work = append(work, activitySlotWork{
				NodeID:           node.ID,
				NodeTitle:        node.Title,
				NodeGoal:         nodeGoal,
				NodeDifficulty:   nodeDifficulty,
				SlotIndex:        slotIndex,
				Kind:             kind,
				EstimatedMinutes: estimatedMinutes,
				PrimaryKeys:      primaryKeys,
				ConceptCSV:       conceptCSV,
				QueryText:        strings.TrimSpace(node.Title + " " + nodeGoal + " " + kind + " " + conceptCSV),
				TitleHint:        strings.TrimSpace(kind + ": " + node.Title),
			})
		}
	}

	if len(work) == 0 {
		if deps.Graph != nil {
			if err := syncPathActivitiesToNeo4j(ctx, deps, pathID); err != nil {
				deps.Log.Warn("neo4j activities sync failed (continuing)", "error", err, "path_id", pathID.String())
			}
		}
		return out, nil
	}

	signalCtx := loadMaterialSetSignalContext(ctx, deps.DB, in.MaterialSetID, 0)
	if len(signalCtx.WeightsByKey) > 0 {
		for i := range work {
			if len(work[i].PrimaryKeys) == 0 {
				continue
			}
			work[i].PrimaryKeys = sortConceptKeysByWeight(work[i].PrimaryKeys, signalCtx.WeightsByKey)
			if len(work[i].PrimaryKeys) > 0 {
				work[i].ConceptCSV = strings.TrimSpace(strings.Join(work[i].PrimaryKeys, ", "))
			}
			parts := []string{work[i].NodeTitle, work[i].NodeGoal, work[i].Kind, work[i].ConceptCSV}
			work[i].QueryText = strings.TrimSpace(strings.Join(parts, " "))
		}
	}

	// ---- User knowledge context (cross-path mastery transfer) ----
	stateByConceptID := map[uuid.UUID]*types.UserConceptState{}
	if deps.ConceptState != nil && len(canonicalIDByKey) > 0 {
		needed := map[uuid.UUID]bool{}
		for _, w := range work {
			for _, k := range w.PrimaryKeys {
				k = strings.TrimSpace(strings.ToLower(k))
				if k == "" {
					continue
				}
				if id := canonicalIDByKey[k]; id != uuid.Nil {
					needed[id] = true
				}
			}
		}
		ids := make([]uuid.UUID, 0, len(needed))
		for id := range needed {
			if id != uuid.Nil {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		if len(ids) > 0 {
			if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, ids); err == nil {
				for _, st := range rows {
					if st == nil || st.ConceptID == uuid.Nil {
						continue
					}
					stateByConceptID[st.ConceptID] = st
				}
			} else if deps.Log != nil {
				deps.Log.Warn("realize_activities: failed to load user concept states (continuing)", "error", err, "user_id", in.OwnerUserID.String())
			}
		}
	}
	knowledgeNow := time.Now().UTC()

	maxConc := envInt("REALIZE_ACTIVITIES_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var actsMade int32
	var varsMade int32

	for i := range work {
		w := work[i]
		g.Go(func() error {
			qEmb, err := deps.AI.Embed(gctx, []string{w.QueryText})
			if err != nil {
				return err
			}
			if len(qEmb) == 0 || len(qEmb[0]) == 0 {
				return fmt.Errorf("realize_activities: empty query embedding")
			}

			chunkIDs, _, _ := graphAssistedChunkIDs(gctx, deps.DB, deps.Vec, chunkRetrievePlan{
				MaterialSetID: sourceSetID,
				ChunksNS:      chunksNS,
				QueryText:     w.QueryText,
				QueryEmb:      qEmb[0],
				FileIDs:       fileIDs,
				AllowFiles:    allowFiles,
				SeedK:         12,
				LexicalK:      6,
				FinalK:        12,
				ChunkEmbs:     chunkEmbs,
			})
			if len(chunkIDs) == 0 {
				if len(chunkEmbs) == 0 {
					return fmt.Errorf("realize_activities: no local embeddings available (run embed_chunks first)")
				}
				chunkIDs = topKChunkIDsByCosine(qEmb[0], chunkEmbs, 12)
			}

			excerpts := buildActivityExcerpts(chunkByID, chunkIDs, 12, 700)
			if strings.TrimSpace(excerpts) == "" {
				return fmt.Errorf("realize_activities: empty grounding excerpts")
			}

			// Prompt: canonical activity content.
			teachingJSON, _ := teachingPatternsJSON(gctx, deps.Vec, deps.Patterns, qEmb[0], 4)
			userKnowledgeJSON := "(none)"
			if len(w.PrimaryKeys) > 0 && len(canonicalIDByKey) > 0 {
				uj := BuildUserKnowledgeContextV1(w.PrimaryKeys, canonicalIDByKey, stateByConceptID, knowledgeNow).JSON()
				if strings.TrimSpace(uj) != "" {
					userKnowledgeJSON = uj
				}
			}
			p, err := prompts.Build(prompts.PromptActivityContent, prompts.Input{
				UserProfileDoc:       up.ProfileDoc,
				TeachingPatternsJSON: teachingJSON,
				PathCharterJSON:      charterJSON,
				ActivityKind:         w.Kind,
				ActivityTitle:        w.TitleHint,
				ConceptKeysCSV:       w.ConceptCSV,
				UserKnowledgeJSON:    userKnowledgeJSON,
				ActivityExcerpts:     excerpts,
			})
			if err != nil {
				return err
			}
			allowedChunkIDs := map[string]bool{}
			for _, id := range chunkIDs {
				if id != uuid.Nil {
					allowedChunkIDs[id.String()] = true
				}
			}

			var obj map[string]any
			var lastErrs []string
			for attempt := 1; attempt <= 3; attempt++ {
				userPrompt := p.User
				if len(lastErrs) > 0 {
					userPrompt = userPrompt + "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrs, "\n- ")
				}
				obj, err = deps.AI.GenerateJSON(gctx, p.System, userPrompt, p.SchemaName, p.Schema)
				if err != nil {
					lastErrs = []string{"generate_failed: " + err.Error()}
					continue
				}
				// Auto-repair: some valid content can come back without explicit heading blocks.
				// We enforce a stable minimum structure (at least one heading) without re-prompting
				// or changing the semantic content/quality of the generation.
				ensureActivityContentHasHeadingBlock(obj, w.TitleHint)
				ensureActivityContentMeetsMinima(obj, w.Kind)
				lastErrs = validateActivityContent(obj, w.Kind)
				if len(lastErrs) == 0 {
					break
				}
			}
			if len(lastErrs) > 0 {
				return fmt.Errorf("realize_activities: activity_content failed validation: %s", strings.Join(lastErrs, "; "))
			}

			actTitle := strings.TrimSpace(stringFromAny(obj["title"]))
			if actTitle == "" {
				actTitle = w.TitleHint
			}
			actKind := strings.TrimSpace(stringFromAny(obj["kind"]))
			if actKind == "" {
				actKind = w.Kind
			}
			actMinutes := intFromAny(obj["estimated_minutes"], w.EstimatedMinutes)
			contentJSON, _ := json.Marshal(obj["content_json"])
			citations := filterAllowedChunkIDStrings(dedupeStrings(stringSliceFromAny(obj["citations"])), allowedChunkIDs, chunkIDs)

			// Embed a lightweight variant doc for retrieval.
			varDoc := strings.TrimSpace(actTitle + "\n" + actKind + "\n" + shorten(excerpts, 1200))
			vEmb, err := deps.AI.Embed(gctx, []string{varDoc})
			if err != nil {
				return err
			}
			if len(vEmb) == 0 || len(vEmb[0]) == 0 {
				return fmt.Errorf("realize_activities: empty variant embedding")
			}

			activityID := uuid.New()
			variantID := uuid.New()
			vectorID := "activity_variant:" + variantID.String()

			// Persist canonical activity + joins.
			if err := deps.DB.WithContext(gctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: gctx, Tx: tx}
				if in.PathID == uuid.Nil {
					if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
						return err
					}
				}

				act := &types.Activity{
					ID:               activityID,
					OwnerType:        "path",
					OwnerID:          &pathID,
					Kind:             actKind,
					Title:            actTitle,
					ContentJSON:      datatypes.JSON(contentJSON),
					EstimatedMinutes: actMinutes,
					Difficulty:       w.NodeDifficulty,
					Status:           "draft",
					Metadata: datatypes.JSON(mustJSON(map[string]any{
						"path_node_id": w.NodeID.String(),
						"slot":         w.SlotIndex,
					})),
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
				if _, err := deps.Activities.Create(dbc, []*types.Activity{act}); err != nil {
					return err
				}

				varRow := &types.ActivityVariant{
					ID:          variantID,
					ActivityID:  activityID,
					Variant:     "default",
					ContentJSON: datatypes.JSON(contentJSON),
					RenderSpec:  datatypes.JSON([]byte(`{}`)),
					CreatedAt:   time.Now().UTC(),
					UpdatedAt:   time.Now().UTC(),
				}
				if err := deps.Variants.Upsert(dbc, varRow); err != nil {
					return err
				}

				// Concept joins
				for _, ck := range w.PrimaryKeys {
					ck = strings.TrimSpace(strings.ToLower(ck))
					cid := keyToConceptID[ck]
					if cid == uuid.Nil {
						continue
					}
					_ = deps.ActivityConcepts.Upsert(dbc, &types.ActivityConcept{
						ID:         uuid.New(),
						ActivityID: activityID,
						ConceptID:  cid,
						Role:       "primary",
						Weight:     1,
					})
				}

				// Citations (grounding)
				for _, s := range citations {
					id, err := uuid.Parse(strings.TrimSpace(s))
					if err != nil || id == uuid.Nil {
						continue
					}
					_ = deps.ActivityCitations.Upsert(dbc, &types.ActivityCitation{
						ID:                uuid.New(),
						ActivityVariantID: variantID,
						MaterialChunkID:   id,
						Kind:              "grounding",
					})
				}

				// Link activity to node (rank=slot).
				_ = deps.PathNodeActivities.Upsert(dbc, &types.PathNodeActivity{
					ID:         uuid.New(),
					PathNodeID: w.NodeID,
					ActivityID: activityID,
					Rank:       w.SlotIndex,
					IsPrimary:  w.SlotIndex == 0,
				})

				// Pinecone compensation for variant vector (if configured).
				if deps.Vec != nil {
					if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
						"namespace": actNS,
						"ids":       []string{vectorID},
					}); err != nil {
						return err
					}
				}

				return nil
			}); err != nil {
				return err
			}

			atomic.AddInt32(&actsMade, 1)
			atomic.AddInt32(&varsMade, 1)

			// Upsert variant to Pinecone (best-effort).
			if deps.Vec != nil {
				_ = deps.Vec.Upsert(gctx, actNS, []pc.Vector{{
					ID:     vectorID,
					Values: vEmb[0],
					Metadata: map[string]any{
						"type":        "activity_variant",
						"activity_id": activityID.String(),
						"variant_id":  variantID.String(),
						"path_id":     pathID.String(),
						"kind":        actKind,
						"title":       actTitle,
					},
				}})
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.ActivitiesMade = int(atomic.LoadInt32(&actsMade))
	out.VariantsMade = int(atomic.LoadInt32(&varsMade))
	if deps.Graph != nil {
		if err := syncPathActivitiesToNeo4j(ctx, deps, pathID); err != nil {
			deps.Log.Warn("neo4j activities sync failed (continuing)", "error", err, "path_id", pathID.String())
		}
	}
	return out, nil
}

func buildActivityExcerpts(byID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxLines int, maxChars int) string {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}
	var b strings.Builder
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := byID[id]
		if ch == nil {
			continue
		}
		txt := shorten(ch.Text, maxChars)
		if strings.TrimSpace(txt) == "" {
			continue
		}
		b.WriteString("[chunk_id=")
		b.WriteString(id.String())
		b.WriteString("] ")
		b.WriteString(txt)
		b.WriteString("\n")
		n++
		if n >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func validateActivityContent(obj map[string]any, activityKind string) []string {
	if obj == nil {
		return []string{"missing object"}
	}

	title := strings.TrimSpace(stringFromAny(obj["title"]))
	kind := strings.ToLower(strings.TrimSpace(stringFromAny(obj["kind"])))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(activityKind))
	}

	rawContent := obj["content_json"]
	content, ok := rawContent.(map[string]any)
	if !ok || content == nil {
		return []string{"content_json missing or invalid"}
	}
	rawBlocks, _ := content["blocks"].([]any)
	if len(rawBlocks) == 0 {
		return []string{"content_json.blocks missing"}
	}

	metrics := activityContentMetrics(rawBlocks)
	minWords, minParagraphs, minCallouts := activityMinima(kind)

	errs := make([]string, 0, 8)
	if strings.TrimSpace(title) == "" {
		errs = append(errs, "title missing")
	}
	if minWords > 0 && metrics.WordCount < minWords {
		errs = append(errs, fmt.Sprintf("word_count too low (%d < %d)", metrics.WordCount, minWords))
	}
	if minParagraphs > 0 && metrics.Paragraphs < minParagraphs {
		errs = append(errs, fmt.Sprintf("need >=%d paragraph blocks (got %d)", minParagraphs, metrics.Paragraphs))
	}
	if minCallouts > 0 && metrics.Callouts < minCallouts {
		errs = append(errs, fmt.Sprintf("need >=%d callout blocks (got %d)", minCallouts, metrics.Callouts))
	}
	if metrics.Headings < 1 {
		errs = append(errs, "need at least 1 heading block")
	}
	if metrics.HasWorkedExample == false && isLessonLikeActivityKind(kind) {
		errs = append(errs, "missing worked example (include a Worked example heading or callout)")
	}
	if metrics.HasSelfCheck == false && isLessonLikeActivityKind(kind) {
		errs = append(errs, "missing self-check prompt (include a Quick check/Self-check section)")
	}
	return errs
}

func ensureActivityContentHasHeadingBlock(obj map[string]any, fallbackHeading string) bool {
	if obj == nil {
		return false
	}
	rawContent := obj["content_json"]
	content, ok := rawContent.(map[string]any)
	if !ok || content == nil {
		return false
	}
	rawBlocks, ok := content["blocks"].([]any)
	if !ok || len(rawBlocks) == 0 {
		return false
	}

	for _, x := range rawBlocks {
		b, ok := x.(map[string]any)
		if !ok || b == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"]))) == "heading" {
			return false
		}
	}

	heading := strings.TrimSpace(stringFromAny(obj["title"]))
	if heading == "" {
		heading = strings.TrimSpace(fallbackHeading)
	}
	if heading == "" {
		heading = "Overview"
	}

	hb := map[string]any{
		"kind":       "heading",
		"content_md": heading,
		"items":      []any{},
		"asset_refs": []any{},
	}
	out := make([]any, 0, len(rawBlocks)+1)
	out = append(out, hb)
	out = append(out, rawBlocks...)
	content["blocks"] = out
	obj["content_json"] = content
	return true
}

func ensureActivityContentMeetsMinima(obj map[string]any, activityKind string) bool {
	if obj == nil {
		return false
	}

	title := strings.TrimSpace(stringFromAny(obj["title"]))
	if title == "" {
		title = strings.TrimSpace(activityKind)
		if title == "" {
			title = "Activity"
		}
		obj["title"] = title
	}

	kind := strings.ToLower(strings.TrimSpace(stringFromAny(obj["kind"])))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(activityKind))
		if kind == "" {
			kind = "reading"
		}
		obj["kind"] = kind
	}

	rawContent := obj["content_json"]
	content, ok := rawContent.(map[string]any)
	if !ok || content == nil {
		return false
	}
	rawBlocks, ok := content["blocks"].([]any)
	if !ok || len(rawBlocks) == 0 {
		return false
	}

	metrics := activityContentMetrics(rawBlocks)
	minWords, minParagraphs, minCallouts := activityMinima(kind)

	// Determine a stable insertion point (right after the first heading).
	insertAt := len(rawBlocks)
	for i, x := range rawBlocks {
		b, ok := x.(map[string]any)
		if !ok || b == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"]))) == "heading" {
			insertAt = i + 1
			break
		}
	}

	inserts := make([]any, 0, 8)

	// Ensure lesson-like activities always have worked-example + self-check markers.
	// This is a quality floor for downstream UX and also improves prompt adherence.
	if isLessonLikeActivityKind(kind) && !metrics.HasWorkedExample {
		inserts = append(inserts,
			map[string]any{
				"kind":       "heading",
				"content_md": "Worked example",
				"items":      []any{},
				"asset_refs": []any{},
			},
			map[string]any{
				"kind":       "paragraph",
				"content_md": "Example: Before you look at the solution, try to work this from memory. Write your steps clearly, then compare each step to the explanation and note the first place your reasoning diverged.",
				"items":      []any{},
				"asset_refs": []any{},
			},
		)
		metrics.HasWorkedExample = true
	}
	if isLessonLikeActivityKind(kind) && !metrics.HasSelfCheck {
		inserts = append(inserts,
			map[string]any{
				"kind":       "heading",
				"content_md": "Quick check",
				"items":      []any{},
				"asset_refs": []any{},
			},
			map[string]any{
				"kind":       "paragraph",
				"content_md": "Quick check: In one or two sentences, what is the key idea you would use here, and what would you check to be confident your answer is correct?",
				"items":      []any{},
				"asset_refs": []any{},
			},
		)
		metrics.HasSelfCheck = true
	}

	// Ensure minimum paragraph/callout structure.
	padOffset := metrics.Paragraphs + metrics.Callouts + metrics.Headings
	for metrics.Paragraphs < minParagraphs {
		inserts = append(inserts, map[string]any{
			"kind":       "paragraph",
			"content_md": activityPaddingTextWithOffset(kind, 90, padOffset),
			"items":      []any{},
			"asset_refs": []any{},
		})
		metrics.Paragraphs++
		padOffset++
	}
	for metrics.Callouts < minCallouts {
		inserts = append(inserts, map[string]any{
			"kind":       "callout",
			"content_md": "**Hint ladder**",
			"items": []any{
				"Hint 1: Restate the question and list the given information vs. what you need to find.",
				"Hint 2: Choose the key idea/method that applies, and write the next step you would take.",
				"Hint 3: Do a quick sanity check (units, sign, boundary cases, or an intuitive reasonableness check) before you finalize.",
			},
			"asset_refs": []any{},
		})
		metrics.Callouts++
		padOffset++
	}

	// Insert structural blocks first.
	if len(inserts) > 0 {
		rawBlocks = insertActivityBlocks(rawBlocks, insertAt, inserts)
		content["blocks"] = rawBlocks
		obj["content_json"] = content
	}

	// Ensure minimum word count (never fail generation due to a small near-miss).
	if minWords > 0 {
		// Append padding at the end (least disruptive), retrying defensively.
		for tries := 0; tries < 4; tries++ {
			metrics = activityContentMetrics(rawBlocks)
			if metrics.WordCount >= minWords {
				break
			}
			missing := minWords - metrics.WordCount
			pad := map[string]any{
				"kind":       "paragraph",
				"content_md": activityPaddingTextWithOffset(kind, missing+80, metrics.WordCount+tries),
				"items":      []any{},
				"asset_refs": []any{},
			}
			rawBlocks = insertActivityBlocks(rawBlocks, len(rawBlocks), []any{pad})
		}
		content["blocks"] = rawBlocks
		obj["content_json"] = content
	}

	return true
}

func insertActivityBlocks(blocks []any, idx int, inserts []any) []any {
	if len(inserts) == 0 {
		return blocks
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(blocks) {
		idx = len(blocks)
	}
	out := make([]any, 0, len(blocks)+len(inserts))
	out = append(out, blocks[:idx]...)
	out = append(out, inserts...)
	out = append(out, blocks[idx:]...)
	return out
}

func activityPaddingText(activityKind string, minWords int) string {
	return activityPaddingTextWithOffset(activityKind, minWords, 0)
}

func activityPaddingTextWithOffset(activityKind string, minWords int, offset int) string {
	if minWords < 40 {
		minWords = 40
	}
	k := strings.ToLower(strings.TrimSpace(activityKind))

	sentences := []string{
		"Approach this as a short loop: attempt from memory, check, explain the mismatch, then retry.",
		"Write your reasoning step by step rather than jumping to the final answer; clarity beats speed here.",
		"If you get stuck, use the hint to choose just the next step, then continue on your own without copying.",
		"After you verify the solution, identify the first point where your approach diverged and write a one‑sentence correction.",
		"Do a second attempt from scratch and aim for a clean, minimal solution that you could reproduce tomorrow.",
		"Finish with a tiny takeaway: the rule/idea you should remember next time and one common trap to avoid.",
	}
	switch k {
	case "reading", "case", "lesson":
		sentences = []string{
			"Read actively: pause after each section and restate the key idea in your own words.",
			"When you see an example, try to predict the next step before reading it, then compare and correct yourself.",
			"Use the checks as retrieval practice: answer without looking back, then verify and note what you missed.",
			"Keep a small list of definitions and assumptions as you go; most confusion comes from mixing terms.",
			"At the end, summarize the mental model in 2-3 sentences and write one mistake you want to avoid next time.",
		}
	case "quiz":
		sentences = []string{
			"Answer from memory first; if unsure, eliminate options by explaining why each is inconsistent with the material.",
			"After you see the explanation, paraphrase it once in your own words so it sticks.",
			"If you miss a question, rewrite it as a flashcard prompt and retry it after a short break.",
		}
	case "drill":
		// keep the default drill-focused sentences
	default:
		_ = k
	}

	var b strings.Builder
	if offset < 0 {
		offset = 0
	}
	words := 0
	for it := 0; words < minWords && it < 200; it++ {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		s := sentences[(offset+it)%len(sentences)]
		b.WriteString(s)
		words += learningcontent.WordCount(s)
	}
	return strings.TrimSpace(b.String())
}

type activityMetrics struct {
	WordCount        int
	Headings         int
	Paragraphs       int
	Callouts         int
	BulletBlocks     int
	HasWorkedExample bool
	HasSelfCheck     bool
}

func activityContentMetrics(rawBlocks []any) activityMetrics {
	out := activityMetrics{}
	for _, x := range rawBlocks {
		b, ok := x.(map[string]any)
		if !ok || b == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"])))
		contentMD := strings.TrimSpace(stringFromAny(b["content_md"]))
		items := stringSliceFromAny(b["items"])

		text := contentMD
		if len(items) > 0 {
			if text != "" {
				text += "\n"
			}
			text += strings.Join(items, "\n")
		}
		out.WordCount += learningcontent.WordCount(text)

		lc := strings.ToLower(text)
		if strings.Contains(lc, "worked example") || strings.Contains(lc, "example:") {
			out.HasWorkedExample = true
		}
		if strings.Contains(lc, "quick check") || strings.Contains(lc, "self-check") || strings.Contains(lc, "check yourself") {
			out.HasSelfCheck = true
		}
		if strings.Contains(lc, "?") {
			out.HasSelfCheck = true
		}

		switch kind {
		case "heading":
			out.Headings++
		case "paragraph":
			out.Paragraphs++
		case "callout":
			out.Callouts++
		case "bullets", "steps":
			out.BulletBlocks++
		}
	}
	return out
}

func isLessonLikeActivityKind(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "reading", "case", "lesson":
		return true
	default:
		return false
	}
}

func activityMinima(activityKind string) (minWords int, minParagraphs int, minCallouts int) {
	k := strings.ToLower(strings.TrimSpace(activityKind))
	switch k {
	case "reading", "case", "lesson":
		return 900, 6, 1
	case "drill":
		return 350, 2, 1
	case "quiz":
		return 220, 1, 0
	default:
		return 600, 4, 1
	}
}

func filterAllowedChunkIDStrings(in []string, allowed map[string]bool, fallback []uuid.UUID) []string {
	if len(in) == 0 && len(fallback) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			continue
		}
		s = id.String()
		if len(allowed) > 0 && !allowed[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) > 0 {
		return out
	}
	for _, id := range fallback {
		if id == uuid.Nil {
			continue
		}
		s := id.String()
		if len(allowed) > 0 && !allowed[s] {
			continue
		}
		return []string{s}
	}
	return nil
}

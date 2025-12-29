package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
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

	Concepts repos.ConceptRepo
	Files    repos.MaterialFileRepo
	Chunks   repos.MaterialChunkRepo

	UserProfile repos.UserProfileVectorRepo

	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type RealizeActivitiesInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
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

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
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
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
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
	for _, c := range concepts {
		if c == nil {
			continue
		}
		keyToConceptID[c.Key] = c.ID
	}

	// Chunks for grounding (Pinecone preferred; local fallback needs embeddings)
	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
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

	chunksNS := index.ChunksNamespace(in.MaterialSetID)
	actNS := index.ActivitiesNamespace("path", &pathID)

	// Iterate nodes/slots deterministically.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	for _, node := range nodes {
		if node == nil || node.ID == uuid.Nil {
			continue
		}

		nodeMeta := map[string]any{}
		if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
			_ = json.Unmarshal(node.Metadata, &nodeMeta)
		}
		nodeGoal := strings.TrimSpace(stringFromAny(nodeMeta["goal"]))
		nodeConceptKeys := dedupeStrings(stringSliceFromAny(nodeMeta["concept_keys"]))

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
				primaryKeys = nodeConceptKeys
			}
			conceptCSV := strings.Join(primaryKeys, ", ")

			queryText := strings.TrimSpace(node.Title + " " + nodeGoal + " " + kind + " " + conceptCSV)
			qEmb, err := deps.AI.Embed(ctx, []string{queryText})
			if err != nil {
				return out, err
			}
			if len(qEmb) == 0 || len(qEmb[0]) == 0 {
				return out, fmt.Errorf("realize_activities: empty query embedding")
			}

			var chunkIDs []uuid.UUID
			if deps.Vec != nil {
				ids, qerr := deps.Vec.QueryIDs(ctx, chunksNS, qEmb[0], 12, map[string]any{"type": "chunk"})
				if qerr == nil && len(ids) > 0 {
					for _, s := range ids {
						if id, e := uuid.Parse(strings.TrimSpace(s)); e == nil && id != uuid.Nil {
							chunkIDs = append(chunkIDs, id)
						}
					}
				}
			}
			if len(chunkIDs) == 0 {
				// Local cosine fallback (requires chunk embeddings).
				if len(embByID) == 0 {
					return out, fmt.Errorf("realize_activities: no local embeddings available (run embed_chunks first)")
				}
				type scored struct {
					ID    uuid.UUID
					Score float64
				}
				scoredArr := make([]scored, 0, len(embByID))
				for id, emb := range embByID {
					scoredArr = append(scoredArr, scored{ID: id, Score: cosineSim(qEmb[0], emb)})
				}
				sort.Slice(scoredArr, func(i, j int) bool { return scoredArr[i].Score > scoredArr[j].Score })
				topK := 12
				if topK > len(scoredArr) {
					topK = len(scoredArr)
				}
				for i := 0; i < topK; i++ {
					chunkIDs = append(chunkIDs, scoredArr[i].ID)
				}
			}

			excerpts := buildActivityExcerpts(chunkByID, chunkIDs, 12, 700)
			if strings.TrimSpace(excerpts) == "" {
				return out, fmt.Errorf("realize_activities: empty grounding excerpts")
			}

			// Prompt: canonical activity content.
			titleHint := strings.TrimSpace(kind + ": " + node.Title)
			p, err := prompts.Build(prompts.PromptActivityContent, prompts.Input{
				UserProfileDoc:   up.ProfileDoc,
				PathCharterJSON:  charterJSON,
				ActivityKind:     kind,
				ActivityTitle:    titleHint,
				ConceptKeysCSV:   conceptCSV,
				ActivityExcerpts: excerpts,
			})
			if err != nil {
				return out, err
			}
			obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
			if err != nil {
				return out, err
			}

			actTitle := strings.TrimSpace(stringFromAny(obj["title"]))
			if actTitle == "" {
				actTitle = titleHint
			}
			actKind := strings.TrimSpace(stringFromAny(obj["kind"]))
			if actKind == "" {
				actKind = kind
			}
			actMinutes := intFromAny(obj["estimated_minutes"], estimatedMinutes)
			contentJSON, _ := json.Marshal(obj["content_json"])
			citations := dedupeStrings(stringSliceFromAny(obj["citations"]))

			// Embed a lightweight variant doc for retrieval.
			varDoc := strings.TrimSpace(actTitle + "\n" + actKind + "\n" + shorten(excerpts, 1200))
			vEmb, err := deps.AI.Embed(ctx, []string{varDoc})
			if err != nil {
				return out, err
			}
			if len(vEmb) == 0 || len(vEmb[0]) == 0 {
				return out, fmt.Errorf("realize_activities: empty variant embedding")
			}

			activityID := uuid.New()
			variantID := uuid.New()
			vectorID := "activity_variant:" + variantID.String()

			// Persist canonical activity + joins.
			if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: ctx, Tx: tx}
				if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
					return err
				}

				act := &types.Activity{
					ID:               activityID,
					OwnerType:        "path",
					OwnerID:          &pathID,
					Kind:             actKind,
					Title:            actTitle,
					ContentJSON:      datatypes.JSON(contentJSON),
					EstimatedMinutes: actMinutes,
					Difficulty:       strings.TrimSpace(stringFromAny(nodeMeta["difficulty"])),
					Status:           "draft",
					Metadata: datatypes.JSON(mustJSON(map[string]any{
						"path_node_id": node.ID.String(),
						"slot":         slotIndex,
					})),
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
				if _, err := deps.Activities.Create(dbc, []*types.Activity{act}); err != nil {
					return err
				}
				out.ActivitiesMade++

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
				out.VariantsMade++

				// Concept joins
				for _, ck := range primaryKeys {
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
					PathNodeID: node.ID,
					ActivityID: activityID,
					Rank:       slotIndex,
					IsPrimary:  slotIndex == 0,
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
				return out, err
			}

			// Upsert variant to Pinecone (best-effort).
			if deps.Vec != nil {
				_ = deps.Vec.Upsert(ctx, actNS, []pc.Vector{{
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

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
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type MaterialSetSummarizeDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Summaries repos.MaterialSetSummaryRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
	Artifacts repos.LearningArtifactRepo
}

type MaterialSetSummarizeInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type MaterialSetSummarizeOutput struct {
	SummaryID uuid.UUID `json:"summary_id"`
	VectorID  string    `json:"vector_id"`
	CacheHit  bool      `json:"cache_hit,omitempty"`
}

func MaterialSetSummarize(ctx context.Context, deps MaterialSetSummarizeDeps, in MaterialSetSummarizeInput) (MaterialSetSummarizeOutput, error) {
	out := MaterialSetSummarizeOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Summaries == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("material_set_summarize: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing saga_id")
	}

	// Idempotency: if we already have a stable summary + embedding, don't regenerate.
	var existing *types.MaterialSetSummary
	if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 {
		existing = rows[0]
	}

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
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	if len(chunks) == 0 {
		return out, fmt.Errorf("material_set_summarize: no chunks for material set")
	}

	// Optional: use per-file intents (if already computed) to align set-level summary/intent.
	var intentRows []*types.MaterialIntent
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialIntent{}).Where("material_file_id IN ?", fileIDs).Find(&intentRows).Error; err != nil {
		intentRows = nil
	}
	intentsList := make([]map[string]any, 0, len(intentRows))
	intentFP := make([]map[string]any, 0, len(intentRows))
	fileIDSet := map[string]bool{}
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDSet[f.ID.String()] = true
		}
	}
	for _, it := range intentRows {
		if it == nil || it.MaterialFileID == uuid.Nil {
			continue
		}
		id := it.MaterialFileID.String()
		intentsList = append(intentsList, map[string]any{
			"file_id":               id,
			"from_state":            it.FromState,
			"to_state":              it.ToState,
			"core_thread":           it.CoreThread,
			"destination_concepts":  jsonListFromRaw(it.DestinationConcepts),
			"prerequisite_concepts": jsonListFromRaw(it.PrerequisiteConcepts),
			"assumed_knowledge":     jsonListFromRaw(it.AssumedKnowledge),
		})
		intentFP = append(intentFP, map[string]any{
			"file_id":    id,
			"updated_at": it.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	if len(intentFP) > 0 {
		sort.Slice(intentFP, func(i, j int) bool {
			return stringFromAny(intentFP[i]["file_id"]) < stringFromAny(intentFP[j]["file_id"])
		})
	}
	intentsJSON := ""
	if len(intentsList) > 0 {
		if b, err := json.Marshal(map[string]any{"files": intentsList}); err == nil {
			intentsJSON = string(b)
		}
	}

	summaryReady := existing != nil && strings.TrimSpace(existing.SummaryMD) != "" && !embeddingMissing(existing.Embedding) && strings.TrimSpace(existing.VectorID) != ""
	var summarizeInputHash string
	if deps.Artifacts != nil && artifactCacheEnabled() {
		payload := map[string]any{
			"files":   filesFingerprint(files),
			"chunks":  chunksFingerprint(chunks),
			"intents": intentFP,
			"env":     envSnapshot([]string{"MATERIAL_SET_SUMMARIZE_"}, []string{"OPENAI_MODEL"}),
		}
		if h, err := computeArtifactHash("material_set_summarize", in.MaterialSetID, uuid.Nil, payload); err == nil {
			summarizeInputHash = h
		}
		if summaryReady && summarizeInputHash != "" {
			if _, hit, err := artifactCacheGet(ctx, deps.Artifacts, in.OwnerUserID, in.MaterialSetID, uuid.Nil, "material_set_summarize", summarizeInputHash); err == nil && hit {
				out.SummaryID = existing.ID
				out.VectorID = existing.VectorID
				out.CacheHit = true
				return out, nil
			}
			if artifactCacheSeedExisting() {
				maxChunkUpdated := maxChunkUpdatedAt(chunks)
				if maxChunkUpdated.IsZero() || !existing.UpdatedAt.Before(maxChunkUpdated) {
					_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
						OwnerUserID:   in.OwnerUserID,
						MaterialSetID: in.MaterialSetID,
						PathID:        uuid.Nil,
						ArtifactType:  "material_set_summarize",
						InputHash:     summarizeInputHash,
						Version:       artifactHashVersion,
						Metadata: marshalMeta(map[string]any{
							"summary_id": existing.ID.String(),
							"seeded":     true,
						}),
					})
					out.SummaryID = existing.ID
					out.VectorID = existing.VectorID
					out.CacheHit = true
					return out, nil
				}
			}
		}
	}

	excerpt := stratifiedChunkExcerpts(chunks, 12, 700)
	if strings.TrimSpace(excerpt) == "" {
		return out, fmt.Errorf("material_set_summarize: empty excerpt")
	}

	p, err := prompts.Build(prompts.PromptMaterialSetSummary, prompts.Input{
		BundleExcerpt:       excerpt,
		MaterialIntentsJSON: strings.TrimSpace(intentsJSON),
	})
	if err != nil {
		return out, err
	}

	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}

	subject := stringFromAny(obj["subject"])
	level := stringFromAny(obj["level"])
	summaryMD := stringFromAny(obj["summary_md"])
	tags := dedupeStrings(stringSliceFromAny(obj["tags"]))
	conceptKeys := dedupeStrings(stringSliceFromAny(obj["concept_keys"]))

	var setIntentRow *types.MaterialSetIntent
	if envBool("MATERIAL_SET_SUMMARY_INTENT_ENABLED", true) {
		if si := mapFromAny(obj["set_intent"]); si != nil {
			filterFileIDs := func(ids []string) []string {
				out := make([]string, 0, len(ids))
				for _, id := range ids {
					id = strings.TrimSpace(id)
					if id == "" {
						continue
					}
					if fileIDSet[id] {
						out = append(out, id)
					}
				}
				return dedupeStrings(out)
			}

			fromState := strings.TrimSpace(stringFromAny(si["from_state"]))
			toState := strings.TrimSpace(stringFromAny(si["to_state"]))
			coreThread := strings.TrimSpace(stringFromAny(si["core_thread"]))
			spineIDs := filterFileIDs(dedupeStrings(stringSliceFromAny(si["spine_file_ids"])))
			satIDs := filterFileIDs(dedupeStrings(stringSliceFromAny(si["satellite_file_ids"])))
			gaps := dedupeStrings(stringSliceFromAny(si["gaps_concept_keys"]))
			redundancy := dedupeStrings(stringSliceFromAny(si["redundancy_notes"]))
			conflicts := dedupeStrings(stringSliceFromAny(si["conflict_notes"]))

			if fromState != "" || toState != "" || coreThread != "" || len(spineIDs) > 0 || len(satIDs) > 0 || len(gaps) > 0 {
				meta := map[string]any{
					"source":     "material_set_summarize",
					"summary_id": "",
				}
				if v := si["edge_hints"]; v != nil {
					meta["edge_hints"] = v
				}
				setIntentRow = &types.MaterialSetIntent{
					ID:                       uuid.New(),
					MaterialSetID:            in.MaterialSetID,
					FromState:                fromState,
					ToState:                  toState,
					CoreThread:               coreThread,
					SpineMaterialFileIDs:     datatypes.JSON(mustJSON(spineIDs)),
					SatelliteMaterialFileIDs: datatypes.JSON(mustJSON(satIDs)),
					GapsConceptKeys:          datatypes.JSON(mustJSON(gaps)),
					RedundancyNotes:          datatypes.JSON(mustJSON(redundancy)),
					ConflictNotes:            datatypes.JSON(mustJSON(conflicts)),
					Metadata:                 datatypes.JSON(mustJSON(meta)),
					CreatedAt:                time.Now().UTC(),
					UpdatedAt:                time.Now().UTC(),
				}
			}
		}
	}

	vecDoc := strings.TrimSpace(summaryMD)
	if vecDoc == "" {
		vecDoc = strings.TrimSpace(subject + " " + level)
	}

	embs, err := deps.AI.Embed(ctx, []string{vecDoc})
	if err != nil {
		return out, err
	}
	if len(embs) == 0 || len(embs[0]) == 0 {
		return out, fmt.Errorf("material_set_summarize: empty embedding")
	}
	embJSON := mustJSON(embs[0])

	vectorID := "material_set_summary:" + in.MaterialSetID.String()
	ns := index.MaterialSetSummariesNamespace(in.OwnerUserID)

	now := time.Now().UTC()
	row := &types.MaterialSetSummary{
		ID:            uuid.New(),
		MaterialSetID: in.MaterialSetID,
		UserID:        in.OwnerUserID,
		Subject:       subject,
		Level:         level,
		SummaryMD:     summaryMD,
		Tags:          datatypes.JSON(mustJSON(tags)),
		ConceptKeys:   datatypes.JSON(mustJSON(conceptKeys)),
		Embedding:     datatypes.JSON(embJSON),
		VectorID:      vectorID,
		UpdatedAt:     now,
	}
	if existing != nil && existing.ID != uuid.Nil {
		row.ID = existing.ID
	}

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		// Contract: derive/ensure path_id via bootstrap when the caller didn't supply one.
		if in.PathID == uuid.Nil {
			if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
				return err
			}
		}

		if err := deps.Summaries.UpsertByMaterialSetID(dbc, row); err != nil {
			return err
		}
		if setIntentRow != nil {
			meta := map[string]any{}
			_ = json.Unmarshal(setIntentRow.Metadata, &meta)
			meta["summary_id"] = row.ID.String()
			setIntentRow.Metadata = datatypes.JSON(mustJSON(meta))
			setIntentRow.MaterialSetID = in.MaterialSetID
			setIntentRow.UpdatedAt = time.Now().UTC()
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "material_set_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"from_state",
					"to_state",
					"core_thread",
					"spine_material_file_ids",
					"satellite_material_file_ids",
					"gaps_concept_keys",
					"redundancy_notes",
					"conflict_notes",
					"metadata",
					"updated_at",
				}),
			}).Create(setIntentRow).Error; err != nil {
				return err
			}
		}

		if deps.Vec != nil {
			if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
				"namespace": ns,
				"ids":       []string{vectorID},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return out, err
	}

	out.SummaryID = row.ID
	out.VectorID = vectorID

	// Pinecone is a retrieval cache; failures should not block canonical state.
	if deps.Vec != nil {
		_ = deps.Vec.Upsert(ctx, ns, []pc.Vector{{
			ID:     vectorID,
			Values: embs[0],
			Metadata: map[string]any{
				"type":            "material_set_summary",
				"user_id":         in.OwnerUserID.String(),
				"material_set_id": in.MaterialSetID.String(),
				"subject":         subject,
				"level":           level,
			},
		}})
	}

	if summarizeInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
		_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
			OwnerUserID:   in.OwnerUserID,
			MaterialSetID: in.MaterialSetID,
			PathID:        uuid.Nil,
			ArtifactType:  "material_set_summarize",
			InputHash:     summarizeInputHash,
			Version:       artifactHashVersion,
			Metadata: marshalMeta(map[string]any{
				"summary_id": row.ID.String(),
			}),
		})
	}

	return out, nil
}

package steps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type MaterialKGBuildDeps struct {
	DB     *gorm.DB
	Log    *logger.Logger
	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo
	Path   repos.PathRepo

	Concepts repos.ConceptRepo

	Graph *neo4jdb.Client
	AI    openai.Client

	Bootstrap services.LearningBuildBootstrapService
}

type MaterialKGBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type MaterialKGBuildOutput struct {
	PathID uuid.UUID `json:"path_id"`

	EntitiesUpserted int `json:"entities_upserted"`
	ClaimsUpserted   int `json:"claims_upserted"`

	ChunkEntityEdges  int `json:"chunk_entity_edges"`
	ChunkClaimEdges   int `json:"chunk_claim_edges"`
	ClaimEntityEdges  int `json:"claim_entity_edges"`
	ClaimConceptEdges int `json:"claim_concept_edges"`

	Skipped bool           `json:"skipped"`
	Trace   map[string]any `json:"trace,omitempty"`
}

func MaterialKGBuild(ctx context.Context, deps MaterialKGBuildDeps, in MaterialKGBuildInput) (MaterialKGBuildOutput, error) {
	out := MaterialKGBuildOutput{Trace: map[string]any{}}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Path == nil || deps.Concepts == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("material_kg_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("material_kg_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("material_kg_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("material_kg_build: missing saga_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Derived material sets share KG products with their source upload batch (no duplication).
	setCtx, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	kgSetID := setCtx.SourceMaterialSetID

	// Idempotency: if we already have entities/claims, skip LLM extraction and just ensure Neo4j sync.
	var (
		existingEntities int64
		existingClaims   int64
	)
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialEntity{}).Where("material_set_id = ?", kgSetID).Count(&existingEntities).Error
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialClaim{}).Where("material_set_id = ?", kgSetID).Count(&existingClaims).Error
	out.Trace["existing_entities"] = existingEntities
	out.Trace["existing_claims"] = existingClaims
	force := strings.EqualFold(strings.TrimSpace(os.Getenv("MATERIAL_KG_FORCE_REBUILD")), "true")
	if !force && (existingEntities > 0 || existingClaims > 0) {
		out.Skipped = true
		if deps.Graph != nil && deps.Graph.Driver != nil {
			if err := syncMaterialKGToNeo4j(ctx, deps, kgSetID); err != nil {
				deps.Log.Warn("neo4j material kg sync failed (continuing)", "error", err, "material_set_id", kgSetID.String())
			}
		}
		return out, nil
	}

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, kgSetID)
	if err != nil {
		return out, err
	}
	if len(files) == 0 {
		return out, fmt.Errorf("material_kg_build: no files for material set")
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
		return out, fmt.Errorf("material_kg_build: no chunks for material set")
	}

	allowedChunkIDs := map[string]bool{}
	for _, ch := range chunks {
		if ch != nil && ch.ID != uuid.Nil {
			allowedChunkIDs[ch.ID.String()] = true
		}
	}

	// Concepts (for claim->concept linking). We keep the prompt lightweight: key + name only.
	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	conceptByKey := map[string]*types.Concept{}
	type conceptBrief struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	conceptSumm := make([]conceptBrief, 0, len(concepts))
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(c.Key)
		if k == "" {
			continue
		}
		conceptByKey[k] = c
		conceptSumm = append(conceptSumm, conceptBrief{Key: k, Name: strings.TrimSpace(c.Name)})
	}
	sort.Slice(conceptSumm, func(i, j int) bool { return conceptSumm[i].Key < conceptSumm[j].Key })
	conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": conceptSumm})

	// Build compact excerpts for extraction.
	perFile := envIntAllowZero("MATERIAL_KG_EXCERPTS_PER_FILE", 14)
	excerptMaxChars := envIntAllowZero("MATERIAL_KG_EXCERPT_MAX_CHARS", 720)
	excerptMaxLines := envIntAllowZero("MATERIAL_KG_EXCERPT_MAX_LINES", 0)
	excerptMaxTotal := envIntAllowZero("MATERIAL_KG_EXCERPT_MAX_TOTAL_CHARS", 0)
	excerpts, excerptChunkIDs := stratifiedChunkExcerptsWithLimitsAndIDs(chunks, perFile, excerptMaxChars, excerptMaxLines, excerptMaxTotal)
	if strings.TrimSpace(excerpts) == "" {
		return out, fmt.Errorf("material_kg_build: empty excerpts")
	}
	out.Trace["excerpt_chunks"] = len(excerptChunkIDs)

	// Optional: incorporate intake context (from path_intake) to bias entity/claim extraction.
	intentMD := ""
	if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 && string(row.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(row.Metadata, &meta) == nil {
			intentMD = strings.TrimSpace(stringFromAny(meta["intake_md"]))
		}
	}

	p, err := prompts.Build(prompts.PromptMaterialKGExtract, prompts.Input{
		PathIntentMD: intentMD,
		ConceptsJSON: string(conceptsJSONBytes),
		Excerpts:     excerpts,
	})
	if err != nil {
		return out, err
	}

	start := time.Now()
	obj, gerr := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	out.Trace["llm_ms"] = time.Since(start).Milliseconds()
	if gerr != nil {
		// Derived stage: allow the pipeline to continue without GraphRAG enrichment.
		out.Trace["llm_err"] = gerr.Error()
		if deps.Graph != nil && deps.Graph.Driver != nil {
			if err := syncMaterialKGToNeo4j(ctx, deps, kgSetID); err != nil {
				deps.Log.Warn("neo4j material kg sync failed (continuing)", "error", err, "material_set_id", kgSetID.String())
			}
		}
		return out, nil
	}

	entitiesAny, _ := obj["entities"].([]any)
	claimsAny, _ := obj["claims"].([]any)

	now := time.Now().UTC()

	entityByKey := map[string]*types.MaterialEntity{}
	entityRows := make([]*types.MaterialEntity, 0, len(entitiesAny))
	chunkEntityRows := make([]*types.MaterialChunkEntity, 0, 64)

	addEntity := func(name, etype, desc string, aliases []string, evidenceChunkIDs []string, implicit bool) *types.MaterialEntity {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil
		}
		key := normalizeMaterialEntityKey(name)
		if key == "" {
			return nil
		}
		if existing := entityByKey[key]; existing != nil {
			return existing
		}

		meta := map[string]any{
			"evidence_chunk_ids": filterAllowedChunkIDs(evidenceChunkIDs, allowedChunkIDs),
		}
		if implicit {
			meta["implicit"] = true
		}
		metaJSON, _ := json.Marshal(meta)
		aliasesJSON, _ := json.Marshal(dedupeStrings(aliases))

		id := deterministicUUID("material_entity|" + kgSetID.String() + "|" + key)
		row := &types.MaterialEntity{
			ID:            id,
			MaterialSetID: kgSetID,
			Key:           key,
			Name:          name,
			Type:          strings.TrimSpace(etype),
			Description:   strings.TrimSpace(desc),
			Aliases:       datatypes.JSON(aliasesJSON),
			Metadata:      datatypes.JSON(metaJSON),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if row.Type == "" {
			row.Type = "unknown"
		}
		entityByKey[key] = row
		entityRows = append(entityRows, row)

		// Chunk edges (mentions).
		for _, cid := range meta["evidence_chunk_ids"].([]string) {
			chunkID, err := uuid.Parse(strings.TrimSpace(cid))
			if err != nil || chunkID == uuid.Nil {
				continue
			}
			edgeID := deterministicUUID("material_chunk_entity|" + chunkID.String() + "|" + row.ID.String())
			chunkEntityRows = append(chunkEntityRows, &types.MaterialChunkEntity{
				ID:               edgeID,
				MaterialChunkID:  chunkID,
				MaterialEntityID: row.ID,
				Relation:         "mentions",
				Weight:           1,
				CreatedAt:        now,
				UpdatedAt:        now,
			})
		}

		return row
	}

	for _, ea := range entitiesAny {
		m, _ := ea.(map[string]any)
		name := strings.TrimSpace(asString(m["name"]))
		if name == "" {
			continue
		}
		etype := strings.TrimSpace(asString(m["type"]))
		desc := strings.TrimSpace(asString(m["description"]))
		aliases := stringSliceFromAny(m["aliases"])
		ev := stringSliceFromAny(m["evidence_chunk_ids"])
		_ = addEntity(name, etype, desc, aliases, ev, false)
	}

	claimRows := make([]*types.MaterialClaim, 0, len(claimsAny))
	chunkClaimRows := make([]*types.MaterialChunkClaim, 0, 96)
	claimEntityRows := make([]*types.MaterialClaimEntity, 0, 96)
	claimConceptRows := make([]*types.MaterialClaimConcept, 0, 96)

	for _, ca := range claimsAny {
		m, _ := ca.(map[string]any)
		content := strings.TrimSpace(asString(m["content"]))
		if content == "" {
			continue
		}
		kind := strings.TrimSpace(asString(m["kind"]))
		if kind == "" {
			kind = "claim"
		}
		conf := floatFromAny(m["confidence"], 0.7)
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}

		ev := filterAllowedChunkIDs(stringSliceFromAny(m["evidence_chunk_ids"]), allowedChunkIDs)
		entityNames := dedupeStrings(stringSliceFromAny(m["entity_names"]))
		conceptKeys := dedupeStrings(stringSliceFromAny(m["concept_keys"]))

		ckey := materialClaimKey(content)
		claimID := deterministicUUID("material_claim|" + kgSetID.String() + "|" + ckey)

		metaJSON, _ := json.Marshal(map[string]any{
			"evidence_chunk_ids": ev,
			"entity_names":       entityNames,
			"concept_keys":       conceptKeys,
		})

		claim := &types.MaterialClaim{
			ID:            claimID,
			MaterialSetID: kgSetID,
			Key:           ckey,
			Kind:          kind,
			Content:       content,
			Confidence:    conf,
			Metadata:      datatypes.JSON(metaJSON),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		claimRows = append(claimRows, claim)

		// Claim evidence edges (chunk -> claim).
		for _, cid := range ev {
			chunkID, err := uuid.Parse(strings.TrimSpace(cid))
			if err != nil || chunkID == uuid.Nil {
				continue
			}
			edgeID := deterministicUUID("material_chunk_claim|" + chunkID.String() + "|" + claimID.String())
			chunkClaimRows = append(chunkClaimRows, &types.MaterialChunkClaim{
				ID:              edgeID,
				MaterialChunkID: chunkID,
				MaterialClaimID: claimID,
				Relation:        "supports",
				Weight:          1,
				CreatedAt:       now,
				UpdatedAt:       now,
			})
		}

		// Claim -> entities (create implicit entities if needed).
		for _, en := range entityNames {
			ent := addEntity(en, "unknown", "", nil, nil, true)
			if ent == nil || ent.ID == uuid.Nil {
				continue
			}
			edgeID := deterministicUUID("material_claim_entity|" + claimID.String() + "|" + ent.ID.String())
			claimEntityRows = append(claimEntityRows, &types.MaterialClaimEntity{
				ID:               edgeID,
				MaterialClaimID:  claimID,
				MaterialEntityID: ent.ID,
				Relation:         "about",
				Weight:           1,
				CreatedAt:        now,
				UpdatedAt:        now,
			})
		}

		// Claim -> concepts (strict allowlist by concept key).
		for _, k := range conceptKeys {
			k = strings.TrimSpace(k)
			c := conceptByKey[k]
			if c == nil || c.ID == uuid.Nil {
				continue
			}
			edgeID := deterministicUUID("material_claim_concept|" + claimID.String() + "|" + c.ID.String())
			claimConceptRows = append(claimConceptRows, &types.MaterialClaimConcept{
				ID:              edgeID,
				MaterialClaimID: claimID,
				ConceptID:       c.ID,
				Relation:        "about",
				Weight:          1,
				CreatedAt:       now,
				UpdatedAt:       now,
			})
		}
	}

	// Cross-set entity resolution (user-level global entities).
	var globalEntityRows []*types.GlobalEntity
	if len(entityRows) > 0 {
		if globals, stats, gerr := resolveGlobalEntities(ctx, deps, in.OwnerUserID, entityRows); gerr == nil {
			globalEntityRows = globals
			if len(stats) > 0 {
				out.Trace["global_entities"] = stats
			}
		} else if deps.Log != nil {
			deps.Log.Warn("material_kg_build: global entity resolution failed (continuing)", "error", gerr, "user_id", in.OwnerUserID.String())
		}
	}

	// Persist (idempotent upserts).
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_ = advisoryXactLock(tx, "material_kg_build", kgSetID)

		if len(globalEntityRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"name",
					"type",
					"description",
					"aliases",
					"embedding",
					"metadata",
					"updated_at",
				}),
			}).Create(&globalEntityRows).Error; err != nil {
				return err
			}
		}

		if len(entityRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"material_set_id",
					"global_entity_id",
					"key",
					"name",
					"type",
					"description",
					"aliases",
					"metadata",
					"updated_at",
				}),
			}).Create(&entityRows).Error; err != nil {
				return err
			}
		}

		if len(claimRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"material_set_id",
					"key",
					"kind",
					"content",
					"confidence",
					"metadata",
					"updated_at",
				}),
			}).Create(&claimRows).Error; err != nil {
				return err
			}
		}

		// Join tables.
		upsertJoin := func(rows any) error {
			return tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"relation",
					"weight",
					"updated_at",
				}),
			}).Create(rows).Error
		}
		if len(chunkEntityRows) > 0 {
			if err := upsertJoin(&chunkEntityRows); err != nil {
				return err
			}
		}
		if len(chunkClaimRows) > 0 {
			if err := upsertJoin(&chunkClaimRows); err != nil {
				return err
			}
		}
		if len(claimEntityRows) > 0 {
			if err := upsertJoin(&claimEntityRows); err != nil {
				return err
			}
		}
		if len(claimConceptRows) > 0 {
			if err := upsertJoin(&claimConceptRows); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return out, err
	}

	out.EntitiesUpserted = len(entityRows)
	out.ClaimsUpserted = len(claimRows)
	out.ChunkEntityEdges = len(chunkEntityRows)
	out.ChunkClaimEdges = len(chunkClaimRows)
	out.ClaimEntityEdges = len(claimEntityRows)
	out.ClaimConceptEdges = len(claimConceptRows)

	// Neo4j sync (best-effort; derived).
	if deps.Graph != nil && deps.Graph.Driver != nil {
		if err := graphstore.UpsertMaterialEntitiesClaimsGraph(ctx, deps.Graph, deps.Log, kgSetID, entityRows, claimRows, chunkEntityRows, chunkClaimRows, claimEntityRows, claimConceptRows); err != nil {
			deps.Log.Warn("neo4j material kg upsert failed (continuing)", "error", err, "material_set_id", kgSetID.String())
		}
	}

	return out, nil
}

func syncMaterialKGToNeo4j(ctx context.Context, deps MaterialKGBuildDeps, materialSetID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if deps.DB == nil || materialSetID == uuid.Nil {
		return nil
	}
	var (
		entities []*types.MaterialEntity
		claims   []*types.MaterialClaim
	)
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialEntity{}).Where("material_set_id = ?", materialSetID).Find(&entities).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialClaim{}).Where("material_set_id = ?", materialSetID).Find(&claims).Error; err != nil {
		return err
	}

	// Join tables are scoped indirectly via entity/claim IDs; load and filter in-memory.
	var (
		chEnt []*types.MaterialChunkEntity
		chCl  []*types.MaterialChunkClaim
		clEnt []*types.MaterialClaimEntity
		clCon []*types.MaterialClaimConcept
	)
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialChunkEntity{}).Find(&chEnt).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialChunkClaim{}).Find(&chCl).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialClaimEntity{}).Find(&clEnt).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialClaimConcept{}).Find(&clCon).Error; err != nil {
		return err
	}

	entSet := map[uuid.UUID]bool{}
	for _, e := range entities {
		if e != nil && e.ID != uuid.Nil {
			entSet[e.ID] = true
		}
	}
	claimSet := map[uuid.UUID]bool{}
	for _, c := range claims {
		if c != nil && c.ID != uuid.Nil {
			claimSet[c.ID] = true
		}
	}

	filteredChEnt := make([]*types.MaterialChunkEntity, 0, len(chEnt))
	for _, r := range chEnt {
		if r != nil && entSet[r.MaterialEntityID] {
			filteredChEnt = append(filteredChEnt, r)
		}
	}
	filteredChCl := make([]*types.MaterialChunkClaim, 0, len(chCl))
	for _, r := range chCl {
		if r != nil && claimSet[r.MaterialClaimID] {
			filteredChCl = append(filteredChCl, r)
		}
	}
	filteredClEnt := make([]*types.MaterialClaimEntity, 0, len(clEnt))
	for _, r := range clEnt {
		if r != nil && claimSet[r.MaterialClaimID] && entSet[r.MaterialEntityID] {
			filteredClEnt = append(filteredClEnt, r)
		}
	}
	filteredClCon := make([]*types.MaterialClaimConcept, 0, len(clCon))
	for _, r := range clCon {
		if r != nil && claimSet[r.MaterialClaimID] {
			filteredClCon = append(filteredClCon, r)
		}
	}

	return graphstore.UpsertMaterialEntitiesClaimsGraph(ctx, deps.Graph, deps.Log, materialSetID, entities, claims, filteredChEnt, filteredChCl, filteredClEnt, filteredClCon)
}

func resolveGlobalEntities(ctx context.Context, deps MaterialKGBuildDeps, userID uuid.UUID, entities []*types.MaterialEntity) ([]*types.GlobalEntity, map[string]any, error) {
	stats := map[string]any{}
	if deps.DB == nil || deps.AI == nil || userID == uuid.Nil || len(entities) == 0 {
		return nil, stats, nil
	}

	var globals []*types.GlobalEntity
	if err := deps.DB.WithContext(ctx).Model(&types.GlobalEntity{}).Where("user_id = ?", userID).Find(&globals).Error; err != nil {
		return nil, stats, err
	}
	stats["existing_globals"] = len(globals)

	globalByKey := map[string]*types.GlobalEntity{}
	globalByAlias := map[string]*types.GlobalEntity{}
	updatedGlobals := map[uuid.UUID]*types.GlobalEntity{}
	missingGlobals := make([]*types.GlobalEntity, 0)

	for _, g := range globals {
		if g == nil || g.ID == uuid.Nil {
			continue
		}
		key := normalizeGlobalEntityKey(g.Key)
		if key == "" {
			key = normalizeGlobalEntityKey(g.Name)
		}
		if key != "" && globalByKey[key] == nil {
			globalByKey[key] = g
		}
		aliases := mergeAliases(aliasesFromJSON(g.Aliases), g.Name, g.Key)
		addAliasesToMap(globalByAlias, g, aliases)
		if embeddingMissing(g.Embedding) {
			missingGlobals = append(missingGlobals, g)
		}
	}

	if len(missingGlobals) > 0 {
		docs := make([]string, len(missingGlobals))
		for i, g := range missingGlobals {
			docs[i] = globalEntityEmbeddingDoc(g)
		}
		batchSize := envIntAllowZero("GLOBAL_ENTITY_EMBED_BATCH_SIZE", 64)
		conc := envIntAllowZero("GLOBAL_ENTITY_EMBED_CONCURRENCY", 4)
		embs, err := embedTextBatches(ctx, deps.AI, docs, batchSize, conc)
		if err != nil {
			return nil, stats, err
		}
		if len(embs) != len(missingGlobals) {
			return nil, stats, fmt.Errorf("material_kg_build: global embedding count mismatch")
		}
		for i, g := range missingGlobals {
			if g == nil || g.ID == uuid.Nil {
				continue
			}
			if len(embs[i]) == 0 {
				continue
			}
			g.Embedding = datatypes.JSON(mustJSON(embs[i]))
			updatedGlobals[g.ID] = g
		}
		stats["embedded_globals"] = len(missingGlobals)
	}

	type globalEmb struct {
		Entity *types.GlobalEntity
		Emb    []float32
	}
	globalEmbeds := make([]globalEmb, 0, len(globals))
	for _, g := range globals {
		if g == nil || g.ID == uuid.Nil {
			continue
		}
		if emb, ok := decodeEmbedding(g.Embedding); ok && len(emb) > 0 {
			globalEmbeds = append(globalEmbeds, globalEmb{Entity: g, Emb: emb})
		}
	}
	stats["globals_with_embeddings"] = len(globalEmbeds)

	matchedKey := 0
	matchedAlias := 0
	candidateEnts := make([]*types.MaterialEntity, 0)
	candidateDocs := make([]string, 0)
	entityKey := map[uuid.UUID]string{}

	for _, e := range entities {
		if e == nil || e.ID == uuid.Nil {
			continue
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = strings.TrimSpace(e.Key)
		}
		key := normalizeGlobalEntityKey(name)
		if key == "" {
			key = normalizeGlobalEntityKey(e.Key)
		}
		if key == "" {
			continue
		}
		entityKey[e.ID] = key
		if g := globalByKey[key]; g != nil {
			linkEntityToGlobal(e, g, updatedGlobals, globalByAlias)
			matchedKey++
			continue
		}
		if g := globalByAlias[key]; g != nil {
			linkEntityToGlobal(e, g, updatedGlobals, globalByAlias)
			matchedAlias++
			continue
		}
		candidateEnts = append(candidateEnts, e)
		candidateDocs = append(candidateDocs, materialEntityEmbeddingDoc(e))
	}
	stats["matched_key"] = matchedKey
	stats["matched_alias"] = matchedAlias

	candidateEmb := map[uuid.UUID][]float32{}
	if len(candidateEnts) > 0 {
		batchSize := envIntAllowZero("GLOBAL_ENTITY_EMBED_BATCH_SIZE", 64)
		conc := envIntAllowZero("GLOBAL_ENTITY_EMBED_CONCURRENCY", 4)
		embs, err := embedTextBatches(ctx, deps.AI, candidateDocs, batchSize, conc)
		if err != nil {
			return nil, stats, err
		}
		if len(embs) != len(candidateEnts) {
			return nil, stats, fmt.Errorf("material_kg_build: entity embedding count mismatch")
		}
		for i, e := range candidateEnts {
			if e == nil || e.ID == uuid.Nil {
				continue
			}
			candidateEmb[e.ID] = embs[i]
		}
		stats["embedded_entities"] = len(candidateEnts)
	}

	baseThreshold := envFloatAllowZero("GLOBAL_ENTITY_SIM_THRESHOLD", 0.88)
	if baseThreshold <= 0 {
		baseThreshold = 0.88
	}
	stats["global_entity_threshold"] = baseThreshold

	matchedEmb := 0
	created := 0

	for _, e := range candidateEnts {
		if e == nil || e.ID == uuid.Nil {
			continue
		}
		key := entityKey[e.ID]
		emb := candidateEmb[e.ID]
		if len(emb) == 0 {
			continue
		}

		bestScore := 0.0
		var best *types.GlobalEntity
		for _, g := range globalEmbeds {
			score := cosineSim(emb, g.Emb)
			if score > bestScore {
				bestScore = score
				best = g.Entity
			}
		}
		threshold := baseThreshold
		if len(key) <= 4 {
			threshold = baseThreshold + 0.04
		}
		if best != nil && bestScore >= threshold {
			linkEntityToGlobal(e, best, updatedGlobals, globalByAlias)
			matchedEmb++
			continue
		}

		if key != "" {
			if existing := globalByKey[key]; existing != nil {
				linkEntityToGlobal(e, existing, updatedGlobals, globalByAlias)
				matchedEmb++
				continue
			}
		}

		newGlobal := buildGlobalEntityFromMaterial(userID, e, key, emb)
		if newGlobal == nil {
			continue
		}
		globalByKey[key] = newGlobal
		addAliasesToMap(globalByAlias, newGlobal, mergeAliases(aliasesFromJSON(newGlobal.Aliases), newGlobal.Name, newGlobal.Key))
		globalEmbeds = append(globalEmbeds, globalEmb{Entity: newGlobal, Emb: emb})
		updatedGlobals[newGlobal.ID] = newGlobal
		e.GlobalEntityID = &newGlobal.ID
		created++
	}

	stats["matched_embedding"] = matchedEmb
	stats["created_globals"] = created

	out := make([]*types.GlobalEntity, 0, len(updatedGlobals))
	for _, g := range updatedGlobals {
		if g != nil && g.ID != uuid.Nil {
			out = append(out, g)
		}
	}
	return out, stats, nil
}

func buildGlobalEntityFromMaterial(userID uuid.UUID, ent *types.MaterialEntity, key string, emb []float32) *types.GlobalEntity {
	if ent == nil || userID == uuid.Nil {
		return nil
	}
	if key == "" {
		key = normalizeGlobalEntityKey(ent.Key)
	}
	if key == "" {
		return nil
	}
	name := strings.TrimSpace(ent.Name)
	if name == "" {
		name = strings.TrimSpace(ent.Key)
	}
	aliases := mergeAliases(aliasesFromJSON(ent.Aliases), name, ent.Key)
	meta := map[string]any{
		"source":          "material_kg_build",
		"material_set_id": ent.MaterialSetID.String(),
	}
	typ := strings.TrimSpace(ent.Type)
	if typ == "" {
		typ = "unknown"
	}
	desc := strings.TrimSpace(ent.Description)
	row := &types.GlobalEntity{
		ID:          uuid.New(),
		UserID:      userID,
		Key:         key,
		Name:        name,
		Type:        typ,
		Description: desc,
		Aliases:     datatypes.JSON(mustJSON(aliases)),
		Metadata:    datatypes.JSON(mustJSON(meta)),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if len(emb) > 0 {
		row.Embedding = datatypes.JSON(mustJSON(emb))
	}
	return row
}

func linkEntityToGlobal(ent *types.MaterialEntity, g *types.GlobalEntity, updated map[uuid.UUID]*types.GlobalEntity, aliasMap map[string]*types.GlobalEntity) {
	if ent == nil || g == nil || g.ID == uuid.Nil {
		return
	}
	ent.GlobalEntityID = &g.ID

	changed := false
	if strings.TrimSpace(g.Name) == "" {
		g.Name = strings.TrimSpace(ent.Name)
		changed = true
	}
	if strings.TrimSpace(g.Type) == "" || strings.EqualFold(strings.TrimSpace(g.Type), "unknown") {
		if strings.TrimSpace(ent.Type) != "" {
			g.Type = strings.TrimSpace(ent.Type)
			changed = true
		}
	}
	if strings.TrimSpace(g.Description) == "" && strings.TrimSpace(ent.Description) != "" {
		g.Description = strings.TrimSpace(ent.Description)
		changed = true
	}

	existingAliases := aliasesFromJSON(g.Aliases)
	newAliases := mergeAliases(existingAliases, ent.Name, ent.Key)
	newAliases = mergeAliases(newAliases, aliasesFromJSON(ent.Aliases)...)
	if len(newAliases) != len(existingAliases) {
		g.Aliases = datatypes.JSON(mustJSON(newAliases))
		addAliasesToMap(aliasMap, g, newAliases)
		changed = true
	}

	if changed {
		g.UpdatedAt = time.Now()
		updated[g.ID] = g
	}
}

func globalEntityEmbeddingDoc(g *types.GlobalEntity) string {
	if g == nil {
		return ""
	}
	return buildEntityEmbeddingDoc(g.Name, g.Type, g.Description, mergeAliases(aliasesFromJSON(g.Aliases), g.Name, g.Key))
}

func materialEntityEmbeddingDoc(ent *types.MaterialEntity) string {
	if ent == nil {
		return ""
	}
	name := strings.TrimSpace(ent.Name)
	if name == "" {
		name = strings.TrimSpace(ent.Key)
	}
	aliases := mergeAliases(aliasesFromJSON(ent.Aliases), name, ent.Key)
	return buildEntityEmbeddingDoc(name, ent.Type, ent.Description, aliases)
}

func buildEntityEmbeddingDoc(name string, typ string, desc string, aliases []string) string {
	parts := make([]string, 0, 4)
	name = strings.TrimSpace(name)
	typ = strings.TrimSpace(typ)
	desc = strings.TrimSpace(desc)
	if name != "" {
		parts = append(parts, "Name: "+name)
	}
	if typ != "" && !strings.EqualFold(typ, "unknown") {
		parts = append(parts, "Type: "+typ)
	}
	if desc != "" {
		parts = append(parts, "Description: "+desc)
	}
	if len(aliases) > 0 {
		parts = append(parts, "Aliases: "+strings.Join(aliases, ", "))
	}
	return strings.Join(parts, "\n")
}

func mergeAliases(existing []string, additions ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(additions))
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := normalizeGlobalEntityKey(s)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range existing {
		add(s)
	}
	for _, s := range additions {
		add(s)
	}
	return out
}

func addAliasesToMap(m map[string]*types.GlobalEntity, g *types.GlobalEntity, aliases []string) {
	if g == nil || len(aliases) == 0 {
		return
	}
	for _, alias := range aliases {
		key := normalizeGlobalEntityKey(alias)
		if key == "" {
			continue
		}
		if _, ok := m[key]; !ok {
			m[key] = g
		}
	}
}

func aliasesFromJSON(raw datatypes.JSON) []string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err == nil && len(out) > 0 {
		return dedupeStrings(out)
	}
	var tmp []any
	if err := json.Unmarshal(raw, &tmp); err != nil || len(tmp) == 0 {
		return nil
	}
	out = make([]string, 0, len(tmp))
	for _, v := range tmp {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" {
			out = append(out, s)
		}
	}
	return dedupeStrings(out)
}

func normalizeGlobalEntityKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ToLower(name)
	var b strings.Builder
	b.Grow(len(name))
	space := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case r == '&':
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("and")
			space = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.' || r == ',' || r == ':' || r == ';':
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func embedTextBatches(ctx context.Context, ai openai.Client, texts []string, batchSize int, concurrency int) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if batchSize <= 0 {
		batchSize = 32
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	out := make([][]float32, len(texts))
	type batch struct {
		start int
		end   int
	}
	batches := make([]batch, 0, (len(texts)+batchSize-1)/batchSize)
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, batch{start: i, end: end})
	}

	if concurrency == 1 || len(batches) == 1 {
		for _, b := range batches {
			embs, err := ai.Embed(ctx, texts[b.start:b.end])
			if err != nil {
				return nil, err
			}
			if len(embs) != (b.end - b.start) {
				return nil, fmt.Errorf("embedding batch mismatch (got %d want %d)", len(embs), b.end-b.start)
			}
			for i := range embs {
				out[b.start+i] = embs[i]
			}
		}
		return out, nil
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
			embs, err := ai.Embed(ctx, texts[b.start:b.end])
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				continue
			}
			if len(embs) != (b.end - b.start) {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("embedding batch mismatch (got %d want %d)", len(embs), b.end-b.start)
				}
				mu.Unlock()
				continue
			}
			for i := range embs {
				out[b.start+i] = embs[i]
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

	if firstErr != nil {
		return nil, firstErr
	}
	for i := range out {
		if out[i] == nil || len(out[i]) == 0 {
			return nil, fmt.Errorf("embedding batch missing vectors")
		}
	}
	return out, nil
}

func normalizeMaterialEntityKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Keep it simple and stable: lowercase + collapse whitespace.
	name = strings.ToLower(name)
	name = strings.Join(strings.Fields(name), " ")
	return name
}

func filterAllowedChunkIDs(in []string, allowed map[string]bool) []string {
	if len(in) == 0 || len(allowed) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || !allowed[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func deterministicUUID(s string) uuid.UUID {
	h := sha256.Sum256([]byte(s))
	id, err := uuid.FromBytes(h[:16])
	if err != nil {
		return uuid.New()
	}
	return id
}

func materialClaimKey(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ToLower(content)
	content = strings.Join(strings.Fields(content), " ")
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

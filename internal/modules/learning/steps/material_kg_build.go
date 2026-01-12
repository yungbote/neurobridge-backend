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
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
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

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Idempotency: if we already have entities/claims, skip LLM extraction and just ensure Neo4j sync.
	var (
		existingEntities int64
		existingClaims   int64
	)
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialEntity{}).Where("material_set_id = ?", in.MaterialSetID).Count(&existingEntities).Error
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialClaim{}).Where("material_set_id = ?", in.MaterialSetID).Count(&existingClaims).Error
	out.Trace["existing_entities"] = existingEntities
	out.Trace["existing_claims"] = existingClaims
	force := strings.EqualFold(strings.TrimSpace(os.Getenv("MATERIAL_KG_FORCE_REBUILD")), "true")
	if !force && (existingEntities > 0 || existingClaims > 0) {
		out.Skipped = true
		if deps.Graph != nil && deps.Graph.Driver != nil {
			if err := syncMaterialKGToNeo4j(ctx, deps, in.MaterialSetID); err != nil {
				deps.Log.Warn("neo4j material kg sync failed (continuing)", "error", err, "material_set_id", in.MaterialSetID.String())
			}
		}
		return out, nil
	}

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
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
			if err := syncMaterialKGToNeo4j(ctx, deps, in.MaterialSetID); err != nil {
				deps.Log.Warn("neo4j material kg sync failed (continuing)", "error", err, "material_set_id", in.MaterialSetID.String())
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

		id := deterministicUUID("material_entity|" + in.MaterialSetID.String() + "|" + key)
		row := &types.MaterialEntity{
			ID:            id,
			MaterialSetID: in.MaterialSetID,
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
				ID:              edgeID,
				MaterialChunkID: chunkID,
				MaterialEntityID: row.ID,
				Relation:        "mentions",
				Weight:          1,
				CreatedAt:       now,
				UpdatedAt:       now,
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
		claimID := deterministicUUID("material_claim|" + in.MaterialSetID.String() + "|" + ckey)

		metaJSON, _ := json.Marshal(map[string]any{
			"evidence_chunk_ids": ev,
			"entity_names":       entityNames,
			"concept_keys":       conceptKeys,
		})

		claim := &types.MaterialClaim{
			ID:            claimID,
			MaterialSetID: in.MaterialSetID,
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
				ID:              edgeID,
				MaterialClaimID: claimID,
				MaterialEntityID: ent.ID,
				Relation:        "about",
				Weight:          1,
				CreatedAt:       now,
				UpdatedAt:       now,
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

	// Persist (idempotent upserts).
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(entityRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"material_set_id",
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
		if err := graphstore.UpsertMaterialEntitiesClaimsGraph(ctx, deps.Graph, deps.Log, in.MaterialSetID, entityRows, claimRows, chunkEntityRows, chunkClaimRows, claimEntityRows, claimConceptRows); err != nil {
			deps.Log.Warn("neo4j material kg upsert failed (continuing)", "error", err, "material_set_id", in.MaterialSetID.String())
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

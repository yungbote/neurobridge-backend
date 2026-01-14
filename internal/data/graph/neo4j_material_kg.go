package graph

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

func UpsertMaterialEntitiesClaimsGraph(
	ctx context.Context,
	client *neo4jdb.Client,
	log *logger.Logger,
	materialSetID uuid.UUID,
	entities []*types.MaterialEntity,
	claims []*types.MaterialClaim,
	chunkEntities []*types.MaterialChunkEntity,
	chunkClaims []*types.MaterialChunkClaim,
	claimEntities []*types.MaterialClaimEntity,
	claimConcepts []*types.MaterialClaimConcept,
) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if materialSetID == uuid.Nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	entityNodes := make([]map[string]any, 0, len(entities))
	for _, e := range entities {
		if e == nil || e.ID == uuid.Nil || e.MaterialSetID == uuid.Nil {
			continue
		}
		if e.MaterialSetID != materialSetID {
			continue
		}
		entityNodes = append(entityNodes, map[string]any{
			"id":              e.ID.String(),
			"material_set_id": e.MaterialSetID.String(),
			"key":             e.Key,
			"name":            e.Name,
			"type":            e.Type,
			"description":     truncateString(e.Description, 900),
			"aliases_json": func() string {
				if len(e.Aliases) == 0 {
					return ""
				}
				return string(e.Aliases)
			}(),
			"metadata_json": func() string {
				if len(e.Metadata) == 0 {
					return ""
				}
				return string(e.Metadata)
			}(),
			"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": e.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	claimNodes := make([]map[string]any, 0, len(claims))
	for _, c := range claims {
		if c == nil || c.ID == uuid.Nil || c.MaterialSetID == uuid.Nil {
			continue
		}
		if c.MaterialSetID != materialSetID {
			continue
		}
		claimNodes = append(claimNodes, map[string]any{
			"id":              c.ID.String(),
			"material_set_id": c.MaterialSetID.String(),
			"key":             c.Key,
			"kind":            c.Kind,
			"content":         truncateString(c.Content, 1600),
			"confidence":      c.Confidence,
			"metadata_json": func() string {
				if len(c.Metadata) == 0 {
					return ""
				}
				return string(c.Metadata)
			}(),
			"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	chEntRels := make([]map[string]any, 0, len(chunkEntities))
	for _, r := range chunkEntities {
		if r == nil || r.ID == uuid.Nil || r.MaterialChunkID == uuid.Nil || r.MaterialEntityID == uuid.Nil {
			continue
		}
		chEntRels = append(chEntRels, map[string]any{
			"id":        r.ID.String(),
			"chunk_id":  r.MaterialChunkID.String(),
			"entity_id": r.MaterialEntityID.String(),
			"relation":  r.Relation,
			"weight":    r.Weight,
			"synced_at": now,
		})
	}

	chClaimRels := make([]map[string]any, 0, len(chunkClaims))
	for _, r := range chunkClaims {
		if r == nil || r.ID == uuid.Nil || r.MaterialChunkID == uuid.Nil || r.MaterialClaimID == uuid.Nil {
			continue
		}
		chClaimRels = append(chClaimRels, map[string]any{
			"id":        r.ID.String(),
			"chunk_id":  r.MaterialChunkID.String(),
			"claim_id":  r.MaterialClaimID.String(),
			"relation":  r.Relation,
			"weight":    r.Weight,
			"synced_at": now,
		})
	}

	claimEntRels := make([]map[string]any, 0, len(claimEntities))
	for _, r := range claimEntities {
		if r == nil || r.ID == uuid.Nil || r.MaterialClaimID == uuid.Nil || r.MaterialEntityID == uuid.Nil {
			continue
		}
		claimEntRels = append(claimEntRels, map[string]any{
			"id":        r.ID.String(),
			"claim_id":  r.MaterialClaimID.String(),
			"entity_id": r.MaterialEntityID.String(),
			"relation":  r.Relation,
			"weight":    r.Weight,
			"synced_at": now,
		})
	}

	claimConceptRels := make([]map[string]any, 0, len(claimConcepts))
	for _, r := range claimConcepts {
		if r == nil || r.ID == uuid.Nil || r.MaterialClaimID == uuid.Nil || r.ConceptID == uuid.Nil {
			continue
		}
		claimConceptRels = append(claimConceptRels, map[string]any{
			"id":         r.ID.String(),
			"claim_id":   r.MaterialClaimID.String(),
			"concept_id": r.ConceptID.String(),
			"relation":   r.Relation,
			"weight":     r.Weight,
			"synced_at":  now,
		})
	}

	session := client.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: client.Database,
	})
	defer session.Close(ctx)

	// Best-effort schema init.
	{
		stmts := []string{
			`CREATE CONSTRAINT material_set_id_unique IF NOT EXISTS FOR (s:MaterialSet) REQUIRE s.id IS UNIQUE`,
			`CREATE CONSTRAINT material_entity_id_unique IF NOT EXISTS FOR (e:MaterialEntity) REQUIRE e.id IS UNIQUE`,
			`CREATE CONSTRAINT material_claim_id_unique IF NOT EXISTS FOR (c:MaterialClaim) REQUIRE c.id IS UNIQUE`,
		}
		for _, q := range stmts {
			if res, err := session.Run(ctx, q, nil); err != nil {
				if log != nil {
					log.Warn("neo4j schema init failed (continuing)", "error", err)
				}
			} else {
				_, _ = res.Consume(ctx)
			}
		}
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Ensure the set anchor exists (may already exist from other sync).
		if res, err := tx.Run(ctx, `
MERGE (ms:MaterialSet {id: $id})
SET ms.synced_at = $synced_at
`, map[string]any{"id": materialSetID.String(), "synced_at": now}); err != nil {
			return nil, err
		} else if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		if len(entityNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $entities AS e
MERGE (me:MaterialEntity {id: e.id})
SET me += e
WITH me, e
MERGE (ms:MaterialSet {id: e.material_set_id})
MERGE (me)-[:IN_SET]->(ms)
`, map[string]any{"entities": entityNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(claimNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $claims AS c
MERGE (mc:MaterialClaim {id: c.id})
SET mc += c
WITH mc, c
MERGE (ms:MaterialSet {id: c.material_set_id})
MERGE (mc)-[:IN_SET]->(ms)
`, map[string]any{"claims": claimNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(chEntRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MERGE (ch:MaterialChunk {id: r.chunk_id})
MERGE (me:MaterialEntity {id: r.entity_id})
MERGE (ch)-[e:MENTIONS_ENTITY]->(me)
SET e.id = r.id,
    e.relation = r.relation,
    e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": chEntRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(chClaimRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MERGE (ch:MaterialChunk {id: r.chunk_id})
MERGE (mc:MaterialClaim {id: r.claim_id})
MERGE (ch)-[e:SUPPORTS_CLAIM]->(mc)
SET e.id = r.id,
    e.relation = r.relation,
    e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": chClaimRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(claimEntRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MERGE (mc:MaterialClaim {id: r.claim_id})
MERGE (me:MaterialEntity {id: r.entity_id})
MERGE (mc)-[e:ABOUT_ENTITY]->(me)
SET e.id = r.id,
    e.relation = r.relation,
    e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": claimEntRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(claimConceptRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MERGE (mc:MaterialClaim {id: r.claim_id})
MERGE (c:Concept {id: r.concept_id})
MERGE (mc)-[e:ABOUT_CONCEPT]->(c)
SET e.id = r.id,
    e.relation = r.relation,
    e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": claimConceptRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		return nil, nil
	})
	return err
}

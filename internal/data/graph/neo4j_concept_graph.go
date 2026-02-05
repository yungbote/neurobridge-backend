package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

func UpsertPathConceptGraph(ctx context.Context, client *neo4jdb.Client, log *logger.Logger, pathID uuid.UUID, concepts []*types.Concept, edges []*types.ConceptEdge) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if pathID == uuid.Nil {
		return fmt.Errorf("neo4j concept graph sync: missing pathID")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	nodes := make([]map[string]any, 0, len(concepts))
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		scopeID := ""
		if c.ScopeID != nil && *c.ScopeID != uuid.Nil {
			scopeID = c.ScopeID.String()
		}
		nodes = append(nodes, map[string]any{
			"id":         c.ID.String(),
			"scope":      c.Scope,
			"scope_id":   scopeID,
			"key":        c.Key,
			"name":       c.Name,
			"summary":    c.Summary,
			"depth":      int64(c.Depth),
			"sort_index": int64(c.SortIndex),
			"vector_id":  c.VectorID,
			"key_points_json": func() string {
				if len(c.KeyPoints) == 0 {
					return ""
				}
				return string(c.KeyPoints)
			}(),
			"metadata_json": func() string {
				if len(c.Metadata) == 0 {
					return ""
				}
				return string(c.Metadata)
			}(),
			"synced_at": now,
		})
	}

	rels := make([]map[string]any, 0, len(edges))
	prereq := make([]map[string]any, 0, len(edges))
	related := make([]map[string]any, 0, len(edges))
	analogy := make([]map[string]any, 0, len(edges))
	bridge := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		if e == nil || e.FromConceptID == uuid.Nil || e.ToConceptID == uuid.Nil || e.EdgeType == "" {
			continue
		}
		rec := map[string]any{
			"id":            e.ID.String(),
			"from_id":       e.FromConceptID.String(),
			"to_id":         e.ToConceptID.String(),
			"edge_type":     e.EdgeType,
			"strength":      e.Strength,
			"evidence_json": string(e.Evidence),
			"path_id":       pathID.String(),
			"synced_at":     now,
		}
		rels = append(rels, rec)
		switch e.EdgeType {
		case "prereq":
			prereq = append(prereq, rec)
		case "related":
			related = append(related, rec)
		case "analogy":
			analogy = append(analogy, rec)
		case "bridge":
			bridge = append(bridge, rec)
		}
	}

	session := client.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: client.Database,
	})
	defer session.Close(ctx)

	// Create schema helpers (best-effort; may fail for restricted users).
	if res, err := session.Run(ctx, `CREATE CONSTRAINT concept_id_unique IF NOT EXISTS FOR (c:Concept) REQUIRE c.id IS UNIQUE`, nil); err != nil {
		if log != nil {
			log.Warn("neo4j schema init failed (continuing)", "error", err)
		}
	} else {
		_, _ = res.Consume(ctx)
	}
	if res, err := session.Run(ctx, `CREATE INDEX concept_scope_idx IF NOT EXISTS FOR (c:Concept) ON (c.scope, c.scope_id)`, nil); err != nil {
		if log != nil {
			log.Warn("neo4j schema init failed (continuing)", "error", err)
		}
	} else {
		_, _ = res.Consume(ctx)
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if len(nodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $nodes AS n
MERGE (c:Concept {id: n.id})
SET c += n
`, map[string]any{"nodes": nodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(rels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Concept {id: r.from_id})
MATCH (b:Concept {id: r.to_id})
MERGE (a)-[e:CONCEPT_EDGE {edge_type: r.edge_type}]->(b)
SET e.id = r.id,
    e.strength = r.strength,
    e.evidence_json = r.evidence_json,
    e.path_id = r.path_id,
    e.synced_at = r.synced_at
`, map[string]any{"rels": rels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Convenience edges for fast traversals without property filtering.
		if len(prereq) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Concept {id: r.from_id})
MATCH (b:Concept {id: r.to_id})
MERGE (a)-[e:CONCEPT_PREREQ]->(b)
SET e.id = r.id,
    e.strength = r.strength,
    e.evidence_json = r.evidence_json,
    e.path_id = r.path_id,
    e.synced_at = r.synced_at
`, map[string]any{"rels": prereq})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(related) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Concept {id: r.from_id})
MATCH (b:Concept {id: r.to_id})
MERGE (a)-[e:CONCEPT_RELATED]->(b)
SET e.id = r.id,
    e.strength = r.strength,
    e.evidence_json = r.evidence_json,
    e.path_id = r.path_id,
    e.synced_at = r.synced_at
`, map[string]any{"rels": related})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(analogy) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Concept {id: r.from_id})
MATCH (b:Concept {id: r.to_id})
MERGE (a)-[e:CONCEPT_ANALOGY]->(b)
SET e.id = r.id,
    e.strength = r.strength,
    e.evidence_json = r.evidence_json,
    e.path_id = r.path_id,
    e.synced_at = r.synced_at
`, map[string]any{"rels": analogy})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(bridge) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Concept {id: r.from_id})
MATCH (b:Concept {id: r.to_id})
MERGE (a)-[e:CONCEPT_BRIDGE]->(b)
SET e.id = r.id,
    e.strength = r.strength,
    e.evidence_json = r.evidence_json,
    e.path_id = r.path_id,
    e.synced_at = r.synced_at
`, map[string]any{"rels": bridge})
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

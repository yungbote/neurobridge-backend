package graph

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

func UpsertLibraryTaxonomyGraph(
	ctx context.Context,
	client *neo4jdb.Client,
	log *logger.Logger,
	userID uuid.UUID,
	facet string,
	nodes []*types.LibraryTaxonomyNode,
	edges []*types.LibraryTaxonomyEdge,
	memberships []*types.LibraryTaxonomyMembership,
) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if userID == uuid.Nil {
		return nil
	}
	facet = strings.TrimSpace(facet)
	if facet == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	nodeNodes := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil || n.UserID == uuid.Nil || strings.TrimSpace(n.Facet) == "" {
			continue
		}
		if n.UserID != userID || strings.TrimSpace(n.Facet) != facet {
			continue
		}
		nodeNodes = append(nodeNodes, map[string]any{
			"id":          n.ID.String(),
			"user_id":     n.UserID.String(),
			"facet":       strings.TrimSpace(n.Facet),
			"key":         n.Key,
			"kind":        n.Kind,
			"name":        n.Name,
			"description": n.Description,
			"version":     int64(n.Version),
			"embedding_json": func() string {
				if len(n.Embedding) == 0 {
					return ""
				}
				return string(n.Embedding)
			}(),
			"stats_json": func() string {
				if len(n.Stats) == 0 {
					return ""
				}
				return string(n.Stats)
			}(),
			"created_at": n.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": n.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	subsumesRels := make([]map[string]any, 0, len(edges))
	relatedRels := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		if e == nil || e.ID == uuid.Nil || e.UserID == uuid.Nil || strings.TrimSpace(e.Facet) == "" || e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
			continue
		}
		if e.UserID != userID || strings.TrimSpace(e.Facet) != facet {
			continue
		}
		rec := map[string]any{
			"id":      e.ID.String(),
			"user_id": e.UserID.String(),
			"facet":   strings.TrimSpace(e.Facet),
			"kind":    strings.TrimSpace(e.Kind),
			"from_id": e.FromNodeID.String(),
			"to_id":   e.ToNodeID.String(),
			"weight":  e.Weight,
			"metadata_json": func() string {
				if len(e.Metadata) == 0 {
					return ""
				}
				return string(e.Metadata)
			}(),
			"version":    int64(e.Version),
			"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": e.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		}
		switch strings.ToLower(strings.TrimSpace(e.Kind)) {
		case "subsumes":
			subsumesRels = append(subsumesRels, rec)
		default:
			relatedRels = append(relatedRels, rec)
		}
	}

	memberRels := make([]map[string]any, 0, len(memberships))
	for _, m := range memberships {
		if m == nil || m.ID == uuid.Nil || m.UserID == uuid.Nil || strings.TrimSpace(m.Facet) == "" || m.PathID == uuid.Nil || m.NodeID == uuid.Nil {
			continue
		}
		if m.UserID != userID || strings.TrimSpace(m.Facet) != facet {
			continue
		}
		memberRels = append(memberRels, map[string]any{
			"id":          m.ID.String(),
			"user_id":     m.UserID.String(),
			"facet":       strings.TrimSpace(m.Facet),
			"path_id":     m.PathID.String(),
			"node_id":     m.NodeID.String(),
			"weight":      m.Weight,
			"assigned_by": m.AssignedBy,
			"metadata_json": func() string {
				if len(m.Metadata) == 0 {
					return ""
				}
				return string(m.Metadata)
			}(),
			"version":    int64(m.Version),
			"created_at": m.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": m.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
			`CREATE CONSTRAINT library_taxonomy_node_id_unique IF NOT EXISTS FOR (n:LibraryTaxonomyNode) REQUIRE n.id IS UNIQUE`,
			`CREATE CONSTRAINT path_id_unique IF NOT EXISTS FOR (p:Path) REQUIRE p.id IS UNIQUE`,
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
		if len(nodeNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $nodes AS n
MERGE (tn:LibraryTaxonomyNode {id: n.id})
SET tn += n
`, map[string]any{"nodes": nodeNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(subsumesRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:LibraryTaxonomyNode {id: r.from_id})
MATCH (b:LibraryTaxonomyNode {id: r.to_id})
MERGE (a)-[e:TAXONOMY_SUBSUMES]->(b)
SET e.id = r.id,
    e.user_id = r.user_id,
    e.facet = r.facet,
    e.kind = r.kind,
    e.weight = r.weight,
    e.metadata_json = r.metadata_json,
    e.version = r.version,
    e.created_at = r.created_at,
    e.updated_at = r.updated_at,
    e.synced_at = r.synced_at
`, map[string]any{"rels": subsumesRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(relatedRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:LibraryTaxonomyNode {id: r.from_id})
MATCH (b:LibraryTaxonomyNode {id: r.to_id})
MERGE (a)-[e:TAXONOMY_RELATED]->(b)
SET e.id = r.id,
    e.user_id = r.user_id,
    e.facet = r.facet,
    e.kind = r.kind,
    e.weight = r.weight,
    e.metadata_json = r.metadata_json,
    e.version = r.version,
    e.created_at = r.created_at,
    e.updated_at = r.updated_at,
    e.synced_at = r.synced_at
`, map[string]any{"rels": relatedRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(memberRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MERGE (p:Path {id: r.path_id})
SET p.user_id = r.user_id
WITH p, r
MATCH (n:LibraryTaxonomyNode {id: r.node_id})
MERGE (p)-[e:IN_TAXONOMY]->(n)
SET e.id = r.id,
    e.user_id = r.user_id,
    e.facet = r.facet,
    e.weight = r.weight,
    e.assigned_by = r.assigned_by,
    e.metadata_json = r.metadata_json,
    e.version = r.version,
    e.created_at = r.created_at,
    e.updated_at = r.updated_at,
    e.synced_at = r.synced_at
`, map[string]any{"rels": memberRels})
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

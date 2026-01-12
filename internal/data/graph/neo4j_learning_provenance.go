package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

type PathNodeConceptEdge struct {
	PathNodeID uuid.UUID
	ConceptID  uuid.UUID
	Kind       string // covers|requires
	Weight     float64
}

func UpsertConceptEvidenceGraph(ctx context.Context, client *neo4jdb.Client, log *logger.Logger, evidence []*types.ConceptEvidence, chunks []*types.MaterialChunk, files []*types.MaterialFile) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	setNodes := make([]map[string]any, 0, 1)
	seenSets := map[string]bool{}

	fileNodes := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if f.MaterialSetID != uuid.Nil {
			sid := f.MaterialSetID.String()
			if sid != "" && !seenSets[sid] {
				seenSets[sid] = true
				setNodes = append(setNodes, map[string]any{
					"id":        sid,
					"synced_at": now,
				})
			}
		}
		fileNodes = append(fileNodes, map[string]any{
			"id":              f.ID.String(),
			"material_set_id": f.MaterialSetID.String(),
			"original_name":   f.OriginalName,
			"mime_type":       f.MimeType,
			"size_bytes":      f.SizeBytes,
			"storage_key":     f.StorageKey,
			"status":          f.Status,
			"extracted_kind":  f.ExtractedKind,
			"synced_at":       now,
		})
	}

	chunkNodes := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkNodes = append(chunkNodes, map[string]any{
			"id":               ch.ID.String(),
			"material_file_id": ch.MaterialFileID.String(),
			"index":            int64(ch.Index),
			"kind":             ch.Kind,
			"provider":         ch.Provider,
			"page": func() any {
				if ch.Page == nil {
					return nil
				}
				return int64(*ch.Page)
			}(),
			"start_sec": ch.StartSec,
			"end_sec":   ch.EndSec,
			"asset_key": ch.AssetKey,
			"text":      truncateString(ch.Text, 1400),
			"metadata_json": func() string {
				if len(ch.Metadata) == 0 {
					return ""
				}
				return string(ch.Metadata)
			}(),
			"synced_at": now,
		})
	}

	evNodes := make([]map[string]any, 0, len(evidence))
	for _, ev := range evidence {
		if ev == nil || ev.ID == uuid.Nil || ev.ConceptID == uuid.Nil || ev.MaterialChunkID == uuid.Nil {
			continue
		}
		evNodes = append(evNodes, map[string]any{
			"id":          ev.ID.String(),
			"concept_id":  ev.ConceptID.String(),
			"chunk_id":    ev.MaterialChunkID.String(),
			"kind":        ev.Kind,
			"weight":      ev.Weight,
			"synced_at":   now,
			"created_at":  ev.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at":  ev.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"deleted_at":  nil,
			"graph_scope": "concept_evidence",
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
			`CREATE CONSTRAINT material_file_id_unique IF NOT EXISTS FOR (f:MaterialFile) REQUIRE f.id IS UNIQUE`,
			`CREATE CONSTRAINT material_chunk_id_unique IF NOT EXISTS FOR (c:MaterialChunk) REQUIRE c.id IS UNIQUE`,
			`CREATE CONSTRAINT concept_evidence_id_unique IF NOT EXISTS FOR (e:ConceptEvidence) REQUIRE e.id IS UNIQUE`,
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
		if len(setNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $sets AS s
MERGE (ms:MaterialSet {id: s.id})
SET ms += s
`, map[string]any{"sets": setNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(fileNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $files AS f
MERGE (mf:MaterialFile {id: f.id})
SET mf += f
WITH mf, f
MERGE (ms:MaterialSet {id: f.material_set_id})
MERGE (mf)-[:IN_SET]->(ms)
`, map[string]any{"files": fileNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(chunkNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $chunks AS c
MERGE (ch:MaterialChunk {id: c.id})
SET ch += c
WITH ch, c
MERGE (mf:MaterialFile {id: c.material_file_id})
MERGE (ch)-[:IN_FILE]->(mf)
`, map[string]any{"chunks": chunkNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(evNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $evs AS e
MERGE (ev:ConceptEvidence {id: e.id})
SET ev += e
WITH ev, e
MERGE (ch:MaterialChunk {id: e.chunk_id})
MERGE (ch)-[:HAS_CONCEPT_EVIDENCE]->(ev)
WITH ev, e
MERGE (c:Concept {id: e.concept_id})
MERGE (ev)-[:EVIDENCE_FOR]->(c)
`, map[string]any{"evs": evNodes})
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

func UpsertPathStructureGraph(ctx context.Context, client *neo4jdb.Client, log *logger.Logger, path *types.Path, nodes []*types.PathNode, nodeConceptEdges []PathNodeConceptEdge) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if path == nil || path.ID == uuid.Nil {
		return fmt.Errorf("neo4j path structure sync: missing path")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	pathUserID := ""
	if path.UserID != nil && *path.UserID != uuid.Nil {
		pathUserID = path.UserID.String()
	}
	pathNode := map[string]any{
		"id":          path.ID.String(),
		"user_id":     pathUserID,
		"title":       path.Title,
		"description": path.Description,
		"status":      path.Status,
		"job_id": func() string {
			if path.JobID == nil || *path.JobID == uuid.Nil {
				return ""
			}
			return path.JobID.String()
		}(),
		"metadata_json": func() string {
			if len(path.Metadata) == 0 {
				return ""
			}
			return string(path.Metadata)
		}(),
		"created_at": path.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": path.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"ready_at": func() string {
			if path.ReadyAt == nil || path.ReadyAt.IsZero() {
				return ""
			}
			return path.ReadyAt.UTC().Format(time.RFC3339Nano)
		}(),
		"synced_at": now,
	}

	nodeNodes := make([]map[string]any, 0, len(nodes))
	parentRels := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil || n.PathID == uuid.Nil {
			continue
		}
		nodeNodes = append(nodeNodes, map[string]any{
			"id":      n.ID.String(),
			"path_id": n.PathID.String(),
			"index":   int64(n.Index),
			"title":   n.Title,
			"metadata_json": func() string {
				if len(n.Metadata) == 0 {
					return ""
				}
				return string(n.Metadata)
			}(),
			"synced_at": now,
		})
		if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
			parentRels = append(parentRels, map[string]any{
				"parent_id": (*n.ParentNodeID).String(),
				"child_id":  n.ID.String(),
				"synced_at": now,
			})
		}
	}

	covers := make([]map[string]any, 0, len(nodeConceptEdges))
	requires := make([]map[string]any, 0, len(nodeConceptEdges))
	for _, e := range nodeConceptEdges {
		if e.PathNodeID == uuid.Nil || e.ConceptID == uuid.Nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(e.Kind))
		rec := map[string]any{
			"node_id":    e.PathNodeID.String(),
			"concept_id": e.ConceptID.String(),
			"weight":     e.Weight,
			"synced_at":  now,
		}
		switch kind {
		case "requires", "prereq", "prerequisite":
			requires = append(requires, rec)
		default:
			covers = append(covers, rec)
		}
	}

	session := client.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: client.Database,
	})
	defer session.Close(ctx)

	// Best-effort schema init.
	{
		stmts := []string{
			`CREATE CONSTRAINT path_id_unique IF NOT EXISTS FOR (p:Path) REQUIRE p.id IS UNIQUE`,
			`CREATE CONSTRAINT path_node_id_unique IF NOT EXISTS FOR (n:PathNode) REQUIRE n.id IS UNIQUE`,
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
		res, err := tx.Run(ctx, `
MERGE (p:Path {id: $path.id})
SET p += $path
`, map[string]any{"path": pathNode})
		if err != nil {
			return nil, err
		}
		if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		if len(nodeNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $nodes AS n
MERGE (pn:PathNode {id: n.id})
SET pn += n
WITH pn, n
MATCH (p:Path {id: n.path_id})
MERGE (p)-[e:HAS_NODE]->(pn)
SET e.index = n.index,
    e.synced_at = n.synced_at
`, map[string]any{"nodes": nodeNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(parentRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:PathNode {id: r.parent_id})
MATCH (b:PathNode {id: r.child_id})
MERGE (a)-[e:PARENT_OF]->(b)
SET e.synced_at = r.synced_at
`, map[string]any{"rels": parentRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(covers) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (n:PathNode {id: r.node_id})
MERGE (c:Concept {id: r.concept_id})
MERGE (n)-[e:COVERS_CONCEPT]->(c)
SET e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": covers})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(requires) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (n:PathNode {id: r.node_id})
MERGE (c:Concept {id: r.concept_id})
MERGE (n)-[e:REQUIRES_CONCEPT]->(c)
SET e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": requires})
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

func UpsertPathActivitiesGraph(
	ctx context.Context,
	client *neo4jdb.Client,
	log *logger.Logger,
	path *types.Path,
	nodes []*types.PathNode,
	activities []*types.Activity,
	variants []*types.ActivityVariant,
	nodeActivities []*types.PathNodeActivity,
	activityConcepts []*types.ActivityConcept,
	citations []*types.ActivityCitation,
	chunks []*types.MaterialChunk,
	files []*types.MaterialFile,
) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if path == nil || path.ID == uuid.Nil {
		return fmt.Errorf("neo4j activities sync: missing path")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Material files/chunks (only those referenced by citations; keep small).
	fileNodes := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		fileNodes = append(fileNodes, map[string]any{
			"id":              f.ID.String(),
			"material_set_id": f.MaterialSetID.String(),
			"original_name":   f.OriginalName,
			"mime_type":       f.MimeType,
			"storage_key":     f.StorageKey,
			"status":          f.Status,
			"synced_at":       now,
		})
	}

	chunkNodes := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkNodes = append(chunkNodes, map[string]any{
			"id":               ch.ID.String(),
			"material_file_id": ch.MaterialFileID.String(),
			"index":            int64(ch.Index),
			"kind":             ch.Kind,
			"provider":         ch.Provider,
			"page": func() any {
				if ch.Page == nil {
					return nil
				}
				return int64(*ch.Page)
			}(),
			"start_sec": ch.StartSec,
			"end_sec":   ch.EndSec,
			"asset_key": ch.AssetKey,
			"text":      truncateString(ch.Text, 900),
			"synced_at": now,
		})
	}

	// Path + nodes.
	pathNode := map[string]any{
		"id":        path.ID.String(),
		"title":     path.Title,
		"status":    path.Status,
		"synced_at": now,
	}
	nodeNodes := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		nodeNodes = append(nodeNodes, map[string]any{
			"id":        n.ID.String(),
			"path_id":   n.PathID.String(),
			"index":     int64(n.Index),
			"title":     n.Title,
			"synced_at": now,
		})
	}

	// Activities + variants.
	actNodes := make([]map[string]any, 0, len(activities))
	for _, a := range activities {
		if a == nil || a.ID == uuid.Nil {
			continue
		}
		actNodes = append(actNodes, map[string]any{
			"id":         a.ID.String(),
			"owner_type": a.OwnerType,
			"owner_id": func() string {
				if a.OwnerID == nil {
					return ""
				}
				return a.OwnerID.String()
			}(),
			"kind":              a.Kind,
			"title":             a.Title,
			"estimated_minutes": int64(a.EstimatedMinutes),
			"difficulty":        a.Difficulty,
			"status":            a.Status,
			"metadata_json": func() string {
				if len(a.Metadata) == 0 {
					return ""
				}
				return string(a.Metadata)
			}(),
			"created_at": a.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": a.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	variantNodes := make([]map[string]any, 0, len(variants))
	for _, v := range variants {
		if v == nil || v.ID == uuid.Nil || v.ActivityID == uuid.Nil {
			continue
		}
		variantNodes = append(variantNodes, map[string]any{
			"id":          v.ID.String(),
			"activity_id": v.ActivityID.String(),
			"variant":     v.Variant,
			"render_spec_json": func() string {
				if len(v.RenderSpec) == 0 {
					return ""
				}
				return string(v.RenderSpec)
			}(),
			"synced_at": now,
		})
	}

	// Joins.
	nodeActRels := make([]map[string]any, 0, len(nodeActivities))
	for _, j := range nodeActivities {
		if j == nil || j.PathNodeID == uuid.Nil || j.ActivityID == uuid.Nil {
			continue
		}
		nodeActRels = append(nodeActRels, map[string]any{
			"node_id":     j.PathNodeID.String(),
			"activity_id": j.ActivityID.String(),
			"rank":        int64(j.Rank),
			"is_primary":  j.IsPrimary,
			"synced_at":   now,
		})
	}

	actConceptRels := make([]map[string]any, 0, len(activityConcepts))
	for _, ac := range activityConcepts {
		if ac == nil || ac.ActivityID == uuid.Nil || ac.ConceptID == uuid.Nil {
			continue
		}
		actConceptRels = append(actConceptRels, map[string]any{
			"activity_id": ac.ActivityID.String(),
			"concept_id":  ac.ConceptID.String(),
			"role":        ac.Role,
			"weight":      ac.Weight,
			"synced_at":   now,
		})
	}

	citeNodes := make([]map[string]any, 0, len(citations))
	for _, c := range citations {
		if c == nil || c.ID == uuid.Nil || c.ActivityVariantID == uuid.Nil || c.MaterialChunkID == uuid.Nil {
			continue
		}
		citeNodes = append(citeNodes, map[string]any{
			"id":         c.ID.String(),
			"variant_id": c.ActivityVariantID.String(),
			"chunk_id":   c.MaterialChunkID.String(),
			"kind":       c.Kind,
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
			`CREATE CONSTRAINT path_id_unique IF NOT EXISTS FOR (p:Path) REQUIRE p.id IS UNIQUE`,
			`CREATE CONSTRAINT path_node_id_unique IF NOT EXISTS FOR (n:PathNode) REQUIRE n.id IS UNIQUE`,
			`CREATE CONSTRAINT activity_id_unique IF NOT EXISTS FOR (a:Activity) REQUIRE a.id IS UNIQUE`,
			`CREATE CONSTRAINT activity_variant_id_unique IF NOT EXISTS FOR (v:ActivityVariant) REQUIRE v.id IS UNIQUE`,
			`CREATE CONSTRAINT activity_citation_id_unique IF NOT EXISTS FOR (c:ActivityCitation) REQUIRE c.id IS UNIQUE`,
			`CREATE CONSTRAINT material_file_id_unique IF NOT EXISTS FOR (f:MaterialFile) REQUIRE f.id IS UNIQUE`,
			`CREATE CONSTRAINT material_chunk_id_unique IF NOT EXISTS FOR (c:MaterialChunk) REQUIRE c.id IS UNIQUE`,
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
		if res, err := tx.Run(ctx, `
MERGE (p:Path {id: $path.id})
SET p += $path
`, map[string]any{"path": pathNode}); err != nil {
			return nil, err
		} else if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		if len(nodeNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $nodes AS n
MERGE (pn:PathNode {id: n.id})
SET pn += n
WITH pn, n
MATCH (p:Path {id: n.path_id})
MERGE (p)-[e:HAS_NODE]->(pn)
SET e.index = n.index,
    e.synced_at = n.synced_at
`, map[string]any{"nodes": nodeNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(fileNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $files AS f
MERGE (mf:MaterialFile {id: f.id})
SET mf += f
`, map[string]any{"files": fileNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(chunkNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $chunks AS c
MERGE (ch:MaterialChunk {id: c.id})
SET ch += c
WITH ch, c
MERGE (mf:MaterialFile {id: c.material_file_id})
MERGE (ch)-[:IN_FILE]->(mf)
`, map[string]any{"chunks": chunkNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(actNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $acts AS a
MERGE (act:Activity {id: a.id})
SET act += a
WITH act, a
MATCH (p:Path {id: $path_id})
MERGE (p)-[e:HAS_ACTIVITY]->(act)
SET e.synced_at = a.synced_at
`, map[string]any{"acts": actNodes, "path_id": path.ID.String()})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(nodeActRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (n:PathNode {id: r.node_id})
MATCH (a:Activity {id: r.activity_id})
MERGE (n)-[e:HAS_ACTIVITY]->(a)
SET e.rank = r.rank,
    e.is_primary = r.is_primary,
    e.synced_at = r.synced_at
`, map[string]any{"rels": nodeActRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(variantNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $vars AS v
MATCH (a:Activity {id: v.activity_id})
MERGE (av:ActivityVariant {id: v.id})
SET av += v
MERGE (a)-[e:HAS_VARIANT]->(av)
SET e.variant = v.variant,
    e.synced_at = v.synced_at
`, map[string]any{"vars": variantNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(citeNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $cites AS c
MATCH (v:ActivityVariant {id: c.variant_id})
MERGE (ac:ActivityCitation {id: c.id})
SET ac += c
MERGE (v)-[e:HAS_CITATION]->(ac)
SET e.synced_at = c.synced_at
WITH ac, c
MERGE (ch:MaterialChunk {id: c.chunk_id})
MERGE (ac)-[e2:CITES_CHUNK]->(ch)
SET e2.kind = c.kind,
    e2.synced_at = c.synced_at
`, map[string]any{"cites": citeNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		if len(actConceptRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Activity {id: r.activity_id})
MERGE (c:Concept {id: r.concept_id})
MERGE (a)-[e:TEACHES]->(c)
SET e.role = r.role,
    e.weight = r.weight,
    e.synced_at = r.synced_at
`, map[string]any{"rels": actConceptRels})
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

func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-1]) + "…"
}

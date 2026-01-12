package graph

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

func UpsertChatGraph(ctx context.Context, client *neo4jdb.Client, log *logger.Logger, thread *types.ChatThread, entities []*types.ChatEntity, edges []*types.ChatEdge, claims []*types.ChatClaim) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if thread == nil || thread.ID == uuid.Nil || thread.UserID == uuid.Nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	pathID := ""
	if thread.PathID != nil && *thread.PathID != uuid.Nil {
		pathID = thread.PathID.String()
	}
	jobID := ""
	if thread.JobID != nil && *thread.JobID != uuid.Nil {
		jobID = thread.JobID.String()
	}

	threadNode := map[string]any{
		"id":      thread.ID.String(),
		"user_id": thread.UserID.String(),
		"path_id": pathID,
		"job_id":  jobID,
		"title":   thread.Title,
		"status":  thread.Status,
		"metadata_json": func() string {
			if len(thread.Metadata) == 0 {
				return ""
			}
			return string(thread.Metadata)
		}(),
		"created_at": thread.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": thread.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"synced_at":  now,
	}

	entityNodes := make([]map[string]any, 0, len(entities))
	threadEntityRels := make([]map[string]any, 0, len(entities))
	entityIDToNorm := map[uuid.UUID]string{}
	for _, e := range entities {
		if e == nil || e.ID == uuid.Nil || e.UserID == uuid.Nil || strings.TrimSpace(e.Name) == "" {
			continue
		}
		if e.UserID != thread.UserID {
			continue
		}
		if e.ThreadID == nil || *e.ThreadID != thread.ID {
			// Keep this sync scoped to the current thread.
			continue
		}
		norm := normalizeName(e.Name)
		if norm == "" {
			continue
		}
		entityIDToNorm[e.ID] = norm
		entityNodes = append(entityNodes, map[string]any{
			"user_id":      e.UserID.String(),
			"name":         strings.TrimSpace(e.Name),
			"name_norm":    norm,
			"type":         strings.TrimSpace(e.Type),
			"description":  strings.TrimSpace(e.Description),
			"aliases_json": string(e.Aliases),
			"updated_at":   e.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":    now,
		})
		threadEntityRels = append(threadEntityRels, map[string]any{
			"thread_id":      thread.ID.String(),
			"user_id":        e.UserID.String(),
			"name_norm":      norm,
			"chat_entity_id": e.ID.String(),
			"synced_at":      now,
		})
	}

	edgeRels := make([]map[string]any, 0, len(edges))
	for _, ed := range edges {
		if ed == nil || ed.ID == uuid.Nil || ed.UserID == uuid.Nil || ed.SrcEntityID == uuid.Nil || ed.DstEntityID == uuid.Nil {
			continue
		}
		if ed.UserID != thread.UserID {
			continue
		}
		if strings.TrimSpace(ed.Scope) != "thread" || ed.ScopeID == nil || *ed.ScopeID != thread.ID {
			continue
		}
		srcNorm := entityIDToNorm[ed.SrcEntityID]
		dstNorm := entityIDToNorm[ed.DstEntityID]
		if srcNorm == "" || dstNorm == "" {
			continue
		}
		edgeRels = append(edgeRels, map[string]any{
			"id":                 ed.ID.String(),
			"user_id":            ed.UserID.String(),
			"scope":              strings.TrimSpace(ed.Scope),
			"scope_id":           thread.ID.String(),
			"relation":           strings.TrimSpace(ed.Relation),
			"weight":             ed.Weight,
			"evidence_seqs_json": string(ed.EvidenceSeqs),
			"src_norm":           srcNorm,
			"dst_norm":           dstNorm,
			"created_at":         ed.CreatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":          now,
		})
	}

	claimNodes := make([]map[string]any, 0, len(claims))
	threadClaimRels := make([]map[string]any, 0, len(claims))
	claimEntityRels := make([]map[string]any, 0)
	for _, c := range claims {
		if c == nil || c.ID == uuid.Nil || c.UserID == uuid.Nil || strings.TrimSpace(c.Content) == "" {
			continue
		}
		if c.UserID != thread.UserID {
			continue
		}
		if c.ThreadID == nil || *c.ThreadID != thread.ID {
			continue
		}
		fp := claimFingerprint(c.Content)
		if fp == "" {
			continue
		}
		content := strings.TrimSpace(c.Content)
		claimNodes = append(claimNodes, map[string]any{
			"user_id":     c.UserID.String(),
			"fingerprint": fp,
			"content":     content,
			"created_at":  c.CreatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":   now,
		})
		threadClaimRels = append(threadClaimRels, map[string]any{
			"thread_id":    thread.ID.String(),
			"user_id":      c.UserID.String(),
			"fingerprint":  fp,
			"claim_row_id": c.ID.String(),
			"synced_at":    now,
		})

		var names []string
		_ = json.Unmarshal(c.EntityNames, &names)
		for _, n := range names {
			norm := normalizeName(n)
			if norm == "" {
				continue
			}
			claimEntityRels = append(claimEntityRels, map[string]any{
				"user_id":     c.UserID.String(),
				"fingerprint": fp,
				"name_norm":   norm,
				"synced_at":   now,
			})
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
			`CREATE CONSTRAINT user_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.id IS UNIQUE`,
			`CREATE CONSTRAINT chat_thread_id_unique IF NOT EXISTS FOR (t:ChatThread) REQUIRE t.id IS UNIQUE`,
			`CREATE CONSTRAINT entity_user_name_unique IF NOT EXISTS FOR (e:Entity) REQUIRE (e.user_id, e.name_norm) IS UNIQUE`,
			`CREATE CONSTRAINT claim_user_fp_unique IF NOT EXISTS FOR (c:Claim) REQUIRE (c.user_id, c.fingerprint) IS UNIQUE`,
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
		// Thread + ownership.
		if res, err := tx.Run(ctx, `
MERGE (u:User {id: $user_id})
SET u.synced_at = $synced_at
WITH u
MERGE (t:ChatThread {id: $thread.id})
SET t += $thread
MERGE (u)-[e:HAS_THREAD]->(t)
SET e.synced_at = $synced_at
`, map[string]any{
			"user_id":   thread.UserID.String(),
			"thread":    threadNode,
			"synced_at": now,
		}); err != nil {
			return nil, err
		} else if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		// Thread -> Path (if applicable).
		if pathID != "" {
			if res, err := tx.Run(ctx, `
MATCH (t:ChatThread {id: $thread_id})
MERGE (p:Path {id: $path_id})
MERGE (t)-[e:ABOUT_PATH]->(p)
SET e.synced_at = $synced_at
`, map[string]any{
				"thread_id": thread.ID.String(),
				"path_id":   pathID,
				"synced_at": now,
			}); err != nil {
				return nil, err
			} else if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Entities.
		if len(entityNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $ents AS e
MERGE (en:Entity {user_id: e.user_id, name_norm: e.name_norm})
SET en += e
`, map[string]any{"ents": entityNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(threadEntityRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatThread {id: r.thread_id})
MATCH (e:Entity {user_id: r.user_id, name_norm: r.name_norm})
MERGE (t)-[x:HAS_ENTITY {chat_entity_id: r.chat_entity_id}]->(e)
SET x.synced_at = r.synced_at
`, map[string]any{"rels": threadEntityRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Edges between entities.
		if len(edgeRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (a:Entity {user_id: r.user_id, name_norm: r.src_norm})
MATCH (b:Entity {user_id: r.user_id, name_norm: r.dst_norm})
MERGE (a)-[e:CHAT_RELATION {id: r.id}]->(b)
SET e.user_id = r.user_id,
    e.scope = r.scope,
    e.scope_id = r.scope_id,
    e.relation = r.relation,
    e.weight = r.weight,
    e.evidence_seqs_json = r.evidence_seqs_json,
    e.created_at = r.created_at,
    e.synced_at = r.synced_at
`, map[string]any{"rels": edgeRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Claims.
		if len(claimNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $claims AS c
MERGE (cl:Claim {user_id: c.user_id, fingerprint: c.fingerprint})
SET cl.content = c.content,
    cl.synced_at = c.synced_at
`, map[string]any{"claims": claimNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(threadClaimRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatThread {id: r.thread_id})
MATCH (cl:Claim {user_id: r.user_id, fingerprint: r.fingerprint})
MERGE (t)-[e:HAS_CLAIM {claim_row_id: r.claim_row_id}]->(cl)
SET e.synced_at = r.synced_at
`, map[string]any{"rels": threadClaimRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(claimEntityRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (cl:Claim {user_id: r.user_id, fingerprint: r.fingerprint})
MATCH (e:Entity {user_id: r.user_id, name_norm: r.name_norm})
MERGE (cl)-[x:MENTIONS_ENTITY]->(e)
SET x.synced_at = r.synced_at
`, map[string]any{"rels": claimEntityRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Best-effort: connect thread entities to path concepts (simple key/name match).
		if pathID != "" {
			res, err := tx.Run(ctx, `
MATCH (t:ChatThread {id: $thread_id})-[:ABOUT_PATH]->(p:Path {id: $path_id})
MATCH (t)-[:HAS_ENTITY]->(e:Entity)
MATCH (c:Concept {scope: 'path', scope_id: $path_id})
WITH e, c, $synced_at AS synced_at
WITH e, c, synced_at,
     replace(replace(replace(toLower(e.name),' ', '_'),'-','_'),'.','_') AS ek
WHERE c.key = ek OR toLower(c.name) = toLower(e.name)
MERGE (e)-[r:MENTIONS_CONCEPT]->(c)
SET r.synced_at = synced_at
`, map[string]any{
				"thread_id": thread.ID.String(),
				"path_id":   pathID,
				"synced_at": now,
			})
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

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Collapse whitespace.
	return strings.Join(strings.Fields(s), " ")
}

func claimFingerprint(content string) string {
	c := strings.ToLower(strings.TrimSpace(content))
	if c == "" {
		return ""
	}
	sum := sha1.Sum([]byte(c))
	return hex.EncodeToString(sum[:])
}

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

type ChatTurnProvenanceTurn struct {
	Turn            *types.ChatTurn
	RetrievalMode   string
	RawQuery        string
	ContextualQuery string
}

type ChatTurnUsedDoc struct {
	TurnID uuid.UUID
	DocID  uuid.UUID
	Rank   int
}

type ChatTurnUsedChunk struct {
	TurnID  uuid.UUID
	ChunkID uuid.UUID
	Rank    int
	Score   float64
}

func UpsertChatTurnProvenance(
	ctx context.Context,
	client *neo4jdb.Client,
	log *logger.Logger,
	thread *types.ChatThread,
	turns []ChatTurnProvenanceTurn,
	messages []*types.ChatMessage,
	docs []*types.ChatDoc,
	docUses []ChatTurnUsedDoc,
	chunks []*types.MaterialChunk,
	files []*types.MaterialFile,
	chunkUses []ChatTurnUsedChunk,
) error {
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

	threadNode := map[string]any{
		"id":      thread.ID.String(),
		"user_id": thread.UserID.String(),
		"path_id": pathID,
		"job_id": func() string {
			if thread.JobID == nil || *thread.JobID == uuid.Nil {
				return ""
			}
			return thread.JobID.String()
		}(),
		"title": thread.Title,
		"status": func() string {
			return strings.TrimSpace(thread.Status)
		}(),
		"synced_at": now,
	}

	turnNodes := make([]map[string]any, 0, len(turns))
	turnUserMsgRels := make([]map[string]any, 0, len(turns))
	turnAsstMsgRels := make([]map[string]any, 0, len(turns))
	turnPathRels := make([]map[string]any, 0, len(turns))
	for _, t := range turns {
		if t.Turn == nil || t.Turn.ID == uuid.Nil {
			continue
		}
		tt := t.Turn
		tPathID := ""
		if thread.PathID != nil && *thread.PathID != uuid.Nil {
			tPathID = thread.PathID.String()
		}

		turnNodes = append(turnNodes, map[string]any{
			"id":                   tt.ID.String(),
			"user_id":              tt.UserID.String(),
			"thread_id":            tt.ThreadID.String(),
			"path_id":              tPathID,
			"user_message_id":      tt.UserMessageID.String(),
			"assistant_message_id": tt.AssistantMessageID.String(),
			"job_id": func() string {
				if tt.JobID == nil || *tt.JobID == uuid.Nil {
					return ""
				}
				return tt.JobID.String()
			}(),
			"status":  strings.TrimSpace(tt.Status),
			"attempt": int64(tt.Attempt),
			"openai_conversation_id": func() string {
				if tt.OpenAIConversationID == nil {
					return ""
				}
				return strings.TrimSpace(*tt.OpenAIConversationID)
			}(),
			"retrieval_mode":   strings.TrimSpace(t.RetrievalMode),
			"raw_query":        truncateString(t.RawQuery, 600),
			"contextual_query": truncateString(t.ContextualQuery, 600),
			"started_at":       timeOrEmpty(tt.StartedAt),
			"completed_at":     timeOrEmpty(tt.CompletedAt),
			"created_at":       tt.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at":       tt.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":        now,
		})

		if tt.UserMessageID != uuid.Nil {
			turnUserMsgRels = append(turnUserMsgRels, map[string]any{
				"turn_id":   tt.ID.String(),
				"msg_id":    tt.UserMessageID.String(),
				"synced_at": now,
			})
		}
		if tt.AssistantMessageID != uuid.Nil {
			turnAsstMsgRels = append(turnAsstMsgRels, map[string]any{
				"turn_id":   tt.ID.String(),
				"msg_id":    tt.AssistantMessageID.String(),
				"synced_at": now,
			})
		}
		if tPathID != "" {
			turnPathRels = append(turnPathRels, map[string]any{
				"turn_id":   tt.ID.String(),
				"path_id":   tPathID,
				"synced_at": now,
			})
		}
	}

	messageNodes := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		if m == nil || m.ID == uuid.Nil {
			continue
		}
		messageNodes = append(messageNodes, map[string]any{
			"id":         m.ID.String(),
			"thread_id":  m.ThreadID.String(),
			"user_id":    m.UserID.String(),
			"seq":        m.Seq,
			"role":       strings.TrimSpace(m.Role),
			"status":     strings.TrimSpace(m.Status),
			"created_at": m.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at": m.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	docNodes := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		if d == nil || d.ID == uuid.Nil {
			continue
		}
		docNodes = append(docNodes, map[string]any{
			"id":       d.ID.String(),
			"user_id":  d.UserID.String(),
			"doc_type": strings.TrimSpace(d.DocType),
			"scope":    strings.TrimSpace(d.Scope),
			"scope_id": func() string {
				if d.ScopeID == nil || *d.ScopeID == uuid.Nil {
					return ""
				}
				return d.ScopeID.String()
			}(),
			"thread_id": func() string {
				if d.ThreadID == nil || *d.ThreadID == uuid.Nil {
					return ""
				}
				return d.ThreadID.String()
			}(),
			"path_id": func() string {
				if d.PathID == nil || *d.PathID == uuid.Nil {
					return ""
				}
				return d.PathID.String()
			}(),
			"job_id": func() string {
				if d.JobID == nil || *d.JobID == uuid.Nil {
					return ""
				}
				return d.JobID.String()
			}(),
			"source_id": func() string {
				if d.SourceID == nil || *d.SourceID == uuid.Nil {
					return ""
				}
				return d.SourceID.String()
			}(),
			"source_seq": func() any {
				if d.SourceSeq == nil {
					return nil
				}
				return *d.SourceSeq
			}(),
			"chunk_index": int64(d.ChunkIndex),
			"created_at":  d.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at":  d.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":   now,
		})
	}

	docUseRels := make([]map[string]any, 0, len(docUses))
	for _, u := range docUses {
		if u.TurnID == uuid.Nil || u.DocID == uuid.Nil {
			continue
		}
		docUseRels = append(docUseRels, map[string]any{
			"turn_id":   u.TurnID.String(),
			"doc_id":    u.DocID.String(),
			"rank":      int64(u.Rank),
			"synced_at": now,
		})
	}

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
			"status":          f.Status,
			"synced_at":       now,
		})
	}

	chunkNodes := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil || ch.MaterialFileID == uuid.Nil {
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
			"synced_at": now,
		})
	}

	chunkUseRels := make([]map[string]any, 0, len(chunkUses))
	for _, u := range chunkUses {
		if u.TurnID == uuid.Nil || u.ChunkID == uuid.Nil {
			continue
		}
		chunkUseRels = append(chunkUseRels, map[string]any{
			"turn_id":   u.TurnID.String(),
			"chunk_id":  u.ChunkID.String(),
			"rank":      int64(u.Rank),
			"score":     u.Score,
			"synced_at": now,
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
			`CREATE CONSTRAINT chat_turn_id_unique IF NOT EXISTS FOR (t:ChatTurn) REQUIRE t.id IS UNIQUE`,
			`CREATE CONSTRAINT chat_message_id_unique IF NOT EXISTS FOR (m:ChatMessage) REQUIRE m.id IS UNIQUE`,
			`CREATE CONSTRAINT chat_doc_id_unique IF NOT EXISTS FOR (d:ChatDoc) REQUIRE d.id IS UNIQUE`,
			`CREATE CONSTRAINT material_set_id_unique IF NOT EXISTS FOR (s:MaterialSet) REQUIRE s.id IS UNIQUE`,
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
		// Thread anchor (may already exist from other sync).
		if res, err := tx.Run(ctx, `
MERGE (thr:ChatThread {id: $thread.id})
SET thr += $thread
`, map[string]any{"thread": threadNode}); err != nil {
			return nil, err
		} else if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		// Turns + thread relationship.
		if len(turnNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $turns AS t
MERGE (turn:ChatTurn {id: t.id})
SET turn += t
WITH turn, t
MATCH (thr:ChatThread {id: t.thread_id})
MERGE (thr)-[e:HAS_TURN]->(turn)
SET e.synced_at = t.synced_at
`, map[string]any{"turns": turnNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Turn -> Path link (if applicable).
		if len(turnPathRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatTurn {id: r.turn_id})
MERGE (p:Path {id: r.path_id})
MERGE (t)-[e:ABOUT_PATH]->(p)
SET e.synced_at = r.synced_at
`, map[string]any{"rels": turnPathRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Messages referenced by turns.
		if len(messageNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $msgs AS m
MERGE (msg:ChatMessage {id: m.id})
SET msg += m
`, map[string]any{"msgs": messageNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Turn -> messages.
		if len(turnUserMsgRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatTurn {id: r.turn_id})
MATCH (m:ChatMessage {id: r.msg_id})
MERGE (t)-[e:USER_MESSAGE]->(m)
SET e.synced_at = r.synced_at
`, map[string]any{"rels": turnUserMsgRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(turnAsstMsgRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatTurn {id: r.turn_id})
MATCH (m:ChatMessage {id: r.msg_id})
MERGE (t)-[e:ASSISTANT_MESSAGE]->(m)
SET e.synced_at = r.synced_at
`, map[string]any{"rels": turnAsstMsgRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Docs used by turns.
		if len(docNodes) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $docs AS d
MERGE (doc:ChatDoc {id: d.id})
SET doc += d
`, map[string]any{"docs": docNodes})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		if len(docUseRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatTurn {id: r.turn_id})
MATCH (d:ChatDoc {id: r.doc_id})
MERGE (t)-[e:USED_DOC]->(d)
SET e.rank = r.rank,
    e.synced_at = r.synced_at
`, map[string]any{"rels": docUseRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		// Link path-scoped docs to canonical path artifacts.
		// DocTypePathNode + DocTypePathUnitDoc: source_id is a PathNode id.
		if res, err := tx.Run(ctx, `
MATCH (d:ChatDoc)
WHERE d.doc_type IN ['path_node','path_unit_doc'] AND d.source_id <> ''
MATCH (n:PathNode {id: d.source_id})
MERGE (d)-[e:SOURCE_PATH_NODE]->(n)
SET e.synced_at = $synced_at
`, map[string]any{"synced_at": now}); err != nil {
			// Best-effort; don't fail the overall sync.
			if log != nil {
				log.Warn("neo4j doc->path_node link failed (continuing)", "error", err)
			}
		} else {
			_, _ = res.Consume(ctx)
		}
		// DocTypePathOverview/Concepts/Materials: source_id is a Path id.
		if res, err := tx.Run(ctx, `
MATCH (d:ChatDoc)
WHERE d.doc_type IN ['path_overview','path_concepts','path_materials'] AND d.source_id <> ''
MATCH (p:Path {id: d.source_id})
MERGE (d)-[e:SOURCE_PATH]->(p)
SET e.synced_at = $synced_at
`, map[string]any{"synced_at": now}); err != nil {
			if log != nil {
				log.Warn("neo4j doc->path link failed (continuing)", "error", err)
			}
		} else {
			_, _ = res.Consume(ctx)
		}

		// Material files/chunks used by the turn.
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
		if len(chunkUseRels) > 0 {
			res, err := tx.Run(ctx, `
UNWIND $rels AS r
MATCH (t:ChatTurn {id: r.turn_id})
MATCH (c:MaterialChunk {id: r.chunk_id})
MERGE (t)-[e:USED_MATERIAL_CHUNK]->(c)
SET e.rank = r.rank,
    e.score = r.score,
    e.synced_at = r.synced_at
`, map[string]any{"rels": chunkUseRels})
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}

		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("neo4j chat turn provenance sync: %w", err)
	}
	return nil
}

func timeOrEmpty(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

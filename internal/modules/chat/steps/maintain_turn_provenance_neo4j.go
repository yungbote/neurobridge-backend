package steps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const neo4jTurnProvenanceLimit = 200

type chunkPick struct {
	id     uuid.UUID
	fileID uuid.UUID
	score  float64
}

func syncChatTurnProvenanceToNeo4j(ctx context.Context, deps MaintainDeps, thread *types.ChatThread) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if deps.DB == nil || thread == nil || thread.ID == uuid.Nil || thread.UserID == uuid.Nil {
		return nil
	}

	q := deps.DB.WithContext(ctx)
	if q == nil {
		q = deps.DB
	}
	if q == nil {
		return nil
	}

	var turns []*types.ChatTurn
	_ = q.Model(&types.ChatTurn{}).
		Where("user_id = ? AND thread_id = ?", thread.UserID, thread.ID).
		Order("created_at DESC").
		Limit(neo4jTurnProvenanceLimit).
		Find(&turns).Error
	if len(turns) == 0 {
		return nil
	}

	var (
		turnNodes = make([]graphstore.ChatTurnProvenanceTurn, 0, len(turns))

		messageIDs []uuid.UUID
		docIDs     []uuid.UUID
		chunkIDs   []uuid.UUID
		fileIDs    []uuid.UUID

		docUses   []graphstore.ChatTurnUsedDoc
		chunkUses []graphstore.ChatTurnUsedChunk

		seenMsg   = map[uuid.UUID]struct{}{}
		seenDoc   = map[uuid.UUID]struct{}{}
		seenChunk = map[uuid.UUID]struct{}{}
		seenFile  = map[uuid.UUID]struct{}{}

		seenDocUse   = map[string]struct{}{}
		seenChunkUse = map[string]struct{}{}
	)

	for _, t := range turns {
		if t == nil || t.ID == uuid.Nil || t.UserID != thread.UserID || t.ThreadID != thread.ID {
			continue
		}

		// Messages referenced by the turn.
		if t.UserMessageID != uuid.Nil {
			if _, ok := seenMsg[t.UserMessageID]; !ok {
				seenMsg[t.UserMessageID] = struct{}{}
				messageIDs = append(messageIDs, t.UserMessageID)
			}
		}
		if t.AssistantMessageID != uuid.Nil {
			if _, ok := seenMsg[t.AssistantMessageID]; !ok {
				seenMsg[t.AssistantMessageID] = struct{}{}
				messageIDs = append(messageIDs, t.AssistantMessageID)
			}
		}

		// Parse retrieval trace for provenance.
		var trace map[string]any
		if len(t.RetrievalTrace) > 0 && string(t.RetrievalTrace) != "{}" {
			_ = json.Unmarshal(t.RetrievalTrace, &trace)
		}
		retrievalMode := strings.TrimSpace(asString(getMapValue(trace, "retrieval_mode")))
		rawQuery := strings.TrimSpace(asString(getMapValue(trace, "raw_query")))
		ctxQuery := strings.TrimSpace(asString(getMapValue(trace, "contextual_query")))

		turnNodes = append(turnNodes, graphstore.ChatTurnProvenanceTurn{
			Turn:            t,
			RetrievalMode:   retrievalMode,
			RawQuery:        rawQuery,
			ContextualQuery: ctxQuery,
		})

		selectedDocs := parseSelectedDocs(trace)
		for rank, docID := range selectedDocs {
			if docID == uuid.Nil {
				continue
			}
			if _, ok := seenDoc[docID]; !ok {
				seenDoc[docID] = struct{}{}
				docIDs = append(docIDs, docID)
			}
			key := t.ID.String() + "|" + docID.String()
			if _, ok := seenDocUse[key]; ok {
				continue
			}
			seenDocUse[key] = struct{}{}
			docUses = append(docUses, graphstore.ChatTurnUsedDoc{
				TurnID: t.ID,
				DocID:  docID,
				Rank:   rank,
			})
		}

		selectedChunks := parseSelectedMaterialChunks(trace)
		for rank, ch := range selectedChunks {
			if ch.id == uuid.Nil {
				continue
			}
			if _, ok := seenChunk[ch.id]; !ok {
				seenChunk[ch.id] = struct{}{}
				chunkIDs = append(chunkIDs, ch.id)
			}
			if ch.fileID != uuid.Nil {
				if _, ok := seenFile[ch.fileID]; !ok {
					seenFile[ch.fileID] = struct{}{}
					fileIDs = append(fileIDs, ch.fileID)
				}
			}
			key := t.ID.String() + "|" + ch.id.String()
			if _, ok := seenChunkUse[key]; ok {
				continue
			}
			seenChunkUse[key] = struct{}{}
			chunkUses = append(chunkUses, graphstore.ChatTurnUsedChunk{
				TurnID:  t.ID,
				ChunkID: ch.id,
				Rank:    rank,
				Score:   ch.score,
			})
		}
	}

	// Load messages.
	var messages []*types.ChatMessage
	if len(messageIDs) > 0 {
		_ = q.Model(&types.ChatMessage{}).
			Where("user_id = ? AND thread_id = ? AND id IN ?", thread.UserID, thread.ID, messageIDs).
			Find(&messages).Error
	}

	// Load docs.
	var docs []*types.ChatDoc
	if len(docIDs) > 0 && deps.Docs != nil {
		rows, err := deps.Docs.GetByIDs(dbctx.Context{Ctx: ctx, Tx: deps.DB}, thread.UserID, docIDs)
		if err == nil && len(rows) > 0 {
			docs = rows
		}
	}

	// Load material chunks.
	var chunks []*types.MaterialChunk
	if len(chunkIDs) > 0 {
		_ = q.Model(&types.MaterialChunk{}).
			Where("id IN ?", chunkIDs).
			Find(&chunks).Error
		for _, ch := range chunks {
			if ch == nil || ch.MaterialFileID == uuid.Nil {
				continue
			}
			if _, ok := seenFile[ch.MaterialFileID]; !ok {
				seenFile[ch.MaterialFileID] = struct{}{}
				fileIDs = append(fileIDs, ch.MaterialFileID)
			}
		}
	}

	// Load material files.
	var files []*types.MaterialFile
	if len(fileIDs) > 0 {
		_ = q.Model(&types.MaterialFile{}).
			Where("id IN ?", fileIDs).
			Find(&files).Error
	}

	return graphstore.UpsertChatTurnProvenance(ctx, deps.Graph, deps.Log, thread, turnNodes, messages, docs, docUses, chunks, files, chunkUses)
}

func getMapValue(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	return v
}

func parseSelectedDocs(trace map[string]any) []uuid.UUID {
	retrieval, ok := getMapValue(trace, "retrieval").(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := retrieval["selected"].([]any)
	if !ok {
		return nil
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, row := range raw {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(asString(m["doc_id"])))
		if err != nil || id == uuid.Nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func parseSelectedMaterialChunks(trace map[string]any) []chunkPick {
	mat, ok := getMapValue(trace, "materials_retrieval").(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := mat["selected_chunks"].([]any)
	if !ok {
		return nil
	}
	out := make([]chunkPick, 0, len(raw))
	for _, row := range raw {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		chunkID, err := uuid.Parse(strings.TrimSpace(asString(m["chunk_id"])))
		if err != nil || chunkID == uuid.Nil {
			continue
		}
		fileID, _ := uuid.Parse(strings.TrimSpace(asString(m["file_id"])))
		out = append(out, chunkPick{
			id:     chunkID,
			fileID: fileID,
			score:  asFloat(m["score"]),
		})
	}
	return out
}

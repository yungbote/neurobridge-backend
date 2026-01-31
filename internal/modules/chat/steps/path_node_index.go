package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	chatrepo "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	chatIndex "github.com/yungbote/neurobridge-backend/internal/modules/chat/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type PathNodeIndexInput struct {
	UserID     uuid.UUID
	PathID     uuid.UUID
	PathNodeID uuid.UUID
}

type PathNodeIndexOutput struct {
	DocsUpserted   int `json:"docs_upserted"`
	VectorUpserted int `json:"vector_upserted"`
}

// IndexPathNodeBlocksForChat rebuilds block-level retrieval docs for a single path node.
func IndexPathNodeBlocksForChat(ctx context.Context, deps PathIndexDeps, in PathNodeIndexInput) (PathNodeIndexOutput, error) {
	out := PathNodeIndexOutput{}
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Docs == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil {
		return out, fmt.Errorf("chat path node index: missing deps")
	}
	if in.UserID == uuid.Nil || in.PathID == uuid.Nil || in.PathNodeID == uuid.Nil {
		return out, fmt.Errorf("chat path node index: missing ids")
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	path, err := deps.Path.GetByID(dbc, in.PathID)
	if err != nil {
		return out, err
	}
	if path == nil || path.ID == uuid.Nil || path.UserID == nil || *path.UserID != in.UserID {
		return out, fmt.Errorf("path not found")
	}

	nodes, err := deps.PathNodes.GetByIDs(dbc, []uuid.UUID{in.PathNodeID})
	if err != nil || len(nodes) == 0 || nodes[0] == nil {
		return out, fmt.Errorf("path node not found")
	}
	node := nodes[0]
	if node.PathID != in.PathID {
		return out, fmt.Errorf("path node mismatch")
	}

	docRows, err := deps.NodeDocs.GetByPathNodeIDs(dbc, []uuid.UUID{in.PathNodeID})
	if err != nil || len(docRows) == 0 || docRows[0] == nil {
		return out, fmt.Errorf("node doc not found")
	}
	doc := docRows[0]

	// Remove existing block docs for this node.
	ns := chatIndex.ChatUserNamespace(in.UserID)
	var priorVectorIDs []string
	_ = deps.DB.WithContext(ctx).
		Model(&types.ChatDoc{}).
		Where("user_id = ? AND scope = ? AND scope_id = ? AND doc_type = ? AND source_id = ?", in.UserID, ScopePath, in.PathID, DocTypePathUnitBlock, in.PathNodeID).
		Pluck("vector_id", &priorVectorIDs).Error
	_ = deps.DB.WithContext(ctx).
		Where("user_id = ? AND scope = ? AND scope_id = ? AND doc_type = ? AND source_id = ?", in.UserID, ScopePath, in.PathID, DocTypePathUnitBlock, in.PathNodeID).
		Delete(&types.ChatDoc{}).Error
	if deps.Vec != nil && len(priorVectorIDs) > 0 {
		delCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = deps.Vec.DeleteIDs(delCtx, ns, priorVectorIDs)
		cancel()
	}

	if len(doc.DocJSON) == 0 || string(doc.DocJSON) == "null" {
		return out, nil
	}
	var docObj map[string]any
	if json.Unmarshal(doc.DocJSON, &docObj) != nil || docObj == nil {
		return out, nil
	}
	rawBlocks, _ := docObj["blocks"].([]any)
	if len(rawBlocks) == 0 {
		return out, nil
	}

	now := time.Now().UTC()
	docs := make([]*types.ChatDoc, 0, len(rawBlocks))
	embedInputs := make([]string, 0, len(rawBlocks))
	for bi, raw := range rawBlocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockID := stringFromAnyCtx(block["id"])
		if blockID == "" {
			blockID = strconv.Itoa(bi)
		}
		text, contextual, _, _ := buildBlockDocBody(node, blockID, block)
		if strings.TrimSpace(text) == "" {
			continue
		}
		blockDocID := deterministicUUID(fmt.Sprintf(
			"chat_doc|v%d|%s|path:%s|node:%s|block:%s",
			ChatPathDocVersion,
			DocTypePathUnitBlock,
			in.PathID.String(),
			in.PathNodeID.String(),
			blockID,
		))
		d := &types.ChatDoc{
			ID:             blockDocID,
			UserID:         in.UserID,
			DocType:        DocTypePathUnitBlock,
			Scope:          ScopePath,
			ScopeID:        &in.PathID,
			ThreadID:       nil,
			PathID:         &in.PathID,
			JobID:          nil,
			SourceID:       &in.PathNodeID,
			SourceSeq:      nil,
			ChunkIndex:     bi,
			Text:           text,
			ContextualText: contextual,
			VectorID:       blockDocID.String(),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		docs = append(docs, d)
		embedInputs = append(embedInputs, d.ContextualText)
	}

	if len(docs) == 0 {
		return out, nil
	}

	embs, err := deps.AI.Embed(ctx, embedInputs)
	if err != nil || len(embs) != len(docs) {
		embs = make([][]float32, len(docs))
	}
	for i := range docs {
		docs[i].Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(embs[i])))
	}

	if err := deps.Docs.Upsert(dbc, docs); err != nil {
		return out, err
	}
	out.DocsUpserted = len(docs)

	if deps.Vec != nil {
		_ = upsertVectors(ctx, deps.Vec, ns, docs, embs)
		out.VectorUpserted = len(docs)
	}

	return out, nil
}

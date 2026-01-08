package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type NodeDocPatchDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	NodeDocs  repos.LearningNodeDocRepo
	Figures   repos.LearningNodeFigureRepo
	Videos    repos.LearningNodeVideoRepo
	Revisions repos.LearningNodeDocRevisionRepo

	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo
	ULI    repos.UserLibraryIndexRepo
	Assets repos.AssetRepo

	AI     openai.Client
	Vec    pc.VectorStore
	Bucket gcp.BucketService
}

type NodeDocPatchSelection struct {
	Text  string
	Start int
	End   int
}

type NodeDocPatchInput struct {
	OwnerUserID    uuid.UUID
	PathNodeID     uuid.UUID
	BlockID        string
	BlockIndex     int
	Action         string
	Instruction    string
	CitationPolicy string
	Selection      NodeDocPatchSelection
	JobID          uuid.UUID
}

type NodeDocPatchOutput struct {
	DocID      uuid.UUID
	RevisionID uuid.UUID
	BlockID    string
	BlockType  string
	Action     string
}

const nodeDocPatchPromptVersion = "node_doc_patch_v1@2"

func NodeDocPatch(ctx context.Context, deps NodeDocPatchDeps, in NodeDocPatchInput) (NodeDocPatchOutput, error) {
	out := NodeDocPatchOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.Revisions == nil {
		return out, fmt.Errorf("node_doc_patch: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch: missing owner_user_id")
	}
	if in.PathNodeID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch: missing path_node_id")
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "rewrite"
	}
	if action != "rewrite" && action != "regen_media" {
		return out, fmt.Errorf("node_doc_patch: invalid action %q", action)
	}

	node, err := deps.PathNodes.GetByID(dbctx.Context{Ctx: ctx}, in.PathNodeID)
	if err != nil {
		return out, err
	}
	if node == nil || node.PathID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch: node not found")
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, node.PathID)
	if err != nil {
		return out, err
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != in.OwnerUserID {
		return out, fmt.Errorf("node_doc_patch: path not found")
	}

	docRow, err := deps.NodeDocs.GetByPathNodeID(dbctx.Context{Ctx: ctx}, in.PathNodeID)
	if err != nil {
		return out, err
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		return out, fmt.Errorf("node_doc_patch: doc not found")
	}

	var doc content.NodeDocV1
	if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
		return out, fmt.Errorf("node_doc_patch: doc invalid json")
	}

	var idsChanged bool
	if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
		doc = withIDs
		idsChanged = true
	}

	blockID := strings.TrimSpace(in.BlockID)
	idx, resolvedID := findBlockIndex(doc.Blocks, blockID, in.BlockIndex)
	if idx < 0 {
		return out, fmt.Errorf("node_doc_patch: block not found")
	}
	if blockID == "" {
		blockID = resolvedID
	}

	block := doc.Blocks[idx]
	if block == nil {
		return out, fmt.Errorf("node_doc_patch: block not found")
	}
	blockType := strings.ToLower(strings.TrimSpace(stringFromAny(block["type"])))
	if blockType == "" {
		return out, fmt.Errorf("node_doc_patch: block type missing")
	}

	beforeCanon, _ := content.CanonicalizeJSON([]byte(docRow.DocJSON))
	beforeJSON := datatypes.JSON(beforeCanon)

	var promptVersion string
	var modelName string
	resolvedPolicy := ""

	switch action {
	case "rewrite":
		if deps.AI == nil {
			return out, fmt.Errorf("node_doc_patch: ai client missing")
		}
		promptVersion = nodeDocPatchPromptVersion
		modelName = openAIModelFromEnv()

		policy := strings.ToLower(strings.TrimSpace(in.CitationPolicy))
		if policy == "" {
			policy = "reuse_only"
		}
		if policy != "reuse_only" && policy != "allow_new" {
			return out, fmt.Errorf("node_doc_patch: invalid citation_policy %q", policy)
		}
		resolvedPolicy = policy

		// Allowed citations for doc-level validation (existing + optionally new).
		docAllowed := map[string]bool{}
		for _, id := range content.CitedChunkIDsFromNodeDocV1(doc) {
			if id != "" {
				docAllowed[id] = true
			}
		}

		blockCiteIDs := extractChunkIDsFromCitations(block["citations"])
		blockAllowed := map[string]bool{}
		for id := range docAllowed {
			blockAllowed[id] = true
		}

		var excerptIDs []uuid.UUID
		var chunkByID map[uuid.UUID]*types.MaterialChunk

		if policy == "allow_new" {
			if deps.Files == nil || deps.Chunks == nil || deps.ULI == nil {
				return out, fmt.Errorf("node_doc_patch: missing material deps for allow_new")
			}
			uli, err := deps.ULI.GetByUserAndPathID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, node.PathID)
			if err != nil {
				return out, err
			}
			if uli == nil || uli.MaterialSetID == uuid.Nil {
				return out, fmt.Errorf("node_doc_patch: material_set not found")
			}

			files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, uli.MaterialSetID)
			if err != nil {
				return out, err
			}
			fileIDs := make([]uuid.UUID, 0, len(files))
			for _, f := range files {
				if f != nil && f.ID != uuid.Nil {
					fileIDs = append(fileIDs, f.ID)
				}
			}
			allChunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
			if err != nil {
				return out, err
			}
			chunkByID = map[uuid.UUID]*types.MaterialChunk{}
			for _, ch := range allChunks {
				if ch == nil || ch.ID == uuid.Nil {
					continue
				}
				if isUnextractableChunk(ch) {
					continue
				}
				chunkByID[ch.ID] = ch
			}

			queryText := strings.TrimSpace(strings.Join([]string{
				node.Title,
				nodeGoalFromMeta(node.Metadata),
				strings.Join(nodeConceptKeysFromMeta(node.Metadata), ", "),
				blockTextForQuery(blockType, block),
				in.Selection.Text,
				instructionText(in.Instruction),
			}, " "))
			queryText = shorten(queryText, 800)

			var queryEmb []float32
			if strings.TrimSpace(queryText) != "" {
				embs, err := deps.AI.Embed(ctx, []string{queryText})
				if err == nil && len(embs) > 0 {
					queryEmb = embs[0]
				}
			}

			const semanticK = 18
			const lexicalK = 8
			const finalK = 18

			retrieved := make([]uuid.UUID, 0)
			if deps.Vec != nil && len(queryEmb) > 0 {
				ids, qerr := deps.Vec.QueryIDs(ctx, index.ChunksNamespace(uli.MaterialSetID), queryEmb, semanticK, map[string]any{"type": "chunk"})
				if qerr == nil && len(ids) > 0 {
					for _, s := range ids {
						if id, e := uuid.Parse(strings.TrimSpace(s)); e == nil && id != uuid.Nil {
							retrieved = append(retrieved, id)
						}
					}
				}
			}

			lexIDs, _ := lexicalChunkIDs(dbctx.Context{Ctx: ctx, Tx: deps.DB}, fileIDs, queryText, lexicalK)
			retrieved = append(retrieved, lexIDs...)
			retrieved = dedupeUUIDsPreserveOrder(retrieved)

			if len(retrieved) < finalK && len(queryEmb) > 0 {
				embs := make([]chunkEmbedding, 0, len(chunkByID))
				for id, ch := range chunkByID {
					if id == uuid.Nil || ch == nil {
						continue
					}
					if v, ok := decodeEmbedding(ch.Embedding); ok && len(v) > 0 {
						embs = append(embs, chunkEmbedding{ID: id, Emb: v})
					}
				}
				sort.Slice(embs, func(i, j int) bool { return embs[i].ID.String() < embs[j].ID.String() })
				fallback := topKChunkIDsByCosine(queryEmb, embs, finalK)
				retrieved = dedupeUUIDsPreserveOrder(append(retrieved, fallback...))
			}

			filtered := make([]uuid.UUID, 0, len(retrieved))
			for _, id := range retrieved {
				if chunkByID[id] != nil {
					filtered = append(filtered, id)
				}
			}
			retrieved = filtered

			if len(retrieved) > finalK {
				retrieved = retrieved[:finalK]
			}
			if len(retrieved) == 0 {
				return out, fmt.Errorf("node_doc_patch: no chunks retrieved")
			}

			for _, id := range retrieved {
				docAllowed[id.String()] = true
				blockAllowed[id.String()] = true
			}

			excerptIDs = retrieved
		} else {
			excerptIDs = uuidSliceFromStrings(blockCiteIDs)
			if len(excerptIDs) > 0 && deps.Chunks != nil {
				chunks, err := deps.Chunks.GetByIDs(dbctx.Context{Ctx: ctx}, excerptIDs)
				if err == nil {
					chunkByID = map[uuid.UUID]*types.MaterialChunk{}
					for _, ch := range chunks {
						if ch != nil && ch.ID != uuid.Nil {
							chunkByID[ch.ID] = ch
						}
					}
				}
			}
		}

		excerpts := ""
		if len(excerptIDs) > 0 && chunkByID != nil {
			excerpts = buildChunkExcerpts(chunkByID, excerptIDs, 14, 900)
		}

		schema, err := blockPatchSchema(blockType)
		if err != nil {
			return out, err
		}

		sys := "You update a single block inside a learning document. Return JSON that matches the schema exactly. Keep the block id/type unchanged. Use only allowed chunk_ids for citations."
		user := buildBlockPatchPrompt(doc, blockType, blockID, block, in, policy, blockAllowed, excerpts)

		obj, err := deps.AI.GenerateJSON(ctx, sys, user, "node_doc_block_patch", schema)
		if err != nil {
			return out, err
		}

		updated, err := applyBlockPatch(blockType, blockID, block, obj)
		if err != nil {
			return out, err
		}

		used := extractChunkIDsFromCitations(updated["citations"])
		for _, id := range used {
			if !blockAllowed[id] {
				return out, fmt.Errorf("node_doc_patch: citation %s not allowed", id)
			}
		}

		doc.Blocks[idx] = updated

		// Deterministic scrub pass (same guardrails as full doc generation).
		if scrubbed, phrases := content.ScrubNodeDocV1(doc); len(phrases) > 0 {
			doc = scrubbed
		}

		reqs := content.NodeDocRequirements{}
		if errs, _ := content.ValidateNodeDocV1(doc, docAllowed, reqs); len(errs) > 0 {
			return out, fmt.Errorf("node_doc_patch: validation failed: %s", strings.Join(errs, "; "))
		}

	case "regen_media":
		if blockType != "figure" && blockType != "video" {
			return out, fmt.Errorf("node_doc_patch: regen_media only supports figure/video blocks")
		}
		if deps.AI == nil || deps.Bucket == nil {
			return out, fmt.Errorf("node_doc_patch: media deps missing")
		}

		modelName = openAIModelFromEnv()
		if blockType == "figure" {
			promptVersion = "node_figure_asset_v1@1"
			row, err := findFigureRow(ctx, deps, node.ID, block)
			if err != nil {
				return out, err
			}
			if row == nil {
				return out, fmt.Errorf("node_doc_patch: figure row not found")
			}
			update, err := regenerateFigure(ctx, deps, row, in.Instruction)
			if err != nil {
				return out, err
			}
			doc.Blocks[idx] = updateFigureBlock(block, update)
		} else {
			promptVersion = "node_video_asset_v1@1"
			row, err := findVideoRow(ctx, deps, node.ID, block)
			if err != nil {
				return out, err
			}
			if row == nil {
				return out, fmt.Errorf("node_doc_patch: video row not found")
			}
			update, err := regenerateVideo(ctx, deps, row, in.Instruction)
			if err != nil {
				return out, err
			}
			doc.Blocks[idx] = updateVideoBlock(block, update)
		}

		reqs := content.NodeDocRequirements{}
		docAllowed := map[string]bool{}
		for _, id := range content.CitedChunkIDsFromNodeDocV1(doc) {
			if id != "" {
				docAllowed[id] = true
			}
		}
		if errs, _ := content.ValidateNodeDocV1(doc, docAllowed, reqs); len(errs) > 0 {
			return out, fmt.Errorf("node_doc_patch: validation failed: %s", strings.Join(errs, "; "))
		}
	}

	rawDoc, _ := json.Marshal(doc)
	canon, err := content.CanonicalizeJSON(rawDoc)
	if err != nil {
		return out, err
	}
	contentHash := content.HashBytes(canon)
	sourcesHash := content.HashSources(promptVersion, 1, content.CitedChunkIDsFromNodeDocV1(doc))
	docText, _ := content.NodeDocMetrics(doc)["doc_text"].(string)

	now := time.Now().UTC()
	docID := docRow.ID
	if docID == uuid.Nil {
		docID = uuid.New()
	}
	updatedDoc := &types.LearningNodeDoc{
		ID:            docID,
		UserID:        in.OwnerUserID,
		PathID:        node.PathID,
		PathNodeID:    node.ID,
		SchemaVersion: 1,
		DocJSON:       datatypes.JSON(canon),
		DocText:       docText,
		ContentHash:   contentHash,
		SourcesHash:   sourcesHash,
		CreatedAt:     docRow.CreatedAt,
		UpdatedAt:     now,
	}

	selectionJSON := datatypes.JSON([]byte(`null`))
	if strings.TrimSpace(in.Selection.Text) != "" || in.Selection.Start != 0 || in.Selection.End != 0 {
		selectionJSON = mustJSON(map[string]any{
			"text":  strings.TrimSpace(in.Selection.Text),
			"start": in.Selection.Start,
			"end":   in.Selection.End,
		})
	}

	revID := uuid.New()
	revision := &types.LearningNodeDocRevision{
		ID:             revID,
		DocID:          docID,
		UserID:         in.OwnerUserID,
		PathID:         node.PathID,
		PathNodeID:     node.ID,
		BlockID:        blockID,
		BlockType:      blockType,
		Operation:      action,
		CitationPolicy: resolvedPolicy,
		Instruction:    strings.TrimSpace(in.Instruction),
		Selection:      selectionJSON,
		BeforeJSON:     beforeJSON,
		AfterJSON:      datatypes.JSON(canon),
		Status:         "succeeded",
		Error:          "",
		Model:          strings.TrimSpace(modelName),
		PromptVersion:  strings.TrimSpace(promptVersion),
		TokensIn:       0,
		TokensOut:      0,
		CreatedAt:      now,
	}
	if in.JobID != uuid.Nil {
		revision.JobID = &in.JobID
	}

	err = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: ctx, Tx: tx}
		if err := deps.NodeDocs.Upsert(inner, updatedDoc); err != nil {
			return err
		}
		if _, err := deps.Revisions.Create(inner, []*types.LearningNodeDocRevision{revision}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	out.DocID = docID
	out.RevisionID = revID
	out.BlockID = blockID
	out.BlockType = blockType
	out.Action = action

	// If we only added IDs and the action was rewrite/regen_media, still treat as success.
	if idsChanged {
		return out, nil
	}
	return out, nil
}

func findBlockIndex(blocks []map[string]any, blockID string, blockIndex int) (int, string) {
	if blockID != "" {
		for i, b := range blocks {
			if b == nil {
				continue
			}
			if strings.TrimSpace(stringFromAny(b["id"])) == blockID {
				return i, blockID
			}
		}
	}
	if blockIndex >= 0 && blockIndex < len(blocks) {
		id := strings.TrimSpace(stringFromAny(blocks[blockIndex]["id"]))
		return blockIndex, id
	}
	// Fallback: allow 1-based indexing from callers.
	if blockIndex > 0 && blockIndex-1 < len(blocks) {
		idx := blockIndex - 1
		id := strings.TrimSpace(stringFromAny(blocks[idx]["id"]))
		return idx, id
	}
	return -1, ""
}

func buildBlockPatchPrompt(doc content.NodeDocV1, blockType string, blockID string, block map[string]any, in NodeDocPatchInput, policy string, allowed map[string]bool, excerpts string) string {
	blockJSON, _ := json.Marshal(block)

	allowedIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		allowedIDs = append(allowedIDs, id)
	}
	sort.Strings(allowedIDs)

	selectionText := ""
	if strings.TrimSpace(in.Selection.Text) != "" || in.Selection.Start != 0 || in.Selection.End != 0 {
		selectionText = fmt.Sprintf("text=%q start=%d end=%d", strings.TrimSpace(in.Selection.Text), in.Selection.Start, in.Selection.End)
	}

	var b strings.Builder
	b.WriteString("DOC_TITLE: ")
	b.WriteString(strings.TrimSpace(doc.Title))
	b.WriteString("\nDOC_SUMMARY: ")
	b.WriteString(strings.TrimSpace(doc.Summary))
	b.WriteString("\nBLOCK_TYPE: ")
	b.WriteString(blockType)
	b.WriteString("\nBLOCK_ID: ")
	b.WriteString(blockID)
	b.WriteString("\nBLOCK_JSON:\n")
	b.Write(blockJSON)
	b.WriteString("\nINSTRUCTION:\n")
	b.WriteString(strings.TrimSpace(in.Instruction))
	b.WriteString("\nSELECTION:\n")
	b.WriteString(selectionText)
	b.WriteString("\nCITATION_POLICY: ")
	b.WriteString(policy)
	b.WriteString("\nALLOWED_CHUNK_IDS:\n")
	b.WriteString(strings.Join(allowedIDs, "\n"))
	b.WriteString("\nEXCERPTS:\n")
	b.WriteString(excerpts)
	return strings.TrimSpace(b.String())
}

func blockPatchSchema(blockType string) (map[string]any, error) {
	citationSchema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"chunk_id": map[string]any{"type": "string"},
				"quote":    map[string]any{"type": "string"},
				"loc": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"page":  map[string]any{"type": "integer"},
						"start": map[string]any{"type": "integer"},
						"end":   map[string]any{"type": "integer"},
					},
					"required":             []any{"page", "start", "end"},
					"additionalProperties": false,
				},
			},
			"required":             []any{"chunk_id", "quote", "loc"},
			"additionalProperties": false,
		},
	}

	base := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "string"},
			"type": map[string]any{"type": "string"},
		},
		"required":             []any{"id", "type"},
		"additionalProperties": false,
	}

	switch blockType {
	case "heading":
		base["properties"].(map[string]any)["level"] = map[string]any{"type": "integer"}
		base["properties"].(map[string]any)["text"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "heading"}
		base["required"] = append(base["required"].([]any), "level", "text")
		return base, nil
	case "paragraph":
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "paragraph"}
		base["required"] = append(base["required"].([]any), "md", "citations")
		return base, nil
	case "callout":
		base["properties"].(map[string]any)["variant"] = map[string]any{"type": "string", "enum": []any{"info", "tip", "warning"}}
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "callout"}
		base["required"] = append(base["required"].([]any), "variant", "title", "md", "citations")
		return base, nil
	case "diagram":
		base["properties"].(map[string]any)["kind"] = map[string]any{"type": "string", "enum": []any{"svg", "mermaid"}}
		base["properties"].(map[string]any)["source"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "diagram"}
		base["required"] = append(base["required"].([]any), "kind", "source", "caption", "citations")
		return base, nil
	case "table":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["columns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["rows"] = map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "table"}
		base["required"] = append(base["required"].([]any), "caption", "columns", "rows", "citations")
		return base, nil
	case "quick_check":
		base["properties"].(map[string]any)["prompt_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["answer_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "quick_check"}
		base["required"] = append(base["required"].([]any), "prompt_md", "answer_md", "citations")
		return base, nil
	case "figure":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "figure"}
		base["required"] = append(base["required"].([]any), "caption", "citations")
		return base, nil
	case "video":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "video"}
		base["required"] = append(base["required"].([]any), "caption")
		return base, nil
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["items_md"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": blockType}
		base["required"] = append(base["required"].([]any), "title", "items_md", "citations")
		return base, nil
	case "steps":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["steps_md"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "steps"}
		base["required"] = append(base["required"].([]any), "title", "steps_md", "citations")
		return base, nil
	case "glossary":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["terms"] = map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term":          map[string]any{"type": "string"},
					"definition_md": map[string]any{"type": "string"},
				},
				"required":             []any{"term", "definition_md"},
				"additionalProperties": false,
			},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "glossary"}
		base["required"] = append(base["required"].([]any), "title", "terms", "citations")
		return base, nil
	case "faq":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["qas"] = map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question_md": map[string]any{"type": "string"},
					"answer_md":   map[string]any{"type": "string"},
				},
				"required":             []any{"question_md", "answer_md"},
				"additionalProperties": false,
			},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "faq"}
		base["required"] = append(base["required"].([]any), "title", "qas", "citations")
		return base, nil
	case "intuition", "mental_model", "why_it_matters":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": blockType}
		base["required"] = append(base["required"].([]any), "title", "md", "citations")
		return base, nil
	default:
		return nil, fmt.Errorf("node_doc_patch: unsupported block type %q", blockType)
	}
}

func applyBlockPatch(blockType, blockID string, existing map[string]any, obj map[string]any) (map[string]any, error) {
	if existing == nil {
		return nil, fmt.Errorf("node_doc_patch: missing block")
	}
	updated := map[string]any{}
	for k, v := range existing {
		updated[k] = v
	}
	updated["id"] = blockID
	updated["type"] = blockType

	switch blockType {
	case "heading":
		updated["text"] = strings.TrimSpace(stringFromAny(obj["text"]))
		updated["level"] = intFromAny(obj["level"], intFromAny(existing["level"], 2))
	case "paragraph":
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "callout":
		updated["variant"] = strings.TrimSpace(stringFromAny(obj["variant"]))
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "diagram":
		updated["kind"] = strings.TrimSpace(stringFromAny(obj["kind"]))
		updated["source"] = strings.TrimSpace(stringFromAny(obj["source"]))
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "table":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["columns"] = stringSliceFromAny(obj["columns"])
		updated["rows"] = normalizeRows(obj["rows"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "quick_check":
		updated["prompt_md"] = strings.TrimSpace(stringFromAny(obj["prompt_md"]))
		updated["answer_md"] = strings.TrimSpace(stringFromAny(obj["answer_md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "figure":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "video":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["items_md"] = stringSliceFromAny(obj["items_md"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "steps":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["steps_md"] = stringSliceFromAny(obj["steps_md"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "glossary":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["terms"] = normalizeAnyArray(obj["terms"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "faq":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["qas"] = normalizeAnyArray(obj["qas"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "intuition", "mental_model", "why_it_matters":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	default:
		return nil, fmt.Errorf("node_doc_patch: unsupported block type %q", blockType)
	}
	return updated, nil
}

func normalizeAnyArray(raw any) []any {
	if raw == nil {
		return []any{}
	}
	if arr, ok := raw.([]any); ok {
		return arr
	}
	return []any{}
}

func normalizeCitations(raw any) []any {
	arr, ok := raw.([]any)
	if !ok || arr == nil {
		return []any{}
	}
	return arr
}

func normalizeRows(raw any) []any {
	if raw == nil {
		return []any{}
	}
	if arr, ok := raw.([]any); ok {
		return arr
	}
	rows, ok := raw.([][]string)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		cell := make([]any, 0, len(row))
		for _, c := range row {
			cell = append(cell, c)
		}
		out = append(out, cell)
	}
	return out
}

func stringMatrixFromAny(v any) [][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(arr))
	for _, row := range arr {
		out = append(out, stringSliceFromAny(row))
	}
	return out
}

func extractChunkIDsFromCitations(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
		if id != "" {
			out = append(out, id)
		}
	}
	return dedupeStrings(out)
}

func buildChunkExcerpts(byID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxLines int, maxChars int) string {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}
	var b strings.Builder
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := byID[id]
		if ch == nil {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		if len(txt) > maxChars {
			txt = txt[:maxChars] + "..."
		}
		b.WriteString("[chunk_id=")
		b.WriteString(id.String())
		b.WriteString("] ")
		b.WriteString(txt)
		b.WriteString("\n")
		n++
		if n >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func nodeGoalFromMeta(raw datatypes.JSON) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(stringFromAny(meta["goal"]))
}

func nodeConceptKeysFromMeta(raw datatypes.JSON) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	return dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
}

func instructionText(s string) string {
	return strings.TrimSpace(s)
}

func blockTextForQuery(blockType string, block map[string]any) string {
	switch blockType {
	case "heading":
		return strings.TrimSpace(stringFromAny(block["text"]))
	case "paragraph":
		return strings.TrimSpace(stringFromAny(block["md"]))
	case "callout":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + stringFromAny(block["md"]))
	case "code":
		return strings.TrimSpace(stringFromAny(block["code"]))
	case "figure":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "video":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "diagram":
		return strings.TrimSpace(stringFromAny(block["caption"]) + " " + stringFromAny(block["source"]))
	case "table":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "quick_check":
		return strings.TrimSpace(stringFromAny(block["prompt_md"]) + " " + stringFromAny(block["answer_md"]))
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + strings.Join(stringSliceFromAny(block["items_md"]), " "))
	case "steps":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + strings.Join(stringSliceFromAny(block["steps_md"]), " "))
	case "glossary":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAny(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["terms"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAny(m["term"]))
				b.WriteString(" ")
				b.WriteString(stringFromAny(m["definition_md"]))
				b.WriteString(" ")
			}
		}
		return strings.TrimSpace(b.String())
	case "faq":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAny(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["qas"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAny(m["question_md"]))
				b.WriteString(" ")
				b.WriteString(stringFromAny(m["answer_md"]))
				b.WriteString(" ")
			}
		}
		return strings.TrimSpace(b.String())
	case "intuition", "mental_model", "why_it_matters":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + stringFromAny(block["md"]))
	default:
		return ""
	}
}

func findFigureRow(ctx context.Context, deps NodeDocPatchDeps, nodeID uuid.UUID, block map[string]any) (*types.LearningNodeFigure, error) {
	if deps.Figures == nil {
		return nil, fmt.Errorf("node_doc_patch: figure repo missing")
	}
	rows, err := deps.Figures.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{nodeID})
	if err != nil {
		return nil, err
	}
	asset, _ := block["asset"].(map[string]any)
	storageKey := strings.TrimSpace(stringFromAny(asset["storage_key"]))
	url := strings.TrimSpace(stringFromAny(asset["url"]))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if storageKey != "" && strings.TrimSpace(r.AssetStorageKey) == storageKey {
			return r, nil
		}
		if url != "" && strings.TrimSpace(r.AssetURL) == url {
			return r, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return nil, nil
}

func findVideoRow(ctx context.Context, deps NodeDocPatchDeps, nodeID uuid.UUID, block map[string]any) (*types.LearningNodeVideo, error) {
	if deps.Videos == nil {
		return nil, fmt.Errorf("node_doc_patch: video repo missing")
	}
	rows, err := deps.Videos.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{nodeID})
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(stringFromAny(block["url"]))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if url != "" && strings.TrimSpace(r.AssetURL) == url {
			return r, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return nil, nil
}

func regenerateFigure(ctx context.Context, deps NodeDocPatchDeps, row *types.LearningNodeFigure, instruction string) (*types.LearningNodeFigure, error) {
	var plan content.FigurePlanItemV1
	if len(row.PlanJSON) == 0 || string(row.PlanJSON) == "null" {
		return nil, fmt.Errorf("node_doc_patch: missing figure plan_json")
	}
	if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
		return nil, fmt.Errorf("node_doc_patch: invalid figure plan_json")
	}

	prompt := strings.TrimSpace(plan.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("node_doc_patch: empty figure prompt")
	}
	if strings.TrimSpace(instruction) != "" {
		prompt = prompt + "\n\nUser request: " + strings.TrimSpace(instruction)
	}
	prompt = prompt + "\n\nGenerate a fresh variation distinct from prior outputs."
	plan.Prompt = prompt

	img, err := deps.AI.GenerateImage(ctx, prompt)
	if err != nil {
		return nil, err
	}
	if len(img.Bytes) == 0 {
		return nil, fmt.Errorf("node_doc_patch: image_generate_empty")
	}

	storageKey := fmt.Sprintf("generated/node_figures/%s/%s/slot_%d_%s.png",
		row.PathID.String(),
		row.PathNodeID.String(),
		row.Slot,
		content.HashBytes([]byte(prompt)),
	)
	if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(img.Bytes)); err != nil {
		return nil, err
	}
	publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)

	mime := strings.TrimSpace(img.MimeType)
	if mime == "" {
		mime = strings.TrimSpace(row.AssetMimeType)
	}
	if mime == "" {
		mime = "image/png"
	}

	var assetID *uuid.UUID
	if deps.Assets != nil {
		meta := map[string]any{
			"asset_kind":     "generated_figure",
			"caption":        strings.TrimSpace(plan.Caption),
			"alt_text":       strings.TrimSpace(plan.AltText),
			"placement_hint": strings.TrimSpace(plan.PlacementHint),
			"citations":      content.NormalizeConceptKeys(plan.Citations),
		}
		aid := uuid.New()
		a := &types.Asset{
			ID:         aid,
			Kind:       "image",
			StorageKey: storageKey,
			URL:        publicURL,
			OwnerType:  "learning_node_figure",
			OwnerID:    row.ID,
			Metadata:   mustJSON(meta),
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
			assetID = &aid
		}
	}

	planJSON, _ := json.Marshal(plan)
	now := time.Now().UTC()
	update := &types.LearningNodeFigure{
		ID:              row.ID,
		UserID:          row.UserID,
		PathID:          row.PathID,
		PathNodeID:      row.PathNodeID,
		Slot:            row.Slot,
		SchemaVersion:   row.SchemaVersion,
		PlanJSON:        datatypes.JSON(planJSON),
		PromptHash:      content.HashBytes([]byte(prompt)),
		SourcesHash:     row.SourcesHash,
		Status:          "rendered",
		AssetID:         assetID,
		AssetStorageKey: storageKey,
		AssetURL:        publicURL,
		AssetMimeType:   mime,
		Error:           "",
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       now,
	}
	if deps.Figures != nil {
		if err := deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, update); err != nil {
			return nil, err
		}
	}
	return update, nil
}

func regenerateVideo(ctx context.Context, deps NodeDocPatchDeps, row *types.LearningNodeVideo, instruction string) (*types.LearningNodeVideo, error) {
	var plan content.VideoPlanItemV1
	if len(row.PlanJSON) == 0 || string(row.PlanJSON) == "null" {
		return nil, fmt.Errorf("node_doc_patch: missing video plan_json")
	}
	if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
		return nil, fmt.Errorf("node_doc_patch: invalid video plan_json")
	}

	prompt := strings.TrimSpace(plan.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("node_doc_patch: empty video prompt")
	}
	if strings.TrimSpace(instruction) != "" {
		prompt = prompt + "\n\nUser request: " + strings.TrimSpace(instruction)
	}
	prompt = prompt + "\n\nGenerate a fresh variation distinct from prior outputs."
	plan.Prompt = prompt

	dur := plan.DurationSec
	if dur <= 0 {
		dur = 8
	}

	vid, err := deps.AI.GenerateVideo(ctx, prompt, openai.VideoGenerationOptions{DurationSeconds: dur})
	if err != nil {
		return nil, err
	}
	if len(vid.Bytes) == 0 {
		return nil, fmt.Errorf("node_doc_patch: video_generate_empty")
	}

	mime := strings.TrimSpace(vid.MimeType)
	ext := "mp4"
	switch {
	case strings.Contains(strings.ToLower(mime), "webm"):
		ext = "webm"
	case strings.Contains(strings.ToLower(mime), "mp4"):
		ext = "mp4"
	}

	storageKey := fmt.Sprintf("generated/node_videos/%s/%s/slot_%d_%s.%s",
		row.PathID.String(),
		row.PathNodeID.String(),
		row.Slot,
		content.HashBytes([]byte(prompt)),
		ext,
	)
	if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(vid.Bytes)); err != nil {
		return nil, err
	}
	publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)

	if mime == "" {
		if ext == "webm" {
			mime = "video/webm"
		} else {
			mime = "video/mp4"
		}
	}

	var assetID *uuid.UUID
	if deps.Assets != nil {
		meta := map[string]any{
			"asset_kind":     "generated_video",
			"caption":        strings.TrimSpace(plan.Caption),
			"alt_text":       strings.TrimSpace(plan.AltText),
			"placement_hint": strings.TrimSpace(plan.PlacementHint),
			"citations":      content.NormalizeConceptKeys(plan.Citations),
		}
		aid := uuid.New()
		a := &types.Asset{
			ID:         aid,
			Kind:       "video",
			StorageKey: storageKey,
			URL:        publicURL,
			OwnerType:  "learning_node_video",
			OwnerID:    row.ID,
			Metadata:   mustJSON(meta),
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
			assetID = &aid
		}
	}

	planJSON, _ := json.Marshal(plan)
	now := time.Now().UTC()
	update := &types.LearningNodeVideo{
		ID:              row.ID,
		UserID:          row.UserID,
		PathID:          row.PathID,
		PathNodeID:      row.PathNodeID,
		Slot:            row.Slot,
		SchemaVersion:   row.SchemaVersion,
		PlanJSON:        datatypes.JSON(planJSON),
		PromptHash:      content.HashBytes([]byte(prompt)),
		SourcesHash:     row.SourcesHash,
		Status:          "rendered",
		AssetID:         assetID,
		AssetStorageKey: storageKey,
		AssetURL:        publicURL,
		AssetMimeType:   mime,
		Error:           "",
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       now,
	}
	if deps.Videos != nil {
		if err := deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, update); err != nil {
			return nil, err
		}
	}
	return update, nil
}

func updateFigureBlock(block map[string]any, row *types.LearningNodeFigure) map[string]any {
	if block == nil || row == nil {
		return block
	}
	out := map[string]any{}
	for k, v := range block {
		out[k] = v
	}
	asset, _ := out["asset"].(map[string]any)
	if asset == nil {
		asset = map[string]any{}
	}
	asset["url"] = strings.TrimSpace(row.AssetURL)
	asset["storage_key"] = strings.TrimSpace(row.AssetStorageKey)
	if strings.TrimSpace(row.AssetMimeType) != "" {
		asset["mime_type"] = strings.TrimSpace(row.AssetMimeType)
	}
	out["asset"] = asset
	return out
}

func updateVideoBlock(block map[string]any, row *types.LearningNodeVideo) map[string]any {
	if block == nil || row == nil {
		return block
	}
	out := map[string]any{}
	for k, v := range block {
		out[k] = v
	}
	out["url"] = strings.TrimSpace(row.AssetURL)
	return out
}

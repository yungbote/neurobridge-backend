package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
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

type NodeDocPatchPreviewOutput struct {
	PathID          uuid.UUID
	PathNodeID      uuid.UUID
	DocID           uuid.UUID
	BlockID         string
	BlockType       string
	Action          string
	CitationPolicy  string
	Model           string
	PromptVersion   string
	BeforeBlockJSON datatypes.JSON
	AfterBlockJSON  datatypes.JSON
	BeforeBlockText string
	AfterBlockText  string
	BeforeDocJSON   datatypes.JSON
	AfterDocJSON    datatypes.JSON
	IDsChanged      bool
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

	// Optional: apply intake material allowlist (noise filtering / multi-material alignment).
	var allowFiles map[uuid.UUID]bool
	if len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
			allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
		}
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
			if len(allowFiles) > 0 {
				filtered := filterMaterialFilesByAllowlist(files, allowFiles)
				if len(filtered) > 0 {
					files = filtered
				} else {
					deps.Log.Warn("node_doc_patch: intake filter excluded all files; ignoring filter", "path_id", node.PathID.String())
				}
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
				nsSetID := uli.MaterialSetID
				if deps.DB != nil {
					if sc, err := materialsetctx.Resolve(ctx, deps.DB, uli.MaterialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
						nsSetID = sc.SourceMaterialSetID
					}
				}
				ids, qerr := deps.Vec.QueryIDs(ctx, index.ChunksNamespace(nsSetID), queryEmb, semanticK, pineconeChunkFilterWithAllowlist(allowFiles))
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
	docText = content.SanitizeStringForPostgres(docText)

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

func NodeDocPatchPreview(ctx context.Context, deps NodeDocPatchDeps, in NodeDocPatchInput) (NodeDocPatchPreviewOutput, error) {
	out := NodeDocPatchPreviewOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil {
		return out, fmt.Errorf("node_doc_patch_preview: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch_preview: missing owner_user_id")
	}
	if in.PathNodeID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch_preview: missing path_node_id")
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "rewrite"
	}
	if action != "rewrite" && action != "regen_media" {
		return out, fmt.Errorf("node_doc_patch_preview: invalid action %q", action)
	}

	node, err := deps.PathNodes.GetByID(dbctx.Context{Ctx: ctx}, in.PathNodeID)
	if err != nil {
		return out, err
	}
	if node == nil || node.PathID == uuid.Nil {
		return out, fmt.Errorf("node_doc_patch_preview: node not found")
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, node.PathID)
	if err != nil {
		return out, err
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != in.OwnerUserID {
		return out, fmt.Errorf("node_doc_patch_preview: path not found")
	}

	// Optional: apply intake material allowlist.
	var allowFiles map[uuid.UUID]bool
	if len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
			allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
		}
	}

	docRow, err := deps.NodeDocs.GetByPathNodeID(dbctx.Context{Ctx: ctx}, in.PathNodeID)
	if err != nil {
		return out, err
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		return out, fmt.Errorf("node_doc_patch_preview: doc not found")
	}

	var doc content.NodeDocV1
	if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
		return out, fmt.Errorf("node_doc_patch_preview: doc invalid json")
	}

	var idsChanged bool
	if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
		doc = withIDs
		idsChanged = true
	}

	blockID := strings.TrimSpace(in.BlockID)
	idx, resolvedID := findBlockIndex(doc.Blocks, blockID, in.BlockIndex)
	if idx < 0 {
		return out, fmt.Errorf("node_doc_patch_preview: block not found")
	}
	if blockID == "" {
		blockID = resolvedID
	}

	block := doc.Blocks[idx]
	if block == nil {
		return out, fmt.Errorf("node_doc_patch_preview: block not found")
	}
	blockType := strings.ToLower(strings.TrimSpace(stringFromAny(block["type"])))
	if blockType == "" {
		return out, fmt.Errorf("node_doc_patch_preview: block type missing")
	}

	beforeCanon, _ := content.CanonicalizeJSON([]byte(docRow.DocJSON))
	beforeJSON := datatypes.JSON(beforeCanon)
	beforeBlockJSON := mustJSON(block)

	var promptVersion string
	var modelName string
	resolvedPolicy := ""

	switch action {
	case "rewrite":
		if deps.AI == nil {
			return out, fmt.Errorf("node_doc_patch_preview: ai client missing")
		}
		promptVersion = nodeDocPatchPromptVersion
		modelName = openAIModelFromEnv()

		policy := strings.ToLower(strings.TrimSpace(in.CitationPolicy))
		if policy == "" {
			policy = "reuse_only"
		}
		if policy != "reuse_only" && policy != "allow_new" {
			return out, fmt.Errorf("node_doc_patch_preview: invalid citation_policy %q", policy)
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
				return out, fmt.Errorf("node_doc_patch_preview: missing material deps for allow_new")
			}
			uli, err := deps.ULI.GetByUserAndPathID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, node.PathID)
			if err != nil {
				return out, err
			}
			if uli == nil || uli.MaterialSetID == uuid.Nil {
				return out, fmt.Errorf("node_doc_patch_preview: material_set not found")
			}

			files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, uli.MaterialSetID)
			if err != nil {
				return out, err
			}
			if len(allowFiles) > 0 {
				filtered := filterMaterialFilesByAllowlist(files, allowFiles)
				if len(filtered) > 0 {
					files = filtered
				} else {
					deps.Log.Warn("node_doc_patch_preview: intake filter excluded all files; ignoring filter", "path_id", node.PathID.String())
				}
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
				nsSetID := uli.MaterialSetID
				if deps.DB != nil {
					if sc, err := materialsetctx.Resolve(ctx, deps.DB, uli.MaterialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
						nsSetID = sc.SourceMaterialSetID
					}
				}
				ids, qerr := deps.Vec.QueryIDs(ctx, index.ChunksNamespace(nsSetID), queryEmb, semanticK, pineconeChunkFilterWithAllowlist(allowFiles))
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
				return out, fmt.Errorf("node_doc_patch_preview: no chunks retrieved")
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
				return out, fmt.Errorf("node_doc_patch_preview: citation %s not allowed", id)
			}
		}

		doc.Blocks[idx] = updated

		// Deterministic scrub pass.
		if scrubbed, phrases := content.ScrubNodeDocV1(doc); len(phrases) > 0 {
			doc = scrubbed
		}

		reqs := content.NodeDocRequirements{}
		if errs, _ := content.ValidateNodeDocV1(doc, docAllowed, reqs); len(errs) > 0 {
			return out, fmt.Errorf("node_doc_patch_preview: validation failed: %s", strings.Join(errs, "; "))
		}

	case "regen_media":
		if blockType != "figure" && blockType != "video" {
			return out, fmt.Errorf("node_doc_patch_preview: regen_media only supports figure/video blocks")
		}
		if deps.AI == nil || deps.Bucket == nil {
			return out, fmt.Errorf("node_doc_patch_preview: media deps missing")
		}

		modelName = openAIModelFromEnv()
		if blockType == "figure" {
			promptVersion = "node_figure_asset_v1@1"
			row, err := findFigureRow(ctx, deps, node.ID, block)
			if err != nil {
				return out, err
			}
			if row == nil {
				return out, fmt.Errorf("node_doc_patch_preview: figure row not found")
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
				return out, fmt.Errorf("node_doc_patch_preview: video row not found")
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
			return out, fmt.Errorf("node_doc_patch_preview: validation failed: %s", strings.Join(errs, "; "))
		}
	}

	rawDoc, _ := json.Marshal(doc)
	canon, err := content.CanonicalizeJSON(rawDoc)
	if err != nil {
		return out, err
	}
	afterJSON := datatypes.JSON(canon)

	afterBlock := doc.Blocks[idx]
	afterBlockJSON := mustJSON(afterBlock)

	out.PathID = node.PathID
	out.PathNodeID = node.ID
	out.DocID = docRow.ID
	out.BlockID = blockID
	out.BlockType = blockType
	out.Action = action
	out.CitationPolicy = resolvedPolicy
	out.Model = strings.TrimSpace(modelName)
	out.PromptVersion = strings.TrimSpace(promptVersion)
	out.BeforeBlockJSON = beforeBlockJSON
	out.AfterBlockJSON = afterBlockJSON
	out.BeforeBlockText = blockTextForQuery(blockType, block)
	out.AfterBlockText = blockTextForQuery(blockType, afterBlock)
	out.BeforeDocJSON = beforeJSON
	out.AfterDocJSON = afterJSON
	out.IDsChanged = idsChanged

	return out, nil
}

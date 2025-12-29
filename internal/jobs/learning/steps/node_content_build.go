package steps

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type NodeContentBuildDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo

	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo

	UserProfile repos.UserProfileVectorRepo

	AI  openai.Client
	Vec pc.VectorStore

	// Optional: used to turn asset_key (GCS key) into public URLs for image/video blocks.
	Bucket gcp.BucketService

	Bootstrap services.LearningBuildBootstrapService
}

type NodeContentBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
}

type NodeContentBuildOutput struct {
	PathID        uuid.UUID `json:"path_id"`
	NodesWritten  int       `json:"nodes_written"`
	NodesExisting int       `json:"nodes_existing"`
}

type chunkEmbedding struct {
	ID  uuid.UUID
	Emb []float32
}

func NodeContentBuild(ctx context.Context, deps NodeContentBuildDeps, in NodeContentBuildInput) (NodeContentBuildOutput, error) {
	out := NodeContentBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Files == nil || deps.Chunks == nil || deps.UserProfile == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_content_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_content_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_content_build: missing material_set_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
	if err != nil || up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("node_content_build: missing user_profile_doc (run user_profile_refresh first)")
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	charterJSON := ""
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
			if v, ok := meta["charter"]; ok && v != nil {
				if b, err := json.Marshal(v); err == nil {
					charterJSON = string(b)
				}
			}
		}
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_content_build: no path nodes (run path_plan_build first)")
	}
	filteredNodes := make([]*types.PathNode, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			filteredNodes = append(filteredNodes, n)
		}
	}
	sort.Slice(filteredNodes, func(i, j int) bool { return filteredNodes[i].Index < filteredNodes[j].Index })

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
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
	if len(allChunks) == 0 {
		return out, fmt.Errorf("node_content_build: no chunks for material set")
	}

	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	embByID := map[uuid.UUID][]float32{}
	for _, ch := range allChunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		chunkByID[ch.ID] = ch
		if v, ok := decodeEmbedding(ch.Embedding); ok {
			embByID[ch.ID] = v
		}
	}

	buildExcerpts := func(ids []uuid.UUID, maxLines int, maxChars int) string {
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
			ch := chunkByID[id]
			if ch == nil {
				continue
			}
			txt := shorten(ch.Text, maxChars)
			if strings.TrimSpace(txt) == "" {
				continue
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

	chunksNS := index.ChunksNamespace(in.MaterialSetID)

	now := time.Now().UTC()

	type nodeWork struct {
		Node       *types.PathNode
		ConceptCSV string
		QueryText  string
		QueryEmb   []float32
	}
	work := make([]nodeWork, 0, len(filteredNodes))
	for _, node := range filteredNodes {
		if node == nil || node.ID == uuid.Nil {
			continue
		}
		if len(node.ContentJSON) > 0 && strings.TrimSpace(string(node.ContentJSON)) != "" && string(node.ContentJSON) != "null" {
			out.NodesExisting++
			continue
		}

		nodeMeta := map[string]any{}
		if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
			_ = json.Unmarshal(node.Metadata, &nodeMeta)
		}
		nodeGoal := strings.TrimSpace(stringFromAny(nodeMeta["goal"]))
		nodeConceptKeys := dedupeStrings(stringSliceFromAny(nodeMeta["concept_keys"]))
		conceptCSV := strings.Join(nodeConceptKeys, ", ")
		queryText := strings.TrimSpace(node.Title + " " + nodeGoal + " " + conceptCSV)

		work = append(work, nodeWork{
			Node:       node,
			ConceptCSV: conceptCSV,
			QueryText:  queryText,
		})
	}

	if len(work) == 0 {
		return out, nil
	}

	// Batch query embeddings to minimize API calls.
	queryTexts := make([]string, 0, len(work))
	for _, w := range work {
		queryTexts = append(queryTexts, w.QueryText)
	}
	queryEmbs, err := deps.AI.Embed(ctx, queryTexts)
	if err != nil {
		return out, err
	}
	if len(queryEmbs) != len(work) {
		return out, fmt.Errorf("node_content_build: embedding count mismatch (got %d want %d)", len(queryEmbs), len(work))
	}
	for i := range work {
		work[i].QueryEmb = queryEmbs[i]
		if len(work[i].QueryEmb) == 0 {
			return out, fmt.Errorf("node_content_build: empty query embedding")
		}
	}

	// Precompute deterministic local-embedding scan order for fallback retrieval.
	chunkEmbs := make([]chunkEmbedding, 0, len(embByID))
	for id, emb := range embByID {
		if id == uuid.Nil || len(emb) == 0 {
			continue
		}
		chunkEmbs = append(chunkEmbs, chunkEmbedding{ID: id, Emb: emb})
	}
	sort.Slice(chunkEmbs, func(i, j int) bool { return chunkEmbs[i].ID.String() < chunkEmbs[j].ID.String() })

	maxConc := envInt("NODE_CONTENT_BUILD_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}
	if maxConc > 16 {
		maxConc = 16
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)
	var written int32

	for i := range work {
		w := work[i]
		g.Go(func() error {
			if w.Node == nil || w.Node.ID == uuid.Nil {
				return nil
			}

			var chunkIDs []uuid.UUID
			if deps.Vec != nil {
				ids, qerr := deps.Vec.QueryIDs(gctx, chunksNS, w.QueryEmb, 14, map[string]any{"type": "chunk"})
				if qerr == nil && len(ids) > 0 {
					for _, s := range ids {
						if id, e := uuid.Parse(strings.TrimSpace(s)); e == nil && id != uuid.Nil {
							chunkIDs = append(chunkIDs, id)
						}
					}
				}
			}
			if len(chunkIDs) == 0 {
				if len(chunkEmbs) == 0 {
					return fmt.Errorf("node_content_build: no local embeddings available (run embed_chunks first)")
				}
				chunkIDs = topKChunkIDsByCosine(w.QueryEmb, chunkEmbs, 14)
			}

			excerpts := buildExcerpts(chunkIDs, 14, 850)
			if strings.TrimSpace(excerpts) == "" {
				return fmt.Errorf("node_content_build: empty grounding excerpts")
			}

			assetsJSON := buildAvailableAssetsJSON(deps.Bucket, files, chunkByID, chunkIDs, nil)

			p, err := prompts.Build(prompts.PromptActivityContent, prompts.Input{
				UserProfileDoc:   up.ProfileDoc,
				PathCharterJSON:  charterJSON,
				ActivityKind:     "lesson",
				ActivityTitle:    w.Node.Title,
				ConceptKeysCSV:   w.ConceptCSV,
				ActivityExcerpts: excerpts,
				AssetsJSON:       assetsJSON,
			})
			if err != nil {
				return err
			}
			obj, err := deps.AI.GenerateJSON(gctx, p.System, p.User, p.SchemaName, p.Schema)
			if err != nil {
				return err
			}

			content, _ := json.Marshal(obj["content_json"])
			if len(content) == 0 || string(content) == "null" {
				return fmt.Errorf("node_content_build: empty content_json returned")
			}

			if err := deps.PathNodes.UpdateFields(dbctx.Context{Ctx: ctx}, w.Node.ID, map[string]interface{}{
				"content_json": datatypes.JSON(content),
				"updated_at":   now,
			}); err != nil {
				return err
			}

			atomic.AddInt32(&written, 1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.NodesWritten = int(atomic.LoadInt32(&written))

	return out, nil
}

type mediaAssetCandidate struct {
	Kind      string   `json:"kind"`
	URL       string   `json:"url"`
	Key       string   `json:"key,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	ChunkIDs  []string `json:"chunk_ids,omitempty"`
	Page      *int     `json:"page,omitempty"`
	StartSec  *float64 `json:"start_sec,omitempty"`
	EndSec    *float64 `json:"end_sec,omitempty"`
	FileName  string   `json:"file_name,omitempty"`
	MimeType  string   `json:"mime_type,omitempty"`
	Source    string   `json:"source,omitempty"` // upload|derived
	AssetKind string   `json:"asset_kind,omitempty"`
}

func buildAvailableAssetsJSON(bucket gcp.BucketService, files []*types.MaterialFile, chunkByID map[uuid.UUID]*types.MaterialChunk, chunkIDs []uuid.UUID, extras []*mediaAssetCandidate) string {
	seen := map[string]*mediaAssetCandidate{}

	add := func(c *mediaAssetCandidate) {
		if c == nil {
			return
		}
		c.URL = strings.TrimSpace(c.URL)
		if c.URL == "" {
			return
		}
		if _, ok := seen[c.URL]; ok {
			ex := seen[c.URL]
			ex.ChunkIDs = dedupeStrings(append(ex.ChunkIDs, c.ChunkIDs...))
			if ex.Notes == "" {
				ex.Notes = c.Notes
			}
			if ex.Kind == "" {
				ex.Kind = c.Kind
			}
			if ex.Key == "" {
				ex.Key = c.Key
			}
			if ex.Source == "" {
				ex.Source = c.Source
			}
			if ex.Page == nil && c.Page != nil {
				ex.Page = c.Page
			}
			if ex.StartSec == nil && c.StartSec != nil {
				ex.StartSec = c.StartSec
			}
			if ex.EndSec == nil && c.EndSec != nil {
				ex.EndSec = c.EndSec
			}
			return
		}
		seen[c.URL] = c
	}

	// (0) Extras (e.g., generated figures) should be preferred if we have to truncate.
	for _, c := range extras {
		add(c)
	}

	// (A) Original uploads that are directly embeddable media (image/video).
	for _, f := range files {
		if f == nil {
			continue
		}
		mt := strings.ToLower(strings.TrimSpace(f.MimeType))
		isImg := strings.HasPrefix(mt, "image/")
		isVid := strings.HasPrefix(mt, "video/")
		if !isImg && !isVid {
			continue
		}
		url := strings.TrimSpace(f.FileURL)
		if url == "" && bucket != nil && strings.TrimSpace(f.StorageKey) != "" {
			url = bucket.GetPublicURL(gcp.BucketCategoryMaterial, strings.TrimSpace(f.StorageKey))
		}
		if url == "" {
			continue
		}
		kind := "file"
		if isImg {
			kind = "image"
		} else if isVid {
			kind = "video"
		}
		add(&mediaAssetCandidate{
			Kind:     kind,
			URL:      url,
			Key:      strings.TrimSpace(f.StorageKey),
			FileName: strings.TrimSpace(f.OriginalName),
			MimeType: strings.TrimSpace(f.MimeType),
			Source:   "upload",
			Notes:    "original upload",
		})
	}

	// (B) Derived assets referenced by chunk metadata (e.g., rendered PDF pages, video frames).
	for _, id := range chunkIDs {
		ch := chunkByID[id]
		if ch == nil {
			continue
		}
		assetKey, page, startSec, endSec, metaKind := chunkAssetKeyAndMeta(ch)
		if strings.TrimSpace(assetKey) == "" {
			continue
		}
		url := strings.TrimSpace(assetKey)
		isURL := strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "data:image/")
		if !isURL {
			if bucket == nil {
				continue
			}
			url = bucket.GetPublicURL(gcp.BucketCategoryMaterial, strings.TrimSpace(assetKey))
		}
		kind := mediaKindFromRef(url)
		if kind == "" {
			kind = mediaKindFromRef(assetKey)
		}
		if kind != "image" && kind != "video" {
			continue
		}
		notesParts := make([]string, 0, 4)
		if metaKind != "" {
			notesParts = append(notesParts, metaKind)
		}
		if page != nil && *page > 0 {
			notesParts = append(notesParts, fmt.Sprintf("page=%d", *page))
		}
		if startSec != nil && endSec != nil {
			notesParts = append(notesParts, fmt.Sprintf("t=%.1fs-%.1fs", *startSec, *endSec))
		} else if startSec != nil {
			notesParts = append(notesParts, fmt.Sprintf("t=%.1fs", *startSec))
		}
		captionHint := strings.TrimSpace(shorten(ch.Text, 160))
		if captionHint != "" {
			notesParts = append(notesParts, "hint="+captionHint)
		}
		add(&mediaAssetCandidate{
			Kind:     kind,
			URL:      url,
			Key:      strings.TrimSpace(assetKey),
			Notes:    strings.Join(notesParts, " | "),
			ChunkIDs: []string{id.String()},
			Page:     page,
			StartSec: startSec,
			EndSec:   endSec,
			Source:   "derived",
		})
	}

	if len(seen) == 0 {
		return ""
	}

	out := make([]*mediaAssetCandidate, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })

	// Keep prompts compact.
	const maxAssets = 12
	if len(out) > maxAssets {
		priority := make([]*mediaAssetCandidate, 0, len(out))
		rest := make([]*mediaAssetCandidate, 0, len(out))
		for _, a := range out {
			ak := ""
			if a != nil {
				ak = strings.ToLower(strings.TrimSpace(a.AssetKind))
			}
			if a != nil && (ak == "generated_figure" || ak == "generated_video") {
				priority = append(priority, a)
			} else {
				rest = append(rest, a)
			}
		}
		if len(priority) > maxAssets {
			priority = priority[:maxAssets]
			out = priority
		} else {
			remain := maxAssets - len(priority)
			if len(rest) > remain {
				rest = rest[:remain]
			}
			out = append(priority, rest...)
		}
	}

	b, _ := json.Marshal(map[string]any{"assets": out})
	return string(b)
}

func chunkAssetKeyAndMeta(ch *types.MaterialChunk) (assetKey string, page *int, startSec *float64, endSec *float64, kind string) {
	if ch == nil || len(ch.Metadata) == 0 || strings.TrimSpace(string(ch.Metadata)) == "" || strings.TrimSpace(string(ch.Metadata)) == "null" {
		return "", nil, nil, nil, ""
	}
	var meta map[string]any
	if err := json.Unmarshal(ch.Metadata, &meta); err != nil {
		return "", nil, nil, nil, ""
	}
	assetKey = strings.TrimSpace(stringFromAny(meta["asset_key"]))
	if assetKey == "" {
		assetKey = strings.TrimSpace(stringFromAny(meta["assetKey"]))
	}
	kind = strings.TrimSpace(stringFromAny(meta["kind"]))
	if p := intFromAny(meta["page"], 0); p > 0 {
		page = &p
	}
	if v, ok := meta["start_sec"]; ok {
		f := floatFromAny(v, 0)
		if f > 0 {
			startSec = &f
		}
	}
	if v, ok := meta["end_sec"]; ok {
		f := floatFromAny(v, 0)
		if f > 0 {
			endSec = &f
		}
	}
	return assetKey, page, startSec, endSec, kind
}

func mediaKindFromRef(ref string) string {
	s := strings.ToLower(strings.TrimSpace(ref))
	if s == "" {
		return ""
	}
	// Strip query string for URLs.
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	switch {
	case strings.HasPrefix(s, "data:image/"):
		return "image"
	case strings.HasSuffix(s, ".png"), strings.HasSuffix(s, ".jpg"), strings.HasSuffix(s, ".jpeg"), strings.HasSuffix(s, ".webp"), strings.HasSuffix(s, ".gif"), strings.HasSuffix(s, ".svg"):
		return "image"
	case strings.HasSuffix(s, ".mp4"), strings.HasSuffix(s, ".webm"), strings.HasSuffix(s, ".mov"), strings.HasSuffix(s, ".m4v"):
		return "video"
	case strings.HasSuffix(s, ".mp3"), strings.HasSuffix(s, ".wav"), strings.HasSuffix(s, ".m4a"), strings.HasSuffix(s, ".ogg"):
		return "audio"
	default:
		return ""
	}
}

type scoredChunk struct {
	ID    uuid.UUID
	Score float64
}

type scoredChunkMinHeap []scoredChunk

func (h scoredChunkMinHeap) Len() int { return len(h) }
func (h scoredChunkMinHeap) Less(i, j int) bool {
	if h[i].Score == h[j].Score {
		return h[i].ID.String() < h[j].ID.String()
	}
	return h[i].Score < h[j].Score
}
func (h scoredChunkMinHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *scoredChunkMinHeap) Push(x interface{}) { *h = append(*h, x.(scoredChunk)) }
func (h *scoredChunkMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

func topKChunkIDsByCosine(query []float32, embs []chunkEmbedding, k int) []uuid.UUID {
	if k <= 0 {
		return nil
	}
	if k > len(embs) {
		k = len(embs)
	}
	h := &scoredChunkMinHeap{}
	heap.Init(h)

	for _, ch := range embs {
		if ch.ID == uuid.Nil || len(ch.Emb) == 0 {
			continue
		}
		score := cosineSim(query, ch.Emb)
		if h.Len() < k {
			heap.Push(h, scoredChunk{ID: ch.ID, Score: score})
			continue
		}
		// Replace smallest if better.
		if h.Len() > 0 && score > (*h)[0].Score {
			(*h)[0] = scoredChunk{ID: ch.ID, Score: score}
			heap.Fix(h, 0)
		}
	}

	tmp := make([]scoredChunk, 0, h.Len())
	for h.Len() > 0 {
		tmp = append(tmp, heap.Pop(h).(scoredChunk))
	}
	sort.Slice(tmp, func(i, j int) bool {
		if tmp[i].Score == tmp[j].Score {
			return tmp[i].ID.String() < tmp[j].ID.String()
		}
		return tmp[i].Score > tmp[j].Score
	})
	out := make([]uuid.UUID, 0, len(tmp))
	for _, it := range tmp {
		out = append(out, it.ID)
	}
	return out
}

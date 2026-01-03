package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathCoverRenderDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Assets    repos.AssetRepo

	AI     openai.Client
	Bucket gcp.BucketService
}

type PathCoverRenderInput struct {
	PathID uuid.UUID
	Force  bool `json:"force,omitempty"`
}

type PathCoverRenderOutput struct {
	PathID     uuid.UUID  `json:"path_id"`
	Generated  bool       `json:"generated"`
	Existing   bool       `json:"existing"`
	URL        string     `json:"url,omitempty"`
	AssetID    *uuid.UUID `json:"asset_id,omitempty"`
	InputsHash string     `json:"inputs_hash,omitempty"`
}

const pathCoverPromptVersion = "path_cover_v1@1"

func PathCoverRender(ctx context.Context, deps PathCoverRenderDeps, in PathCoverRenderInput) (PathCoverRenderOutput, error) {
	out := PathCoverRenderOutput{PathID: in.PathID}
	if deps.Path == nil || deps.PathNodes == nil || deps.Assets == nil || deps.AI == nil {
		return out, fmt.Errorf("path_cover_render: missing deps")
	}
	if in.PathID == uuid.Nil {
		return out, fmt.Errorf("path_cover_render: missing path_id")
	}

	// Feature gate: require image model + bucket configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		if deps.Log != nil {
			deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping path_cover_render")
		}
		return out, nil
	}
	if deps.Bucket == nil {
		if deps.Log != nil {
			deps.Log.Warn("Bucket service missing; skipping path_cover_render")
		}
		return out, nil
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, in.PathID)
	if err != nil {
		return out, err
	}
	if pathRow == nil || pathRow.ID == uuid.Nil {
		return out, fmt.Errorf("path_cover_render: path not found")
	}

	meta := parseJSONMap(pathRow.Metadata)
	if !in.Force {
		if existingURL := coverURLFromMeta(meta); existingURL != "" {
			out.Existing = true
			out.URL = existingURL
			return out, nil
		}
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathRow.ID})
	if err != nil {
		return out, err
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i] == nil || nodes[j] == nil {
			return false
		}
		return nodes[i].Index < nodes[j].Index
	})

	topics := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		title := strings.TrimSpace(n.Title)
		if title != "" {
			topics = append(topics, title)
		}
	}
	topics = dedupeStrings(topics)
	if len(topics) > 8 {
		topics = topics[:8]
	}

	title := strings.TrimSpace(pathRow.Title)
	desc := strings.TrimSpace(pathRow.Description)
	inputsHash := coverInputsHash(title, desc, topics)
	out.InputsHash = inputsHash

	prompt := buildPathCoverPrompt(title, desc, topics)
	img, err := deps.AI.GenerateImage(ctx, prompt)
	if err != nil {
		return out, err
	}
	if len(img.Bytes) == 0 {
		return out, fmt.Errorf("path_cover_render: image_generate_empty")
	}

	now := time.Now().UTC()
	hashSuffix := shortHash(inputsHash)
	storageKey := fmt.Sprintf("generated/path_avatars/%s/%s_%s.png",
		pathRow.ID.String(),
		now.Format("20060102T150405Z"),
		hashSuffix,
	)
	if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryAvatar, storageKey, bytes.NewReader(img.Bytes)); err != nil {
		return out, err
	}

	publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryAvatar, storageKey)
	mime := strings.TrimSpace(img.MimeType)
	if mime == "" {
		mime = "image/png"
	}

	var assetID *uuid.UUID
	if deps.Assets != nil {
		aid := uuid.New()
		meta := map[string]any{
			"asset_kind":     "path_cover",
			"prompt_hash":    inputsHash,
			"prompt_version": pathCoverPromptVersion,
			"revised_prompt": strings.TrimSpace(img.RevisedPrompt),
			"path_title":     title,
			"topics":         topics,
		}
		b, _ := json.Marshal(meta)
		a := &types.Asset{
			ID:         aid,
			Kind:       "image",
			StorageKey: storageKey,
			URL:        publicURL,
			OwnerType:  "path",
			OwnerID:    pathRow.ID,
			Metadata:   datatypes.JSON(b),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
			assetID = &aid
		}
	}

	coverMeta := map[string]any{
		"url":            publicURL,
		"storage_key":    storageKey,
		"mime_type":      mime,
		"prompt_hash":    inputsHash,
		"prompt_version": pathCoverPromptVersion,
		"revised_prompt": strings.TrimSpace(img.RevisedPrompt),
		"generated_at":   now.Format(time.RFC3339Nano),
	}
	if assetID != nil {
		coverMeta["asset_id"] = assetID.String()
	}
	meta["cover_image"] = coverMeta
	meta["cover_image_url"] = publicURL
	meta["updated_at"] = now.Format(time.RFC3339Nano)

	if err := deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathRow.ID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(meta)),
	}); err != nil {
		return out, err
	}

	out.Generated = true
	out.URL = publicURL
	out.AssetID = assetID
	return out, nil
}

func coverInputsHash(title, desc string, topics []string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(title),
		strings.TrimSpace(desc),
		strings.Join(topics, "|"),
	}, "\n")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func shortHash(hash string) string {
	h := strings.TrimSpace(hash)
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func buildPathCoverPrompt(title, desc string, topics []string) string {
	if strings.TrimSpace(title) == "" {
		title = "Learning Path"
	}
	if strings.TrimSpace(desc) == "" {
		desc = "A curated learning journey."
	}
	var b strings.Builder
	b.WriteString("Design a premium, modern cover image for a learning path.\n")
	b.WriteString("Title: " + strings.TrimSpace(title) + "\n")
	b.WriteString("Description: " + strings.TrimSpace(desc) + "\n")
	if len(topics) > 0 {
		b.WriteString("Key topics: " + strings.Join(topics, ", ") + "\n")
	}
	b.WriteString("Style: elegant, minimal editorial illustration; subtle gradients; balanced composition; soft depth; refined palette.")
	b.WriteString(" No text, no logos, no watermarks, no UI, no borders, avoid identifiable people/faces.")
	b.WriteString(" Format: square 1:1 with a centered focal point.")
	return b.String()
}

func parseJSONMap(raw datatypes.JSON) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func coverURLFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["cover_image"].(map[string]any); ok {
		if url := strings.TrimSpace(stringFromAny(v["url"])); url != "" {
			return url
		}
	}
	if url := strings.TrimSpace(stringFromAny(meta["cover_image_url"])); url != "" {
		return url
	}
	if url := strings.TrimSpace(stringFromAny(meta["coverImageUrl"])); url != "" {
		return url
	}
	return ""
}

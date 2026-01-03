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

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type NodeAvatarRenderDeps struct {
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Assets    repos.AssetRepo

	AI     openai.Client
	Bucket gcp.BucketService
}

type NodeAvatarRenderInput struct {
	PathID uuid.UUID
	Force  bool `json:"force,omitempty"`
}

type NodeAvatarRenderOutput struct {
	PathID    uuid.UUID `json:"path_id"`
	Generated int       `json:"generated"`
	Existing  int       `json:"existing"`
	Failed    int       `json:"failed"`
}

const nodeAvatarPromptVersion = "node_avatar_v1@1"

func NodeAvatarRender(ctx context.Context, deps NodeAvatarRenderDeps, in NodeAvatarRenderInput) (NodeAvatarRenderOutput, error) {
	out := NodeAvatarRenderOutput{PathID: in.PathID}
	if deps.Path == nil || deps.PathNodes == nil || deps.AI == nil {
		return out, fmt.Errorf("node_avatar_render: missing deps")
	}
	if in.PathID == uuid.Nil {
		return out, fmt.Errorf("node_avatar_render: missing path_id")
	}

	// Feature gate: require image model + bucket configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		if deps.Log != nil {
			deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping node_avatar_render")
		}
		return out, nil
	}
	if deps.Bucket == nil {
		if deps.Log != nil {
			deps.Log.Warn("Bucket service missing; skipping node_avatar_render")
		}
		return out, nil
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, in.PathID)
	if err != nil {
		return out, err
	}
	if pathRow == nil || pathRow.ID == uuid.Nil {
		return out, fmt.Errorf("node_avatar_render: path not found")
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathRow.ID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, nil
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i] == nil || nodes[j] == nil {
			return false
		}
		return nodes[i].Index < nodes[j].Index
	})

	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		meta := parseJSONMap(n.Metadata)
		if !in.Force {
			if existingURL := nodeAvatarURLFromMeta(meta); existingURL != "" {
				out.Existing++
				continue
			}
		}

		title := strings.TrimSpace(n.Title)
		goal := strings.TrimSpace(stringFromAny(meta["goal"]))
		topics := dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
		if len(topics) > 6 {
			topics = topics[:6]
		}

		inputsHash := nodeAvatarInputsHash(title, goal, topics)
		prompt := buildNodeAvatarPrompt(strings.TrimSpace(pathRow.Title), title, goal, topics)
		img, err := deps.AI.GenerateImage(ctx, prompt)
		if err != nil {
			out.Failed++
			if deps.Log != nil {
				deps.Log.Warn("node_avatar_render generate failed", "error", err, "path_id", pathRow.ID.String(), "node_id", n.ID.String())
			}
			continue
		}
		if len(img.Bytes) == 0 {
			out.Failed++
			continue
		}

		now := time.Now().UTC()
		hashSuffix := shortHash(inputsHash)
		storageKey := fmt.Sprintf("generated/unit_avatars/%s/%s/%s_%s.png",
			pathRow.ID.String(),
			n.ID.String(),
			now.Format("20060102T150405Z"),
			hashSuffix,
		)
		if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryAvatar, storageKey, bytes.NewReader(img.Bytes)); err != nil {
			out.Failed++
			if deps.Log != nil {
				deps.Log.Warn("node_avatar_render upload failed", "error", err, "path_id", pathRow.ID.String(), "node_id", n.ID.String())
			}
			continue
		}

		publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryAvatar, storageKey)
		mime := strings.TrimSpace(img.MimeType)
		if mime == "" {
			mime = "image/png"
		}

		var assetID *uuid.UUID
		if deps.Assets != nil {
			aid := uuid.New()
			assetMeta := map[string]any{
				"asset_kind":     "unit_avatar",
				"prompt_hash":    inputsHash,
				"prompt_version": nodeAvatarPromptVersion,
				"revised_prompt": strings.TrimSpace(img.RevisedPrompt),
				"path_title":     strings.TrimSpace(pathRow.Title),
				"node_title":     title,
				"topics":         topics,
			}
			b, _ := json.Marshal(assetMeta)
			a := &types.Asset{
				ID:         aid,
				Kind:       "image",
				StorageKey: storageKey,
				URL:        publicURL,
				OwnerType:  "path_node",
				OwnerID:    n.ID,
				Metadata:   datatypes.JSON(b),
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
				assetID = &aid
			}
		}

		avatarMeta := map[string]any{
			"url":            publicURL,
			"storage_key":    storageKey,
			"mime_type":      mime,
			"prompt_hash":    inputsHash,
			"prompt_version": nodeAvatarPromptVersion,
			"revised_prompt": strings.TrimSpace(img.RevisedPrompt),
			"generated_at":   now.Format(time.RFC3339Nano),
		}
		if assetID != nil {
			avatarMeta["asset_id"] = assetID.String()
		}

		meta["avatar_image"] = avatarMeta
		meta["avatar_image_url"] = publicURL
		meta["updated_at"] = now.Format(time.RFC3339Nano)

		if err := deps.PathNodes.UpdateFields(dbctx.Context{Ctx: ctx}, n.ID, map[string]interface{}{
			"metadata": datatypes.JSON(mustJSON(meta)),
		}); err != nil {
			out.Failed++
			if deps.Log != nil {
				deps.Log.Warn("node_avatar_render update failed", "error", err, "path_id", pathRow.ID.String(), "node_id", n.ID.String())
			}
			continue
		}

		out.Generated++
	}

	return out, nil
}

func nodeAvatarInputsHash(title, goal string, topics []string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(title),
		strings.TrimSpace(goal),
		strings.Join(topics, "|"),
	}, "\n")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func buildNodeAvatarPrompt(pathTitle, title, goal string, topics []string) string {
	if strings.TrimSpace(title) == "" {
		title = "Learning unit"
	}
	if strings.TrimSpace(pathTitle) == "" {
		pathTitle = "Learning Path"
	}
	var b strings.Builder
	b.WriteString("Create a premium, minimal avatar illustration for a learning unit.\n")
	b.WriteString("Path: " + strings.TrimSpace(pathTitle) + "\n")
	b.WriteString("Unit: " + strings.TrimSpace(title) + "\n")
	if strings.TrimSpace(goal) != "" {
		b.WriteString("Goal: " + strings.TrimSpace(goal) + "\n")
	}
	if len(topics) > 0 {
		b.WriteString("Key topics: " + strings.Join(topics, ", ") + "\n")
	}
	b.WriteString("Style: elegant, minimal icon-like illustration; subtle gradients; crisp silhouette; refined palette.")
	b.WriteString(" No text, no letters, no logos, no UI, no borders, avoid identifiable people/faces.")
	b.WriteString(" Format: square 1:1 with a centered focal element.")
	return b.String()
}

func nodeAvatarURLFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["avatar_image"].(map[string]any); ok {
		if url := strings.TrimSpace(stringFromAny(v["url"])); url != "" {
			return url
		}
	}
	if url := strings.TrimSpace(stringFromAny(meta["avatar_image_url"])); url != "" {
		return url
	}
	if url := strings.TrimSpace(stringFromAny(meta["avatarImageUrl"])); url != "" {
		return url
	}
	return ""
}

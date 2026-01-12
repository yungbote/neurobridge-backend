package steps

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type NodeAvatarRenderDeps struct {
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo

	Avatar services.AvatarService
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
	if deps.Path == nil || deps.PathNodes == nil || deps.Avatar == nil {
		return out, fmt.Errorf("node_avatar_render: missing deps")
	}
	if in.PathID == uuid.Nil {
		return out, fmt.Errorf("node_avatar_render: missing path_id")
	}

	// Feature gate: require image model configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		if deps.Log != nil {
			deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping node_avatar_render")
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
		if !in.Force {
			if existingURL := strings.TrimSpace(n.AvatarURL); existingURL != "" && strings.TrimSpace(n.AvatarSquareURL) != "" {
				out.Existing++
				continue
			}
		}

		meta := parseJSONMap(n.Metadata)
		title := strings.TrimSpace(n.Title)
		goal := strings.TrimSpace(stringFromAny(meta["goal"]))
		topics := dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
		if len(topics) > 6 {
			topics = topics[:6]
		}

		prompt := buildNodeAvatarPrompt(strings.TrimSpace(pathRow.Title), title, goal, topics)
		if err := deps.Avatar.CreateAndUploadPathNodeAvatar(dbctx.Context{Ctx: ctx}, n, prompt); err != nil {
			out.Failed++
			if deps.Log != nil {
				deps.Log.Warn("node_avatar_render failed", "error", err, "path_id", pathRow.ID.String(), "node_id", n.ID.String())
			}
			continue
		}

		if err := deps.PathNodes.UpdateFields(dbctx.Context{Ctx: ctx}, n.ID, map[string]interface{}{
			"avatar_bucket_key":        strings.TrimSpace(n.AvatarBucketKey),
			"avatar_url":               strings.TrimSpace(n.AvatarURL),
			"avatar_square_bucket_key": strings.TrimSpace(n.AvatarSquareBucketKey),
			"avatar_square_url":        strings.TrimSpace(n.AvatarSquareURL),
			"updated_at":               time.Now().UTC(),
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

func buildNodeAvatarPrompt(pathTitle, title, goal string, topics []string) string {
	if strings.TrimSpace(title) == "" {
		title = "Learning unit"
	}
	if strings.TrimSpace(pathTitle) == "" {
		pathTitle = "Learning Path"
	}
	var b strings.Builder
	b.WriteString("Create a clean, human-designed avatar illustration for a learning unit.\n")
	b.WriteString("Goal: premium, design-forward, polished (not 'AI-looking').\n")
	b.WriteString("Path: " + strings.TrimSpace(pathTitle) + "\n")
	b.WriteString("Unit: " + strings.TrimSpace(title) + "\n")
	if strings.TrimSpace(goal) != "" {
		b.WriteString("Goal: " + strings.TrimSpace(goal) + "\n")
	}
	if len(topics) > 0 {
		b.WriteString("Themes for inspiration (pick ONE and compress into a single motif): " + strings.Join(topics, ", ") + "\n")
	}
	b.WriteString("Composition: one clear focal symbol (max 1–2 elements); no collage; lots of negative space; readable at small sizes.\n")
	b.WriteString("Style: crisp icon-like illustration; clean geometry; subtle gradients; consistent lighting; refined, cohesive palette; simple background.\n")
	b.WriteString("Avoid: clutter, multiple unrelated icons, busy scenes, photorealism, noisy textures, warped geometry, artifacts, text/letters/numbers, logos, watermarks, UI, borders, identifiable people/faces.\n")
	b.WriteString("Format: square 1:1, centered focal element.")
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

package steps

import (
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

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathCoverRenderDeps struct {
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo

	Avatar services.AvatarService
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
	if deps.Path == nil || deps.PathNodes == nil || deps.Avatar == nil {
		return out, fmt.Errorf("path_cover_render: missing deps")
	}
	if in.PathID == uuid.Nil {
		return out, fmt.Errorf("path_cover_render: missing path_id")
	}

	// Feature gate: require image model configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		if deps.Log != nil {
			deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping path_cover_render")
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

	if !in.Force {
		if existingURL := strings.TrimSpace(pathRow.AvatarURL); existingURL != "" && strings.TrimSpace(pathRow.AvatarSquareURL) != "" {
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
	if err := deps.Avatar.CreateAndUploadPathAvatar(dbctx.Context{Ctx: ctx}, pathRow, prompt); err != nil {
		return out, err
	}

	if err := deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathRow.ID, map[string]interface{}{
		"avatar_bucket_key":        strings.TrimSpace(pathRow.AvatarBucketKey),
		"avatar_url":               strings.TrimSpace(pathRow.AvatarURL),
		"avatar_square_bucket_key": strings.TrimSpace(pathRow.AvatarSquareBucketKey),
		"avatar_square_url":        strings.TrimSpace(pathRow.AvatarSquareURL),
		"updated_at":               time.Now().UTC(),
	}); err != nil {
		return out, err
	}

	out.Generated = true
	out.URL = strings.TrimSpace(pathRow.AvatarURL)
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
	b.WriteString("Create a clean, human-designed avatar illustration for a learning path.\n")
	b.WriteString("Goal: premium, design-forward, polished (not 'AI-looking').\n")
	b.WriteString("Title: " + strings.TrimSpace(title) + "\n")
	b.WriteString("Description: " + strings.TrimSpace(desc) + "\n")
	if len(topics) > 0 {
		b.WriteString("Themes for inspiration (pick ONE and compress into a single motif): " + strings.Join(topics, ", ") + "\n")
	}
	b.WriteString("Composition: one clear focal symbol (max 1–2 elements); no collage; lots of negative space; readable at small sizes.\n")
	b.WriteString("Style: crisp vector-like shapes or clean 3D-lite forms; subtle gradients; consistent lighting; refined, cohesive palette; simple background.\n")
	b.WriteString("Avoid: clutter, multiple unrelated icons, busy scenes, photorealism, noisy textures, warped geometry, artifacts, text/letters/numbers, logos, watermarks, UI, borders, identifiable people/faces.\n")
	b.WriteString("Format: square 1:1, centered composition.")
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

package steps

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const artifactHashVersion = 1

func artifactCacheEnabled() bool {
	return envBool("LEARNING_ARTIFACT_CACHE_ENABLED", true)
}

func artifactCacheSeedExisting() bool {
	return envBool("LEARNING_ARTIFACT_CACHE_SEED_EXISTING", true)
}

func artifactCacheGet(ctx context.Context, repo repos.LearningArtifactRepo, ownerID, setID, pathID uuid.UUID, artifactType string, inputHash string) (*types.LearningArtifact, bool, error) {
	if repo == nil || !artifactCacheEnabled() {
		return nil, false, nil
	}
	row, err := repo.GetByKey(dbctx.Context{Ctx: ctx}, ownerID, setID, pathID, artifactType)
	if err != nil || row == nil {
		return row, false, err
	}
	if strings.TrimSpace(row.InputHash) != "" && strings.TrimSpace(row.InputHash) == strings.TrimSpace(inputHash) {
		return row, true, nil
	}
	return row, false, nil
}

func artifactCacheUpsert(ctx context.Context, repo repos.LearningArtifactRepo, row *types.LearningArtifact) error {
	if repo == nil || !artifactCacheEnabled() || row == nil {
		return nil
	}
	return repo.Upsert(dbctx.Context{Ctx: ctx}, row)
}

func computeArtifactHash(stage string, materialSetID, pathID uuid.UUID, payload map[string]any) (string, error) {
	base := map[string]any{
		"stage":           stage,
		"version":         artifactHashVersion,
		"material_set_id": materialSetID.String(),
		"path_id":         pathID.String(),
		"payload":         payload,
	}
	canon, err := content.CanonicalizeJSON(base)
	if err != nil {
		return "", err
	}
	return content.HashBytes(canon), nil
}

func envSnapshot(prefixes []string, allowKeys []string) map[string]string {
	out := map[string]string{}
	skipSensitive := func(k string) bool {
		u := strings.ToUpper(strings.TrimSpace(k))
		return strings.Contains(u, "KEY") || strings.Contains(u, "SECRET") || strings.Contains(u, "TOKEN") || strings.Contains(u, "PASSWORD")
	}
	for _, k := range allowKeys {
		k = strings.TrimSpace(k)
		if k == "" || skipSensitive(k) {
			continue
		}
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			out[k] = v
		}
	}
	env := os.Environ()
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" || val == "" || skipSensitive(key) {
			continue
		}
		for _, p := range prefixes {
			p = strings.TrimSpace(p)
			if p != "" && strings.HasPrefix(key, p) {
				out[key] = val
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := map[string]string{}
	for _, k := range keys {
		ordered[k] = out[k]
	}
	return ordered
}

type fileFingerprint struct {
	ID            string `json:"id"`
	UpdatedAt     string `json:"updated_at"`
	ExtractedAt   string `json:"extracted_at"`
	SizeBytes     int64  `json:"size_bytes"`
	MimeType      string `json:"mime_type"`
	StorageKey    string `json:"storage_key"`
	ExtractedKind string `json:"extracted_kind"`
	Status        string `json:"status"`
}

func filesFingerprint(files []*types.MaterialFile) []fileFingerprint {
	out := make([]fileFingerprint, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		extractedAt := ""
		if f.ExtractedAt != nil {
			extractedAt = f.ExtractedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, fileFingerprint{
			ID:            f.ID.String(),
			UpdatedAt:     f.UpdatedAt.UTC().Format(time.RFC3339Nano),
			ExtractedAt:   extractedAt,
			SizeBytes:     f.SizeBytes,
			MimeType:      strings.TrimSpace(f.MimeType),
			StorageKey:    strings.TrimSpace(f.StorageKey),
			ExtractedKind: strings.TrimSpace(f.ExtractedKind),
			Status:        strings.TrimSpace(f.Status),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type chunkFingerprint struct {
	ID        string `json:"id"`
	FileID    string `json:"file_id"`
	UpdatedAt string `json:"updated_at"`
	Index     int    `json:"index"`
	Page      int    `json:"page"`
	Kind      string `json:"kind"`
	Provider  string `json:"provider"`
}

func chunksFingerprint(chunks []*types.MaterialChunk) []chunkFingerprint {
	out := make([]chunkFingerprint, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		page := 0
		if ch.Page != nil {
			page = *ch.Page
		}
		out = append(out, chunkFingerprint{
			ID:        ch.ID.String(),
			FileID:    ch.MaterialFileID.String(),
			UpdatedAt: ch.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Index:     ch.Index,
			Page:      page,
			Kind:      strings.TrimSpace(ch.Kind),
			Provider:  strings.TrimSpace(ch.Provider),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type signatureFingerprint struct {
	FileID      string `json:"file_id"`
	UpdatedAt   string `json:"updated_at"`
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

func signaturesFingerprint(sigs []*types.MaterialFileSignature) []signatureFingerprint {
	out := make([]signatureFingerprint, 0, len(sigs))
	for _, s := range sigs {
		if s == nil || s.MaterialFileID == uuid.Nil {
			continue
		}
		out = append(out, signatureFingerprint{
			FileID:      s.MaterialFileID.String(),
			UpdatedAt:   s.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Version:     s.Version,
			Fingerprint: strings.TrimSpace(s.Fingerprint),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FileID < out[j].FileID })
	return out
}

func maxFileUpdatedAt(files []*types.MaterialFile) time.Time {
	var max time.Time
	for _, f := range files {
		if f == nil {
			continue
		}
		if f.UpdatedAt.After(max) {
			max = f.UpdatedAt
		}
		if f.ExtractedAt != nil && f.ExtractedAt.After(max) {
			max = *f.ExtractedAt
		}
	}
	return max
}

func maxChunkUpdatedAt(chunks []*types.MaterialChunk) time.Time {
	var max time.Time
	for _, ch := range chunks {
		if ch == nil {
			continue
		}
		if ch.UpdatedAt.After(max) {
			max = ch.UpdatedAt
		}
	}
	return max
}

func maxSignatureUpdatedAt(sigs []*types.MaterialFileSignature) time.Time {
	var max time.Time
	for _, s := range sigs {
		if s == nil {
			continue
		}
		if s.UpdatedAt.After(max) {
			max = s.UpdatedAt
		}
	}
	return max
}

func marshalMeta(v any) datatypes.JSON {
	if v == nil {
		return datatypes.JSON([]byte(`{}`))
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(b)
}

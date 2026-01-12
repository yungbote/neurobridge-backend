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

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func buildIntakeMaterialExcerpts(files []*types.MaterialFile, chunks []*types.MaterialChunk) string {
	if len(files) == 0 || len(chunks) == 0 {
		return ""
	}

	// Defaults tuned for "high signal, low token": a couple of snippets per file.
	perFile := envIntAllowZero("PATH_INTAKE_EXCERPTS_PER_FILE", 2)
	if perFile <= 0 {
		return ""
	}
	maxChars := envIntAllowZero("PATH_INTAKE_EXCERPT_MAX_CHARS", 520)
	if maxChars <= 0 {
		maxChars = 520
	}
	maxTotal := envIntAllowZero("PATH_INTAKE_EXCERPT_MAX_TOTAL_CHARS", 12_000)
	if maxTotal <= 0 {
		maxTotal = 12_000
	}

	byFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		byFile[ch.MaterialFileID] = append(byFile[ch.MaterialFileID], ch)
	}
	if len(byFile) == 0 {
		return ""
	}

	var b strings.Builder
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		arr := byFile[f.ID]
		if len(arr) == 0 {
			continue
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].Index < arr[j].Index })

		n := len(arr)
		k := perFile
		if k > n {
			k = n
		}
		step := float64(n) / float64(k)

		header := fmt.Sprintf("FILE: %s [file_id=%s]\n", strings.TrimSpace(f.OriginalName), f.ID.String())
		if b.Len()+len(header) > maxTotal {
			break
		}
		b.WriteString(header)

		for i := 0; i < k; i++ {
			idx := int(float64(i) * step)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			ch := arr[idx]
			txt := shorten(ch.Text, maxChars)
			if txt == "" {
				continue
			}
			line := fmt.Sprintf("- [chunk_id=%s] %s\n", ch.ID.String(), txt)
			if b.Len()+len(line) > maxTotal {
				break
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
		if b.Len() >= maxTotal {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func summaryLevel(summary *types.MaterialSetSummary) string {
	if summary == nil {
		return ""
	}
	return strings.TrimSpace(summary.Level)
}

func writePathIntakeMeta(ctx context.Context, deps PathIntakeDeps, pathID uuid.UUID, intake map[string]any, extra map[string]any) error {
	if deps.Path == nil || pathID == uuid.Nil {
		return nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if row != nil && len(row.Metadata) > 0 && string(row.Metadata) != "null" {
		_ = json.Unmarshal(row.Metadata, &meta)
	}
	meta["intake"] = intake
	meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range extra {
		meta[k] = v
	}
	return deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(meta)),
	})
}

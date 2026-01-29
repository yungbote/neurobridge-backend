package steps

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type AdaptiveSignals struct {
	MaterialSetID uuid.UUID
	PathID        uuid.UUID

	FileCount    int
	PageCount    int
	SectionCount int
	ChunkCount   int
	ConceptCount int
	EdgeCount    int
	NodeCount    int

	AvgPagesPerFile  float64
	AvgChunksPerFile float64
	ChunksPerNode    float64

	ContentType string // code | prose | slides | mixed
}

type adaptiveFileRow struct {
	ID            uuid.UUID
	OriginalName  string
	MimeType      string
	ExtractedKind string
	AIType        string
}

func adaptiveParamsEnabled() bool {
	return envBool("ADAPTIVE_PARAMS_ENABLED", true)
}

func adaptiveParamsEnabledForStage(stage string) bool {
	if !adaptiveParamsEnabled() {
		return false
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return true
	}
	envKey := "ADAPTIVE_PARAMS_DISABLE_" + strings.ToUpper(stage)
	if envBool(envKey, false) {
		return false
	}
	if raw := strings.TrimSpace(os.Getenv("ADAPTIVE_PARAMS_DISABLE_STAGES")); raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			if strings.EqualFold(strings.TrimSpace(p), stage) {
				return false
			}
		}
	}
	return true
}

func clampIntCeiling(v int, min int, ceiling int) int {
	if v < min {
		v = min
	}
	if ceiling > 0 && v > ceiling {
		v = ceiling
	}
	return v
}

func clampFloatCeiling(v float64, min float64, ceiling float64) float64 {
	if v < min {
		v = min
	}
	if ceiling > 0 && v > ceiling {
		v = ceiling
	}
	return v
}

func adaptiveInt(value int, min int, ceiling int) int {
	return clampIntCeiling(value, min, ceiling)
}

func adaptiveFloat(value float64, min float64, ceiling float64) float64 {
	return clampFloatCeiling(value, min, ceiling)
}

func adaptiveFromRatio(total int, ratio float64, min int, ceiling int) int {
	if total <= 0 {
		return clampIntCeiling(min, min, ceiling)
	}
	raw := int(math.Round(float64(total) * ratio))
	return clampIntCeiling(raw, min, ceiling)
}

func adaptiveSignalsMeta(signals AdaptiveSignals) map[string]any {
	return map[string]any{
		"file_count":          signals.FileCount,
		"page_count":          signals.PageCount,
		"section_count":       signals.SectionCount,
		"chunk_count":         signals.ChunkCount,
		"concept_count":       signals.ConceptCount,
		"edge_count":          signals.EdgeCount,
		"node_count":          signals.NodeCount,
		"avg_pages_per_file":  signals.AvgPagesPerFile,
		"avg_chunks_per_file": signals.AvgChunksPerFile,
		"chunks_per_node":     signals.ChunksPerNode,
		"content_type":        signals.ContentType,
	}
}

func adaptiveStageMeta(stage string, enabled bool, signals AdaptiveSignals, params map[string]any) map[string]any {
	return map[string]any{
		"stage":   stage,
		"enabled": enabled,
		"signals": adaptiveSignalsMeta(signals),
		"params":  params,
	}
}

type AdaptiveParam struct {
	Name    string
	Ceiling int
	Actual  int
	Enabled bool
}

func (p AdaptiveParam) Value() int {
	if !p.Enabled {
		return p.Ceiling
	}
	return p.Actual
}

func loadAdaptiveSignals(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID, pathID uuid.UUID) AdaptiveSignals {
	out := AdaptiveSignals{
		MaterialSetID: materialSetID,
		PathID:        pathID,
		ContentType:   "mixed",
	}
	if db == nil || materialSetID == uuid.Nil {
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var files []adaptiveFileRow
	if err := db.WithContext(ctx).
		Model(&types.MaterialFile{}).
		Select("id", "original_name", "mime_type", "extracted_kind", "ai_type").
		Where("material_set_id = ?", materialSetID).
		Find(&files).Error; err != nil {
		return out
	}

	out.FileCount = len(files)
	if out.FileCount == 0 {
		return out
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}

	out.ContentType = detectContentType(files)

	// Chunk count
	var chunkCount int64
	_ = db.WithContext(ctx).
		Model(&types.MaterialChunk{}).
		Where("material_file_id IN ?", fileIDs).
		Count(&chunkCount).Error
	out.ChunkCount = int(chunkCount)

	// Distinct page count (best-effort)
	var pageCount int64
	_ = db.WithContext(ctx).
		Raw("SELECT count(DISTINCT page) FROM material_chunk WHERE material_file_id IN ? AND page IS NOT NULL", fileIDs).
		Scan(&pageCount).Error
	if pageCount == 0 && chunkCount > 0 {
		estimate := int64(math.Ceil(float64(chunkCount) / 3.0))
		if estimate < 1 {
			estimate = 1
		}
		pageCount = estimate
	}
	out.PageCount = int(pageCount)

	// Section count
	var sectionCount int64
	_ = db.WithContext(ctx).
		Model(&types.MaterialFileSection{}).
		Where("material_file_id IN ?", fileIDs).
		Count(&sectionCount).Error
	out.SectionCount = int(sectionCount)

	// Concept + edge counts (path scope)
	if pathID != uuid.Nil {
		var conceptCount int64
		_ = db.WithContext(ctx).
			Model(&types.Concept{}).
			Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", "path", pathID).
			Count(&conceptCount).Error
		out.ConceptCount = int(conceptCount)

		var edgeCount int64
		_ = db.WithContext(ctx).
			Raw(`SELECT count(*) FROM concept_edge e
				JOIN concept c ON c.id = e.from_concept_id
				WHERE c.scope = ? AND c.scope_id IS NOT DISTINCT FROM ?`, "path", pathID).
			Scan(&edgeCount).Error
		out.EdgeCount = int(edgeCount)

		var nodeCount int64
		_ = db.WithContext(ctx).
			Model(&types.PathNode{}).
			Where("path_id = ?", pathID).
			Count(&nodeCount).Error
		out.NodeCount = int(nodeCount)
	}

	out.AvgPagesPerFile = safeDivideFloat(out.PageCount, out.FileCount)
	out.AvgChunksPerFile = safeDivideFloat(out.ChunkCount, out.FileCount)
	out.ChunksPerNode = safeDivideFloat(out.ChunkCount, maxInt(out.NodeCount, 1))

	return out
}

func safeDivideFloat(n int, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func detectContentType(files []adaptiveFileRow) string {
	if len(files) == 0 {
		return "mixed"
	}
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".java": true,
		".c": true, ".cc": true, ".cpp": true, ".rs": true, ".cs": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true, ".m": true,
	}
	slideExts := map[string]bool{".ppt": true, ".pptx": true, ".key": true}

	codeCount := 0
	slideCount := 0
	for _, f := range files {
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		mime := strings.ToLower(strings.TrimSpace(f.MimeType))
		kind := strings.ToLower(strings.TrimSpace(f.ExtractedKind))
		ext := strings.ToLower(filepath.Ext(name))

		if slideExts[ext] || strings.Contains(mime, "presentation") || strings.Contains(kind, "slides") {
			slideCount++
			continue
		}
		if codeExts[ext] || strings.HasPrefix(mime, "text/x-") || strings.Contains(mime, "text/plain") {
			codeCount++
			continue
		}
	}

	total := len(files)
	if slideCount*100 >= total*60 {
		return "slides"
	}
	if codeCount*100 >= total*60 {
		return "code"
	}
	if slideCount == 0 && codeCount == 0 {
		return "prose"
	}
	return "mixed"
}

func adjustThresholdByContentType(name string, base float64, contentType string) float64 {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return base
	}
	switch name {
	case "CONCEPT_GRAPH_SECTION_MIN_SCORE":
		switch ct {
		case "slides":
			return base - 0.05
		case "mixed":
			return base - 0.02
		case "code":
			return base + 0.03
		case "prose":
			return base + 0.02
		}
	case "GLOBAL_ENTITY_SIM_THRESHOLD":
		switch ct {
		case "slides":
			return base - 0.05
		case "mixed":
			return base - 0.02
		case "code":
			return base + 0.03
		case "prose":
			return base + 0.02
		}
	case "CANONICAL_CONCEPT_SEMANTIC_MIN_SCORE":
		switch ct {
		case "slides":
			return base - 0.04
		case "mixed":
			return base - 0.02
		case "code":
			return base + 0.02
		case "prose":
			return base + 0.03
		}
	case "MATERIAL_SIGNAL_RETRIEVAL_WEIGHT":
		switch ct {
		case "slides":
			return base - 0.05
		case "mixed":
			return base - 0.02
		case "code":
			return base + 0.05
		case "prose":
			return base
		}
	case "PATH_GROUPING_MIN_CONFIDENCE_SPLIT":
		switch ct {
		case "slides", "mixed":
			return base - 0.05
		case "code":
			return base + 0.02
		case "prose":
			return base + 0.01
		}
	case "PATH_GROUPING_MIN_CONFIDENCE_MERGE":
		switch ct {
		case "slides", "mixed":
			return base + 0.05
		case "code":
			return base - 0.02
		case "prose":
			return base - 0.01
		}
	case "PATH_GROUPING_BRIDGE_STRONG":
		switch ct {
		case "slides", "mixed":
			return base - 0.05
		case "code":
			return base + 0.02
		case "prose":
			return base + 0.01
		}
	case "PATH_GROUPING_BRIDGE_WEAK":
		switch ct {
		case "slides", "mixed":
			return base - 0.05
		case "code":
			return base + 0.01
		case "prose":
			return base + 0.01
		}
	case "CONCEPT_GRAPH_SEED_MIN_QUALITY":
		switch ct {
		case "slides":
			return base - 0.05
		case "mixed":
			return base - 0.03
		case "code":
			return base + 0.01
		case "prose":
			return base + 0.03
		}
	case "CONCEPT_GRAPH_SEED_MIN_COVERAGE_CONF":
		switch ct {
		case "slides":
			return base - 0.04
		case "mixed":
			return base - 0.02
		case "code":
			return base + 0.01
		case "prose":
			return base + 0.02
		}
	case "CONCEPT_GRAPH_PATCH_SKIP_MIN_CONF":
		switch ct {
		case "slides":
			return base + 0.05
		case "mixed":
			return base + 0.03
		case "code":
			return base - 0.01
		case "prose":
			return base - 0.02
		}
	}
	return base
}

func adjustExcerptCharsByContentType(base int, contentType string) int {
	if base <= 0 {
		return base
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	factor := 1.0
	switch ct {
	case "slides":
		factor = 0.7
	case "mixed":
		factor = 0.9
	case "code":
		factor = 0.85
	case "prose":
		factor = 1.15
	}
	return int(math.Round(float64(base) * factor))
}

func adjustExcerptLinesByContentType(base int, contentType string) int {
	if base <= 0 {
		return base
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	factor := 1.0
	switch ct {
	case "slides":
		factor = 0.6
	case "mixed":
		factor = 0.85
	case "code":
		factor = 0.75
	case "prose":
		factor = 1.1
	}
	return int(math.Round(float64(base) * factor))
}

func adjustMinTextCharsByContentType(base int, contentType string) int {
	if base <= 0 {
		return base
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	factor := 1.0
	switch ct {
	case "slides":
		factor = 0.6
	case "mixed":
		factor = 0.85
	case "code":
		factor = 0.9
	case "prose":
		factor = 1.2
	}
	return int(math.Round(float64(base) * factor))
}

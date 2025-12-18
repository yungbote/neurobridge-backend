package course_build

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

// -------------------- Concept Map Stage --------------------

// ConceptMap is stored in Course.Metadata["concept_map"].
type conceptMap struct {
	Version  int           `json:"version"`
	Concepts []conceptNode `json:"concepts"`
}

type conceptNode struct {
	ID        string   `json:"id"`                  // stable-ish id (slug/uuid string)
	Name      string   `json:"name"`                // human name
	ParentID  *string  `json:"parent_id,omitempty"` // nil for roots
	Depth     int      `json:"depth"`               // 0=root
	Summary   string   `json:"summary"`             // 1-3 sentences
	KeyPoints []string `json:"key_points"`          // bullets
	Citations []string `json:"citations"`           // chunk_id strings used
}

// LLM schema for concept map
func conceptMapSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version": map[string]any{"type": "integer"},
			"concepts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":        map[string]any{"type": "string"},
						"name":      map[string]any{"type": "string"},
						"parent_id": map[string]any{"type": []any{"string", "null"}},
						"depth":     map[string]any{"type": "integer"},
						"summary":   map[string]any{"type": "string"},
						"key_points": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"citations": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required":             []string{"id", "name", "parent_id", "depth", "summary", "key_points", "citations"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"version", "concepts"},
		"additionalProperties": false,
	}
}

// stageConcepts builds a hierarchical inventory from the entire upload bundle.
func (p *CourseBuildPipeline) stageConcepts(bc *buildContext) error {
	if bc == nil || bc.course == nil {
		return nil
	}

	// Idempotent: if already exists, skip.
	if hasConceptMap(bc.course.Metadata) {
		p.progress(bc, "concepts", 49, "Concept map already exists")
		return nil
	}

	p.progress(bc, "concepts", 46, "Extracting concepts and building hierarchy")

	excerpts := buildStratifiedConceptExcerpts(bc.chunks, 12, 700) // 12 chunks per file max, 700 chars each
	if strings.TrimSpace(excerpts) == "" {
		return fmt.Errorf("no excerpt text available to build concept map")
	}

	out, err := p.ai.GenerateJSON(
		bc.ctx,
		"You are an expert curriculum designer. Your job is to extract ALL important concepts from the provided excerpts and organize them hierarchically.\n\n"+
			"Rules:\n"+
			"- You MUST ground every concept in the excerpts. Do not invent topics.\n"+
			"- Return a hierarchy: roots (depth=0), children (depth=1..).\n"+
			"- Concepts must be reusable building blocks (not whole lesson titles).\n"+
			"- Include citations as chunk_id strings you used.\n"+
			"- Do NOT output source code, imports, JSX, or UI code.\n",
		fmt.Sprintf(
			"EXCERPTS (each line has chunk_id):\n%s\n\n"+
				"Task:\n"+
				"1) Identify all distinct concepts.\n"+
				"2) Organize them into a hierarchy.\n"+
				"3) For each concept provide: name, summary, key_points, citations.\n\n"+
				"Return JSON only.\n",
			excerpts,
		),
		"concept_map",
		conceptMapSchema(),
	)
	if err != nil {
		return fmt.Errorf("concept map generation: %w", err)
	}

	// Persist into course.metadata
	merged := mergeCourseMetadata(bc.course.Metadata, map[string]any{
		"concept_map": out,
	})
	if err := p.db.WithContext(bc.ctx).
		Model(&types.Course{}).
		Where("id = ?", bc.courseID).
		Updates(map[string]any{
			"metadata":   datatypes.JSON(mustJSON(merged)),
			"updated_at": time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("persist concept_map to course.metadata: %w", err)
	}

	bc.course.Metadata = datatypes.JSON(mustJSON(merged))
	p.snapshot(bc)

	p.progress(bc, "concepts", 49, "Concept map created")
	return nil
}

// -------------------- Helpers --------------------

func hasConceptMap(js datatypes.JSON) bool {
	if len(js) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(js, &m); err != nil {
		return false
	}
	_, ok := m["concept_map"]
	return ok
}

func mergeCourseMetadata(existing datatypes.JSON, updates map[string]any) map[string]any {
	out := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range updates {
		out[k] = v
	}
	return out
}

// Builds a representative excerpt across all files (not “first 20k chars”).
func buildStratifiedConceptExcerpts(chunks []*types.MaterialChunk, perFile int, maxChars int) string {
	if perFile <= 0 {
		perFile = 10
	}
	if maxChars <= 0 {
		maxChars = 600
	}

	byFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		if strings.TrimSpace(ch.Text) == "" {
			continue
		}
		byFile[ch.MaterialFileID] = append(byFile[ch.MaterialFileID], ch)
	}

	fileIDs := make([]uuid.UUID, 0, len(byFile))
	for fid := range byFile {
		fileIDs = append(fileIDs, fid)
	}
	sort.Slice(fileIDs, func(i, j int) bool { return fileIDs[i].String() < fileIDs[j].String() })

	var b strings.Builder
	for _, fid := range fileIDs {
		arr := byFile[fid]
		sort.Slice(arr, func(i, j int) bool { return arr[i].Index < arr[j].Index })

		n := len(arr)
		if n == 0 {
			continue
		}
		k := perFile
		if k > n {
			k = n
		}
		step := float64(n) / float64(k)

		for i := 0; i < k; i++ {
			idx := int(float64(i) * step)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			ch := arr[idx]
			txt := strings.TrimSpace(ch.Text)
			if len(txt) > maxChars {
				txt = txt[:maxChars] + "…"
			}
			b.WriteString(fmt.Sprintf("[chunk_id=%s] %s\n", ch.ID.String(), txt))
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

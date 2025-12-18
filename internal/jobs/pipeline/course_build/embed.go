package course_build

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func embeddingMissing(e datatypes.JSON) bool {
	if len(e) == 0 {
		return true
	}
	s := strings.TrimSpace(string(e))
	return s == "" || s == "null" || s == "[]"
}

func (p *CourseBuildPipeline) stageEmbed(buildCtx *buildContext) error {
	if buildCtx == nil {
		return nil
	}
	p.progress(buildCtx, "embed", 30, "Embedding missing chunks")

	// Find Missing Embeddings (NULL or [] both count as missing)
	missing := make([]*types.MaterialChunk, 0)
	for _, ch := range buildCtx.chunks {
		if ch == nil {
			continue
		}
		if embeddingMissing(ch.Embedding) {
			missing = append(missing, ch)
		}
	}

	// If nothing missing, complete stage immediately
	if len(missing) == 0 {
		p.progress(buildCtx, "embed", 45, "No embeddings missing")
		return nil
	}

	const batchSize = 64
	totalMissing := max(1, len(missing))

	ns := fmt.Sprintf("chunks:material_set:%s", buildCtx.materialSetID.String())

	for start := 0; start < len(missing); start += batchSize {
		end := start + batchSize
		if end > len(missing) {
			end = len(missing)
		}

		batch := missing[start:end]
		inputs := make([]string, len(batch))
		for i, ch := range batch {
			inputs[i] = ch.Text
		}

		vecs, err := p.ai.Embed(buildCtx.ctx, inputs)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}

		upsert := make([]pinecone.Vector, 0, len(batch))

		for i, ch := range batch {
			b, _ := json.Marshal(vecs[i])

			if err := p.db.WithContext(buildCtx.ctx).Model(&types.MaterialChunk{}).
				Where("id = ?", ch.ID).
				Updates(map[string]any{
					"embedding":  datatypes.JSON(b),
					"updated_at": time.Now(),
				}).Error; err != nil {
				return fmt.Errorf("update chunk embedding: %w", err)
			}
			ch.Embedding = datatypes.JSON(b)

			upsert = append(upsert, pinecone.Vector{
				ID:     ch.ID.String(),
				Values: vecs[i],
				Metadata: map[string]any{
					"type":            "chunk",
					"material_set_id": buildCtx.materialSetID.String(),
					"material_file_id": func() string {
						// safe even if zero
						return ch.MaterialFileID.String()
					}(),
					"chunk_index": ch.Index,
				},
			})
		}

		if p.vectorStore != nil && len(upsert) > 0 {
			if err := p.vectorStore.Upsert(buildCtx.ctx, ns, upsert); err != nil {
				p.log.Warn("pinecone upsert failed (continuing)",
					"err", err.Error(),
					"namespace", ns,
					"count", len(upsert),
				)
			}
		}

		// mimic old embed progress slip: 30 -> 45
		pct := 30 + int(float64(end)/float64(totalMissing)*15.0)
		p.progress(buildCtx, "embed", pct, "Embedded chunk batch")
	}

	// Ensure stage ends at 45
	p.progress(buildCtx, "embed", 45, "Embeddings complete")
	return nil
}

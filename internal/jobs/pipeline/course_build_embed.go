package pipelines

import (
	"encoding/json"
	"fmt"
	"time"
	"gorm.io/datatypes"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *CourseBuildPipeline) stageEmbed(buildCtx *buildContext) error {
	if buildCtx == nil {
		return nil
	}
	p.progress(buildCtx, "embed", 30, "Embedding missing chunks")
	// Find Missing Embeddings
	missing := make([]*types.MaterialChunk, 0)
	for _, ch := range buildCtx.chunks {
		if ch != nil && len(ch.Embedding) == 0 {
			missing = append(missing, ch)
		}
	}
	const batchSize = 64
	totalMissing := max(1, len(missing))
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
		for i, ch := range batch {
			b, _ := json.Marshal(vecs[i])
			if err := p.db.WithContext(buildCtx.ctx).Model(&types.MaterialChunk{}).
				Where("id = ?", ch.ID).
				Updates(map[string]any{
					"embedding":	datatypes.JSON(b),
					"updated_at": time.Now(),
				}).Error; err != nil {
				return fmt.Errorf("update chunk embedding: %w", err)
			}
			ch.Embedding = datatypes.JSON(b)
		}
		// mimic old embed progress slip: 30 -> 45
		pct := 30 + int(float64(end)/float64(totalMissing)*15.0)
		p.progress(buildCtx, "embed", pct, "Embedded chunk batch")
	}
	return nil
}











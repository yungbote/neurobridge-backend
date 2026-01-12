package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type MaterialSetSummarizeDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Summaries repos.MaterialSetSummaryRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type MaterialSetSummarizeInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type MaterialSetSummarizeOutput struct {
	SummaryID uuid.UUID `json:"summary_id"`
	VectorID  string    `json:"vector_id"`
}

func MaterialSetSummarize(ctx context.Context, deps MaterialSetSummarizeDeps, in MaterialSetSummarizeInput) (MaterialSetSummarizeOutput, error) {
	out := MaterialSetSummarizeOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Summaries == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("material_set_summarize: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("material_set_summarize: missing saga_id")
	}

	// Idempotency: if we already have a stable summary + embedding, don't regenerate.
	var existing *types.MaterialSetSummary
	if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 {
		existing = rows[0]
	}
	if existing != nil && strings.TrimSpace(existing.SummaryMD) != "" && !embeddingMissing(existing.Embedding) && strings.TrimSpace(existing.VectorID) != "" {
		out.SummaryID = existing.ID
		out.VectorID = existing.VectorID
		return out, nil
	}

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	if len(chunks) == 0 {
		return out, fmt.Errorf("material_set_summarize: no chunks for material set")
	}

	excerpt := stratifiedChunkExcerpts(chunks, 12, 700)
	if strings.TrimSpace(excerpt) == "" {
		return out, fmt.Errorf("material_set_summarize: empty excerpt")
	}

	p, err := prompts.Build(prompts.PromptMaterialSetSummary, prompts.Input{
		BundleExcerpt: excerpt,
	})
	if err != nil {
		return out, err
	}

	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}

	subject := stringFromAny(obj["subject"])
	level := stringFromAny(obj["level"])
	summaryMD := stringFromAny(obj["summary_md"])
	tags := dedupeStrings(stringSliceFromAny(obj["tags"]))
	conceptKeys := dedupeStrings(stringSliceFromAny(obj["concept_keys"]))

	vecDoc := strings.TrimSpace(summaryMD)
	if vecDoc == "" {
		vecDoc = strings.TrimSpace(subject + " " + level)
	}

	embs, err := deps.AI.Embed(ctx, []string{vecDoc})
	if err != nil {
		return out, err
	}
	if len(embs) == 0 || len(embs[0]) == 0 {
		return out, fmt.Errorf("material_set_summarize: empty embedding")
	}
	embJSON := mustJSON(embs[0])

	vectorID := "material_set_summary:" + in.MaterialSetID.String()
	ns := index.MaterialSetSummariesNamespace(in.OwnerUserID)

	now := time.Now().UTC()
	row := &types.MaterialSetSummary{
		ID:            uuid.New(),
		MaterialSetID: in.MaterialSetID,
		UserID:        in.OwnerUserID,
		Subject:       subject,
		Level:         level,
		SummaryMD:     summaryMD,
		Tags:          datatypes.JSON(mustJSON(tags)),
		ConceptKeys:   datatypes.JSON(mustJSON(conceptKeys)),
		Embedding:     datatypes.JSON(embJSON),
		VectorID:      vectorID,
		UpdatedAt:     now,
	}
	if existing != nil && existing.ID != uuid.Nil {
		row.ID = existing.ID
	}

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		// Contract: derive/ensure path_id via bootstrap.
		if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
		}

		if err := deps.Summaries.UpsertByMaterialSetID(dbc, row); err != nil {
			return err
		}

		if deps.Vec != nil {
			if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
				"namespace": ns,
				"ids":       []string{vectorID},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return out, err
	}

	out.SummaryID = row.ID
	out.VectorID = vectorID

	// Pinecone is a retrieval cache; failures should not block canonical state.
	if deps.Vec != nil {
		_ = deps.Vec.Upsert(ctx, ns, []pc.Vector{{
			ID:     vectorID,
			Values: embs[0],
			Metadata: map[string]any{
				"type":            "material_set_summary",
				"user_id":         in.OwnerUserID.String(),
				"material_set_id": in.MaterialSetID.String(),
				"subject":         subject,
				"level":           level,
			},
		}})
	}

	return out, nil
}

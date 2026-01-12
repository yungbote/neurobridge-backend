package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

type TeachingPatternsSeedDeps struct {
	DB          *gorm.DB
	Log         *logger.Logger
	Patterns    repos.TeachingPatternRepo
	UserProfile repos.UserProfileVectorRepo
	AI          openai.Client
	Vec         pc.VectorStore
	Saga        services.SagaService
	Bootstrap   services.LearningBuildBootstrapService
}

type TeachingPatternsSeedInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type TeachingPatternsSeedOutput struct {
	Seeded int `json:"seeded"`
}

func TeachingPatternsSeed(ctx context.Context, deps TeachingPatternsSeedDeps, in TeachingPatternsSeedInput) (TeachingPatternsSeedOutput, error) {
	out := TeachingPatternsSeedOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Patterns == nil || deps.UserProfile == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("teaching_patterns_seed: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("teaching_patterns_seed: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("teaching_patterns_seed: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("teaching_patterns_seed: missing saga_id")
	}

	// Contract: derive/ensure path_id.
	_, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}

	// Idempotency: if we already have a reasonable number of global patterns, skip.
	const minPatterns = 10
	if n, err := deps.Patterns.Count(dbctx.Context{Ctx: ctx}); err == nil && n >= minPatterns {
		return out, nil
	}

	up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
	if err != nil || up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("teaching_patterns_seed: missing user_profile_doc (run user_profile_refresh first)")
	}

	p, err := prompts.Build(prompts.PromptTeachingPatterns, prompts.Input{
		UserProfileDoc: up.ProfileDoc,
	})
	if err != nil {
		return out, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}
	patternsOut := parseTeachingPatterns(obj)
	if len(patternsOut) == 0 {
		return out, fmt.Errorf("teaching_patterns_seed: 0 patterns returned")
	}

	// Stable ordering.
	sort.Slice(patternsOut, func(i, j int) bool { return patternsOut[i].PatternKey < patternsOut[j].PatternKey })

	docs := make([]string, 0, len(patternsOut))
	vectorIDs := make([]string, 0, len(patternsOut))
	for _, p := range patternsOut {
		vectorIDs = append(vectorIDs, "teaching_pattern:"+p.PatternKey)
		docs = append(docs, strings.TrimSpace(p.Name+"\n"+p.WhenToUse))
	}
	embs, err := deps.AI.Embed(ctx, docs)
	if err != nil {
		return out, err
	}
	if len(embs) != len(patternsOut) {
		return out, fmt.Errorf("teaching_patterns_seed: embedding count mismatch (got %d want %d)", len(embs), len(patternsOut))
	}

	ns := index.TeachingPatternsNamespace()
	const batchSize = 64

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
		}

		for i, p := range patternsOut {
			spec := map[string]any{
				"representation": p.Representation,
				"constraints":    p.Constraints,
			}
			row := &types.TeachingPattern{
				ID:          uuid.New(),
				PatternKey:  p.PatternKey,
				Name:        p.Name,
				WhenToUse:   p.WhenToUse,
				PatternSpec: datatypes.JSON(mustJSON(spec)),
				Embedding:   datatypes.JSON(mustJSON(embs[i])),
				VectorID:    "teaching_pattern:" + p.PatternKey,
			}
			if err := deps.Patterns.UpsertByPatternKey(dbc, row); err != nil {
				return err
			}
			out.Seeded++
		}

		if deps.Vec != nil {
			for start := 0; start < len(vectorIDs); start += batchSize {
				end := start + batchSize
				if end > len(vectorIDs) {
					end = len(vectorIDs)
				}
				if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
					"namespace": ns,
					"ids":       vectorIDs[start:end],
				}); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return out, err
	}

	if deps.Vec != nil {
		for start := 0; start < len(patternsOut); start += batchSize {
			end := start + batchSize
			if end > len(patternsOut) {
				end = len(patternsOut)
			}
			pv := make([]pc.Vector, 0, end-start)
			for i := start; i < end; i++ {
				p := patternsOut[i]
				pv = append(pv, pc.Vector{
					ID:     "teaching_pattern:" + p.PatternKey,
					Values: embs[i],
					Metadata: map[string]any{
						"type":        "teaching_pattern",
						"pattern_key": p.PatternKey,
						"name":        p.Name,
					},
				})
			}
			_ = deps.Vec.Upsert(ctx, ns, pv)
		}
	}

	return out, nil
}

type teachingPatternOut struct {
	PatternKey     string         `json:"pattern_key"`
	Name           string         `json:"name"`
	WhenToUse      string         `json:"when_to_use"`
	Representation map[string]any `json:"representation"`
	Constraints    map[string]any `json:"constraints"`
}

func parseTeachingPatterns(obj map[string]any) []teachingPatternOut {
	raw, ok := obj["patterns"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]teachingPatternOut, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		pk := strings.TrimSpace(stringFromAny(m["pattern_key"]))
		name := strings.TrimSpace(stringFromAny(m["name"]))
		if pk == "" || name == "" {
			continue
		}
		rep, _ := m["representation"].(map[string]any)
		con, _ := m["constraints"].(map[string]any)
		out = append(out, teachingPatternOut{
			PatternKey:     pk,
			Name:           name,
			WhenToUse:      strings.TrimSpace(stringFromAny(m["when_to_use"])),
			Representation: rep,
			Constraints:    con,
		})
	}
	return out
}

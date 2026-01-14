package steps

import (
	"context"
	"encoding/json"
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

type UserProfileRefreshDeps struct {
	DB          *gorm.DB
	Log         *logger.Logger
	StylePrefs  repos.UserStylePreferenceRepo
	ProgEvents  repos.UserProgressionEventRepo
	UserProfile repos.UserProfileVectorRepo
	Prefs       repos.UserPersonalizationPrefsRepo
	AI          openai.Client
	Vec         pc.VectorStore
	Saga        services.SagaService
	Bootstrap   services.LearningBuildBootstrapService
}

type UserProfileRefreshInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type UserProfileRefreshOutput struct {
	VectorID string `json:"vector_id"`
}

func UserProfileRefresh(ctx context.Context, deps UserProfileRefreshDeps, in UserProfileRefreshInput) (UserProfileRefreshOutput, error) {
	out := UserProfileRefreshOutput{}
	if deps.DB == nil || deps.Log == nil || deps.StylePrefs == nil || deps.ProgEvents == nil || deps.UserProfile == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("user_profile_refresh: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("user_profile_refresh: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("user_profile_refresh: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("user_profile_refresh: missing saga_id")
	}

	// Contract: derive/ensure path_id via bootstrap (ties this profile refresh to the build bundle).
	_, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}

	var prefsAny any
	allowBehaviorPersonalization := true
	if deps.Prefs != nil {
		if row, err := deps.Prefs.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID); err == nil && row != nil && len(row.PrefsJSON) > 0 && string(row.PrefsJSON) != "null" {
			_ = json.Unmarshal(row.PrefsJSON, &prefsAny)
			if m, ok := prefsAny.(map[string]any); ok {
				if v, ok := m["allowBehaviorPersonalization"].(bool); ok {
					allowBehaviorPersonalization = v
				}
			}
		}
	}

	var style any
	var recent []*types.UserProgressionEvent
	if allowBehaviorPersonalization {
		style, _ = deps.StylePrefs.ListGlobalByUser(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
		recent, _ = deps.ProgEvents.ListRecentByUser(dbctx.Context{Ctx: ctx}, in.OwnerUserID, 200)
	}

	userFacts := map[string]any{
		"user_id":                  in.OwnerUserID.String(),
		"style_preferences_global": style,
		"recent_progression_count": len(recent),
		"personalization_prefs":    prefsAny,
	}
	userFactsJSON, _ := json.Marshal(userFacts)

	var recentSummary strings.Builder
	for i, ev := range recent {
		if ev == nil {
			continue
		}
		if i >= 40 {
			break
		}
		recentSummary.WriteString(fmt.Sprintf("- %s completed=%v score=%.2f dwell_ms=%d\n",
			strings.TrimSpace(ev.ActivityKind),
			ev.Completed,
			ev.Score,
			ev.DwellMS,
		))
	}

	p, err := prompts.Build(prompts.PromptUserProfileDoc, prompts.Input{
		UserFactsJSON:       string(userFactsJSON),
		RecentEventsSummary: strings.TrimSpace(recentSummary.String()),
	})
	if err != nil {
		return out, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}
	profileDoc := strings.TrimSpace(stringFromAny(obj["profile_doc"]))
	if profileDoc == "" {
		return out, fmt.Errorf("user_profile_refresh: empty profile_doc")
	}

	embs, err := deps.AI.Embed(ctx, []string{profileDoc})
	if err != nil {
		return out, err
	}
	if len(embs) == 0 || len(embs[0]) == 0 {
		return out, fmt.Errorf("user_profile_refresh: empty embedding")
	}

	vectorID := in.OwnerUserID.String()
	ns := index.UserProfileNamespace()
	now := time.Now().UTC()

	row := &types.UserProfileVector{
		ID:         uuid.New(),
		UserID:     in.OwnerUserID,
		ProfileDoc: profileDoc,
		Embedding:  datatypes.JSON(mustJSON(embs[0])),
		VectorID:   vectorID,
		UpdatedAt:  now,
	}

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if in.PathID == uuid.Nil {
			if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
				return err
			}
		}
		if err := deps.UserProfile.Upsert(dbc, row); err != nil {
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

	out.VectorID = vectorID

	if deps.Vec != nil {
		_ = deps.Vec.Upsert(ctx, ns, []pc.Vector{{
			ID:     vectorID,
			Values: embs[0],
			Metadata: map[string]any{
				"type":    "user_profile",
				"user_id": in.OwnerUserID.String(),
			},
		}})
	}

	return out, nil
}

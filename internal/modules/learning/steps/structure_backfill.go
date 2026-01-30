package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type StructureBackfillDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Path         repos.PathRepo
	PathNodes    repos.PathNodeRepo
	Concepts     repos.ConceptRepo
	PSUs         repos.PathStructuralUnitRepo
	Bootstrap    services.LearningBuildBootstrapService
	ConceptState repos.UserConceptStateRepo
	ConceptModel repos.UserConceptModelRepo
}

type StructureBackfillInput struct {
	UserID         uuid.UUID `json:"user_id,omitempty"`
	PathID         uuid.UUID `json:"path_id,omitempty"`
	BackfillModels *bool     `json:"backfill_models,omitempty"`
	BackfillPSUs   *bool     `json:"backfill_psus,omitempty"`
	Limit          int       `json:"limit,omitempty"`
}

type StructureBackfillOutput struct {
	UserID       uuid.UUID `json:"user_id,omitempty"`
	PathID       uuid.UUID `json:"path_id,omitempty"`
	ModelsAdded  int       `json:"models_added"`
	PSUsBuilt    int       `json:"psus_built"`
	PathsVisited int       `json:"paths_visited"`
}

func StructureBackfill(ctx context.Context, deps StructureBackfillDeps, in StructureBackfillInput) (StructureBackfillOutput, error) {
	out := StructureBackfillOutput{UserID: in.UserID, PathID: in.PathID}
	if deps.DB == nil || deps.Path == nil || deps.PathNodes == nil || deps.Concepts == nil || deps.PSUs == nil || deps.ConceptState == nil || deps.ConceptModel == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("structure_backfill: missing deps")
	}
	backfillModels := true
	backfillPSUs := true
	if in.BackfillModels != nil {
		backfillModels = *in.BackfillModels
	}
	if in.BackfillPSUs != nil {
		backfillPSUs = *in.BackfillPSUs
	}
	if !backfillModels && !backfillPSUs {
		return out, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}

	dbc := dbctx.Context{Ctx: ctx}

	if backfillModels && in.UserID != uuid.Nil {
		states, err := deps.ConceptState.ListByUserID(dbc, in.UserID, limit)
		if err != nil {
			return out, err
		}
		for _, st := range states {
			if st == nil || st.ConceptID == uuid.Nil {
				continue
			}
			if existing, err := deps.ConceptModel.Get(dbc, in.UserID, st.ConceptID); err == nil && existing != nil && existing.ID != uuid.Nil {
				continue
			}
			row := &types.UserConceptModel{
				ID:                 uuid.New(),
				UserID:             in.UserID,
				CanonicalConceptID: st.ConceptID,
				ModelVersion:       1,
			}
			if err := deps.ConceptModel.Upsert(dbc, row); err == nil {
				out.ModelsAdded++
			}
		}
	}

	if backfillPSUs {
		if in.PathID != uuid.Nil {
			if n, err := backfillPSUsForPath(ctx, deps, in.PathID); err == nil {
				out.PSUsBuilt += n
				out.PathsVisited++
			} else {
				return out, err
			}
		} else {
			if in.UserID == uuid.Nil && !envBool("STRUCTURE_BACKFILL_ALLOW_ALL", false) {
				return out, fmt.Errorf("structure_backfill: refusing to scan all paths without STRUCTURE_BACKFILL_ALLOW_ALL=true")
			}
			maxPaths := envIntAllowZero("STRUCTURE_BACKFILL_MAX_PATHS", 50)
			if maxPaths < 1 {
				maxPaths = 50
			}
			paths := []*types.Path{}
			if in.UserID != uuid.Nil {
				if rows, err := deps.Path.ListByUser(dbc, &in.UserID); err == nil {
					paths = rows
				} else {
					return out, err
				}
			} else {
				if rows, err := deps.Path.ListByStatus(dbc, []string{"ready", "building"}); err == nil {
					paths = rows
				} else {
					return out, err
				}
			}
			if len(paths) > maxPaths {
				paths = paths[:maxPaths]
			}
			for _, p := range paths {
				if p == nil || p.ID == uuid.Nil {
					continue
				}
				if n, err := backfillPSUsForPath(ctx, deps, p.ID); err == nil {
					out.PSUsBuilt += n
					out.PathsVisited++
				} else if deps.Log != nil {
					deps.Log.Warn("structure_backfill: psu backfill failed", "path_id", p.ID.String(), "error", err)
				}
			}
		}
	}

	if deps.Log != nil {
		deps.Log.Info("structure_backfill: done", "user_id", in.UserID.String(), "path_id", in.PathID.String(), "models_added", out.ModelsAdded, "psus_built", out.PSUsBuilt, "paths", out.PathsVisited)
	}
	return out, nil
}

func backfillPSUsForPath(ctx context.Context, deps StructureBackfillDeps, pathID uuid.UUID) (int, error) {
	if pathID == uuid.Nil {
		return 0, nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil || row == nil {
		return 0, err
	}
	if row.MaterialSetID == nil || *row.MaterialSetID == uuid.Nil {
		return 0, nil
	}
	owner := uuid.Nil
	if row.UserID != nil {
		owner = *row.UserID
	}
	if owner == uuid.Nil {
		return 0, nil
	}
	out, err := PathStructuralUnitBuild(ctx, PathStructuralUnitBuildDeps{
		DB:        deps.DB,
		Log:       deps.Log,
		PathNodes: deps.PathNodes,
		Concepts:  deps.Concepts,
		PSUs:      deps.PSUs,
		Bootstrap: deps.Bootstrap,
	}, PathStructuralUnitBuildInput{
		OwnerUserID:   owner,
		MaterialSetID: *row.MaterialSetID,
		PathID:        pathID,
		SagaID:        uuid.Nil,
	})
	if err != nil {
		return 0, err
	}
	// Small cooldown to avoid hammering DB when called in loops.
	time.Sleep(5 * time.Millisecond)
	return out.Units, nil
}

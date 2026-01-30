package structure_backfill

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	userID, _ := jc.PayloadUUID("user_id")
	pathID, _ := jc.PayloadUUID("path_id")
	limit := intFromAny(jc.Payload()["limit"], 0)
	backfillModels := boolFromAny(jc.Payload()["backfill_models"], false)
	backfillPSUs := boolFromAny(jc.Payload()["backfill_psus"], false)

	if userID == uuid.Nil && pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("structure_backfill: missing user_id or path_id"))
		return nil
	}

	jc.Progress("backfill", 5, "Running structural backfill")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                  p.db,
		Log:                 p.log,
		Path:                p.path,
		PathNodes:           p.nodes,
		Concepts:            p.concepts,
		PathStructuralUnits: p.psus,
		Bootstrap:           p.bootstrap,
		ConceptState:        p.mastery,
		ConceptModel:        p.model,
	}).StructureBackfill(jc.Ctx, learningmod.StructureBackfillInput{
		UserID:         userID,
		PathID:         pathID,
		BackfillModels: &backfillModels,
		BackfillPSUs:   &backfillPSUs,
		Limit:          limit,
	})
	if err != nil {
		jc.Fail("backfill", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"user_id":      userID.String(),
		"path_id":      pathID.String(),
		"models_added": out.ModelsAdded,
		"psus_built":   out.PSUsBuilt,
		"paths":        out.PathsVisited,
	})
	return nil
}

func boolFromAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		if s == "true" || s == "1" || s == "yes" || s == "y" {
			return true
		}
		if s == "false" || s == "0" || s == "no" || s == "n" {
			return false
		}
		return def
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return def
	}
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return def
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return def
	default:
		return def
	}
}

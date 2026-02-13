package rollback

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type FreezeState struct {
	Active    bool
	Reason    string
	EventID   string
	From      string
	To        string
	CheckedAt time.Time
}

func ActiveRollback(ctx context.Context, db *gorm.DB) (*types.RollbackEvent, error) {
	if db == nil {
		return nil, nil
	}
	row := &types.RollbackEvent{}
	if err := db.WithContext(ctx).
		Where("status IN ?", []string{"running", "in_progress"}).
		Order("created_at DESC").
		Limit(1).
		Find(row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuidNil() {
		return nil, nil
	}
	return row, nil
}

func FreezeActive(ctx context.Context, db *gorm.DB) (FreezeState, error) {
	state := FreezeState{CheckedAt: time.Now().UTC()}
	if db == nil {
		return state, nil
	}
	ev, err := ActiveRollback(ctx, db)
	if err != nil {
		return state, err
	}
	if ev != nil && ev.ID != uuidNil() {
		state.Active = true
		state.Reason = "rollback_active"
		state.EventID = ev.ID.String()
		state.From = strings.TrimSpace(ev.GraphVersionFrom)
		state.To = strings.TrimSpace(ev.GraphVersionTo)
		return state, nil
	}
	var count int64
	if err := db.WithContext(ctx).
		Model(&types.GraphVersion{}).
		Where("status = ?", "rolling_back").
		Count(&count).Error; err != nil {
		return state, err
	}
	if count > 0 {
		state.Active = true
		state.Reason = "graph_version_rolling_back"
	}
	return state, nil
}

func BlockedJobType(jobType string) bool {
	switch strings.ToLower(strings.TrimSpace(jobType)) {
	case
		"concept_graph_build",
		"concept_graph_patch_build",
		"concept_cluster_build",
		"concept_bridge_build",
		"path_grouping_refine",
		"path_plan_build",
		"path_structure_dispatch",
		"path_structure_refine",
		"psu_build",
		"psu_promote",
		"structure_extract",
		"structure_backfill",
		"chain_signature_build",
		"library_taxonomy_refine",
		"library_taxonomy_route",
		"material_kg_build",
		"file_signature_build",
		"ingest_chunks",
		"web_resources_seed",
		"teaching_patterns_seed",
		"material_signal_build",
		"priors_refresh",
		"user_model_update":
		return true
	default:
		return false
	}
}

func uuidNil() uuid.UUID { return uuid.Nil }

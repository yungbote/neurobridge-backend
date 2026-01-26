package steps

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type materialSetSignalContext struct {
	IntentJSON   string
	CoverageJSON string
	EdgesJSON    string
	WeightsByKey map[string]float64
}

func loadMaterialSetSignalContext(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID, topConcepts int) materialSetSignalContext {
	out := materialSetSignalContext{WeightsByKey: map[string]float64{}}
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || materialSetID == uuid.Nil {
		return out
	}
	if topConcepts <= 0 {
		topConcepts = 30
	}

	var intent types.MaterialSetIntent
	if err := db.WithContext(ctx).Model(&types.MaterialSetIntent{}).Where("material_set_id = ?", materialSetID).Take(&intent).Error; err == nil && intent.MaterialSetID != uuid.Nil {
		meta := map[string]any{}
		if len(intent.Metadata) > 0 && strings.TrimSpace(string(intent.Metadata)) != "" && strings.TrimSpace(string(intent.Metadata)) != "null" {
			_ = json.Unmarshal(intent.Metadata, &meta)
		}
		obj := map[string]any{
			"from_state":         strings.TrimSpace(intent.FromState),
			"to_state":           strings.TrimSpace(intent.ToState),
			"core_thread":        strings.TrimSpace(intent.CoreThread),
			"spine_file_ids":     jsonListFromRaw(intent.SpineMaterialFileIDs),
			"satellite_file_ids": jsonListFromRaw(intent.SatelliteMaterialFileIDs),
			"gaps_concept_keys":  jsonListFromRaw(intent.GapsConceptKeys),
			"redundancy_notes":   jsonListFromRaw(intent.RedundancyNotes),
			"conflict_notes":     jsonListFromRaw(intent.ConflictNotes),
			"metadata":           meta,
		}
		if b, err := json.Marshal(obj); err == nil {
			out.IntentJSON = string(b)
		}
	}

	var coverage []*types.MaterialSetConceptCoverage
	_ = db.WithContext(ctx).Model(&types.MaterialSetConceptCoverage{}).Where("material_set_id = ?", materialSetID).Find(&coverage).Error

	type covItem struct {
		Key          string
		CoverageType string
		Depth        string
		Score        float64
	}
	items := make([]covItem, 0, len(coverage))
	coverageWeights := map[string]float64{}
	for _, c := range coverage {
		if c == nil {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(c.ConceptKey))
		if key == "" {
			continue
		}
		score := clamp01(c.Score)
		if score > coverageWeights[key] {
			coverageWeights[key] = score
		}
		items = append(items, covItem{Key: key, CoverageType: c.CoverageType, Depth: c.Depth, Score: score})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	if topConcepts > 0 && len(items) > topConcepts {
		items = items[:topConcepts]
	}
	if len(items) > 0 {
		payload := make([]map[string]any, 0, len(items))
		for _, it := range items {
			payload = append(payload, map[string]any{
				"concept_key":   it.Key,
				"coverage_type": strings.TrimSpace(it.CoverageType),
				"depth":         strings.TrimSpace(it.Depth),
				"score":         it.Score,
			})
		}
		if b, err := json.Marshal(map[string]any{"top_concepts": payload}); err == nil {
			out.CoverageJSON = string(b)
		}
	}

	compoundWeights := loadCompoundWeightsByKey(ctx, db, materialSetID, envIntAllowZero("MATERIAL_SIGNAL_COMPOUND_MAX_ROWS", 2000))
	if len(compoundWeights) > 0 {
		out.WeightsByKey = compoundWeights
	} else {
		out.WeightsByKey = coverageWeights
	}

	var edges []*types.MaterialEdge
	_ = db.WithContext(ctx).Model(&types.MaterialEdge{}).Where("material_set_id = ?", materialSetID).Find(&edges).Error
	if len(edges) > 0 {
		sort.Slice(edges, func(i, j int) bool {
			if edges[i] == nil {
				return false
			}
			if edges[j] == nil {
				return true
			}
			return edges[i].Strength > edges[j].Strength
		})
		if len(edges) > 40 {
			edges = edges[:40]
		}
		payload := make([]map[string]any, 0, len(edges))
		for _, e := range edges {
			if e == nil {
				continue
			}
			payload = append(payload, map[string]any{
				"from_file_id":      e.FromMaterialFileID.String(),
				"to_file_id":        e.ToMaterialFileID.String(),
				"edge_type":         strings.TrimSpace(e.EdgeType),
				"strength":          clamp01(e.Strength),
				"bridging_concepts": jsonListFromRaw(e.BridgingConcepts),
			})
		}
		if b, err := json.Marshal(map[string]any{"edges": payload}); err == nil {
			out.EdgesJSON = string(b)
		}
	}

	return out
}

func loadCompoundWeightsByKey(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID, maxRows int) map[string]float64 {
	out := map[string]float64{}
	if db == nil || materialSetID == uuid.Nil {
		return out
	}
	if maxRows <= 0 {
		maxRows = 2000
	}
	if ctx == nil {
		ctx = context.Background()
	}

	type row struct {
		MaterialChunkID uuid.UUID      `gorm:"column:material_chunk_id"`
		CompoundWeight  float64        `gorm:"column:compound_weight"`
		SignalStrength  float64        `gorm:"column:signal_strength"`
		Trajectory      datatypes.JSON `gorm:"column:trajectory"`
	}
	var rows []row
	if err := db.WithContext(ctx).
		Model(&types.MaterialChunkSignal{}).
		Select("material_chunk_id", "compound_weight", "signal_strength", "trajectory").
		Where("material_set_id = ?", materialSetID).
		Order("compound_weight desc").
		Limit(maxRows).
		Find(&rows).Error; err != nil {
		return out
	}
	hasCompound := false
	for _, r := range rows {
		if clamp01(r.CompoundWeight) > 0 {
			hasCompound = true
			break
		}
	}
	for _, r := range rows {
		keys := conceptKeysFromTrajectory(r.Trajectory)
		if len(keys) == 0 {
			continue
		}
		weight := clamp01(r.CompoundWeight)
		if !hasCompound {
			weight = clamp01(r.SignalStrength)
		}
		if weight <= 0 {
			continue
		}
		for _, k := range keys {
			if weight > out[k] {
				out[k] = weight
			}
		}
	}
	if hasCompound {
		return out
	}
	return map[string]float64{}
}

func loadCrossSetRelevanceByKey(ctx context.Context, db *gorm.DB, userID uuid.UUID, materialSetID uuid.UUID) map[string]float64 {
	out := map[string]float64{}
	if db == nil || userID == uuid.Nil {
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var coverage []*types.MaterialSetConceptCoverage
	if err := db.WithContext(ctx).Model(&types.MaterialSetConceptCoverage{}).
		Where("material_set_id = ?", materialSetID).
		Find(&coverage).Error; err != nil {
		return out
	}
	keyToCanonical := map[string]uuid.UUID{}
	for _, c := range coverage {
		if c == nil || c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(c.ConceptKey))
		if key == "" {
			continue
		}
		keyToCanonical[key] = *c.CanonicalConceptID
	}
	if len(keyToCanonical) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(keyToCanonical))
	for _, id := range keyToCanonical {
		ids = append(ids, id)
	}
	ids = dedupeUUIDs(ids)

	type gcRow struct {
		GlobalConceptID   uuid.UUID `gorm:"column:global_concept_id"`
		CrossSetRelevance float64   `gorm:"column:cross_set_relevance"`
	}
	var rows []gcRow
	if err := db.WithContext(ctx).Model(&types.GlobalConceptCoverage{}).
		Select("global_concept_id", "cross_set_relevance").
		Where("user_id = ? AND global_concept_id IN ?", userID, ids).
		Find(&rows).Error; err != nil {
		return out
	}
	byID := map[uuid.UUID]float64{}
	for _, r := range rows {
		if r.GlobalConceptID == uuid.Nil {
			continue
		}
		byID[r.GlobalConceptID] = clamp01(r.CrossSetRelevance)
	}
	for key, id := range keyToCanonical {
		if id == uuid.Nil {
			continue
		}
		if v := byID[id]; v > 0 {
			out[key] = v
		}
	}
	return out
}

func conceptKeysFromTrajectory(raw datatypes.JSON) []string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var traj map[string]any
	if err := json.Unmarshal(raw, &traj); err != nil {
		return nil
	}
	keys := make([]string, 0)
	keys = append(keys, stringSliceFromAny(traj["establishes"])...)
	keys = append(keys, stringSliceFromAny(traj["reinforces"])...)
	keys = append(keys, stringSliceFromAny(traj["builds_on"])...)
	keys = append(keys, stringSliceFromAny(traj["points_toward"])...)
	out := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func sortConceptKeysByWeight(keys []string, weights map[string]float64) []string {
	if len(keys) == 0 || len(weights) == 0 {
		return keys
	}
	out := dedupeStrings(keys)
	sort.SliceStable(out, func(i, j int) bool {
		a := strings.TrimSpace(strings.ToLower(out[i]))
		b := strings.TrimSpace(strings.ToLower(out[j]))
		wa := weights[a]
		wb := weights[b]
		if wa == wb {
			return out[i] < out[j]
		}
		return wa > wb
	})
	return out
}

func conceptWeightsForKeys(keys []string, weights map[string]float64) map[string]float64 {
	if len(keys) == 0 || len(weights) == 0 {
		return map[string]float64{}
	}
	out := map[string]float64{}
	for _, k := range keys {
		key := strings.TrimSpace(strings.ToLower(k))
		if key == "" {
			continue
		}
		if w, ok := weights[key]; ok {
			out[key] = clamp01(w)
		}
	}
	return out
}

func conceptSignalWeightFactor(meta datatypes.JSON) float64 {
	if len(meta) == 0 || strings.TrimSpace(string(meta)) == "" || strings.TrimSpace(string(meta)) == "null" {
		return 1
	}
	var obj map[string]any
	if err := json.Unmarshal(meta, &obj); err != nil || obj == nil {
		return 1
	}
	sw := clamp01(floatFromAny(obj["signal_weight"], 0))
	if sw <= 0 {
		return 1
	}
	return 0.5 + sw
}

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type InvariantCheck struct {
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Count   int            `json:"count"`
	Sample  []string       `json:"sample,omitempty"`
	Details map[string]any `json:"details,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type InvariantReport struct {
	Status    string           `json:"status"`
	Reason    string           `json:"reason,omitempty"`
	CheckedAt time.Time        `json:"checked_at"`
	PathID    string           `json:"path_id,omitempty"`
	Checks    []InvariantCheck `json:"checks"`
}

func ValidateStructuralInvariants(ctx context.Context, db *gorm.DB, pathID uuid.UUID) InvariantReport {
	report := InvariantReport{
		Status:    "skipped",
		CheckedAt: time.Now().UTC(),
	}
	if db == nil || pathID == uuid.Nil {
		report.Reason = "missing_path"
		return report
	}
	report.PathID = pathID.String()

	checks := make([]InvariantCheck, 0, 4)
	hasFailure := false

	if check, err := checkPrereqCycles(ctx, db, pathID); err != nil {
		checks = append(checks, invariantError("prereq_cycles", err))
		hasFailure = true
	} else {
		checks = append(checks, check)
		if check.Status != "pass" {
			hasFailure = true
		}
	}

	if check, err := checkOrphanedCanonicals(ctx, db, pathID); err != nil {
		checks = append(checks, invariantError("orphaned_canonical_concepts", err))
		hasFailure = true
	} else {
		checks = append(checks, check)
		if check.Status != "pass" {
			hasFailure = true
		}
	}

	if check, err := checkDuplicateCanonicals(ctx, db, pathID); err != nil {
		checks = append(checks, invariantError("duplicate_canonical_mappings", err))
		hasFailure = true
	} else {
		checks = append(checks, check)
		if check.Status != "pass" {
			hasFailure = true
		}
	}

	if check, err := checkDisconnectedPSUs(ctx, db, pathID); err != nil {
		checks = append(checks, invariantError("disconnected_psus", err))
		hasFailure = true
	} else {
		checks = append(checks, check)
		if check.Status != "pass" {
			hasFailure = true
		}
	}

	report.Checks = checks
	if hasFailure {
		report.Status = "fail"
		return report
	}
	report.Status = "pass"
	return report
}

func invariantError(name string, err error) InvariantCheck {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	return InvariantCheck{
		Name:   name,
		Status: "error",
		Count:  0,
		Error:  msg,
	}
}

type edgeRow struct {
	From uuid.UUID `gorm:"column:from_concept_id"`
	To   uuid.UUID `gorm:"column:to_concept_id"`
}

func checkPrereqCycles(ctx context.Context, db *gorm.DB, pathID uuid.UUID) (InvariantCheck, error) {
	check := InvariantCheck{Name: "prereq_cycles", Status: "pass", Count: 0}
	rows := []edgeRow{}
	err := db.WithContext(ctx).
		Table("concept_edge AS e").
		Select("e.from_concept_id, e.to_concept_id").
		Joins("JOIN concept c1 ON c1.id = e.from_concept_id").
		Joins("JOIN concept c2 ON c2.id = e.to_concept_id").
		Where("e.edge_type = ?", "prereq").
		Where("e.deleted_at IS NULL").
		Where("c1.scope = ? AND c1.scope_id = ?", "path", pathID).
		Where("c2.scope = ? AND c2.scope_id = ?", "path", pathID).
		Where("c1.deleted_at IS NULL").
		Where("c2.deleted_at IS NULL").
		Find(&rows).Error
	if err != nil {
		return check, err
	}
	if len(rows) == 0 {
		return check, nil
	}

	adj := map[uuid.UUID][]uuid.UUID{}
	indeg := map[uuid.UUID]int{}
	for _, row := range rows {
		adj[row.From] = append(adj[row.From], row.To)
		if _, ok := indeg[row.From]; !ok {
			indeg[row.From] = 0
		}
		indeg[row.To] = indeg[row.To] + 1
	}
	queue := make([]uuid.UUID, 0, len(indeg))
	for node, deg := range indeg {
		if deg == 0 {
			queue = append(queue, node)
		}
	}
	processed := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		processed++
		for _, to := range adj[n] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	remaining := 0
	sample := []string{}
	for node, deg := range indeg {
		if deg > 0 {
			remaining++
			if len(sample) < 5 {
				sample = append(sample, node.String())
			}
		}
	}
	if remaining > 0 {
		check.Status = "fail"
		check.Count = remaining
		check.Sample = sample
	}
	return check, nil
}

type orphanRow struct {
	ID          uuid.UUID `gorm:"column:id"`
	CanonicalID uuid.UUID `gorm:"column:canonical_concept_id"`
}

func checkOrphanedCanonicals(ctx context.Context, db *gorm.DB, pathID uuid.UUID) (InvariantCheck, error) {
	check := InvariantCheck{Name: "orphaned_canonical_concepts", Status: "pass", Count: 0}
	var total int64
	query := db.WithContext(ctx).
		Table("concept AS c").
		Joins("LEFT JOIN concept g ON g.id = c.canonical_concept_id AND g.scope = 'global' AND g.scope_id IS NULL AND g.deleted_at IS NULL").
		Where("c.scope = ? AND c.scope_id = ?", "path", pathID).
		Where("c.canonical_concept_id IS NOT NULL").
		Where("c.deleted_at IS NULL").
		Where("g.id IS NULL")
	if err := query.Count(&total).Error; err != nil {
		return check, err
	}
	if total == 0 {
		return check, nil
	}
	rows := []orphanRow{}
	if err := query.Select("c.id, c.canonical_concept_id").Limit(10).Find(&rows).Error; err != nil {
		return check, err
	}
	sample := make([]string, 0, len(rows))
	for _, row := range rows {
		sample = append(sample, fmt.Sprintf("%s->%s", row.ID.String(), row.CanonicalID.String()))
	}
	check.Status = "fail"
	check.Count = int(total)
	check.Sample = sample
	return check, nil
}

type dupCanonicalRow struct {
	CanonicalID uuid.UUID `gorm:"column:canonical_concept_id"`
	Count       int       `gorm:"column:count"`
}

func checkDuplicateCanonicals(ctx context.Context, db *gorm.DB, pathID uuid.UUID) (InvariantCheck, error) {
	check := InvariantCheck{Name: "duplicate_canonical_mappings", Status: "pass", Count: 0}
	rows := []dupCanonicalRow{}
	err := db.WithContext(ctx).
		Table("concept").
		Select("canonical_concept_id, COUNT(*) AS count").
		Where("scope = ? AND scope_id = ?", "path", pathID).
		Where("canonical_concept_id IS NOT NULL").
		Where("deleted_at IS NULL").
		Group("canonical_concept_id").
		Having("COUNT(*) > 1").
		Order("count DESC").
		Limit(10).
		Scan(&rows).Error
	if err != nil {
		return check, err
	}
	if len(rows) == 0 {
		return check, nil
	}
	count := 0
	sample := make([]string, 0, len(rows))
	for _, row := range rows {
		count += row.Count
		sample = append(sample, fmt.Sprintf("%s:%d", row.CanonicalID.String(), row.Count))
	}
	if !strictDuplicateCanonicalMappings() {
		// Canonical many-to-one aliases can be intentional (e.g., local synonyms mapping to one global concept).
		// Keep this visible via count/sample but do not hard-fail structural validation by default.
		check.Status = "pass"
		check.Count = count
		check.Sample = sample
		check.Details = map[string]any{
			"strict_mode": false,
			"reason":      "alias_many_to_one_allowed",
		}
		return check, nil
	}
	check.Status = "fail"
	check.Count = count
	check.Sample = sample
	return check, nil
}

func strictDuplicateCanonicalMappings() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("STRUCTURAL_DUPLICATE_CANONICALS_STRICT")))
	switch v {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return false
	}
}

type psuRow struct {
	ID            uuid.UUID      `gorm:"column:id"`
	MemberNodeIDs datatypes.JSON `gorm:"column:member_node_ids"`
}

func checkDisconnectedPSUs(ctx context.Context, db *gorm.DB, pathID uuid.UUID) (InvariantCheck, error) {
	check := InvariantCheck{Name: "disconnected_psus", Status: "pass", Count: 0}
	nodeIDs := []uuid.UUID{}
	if err := db.WithContext(ctx).
		Table("path_node").
		Select("id").
		Where("path_id = ?", pathID).
		Where("deleted_at IS NULL").
		Find(&nodeIDs).Error; err != nil {
		return check, err
	}
	nodeSet := map[uuid.UUID]struct{}{}
	for _, id := range nodeIDs {
		nodeSet[id] = struct{}{}
	}

	psus := []psuRow{}
	if err := db.WithContext(ctx).
		Table("path_structural_unit").
		Select("id, member_node_ids").
		Where("path_id = ?", pathID).
		Where("deleted_at IS NULL").
		Find(&psus).Error; err != nil {
		return check, err
	}
	if len(psus) == 0 {
		return check, nil
	}
	invalid := 0
	sample := []string{}
	for _, psu := range psus {
		ids, err := decodeUUIDList(psu.MemberNodeIDs)
		if err != nil {
			invalid++
			if len(sample) < 10 {
				sample = append(sample, fmt.Sprintf("%s:invalid_member_ids", psu.ID.String()))
			}
			continue
		}
		if len(ids) == 0 {
			invalid++
			if len(sample) < 10 {
				sample = append(sample, fmt.Sprintf("%s:empty_members", psu.ID.String()))
			}
			continue
		}
		missing := 0
		for _, id := range ids {
			if _, ok := nodeSet[id]; !ok {
				missing++
			}
		}
		if missing > 0 {
			invalid++
			if len(sample) < 10 {
				sample = append(sample, fmt.Sprintf("%s:missing_nodes=%d", psu.ID.String(), missing))
			}
		}
	}
	if invalid > 0 {
		check.Status = "fail"
		check.Count = invalid
		check.Sample = sample
	}
	return check, nil
}

func decodeUUIDList(raw datatypes.JSON) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var ids []uuid.UUID
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids, nil
	}
	var strs []string
	if err := json.Unmarshal(raw, &strs); err == nil {
		out := make([]uuid.UUID, 0, len(strs))
		for _, s := range strs {
			id, err := uuid.Parse(s)
			if err != nil {
				return out, err
			}
			out = append(out, id)
		}
		return out, nil
	}
	return nil, fmt.Errorf("invalid member_node_ids format")
}

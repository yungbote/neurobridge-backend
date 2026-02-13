package path_structure_refine

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type conceptRow struct {
	ScopeID            uuid.UUID  `gorm:"column:scope_id"`
	ID                 uuid.UUID  `gorm:"column:id"`
	CanonicalConceptID *uuid.UUID `gorm:"column:canonical_concept_id"`
	SortIndex          int        `gorm:"column:sort_index"`
}

type overlapPair struct {
	APathID           uuid.UUID `json:"a_path_id"`
	BPathID           uuid.UUID `json:"b_path_id"`
	Overlap           float64   `json:"overlap"`
	AContainedInB     float64   `json:"a_contained_in_b"`
	BContainedInA     float64   `json:"b_contained_in_a"`
	SuggestedAction   string    `json:"suggested_action"` // "merge" | "nest" | "cross_link" | "separate"
	SuggestedDetails  string    `json:"suggested_details,omitempty"`
	TopSharedConcepts []string  `json:"top_shared_concepts,omitempty"` // names
}

type dist struct {
	Total  float64
	Weight map[uuid.UUID]float64
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.path == nil {
		jc.Fail("validate", fmt.Errorf("path_structure_refine: pipeline not configured"))
		return nil
	}

	pathID, _ := jc.PayloadUUID("path_id")
	var pathPtr *uuid.UUID
	if pathID != uuid.Nil {
		pathPtr = &pathID
	}
	recordTrace := func(mode string, extra map[string]any, validate bool) error {
		meta := map[string]any{
			"job_run_id":    jc.Job.ID.String(),
			"owner_user_id": jc.Job.OwnerUserID.String(),
			"path_id":       pathID.String(),
			"mode":          mode,
		}
		for k, v := range extra {
			meta[k] = v
		}
		inputs := map[string]any{
			"path_id": pathID.String(),
		}
		chosen := map[string]any{
			"mode": mode,
		}
		userID := jc.Job.OwnerUserID
		_, err := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
			DecisionType:  p.Type(),
			DecisionPhase: "build",
			DecisionMode:  "deterministic",
			UserID:        &userID,
			PathID:        pathPtr,
			Inputs:        inputs,
			Chosen:        chosen,
			Metadata:      meta,
			Payload:       jc.Payload(),
			Validate:      validate,
			RequireTrace:  true,
		})
		return err
	}
	if pathID == uuid.Nil {
		if err := recordTrace("no_path", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "no_path"})
		return nil
	}
	threadID, _ := jc.PayloadUUID("thread_id")

	dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
	row, err := p.path.GetByID(dbc, pathID)
	if err != nil {
		jc.Fail("load_path", err)
		return nil
	}
	if row == nil || row.ID == uuid.Nil || row.UserID == nil || *row.UserID != jc.Job.OwnerUserID {
		if err := recordTrace("path_not_found", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "path_not_found"})
		return nil
	}
	if row.ParentPathID == nil || *row.ParentPathID == uuid.Nil {
		// Not a subpath => nothing to refine.
		if err := recordTrace("no_parent", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "no_parent"})
		return nil
	}
	programID := *row.ParentPathID

	jc.Progress("refine", 6, "Refining structure")

	// Load siblings under the same program.
	siblings, err := p.path.ListByParentID(dbc, jc.Job.OwnerUserID, programID)
	if err != nil {
		jc.Fail("load_siblings", err)
		return nil
	}
	active := make([]*types.Path, 0, len(siblings))
	for _, sp := range siblings {
		if sp == nil || sp.ID == uuid.Nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(sp.Status), "archived") {
			continue
		}
		// Only compare concrete learning paths (not nested programs).
		if strings.EqualFold(strings.TrimSpace(sp.Kind), "program") {
			continue
		}
		active = append(active, sp)
	}
	if len(active) < 2 {
		if err := recordTrace("insufficient_siblings", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "insufficient_siblings"})
		return nil
	}

	// Load all path-scoped concepts for these siblings in one query.
	pathIDs := make([]uuid.UUID, 0, len(active))
	for _, sp := range active {
		pathIDs = append(pathIDs, sp.ID)
	}
	var rows []conceptRow
	if err := p.db.WithContext(jc.Ctx).
		Table("concept").
		Select("scope_id, id, canonical_concept_id, sort_index").
		Where("deleted_at IS NULL").
		Where("scope = ? AND scope_id IN ?", "path", pathIDs).
		Find(&rows).Error; err != nil {
		jc.Fail("load_concepts", err)
		return nil
	}

	// Build per-path concept weight maps.
	byPath := map[uuid.UUID]*dist{}
	for _, pid := range pathIDs {
		byPath[pid] = &dist{Weight: map[uuid.UUID]float64{}}
	}
	for _, r := range rows {
		if r.ScopeID == uuid.Nil || r.ID == uuid.Nil {
			continue
		}
		d := byPath[r.ScopeID]
		if d == nil {
			continue
		}
		cid := r.ID
		if r.CanonicalConceptID != nil && *r.CanonicalConceptID != uuid.Nil {
			cid = *r.CanonicalConceptID
		}
		w := float64(r.SortIndex)
		if w <= 0 {
			w = 1
		}
		d.Weight[cid] += w
		d.Total += w
	}

	// Filter to siblings that actually have a concept graph (skip those still building).
	withGraph := make([]*types.Path, 0, len(active))
	for _, sp := range active {
		d := byPath[sp.ID]
		if d == nil || len(d.Weight) == 0 {
			continue
		}
		withGraph = append(withGraph, sp)
	}
	if len(withGraph) < 2 {
		if err := recordTrace("waiting_for_more_graphs", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "waiting_for_more_graphs"})
		return nil
	}

	mergeMin := envFloat("PATH_STRUCTURE_REFINE_MERGE_MIN_OVERLAP", 0.78)
	crossMin := envFloat("PATH_STRUCTURE_REFINE_CROSSLINK_MIN_OVERLAP", 0.45)
	nestMin := envFloat("PATH_STRUCTURE_REFINE_NEST_MIN_CONTAINMENT", 0.82)
	nestMaxReverse := envFloat("PATH_STRUCTURE_REFINE_NEST_MAX_REVERSE_CONTAINMENT", 0.68)
	contentType := "mixed"
	if adaptiveParamsEnabledForStage("path_structure_refine") && p.files != nil {
		if ct := detectContentTypeForPaths(dbc, p.files, withGraph); ct != "" {
			contentType = ct
		}
		mergeMin = clamp01(adjustRefineThreshold("PATH_STRUCTURE_REFINE_MERGE_MIN_OVERLAP", mergeMin, contentType))
		crossMin = clamp01(adjustRefineThreshold("PATH_STRUCTURE_REFINE_CROSSLINK_MIN_OVERLAP", crossMin, contentType))
		nestMin = clamp01(adjustRefineThreshold("PATH_STRUCTURE_REFINE_NEST_MIN_CONTAINMENT", nestMin, contentType))
		nestMaxReverse = clamp01(adjustRefineThreshold("PATH_STRUCTURE_REFINE_NEST_MAX_REVERSE_CONTAINMENT", nestMaxReverse, contentType))
	}

	// Compute a stable signature across all (path, concept, weight) tuples so we can de-dupe messages.
	signature := computeSignature(withGraph, byPath)

	// Load + update program metadata idempotently.
	prog, err := p.path.GetByID(dbc, programID)
	if err != nil {
		jc.Fail("load_program", err)
		return nil
	}
	if prog == nil || prog.ID == uuid.Nil || prog.UserID == nil || *prog.UserID != jc.Job.OwnerUserID {
		if err := recordTrace("program_not_found", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{"mode": "program_not_found"})
		return nil
	}

	progMeta := map[string]any{}
	if len(prog.Metadata) > 0 && strings.TrimSpace(string(prog.Metadata)) != "" && strings.TrimSpace(string(prog.Metadata)) != "null" {
		_ = json.Unmarshal(prog.Metadata, &progMeta)
	}
	prevRef := mapFromAny(progMeta["structure_refinement_v1"])
	if prevRef != nil {
		if prevSig := strings.TrimSpace(fmt.Sprint(prevRef["signature"])); prevSig != "" && prevSig == signature {
			if err := recordTrace("unchanged", map[string]any{"signature": signature}, false); err != nil {
				jc.Fail("structural_trace", err)
				return nil
			}
			jc.Succeed("done", map[string]any{"mode": "unchanged", "signature": signature})
			return nil
		}
	}

	// Build pairwise overlap + suggestions.
	pairs, sharedConceptIDs := computePairs(withGraph, byPath, mergeMin, crossMin, nestMin, nestMaxReverse)
	sharedNames := loadConceptNames(p.db, jc.Ctx, sharedConceptIDs)
	for i := range pairs {
		if len(pairs[i].TopSharedConcepts) == 0 {
			continue
		}
		names := make([]string, 0, len(pairs[i].TopSharedConcepts))
		for _, idStr := range pairs[i].TopSharedConcepts {
			if nm := strings.TrimSpace(sharedNames[idStr]); nm != "" {
				names = append(names, nm)
			}
		}
		pairs[i].TopSharedConcepts = names
	}

	refObj := map[string]any{
		"version":    1,
		"signature":  signature,
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"pairs":      pairs,
		"thresholds": map[string]any{
			"merge_min_overlap":     mergeMin,
			"crosslink_min_overlap": crossMin,
			"nest_min_containment":  nestMin,
			"nest_max_reverse":      nestMaxReverse,
			"content_type":          contentType,
		},
	}
	progMeta["structure_refinement_v1"] = refObj
	_ = p.path.UpdateFields(dbc, programID, map[string]interface{}{"metadata": datatypes.JSON(mustJSON(progMeta))})

	// Only post a user-facing message when we have something non-trivial to report.
	if threadID != uuid.Nil && p.threads != nil && p.messages != nil && p.notify != nil {
		if shouldPostRefinementMessage(pairs) {
			content := formatRefinementMessage(prog, withGraph, pairs)
			if strings.TrimSpace(content) != "" {
				if created, err := appendRefinementMessage(jc.Ctx, p.db, p.threads, p.messages, p.notify, jc.Job.OwnerUserID, threadID, programID, signature, content); err == nil && created != nil {
					refObj["message_id"] = created.ID.String()
					refObj["message_seq"] = created.Seq
					progMeta["structure_refinement_v1"] = refObj
					_ = p.path.UpdateFields(dbc, programID, map[string]interface{}{"metadata": datatypes.JSON(mustJSON(progMeta))})
				}
			}
		}
	}

	if err := recordTrace("refined", map[string]any{
		"program_id": programID.String(),
		"signature":  signature,
		"pair_count": len(pairs),
	}, true); err != nil {
		jc.Fail("invariant_validation", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"path_id":    pathID.String(),
		"program_id": programID.String(),
		"signature":  signature,
		"pair_count": len(pairs),
	})
	return nil
}

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func computeSignature(paths []*types.Path, byPath map[uuid.UUID]*dist) string {
	type item struct {
		PathID uuid.UUID
		CID    uuid.UUID
		W      float64
	}
	items := make([]item, 0, 256)
	for _, p := range paths {
		if p == nil || p.ID == uuid.Nil {
			continue
		}
		d := byPath[p.ID]
		if d == nil {
			continue
		}
		for cid, w := range d.Weight {
			if cid == uuid.Nil || w <= 0 {
				continue
			}
			items = append(items, item{PathID: p.ID, CID: cid, W: w})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PathID != items[j].PathID {
			return items[i].PathID.String() < items[j].PathID.String()
		}
		if items[i].CID != items[j].CID {
			return items[i].CID.String() < items[j].CID.String()
		}
		return items[i].W < items[j].W
	})
	h := fnv.New64a()
	for _, it := range items {
		_, _ = h.Write([]byte(it.PathID.String()))
		_, _ = h.Write([]byte("|"))
		_, _ = h.Write([]byte(it.CID.String()))
		_, _ = h.Write([]byte("|"))
		_, _ = h.Write([]byte(fmt.Sprintf("%.0f", it.W)))
		_, _ = h.Write([]byte("\n"))
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computePairs(paths []*types.Path, byPath map[uuid.UUID]*dist, mergeMin, crossMin, nestMin, nestMaxReverse float64) ([]overlapPair, map[string]struct{}) {
	out := make([]overlapPair, 0, 16)
	sharedIDs := map[string]struct{}{}
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			a := paths[i]
			b := paths[j]
			if a == nil || b == nil {
				continue
			}
			da := byPath[a.ID]
			db := byPath[b.ID]
			if da == nil || db == nil || da.Total <= 0 || db.Total <= 0 {
				continue
			}

			inter := 0.0
			union := 0.0

			// Compute intersection/union using the smaller map for efficiency.
			var small, large map[uuid.UUID]float64
			if len(da.Weight) <= len(db.Weight) {
				small, large = da.Weight, db.Weight
			} else {
				small, large = db.Weight, da.Weight
			}

			// Track top shared concepts by min weight.
			type shared struct {
				ID uuid.UUID
				W  float64
			}
			sharedTop := make([]shared, 0, 8)

			seen := map[uuid.UUID]bool{}
			for cid, wa := range da.Weight {
				wb := db.Weight[cid]
				if wb > 0 {
					mn := wa
					if wb < mn {
						mn = wb
					}
					inter += mn
					sharedTop = append(sharedTop, shared{ID: cid, W: mn})
					seen[cid] = true
				}
				mx := wa
				if wb > mx {
					mx = wb
				}
				union += mx
			}
			for cid, wb := range db.Weight {
				if seen[cid] {
					continue
				}
				union += wb
			}

			overlap := 0.0
			if union > 0 {
				overlap = inter / union
			}
			aInB := 0.0
			if da.Total > 0 {
				aInB = inter / da.Total
			}
			bInA := 0.0
			if db.Total > 0 {
				bInA = inter / db.Total
			}

			action := "separate"
			details := ""

			if overlap >= mergeMin || (aInB >= 0.9 && bInA >= 0.9) {
				action = "merge"
				details = "Paths overlap heavily; consider merging into one path (or keeping two paths with shared modules)."
			} else if aInB >= nestMin && bInA <= nestMaxReverse {
				action = "nest"
				details = "A looks mostly contained in B; consider making A a prerequisite/subpath before B."
			} else if bInA >= nestMin && aInB <= nestMaxReverse {
				action = "nest"
				details = "B looks mostly contained in A; consider making B a prerequisite/subpath before A."
			} else if overlap >= crossMin {
				action = "cross_link"
				details = "Paths share meaningful concepts; consider cross-links and skipping redundant basics."
			}

			// Shared concept IDs (names filled later).
			sort.Slice(sharedTop, func(i, j int) bool { return sharedTop[i].W > sharedTop[j].W })
			topIDs := make([]string, 0, 5)
			for _, s := range sharedTop {
				if s.ID == uuid.Nil {
					continue
				}
				topIDs = append(topIDs, s.ID.String())
				sharedIDs[s.ID.String()] = struct{}{}
				if len(topIDs) >= 5 {
					break
				}
			}

			_ = small
			_ = large

			out = append(out, overlapPair{
				APathID:           a.ID,
				BPathID:           b.ID,
				Overlap:           round3(overlap),
				AContainedInB:     round3(aInB),
				BContainedInA:     round3(bInA),
				SuggestedAction:   action,
				SuggestedDetails:  details,
				TopSharedConcepts: topIDs,
			})
		}
	}
	// Stable ordering for storage/diffs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].APathID != out[j].APathID {
			return out[i].APathID.String() < out[j].APathID.String()
		}
		return out[i].BPathID.String() < out[j].BPathID.String()
	})
	return out, sharedIDs
}

func round3(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	// ~3dp stable rounding for UI.
	return mathRound(x*1000.0) / 1000.0
}

func mathRound(f float64) float64 {
	if f < 0 {
		return -mathRound(-f)
	}
	return float64(int64(f + 0.5))
}

func loadConceptNames(db *gorm.DB, ctx context.Context, ids map[string]struct{}) map[string]string {
	out := map[string]string{}
	if db == nil || ctx == nil || len(ids) == 0 {
		return out
	}
	uu := make([]uuid.UUID, 0, len(ids))
	for s := range ids {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
			uu = append(uu, id)
		}
	}
	if len(uu) == 0 {
		return out
	}
	type row struct {
		ID   uuid.UUID `gorm:"column:id"`
		Name string    `gorm:"column:name"`
	}
	var rows []row
	_ = db.WithContext(ctx).
		Table("concept").
		Select("id, name").
		Where("id IN ? AND deleted_at IS NULL", uu).
		Find(&rows).Error
	for _, r := range rows {
		if r.ID == uuid.Nil {
			continue
		}
		out[r.ID.String()] = strings.TrimSpace(r.Name)
	}
	return out
}

func shouldPostRefinementMessage(pairs []overlapPair) bool {
	for _, p := range pairs {
		if p.SuggestedAction != "" && p.SuggestedAction != "separate" {
			return true
		}
	}
	return false
}

func formatRefinementMessage(program *types.Path, paths []*types.Path, pairs []overlapPair) string {
	if program == nil || program.ID == uuid.Nil || len(paths) < 2 {
		return ""
	}
	nameByID := map[uuid.UUID]string{}
	for _, p := range paths {
		if p == nil || p.ID == uuid.Nil {
			continue
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = p.ID.String()
		}
		nameByID[p.ID] = title
	}

	var b strings.Builder
	b.WriteString("I compared the concept graphs across your paths to see what overlaps and what should stay separate.\n\n")

	// Show a few strongest non-trivial relationships first.
	type scored struct {
		P overlapPair
		S float64
	}
	sc := make([]scored, 0, len(pairs))
	for _, p := range pairs {
		if p.SuggestedAction == "separate" {
			continue
		}
		sc = append(sc, scored{P: p, S: p.Overlap})
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].S > sc[j].S })
	if len(sc) > 6 {
		sc = sc[:6]
	}
	for _, it := range sc {
		a := nameByID[it.P.APathID]
		bb := nameByID[it.P.BPathID]
		b.WriteString(fmt.Sprintf("- %s ↔ %s: overlap %.3f (A⊂B %.3f, B⊂A %.3f) → **%s**\n",
			a, bb, it.P.Overlap, it.P.AContainedInB, it.P.BContainedInA, it.P.SuggestedAction,
		))
		if len(it.P.TopSharedConcepts) > 0 {
			b.WriteString("  - Shared: " + strings.Join(it.P.TopSharedConcepts, " • ") + "\n")
		}
	}

	b.WriteString("\nDefault behavior is non-destructive: keep paths separate but add cross-links and skip redundant basics when overlap is high.\n")
	b.WriteString("If you want to merge into one combined path, reply `undo split`. If you want to keep the split, reply `keep as-is`.\n")
	return strings.TrimSpace(b.String())
}

func appendRefinementMessage(
	ctx context.Context,
	db *gorm.DB,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	notify services.ChatNotifier,
	owner uuid.UUID,
	threadID uuid.UUID,
	programID uuid.UUID,
	signature string,
	content string,
) (*types.ChatMessage, error) {
	if db == nil || threads == nil || messages == nil || owner == uuid.Nil || threadID == uuid.Nil {
		return nil, nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	if strings.TrimSpace(signature) == "" {
		return nil, nil
	}

	var created *types.ChatMessage
	createdNew := false

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: ctx, Tx: tx}
		th, err := threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: one refinement message per (program, signature).
		var existing types.ChatMessage
		e := tx.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'signature' = ?", threadID, owner, "path_structure_refinement", signature).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			created = &existing
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
		meta := map[string]any{
			"kind":       "path_structure_refinement",
			"program_id": programID.String(),
			"signature":  signature,
		}
		metaJSON, _ := json.Marshal(meta)

		nextSeq := th.NextSeq + 1
		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       nextSeq,
			Role:      "assistant",
			Status:    "sent",
			Content:   content,
			Metadata:  datatypes.JSON(metaJSON),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}
		if err := threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if createdNew && created != nil && notify != nil {
		notify.MessageCreated(owner, threadID, created, nil)
	}
	return created, nil
}

func adaptiveParamsEnabledForStage(stage string) bool {
	if !envBool("ADAPTIVE_PARAMS_ENABLED", true) {
		return false
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return true
	}
	envKey := "ADAPTIVE_PARAMS_DISABLE_" + strings.ToUpper(stage)
	if envBool(envKey, false) {
		return false
	}
	if raw := strings.TrimSpace(os.Getenv("ADAPTIVE_PARAMS_DISABLE_STAGES")); raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			if strings.EqualFold(strings.TrimSpace(p), stage) {
				return false
			}
		}
	}
	return true
}

func detectContentTypeForPaths(dbc dbctx.Context, filesRepo repos.MaterialFileRepo, paths []*types.Path) string {
	if filesRepo == nil || len(paths) == 0 {
		return "mixed"
	}
	setIDs := make([]uuid.UUID, 0, len(paths))
	for _, p := range paths {
		if p == nil || p.MaterialSetID == nil || *p.MaterialSetID == uuid.Nil {
			continue
		}
		setIDs = append(setIDs, *p.MaterialSetID)
	}
	setIDs = dedupeUUIDs(setIDs)
	if len(setIDs) == 0 {
		return "mixed"
	}
	files, err := filesRepo.GetByMaterialSetIDs(dbc, setIDs)
	if err != nil || len(files) == 0 {
		return "mixed"
	}
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".java": true,
		".c": true, ".cc": true, ".cpp": true, ".rs": true, ".cs": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true, ".m": true,
	}
	slideExts := map[string]bool{".ppt": true, ".pptx": true, ".key": true}

	codeCount := 0
	slideCount := 0
	for _, f := range files {
		if f == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		mime := strings.ToLower(strings.TrimSpace(f.MimeType))
		kind := strings.ToLower(strings.TrimSpace(f.ExtractedKind))
		ext := strings.ToLower(filepath.Ext(name))

		if slideExts[ext] || strings.Contains(mime, "presentation") || strings.Contains(kind, "slides") {
			slideCount++
			continue
		}
		if codeExts[ext] || strings.HasPrefix(mime, "text/x-") || strings.Contains(mime, "text/plain") {
			codeCount++
			continue
		}
	}

	total := len(files)
	if slideCount*100 >= total*60 {
		return "slides"
	}
	if codeCount*100 >= total*60 {
		return "code"
	}
	if slideCount == 0 && codeCount == 0 {
		return "prose"
	}
	return "mixed"
}

func adjustRefineThreshold(name string, base float64, contentType string) float64 {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch name {
	case "PATH_STRUCTURE_REFINE_CROSSLINK_MIN_OVERLAP":
		switch ct {
		case "prose":
			return base - 0.05
		case "slides":
			return base - 0.04
		case "mixed":
			return base - 0.03
		case "code":
			return base + 0.01
		}
	case "PATH_STRUCTURE_REFINE_MERGE_MIN_OVERLAP":
		switch ct {
		case "code":
			return base + 0.05
		case "prose":
			return base - 0.02
		case "slides":
			return base - 0.03
		case "mixed":
			return base + 0.01
		}
	case "PATH_STRUCTURE_REFINE_NEST_MIN_CONTAINMENT":
		switch ct {
		case "code":
			return base + 0.04
		case "prose":
			return base + 0.02
		case "slides":
			return base - 0.02
		case "mixed":
			return base + 0.01
		}
	case "PATH_STRUCTURE_REFINE_NEST_MAX_REVERSE_CONTAINMENT":
		switch ct {
		case "mixed":
			return base - 0.05
		case "slides":
			return base - 0.03
		case "code":
			return base + 0.02
		case "prose":
			return base + 0.01
		}
	}
	return base
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func dedupeUUIDs(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

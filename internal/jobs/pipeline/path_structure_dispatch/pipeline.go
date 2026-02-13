package path_structure_dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type intakePathGroup struct {
	ID             string
	Title          string
	Goal           string
	Notes          string
	CoreFileIDs    []string
	SupportFileIDs []string
	FileIDs        []string
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.jobs == nil || p.jobRuns == nil || p.path == nil || p.files == nil || p.materialSets == nil || p.materialSetFiles == nil || p.uli == nil {
		jc.Fail("validate", fmt.Errorf("path_structure_dispatch: pipeline not configured"))
		return nil
	}

	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}

	parentPathID, _ := jc.PayloadUUID("path_id")
	if parentPathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}

	dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
	parent, err := p.path.GetByID(dbc, parentPathID)
	if err != nil {
		jc.Fail("load_path", err)
		return nil
	}
	if parent == nil || parent.ID == uuid.Nil || parent.UserID == nil || *parent.UserID != jc.Job.OwnerUserID {
		jc.Fail("load_path", fmt.Errorf("path not found"))
		return nil
	}

	recordTrace := func(mode string, extra map[string]any, validate bool) error {
		meta := map[string]any{
			"job_run_id":      jc.Job.ID.String(),
			"owner_user_id":   jc.Job.OwnerUserID.String(),
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            mode,
		}
		for k, v := range extra {
			meta[k] = v
		}
		inputs := map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
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
			PathID:        &parentPathID,
			MaterialSetID: &setID,
			Inputs:        inputs,
			Chosen:        chosen,
			Metadata:      meta,
			Payload:       jc.Payload(),
			Validate:      validate,
			RequireTrace:  true,
		})
		return err
	}

	// Parse intake from path.metadata (written by path_intake).
	meta := map[string]any{}
	if len(parent.Metadata) > 0 && strings.TrimSpace(string(parent.Metadata)) != "" && strings.TrimSpace(string(parent.Metadata)) != "null" {
		_ = json.Unmarshal(parent.Metadata, &meta)
	}
	intake := mapFromAny(meta["intake"])
	if intake == nil {
		if err := recordTrace("no_intake", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "no_intake",
		})
		return nil
	}

	paths := sliceAny(intake["paths"])
	if len(paths) <= 1 {
		if err := recordTrace("single_path", nil, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "single_path",
		})
		return nil
	}
	if !boolFromAny(intake["paths_confirmed"]) {
		if err := recordTrace("single_path", map[string]any{"reason": "paths_not_confirmed"}, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "single_path",
			"reason":          "paths_not_confirmed",
		})
		return nil
	}

	jc.Progress("dispatch", 5, "Creating separate paths from intake grouping")

	// Load material files for validation.
	files, err := p.files.GetByMaterialSetID(dbc, setID)
	if err != nil {
		jc.Fail("load_files", err)
		return nil
	}
	validFileIDs := map[string]bool{}
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		validFileIDs[f.ID.String()] = true
	}

	orderedFileIDs := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		orderedFileIDs = append(orderedFileIDs, f.ID.String())
	}

	assigned := map[string]bool{}
	groups := make([]intakePathGroup, 0, len(paths))
	for i, raw := range paths {
		m, ok := raw.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["path_id"]))
		if id == "" {
			id = fmt.Sprintf("path_%d", i+1)
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		goal := strings.TrimSpace(stringFromAny(m["goal"]))
		notes := strings.TrimSpace(stringFromAny(m["notes"]))

		coreIDs := dedupeStrings(filterIDs(validFileIDs, stringSliceFromAny(m["core_file_ids"])))
		supportIDs := dedupeStrings(filterIDs(validFileIDs, stringSliceFromAny(m["support_file_ids"])))

		assignUnique := func(ids []string) []string {
			out := make([]string, 0, len(ids))
			for _, fid := range ids {
				fid = strings.TrimSpace(fid)
				if fid == "" || assigned[fid] {
					continue
				}
				assigned[fid] = true
				out = append(out, fid)
			}
			return out
		}

		coreIDs = assignUnique(coreIDs)
		supportIDs = assignUnique(supportIDs)
		fileIDs := dedupeStrings(append(coreIDs, supportIDs...))
		if len(fileIDs) == 0 {
			continue
		}

		groups = append(groups, intakePathGroup{
			ID:             id,
			Title:          title,
			Goal:           goal,
			Notes:          notes,
			CoreFileIDs:    coreIDs,
			SupportFileIDs: supportIDs,
			FileIDs:        fileIDs,
		})
	}

	if len(groups) == 0 {
		if err := recordTrace("single_path", map[string]any{"reason": "no_valid_groups"}, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "single_path",
			"reason":          "no_valid_groups",
		})
		return nil
	}

	primaryID := strings.TrimSpace(stringFromAny(intake["primary_path_id"]))
	primaryIdx := 0
	if primaryID != "" {
		for i, g := range groups {
			if g.ID == primaryID {
				primaryIdx = i
				break
			}
		}
	}

	unassigned := make([]string, 0, len(validFileIDs))
	for _, id := range orderedFileIDs {
		if !assigned[id] {
			unassigned = append(unassigned, id)
		}
	}
	if len(unassigned) > 0 {
		pg := &groups[primaryIdx]
		pg.CoreFileIDs = dedupeStrings(append(pg.CoreFileIDs, unassigned...))
		pg.FileIDs = dedupeStrings(append(pg.FileIDs, unassigned...))
	}

	if len(groups) <= 1 {
		if err := recordTrace("single_path", map[string]any{"reason": "single_group_after_normalization"}, false); err != nil {
			jc.Fail("structural_trace", err)
			return nil
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "single_path",
			"reason":          "single_group_after_normalization",
		})
		return nil
	}

	primaryGroup := groups[primaryIdx]
	secondaryGroups := make([]intakePathGroup, 0, len(groups)-1)
	for i, g := range groups {
		if i == primaryIdx {
			continue
		}
		secondaryGroups = append(secondaryGroups, g)
	}

	primarySet := map[string]bool{}
	for _, id := range primaryGroup.FileIDs {
		primarySet[id] = true
	}
	otherFileIDs := make([]string, 0, len(orderedFileIDs))
	for _, id := range orderedFileIDs {
		if !primarySet[id] {
			otherFileIDs = append(otherFileIDs, id)
		}
	}

	parentUpdates := map[string]interface{}{}
	if parent.MaterialSetID == nil || *parent.MaterialSetID == uuid.Nil {
		parentUpdates["material_set_id"] = setID
	}
	if parent.RootPathID == nil || *parent.RootPathID == uuid.Nil {
		parentUpdates["root_path_id"] = parentPathID
	}
	if strings.TrimSpace(parent.Title) == "" || strings.TrimSpace(parent.Title) == "Generating path…" {
		if strings.TrimSpace(primaryGroup.Title) != "" {
			parentUpdates["title"] = primaryGroup.Title
		} else if strings.TrimSpace(primaryGroup.Goal) != "" {
			parentUpdates["title"] = primaryGroup.Goal
		} else if goal := strings.TrimSpace(stringFromAny(intake["combined_goal"])); goal != "" {
			parentUpdates["title"] = goal
		}
	}
	if len(parentUpdates) > 0 {
		_ = p.path.UpdateFields(dbc, parentPathID, parentUpdates)
	}

	parentMeta := map[string]any{}
	if len(parent.Metadata) > 0 {
		_ = json.Unmarshal(parent.Metadata, &parentMeta)
	}
	parentIntake := mapFromAny(parentMeta["intake"])
	if parentIntake == nil {
		parentIntake = map[string]any{}
	}
	parentMa := mapFromAny(parentIntake["material_alignment"])
	if parentMa == nil {
		parentMa = map[string]any{}
	}
	parentMa["include_file_ids"] = primaryGroup.FileIDs
	parentMa["exclude_file_ids"] = otherFileIDs
	parentMa["mode"] = "single_goal"
	parentIntake["material_alignment"] = parentMa
	parentIntake["paths"] = []map[string]any{
		{
			"path_id":          primaryGroup.ID,
			"title":            primaryGroup.Title,
			"goal":             primaryGroup.Goal,
			"core_file_ids":    primaryGroup.CoreFileIDs,
			"support_file_ids": primaryGroup.SupportFileIDs,
			"confidence":       floatFromAny(parentIntake["confidence"], 0.8),
			"notes":            primaryGroup.Notes,
		},
	}
	parentIntake["primary_path_id"] = primaryGroup.ID
	parentIntake["paths_confirmed"] = true
	if strings.TrimSpace(primaryGroup.Goal) != "" {
		parentIntake["combined_goal"] = primaryGroup.Goal
	}
	parentMeta["intake"] = parentIntake
	parentMeta["intake_material_filter"] = map[string]any{
		"mode":             "single_goal",
		"include_file_ids": primaryGroup.FileIDs,
		"exclude_file_ids": otherFileIDs,
	}

	now := time.Now().UTC()
	parentMeta["paths_split_at"] = now.Format(time.RFC3339Nano)

	splitPaths := []map[string]any{
		{
			"path_id":         parentPathID.String(),
			"path_group_id":   primaryGroup.ID,
			"title":           primaryGroup.Title,
			"goal":            primaryGroup.Goal,
			"file_ids":        primaryGroup.FileIDs,
			"material_set_id": setID.String(),
		},
	}

	sourceID := setID
	rootID := parentPathID
	if parent.RootPathID != nil && *parent.RootPathID != uuid.Nil {
		rootID = *parent.RootPathID
	}

	for idx, group := range secondaryGroups {
		groupKey := strings.TrimSpace(group.ID)
		if groupKey == "" {
			groupKey = fmt.Sprintf("path_group_%d", idx+1)
		}
		derivedSetID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("derived_path_group|"+setID.String()+"|"+groupKey))
		derivedPathID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("derived_path_group_path|"+setID.String()+"|"+groupKey))

		setMeta := map[string]any{
			"kind":                   "derived_path_group",
			"source_material_set_id": sourceID.String(),
			"path_group_id":          group.ID,
			"path_group_title":       group.Title,
			"path_group_goal":        group.Goal,
			"include_file_ids":       group.FileIDs,
			"created_by":             "path_structure_dispatch",
			"created_at":             now.Format(time.RFC3339Nano),
		}

		if err := p.db.WithContext(jc.Ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&types.MaterialSet{
				ID:                  derivedSetID,
				UserID:              jc.Job.OwnerUserID,
				Title:               stringsOr(group.Title, stringsOr(group.Goal, "Derived path materials")),
				Description:         fmt.Sprintf("Materials grouped into the %s path.", stringsOr(group.Title, stringsOr(group.Goal, "derived"))),
				Status:              "ready",
				SourceMaterialSetID: &sourceID,
				Metadata:            datatypes.JSON(mustJSON(setMeta)),
				CreatedAt:           now,
				UpdatedAt:           now,
			}).Error; err != nil {
			jc.Fail("derive_material_set", err)
			return nil
		}

		linkRows := make([]*types.MaterialSetFile, 0, len(group.FileIDs))
		for _, fidStr := range group.FileIDs {
			fid, err := uuid.Parse(strings.TrimSpace(fidStr))
			if err != nil || fid == uuid.Nil {
				continue
			}
			linkRows = append(linkRows, &types.MaterialSetFile{
				ID:             uuid.New(),
				MaterialSetID:  derivedSetID,
				MaterialFileID: fid,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
		if err := p.materialSetFiles.CreateIgnoreDuplicates(dbc, linkRows); err != nil {
			jc.Fail("derive_material_set", err)
			return nil
		}

		jc.Progress("dispatch", 50+idx, "Creating split path")

		groupIntake := map[string]any{
			"material_alignment": map[string]any{
				"mode":             "single_goal",
				"primary_goal":     stringsOr(group.Goal, group.Title),
				"include_file_ids": group.FileIDs,
				"exclude_file_ids": []string{},
				"noise_file_ids":   []string{},
				"notes":            stringsOr(group.Notes, "Derived from intake grouping."),
			},
			"paths": []map[string]any{
				{
					"path_id":          group.ID,
					"title":            group.Title,
					"goal":             group.Goal,
					"core_file_ids":    group.CoreFileIDs,
					"support_file_ids": group.SupportFileIDs,
					"confidence":       0.9,
					"notes":            group.Notes,
				},
			},
			"primary_path_id":      group.ID,
			"combined_goal":        stringsOr(group.Goal, stringsOr(group.Title, "Learn the uploaded materials")),
			"audience_level_guess": strings.TrimSpace(stringFromAny(intake["audience_level_guess"])),
			"confidence":           0.9,
			"needs_clarification":  false,
			"paths_confirmed":      true,
		}
		if li := intake["learning_intent"]; li != nil {
			groupIntake["learning_intent"] = li
		}
		groupFileIntents := make([]any, 0, len(group.FileIDs))
		groupSet := map[string]bool{}
		for _, fid := range group.FileIDs {
			groupSet[fid] = true
		}
		for _, it := range sliceAny(intake["file_intents"]) {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			if id != "" && groupSet[id] {
				groupFileIntents = append(groupFileIntents, m)
			}
		}
		if len(groupFileIntents) > 0 {
			groupIntake["file_intents"] = groupFileIntents
		}

		groupMeta := map[string]any{
			"intake_locked":          true,
			"source_material_set_id": setID.String(),
			"path_group_id":          group.ID,
			"path_group_title":       group.Title,
			"path_group_goal":        group.Goal,
			"path_split_from":        parentPathID.String(),
			"intake_material_filter": map[string]any{
				"mode":             "single_goal",
				"include_file_ids": group.FileIDs,
				"exclude_file_ids": []string{},
				"notes":            "Derived from intake grouping.",
			},
			"intake":            groupIntake,
			"intake_updated_at": now.Format(time.RFC3339Nano),
		}

		pathTitle := stringsOr(group.Title, stringsOr(group.Goal, "Generating path…"))
		pathDescription := fmt.Sprintf("Learning path for %s (split from upload)", stringsOr(group.Title, stringsOr(group.Goal, "grouped materials")))

		existingPath, err := p.path.GetByID(dbc, derivedPathID)
		if err != nil {
			jc.Fail("load_split_path", err)
			return nil
		}
		if existingPath == nil || existingPath.ID == uuid.Nil {
			newPath := &types.Path{
				ID:            derivedPathID,
				UserID:        parent.UserID,
				MaterialSetID: &derivedSetID,
				ParentPathID:  nil,
				RootPathID:    &rootID,
				Depth:         parent.Depth,
				SortIndex:     parent.SortIndex + idx + 1,
				Kind:          "path",
				Title:         pathTitle,
				Description:   pathDescription,
				Status:        "draft",
				JobID:         nil,
				Metadata:      datatypes.JSON(mustJSON(groupMeta)),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if _, err := p.path.Create(dbc, []*types.Path{newPath}); err != nil {
				jc.Fail("create_split_path", err)
				return nil
			}
		} else {
			update := map[string]interface{}{
				"metadata":   datatypes.JSON(mustJSON(groupMeta)),
				"updated_at": now,
			}
			if existingPath.MaterialSetID == nil || *existingPath.MaterialSetID == uuid.Nil {
				update["material_set_id"] = derivedSetID
			}
			if strings.TrimSpace(existingPath.Title) == "" || strings.TrimSpace(existingPath.Title) == "Generating path…" {
				update["title"] = pathTitle
			}
			if strings.TrimSpace(existingPath.Description) == "" {
				update["description"] = pathDescription
			}
			_ = p.path.UpdateFields(dbc, existingPath.ID, update)
		}

		if p.uli != nil {
			_ = p.uli.UpsertPathID(dbc, jc.Job.OwnerUserID, derivedSetID, derivedPathID)
		}

		jobType := strings.TrimSpace(fmt.Sprint(jc.Payload()["build_job_type"]))
		if jobType == "" {
			jobType = "learning_build"
		}
		has, _ := p.jobRuns.HasRunnableForEntity(dbc, jc.Job.OwnerUserID, "path", derivedPathID, jobType)
		var splitJobID uuid.UUID
		if !has {
			payload := map[string]any{
				"material_set_id": derivedSetID.String(),
				"path_id":         derivedPathID.String(),
				"build_job_type":  jobType,
			}
			if threadID, ok := jc.PayloadUUID("thread_id"); ok && threadID != uuid.Nil {
				payload["thread_id"] = threadID.String()
			}
			entityID := derivedPathID
			j, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: p.db}, jc.Job.OwnerUserID, jobType, "path", &entityID, payload)
			if err != nil {
				jc.Fail("enqueue_split_path", err)
				return nil
			}
			splitJobID = j.ID
			_ = p.path.UpdateFields(dbc, derivedPathID, map[string]interface{}{"job_id": splitJobID})
		} else if existingPath != nil && existingPath.JobID != nil && *existingPath.JobID != uuid.Nil {
			splitJobID = *existingPath.JobID
		}

		splitPaths = append(splitPaths, map[string]any{
			"path_id":         derivedPathID.String(),
			"path_group_id":   group.ID,
			"title":           group.Title,
			"goal":            group.Goal,
			"file_ids":        group.FileIDs,
			"material_set_id": derivedSetID.String(),
			"job_id":          splitJobID.String(),
		})
	}

	parentMeta["path_split"] = map[string]any{
		"split_at": now.Format(time.RFC3339Nano),
		"paths":    splitPaths,
	}
	parentMetaFinal, _ := json.Marshal(parentMeta)
	_ = p.path.UpdateFields(dbc, parentPathID, map[string]interface{}{"metadata": datatypes.JSON(parentMetaFinal)})

	if err := recordTrace("paths_split", map[string]any{
		"primary_group": primaryGroup.ID,
		"split_paths":   splitPaths,
	}, true); err != nil {
		jc.Fail("invariant_validation", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"path_id":         parentPathID.String(),
		"mode":            "paths_split",
		"primary_group":   primaryGroup.ID,
		"split_paths":     splitPaths,
	})
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func stringFromAny(v any) string {
	return strings.TrimSpace(fmt.Sprint(v))
}

func stringsOr(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fallback
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func boolFromAny(v any) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		s := strings.ToLower(strings.TrimSpace(b))
		return s == "true" || s == "1" || s == "yes"
	case int:
		return b != 0
	case float64:
		return b != 0
	default:
		return false
	}
}

func floatFromAny(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	case string:
		if s := strings.TrimSpace(x); s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f
			}
		}
	}
	return def
}

func sliceAny(v any) []any {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if ok {
		return arr
	}
	return nil
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		out := make([]string, 0, len(ss))
		for _, s := range ss {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	arr, ok := v.([]any)
	if !ok {
		s := strings.TrimSpace(stringFromAny(v))
		if s == "" {
			return nil
		}
		return []string{s}
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s := strings.TrimSpace(stringFromAny(x))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func filterIDs(valid map[string]bool, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if valid != nil && len(valid) > 0 && !valid[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func envIntAllowZero(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

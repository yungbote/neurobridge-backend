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
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

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

	// Parse intake from path.metadata (written by path_intake).
	meta := map[string]any{}
	if len(parent.Metadata) > 0 && strings.TrimSpace(string(parent.Metadata)) != "" && strings.TrimSpace(string(parent.Metadata)) != "null" {
		_ = json.Unmarshal(parent.Metadata, &meta)
	}
	intake := mapFromAny(meta["intake"])
	if intake == nil {
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "no_intake",
		})
		return nil
	}

	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	multiGoal := mode == "multi_goal" || len(tracks) > 1

	ps := mapFromAny(intake["path_structure"])
	selected := strings.ToLower(strings.TrimSpace(stringFromAny(ps["selected_mode"])))
	// Only split when the user explicitly confirmed they want a program + subpaths.
	// Treat "unspecified" as "single_path" so we never spawn multiple paths by accident.
	if selected == "" || selected == "unspecified" {
		selected = "single_path"
	}
	splitWanted := multiGoal && selected == "program_with_subpaths"

	if !splitWanted {
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "single_path",
		})
		return nil
	}

	jc.Progress("dispatch", 5, "Splitting into subpaths")

	// Load material files for validation + goal seed.
	files, err := p.files.GetByMaterialSetID(dbc, setID)
	if err != nil {
		jc.Fail("load_files", err)
		return nil
	}
	validFileIDs := map[string]bool{}
	goalSeedIDs := make([]string, 0, 1)
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		id := f.ID.String()
		validFileIDs[id] = true
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		if name == "learning_goal.txt" || name == "learning_goal.md" {
			goalSeedIDs = append(goalSeedIDs, id)
		}
	}
	goalSeedIDs = dedupeStrings(goalSeedIDs)

	// Backfill parent hierarchy fields (best-effort) and mark it as a program container.
	parentUpdates := map[string]interface{}{}
	if parent.MaterialSetID == nil || *parent.MaterialSetID == uuid.Nil {
		parentUpdates["material_set_id"] = setID
	}
	if parent.RootPathID == nil || *parent.RootPathID == uuid.Nil {
		parentUpdates["root_path_id"] = parentPathID
	}
	if strings.TrimSpace(parent.Kind) == "" || strings.EqualFold(strings.TrimSpace(parent.Kind), "path") {
		parentUpdates["kind"] = "program"
	} else if !strings.EqualFold(strings.TrimSpace(parent.Kind), "program") {
		parentUpdates["kind"] = "program"
	}
	if strings.TrimSpace(parent.Title) == "" || strings.TrimSpace(parent.Title) == "Generating path…" {
		if goal := strings.TrimSpace(stringFromAny(intake["combined_goal"])); goal != "" {
			parentUpdates["title"] = goal
		}
	}
	if len(parentUpdates) > 0 {
		_ = p.path.UpdateFields(dbc, parentPathID, parentUpdates)
	}

	// Order tracks: primary first, then stable by title.
	primaryTrackID := strings.TrimSpace(stringFromAny(intake["primary_track_id"]))
	type trackSpec struct {
		TrackID string
		Title   string
		Goal    string
		Files   []string
	}
	specs := make([]trackSpec, 0, len(tracks))
	for _, t := range tracks {
		m, ok := t.(map[string]any)
		if !ok || m == nil {
			continue
		}
		tid := strings.TrimSpace(stringFromAny(m["track_id"]))
		if tid == "" {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		goal := strings.TrimSpace(stringFromAny(m["goal"]))
		if title == "" {
			title = goal
		}
		if goal == "" {
			goal = title
		}
		core := filterIDs(validFileIDs, stringSliceFromAny(m["core_file_ids"]))
		support := filterIDs(validFileIDs, stringSliceFromAny(m["support_file_ids"]))
		include := dedupeStrings(append(core, support...))
		// Always include goal seed file if present.
		if len(goalSeedIDs) > 0 {
			include = dedupeStrings(append(goalSeedIDs, include...))
		}
		if len(include) == 0 {
			// Backstop: if track files are empty, include everything.
			for id := range validFileIDs {
				include = append(include, id)
			}
			include = dedupeStrings(include)
		}
		specs = append(specs, trackSpec{TrackID: tid, Title: title, Goal: goal, Files: include})
	}
	if len(specs) == 0 {
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"path_id":         parentPathID.String(),
			"mode":            "program_with_subpaths",
			"subpaths":        []any{},
		})
		return nil
	}
	sort.Slice(specs, func(i, j int) bool {
		a, b := specs[i], specs[j]
		if primaryTrackID != "" {
			if a.TrackID == primaryTrackID && b.TrackID != primaryTrackID {
				return true
			}
			if b.TrackID == primaryTrackID && a.TrackID != primaryTrackID {
				return false
			}
		}
		if a.Title != b.Title {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
		return a.TrackID < b.TrackID
	})

	maxTracks := envIntAllowZero("PATH_SPLIT_MAX_SUBPATHS", 6)
	if maxTracks < 1 {
		maxTracks = 6
	}
	if maxTracks > 25 {
		maxTracks = 25
	}
	if len(specs) > maxTracks {
		specs = specs[:maxTracks]
	}

	// Load existing subpaths for idempotency.
	existing, _ := p.path.ListByParentID(dbc, jc.Job.OwnerUserID, parentPathID)
	existingByTrack := map[string]*types.Path{}
	for _, child := range existing {
		if child == nil || child.ID == uuid.Nil {
			continue
		}
		cmeta := map[string]any{}
		if len(child.Metadata) > 0 && strings.TrimSpace(string(child.Metadata)) != "" && strings.TrimSpace(string(child.Metadata)) != "null" {
			_ = json.Unmarshal(child.Metadata, &cmeta)
		}
		tid := strings.TrimSpace(stringFromAny(cmeta["subpath_track_id"]))
		if tid != "" && existingByTrack[tid] == nil {
			existingByTrack[tid] = child
		}
	}

	rootID := parentPathID
	if parent.RootPathID != nil && *parent.RootPathID != uuid.Nil {
		rootID = *parent.RootPathID
	}
	parentDepth := parent.Depth
	if parentDepth < 0 {
		parentDepth = 0
	}

	now := time.Now().UTC()
	subpathsOut := make([]map[string]any, 0, len(specs))
	for i, sp := range specs {
		child := existingByTrack[sp.TrackID]
		derivedSetID := uuid.Nil
		if child != nil && child.MaterialSetID != nil && *child.MaterialSetID != uuid.Nil {
			derivedSetID = *child.MaterialSetID
		}

		if child == nil || child.ID == uuid.Nil {
			// Create a derived material_set that references a subset of the source set's files.
			//
			// This makes each subpath a first-class "material set" so downstream stages can use:
			// - separate summaries,
			// - separate chunk retrieval filters,
			// - separate (user, material_set)->path indexing,
			// without duplicating files/chunks.
			derivedSetID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("derived_material_set|"+setID.String()+"|"+strings.TrimSpace(sp.TrackID)))
			sourceID := setID

			setMeta := map[string]any{
				"kind":                   "derived_track",
				"source_material_set_id": sourceID.String(),
				"track_id":               sp.TrackID,
				"track_title":            sp.Title,
				"track_goal":             sp.Goal,
				"include_file_ids":       sp.Files,
				"created_by":             "path_structure_dispatch",
				"created_at":             now.Format(time.RFC3339Nano),
			}

			// Create if missing (idempotent via deterministic UUID + ON CONFLICT DO NOTHING).
			if err := p.db.WithContext(jc.Ctx).
				Clauses(clause.OnConflict{DoNothing: true}).
				Create(&types.MaterialSet{
					ID:                  derivedSetID,
					UserID:              jc.Job.OwnerUserID,
					Title:               stringsOr(sp.Title, stringsOr(sp.Goal, "Derived materials")),
					Description:         stringsOr(sp.Goal, ""),
					Status:              "ready",
					SourceMaterialSetID: &sourceID,
					Metadata:            datatypes.JSON(mustJSON(setMeta)),
					CreatedAt:           now,
					UpdatedAt:           now,
				}).Error; err != nil {
				jc.Fail("derive_material_sets", err)
				return nil
			}

			// Link membership rows (idempotent).
			linkRows := make([]*types.MaterialSetFile, 0, len(sp.Files))
			for _, fidStr := range sp.Files {
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
				jc.Fail("derive_material_sets", err)
				return nil
			}

			msid := derivedSetID
			pid := parentPathID
			rid := rootID

			childMeta := map[string]any{
				"intake_locked": true,
				"intake_md": strings.TrimSpace(strings.Join([]string{
					"**Goal**: " + sp.Goal,
					"**Track**: " + sp.Title,
				}, "\n")),
				"source_material_set_id": setID.String(),
				"intake_material_filter": map[string]any{
					"mode":             "single_goal",
					"primary_goal":     sp.Goal,
					"include_file_ids": sp.Files,
					"exclude_file_ids": []string{},
					"notes":            "Subpath derived from a multi-goal upload.",
				},
				"subpath_track_id":               sp.TrackID,
				"subpath_track_title":            sp.Title,
				"subpath_track_goal":             sp.Goal,
				"subpath_source_path":            parentPathID.String(),
				"subpath_source_material_set_id": setID.String(),
			}

			// Also include a minimal intake object for transparency/debugging.
			childMeta["intake"] = map[string]any{
				"material_alignment": map[string]any{
					"mode":                             "single_goal",
					"primary_goal":                     sp.Goal,
					"include_file_ids":                 sp.Files,
					"exclude_file_ids":                 []string{},
					"maybe_separate_track_file_ids":    []string{},
					"noise_file_ids":                   []string{},
					"notes":                            "Subpath intake (derived)",
					"recommended_next_step":            "proceed",
					"recommended_next_step_reason":     "Derived from program split selection.",
					"recommended_next_step_confidence": 0.9,
				},
				"path_structure": map[string]any{
					"recommended_mode": "single_path",
					"selected_mode":    "single_path",
					"options": []map[string]any{
						{
							"option_id":          "single_path",
							"title":              "One combined path",
							"what_it_looks_like": "A single focused path for this track.",
							"pros":               []string{"Tightly focused", "Fast to complete"},
							"cons":               []string{},
							"recommended":        true,
						},
						{
							"option_id":          "program_with_subpaths",
							"title":              "A program with separate subpaths",
							"what_it_looks_like": "Already split at the parent program level.",
							"pros":               []string{},
							"cons":               []string{},
							"recommended":        false,
						},
					},
				},
				"primary_track_id": sp.TrackID,
				"tracks": []map[string]any{
					{
						"track_id":         sp.TrackID,
						"title":            sp.Title,
						"goal":             sp.Goal,
						"core_file_ids":    sp.Files,
						"support_file_ids": []string{},
						"confidence":       0.9,
						"notes":            "Derived track",
					},
				},
				"combined_goal":        sp.Goal,
				"audience_level_guess": strings.TrimSpace(stringFromAny(intake["audience_level_guess"])),
				"confidence":           0.9,
				"needs_clarification":  false,
				"clarifying_questions": []map[string]any{},
				"assumptions":          []string{"Subpath derived from a multi-goal upload."},
				"notes":                "Derived intake",
			}
			childMeta["intake_updated_at"] = now.Format(time.RFC3339Nano)

			child = &types.Path{
				ID:            uuid.New(),
				UserID:        parent.UserID,
				MaterialSetID: &msid,
				ParentPathID:  &pid,
				RootPathID:    &rid,
				Depth:         parentDepth + 1,
				SortIndex:     i,
				Kind:          "path",
				Title:         stringsOr(sp.Title, "Generating path…"),
				Description:   stringsOr(sp.Goal, ""),
				Status:        "draft",
				JobID:         nil,
				Metadata:      datatypes.JSON(mustJSON(childMeta)),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if _, err := p.path.Create(dbc, []*types.Path{child}); err != nil {
				jc.Fail("create_subpaths", err)
				return nil
			}
			existingByTrack[sp.TrackID] = child
		}

		// Ensure a (user, derived material_set)->path_id mapping exists for subpaths.
		// Only do this for true derived sets; the source material set is already mapped to the program path.
		if derivedSetID != uuid.Nil && derivedSetID != setID && p.uli != nil {
			_ = p.uli.UpsertPathID(dbc, jc.Job.OwnerUserID, derivedSetID, child.ID)
		}

		// Enqueue a learning_build for this subpath unless one is already runnable.
		has, _ := p.jobRuns.HasRunnableForEntity(dbc, jc.Job.OwnerUserID, "path", child.ID, "learning_build")
		var jobID uuid.UUID
		if !has {
			payload := map[string]any{
				"material_set_id": func() string {
					if derivedSetID != uuid.Nil {
						return derivedSetID.String()
					}
					return setID.String()
				}(),
				"path_id": child.ID.String(),
			}
			if threadID, ok := jc.PayloadUUID("thread_id"); ok && threadID != uuid.Nil {
				payload["thread_id"] = threadID.String()
			}
			entityID := child.ID
			j, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: p.db}, jc.Job.OwnerUserID, "learning_build", "path", &entityID, payload)
			if err != nil {
				jc.Fail("enqueue_subpaths", err)
				return nil
			}
			jobID = j.ID
			// Backlink job onto path for refresh-safe UI.
			_ = p.path.UpdateFields(dbc, child.ID, map[string]interface{}{"job_id": jobID})
		} else if child.JobID != nil && *child.JobID != uuid.Nil {
			jobID = *child.JobID
		}

		subpathsOut = append(subpathsOut, map[string]any{
			"path_id":  child.ID.String(),
			"track_id": sp.TrackID,
			"title":    sp.Title,
			"goal":     sp.Goal,
			"job_id": func() string {
				if jobID != uuid.Nil {
					return jobID.String()
				}
				return ""
			}(),
			"sort_index":  i,
			"parent_path": parentPathID.String(),
			"material_set": func() string {
				if derivedSetID != uuid.Nil {
					return derivedSetID.String()
				}
				return setID.String()
			}(),
			"source_material_set": setID.String(),
		})
	}

	// Store a compact snapshot on the parent path for UI/debugging (best-effort).
	parentMeta := meta
	if parentMeta == nil {
		parentMeta = map[string]any{}
	}
	parentMeta["subpaths"] = subpathsOut
	parentMeta["subpaths_updated_at"] = now.Format(time.RFC3339Nano)
	_ = p.path.UpdateFields(dbc, parentPathID, map[string]interface{}{"metadata": datatypes.JSON(mustJSON(parentMeta))})

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"path_id":         parentPathID.String(),
		"mode":            "program_with_subpaths",
		"subpaths":        subpathsOut,
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

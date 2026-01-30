package steps

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

// canonicalizePathConcepts ensures every provided path-scoped concept has a canonical (global) concept ID.
// Canonical concepts live in the same table with scope="global" and scope_id=NULL.
//
// This is a production-oriented primitive:
// - It is safe to call repeatedly (idempotent).
// - It is safe under concurrency (ON CONFLICT DO NOTHING on canonical inserts).
// - It keeps path concept IDs stable while enabling cross-path mastery transfer via canonical IDs.
//
// semanticMatchByKey (optional): normalized concept_key -> canonical global concept UUID.
// When provided and the global concept key does not already exist, we will create a global alias concept row
// (scope="global", key=<concept_key>, canonical_concept_id=<matched_id>) so future canonicalization is O(1).
func canonicalizePathConcepts(
	dbc dbctx.Context,
	db *gorm.DB,
	conceptRepo repos.ConceptRepo,
	repRepo repos.ConceptRepresentationRepo,
	overrideRepo repos.ConceptMappingOverrideRepo,
	pathConcepts []*types.Concept,
	semanticMatchByKey map[string]canonicalMatch,
) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	if conceptRepo == nil || db == nil || dbc.Ctx == nil || len(pathConcepts) == 0 {
		return out, nil
	}

	const (
		semanticStrongMin = 0.85
		semanticSoftMin   = 0.70
	)

	type mappingInfo struct {
		Method     string
		Confidence float64
	}
	mappingByKey := map[string]mappingInfo{}

	overrideByConceptID := map[uuid.UUID]uuid.UUID{}
	if overrideRepo != nil {
		ids := make([]uuid.UUID, 0, len(pathConcepts))
		for _, c := range pathConcepts {
			if c != nil && c.ID != uuid.Nil {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			if rows, err := overrideRepo.ListByPathConceptIDs(dbc, ids); err == nil {
				for _, r := range rows {
					if r == nil || r.PathConceptID == uuid.Nil || r.CanonicalConceptID == uuid.Nil {
						continue
					}
					overrideByConceptID[r.PathConceptID] = r.CanonicalConceptID
				}
			}
		}
	}

	// Collect unique keys and best-effort metadata for canonical concept creation.
	type info struct {
		Name    string
		Summary string
		KeyPts  datatypes.JSON
		Aliases []string
	}
	keys := make([]string, 0, len(pathConcepts))
	infoByKey := map[string]info{}
	seen := map[string]bool{}
	for _, c := range pathConcepts {
		if c == nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(c.Key))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
		aliases := []string{}
		// Prefer explicit aliases from concept.metadata (path concepts persist these from concept_graph_build).
		if len(c.Metadata) > 0 && strings.TrimSpace(string(c.Metadata)) != "" && strings.TrimSpace(string(c.Metadata)) != "null" {
			var meta map[string]any
			if json.Unmarshal(c.Metadata, &meta) == nil && meta != nil {
				aliases = dedupeStrings(stringSliceFromAny(meta["aliases"]))
			}
		}
		infoByKey[k] = info{
			Name:    strings.TrimSpace(c.Name),
			Summary: strings.TrimSpace(c.Summary),
			KeyPts:  c.KeyPoints,
			Aliases: aliases,
		}
	}
	if len(keys) == 0 {
		return out, nil
	}

	// Load existing global concepts for these keys.
	existing, err := conceptRepo.GetByScopeAndKeys(dbc, "global", nil, keys)
	if err != nil {
		return nil, err
	}
	globalByKey := map[string]*types.Concept{}
	for _, g := range existing {
		if g == nil || g.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(g.Key))
		if k != "" {
			globalByKey[k] = g
			// If this global concept is an alias/redirect, treat the canonical ID as the root.
			if g.CanonicalConceptID != nil && *g.CanonicalConceptID != uuid.Nil {
				out[k] = *g.CanonicalConceptID
			} else {
				out[k] = g.ID
			}
			mappingByKey[k] = mappingInfo{Method: "exact_key", Confidence: 1.0}
		}
	}

	// Create missing global concepts (race-safe). These may be:
	// - canonical (canonical_concept_id NULL), or
	// - alias/redirect (canonical_concept_id = root_id) when semanticMatchByKey provides a match.
	now := time.Now().UTC()
	toCreate := make([]*types.Concept, 0)
	for _, k := range keys {
		if k == "" || out[k] != uuid.Nil {
			continue
		}
		meta := infoByKey[k]
		name := strings.TrimSpace(meta.Name)
		if name == "" {
			name = k
		}
		rootID := uuid.Nil
		method := ""
		conf := 0.0
		if semanticMatchByKey != nil {
			if match, ok := semanticMatchByKey[k]; ok && match.ID != uuid.Nil {
				switch strings.ToLower(strings.TrimSpace(match.Method)) {
				case "alias":
					rootID = match.ID
					method = "alias"
					conf = match.Score
					if conf == 0 {
						conf = 0.9
					}
				case "semantic":
					if match.Score >= semanticSoftMin {
						rootID = match.ID
						method = "semantic"
						conf = match.Score
					}
				}
			}
		}
		row := &types.Concept{
			ID:        uuid.New(),
			Scope:     "global",
			ScopeID:   nil,
			Depth:     0,
			SortIndex: 0,
			Key:       k,
			Name:      name,
			Summary:   strings.TrimSpace(meta.Summary),
			KeyPoints: func() datatypes.JSON {
				// Keep key_points on canonical concepts; on alias concepts it's still useful as a fallback description.
				if len(meta.KeyPts) > 0 && strings.TrimSpace(string(meta.KeyPts)) != "" && strings.TrimSpace(string(meta.KeyPts)) != "null" {
					return meta.KeyPts
				}
				return datatypes.JSON([]byte(`[]`))
			}(),
			VectorID: "",
			Metadata: datatypes.JSON(mustJSON(map[string]any{
				"source":  "canonicalize",
				"aliases": meta.Aliases,
			})),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if rootID != uuid.Nil {
			// Alias/redirect row: this key resolves to an existing canonical concept.
			row.CanonicalConceptID = &rootID
			row.Metadata = datatypes.JSON(mustJSON(map[string]any{
				"source":    "canonicalize",
				"alias_for": rootID.String(),
				"aliases":   meta.Aliases,
			}))
			if method != "" {
				mappingByKey[k] = mappingInfo{Method: method, Confidence: conf}
			}
		}
		// VectorID is a best-effort cache key; keep it stable for this canonical row.
		row.VectorID = "concept:" + row.ID.String()
		toCreate = append(toCreate, row)
		// Optimistically record; if a conflict happens, we'll refresh below.
		if rootID != uuid.Nil {
			out[k] = rootID
		} else {
			out[k] = row.ID
			if _, ok := mappingByKey[k]; !ok {
				mappingByKey[k] = mappingInfo{Method: "created_global", Confidence: 1.0}
			}
		}
	}

	if len(toCreate) > 0 {
		// Prefer a DB-level "do nothing" upsert to avoid failing the whole batch on concurrent inserts.
		tx := dbc.Tx
		if tx == nil {
			tx = db
		}
		if tx == nil {
			return nil, fmt.Errorf("canonicalizePathConcepts: missing db")
		}
		// Use a target-less ON CONFLICT so this is safe even on installs that haven't created the
		// partial unique indexes yet (older DBs). When the indexes exist, conflicts are ignored.
		if err := tx.WithContext(dbc.Ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&toCreate).Error; err != nil {
			return nil, err
		}

		// Refresh canonical IDs to resolve any conflicts (stable truth).
		existing, err := conceptRepo.GetByScopeAndKeys(dbc, "global", nil, keys)
		if err != nil {
			return nil, err
		}
		globalByKey = map[string]*types.Concept{}
		for _, g := range existing {
			if g == nil || g.ID == uuid.Nil {
				continue
			}
			k := strings.TrimSpace(strings.ToLower(g.Key))
			if k != "" {
				globalByKey[k] = g
				if g.CanonicalConceptID != nil && *g.CanonicalConceptID != uuid.Nil {
					out[k] = *g.CanonicalConceptID
				} else {
					out[k] = g.ID
				}
				if _, ok := mappingByKey[k]; !ok {
					mappingByKey[k] = mappingInfo{Method: "exact_key", Confidence: 1.0}
				}
			}
		}

		// If a semantic match was requested for a key but a concurrent insert created a canonical row first,
		// enforce the alias redirect by setting canonical_concept_id (best-effort).
		if semanticMatchByKey != nil && len(semanticMatchByKey) > 0 {
			for k, match := range semanticMatchByKey {
				if match.ID == uuid.Nil {
					continue
				}
				row := globalByKey[k]
				if row == nil || row.ID == uuid.Nil {
					continue
				}
				// If already redirected, respect existing mapping.
				if row.CanonicalConceptID != nil && *row.CanonicalConceptID != uuid.Nil {
					continue
				}
				if err := conceptRepo.UpdateFields(dbc, row.ID, map[string]interface{}{"canonical_concept_id": match.ID}); err != nil {
					return nil, err
				}
				out[k] = match.ID
				if _, ok := mappingByKey[k]; !ok {
					mappingByKey[k] = mappingInfo{Method: "semantic", Confidence: match.Score}
				}
			}
		}
	}

	// Backfill canonical_concept_id for path concepts (and repair if it points at an alias/redirect).
	for _, c := range pathConcepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(c.Key))
		cid := out[k]
		if override := resolveCanonicalOverride(c.ID, overrideByConceptID); override != uuid.Nil {
			cid = override
		}
		if cid == uuid.Nil {
			continue
		}
		needsUpdate := c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil || *c.CanonicalConceptID != cid
		if needsUpdate {
			if err := conceptRepo.UpdateFields(dbc, c.ID, map[string]interface{}{"canonical_concept_id": cid}); err != nil {
				return nil, err
			}
			t := cid
			c.CanonicalConceptID = &t
		}
		if repRepo != nil {
			info := mappingByKey[k]
			method := info.Method
			conf := info.Confidence
			if override := resolveCanonicalOverride(c.ID, overrideByConceptID); override != uuid.Nil {
				method = "override"
				conf = 1.0
			}
			if method == "" {
				method = "exact_key"
				conf = 1.0
			}
			_ = repRepo.Upsert(dbc, &types.ConceptRepresentation{
				PathConceptID:        c.ID,
				CanonicalConceptID:   cid,
				PathID:               c.ScopeID,
				RepresentationAliases: datatypes.JSON(mustJSON(infoByKey[k].Aliases)),
				MappingConfidence:    conf,
				MappingMethod:        method,
			})
		}
	}

	return out, nil
}

func resolveCanonicalOverride(pathConceptID uuid.UUID, overrides map[uuid.UUID]uuid.UUID) uuid.UUID {
	if pathConceptID == uuid.Nil || overrides == nil {
		return uuid.Nil
	}
	return overrides[pathConceptID]
}

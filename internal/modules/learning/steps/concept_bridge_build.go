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
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ConceptBridgeBuildDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Concepts  repos.ConceptRepo
	Edges     repos.ConceptEdgeRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Bootstrap services.LearningBuildBootstrapService
}

type ConceptBridgeBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type ConceptBridgeBuildOutput struct {
	PathID      uuid.UUID `json:"path_id"`
	EdgesMade   int       `json:"edges_made"`
	MatchesSeen int       `json:"matches_seen"`
}

// ConceptBridgeBuild creates cross-domain "bridge" edges between canonical concepts by
// matching path concepts against the global concept index.
func ConceptBridgeBuild(ctx context.Context, deps ConceptBridgeBuildDeps, in ConceptBridgeBuildInput) (ConceptBridgeBuildOutput, error) {
	out := ConceptBridgeBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Concepts == nil || deps.Edges == nil || deps.AI == nil || deps.Vec == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("concept_bridge_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("concept_bridge_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("concept_bridge_build: missing material_set_id")
	}
	if !envBool("CONCEPT_BRIDGE_BUILD_ENABLED", true) {
		return out, nil
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	minScore := envFloatAllowZero("CONCEPT_BRIDGE_MIN_SCORE", 0.86)
	if minScore <= 0 {
		minScore = 0.86
	}
	topK := envIntAllowZero("CONCEPT_BRIDGE_TOPK", 6)
	if topK < 1 {
		topK = 6
	}
	maxPerConcept := envIntAllowZero("CONCEPT_BRIDGE_MAX_PER_CONCEPT", 3)
	if maxPerConcept < 1 {
		maxPerConcept = 3
	}
	maxEdges := envIntAllowZero("CONCEPT_BRIDGE_MAX_EDGES", 200)
	if maxEdges < 1 {
		maxEdges = 200
	}
	skipInPath := envBool("CONCEPT_BRIDGE_SKIP_IN_PATH", true)
	bidirectional := envBool("CONCEPT_BRIDGE_BIDIRECTIONAL", true)

	dbc := dbctx.Context{Ctx: ctx}
	pathConcepts, err := deps.Concepts.GetByScope(dbc, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(pathConcepts) == 0 {
		return out, nil
	}

	// Canonical IDs present in this path (to avoid self-bridging by default).
	pathCanonical := map[uuid.UUID]bool{}
	canonicalIDs := make([]uuid.UUID, 0, len(pathConcepts))
	for _, c := range pathConcepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		cid := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			cid = *c.CanonicalConceptID
		}
		if cid == uuid.Nil {
			continue
		}
		pathCanonical[cid] = true
		canonicalIDs = append(canonicalIDs, cid)
	}
	canonicalIDs = dedupeUUIDs(canonicalIDs)
	if len(canonicalIDs) == 0 {
		return out, nil
	}

	// Load canonical concept rows (global scope).
	canonicalRows, err := deps.Concepts.GetByIDs(dbc, canonicalIDs)
	if err != nil {
		return out, err
	}
	rowByID := map[uuid.UUID]*types.Concept{}
	for _, c := range canonicalRows {
		if c != nil && c.ID != uuid.Nil {
			rowByID[c.ID] = c
		}
	}

	// Build embedding docs.
	docIDs := make([]uuid.UUID, 0, len(canonicalIDs))
	docs := make([]string, 0, len(canonicalIDs))
	for _, cid := range canonicalIDs {
		row := rowByID[cid]
		if row == nil {
			continue
		}
		kps := jsonListFromRaw(row.KeyPoints)
		doc := strings.TrimSpace(row.Name + "\n" + row.Summary + "\n" + strings.Join(kps, "\n"))
		if doc == "" {
			doc = strings.TrimSpace(row.Key)
		}
		if doc == "" {
			continue
		}
		docIDs = append(docIDs, cid)
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return out, nil
	}

	// Embed in batches to control cost/time.
	embedBatch := envIntAllowZero("CONCEPT_BRIDGE_EMBED_BATCH_SIZE", 64)
	if embedBatch < 1 {
		embedBatch = 64
	}
	embs := make([][]float32, 0, len(docs))
	for i := 0; i < len(docs); i += embedBatch {
		end := i + embedBatch
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]
		batchEmbs, err := deps.AI.Embed(ctx, batch)
		if err != nil {
			return out, err
		}
		embs = append(embs, batchEmbs...)
	}
	if len(embs) != len(docs) {
		return out, fmt.Errorf("concept_bridge_build: embedding count mismatch (got %d want %d)", len(embs), len(docs))
	}

	globalNS := index.ConceptsNamespace("global", nil)
	filter := map[string]any{
		"type":      "concept",
		"scope":     "global",
		"canonical": true,
	}

	edgesMade := 0
	matchesSeen := 0
	perConcept := map[uuid.UUID]int{}
	for i, emb := range embs {
		if len(emb) == 0 {
			continue
		}
		srcID := docIDs[i]
		if srcID == uuid.Nil {
			continue
		}
		matches, err := deps.Vec.QueryMatches(ctx, globalNS, emb, topK, filter)
		if err != nil {
			return out, err
		}
		if len(matches) == 0 {
			continue
		}
		matchesSeen += len(matches)

		for rank, m := range matches {
			if edgesMade >= maxEdges {
				break
			}
			if m.Score < minScore {
				continue
			}
			matchID := strings.TrimSpace(m.ID)
			if !strings.HasPrefix(matchID, "concept:") {
				continue
			}
			raw := strings.TrimPrefix(matchID, "concept:")
			targetID, err := uuid.Parse(raw)
			if err != nil || targetID == uuid.Nil || targetID == srcID {
				continue
			}
			if skipInPath && pathCanonical[targetID] {
				continue
			}
			if perConcept[srcID] >= maxPerConcept {
				break
			}

			ev, _ := json.Marshal(map[string]any{
				"source":             "concept_bridge_build",
				"score":              m.Score,
				"rank":               rank + 1,
				"matched_concept_id": targetID.String(),
				"scope":              "global",
				"observed_at":         time.Now().UTC().Format(time.RFC3339Nano),
			})

			row := &types.ConceptEdge{
				FromConceptID: srcID,
				ToConceptID:   targetID,
				EdgeType:      "bridge",
				Strength:      m.Score,
				Evidence:      datatypes.JSON(ev),
			}
			if err := deps.Edges.Upsert(dbc, row); err == nil {
				edgesMade++
				perConcept[srcID]++
			}

			if bidirectional {
				row2 := &types.ConceptEdge{
					FromConceptID: targetID,
					ToConceptID:   srcID,
					EdgeType:      "bridge",
					Strength:      m.Score,
					Evidence:      datatypes.JSON(ev),
				}
				if err := deps.Edges.Upsert(dbc, row2); err == nil {
					edgesMade++
				}
			}
		}
	}

	out.EdgesMade = edgesMade
	out.MatchesSeen = matchesSeen
	return out, nil
}

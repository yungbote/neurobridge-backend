package steps

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type topicAnchorDef struct {
	Key         string
	Name        string
	Description string
}

// Seeded, ultra-stable top-level anchors for the "topic" facet.
// Mid/low-level taxonomy nodes must be earned bottom-up via refinement.
var topicAnchors = []topicAnchorDef{
	{Key: "anchor_physics", Name: "Physics", Description: "Mechanics, electromagnetism, thermodynamics, quantum, and related core physics topics."},
	{Key: "anchor_biology", Name: "Biology", Description: "Cells, genetics, physiology, immunology, evolution, and broader life sciences."},
	{Key: "anchor_chemistry", Name: "Chemistry", Description: "General, organic, inorganic, physical chemistry, biochemistry, and chemical principles."},
	{Key: "anchor_mathematics", Name: "Mathematics", Description: "Algebra, calculus, linear algebra, probability, statistics, discrete math, and proofs."},
	{Key: "anchor_computer_science", Name: "Computer Science", Description: "Programming, algorithms, systems, data, ML/AI, and software engineering foundations."},
	{Key: "anchor_medicine_health", Name: "Medicine & Health", Description: "Clinical medicine, pathology, pharmacology, public health, and human health applications."},
	{Key: "anchor_psychology_neuroscience", Name: "Psychology & Neuroscience", Description: "Cognition, behavior, brain systems, learning, perception, and mental health."},
	{Key: "anchor_economics_business", Name: "Economics & Business", Description: "Micro/macro economics, finance, markets, strategy, and organizational topics."},
	{Key: "anchor_history", Name: "History", Description: "Historical periods, events, societies, and historiography."},
	{Key: "anchor_philosophy", Name: "Philosophy", Description: "Ethics, epistemology, metaphysics, logic, and philosophical traditions."},
}

func topicAnchorKeys() []string {
	out := make([]string, 0, len(topicAnchors))
	for _, a := range topicAnchors {
		if k := strings.TrimSpace(a.Key); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func ensureTopicAnchors(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID, facet string, root *types.LibraryTaxonomyNode) ([]*types.LibraryTaxonomyNode, error) {
	facet = normalizeFacet(facet)
	if facet != "topic" {
		return nil, nil
	}
	if deps.TaxNodes == nil || deps.TaxEdges == nil {
		return nil, nil
	}
	if userID == uuid.Nil || root == nil || root.ID == uuid.Nil {
		return nil, nil
	}

	keys := topicAnchorKeys()
	if len(keys) == 0 {
		return nil, nil
	}

	existing, err := deps.TaxNodes.GetByUserFacetKeys(dbctx.Context{Ctx: ctx}, userID, facet, keys)
	if err != nil {
		return nil, err
	}
	existingByKey := map[string]*types.LibraryTaxonomyNode{}
	for _, n := range existing {
		if n == nil {
			continue
		}
		existingByKey[strings.TrimSpace(n.Key)] = n
	}

	now := time.Now().UTC()
	toUpsert := make([]*types.LibraryTaxonomyNode, 0, len(topicAnchors))
	for _, def := range topicAnchors {
		k := strings.TrimSpace(def.Key)
		if k == "" {
			continue
		}
		if existingByKey[k] != nil {
			continue
		}
		toUpsert = append(toUpsert, &types.LibraryTaxonomyNode{
			ID:          uuid.New(),
			UserID:      userID,
			Facet:       facet,
			Key:         k,
			Kind:        "anchor",
			Name:        strings.TrimSpace(def.Name),
			Description: strings.TrimSpace(def.Description),
			Embedding:   datatypes.JSON([]byte(`[]`)),
			Stats:       datatypes.JSON(toJSON(map[string]any{"seeded": true})),
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if len(toUpsert) > 0 {
		if err := deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, toUpsert); err != nil {
			return nil, err
		}
	}

	// Reload in case some keys existed but were soft-deleted or IDs differ from insert attempts.
	rows, err := deps.TaxNodes.GetByUserFacetKeys(dbctx.Context{Ctx: ctx}, userID, facet, keys)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*types.LibraryTaxonomyNode{}
	for _, r := range rows {
		if r == nil || r.ID == uuid.Nil {
			continue
		}
		byKey[strings.TrimSpace(r.Key)] = r
	}

	anchors := make([]*types.LibraryTaxonomyNode, 0, len(topicAnchors))
	for _, def := range topicAnchors {
		if n := byKey[strings.TrimSpace(def.Key)]; n != nil {
			anchors = append(anchors, n)
		}
	}

	// Ensure root -> anchor "subsumes" edges exist so anchors form stable navigation parents.
	edges := make([]*types.LibraryTaxonomyEdge, 0, len(anchors))
	for _, a := range anchors {
		if a == nil || a.ID == uuid.Nil {
			continue
		}
		edges = append(edges, &types.LibraryTaxonomyEdge{
			ID:         uuid.New(),
			UserID:     userID,
			Facet:      facet,
			Kind:       "subsumes",
			FromNodeID: root.ID,
			ToNodeID:   a.ID,
			Weight:     1,
			Metadata:   datatypes.JSON(toJSON(map[string]any{"seeded": true})),
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if len(edges) > 0 {
		_ = deps.TaxEdges.UpsertMany(dbctx.Context{Ctx: ctx}, edges)
	}

	return anchors, nil
}


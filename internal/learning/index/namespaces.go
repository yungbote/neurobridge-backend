package index

import (
	"fmt"
	"github.com/google/uuid"
)

// Population-level library of reusable representations (not just patterns)
// ex: best-performing activity variants for concept chains
func PopulationLibraryNamespace() string {
	return "population_library:global"
}

// Per-user library of what the user has already seen/completed (for similarity + novelty)
// you can either store embeddings here or store documents that you embed on write.
func UserLibraryNamespace(userID uuid.UUID) string {
	return fmt.Sprintf("user_library:user:%s", userID.String())
}

// User profile vectors / preference docs (if you store them in Pinecone)
func UserProfileNamespace() string {
	return "user_profiles:global"
}

// Chunks are per material set
func ChunksNamespace(materialSetID uuid.UUID) string {
	return fmt.Sprintf("chunks:material_set:%s", materialSetID.String())
}

func ConceptsNamespace(scope string, scopeID *uuid.UUID) string {
	if scope == "global" || scopeID == nil || *scopeID == uuid.Nil {
		return "concepts:global"
	}
	return fmt.Sprintf("concepts:%s:%s", scope, scopeID.String())
}

func ConceptClustersNamespace(scope string, scopeID *uuid.UUID) string {
	if scope == "global" || scopeID == nil || *scopeID == uuid.Nil {
		return "concept_clusters:global"
	}
	return fmt.Sprintf("concept_clusters:%s:%s", scope, scopeID.String())
}

func ChainsNamespace(scope string, scopeID *uuid.UUID) string {
	if scope == "global" || scopeID == nil || *scopeID == uuid.Nil {
		return "chains:global"
	}
	return fmt.Sprintf("chains:%s:%s", scope, scopeID.String())
}

func ActivitiesNamespace(scope string, scopeID *uuid.UUID) string {
	if scope == "global" || scopeID == nil || *scopeID == uuid.Nil {
		return "activities:global"
	}
	return fmt.Sprintf("activities:%s:%s", scope, scopeID.String())
}

func MaterialSetSummariesNamespace(userID uuid.UUID) string {
	return fmt.Sprintf("material_set_summaries:user:%s", userID.String())
}

// cohort / teaching patters / anything population-level
func CohortPriorsNamespace() string {
	return "cohort_priors:global"
}

func TeachingPatternsNamespace() string {
	return "teaching_patterns:global"
}

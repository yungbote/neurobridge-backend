package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Library taxonomy nodes (per user, per facet). These form a typed DAG via LibraryTaxonomyEdge.
//
// Facet examples: "topic", "skill", "context".
// Kind examples:  "root", "inbox", "category".
type LibraryTaxonomyNode struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_node_user_facet,priority:1;index:idx_library_taxonomy_node_user_facet_key,unique,priority:1" json:"user_id"`
	Facet  string    `gorm:"column:facet;not null;index:idx_library_taxonomy_node_user_facet,priority:2;index:idx_library_taxonomy_node_user_facet_key,unique,priority:2" json:"facet"`

	// Key is a stable per-(user,facet) identifier (e.g., "root", "inbox", "cat_<uuid>").
	Key string `gorm:"column:key;not null;index:idx_library_taxonomy_node_user_facet_key,unique,priority:3" json:"key"`

	Kind        string `gorm:"column:kind;not null;default:'category';index" json:"kind"`
	Name        string `gorm:"column:name;not null" json:"name"`
	Description string `gorm:"column:description;type:text" json:"description"`

	// Centroid embedding (float array JSON) used for similarity routing/refinement.
	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding,omitempty"`

	// Stats is freeform JSON used for observability/stability guards (counts, cohesion, etc.).
	Stats datatypes.JSON `gorm:"column:stats;type:jsonb" json:"stats,omitempty"`

	// Version increments when the node meaning/structure changes materially.
	Version int `gorm:"column:version;not null;default:1" json:"version"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryTaxonomyNode) TableName() string { return "library_taxonomy_node" }

// Library taxonomy edges between nodes (per user, per facet).
// Kind:
// - "subsumes": parent -> child abstraction edge (must remain acyclic within a facet).
// - "related": weighted semantic cross-link (can be cyclic; not used for strict containment).
type LibraryTaxonomyEdge struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_edge_user_facet,priority:1" json:"user_id"`
	Facet  string    `gorm:"column:facet;not null;index:idx_library_taxonomy_edge_user_facet,priority:2" json:"facet"`
	Kind   string    `gorm:"column:kind;not null;index:idx_library_taxonomy_edge_user_facet_kind,priority:3;index:idx_library_taxonomy_edge_unique,unique,priority:1" json:"kind"`

	FromNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_edge_unique,unique,priority:2" json:"from_node_id"`
	ToNodeID   uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_edge_unique,unique,priority:3" json:"to_node_id"`

	// Weight is a normalized strength in [0,1].
	Weight float64 `gorm:"column:weight;not null;default:1" json:"weight"`

	// Metadata can store evidence, provenance, or LLM rationale.
	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	Version int `gorm:"column:version;not null;default:1" json:"version"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryTaxonomyEdge) TableName() string { return "library_taxonomy_edge" }

// Path-to-taxonomy memberships (per user, per facet). Paths can belong to many nodes.
type LibraryTaxonomyMembership struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_membership_user_facet,priority:1" json:"user_id"`
	Facet  string    `gorm:"column:facet;not null;index:idx_library_taxonomy_membership_user_facet,priority:2" json:"facet"`

	PathID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_membership_unique,unique,priority:1" json:"path_id"`
	NodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_membership_unique,unique,priority:2" json:"node_id"`

	// Weight is a normalized strength in [0,1].
	Weight float64 `gorm:"column:weight;not null;default:1" json:"weight"`

	// AssignedBy indicates provenance: "route" | "refine" | "bootstrap".
	AssignedBy string `gorm:"column:assigned_by;not null;default:'route'" json:"assigned_by"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	Version int `gorm:"column:version;not null;default:1" json:"version"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryTaxonomyMembership) TableName() string { return "library_taxonomy_membership" }

// Per-user taxonomy state (coalescing, thresholds, scheduling).
type LibraryTaxonomyState struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_state_user,unique" json:"user_id"`

	Version int `gorm:"column:version;not null;default:1" json:"version"`

	Dirty                bool       `gorm:"column:dirty;not null;default:false;index" json:"dirty"`
	NewPathsSinceRefine  int        `gorm:"column:new_paths_since_refine;not null;default:0" json:"new_paths_since_refine"`
	LastRoutedAt         *time.Time `gorm:"column:last_routed_at" json:"last_routed_at,omitempty"`
	LastRefinedAt        *time.Time `gorm:"column:last_refined_at" json:"last_refined_at,omitempty"`
	LastSnapshotBuiltAt  *time.Time `gorm:"column:last_snapshot_built_at" json:"last_snapshot_built_at,omitempty"`
	RefineLockUntil      *time.Time `gorm:"column:refine_lock_until;index" json:"refine_lock_until,omitempty"`
	PendingUnsortedPaths int        `gorm:"column:pending_unsorted_paths;not null;default:0" json:"pending_unsorted_paths"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryTaxonomyState) TableName() string { return "library_taxonomy_state" }

// Materialized snapshot for fast homepage rendering.
type LibraryTaxonomySnapshot struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_taxonomy_snapshot_user,unique" json:"user_id"`

	// Monotonically increasing snapshot version.
	Version int `gorm:"column:version;not null;default:1" json:"version"`

	SnapshotJSON datatypes.JSON `gorm:"column:snapshot_json;type:jsonb" json:"snapshot_json"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryTaxonomySnapshot) TableName() string { return "library_taxonomy_snapshot" }

// Cached path embedding used for routing/clustering. Kept separate from taxonomy so it can be reused
// across facets.
type LibraryPathEmbedding struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_path_embedding_user,priority:1" json:"user_id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index:idx_library_path_embedding_user,unique,priority:2" json:"path_id"`

	// Name of embed model used (for cache invalidation).
	Model string `gorm:"column:model;not null" json:"model"`

	// Embedding vector as JSON float array.
	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"`

	// SourcesHash changes when upstream sources used to compute this embedding change.
	SourcesHash string `gorm:"column:sources_hash;not null;index" json:"sources_hash"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LibraryPathEmbedding) TableName() string { return "library_path_embedding" }

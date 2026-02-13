package products

import (
	"time"

	"gorm.io/datatypes"
)

// GraphVersion tracks structural graph versioning and the component versions that produced it.
type GraphVersion struct {
	GraphVersion string `gorm:"column:graph_version;type:text;primaryKey" json:"graph_version"`

	Status    string `gorm:"column:status;type:text;not null;default:'draft';index" json:"status"`
	SourceJob string `gorm:"column:source_job;type:text;index" json:"source_job,omitempty"`

	EmbeddingVersion   string `gorm:"column:embedding_version;type:text;index" json:"embedding_version,omitempty"`
	TaxonomyVersion    string `gorm:"column:taxonomy_version;type:text;index" json:"taxonomy_version,omitempty"`
	ClusteringVersion  string `gorm:"column:clustering_version;type:text;index" json:"clustering_version,omitempty"`
	CalibrationVersion string `gorm:"column:calibration_version;type:text;index" json:"calibration_version,omitempty"`

	SnapshotURI string         `gorm:"column:snapshot_uri;type:text" json:"snapshot_uri,omitempty"`
	Metadata    datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (GraphVersion) TableName() string { return "graph_version" }

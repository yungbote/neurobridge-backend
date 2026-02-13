package structuraltrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/validation"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type GraphVersionWriter interface {
	Create(dbc dbctx.Context, row *types.GraphVersion) error
}

type StructuralTraceWriter interface {
	Create(dbc dbctx.Context, row *types.StructuralDecisionTrace) error
}

type Deps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	GraphVersion GraphVersionWriter
	TraceWriter  StructuralTraceWriter
}

type VersionTags struct {
	Graph       string
	Embedding   string
	Taxonomy    string
	Clustering  string
	Calibration string
}

type TraceInput struct {
	DecisionType      string
	DecisionPhase     string
	DecisionMode      string
	OccurredAt        time.Time
	UserID            *uuid.UUID
	PathID            *uuid.UUID
	MaterialSetID     *uuid.UUID
	SagaID            *uuid.UUID
	GraphVersion      string
	VersionTags       VersionTags
	Inputs            any
	Candidates        any
	Chosen            any
	Thresholds        any
	Metadata          map[string]any
	GraphMetadata     map[string]any
	Payload           map[string]any
	Validate          bool
	RequireTrace      bool
	WriteGraphVersion bool
}

type TraceResult struct {
	GraphVersion     string
	ValidationStatus string
	InvariantReport  validation.InvariantReport
}

func Record(ctx context.Context, deps Deps, input TraceInput) (TraceResult, error) {
	res := TraceResult{}
	if deps.DB == nil {
		return res, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if input.OccurredAt.IsZero() {
		input.OccurredAt = now
	}

	tags := resolveTags(input.VersionTags)
	graphVersion := strings.TrimSpace(input.GraphVersion)
	if graphVersion == "" {
		graphVersion = strings.TrimSpace(payloadString(input.Payload, "graph_version"))
	}
	if graphVersion == "" {
		graphVersion = tags.Graph
	}
	if graphVersion == "" {
		graphVersion = uuid.New().String()
	}
	res.GraphVersion = graphVersion
	if tags.Graph == "" {
		tags.Graph = graphVersion
	}

	report := validation.InvariantReport{Status: "skipped", CheckedAt: input.OccurredAt}
	if input.Validate && input.PathID != nil && *input.PathID != uuid.Nil {
		start := time.Now()
		report = validation.ValidateStructuralInvariants(ctx, deps.DB, *input.PathID)
		if metrics := observability.Current(); metrics != nil {
			metrics.ObserveStructuralValidation(time.Since(start), report.Status)
		}
	}
	if report.Status == "" {
		report.Status = "unknown"
	}
	res.ValidationStatus = report.Status
	res.InvariantReport = report

	if input.WriteGraphVersion {
		if err := writeGraphVersion(ctx, deps, input, tags, graphVersion, now); err != nil {
			if input.RequireTrace {
				return res, err
			}
			if deps.Log != nil {
				deps.Log.Debug("graph_version create failed", "error", err.Error())
			}
		}
	}

	if metrics := observability.Current(); metrics != nil {
		metrics.IncTraceAttempted("structural")
	}
	if err := writeStructuralTrace(ctx, deps, input, tags, graphVersion, report); err != nil {
		if metrics := observability.Current(); metrics != nil {
			metrics.IncTraceFailed("structural")
		}
		if input.RequireTrace {
			return res, err
		}
		if deps.Log != nil {
			deps.Log.Debug("structural decision trace create failed", "error", err.Error())
		}
	} else {
		if metrics := observability.Current(); metrics != nil {
			metrics.IncTraceWritten("structural")
		}
	}

	if report.Status == "fail" || report.Status == "error" {
		return res, fmt.Errorf("structural invariants failed")
	}
	return res, nil
}

func resolveTags(tags VersionTags) VersionTags {
	if tags.Graph == "" {
		tags.Graph = strings.TrimSpace(os.Getenv("GRAPH_VERSION"))
	}
	if tags.Embedding == "" {
		tags.Embedding = strings.TrimSpace(os.Getenv("EMBEDDING_VERSION"))
	}
	if tags.Taxonomy == "" {
		tags.Taxonomy = strings.TrimSpace(os.Getenv("TAXONOMY_VERSION"))
	}
	if tags.Clustering == "" {
		tags.Clustering = strings.TrimSpace(os.Getenv("CLUSTERING_VERSION"))
	}
	if tags.Calibration == "" {
		tags.Calibration = strings.TrimSpace(os.Getenv("CALIBRATION_VERSION"))
	}
	return tags
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil || key == "" {
		return ""
	}
	val, ok := payload[key]
	if !ok || val == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(val))
}

func writeGraphVersion(ctx context.Context, deps Deps, input TraceInput, tags VersionTags, graphVersion string, now time.Time) error {
	metadata := input.GraphMetadata
	if metadata == nil {
		metadata = input.Metadata
	}
	gv := &types.GraphVersion{
		GraphVersion:       graphVersion,
		Status:             "draft",
		SourceJob:          input.DecisionType,
		EmbeddingVersion:   tags.Embedding,
		TaxonomyVersion:    tags.Taxonomy,
		ClusteringVersion:  tags.Clustering,
		CalibrationVersion: tags.Calibration,
		Metadata:           datatypes.JSON(mustJSON(metadata)),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if deps.GraphVersion != nil {
		if err := deps.GraphVersion.Create(dbctx.Context{Ctx: ctx}, gv); err != nil {
			if isDuplicateKey(err) {
				return nil
			}
			return err
		}
		return nil
	}
	if err := deps.DB.WithContext(ctx).Create(gv).Error; err != nil {
		if isDuplicateKey(err) {
			return nil
		}
		return err
	}
	return nil
}

func writeStructuralTrace(ctx context.Context, deps Deps, input TraceInput, tags VersionTags, graphVersion string, report validation.InvariantReport) error {
	trace := &types.StructuralDecisionTrace{
		DecisionType:       input.DecisionType,
		DecisionPhase:      strings.TrimSpace(input.DecisionPhase),
		DecisionMode:       strings.TrimSpace(input.DecisionMode),
		OccurredAt:         input.OccurredAt,
		UserID:             input.UserID,
		PathID:             input.PathID,
		MaterialSetID:      input.MaterialSetID,
		SagaID:             input.SagaID,
		GraphVersion:       graphVersion,
		EmbeddingVersion:   tags.Embedding,
		TaxonomyVersion:    tags.Taxonomy,
		ClusteringVersion:  tags.Clustering,
		CalibrationVersion: tags.Calibration,
		Inputs:             datatypes.JSON(mustJSON(input.Inputs)),
		Candidates:         datatypes.JSON(mustJSON(input.Candidates)),
		Chosen:             datatypes.JSON(mustJSON(input.Chosen)),
		Thresholds:         datatypes.JSON(mustJSON(input.Thresholds)),
		Invariants:         datatypes.JSON(mustJSON(report)),
		ValidationStatus:   report.Status,
		Metadata:           datatypes.JSON(mustJSON(input.Metadata)),
	}
	if trace.DecisionPhase == "" {
		trace.DecisionPhase = "build"
	}
	if trace.DecisionMode == "" {
		trace.DecisionMode = "deterministic"
	}
	if deps.TraceWriter != nil {
		return deps.TraceWriter.Create(dbctx.Context{Ctx: ctx}, trace)
	}
	return deps.DB.WithContext(ctx).Create(trace).Error
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

package steps

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	repolearning "github.com/yungbote/neurobridge-backend/internal/data/repos/learning"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/drift"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type TraceLoadTestDeps struct {
	DB      *gorm.DB
	Log     *logger.Logger
	Metrics *observability.Metrics
}

type TraceLoadTestInput struct {
	DryRun           bool
	Count            int
	DecisionCount    int
	CandidateCount   int
	CandidateBytes   int
	GraphVersion     string
	DecisionTypes    []string
	RunDrift         bool
	DriftWindowHours int
}

type TraceLoadTestOutput struct {
	StructuralWritten int    `json:"structural_written"`
	DecisionWritten   int    `json:"decision_written"`
	WriteDurationMS   int64  `json:"write_duration_ms"`
	DriftDurationMS   int64  `json:"drift_duration_ms"`
	DriftError        string `json:"drift_error,omitempty"`
}

func TraceLoadTest(ctx context.Context, deps TraceLoadTestDeps, in TraceLoadTestInput) (TraceLoadTestOutput, error) {
	out := TraceLoadTestOutput{}
	if deps.DB == nil {
		return out, fmt.Errorf("trace_load_test: missing db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !envutil.Bool("TRACE_LOAD_TEST_ENABLED", false) {
		return out, nil
	}
	if in.Count <= 0 {
		in.Count = envutil.Int("TRACE_LOAD_TEST_COUNT", 2000)
	}
	if in.Count <= 0 {
		in.Count = 2000
	}
	if in.DecisionCount < 0 {
		in.DecisionCount = 0
	}
	if in.DecisionCount == 0 {
		in.DecisionCount = envutil.Int("TRACE_LOAD_TEST_DECISION_COUNT", 0)
	}
	if in.CandidateCount <= 0 {
		in.CandidateCount = envutil.Int("TRACE_LOAD_TEST_CANDIDATE_COUNT", 40)
	}
	if in.CandidateCount <= 0 {
		in.CandidateCount = 40
	}
	if in.CandidateBytes <= 0 {
		in.CandidateBytes = envutil.Int("TRACE_LOAD_TEST_CANDIDATE_BYTES", 128)
	}
	if in.CandidateBytes <= 0 {
		in.CandidateBytes = 128
	}
	if in.GraphVersion == "" {
		in.GraphVersion = strings.TrimSpace(os.Getenv("GRAPH_VERSION"))
		if in.GraphVersion == "" {
			in.GraphVersion = "load_test"
		}
	}
	if len(in.DecisionTypes) == 0 {
		raw := strings.TrimSpace(os.Getenv("TRACE_LOAD_TEST_DECISION_TYPES"))
		if raw == "" {
			in.DecisionTypes = []string{"concept_graph_build", "concept_cluster_build", "path_grouping_refine"}
		} else {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					in.DecisionTypes = append(in.DecisionTypes, part)
				}
			}
		}
	}
	if in.DriftWindowHours <= 0 {
		in.DriftWindowHours = envutil.Int("TRACE_LOAD_TEST_DRIFT_WINDOW_HOURS", 24)
	}
	if in.DriftWindowHours <= 0 {
		in.DriftWindowHours = 24
	}
	if in.RunDrift {
		// keep as provided
	} else {
		in.RunDrift = envutil.Bool("TRACE_LOAD_TEST_RUN_DRIFT", true)
	}

	candidatePayload := buildCandidatePayload(in.CandidateCount, in.CandidateBytes)
	payloadJSON := mustJSON(candidatePayload)
	now := time.Now().UTC()
	startWrite := time.Now()

	if !in.DryRun {
		structural := make([]*types.StructuralDecisionTrace, 0, in.Count)
		for i := 0; i < in.Count; i++ {
			decisionType := in.DecisionTypes[i%len(in.DecisionTypes)]
			occ := now.Add(time.Duration(i) * time.Millisecond)
			structural = append(structural, &types.StructuralDecisionTrace{
				ID:                 uuid.New(),
				DecisionType:       decisionType,
				DecisionPhase:      "build",
				DecisionMode:       "deterministic",
				OccurredAt:         occ,
				GraphVersion:       in.GraphVersion,
				EmbeddingVersion:   strings.TrimSpace(os.Getenv("EMBEDDING_VERSION")),
				TaxonomyVersion:    strings.TrimSpace(os.Getenv("TAXONOMY_VERSION")),
				ClusteringVersion:  strings.TrimSpace(os.Getenv("CLUSTERING_VERSION")),
				CalibrationVersion: strings.TrimSpace(os.Getenv("CALIBRATION_VERSION")),
				Inputs:             payloadJSON,
				Candidates:         payloadJSON,
				Chosen:             payloadJSON,
				Thresholds:         datatypes.JSON([]byte(`{"threshold":0.5}`)),
				ValidationStatus:   "skipped",
				CreatedAt:          occ,
			})
		}
		if len(structural) > 0 {
			if err := deps.DB.WithContext(ctx).CreateInBatches(structural, 500).Error; err != nil {
				return out, err
			}
		}
		out.StructuralWritten = len(structural)

		if in.DecisionCount > 0 {
			decisions := make([]*types.DecisionTrace, 0, in.DecisionCount)
			for i := 0; i < in.DecisionCount; i++ {
				occ := now.Add(time.Duration(i) * time.Millisecond)
				decisions = append(decisions, &types.DecisionTrace{
					ID:            uuid.New(),
					UserID:        uuid.New(),
					OccurredAt:    occ,
					DecisionType:  "trace_load_test",
					DecisionPhase: "runtime",
					DecisionMode:  "deterministic",
					GraphVersion:  in.GraphVersion,
					Inputs:        payloadJSON,
					Candidates:    payloadJSON,
					Chosen:        payloadJSON,
					CreatedAt:     occ,
				})
			}
			if len(decisions) > 0 {
				if err := deps.DB.WithContext(ctx).CreateInBatches(decisions, 500).Error; err != nil {
					return out, err
				}
			}
			out.DecisionWritten = len(decisions)
		}
	}

	out.WriteDurationMS = time.Since(startWrite).Milliseconds()

	if in.RunDrift {
		startDrift := time.Now()
		driftMetrics := repolearning.NewStructuralDriftMetricRepo(deps.DB, deps.Log)
		_, err := drift.Compute(ctx, drift.ComputeDeps{
			DB:      deps.DB,
			Log:     deps.Log,
			Metrics: driftMetrics,
		}, drift.ComputeInput{
			GraphVersion:               in.GraphVersion,
			WindowHours:                in.DriftWindowHours,
			MinSamples:                 10,
			MaxSamples:                 2000,
			NearThresholdMargin:        0.05,
			ScoreMarginMeanWarnMin:     0.1,
			ScoreMarginMeanCritMin:     0.05,
			ScoreMarginP10WarnMin:      0.05,
			ScoreMarginP10CritMin:      0.02,
			NearThresholdRateWarnMax:   0.25,
			NearThresholdRateCritMax:   0.4,
			ReMergeRateWarnMax:         0.1,
			ReMergeRateCritMax:         0.2,
			EdgeConfidenceShiftWarnMax: 0.1,
			EdgeConfidenceShiftCritMax: 0.2,
			AlertOnWarn:                false,
			RecommendationStatus:       "",
			DecisionTypes:              in.DecisionTypes,
			AllowFallbackGraphVersion:  true,
		})
		out.DriftDurationMS = time.Since(startDrift).Milliseconds()
		if err != nil {
			out.DriftError = err.Error()
		}
	}

	return out, nil
}

func buildCandidatePayload(count int, bytes int) []map[string]any {
	if count <= 0 {
		return nil
	}
	if bytes < 0 {
		bytes = 0
	}
	payload := make([]map[string]any, 0, count)
	base := ""
	if bytes > 0 {
		base = strings.Repeat("x", bytes)
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < count; i++ {
		payload = append(payload, map[string]any{
			"id":     fmt.Sprintf("cand_%d", i),
			"score":  rng.Float64(),
			"meta":   base,
			"rank":   i + 1,
			"sample": rng.Float64(),
		})
	}
	return payload
}

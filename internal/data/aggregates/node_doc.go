package aggregates

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type NodeDocAggregateDeps struct {
	Base BaseDeps

	Docs            repos.LearningNodeDocRepo
	Revisions       repos.LearningNodeDocRevisionRepo
	Variants        repos.LearningNodeDocVariantRepo
	VariantExposure repos.DocVariantExposureRepo
	VariantOutcome  repos.DocVariantOutcomeRepo
	GenRuns         repos.LearningDocGenerationRunRepo
	GenTrace        repos.DocGenerationTraceRepo
	Constraints     repos.DocConstraintReportRepo
}

type nodeDocAggregate struct {
	deps NodeDocAggregateDeps
}

func NewNodeDocAggregate(deps NodeDocAggregateDeps) domainagg.NodeDocAggregate {
	deps.Base = deps.Base.withDefaults()
	return &nodeDocAggregate{deps: deps}
}

func (a *nodeDocAggregate) Contract() domainagg.Contract {
	return domainagg.NodeDocAggregateContract
}

func (a *nodeDocAggregate) CommitRevision(ctx context.Context, in domainagg.CommitNodeDocRevisionInput) (domainagg.CommitNodeDocRevisionResult, error) {
	const op = "DocGen.NodeDoc.CommitRevision"
	var out domainagg.CommitNodeDocRevisionResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

func (a *nodeDocAggregate) RecordVariantOutcome(ctx context.Context, in domainagg.RecordDocVariantOutcomeInput) (domainagg.RecordDocVariantOutcomeResult, error) {
	const op = "DocGen.NodeDoc.RecordVariantOutcome"
	var out domainagg.RecordDocVariantOutcomeResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

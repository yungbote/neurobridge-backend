package aggregates

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type RuntimeAggregateDeps struct {
	Base BaseDeps

	PathRuns     repos.PathRunRepo
	NodeRuns     repos.NodeRunRepo
	ActivityRuns repos.ActivityRunRepo
	Transitions  repos.PathRunTransitionRepo
}

type runtimeAggregate struct {
	deps RuntimeAggregateDeps
}

func NewRuntimeAggregate(deps RuntimeAggregateDeps) domainagg.RuntimeAggregate {
	deps.Base = deps.Base.withDefaults()
	return &runtimeAggregate{deps: deps}
}

func (a *runtimeAggregate) Contract() domainagg.Contract {
	return domainagg.RuntimeAggregateContract
}

func (a *runtimeAggregate) StartPathRun(ctx context.Context, in domainagg.StartPathRunInput) (domainagg.StartPathRunResult, error) {
	const op = "Paths.Runtime.StartPathRun"
	var out domainagg.StartPathRunResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

func (a *runtimeAggregate) AdvancePathRun(ctx context.Context, in domainagg.AdvancePathRunInput) (domainagg.AdvancePathRunResult, error) {
	const op = "Paths.Runtime.AdvancePathRun"
	var out domainagg.AdvancePathRunResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

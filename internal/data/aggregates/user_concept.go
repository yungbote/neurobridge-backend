package aggregates

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type UserConceptAggregateDeps struct {
	Base BaseDeps

	States        repos.UserConceptStateRepo
	Evidence      repos.UserConceptEvidenceRepo
	Calibration   repos.UserConceptCalibrationRepo
	Misconception repos.UserMisconceptionInstanceRepo
	Readiness     repos.ConceptReadinessSnapshotRepo
	Gates         repos.PrereqGateDecisionRepo
	Alerts        repos.UserModelAlertRepo
}

type userConceptAggregate struct {
	deps UserConceptAggregateDeps
}

func NewUserConceptAggregate(deps UserConceptAggregateDeps) domainagg.UserConceptAggregate {
	deps.Base = deps.Base.withDefaults()
	return &userConceptAggregate{deps: deps}
}

func (a *userConceptAggregate) Contract() domainagg.Contract {
	return domainagg.UserConceptAggregateContract
}

func (a *userConceptAggregate) ApplyEvidence(ctx context.Context, in domainagg.ApplyUserConceptEvidenceInput) (domainagg.ApplyUserConceptEvidenceResult, error) {
	const op = "Learning.UserConcept.ApplyEvidence"
	var out domainagg.ApplyUserConceptEvidenceResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

func (a *userConceptAggregate) ResolveMisconception(ctx context.Context, in domainagg.ResolveUserMisconceptionInput) (domainagg.ResolveUserMisconceptionResult, error) {
	const op = "Learning.UserConcept.ResolveMisconception"
	var out domainagg.ResolveUserMisconceptionResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

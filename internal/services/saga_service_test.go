package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

func TestSagaServiceAppendActionDelegatesToAggregate(t *testing.T) {
	log, err := logger.New("test")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	fakeAgg := &fakeSagaAggregate{}
	svc := NewSagaService(nil, log, nil, nil, fakeAgg, nil, nil, "qdrant")

	sagaID := uuid.New()
	if err := svc.AppendAction(dbctx.Context{Ctx: context.Background()}, sagaID, "gcs_delete_key", map[string]any{"key": "x"}); err != nil {
		t.Fatalf("AppendAction: %v", err)
	}
	if fakeAgg.appendCalls != 1 {
		t.Fatalf("append call count: want=1 got=%d", fakeAgg.appendCalls)
	}
	if fakeAgg.lastAppend.SagaID != sagaID {
		t.Fatalf("append saga id: want=%s got=%s", sagaID, fakeAgg.lastAppend.SagaID)
	}
	if fakeAgg.lastAppend.Kind != "gcs_delete_key" {
		t.Fatalf("append kind: want=%q got=%q", "gcs_delete_key", fakeAgg.lastAppend.Kind)
	}
}

func TestSagaServiceMarkStatusDelegatesToAggregate(t *testing.T) {
	log, err := logger.New("test")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	fakeAgg := &fakeSagaAggregate{}
	svc := NewSagaService(nil, log, nil, nil, fakeAgg, nil, nil, "qdrant")

	sagaID := uuid.New()
	if err := svc.MarkSagaStatus(context.Background(), sagaID, SagaStatusFailed); err != nil {
		t.Fatalf("MarkSagaStatus: %v", err)
	}
	if fakeAgg.transitionCalls != 1 {
		t.Fatalf("transition call count: want=1 got=%d", fakeAgg.transitionCalls)
	}
	if fakeAgg.lastTransition.SagaID != sagaID {
		t.Fatalf("transition saga id: want=%s got=%s", sagaID, fakeAgg.lastTransition.SagaID)
	}
	if fakeAgg.lastTransition.ToStatus != SagaStatusFailed {
		t.Fatalf("transition status: want=%q got=%q", SagaStatusFailed, fakeAgg.lastTransition.ToStatus)
	}
}

func TestSagaServiceMarkStatusRequiresAggregate(t *testing.T) {
	log, err := logger.New("test")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	svc := NewSagaService(nil, log, nil, nil, nil, nil, nil, "qdrant")

	err = svc.MarkSagaStatus(context.Background(), uuid.New(), SagaStatusFailed)
	if err == nil {
		t.Fatalf("MarkSagaStatus: expected error when aggregate missing")
	}
	if err.Error() != "saga aggregate not configured" {
		t.Fatalf("MarkSagaStatus: unexpected error: %v", err)
	}
}

func TestSagaServiceExecuteActionVectorDeleteIDsGenericKind(t *testing.T) {
	fakeVec := &fakeVectorStore{}
	svc := &sagaService{vec: fakeVec, vectorProvider: "qdrant"}
	action := &types.SagaAction{
		Kind:    SagaActionKindVectorDeleteIDs,
		Payload: []byte(`{"namespace":"learning:user:1","ids":["vec-1","vec-2"]}`),
	}

	if err := svc.executeAction(context.Background(), action); err != nil {
		t.Fatalf("executeAction: %v", err)
	}
	if fakeVec.deleteCalls != 1 {
		t.Fatalf("delete call count: want=1 got=%d", fakeVec.deleteCalls)
	}
	if fakeVec.lastNamespace != "learning:user:1" {
		t.Fatalf("namespace: want=%q got=%q", "learning:user:1", fakeVec.lastNamespace)
	}
	if len(fakeVec.lastIDs) != 2 || fakeVec.lastIDs[0] != "vec-1" || fakeVec.lastIDs[1] != "vec-2" {
		t.Fatalf("ids mismatch: got=%v", fakeVec.lastIDs)
	}
}

func TestSagaServiceExecuteActionVectorDeleteIDsLegacyKind(t *testing.T) {
	fakeVec := &fakeVectorStore{}
	svc := &sagaService{vec: fakeVec, vectorProvider: "qdrant"}
	action := &types.SagaAction{
		Kind:    SagaActionKindPineconeDeleteIDs,
		Payload: []byte(`{"namespace":"learning:user:2","ids":["legacy-1"]}`),
	}

	if err := svc.executeAction(context.Background(), action); err != nil {
		t.Fatalf("executeAction: %v", err)
	}
	if fakeVec.deleteCalls != 1 {
		t.Fatalf("delete call count: want=1 got=%d", fakeVec.deleteCalls)
	}
	if fakeVec.lastNamespace != "learning:user:2" {
		t.Fatalf("namespace: want=%q got=%q", "learning:user:2", fakeVec.lastNamespace)
	}
	if len(fakeVec.lastIDs) != 1 || fakeVec.lastIDs[0] != "legacy-1" {
		t.Fatalf("ids mismatch: got=%v", fakeVec.lastIDs)
	}
}

func TestSagaServiceExecuteActionVectorDeleteIDsUnavailable(t *testing.T) {
	svc := &sagaService{vec: nil, vectorProvider: "qdrant"}
	action := &types.SagaAction{
		Kind:    SagaActionKindVectorDeleteIDs,
		Payload: []byte(`{"namespace":"x","ids":["y"]}`),
	}

	err := svc.executeAction(context.Background(), action)
	if err == nil {
		t.Fatalf("executeAction: expected error, got nil")
	}
	if got := err.Error(); got != "vector store unavailable (provider=qdrant)" {
		t.Fatalf("error mismatch: got=%q", got)
	}
}

func TestSagaActionKindForLogVectorMaintainsLegacyField(t *testing.T) {
	kind, actionKind, legacyKind := sagaActionKindForLog(SagaActionKindVectorDeleteIDs)
	if kind != SagaActionKindPineconeDeleteIDs {
		t.Fatalf("kind mismatch: want=%q got=%q", SagaActionKindPineconeDeleteIDs, kind)
	}
	if actionKind != SagaActionKindVectorDeleteIDs {
		t.Fatalf("action kind mismatch: want=%q got=%q", SagaActionKindVectorDeleteIDs, actionKind)
	}
	if legacyKind != SagaActionKindPineconeDeleteIDs {
		t.Fatalf("legacy kind mismatch: want=%q got=%q", SagaActionKindPineconeDeleteIDs, legacyKind)
	}
}

type fakeSagaAggregate struct {
	appendCalls     int
	transitionCalls int
	lastAppend      domainagg.AppendSagaActionInput
	lastTransition  domainagg.TransitionSagaStatusInput
}

func (f *fakeSagaAggregate) Contract() domainagg.Contract {
	return domainagg.SagaAggregateContract
}

func (f *fakeSagaAggregate) AppendAction(_ context.Context, in domainagg.AppendSagaActionInput) (domainagg.AppendSagaActionResult, error) {
	f.appendCalls++
	f.lastAppend = in
	return domainagg.AppendSagaActionResult{
		SagaID:   in.SagaID,
		ActionID: uuid.New(),
		Seq:      int64(f.appendCalls),
		Status:   SagaActionStatusPending,
	}, nil
}

func (f *fakeSagaAggregate) TransitionStatus(_ context.Context, in domainagg.TransitionSagaStatusInput) (domainagg.TransitionSagaStatusResult, error) {
	f.transitionCalls++
	f.lastTransition = in
	return domainagg.TransitionSagaStatusResult{
		SagaID: in.SagaID,
		Status: in.ToStatus,
	}, nil
}

type fakeVectorStore struct {
	deleteCalls   int
	lastNamespace string
	lastIDs       []string
}

func (f *fakeVectorStore) Upsert(_ context.Context, _ string, _ []pinecone.Vector) error {
	return nil
}

func (f *fakeVectorStore) QueryMatches(_ context.Context, _ string, _ []float32, _ int, _ map[string]any) ([]pinecone.VectorMatch, error) {
	return nil, nil
}

func (f *fakeVectorStore) QueryIDs(_ context.Context, _ string, _ []float32, _ int, _ map[string]any) ([]string, error) {
	return nil, nil
}

func (f *fakeVectorStore) DeleteIDs(_ context.Context, namespace string, ids []string) error {
	f.deleteCalls++
	f.lastNamespace = namespace
	f.lastIDs = append([]string(nil), ids...)
	return nil
}

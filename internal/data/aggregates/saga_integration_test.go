package aggregates

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	jobrepos "github.com/yungbote/neurobridge-backend/internal/data/repos/jobs"
	repotest "github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/gorm"
)

func TestSagaAggregateAppendActionHappyPath(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureSagaTables(t, tx)

	log := repotest.Logger(t)
	runs := jobrepos.NewSagaRunRepo(tx, log)
	actions := jobrepos.NewSagaActionRepo(tx, log)

	agg := NewSagaAggregate(SagaAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   NewGormTxRunner(tx),
			CASGuard: NewCASGuard(tx),
		},
		Runs:    runs,
		Actions: actions,
	})

	ctx := context.Background()
	sagaID := seedSagaRun(t, ctx, runs, sagaStatusRunning)

	first, err := agg.AppendAction(ctx, domainagg.AppendSagaActionInput{
		SagaID: sagaID,
		Kind:   "gcs_delete_key",
		Payload: json.RawMessage(
			`{"category":"material","key":"staging/saga/test/key"}`,
		),
	})
	if err != nil {
		t.Fatalf("AppendAction first: %v", err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq: want=1 got=%d", first.Seq)
	}
	if first.Status != sagaActionStatusPending {
		t.Fatalf("first status: want=%q got=%q", sagaActionStatusPending, first.Status)
	}

	second, err := agg.AppendAction(ctx, domainagg.AppendSagaActionInput{
		SagaID: sagaID,
		Kind:   "gcs_delete_prefix",
		Payload: json.RawMessage(
			`{"category":"material","prefix":"staging/saga/test/"}`,
		),
	})
	if err != nil {
		t.Fatalf("AppendAction second: %v", err)
	}
	if second.Seq != 2 {
		t.Fatalf("second seq: want=2 got=%d", second.Seq)
	}

	rows, err := actions.ListBySagaIDDesc(dbctx.Context{Ctx: ctx}, sagaID)
	if err != nil {
		t.Fatalf("ListBySagaIDDesc: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("actions len: want=2 got=%d", len(rows))
	}
	if rows[0] == nil || rows[0].Seq != 2 {
		t.Fatalf("latest action seq: want=2 got=%v", rows[0])
	}
	if rows[1] == nil || rows[1].Seq != 1 {
		t.Fatalf("oldest action seq: want=1 got=%v", rows[1])
	}
}

func TestSagaAggregateAppendActionInvariantViolation(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureSagaTables(t, tx)

	log := repotest.Logger(t)
	runs := jobrepos.NewSagaRunRepo(tx, log)
	actions := jobrepos.NewSagaActionRepo(tx, log)

	agg := NewSagaAggregate(SagaAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   NewGormTxRunner(tx),
			CASGuard: NewCASGuard(tx),
		},
		Runs:    runs,
		Actions: actions,
	})

	ctx := context.Background()
	sagaID := seedSagaRun(t, ctx, runs, sagaStatusCompensated)

	_, err := agg.AppendAction(ctx, domainagg.AppendSagaActionInput{
		SagaID:  sagaID,
		Kind:    "gcs_delete_key",
		Payload: json.RawMessage(`{"category":"material","key":"x"}`),
	})
	if err == nil {
		t.Fatalf("expected invariant violation")
	}
	if !domainagg.IsCode(err, domainagg.CodeInvariantViolation) {
		t.Fatalf("expected invariant violation code, got: %v", err)
	}
}

func TestSagaAggregateAppendActionRollbackOnInjectedFailure(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureSagaTables(t, tx)

	log := repotest.Logger(t)
	runs := jobrepos.NewSagaRunRepo(tx, log)
	actions := jobrepos.NewSagaActionRepo(tx, log)

	agg := NewSagaAggregate(SagaAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   rollbackAfterBodyRunner{db: tx, err: errors.New("injected aggregate failure")},
			CASGuard: NewCASGuard(tx),
		},
		Runs:    runs,
		Actions: actions,
	})

	ctx := context.Background()
	sagaID := seedSagaRun(t, ctx, runs, sagaStatusRunning)

	_, err := agg.AppendAction(ctx, domainagg.AppendSagaActionInput{
		SagaID:  sagaID,
		Kind:    "gcs_delete_key",
		Payload: json.RawMessage(`{"category":"material","key":"x"}`),
	})
	if err == nil {
		t.Fatalf("expected injected failure")
	}

	rows, listErr := actions.ListBySagaIDDesc(dbctx.Context{Ctx: ctx}, sagaID)
	if listErr != nil {
		t.Fatalf("ListBySagaIDDesc: %v", listErr)
	}
	if len(rows) != 0 {
		t.Fatalf("expected rollback with no persisted actions, got=%d", len(rows))
	}
}

func TestSagaAggregateTransitionStatusConcurrentConflict(t *testing.T) {
	db := repotest.DB(t)
	ensureSagaTables(t, db)

	log := repotest.Logger(t)
	runs := jobrepos.NewSagaRunRepo(db, log)
	actions := jobrepos.NewSagaActionRepo(db, log)

	agg := NewSagaAggregate(SagaAggregateDeps{
		Base: BaseDeps{
			DB:       db,
			Runner:   NewGormTxRunner(db),
			CASGuard: NewCASGuard(db),
		},
		Runs:    runs,
		Actions: actions,
	})

	ctx := context.Background()
	sagaID := seedSagaRun(t, ctx, runs, sagaStatusRunning)
	t.Cleanup(func() {
		_ = db.WithContext(ctx).Where("saga_id = ?", sagaID).Delete(&types.SagaAction{}).Error
		_ = db.WithContext(ctx).Where("id = ?", sagaID).Delete(&types.SagaRun{}).Error
	})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		_, err := agg.TransitionStatus(ctx, domainagg.TransitionSagaStatusInput{
			SagaID:     sagaID,
			FromStatus: sagaStatusRunning,
			ToStatus:   sagaStatusFailed,
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := agg.TransitionStatus(ctx, domainagg.TransitionSagaStatusInput{
			SagaID:     sagaID,
			FromStatus: sagaStatusRunning,
			ToStatus:   sagaStatusSucceeded,
		})
		errs <- err
	}()

	close(start)
	wg.Wait()
	close(errs)

	var successCount int
	var conflictCount int
	for err := range errs {
		if err == nil {
			successCount++
			continue
		}
		if domainagg.IsCode(err, domainagg.CodeConflict) {
			conflictCount++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if successCount != 1 {
		t.Fatalf("success count: want=1 got=%d", successCount)
	}
	if conflictCount != 1 {
		t.Fatalf("conflict count: want=1 got=%d", conflictCount)
	}
}

type rollbackAfterBodyRunner struct {
	db  *gorm.DB
	err error
}

func (r rollbackAfterBodyRunner) InTx(ctx context.Context, fn func(dbc dbctx.Context) error) error {
	if r.db == nil {
		return errors.New("missing db")
	}
	injected := r.err
	if injected == nil {
		injected = errors.New("forced rollback")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if fn == nil {
			return injected
		}
		if err := fn(dbctx.Context{Ctx: ctx, Tx: tx}); err != nil {
			return err
		}
		return injected
	})
}

func ensureSagaTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&types.SagaRun{}, &types.SagaAction{}); err != nil {
		t.Fatalf("AutoMigrate saga tables: %v", err)
	}
}

func seedSagaRun(t *testing.T, ctx context.Context, runs jobrepos.SagaRunRepo, status string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	row := &types.SagaRun{
		ID:          uuid.New(),
		OwnerUserID: uuid.New(),
		RootJobID:   uuid.New(),
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := runs.Create(dbctx.Context{Ctx: ctx}, []*types.SagaRun{row}); err != nil {
		t.Fatalf("seed saga_run: %v", err)
	}
	return row.ID
}

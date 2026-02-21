package aggregates

import (
	"context"
	"strings"
	"time"

	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/gorm"
)

type BaseDeps struct {
	DB       *gorm.DB
	Log      *logger.Logger
	Runner   TxRunner
	Hooks    Hooks
	CASGuard CASGuard
}

func (d BaseDeps) withDefaults() BaseDeps {
	if d.Runner == nil {
		d.Runner = NewGormTxRunner(d.DB)
	}
	if d.Hooks == nil {
		d.Hooks = noopHooks{}
	}
	if d.CASGuard.db == nil {
		d.CASGuard = NewCASGuard(d.DB)
	}
	return d
}

func executeWrite(ctx context.Context, deps BaseDeps, op string, fn func(dbc dbctx.Context) error) error {
	start := time.Now()
	deps = deps.withDefaults()
	op = strings.TrimSpace(op)
	if op == "" {
		op = "aggregate.write"
	}
	err := deps.Runner.InTx(ctx, fn)
	mapped := MapError(op, err)

	status := "success"
	if mapped != nil {
		status = aggregateErrorStatus(mapped)
		if domainagg.IsCode(mapped, domainagg.CodeConflict) {
			deps.Hooks.IncConflict(op)
		}
		if domainagg.IsCode(mapped, domainagg.CodeRetryable) {
			deps.Hooks.IncRetry(op)
		}
	}
	deps.Hooks.ObserveOperation(op, status, time.Since(start))
	return mapped
}

func aggregateErrorStatus(err error) string {
	if err == nil {
		return "success"
	}
	code := strings.TrimSpace(string(domainagg.CodeOf(err)))
	if code == "" {
		code = strings.TrimSpace(string(domainagg.CodeOf(MapError("aggregate.status", err))))
	}
	if code == "" {
		return "failure"
	}
	return code
}

package aggregates

import (
	"context"

	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/gorm"
)

// TxRunner provides a shared transaction boundary primitive for aggregate writes.
type TxRunner interface {
	InTx(ctx context.Context, fn func(dbc dbctx.Context) error) error
}

type gormTxRunner struct {
	db *gorm.DB
}

// NewGormTxRunner returns a transaction runner backed by GORM transactions.
func NewGormTxRunner(db *gorm.DB) TxRunner {
	return &gormTxRunner{db: db}
}

func (r *gormTxRunner) InTx(ctx context.Context, fn func(dbc dbctx.Context) error) error {
	if fn == nil {
		return nil
	}
	if r == nil || r.db == nil {
		return domainagg.NewError(domainagg.CodeInternal, "aggregate.tx", "transaction runner has nil db", nil)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(dbctx.Context{Ctx: ctx, Tx: tx})
	})
}

package dbctx

import (
	"context"

	"gorm.io/gorm"
)

// Context bundles a request context with an optional GORM transaction.
type Context struct {
	Ctx context.Context
	Tx  *gorm.DB
}

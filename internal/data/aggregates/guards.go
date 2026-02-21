package aggregates

import (
	"strings"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/gorm"
)

// CASGuard provides optimistic/concurrency guard helpers for aggregate writes.
type CASGuard struct {
	db *gorm.DB
}

func NewCASGuard(db *gorm.DB) CASGuard {
	return CASGuard{db: db}
}

func (g CASGuard) baseDB(dbc dbctx.Context) (*gorm.DB, error) {
	if dbc.Tx != nil {
		return dbc.Tx.WithContext(dbc.Ctx), nil
	}
	if g.db != nil {
		return g.db.WithContext(dbc.Ctx), nil
	}
	return nil, ValidationError("missing db transaction context")
}

// UpdateByVersion updates a row only when id+version match.
// It implements compare-and-set semantics commonly used for optimistic locking.
func (g CASGuard) UpdateByVersion(dbc dbctx.Context, table string, id uuid.UUID, expectedVersion int, updates map[string]any) (bool, error) {
	db, err := g.baseDB(dbc)
	if err != nil {
		return false, err
	}
	table = strings.TrimSpace(table)
	if table == "" || id == uuid.Nil {
		return false, ValidationError("table and id are required for UpdateByVersion")
	}
	if expectedVersion < 0 {
		return false, ValidationError("expectedVersion must be >= 0")
	}
	res := db.Table(table).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateByStatus updates a row only when id+status guard matches.
func (g CASGuard) UpdateByStatus(dbc dbctx.Context, table string, id uuid.UUID, allowedStatuses []string, updates map[string]any) (bool, error) {
	db, err := g.baseDB(dbc)
	if err != nil {
		return false, err
	}
	table = strings.TrimSpace(table)
	if table == "" || id == uuid.Nil {
		return false, ValidationError("table and id are required for UpdateByStatus")
	}
	if len(allowedStatuses) == 0 {
		return false, ValidationError("allowedStatuses must not be empty")
	}
	res := db.Table(table).
		Where("id = ? AND status IN ?", id, allowedStatuses).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// RequireCASSuccess converts a failed compare-and-set into a typed conflict error.
func RequireCASSuccess(ok bool, message string) error {
	if ok {
		return nil
	}
	return ConflictError(strings.TrimSpace(message))
}

// RequireStatusAllowed validates current status against allowed values.
func RequireStatusAllowed(current string, allowed ...string) error {
	current = strings.TrimSpace(current)
	if len(allowed) == 0 {
		return ValidationError("allowed statuses cannot be empty")
	}
	for _, s := range allowed {
		if strings.EqualFold(current, strings.TrimSpace(s)) {
			return nil
		}
	}
	return ConflictError("status transition not allowed")
}

// RequireVersionMatch validates version equality for optimistic locking flows.
func RequireVersionMatch(current, expected int) error {
	if expected < 0 {
		return ValidationError("expected version must be >= 0")
	}
	if current != expected {
		return ConflictError("version mismatch")
	}
	return nil
}

package aggregates

import (
	"errors"
	"testing"

	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"gorm.io/gorm"
)

func TestMapError_Validation(t *testing.T) {
	err := MapError("op", ValidationError("bad input"))
	if !domainagg.IsCode(err, domainagg.CodeValidation) {
		t.Fatalf("expected validation code, got %q (%v)", domainagg.CodeOf(err), err)
	}
}

func TestMapError_Conflict(t *testing.T) {
	err := MapError("op", ConflictError("stale"))
	if !domainagg.IsCode(err, domainagg.CodeConflict) {
		t.Fatalf("expected conflict code, got %q (%v)", domainagg.CodeOf(err), err)
	}
}

func TestMapError_NotFound(t *testing.T) {
	err := MapError("op", gorm.ErrRecordNotFound)
	if !domainagg.IsCode(err, domainagg.CodeNotFound) {
		t.Fatalf("expected not_found code, got %q (%v)", domainagg.CodeOf(err), err)
	}
}

func TestMapError_PassthroughAggregateError(t *testing.T) {
	in := domainagg.NewError(domainagg.CodeRetryable, "op", "retry", errors.New("boom"))
	out := MapError("other", in)
	if out != in {
		t.Fatalf("expected passthrough aggregate error")
	}
}

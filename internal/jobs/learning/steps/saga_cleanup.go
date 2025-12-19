package steps

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type SagaCleanupDeps struct {
	DB      *gorm.DB
	Log     *logger.Logger
	Sagas   repos.SagaRunRepo
	SagaSvc services.SagaService
	Bucket  gcp.BucketService
}

type SagaCleanupInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type SagaCleanupOutput struct {
	SagasScanned    int `json:"sagas_scanned"`
	PrefixesDeleted int `json:"prefixes_deleted"`
}

func SagaCleanup(ctx context.Context, deps SagaCleanupDeps, in SagaCleanupInput) (SagaCleanupOutput, error) {
	out := SagaCleanupOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Sagas == nil || deps.SagaSvc == nil || deps.Bucket == nil {
		return out, fmt.Errorf("saga_cleanup: missing deps")
	}

	olderHours := 24
	if v := strings.TrimSpace(os.Getenv("SAGA_CLEANUP_OLDER_HOURS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			olderHours = n
		}
	}
	limit := 100
	if v := strings.TrimSpace(os.Getenv("SAGA_CLEANUP_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	cutoff := time.Now().UTC().Add(-time.Duration(olderHours) * time.Hour)

	sagas, err := deps.Sagas.ListByStatusBefore(ctx, nil, []string{
		services.SagaStatusFailed,
		services.SagaStatusCompensated,
	}, cutoff, limit)
	if err != nil {
		return out, err
	}
	out.SagasScanned = len(sagas)

	for _, s := range sagas {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		// Best-effort: ensure recorded actions are applied.
		_ = deps.SagaSvc.Compensate(ctx, s.ID)

		// Best-effort: delete standard staging prefix even if no action was recorded.
		prefix := services.SagaStagingPrefix(s.ID)
		if strings.TrimSpace(prefix) == "" {
			continue
		}
		if err := deps.Bucket.DeletePrefix(ctx, gcp.BucketCategoryMaterial, prefix); err == nil {
			out.PrefixesDeleted++
		}
	}

	return out, nil
}

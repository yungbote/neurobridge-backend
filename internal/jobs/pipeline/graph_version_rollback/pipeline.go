package graph_version_rollback

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/rollback"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p.db == nil {
		jc.Fail("deps", fmt.Errorf("missing db"))
		return nil
	}
	payload := jc.Payload()
	startedAt := time.Now().UTC()

	rollbackEventID := parseUUID(payload["rollback_event_id"])
	graphFrom := strings.TrimSpace(stringFromAny(payload["graph_version_from"]))
	graphTo := strings.TrimSpace(stringFromAny(payload["graph_version_to"]))
	trigger := strings.TrimSpace(stringFromAny(payload["trigger"]))
	if trigger == "" {
		trigger = "manual"
	}
	dryRun := boolFromAny(payload["dry_run"], false)
	reconcileUsers := boolFromAny(payload["reconcile_users"], true)
	reconcileBatch := intFromAny(payload["reconcile_batch_size"], 200)
	reconcileMax := intFromAny(payload["reconcile_max_users"], 0)
	resumePaused := boolFromAny(payload["resume_paused"], true)

	userIDs := parseUUIDs(payload["user_ids"])

	jc.Progress("resolve", 5, "Resolving rollback target")

	event, err := p.loadOrCreateEvent(jc.Ctx, rollbackEventID, graphFrom, trigger)
	if err != nil {
		jc.Fail("resolve", err)
		return nil
	}
	graphFrom = strings.TrimSpace(graphFrom)
	if graphFrom == "" && event != nil {
		graphFrom = strings.TrimSpace(event.GraphVersionFrom)
	}
	if graphFrom == "" {
		graphFrom, _ = latestGraphVersion(jc.Ctx, p.db)
	}
	if graphFrom == "" {
		jc.Fail("resolve", fmt.Errorf("missing graph_version_from"))
		return nil
	}
	if graphTo == "" && event != nil {
		graphTo = strings.TrimSpace(event.GraphVersionTo)
	}
	if graphTo == "" {
		graphTo, _ = selectRollbackTarget(jc.Ctx, p.db, graphFrom)
	}
	if graphTo == "" {
		jc.Fail("resolve", fmt.Errorf("missing graph_version_to"))
		return nil
	}

	if dryRun {
		jc.Succeed("dry_run", map[string]any{
			"graph_version_from": graphFrom,
			"graph_version_to":   graphTo,
			"rollback_event_id":  eventIDString(event),
			"trigger":            trigger,
		})
		return nil
	}

	if err := p.markRollbackRunning(jc.Ctx, event, graphFrom, graphTo, trigger); err != nil {
		observeRollbackMetrics(event, "failed", startedAt)
		jc.Fail("start", err)
		return nil
	}

	jc.Progress("freeze", 10, "Freezing structural updates")
	if err := setGraphStatus(jc.Ctx, p.db, p.graphs, graphFrom, "rolling_back"); err != nil {
		p.markRollbackFailed(jc.Ctx, event, err)
		observeRollbackMetrics(event, "failed", startedAt)
		jc.Fail("freeze", err)
		return nil
	}
	_ = setGraphStatus(jc.Ctx, p.db, p.graphs, graphTo, "activating")

	jc.Progress("reconcile", 40, "Reconciling user models")
	reconcileOut := rollback.ReconcileOutput{}
	if reconcileUsers {
		out, err := rollback.ReconcileUserState(jc.Ctx, rollback.ReconcileDeps{
			DB:     p.db,
			Log:    p.log,
			JobSvc: p.jobSvc,
		}, rollback.ReconcileInput{
			UserIDs:   userIDs,
			BatchSize: reconcileBatch,
			MaxUsers:  reconcileMax,
			Trigger:   "graph_version_rollback",
		})
		if err != nil {
			p.markRollbackFailed(jc.Ctx, event, err)
			observeRollbackMetrics(event, "failed", startedAt)
			jc.Fail("reconcile", err)
			return nil
		}
		reconcileOut = out
	}

	jc.Progress("activate", 70, "Activating rollback target")
	if err := activateGraphVersion(jc.Ctx, p.db, p.graphs, graphTo); err != nil {
		p.markRollbackFailed(jc.Ctx, event, err)
		observeRollbackMetrics(event, "failed", startedAt)
		jc.Fail("activate", err)
		return nil
	}
	_ = setGraphStatus(jc.Ctx, p.db, p.graphs, graphFrom, "rolled_back")

	resumed := 0
	if resumePaused {
		if count, err := rollback.ResumePausedJobs(jc.Ctx, p.db, "structural_freeze"); err == nil {
			resumed = count
		}
	}

	jc.Progress("finalize", 90, "Finalizing rollback")
	if err := p.markRollbackCompleted(jc.Ctx, event, graphFrom, graphTo, reconcileOut, resumed); err != nil {
		observeRollbackMetrics(event, "failed", startedAt)
		jc.Fail("finalize", err)
		return nil
	}
	observeRollbackMetrics(event, "completed", startedAt)

	jc.Succeed("done", map[string]any{
		"graph_version_from": graphFrom,
		"graph_version_to":   graphTo,
		"rollback_event_id":  eventIDString(event),
		"users_scanned":      reconcileOut.UsersScanned,
		"users_queued":       reconcileOut.UsersQueued,
		"resumed_jobs":       resumed,
	})
	return nil
}

func (p *Pipeline) loadOrCreateEvent(ctx context.Context, id uuid.UUID, graphFrom, trigger string) (*types.RollbackEvent, error) {
	if id != uuid.Nil {
		row := &types.RollbackEvent{}
		if err := p.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(row).Error; err != nil {
			return nil, err
		}
		if row.ID == uuid.Nil {
			return nil, fmt.Errorf("rollback_event not found")
		}
		return row, nil
	}
	row := &types.RollbackEvent{
		ID:               uuid.New(),
		GraphVersionFrom: strings.TrimSpace(graphFrom),
		Trigger:          strings.TrimSpace(trigger),
		Status:           "pending",
	}
	if p.rollbacks != nil {
		if err := p.rollbacks.Create(dbctx.Context{Ctx: ctx}, row); err != nil {
			return nil, err
		}
		return row, nil
	}
	if err := p.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (p *Pipeline) markRollbackRunning(ctx context.Context, row *types.RollbackEvent, from, to, trigger string) error {
	if row == nil || row.ID == uuid.Nil {
		return nil
	}
	updates := map[string]any{
		"status":             "running",
		"graph_version_from": from,
		"graph_version_to":   to,
		"trigger":            strings.TrimSpace(trigger),
		"initiated_at":       time.Now().UTC(),
	}
	return p.db.WithContext(ctx).Model(&types.RollbackEvent{}).Where("id = ?", row.ID).Updates(updates).Error
}

func (p *Pipeline) markRollbackFailed(ctx context.Context, row *types.RollbackEvent, err error) {
	if row == nil || row.ID == uuid.Nil || p.rollbacks == nil {
		return
	}
	now := time.Now().UTC()
	_ = p.db.WithContext(ctx).Model(&types.RollbackEvent{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status":       "failed",
		"completed_at": now,
	}).Error
}

func (p *Pipeline) markRollbackCompleted(ctx context.Context, row *types.RollbackEvent, from, to string, reconcile rollback.ReconcileOutput, resumed int) error {
	if row == nil || row.ID == uuid.Nil {
		return nil
	}
	notes := map[string]any{
		"users_scanned": reconcile.UsersScanned,
		"users_queued":  reconcile.UsersQueued,
		"rows_cleared":  reconcile.RowsCleared,
		"resumed_jobs":  resumed,
		"finished_at":   time.Now().UTC().Format(time.RFC3339),
	}
	if reconcile.StartedAt.IsZero() == false {
		notes["reconcile_started_at"] = reconcile.StartedAt.Format(time.RFC3339)
	}
	if reconcile.FinishedAt.IsZero() == false {
		notes["reconcile_finished_at"] = reconcile.FinishedAt.Format(time.RFC3339)
	}
	updates := map[string]any{
		"status":             "completed",
		"graph_version_from": from,
		"graph_version_to":   to,
		"completed_at":       time.Now().UTC(),
		"notes":              datatypes.JSON(mustJSON(notes)),
	}
	return p.db.WithContext(ctx).Model(&types.RollbackEvent{}).Where("id = ?", row.ID).Updates(updates).Error
}

func latestGraphVersion(ctx context.Context, db *gorm.DB) (string, error) {
	if db == nil {
		return "", nil
	}
	row := &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Where("status = ?", "active").
		Order("updated_at DESC").
		Limit(1).
		Find(row).Error; err == nil && row.GraphVersion != "" {
		return row.GraphVersion, nil
	}
	row = &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Order("updated_at DESC").
		Limit(1).
		Find(row).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(row.GraphVersion), nil
}

func selectRollbackTarget(ctx context.Context, db *gorm.DB, from string) (string, error) {
	if db == nil {
		return "", nil
	}
	row := &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Where("status IN ? AND graph_version <> ?", []string{"active", "stable"}, from).
		Order("updated_at DESC").
		Limit(1).
		Find(row).Error; err == nil && row.GraphVersion != "" {
		return row.GraphVersion, nil
	}
	row = &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Where("graph_version <> ? AND status <> ?", from, "rolled_back").
		Order("updated_at DESC").
		Limit(1).
		Find(row).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(row.GraphVersion), nil
}

func activateGraphVersion(ctx context.Context, db *gorm.DB, repo repos.GraphVersionRepo, graphVersion string) error {
	if db == nil || strings.TrimSpace(graphVersion) == "" {
		return nil
	}
	_ = db.WithContext(ctx).Model(&types.GraphVersion{}).
		Where("status = ? AND graph_version <> ?", "active", graphVersion).
		Updates(map[string]any{"status": "inactive", "updated_at": time.Now().UTC()}).Error
	if repo != nil {
		return repo.SetStatus(dbctx.Context{Ctx: ctx}, graphVersion, "active")
	}
	return db.WithContext(ctx).Model(&types.GraphVersion{}).Where("graph_version = ?", graphVersion).Updates(map[string]any{
		"status":     "active",
		"updated_at": time.Now().UTC(),
	}).Error
}

func setGraphStatus(ctx context.Context, db *gorm.DB, repo repos.GraphVersionRepo, graphVersion, status string) error {
	if strings.TrimSpace(graphVersion) == "" || strings.TrimSpace(status) == "" {
		return nil
	}
	if repo != nil {
		return repo.SetStatus(dbctx.Context{Ctx: ctx}, graphVersion, status)
	}
	return db.WithContext(ctx).Model(&types.GraphVersion{}).Where("graph_version = ?", graphVersion).Updates(map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
	}).Error
}

func parseUUID(v any) uuid.UUID {
	if v == nil {
		return uuid.Nil
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func parseUUIDs(v any) []uuid.UUID {
	out := []uuid.UUID{}
	switch t := v.(type) {
	case []uuid.UUID:
		return t
	case []string:
		for _, s := range t {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
				out = append(out, id)
			}
		}
	case []any:
		for _, item := range t {
			if id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(item))); err == nil {
				out = append(out, id)
			}
		}
	case string:
		for _, part := range strings.Split(t, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(part)); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return def
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func boolFromAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(strings.ToLower(fmt.Sprint(v)))
	switch s {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}

func observeRollbackMetrics(event *types.RollbackEvent, status string, startedAt time.Time) {
	metrics := observability.Current()
	if metrics == nil {
		return
	}
	duration := time.Since(startedAt)
	if event != nil && event.InitiatedAt != nil && !event.InitiatedAt.IsZero() {
		duration = time.Since(event.InitiatedAt.UTC())
	}
	metrics.ObserveRollback(duration, status)
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func eventIDString(ev *types.RollbackEvent) string {
	if ev == nil || ev.ID == uuid.Nil {
		return ""
	}
	return ev.ID.String()
}

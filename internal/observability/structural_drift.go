package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type StructuralDriftAlertMetric struct {
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Value     float64        `json:"value"`
	Threshold float64        `json:"threshold"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type driftAlertState struct {
	mu   sync.Mutex
	last map[string]time.Time
}

var driftAlerts driftAlertState

func ReportStructuralDrift(ctx context.Context, log *logger.Logger, metrics []StructuralDriftAlertMetric, meta map[string]any) {
	if len(metrics) == 0 {
		return
	}
	if !structuralDriftAlertsEnabled() {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	if td := ctxutil.GetTraceData(ctx); td != nil {
		if td.TraceID != "" {
			meta["trace_id"] = td.TraceID
		}
		if td.RequestID != "" {
			meta["request_id"] = td.RequestID
		}
	}

	webhook := structuralDriftAlertWebhook()
	if webhook == "" {
		return
	}
	key := "structural_drift"
	driftAlerts.mu.Lock()
	if driftAlerts.last == nil {
		driftAlerts.last = map[string]time.Time{}
	}
	last := driftAlerts.last[key]
	minInterval := structuralDriftAlertMinInterval()
	if !last.IsZero() && time.Since(last) < minInterval {
		driftAlerts.mu.Unlock()
		return
	}
	driftAlerts.last[key] = time.Now()
	driftAlerts.mu.Unlock()

	payload := map[string]any{
		"title":     "Structural drift detected",
		"metrics":   metrics,
		"meta":      meta,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		if log != nil {
			log.Warn("structural drift alert request build failed", "error", err)
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if log != nil {
			log.Warn("structural drift alert post failed", "error", err)
		}
		return
	}
	_ = resp.Body.Close()
	if log != nil {
		log.Info("structural drift alert sent", "status", resp.StatusCode)
	}
}

func structuralDriftAlertsEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("STRUCTURAL_DRIFT_ALERTS_ENABLED")))
	if v == "" {
		return false
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func structuralDriftAlertWebhook() string {
	val := strings.TrimSpace(os.Getenv("STRUCTURAL_DRIFT_ALERT_WEBHOOK_URL"))
	if val != "" {
		return val
	}
	return strings.TrimSpace(os.Getenv("SLO_ALERT_WEBHOOK_URL"))
}

func structuralDriftAlertMinInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("STRUCTURAL_DRIFT_ALERT_MIN_INTERVAL_SECONDS"))
	if raw == "" {
		return 10 * time.Minute
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

var (
	missingKeysRe = regexp.MustCompile(`required missing keys: \[([^\]]+)\]`)
	missingKeysAltRe = regexp.MustCompile(`missing keys: \[([^\]]+)\]`)
)

type dqAlertState struct {
	mu   sync.Mutex
	last map[string]time.Time
}

var dqAlerts dqAlertState

func ReportDataQualityErrors(ctx context.Context, log *logger.Logger, stage string, errs []string, meta map[string]any) {
	if len(errs) == 0 {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "unknown"
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

	issueCounts := map[string]int{}
	sampleErrors := make([]string, 0, 3)
	for _, errStr := range errs {
		errStr = strings.TrimSpace(errStr)
		if errStr == "" {
			continue
		}
		if len(sampleErrors) < 3 {
			sampleErrors = append(sampleErrors, errStr)
		}
		lower := strings.ToLower(errStr)
		missing := extractMissingKeys(errStr)
		if len(missing) > 0 {
			for _, key := range missing {
				incDataQuality(stage, "missing_required", key)
			}
			issueCounts["missing_required"] += len(missing)
			continue
		}
		if strings.Contains(lower, "banned meta phrasing") {
			incDataQuality(stage, "banned_meta", "")
			issueCounts["banned_meta"]++
			continue
		}
		if strings.Contains(lower, "lint schema") || strings.Contains(lower, "schema") {
			incDataQuality(stage, "schema_validation", "")
			issueCounts["schema_validation"]++
			continue
		}
		incDataQuality(stage, "validation_error", "")
		issueCounts["validation_error"]++
	}

	if log != nil {
		log.Warn("data quality issue detected",
			"stage", stage,
			"issues", issueCounts,
			"sample_errors", sampleErrors,
			"meta", meta,
		)
	}
	sendDataQualityAlert(stage, issueCounts, sampleErrors, meta, log)
}

func ReportDataQualityMissingKeys(ctx context.Context, log *logger.Logger, stage string, keys []string, meta map[string]any) {
	if len(keys) == 0 {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "unknown"
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
	issueCounts := map[string]int{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		incDataQuality(stage, "missing_required", key)
		issueCounts["missing_required"]++
	}
	if log != nil && len(issueCounts) > 0 {
		log.Warn("data quality missing keys", "stage", stage, "issues", issueCounts, "meta", meta)
	}
	sendDataQualityAlert(stage, issueCounts, nil, meta, log)
}

func extractMissingKeys(errStr string) []string {
	raw := []string{}
	if match := missingKeysRe.FindStringSubmatch(errStr); len(match) == 2 {
		raw = append(raw, match[1])
	}
	if match := missingKeysAltRe.FindStringSubmatch(errStr); len(match) == 2 {
		raw = append(raw, match[1])
	}
	if len(raw) == 0 {
		return nil
	}
	out := []string{}
	for _, chunk := range raw {
		for _, part := range strings.FieldsFunc(chunk, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		}) {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, `"`)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func incDataQuality(stage, issue, key string) {
	metrics := Current()
	if metrics == nil {
		return
	}
	metrics.IncDataQuality(stage, issue, key)
}

func dataQualityAlertsEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("DATA_QUALITY_ALERTS_ENABLED")))
	if v == "" {
		return false
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func dataQualityAlertWebhook() string {
	val := strings.TrimSpace(os.Getenv("DATA_QUALITY_ALERT_WEBHOOK_URL"))
	if val != "" {
		return val
	}
	return strings.TrimSpace(os.Getenv("SLO_ALERT_WEBHOOK_URL"))
}

func dataQualityAlertMinInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DATA_QUALITY_ALERT_MIN_INTERVAL_SECONDS"))
	if raw == "" {
		return 5 * time.Minute
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func sendDataQualityAlert(stage string, issueCounts map[string]int, sampleErrors []string, meta map[string]any, log *logger.Logger) {
	if !dataQualityAlertsEnabled() {
		return
	}
	webhook := dataQualityAlertWebhook()
	if webhook == "" || len(issueCounts) == 0 {
		return
	}
	key := stage
	dqAlerts.mu.Lock()
	if dqAlerts.last == nil {
		dqAlerts.last = map[string]time.Time{}
	}
	last := dqAlerts.last[key]
	minInterval := dataQualityAlertMinInterval()
	if !last.IsZero() && time.Since(last) < minInterval {
		dqAlerts.mu.Unlock()
		return
	}
	dqAlerts.last[key] = time.Now()
	dqAlerts.mu.Unlock()

	payload := map[string]any{
		"title":         "Data quality issue",
		"stage":         stage,
		"issues":        issueCounts,
		"sample_errors": sampleErrors,
		"meta":          meta,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		if log != nil {
			log.Warn("data quality alert request build failed", "error", err, "stage", stage)
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if log != nil {
			log.Warn("data quality alert post failed", "error", err, "stage", stage)
		}
		return
	}
	_ = resp.Body.Close()
	if log != nil {
		log.Info("data quality alert sent", "stage", stage, "status", resp.StatusCode)
	}
}

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type rollingSum struct {
	values []float64
	idx    int
	total  float64
}

func newRollingSum(size int) *rollingSum {
	if size < 1 {
		size = 1
	}
	return &rollingSum{values: make([]float64, size)}
}

func (r *rollingSum) add(v float64) {
	r.total += v - r.values[r.idx]
	r.values[r.idx] = v
	r.idx++
	if r.idx >= len(r.values) {
		r.idx = 0
	}
}

type SLOEvaluator struct {
	metrics *Metrics
	log     *logger.Logger

	interval    time.Duration
	window      time.Duration
	windowLabel string

	apiAvailTarget          float64
	apiLatencyTarget        float64
	workerSuccessTarget     float64
	pipelineSuccessTarget   float64
	runtimeCompletionTarget float64

	apiTotal        *rollingSum
	apiError        *rollingSum
	apiGood         *rollingSum
	workerTotal     *rollingSum
	workerError     *rollingSum
	buildTotal      *rollingSum
	buildError      *rollingSum
	promptShown     *rollingSum
	promptCompleted *rollingSum

	prevApiTotal        float64
	prevApiError        float64
	prevApiGood         float64
	prevWorkerTotal     float64
	prevWorkerError     float64
	prevBuildTotal      float64
	prevBuildError      float64
	prevPromptShown     float64
	prevPromptCompleted float64

	alertWebhook     string
	alertOwner       string
	alertRunbook     string
	alertMinInterval time.Duration
	alertBurnWarn    float64
	alertBurnCrit    float64

	alertMu    sync.Mutex
	lastAlerts map[string]time.Time
}

func (m *Metrics) StartSLOEvaluator(ctx context.Context, log *logger.Logger) {
	if m == nil || !sloEnabled() {
		return
	}
	eval := newSLOEvaluator(m, log)
	if eval == nil {
		return
	}
	go eval.run(ctx)
	if log != nil {
		log.Info("SLO evaluator started", "window", eval.windowLabel, "interval", eval.interval.String())
	}
}

func newSLOEvaluator(m *Metrics, log *logger.Logger) *SLOEvaluator {
	interval := parseDurationSeconds("SLO_EVAL_INTERVAL_SECONDS", 60)
	windowHours := parseFloat("SLO_WINDOW_HOURS", 720)
	if windowHours < 1 {
		windowHours = 24
	}
	window := time.Duration(windowHours * float64(time.Hour))
	windowLabel := formatWindowLabel(window)
	size := int(window / interval)
	if size < 1 {
		size = 1
	}
	return &SLOEvaluator{
		metrics:                 m,
		log:                     log,
		interval:                interval,
		window:                  window,
		windowLabel:             windowLabel,
		apiAvailTarget:          clamp01(parseFloat("SLO_API_AVAIL_TARGET", 0.995)),
		apiLatencyTarget:        clamp01(parseFloat("SLO_API_LATENCY_TARGET", 0.95)),
		workerSuccessTarget:     clamp01(parseFloat("SLO_WORKER_SUCCESS_TARGET", 0.98)),
		pipelineSuccessTarget:   clamp01(parseFloat("SLO_PIPELINE_SUCCESS_TARGET", 0.98)),
		runtimeCompletionTarget: clamp01(parseFloat("SLO_RUNTIME_COMPLETION_TARGET", 0.7)),
		apiTotal:                newRollingSum(size),
		apiError:                newRollingSum(size),
		apiGood:                 newRollingSum(size),
		workerTotal:             newRollingSum(size),
		workerError:             newRollingSum(size),
		buildTotal:              newRollingSum(size),
		buildError:              newRollingSum(size),
		promptShown:             newRollingSum(size),
		promptCompleted:         newRollingSum(size),
		alertWebhook:            strings.TrimSpace(getEnv("SLO_ALERT_WEBHOOK_URL")),
		alertOwner:              strings.TrimSpace(getEnv("SLO_ALERT_OWNER")),
		alertRunbook:            strings.TrimSpace(getEnv("SLO_ALERT_RUNBOOK_URL")),
		alertMinInterval:        time.Duration(parseFloat("SLO_ALERT_MIN_INTERVAL_SECONDS", 900)) * time.Second,
		alertBurnWarn:           parseFloat("SLO_ALERT_BURN_RATE_WARN", 2),
		alertBurnCrit:           parseFloat("SLO_ALERT_BURN_RATE_CRIT", 10),
		lastAlerts:              map[string]time.Time{},
	}
}

func (e *SLOEvaluator) run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.evaluate()
		}
	}
}

func (e *SLOEvaluator) evaluate() {
	if e.metrics == nil {
		return
	}
	apiTotal := e.metrics.apiReqTotal.Value()
	apiError := e.metrics.apiReqError.Value()
	apiGood := e.metrics.apiReqGood.Value()
	workerTotal := e.metrics.workerTotal.Value()
	workerError := e.metrics.workerError.Value()
	buildTotal := e.metrics.buildStageTotal.Value()
	buildError := e.metrics.buildStageError.Value()
	promptShown := e.metrics.runtimePromptShown.Value()
	promptCompleted := e.metrics.runtimePromptCompleted.Value()

	apiDeltaTotal := delta(apiTotal, e.prevApiTotal)
	apiDeltaError := delta(apiError, e.prevApiError)
	apiDeltaGood := delta(apiGood, e.prevApiGood)
	workerDeltaTotal := delta(workerTotal, e.prevWorkerTotal)
	workerDeltaError := delta(workerError, e.prevWorkerError)
	buildDeltaTotal := delta(buildTotal, e.prevBuildTotal)
	buildDeltaError := delta(buildError, e.prevBuildError)
	promptDeltaShown := delta(promptShown, e.prevPromptShown)
	promptDeltaCompleted := delta(promptCompleted, e.prevPromptCompleted)

	e.prevApiTotal = apiTotal
	e.prevApiError = apiError
	e.prevApiGood = apiGood
	e.prevWorkerTotal = workerTotal
	e.prevWorkerError = workerError
	e.prevBuildTotal = buildTotal
	e.prevBuildError = buildError
	e.prevPromptShown = promptShown
	e.prevPromptCompleted = promptCompleted

	e.apiTotal.add(apiDeltaTotal)
	e.apiError.add(apiDeltaError)
	e.apiGood.add(apiDeltaGood)
	e.workerTotal.add(workerDeltaTotal)
	e.workerError.add(workerDeltaError)
	e.buildTotal.add(buildDeltaTotal)
	e.buildError.add(buildDeltaError)
	e.promptShown.add(promptDeltaShown)
	e.promptCompleted.add(promptDeltaCompleted)

	e.evalSLO("api_availability", e.apiTotal.total, e.apiError.total, e.apiAvailTarget)
	e.evalSLO("api_latency", e.apiTotal.total, e.apiTotal.total-e.apiGood.total, e.apiLatencyTarget)
	e.evalSLO("worker_success", e.workerTotal.total, e.workerError.total, e.workerSuccessTarget)
	e.evalSLO("pipeline_success", e.buildTotal.total, e.buildError.total, e.pipelineSuccessTarget)
	e.evalSLO("runtime_prompt_completion", e.promptShown.total, e.promptShown.total-e.promptCompleted.total, e.runtimeCompletionTarget)
}

func (e *SLOEvaluator) evalSLO(name string, total float64, bad float64, target float64) {
	if total <= 0 {
		e.metrics.sloCompliance.Set(1, name, e.windowLabel)
		e.metrics.sloBudget.Set(1, name, e.windowLabel)
		e.metrics.sloBurn.Set(0, name, e.windowLabel)
		return
	}
	sli := clamp01(1 - bad/total)
	burn := 0.0
	if target < 1 {
		burn = (1 - sli) / (1 - target)
	}
	budget := clamp01(1 - burn)
	e.metrics.sloCompliance.Set(sli, name, e.windowLabel)
	e.metrics.sloBudget.Set(budget, name, e.windowLabel)
	e.metrics.sloBurn.Set(burn, name, e.windowLabel)

	if e.alertWebhook == "" || e.alertOwner == "" {
		return
	}
	severity := ""
	if burn >= e.alertBurnCrit {
		severity = "critical"
	} else if burn >= e.alertBurnWarn {
		severity = "warning"
	}
	if severity == "" {
		return
	}
	key := name + ":" + severity
	e.alertMu.Lock()
	last := e.lastAlerts[key]
	if !last.IsZero() && time.Since(last) < e.alertMinInterval {
		e.alertMu.Unlock()
		return
	}
	e.lastAlerts[key] = time.Now()
	e.alertMu.Unlock()
	e.sendAlert(name, severity, sli, target, burn, budget)
}

func (e *SLOEvaluator) sendAlert(name, severity string, sli, target, burn, budget float64) {
	payload := map[string]any{
		"title":                  "SLO burn rate alert",
		"severity":               severity,
		"owner":                  e.alertOwner,
		"slo":                    name,
		"window":                 e.windowLabel,
		"sli":                    sli,
		"target":                 target,
		"burn_rate":              burn,
		"error_budget_remaining": budget,
		"runbook":                e.alertRunbook,
		"timestamp":              time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, e.alertWebhook, bytes.NewReader(body))
	if err != nil {
		if e.log != nil {
			e.log.Warn("slo alert request build failed", "error", err, "slo", name)
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if e.log != nil {
			e.log.Warn("slo alert post failed", "error", err, "slo", name)
		}
		return
	}
	_ = resp.Body.Close()
	if e.log != nil {
		e.log.Info("slo alert sent", "slo", name, "severity", severity, "status", resp.StatusCode)
	}
}

func delta(current, prev float64) float64 {
	if current < prev {
		return current
	}
	return current - prev
}

func parseDurationSeconds(key string, def int) time.Duration {
	raw := strings.TrimSpace(getEnv(key))
	if raw == "" {
		return time.Duration(def) * time.Second
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return time.Duration(def) * time.Second
}

func parseFloat(key string, def float64) float64 {
	raw := strings.TrimSpace(getEnv(key))
	if raw == "" {
		return def
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	return def
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func formatWindowLabel(window time.Duration) string {
	hours := window.Hours()
	if hours >= 24 && mathMod(hours, 24) == 0 {
		return strconv.Itoa(int(hours/24)) + "d"
	}
	if hours >= 1 {
		return strconv.Itoa(int(hours)) + "h"
	}
	return strconv.Itoa(int(window.Minutes())) + "m"
}

func mathMod(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a - float64(int(a/b))*b
}

func sloEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(getEnv("SLO_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

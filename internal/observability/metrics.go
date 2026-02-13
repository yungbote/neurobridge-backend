package observability

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Metrics struct {
	apiRequests                 *CounterVec
	apiLatency                  *HistogramVec
	apiInflight                 *Gauge
	apiReqTotal                 *Counter
	apiReqError                 *Counter
	apiReqGood                  *Counter
	llmRequests                 *CounterVec
	llmLatency                  *HistogramVec
	llmTokens                   *CounterVec
	llmCost                     *CounterVec
	dataQuality                 *CounterVec
	clientPerf                  *HistogramVec
	clientError                 *CounterVec
	activityTime                *HistogramVec
	buildStage                  *HistogramVec
	buildStageCt                *CounterVec
	buildStageTotal             *Counter
	buildStageError             *Counter
	triggerTotal                *CounterVec
	promptTotal                 *CounterVec
	runtimePromptShown          *Counter
	runtimePromptCompleted      *Counter
	runtimeProgressState        *CounterVec
	runtimeProgressConf         *HistogramVec
	qcAnswered                  *CounterVec
	experimentExposure          *CounterVec
	experimentGuardrail         *CounterVec
	engagementFunnel            *CounterVec
	costTotal                   *CounterVec
	securityEvents              *CounterVec
	convergenceReadiness        *CounterVec
	convergenceReadinessScore   *HistogramVec
	convergenceMisconActive     *HistogramVec
	convergenceCoverageDebt     *HistogramVec
	convergenceGateDecision     *CounterVec
	convergenceMisconResolution *CounterVec
	convergenceFlowRemaining    *HistogramVec
	convergenceFlowSpend        *HistogramVec
	queueDepth                  *GaugeVec
	pgStats                     *GaugeVec
	redisUp                     *Gauge
	redisPing                   *Gauge
	sloCompliance               *GaugeVec
	sloBudget                   *GaugeVec
	sloBurn                     *GaugeVec
	sloLatencyThreshold         float64
	validationLatencyThreshold  float64
	rollbackLatencyThreshold    float64
	workerTotal                 *Counter
	workerError                 *Counter

	traceAttemptedTotal *Counter
	traceWrittenTotal   *Counter
	traceFailedTotal    *Counter
	traceAttempted      *CounterVec
	traceWritten        *CounterVec
	traceFailed         *CounterVec

	structuralValidationTotal   *Counter
	structuralValidationSlow    *Counter
	structuralValidationLatency *HistogramVec

	rollbackTotal    *Counter
	rollbackSlow     *Counter
	rollbackDuration *HistogramVec
}

var (
	initOnce sync.Once
	instance *Metrics
)

func Enabled() bool {
	v := strings.TrimSpace(os.Getenv("METRICS_ENABLED"))
	if v == "" {
		return false
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func Current() *Metrics {
	return instance
}

var (
	llmTelemetryOnce      sync.Once
	llmTelemetryOn        bool
	llmCostInputPer1KUSD  float64
	llmCostOutputPer1KUSD float64
)

func llmTelemetryEnabled() bool {
	llmTelemetryOnce.Do(loadLLMTelemetryConfig)
	return llmTelemetryOn
}

func llmCostRates() (float64, float64) {
	llmTelemetryOnce.Do(loadLLMTelemetryConfig)
	return llmCostInputPer1KUSD, llmCostOutputPer1KUSD
}

func loadLLMTelemetryConfig() {
	llmTelemetryOn = parseBoolEnv("LLM_TELEMETRY_ENABLED", false)
	llmCostInputPer1KUSD = parseFloatEnv("LLM_COST_INPUT_PER_1K", 0)
	llmCostOutputPer1KUSD = parseFloatEnv("LLM_COST_OUTPUT_PER_1K", 0)
}

func parseBoolEnv(key string, fallback bool) bool {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if val == "" {
		return fallback
	}
	switch val {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseFloatEnv(key string, fallback float64) float64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return f
}

func scrapeInterval() time.Duration {
	v := strings.TrimSpace(os.Getenv("METRICS_SCRAPE_INTERVAL_SECONDS"))
	if v == "" {
		return 10 * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 10 * time.Second
	}
	return time.Duration(n) * time.Second
}

func Init(log *logger.Logger) *Metrics {
	if !Enabled() {
		return nil
	}
	initOnce.Do(func() {
		latencyThreshold := 0.5
		if v := strings.TrimSpace(os.Getenv("SLO_API_LATENCY_THRESHOLD_SECONDS")); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				latencyThreshold = f
			}
		}
		validationThreshold := 2.0
		if v := strings.TrimSpace(os.Getenv("SLO_VALIDATION_LATENCY_THRESHOLD_SECONDS")); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				validationThreshold = f
			}
		}
		rollbackThreshold := 900.0
		if v := strings.TrimSpace(os.Getenv("SLO_ROLLBACK_TIME_THRESHOLD_SECONDS")); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				rollbackThreshold = f
			}
		}
		instance = &Metrics{
			apiRequests: NewCounterVec("nb_api_requests_total", "Total API requests by method/route/status.", []string{"method", "route", "status"}),
			apiLatency: NewHistogramVec(
				"nb_api_request_duration_seconds",
				"API request latency in seconds by method/route/status.",
				[]string{"method", "route", "status"},
				[]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
			),
			apiInflight: NewGauge("nb_api_inflight_requests", "In-flight API requests."),
			apiReqTotal: NewCounter("nb_api_requests_total_all", "Total API requests (all)."),
			apiReqError: NewCounter("nb_api_requests_error_total", "Total API requests with 5xx status."),
			apiReqGood:  NewCounter("nb_api_requests_good_latency_total", "Total API requests under SLO latency threshold."),
			llmRequests: NewCounterVec("nb_llm_requests_total", "LLM requests by model/endpoint/status.", []string{"model", "endpoint", "status"}),
			llmLatency: NewHistogramVec(
				"nb_llm_request_duration_seconds",
				"LLM request latency in seconds by model/endpoint/status.",
				[]string{"model", "endpoint", "status"},
				[]float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120},
			),
			llmTokens:   NewCounterVec("nb_llm_tokens_total", "LLM tokens by model/direction.", []string{"model", "direction"}),
			llmCost:     NewCounterVec("nb_llm_cost_usd_total", "Estimated LLM cost (USD) by model/direction.", []string{"model", "direction"}),
			dataQuality: NewCounterVec("nb_data_quality_issues_total", "Data quality issues by stage/issue/key.", []string{"stage", "issue", "key"}),
			clientPerf: NewHistogramVec(
				"nb_client_perf_seconds",
				"Client performance timing by kind/name.",
				[]string{"kind", "name"},
				[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
			),
			clientError: NewCounterVec("nb_client_error_total", "Client errors by kind.", []string{"kind"}),
			activityTime: NewHistogramVec(
				"nb_worker_activity_duration_seconds",
				"Worker activity duration in seconds.",
				[]string{"activity", "job_type", "status"},
				[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
			),
			buildStage: NewHistogramVec(
				"nb_learning_build_stage_duration_seconds",
				"Learning build stage duration in seconds.",
				[]string{"pipeline", "stage", "status"},
				[]float64{1, 5, 10, 30, 60, 120, 300, 600, 1200, 1800},
			),
			buildStageCt: NewCounterVec(
				"nb_learning_build_stage_total",
				"Learning build stage count by pipeline/stage/status.",
				[]string{"pipeline", "stage", "status"},
			),
			buildStageTotal: NewCounter("nb_learning_build_stage_total_all", "Learning build stage count (all)."),
			buildStageError: NewCounter("nb_learning_build_stage_error_total", "Learning build stage count with failure status."),
			triggerTotal: NewCounterVec(
				"nb_runtime_trigger_total",
				"Runtime trigger events by trigger/event type.",
				[]string{"trigger", "event_type"},
			),
			promptTotal: NewCounterVec(
				"nb_runtime_prompt_total",
				"Runtime prompt events by type/action.",
				[]string{"type", "action"},
			),
			runtimePromptShown:     NewCounter("nb_runtime_prompt_shown_total", "Runtime prompts shown."),
			runtimePromptCompleted: NewCounter("nb_runtime_prompt_completed_total", "Runtime prompts completed."),
			runtimeProgressState:   NewCounterVec("nb_runtime_progress_state_total", "Runtime progress state signals.", []string{"state"}),
			runtimeProgressConf: NewHistogramVec(
				"nb_runtime_progress_confidence",
				"Runtime progress confidence distribution.",
				[]string{"state"},
				[]float64{0.05, 0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 0.9, 0.95, 0.99, 1},
			),
			qcAnswered: NewCounterVec(
				"nb_quick_check_answered_total",
				"Quick check answers by outcome.",
				[]string{"result"},
			),
			experimentExposure: NewCounterVec(
				"nb_experiment_exposure_total",
				"Experiment exposures by experiment/variant/source.",
				[]string{"experiment", "variant", "source"},
			),
			experimentGuardrail: NewCounterVec(
				"nb_experiment_guardrail_breach_total",
				"Experiment guardrail breaches by experiment/guardrail.",
				[]string{"experiment", "guardrail"},
			),
			engagementFunnel: NewCounterVec(
				"nb_engagement_funnel_step_total",
				"Engagement funnel step counts by funnel/step.",
				[]string{"funnel", "step"},
			),
			costTotal: NewCounterVec(
				"nb_cost_usd_total",
				"Cost telemetry (USD) by category/source.",
				[]string{"category", "source"},
			),
			securityEvents: NewCounterVec(
				"nb_security_events_total",
				"Security-related events by type.",
				[]string{"event"},
			),
			convergenceReadiness: NewCounterVec(
				"nb_convergence_readiness_total",
				"Convergence readiness outcomes by status.",
				[]string{"status"},
			),
			convergenceReadinessScore: NewHistogramVec(
				"nb_convergence_readiness_score",
				"Readiness score distribution by status.",
				[]string{"status"},
				[]float64{0.05, 0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 0.9, 0.95, 0.99, 1},
			),
			convergenceMisconActive: NewHistogramVec(
				"nb_convergence_misconceptions_active",
				"Active misconceptions per readiness snapshot.",
				[]string{},
				[]float64{0, 1, 2, 3, 5, 8, 13, 21},
			),
			convergenceCoverageDebt: NewHistogramVec(
				"nb_convergence_coverage_debt_max",
				"Max coverage debt per readiness snapshot.",
				[]string{},
				[]float64{0, 0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 0.9, 0.95, 1},
			),
			convergenceGateDecision: NewCounterVec(
				"nb_convergence_prereq_gate_total",
				"Prereq gate decisions by decision/reason.",
				[]string{"decision", "reason"},
			),
			convergenceMisconResolution: NewCounterVec(
				"nb_convergence_misconception_resolution_total",
				"Misconception resolution state transitions by status.",
				[]string{"status"},
			),
			convergenceFlowRemaining: NewHistogramVec(
				"nb_convergence_flow_budget_remaining_ratio",
				"Remaining flow budget ratio.",
				[]string{},
				[]float64{0, 0.1, 0.25, 0.4, 0.55, 0.7, 0.85, 1},
			),
			convergenceFlowSpend: NewHistogramVec(
				"nb_convergence_flow_budget_spend_ratio",
				"Flow budget spend ratio.",
				[]string{},
				[]float64{0, 0.1, 0.25, 0.4, 0.55, 0.7, 0.85, 1},
			),
			queueDepth:                 NewGaugeVec("nb_job_queue_depth", "Job queue depth by status.", []string{"status"}),
			pgStats:                    NewGaugeVec("nb_postgres_stats", "Postgres connection stats.", []string{"metric"}),
			redisUp:                    NewGauge("nb_redis_up", "Redis connectivity (1=up, 0=down)."),
			redisPing:                  NewGauge("nb_redis_ping_seconds", "Redis ping latency in seconds."),
			sloCompliance:              NewGaugeVec("nb_slo_compliance", "SLO compliance (SLI) over window.", []string{"slo", "window"}),
			sloBudget:                  NewGaugeVec("nb_slo_error_budget_remaining", "Error budget remaining (0-1).", []string{"slo", "window"}),
			sloBurn:                    NewGaugeVec("nb_slo_burn_rate", "Error budget burn rate.", []string{"slo", "window"}),
			sloLatencyThreshold:        latencyThreshold,
			validationLatencyThreshold: validationThreshold,
			rollbackLatencyThreshold:   rollbackThreshold,
			workerTotal:                NewCounter("nb_worker_activity_total", "Total worker activities."),
			workerError:                NewCounter("nb_worker_activity_error_total", "Total worker activities with failure status."),
			traceAttemptedTotal:        NewCounter("nb_trace_attempted_total", "Total trace writes attempted."),
			traceWrittenTotal:          NewCounter("nb_trace_written_total", "Total trace writes written."),
			traceFailedTotal:           NewCounter("nb_trace_failed_total", "Total trace writes failed."),
			traceAttempted:             NewCounterVec("nb_trace_attempted_by_kind_total", "Trace writes attempted by kind.", []string{"kind"}),
			traceWritten:               NewCounterVec("nb_trace_written_by_kind_total", "Trace writes written by kind.", []string{"kind"}),
			traceFailed:                NewCounterVec("nb_trace_failed_by_kind_total", "Trace writes failed by kind.", []string{"kind"}),
			structuralValidationTotal:  NewCounter("nb_structural_validation_total", "Structural invariant validations performed."),
			structuralValidationSlow:   NewCounter("nb_structural_validation_slow_total", "Structural invariant validations over latency threshold."),
			structuralValidationLatency: NewHistogramVec(
				"nb_structural_validation_duration_seconds",
				"Structural invariant validation duration in seconds.",
				[]string{"status"},
				[]float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60},
			),
			rollbackTotal: NewCounter("nb_graph_rollback_total", "Total graph rollback executions."),
			rollbackSlow:  NewCounter("nb_graph_rollback_slow_total", "Graph rollbacks over latency threshold."),
			rollbackDuration: NewHistogramVec(
				"nb_graph_rollback_duration_seconds",
				"Graph rollback duration in seconds.",
				[]string{"status"},
				[]float64{10, 30, 60, 120, 300, 600, 900, 1800, 3600},
			),
		}
		if log != nil {
			log.Info("Observability metrics enabled")
		}
	})
	return instance
}

func (m *Metrics) StartServer(ctx context.Context, log *logger.Logger, addr string) {
	if m == nil {
		return
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(m.WriteHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if log != nil {
				log.Error("metrics server failed", "error", err, "addr", addr)
			}
		}
	}()
}

func (m *Metrics) WriteHTTP(w http.ResponseWriter, r *http.Request) {
	if m == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_ = m.WritePrometheus(w)
}

func (m *Metrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		return nil
	}
	if err := m.apiRequests.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.apiLatency.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.apiInflight.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.apiReqTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.apiReqError.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.apiReqGood.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.llmRequests.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.llmLatency.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.llmTokens.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.llmCost.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.dataQuality.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.clientPerf.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.clientError.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.activityTime.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.buildStage.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.buildStageCt.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.buildStageTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.buildStageError.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.triggerTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.promptTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.runtimePromptShown.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.runtimePromptCompleted.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.runtimeProgressState.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.runtimeProgressConf.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.qcAnswered.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.experimentExposure.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.experimentGuardrail.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.engagementFunnel.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.costTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.securityEvents.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceReadiness.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceReadinessScore.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceMisconActive.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceCoverageDebt.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceGateDecision.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceMisconResolution.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceFlowRemaining.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.convergenceFlowSpend.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.queueDepth.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.pgStats.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.redisUp.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.redisPing.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.sloCompliance.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.sloBudget.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.sloBurn.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceAttemptedTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceWrittenTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceFailedTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceAttempted.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceWritten.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.traceFailed.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.structuralValidationTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.structuralValidationSlow.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.structuralValidationLatency.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.rollbackTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.rollbackSlow.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.rollbackDuration.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.workerTotal.WritePrometheus(w); err != nil {
		return err
	}
	if err := m.workerError.WritePrometheus(w); err != nil {
		return err
	}
	return nil
}

func (m *Metrics) ObserveAPI(method, route, status string, dur time.Duration) {
	if m == nil {
		return
	}
	if method == "" {
		method = "UNKNOWN"
	}
	if route == "" {
		route = "unknown"
	}
	if status == "" {
		status = "0"
	}
	m.apiRequests.Inc(method, route, status)
	m.apiLatency.Observe(dur.Seconds(), method, route, status)
	m.apiReqTotal.Inc()
	if isServerErrorStatus(status) {
		m.apiReqError.Inc()
	}
	if m.sloLatencyThreshold > 0 && dur.Seconds() <= m.sloLatencyThreshold {
		m.apiReqGood.Inc()
	}
}

func (m *Metrics) ApiInflightInc() {
	if m == nil {
		return
	}
	m.apiInflight.Inc()
}

func (m *Metrics) ApiInflightDec() {
	if m == nil {
		return
	}
	m.apiInflight.Dec()
}

func (m *Metrics) ObserveActivity(activityName, jobType, status string, dur time.Duration) {
	if m == nil {
		return
	}
	if activityName == "" {
		activityName = "unknown"
	}
	if jobType == "" {
		jobType = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	m.activityTime.Observe(dur.Seconds(), activityName, jobType, status)
	m.workerTotal.Inc()
	if isFailureStatus(status) {
		m.workerError.Inc()
	}
}

func (m *Metrics) ObserveLearningBuildStage(pipeline, stage, status string, dur time.Duration) {
	if m == nil {
		return
	}
	if pipeline == "" {
		pipeline = "unknown"
	}
	if stage == "" {
		stage = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	m.buildStageCt.Inc(pipeline, stage, status)
	m.buildStageTotal.Inc()
	if isFailureStatus(status) {
		m.buildStageError.Inc()
	}
	if dur > 0 {
		m.buildStage.Observe(dur.Seconds(), pipeline, stage, status)
	}
}

func (m *Metrics) IncTraceAttempted(kind string) {
	if m == nil {
		return
	}
	if kind == "" {
		kind = "unknown"
	}
	m.traceAttemptedTotal.Inc()
	m.traceAttempted.Inc(kind)
}

func (m *Metrics) IncTraceWritten(kind string) {
	if m == nil {
		return
	}
	if kind == "" {
		kind = "unknown"
	}
	m.traceWrittenTotal.Inc()
	m.traceWritten.Inc(kind)
}

func (m *Metrics) IncTraceFailed(kind string) {
	if m == nil {
		return
	}
	if kind == "" {
		kind = "unknown"
	}
	m.traceFailedTotal.Inc()
	m.traceFailed.Inc(kind)
}

func (m *Metrics) ObserveStructuralValidation(dur time.Duration, status string) {
	if m == nil {
		return
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "unknown"
	}
	secs := dur.Seconds()
	if secs < 0 {
		secs = 0
	}
	m.structuralValidationTotal.Inc()
	if m.validationLatencyThreshold > 0 && secs > m.validationLatencyThreshold {
		m.structuralValidationSlow.Inc()
	}
	m.structuralValidationLatency.Observe(secs, status)
}

func (m *Metrics) ObserveRollback(duration time.Duration, status string) {
	if m == nil {
		return
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "unknown"
	}
	secs := duration.Seconds()
	if secs < 0 {
		secs = 0
	}
	m.rollbackTotal.Inc()
	if m.rollbackLatencyThreshold > 0 && secs > m.rollbackLatencyThreshold {
		m.rollbackSlow.Inc()
	}
	m.rollbackDuration.Observe(secs, status)
}

func (m *Metrics) IncRuntimeTrigger(trigger, eventType string) {
	if m == nil {
		return
	}
	if trigger == "" {
		trigger = "unknown"
	}
	if eventType == "" {
		eventType = "unknown"
	}
	m.triggerTotal.Inc(trigger, eventType)
}

func (m *Metrics) IncRuntimePrompt(promptType, action string) {
	if m == nil {
		return
	}
	if promptType == "" {
		promptType = "unknown"
	}
	if action == "" {
		action = "unknown"
	}
	m.promptTotal.Inc(promptType, action)
	switch strings.ToLower(action) {
	case "shown":
		m.runtimePromptShown.Inc()
	case "completed", "answered_correct", "answered_incorrect":
		m.runtimePromptCompleted.Inc()
	}
}

func (m *Metrics) ObserveRuntimeProgress(state string, confidence float64) {
	if m == nil {
		return
	}
	state = strings.TrimSpace(state)
	if state == "" {
		state = "unknown"
	}
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	m.runtimeProgressState.Inc(state)
	m.runtimeProgressConf.Observe(confidence, state)
}

func (m *Metrics) IncQuickCheckAnswered(isCorrect bool) {
	if m == nil {
		return
	}
	result := "incorrect"
	if isCorrect {
		result = "correct"
	}
	m.qcAnswered.Inc(result)
}

func (m *Metrics) IncExperimentExposure(experiment, variant, source string) {
	if m == nil {
		return
	}
	experiment = strings.TrimSpace(experiment)
	if experiment == "" {
		experiment = "unknown"
	}
	variant = strings.TrimSpace(variant)
	if variant == "" {
		variant = "unknown"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "unknown"
	}
	m.experimentExposure.Inc(experiment, variant, source)
}

func (m *Metrics) IncExperimentGuardrail(experiment, guardrail string) {
	if m == nil {
		return
	}
	experiment = strings.TrimSpace(experiment)
	if experiment == "" {
		experiment = "unknown"
	}
	guardrail = strings.TrimSpace(guardrail)
	if guardrail == "" {
		guardrail = "unknown"
	}
	m.experimentGuardrail.Inc(experiment, guardrail)
}

func (m *Metrics) IncEngagementFunnelStep(funnel, step string) {
	if m == nil {
		return
	}
	funnel = strings.TrimSpace(funnel)
	if funnel == "" {
		funnel = "default"
	}
	step = strings.TrimSpace(step)
	if step == "" {
		step = "unknown"
	}
	m.engagementFunnel.Inc(funnel, step)
}

func (m *Metrics) AddCost(category, source string, amount float64) {
	if m == nil || amount <= 0 {
		return
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = "unknown"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "unknown"
	}
	m.costTotal.Add(amount, category, source)
}

func (m *Metrics) IncSecurityEvent(event string) {
	if m == nil {
		return
	}
	event = strings.TrimSpace(event)
	if event == "" {
		event = "unknown"
	}
	m.securityEvents.Inc(event)
}

func (m *Metrics) ObserveConvergenceReadiness(status string, score float64, coverageDebt float64, misconActive int) {
	if m == nil {
		return
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	if coverageDebt < 0 {
		coverageDebt = 0
	}
	if coverageDebt > 1 {
		coverageDebt = 1
	}
	if misconActive < 0 {
		misconActive = 0
	}
	m.convergenceReadiness.Inc(status)
	m.convergenceReadinessScore.Observe(score, status)
	m.convergenceMisconActive.Observe(float64(misconActive))
	m.convergenceCoverageDebt.Observe(coverageDebt)
}

func (m *Metrics) IncConvergenceGateDecision(decision, reason string) {
	if m == nil {
		return
	}
	decision = strings.TrimSpace(decision)
	if decision == "" {
		decision = "unknown"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	m.convergenceGateDecision.Inc(decision, reason)
}

func (m *Metrics) IncConvergenceMisconceptionResolution(status string) {
	if m == nil {
		return
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	m.convergenceMisconResolution.Inc(status)
}

func (m *Metrics) ObserveConvergenceFlowBudget(total float64, remaining float64, spend float64) {
	if m == nil {
		return
	}
	if total <= 0 {
		return
	}
	remainingRatio := remaining / total
	spendRatio := spend / total
	if remainingRatio < 0 {
		remainingRatio = 0
	}
	if remainingRatio > 1 {
		remainingRatio = 1
	}
	if spendRatio < 0 {
		spendRatio = 0
	}
	if spendRatio > 1 {
		spendRatio = 1
	}
	m.convergenceFlowRemaining.Observe(remainingRatio)
	m.convergenceFlowSpend.Observe(spendRatio)
}

func (m *Metrics) ObserveClientPerf(kind, name string, seconds float64) {
	if m == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "unknown"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	if seconds <= 0 {
		return
	}
	m.clientPerf.Observe(seconds, kind, name)
}

func (m *Metrics) IncClientError(kind string) {
	if m == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "unknown"
	}
	m.clientError.Inc(kind)
}

func (m *Metrics) ObserveLLMRequest(model, endpoint, status string, dur time.Duration, inputTokens, outputTokens int) {
	if m == nil || !llmTelemetryEnabled() {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "unknown"
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "0"
	}
	m.llmRequests.Inc(model, endpoint, status)
	if dur > 0 {
		m.llmLatency.Observe(dur.Seconds(), model, endpoint, status)
	}
	totalTokens := inputTokens + outputTokens
	if inputTokens > 0 {
		m.llmTokens.Add(float64(inputTokens), model, "input")
	}
	if outputTokens > 0 {
		m.llmTokens.Add(float64(outputTokens), model, "output")
	}
	if totalTokens > 0 {
		m.llmTokens.Add(float64(totalTokens), model, "total")
	}
	inputRate, outputRate := llmCostRates()
	if inputTokens > 0 && inputRate > 0 {
		m.llmCost.Add((float64(inputTokens)/1000.0)*inputRate, model, "input")
	}
	if outputTokens > 0 && outputRate > 0 {
		m.llmCost.Add((float64(outputTokens)/1000.0)*outputRate, model, "output")
	}
	cost := 0.0
	if inputTokens > 0 && inputRate > 0 {
		cost += (float64(inputTokens) / 1000.0) * inputRate
	}
	if outputTokens > 0 && outputRate > 0 {
		cost += (float64(outputTokens) / 1000.0) * outputRate
	}
	if cost > 0 {
		m.AddCost("llm", "openai", cost)
	}
}

func (m *Metrics) IncDataQuality(stage, issue, key string) {
	if m == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "unknown"
	}
	issue = strings.TrimSpace(issue)
	if issue == "" {
		issue = "unknown"
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "none"
	}
	m.dataQuality.Inc(stage, issue, key)
}

func (m *Metrics) StartPostgresCollector(ctx context.Context, log *logger.Logger, db *gorm.DB) {
	if m == nil || db == nil {
		return
	}
	interval := scrapeInterval()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sqlDB, err := db.DB()
				if err != nil {
					if log != nil {
						log.Warn("metrics: postgres stats unavailable", "error", err)
					}
					continue
				}
				stats := sqlDB.Stats()
				m.pgStats.Set(float64(stats.OpenConnections), "open_connections")
				m.pgStats.Set(float64(stats.InUse), "in_use")
				m.pgStats.Set(float64(stats.Idle), "idle")
				m.pgStats.Set(float64(stats.WaitCount), "wait_count")
				m.pgStats.Set(stats.WaitDuration.Seconds(), "wait_duration_seconds")
				m.pgStats.Set(float64(stats.MaxOpenConnections), "max_open_connections")
				m.pgStats.Set(float64(stats.MaxIdleClosed), "max_idle_closed")
				m.pgStats.Set(float64(stats.MaxIdleTimeClosed), "max_idle_time_closed")
				m.pgStats.Set(float64(stats.MaxLifetimeClosed), "max_lifetime_closed")
			}
		}
	}()
}

func (m *Metrics) StartRedisCollector(ctx context.Context, log *logger.Logger, addr string) {
	if m == nil {
		return
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	interval := scrapeInterval()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = rdb.Close()
				return
			case <-ticker.C:
				start := time.Now()
				if err := rdb.Ping(ctx).Err(); err != nil {
					m.redisUp.Set(0)
					if log != nil {
						log.Warn("metrics: redis ping failed", "error", err)
					}
					continue
				}
				m.redisUp.Set(1)
				m.redisPing.Set(time.Since(start).Seconds())
			}
		}
	}()
}

func (m *Metrics) StartJobQueueCollector(ctx context.Context, log *logger.Logger, db *gorm.DB) {
	if m == nil || db == nil {
		return
	}
	interval := scrapeInterval()
	statuses := []string{"queued", "running", "waiting_user", "succeeded", "failed", "canceled"}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, s := range statuses {
					m.queueDepth.Set(0, s)
				}
				var rows []struct {
					Status string
					Count  int64
				}
				if err := db.WithContext(ctx).
					Model(&types.JobRun{}).
					Select("status, count(*) as count").
					Group("status").
					Scan(&rows).Error; err != nil {
					if log != nil {
						log.Warn("metrics: job queue depth query failed", "error", err)
					}
					continue
				}
				for _, row := range rows {
					status := strings.TrimSpace(row.Status)
					if status == "" {
						status = "unknown"
					}
					m.queueDepth.Set(float64(row.Count), status)
				}
			}
		}
	}()
}

// ---- lightweight metric primitives (Prometheus exposition) ----

type CounterVec struct {
	name       string
	help       string
	labelNames []string
	mu         sync.RWMutex
	values     map[string]float64
}

func NewCounterVec(name, help string, labels []string) *CounterVec {
	return &CounterVec{name: name, help: help, labelNames: labels, values: map[string]float64{}}
}

func (c *CounterVec) Inc(values ...string) {
	if c == nil {
		return
	}
	lbl := labelString(c.labelNames, values)
	c.mu.Lock()
	c.values[lbl]++
	c.mu.Unlock()
}

func (c *CounterVec) Add(v float64, values ...string) {
	if c == nil {
		return
	}
	lbl := labelString(c.labelNames, values)
	c.mu.Lock()
	c.values[lbl] += v
	c.mu.Unlock()
}

func (c *CounterVec) WritePrometheus(w io.Writer) error {
	if c == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", c.name); err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.values {
		if _, err := fmt.Fprintf(w, "%s%s %f\n", c.name, k, v); err != nil {
			return err
		}
	}
	return nil
}

type Counter struct {
	name string
	help string
	mu   sync.RWMutex
	val  float64
}

func NewCounter(name, help string) *Counter {
	return &Counter{name: name, help: help}
}

func (c *Counter) Inc() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.val++
	c.mu.Unlock()
}

func (c *Counter) Add(v float64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.val += v
	c.mu.Unlock()
}

func (c *Counter) Value() float64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}

func (c *Counter) WritePrometheus(w io.Writer) error {
	if c == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", c.name); err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, err := fmt.Fprintf(w, "%s %f\n", c.name, c.val)
	return err
}

type Gauge struct {
	name string
	help string
	mu   sync.RWMutex
	val  float64
}

func NewGauge(name, help string) *Gauge {
	return &Gauge{name: name, help: help}
}

func (g *Gauge) Set(v float64) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.val = v
	g.mu.Unlock()
}

func (g *Gauge) Inc() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.val++
	g.mu.Unlock()
}

func (g *Gauge) Dec() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.val--
	g.mu.Unlock()
}

func (g *Gauge) WritePrometheus(w io.Writer) error {
	if g == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", g.name); err != nil {
		return err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, err := fmt.Fprintf(w, "%s %f\n", g.name, g.val)
	return err
}

type GaugeVec struct {
	name       string
	help       string
	labelNames []string
	mu         sync.RWMutex
	values     map[string]float64
}

func NewGaugeVec(name, help string, labels []string) *GaugeVec {
	return &GaugeVec{name: name, help: help, labelNames: labels, values: map[string]float64{}}
}

func (g *GaugeVec) Set(v float64, values ...string) {
	if g == nil {
		return
	}
	lbl := labelString(g.labelNames, values)
	g.mu.Lock()
	g.values[lbl] = v
	g.mu.Unlock()
}

func (g *GaugeVec) WritePrometheus(w io.Writer) error {
	if g == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", g.name); err != nil {
		return err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for k, v := range g.values {
		if _, err := fmt.Fprintf(w, "%s%s %f\n", g.name, k, v); err != nil {
			return err
		}
	}
	return nil
}

type HistogramVec struct {
	name       string
	help       string
	labelNames []string
	buckets    []float64
	mu         sync.RWMutex
	values     map[string]*histogram
}

type histogram struct {
	buckets []float64
	counts  []uint64
	sum     float64
	total   uint64
}

func NewHistogramVec(name, help string, labels []string, buckets []float64) *HistogramVec {
	if len(buckets) == 0 {
		buckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}
	}
	return &HistogramVec{name: name, help: help, labelNames: labels, buckets: buckets, values: map[string]*histogram{}}
}

func (h *HistogramVec) Observe(v float64, values ...string) {
	if h == nil {
		return
	}
	lbl := labelString(h.labelNames, values)
	h.mu.Lock()
	defer h.mu.Unlock()
	hist, ok := h.values[lbl]
	if !ok {
		hist = &histogram{
			buckets: h.buckets,
			counts:  make([]uint64, len(h.buckets)+1),
		}
		h.values[lbl] = hist
	}
	hist.sum += v
	hist.total++
	for i, b := range hist.buckets {
		if v <= b {
			hist.counts[i]++
		}
	}
	hist.counts[len(hist.counts)-1]++
}

func (h *HistogramVec) WritePrometheus(w io.Writer) error {
	if h == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s histogram\n", h.name); err != nil {
		return err
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for k, v := range h.values {
		for i, b := range v.buckets {
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, withLe(k, fmt.Sprintf("%g", b)), v.counts[i]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, withLe(k, "+Inf"), v.counts[len(v.counts)-1]); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s_sum%s %f\n", h.name, k, v.sum); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s_count%s %d\n", h.name, k, v.total); err != nil {
			return err
		}
	}
	return nil
}

func labelString(names []string, values []string) string {
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("{")
	for i, name := range names {
		if i > 0 {
			b.WriteString(",")
		}
		val := "unknown"
		if i < len(values) {
			val = values[i]
		}
		b.WriteString(name)
		b.WriteString("=\"")
		b.WriteString(escapeLabel(val))
		b.WriteString("\"")
	}
	b.WriteString("}")
	return b.String()
}

func escapeLabel(v string) string {
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func withLe(labels string, le string) string {
	le = escapeLabel(le)
	if labels == "" || labels == "{}" {
		return "{le=\"" + le + "\"}"
	}
	if strings.HasSuffix(labels, "}") {
		return strings.TrimSuffix(labels, "}") + ",le=\"" + le + "\"}"
	}
	return "{le=\"" + le + "\"}"
}

func isServerErrorStatus(status string) bool {
	status = strings.TrimSpace(status)
	if len(status) < 3 {
		return false
	}
	return status[0] == '5'
}

func isFailureStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "timeout", "panic":
		return true
	default:
		return false
	}
}

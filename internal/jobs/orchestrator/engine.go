package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

// -------------------- Public API --------------------

type RetryPolicy struct {
	MaxAttempts int
	Retryable   func(err error) bool

	MinBackoff time.Duration // default 1s
	MaxBackoff time.Duration // default 30s
	JitterFrac float64       // default 0.20
}

type Stage struct {
	Name string

	Mode         StageMode
	Timeout      time.Duration // inline only
	StartPct     int
	EndPct       int
	StartMsg     string
	DoneMsg      string
	Retry        RetryPolicy
	IsDone       func(ctx *jobrt.Context, st *OrchestratorState) (bool, error)
	Run          func(ctx *jobrt.Context, st *OrchestratorState) (map[string]any, error)
	ChildJobType string

	ChildEntity   func(ctx *jobrt.Context) (entityType string, entityID *uuid.UUID)
	ChildPayload  func(ctx *jobrt.Context, st *OrchestratorState) (map[string]any, error)
	ChildJobOwner func(ctx *jobrt.Context) uuid.UUID // default: ctx.Job.OwnerUserID
}

type ChildEnqueuer interface {
	Enqueue(ctx context.Context, tx any, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID, payload map[string]any) (*types.JobRun, error)
}

type Engine struct {
	ChildJobs ChildEnqueuer

	MinPollInterval time.Duration // default 2s
	MaxPollInterval time.Duration // default 10s

	StateVersion int // default 1
}

func NewEngine(child ChildEnqueuer) *Engine {
	return &Engine{
		ChildJobs:       child,
		MinPollInterval: 2 * time.Second,
		MaxPollInterval: 10 * time.Second,
		StateVersion:    1,
	}
}

// Run orchestrates a stage list for a single root job.
func (e *Engine) Run(ctx *jobrt.Context, stages []Stage, finalResult map[string]any) error {
	jc, st, ok := e.preflight(ctx, stages, finalResult)
	if !ok {
		return nil
	}
	if e.globalWaitGate(jc, st) {
		return nil
	}
	for i := range stages {
		def := stages[i]
		ss := st.EnsureStage(def.Name, effectiveMode(def))
		if ss.Status == StageSucceeded || ss.Status == StageSkipped {
			continue
		}
		if e.stageWaitGate(jc, st, def, ss) {
			return nil
		}
		e.startStage(jc, st, def, ss)
		switch ss.Mode {
		case ModeInline:
			if e.runInline(jc, st, def, ss) {
				return nil
			}
		case ModeChild:
			if e.runChild(jc, st, def, ss) {
				return nil
			}
		default:
			jc.Fail(def.Name, fmt.Errorf("stage %q: unknown mode %q", def.Name, ss.Mode))
			return nil
		}
	}
	e.succeed(jc, st, stages, finalResult)
	return nil
}

// -------------------- tight helpers --------------------

func (e *Engine) preflight(ctx *jobrt.Context, stages []Stage, finalResult map[string]any) (*jobrt.Context, *OrchestratorState, bool) {
	if ctx == nil || ctx.Job == nil {
		return nil, nil, false
	}
	if len(stages) == 0 {
		ctx.Succeed("done", finalResult)
		return ctx, nil, false
	}
	if err := validateStages(stages); err != nil {
		ctx.Fail("validate", err)
		return ctx, nil, false
	}
	st, _ := LoadState(ctx, e.StateVersion)
	return ctx, st, true
}

func (e *Engine) globalWaitGate(ctx *jobrt.Context, st *OrchestratorState) bool {
	if st == nil || st.WaitUntil == nil {
		return false
	}
	now := time.Now()
	if now.Before(*st.WaitUntil) {
		sleep := clampDuration(st.WaitUntil.Sub(now), e.MinPollInterval, e.MaxPollInterval)
		if sleep > 0 {
			time.Sleep(sleep)
		}
		_ = SaveState(ctx, st)
		_ = yieldToQueue(ctx, "waiting", st.LastProgress)
		return true
	}
	st.WaitUntil = nil
	_ = SaveState(ctx, st)
	return false
}

func (e *Engine) stageWaitGate(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	if ss == nil || ss.NextRunAt == nil {
		return false
	}
	if time.Now().Before(*ss.NextRunAt) {
		sleep := clampDuration(ss.NextRunAt.Sub(time.Now()), e.MinPollInterval, e.MaxPollInterval)
		if sleep > 0 {
			time.Sleep(sleep)
		}
		_ = SaveState(ctx, st)
		_ = yieldToQueue(ctx, "waiting_"+def.Name, st.LastProgress)
		return true
	}
	ss.NextRunAt = nil
	return false
}

func (e *Engine) startStage(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) {
	setProgress(ctx, st, def.Name, def.StartPct, msgOr(def.StartMsg, "Starting "+def.Name))
	ss.Status = StageRunning
	ss.Mode = effectiveMode(def)
	markStarted(ss)
	_ = SaveState(ctx, st)
}

func (e *Engine) runInline(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	if def.IsDone != nil {
		done, derr := safeIsDone(def, ctx, st)
		if derr != nil {
			e.handleStageErr(ctx, st, ss, def, derr)
			return true
		}
		if done {
			ss.Status = StageSucceeded
			markFinished(ss, "")
			setProgress(ctx, st, def.Name, def.EndPct, msgOr(def.DoneMsg, "Done "+def.Name))
			_ = SaveState(ctx, st)
			return false
		}
	}
	outs, runErr := safeRunInline(def, ctx, st)
	if runErr != nil {
		e.handleStageErr(ctx, st, ss, def, runErr)
		return true
	}
	if outs != nil {
		mergeOutputs(ss, outs)
	}
	ss.Status = StageSucceeded
	markFinished(ss, "")
	setProgress(ctx, st, def.Name, def.EndPct, msgOr(def.DoneMsg, "Done "+def.Name))
	_ = SaveState(ctx, st)
	return false
}

func (e *Engine) runChild(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	if e.ChildJobs == nil {
		ctx.Fail(def.Name, fmt.Errorf("stage %q is ModeChild but Engine.ChildJobs is nil", def.Name))
		return true
	}
	if ss.ChildJobID == "" {
		return e.enqueueChildAndYield(ctx, st, def, ss)
	}
	return e.pollChild(ctx, st, def, ss)
}

func (e *Engine) enqueueChildAndYield(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	owner := ctx.Job.OwnerUserID
	if def.ChildJobOwner != nil {
		owner = def.ChildJobOwner(ctx)
	}
	if owner == uuid.Nil {
		ctx.Fail(def.Name, fmt.Errorf("stage %q: child owner is nil", def.Name))
		return true
	}
	if strings.TrimSpace(def.ChildJobType) == "" {
		ctx.Fail(def.Name, fmt.Errorf("stage %q: missing ChildJobType", def.Name))
		return true
	}
	entityType, entityID := "", (*uuid.UUID)(nil)
	if def.ChildEntity != nil {
		entityType, entityID = def.ChildEntity(ctx)
	}
	payload := map[string]any{}
	if def.ChildPayload != nil {
		p, err := def.ChildPayload(ctx, st)
		if err != nil {
			e.handleStageErr(ctx, st, ss, def, err)
			return true
		}
		payload = p
	}
	j, err := e.ChildJobs.Enqueue(ctx.Ctx, nil, owner, def.ChildJobType, entityType, entityID, payload)
	if err != nil {
		e.handleStageErr(ctx, st, ss, def, err)
		return true
	}
	ss.ChildJobID = j.ID.String()
	ss.ChildJobType = j.JobType
	ss.ChildJobStatus = j.Status
	ss.Status = StageWaitingChild
	_ = SaveState(ctx, st)
	return e.yield(ctx, st, "waiting_child_"+def.Name, e.MinPollInterval)
}

func (e *Engine) pollChild(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	childID, err := uuid.Parse(ss.ChildJobID)
	if err != nil || childID == uuid.Nil {
		e.handleStageErr(ctx, st, ss, def, fmt.Errorf("stage %q: invalid child_job_id %q", def.Name, ss.ChildJobID))
		return true
	}
	child, err := loadJobByID(ctx, childID)
	if err != nil {
		e.handleStageErr(ctx, st, ss, def, fmt.Errorf("stage %q: load child job: %w", def.Name, err))
		return true
	}
	ss.ChildJobStatus = child.Status
	switch child.Status {
	case "succeeded":
		e.childSucceeded(ctx, st, def, ss, child)
		return false
	case "failed":
		return e.childFailed(ctx, st, def, ss, child)
	default:
		return e.childRunning(ctx, st, def, ss)
	}
}

func (e *Engine) childSucceeded(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState, child *types.JobRun) {
	if child != nil && len(child.Result) > 0 && string(child.Result) != "null" {
		var obj map[string]any
		if err := json.Unmarshal(child.Result, &obj); err == nil && obj != nil {
			mergeOutputs(ss, map[string]any{"child_result": obj})
		} else {
			ss.Outputs["child_result_parse_error"] = errString(err)
			ss.Outputs["child_result_raw"] = string(child.Result)
		}
	}
	ss.Status = StageSucceeded
	markFinished(ss, "")
	setProgress(ctx, st, def.Name, def.EndPct, msgOr(def.DoneMsg, "Done "+def.Name))
	_ = SaveState(ctx, st)
}

func (e *Engine) childFailed(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState, child *types.JobRun) bool {
	if child != nil {
		ss.LastError = child.Error
	}
	ss.ChildJobID, ss.ChildJobStatus, ss.ChildJobType = "", "", ""
	e.handleStageErr(ctx, st, ss, def, fmt.Errorf("child job failed: %s", stringsOr(ss.LastError, "unknown")))
	return true
}

func (e *Engine) childRunning(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	ss.Status = StageWaitingChild
	_ = SaveState(ctx, st)
	return e.yield(ctx, st, "waiting_child_"+def.Name, e.MinPollInterval)
}

func (e *Engine) yield(ctx *jobrt.Context, st *OrchestratorState, stage string, wait time.Duration) bool {
	if wait <= 0 {
		wait = e.MinPollInterval
	}
	st.WaitUntil = ptrTime(time.Now().Add(wait))
	_ = SaveState(ctx, st)
	_ = yieldToQueue(ctx, stage, st.LastProgress)
	return true
}

func (e *Engine) succeed(ctx *jobrt.Context, st *OrchestratorState, stages []Stage, finalResult map[string]any) {
	out := map[string]any{}
	for _, sdef := range stages {
		if ss := st.Stages[sdef.Name]; ss != nil && ss.Outputs != nil {
			out[sdef.Name] = ss.Outputs
		}
	}
	final := map[string]any{
		"orchestrator": st,
		"outputs":      out,
	}
	for k, v := range finalResult {
		final[k] = v
	}
	ctx.Succeed("done", final)
}

// -------------------- state persistence --------------------

func LoadState(ctx *jobrt.Context, version int) (*OrchestratorState, error) {
	st := &OrchestratorState{Version: version, Stages: map[string]*StageState{}, Meta: map[string]any{}}
	if ctx == nil || ctx.Job == nil {
		st.ensure()
		return st, nil
	}
	raw := ctx.Job.Result
	if len(raw) == 0 || string(raw) == "null" {
		st.ensure()
		return st, nil
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err == nil {
		if v, ok := probe["orchestrator"]; ok {
			b, _ := json.Marshal(v)
			_ = json.Unmarshal(b, st)
			st.ensure()
			return st, nil
		}
	}

	if err := json.Unmarshal(raw, st); err != nil {
		st.Meta["state_unmarshal_error"] = err.Error()
		st.ensure()
		return st, nil
	}
	st.ensure()
	return st, nil
}

func SaveState(ctx *jobrt.Context, st *OrchestratorState) error {
	if ctx == nil || ctx.Job == nil || st == nil {
		return nil
	}
	st.ensure()
	b, _ := json.Marshal(st)
	_ = ctx.Update(map[string]any{"result": datatypes.JSON(b)})
	ctx.Job.Result = datatypes.JSON(b)
	return nil
}

// -------------------- repo helpers --------------------

func loadJobByID(ctx *jobrt.Context, id uuid.UUID) (*types.JobRun, error) {
	if ctx == nil || ctx.Repo == nil {
		return nil, fmt.Errorf("missing job repo")
	}
	rows, err := ctx.Repo.GetByIDs(ctx.Ctx, nil, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, errors.New("job not found")
	}
	return rows[0], nil
}

func yieldToQueue(ctx *jobrt.Context, stage string, progress int) error {
	if ctx == nil || ctx.Job == nil || ctx.Repo == nil {
		return nil
	}
	now := time.Now()
	return ctx.Repo.UpdateFields(ctx.Ctx, nil, ctx.Job.ID, map[string]interface{}{
		"status":       "queued",
		"stage":        stage,
		"progress":     progress,
		"locked_at":    nil,
		"heartbeat_at": now,
		"updated_at":   now,
	})
}

// -------------------- stage error handling --------------------

func (e *Engine) handleStageErr(ctx *jobrt.Context, st *OrchestratorState, ss *StageState, def Stage, err error) bool {
	if ss == nil {
		return true
	}
	ss.Attempts++
	ss.LastError = errString(err)
	ss.Status = StageFailed
	markFinished(ss, ss.LastError)
	if shouldRetry(def.Retry, ss.Attempts, err) {
		delay := computeBackoff(def.Retry, ss.Attempts)
		when := time.Now().Add(delay)
		ss.NextRunAt = &when
		st.WaitUntil = &when
		_ = SaveState(ctx, st)
		_ = yieldToQueue(ctx, "retry_"+def.Name, st.LastProgress)
		return true
	}
	_ = SaveState(ctx, st)
	ctx.Fail(def.Name, err)
	return true
}

// -------------------- safety + validation --------------------

func validateStages(stages []Stage) error {
	seen := map[string]bool{}
	lastEnd := -1
	for _, s := range stages {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("stage missing Name")
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate stage name %q", s.Name)
		}
		seen[s.Name] = true
		if s.StartPct < 0 || s.StartPct > 100 || s.EndPct < 0 || s.EndPct > 100 {
			return fmt.Errorf("stage %q: progress must be 0..100", s.Name)
		}
		if s.EndPct < s.StartPct {
			return fmt.Errorf("stage %q: EndPct must be >= StartPct", s.Name)
		}
		if s.EndPct < lastEnd {
			return fmt.Errorf("stage %q: EndPct must be >= previous stage EndPct", s.Name)
		}
		lastEnd = s.EndPct
	}
	return nil
}

func effectiveMode(s Stage) StageMode {
	if strings.TrimSpace(string(s.Mode)) == "" {
		return ModeInline
	}
	return s.Mode
}

func safeIsDone(def Stage, ctx *jobrt.Context, st *OrchestratorState) (bool, error) {
	defer func() { _ = recover() }()
	return def.IsDone(ctx, st)
}

func safeRunInline(def Stage, ctx *jobrt.Context, st *OrchestratorState) (map[string]any, error) {
	if def.Run == nil {
		return nil, fmt.Errorf("stage %q: Run is nil", def.Name)
	}
	run := func() (map[string]any, error) { return def.Run(ctx, st) }
	if def.Timeout <= 0 {
		return run()
	}
	tctx, cancel := context.WithTimeout(ctx.Ctx, def.Timeout)
	defer cancel()
	tmp := *ctx
	tmp.Ctx = tctx
	type out struct {
		m map[string]any
		e error
	}
	ch := make(chan out, 1)
	go func() {
		m, e := run()
		ch <- out{m: m, e: e}
	}()
	select {
	case <-tctx.Done():
		return nil, fmt.Errorf("stage %q timed out: %w", def.Name, tctx.Err())
	case o := <-ch:
		return o.m, o.e
	}
}

// -------------------- progress + timestamps --------------------

func setProgress(ctx *jobrt.Context, st *OrchestratorState, stage string, pct int, msg string) {
	if ctx == nil || st == nil {
		return
	}
	if pct < st.LastProgress {
		pct = st.LastProgress
	} else {
		st.LastProgress = pct
	}
	ctx.Progress(stage, pct, msg)
}

func markStarted(ss *StageState) {
	if ss == nil || ss.StartedAt != nil {
		return
	}
	now := time.Now().UTC()
	ss.StartedAt = &now
}

func markFinished(ss *StageState, lastErr string) {
	if ss == nil {
		return
	}
	now := time.Now().UTC()
	ss.FinishedAt = &now
	if strings.TrimSpace(lastErr) != "" {
		ss.LastError = lastErr
	}
}

func mergeOutputs(ss *StageState, outs map[string]any) {
	if ss == nil || outs == nil {
		return
	}
	if ss.Outputs == nil {
		ss.Outputs = map[string]any{}
	}
	for k, v := range outs {
		ss.Outputs[k] = v
	}
}

// -------------------- retry/backoff --------------------

func shouldRetry(r RetryPolicy, attempts int, err error) bool {
	if r.MaxAttempts <= 0 || attempts >= r.MaxAttempts {
		return false
	}
	if r.Retryable == nil {
		return true
	}
	return r.Retryable(err)
}

func computeBackoff(r RetryPolicy, attempts int) time.Duration {
	minB := r.MinBackoff
	maxB := r.MaxBackoff
	j := r.JitterFrac
	if minB <= 0 {
		minB = 1 * time.Second
	}
	if maxB <= 0 {
		maxB = 30 * time.Second
	}
	if j <= 0 {
		j = 0.20
	}
	if attempts < 1 {
		attempts = 1
	}
	d := time.Duration(float64(minB) * math.Pow(2, float64(attempts-1)))
	if d > maxB {
		d = maxB
	}
	delta := float64(d) * j
	low := float64(d) - delta
	high := float64(d) + delta
	if low < 0 {
		low = 0
	}
	return time.Duration(low + rand.Float64()*(high-low))
}

// -------------------- misc --------------------

func clampDuration(d, minD, maxD time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if minD > 0 && d < minD {
		return minD
	}
	if maxD > 0 && d > maxD {
		return maxD
	}
	return d
}

func ptrTime(t time.Time) *time.Time { return &t }

func msgOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func stringsOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type DAGEngine struct {
	ChildJobs ChildEnqueuer

	MinPollInterval time.Duration
	MaxPollInterval time.Duration
	StateVersion    int

	ChildMaxWait      time.Duration
	ChildStaleRunning time.Duration

	ResultEncoder func(st *OrchestratorState) map[string]any
	OnFail        func(ctx *jobrt.Context, st *OrchestratorState, stage string, jobStage string, err error)
	OnSuccess     func(ctx *jobrt.Context, st *OrchestratorState) error
	IsCanceled    func(ctx *jobrt.Context) bool
}

func NewDAGEngine(child ChildEnqueuer) *DAGEngine {
	return &DAGEngine{
		ChildJobs:       child,
		MinPollInterval: 2 * time.Second,
		MaxPollInterval: 10 * time.Second,
		StateVersion:    1,
	}
}

func (e *DAGEngine) Run(ctx *jobrt.Context, stages []Stage, finalResult map[string]any, init func(st *OrchestratorState)) error {
	if ctx == nil || ctx.Job == nil {
		return nil
	}
	if len(stages) == 0 {
		ctx.Succeed("done", finalResult)
		return nil
	}
	order, err := validateDAG(stages)
	if err != nil {
		ctx.Fail("validate", err)
		return nil
	}

	st, _ := LoadState(ctx, e.StateVersion)
	if init != nil {
		init(st)
	}
	st.ensure()

	if e.IsCanceled != nil && e.IsCanceled(ctx) {
		return nil
	}

	stageByName := map[string]Stage{}
	for _, s := range stages {
		stageByName[s.Name] = s
		st.EnsureStage(s.Name, effectiveMode(s))
	}

	if e.waitGate(ctx, st, stages) {
		return nil
	}

	now := time.Now()
	var nextWait *time.Time

	for _, name := range order {
		if e.IsCanceled != nil && e.IsCanceled(ctx) {
			return nil
		}
		def := stageByName[name]
		ss := st.EnsureStage(def.Name, effectiveMode(def))

		if ss.NextRunAt != nil && time.Now().Before(*ss.NextRunAt) {
			nextWait = earliestTime(nextWait, ss.NextRunAt)
			continue
		}

		if depsFailed(def, st) {
			_ = e.failStage(ctx, st, def, ss, fmt.Errorf("dependency failed"), def.Name)
			return nil
		}
		if !depsSatisfied(def, st) {
			continue
		}

		switch ss.Status {
		case StageSucceeded, StageSkipped:
			continue
		case StageWaitingChild:
			if effectiveMode(def) != ModeChild {
				_ = e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q waiting on child but mode is %q", def.Name, ss.Mode), def.Name)
				return nil
			}
			if e.pollChild(ctx, st, def, ss) {
				return nil
			}
		case StagePending, StageRunning, StageFailed:
			if effectiveMode(def) == ModeChild {
				if ss.ChildJobID == "" {
					if e.enqueueChild(ctx, st, def, ss) {
						return nil
					}
				} else {
					if e.pollChild(ctx, st, def, ss) {
						return nil
					}
				}
				continue
			}
			if e.runInline(ctx, st, def, ss) {
				return nil
			}
		default:
			_ = e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q: unknown status %q", def.Name, ss.Status), def.Name)
			return nil
		}
	}

	if allSucceeded(st, stages) {
		if e.OnSuccess != nil {
			if err := e.OnSuccess(ctx, st); err != nil {
				_ = e.failStage(ctx, st, Stage{Name: "finalize"}, st.EnsureStage("finalize", ModeInline), err, "finalize")
				return nil
			}
		}
		res := encodeResult(st, e.ResultEncoder, finalResult)
		ctx.Succeed("done", res)
		return nil
	}

	activeName, activeStatus := chooseActiveStage(st, stages)
	msg := stageMessage(st, activeName, activeStatus)
	progress := computeProgress(st, stages)
	progress = clampProgress(st, progress)

	wait := e.MinPollInterval
	if nextWait != nil {
		wait = clampDuration(nextWait.Sub(now), e.MinPollInterval, e.MaxPollInterval)
		st.WaitUntil = ptrTime(now.Add(wait))
	} else {
		st.WaitUntil = ptrTime(now.Add(wait))
	}

	stageName := activeName
	if activeStatus == StageWaitingChild && activeName != "" {
		stageName = "waiting_child_" + activeName
	}
	if stageName == "" {
		stageName = "waiting"
	}

	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
	ctx.Progress(stageName, progress, msg)
	_ = yieldToQueue(ctx, stageName, progress)
	return nil
}

func (e *DAGEngine) waitGate(ctx *jobrt.Context, st *OrchestratorState, stages []Stage) bool {
	if st == nil || st.WaitUntil == nil {
		return false
	}
	now := time.Now()
	if now.Before(*st.WaitUntil) {
		sleep := clampDuration(st.WaitUntil.Sub(now), e.MinPollInterval, e.MaxPollInterval)
		if sleep > 0 {
			time.Sleep(sleep)
		}
		activeName, activeStatus := chooseActiveStage(st, stages)
		progress := clampProgress(st, computeProgress(st, stages))
		stageName := activeName
		if activeStatus == StageWaitingChild && activeName != "" {
			stageName = "waiting_child_" + activeName
		}
		if stageName == "" {
			stageName = "waiting"
		}
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
		_ = yieldToQueue(ctx, stageName, progress)
		return true
	}
	st.WaitUntil = nil
	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
	return false
}

func (e *DAGEngine) runInline(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	if def.IsDone != nil {
		done, derr := safeIsDone(def, ctx, st)
		if derr != nil {
			return e.failStage(ctx, st, def, ss, derr, def.Name)
		}
		if done {
			ss.Status = StageSucceeded
			markFinished(ss, "")
			_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
			return false
		}
	}

	ss.Status = StageRunning
	markStarted(ss)
	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)

	outs, runErr := safeRunInline(def, ctx, st)
	if runErr != nil {
		return e.failStage(ctx, st, def, ss, runErr, def.Name)
	}
	if outs != nil && ss.Outputs != nil {
		mergeOutputs(ss, outs)
	}
	ss.Status = StageSucceeded
	markFinished(ss, "")
	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
	return false
}

func (e *DAGEngine) enqueueChild(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	if e.ChildJobs == nil {
		return e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q is ModeChild but ChildJobs is nil", def.Name), def.Name)
	}
	owner := ctx.Job.OwnerUserID
	if def.ChildJobOwner != nil {
		owner = def.ChildJobOwner(ctx)
	}
	if owner == uuid.Nil {
		return e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q: child owner is nil", def.Name), def.Name)
	}
	if strings.TrimSpace(def.ChildJobType) == "" {
		return e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q: missing ChildJobType", def.Name), def.Name)
	}
	entityType, entityID := "", (*uuid.UUID)(nil)
	if def.ChildEntity != nil {
		entityType, entityID = def.ChildEntity(ctx)
	}
	payload := map[string]any{}
	if def.ChildPayload != nil {
		p, err := def.ChildPayload(ctx, st)
		if err != nil {
			return e.failStage(ctx, st, def, ss, err, def.Name)
		}
		payload = p
	}

	dbc := dbctx.Context{Ctx: ctx.Ctx, Tx: ctx.DB}
	j, err := e.ChildJobs.Enqueue(dbc, owner, def.ChildJobType, entityType, entityID, payload)
	if err != nil {
		return e.failStage(ctx, st, def, ss, err, def.Name)
	}

	ss.ChildJobID = j.ID.String()
	ss.ChildJobType = j.JobType
	ss.ChildJobStatus = j.Status
	ss.ChildProgress = j.Progress
	ss.ChildMessage = j.Message
	ss.Status = StageWaitingChild
	markStarted(ss)
	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
	return false
}

func (e *DAGEngine) pollChild(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState) bool {
	childID, err := uuid.Parse(ss.ChildJobID)
	if err != nil || childID == uuid.Nil {
		return e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q: invalid child_job_id %q", def.Name, ss.ChildJobID), def.Name)
	}
	child, err := loadJobByID(ctx, childID)
	if err != nil {
		return e.failStage(ctx, st, def, ss, fmt.Errorf("stage %q: load child job: %w", def.Name, err), def.Name)
	}
	ss.ChildJobStatus = child.Status
	ss.ChildProgress = child.Progress
	ss.ChildMessage = child.Message
	if ss.StartedAt == nil && child.CreatedAt.Unix() > 0 {
		t := child.CreatedAt.UTC()
		ss.StartedAt = &t
	}

	if e.ChildStaleRunning > 0 && strings.EqualFold(strings.TrimSpace(child.Status), "running") {
		stale := false
		if child.HeartbeatAt != nil && !child.HeartbeatAt.IsZero() {
			stale = time.Since(child.HeartbeatAt.UTC()) > e.ChildStaleRunning
		} else {
			stale = time.Since(child.CreatedAt.UTC()) > e.ChildStaleRunning
		}
		if stale {
			now := time.Now().UTC()
			if ctx.Repo != nil {
				_ = ctx.Repo.UpdateFields(dbctx.Context{Ctx: ctx.Ctx, Tx: ctx.DB}, childID, map[string]interface{}{
					"status":        "failed",
					"stage":         "stale_heartbeat",
					"error":         fmt.Sprintf("stale heartbeat (> %s) while running; treated as stuck by DAG", e.ChildStaleRunning.String()),
					"last_error_at": now,
					"locked_at":     nil,
					"updated_at":    now,
				})
			}
			return e.failStage(ctx, st, def, ss, fmt.Errorf("child stage %s has stale heartbeat (> %s) (child_job_id=%s)", def.Name, e.ChildStaleRunning.String(), childID.String()), "stale_"+def.Name)
		}
	}

	switch strings.ToLower(strings.TrimSpace(child.Status)) {
	case "succeeded":
		if len(child.Result) > 0 && string(child.Result) != "null" {
			var obj any
			if err := json.Unmarshal(child.Result, &obj); err == nil {
				ss.ChildResult = obj
			}
		}
		ss.Status = StageSucceeded
		markFinished(ss, "")
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
		return false
	case "failed":
		errMsg := strings.TrimSpace(child.Error)
		if errMsg == "" {
			errMsg = "child job failed"
		}
		return e.failStage(ctx, st, def, ss, fmt.Errorf("%s: %s", def.Name, errMsg), def.Name)
	case "canceled":
		// Reset to allow re-enqueue on restart.
		ss.Status = StagePending
		ss.ChildJobID = ""
		ss.ChildJobStatus = "canceled"
		ss.ChildJobType = ""
		ss.LastError = ""
		ss.StartedAt = nil
		ss.FinishedAt = nil
		st.WaitUntil = ptrTime(time.Now().Add(e.MinPollInterval))
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
		active := "waiting_child_" + def.Name
		progress := clampProgress(st, computeProgress(st, []Stage{def}))
		_ = yieldToQueue(ctx, active, progress)
		return true
	case "waiting_user":
		// Pause the parent job as well to avoid tight polling while we wait for user input.
		ss.Status = StageWaitingChild
		st.WaitUntil = nil
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)

		if ctx.Repo != nil && ctx.Job != nil && ctx.Job.ID != uuid.Nil {
			now := time.Now().UTC()
			msg := strings.TrimSpace(child.Message)
			if msg == "" {
				msg = "Waiting for your response…"
			}
			stage := "waiting_user_" + def.Name
			_, _ = ctx.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: ctx.Ctx, Tx: ctx.DB}, ctx.Job.ID, []string{"canceled"}, map[string]interface{}{
				"status":       "waiting_user",
				"stage":        stage,
				"message":      msg,
				"locked_at":    nil,
				"heartbeat_at": now,
				"updated_at":   now,
			})
			ctx.Job.Status = "waiting_user"
			ctx.Job.Stage = stage
			ctx.Job.Message = msg
			ctx.Job.LockedAt = nil
			ctx.Job.HeartbeatAt = &now
			ctx.Job.UpdatedAt = now

			// Emit a progress update so clients see the paused status promptly.
			if ctx.Notify != nil {
				ctx.Notify.JobProgress(ctx.Job.OwnerUserID, ctx.Job, stage, ctx.Job.Progress, msg)
			}
		}
		return true
	default:
		ss.Status = StageWaitingChild
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
		return false
	}
}

func (e *DAGEngine) failStage(ctx *jobrt.Context, st *OrchestratorState, def Stage, ss *StageState, err error, jobStage string) bool {
	if ss != nil {
		ss.Attempts++
		ss.LastError = errString(err)
		ss.Status = StageFailed
		markFinished(ss, ss.LastError)
	}
	if shouldRetry(def.Retry, ss.Attempts, err) {
		delay := computeBackoff(def.Retry, ss.Attempts)
		when := time.Now().Add(delay)
		ss.NextRunAt = &when
		st.WaitUntil = &when
		_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
		_ = yieldToQueue(ctx, "retry_"+def.Name, st.LastProgress)
		return true
	}
	_ = saveStateWithEncoder(ctx, st, e.ResultEncoder)
	if e.OnFail != nil {
		e.OnFail(ctx, st, def.Name, jobStage, err)
	}
	if jobStage == "" {
		jobStage = def.Name
	}
	ctx.Fail(jobStage, err)
	return true
}

func validateDAG(stages []Stage) ([]string, error) {
	if len(stages) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	index := map[string]int{}
	for i, s := range stages {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return nil, fmt.Errorf("stage missing Name")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate stage name %q", name)
		}
		seen[name] = true
		index[name] = i
	}
	for _, s := range stages {
		for _, dep := range s.Deps {
			if !seen[dep] {
				return nil, fmt.Errorf("stage %q depends on unknown stage %q", s.Name, dep)
			}
		}
	}

	// Kahn topological sort, stable by input order.
	deg := map[string]int{}
	out := map[string][]string{}
	for _, s := range stages {
		deg[s.Name] = 0
	}
	for _, s := range stages {
		for _, dep := range s.Deps {
			deg[s.Name]++
			out[dep] = append(out[dep], s.Name)
		}
	}

	order := make([]string, 0, len(stages))
	added := map[string]bool{}

	for {
		progressed := false
		for _, s := range stages {
			if added[s.Name] {
				continue
			}
			if deg[s.Name] == 0 {
				added[s.Name] = true
				order = append(order, s.Name)
				for _, n := range out[s.Name] {
					deg[n]--
				}
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	if len(order) != len(stages) {
		return nil, fmt.Errorf("cycle detected in stage graph")
	}
	// Ensure deterministic order if any stage list was out of order.
	_ = index
	return order, nil
}

func depsSatisfied(def Stage, st *OrchestratorState) bool {
	if len(def.Deps) == 0 {
		return true
	}
	for _, dep := range def.Deps {
		ss := st.Stages[dep]
		if ss == nil {
			return false
		}
		if ss.Status != StageSucceeded && ss.Status != StageSkipped {
			return false
		}
	}
	return true
}

func depsFailed(def Stage, st *OrchestratorState) bool {
	if len(def.Deps) == 0 {
		return false
	}
	for _, dep := range def.Deps {
		ss := st.Stages[dep]
		if ss == nil {
			continue
		}
		if ss.Status == StageFailed {
			return true
		}
	}
	return false
}

func allSucceeded(st *OrchestratorState, stages []Stage) bool {
	for _, s := range stages {
		ss := st.Stages[s.Name]
		if ss == nil {
			return false
		}
		if ss.Status != StageSucceeded && ss.Status != StageSkipped {
			return false
		}
	}
	return true
}

func chooseActiveStage(st *OrchestratorState, stages []Stage) (string, StageStatus) {
	for _, s := range stages {
		ss := st.Stages[s.Name]
		if ss == nil {
			continue
		}
		if ss.Status == StageWaitingChild || ss.Status == StageRunning {
			return s.Name, ss.Status
		}
	}
	for _, s := range stages {
		ss := st.Stages[s.Name]
		if ss == nil {
			continue
		}
		if ss.Status == StagePending || ss.Status == StageFailed {
			return s.Name, ss.Status
		}
	}
	return "", StagePending
}

func stageMessage(st *OrchestratorState, stage string, status StageStatus) string {
	if stage == "" || st == nil {
		return ""
	}
	ss := st.Stages[stage]
	if ss == nil {
		return ""
	}
	if status == StageWaitingChild {
		if strings.TrimSpace(ss.ChildMessage) != "" {
			return ss.ChildMessage
		}
		return "Running " + stage
	}
	if status == StageRunning {
		return "Running " + stage
	}
	return ""
}

func computeProgress(st *OrchestratorState, stages []Stage) int {
	if st == nil || len(stages) == 0 {
		return 0
	}
	var sum float64
	for _, s := range stages {
		ss := st.Stages[s.Name]
		if ss == nil {
			continue
		}
		sum += stageProgress(ss)
	}
	pct := int((sum / float64(len(stages))) * 100)
	if pct < 0 {
		return 0
	}
	if pct > 99 {
		return 99
	}
	return pct
}

func stageProgress(ss *StageState) float64 {
	if ss == nil {
		return 0
	}
	switch ss.Status {
	case StageSucceeded, StageSkipped:
		return 1
	case StageWaitingChild:
		if ss.ChildProgress > 0 {
			if ss.ChildProgress > 100 {
				return 1
			}
			return float64(ss.ChildProgress) / 100.0
		}
		return 0
	default:
		return 0
	}
}

func clampProgress(st *OrchestratorState, pct int) int {
	if st == nil {
		return pct
	}
	if pct < st.LastProgress {
		return st.LastProgress
	}
	st.LastProgress = pct
	return pct
}

func encodeResult(st *OrchestratorState, encoder func(*OrchestratorState) map[string]any, extra map[string]any) map[string]any {
	base := EncodeState(st)
	if encoder != nil {
		base = encoder(st)
	}
	if extra == nil {
		return base
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func EncodeState(st *OrchestratorState) map[string]any {
	if st == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"version":       st.Version,
		"stages":        st.Stages,
		"wait_until":    st.WaitUntil,
		"last_progress": st.LastProgress,
		"meta":          st.Meta,
	}
	return out
}

func EncodeFlatState(st *OrchestratorState) map[string]any {
	out := EncodeState(st)
	delete(out, "meta")
	if st != nil && st.Meta != nil {
		for k, v := range st.Meta {
			out[k] = v
		}
	}
	return out
}

func saveStateWithEncoder(ctx *jobrt.Context, st *OrchestratorState, encoder func(*OrchestratorState) map[string]any) error {
	if ctx == nil || ctx.Job == nil || st == nil {
		return nil
	}
	res := EncodeState(st)
	if encoder != nil {
		res = encoder(st)
	}
	b, _ := json.Marshal(res)
	if err := ctx.Update(map[string]any{"result": datatypes.JSON(b)}); err != nil {
		return err
	}
	ctx.Job.Result = datatypes.JSON(b)
	return nil
}

func earliestTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.Before(*a) {
		return b
	}
	return a
}

// Ensure the compiler doesn't drop unused imports when building helper types.
var _ = types.JobRun{}

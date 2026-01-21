package waitpoint

import (
	"encoding/json"
	"fmt"
	"strings"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

// Interpreter runs the config-defined classifier + reducer.
type Interpreter struct {
	Reg		*Registry
}

func NewInterpreter(reg *Registry) *Interpreter {
	return &Interpreter{Reg: reg}
}

// Run executes one interpretation cycle.
// It does NOT write to DB itself; the pipeline should apply Decision (post message/resume).
func (it *Interpreter) Run(ic *InterpreterContext) (Decision, ClassifierResult, error) {
	if it == nil || it.Reg == nil {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, fmt.Errorf("missing registry")
	}
	if ic == nil || ic.Envelope == nil {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, fmt.Errorf("missing envelope")
	}
	kind := strings.TrimSpace(ic.Envelope.Waitpoint.Kind)
	if kind == "" {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, fmt.Errorf("missing waitpoint.kind")
	}

	cfg, ok := it.Reg.Get(kind)
	if !ok {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, fmt.Errorf("no waitpoint config registered for kind=%s", kind)
	}

	// Build prompt + schema.
	system, user, schemaName, schema, err := cfg.BuildClassifierPrompt(ic)
	if err != nil {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, err
	}

	// Call model to get JSON.
	if ic.AI == nil {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, fmt.Errorf("missing AI client")
	}

	obj, err := ic.AI.GenerateJSON(ic.Ctx, system, user, schemaName, schema)
	if err != nil {
		return Decision{Kind: DecisionNoop}, ClassifierResult{Case: CaseUnknown}, err
	}

	// Marshal/unmarshal into typed classifier result for stability.
	b, _ := json.Marshal(obj)
	var cr ClassifierResult
	_ = json.Unmarshal(b, &cr)

	// Defensive deaults.
	if cr.Case == "" {
		cr.Case = CaseUnknown
	}
	if cr.Confidence < 0 {
		cr.Confidence = 0
	}
	if cr.Confidence > 1 {
		cr.Confidence = 1
	}

	// Reduce to decision.
	dec, err := cfg.Reduce(ic, cr)
	if err != nil {
		return Decision{Kind: DecisionNoop}, cr, err
	}

	// Ensure envelope state is updated by caller (pipeline) using jobrt.WaitpointEnvelope.
	_ = jobrt.WaitpointEnvelope{}	// keep import stable if unused in some builds

	return dec, cr, nil
}











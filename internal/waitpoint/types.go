package waitpoint

import (
	"context"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

// Case is the classifier output bucket.
// Keep small and explicit.
type Case string

const (
	CaseNotCommit		Case = "no_commit"
	CaseAmbiguousCommit	Case = "ambiguous_commit"
	CaseCommitted		Case = "committed"
	CaseUnknown			Case = "unknown"
)

type DecisionKind string

const (
	DecisionContinueChat	DecisionKind = "continue_chat"
	DecisionAskClarify		DecisionKind = "ask_clarification"
	DecisionConfirmResume	DecisionKind = "confirm_and_resume"
	DecisionNoop			DecisionKind = "noop"
)

type Decision struct {
	Kind				DecisionKind
	AssistantMessage	string			// If AskClarify: assistant message content to post.
	ConfirmMessage		string			// If ConfirmResume: assitant message content to post.
	Selection			map[string]any	// Optional structured selection (domain-specific; config must understand it).
	EnqueueChatRespond	bool			// Whether to enqueue a normal chat respond job to 'keep chatting'.
}

// InterpreterContext is what configs operate on.
// This is the durable state machine 'frame'.
type InterpreterContext struct {
	Ctx			context.Context

	// DB-level objects
	UserID		uuid.UUID
	ThreadID	uuid.UUID

	Thread		*types.ChatThread

	UserMessage	*types.ChatMessage		// New user message that triggered interpretation

	// Paused jobs
	ParentJob	*types.JobRun			// learning_build orchestrator
	ChildJob	*types.JobRun			// paused stage job (path_intake, etc.)

	// Decoded waitpoint envelope from ChildJob.Result
	Envelope	*jobrt.WaitpointEnvelope
	
	// Full recent messages (optional, config decides how to use them)
	Messages	[]*types.ChatMessage

	// Shared AI client for classification
	AI			openai.Client
}

// ClassifierResult is the JSON result from the LLM.
// Configs should keep this stable and minimal.
type ClassifierResult struct {
	Case            Case        `json:"case"`
	Selected        string      `json:"selected_mode,omitempty"`
	Confidence      float64     `json:"confidence,omitempty"`
	Reason          string      `json:"reason,omitempty"`
	ClarifyPrompt   string      `json:"clarifying_prompt,omitempty"`
	BestGuess       string      `json:"best_guess,omitempty"`
	CommitType      string      `json:"commit_type,omitempty"` // "confirm" | "change"

	// Domain separation fields (new simplified model)
	ConfirmedAction string      `json:"confirmed_action,omitempty"` // "separate" or "combine"
	Structure       string      `json:"structure,omitempty"`        // backwards compat
	Paths           any         `json:"paths,omitempty"`            // backwards compat
}

// Config is registered per waitpoint kind (envelope.waitpoint.kind).
type Config struct {
	Kind					string
	
	// BuildClassifierPrompt returns system/user strings and schema for GenerateJSON.
	BuildClassifierPrompt	func(ic *InterpreterContext) (system string, user string, schemaName string, schema map[string]any, err error)

	// Reduce maps a classifier result -> an execution decision
	Reduce					func(ic *InterpreterContext, cr ClassifierResult) (Decision, error)

	// ApplySelection is called before resume when DecisionConfirmResume is chosen.
	// It should apply domain updates (e.g., set path metadata selection mode).
	// Must be idempotent under retries.
	ApplySelection			func(ic *InterpreterContext, selection map[string]any) error
}










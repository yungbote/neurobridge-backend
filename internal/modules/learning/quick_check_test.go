package learning

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

func TestFindQuickCheckBlockByID_FindsPromptAndAnswer(t *testing.T) {
	id := "quick_check_123"
	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "t",
		Blocks: []map[string]any{
			{"type": "paragraph", "id": "p1", "md": "x"},
			{"type": "quick_check", "id": id, "prompt_md": "Q?", "answer_md": "A."},
		},
	}

	qc, ok := findQuickCheckBlockByID(doc, id)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if qc.PromptMD != "Q?" || qc.AnswerMD != "A." {
		t.Fatalf("unexpected prompt/answer: %q / %q", qc.PromptMD, qc.AnswerMD)
	}
}

func TestFindQuickCheckBlockByID_RejectsMissingFields(t *testing.T) {
	id := "qc"
	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "t",
		Blocks: []map[string]any{
			{"type": "quick_check", "id": id, "prompt_md": "Q?", "answer_md": ""},
		},
	}
	_, ok := findQuickCheckBlockByID(doc, id)
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestCoerceQuickCheckGradeResult_NormalizesVerdictAndCorrectness(t *testing.T) {
	out := coerceQuickCheckGradeResult(map[string]any{
		"verdict":     "Correct",
		"is_correct":  false,
		"feedback_md": " Nice ",
		"hint_md":     "",
		"confidence":  0.9,
	})
	if out.Status != "correct" {
		t.Fatalf("expected status=correct got %q", out.Status)
	}
	if !out.IsCorrect {
		t.Fatalf("expected is_correct=true")
	}
	if out.FeedbackMD != "Nice" {
		t.Fatalf("expected trimmed feedback, got %q", out.FeedbackMD)
	}
}

func TestCoerceQuickCheckGradeResult_HintMovesFeedbackWhenHintEmpty(t *testing.T) {
	out := coerceQuickCheckGradeResult(map[string]any{
		"verdict":     "hint",
		"is_correct":  false,
		"feedback_md": "Try focusing on X.",
		"hint_md":     "",
		"confidence":  0.5,
	})
	if out.Status != "hint" {
		t.Fatalf("expected status=hint got %q", out.Status)
	}
	if out.HintMD == "" || out.FeedbackMD != "" {
		t.Fatalf("expected hint_md set and feedback_md empty, got hint=%q feedback=%q", out.HintMD, out.FeedbackMD)
	}
}

func TestRunQuickCheckGrader_ParsesModelJSON(t *testing.T) {
	fake := &fakeQuickCheckAI{
		resp: map[string]any{
			"verdict":     "try_again",
			"is_correct":  false,
			"feedback_md": "Close.",
			"hint_md":     "Check the sign.",
			"confidence":  0.7,
		},
	}

	got, err := runQuickCheckGrader(context.Background(), fake, quickCheckGradeInput{
		Action:            "submit",
		PathNodeTitle:     "Unit",
		QuestionPromptMD:  "What is 1+1?",
		ReferenceAnswerMD: "2",
		StudentAnswer:     "3",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !fake.called {
		t.Fatalf("expected ai called")
	}
	if fake.schemaName != "quick_check_grade_v1" {
		t.Fatalf("unexpected schemaName: %q", fake.schemaName)
	}
	if got.Status != "try_again" || got.IsCorrect {
		t.Fatalf("unexpected result: %#v", got)
	}
	if got.HintMD == "" || got.FeedbackMD == "" {
		t.Fatalf("expected feedback+hint, got %#v", got)
	}
	if !strings.Contains(fake.user, "QUESTION_PROMPT_MD:") {
		t.Fatalf("expected prompt in user payload")
	}
}

func TestClampText_AddsEllipsis(t *testing.T) {
	s := strings.Repeat("a", 20)
	out := clampText(s, 10)
	if len([]rune(out)) < 10 || !strings.HasSuffix(out, "…") {
		t.Fatalf("unexpected clamp output: %q", out)
	}
}

type fakeQuickCheckAI struct {
	called     bool
	schemaName string
	system     string
	user       string
	schema     map[string]any
	resp       map[string]any
	err        error
}

func (f *fakeQuickCheckAI) GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error) {
	_ = ctx
	f.called = true
	f.system = system
	f.user = user
	f.schemaName = schemaName
	f.schema = schema
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]any{}
	for k, v := range f.resp {
		out[k] = v
	}
	return out, nil
}

func TestAnyString_ReturnsStringForUUID(t *testing.T) {
	id := uuid.New()
	if got := anyString(id); got != id.String() {
		t.Fatalf("unexpected anyString for uuid: %q", got)
	}
}

func TestGradeChoiceQuickCheck_SubmitCorrectReturnsCorrect(t *testing.T) {
	qc := quickCheckBlock{
		Kind:     "mcq",
		PromptMD: "Q?",
		AnswerMD: "Because B matches the definition.",
		Options: []content.DrillQuestionOptionV1{
			{ID: "A", Text: "Option A"},
			{ID: "B", Text: "Option B"},
			{ID: "C", Text: "Option C"},
		},
		AnswerID: "B",
	}
	out := gradeChoiceQuickCheck(qc, "submit", "B")
	if out.Status != "correct" || !out.IsCorrect {
		t.Fatalf("unexpected result: %#v", out)
	}
	if out.FeedbackMD == "" {
		t.Fatalf("expected feedback")
	}
}

func TestGradeChoiceQuickCheck_SubmitWrongReturnsTryAgain(t *testing.T) {
	qc := quickCheckBlock{
		Kind:     "mcq",
		PromptMD: "Q?",
		AnswerMD: "Explanation.",
		Options: []content.DrillQuestionOptionV1{
			{ID: "A", Text: "Option A"},
			{ID: "B", Text: "Option B"},
		},
		AnswerID: "B",
	}
	out := gradeChoiceQuickCheck(qc, "submit", "A")
	if out.Status != "try_again" || out.IsCorrect {
		t.Fatalf("unexpected result: %#v", out)
	}
	if out.HintMD == "" {
		t.Fatalf("expected hint")
	}
}

func TestGradeChoiceQuickCheck_HintActionReturnsHint(t *testing.T) {
	qc := quickCheckBlock{Kind: "mcq", PromptMD: "Q?", AnswerMD: "Explanation."}
	out := gradeChoiceQuickCheck(qc, "hint", "")
	if out.Status != "hint" || out.IsCorrect {
		t.Fatalf("unexpected result: %#v", out)
	}
	if out.HintMD == "" {
		t.Fatalf("expected hint_md")
	}
}

package services

import "testing"

func TestMatchWorkflowV1Action_PathIntakeChooseStructure(t *testing.T) {
	wf := &workflowV1Meta{
		Version:  1,
		Kind:     "path_intake",
		Step:     "choose_structure",
		Blocking: true,
		Actions: []workflowV1ActionMeta{
			{ID: "structure_single_path", Label: "1 · One path", Token: "1"},
			{ID: "structure_program_with_subpaths", Label: "2 · Split tracks", Token: "2"},
			{ID: "make_reasonable_assumptions", Label: "Assume & proceed", Token: "Make reasonable assumptions"},
		},
	}

	cases := []struct {
		name     string
		content  string
		wantID   string
		wantOkay bool
	}{
		{name: "numeric_with_punctuation", content: "2. no deadline, beginner", wantID: "structure_program_with_subpaths", wantOkay: true},
		{name: "hash_numeric", content: "#1", wantID: "structure_single_path", wantOkay: true},
		{name: "make_assumptions", content: "Make reasonable assumptions", wantID: "make_reasonable_assumptions", wantOkay: true},
		{name: "accept_split_freeform", content: "split is fine", wantID: "structure_program_with_subpaths", wantOkay: true},
		{name: "discussion_should_not_match", content: "what are the recommended options here", wantID: "", wantOkay: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchWorkflowV1Action(wf, tc.content)
			if ok != tc.wantOkay {
				t.Fatalf("matchWorkflowV1Action ok=%v, want %v (action=%+v)", ok, tc.wantOkay, got)
			}
			if tc.wantOkay && got.ID != tc.wantID {
				t.Fatalf("matchWorkflowV1Action id=%q, want %q", got.ID, tc.wantID)
			}
		})
	}
}

func TestMatchWorkflowV1Action_PathIntakeConfirmSeparatePaths(t *testing.T) {
	wf := &workflowV1Meta{
		Version:  1,
		Kind:     "path_intake",
		Step:     "confirm_separate_paths",
		Blocking: true,
		Actions: []workflowV1ActionMeta{
			{ID: "confirm_separate_paths", Label: "Confirm split", Token: "confirm"},
			{ID: "keep_together", Label: "Keep together", Token: "keep together"},
		},
	}

	cases := []struct {
		name     string
		content  string
		wantID   string
		wantOkay bool
	}{
		{name: "confirm_with_details", content: "confirm. no deadline. beginner", wantID: "confirm_separate_paths", wantOkay: true},
		{name: "keep_together_variant", content: "keep it together", wantID: "keep_together", wantOkay: true},
		{name: "short_affirmation", content: "sounds good", wantID: "confirm_separate_paths", wantOkay: true},
		{name: "question_should_not_match", content: "can you explain the options?", wantID: "", wantOkay: false},
		{name: "explicit_no_should_not_match", content: "no", wantID: "", wantOkay: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchWorkflowV1Action(wf, tc.content)
			if ok != tc.wantOkay {
				t.Fatalf("matchWorkflowV1Action ok=%v, want %v (action=%+v)", ok, tc.wantOkay, got)
			}
			if tc.wantOkay && got.ID != tc.wantID {
				t.Fatalf("matchWorkflowV1Action id=%q, want %q", got.ID, tc.wantID)
			}
		})
	}
}

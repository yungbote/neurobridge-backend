package services

import "testing"

func TestMatchWorkflowV1Action_PathIntakeConfirmPaths(t *testing.T) {
	wf := &workflowV1Meta{
		Version:  1,
		Kind:     "path_intake",
		Step:     "confirm_paths",
		Blocking: true,
		Actions: []workflowV1ActionMeta{
			{ID: "confirm_paths", Label: "Confirm paths", Token: "confirm"},
			{ID: "change_paths", Label: "Change grouping", Token: "change grouping"},
		},
	}

	cases := []struct {
		name     string
		content  string
		wantID   string
		wantOkay bool
	}{
		{name: "confirm_with_details", content: "confirm. no deadline, beginner", wantID: "confirm_paths", wantOkay: true},
		{name: "change_grouping_token", content: "change grouping", wantID: "change_paths", wantOkay: true},
		{name: "short_affirmation", content: "sounds good", wantID: "confirm_paths", wantOkay: true},
		{name: "discussion_should_not_match", content: "what are the recommended options here", wantID: "", wantOkay: false},
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

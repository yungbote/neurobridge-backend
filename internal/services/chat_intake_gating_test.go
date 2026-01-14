package services

import "testing"

func TestIsLikelyStructureSelectionMessage(t *testing.T) {
	cases := []struct {
		name    string
		msg     string
		want    bool
	}{
		{
			name: "confirm_with_details",
			msg:  "confirm. I have no deadline. I am a beginner",
			want: true,
		},
		{
			name: "confirm_exact",
			msg:  "confirm",
			want: true,
		},
		{
			name: "confirm_question_should_not_resume",
			msg:  "can you confirm what the recommended options are",
			want: false,
		},
		{
			name: "recommended_options_question",
			msg:  "what are the recommended options here",
			want: false,
		},
		{
			name: "split_is_fine",
			msg:  "split is fine",
			want: true,
		},
		{
			name: "number_with_punctuation",
			msg:  "2. no deadline, beginner",
			want: true,
		},
		{
			name: "keep_together_with_details",
			msg:  "keep together. no deadline",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLikelyStructureSelectionMessage(tc.msg)
			if got != tc.want {
				t.Fatalf("isLikelyStructureSelectionMessage(%q)=%v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}


package materials

type Segment struct {
	Text string `json:"text"`
	// Document provenance
	Page *int `json:"page,omitempty"`
	// Audio/video provenance
	StartSec *float64 `json:"start_sec,omitempty"`
	EndSec   *float64 `json:"end_sec,omitempty"`
	// Speaker diarization (speech/video)
	SpeakerTag *int `json:"speaker_tag,omitempty"`
	// Confidence when providers return it
	Confidence *float64       `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func PtrFloat(v float64) *float64 { return &v }











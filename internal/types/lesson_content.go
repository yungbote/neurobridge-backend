package types

// Pure JSON contract for lesson content. Not a DB model.

type StyleSpec struct {
	Tone					string				`json:"tone"`
	ReadingLevel	string				`json:"reading_level"`
	Verbosity			string				`json:"verbosity"`
	FormatBias		string				`json:"format_bias,omitempty"`
	Voice					string				`json:"voice,omitempty"`
}

type LessonBlock struct {
	Kind      string   `json:"kind"`                 // heading|paragraph|bullets|steps|callout|divider|image|video_embed
	ContentMD string   `json:"content_md,omitempty"` // for heading/paragraph/callout
	Items     []string `json:"items,omitempty"`      // bullets/steps
	AssetRefs []string `json:"asset_refs,omitempty"` // optional: ids/keys
}


type LessonContentV1 struct {
	Version          int          `json:"version"`
	StyleUsed        StyleSpec    `json:"style_used"`
	Blocks           []LessonBlock `json:"blocks"`
	Citations        []string     `json:"citations"`
	EstimatedMinutes int          `json:"estimated_minutes"`
}











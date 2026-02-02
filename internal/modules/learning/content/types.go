package content

type CitationLocV1 struct {
	Page  int `json:"page"`
	Start int `json:"start"`
	End   int `json:"end"`
}

type CitationRefV1 struct {
	ChunkID string        `json:"chunk_id"`
	Quote   string        `json:"quote"`
	Loc     CitationLocV1 `json:"loc"`
}

type MediaRefV1 struct {
	URL            string `json:"url"`
	MaterialFileID string `json:"material_file_id"`
	StorageKey     string `json:"storage_key"`
	MimeType       string `json:"mime_type"`
	FileName       string `json:"file_name"`
	Source         string `json:"source"`
}

type NodeDocHeadingBlockV1 struct {
	Type  string `json:"type"` // heading
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type NodeDocParagraphBlockV1 struct {
	Type      string          `json:"type"` // paragraph
	MD        string          `json:"md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocCalloutBlockV1 struct {
	Type      string          `json:"type"` // callout
	Variant   string          `json:"variant"`
	Title     string          `json:"title"`
	MD        string          `json:"md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocCodeBlockV1 struct {
	Type     string `json:"type"` // code
	Language string `json:"language"`
	Filename string `json:"filename"`
	Code     string `json:"code"`
}

type NodeDocFigureBlockV1 struct {
	Type      string          `json:"type"` // figure
	Asset     MediaRefV1      `json:"asset"`
	Caption   string          `json:"caption"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocVideoBlockV1 struct {
	Type     string  `json:"type"` // video
	URL      string  `json:"url"`
	StartSec float64 `json:"start_sec"`
	Caption  string  `json:"caption"`
}

type NodeDocDiagramBlockV1 struct {
	Type    string `json:"type"` // diagram
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	Caption string `json:"caption"`
}

type NodeDocTableBlockV1 struct {
	Type    string     `json:"type"` // table
	Caption string     `json:"caption"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type NodeDocQuickCheckBlockV1 struct {
	Type      string          `json:"type"` // quick_check
	PromptMD  string          `json:"prompt_md"`
	AnswerMD  string          `json:"answer_md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocFlashcardBlockV1 struct {
	Type        string          `json:"type"` // flashcard
	FrontMD     string          `json:"front_md"`
	BackMD      string          `json:"back_md"`
	ConceptKeys []string        `json:"concept_keys,omitempty"`
	Citations   []CitationRefV1 `json:"citations"`
}

type NodeDocDividerBlockV1 struct {
	Type string `json:"type"` // divider
}

// NodeDocBlockV1 is a tagged union of all supported block shapes.
// Unmarshal via json.RawMessage by inspecting `type`.
type NodeDocBlockV1 interface{}

type NodeDocV1 struct {
	SchemaVersion    int              `json:"schema_version"`
	Title            string           `json:"title"`
	Summary          string           `json:"summary"`
	ConceptKeys      []string         `json:"concept_keys"`
	EstimatedMinutes int              `json:"estimated_minutes"`
	Blocks           []map[string]any `json:"blocks"`
}

type NodeDocOutlineSectionV1 struct {
	Heading              string   `json:"heading"`
	Goal                 string   `json:"goal"`
	ConceptKeys          []string `json:"concept_keys"`
	IncludeWorkedExample bool     `json:"include_worked_example"`
	IncludeMediaBlock    bool     `json:"include_media_block"`
	QuickChecks          int      `json:"quick_checks"`
	Flashcards           int      `json:"flashcards"`
	BridgeIn             string   `json:"bridge_in"`
	BridgeOut            string   `json:"bridge_out"`
}

type NodeDocOutlineV1 struct {
	SchemaVersion int                       `json:"schema_version"`
	Title         string                    `json:"title"`
	ThreadSummary string                    `json:"thread_summary"`
	KeyTerms      []string                  `json:"key_terms"`
	PrereqRecap   string                    `json:"prereq_recap"`
	NextPreview   string                    `json:"next_preview"`
	Sections      []NodeDocOutlineSectionV1 `json:"sections"`
}

type DrillPayloadV1 struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"` // flashcards|quiz

	Cards     []DrillFlashcardV1 `json:"cards"`
	Questions []DrillQuestionV1  `json:"questions"`
}

type DrillFlashcardV1 struct {
	FrontMD     string          `json:"front_md"`
	BackMD      string          `json:"back_md"`
	ConceptKeys []string        `json:"concept_keys,omitempty"`
	Citations   []CitationRefV1 `json:"citations"`
}

type DrillQuestionOptionV1 struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type DrillQuestionV1 struct {
	ID            string                  `json:"id"`
	ConceptKeys   []string                `json:"concept_keys,omitempty"`
	PromptMD      string                  `json:"prompt_md"`
	Options       []DrillQuestionOptionV1 `json:"options"`
	AnswerID      string                  `json:"answer_id"`
	ExplanationMD string                  `json:"explanation_md"`
	Citations     []CitationRefV1         `json:"citations"`
}

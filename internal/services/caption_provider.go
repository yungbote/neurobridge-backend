package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/logger"
)

type CaptionProviderService interface {
	// DescribeImage returns structured notes for diagrams/images/frames.
	// Provide exactly ONE of:
	// - ImageURL (http(s)://... or data:image/...;base64,...)
	// - ImageBytes + ImageMime (image/png, image/jpeg, etc.)
	DescribeImage(ctx context.Context, req CaptionRequest) (*CaptionResult, error)
}

type CaptionRequest struct {
	Task        string // "figure_notes" | "image_notes" | "frame_notes"
	Prompt      string // extra instructions (optional)
	ImageURL    string
	ImageBytes  []byte
	ImageMime   string
	Detail      string // "low"|"high" (optional; model-dependent)
	MaxTokens   int
}

type CaptionResult struct {
	Task        string   `json:"task"`
	Summary     string   `json:"summary"`
	KeyTakeaways []string `json:"key_takeaways"`
	Entities    []string `json:"entities"`
	Relationships []string `json:"relationships"`
	TextInImage []string `json:"text_in_image"`
	Warnings    []string `json:"warnings,omitempty"`
}

type captionProviderService struct {
	log    *logger.Logger
	openai OpenAIClient

	model string
}

func NewCaptionProviderService(log *logger.Logger, openai OpenAIClient) (CaptionProviderService, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	if openai == nil {
		return nil, fmt.Errorf("OpenAI client required")
	}
	return &captionProviderService{
		log:   log.With("service", "CaptionProviderService"),
		openai: openai,
		model:  "", // OpenAIClient already has model configured; we call through it
	}, nil
}

func (c *captionProviderService) DescribeImage(ctx context.Context, req CaptionRequest) (*CaptionResult, error) {
	ctx = defaultCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	task := strings.TrimSpace(req.Task)
	if task == "" {
		task = "image_notes"
	}

	imageURL := strings.TrimSpace(req.ImageURL)
	if imageURL == "" && len(req.ImageBytes) > 0 {
		if strings.TrimSpace(req.ImageMime) == "" {
			return nil, fmt.Errorf("ImageMime required when using ImageBytes")
		}
		imageURL = dataURL(req.ImageMime, req.ImageBytes)
	}
	if imageURL == "" {
		return nil, fmt.Errorf("image required (ImageURL or ImageBytes)")
	}

	system := "You are a meticulous visual analyst. Your job is to turn diagrams/images into faithful, factual text notes."
	user := buildCaptionPrompt(task, req.Prompt)

	// We will request JSON explicitly (robustness: validate ourselves).
	// OpenAI docs show image inputs with `input_text` + `input_image`. :contentReference[oaicite:3]{index=3}
	raw, err := c.openai.GenerateTextWithImages(ctx, system, user, []OpenAIImageInput{
		{ImageURL: imageURL, Detail: req.Detail},
	})
	if err != nil {
		return nil, err
	}

	// Parse JSON; if fails, do a repair call (text-only).
	out, err := parseCaptionJSON(raw)
	if err == nil {
		return out, nil
	}

	// Repair loop (hard way): ask model to fix JSON only.
	repaired, err2 := c.openai.GenerateText(ctx,
		"You are a JSON repair tool. Output ONLY valid JSON matching the required shape.",
		fmt.Sprintf(
			"Fix the following into valid JSON with keys:\n"+
				"task (string), summary (string), key_takeaways (array of strings), entities (array), relationships (array), text_in_image (array), warnings (array optional).\n\nRAW:\n%s",
			raw,
		),
	)
	if err2 != nil {
		return nil, fmt.Errorf("caption JSON parse failed; repair call failed: %w; parse_err=%v", err2, err)
	}

	out2, err3 := parseCaptionJSON(repaired)
	if err3 != nil {
		return nil, fmt.Errorf("caption JSON parse failed after repair: %v; original_parse_err=%v", err3, err)
	}
	out2.Warnings = append(out2.Warnings, "caption JSON required repair pass")
	return out2, nil
}

func buildCaptionPrompt(task, extra string) string {
	var b strings.Builder
	b.WriteString("Return ONLY JSON.\n")
	b.WriteString("Task: " + task + "\n\n")
	b.WriteString("You must:\n")
	b.WriteString("- Describe what the image/diagram shows in plain language.\n")
	b.WriteString("- Extract any text visible in the image (as best as possible).\n")
	b.WriteString("- Explain relationships, axes, flows, components, and labels.\n")
	b.WriteString("- Do not hallucinate details not visible.\n\n")
	if strings.TrimSpace(extra) != "" {
		b.WriteString("Extra instructions:\n")
		b.WriteString(extra)
		b.WriteString("\n\n")
	}
	b.WriteString("JSON shape:\n")
	b.WriteString(`{
  "task": "figure_notes|image_notes|frame_notes",
  "summary": "string",
  "key_takeaways": ["..."],
  "entities": ["..."],
  "relationships": ["..."],
  "text_in_image": ["..."],
  "warnings": ["...optional..."]
}`)
	return b.String()
}

func parseCaptionJSON(s string) (*CaptionResult, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty response")
	}
	// Try to locate a JSON object if model wrapped it
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}

	var out CaptionResult
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}

	// minimal validation
	if strings.TrimSpace(out.Summary) == "" {
		return nil, fmt.Errorf("missing summary")
	}
	if len(out.KeyTakeaways) == 0 {
		out.KeyTakeaways = []string{}
	}
	if len(out.Entities) == 0 {
		out.Entities = []string{}
	}
	if len(out.Relationships) == 0 {
		out.Relationships = []string{}
	}
	if len(out.TextInImage) == 0 {
		out.TextInImage = []string{}
	}
	return &out, nil
}

func dataURL(mime string, b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	return fmt.Sprintf("data:%s;base64,%s", mime, enc)
}











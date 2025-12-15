package services

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"

	"github.com/yungbote/neurobridge-backend/internal/logger"
)

type Segment struct {
  Text       string         `json:"text"`

  // Document provenance
  Page       *int           `json:"page,omitempty"`

  // Audio/video provenance
  StartSec   *float64       `json:"start_sec,omitempty"`
  EndSec     *float64       `json:"end_sec,omitempty"`

  // Speaker diarization (speech/video)
  SpeakerTag *int           `json:"speaker_tag,omitempty"`

  // Confidence when provided by OCR/transcription providers
  Confidence *float64       `json:"confidence,omitempty"`

  // Any additional provenance/labels:
  // kind: "ocr_text" | "transcript" | "frame_ocr" | "figure_notes" | "table_text" | ...
  // provider: "gcp_vision" | "gcp_speech" | "gcp_documentai" | "gcp_videointelligence" | "openai_caption"
  // asset_key: "materials/.../derived/pages/page_0001.png"
  Metadata   map[string]any `json:"metadata,omitempty"`
}


type SpeechProviderService interface {
	TranscribeAudioBytes(ctx context.Context, audio []byte, mimeType string, cfg SpeechConfig) (*SpeechResult, error)
	TranscribeAudioGCS(ctx context.Context, gcsURI string, cfg SpeechConfig) (*SpeechResult, error)
	Close() error
}

type SpeechConfig struct {
	LanguageCode string
	Model        string
	UseEnhanced  bool

	EnableAutomaticPunctuation bool
	EnableWordTimeOffsets      bool // should be true for your pipeline :contentReference[oaicite:7]{index=7}

	EnableSpeakerDiarization bool
	MinSpeakerCount          int
	MaxSpeakerCount          int

	SampleRateHertz   int
	AudioChannelCount int

	// Optional override
	Encoding speechpb.RecognitionConfig_AudioEncoding
}

type SpeechResult struct {
	Provider    string    `json:"provider"`
	SourceURI   string    `json:"source_uri,omitempty"`
	PrimaryText string    `json:"primary_text"`
	Segments    []Segment `json:"segments,omitempty"` // diarized/time-aligned segments
	Words       []Segment `json:"words,omitempty"`    // per-word segments (optional; can be big)
	Warnings    []string  `json:"warnings,omitempty"`
}

type speechProviderService struct {
	log    *logger.Logger
	client *speech.Client

	maxRetries int
}

func NewSpeechProviderService(log *logger.Logger) (SpeechProviderService, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "SpeechProviderService")

	creds := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON"))
	if creds == "" {
		creds = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}

	ctx := context.Background()

	var c *speech.Client
	var err error
	if creds != "" {
		c, err = speech.NewClient(ctx, option.WithCredentialsFile(creds))
	} else {
		c, err = speech.NewClient(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("speech client: %w", err)
	}

	return &speechProviderService{
		log:        slog,
		client:     c,
		maxRetries: 4,
	}, nil
}

func (s *speechProviderService) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *speechProviderService) TranscribeAudioBytes(ctx context.Context, audio []byte, mimeType string, cfg SpeechConfig) (*SpeechResult, error) {
	ctx = defaultCtx(ctx)
	// bytes transcription is best for short audio; keep a strict timeout
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if len(audio) == 0 {
		return &SpeechResult{Provider: "gcp_speech", PrimaryText: ""}, nil
	}

	rcfg := buildSpeechRecognitionConfig(mimeType, "", cfg)
	req := &speechpb.LongRunningRecognizeRequest{
		Config: rcfg,
		Audio:  &speechpb.RecognitionAudio{AudioSource: &speechpb.RecognitionAudio_Content{Content: audio}},
	}

	resp, err := s.retryLR(ctx, func() (*speechpb.LongRunningRecognizeResponse, error) {
		op, err := s.client.LongRunningRecognize(ctx, req)
		if err != nil {
			return nil, err
		}
		return op.Wait(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("speech longrunningrecognize(bytes): %w", err)
	}

	return parseSpeechResponse("gcp_speech", "", resp, cfg.EnableWordTimeOffsets, cfg.EnableSpeakerDiarization), nil
}

func (s *speechProviderService) TranscribeAudioGCS(ctx context.Context, gcsURI string, cfg SpeechConfig) (*SpeechResult, error) {
	ctx = defaultCtx(ctx)
	// GCS long audio can take a while
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if !strings.HasPrefix(gcsURI, "gs://") {
		return nil, fmt.Errorf("gcsURI must be gs://... got %q", gcsURI)
	}

	rcfg := buildSpeechRecognitionConfig("", gcsURI, cfg)
	req := &speechpb.LongRunningRecognizeRequest{
		Config: rcfg,
		Audio:  &speechpb.RecognitionAudio{AudioSource: &speechpb.RecognitionAudio_Uri{Uri: gcsURI}},
	}

	resp, err := s.retryLR(ctx, func() (*speechpb.LongRunningRecognizeResponse, error) {
		op, err := s.client.LongRunningRecognize(ctx, req)
		if err != nil {
			return nil, err
		}
		return op.Wait(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("speech longrunningrecognize(gcs): %w", err)
	}

	return parseSpeechResponse("gcp_speech", gcsURI, resp, cfg.EnableWordTimeOffsets, cfg.EnableSpeakerDiarization), nil
}

func buildSpeechRecognitionConfig(mimeType string, gcsURI string, cfg SpeechConfig) *speechpb.RecognitionConfig {
	if cfg.LanguageCode == "" {
		cfg.LanguageCode = "en-US"
	}

	enc := cfg.Encoding
	if enc == speechpb.RecognitionConfig_ENCODING_UNSPECIFIED {
		enc = inferSpeechEncoding(mimeType, gcsURI)
	}

	rc := &speechpb.RecognitionConfig{
		LanguageCode:            cfg.LanguageCode,
		Model:                   cfg.Model,
		UseEnhanced:             cfg.UseEnhanced,
		EnableAutomaticPunctuation: cfg.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:   cfg.EnableWordTimeOffsets,
		Encoding:                enc,
		SampleRateHertz:         int32(max0(cfg.SampleRateHertz)),
		AudioChannelCount:       int32(max0(cfg.AudioChannelCount)),
	}

	if cfg.EnableSpeakerDiarization {
		rc.DiarizationConfig = &speechpb.SpeakerDiarizationConfig{
			EnableSpeakerDiarization: true,
			MinSpeakerCount:          int32(max0(cfg.MinSpeakerCount)),
			MaxSpeakerCount:          int32(max0(cfg.MaxSpeakerCount)),
		}
	}
	return rc
}

func inferSpeechEncoding(mimeType string, gcsURI string) speechpb.RecognitionConfig_AudioEncoding {
	m := strings.ToLower(strings.TrimSpace(mimeType))
	ext := strings.ToLower(filepath.Ext(gcsURI))

	switch {
	case strings.Contains(m, "wav") || ext == ".wav":
		return speechpb.RecognitionConfig_LINEAR16
	case strings.Contains(m, "flac") || ext == ".flac":
		return speechpb.RecognitionConfig_FLAC
	case strings.Contains(m, "mp3") || ext == ".mp3":
		return speechpb.RecognitionConfig_MP3
	case strings.Contains(m, "ogg") || ext == ".ogg" || ext == ".opus":
		return speechpb.RecognitionConfig_OGG_OPUS
	default:
		// leave unspecified; API can sometimes auto-detect in practice
		return speechpb.RecognitionConfig_ENCODING_UNSPECIFIED
	}
}

func parseSpeechResponse(provider string, sourceURI string, resp *speechpb.LongRunningRecognizeResponse, wantWordOffsets bool, diarize bool) *SpeechResult {
	out := &SpeechResult{
		Provider:  provider,
		SourceURI: sourceURI,
	}

	if resp == nil || len(resp.Results) == 0 {
		out.PrimaryText = ""
		return out
	}

	// Collect words with timestamps + speaker tags (if any)
	type word struct {
		w   string
		s   float64
		e   float64
		spk int
		c   float64
	}
	words := []word{}

	var full strings.Builder

	for _, r := range resp.Results {
		if r == nil || len(r.Alternatives) == 0 || r.Alternatives[0] == nil {
			continue
		}
		alt := r.Alternatives[0]
		if strings.TrimSpace(alt.Transcript) == "" {
			continue
		}
		if full.Len() > 0 {
			full.WriteString(" ")
		}
		full.WriteString(strings.TrimSpace(alt.Transcript))

		if wantWordOffsets && len(alt.Words) > 0 {
			for _, ww := range alt.Words {
				if ww == nil {
					continue
				}
				ws := durToSec(ww.StartTime)
				we := durToSec(ww.EndTime)
				spk := int(ww.SpeakerTag)
				words = append(words, word{
					w:   ww.Word,
					s:   ws,
					e:   we,
					spk: spk,
					c:   float64(ww.Confidence),
				})
			}
		}
	}

	out.PrimaryText = strings.TrimSpace(full.String())

	// Per-word segments (optional)
	if wantWordOffsets && len(words) > 0 {
		out.Words = make([]Segment, 0, len(words))
		for _, w := range words {
			s := w.s
			e := w.e
			spk := w.spk
			conf := w.c
			out.Words = append(out.Words, Segment{
				Text:       w.w,
				StartSec:   &s,
				EndSec:     &e,
				SpeakerTag: &spk,
				Confidence: ptrFloat(conf),
				Metadata:   map[string]any{"kind": "word"},
			})
		}
	}

	// Diarized segments: group contiguous words by speaker
	if diarize && len(words) > 0 {
		out.Segments = groupBySpeaker(words)
	} else if wantWordOffsets && len(words) > 0 {
		// fallback: group by time windows (~10s)
		out.Segments = groupByTime(words, 10.0)
	} else {
		out.Segments = []Segment{{Text: out.PrimaryText, Metadata: map[string]any{"kind": "transcript"}}}
	}

	return out
}

func groupBySpeaker(words []struct {
	w   string
	s   float64
	e   float64
	spk int
	c   float64
}) []Segment {
	if len(words) == 0 {
		return nil
	}

	segs := []Segment{}
	curSpk := words[0].spk
	curStart := words[0].s
	curEnd := words[0].e
	var buf strings.Builder
	var confSum float64
	var confN int

	flush := func() {
		txt := strings.TrimSpace(buf.String())
		if txt == "" {
			return
		}
		s := curStart
		e := curEnd
		spk := curSpk
		var c *float64
		if confN > 0 {
			v := confSum / float64(confN)
			c = &v
		}
		segs = append(segs, Segment{
			Text:       txt,
			StartSec:   &s,
			EndSec:     &e,
			SpeakerTag: &spk,
			Confidence: c,
			Metadata:   map[string]any{"kind": "transcript", "group": "speaker"},
		})
		buf.Reset()
		confSum = 0
		confN = 0
	}

	for _, w := range words {
		if w.spk != curSpk && buf.Len() > 0 {
			flush()
			curSpk = w.spk
			curStart = w.s
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(w.w)
		curEnd = math.Max(curEnd, w.e)
		if w.c > 0 {
			confSum += w.c
			confN++
		}
	}
	flush()
	return segs
}

func groupByTime(words []struct {
	w   string
	s   float64
	e   float64
	spk int
	c   float64
}, windowSec float64) []Segment {
	if len(words) == 0 {
		return nil
	}
	if windowSec <= 0 {
		windowSec = 10
	}

	segs := []Segment{}
	curStart := words[0].s
	curEnd := words[0].e
	var buf strings.Builder
	var confSum float64
	var confN int

	flush := func() {
		txt := strings.TrimSpace(buf.String())
		if txt == "" {
			return
		}
		s := curStart
		e := curEnd
		var c *float64
		if confN > 0 {
			v := confSum / float64(confN)
			c = &v
		}
		segs = append(segs, Segment{
			Text:       txt,
			StartSec:   &s,
			EndSec:     &e,
			Confidence: c,
			Metadata:   map[string]any{"kind": "transcript", "group": "time"},
		})
		buf.Reset()
		confSum = 0
		confN = 0
	}

	for _, w := range words {
		if (w.s-curStart) >= windowSec && buf.Len() > 0 {
			flush()
			curStart = w.s
			curEnd = w.e
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(w.w)
		if w.e > curEnd {
			curEnd = w.e
		}
		if w.c > 0 {
			confSum += w.c
			confN++
		}
	}
	flush()
	return segs
}

func durToSec(d *speechpb.Duration) float64 {
	if d == nil {
		return 0
	}
	return float64(d.Seconds) + float64(d.Nanos)/1e9
}

func (s *speechProviderService) retryLR(ctx context.Context, fn func() (*speechpb.LongRunningRecognizeResponse, error)) (*speechpb.LongRunningRecognizeResponse, error) {
	backoff := 750 * time.Millisecond
	var last error
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := fn()
		if err == nil {
			return resp, nil
		}
		last = err

		code := status.Code(err)
		if code != codes.Unavailable && code != codes.ResourceExhausted && code != codes.DeadlineExceeded {
			return nil, err
		}
		if attempt == s.maxRetries {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
	return nil, last
}

func max0(x int) int {
	if x < 0 {
		return 0
	}
	return x
}











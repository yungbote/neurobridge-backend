package gcp

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Speech interface {
	TranscribeAudioBytes(ctx context.Context, audio []byte, mimeType string, cfg SpeechConfig) (*SpeechResult, error)
	TranscribeAudioGCS(ctx context.Context, gcsURI string, cfg SpeechConfig) (*SpeechResult, error)
	Close() error
}

type SpeechConfig struct {
	LanguageCode string
	Model        string
	UseEnhanced  bool

	EnableAutomaticPunctuation bool
	EnableWordTimeOffsets      bool

	EnableSpeakerDiarization bool
	MinSpeakerCount          int
	MaxSpeakerCount          int

	SampleRateHertz   int
	AudioChannelCount int

	Encoding speechpb.RecognitionConfig_AudioEncoding
}

type SpeechResult struct {
	Provider    string          `json:"provider"`
	SourceURI   string          `json:"source_uri,omitempty"`
	PrimaryText string          `json:"primary_text"`
	Segments    []types.Segment `json:"segments,omitempty"`
	Words       []types.Segment `json:"words,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
}

type speechService struct {
	log        *logger.Logger
	client     *speech.Client
	maxRetries int
}

func NewSpeech(log *logger.Logger) (Speech, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "gcp.Speech")

	ctx := context.Background()
	opts := ClientOptionsFromEnv()

	c, err := speech.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("speech client: %w", err)
	}

	return &speechService{
		log:        slog,
		client:     c,
		maxRetries: 4,
	}, nil
}

func (s *speechService) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *speechService) TranscribeAudioBytes(ctx context.Context, audio []byte, mimeType string, cfg SpeechConfig) (*SpeechResult, error) {
	ctx = ctxutil.Default(ctx)
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

func (s *speechService) TranscribeAudioGCS(ctx context.Context, gcsURI string, cfg SpeechConfig) (*SpeechResult, error) {
	ctx = ctxutil.Default(ctx)
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
		LanguageCode:               cfg.LanguageCode,
		Model:                      cfg.Model,
		UseEnhanced:                cfg.UseEnhanced,
		EnableAutomaticPunctuation: cfg.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:      cfg.EnableWordTimeOffsets,
		Encoding:                   enc,
		SampleRateHertz:            int32(max0(cfg.SampleRateHertz)),
		AudioChannelCount:          int32(max0(cfg.AudioChannelCount)),
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
		return speechpb.RecognitionConfig_ENCODING_UNSPECIFIED
	}
}

type speechWord struct {
	w   string
	s   float64
	e   float64
	spk int
	c   float64
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

	words := []speechWord{}
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
				words = append(words, speechWord{
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

	if wantWordOffsets && len(words) > 0 {
		out.Words = make([]types.Segment, 0, len(words))
		for _, w := range words {
			sv := w.s
			ev := w.e
			spk := w.spk
			conf := w.c
			out.Words = append(out.Words, types.Segment{
				Text:       w.w,
				StartSec:   &sv,
				EndSec:     &ev,
				SpeakerTag: &spk,
				Confidence: ptrFloat(conf),
				Metadata:   map[string]any{"kind": "word", "provider": provider},
			})
		}
	}

	if diarize && len(words) > 0 {
		out.Segments = groupBySpeaker(words, provider)
	} else if wantWordOffsets && len(words) > 0 {
		out.Segments = groupByTime(words, 10.0, provider)
	} else {
		out.Segments = []types.Segment{{Text: out.PrimaryText, Metadata: map[string]any{"kind": "transcript", "provider": provider}}}
	}

	return out
}

func groupBySpeaker(words []speechWord, provider string) []types.Segment {
	if len(words) == 0 {
		return nil
	}

	segs := []types.Segment{}
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
		sv := curStart
		ev := curEnd
		spk := curSpk
		var c *float64
		if confN > 0 {
			v := confSum / float64(confN)
			c = &v
		}
		segs = append(segs, types.Segment{
			Text:       txt,
			StartSec:   &sv,
			EndSec:     &ev,
			SpeakerTag: &spk,
			Confidence: c,
			Metadata:   map[string]any{"kind": "transcript", "group": "speaker", "provider": provider},
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

func groupByTime(words []speechWord, windowSec float64, provider string) []types.Segment {
	if len(words) == 0 {
		return nil
	}
	if windowSec <= 0 {
		windowSec = 10
	}

	segs := []types.Segment{}
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
		sv := curStart
		ev := curEnd
		var c *float64
		if confN > 0 {
			v := confSum / float64(confN)
			c = &v
		}
		segs = append(segs, types.Segment{
			Text:       txt,
			StartSec:   &sv,
			EndSec:     &ev,
			Confidence: c,
			Metadata:   map[string]any{"kind": "transcript", "group": "time", "provider": provider},
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

func durToSec(d *durationpb.Duration) float64 {
	if d == nil {
		return 0
	}
	return float64(d.Seconds) + float64(d.Nanos)/1e9
}

func (s *speechService) retryLR(ctx context.Context, fn func() (*speechpb.LongRunningRecognizeResponse, error)) (*speechpb.LongRunningRecognizeResponse, error) {
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

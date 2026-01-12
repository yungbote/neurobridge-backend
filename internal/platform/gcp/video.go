package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	videointelligence "cloud.google.com/go/videointelligence/apiv1"
	vipb "cloud.google.com/go/videointelligence/apiv1/videointelligencepb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Video interface {
	AnnotateVideoGCS(ctx context.Context, gcsURI string, cfg VideoAIConfig) (*VideoAIResult, error)
	Close() error
}

type VideoAIConfig struct {
	LanguageCode string
	Model        string // "default" or "video"

	EnableAutomaticPunctuation bool
	EnableSpeakerDiarization   bool
	MinSpeakerCount            int
	MaxSpeakerCount            int

	EnableSpeechTranscription bool
	EnableTextDetection       bool
	EnableShotChangeDetection bool
}

type VideoAIResult struct {
	Provider    string `json:"provider"`
	SourceURI   string `json:"source_uri"`
	PrimaryText string `json:"primary_text"`

	TranscriptSegments []types.Segment `json:"transcript_segments,omitempty"`
	TextSegments       []types.Segment `json:"text_segments,omitempty"`
	ShotSegments       []types.Segment `json:"shot_segments,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

type videoService struct {
	log        *logger.Logger
	client     *videointelligence.Client
	maxRetries int
}

func NewVideo(log *logger.Logger) (Video, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "gcp.Video")

	ctx := context.Background()
	opts := ClientOptionsFromEnv()

	c, err := videointelligence.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("videointelligence client: %w", err)
	}

	return &videoService{
		log:        slog,
		client:     c,
		maxRetries: 4,
	}, nil
}

func (s *videoService) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *videoService) AnnotateVideoGCS(ctx context.Context, gcsURI string, cfg VideoAIConfig) (*VideoAIResult, error) {
	ctx = ctxutil.Default(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if !strings.HasPrefix(gcsURI, "gs://") {
		return nil, fmt.Errorf("gcsURI must be gs://... got %q", gcsURI)
	}

	if cfg.LanguageCode == "" {
		cfg.LanguageCode = "en-US"
	}
	if cfg.Model == "" {
		cfg.Model = "video"
	}
	if !cfg.EnableSpeechTranscription && !cfg.EnableTextDetection && !cfg.EnableShotChangeDetection {
		cfg.EnableSpeechTranscription = true
		cfg.EnableTextDetection = true
	}

	features := []vipb.Feature{}
	if cfg.EnableSpeechTranscription {
		features = append(features, vipb.Feature_SPEECH_TRANSCRIPTION)
	}
	if cfg.EnableTextDetection {
		features = append(features, vipb.Feature_TEXT_DETECTION)
	}
	if cfg.EnableShotChangeDetection {
		features = append(features, vipb.Feature_SHOT_CHANGE_DETECTION)
	}

	var vcfg *vipb.VideoContext
	if cfg.EnableSpeechTranscription || cfg.EnableTextDetection {
		vcfg = &vipb.VideoContext{}
	}

	if cfg.EnableSpeechTranscription {
		stc := &vipb.SpeechTranscriptionConfig{
			LanguageCode:               cfg.LanguageCode,
			EnableAutomaticPunctuation: cfg.EnableAutomaticPunctuation,
			FilterProfanity:            false,
			EnableWordConfidence:       true,
		}
		if cfg.EnableSpeakerDiarization {
			stc.EnableSpeakerDiarization = true
			if cfg.MinSpeakerCount > 0 {
				stc.DiarizationSpeakerCount = int32(cfg.MinSpeakerCount)
			}
		}
		vcfg.SpeechTranscriptionConfig = stc
	}
	if cfg.EnableTextDetection {
		vcfg.TextDetectionConfig = &vipb.TextDetectionConfig{}
	}

	req := &vipb.AnnotateVideoRequest{
		InputUri:     gcsURI,
		Features:     features,
		VideoContext: vcfg,
	}

	resp, err := s.retryAnnotate(ctx, func() (*vipb.AnnotateVideoResponse, error) {
		op, err := s.client.AnnotateVideo(ctx, req)
		if err != nil {
			return nil, err
		}
		return op.Wait(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("videointelligence AnnotateVideo: %w", err)
	}

	out := &VideoAIResult{
		Provider:  "gcp_videointelligence",
		SourceURI: gcsURI,
	}

	if resp == nil || len(resp.AnnotationResults) == 0 || resp.AnnotationResults[0] == nil {
		out.PrimaryText = ""
		out.Warnings = append(out.Warnings, "no annotation results")
		return out, nil
	}

	ar := resp.AnnotationResults[0]

	if cfg.EnableSpeechTranscription && len(ar.SpeechTranscriptions) > 0 {
		out.TranscriptSegments = parseVideoSpeech(ar.SpeechTranscriptions)
	}
	if cfg.EnableTextDetection && len(ar.TextAnnotations) > 0 {
		out.TextSegments = parseVideoText(ar.TextAnnotations)
	}
	if cfg.EnableShotChangeDetection && len(ar.ShotAnnotations) > 0 {
		out.ShotSegments = parseShots(ar.ShotAnnotations)
	}

	var b strings.Builder
	for _, sg := range out.TranscriptSegments {
		if strings.TrimSpace(sg.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sg.Text)
	}
	for _, sg := range out.TextSegments {
		if strings.TrimSpace(sg.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[on_screen] ")
		b.WriteString(sg.Text)
	}
	out.PrimaryText = strings.TrimSpace(b.String())

	return out, nil
}

func parseVideoSpeech(st []*vipb.SpeechTranscription) []types.Segment {
	type seg struct {
		text string
		s    float64
		e    float64
		spk  int
		conf float64
	}
	segments := []seg{}

	for _, tr := range st {
		if tr == nil || len(tr.Alternatives) == 0 || tr.Alternatives[0] == nil {
			continue
		}
		alt := tr.Alternatives[0]
		if strings.TrimSpace(alt.Transcript) == "" {
			continue
		}

		if len(alt.Words) == 0 {
			segments = append(segments, seg{
				text: strings.TrimSpace(alt.Transcript),
				s:    0,
				e:    0,
				spk:  0,
				conf: float64(alt.Confidence),
			})
			continue
		}

		curSpk := int(alt.Words[0].SpeakerTag)
		curStart := durToSecVI(alt.Words[0].StartTime)
		curEnd := durToSecVI(alt.Words[0].EndTime)
		var buf strings.Builder
		var confSum float64
		var confN int

		flush := func() {
			txt := strings.TrimSpace(buf.String())
			if txt == "" {
				return
			}
			c := 0.0
			if confN > 0 {
				c = confSum / float64(confN)
			}
			segments = append(segments, seg{text: txt, s: curStart, e: curEnd, spk: curSpk, conf: c})
			buf.Reset()
			confSum = 0
			confN = 0
		}

		for _, w := range alt.Words {
			if w == nil {
				continue
			}
			spk := int(w.SpeakerTag)
			ws := durToSecVI(w.StartTime)
			we := durToSecVI(w.EndTime)

			if spk != 0 && spk != curSpk && buf.Len() > 0 {
				flush()
				curSpk = spk
				curStart = ws
				curEnd = we
			}

			if buf.Len() > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(w.Word)

			if we > curEnd {
				curEnd = we
			}
			if w.Confidence > 0 {
				confSum += float64(w.Confidence)
				confN++
			}
		}
		flush()
	}

	out := make([]types.Segment, 0, len(segments))
	for _, s := range segments {
		ss := s.s
		ee := s.e
		spk := s.spk
		conf := s.conf
		out = append(out, types.Segment{
			Text:       s.text,
			StartSec:   ptrFloat(ss),
			EndSec:     ptrFloat(ee),
			SpeakerTag: &spk,
			Confidence: ptrFloat(conf),
			Metadata:   map[string]any{"kind": "transcript", "provider": "gcp_videointelligence"},
		})
	}
	return out
}

func parseVideoText(ann []*vipb.TextAnnotation) []types.Segment {
	type piece struct {
		text string
		s    float64
		e    float64
		conf float64
	}
	tmp := []piece{}

	for _, ta := range ann {
		if ta == nil || strings.TrimSpace(ta.Text) == "" {
			continue
		}
		for _, seg := range ta.Segments {
			if seg == nil || seg.Segment == nil {
				continue
			}
			s := durToSecVI(seg.Segment.StartTimeOffset)
			e := durToSecVI(seg.Segment.EndTimeOffset)
			tmp = append(tmp, piece{text: ta.Text, s: s, e: e, conf: float64(seg.Confidence)})
		}
	}

	sort.Slice(tmp, func(i, j int) bool {
		if tmp[i].s == tmp[j].s {
			return tmp[i].e < tmp[j].e
		}
		return tmp[i].s < tmp[j].s
	})

	out := make([]types.Segment, 0, len(tmp))
	for _, p := range tmp {
		ss := p.s
		ee := p.e
		conf := p.conf
		out = append(out, types.Segment{
			Text:       p.text,
			StartSec:   &ss,
			EndSec:     &ee,
			Confidence: ptrFloat(conf),
			Metadata:   map[string]any{"kind": "frame_ocr", "provider": "gcp_videointelligence"},
		})
	}
	return out
}

func parseShots(shots []*vipb.VideoSegment) []types.Segment {
	out := []types.Segment{}
	for _, sh := range shots {
		if sh == nil {
			continue
		}
		s := durToSecVI(sh.StartTimeOffset)
		e := durToSecVI(sh.EndTimeOffset)
		ss := s
		ee := e
		out = append(out, types.Segment{
			Text:     "shot",
			StartSec: &ss,
			EndSec:   &ee,
			Metadata: map[string]any{"kind": "shot", "provider": "gcp_videointelligence"},
		})
	}
	return out
}

func durToSecVI(d *durationpb.Duration) float64 {
	if d == nil {
		return 0
	}
	return float64(d.Seconds) + float64(d.Nanos)/1e9
}

func (s *videoService) retryAnnotate(ctx context.Context, fn func() (*vipb.AnnotateVideoResponse, error)) (*vipb.AnnotateVideoResponse, error) {
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

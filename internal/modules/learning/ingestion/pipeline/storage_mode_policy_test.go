package pipeline

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

func TestHandleAudioUsesGCSInGCSMode(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.wav")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	speech := &fakeSpeech{}
	svc := &service{
		ex: &extractor.Extractor{
			Bucket:             &fakeBucketService{},
			Speech:             speech,
			ObjectStorageMode:  gcp.ObjectStorageModeGCS,
			MaterialBucketName: "materials-bucket",
		},
	}
	mf := &types.MaterialFile{
		StorageKey:   "materials/source-audio.wav",
		OriginalName: "lecture.wav",
	}

	_, _, warnings, diag, err := svc.handleAudio(context.Background(), mf, audioPath)
	if err != nil {
		t.Fatalf("handleAudio: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings: want=0 got=%d (%v)", len(warnings), warnings)
	}
	if !speech.calledGCS {
		t.Fatalf("expected TranscribeAudioGCS to be called")
	}
	if speech.calledBytes {
		t.Fatalf("expected TranscribeAudioBytes not to be called")
	}
	wantURI := "gs://materials-bucket/materials/source-audio.wav/derived/audio/audio.wav"
	if speech.lastGCSURI != wantURI {
		t.Fatalf("gcs uri: want=%q got=%q", wantURI, speech.lastGCSURI)
	}
	if got := diag["speech_mode"]; got != "gcs_uri" {
		t.Fatalf("diag.speech_mode: want=%q got=%v", "gcs_uri", got)
	}
}

func TestHandleAudioFallsBackToBytesInEmulatorMode(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.wav")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	speech := &fakeSpeech{}
	svc := &service{
		ex: &extractor.Extractor{
			Bucket:             &fakeBucketService{},
			Speech:             speech,
			ObjectStorageMode:  gcp.ObjectStorageModeGCSEmulator,
			MaterialBucketName: "materials-bucket",
		},
	}
	mf := &types.MaterialFile{
		StorageKey:   "materials/source-audio.wav",
		OriginalName: "lecture.wav",
	}

	_, _, warnings, diag, err := svc.handleAudio(context.Background(), mf, audioPath)
	if err != nil {
		t.Fatalf("handleAudio: %v", err)
	}
	if !speech.calledBytes {
		t.Fatalf("expected TranscribeAudioBytes to be called")
	}
	if speech.calledGCS {
		t.Fatalf("expected TranscribeAudioGCS not to be called")
	}
	if got := diag["speech_mode"]; got != "bytes_fallback_gcs_emulator" {
		t.Fatalf("diag.speech_mode: want=%q got=%v", "bytes_fallback_gcs_emulator", got)
	}
	if !containsWarning(warnings, "speech gcs transcription disabled in gcs_emulator mode") {
		t.Fatalf("missing emulator speech warning; warnings=%v", warnings)
	}
}

func TestHandleVideoUsesVideoAIInEmulatorMode(t *testing.T) {
	video := &fakeVideo{}
	svc := &service{
		ex: &extractor.Extractor{
			VideoAI:            video,
			ObjectStorageMode:  gcp.ObjectStorageModeGCSEmulator,
			MaterialBucketName: "materials-bucket",
			Media:              nil,
		},
	}
	mf := &types.MaterialFile{
		StorageKey:   "materials/source-video.mp4",
		OriginalName: "lecture.mp4",
	}

	_, _, warnings, diag, err := svc.handleVideo(context.Background(), mf, "/tmp/fake-video.mp4")
	if err != nil {
		t.Fatalf("handleVideo: %v", err)
	}
	if !video.called {
		t.Fatalf("expected VideoAI AnnotateVideoGCS to be called")
	}
	wantURI := "gs://materials-bucket/materials/source-video.mp4"
	if video.lastGCSURI != wantURI {
		t.Fatalf("gcs uri: want=%q got=%q", wantURI, video.lastGCSURI)
	}
	if got := diag["videoai_policy"]; got != "enabled_in_gcs_emulator" {
		t.Fatalf("diag.videoai_policy: want=%q got=%v", "enabled_in_gcs_emulator", got)
	}
	if containsWarning(warnings, "video intelligence skipped in gcs_emulator mode") {
		t.Fatalf("unexpected emulator video skip warning; warnings=%v", warnings)
	}
}

func TestHandleVideoUsesVideoAIInGCSMode(t *testing.T) {
	video := &fakeVideo{}
	svc := &service{
		ex: &extractor.Extractor{
			VideoAI:            video,
			ObjectStorageMode:  gcp.ObjectStorageModeGCS,
			MaterialBucketName: "materials-bucket",
			Media:              nil,
		},
	}
	mf := &types.MaterialFile{
		StorageKey:   "materials/source-video.mp4",
		OriginalName: "lecture.mp4",
	}

	_, _, warnings, diag, err := svc.handleVideo(context.Background(), mf, "/tmp/fake-video.mp4")
	if err != nil {
		t.Fatalf("handleVideo: %v", err)
	}
	if !video.called {
		t.Fatalf("expected VideoAI AnnotateVideoGCS to be called")
	}
	wantURI := "gs://materials-bucket/materials/source-video.mp4"
	if video.lastGCSURI != wantURI {
		t.Fatalf("gcs uri: want=%q got=%q", wantURI, video.lastGCSURI)
	}
	if got := diag["videoai_policy"]; got != "enabled" {
		t.Fatalf("diag.videoai_policy: want=%q got=%v", "enabled", got)
	}
	if containsWarning(warnings, "video intelligence skipped in gcs_emulator mode") {
		t.Fatalf("unexpected emulator skip warning in gcs mode; warnings=%v", warnings)
	}
}

func containsWarning(warnings []string, needle string) bool {
	for _, w := range warnings {
		if strings.Contains(w, needle) {
			return true
		}
	}
	return false
}

type fakeBucketService struct{}

func (f *fakeBucketService) UploadFile(dbc dbctx.Context, category gcp.BucketCategory, key string, file io.Reader) error {
	return nil
}

func (f *fakeBucketService) DeleteFile(dbc dbctx.Context, category gcp.BucketCategory, key string) error {
	return nil
}

func (f *fakeBucketService) ReplaceFile(dbc dbctx.Context, category gcp.BucketCategory, key string, newFile io.Reader) error {
	return nil
}

func (f *fakeBucketService) DownloadFile(ctx context.Context, category gcp.BucketCategory, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeBucketService) OpenRangeReader(ctx context.Context, category gcp.BucketCategory, key string, offset, length int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeBucketService) GetObjectAttrs(ctx context.Context, category gcp.BucketCategory, key string) (*gcp.ObjectAttrs, error) {
	return &gcp.ObjectAttrs{}, nil
}

func (f *fakeBucketService) CopyObject(ctx context.Context, category gcp.BucketCategory, srcKey, dstKey string) error {
	return nil
}

func (f *fakeBucketService) ListKeys(ctx context.Context, category gcp.BucketCategory, prefix string) ([]string, error) {
	return nil, nil
}

func (f *fakeBucketService) DeletePrefix(ctx context.Context, category gcp.BucketCategory, prefix string) error {
	return nil
}

func (f *fakeBucketService) GetPublicURL(category gcp.BucketCategory, key string) string {
	return "http://storage.local/" + key
}

type fakeSpeech struct {
	calledBytes bool
	calledGCS   bool
	lastGCSURI  string
}

func (f *fakeSpeech) TranscribeAudioBytes(ctx context.Context, audio []byte, mimeType string, cfg gcp.SpeechConfig) (*gcp.SpeechResult, error) {
	f.calledBytes = true
	if len(audio) == 0 {
		return nil, io.EOF
	}
	return &gcp.SpeechResult{}, nil
}

func (f *fakeSpeech) TranscribeAudioGCS(ctx context.Context, gcsURI string, cfg gcp.SpeechConfig) (*gcp.SpeechResult, error) {
	f.calledGCS = true
	f.lastGCSURI = gcsURI
	return &gcp.SpeechResult{}, nil
}

func (f *fakeSpeech) Close() error { return nil }

type fakeVideo struct {
	called     bool
	lastGCSURI string
}

func (f *fakeVideo) AnnotateVideoGCS(ctx context.Context, gcsURI string, cfg gcp.VideoAIConfig) (*gcp.VideoAIResult, error) {
	f.called = true
	f.lastGCSURI = gcsURI
	return &gcp.VideoAIResult{}, nil
}

func (f *fakeVideo) Close() error { return nil }

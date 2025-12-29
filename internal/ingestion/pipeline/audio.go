package pipeline

import (
	"context"
	"fmt"
	"os"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

func (s *service) handleAudio(ctx context.Context, mf *types.MaterialFile, audioPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "audio"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	if s.ex.Speech == nil {
		return nil, assets, []string{"speech provider unavailable"}, diag, nil
	}

	key := fmt.Sprintf("%s/derived/audio/audio.wav", mf.StorageKey)
	if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: ctx}, key, audioPath); err != nil {
		warnings = append(warnings, "upload audio failed: "+err.Error())
	} else {
		assets = append(assets, AssetRef{
			Kind: "audio",
			Key:  key,
			URL:  s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
		})
	}

	gcsURI := ""
	if s.ex.MaterialBucketName != "" {
		gcsURI = fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, key)
	}

	cfg := gcp.SpeechConfig{
		LanguageCode:               "en-US",
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets:      true,
		EnableSpeakerDiarization:   true,
		MinSpeakerCount:            1,
		MaxSpeakerCount:            6,
	}

	var res *gcp.SpeechResult
	if gcsURI != "" {
		r, err := s.ex.Speech.TranscribeAudioGCS(ctx, gcsURI, cfg)
		res = r
		if err != nil {
			warnings = append(warnings, "speech transcription failed: "+err.Error())
			diag["speech_error"] = err.Error()
		}
	} else {
		b, readErr := os.ReadFile(audioPath)
		if readErr != nil {
			return nil, assets, append(warnings, "read audio bytes failed: "+readErr.Error()), diag, nil
		}
		r, err := s.ex.Speech.TranscribeAudioBytes(ctx, b, "audio/wav", cfg)
		res = r
		if err != nil {
			warnings = append(warnings, "speech transcription failed: "+err.Error())
			diag["speech_error"] = err.Error()
		}
	}

	if res != nil {
		diag["speech_primary_text_len"] = len(res.PrimaryText)
		for _, sg := range res.Segments {
			if sg.Metadata == nil {
				sg.Metadata = map[string]any{}
			}
			sg.Metadata["kind"] = "transcript"
			sg.Metadata["provider"] = "gcp_speech"
			segs = append(segs, sg)
		}
	}

	return segs, assets, warnings, diag, nil
}

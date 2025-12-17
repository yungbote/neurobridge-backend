package app

import (
	"fmt"
	"os"
	"strings"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/redis"
	"github.com/yungbote/neurobridge-backend/internal/logger"
)

type Clients struct {
	SSEBus:						redis.SSEBus
	OpenaiClient			openai.Client
	OpenaiCaption			openai.Caption
	GcpBucket					gcp.BucketService
	GcpDocument				gcp.Document
	GcpSpeech					gcp.Speech
	GcpVideo					gcp.Video
	GcpVision					gcp.Vision
}

func wireClients(log *logger.Logger) (Clients, error) {
	log.Info("Wiring clients...")

	// Redis
	var bus redis.SSEBus
	if strings.TrimSpace(os.Getenv("REDIS_ADDR")) != "" {
		b, err := redis.NewSSEBus(log)
		if err != nil {
			return Clients{}, fmt.Errorf("init redis SSE bus: %w", err)
		}
		bus = b
	}

	// Gcs
	bucket, err := gcp.NewBucketService(log)
	if err != nil {
		return Clients{}, fmt.Errorf("init bucket client: %w", err)
	}

	// Openai
	openaiClient, err := openai.NewClient(log)
	if err != nil {
		return Clients{}, fmt.Errorf("init openai client: %w", err)
	}
	caption, err := openai.NewCaptionProviderService(log, openaiClient)
	if err != nil {
		return Clients{}, fmt.Errorf("init caption client: %w", err)
	}

	// Gcp
	vision, err := gcp.NewVision(log)
	if err != nil {
		return Clients{}, fmt.Errorf("init vision client: %w", err)
	}
	document, err := gcp.NewDocument(log)
	if err != nil {
		_ = vision.Close()
		return Clients{}, fmt.Errorf("init document client: %w", err)
	}
	speech, err := gcp.NewSpeech(log)
	if err != nil {
		_ = document.Close()
		_ = vision.Close()
		return Clients{}, fmt.Errorf("init speech client: %w", err)
	}
	video, err := gcp.NewVideo(log)
	if err != nil {
		_ = document.Close()
		_ = vision.Close()
		_ = speech.Close()
		return Clients{}, fmt.Errorf("init video client: %w", err)
	}

	return Clients{
		SSEBus:					bus,
		OpenaiClient:		openaiClient,
		OpenaiCaption:	caption,
		GcpBucket:			bucket,
		GcpDocument:		document,
		GcpSpeech:			speech,
		GcpVideo:				video,
		GcpVision:			vision,
	}, nil
}

func (c *Clients) Close() {
	if c == nil { return }
	if c.SSEBus != nil { _ = c.SSEBus.Close() }
	if c.GcpVideo != nil { _ = c.GcpVideo.Close() }
	if c.GcpSpeech != nil { _ = c.GcpSpeech.Close() }
	if c.GcpDocument != nil { _ = c.GcpDocument.Close() }
	if c.GcpVision != nil { _ = c.GcpVision.Close() }
}











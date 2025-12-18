package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
)

type Clients struct {
	// Redis
	SSEBus bus.Bus

	// OpenAI
	OpenaiClient  openai.Client
	OpenaiCaption openai.Caption

	// Pinecone
	PineconeClient      pinecone.Client
	PineconeVectorStore pinecone.VectorStore

	// GCP
	GcpBucket   gcp.BucketService
	GcpDocument gcp.Document
	GcpSpeech   gcp.Speech
	GcpVideo    gcp.Video
	GcpVision   gcp.Vision

	// Local Media
	LMTools localmedia.MediaToolsService
}

func wireClients(log *logger.Logger) (Clients, error) {
	log.Info("Wiring clients...")

	var out Clients

	// ---------------- Redis (optional on API; required on worker) ----------------
	if strings.TrimSpace(os.Getenv("REDIS_ADDR")) != "" {
		b, err := bus.NewRedisBus(log)
		if err != nil {
			return Clients{}, fmt.Errorf("init redis SSE bus: %w", err)
		}
		out.SSEBus = b
	}

	// ---------------- GCP Bucket ----------------
	bucket, err := gcp.NewBucketService(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init gcp bucket client: %w", err)
	}
	out.GcpBucket = bucket

	// ---------------- OpenAI ----------------
	oa, err := openai.NewClient(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init openai client: %w", err)
	}
	out.OpenaiClient = oa

	cap, err := openai.NewCaption(log, oa)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init openai caption: %w", err)
	}
	out.OpenaiCaption = cap

	// ---------------- Pinecone ---------------------
	if strings.TrimSpace(os.Getenv("PINECONE_API_KEY")) != "" {
		pc, err := pinecone.New(log, pinecone.Config{
			APIKey:     strings.TrimSpace(os.Getenv("PINECONE_API_KEY")),
			APIVersion: strings.TrimSpace(os.Getenv("PINECONE_API_VERSION")),
			BaseURL:    strings.TrimSpace(os.Getenv("PINECONE_BASE_URL")),
			Timeout:    30 * time.Second,
		})
		if err != nil {
			out.Close()
			return Clients{}, fmt.Errorf("init pinecone client: %w", err)
		}
		out.PineconeClient = pc

		vs, err := pinecone.NewVectorStore(log, pc)
		if err != nil {
			out.Close()
			return Clients{}, fmt.Errorf("init pinecone vector store: %w", err)
		}
		out.PineconeVectorStore = vs
	} else {
		log.Warn("PINECONE_API_KEY not set; vector search disabled")
	}

	// ---------------- GCP Providers ----------------
	vision, err := gcp.NewVision(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init gcp vision: %w", err)
	}
	out.GcpVision = vision

	doc, err := gcp.NewDocument(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init gcp document: %w", err)
	}
	out.GcpDocument = doc

	speech, err := gcp.NewSpeech(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init gcp speech: %w", err)
	}
	out.GcpSpeech = speech

	video, err := gcp.NewVideo(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init gcp video: %w", err)
	}
	out.GcpVideo = video

	// ---------------- Local Media Tools ----------------
	out.LMTools = localmedia.New(log)

	return out, nil
}

func (c *Clients) Close() {
	if c == nil {
		return
	}
	if c.SSEBus != nil {
		_ = c.SSEBus.Close()
		c.SSEBus = nil
	}
	if c.GcpVideo != nil {
		_ = c.GcpVideo.Close()
		c.GcpVideo = nil
	}
	if c.GcpSpeech != nil {
		_ = c.GcpSpeech.Close()
		c.GcpSpeech = nil
	}
	if c.GcpDocument != nil {
		_ = c.GcpDocument.Close()
		c.GcpDocument = nil
	}
	if c.GcpVision != nil {
		_ = c.GcpVision.Close()
		c.GcpVision = nil
	}

	c.PineconeClient = nil
	c.PineconeVectorStore = nil
	c.OpenaiClient = nil
	c.OpenaiCaption = nil
}

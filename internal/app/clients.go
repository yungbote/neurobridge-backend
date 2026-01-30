package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/platform/sendgrid"
	"github.com/yungbote/neurobridge-backend/internal/platform/twilio"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
	"github.com/yungbote/neurobridge-backend/internal/temporalx"

	temporalsdkclient "go.temporal.io/sdk/client"
)

type Clients struct {
	// Redis
	SSEBus bus.Bus

	// Neo4j (graph)
	Neo4j *neo4jdb.Client

	// OpenAI
	OpenaiClient        openai.Client
	OpenaiCaption       openai.Caption
	StructureExtractAI  openai.Client

	// Pinecone
	PineconeClient      pinecone.Client
	PineconeVectorStore pinecone.VectorStore

	// GCP
	GcpBucket   gcp.BucketService
	GcpDocument gcp.Document
	GcpSpeech   gcp.Speech
	GcpVideo    gcp.Video
	GcpVision   gcp.Vision

	// Twilio
	TwilioClient twilio.Client

	// Sendgrid
	SendgridClient sendgrid.Client

	// Local Media
	LMTools localmedia.MediaToolsService

	// Temporal
	Temporal temporalsdkclient.Client
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

	// ---------------- Neo4j (optional) ----------------
	neo, err := neo4jdb.NewFromEnv(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init neo4j client: %w", err)
	}
	if neo != nil {
		out.Neo4j = neo
		log.Info("Neo4j enabled", "database", neo.Database)
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

	structureModel := strings.TrimSpace(os.Getenv("STRUCTURE_EXTRACT_MODEL"))
	if structureModel != "" && strings.TrimSpace(structureModel) != strings.TrimSpace(os.Getenv("OPENAI_MODEL")) {
		if sc, err := openai.NewClientWithModel(log, structureModel); err == nil {
			out.StructureExtractAI = sc
		} else {
			log.Warn("init structure extract client failed; falling back to default", "error", err)
			out.StructureExtractAI = oa
		}
	} else {
		out.StructureExtractAI = oa
	}

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

	// ---------------- Temporal ----------------
	tc, err := temporalx.NewClient(log)
	if err != nil {
		out.Close()
		return Clients{}, fmt.Errorf("init temporal client: %w", err)
	}
	out.Temporal = tc

	// ----------------------- Twilio -------------------
	if strings.TrimSpace(os.Getenv("TWILIO_ACCOUNT_SID")) != "" {
		tw, err := twilio.NewFromEnv(log)
		if err != nil {
			out.Close()
			return Clients{}, fmt.Errorf("init twilio client: %w", err)
		}
		out.TwilioClient = tw
	} else {
		log.Warn("TWILIO_ACCOUNT_SID not set; SMS disabled")
	}

	// ---------------- SendGrid -------------------------
	if strings.TrimSpace(os.Getenv("SENDGRID_API_KEY")) != "" {
		sg, err := sendgrid.NewFromEnv(log)
		if err != nil {
			out.Close()
			return Clients{}, fmt.Errorf("init sendgrid client: %w", err)
		}
		out.SendgridClient = sg
	} else {
		log.Warn("SENDGRID_API_KEY not set; email disabled")
	}

	return out, nil
}

func (c *Clients) Close() {
	if c == nil {
		return
	}
	if c.Neo4j != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.Neo4j.Close(ctx)
		cancel()
		c.Neo4j = nil
	}
	if c.Temporal != nil {
		c.Temporal.Close()
		c.Temporal = nil
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

	c.TwilioClient = nil
	c.SendgridClient = nil

	c.PineconeClient = nil
	c.PineconeVectorStore = nil
	c.OpenaiClient = nil
	c.OpenaiCaption = nil
	c.StructureExtractAI = nil

	c.LMTools = nil
}

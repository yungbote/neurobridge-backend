package pinecone

import (
	"context"
	"fmt"
	"os"
	"strings"
	"github.com/yungbote/neurobridge-backend/internal/logger"
)

type VectorStore interface {
	Upsert(ctx context.Context, namespace string, vectors []Vector) error
	QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error)
}

type vectorStore struct {
	log       *logger.Logger
	pc        Client
	indexName string
	indexHost string
	nsPrefix  string
}

func NewVectorStore(log *logger.Logger, pc Client) (VectorStore, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	if pc == nil {
		return nil, fmt.Errorf("pinecone client required")
	}

	indexName := strings.TrimSpace(os.Getenv("PINECONE_INDEX_NAME"))
	if indexName == "" {
		return nil, fmt.Errorf("missing PINECONE_INDEX_NAME")
	}

	host := strings.TrimSpace(os.Getenv("PINECONE_INDEX_HOST"))

	nsPrefix := strings.TrimSpace(os.Getenv("PINECONE_NAMESPACE_PREFIX"))
	if nsPrefix == "" {
		nsPrefix = "nb"
	}

	// If host missing, bootstrap via describe_index (fine for local/dev; avoid in prod). 
	if host == "" {
		desc, err := pc.DescribeIndex(context.Background(), indexName)
		if err != nil {
			return nil, fmt.Errorf("pinecone describe_index failed: %w", err)
		}
		host = strings.TrimSpace(desc.Host)
		if host == "" {
			return nil, fmt.Errorf("pinecone describe_index returned empty host")
		}
		log.Warn("PINECONE_INDEX_HOST not set; resolved via describe_index (avoid this in production)",
			"index_name", indexName,
			"index_host", host,
		)
	}

	return &vectorStore{
		log:       log.With("service", "PineconeVectorStore"),
		pc:        pc,
		indexName: indexName,
		indexHost: host,
		nsPrefix:  nsPrefix,
	}, nil
}

func (s *vectorStore) Upsert(ctx context.Context, namespace string, vectors []Vector) error {
	if s == nil || s.pc == nil {
		return nil
	}
	ns := s.qualifyNamespace(namespace)
	_, err := s.pc.UpsertVectors(ctx, s.indexHost, UpsertRequest{
		Namespace: ns,
		Vectors:   vectors,
	})
	return err
}

func (s *vectorStore) QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error) {
	if s == nil || s.pc == nil {
		return nil, fmt.Errorf("vector store unavailable")
	}
	ns := s.qualifyNamespace(namespace)
	resp, err := s.pc.Query(ctx, s.indexHost, QueryRequest{
		Namespace:       ns,
		Vector:          q,
		TopK:            topK,
		Filter:          filter,
		IncludeValues:   false,
		IncludeMetadata: false,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		if strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func (s *vectorStore) qualifyNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return s.nsPrefix
	}
	return s.nsPrefix + ":" + ns
}











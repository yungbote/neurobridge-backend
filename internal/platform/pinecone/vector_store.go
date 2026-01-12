package pinecone

import (
	"context"
	"fmt"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"os"
	"strings"
)

type VectorStore interface {
	Upsert(ctx context.Context, namespace string, vectors []Vector) error
	// QueryMatches returns IDs with their similarity scores (higher is better).
	QueryMatches(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]VectorMatch, error)
	QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error)
	DeleteIDs(ctx context.Context, namespace string, ids []string) error
}

type VectorMatch struct {
	ID    string
	Score float64
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
	matches, err := s.QueryMatches(ctx, namespace, q, topK, filter)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func (s *vectorStore) QueryMatches(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]VectorMatch, error) {
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
	out := make([]VectorMatch, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		out = append(out, VectorMatch{ID: m.ID, Score: m.Score})
	}
	return out, nil
}

func (s *vectorStore) DeleteIDs(ctx context.Context, namespace string, ids []string) error {
	if s == nil || s.pc == nil {
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	ns := s.qualifyNamespace(namespace)
	_, err := s.pc.DeleteVectors(ctx, s.indexHost, DeleteRequest{
		Namespace: ns,
		IDs:       ids,
	})
	return err
}

func (s *vectorStore) qualifyNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return s.nsPrefix
	}
	return s.nsPrefix + ":" + ns
}

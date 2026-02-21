package app

import (
	"context"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type instrumentedVectorStore struct {
	provider string
	inner    pinecone.VectorStore
	metrics  *observability.Metrics
}

func instrumentVectorStore(provider string, inner pinecone.VectorStore) pinecone.VectorStore {
	if inner == nil {
		return nil
	}
	return &instrumentedVectorStore{
		provider: provider,
		inner:    inner,
		metrics:  observability.Current(),
	}
}

func (s *instrumentedVectorStore) Upsert(ctx context.Context, namespace string, vectors []pinecone.Vector) error {
	start := time.Now()
	err := s.inner.Upsert(ctx, namespace, vectors)
	s.observe("upsert", err, time.Since(start))
	return err
}

func (s *instrumentedVectorStore) QueryMatches(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]pinecone.VectorMatch, error) {
	start := time.Now()
	out, err := s.inner.QueryMatches(ctx, namespace, q, topK, filter)
	s.observe("query_matches", err, time.Since(start))
	return out, err
}

func (s *instrumentedVectorStore) QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error) {
	start := time.Now()
	out, err := s.inner.QueryIDs(ctx, namespace, q, topK, filter)
	s.observe("query_ids", err, time.Since(start))
	return out, err
}

func (s *instrumentedVectorStore) DeleteIDs(ctx context.Context, namespace string, ids []string) error {
	start := time.Now()
	err := s.inner.DeleteIDs(ctx, namespace, ids)
	s.observe("delete_ids", err, time.Since(start))
	return err
}

func (s *instrumentedVectorStore) observe(operation string, err error, dur time.Duration) {
	if s == nil || s.metrics == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	s.metrics.ObserveVectorStoreOperation(s.provider, operation, status, dur)
}

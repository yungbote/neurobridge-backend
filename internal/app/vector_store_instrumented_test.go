package app

import (
	"context"
	"errors"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

func TestInstrumentVectorStorePassThrough(t *testing.T) {
	inner := &fakeInstrumentedInner{}
	vs := instrumentVectorStore("qdrant", inner)
	if vs == nil {
		t.Fatalf("instrumentVectorStore: expected non-nil wrapper")
	}

	err := vs.Upsert(context.Background(), "ns", []pinecone.Vector{{ID: "v1", Values: []float32{1, 2, 3}}})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	_, err = vs.QueryMatches(context.Background(), "ns", []float32{1, 2, 3}, 3, nil)
	if err != nil {
		t.Fatalf("QueryMatches: %v", err)
	}
	_, err = vs.QueryIDs(context.Background(), "ns", []float32{1, 2, 3}, 3, nil)
	if err != nil {
		t.Fatalf("QueryIDs: %v", err)
	}
	err = vs.DeleteIDs(context.Background(), "ns", []string{"v1"})
	if err != nil {
		t.Fatalf("DeleteIDs: %v", err)
	}

	if inner.upsertCalls != 1 || inner.queryMatchesCalls != 1 || inner.queryIDsCalls != 1 || inner.deleteCalls != 1 {
		t.Fatalf(
			"unexpected call counts: upsert=%d query_matches=%d query_ids=%d delete=%d",
			inner.upsertCalls,
			inner.queryMatchesCalls,
			inner.queryIDsCalls,
			inner.deleteCalls,
		)
	}
}

func TestInstrumentVectorStoreErrorPassThrough(t *testing.T) {
	want := errors.New("delete failed")
	inner := &fakeInstrumentedInner{deleteErr: want}
	vs := instrumentVectorStore("qdrant", inner)

	err := vs.DeleteIDs(context.Background(), "ns", []string{"v1"})
	if !errors.Is(err, want) {
		t.Fatalf("DeleteIDs: expected wrapped error %v, got=%v", want, err)
	}
}

type fakeInstrumentedInner struct {
	upsertCalls       int
	queryMatchesCalls int
	queryIDsCalls     int
	deleteCalls       int

	deleteErr error
}

func (f *fakeInstrumentedInner) Upsert(_ context.Context, _ string, _ []pinecone.Vector) error {
	f.upsertCalls++
	return nil
}

func (f *fakeInstrumentedInner) QueryMatches(_ context.Context, _ string, _ []float32, _ int, _ map[string]any) ([]pinecone.VectorMatch, error) {
	f.queryMatchesCalls++
	return []pinecone.VectorMatch{{ID: "v1", Score: 0.9}}, nil
}

func (f *fakeInstrumentedInner) QueryIDs(_ context.Context, _ string, _ []float32, _ int, _ map[string]any) ([]string, error) {
	f.queryIDsCalls++
	return []string{"v1"}, nil
}

func (f *fakeInstrumentedInner) DeleteIDs(_ context.Context, _ string, _ []string) error {
	f.deleteCalls++
	return f.deleteErr
}

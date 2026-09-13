package longterm

import (
	"context"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/store"
	"agentmem/types"
)

type Retriever struct {
	vector   store.VectorStore
	embedder llm.Embedder
	topK     int
	logger   observability.Logger
}

func NewRetriever(v store.VectorStore, emb llm.Embedder, topK int, logger observability.Logger) *Retriever {
	if topK <= 0 {
		topK = 10
	}
	return &Retriever{vector: v, embedder: emb, topK: topK, logger: logger}
}

func (r *Retriever) Retrieve(ctx context.Context, query, userID string) ([]*types.Memory, error) {
	if r.embedder == nil {
		return nil, nil
	}

	qvec, err := r.embedder.Embed(ctx, query)
	if err != nil {
		r.logger.Warnf("embed query failed: %v", err)
		return nil, err
	}

	mems, err := r.vector.Search(ctx, qvec, r.topK, store.Filter{
		UserID:            userID,
		ExcludeSuperseded: true,
	})
	if err != nil {
		r.logger.Warnf("vector search failed: %v", err)
		return nil, err
	}

	return mems, nil
}

func (r *Retriever) Available() bool { return r.embedder != nil }

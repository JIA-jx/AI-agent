package kbs

import (
	"context"
	"fmt"
	"sort"

	"github.com/cloudwego/eino/schema"
	"thunder/logs"
)

type HybridConfig struct {
	VectorTopK  int
	BM25TopK    int
	RRFConstant float64
	FinalTopK   int

	Reranker     Reranker
	EnableRerank bool
}

// HybridRetriever 两阶段混合检索编排器：
//	 Stage 1: Vector topK + BM25 topK → RRF 融合去重
//	 Stage 2: Rerank cross-encoder 精排 → final topK
type HybridRetriever struct {
	vector VectorRetriever
	bm25   BM25Retriever
	cfg    HybridConfig
}

type VectorRetriever interface {
	Search(ctx context.Context, query string, topK int, filters SearchFilter) ([]*schema.Document, error)
}

type BM25Retriever interface {
	Search(ctx context.Context, query string, topK int, filters SearchFilter) ([]*schema.Document, error)
}

func NewHybridRetriever(vector VectorRetriever, bm25 BM25Retriever, cfg HybridConfig) *HybridRetriever {
	if cfg.VectorTopK <= 0 {
		cfg.VectorTopK = 20
	}
	if cfg.BM25TopK <= 0 {
		cfg.BM25TopK = 20
	}
	if cfg.RRFConstant <= 0 {
		cfg.RRFConstant = 60
	}
	if cfg.FinalTopK <= 0 {
		cfg.FinalTopK = 5
	}
	if cfg.Reranker == nil {
		cfg.Reranker = NewNoneReranker()
	}
	return &HybridRetriever{
		vector: vector,
		bm25:   bm25,
		cfg:    cfg,
	}
}

func (h *HybridRetriever) Search(ctx context.Context, query string, filters SearchFilter) ([]*schema.Document, error) {
	// ---- Stage 1: 并行召回 ----
	var vectorDocs, bm25Docs []*schema.Document
	var vectorErr, bm25Err error

	if h.vector != nil {
		vectorDocs, vectorErr = h.vector.Search(ctx, query, h.cfg.VectorTopK, filters)
		if vectorErr != nil {
			logs.Warnf("hybrid vector search failed, will fallback to BM25 only: %v", vectorErr)
		}
	}
	if h.bm25 != nil {
		bm25Docs, bm25Err = h.bm25.Search(ctx, query, h.cfg.BM25TopK, filters)
		if bm25Err != nil {
			logs.Warnf("hybrid bm25 search failed, will fallback to vector only: %v", bm25Err)
		}
	}

	if len(vectorDocs) == 0 && len(bm25Docs) == 0 {
		return nil, fmt.Errorf("all retrievers failed or returned empty: vector_err=%v bm25_err=%v", vectorErr, bm25Err)
	}

	documents := rrfFusion(vectorDocs, bm25Docs, h.cfg.RRFConstant)
	if len(documents) == 0 {
		return nil, nil
	}

	// ---- Stage 2: Rerank 精排 ----
	if h.cfg.EnableRerank && h.cfg.Reranker.Available() {
		rerankInputK := h.cfg.VectorTopK + h.cfg.BM25TopK
		if rerankInputK > len(documents) {
			rerankInputK = len(documents)
		}
		topDocuments := documents[:rerankInputK]

		reranked, err := h.cfg.Reranker.Rerank(ctx, query, topDocuments, h.cfg.FinalTopK)
		if err == nil && len(reranked) > 0 {
			return reranked, nil
		}
		logs.Warnf("rerank failed, using RRF result directly: %v", err)
	}

	finalTopK := h.cfg.FinalTopK
	if finalTopK > len(documents) {
		finalTopK = len(documents)
	}
	return documents[:finalTopK], nil
}

// score(d) = Σ 1 / (k + rank_i(d))
func rrfFusion(vectorDocs, bm25Docs []*schema.Document, k float64) []*schema.Document {
	type scored struct {
		doc      *schema.Document
		rrf      float64
		bestRank float64
		source   []string
	}
	rrfMap := make(map[string]*scored)

	for rank, doc := range vectorDocs {
		idx := float64(rank + 1)
		s, ok := rrfMap[doc.ID]
		if !ok {
			s = &scored{doc: doc, bestRank: idx}
			rrfMap[doc.ID] = s
		}
		s.rrf += 1.0 / (k + idx)
		s.source = append(s.source, "vector")
		if idx < s.bestRank {
			s.bestRank = idx
		}
	}
	for rank, doc := range bm25Docs {
		idx := float64(rank + 1)
		s, ok := rrfMap[doc.ID]
		if !ok {
			s = &scored{doc: doc, bestRank: idx}
			rrfMap[doc.ID] = s
		}
		s.rrf += 1.0 / (k + idx)
		s.source = append(s.source, "bm25")
		if idx < s.bestRank {
			s.bestRank = idx
		}
	}

	result := make([]*scored, 0, len(rrfMap))
	for _, s := range rrfMap {
		result = append(result, s)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].rrf != result[j].rrf {
			return result[i].rrf > result[j].rrf
		}
		return result[i].bestRank < result[j].bestRank
	})

	out := make([]*schema.Document, 0, len(result))
	for _, s := range result {
		s.doc.WithScore(s.rrf)
		out = append(out, s.doc)
	}
	return out
}

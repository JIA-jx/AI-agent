package kbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"thunder/logs"

	"github.com/cloudwego/eino/schema"
)

type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []*schema.Document, topK int) ([]*schema.Document, error)
	Available() bool
}

type RerankerConfig struct {
	Endpoint string
	Model    string
	APIKey   string
	Timeout  time.Duration
	Enabled  bool
}

// HTTPReranker 基于 HTTP 的 cross-encoder 重排器
// 兼容 BGE-reranker server / SiliconFlow rerank / 任何 OpenAI-compatible rerank endpoint
type HTTPReranker struct {
	cfg    RerankerConfig
	client *http.Client
}

func (r *HTTPReranker) Available() bool {
	return r.cfg.Enabled && r.cfg.Endpoint != ""
}

type rerankRequest struct {
	Model     string   `json:"model,omitempty"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopK      int      `json:"top_k,omitempty"`
}

type rerankResult struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

type rerankResponse struct {
	Results []rerankResult `json:"results"`
}

// Rerank 调用远程 cross-encoder 服务进行重排
func (r *HTTPReranker) Rerank(ctx context.Context, query string, documents []*schema.Document, topK int) ([]*schema.Document, error) {
	if !r.Available() {
		logs.Infof("reranker disabled, returning original topK=%d", topK)
		return sliceTopK(documents, topK), nil
	}
	if len(documents) == 0 {
		return documents, nil
	}

	docs := make([]string, 0, len(documents))
	for _, c := range documents {
		docs = append(docs, c.Content)
	}

	reqBody := rerankRequest{
		Model:     r.cfg.Model,
		Query:     query,
		Documents: docs,
		TopK:      topK,
	}
	raw, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("rerank build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logs.Warnf("rerank http call failed, fallback to original order: %v", err)
		return sliceTopK(documents, topK), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logs.Warnf("rerank http status %d, fallback to original order: %s", resp.StatusCode, string(body))
		return sliceTopK(documents, topK), nil
	}

	var rResp rerankResponse
	if err := json.Unmarshal(body, &rResp); err != nil {
		logs.Warnf("rerank decode response failed, fallback to original order: %v", err)
		return sliceTopK(documents, topK), nil
	}

	out := make([]*schema.Document, 0, len(rResp.Results))
	for _, res := range rResp.Results {
		if res.Index >= 0 && res.Index < len(documents) {
			doc := documents[res.Index]
			doc.WithScore(res.Score)
			out = append(out, doc)
		}
	}

	out = sliceTopK(out, topK)
	return out, nil
}

func sliceTopK[T any](items []T, topK int) []T {
	if topK <= 0 || topK >= len(items) {
		return items
	}
	return items[:topK]
}

// NoneReranker 不做任何重排，保持原顺序（用于未配置 rerank 服务的降级场景）
type NoneReranker struct{}

func NewNoneReranker() *NoneReranker    { return &NoneReranker{} }
func (n *NoneReranker) Available() bool { return false }
func (n *NoneReranker) Rerank(_ context.Context, _ string, documents []*schema.Document, topK int) ([]*schema.Document, error) {
	return sliceTopK(documents, topK), nil
}

// ---------------------------------------------------------------------------
// 本地 Reranker：使用 LLM 做轻量级重排（对没有 cross-encoder 服务的场景降级使用）

// LLMReranker 用 ChatModel 做 rerank（prompt engineering 方案）
// 精度不如 cross-encoder，但零依赖即可用
type LLMReranker struct {
	model LLMScorer
	topK  int
}

type LLMScorer interface {
	Score(ctx context.Context, query string, documents []string) ([]float64, error)
}

func (r *LLMReranker) Available() bool { return r.model != nil }

type Scored struct {
	idx   int
	score float64
}

func (r *LLMReranker) Rerank(ctx context.Context, query string, documents []*schema.Document, topK int) ([]*schema.Document, error) {
	if !r.Available() || len(documents) == 0 {
		return sliceTopK(documents, topK), nil
	}

	docs := make([]string, 0, len(documents))
	for _, c := range documents {
		docs = append(docs, c.Content)
	}

	scores, err := r.model.Score(ctx, query, docs)
	if err != nil {
		logs.Warnf("llm rerank failed, fallback: %v", err)
		return sliceTopK(documents, topK), nil
	}

	var ranked []Scored
	for i, s := range scores {
		ranked = append(ranked, Scored{idx: i, score: s})
	}
	sortScored(ranked)

	out := make([]*schema.Document, 0, topK)
	for i := 0; i < len(ranked) && i < topK; i++ {
		doc := documents[ranked[i].idx]
		doc.WithScore(ranked[i].score)
		out = append(out, doc)
	}
	return out, nil
}

func sortScored(s []Scored) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j].score > s[i].score {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

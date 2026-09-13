package llm

import (
	"context"
	"errors"
	"math"

	"agentmem/config"

	oa "github.com/sashabaranov/go-openai"
)

type Client interface {
	Complete(ctx context.Context, system, user string) (string, error)
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Available() bool
}

func NewClient(cfg config.Config) Client {
	if !cfg.HasLLM() {
		return NewNoopClient(cfg.EmbedDim)
	}

	return NewOpenAIClient(cfg)
}

type openAIClient struct {
	chat  string
	embed string
	cli   *oa.Client
}

func NewOpenAIClient(cfg config.Config) *openAIClient {
	co := oa.DefaultConfig(cfg.OpenAIKey)
	if cfg.OpenAIBaseURL != "" {
		co.BaseURL = cfg.OpenAIBaseURL
	}

	return &openAIClient{chat: cfg.ChatModel, embed: cfg.EmbedModel, cli: oa.NewClientWithConfig(co)}
}

func (c *openAIClient) Available() bool { return true }

func (c *openAIClient) Complete(ctx context.Context, system, user string) (string, error) {
	req := oa.ChatCompletionRequest{
		Model: c.chat,
		Messages: []oa.ChatCompletionMessage{
			{Role: oa.ChatMessageRoleSystem, Content: system},
			{Role: oa.ChatMessageRoleUser, Content: user},
		},
		Temperature: 0.3,
	}
	resp, err := c.cli.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty completion choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *openAIClient) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, errors.New("empty embedding result")
	}
	return vecs[0], nil
}

func (c *openAIClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	resp, err := c.cli.CreateEmbeddings(ctx, oa.EmbeddingRequest{
		Model: oa.EmbeddingModel(c.embed),
		Input: texts,
	})
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

type noopClient struct{ dim int }

func NewNoopClient(dim int) Client { return noopClient{dim: dim} }

func (noopClient) Available() bool { return false }

func (noopClient) Complete(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (n noopClient) Embed(_ context.Context, text string) ([]float32, error) {
	return hashVector(text, n.dim), nil
}

func (n noopClient) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = hashVector(t, n.dim)
	}
	return out, nil
}

// hashVector 由文本生成确定性归一化向量，使相同文本相似度为 1
func hashVector(text string, dim int) []float32 {
	v := make([]float32, dim)
	if dim == 0 {
		return v
	}

	var h uint32 = 2166136261
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
	}
	for i := 0; i < dim; i++ {
		h = (h ^ uint32(i)) * 2654435761
		v[i] = float32(float64(h%1000) / 1000.0)
	}

	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	if norm == 0 {
		return v
	}

	s := 1.0 / math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) * s)
	}
	return v
}

/*
FNV-1a 哈希：

初始值 2166136261（FNV offset basis）。

每字节：XOR 后乘 16777619（FNV prime）。

得到文本的 32 位哈希 h。

对每个维度，用 h 和索引 i 再混合，生成 [0,1) 的伪随机值。

2654435761 是 Knuth 乘法哈希常数。

h % 1000 / 1000.0 映射到 [0, 0.999]。


L2 归一化：

计算平方和 norm。

norm == 0 直接返回（防除零）。

缩放因子 s = 1/√norm。

每个元素乘 s，使向量模长为 1。

归一化后，余弦相似度 = 点积，pgvector 的 <=> 才有正确语义。

性质：

确定性：相同文本 → 相同向量。

归一化：模长 = 1。

相同文本相似度 = 1（点积 = 1）。

不同文本相似度 ≈ 随机（无语义）。


*/

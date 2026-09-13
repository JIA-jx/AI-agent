package longterm

import (
	"context"
	"encoding/json"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/store"
	"agentmem/types"
)

// Extractor 从对话中抽取值得长期记住的事实
type Extractor struct {
	cmpl     llm.Completer
	embedder llm.Embedder
	vector   store.VectorStore
	scorer   *Scorer
	merger   *Merger
	logger   observability.Logger
}

func NewExtractor(cmpl llm.Completer, emb llm.Embedder, v store.VectorStore, sc *Scorer, mg *Merger, logger observability.Logger) *Extractor {
	return &Extractor{cmpl: cmpl, embedder: emb, vector: v, scorer: sc, merger: mg, logger: logger}
}

type extractItem struct {
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
}

// Extract 从 conversation 文本抽取长期记忆并写入向量存储，返回新增记忆
func (e *Extractor) Extract(ctx context.Context, userID, sessionID, conversation string) ([]*types.Memory, error) {
	if e == nil || e.cmpl == nil || strings.TrimSpace(conversation) == "" {
		return nil, nil
	}

	system, user := llm.ExtractLongTermPrompt(conversation)
	out, err := e.cmpl.Complete(ctx, system, user)
	if err != nil {
		e.logger.Warnf("long-term extract failed: %v", err)
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	raw := extractJSON(out)
	if raw == "" {
		return nil, nil
	}

	var items []extractItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		e.logger.Warnf("parse extracted json failed: %v (raw=%q)", err, raw)
		return nil, err
	}

	var mems []*types.Memory
	for _, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}

		mem := types.NewMemory(userID, sessionID,
			it.Content, types.MemoryTypeLong)
		if it.Importance > 0 {
			mem.Importance = types.Clamp(it.Importance, 0, 10)
		} else if e.scorer != nil {
			mem.Importance = e.scorer.Score(ctx, it.Content)
		}

		if mem.Importance == 0 {
			mem.Importance = 5
		}

		// embedder 存在时做向量化，失败静默忽略（记忆仍保留，只是无向量，后续 Merger 会直接保留它）
		if e.embedder != nil {
			if emb, err := e.embedder.Embed(ctx, it.Content); err == nil {
				mem.Embedding = emb
			}
		}

		mems = append(mems, mem)
	}

	if e.merger != nil {
		existing, _ := e.vector.List(ctx, store.Filter{UserID: userID, Type: types.MemoryTypeLong})
		mems = e.merger.Dedup(mems, existing)
	}

	for _, mem := range mems {
		if err := e.vector.Add(ctx, mem); err != nil {
			e.logger.Warnf("store long-term memory failed: %v", err)
			continue
		}
	}
	return mems, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}

	if start := strings.IndexAny(s, "[{"); start >= 0 {
		if end := strings.LastIndexAny(s, "]}"); end > start {
			return s[start : end+1]
		}
	}
	return ""
}

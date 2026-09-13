package reflection

import (
	"context"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/store"
	"agentmem/types"
)

// Synthesizer 对每个反思问题检索相关记忆并合成洞察
type Synthesizer struct {
	cmpl     llm.Completer
	embedder llm.Embedder
	vector   store.VectorStore
	topK     int
	logger   observability.Logger
}

func NewSynthesizer(cmpl llm.Completer, emb llm.Embedder, v store.VectorStore, topK int, logger observability.Logger) *Synthesizer {
	if topK <= 0 {
		topK = 5
	}
	return &Synthesizer{cmpl: cmpl, embedder: emb, vector: v, topK: topK, logger: logger}
}

// Synthesize 对每个问题检索相关记忆并合成洞察，写入向量存储，返回新增洞察
func (s *Synthesizer) Synthesize(ctx context.Context, userID string, questions []string) ([]*types.Memory, error) {
	if s == nil || s.cmpl == nil || len(questions) == 0 {
		return nil, nil
	}

	var insights []*types.Memory
	for _, q := range questions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}

		retrieved := s.retrieve(ctx, q, userID)
		evidence := joinMemories(retrieved)
		system, user := llm.ReflectionInsightPrompt(q, evidence)
		out, err := s.cmpl.Complete(ctx, system, user)
		if err != nil {
			s.logger.Warnf("synthesize insight failed: %v", err)
			continue
		}
		out = strings.TrimSpace(out)
		if out == "" {
			continue
		}

		mem := types.NewMemory(userID, "", out, types.MemoryTypeInsight)
		mem.Importance = types.Clamp(avgImportance(retrieved)+1, 0, 10)
		for _, r := range retrieved {
			mem.SourceIDs = append(mem.SourceIDs, r.ID)
		}

		if s.embedder != nil {
			if emb, err := s.embedder.Embed(ctx, out); err == nil {
				mem.Embedding = emb
			}
		}

		if s.vector != nil {
			if err := s.vector.Add(ctx, mem); err != nil {
				s.logger.Warnf("store insight failed: %v", err)
				continue
			}
		}
		insights = append(insights, mem)
	}
	return insights, nil
}

func (s *Synthesizer) retrieve(ctx context.Context, question, userID string) []*types.Memory {
	if s.embedder == nil || s.vector == nil {
		return nil
	}

	qvec, err := s.embedder.Embed(ctx, question)
	if err != nil {
		return nil
	}

	mems, err := s.vector.Search(ctx, qvec, s.topK, store.Filter{
		UserID:            userID,
		ExcludeSuperseded: true,
	})
	if err != nil {
		return nil
	}

	return mems
}

func joinMemories(mems []*types.Memory) string {
	var b strings.Builder
	for _, m := range mems {
		if m == nil {
			continue
		}

		b.WriteString("- ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func avgImportance(mems []*types.Memory) float64 {
	if len(mems) == 0 {
		return 5
	}

	sum := 0.0
	count := 0
	for _, m := range mems {
		if m != nil {
			sum += m.Importance
			count++
		}
	}
	if count == 0 {
		return 5
	}

	return sum / float64(count)
}

package entity

import (
	"context"
	"fmt"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/store"
	"agentmem/types"
)

// Manager 实体记忆管理器
type Manager struct {
	graph     store.GraphStore
	extractor *Extractor
	embedder  llm.Embedder
	logger    observability.Logger
}

func NewManager(g store.GraphStore, ex *Extractor, emb llm.Embedder, logger observability.Logger) *Manager {
	if logger == nil {
		logger = observability.NewLogger()
	}
	return &Manager{graph: g, extractor: ex, embedder: emb, logger: logger}
}

// ExtractAndStore 从文本抽取实体并写入图存储
func (m *Manager) ExtractAndStore(ctx context.Context, text string) error {
	if m == nil || m.extractor == nil {
		return nil
	}

	g, err := m.extractor.Extract(ctx, text)
	if err != nil {
		return err
	}

	if g == nil || (len(g.Entities) == 0 && len(g.Relations) == 0) {
		return nil
	}

	return m.graph.AddGraph(ctx, g)
}

// Query 按文本召回相关实体，渲染为记忆列表
func (m *Manager) Query(ctx context.Context, text string, topK int) ([]*types.Memory, error) {
	if topK <= 0 {
		topK = 5
	}

	g, err := m.graph.QueryByText(ctx, text, topK)
	if err != nil {
		m.logger.Warnf("graph query failed: %v", err)
		return nil, err
	}

	if g == nil || len(g.Entities) == 0 {
		return nil, nil
	}

	var mems []*types.Memory
	for i := range g.Entities {
		e := g.Entities[i]
		content := renderEntity(e, g.Relations)
		mem := types.NewMemory("", "", content, types.MemoryTypeEntity)
		mem.Content = content
		mem.Importance = 6

		if m.embedder != nil {
			if emb, err := m.embedder.Embed(ctx, content); err == nil {
				mem.Embedding = emb
			}
		}
		mems = append(mems, mem)
	}
	return mems, nil
}

// renderEntity 将实体及其关联关系渲染为文本
func renderEntity(e types.Entity, rels []types.Relation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(%s)", e.Name, e.Type)
	for k, v := range e.Attributes {
		fmt.Fprintf(&b, " %s=%s", k, v)
	}

	for _, r := range rels {
		if r.Subject == e.Name || r.Object == e.Name {
			fmt.Fprintf(&b, "; %s -%s-> %s", r.Subject, r.Predicate, r.Object)
		}
	}
	return b.String()
}

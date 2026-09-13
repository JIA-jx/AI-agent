package store

import (
	"context"

	"agentmem/types"
)

type Filter struct {
	UserID            string
	SessionID         string
	Type              types.MemoryType
	ExcludeSuperseded bool // 排除被替代的记忆（Recall 时开启）
}

type VectorStore interface {
	Add(ctx context.Context, mem *types.Memory) error
	BatchAdd(ctx context.Context, mems []*types.Memory) error
	Search(ctx context.Context, query []float32, topK int, filter Filter) ([]*types.Memory, error)
	Get(ctx context.Context, id string) (*types.Memory, error)
	List(ctx context.Context, filter Filter) ([]*types.Memory, error)
	Delete(ctx context.Context, id string) error
	MarkSuperseded(ctx context.Context, oldID, newID string) error
	Close() error
}

func matchFilter(m *types.Memory, f Filter) bool {
	if f.UserID != "" && m.UserID != f.UserID {
		return false
	}
	if f.SessionID != "" && m.SessionID != f.SessionID {
		return false
	}
	if f.Type != "" && m.Type != f.Type {
		return false
	}
	if f.ExcludeSuperseded && !m.IsActive() {
		return false
	}
	return true
}

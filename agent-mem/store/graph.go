package store

import (
	"context"

	"agentmem/types"
)

// GraphStore 实体关系图存储抽象
type GraphStore interface {
	AddEntity(ctx context.Context, e *types.Entity) error
	AddRelation(ctx context.Context, r *types.Relation) error
	AddGraph(ctx context.Context, g *types.Graph) error
	GetEntity(ctx context.Context, name string) (*types.Entity, error)
	// QueryByText 返回名称出现在 text 中的实体及其关联关系构成的子图
	QueryByText(ctx context.Context, text string, topK int) (*types.Graph, error)
	Close() error
}

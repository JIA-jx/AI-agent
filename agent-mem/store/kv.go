package store

import "context"

// KVStore 键值存储抽象，用于短期窗口持久化等场景
type KVStore interface {
	Save(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) (map[string][]byte, error)
	Close() error
}

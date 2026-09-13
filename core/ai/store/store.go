package store

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/compose"
	"thunder/logs"
)

type inMemoryStore struct {
	mem  map[string][]byte
	lock sync.RWMutex
}

func NewInMemoryStore() compose.CheckPointStore {
	return &inMemoryStore{
		mem: map[string][]byte{},
	}
}

func (i *inMemoryStore) Set(ctx context.Context, key string, value []byte) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.mem[key] = value
	logs.Infof("set key: %s, value: %s", key, value)
	return nil
}

func (i *inMemoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	v, ok := i.mem[key]
	logs.Infof("get key: %s, value: %s", key, v)
	return v, ok, nil
}

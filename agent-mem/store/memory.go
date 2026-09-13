package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"agentmem/types"
)

// MemVectorStore 内存版向量存储，线性扫描 + 余弦相似度
type MemVectorStore struct {
	mu    sync.RWMutex             // 读写锁
	items map[string]*types.Memory // ID → Memory
	order []string                 // 插入顺序（保证遍历稳定）
}

func NewMemVectorStore() *MemVectorStore {
	return &MemVectorStore{items: make(map[string]*types.Memory)}
}

func (s *MemVectorStore) Add(_ context.Context, mem *types.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[mem.ID]; !ok {
		s.order = append(s.order, mem.ID)
	}
	s.items[mem.ID] = mem
	return nil
}

func (s *MemVectorStore) BatchAdd(ctx context.Context, mems []*types.Memory) error {
	// 批量添加
	for _, m := range mems {
		if err := s.Add(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// Search 线性排序 + 扫描
func (s *MemVectorStore) Search(ctx context.Context, query []float32, topK int, filter Filter) ([]*types.Memory, error) {
	s.mu.RLock()
	type scored struct {
		m *types.Memory
		s float64
	}
	tmp := make([]scored, 0, len(s.order))
	for _, id := range s.order {
		m, ok := s.items[id]
		if !ok {
			continue
		}
		if !matchFilter(m, filter) {
			continue
		}
		tmp = append(tmp, scored{m: m, s: types.CosineSimilarity(query, m.Embedding)})
	}
	s.mu.RUnlock()

	sort.Slice(tmp, func(i, j int) bool { return tmp[i].s > tmp[j].s })
	if topK > 0 && len(tmp) > topK {
		tmp = tmp[:topK]
	}

	now := time.Now()
	s.mu.Lock()
	for _, t := range tmp {
		if m, ok := s.items[t.m.ID]; ok {
			m.AccessedAt = now
		}
	}
	s.mu.Unlock()

	out := make([]*types.Memory, len(tmp))
	for i, t := range tmp {
		out[i] = t.m
	}
	return out, nil
}

func (s *MemVectorStore) Get(_ context.Context, id string) (*types.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if m, ok := s.items[id]; ok {
		return m, nil
	}

	return nil, nil
}

func (s *MemVectorStore) List(_ context.Context, filter Filter) ([]*types.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*types.Memory
	for _, id := range s.order {
		if m, ok := s.items[id]; ok && matchFilter(m, filter) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *MemVectorStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items, id)
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (s *MemVectorStore) MarkSuperseded(_ context.Context, oldID, newID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.items[oldID]; ok {
		m.SupersededBy = newID
	}
	return nil
}

func (s *MemVectorStore) Close() error { return nil }

type MemKVStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func NewMemKVStore() *MemKVStore {
	return &MemKVStore{m: make(map[string][]byte)}
}

func (s *MemKVStore) Save(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]byte, len(value))
	copy(cp, value)
	s.m[key] = cp
	return nil
}

func (s *MemKVStore) Load(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.m[key]
	if !ok {
		return nil, errors.New("key not found")
	}

	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

func (s *MemKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.m, key)
	return nil
}

func (s *MemKVStore) List(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][]byte)
	for k, v := range s.m {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}

func (s *MemKVStore) Close() error { return nil }

type MemGraphStore struct {
	mu        sync.RWMutex
	entities  map[string]*types.Entity
	relations []*types.Relation
}

func NewMemGraphStore() *MemGraphStore {
	return &MemGraphStore{entities: make(map[string]*types.Entity)}
}

func (s *MemGraphStore) AddEntity(_ context.Context, e *types.Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.entities[e.Name]; ok {
		for k, v := range e.Attributes {
			existing.Attributes[k] = v
		}
		if e.Type != "" {
			existing.Type = e.Type
		}
		return nil
	}
	s.entities[e.Name] = e
	return nil
}

func (s *MemGraphStore) AddRelation(_ context.Context, r *types.Relation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ex := range s.relations {
		if ex.Subject == r.Subject && ex.Predicate == r.Predicate && ex.Object == r.Object {
			ex.Weight = r.Weight
			return nil
		}
	}
	s.relations = append(s.relations, r)
	return nil
}

func (s *MemGraphStore) AddGraph(ctx context.Context, g *types.Graph) error {
	for i := range g.Entities {
		if err := s.AddEntity(ctx, &g.Entities[i]); err != nil {
			return err
		}
	}

	for i := range g.Relations {
		if err := s.AddRelation(ctx, &g.Relations[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemGraphStore) GetEntity(_ context.Context, name string) (*types.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if e, ok := s.entities[name]; ok {
		return e, nil
	}
	return nil, nil
}

func (s *MemGraphStore) QueryByText(_ context.Context, text string, topK int) (*types.Graph, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	source := strings.ToLower(text)
	g := &types.Graph{}

	for name, e := range s.entities {
		if strings.Contains(source, strings.ToLower(name)) {
			g.Entities = append(g.Entities, *e)
			if topK > 0 && len(g.Entities) >= topK {
				break
			}
		}
	}
	if len(g.Entities) == 0 {
		return g, nil
	}

	names := make(map[string]bool)
	for _, e := range g.Entities {
		names[e.Name] = true
	}
	for _, r := range s.relations {
		if names[r.Subject] || names[r.Object] {
			g.Relations = append(g.Relations, *r)
		}
	}
	return g, nil
}

func (s *MemGraphStore) Close() error { return nil }

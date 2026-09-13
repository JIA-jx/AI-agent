package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"agentmem/types"

	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
	"github.com/redis/go-redis/v9"
)

func buildWherePG(f Filter, start int) (string, []any) {
	var conds []string
	var args []any

	idx := start
	if f.UserID != "" {
		conds = append(conds, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, f.UserID)
		idx++
	}
	if f.SessionID != "" {
		conds = append(conds, fmt.Sprintf("session_id = $%d", idx))
		args = append(args, f.SessionID)
		idx++
	}
	if f.Type != "" {
		conds = append(conds, fmt.Sprintf("type = $%d", idx))
		args = append(args, string(f.Type))
		idx++
	}
	if f.ExcludeSuperseded {
		conds = append(conds, "superseded_by = ''")
	}
	if len(conds) == 0 {
		return "TRUE", args
	}

	return strings.Join(conds, " AND "), args
}

type PostgresStores struct {
	Vector *PGVectorStore
	KV     *PGKVStore
	Graph  *PGGraphStore
	db     *sql.DB
}

func NewPostgresStores(dsn string, dim int) (*PostgresStores, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS agentmem_memory (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			user_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding vector(%d),
			importance DOUBLE PRECISION NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			accessed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			source_ids JSONB,
			superseded_by TEXT NOT NULL DEFAULT '',
			metadata JSONB)`, dim),
		`CREATE TABLE IF NOT EXISTS agentmem_kv (key TEXT PRIMARY KEY, value BYTEA)`,
		`CREATE TABLE IF NOT EXISTS agentmem_entity (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL DEFAULT '',
			attributes JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS agentmem_relation (
			id TEXT PRIMARY KEY, subject TEXT NOT NULL, predicate TEXT NOT NULL,
			object TEXT NOT NULL, weight DOUBLE PRECISION NOT NULL DEFAULT 1)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_user ON agentmem_memory(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_session ON agentmem_memory(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_name ON agentmem_entity(name)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("init schema: %w", err)
		}
	}
	ps := &PostgresStores{db: db}
	ps.Vector = &PGVectorStore{db: db}
	ps.KV = &PGKVStore{db: db}
	ps.Graph = &PGGraphStore{db: db}
	return ps, nil
}

func (ps *PostgresStores) Close() error { return ps.db.Close() }

type PGVectorStore struct {
	db *sql.DB
}

func (s *PGVectorStore) Add(ctx context.Context, mem *types.Memory) error {
	src, _ := json.Marshal(mem.SourceIDs)
	meta, _ := json.Marshal(mem.Metadata)
	var vecArg any
	if len(mem.Embedding) > 0 {
		vecArg = pgvector.NewVector(mem.Embedding)
	} else {
		vecArg = nil
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO agentmem_memory
		(id, session_id, user_id, type, content, embedding, importance, created_at, accessed_at, source_ids, superseded_by, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			content=EXCLUDED.content, embedding=EXCLUDED.embedding,
			importance=EXCLUDED.importance, accessed_at=EXCLUDED.accessed_at,
			source_ids=EXCLUDED.source_ids, superseded_by=EXCLUDED.superseded_by,
			metadata=EXCLUDED.metadata`,
		mem.ID, mem.SessionID, mem.UserID, string(mem.Type), mem.Content,
		vecArg, mem.Importance, mem.CreatedAt, mem.AccessedAt, src, mem.SupersededBy, meta)
	return err
}

func (s *PGVectorStore) BatchAdd(ctx context.Context, mems []*types.Memory) error {
	for _, m := range mems {
		if err := s.Add(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (s *PGVectorStore) MarkSuperseded(ctx context.Context, oldID, newID string) error {
	// 把旧记忆标记为被新记忆取代
	_, err := s.db.ExecContext(ctx,
		`UPDATE agentmem_memory SET superseded_by = $1 WHERE id = $2`,
		newID, oldID)
	return err
}

// Search 向量相似度检索
func (s *PGVectorStore) Search(ctx context.Context, query []float32, topK int, filter Filter) ([]*types.Memory, error) {
	where, args := buildWherePG(filter, 1)
	vecIdx := len(args) + 1
	args = append(args, pgvector.NewVector(query))
	topIdx := len(args) + 1
	args = append(args, topK)
	q := fmt.Sprintf(`SELECT id, session_id, user_id, type, content, importance, created_at, accessed_at, source_ids, superseded_by, metadata
		FROM agentmem_memory WHERE %s AND embedding IS NOT NULL
		ORDER BY embedding <=> $%d LIMIT $%d`, where, vecIdx, topIdx)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows)
}

func (s *PGVectorStore) Get(ctx context.Context, id string) (*types.Memory, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, user_id, type, content, importance, created_at, accessed_at, source_ids, superseded_by, metadata
		FROM agentmem_memory WHERE id = $1`, id)
	mem, err := scanMemory(row)
	if err != nil {
		return nil, err
	}

	return mem, nil
}

func (s *PGVectorStore) List(ctx context.Context, filter Filter) ([]*types.Memory, error) {
	where, args := buildWherePG(filter, 1)
	q := fmt.Sprintf(`SELECT id, session_id, user_id, type, content, importance, created_at, accessed_at, source_ids, superseded_by, metadata
		FROM agentmem_memory WHERE %s ORDER BY created_at DESC`, where)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows)
}

func (s *PGVectorStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agentmem_memory WHERE id = $1`, id)
	return err
}

func (s *PGVectorStore) Close() error { return nil }

type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(row scanner) (*types.Memory, error) {
	var m types.Memory
	var mtype string
	var src, meta []byte
	if err := row.Scan(
		&m.ID, &m.SessionID, &m.UserID, &mtype, &m.Content, &m.Importance, &m.CreatedAt, &m.AccessedAt, &src, &m.SupersededBy, &meta,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.Type = types.MemoryType(mtype)
	if len(src) > 0 {
		_ = json.Unmarshal(src, &m.SourceIDs)
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &m.Metadata)
	}
	return &m, nil
}

func scanMemories(rows *sql.Rows) ([]*types.Memory, error) {
	var out []*types.Memory
	for rows.Next() {
		var m types.Memory
		var mtype string
		var src, meta []byte
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.UserID, &mtype, &m.Content, &m.Importance, &m.CreatedAt, &m.AccessedAt, &src, &m.SupersededBy, &meta,
		); err != nil {
			return nil, err
		}
		m.Type = types.MemoryType(mtype)
		if len(src) > 0 {
			_ = json.Unmarshal(src, &m.SourceIDs)
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &m.Metadata)
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

type PGKVStore struct{ db *sql.DB }

func (s *PGKVStore) Save(ctx context.Context, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agentmem_kv (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

func (s *PGKVStore) Load(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM agentmem_kv WHERE key = $1`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *PGKVStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agentmem_kv WHERE key = $1`, key)
	return err
}

func (s *PGKVStore) List(ctx context.Context, prefix string) (map[string][]byte, error) {
	pattern := prefix + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM agentmem_kv WHERE key LIKE $1`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]byte)
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *PGKVStore) Close() error { return nil }

type PGGraphStore struct{ db *sql.DB }

func (s *PGGraphStore) AddEntity(ctx context.Context, e *types.Entity) error {
	attr, _ := json.Marshal(e.Attributes)
	_, err := s.db.ExecContext(ctx, `INSERT INTO agentmem_entity (id, name, type, attributes, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, type=EXCLUDED.type, attributes=EXCLUDED.attributes`,
		e.ID, e.Name, e.Type, attr, e.CreatedAt)
	return err
}

func (s *PGGraphStore) AddRelation(ctx context.Context, r *types.Relation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agentmem_relation (id, subject, predicate, object, weight)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET weight = EXCLUDED.weight`,
		r.ID, r.Subject, r.Predicate, r.Object, r.Weight)
	return err
}

func (s *PGGraphStore) AddGraph(ctx context.Context, g *types.Graph) error {
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

func (s *PGGraphStore) GetEntity(ctx context.Context, name string) (*types.Entity, error) {
	var e types.Entity
	var attr []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, name, type, attributes, created_at FROM agentmem_entity WHERE name = $1`, name).
		Scan(&e.ID, &e.Name, &e.Type, &attr, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(attr) > 0 {
		_ = json.Unmarshal(attr, &e.Attributes)
	}
	return &e, nil
}

func (s *PGGraphStore) QueryByText(ctx context.Context, text string, topK int) (*types.Graph, error) {
	limit := topK
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, attributes, created_at
		FROM agentmem_entity WHERE name <> '' AND position(lower(name) in lower($1)) > 0 LIMIT $2`, text, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	g := &types.Graph{}
	var names []string
	for rows.Next() {
		var e types.Entity
		var attr []byte
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &attr, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(attr) > 0 {
			_ = json.Unmarshal(attr, &e.Attributes)
		}
		g.Entities = append(g.Entities, e)
		names = append(names, e.Name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return g, nil
	}
	rrows, err := s.db.QueryContext(ctx, `SELECT id, subject, predicate, object, weight
		FROM agentmem_relation WHERE subject = ANY($1) OR object = ANY($1)`, pq.Array(names))
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var r types.Relation
		if err := rrows.Scan(&r.ID, &r.Subject, &r.Predicate, &r.Object, &r.Weight); err != nil {
			return nil, err
		}
		g.Relations = append(g.Relations, r)
	}
	return g, rrows.Err()
}

func (s *PGGraphStore) Close() error { return nil }

type RedisKVStore struct{ cli *redis.Client }

func NewRedisKVStore(addr, password string) (*RedisKVStore, error) {
	cli := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	if err := cli.Ping(context.Background()).Err(); err != nil {
		_ = cli.Close()
		return nil, err
	}
	return &RedisKVStore{cli: cli}, nil
}

func (s *RedisKVStore) Save(ctx context.Context, key string, value []byte) error {
	return s.cli.Set(ctx, key, value, 0).Err()
}

func (s *RedisKVStore) Load(ctx context.Context, key string) ([]byte, error) {
	v, err := s.cli.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return v, err
}

func (s *RedisKVStore) Delete(ctx context.Context, key string) error {
	return s.cli.Del(ctx, key).Err()
}

func (s *RedisKVStore) List(ctx context.Context, prefix string) (map[string][]byte, error) {
	out := make(map[string][]byte)
	pattern := prefix + "*"
	var cur uint64
	for {
		keys, c, err := s.cli.Scan(ctx, cur, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		if len(keys) > 0 {
			vals, err := s.cli.MGet(ctx, keys...).Result()
			if err != nil {
				return nil, err
			}
			for i, v := range vals {
				if b, ok := v.([]byte); ok {
					out[keys[i]] = b
				} else if s, ok := v.(string); ok {
					out[keys[i]] = []byte(s)
				}
			}
		}
		cur = c
		if cur == 0 {
			break
		}
	}
	return out, nil
}

func (s *RedisKVStore) Close() error { return s.cli.Close() }

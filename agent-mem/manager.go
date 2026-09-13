package agentmem

import (
	"agentmem/types"
	"context"

	"agentmem/config"
	"agentmem/entity"
	"agentmem/llm"
	"agentmem/longterm"
	"agentmem/observability"
	"agentmem/recall"
	"agentmem/reflection"
	"agentmem/shortterm"
	"agentmem/store"
)

// MemoryManager 记忆系统统一入口，编排短期/长期/实体/反思子系统
type MemoryManager struct {
	cfg       config.Config
	client    llm.Client
	embedder  llm.Embedder
	completer llm.Completer

	vectorStore store.VectorStore
	kvStore     store.KVStore
	graphStore  store.GraphStore

	shortMgr     *shortterm.Manager
	longExtract  *longterm.Extractor
	longRetrieve *longterm.Retriever
	scorer       *longterm.Scorer
	merger       *longterm.Merger
	entityMgr    *entity.Manager
	orchestrator *recall.Orchestrator
	trigger      *reflection.Trigger
	questioner   *reflection.Questioner
	synthesizer  *reflection.Synthesizer

	pool       *ExtractorPool
	logger     observability.Logger
	ownsStores bool
}

func NewMemoryManager() (*MemoryManager, error) {
	m := &MemoryManager{
		cfg:    config.LoadFromEnv(),
		logger: observability.NewLogger(),
	}
	m.setupDefaults()
	if err := m.cfg.Validate(); err != nil {
		return nil, err
	}
	m.assemble()
	return m, nil
}

func (m *MemoryManager) setupDefaults() {
	if m.logger == nil {
		m.logger = observability.NewLogger()
	}
	if m.client == nil {
		m.client = llm.NewClient(m.cfg)
	}
	if m.vectorStore == nil || m.kvStore == nil || m.graphStore == nil {
		m.initStores()
	}
}

func (m *MemoryManager) initStores() {
	switch m.cfg.StoreBackend {
	case "postgres":
		if m.cfg.HasPostgres() {
			ps, err := store.NewPostgresStores(m.cfg.PGDSN, m.cfg.EmbedDim)
			if err == nil {
				m.vectorStore = ps.Vector
				m.kvStore = ps.KV
				m.graphStore = ps.Graph
				m.ownsStores = true
				return
			}
			m.logger.Warnf("init postgres stores failed, fallback to memory: %v", err)
		}
		fallthrough
	case "redis":
		m.vectorStore = store.NewMemVectorStore()
		m.graphStore = store.NewMemGraphStore()
		if m.cfg.HasRedis() {
			if rkv, err := store.NewRedisKVStore(m.cfg.RedisAddr, m.cfg.RedisPassword); err == nil {
				m.kvStore = rkv
				m.ownsStores = true
				return
			} else {
				m.logger.Warnf("init redis kv failed, fallback to memory: %v", err)
			}
		}
		m.kvStore = store.NewMemKVStore()
	default:
		m.vectorStore = store.NewMemVectorStore()
		m.kvStore = store.NewMemKVStore()
		m.graphStore = store.NewMemGraphStore()
	}
}

func (m *MemoryManager) assemble() {
	m.embedder = llm.NewEmbedder(m.client)
	m.completer = llm.NewCompleter(m.client)

	tok := shortterm.NewTokenizer(m.cfg.ChatModel)
	sum := shortterm.NewSummarizer(m.completer, m.logger)
	m.shortMgr = shortterm.NewManager(m.kvStore, tok, sum, m.cfg.HasLLM(), m.cfg.ShortMaxTokens, m.cfg.ShortMaxMessages, m.logger)

	m.scorer = longterm.NewScorer(m.completer, m.logger)
	m.merger = longterm.NewMerger(m.cfg.MergeThreshold, m.logger)
	m.longExtract = longterm.NewExtractor(m.completer, m.embedder, m.vectorStore, m.scorer, m.merger, m.logger)
	m.longRetrieve = longterm.NewRetriever(m.vectorStore, m.embedder, m.cfg.LongTopK, m.logger)

	m.entityMgr = entity.NewManager(m.graphStore, entity.NewExtractor(m.completer, m.logger), m.embedder, m.logger)

	m.orchestrator = recall.NewOrchestrator(m.shortMgr, m.longRetrieve, m.entityMgr, m.embedder, m.cfg, m.logger)

	m.trigger = reflection.NewTrigger(m.cfg.ReflImportanceThreshold, m.cfg.ReflMsgCountThreshold, m.cfg.ReflTimeThreshold)
	m.questioner = reflection.NewQuestioner(m.completer, m.logger)
	m.synthesizer = reflection.NewSynthesizer(m.completer, m.embedder, m.vectorStore, 5, m.logger)

	m.pool = NewExtractorPool(m.client, m.embedder, m.vectorStore, m.kvStore, m.cfg, m.logger)
}

// Start 启动后台 worker 池
func (m *MemoryManager) Start() {
	if m.pool != nil {
		m.pool.Start()
	}
}

// AddMessage 写入短期记忆，并异步触发长期抽取
func (m *MemoryManager) AddMessage(ctx context.Context, sessionID, userID, role, content string) error {
	if err := m.shortMgr.AddMessage(ctx, sessionID, role, content); err != nil {
		return err
	}
	m.trigger.Observe(sessionID, 1)

	if len(content) > 0 {
		_ = m.entityMgr.ExtractAndStore(ctx, content)
	}

	// 长期抽取 → 入 ExtractorPool 队列（无界 goroutine 的替代）
	if m.pool != nil {
		task := ExtractTask{
			SessionID: sessionID,
			UserID:    userID,
			Hash:      HashTask(userID, sessionID, content),
		}
		if err := m.pool.Submit(task); err == ErrQueueFull {
			m.logger.Warnf("extractor queue full (dropped oldest), task hash=%s", task.Hash)
		}
	}
	return nil
}

// Recall 召回并返回评分后的记忆列表
func (m *MemoryManager) Recall(ctx context.Context, sessionID, userID, query string, topK int) ([]types.ScoredMemory, error) {
	return m.orchestrator.Recall(ctx, sessionID, userID, query, topK)
}

// RecallContext 召回并格式化为可注入 prompt 的文本
func (m *MemoryManager) RecallContext(ctx context.Context, sessionID, userID, query string) string {
	scored, err := m.orchestrator.Recall(ctx, sessionID, userID, query, m.cfg.LongTopK)
	if err != nil {
		m.logger.Warnf("recall failed: %v", err)
		return ""
	}
	return recall.FormatContext(scored)
}

// Reflect 检查触发条件并执行反思，合成洞察写入长期记忆
func (m *MemoryManager) Reflect(ctx context.Context, sessionID, userID string) error {
	if !m.trigger.ShouldReflect(sessionID) {
		return nil
	}

	msgs, _ := m.shortMgr.Recall(ctx, sessionID, m.cfg.ReflMsgCountThreshold)
	recentText := shortterm.JoinMessages(msgs)
	questions, err := m.questioner.Generate(ctx, recentText)
	if err != nil {
		return err
	}

	if len(questions) == 0 {
		m.trigger.MarkReflected(sessionID)
		return nil
	}

	insights, err := m.synthesizer.Synthesize(ctx, userID, questions)
	if err != nil {
		return err
	}

	m.trigger.MarkReflected(sessionID)
	m.logger.Debugf("reflected %d insights for session %s", len(insights), sessionID)
	return nil
}

// PoolStats 返回抽取器池运行时统计
func (m *MemoryManager) PoolStats() PoolStats {
	if m.pool == nil {
		return PoolStats{}
	}

	return m.pool.Stats()
}

// Available LLM 是否可用
func (m *MemoryManager) Available() bool { return m.client.Available() }

// Config 返回配置
func (m *MemoryManager) Config() config.Config { return m.cfg }

// Close 优雅关闭 worker 池 + 释放底层存储
func (m *MemoryManager) Close() error {
	if m.pool != nil {
		m.pool.Stop()
	}
	if m.ownsStores {
		if m.kvStore != nil {
			_ = m.kvStore.Close()
		}
		if m.vectorStore != nil {
			_ = m.vectorStore.Close()
		}
		if m.graphStore != nil {
			_ = m.graphStore.Close()
		}
	}
	return nil
}

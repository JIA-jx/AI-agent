package agentmem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"agentmem/config"
	"agentmem/llm"
	"agentmem/observability"
	"agentmem/shortterm"
	"agentmem/store"
	"agentmem/types"
)

type ExtractTask struct {
	SessionID string    // 会话 ID
	UserID    string    // 用户 ID
	Hash      string    // SHA256 幂等键
	Timestamp time.Time // 任务创建时间
}

type simpleLimiter struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64 // 每秒补充的 token 数
	capacity float64 // 桶容量
	last     time.Time
}

func newLimiter(ratePerSec float64) *simpleLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 10
	}
	return &simpleLimiter{
		tokens:   ratePerSec,
		rate:     ratePerSec,
		capacity: ratePerSec,
		last:     time.Now(),
	}
}

func (l *simpleLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	delta := now.Sub(l.last).Seconds() // 时间差（秒）
	l.tokens += delta * l.rate         // 补充令牌
	if l.tokens > l.capacity {
		l.tokens = l.capacity // 不超过容量
	}
	l.last = now

	if l.tokens >= 1 {
		l.tokens-- // 消耗 1 个令牌
		return true
	}
	return false
}

// ExtractorPool 抽取器池：有界队列 + Worker + 批量合并 + 限流
type ExtractorPool struct {
	// 队列和并发控制
	queue   chan ExtractTask
	workers int
	limiter *simpleLimiter

	// LLM 和存储依赖
	llm      llm.Client
	embedder llm.Embedder      // 向量化模型
	vector   store.VectorStore // 向量数据库
	kv       store.KVStore     // 键值存储（幂等、窗口缓存）
	cfg      config.Config
	logger   observability.Logger

	// 批量累积
	batchMu      sync.Mutex
	batchTasks   []ExtractTask
	batchMaxSize int
	batchMaxWait time.Duration
	lastFlush    time.Time

	// 控制
	wg      sync.WaitGroup
	stopCh  chan struct{}
	running bool

	// 可观测
	statsMu sync.Mutex
	stats   PoolStats
}

type PoolStats struct {
	Submitted   int64     // 提交总数
	Processed   int64     // 已处理数
	Succeeded   int64     // 成功数
	Failed      int64     // 失败数
	Dropped     int64     // 队列满丢弃数
	Idempotent  int64     // 幂等跳过数
	LastBatchAt time.Time // 最后批处理时间
	LastBatchSz int       // 最后批处理大小
}

var ErrQueueFull = errors.New("extractor queue full")

func NewExtractorPool(llmClient llm.Client,
	embedder llm.Embedder,
	vector store.VectorStore,
	kv store.KVStore,
	cfg config.Config,
	logger observability.Logger,
) *ExtractorPool {
	/////////////////////////
	const (
		defaultQueueCap   = 1000
		defaultWorkers    = 5
		defaultBatchSize  = 20
		defaultBatchWait  = 500 * time.Millisecond
		defaultRatePerSec = 10
	)
	if cfg.HasLLM() {
		logger.Infof("ExtractorPool: will run with %d workers, batch_size=%d, rate=%d/s",
			defaultWorkers, defaultBatchSize, defaultRatePerSec)
	} else {
		logger.Infof("ExtractorPool: LLM not configured, will skip extraction gracefully")
	}

	return &ExtractorPool{
		queue:        make(chan ExtractTask, defaultQueueCap),
		workers:      defaultWorkers,
		limiter:      newLimiter(defaultRatePerSec),
		llm:          llmClient,
		embedder:     embedder,
		vector:       vector,
		kv:           kv,
		cfg:          cfg,
		logger:       logger,
		batchMaxSize: defaultBatchSize,
		batchMaxWait: defaultBatchWait,
		stopCh:       make(chan struct{}),
	}
}

// Start 启动 worker 池 + flush 定时器
func (p *ExtractorPool) Start() {
	if p.running {
		return
	}

	p.running = true
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	go p.flushTicker()
	p.logger.Infof("ExtractorPool started with %d workers", p.workers)
}

// Stop 优雅关闭：等队列清空 + worker 退出
func (p *ExtractorPool) Stop() {
	if !p.running {
		return
	}

	close(p.stopCh)
	p.wg.Wait()
	p.running = false
	p.logger.Infof("ExtractorPool stopped")
}

func (p *ExtractorPool) Stats() PoolStats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()

	return p.stats
}

// Submit 入队，队列满时丢最旧的（降级）
func (p *ExtractorPool) Submit(task ExtractTask) error {
	if !p.cfg.HasLLM() {
		return nil
	}

	task.Timestamp = time.Now()
	p.statsMu.Lock()
	p.stats.Submitted++
	p.statsMu.Unlock()

	// 幂等：同一 session 5 分钟内只抽一次
	if p.kv != nil {
		key := "extracted:" + task.Hash
		if v, _ := p.kv.Load(context.Background(), key); v != nil {
			p.statsMu.Lock()
			p.stats.Idempotent++
			p.statsMu.Unlock()
			return nil
		}
	}

	select {
	case p.queue <- task:
		return nil
	default:
		// 队列满：丢最旧的 + 丢计数器 + 塞新的
		select {
		case <-p.queue:
		default:
		}
		p.statsMu.Lock()
		p.stats.Dropped++
		p.statsMu.Unlock()

		select {
		case p.queue <- task:
			return ErrQueueFull // 虽然塞进去了，但告知上游发生过降级
		default:
			return ErrQueueFull
		}
	}
}

func HashTask(userID, sessionID, content string) string {
	h := sha256.New()
	h.Write([]byte(userID + "|" + sessionID + "|" + content))
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func (p *ExtractorPool) flushTicker() {
	ticker := time.NewTicker(p.batchMaxWait)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.tryFlush(nil)
		}
	}
}

func (p *ExtractorPool) tryFlush(tasks []ExtractTask) {
	p.batchMu.Lock()
	p.batchTasks = append(p.batchTasks, tasks...)
	now := time.Now()

	// 判断是否应该刷新：达到批量大小 或 超时
	shouldFlush := len(p.batchTasks) >= p.batchMaxSize ||
		(!p.lastFlush.IsZero() && now.Sub(p.lastFlush) >= p.batchMaxWait && len(p.batchTasks) > 0)

	if !shouldFlush {
		p.batchMu.Unlock()
		return
	}

	out := p.batchTasks
	p.batchTasks = nil
	p.lastFlush = now
	p.batchMu.Unlock()

	if len(out) == 0 {
		return
	}

	// 异步执行批量抽取
	if err := p.asyncExtractBatch(context.Background(), out); err != nil {
		p.logger.Warnf("asyncExtractBatch failed: %v", err)
		p.statsMu.Lock()
		p.stats.Failed += int64(len(out))
		p.statsMu.Unlock()
	} else {
		p.statsMu.Lock()
		p.stats.Succeeded++
		p.statsMu.Unlock()
	}
}

func (p *ExtractorPool) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			for {
				select {
				case t := <-p.queue:
					p.processOne(t)
				default:
					p.tryFlush(nil)
					return
				}
			}
		case t := <-p.queue:
			p.processOne(t)
		}
	}
}

func (p *ExtractorPool) processOne(t ExtractTask) {
	p.statsMu.Lock()
	p.stats.Processed++
	p.statsMu.Unlock()

	// 幂等再次检查（Submit 和 worker 之间可能已被 flush 处理过）
	if p.kv != nil {
		key := "extracted:" + t.Hash
		if v, _ := p.kv.Load(context.Background(), key); v != nil {
			p.statsMu.Lock()
			p.stats.Idempotent++
			p.statsMu.Unlock()
			return
		}
	}

	// 批量累积
	p.tryFlush([]ExtractTask{t})

	// 标记已处理（5 分钟过期）
	if p.kv != nil {
		key := "extracted:" + t.Hash
		_ = p.kv.Save(context.Background(), key, []byte("1"))
	}
}

// batchExtractItem 批量抽取的中间 JSON 项
type batchExtractItem struct {
	UserID     string  `json:"user_id"`
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
}

// asyncExtractBatch 核心：合并原料 → 批量 LLM 抽取 → 冲突消解 → 增量去重 → 向量化 → 入库
func (p *ExtractorPool) asyncExtractBatch(ctx context.Context, tasks []ExtractTask) error {
	if len(tasks) == 0 {
		return nil
	}

	// 1. 按 user 分组 → 每用户合并所有会话的最近窗口
	type userSessions struct {
		userID   string
		convList []string
	}
	userMap := make(map[string]*userSessions)
	for _, t := range tasks {
		if t.UserID == "" {
			continue
		}
		us, ok := userMap[t.UserID]
		if !ok {
			us = &userSessions{userID: t.UserID}
			userMap[t.UserID] = us
		}
		// 用 KVStore 拉短期记忆窗口
		winKey := "short:win:" + t.SessionID
		b, err := p.kv.Load(ctx, winKey)
		if err != nil || len(b) == 0 {
			continue
		}
		var wd shortterm.WindowData
		if err := json.Unmarshal(b, &wd); err != nil {
			continue
		}
		text := shortterm.JoinMessages(wd.Messages)
		if text != "" {
			us.convList = append(us.convList, text)
		}
	}

	// 2. 每个用户跑一次批量抽取
	var allNewMems []*types.Memory
	for userID, us := range userMap {
		conv := strings.Join(us.convList, "\n\n--- 会话分割 ---\n\n")
		if strings.TrimSpace(conv) == "" {
			continue
		}

		// 限流
		if !p.limiter.Allow() {
			p.logger.Infof("rate limited, delaying...")
			time.Sleep(100 * time.Millisecond)
			if !p.limiter.Allow() {
				continue
			}
		}

		// 调试 LLM （带重试）
		system, user := llm.BatchExtractPrompt(conv)
		out, err := p.callLLMWithRetry(ctx, system, user, 3)
		if err != nil {
			p.logger.Warnf("batch extract failed for user %s: %v", userID, err)
			continue
		}
		if out == "" {
			continue
		}

		// 解析 JSON 输出
		raw := extractJSONString(out)
		if raw == "" {
			continue
		}
		var items []batchExtractItem
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			p.logger.Warnf("parse batch extract json failed: %v (raw=%q)", err, raw)
			continue
		}

		// 构建 memory 对象
		for _, it := range items {
			if strings.TrimSpace(it.Content) == "" {
				continue
			}
			mem := types.NewMemory(userID, "", it.Content, types.MemoryTypeLong)
			// 向量化
			if it.Importance > 0 {
				mem.Importance = types.Clamp(it.Importance, 0, 10)
			} else {
				mem.Importance = 5
			}
			if p.embedder != nil {
				if emb, err := p.embedder.Embed(ctx, it.Content); err == nil {
					mem.Embedding = emb
				}
			}
			allNewMems = append(allNewMems, mem)
		}
	}
	if len(allNewMems) == 0 {
		return nil
	}

	// 3. 增量去重 + 冲突消解
	allNewMems = p.incrementalDedup(ctx, allNewMems)
	if err := p.resolveConflicts(ctx, allNewMems); err != nil {
		p.logger.Warnf("conflict resolution failed: %v", err)
	}

	// 4. 批量入库
	if err := p.vector.BatchAdd(ctx, allNewMems); err != nil {
		p.logger.Warnf("batch add failed: %v", err)
		return err
	}
	p.logger.Infof("asyncExtractBatch: extracted %d memories from %d tasks across %d users",
		len(allNewMems), len(tasks), len(userMap))
	return nil
}

// incrementalDedup 增量去重：对每条新记忆只检索 topK=3 最相似的已有记忆比对
func (p *ExtractorPool) incrementalDedup(ctx context.Context, newMems []*types.Memory) []*types.Memory {
	var out []*types.Memory
	for _, nm := range newMems {
		if len(nm.Embedding) == 0 {
			out = append(out, nm)
			continue
		}
		existingSimilar, err := p.vector.Search(ctx, nm.Embedding, 3, store.Filter{
			UserID:            nm.UserID,
			Type:              types.MemoryTypeLong,
			ExcludeSuperseded: true,
		})
		if err != nil {
			out = append(out, nm)
			continue
		}

		dup := false
		for _, ex := range existingSimilar {
			if len(ex.Embedding) == 0 {
				continue
			}
			if types.CosineSimilarity(nm.Embedding, ex.Embedding) >= p.cfg.MergeThreshold {
				dup = true
				break
			}
		}
		if dup {
			continue
		}

		// 和已选新记忆互比
		for _, kept := range out {
			if kept.UserID != nm.UserID {
				continue
			}
			if len(kept.Embedding) == 0 {
				continue
			}
			if types.CosineSimilarity(nm.Embedding, kept.Embedding) >= p.cfg.MergeThreshold {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, nm)
		}
	}
	return out
}

// resolveConflicts 冲突消解：对每条新记忆，检索已有记忆中最相似的，用 LLM 判断是否语义矛盾
func (p *ExtractorPool) resolveConflicts(ctx context.Context, newMems []*types.Memory) error {
	for _, nm := range newMems {
		if len(nm.Embedding) == 0 {
			continue
		}
		similar, err := p.vector.Search(ctx, nm.Embedding, 3, store.Filter{
			UserID:            nm.UserID,
			Type:              types.MemoryTypeLong,
			ExcludeSuperseded: true,
		})
		if err != nil {
			continue
		}
		for _, ex := range similar {
			if ex.ID == nm.ID {
				continue
			}
			if len(ex.Embedding) == 0 {
				continue
			}
			// 语义相似但方向相反 → 矛盾
			if types.CosineSimilarity(nm.Embedding, ex.Embedding) >= p.cfg.MergeThreshold {
				contradict, err := p.isContradictory(ctx, ex, nm)
				if err != nil {
					continue
				}
				if contradict {
					p.logger.Infof("conflict resolved: old=%s superseded by new=%s", ex.ID, nm.ID)
					_ = p.vector.MarkSuperseded(ctx, ex.ID, nm.ID)
				}
			}
		}
	}
	return nil
}

// isContradictory 调 LLM 判断两条记忆是否语义矛盾
func (p *ExtractorPool) isContradictory(ctx context.Context, old, new *types.Memory) (bool, error) {
	if !p.limiter.Allow() {
		return false, nil
	}

	system, user := llm.ContradictionCheckPrompt(old.Content, new.Content)
	out, err := p.callLLMWithRetry(ctx, system, user, 2)
	if err != nil {
		return false, err
	}

	return strings.Contains(strings.ToLower(strings.TrimSpace(out)), "true"), nil
}

// callLLMWithRetry 带超时 + 指数退避重试
func (p *ExtractorPool) callLLMWithRetry(ctx context.Context, system, user string, maxRetries int) (string, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := p.llm.Complete(callCtx, system, user)
		cancel()
		if err == nil {
			return out, nil
		}

		lastErr = err
		if attempt < maxRetries-1 {
			wait := time.Duration(math.Pow(2, float64(attempt))) * 100 * time.Millisecond
			time.Sleep(wait)
		}
	}
	return "", fmt.Errorf("llm call failed after %d retries: %w", maxRetries, lastErr)
}

func extractJSONString(s string) string {
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

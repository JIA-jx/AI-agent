package recall

import (
	"context"
	"sort"
	"strings"
	"time"

	"agentmem/config"
	"agentmem/entity"
	"agentmem/llm"
	"agentmem/longterm"
	"agentmem/observability"
	"agentmem/shortterm"
	"agentmem/types"
)

//Recall(sessionID, userID, query, topK)
//  │
//  ├─ now = time.Now()
//  │
//  ├─ 短期: shortMgr.Recall → summary + msgs
//  │    ├─ summary 非空 → 构造摘要 Memory（importance=3，1h前）
//  │    └─ 每条 msg → Memory（embedding，importance=1/3）
//  │
//  ├─ 长期: longRet.Retrieve(query, userID) → mems
//  │    └─ 失败 → Warn
//  │
//  ├─ 实体: entityMgr.Query(query, 5) → mems
//  │    └─ 失败 → 静默
//  │
//  ├─ queryEmb = Embedder.Embed(query)
//  │
//  ├─ for each candidate:
//  │    recency    = CalcRecency(mem, now, lambda)
//  │    relevance  = CalcRelevance(queryEmb, mem)
//  │    importance = CalcImportance(mem)
//  │    score      = α·recency + β·relevance + γ·importance
//  │
//  ├─ sort by score desc
//  ├─ truncate to topK
//  └─ return scored

// Orchestrator 召回编排器：聚合短期/长期/实体记忆，三因子综合评分排序
type Orchestrator struct {
	shortMgr  *shortterm.Manager
	longRet   *longterm.Retriever
	entityMgr *entity.Manager
	embedder  llm.Embedder
	cfg       config.Config
	logger    observability.Logger
	weights   types.ScoreWeights
	lambda    float64
}

func NewOrchestrator(shortMgr *shortterm.Manager, longRet *longterm.Retriever, entityMgr *entity.Manager, emb llm.Embedder, cfg config.Config, logger observability.Logger) *Orchestrator {
	if logger == nil {
		logger = observability.NewLogger()
	}
	a, b, g := cfg.NormalizedWeights()
	return &Orchestrator{
		shortMgr:  shortMgr,
		longRet:   longRet,
		entityMgr: entityMgr,
		embedder:  emb,
		cfg:       cfg,
		logger:    logger,
		weights:   types.ScoreWeights{Alpha: a, Beta: b, Gamma: g},
		lambda:    cfg.RecencyLambda,
	}
}

// Recall 聚合并评分，返回 topK 条记忆
func (o *Orchestrator) Recall(ctx context.Context, sessionID, userID, query string, topK int) ([]types.ScoredMemory, error) {
	start := time.Now()
	now := start
	var candidates []*types.Memory

	if o.shortMgr != nil {
		msgs, summary := o.shortMgr.Recall(ctx, sessionID, o.cfg.ShortMaxMessages)
		if summary != "" {
			mem := types.NewMemory(userID, sessionID, "[对话摘要] "+summary, types.MemoryTypeShort)
			mem.Importance = 3
			mem.CreatedAt = now.Add(-time.Hour)
			mem.AccessedAt = mem.CreatedAt
			candidates = append(candidates, mem)
		}
		for _, msg := range msgs {
			content := msg.Content
			isSummary := strings.HasPrefix(content, "[对话摘要]")
			mem := types.NewMemory(userID, sessionID, content, types.MemoryTypeShort)
			mem.CreatedAt = msg.CreatedAt
			mem.AccessedAt = msg.CreatedAt
			if isSummary {
				mem.Importance = 3
			} else {
				mem.Importance = 1
			}
			if o.embedder != nil {
				if emb, err := o.embedder.Embed(ctx, content); err == nil {
					mem.Embedding = emb
				}
			}
			candidates = append(candidates, mem)
		}
	}

	if o.longRet != nil && o.longRet.Available() {
		if mems, err := o.longRet.Retrieve(ctx, query, userID); err == nil {
			candidates = append(candidates, mems...)
		} else {
			o.logger.Warnf("long-term retrieve failed: %v", err)
		}
	}

	if o.entityMgr != nil {
		if mems, err := o.entityMgr.Query(ctx, query, 5); err == nil && len(mems) > 0 {
			candidates = append(candidates, mems...)
		}
	}

	var queryEmb []float32
	if o.embedder != nil && query != "" {
		queryEmb, _ = o.embedder.Embed(ctx, query)
	}

	scored := make([]types.ScoredMemory, 0, len(candidates))
	for _, mem := range candidates {
		recency := CalcRecency(mem, now, o.lambda)
		relevance := CalcRelevance(queryEmb, mem)
		importance := CalcImportance(mem)
		score := o.weights.Alpha*recency + o.weights.Beta*relevance + o.weights.Gamma*importance
		scored = append(scored, types.ScoredMemory{
			Memory:     mem,
			Score:      score,
			Recency:    recency,
			Relevance:  relevance,
			Importance: importance,
		})
	}

	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if topK > 0 && len(scored) > topK {
		scored = scored[:topK]
	}

	o.logger.Debugf("recall done in %v, got %d results (topK=%d)", time.Since(start), len(scored), topK)
	return scored, nil
}

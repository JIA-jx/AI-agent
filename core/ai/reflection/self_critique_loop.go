package reflection

import (
	"context"
	"encoding/json"
	"strings"

	"thunder/logs"

	"github.com/cloudwego/eino/adk"
)

// ReAct 循环外加自检迭代   行为：
//   1. 第一轮 runner.Query(question) → 收集所有事件
//   2. Critique 评估第一轮答案
//   3. 如果不够好 + 未达最大轮次 → BuildRevisedQuery → 第二轮 runner.Query
//   4. 重复直到通过或耗尽轮次
//   5. 把所有事件（含中间轮次的事件）串联返回
type runnerIter interface {
	Next() (*adk.AgentEvent, bool)
}

type SelfCritiqueLoop struct {
	runner   *adk.Runner
	critique *SelfCritique
	cfg      SelfCritiqueConfig
}

func NewSelfCritiqueLoop(runner *adk.Runner, critique *SelfCritique) *SelfCritiqueLoop {
	cfg := critique.cfg
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 3
	}
	return &SelfCritiqueLoop{runner: runner, critique: critique, cfg: cfg}
}

// SelfCritiqueIter 带自检的事件迭代器
type SelfCritiqueIter struct {
	ctx          context.Context
	loop         *SelfCritiqueLoop
	question     string
	currentQuery string // 当前轮次要丢给 runner 的 query
	round        int    // 当前轮次（0-indexed）
	subIter      runnerIter
	finalAnswer  string
	done         bool
}

// Query 执行带自检迭代的查询，返回流式事件迭代器
func (l *SelfCritiqueLoop) Query(ctx context.Context, question string) *SelfCritiqueIter {
	return &SelfCritiqueIter{
		ctx:          ctx,
		loop:         l,
		question:     question,
		round:        0,
		currentQuery: question,
	}
}

func (it *SelfCritiqueIter) Next() (*adk.AgentEvent, bool) {
	// 1. 如果还没启动当前轮次，初始化 runner.Query
	if it.subIter == nil && !it.done {
		var err error
		it.subIter, err = it.initRound()
		if err != nil {
			it.done = true
			return &adk.AgentEvent{Err: err}, false
		}
	}

	// 2. 消费当前轮次的事件
	for {
		event, ok := it.subIter.Next()
		if !ok {
			// 当前轮次结束
			it.subIter = nil

			// 3. 收集完答案，做 critique
			shouldRetry := it.doCritiqueAndDecide()
			if shouldRetry {
				return it.Next() // 递归启动下一轮
			}
			it.done = true
			return nil, false
		}

		// 4. 收集最终答案内容，同时把事件透传给上游
		it.collectAnswer(event)
		return event, true
	}
}

func (it *SelfCritiqueIter) initRound() (runnerIter, error) {
	it.round++
	logs.Infof("SelfCritiqueLoop: starting round %d/%d, query=%s", it.round, it.loop.cfg.MaxRounds, truncate(it.currentQuery, 100))
	it.finalAnswer = ""

	return it.loop.runner.Query(it.ctx, it.currentQuery), nil
}

func (it *SelfCritiqueIter) collectAnswer(event *adk.AgentEvent) {
	if event.Output == nil || event.Output.MessageOutput == nil {
		return
	}

	msg, err := event.Output.MessageOutput.GetMessage()
	if err != nil {
		return
	}
	if msg.Content != "" {
		it.finalAnswer += msg.Content
	}
}

func (it *SelfCritiqueIter) doCritiqueAndDecide() bool {
	if !it.loop.critique.Enabled() || it.round >= it.loop.cfg.MaxRounds {
		return false // 不再迭代
	}

	if strings.TrimSpace(it.finalAnswer) == "" {
		logs.Warnf("SelfCritiqueLoop: round %d got empty answer, skipping critique", it.round)
		return false
	}

	result, err := it.loop.critique.Critique(it.ctx, it.question, it.finalAnswer)
	if err != nil || result == nil {
		logs.Warnf("SelfCritiqueLoop: critique failed, stop iterating: %v", err)
		return false
	}
	if result.Pass() {
		logs.Infof("SelfCritiqueLoop: round %d passed (overall=%.1f, verdict=%s)", it.round, result.Overall, result.Verdict)
		return false // 通过了，不再迭代
	}

	// 不够好 → 构建改进 query → 重启 runner
	logs.Infof("SelfCritiqueLoop: round %d not passed (overall=%.1f), retrying with feedback", it.round, result.Overall)
	it.currentQuery = it.loop.critique.BuildRevisedQuery(it.question, it.finalAnswer, result)

	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseCritiqueJSON(s string, out *CritiqueResult) error {
	return json.Unmarshal([]byte(s), out)
}

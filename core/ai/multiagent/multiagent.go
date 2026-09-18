package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"basemodel/logs"

	"github.com/cloudwego/eino/adk"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// RunnerAgent
// Debate 模式下每个 Agent 需要看到其他 Agent 的完整答案才能互评修订，
// 逐 token 流式会导致其他 Agent 看到半拉子答案。
// 流式由 Debate.Stream() 对外暴露结构化事件实现（每轮结束推事件）。
type RunnerAgent struct {
	Name   string
	Runner *adk.Runner
}

func NewRunnerAgent(name string, runner *adk.Runner) *RunnerAgent {
	return &RunnerAgent{Name: name, Runner: runner}
}

func (r *RunnerAgent) Run(ctx context.Context, query string) (string, error) {
	iter := r.Runner.Query(ctx, query)
	var sb strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return sb.String(), event.Err
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			if msg != nil && msg.Content != "" {
				sb.WriteString(msg.Content)
			}
		}
	}
	return sb.String(), nil
}

type DebateConfig struct {
	Rounds       int                     // 辩论轮次，默认 2
	VoteModel    einoModel.BaseChatModel // LLM 投票模型
	EnableDebate bool
}

func DefaultDebateConfig() DebateConfig {
	return DebateConfig{
		Rounds:       2,
		EnableDebate: true,
	}
}

type DebateResult struct {
	WinnerName string
	WinnerAns  string
	AllAnswers map[string]string
	Rounds     int
}

type DebateStreamEventType string

const (
	EventRoundStart    DebateStreamEventType = "round_start"    // 某轮开始
	EventAgentAnswered DebateStreamEventType = "agent_answered" // 某 agent 本轮回答完毕
	EventRoundEnd      DebateStreamEventType = "round_end"      // 某轮结束
	EventVote          DebateStreamEventType = "vote"           // 投票结果
	EventError         DebateStreamEventType = "error"
)

// DebateStreamEvent 推给前端的结构化事件
type DebateStreamEvent struct {
	Type       DebateStreamEventType `json:"type"`
	Round      int                   `json:"round,omitempty"`
	AgentName  string                `json:"agent_name,omitempty"`
	AgentAns   string                `json:"agent_ans,omitempty"`
	WinnerName string                `json:"winner_name,omitempty"`
	WinnerAns  string                `json:"winner_ans,omitempty"`
	Reason     string                `json:"reason,omitempty"`
	Err        error                 `json:"-"`
}

// Debate 辩论编排器
// 两种使用模式：
//  1. Run(ctx, question) —— silent 执行完毕后一次性返回 DebateResult
//  2. Stream(ctx, question) —— 返回迭代器，每轮结束推 DebateStreamEvent
//
// 流程（每轮都相同）：
//   - Round 0: 所有 Agent 并行 silent 回答首轮问题
//   - Round 1~N: 每个 Agent 看到其他人上轮答案 → 修订自己的答案
//   - 最后: VoteModel（或启发式）投票选最佳
type Debate struct {
	agents []*RunnerAgent
	cfg    DebateConfig
}

func NewDebate(agents []*RunnerAgent, cfg DebateConfig) *Debate {
	if cfg.Rounds <= 0 {
		cfg.Rounds = 2
	}
	return &Debate{agents: agents, cfg: cfg}
}

func (d *Debate) Enabled() bool {
	return d.cfg.EnableDebate && len(d.agents) >= 2
}

func (d *Debate) Run(ctx context.Context, question string) (*DebateResult, error) {
	if !d.Enabled() {
		return nil, fmt.Errorf("debate not enabled or agents < 2")
	}

	answers, err := d.runAllRounds(ctx, question, nil)
	if err != nil {
		return nil, err
	}
	winnerName, winnerAns := d.vote(ctx, answers)

	return &DebateResult{
		WinnerName: winnerName,
		WinnerAns:  winnerAns,
		AllAnswers: answers,
		Rounds:     d.cfg.Rounds,
	}, nil
}

func (d *Debate) Stream(ctx context.Context, question string) *DebateIter {
	iter := &DebateIter{
		ctx:      ctx,
		question: question,
		ch:       make(chan DebateStreamEvent, 16),
	}
	go func() {
		defer close(iter.ch)
		answers, err := d.runAllRounds(ctx, question, func(ev DebateStreamEvent) {
			select {
			case iter.ch <- ev:
			case <-ctx.Done():
			}
		})
		if err != nil {
			iter.ch <- DebateStreamEvent{Type: EventError, Err: err}
			return
		}

		// 最后推投票结果
		winnerName, winnerAns := d.vote(ctx, answers)
		iter.ch <- DebateStreamEvent{
			Type:       EventVote,
			WinnerName: winnerName,
			WinnerAns:  winnerAns,
		}
	}()
	return iter
}

type DebateIter struct {
	ctx      context.Context
	question string
	ch       chan DebateStreamEvent
}

// Next 消费下一个 DebateStreamEvent。done=true 表示流结束。
func (it *DebateIter) Next() (DebateStreamEvent, bool) {
	select {
	case <-it.ctx.Done():
		return DebateStreamEvent{Type: EventError, Err: it.ctx.Err()}, false
	case ev, ok := <-it.ch:
		return ev, ok
	}
}

func (d *Debate) runAllRounds(ctx context.Context, question string, onEvent func(DebateStreamEvent)) (map[string]string, error) {
	answers := make(map[string]string)

	for r := 0; r < d.cfg.Rounds; r++ {
		if onEvent != nil {
			onEvent(DebateStreamEvent{Type: EventRoundStart, Round: r})
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		roundErrCh := make(chan error, len(d.agents))

		for _, agent := range d.agents {
			wg.Add(1)
			go func(a *RunnerAgent, roundIdx int) {
				defer wg.Done()

				// 构建查询：首轮原始问题，后续轮次带其他 Agent 答案
				var q string
				if roundIdx == 0 {
					q = question
				} else {
					q = d.buildRevisedQuery(question, a.Name, answers)
				}

				ans, err := a.Run(ctx, q)
				if err != nil {
					roundErrCh <- fmt.Errorf("agent %s round %d failed: %w", a.Name, roundIdx, err)
					return
				}
				mu.Lock()
				answers[a.Name] = ans
				mu.Unlock()

				logs.Infof("Debate round %d: agent %s answered (%d chars)", roundIdx, a.Name, len(ans))
				if onEvent != nil {
					onEvent(DebateStreamEvent{
						Type:      EventAgentAnswered,
						Round:     roundIdx,
						AgentName: a.Name,
						AgentAns:  ans,
					})
				}
			}(agent, r)
		}
		wg.Wait()
		close(roundErrCh)

		for e := range roundErrCh {
			logs.Warnf("Debate: %v", e)
		}

		if onEvent != nil {
			onEvent(DebateStreamEvent{Type: EventRoundEnd, Round: r})
		}
	}

	return answers, nil
}

// buildRevisedQuery 给某个 agent 构建改进 prompt
func (d *Debate) buildRevisedQuery(question, selfName string, answers map[string]string) string {
	var sb strings.Builder
	sb.WriteString("【你参与的是一个 AI 辩论系统，以下是你和其他 Agent 对同一问题的回答】\n\n")

	for name, ans := range answers {
		if name == selfName {
			sb.WriteString(fmt.Sprintf("### %s（你的回答）\n%s\n\n", name, ans))
		}
	}
	for name, ans := range answers {
		if name != selfName {
			sb.WriteString(fmt.Sprintf("### %s（对方回答）\n%s\n\n", name, ans))
		}
	}

	sb.WriteString("请对比分析：\n")
	sb.WriteString("1. 对方的回答有什么优点值得你学习？\n")
	sb.WriteString("2. 对方的回答有什么错误或疏漏？\n")
	sb.WriteString("3. 综合后，你给出一个更优的回答\n\n")
	sb.WriteString(fmt.Sprintf("【原始问题】\n%s", question))
	return sb.String()
}

// vote 投票选最佳（启发式 fallback 或 LLM 投票）
func (d *Debate) vote(ctx context.Context, answers map[string]string) (string, string) {
	if d.cfg.VoteModel == nil {
		return voteByLength(answers)
	}

	// LLM 投票
	var sb strings.Builder
	sb.WriteString("请从以下多个 AI Agent 的回答中选出最佳答案（JSON 输出）：\n\n")
	for name, ans := range answers {
		sb.WriteString(fmt.Sprintf("--- Agent: %s ---\n%s\n\n", name, ans))
	}
	sb.WriteString("输出 JSON: {\"best_agent\": \"名称\", \"reason\": \"理由\"}")

	resp, err := d.cfg.VoteModel.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: "你是一个公正的 AI 评审专家，负责在多个 AI 回答中选出最佳。"},
		{Role: schema.User, Content: sb.String()},
	})
	if err != nil {
		logs.Warnf("Debate vote LLM failed, fallback to longest: %v", err)
		return voteByLength(answers)
	}

	var result struct {
		BestAgent string `json:"best_agent"`
		Reason    string `json:"reason"`
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimSuffix(content, "```")
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		logs.Warnf("Debate vote parse failed, fallback: %v", err)
	}

	if result.BestAgent != "" {
		if ans, ok := answers[result.BestAgent]; ok {
			return result.BestAgent, ans
		}
	}
	return voteByLength(answers)
}

// voteByLength 启发式投票：选答案最长的
func voteByLength(answers map[string]string) (string, string) {
	bestName, bestLen := "", 0
	for name, ans := range answers {
		if len(ans) > bestLen {
			bestLen = len(ans)
			bestName = name
		}
	}
	if bestName == "" {
		for n, a := range answers {
			return n, a
		}
	}
	return bestName, answers[bestName]
}

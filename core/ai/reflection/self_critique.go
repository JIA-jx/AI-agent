package reflection

import (
	"context"
	"fmt"
	"strings"

	"basemodel/logs"

	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const critiqueSystemPrompt = `你是一个严谨的 AI 输出质量评审专家。请根据以下标准评估 AI 助理的回答质量：

评估维度（每项 0-10 分）：
1. Accuracy（准确性）：信息是否正确、是否有事实错误
2. Completeness（完整性）：是否覆盖了问题的所有方面
3. Relevance（相关性）：回答是否紧扣问题、有无跑题
4. Clarity（清晰度）：表达是否清晰、结构是否合理
5. Safety（安全性）：是否有有害/不当内容

输出 JSON 格式，举例为：
{
  "accuracy": 8,
  "completeness": 7,
  "relevance": 9,
  "clarity": 8,
  "safety": 10,
  "overall": 8.4,
  "verdict": "pass" | "improve",
  "feedback": "具体的改进建议，指出哪里不足、怎么改",
  "missing_points": ["遗漏的要点1", "遗漏的要点2"]
}`

type CritiqueResult struct {
	Accuracy      int      `json:"accuracy"`
	Completeness  int      `json:"completeness"`
	Relevance     int      `json:"relevance"`
	Clarity       int      `json:"clarity"`
	Safety        int      `json:"safety"`
	Overall       float64  `json:"overall"`        // 综合得分
	Verdict       string   `json:"verdict"`        // 判决结果
	Feedback      string   `json:"feedback"`       // 改进建议
	MissingPoints []string `json:"missing_points"` // 遗漏要点
}

func (c *CritiqueResult) Pass() bool {
	if c.Verdict == "pass" {
		return true
	}
	return c.Overall >= 7.5
}

type SelfCritiqueConfig struct {
	MaxRounds      int     // 最大迭代轮次，默认 3
	PassThreshold  float64 // 通过阈值，默认 7.5
	EnableCritique bool    // 总开关
}

func DefaultCritiqueConfig() SelfCritiqueConfig {
	return SelfCritiqueConfig{
		MaxRounds:      3,    // 包含首次生成，所以最多改进2次
		PassThreshold:  7.5,  // 综合得分达到此值即停止迭代
		EnableCritique: true, // 总开关可全局关闭自检功能
	}
}

type SelfCritique struct {
	model einoModel.BaseChatModel
	cfg   SelfCritiqueConfig
}

func NewSelfCritique(model einoModel.BaseChatModel, cfg SelfCritiqueConfig) *SelfCritique {
	return &SelfCritique{model: model, cfg: cfg}
}

func (s *SelfCritique) Enabled() bool {
	return s.cfg.EnableCritique && s.model != nil
}

func (s *SelfCritique) Critique(ctx context.Context, question, answer string) (*CritiqueResult, error) {
	if !s.Enabled() {
		return &CritiqueResult{Verdict: "pass", Overall: 10}, nil
	}

	userPrompt := fmt.Sprintf(
		"请评审以下 AI 回答的质量：\n\n【用户问题】\n%s\n\n【AI 回答】\n%s",
		question, answer,
	)

	resp, err := s.model.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: critiqueSystemPrompt},
		{Role: schema.User, Content: userPrompt},
	})
	if err != nil {
		logs.Warnf("SelfCritique: critique LLM call failed: %v", err)
		return nil, err
	}

	result := &CritiqueResult{}
	// 去除返回的外层 JSON 结构包装
	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if err := parseCritiqueJSON(content, result); err != nil {
		logs.Warnf("SelfCritique: parse critique result failed: %v (raw=%s)", err, content)
		// 降级：整体打分偏低
		result.Overall = 5.0
		result.Verdict = "improve"
		result.Feedback = "评审结果解析失败，请人工核查"
	}

	logs.Infof("SelfCritique: verdict=%s, overall=%.1f, feedback=%s", result.Verdict, result.Overall, result.Feedback)
	return result, nil
}

func (s *SelfCritique) BuildRevisedQuery(originalQuestion string, prevAnswer string, critique *CritiqueResult) string {
	var sb strings.Builder
	sb.WriteString("【你之前的回答存在以下问题，请针对性改进】\n\n")
	sb.WriteString(fmt.Sprintf("之前的回答：\n%s\n\n", prevAnswer))
	sb.WriteString(fmt.Sprintf("评审反馈：%s\n\n", critique.Feedback))
	if len(critique.MissingPoints) > 0 {
		sb.WriteString("遗漏的要点：\n")
		for _, p := range critique.MissingPoints {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("【原始问题】\n%s\n\n", originalQuestion))
	sb.WriteString("请重新给出更准确、更完整的回答。")
	return sb.String()
}

package reflection

import (
	"context"
	"encoding/json"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
)

// Questioner 用 LLM 从近期对话生成高阶反思问题
type Questioner struct {
	cmpl   llm.Completer
	logger observability.Logger
}

func NewQuestioner(cmpl llm.Completer, logger observability.Logger) *Questioner {
	return &Questioner{cmpl: cmpl, logger: logger}
}

func (q *Questioner) Generate(ctx context.Context, recentText string) ([]string, error) {
	if q == nil || q.cmpl == nil || strings.TrimSpace(recentText) == "" {
		return nil, nil
	}

	system, user := llm.ReflectionQuestionsPrompt(recentText)
	out, err := q.cmpl.Complete(ctx, system, user)
	if err != nil {
		q.logger.Warnf("generate reflection questions failed: %v", err)
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	raw := extractJSON(out)
	if raw == "" {
		return nil, nil
	}

	var qs []string
	if err := json.Unmarshal([]byte(raw), &qs); err != nil {
		q.logger.Warnf("parse reflection questions failed: %v (raw=%q)", err, raw)
		return nil, err
	}
	return qs, nil
}

func extractJSON(s string) string {
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

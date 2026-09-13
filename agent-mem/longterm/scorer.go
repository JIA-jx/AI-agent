package longterm

import (
	"context"
	"strconv"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/types"
)

type Scorer struct {
	cmpl   llm.Completer
	logger observability.Logger
}

func NewScorer(cmpl llm.Completer, logger observability.Logger) *Scorer {
	return &Scorer{cmpl: cmpl, logger: logger}
}

// Score 返回 0-10 的重要性，LLM 不可用时回落到 5
func (s *Scorer) Score(ctx context.Context, content string) float64 {
	if s == nil || s.cmpl == nil || strings.TrimSpace(content) == "" {
		return 5
	}

	system, user := llm.ImportancePrompt(content)
	out, err := s.cmpl.Complete(ctx, system, user)
	if err != nil {
		s.logger.Warnf("importance score failed: %v", err)
		return 5
	}

	return types.Clamp(parseFloat(out), 0, 10)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	end := 0
	for end < len(s) && (isDigit(s[end]) || s[end] == '.' || s[end] == '-') {
		end++
	}
	if end == 0 {
		return 0
	}

	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0
	}

	return f
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

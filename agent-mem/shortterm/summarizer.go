package shortterm

import (
	"context"

	"agentmem/llm"
	"agentmem/observability"
)

type Summarizer struct {
	cmpl   llm.Completer
	logger observability.Logger
}

func NewSummarizer(cmpl llm.Completer, logger observability.Logger) *Summarizer {
	return &Summarizer{cmpl: cmpl, logger: logger}
}

func (s *Summarizer) Summarize(ctx context.Context, msgs []Message) (string, error) {
	if s == nil || s.cmpl == nil {
		return "", nil
	}

	text := JoinMessages(msgs)
	if text == "" {
		return "", nil
	}

	system, user := llm.SummaryPrompt(text)
	out, err := s.cmpl.Complete(ctx, system, user)
	if err != nil {
		s.logger.Warnf("summarize failed: %v", err)
		return "", err
	}
	return out, nil
}

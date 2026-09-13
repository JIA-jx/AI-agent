package shortterm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (m *Manager) Recall(ctx context.Context, sessionID string, n int) ([]Message, string) {
	win, sum := m.loadWindow(ctx, sessionID)
	if n <= 0 || n > len(win) {
		n = len(win)
	}

	start := len(win) - n
	if start < 0 {
		start = 0
	}
	return win[start:], sum
}

func (m *Manager) loadWindow(ctx context.Context, sessionID string) ([]Message, string) {
	b, err := m.kv.Load(ctx, windowKey(sessionID))
	if err != nil {
		m.logger.Warnf("load window failed: %v", err)
		return nil, ""
	}
	if len(b) == 0 {
		return nil, ""
	}

	var wd WindowData
	if err := json.Unmarshal(b, &wd); err != nil {
		return nil, ""
	}
	return wd.Messages, wd.Summary
}

func (m *Manager) saveWindow(ctx context.Context, sessionID string, win []Message, sum string) {
	wd := WindowData{Messages: win, Summary: sum}
	b, _ := json.Marshal(wd)
	if err := m.kv.Save(ctx, windowKey(sessionID), b); err != nil {
		m.logger.Warnf("save window failed: %v", err)
	}
}

func windowKey(sessionID string) string {
	return "short:win:" + sessionID
}

func totalTokens(msgs []Message) int {
	total := 0
	for i := range msgs {
		total += msgs[i].Tokens
	}
	return total
}

func JoinMessages(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}
	return b.String()
}

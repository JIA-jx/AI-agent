package shortterm

import (
	"context"
	"sync"
	"time"

	"agentmem/observability"
	"agentmem/store"
)

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Tokens    int       `json:"tokens"`
}

type WindowData struct {
	Messages []Message `json:"messages"`
	Summary  string    `json:"summary"`
}

type Manager struct {
	kv           store.KVStore
	tokenizer    *Tokenizer
	summarizer   *Summarizer
	canSummarize bool
	maxTokens    int
	maxMessages  int
	logger       observability.Logger
	locks        sync.Map
}

func NewManager(kv store.KVStore, tok *Tokenizer, sum *Summarizer, canSummarize bool, maxTokens, maxMessages int, logger observability.Logger) *Manager {
	if maxTokens <= 0 {
		maxTokens = 4000
	}

	if maxMessages <= 0 {
		maxMessages = 50
	}

	if logger == nil {
		logger = observability.NewLogger()
	}

	return &Manager{
		kv:           kv,
		tokenizer:    tok,
		summarizer:   sum,
		canSummarize: canSummarize,
		maxTokens:    maxTokens,
		maxMessages:  maxMessages,
		logger:       logger,
	}
}

func (m *Manager) lockFor(sessionID string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *Manager) AddMessage(ctx context.Context, sessionID, role, content string) error {
	mu := m.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()

	win, sum := m.loadWindow(ctx, sessionID)
	tokens := m.tokenizer.Count(content)
	msg := Message{Role: role, Content: content, CreatedAt: time.Now(), Tokens: tokens}
	win = append(win, msg)

	if len(win) > m.maxMessages || totalTokens(win) > m.maxTokens {
		win, sum = m.compact(ctx, win, sum)
	}
	m.saveWindow(ctx, sessionID, win, sum)
	return nil
}

func (m *Manager) GetWindow(ctx context.Context, sessionID string) ([]Message, string) {
	return m.loadWindow(ctx, sessionID)
}

func (m *Manager) Count(ctx context.Context, sessionID string) int {
	win, _ := m.loadWindow(ctx, sessionID)
	return len(win)
}

func (m *Manager) compact(ctx context.Context, win []Message, sum string) ([]Message, string) {
	if len(win) < 4 {
		return win, sum
	}

	half := len(win) / 2
	older := win[:half]
	recent := win[half:]
	if m.canSummarize && m.summarizer != nil {
		summary, err := m.summarizer.Summarize(ctx, older)
		if err == nil && summary != "" {
			if sum != "" {
				sum = sum + "\n" + summary
			} else {
				sum = summary
			}
		}
	}
	if sum != "" {
		head := Message{
			Role:      "system",
			Content:   "[对话摘要] " + sum,
			CreatedAt: time.Now(),
			Tokens:    m.tokenizer.Count(sum),
		}
		recent = append([]Message{head}, recent...)
	}
	return recent, sum
}

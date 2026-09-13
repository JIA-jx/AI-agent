package types

import (
	"time"

	"github.com/google/uuid"
)

type MemoryType string

const (
	MemoryTypeShort   MemoryType = "short"   // 短期记忆：当前会话窗口
	MemoryTypeLong    MemoryType = "long"    // 长期记忆：跨会话事实
	MemoryTypeEntity  MemoryType = "entity"  // 实体记忆：结构化事实
	MemoryTypeInsight MemoryType = "insight" // 洞察：反思合成的高阶记忆
)

type Memory struct {
	ID           string         `json:"id"`
	SessionID    string         `json:"session_id"`
	UserID       string         `json:"user_id"`
	Type         MemoryType     `json:"type"`
	Content      string         `json:"content"`
	Embedding    []float32      `json:"embedding,omitempty"` // 高维向量值存储
	Importance   float64        `json:"importance"`
	CreatedAt    time.Time      `json:"created_at"`
	AccessedAt   time.Time      `json:"accessed_at"`             // 最近访问时间，用于 recency 计算
	SourceIDs    []string       `json:"source_ids,omitempty"`    // 洞察记忆的源记忆 ID
	SupersededBy string         `json:"superseded_by,omitempty"` // 被哪条新记忆替代（冲突消解）
	Metadata     map[string]any `json:"metadata,omitempty"`
}

func NewMemory(userID, sessionID, content string, mtype MemoryType) *Memory {
	now := time.Now()
	return &Memory{
		ID:         uuid.NewString(),
		SessionID:  sessionID,
		UserID:     userID,
		Type:       mtype,
		Content:    content,
		Importance: 1.0,
		CreatedAt:  now,
		AccessedAt: now,
	}
}

func (m *Memory) Touch() {
	m.AccessedAt = time.Now()
}

func (m *Memory) NormalizeImportance() float64 {
	if m.Importance <= 0 {
		return 0
	}
	if m.Importance >= 10 {
		return 1
	}
	return m.Importance / 10.0
}

func (m *Memory) IsInsight() bool { return m.Type == MemoryTypeInsight }

func (m *Memory) IsActive() bool { return m.SupersededBy == "" }

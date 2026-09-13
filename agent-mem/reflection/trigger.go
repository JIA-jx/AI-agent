package reflection

import (
	"sync"
	"time"
)

// Trigger 反思
//  1. 累计重要性达到阈值
//  2. 消息数达到阈值
//  3. 距上次反思超过时间周期
type Trigger struct {
	impThreshold  float64
	msgThreshold  int
	timeThreshold time.Duration
	mu            sync.Mutex
	stats         map[string]*sessionStats
}

type sessionStats struct {
	importanceSum float64
	msgCount      int
	lastReflect   time.Time
}

func NewTrigger(impThreshold float64, msgThreshold int, timeThreshold time.Duration) *Trigger {
	return &Trigger{
		impThreshold:  impThreshold,
		msgThreshold:  msgThreshold,
		timeThreshold: timeThreshold,
		stats:         make(map[string]*sessionStats),
	}
}

func (t *Trigger) statsOf(sessionID string) *sessionStats {
	if s, ok := t.stats[sessionID]; ok {
		return s
	}

	s := &sessionStats{}
	t.stats[sessionID] = s
	return s
}

// Observe 记录一条新消息对统计的贡献
func (t *Trigger) Observe(sessionID string, importance float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.statsOf(sessionID)
	s.importanceSum += importance
	s.msgCount++
}

// ShouldReflect 是否应触发反思
func (t *Trigger) ShouldReflect(sessionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.statsOf(sessionID)
	if s.importanceSum >= t.impThreshold {
		return true
	}

	if s.msgCount >= t.msgThreshold {
		return true
	}

	if !s.lastReflect.IsZero() && time.Since(s.lastReflect) >= t.timeThreshold {
		return true
	}
	return false
}

// MarkReflected 标记已完成反思并重置累计统计
func (t *Trigger) MarkReflected(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.statsOf(sessionID)
	s.importanceSum = 0
	s.msgCount = 0
	s.lastReflect = time.Now()
}

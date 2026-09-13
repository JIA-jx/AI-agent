package recall

import (
	"math"
	"time"

	"agentmem/types"
)

// CalcRecency 时间近度：exp(-lambda * deltaHours)，最近访问越久值越小
func CalcRecency(mem *types.Memory, now time.Time, lambda float64) float64 {
	if mem == nil {
		return 0
	}

	delta := now.Sub(mem.AccessedAt)
	if delta < 0 {
		delta = 0
	}

	hours := delta.Hours()
	return math.Exp(-lambda * hours)
}

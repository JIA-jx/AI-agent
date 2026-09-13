package longterm

import (
	"agentmem/observability"
	"agentmem/types"
)

type Merger struct {
	threshold float64
	logger    observability.Logger
}

func NewMerger(threshold float64, logger observability.Logger) *Merger {
	if threshold <= 0 {
		threshold = 0.85
	}
	return &Merger{threshold: threshold, logger: logger}
}

// Dedup 返回去重后的新记忆
func (m *Merger) Dedup(newMems, existing []*types.Memory) []*types.Memory {
	if len(newMems) == 0 {
		return nil
	}

	var out []*types.Memory
	for _, nm := range newMems {
		if len(nm.Embedding) == 0 {
			out = append(out, nm)
			continue
		}

		dup := false
		for _, ex := range existing {
			if len(ex.Embedding) == 0 {
				continue
			}
			if !ex.IsActive() {
				continue
			}
			if types.CosineSimilarity(nm.Embedding, ex.Embedding) >= m.threshold {
				dup = true
				break
			}
		}
		if dup {
			continue
		}

		for _, kept := range out {
			if types.CosineSimilarity(nm.Embedding, kept.Embedding) >= m.threshold {
				dup = true
				break
			}
		}
		if dup {
			continue
		}

		out = append(out, nm)
	}
	return out
}

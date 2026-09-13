package recall

import "agentmem/types"

func CalcImportance(mem *types.Memory) float64 {
	if mem == nil {
		return 0
	}
	return mem.NormalizeImportance()
}

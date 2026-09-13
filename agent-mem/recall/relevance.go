package recall

import "agentmem/types"

func CalcRelevance(queryEmb []float32, mem *types.Memory) float64 {
	if mem == nil || len(mem.Embedding) == 0 {
		return 0
	}

	sim := types.CosineSimilarity(queryEmb, mem.Embedding)
	if sim < 0 {
		return 0
	}

	return sim
}

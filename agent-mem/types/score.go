package types

import (
	"math"
	"time"
)

type ScoredMemory struct {
	Memory     *Memory `json:"memory"`
	Score      float64 `json:"score"`      // 综合得分
	Recency    float64 `json:"recency"`    // 时间近度
	Relevance  float64 `json:"relevance"`  // 语义相关度
	Importance float64 `json:"importance"` // 重要性归一化
}

type ScoreWeights struct {
	Alpha float64 // recency 权重  (时间近度)
	Beta  float64 // relevance 权重 (语义相关度)
	Gamma float64 // importance 权重 (重要性)
}

// CosineSimilarity 余弦相似度，向量长度不等或为零向量时返回 0
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Clamp 控分   将 x 限制在 [lo, hi]
func Clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func (m *Memory) RecencyScore() float64 {
	hours := time.Since(m.AccessedAt).Hours()
	return math.Exp(-hours / 24) // 1天内不衰减，之后指数衰减
}

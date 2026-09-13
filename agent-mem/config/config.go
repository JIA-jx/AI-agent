package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config  agent-mem 全局配置
type Config struct {
	OpenAIKey     string
	OpenAIBaseURL string
	ChatModel     string
	EmbedModel    string
	EmbedDim      int // 向量维数

	PGDSN         string
	RedisAddr     string
	RedisPassword string

	ShortMaxTokens   int
	ShortMaxMessages int

	RecencyLambda float64 // 时间衰减系数，exp(-lambda*deltaHours)
	Alpha         float64 // recency 权重
	Beta          float64 // relevance 权重
	Gamma         float64 // importance 权重

	LongTopK       int
	MergeThreshold float64 // 合并去重的相似度阈值

	ReflImportanceThreshold float64       // 累计重要性达到该值触发反思
	ReflMsgCountThreshold   int           // 消息数达到该值触发反思
	ReflTimeThreshold       time.Duration // 时间周期触发反思

	StoreBackend string
	SessionTTL   time.Duration
}

func Default() Config {
	return Config{
		ChatModel:               "gpt-4o-mini",
		EmbedModel:              "text-embedding-3-small",
		EmbedDim:                1536,
		ShortMaxTokens:          4000,
		ShortMaxMessages:        50,
		RecencyLambda:           0.01,
		Alpha:                   0.35,
		Beta:                    0.45,
		Gamma:                   0.20,
		LongTopK:                10,
		MergeThreshold:          0.85,
		ReflImportanceThreshold: 150,
		ReflMsgCountThreshold:   10,
		ReflTimeThreshold:       30 * time.Minute,
		StoreBackend:            "memory",
		SessionTTL:              24 * time.Hour,
	}
}

func LoadFromEnv() Config {
	c := Default()
	envStr("OPENAI_API_KEY", &c.OpenAIKey)
	envStr("OPENAI_BASE_URL", &c.OpenAIBaseURL)
	envStr("AGENTMEM_CHAT_MODEL", &c.ChatModel)
	envStr("AGENTMEM_EMBED_MODEL", &c.EmbedModel)
	envStr("AGENTMEM_PG_DSN", &c.PGDSN)
	envStr("AGENTMEM_REDIS_ADDR", &c.RedisAddr)
	envStr("AGENTMEM_REDIS_PASSWORD", &c.RedisPassword)
	envStr("AGENTMEM_STORE_BACKEND", &c.StoreBackend)
	envInt("AGENTMEM_SHORT_MAX_TOKENS", &c.ShortMaxTokens)
	envInt("AGENTMEM_LONG_TOPK", &c.LongTopK)
	envFloat("AGENTMEM_RECENCY_LAMBDA", &c.RecencyLambda)
	envFloat("AGENTMEM_ALPHA", &c.Alpha)
	envFloat("AGENTMEM_BETA", &c.Beta)
	envFloat("AGENTMEM_GAMMA", &c.Gamma)
	envFloat("AGENTMEM_MERGE_THRESHOLD", &c.MergeThreshold)
	return c
}

func (c Config) Validate() error {
	if c.Alpha+c.Beta+c.Gamma == 0 {
		return fmt.Errorf("score weights sum to zero")
	}

	if c.EmbedDim <= 0 {
		return fmt.Errorf("embed dim must be positive")
	}

	switch c.StoreBackend {
	case "memory", "postgres", "redis":
	default:
		return fmt.Errorf("unknown store backend: %s", c.StoreBackend)
	}
	return nil
}

// NormalizedWeights 返回归一化后的三因子权重
func (c Config) NormalizedWeights() (alpha, beta, gamma float64) {
	sum := c.Alpha + c.Beta + c.Gamma
	if sum == 0 {
		return 0, 0, 0
	}
	return c.Alpha / sum, c.Beta / sum, c.Gamma / sum
}

func (c Config) HasLLM() bool { return c.OpenAIKey != "" }

func (c Config) HasPostgres() bool { return c.PGDSN != "" }

func (c Config) HasRedis() bool { return c.RedisAddr != "" }

func envStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func envInt(key string, dst *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func envFloat(key string, dst *float64) {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*dst = f
		}
	}
}

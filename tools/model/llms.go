package model

import (
	"github.com/cloudwego/eino-ext/components/embedding/dashscope"
	"github.com/cloudwego/eino-ext/components/embedding/ollama"
	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/google/uuid"
	"thunder/ai/einos"
)

var (
	OllamaProvider = "ollama"
	OpenAIProvider = "openai"
	QwenProvider   = "qwen"
)

type LLMStatus string

var (
	LLMStatusActive   LLMStatus = "active"
	LLMStatusInactive LLMStatus = "inactive"
)

type LLMType string

var (
	LLMTypeChat      LLMType = "chat"
	LLMTypeEmbedding LLMType = "embedding"
)

type ProviderConfig struct {
	BaseModel
	UserID      uuid.UUID `json:"userId" gorm:"column:user_id;type:uuid;not null;index"`
	Name        string    `json:"name" gorm:"column:name;type:varchar(255);not null"`        // 提供商名称
	Provider    string    `json:"provider" gorm:"column:provider;type:varchar(50);not null"` // 提供商标识
	Description string    `json:"description" gorm:"column:description;type:text"`
	APIKey      string    `json:"apiKey" gorm:"column:api_key;type:varchar(255)"`
	APIBase     string    `json:"apiBase" gorm:"column:api_base;type:varchar(255)"` // API地址
	Status      LLMStatus `json:"status" gorm:"column:status;type:varchar(20);default:'active'"`
}

func (ProviderConfig) TableName() string {
	return "provider_configs"
}

type LLM struct {
	BaseModel
	UserID           uuid.UUID      `json:"userId" gorm:"column:user_id;type:uuid;not null;index"`
	Name             string         `json:"name" gorm:"column:name;type:varchar(255);not null"`
	Description      string         `json:"description" gorm:"column:description;type:text"`
	ProviderConfigID uuid.UUID      `json:"providerConfigId" gorm:"column:provider_config_id;type:uuid"`
	ProviderConfig   ProviderConfig `json:"providerConfig" gorm:"foreignKey:ProviderConfigID"`
	ModelName        string         `json:"modelName" gorm:"column:model_name;type:varchar(255);not null"`
	ModelType        LLMType        `json:"modelType" gorm:"column:model_type;type:varchar(20);default:'chat'"`
	Config           LLMConfig      `json:"config" gorm:"column:config;type:jsonb"`
	Status           LLMStatus      `json:"status" gorm:"column:status;type:varchar(20);default:'active'"`
}

func (*LLM) TableName() string {
	return "llms"
}

func (l *LLM) ToEmbeddingConfig() *einos.EmbeddingModelConfig {
	var config *einos.EmbeddingModelConfig
	switch l.ProviderConfig.Provider {
	case einos.EmbeddingOllama:
		config = &einos.EmbeddingModelConfig{
			OllamaConfig: &ollama.EmbeddingConfig{
				Model:   l.ModelName,
				BaseURL: l.ProviderConfig.APIBase,
			},
		}
	case einos.EmbeddingOpenai:
		config = &einos.EmbeddingModelConfig{
			OpenaiConfig: &openai.EmbeddingConfig{
				Model:   l.ModelName,
				BaseURL: l.ProviderConfig.APIBase,
				APIKey:  l.ProviderConfig.APIKey,
			},
		}
	case einos.EmbeddingDashscope:
		config = &einos.EmbeddingModelConfig{
			DashscopeConfig: &dashscope.EmbeddingConfig{
				Model:  l.ModelName,
				APIKey: l.ProviderConfig.APIKey,
			},
		}
	default:
		config = &einos.EmbeddingModelConfig{
			OpenaiConfig: &openai.EmbeddingConfig{
				Model:   l.ModelName,
				BaseURL: l.ProviderConfig.APIBase,
				APIKey:  l.ProviderConfig.APIKey,
			},
		}
	}
	return config
}

type LLMConfig struct {
	MaxTokens   int     `json:"maxTokens"`
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
}

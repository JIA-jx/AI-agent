package model

import (
	"time"

	"github.com/google/uuid"
)

type AgentStatus string

var (
	Private  AgentStatus = "private"
	Public               = "public"
	LinkOnly             = "link_only"
)

type Agent struct {
	BaseModel
	CreatorID    uuid.UUID `json:"creatorId" gorm:"column:creator_id;type:uuid;not null"`
	Name         string    `json:"name" gorm:"column:name;type:varchar(255);not null"`
	Description  string    `json:"description" gorm:"column:description;type:text"`
	Icon         string    `json:"icon" gorm:"column:icon;type:varchar(512)"`
	SystemPrompt string    `json:"systemPrompt" gorm:"column:system_prompt;type:text"`

	ModelProvider   string `json:"modelProvider" gorm:"column:model_provider;type:varchar(50);not null;default:'openai'"`
	ModelName       string `json:"modelName" gorm:"column:model_name;type:varchar(100);not null"`
	ModelParameters JSON   `json:"modelParameters" gorm:"column:model_parameters;type:jsonb"`

	// OpeningDialogue 开场白
	OpeningDialogue    string `json:"openingDialogue" gorm:"column:opening_dialogue;type:text"`
	SuggestedQuestions JSON   `json:"suggestedQuestions" gorm:"column:suggested_questions;type:jsonb"`

	Version    uint        `json:"version" gorm:"column:version;type:int;not null;default:1"`
	Status     AgentStatus `json:"status" gorm:"column:status;type:varchar(20);not null;default:'draft'"`
	Visibility AgentStatus `json:"visibility" gorm:"column:visibility;type:varchar(20);not null;default:'private'"`
	// InvocationCount 调用次数
	InvocationCount uint64     `json:"invocationCount" gorm:"column:invocation_count;type:bigint;not null;default:0"`
	PublishedAt     *time.Time `json:"publishedAt" gorm:"column:published_at;type:timestamptz"`

	Tools          []*Tool          `json:"tools" gorm:"many2many:agent_tools"`
	KnowledgeBases []*KnowledgeBase `json:"knowledgeBases" gorm:"many2many:agent_knowledge_bases"`
	Agents         []*AgentMarket   `json:"agentMarkets" gorm:"many2many:agent_agents"`
	Workflows      []*Workflow      `json:"workflows" gorm:"many2many:agent_workflows"`
	Skills         []*Skill         `json:"skills" gorm:"many2many:agent_skills"`

	// Agent运行模式：general(通用对话), supervisor(监督者模式), deep(深度编排模式)
	Mode AgentMode `json:"mode" gorm:"column:agent_mode;type:varchar(50);not null;default:'general'"`
	// DeepConfig DeepAgent专属配置，JSON格式存储
	DeepConfig JSON `json:"deepConfig" gorm:"column:deep_config;type:jsonb"`
}

type AgentMode string

const (
	GeneralAgentMode AgentMode = "general"
	DeepAgentMode    AgentMode = "deep"
)

type DeepAgentConfig struct {
	MaxIterations int         `json:"max_iterations"` // 最大迭代轮数
	EnableTodos   bool        `json:"enable_todos"`   // 待办事项启用
	SubAgentIDs   []uuid.UUID `json:"sub_agent_ids"`  // 子智能体
}

func (j JSON) ToDeepAgentConfig() *DeepAgentConfig {
	config := &DeepAgentConfig{
		EnableTodos:   true,
		MaxIterations: 10,
	}

	if maxIter, ok := j["max_iterations"].(float64); ok {
		config.MaxIterations = int(maxIter)
	}

	if todos, ok := j["enable_todos"].(bool); ok {
		config.EnableTodos = todos
	}

	if subAgentIDs, ok := j["sub_agent_ids"].([]any); ok {
		for _, id := range subAgentIDs {
			if agentId, err := uuid.Parse(id.(string)); err == nil {
				config.SubAgentIDs = append(config.SubAgentIDs, agentId)
			}
		}
	}
	return config
}

func (Agent) TableName() string {
	return "agents"
}

type ModelsParams struct {
	MaxTokens        int     `json:"maxTokens"`
	Temperature      float64 `json:"temperature"`
	TopP             float64 `json:"topP"`
	N                int     `json:"n"`                // 多回复筛选用
	Stop             []any   `json:"stop"`             // 停止指示词
	PresencePenalty  float64 `json:"presencePenalty"`  // 新鲜度惩罚
	FrequencyPenalty float64 `json:"frequencyPenalty"` // 重复度惩罚
}

func (j JSON) ToModelParams() ModelsParams {
	params := ModelsParams{}

	if maxTokens, ok := j["maxTokens"].(float64); ok {
		params.MaxTokens = int(maxTokens)
	}

	if temperature, ok := j["temperature"].(float64); ok {
		params.Temperature = temperature
	}

	if topP, ok := j["topP"].(float64); ok {
		params.TopP = topP
	}

	if n, ok := j["n"].(float64); ok {
		params.N = int(n)
	}

	if stop, ok := j["stop"].([]any); ok {
		params.Stop = stop
	}

	if presencePenalty, ok := j["presencePenalty"].(float64); ok {
		params.PresencePenalty = presencePenalty
	}

	if frequencyPenalty, ok := j["frequencyPenalty"].(float64); ok {
		params.FrequencyPenalty = frequencyPenalty
	}

	return params
}

func DefaultAgent(userId uuid.UUID, name string, description string, status AgentStatus) *Agent {
	return &Agent{
		BaseModel: BaseModel{
			ID: uuid.New(),
		},
		CreatorID:   userId,
		Name:        name,
		Description: description,
		Status:      status,

		SuggestedQuestions: JSON{},
		OpeningDialogue:    "",
		SystemPrompt:       "",
		ModelProvider:      "",
		ModelName:          "",
		ModelParameters:    JSON{},
		Version:            1,
		Visibility:         Private,
		InvocationCount:    0,
	}
}

var (
	Enabled  = "enabled"
	Disabled = "disabled"
)

type AgentTool struct {
	AgentID   uuid.UUID `json:"agentId" gorm:"type:uuid;primaryKey"`
	ToolID    uuid.UUID `json:"toolId" gorm:"type:uuid;primaryKey;index"`
	Status    string    `json:"status" gorm:"size:50;default:'active'"`
	CreatedAt time.Time `json:"createdAt"`
}

func (AgentTool) TableName() string {
	return "agent_tools"
}

type AgentKnowledgeStatus string

const (
	AgentKnowledgeStatusEnabled  = "enabled"
	AgentKnowledgeStatusDisabled = "disabled"
)

type AgentKnowledgeBase struct {
	AgentID         uuid.UUID            `json:"agentId" gorm:"column:agent_id;type:bigint unsigned;not null;index:idx_agent_id"`
	KnowledgeBaseId uuid.UUID            `json:"knowledgeBaseId" gorm:"column:knowledge_base_id;type:bigint unsigned;not null;index:idx_kb_id"`
	Status          AgentKnowledgeStatus `json:"status" gorm:"column:status;type:varchar(20);not null;default:'enabled'"`

	KnowledgeBase *KnowledgeBase `json:"knowledge_base" gorm:"foreignKey:knowledge_base_id"`
}

func (AgentKnowledgeBase) TableName() string {
	return "agent_knowledge_bases"
}

type AgentAgent struct {
	AgentId       uuid.UUID   `json:"agentId"`
	AgentMarketId uuid.UUID   `json:"agentMarketId" `
	AgentMarket   AgentMarket `json:"agentMarket" gorm:"foreignKey:agent_market_id"`
}

func (AgentAgent) TableName() string {
	return "agent_agents"
}

type AgentWorkflow struct {
	AgentID    uuid.UUID `json:"agent_id" gorm:"column:agent_id;type:uuid;not null;primaryKey;index:idx_agent_id_status"`
	WorkflowID uuid.UUID `json:"workflow_id" gorm:"column:workflow_id;type:uuid;not null;primaryKey;index:idx_workflow_id"`

	IsDefault        bool      `json:"is_default" gorm:"column:is_default;type:boolean;not null;default:false"`
	TriggerCondition string    `json:"trigger_condition" gorm:"column:trigger_condition;type:varchar(255)"`
	Priority         int       `json:"priority" gorm:"column:priority;type:int;not null;default:0"`
	Status           string    `json:"status" gorm:"column:status;type:varchar(20);not null;default:'enabled'"`
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`

	Workflow *Workflow `json:"workflow" gorm:"foreignKey:WorkflowID"`
}

func (AgentWorkflow) TableName() string {
	return "agent_workflows"
}

type ChatSession struct {
	BaseModel
	AgentID  uuid.UUID     `json:"agentId" gorm:"type:uuid;not null;index"`
	UserID   uuid.UUID     `json:"userId" gorm:"type:uuid;not null;index"`
	Title    string        `json:"title" gorm:"type:varchar(255)"`
	Messages []ChatMessage `json:"messages" gorm:"foreignKey:session_id"`
}

func (ChatSession) TableName() string {
	return "chat_sessions"
}

type ChatMessage struct {
	BaseModel
	SessionID uuid.UUID `json:"sessionId" gorm:"type:uuid;not null;index"`
	Role      string    `json:"role" gorm:"type:varchar(50);not null"`
	Content   string    `json:"content" gorm:"type:text;not null"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}

type AgentSkill struct {
	AgentID   uuid.UUID `json:"agentId" gorm:"column:agent_id;type:uuid;not null;primaryKey;index:idx_agent_skill"`
	SkillID   uuid.UUID `json:"skillId" gorm:"column:skill_id;type:uuid;not null;primaryKey;index:idx_skill_agent"`
	Status    string    `json:"status" gorm:"column:status;type:varchar(20);not null;default:'active'"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`

	Skill *Skill `json:"skill" gorm:"foreignKey:SkillID"`
}

func (AgentSkill) TableName() string {
	return "agent_skills"
}

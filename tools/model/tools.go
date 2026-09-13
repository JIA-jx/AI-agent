package model

import (
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type ToolType string

const (
	McpToolType    ToolType = "mcp"
	SystemToolType          = "system"
)

type Tool struct {
	BaseModel
	CreatorID        uuid.UUID        `json:"creatorId" gorm:"type:uuid;index;not null"`
	Name             string           `json:"name" gorm:"size:255;not null;index"`
	Description      string           `json:"description" gorm:"type:text"`
	ToolType         ToolType         `json:"toolType" gorm:"size:50;not null"`
	IsEnable         bool             `json:"isEnable" gorm:"default:true"`
	ParametersSchema ParametersSchema `json:"parametersSchema" gorm:"type:jsonb"`
	McpConfig        *McpConfig       `json:"mcpConfig" gorm:"type:jsonb"`

	Agents []Agent `json:"agents" gorm:"many2many:agent_tools;"`
}

func (Tool) TableName() string {
	return "tools"
}

type McpConfig struct {
	Type                   string `json:"type,omitempty"`
	Url                    string `json:"url,omitempty"`
	AuthenticationRequired bool   `json:"authenticationRequired,omitempty"`
	CredentialType         string `json:"credentialType,omitempty"`
}

type ParametersSchema map[string]*schema.ParameterInfo

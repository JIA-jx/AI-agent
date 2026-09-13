package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Workflow struct {
	BaseModel
	UserID      uuid.UUID      `json:"user_id" gorm:"column:user_id;type:uuid;not null;index"`
	Name        string         `json:"name" gorm:"column:name;type:varchar(255);not null"`
	Description string         `json:"description" gorm:"column:description;type:varchar(511)"`
	Type        WorkflowType   `json:"type" gorm:"column:type;type:varchar(31);not null"`
	Status      WorkflowStatus `json:"status" gorm:"column:status;type:varchar(31);not null"`
	Version     int            `json:"version" gorm:"column:version;type:int;not null;default:1"`
	Config      JSON           `json:"config" gorm:"column:config;type:jsonb;not null"`
	Data        *Graph         `json:"data" gorm:"column:data;type:jsonb;not null"`

	Agents []Agent `json:"agents" gorm:"many2many:agent_workflows;"`
}

type Graph struct {
	Nodes []*Node `json:"nodes"`
	Edges []*Edge `json:"edges"`
}

func (j *Graph) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *Graph) Scan(value interface{}) error {
	if value == nil {
		*j = Graph{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal JSON value: not a byte slice")
	}

	result := Graph{}
	err := json.Unmarshal(bytes, &result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON value: %w", err)
	}

	*j = result
	return nil
}

func (*Workflow) TableName() string {
	return "workflows"
}

type WorkflowStatus string

const (
	WorkflowStatusValid   WorkflowStatus = "valid"
	WorkflowStatusInValid WorkflowStatus = "invalid"
)

type WorkflowType string

const (
	WorkflowTypeNormal   WorkflowType = "normal"   // 普通工作流
	WorkflowTypeTemplate WorkflowType = "template" // 模板工作流
	WorkflowTypeSystem   WorkflowType = "system"   // 系统工作流
)

type RetryPolicy struct {
	MaxRetries int           `json:"maxRetries"`
	Delay      time.Duration `json:"delay"`
}
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Node struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Category    string                 `json:"category"`
	Data        map[string]interface{} `json:"data"`
	Position    *Position              `json:"position,omitempty"`
	RetryPolicy *RetryPolicy           `json:"retryPolicy,omitempty"`
}

type Edge struct {
	ID           string                 `json:"id"`
	Source       string                 `json:"source"`
	Target       string                 `json:"target"`
	SourceHandle string                 `json:"sourceHandle"`
	TargetHandle string                 `json:"targetHandle"`
	Animated     bool                   `json:"animated"`
	Style        map[string]interface{} `json:"style"`
}

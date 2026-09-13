package types

import (
	"time"

	"github.com/google/uuid"
)

// Entity 实体：用户、组织、技能等结构化对象
type Entity struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Relation 实体间关系，三元组 (subject, predicate, object)
type Relation struct {
	ID        string  `json:"id"`
	Subject   string  `json:"subject"`   // 实体名称或 ID
	Predicate string  `json:"predicate"` // 关系谓词，如 likes / works_at
	Object    string  `json:"object"`    // 实体名称或 ID
	Weight    float64 `json:"weight"`    // 关系强度/置信度
}

// Graph 实体关系子图
type Graph struct {
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

func NewEntity(name, etype string) *Entity {
	return &Entity{
		ID:         uuid.NewString(),
		Name:       name,
		Type:       etype,
		Attributes: map[string]string{},
		CreatedAt:  time.Now(),
	}
}

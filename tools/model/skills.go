package model

import (
	"github.com/google/uuid"
)

type SkillStatus string

const (
	SkillStatusActive   SkillStatus = "active"
	SkillStatusInactive SkillStatus = "inactive"
)

type Skill struct {
	BaseModel
	Name        string      `json:"name" gorm:"column:name;type:varchar(100);not null;uniqueIndex"`
	Description string      `json:"description" gorm:"column:description;type:text"`
	BaseDir     string      `json:"baseDir" gorm:"column:base_dir;type:varchar(512);not null"`
	SourceId    string      `json:"sourceId" gorm:"column:source_id;type:varchar(255);not null"`
	Status      SkillStatus `json:"status" gorm:"column:status;type:varchar(20);not null;default:'active'"`
	CreatorID   uuid.UUID   `json:"creatorId" gorm:"column:creator_id;type:uuid;not null"`
}

func (Skill) TableName() string {
	return "skills"
}

type SkillWithAgentStatus struct {
	Skill
	IsAssociated bool `json:"isAssociated"`
}

type GitHubSource struct {
	BaseModel
	Name        string    `json:"name" gorm:"column:name;type:varchar(100);not null;uniqueIndex"`
	RepoUrl     string    `json:"repoUrl" gorm:"column:repo_url;type:varchar(512);not null"`
	Description string    `json:"description" gorm:"column:description;type:text"`
	CreatorID   uuid.UUID `json:"creatorId" gorm:"column:creator_id;type:uuid;not null"`
}

func (GitHubSource) TableName() string {
	return "github_sources"
}

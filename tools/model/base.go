package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BaseModel struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time      `json:"createdAt" gorm:"column:created_at;not null"`
	UpdatedAt time.Time      `json:"updatedAt" gorm:"column:updated_at;not null"`
	DeletedAt gorm.DeletedAt `json:"deletedAt,omitempty" gorm:"column:deleted_at;index"`
}

type JSON map[string]interface{}

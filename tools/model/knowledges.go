package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

type StringArrayJSON []string

func (s StringArrayJSON) Value() (driver.Value, error) {
	if len(s) == 0 {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *StringArrayJSON) Scan(value interface{}) error {
	if value == nil {
		*s = StringArrayJSON{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan StringArrayJSON")
	}

	return json.Unmarshal(bytes, s)
}

type StorageType string

const (
	StorageTypeElasticSearch StorageType = "es"
	StorageTypeMilvus        StorageType = "milvus"
)

type KnowledgeBase struct {
	BaseModel
	CreatorID              uuid.UUID           `json:"creatorId" gorm:"column:creator_id;type:uuid;not null;index"`
	Name                   string              `json:"name" gorm:"column:name;type:varchar(255);not null;index"`
	Description            string              `json:"description" gorm:"column:description;type:text"`
	ChatModelName          string              `json:"chatModelName" gorm:"column:chat_model_name;type:varchar(255)"`
	ChatModelProvider      string              `json:"chatModelProvider" gorm:"column:chat_model_provider;type:varchar(50)"`
	EmbeddingModelName     string              `json:"embeddingModelName" gorm:"column:embedding_model_name;type:varchar(255)"`
	EmbeddingModelProvider string              `json:"embeddingModelProvider" gorm:"column:embedding_model_provider;type:varchar(50)"`
	EmbeddingDimension     int                 `json:"embeddingDimension" gorm:"column:embedding_dimension;type:integer;not null"`
	StorageType            StorageType         `json:"storageType" gorm:"column:storage_type;type:varchar(50);not null;default:'es'"`
	StorageConfig          JSON                `json:"storageConfig" gorm:"column:storage_config;type:jsonb"`
	DocumentCount          uint                `json:"documentCount" gorm:"column:document_count;type:integer;not null;default:0"`
	Tags                   StringArrayJSON     `json:"tags" gorm:"column:tags;type:jsonb"`
	Status                 KnowledgeBaseStatus `json:"status" gorm:"column:status;type:varchar(20);not null;default:'active'"`

	// 关联关系
	Agents []Agent `json:"agents" gorm:"many2many:agent_knowledge_bases;"`
}

type KnowledgeBaseStatus string

const (
	KnowledgeBaseStatusActive   KnowledgeBaseStatus = "active"
	KnowledgeBaseStatusDisabled KnowledgeBaseStatus = "disabled"
)

func (*KnowledgeBase) TableName() string {
	return "knowledge_bases"
}

type Document struct {
	BaseModel
	KnowledgeBaseID uuid.UUID `json:"knowledgeBaseId" gorm:"column:kb_id;type:uuid;not null;index"`
	CreatorID       uuid.UUID `gorm:"type:uuid;not null;index"`

	Name       string `json:"name" gorm:"column:name;type:varchar(255);not null"`
	FileType   string `json:"fileType" gorm:"column:file_type;type:varchar(50);not null"`
	Size       int64  `json:"size" gorm:"column:size;type:bigint;not null;default:0"`
	TokenCount int    `json:"tokenCount" gorm:"column:token_count;type:integer;default:0"`

	StorageKey string `json:"storageKey" gorm:"column:storage_key;type:varchar(512);not null"`
	FileHash   string `json:"fileHash" gorm:"column:file_hash;type:varchar(64);index"`

	Status       DocumentStatus `json:"status" gorm:"column:status;type:varchar(20);not null;default:'pending';index"`
	ErrorMessage string         `json:"errorMessage" gorm:"column:error_message;type:text"`

	MetaInfo JSON `json:"metaInfo" gorm:"column:meta_info;type:jsonb"`

	Enabled bool `json:"enabled" gorm:"column:enabled;type:boolean;not null;default:true"`

	Chunks []DocumentChunk `json:"chunks,omitempty" gorm:"foreignKey:DocumentID"`
}
type DocumentStatus string

const (
	DocumentStatusPending    DocumentStatus = "pending"
	DocumentStatusProcessing DocumentStatus = "processing"
	DocumentStatusCompleted  DocumentStatus = "completed"
	DocumentStatusFailed     DocumentStatus = "failed"
)

func (*Document) TableName() string {
	return "documents"
}

type DocumentChunk struct {
	BaseModel

	DocumentID      uuid.UUID `json:"documentId" gorm:"column:document_id;type:uuid;not null;index"`
	KnowledgeBaseID uuid.UUID `json:"knowledgeBaseId" gorm:"column:kb_id;type:uuid;not null;index"`

	ElasticSearchID string `json:"esId" gorm:"column:es_id;type:varchar(100);index"`

	ChunkIndex int    `json:"chunkIndex" gorm:"column:chunk_index;type:integer;not null"`
	Content    string `json:"content" gorm:"column:content;type:text;not null"`

	TokenCount int `json:"tokenCount" gorm:"column:token_count;type:integer"`

	MetaInfo JSON        `json:"metaInfo" gorm:"column:meta_info;type:jsonb"`
	Status   ChunkStatus `gorm:"column:status;type:varchar(20);not null;default:'pending'"`
}
type ChunkStatus string

const (
	ChunkStatusPending  ChunkStatus = "pending"
	ChunkStatusEmbedded ChunkStatus = "embedded"
	ChunkStatusDeleted  ChunkStatus = "deleted"
	ChunkStatusDisabled ChunkStatus = "disabled"
)

func (*DocumentChunk) TableName() string {
	return "document_chunks"
}

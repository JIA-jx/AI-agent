package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	Id       uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Username string    `json:"username" gorm:"uniqueIndex;not null"`
	Password string    `json:"password"`

	Avatar        string           `json:"avatar"`
	Status        StatusEnum       `json:"status" gorm:"type:smallint;default:3"`
	LastLoginTime time.Time        `json:"lastLoginTime"`
	CurrentPlan   SubscriptionPlan `json:"currentPlan" gorm:"type:varchar(20);default:'free'"`
	Email         string           `json:"email" gorm:"type:varchar(100);uniqueIndex;not null"`
	EmailVerified bool             `json:"emailVerified" gorm:"type:boolean;default:false"`
}

func (User) TableName() string {
	return "users"
}

type StatusEnum int

var (
	UserStatusNormal  StatusEnum = 1
	UserStatusDisable StatusEnum = 2
	UserStatusPending StatusEnum = 3
)

type UserDTO struct {
	Id            uuid.UUID        `json:"id"`
	Username      string           `json:"username"`
	Avatar        string           `json:"avatar"`
	Status        StatusEnum       `json:"status"`
	LastLoginTime time.Time        `json:"lastLoginTime"`
	CurrentPlan   SubscriptionPlan `json:"currentPlan"`
}

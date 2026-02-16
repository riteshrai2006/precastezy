package models

import (
	"time"

	"gorm.io/gorm"
)

// SkillTypeGorm represents the skill_types table with GORM tags
type SkillTypeGorm struct {
	ID        uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	Name      string         `gorm:"column:name;not null;uniqueIndex" json:"name" example:"Carpentry"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Skills    []SkillGorm    `gorm:"foreignKey:SkillTypeID" json:"skills,omitempty"`
}

// TableName specifies the table name for SkillTypeGorm
func (SkillTypeGorm) TableName() string {
	return "skill_types"
}

// SkillGorm represents the skills table with GORM tags
type SkillGorm struct {
	ID          uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	Name        string         `gorm:"column:name;not null" json:"name" example:"Formwork"`
	SkillTypeID uint           `gorm:"column:skill_type_id;not null" json:"skill_type_id" example:"1"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	SkillType   SkillTypeGorm  `gorm:"foreignKey:SkillTypeID" json:"skill_type,omitempty"`
}

// TableName specifies the table name for SkillGorm
func (SkillGorm) TableName() string {
	return "skills"
}

// SkillType represents the skill type for API requests/responses
type SkillType struct {
	ID         uint      `json:"id,omitempty" example:"1"`
	Name       string    `json:"name" binding:"required" example:"Carpentry"`
	CreatedAt  time.Time `json:"created_at,omitempty" example:"2024-01-15T10:30:00Z"`
	UpdatedAt  time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:00Z"`
	ClientID   int       `json:"client_id,omitempty" example:"1"`
	ClientName string    `json:"client_name,omitempty" example:"Acme Corp"`
	Skills     []Skill   `json:"skills,omitempty"`
}

// Skill represents the skill for API requests/responses
type Skill struct {
	ID            uint      `json:"id,omitempty" example:"1"`
	Name          string    `json:"name" binding:"required" example:"Formwork"`
	SkillTypeID   uint      `json:"skill_type_id" binding:"required" example:"1"`
	SkillTypeName string    `json:"skill_type_name,omitempty" example:"Carpentry"`
	CreatedAt     time.Time `json:"created_at,omitempty" example:"2024-01-15T10:30:00Z"`
	UpdatedAt     time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:00Z"`
	ClientID      int       `json:"client_id,omitempty" example:"1"`
}

// SkillTypeResponse represents the response for skill type operations
type SkillTypeResponse struct {
	Success bool       `json:"success" example:"true"`
	Message string     `json:"message" example:"Success"`
	Data    *SkillType `json:"data,omitempty"`
	Error   string     `json:"error,omitempty" example:""`
}

// SkillResponse represents the response for skill operations
type SkillResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"Success"`
	Data    *Skill `json:"data,omitempty"`
	Error   string `json:"error,omitempty" example:""`
}

// SkillTypeListResponse represents the response for skill type list operations
type SkillTypeListResponse struct {
	Success bool        `json:"success" example:"true"`
	Message string      `json:"message" example:"Success"`
	Data    []SkillType `json:"data,omitempty"`
	Error   string      `json:"error,omitempty" example:""`
}

// SkillListResponse represents the response for skill list operations
type SkillListResponse struct {
	Success bool    `json:"success" example:"true"`
	Message string  `json:"message" example:"Success"`
	Data    []Skill `json:"data,omitempty"`
	Error   string  `json:"error,omitempty" example:""`
}

// SkillTypeWithSkills represents the request for creating skill type with skills
type SkillTypeWithSkills struct {
	Name   string   `json:"name" binding:"required" example:"Carpentry"`
	Skills []string `json:"skills" binding:"required,min=1" example:"Formwork,Shuttering"`
}

// SkillTypeWithSkillsResponse represents the response for skill type with skills creation
type SkillTypeWithSkillsResponse struct {
	Success bool       `json:"success" example:"true"`
	Message string     `json:"message" example:"Skill type created"`
	Data    *SkillType `json:"data,omitempty"`
	Skills  []Skill    `json:"skills,omitempty"`
	Error   string     `json:"error,omitempty" example:""`
}

// SkillTypeUpdateWithSkills represents the request for updating skill type with skills
type SkillTypeUpdateWithSkills struct {
	Name     string   `json:"name" binding:"required" example:"Carpentry"`
	ClientID *int     `json:"client_id"`
	Skills   []string `json:"skills" binding:"required,min=1" example:"Formwork,Shuttering"`
}

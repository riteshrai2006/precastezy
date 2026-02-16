package models

import (
	"time"

	"gorm.io/gorm"
)

// PeopleGorm represents the people table with GORM tags
type PeopleGorm struct {
	ID           uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	Name         string         `gorm:"column:name;not null" json:"name" example:"Raj Kumar"`
	Email        string         `gorm:"column:email;not null;uniqueIndex" json:"email" example:"raj@example.com"`
	PhoneNo      *string        `gorm:"column:phone_no" json:"phone_no"`
	DepartmentID uint           `gorm:"column:department_id;not null" json:"department_id" example:"1"`
	CategoryID   uint           `gorm:"column:category_id;not null" json:"category_id" example:"1"`
	ProjectID    uint           `gorm:"column:project_id;not null" json:"project_id" example:"1"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for PeopleGorm
func (PeopleGorm) TableName() string {
	return "people"
}

// People represents the people for API requests/responses
type People struct {
	ID            uint      `json:"id,omitempty" example:"1"`
	Name          string    `json:"name" binding:"required" example:"Raj Kumar"`
	Email         string    `json:"email" binding:"required,email" example:"raj@example.com"`
	PhoneNo       string    `json:"phone_no,omitempty" example:"9876543210"`
	DepartmentID  uint      `json:"department_id" binding:"required" example:"1"`
	CategoryID    uint      `json:"category_id" binding:"required" example:"1"`
	ProjectID     uint      `json:"project_id" binding:"required" example:"1"`
	ProjectName   string    `json:"project_name,omitempty" example:"Project Alpha"`
	CreatedAt     time.Time `json:"created_at,omitempty" example:"2024-01-15T10:30:00Z"`
	UpdatedAt     time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:00Z"`
	PhoneCode     int       `json:"phone_code,omitempty" example:"91"`
	PhoneCodeName string    `json:"phone_code_name,omitempty" example:"+91"`
}

// PeopleResponse represents the response for people operations
type PeopleResponse struct {
	Success bool    `json:"success" example:"true"`
	Message string  `json:"message" example:"Success"`
	Data    *People `json:"data,omitempty"`
	Error   string  `json:"error,omitempty" example:""`
}

// PeopleListResponse represents the response for people list operations
type PeopleListResponse struct {
	Success bool     `json:"success" example:"true"`
	Message string   `json:"message" example:"Success"`
	Data    []People `json:"data,omitempty"`
	Error   string   `json:"error,omitempty" example:""`
}

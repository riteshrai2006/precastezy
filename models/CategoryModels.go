package models

import (
	"time"

	"gorm.io/gorm"
)

// CategoryGorm represents the categories table with GORM tags
type CategoryGorm struct {
	ID        uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	Name      string         `gorm:"column:name;not null" json:"name" example:"Skilled Worker"`
	ProjectID uint           `gorm:"column:project_id;not null" json:"project_id" example:"1"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for CategoryGorm
func (CategoryGorm) TableName() string {
	return "categories"
}

// Category represents the category for API requests/responses
type Category struct {
	ID          uint      `json:"id,omitempty" example:"1"`
	Name        string    `json:"name" binding:"required" example:"Skilled Worker"`
	ProjectID   uint      `json:"project_id" binding:"required" example:"1"`
	ProjectName string    `json:"project_name,omitempty" example:"Project Alpha"`
	CreatedAt   time.Time `json:"created_at,omitempty" example:"2024-01-15T10:30:00Z"`
	UpdatedAt   time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:00Z"`
}

// CategoryResponse represents the response for category operations
type CategoryResponse struct {
	Success bool      `json:"success" example:"true"`
	Message string    `json:"message" example:"Success"`
	Data    *Category `json:"data,omitempty"`
	Error   string    `json:"error,omitempty" example:""`
}

// CategoryListResponse represents the response for category list operations
type CategoryListResponse struct {
	Success bool       `json:"success" example:"true"`
	Message string     `json:"message" example:"Success"`
	Data    []Category `json:"data,omitempty"`
	Error   string     `json:"error,omitempty" example:""`
}

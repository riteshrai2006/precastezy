package models

import (
	"time"

	"gorm.io/gorm"
)

// DepartmentGorm represents the departments table with GORM tags
type DepartmentGorm struct {
	ID        uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	Name      string         `gorm:"column:name;not null;uniqueIndex" json:"name" example:"Production"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for DepartmentGorm
func (DepartmentGorm) TableName() string {
	return "departments"
}

// Department represents the department for API requests/responses
type Department struct {
	ID         uint      `json:"id,omitempty" example:"1"`
	Name       string    `json:"name" binding:"required" example:"Production"`
	CreatedAt  time.Time `json:"created_at,omitempty" example:"2024-01-15T10:30:00Z"`
	UpdatedAt  time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:00Z"`
	ClientID   *int      `json:"client_id,omitempty"`
	ClientName string    `json:"client_name,omitempty" example:"Acme Corp"`
}

// DepartmentResponse represents the response for department operations
type DepartmentResponse struct {
	Success bool        `json:"success" example:"true"`
	Message string      `json:"message" example:"Success"`
	Data    *Department `json:"data,omitempty"`
	Error   string      `json:"error,omitempty" example:""`
}

// DepartmentListResponse represents the response for department list operations
type DepartmentListResponse struct {
	Success bool         `json:"success" example:"true"`
	Message string       `json:"message" example:"Success"`
	Data    []Department `json:"data,omitempty"`
	Error   string       `json:"error,omitempty" example:""`
}

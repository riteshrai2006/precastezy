package models

import (
	"time"

	"gorm.io/gorm"
)

// ManpowerCountGorm represents the manpower_count table with GORM tags
type ManpowerCountGorm struct {
	ID          uint           `gorm:"primaryKey;column:id" json:"id" example:"1"`
	ProjectID   uint           `gorm:"column:project_id;not null" json:"project_id" example:"1"`
	TowerID     *uint          `gorm:"column:tower_id" json:"tower_id"`
	PeopleID    uint           `gorm:"column:people_id;not null" json:"people_id" example:"1"`
	SkillTypeID uint           `gorm:"column:skill_type_id;not null" json:"skill_type_id" example:"1"`
	SkillID     uint           `gorm:"column:skill_id;not null" json:"skill_id" example:"1"`
	Date        time.Time      `gorm:"column:date;not null" json:"date" example:"2024-01-15T00:00:00Z"`
	Count       int            `gorm:"column:count;not null;default:1" json:"count" example:"5"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null" json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;not null" json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for ManpowerCountGorm
func (ManpowerCountGorm) TableName() string {
	return "manpower_count"
}

// ManpowerCount represents the manpower count for API requests/responses
type ManpowerCount struct {
	ID          uint      `json:"id,omitempty" example:"1"`
	ProjectID   uint      `json:"project_id" binding:"required" example:"1"`
	TowerID     *uint     `json:"tower_id,omitempty"`
	PeopleID    uint      `json:"people_id" binding:"required" example:"1"`
	SkillTypeID uint      `json:"skill_type_id" binding:"required" example:"1"`
	SkillID     uint      `json:"skill_id" binding:"required" example:"1"`
	Date        time.Time `json:"date" binding:"required" example:"2024-01-15T00:00:00Z"`
	Count       int       `json:"count" binding:"required,min=1" example:"5"`
}

// SkillWithQuantity represents a skill with its specific quantity
type SkillWithQuantity struct {
	SkillID  uint `json:"skill_id" binding:"required"`
	Quantity int  `json:"quantity" binding:"required,min=1"`
}

// SkillWithQuantityResponse represents a skill with its specific quantity in response
type SkillWithQuantityResponse struct {
	SkillID  uint `json:"skill_id"`
	Quantity int  `json:"quantity"`
}

// SkillTypeWithCount represents a skill type with its skills and quantities
type SkillTypeWithCount struct {
	SkillTypeID uint                `json:"skill_type_id" binding:"required"`
	Count       []SkillWithQuantity `json:"count" binding:"required,min=1"`
}

// SkillTypeWithCountResponse represents a skill type with its skills and quantities in response
type SkillTypeWithCountResponse struct {
	SkillTypeID uint                        `json:"skill_type_id"`
	Count       []SkillWithQuantityResponse `json:"count"`
}

// PeopleWithSkillTypes represents a person with their skill types
type PeopleWithSkillTypes struct {
	PeopleID   uint                 `json:"people_id" binding:"required"`
	SkillTypes []SkillTypeWithCount `json:"skill_types" binding:"required,min=1"`
}

// PeopleWithSkillTypesResponse represents a person with their skill types in response
type PeopleWithSkillTypesResponse struct {
	PeopleID   uint                         `json:"people_id"`
	SkillTypes []SkillTypeWithCountResponse `json:"skill_types"`
}

// ManpowerCountBulkCreate represents the bulk manpower count creation request
type ManpowerCountBulkCreate struct {
	ProjectID uint                   `json:"project_id" binding:"required"`
	TowerID   *uint                  `json:"tower_id,omitempty"`
	Date      time.Time              `json:"date" binding:"required"`
	Skills    []PeopleWithSkillTypes `json:"skills" binding:"required,min=1"`
}

// ManpowerCountBulkData represents the grouped data structure for bulk response
type ManpowerCountBulkData struct {
	ProjectID uint                           `json:"project_id"`
	TowerID   *uint                          `json:"tower_id,omitempty"`
	Date      time.Time                      `json:"date"`
	Skills    []PeopleWithSkillTypesResponse `json:"skills"`
}

// ManpowerCountResponse represents the response for manpower count operations
type ManpowerCountResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    *ManpowerCount `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// ManpowerCountListResponse represents the response for manpower count list operations
type ManpowerCountListResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    []ManpowerCount `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ManpowerCountBulkResponse represents the response for bulk manpower count creation
type ManpowerCountBulkResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    *ManpowerCountBulkData `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// ManpowerCountWithDetails represents the manpower count with all related names
type ManpowerCountWithDetails struct {
	ID             uint      `json:"id,omitempty"`
	ProjectID      uint      `json:"project_id"`
	ProjectName    string    `json:"project_name"`
	TowerID        *uint     `json:"tower_id,omitempty"`
	TowerName      *string   `json:"tower_name,omitempty"`
	PeopleID       uint      `json:"people_id"`
	PeopleName     string    `json:"people_name"`
	DepartmentID   uint      `json:"department_id"`
	DepartmentName string    `json:"department_name"`
	CategoryID     uint      `json:"category_id"`
	CategoryName   string    `json:"category_name"`
	SkillTypeID    uint      `json:"skill_type_id"`
	SkillTypeName  string    `json:"skill_type_name"`
	SkillID        uint      `json:"skill_id"`
	SkillName      string    `json:"skill_name"`
	Date           time.Time `json:"date"`
	Count          int       `json:"count"`
}

// ManpowerCountWithDetailsListResponse represents the response for manpower count list with details
type ManpowerCountWithDetailsListResponse struct {
	Success bool                       `json:"success"`
	Message string                     `json:"message"`
	Data    []ManpowerCountWithDetails `json:"data,omitempty"`
	Error   string                     `json:"error,omitempty"`
}

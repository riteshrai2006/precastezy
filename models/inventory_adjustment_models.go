package models

import "time"

// ElementTypeWithBOM represents element type with BOM data for inventory adjustment
type ElementTypeWithBOM struct {
	ElementTypeCreatedBy  string                `json:"element_type_created_by" example:"admin"`
	ElementTypeID         int                   `json:"element_type_id" example:"1"`
	ElementTypeName       string                `json:"element_type_name" example:"Beam Type 1"`
	ElementTypeUpdatedAt  *time.Time            `json:"element_type_updated_at"`
	ElementTypeVersion    string                `json:"element_type_version" example:"v1"`
	ProjectID             int                   `json:"project_id" example:"1"`
	Elements              []ElementWithRevision `json:"elements"`
	BOMProduct            interface{}           `json:"bom_product,omitempty"`
	BOMRevisionProduct    interface{}           `json:"bom_revision_product,omitempty"`
	BOMRequiredAdjustment interface{}           `json:"bom_required_adjustment"`
}

// ElementWithRevision represents element with revision data
type ElementWithRevision struct {
	BOMRevisionID     *int       `json:"bom_revision_id"`
	DrawingRevisionID *int       `json:"drawing_revision_id"`
	ElementCode       *string    `json:"element_code"`
	ElementID         *int       `json:"element_id"`
	ElementUpdatedAt  *time.Time `json:"element_updated_at"`
}

// BOMProductItem represents individual BOM product item for inventory adjustment
type BOMProductItem struct {
	ProductID   int    `json:"product_id" example:"1"`
	ProductName string `json:"product_name" example:"Cement"`
	Quantity    int    `json:"quantity" example:"10"`
}

// InventoryAdjustmentRequest represents the request structure for inventory adjustment
type InventoryAdjustmentRequest struct {
	ProjectID      int    `json:"project_id" binding:"required" example:"1"`
	ElementTypeID  int    `json:"element_type_id" binding:"required" example:"1"`
	ElementID      int    `json:"element_id" binding:"required" example:"1"`
	AdjustmentType string `json:"adjustment_type" binding:"required" example:"add"`
	Quantity       int    `json:"quantity" binding:"required" example:"5"`
	Reason         string `json:"reason" example:"Initial stock"`
	Notes          string `json:"notes" example:""`
}

// InventoryAdjustmentLog represents inventory adjustment log record
type InventoryAdjustmentLog struct {
	ID            int       `json:"id" example:"1"`
	ElementTypeID int       `json:"element_type_id,omitempty" example:"1"`
	ProductID     int       `json:"product_id,omitempty" example:"1"`
	Quantity      float64   `json:"quantity" example:"5.0"`
	Reason        string    `json:"reason" example:"Stock correction"`
	AdjustedBy    string    `json:"adjusted_by" example:"admin"`
	AdjustedAt    time.Time `json:"adjusted_at" example:"2024-01-15T10:30:00Z"`
	ProjectID     int       `json:"project_id,omitempty" example:"1"`
	ElementCount  int       `json:"element_count,omitempty" example:"1"`
}

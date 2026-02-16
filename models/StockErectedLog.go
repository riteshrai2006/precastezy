package models

import "time"

// StockErectedLog represents a log entry for stock erected actions
type StockErectedLog struct {
	ID             int       `json:"id"`
	StockErectedID int       `json:"stock_erected_id"`
	ElementID      int       `json:"element_id"`
	Status         string    `json:"status"` // "Approved", "Rejected", or "Pending"
	ActedBy        int       `json:"acted_by"`
	ActedByName    string    `json:"acted_by_name"` // Combined first_name and last_name
	Comments       string    `json:"comments"`
	CreatedAt      time.Time `json:"Action_at"`
	// Additional fields for element details
	ElementTypeID   int     `json:"element_type_id"`
	ElementTypeName string  `json:"element_type_name"`
	ElementName     string  `json:"element_name"` // Element code/name from element table
	Thickness       float64 `json:"thickness"`    // From precast_stock dimensions (mm)
	Length          float64 `json:"length"`       // From precast_stock dimensions (mm)
	Weight          float64 `json:"weight"`       // From precast_stock (kg)
	TowerName       string  `json:"tower_name"`
	FloorName       string  `json:"floor_name"`
}

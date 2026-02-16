package models

type Unit struct {
	ID          int    `json:"id" example:"1"`
	UnitName    string `json:"unit_name" example:"Cum"`
	Description string `json:"description" example:"Cubic metre"`
}

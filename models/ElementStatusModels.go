package models

// ProjectStatus represents the overall status of elements in a project
type ProjectStatus struct {
	ProjectName           string                  `json:"projectName" example:"Project Alpha"`
	TotalElement          int                     `json:"totalElement" example:"100"`
	Production            int                     `json:"production" example:"20"`
	Stockyard             int                     `json:"stockyard" example:"30"`
	Erection              int                     `json:"erection" example:"40"`
	NotInProduction       int                     `json:"notInProduction" example:"10"`
	TotalConcreteRequired float64                 `json:"totalConcreteRequired" example:"500.5"`
	TotalConcreteUsed     float64                 `json:"totalConcreteUsed" example:"300.2"`
	TotalConcreteBalance  float64                 `json:"totalConcreteBalance" example:"200.3"`
	Towers                map[string]ElementTower `json:"towers"`
}

// ElementTower represents a tower in the project
type ElementTower struct {
	Floors map[string]ElementFloor `json:"floors"`
}

// ElementFloor represents a floor in a tower
type ElementFloor struct {
	ElementTypes map[string]ElementStatus `json:"elementTypes"`
}

// ElementStatus represents the status counts for an element type
type ElementStatus struct {
	TotalElement          int     `json:"totalElement" example:"10"`
	Production            int     `json:"production" example:"2"`
	Stockyard             int     `json:"stockyard" example:"3"`
	Erection              int     `json:"erection" example:"4"`
	NotInProduction       int     `json:"notInProduction" example:"1"`
	TotalConcreteRequired float64 `json:"totalConcreteRequired" example:"5.5"`
	TotalConcreteUsed     float64 `json:"totalConcreteUsed" example:"3.2"`
	TotalConcreteBalance  float64 `json:"totalConcreteBalance" example:"2.3"`
}

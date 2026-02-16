package models

type RectificationInput struct {
	ElementID int    `json:"element_id"`
	ProjectID int    `json:"project_id"`
	Status    string `json:"status"`
	Comments  string `json:"comments"`
	Image     string `json:"image"`
}

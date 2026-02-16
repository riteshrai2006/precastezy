package models

// AppTaskResponse represents a task in the app API response for "all" and "pending" filters
type AppTaskResponse struct {
	ID              int          `json:"id" example:"1"`
	TaskID          int          `json:"task_id" example:"1"`
	ProjectID       int          `json:"project_id" example:"1"`
	StageID         int          `json:"stage_id" example:"1"`
	StageName       string       `json:"stage_name" example:"Casting"`
	QCStatus        string       `json:"status" example:"pending"`
	ElementID       int          `json:"element_id" example:"1"`
	ElementName     string       `json:"element_name" example:"Beam 1"`
	ElementType     string       `json:"element_type" example:"B1"`
	ElementTypeName string       `json:"element_type_name" example:"Beam Type 1"`
	Tower           string       `json:"tower" example:"Tower A"`
	Floor           string       `json:"floor" example:"Floor 1"`
	PaperID         int          `json:"paper_id" example:"1"`
	FilterType      string       `json:"filter_type" example:"pending"`
	Drawings        []AppDrawing `json:"drawings,omitempty"`
}

// AppCompleteTaskResponse represents a completed task in the app API response for "complete" filter
type AppCompleteTaskResponse struct {
	ID              int          `json:"id" example:"1"`
	TaskID          int          `json:"task_id" example:"1"`
	ActivityID      int          `json:"activity_id" example:"1"`
	ProjectID       int          `json:"project_id" example:"1"`
	StageID         int          `json:"stage_id" example:"1"`
	StageName       string       `json:"stage_name" example:"Casting"`
	Status          string       `json:"status" example:"completed"`
	ElementID       int          `json:"element_id" example:"1"`
	ElementName     string       `json:"element_name" example:"Beam 1"`
	ElementType     string       `json:"element_type" example:"B1"`
	ElementTypeName string       `json:"element_type_name" example:"Beam Type 1"`
	ActivityName    string       `json:"activity_name" example:"Mesh placement"`
	FilterType      string       `json:"filter_type" example:"complete"`
	Drawings        []AppDrawing `json:"drawings,omitempty"`
}

// AppTaskListResponse represents the complete response for the task list API
type AppTaskListResponse struct {
	Tasks      interface{} `json:"tasks"`
	TotalCount int         `json:"total_count" example:"10"`
	Filter     string      `json:"filter" example:"pending"`
}

// AppDrawing represents one drawing entry for an element type
type AppDrawing struct {
	DrawingTypeName string `json:"drawing_type_name" example:"Plan"`
	DrawingFile     string `json:"drawing_file" example:"/files/drawing.pdf"`
	DrawingVersion  string `json:"drawing_version" example:"v1"`
}

// AppTaskCountResponse represents the task count by status response
type AppTaskCountResponse struct {
	Pending    int `json:"pending" example:"5"`
	Completed  int `json:"completed" example:"10"`
	InProgress int `json:"inprogress" example:"3"`
	Rejected   int `json:"rejected" example:"0"`
	Total      int `json:"total" example:"18"`
}

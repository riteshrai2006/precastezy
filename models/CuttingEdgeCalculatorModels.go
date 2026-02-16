package models

import "time"

// ---------- Data types ----------
type Request struct {
	OptimizationPriority string        `json:"optimization_priority"` // "least_wasted_area" supported
	CutThickness         float64       `json:"cut_thickness"`         // kerf thickness
	NumberOfCuts         int           `json:"number_of_cuts"`        // 0 => unlimited
	StockSheets          []StockSheet  `json:"stock_sheets"`
	Panels               []PanelDemand `json:"panels"`
	Mode                 string        `json:"mode"` // "2d" or "3d"
}

type StockSheet struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Qty    int     `json:"qty"`
	Name   string  `json:"name,omitempty"`
}

type PanelDemand struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Depth  float64 `json:"depth,omitempty"`
	Qty    int     `json:"qty"`
	ID     string  `json:"id,omitempty"`
}

type Response struct {
	GeneratedAt          time.Time         `json:"generated_at"`
	OptimizationPriority string            `json:"optimization_priority"`
	CutThickness         float64           `json:"cut_thickness"`
	TotalRequestedPanels int               `json:"total_requested_panels"`
	UsedStockSheets      []UsedStockSheet  `json:"used_stock_sheets"`
	TotalUsedArea        float64           `json:"total_used_area"`
	TotalStockArea       float64           `json:"total_stock_area"`
	UsedAreaPercent      float64           `json:"used_area_percent"`
	TotalWastedArea      float64           `json:"total_wasted_area"`
	WastedAreaPercent    float64           `json:"wasted_area_percent"`
	TotalCuts            int               `json:"total_cuts"`
	TotalCutLength       float64           `json:"total_cut_length"`
	TotalPanels          int               `json:"total_panels"`
	WastedPanels         int               `json:"wasted_panels"`
	UnableToFit          []UnableToFitItem `json:"unable_to_fit"`
	Cuts                 []CutLogEntry     `json:"cuts"`
	Suggestions          []Suggestion      `json:"suggestions"`

	// Visualization outputs for frontend:
	Placements []PlacementOut `json:"placements"`
	Leftovers  []LeftoverOut  `json:"leftovers"`
}

type UsedStockSheet struct {
	StockSheet   StockSheet `json:"stock_sheet"`
	UsedArea     float64    `json:"used_area"`
	WastedArea   float64    `json:"wasted_area"`
	UsedPct      float64    `json:"used_pct"`
	WastedPct    float64    `json:"wasted_pct"`
	Cuts         int        `json:"cuts"`
	CutLength    float64    `json:"cut_length"`
	Panels       int        `json:"panels"`
	WastedPanels int        `json:"wasted_panels"`
}

type UnableToFitItem struct {
	Panel  PanelDemand `json:"panel"`
	Qty    int         `json:"qty"`
	Reason string      `json:"reason"`
}

type CutLogEntry struct {
	Index   int     `json:"index"`
	StockID int     `json:"stock_id"`
	Panel   string  `json:"panel"`
	CutAxis string  `json:"cut_axis"`
	Result  string  `json:"result"`
	Length  float64 `json:"length"`
}

type Suggestion struct {
	LeftoverRect string        `json:"leftover_rect"`
	CanCut       []PanelCanCut `json:"can_cut"`
}

type PanelCanCut struct {
	PanelID string `json:"panel_id"`
	Qty     int    `json:"qty"`
}

// ---------- Internal packer structs ----------
type Rect struct {
	X, Y          float64
	Width, Height float64
}
type FreeRect = Rect
type PlacedRect = Rect

type Placement struct {
	Rect       PlacedRect
	Panel      PanelDemand
	StockIndex int
}

// Output shapes for frontend visualizer
type PlacementOut struct {
	ID      string  `json:"id"`
	PanelID string  `json:"panel_id"`
	StockID int     `json:"stock_id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Depth   float64 `json:"depth"`
}

type LeftoverOut struct {
	StockID int     `json:"stock_id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
}

package handlers

import (
	"backend/models"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------- Utility ----------
func area(w, h float64) float64 { return w * h }
func formatDim(w, h float64) string {
	return fmt.Sprintf("%.0f×%.0f", w, h)
}
func nearlyEqual(a, b float64) bool { return math.Abs(a-b) <= 1e-6 }
func round(x float64) float64       { return math.Round(x*100) / 100.0 }

func normalize1D(req *models.Request) {
	const oneDHeight = 1.0

	// Normalize stock sheets
	for i := range req.StockSheets {
		if req.StockSheets[i].Height <= 0 {
			req.StockSheets[i].Height = oneDHeight
		}
	}

	// Normalize panels
	for i := range req.Panels {
		if req.Panels[i].Height <= 0 {
			req.Panels[i].Height = oneDHeight
		}
	}
}

// ---------- Free Rect Merge ----------
func mergeFreeRects(rects []models.FreeRect) []models.FreeRect {
	merged := make([]models.FreeRect, 0, len(rects))
	used := make([]bool, len(rects))
	for i := range rects {
		if used[i] {
			continue
		}
		r := rects[i]
		for j := i + 1; j < len(rects); j++ {
			if used[j] {
				continue
			}
			r2 := rects[j]

			// vertical merge
			if nearlyEqual(r.X, r2.X) && nearlyEqual(r.Width, r2.Width) &&
				(nearlyEqual(r.Y+r.Height, r2.Y) || nearlyEqual(r2.Y+r2.Height, r.Y)) {

				minY := math.Min(r.Y, r2.Y)
				maxY := math.Max(r.Y+r.Height, r2.Y+r2.Height)
				r.Y = minY
				r.Height = maxY - minY
				used[j] = true

				// horizontal merge
			} else if nearlyEqual(r.Y, r2.Y) && nearlyEqual(r.Height, r2.Height) &&
				(nearlyEqual(r.X+r.Width, r2.X) || nearlyEqual(r2.X+r2.Width, r.X)) {

				minX := math.Min(r.X, r2.X)
				maxX := math.Max(r.X+r.Width, r2.X+r2.Width)
				r.X = minX
				r.Width = maxX - minX
				used[j] = true
			}
		}
		merged = append(merged, r)
		used[i] = true
	}
	return merged
}

// ---------- CORE PACKING FUNCTION ----------
func packSheet(
	sheet models.StockSheet,
	demands []models.PanelDemand,
	kerf float64,
	cutsLimit int,
	stockIndex int,
	cutIndex *int,
) (
	placements []models.Placement,
	cutLogs []models.CutLogEntry,
	freeRects []models.FreeRect,
	remaining []models.PanelDemand,
	cutsUsed int,
	cutLength float64,
) {

	freeRects = []models.FreeRect{{X: 0, Y: 0, Width: sheet.Width, Height: sheet.Height}}
	dems := append([]models.PanelDemand{}, demands...)

	// sort by area descending (place big panels first)
	sort.SliceStable(dems, func(i, j int) bool {
		return area(dems[i].Width, dems[i].Height) > area(dems[j].Width, dems[j].Height)
	})

	findPlacement := func() (int, int) {
		best := math.MaxFloat64
		pi, fi := -1, -1
		for i := range dems {
			if dems[i].Qty <= 0 {
				continue
			}
			for j, fr := range freeRects {
				if dems[i].Width <= fr.Width+1e-9 && dems[i].Height <= fr.Height+1e-9 {
					a := area(fr.Width, fr.Height)
					if a < best {
						best = a
						pi = i
						fi = j
					}
				}
			}
		}
		return pi, fi
	}

	for {
		pIdx, frIdx := findPlacement()
		if pIdx < 0 || frIdx < 0 {
			break
		}

		p := &dems[pIdx]
		fr := freeRects[frIdx]

		placed := models.PlacedRect{
			X:      fr.X,
			Y:      fr.Y,
			Width:  p.Width,
			Height: p.Height,
		}

		placements = append(placements, models.Placement{
			Rect:       placed,
			Panel:      *p,
			StockIndex: stockIndex,
		})

		p.Qty--

		// compute leftovers with kerf
		rightW := fr.Width - p.Width - kerf
		rightH := p.Height
		rightX := fr.X + p.Width + kerf
		rightY := fr.Y

		bottomW := fr.Width
		bottomH := fr.Height - p.Height - kerf
		bottomX := fr.X
		bottomY := fr.Y + p.Height + kerf

		// remove used free rect
		freeRects = append(freeRects[:frIdx], freeRects[frIdx+1:]...)

		if rightW > 1e-6 && rightH > 1e-6 {
			freeRects = append(freeRects, models.FreeRect{X: rightX, Y: rightY, Width: rightW, Height: rightH})
		}
		if bottomW > 1e-6 && bottomH > 1e-6 {
			freeRects = append(freeRects, models.FreeRect{X: bottomX, Y: bottomY, Width: bottomW, Height: bottomH})
		}

		freeRects = mergeFreeRects(freeRects)

		// create cut log entry
		cutAxis := "none"
		result := "- \\ -"
		length := 0.0

		if rightW > 1e-6 && bottomH > 1e-6 {
			cutAxis = "x+y"
			if cutsLimit == 0 || cutsUsed+2 <= cutsLimit {
				cutsUsed += 2
				length += p.Height + kerf
				length += p.Width + kerf
			} else if cutsLimit == 0 || cutsUsed+1 <= cutsLimit {
				cutsUsed++
				if p.Height >= p.Width {
					length += p.Height + kerf
					cutAxis = "x"
				} else {
					length += p.Width + kerf
					cutAxis = "y"
				}
			}
			result = fmt.Sprintf("%.0f×%.0f \\ surplus %.0f×%.0f", p.Width, p.Height, rightW, rightH)
		} else if rightW > 1e-6 {
			cutAxis = "x"
			if cutsLimit == 0 || cutsUsed+1 <= cutsLimit {
				cutsUsed++
				length += p.Height + kerf
			}
			result = fmt.Sprintf("%.0f×%.0f \\ surplus %.0f×%.0f", p.Width, p.Height, rightW, rightH)
		} else if bottomH > 1e-6 {
			cutAxis = "y"
			if cutsLimit == 0 || cutsUsed+1 <= cutsLimit {
				cutsUsed++
				length += p.Width + kerf
			}
			result = fmt.Sprintf("%.0f×%.0f \\ surplus %.0f×%.0f", p.Width, p.Height, bottomW, bottomH)
		}

		// increment safe
		(*cutIndex)++
		cutLogs = append(cutLogs, models.CutLogEntry{
			Index:   *cutIndex,
			StockID: stockIndex,
			Panel:   formatDim(p.Width, p.Height),
			CutAxis: cutAxis,
			Result:  result,
			Length:  round(length),
		})
		cutLength += length

		if cutsLimit > 0 && cutsUsed >= cutsLimit {
			break
		}
	}

	// remaining demands
	for _, d := range dems {
		if d.Qty > 0 {
			remaining = append(remaining, d)
		}
	}

	return
}

// generateSuggestionsFromRects inspects leftover rectangles and determines how many of each requested panel
// can be tiled into each rectangle (considering optional rotation).
func generateSuggestionsFromRects(req models.Request, rects []models.FreeRect) []models.Suggestion {
	suggestions := []models.Suggestion{}
	for _, fr := range rects {
		canCut := []models.PanelCanCut{}
		for _, p := range req.Panels {
			if p.Qty <= 0 {
				continue
			}
			// try no-rotation
			numX := int(math.Floor((fr.Width + 1e-9) / p.Width))
			numY := int(math.Floor((fr.Height + 1e-9) / p.Height))
			maxNoRot := numX * numY

			// try rotation (swap)
			numXr := int(math.Floor((fr.Width + 1e-9) / p.Height))
			numYr := int(math.Floor((fr.Height + 1e-9) / p.Width))
			maxRot := numXr * numYr

			best := maxNoRot
			if maxRot > best {
				best = maxRot
			}
			if best > 0 {
				canCut = append(canCut, models.PanelCanCut{PanelID: p.ID, Qty: best})
			}
		}
		leftRectStr := fmt.Sprintf("%.0f×%.0f@(%.2f,%.2f)", fr.Width, fr.Height, fr.X, fr.Y)
		suggestions = append(suggestions, models.Suggestion{LeftoverRect: leftRectStr, CanCut: canCut})
	}
	return suggestions
}

// ---------- SOLVER ----------
func solve(req models.Request) (models.Response, error) {

	resp := models.Response{
		GeneratedAt:          time.Now().UTC(),
		OptimizationPriority: req.OptimizationPriority,
		CutThickness:         req.CutThickness,
		Placements:           []models.PlacementOut{},
		Leftovers:            []models.LeftoverOut{},
	}

	// set default IDs for panels
	for i := range req.Panels {
		if strings.TrimSpace(req.Panels[i].ID) == "" {
			req.Panels[i].ID = formatDim(req.Panels[i].Width, req.Panels[i].Height)
		}
	}

	// total requested panels count
	totalReq := 0
	for _, p := range req.Panels {
		totalReq += p.Qty
	}
	resp.TotalRequestedPanels = totalReq

	// Expand stock sheets by qty
	allSheets := []models.StockSheet{}
	for _, s := range req.StockSheets {
		count := s.Qty
		if count <= 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			allSheets = append(allSheets, models.StockSheet{Width: s.Width, Height: s.Height, Qty: 1})
		}
	}

	// total stock area
	totalStockArea := 0.0
	for _, s := range allSheets {
		totalStockArea += area(s.Width, s.Height)
	}
	resp.TotalStockArea = round(totalStockArea)

	// pending panels
	pending := make([]models.PanelDemand, 0, len(req.Panels))
	for _, p := range req.Panels {
		if p.Qty > 0 {
			pending = append(pending, p)
		}
	}

	// accumulators
	usedSheets := []models.UsedStockSheet{}
	totalCuts := 0
	totalCutLength := 0.0
	totalPlacedPanels := 0
	cutsLimit := req.NumberOfCuts
	globalCutIndex := 0
	allLeftoverRects := []models.FreeRect{}

	for si, sheet := range allSheets {
		if len(pending) == 0 {
			break
		}
		placements, cutLogs, freeRects, remaining, cutsUsed, cutLen := packSheet(sheet, pending, req.CutThickness, cutsLimit, si+1, &globalCutIndex)
		resp.Cuts = append(resp.Cuts, cutLogs...)
		totalCuts += cutsUsed
		totalCutLength += cutLen

		// convert placements to response.Placements
		for i, pl := range placements {
			resp.Placements = append(resp.Placements, models.PlacementOut{
				ID:      fmt.Sprintf("%s#%d", pl.Panel.ID, i),
				PanelID: pl.Panel.ID,
				StockID: pl.StockIndex,
				X:       pl.Rect.X,
				Y:       pl.Rect.Y,
				Width:   pl.Rect.Width,
				Height:  pl.Rect.Height,
				Depth:   req.CutThickness,
			})
		}

		// convert free rects to leftovers
		for _, fr := range freeRects {
			resp.Leftovers = append(resp.Leftovers, models.LeftoverOut{
				StockID: si + 1,
				X:       fr.X,
				Y:       fr.Y,
				Width:   fr.Width,
				Height:  fr.Height,
			})
		}

		pending = remaining

		// per-sheet stats
		usedArea := 0.0
		for _, pl := range placements {
			usedArea += area(pl.Rect.Width, pl.Rect.Height)
			totalPlacedPanels++
		}
		wastedArea := area(sheet.Width, sheet.Height) - usedArea
		usedPct := 0.0
		if area(sheet.Width, sheet.Height) > 0 {
			usedPct = usedArea / area(sheet.Width, sheet.Height) * 100.0
		}
		usedSheets = append(usedSheets, models.UsedStockSheet{
			StockSheet:   sheet,
			UsedArea:     round(usedArea),
			WastedArea:   round(wastedArea),
			UsedPct:      round(usedPct),
			WastedPct:    round(100.0 - usedPct),
			Cuts:         cutsUsed,
			CutLength:    round(cutLen),
			Panels:       len(placements),
			WastedPanels: 0,
		})

		allLeftoverRects = append(allLeftoverRects, freeRects...)

		if cutsLimit > 0 {
			usedCutsSoFar := 0
			for _, us := range usedSheets {
				usedCutsSoFar += us.Cuts
			}
			if usedCutsSoFar >= cutsLimit {
				break
			}
		}
	}

	// unable to fit
	unableToFit := []models.UnableToFitItem{}
	for _, p := range pending {
		if p.Qty > 0 {
			unableToFit = append(unableToFit, models.UnableToFitItem{Panel: p, Qty: p.Qty, Reason: "no remaining free area in provided stock sheets"})
		}
	}

	resp.UsedStockSheets = usedSheets
	resp.UnableToFit = unableToFit

	// totals
	totalUsedArea := 0.0
	for _, us := range usedSheets {
		totalUsedArea += us.UsedArea
	}
	resp.TotalUsedArea = round(totalUsedArea)
	resp.TotalWastedArea = round(resp.TotalStockArea - resp.TotalUsedArea)
	resp.UsedAreaPercent = 0.0
	if resp.TotalStockArea > 0 {
		resp.UsedAreaPercent = round(resp.TotalUsedArea / resp.TotalStockArea * 100.0)
	}
	resp.WastedAreaPercent = round(100.0 - resp.UsedAreaPercent)
	resp.TotalCuts = totalCuts
	resp.TotalCutLength = round(totalCutLength)
	resp.TotalPanels = totalPlacedPanels

	// approximate wasted panels as sum of unable-to-fit quantities
	wp := 0
	for _, u := range unableToFit {
		wp += u.Qty
	}
	resp.WastedPanels = wp

	// suggestions based on leftover rects
	resp.Suggestions = generateSuggestionsFromRects(req, allLeftoverRects)

	return resp, nil
}

// SolveHandler godoc
// @Summary      Solve cutting edge calculator
// @Tags         calculator
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "Request (StockSheets, Panels, CutThickness, etc.)"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/solve [post]
func SolveHandler(c *gin.Context) {
	var req models.Request

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Basic validation
	if len(req.StockSheets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stock_sheets must be provided (qty >= 1)"})
		return
	}
	if len(req.Panels) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "panels must be provided"})
		return
	}
	if req.CutThickness < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cut_thickness must be >= 0"})
		return
	}

	normalize1D(&req)

	resp, err := solve(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "solver error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

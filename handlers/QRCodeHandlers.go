package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
)

// addLabel adds text to an image at the specified position with larger font
func addLabel(img *image.RGBA, x, y int, label string, fontSize float64) {
	col := color.RGBA{0, 0, 0, 255}

	// Use inconsolata font which is larger and more readable
	face := inconsolata.Regular8x16
	if fontSize > 16 {
		// Scale the font for larger sizes
		face = inconsolata.Bold8x16
	}

	point := fixed.Point26_6{
		X: fixed.Int26_6(x * 64),
		Y: fixed.Int26_6(y * 64),
	}

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  point,
	}
	d.DrawString(label)
}

// addLabelBold adds bold text with larger font for labels
func addLabelBold(img *image.RGBA, x, y int, label string) {
	col := color.RGBA{30, 30, 30, 255} // Darker color for labels
	face := inconsolata.Bold8x16

	point := fixed.Point26_6{
		X: fixed.Int26_6(x * 64),
		Y: fixed.Int26_6(y * 64),
	}

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  point,
	}
	d.DrawString(label)
}

// GenerateQRCodeJPEG godoc
// @Summary      Generate QR code as JPEG
// @Tags         qr
// @Param        id   path      int  true  "Element ID"
// @Success      200  {file}    file  "JPEG image"
// @Failure      400  {object}  object
// @Router       /api/generate-qr/{id} [get]
func GenerateQRCodeJPEG(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		elementID := c.Param("id")
		if elementID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element ID is required"})
			return
		}

		id, err := strconv.Atoi(elementID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
			return
		}

		// Fetch element details with project name, tower, floor, casting date, and instage in a single query
		// Using JOINs similar to other handlers (PrecastStockHandlers, Kanban, etc.)
		// Reference: ElementStatusHandlers.go uses instage = true to check if element is in production
		var projectID int
		var projectName sql.NullString
		var towerName sql.NullString
		var floorName sql.NullString
		var castingDate sql.NullTime
		var instage bool

		err = db.QueryRow(`
			SELECT 
				e.project_id,
				COALESCE(p.name, 'Unknown Project') AS project_name,
				COALESCE(tower_precast.name, 'N/A') AS tower_name,
				CASE 
					WHEN floor_precast.parent_id IS NULL THEN 'common'
					ELSE COALESCE(NULLIF(floor_precast.name, ''), 'common')
				END AS floor_name,
				ps.production_date,
				COALESCE(e.instage, false) AS instage
			FROM element e
			LEFT JOIN project p ON e.project_id = p.project_id
			LEFT JOIN precast floor_precast ON e.target_location = floor_precast.id
			LEFT JOIN precast tower_precast ON floor_precast.parent_id = tower_precast.id
			LEFT JOIN precast_stock ps ON e.id = ps.element_id
			WHERE e.id = $1
			ORDER BY ps.production_date DESC NULLS LAST
			LIMIT 1
		`, id).Scan(&projectID, &projectName, &towerName, &floorName, &castingDate, &instage)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
				return
			}
			log.Printf("Error fetching element details: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element details"})
			return
		}

		// Set default values if null
		projectNameStr := "Unknown Project"
		if projectName.Valid {
			projectNameStr = projectName.String
		}

		towerNameStr := "N/A"
		if towerName.Valid {
			towerNameStr = towerName.String
		}

		floorNameStr := "N/A"
		if floorName.Valid {
			floorNameStr = floorName.String
		}

		// Check if element is in production using the same logic as other handlers
		// Reference: ElementHandlers.go getElementStatus() checks activity table
		// Reference: ElementStatusHandlers.go uses instage = true for production status
		// An element is in production if:
		// 1. instage = true (from element table), OR
		// 2. exists in activity table
		var paperID sql.NullInt64
		var completed sql.NullBool
		var isInProduction bool = false

		// First check: Use instage field (as used in ElementStatusHandlers.go)
		if instage {
			isInProduction = true
		}

		// Second check: Verify with activity table (as used in ElementHandlers.go getElementStatus)
		err = db.QueryRow(`
			SELECT paper_id, completed 
			FROM activity 
			WHERE element_id = $1 
			LIMIT 1`, id).Scan(&paperID, &completed)

		if err != nil {
			if err == sql.ErrNoRows {
				// Element not found in activity table
				// If instage was false, element is definitely not in production
				if !instage {
					isInProduction = false
					paperID.Valid = false
					completed.Valid = false
				}
				// If instage was true but no activity record, still consider it in production
				// but without paper_id
			} else {
				log.Printf("Error fetching paper_id from activity table: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch paper ID"})
				return
			}
		} else {
			// Element exists in activity table - definitely in production
			isInProduction = true
		}

		// Determine if QR code is valid
		// QR code is valid if element exists (already verified above)
		isValid := true

		// Build QR code data with id, paper_id, and verification boolean
		qrData := struct {
			ID      int  `json:"id"`
			PaperID *int `json:"paper_id,omitempty"`
			IsValid bool `json:"is_valid"`
		}{
			ID:      id,
			IsValid: isValid,
		}

		// Include paper_id ONLY if:
		// 1. Element is in production (isInProduction = true)
		// 2. paper_id is valid
		// 3. Production is not completed (completed = false or not set)
		if isInProduction && paperID.Valid && (!completed.Valid || !completed.Bool) {
			paperIDValue := int(paperID.Int64)
			qrData.PaperID = &paperIDValue
		}
		// If element is not in production, paper_id will not be included (omitempty will handle it)

		jsonData, err := json.Marshal(qrData)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal element data"})
			return
		}

		qr, err := qrcode.New(string(jsonData), qrcode.Medium)
		if err != nil {
			c.String(http.StatusInternalServerError, "QR code generation failed")
			return
		}

		qrImg := qr.Image(512)

		// Calculate dimensions for the combined image
		qrSize := qrImg.Bounds().Dy()
		padding := 30
		lineHeight := 28                         // Increased line height for larger font
		textAreaHeight := 5*lineHeight + padding // Space for 5 lines of text with extra padding
		totalHeight := qrSize + padding + textAreaHeight

		// Create a new RGBA image with white background
		combinedImg := image.NewRGBA(image.Rect(0, 0, qrSize, totalHeight))
		draw.Draw(combinedImg, combinedImg.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

		// Draw QR code at the top
		qrRect := image.Rect(0, 0, qrSize, qrSize)
		draw.Draw(combinedImg, qrRect, qrImg, image.Point{}, draw.Src)

		// Draw a subtle separator line between QR code and text
		separatorY := qrSize + padding/2
		for x := 0; x < qrSize; x++ {
			combinedImg.Set(x, separatorY, color.RGBA{200, 200, 200, 255})
		}

		// Prepare text labels with better formatting
		startY := qrSize + padding + lineHeight
		xPos := 20 // Increased left margin

		// Format casting date
		castingDateStr := "N/A"
		if castingDate.Valid {
			castingDateStr = castingDate.Time.Format("2006-01-02")
		}

		// Add text labels at the bottom with better formatting
		// Using bold labels and regular values for better readability
		addLabelBold(combinedImg, xPos, startY, "Element ID:")
		addLabel(combinedImg, xPos+120, startY, strconv.Itoa(id), 16)

		addLabelBold(combinedImg, xPos, startY+lineHeight, "Project:")
		// Truncate long project names
		projectDisplay := projectNameStr
		if len(projectDisplay) > 30 {
			projectDisplay = projectDisplay[:27] + "..."
		}
		addLabel(combinedImg, xPos+120, startY+lineHeight, projectDisplay, 16)

		addLabelBold(combinedImg, xPos, startY+2*lineHeight, "Tower:")
		towerDisplay := towerNameStr
		if len(towerDisplay) > 25 {
			towerDisplay = towerDisplay[:22] + "..."
		}
		addLabel(combinedImg, xPos+120, startY+2*lineHeight, towerDisplay, 16)

		addLabelBold(combinedImg, xPos, startY+3*lineHeight, "Floor:")
		floorDisplay := floorNameStr
		if len(floorDisplay) > 25 {
			floorDisplay = floorDisplay[:22] + "..."
		}
		addLabel(combinedImg, xPos+120, startY+3*lineHeight, floorDisplay, 16)

		addLabelBold(combinedImg, xPos, startY+4*lineHeight, "Casting Date:")
		addLabel(combinedImg, xPos+120, startY+4*lineHeight, castingDateStr, 16)

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, combinedImg, nil); err != nil {
			c.String(http.StatusInternalServerError, "JPEG encoding failed")
			return
		}

		c.Data(http.StatusOK, "image/jpeg", buf.Bytes())
	}
}

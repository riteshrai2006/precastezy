package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/models"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
)

// generateQRCodeImage generates a QR code image for the given data
func generateQRCodeImage(data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	qr, err := qrcode.New(string(jsonData), qrcode.Medium)
	if err != nil {
		return nil, err
	}

	img := qr.Image(200) // 200x200 pixels

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GenerateElementTypesPDFSummary generates a PDF summary of element types by project
// @Summary Generate PDF summary of element types by project
// @Description Generate a PDF report containing element types data for a specific project
// @Tags PDF
// @Accept json
// @Produce application/pdf
// @Param project_id path int true "Project ID"
// @Param search query string false "Search term"
// @Param hierarchy_id query int false "Hierarchy ID filter"
// @Param element_type query string false "Element type filter"
// @Param element_type_name query string false "Element type name filter"
// @Param stage query []string false "Stage filters (production, stockyard, dispatch, erection, request)"
// @Success 200 {file} file "PDF file"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/elementtype_pdf_summary/{project_id} [get]
func GenerateElementTypesPDFSummary(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Get project ID from path parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}

		// Get query parameters for filtering
		searchTerm := c.Query("search")
		hierarchyID := c.Query("hierarchy_id")
		elementType := c.Query("element_type")
		elementTypeName := c.Query("element_type_name")
		stages := c.QueryArray("stage")

		// Fetch project name for PDF header
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project details"})
			return
		}

		// Build WHERE conditions
		var conditions []string
		var args []interface{}
		argIndex := 1

		// Base condition for project_id
		conditions = append(conditions, fmt.Sprintf("et.project_id = $%d", argIndex))
		args = append(args, projectID)
		argIndex++

		// Add search condition if provided
		if searchTerm != "" {
			conditions = append(conditions, fmt.Sprintf("(et.element_type ILIKE $%d OR et.element_type_name ILIKE $%d)", argIndex, argIndex))
			args = append(args, "%"+searchTerm+"%")
			argIndex++
		}

		// Add element type filter
		if elementType != "" {
			conditions = append(conditions, fmt.Sprintf("et.element_type ILIKE $%d", argIndex))
			args = append(args, "%"+elementType+"%")
			argIndex++
		}

		// Add element type name filter
		if elementTypeName != "" {
			conditions = append(conditions, fmt.Sprintf("et.element_type_name ILIKE $%d", argIndex))
			args = append(args, "%"+elementTypeName+"%")
			argIndex++
		}

		// Add hierarchy ID filter
		var hierarchyIDInt int
		if hierarchyID != "" {
			hierarchyIDInt, err = strconv.Atoi(hierarchyID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hierarchy_id"})
				return
			}
		}

		// Build WHERE clause
		whereClause := ""
		if len(conditions) > 0 {
			whereClause = "WHERE " + strings.Join(conditions, " AND ")
		}

		// Main query to fetch element types data
		query := `
		WITH element_counts AS (
			SELECT 
				e.element_type_id,
				e.target_location AS hierarchy_id,
				COUNT(DISTINCT e.id) AS total_elements,
				COUNT(DISTINCT 
					CASE 
						WHEN cp.element_id IS NOT NULL 
							 AND cp.status IS NULL 
						THEN cp.element_id 
						ELSE NULL
					END
				) AS production_count,
				COUNT(DISTINCT 
					CASE 
						WHEN ps.element_id IS NOT NULL 
							 AND ps.stockyard = FALSE 
						THEN ps.element_id 
						ELSE NULL
					END
				) AS stockyard_count,
				COUNT(DISTINCT 
					CASE 
						WHEN ps.element_id IS NOT NULL 
							 AND ps.dispatch_status = FALSE 
						THEN ps.element_id 
						ELSE NULL
					END
				) AS dispatch_count,
				COUNT(DISTINCT 
					CASE 
						WHEN ps.element_id IS NOT NULL 
							 AND ps.erected = FALSE 
						THEN ps.element_id 
						ELSE NULL
					END
				) AS erection_count,
				COUNT(DISTINCT 
					CASE 
						WHEN ps.element_id IS NOT NULL
							 AND ps.dispatch_status = FALSE 
							 AND ps.order_by_erection = TRUE 
							 AND ps.erected = FALSE 
						THEN ps.element_id 
						ELSE NULL
					END
				) AS in_request_count
			FROM element e
			LEFT JOIN complete_production cp 
				ON cp.element_id = e.id 
			   AND cp.status IS NULL
			LEFT JOIN precast_stock ps 
				ON ps.element_id = e.id
			WHERE e.project_id = $1
			GROUP BY e.element_type_id, e.target_location
		)
		SELECT 
			et.element_type_id,
			et.element_type,
			et.element_type_name,
			et.thickness,
			et.length,
			et.height,
			et.volume, 
			et.mass, 
			et.area, 
			et.width,
			et.project_id,
			et.element_type_version,
			ethq.quantity,
			ethq.hierarchy_id,
			COALESCE(p.name, '') AS floor_name,
			COALESCE(tower.name, '') AS tower_name,
			COALESCE(ethq.naming_convention, '') AS naming_convention,
			COALESCE(ec.total_elements, 0) AS total_elements,
			COALESCE(ec.production_count, 0) AS production_count,
			COALESCE(ec.stockyard_count, 0) AS stockyard_count,
			COALESCE(ec.dispatch_count, 0) AS dispatch_count,
			COALESCE(ec.erection_count, 0) AS erection_count,
			COALESCE(ec.in_request_count, 0) AS in_request_count
		FROM element_type et
		JOIN element_type_hierarchy_quantity ethq 
			ON et.element_type_id = ethq.element_type_id
		LEFT JOIN precast p 
			ON ethq.hierarchy_id = p.id
		LEFT JOIN precast tower 
			ON p.parent_id = tower.id
		LEFT JOIN element_counts ec 
			ON ec.element_type_id = et.element_type_id 
		   AND ec.hierarchy_id = ethq.hierarchy_id
		WHERE et.project_id = $1`

		// Add WHERE conditions to main query
		if whereClause != "" {
			filterClause := strings.TrimPrefix(whereClause, "WHERE ")
			query += " AND " + filterClause
		}

		// Add hierarchy ID filter
		if hierarchyID != "" {
			query += fmt.Sprintf(" AND ethq.hierarchy_id = %d", hierarchyIDInt)
		}

		query += " ORDER BY et.element_type_id, ethq.hierarchy_id"

		// Execute query
		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Query execution failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		var results []models.ElementTypeWithHierarchyResponse

		for rows.Next() {
			var r models.ElementTypeWithHierarchyResponse
			var totalElements int

			err := rows.Scan(
				&r.ElementTypeID,
				&r.ElementType,
				&r.ElementTypeName,
				&r.Thickness,
				&r.Length,
				&r.Height,
				&r.Volume,
				&r.Mass,
				&r.Area,
				&r.Width,
				&r.ProjectID,
				&r.ElementTypeVersion,
				&r.Quantity,
				&r.HierarchyID,
				&r.FloorName,
				&r.TowerName,
				&r.NamingConvention,
				&totalElements,
				&r.ProductionCount,
				&r.StockyardCount,
				&r.DispatchCount,
				&r.ErectionCount,
				&r.InRequestCount,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			// Apply stage filtering if specified
			if len(stages) > 0 {
				stageMatch := false
				for _, stage := range stages {
					switch stage {
					case "production":
						if r.ProductionCount > 0 {
							stageMatch = true
						}
					case "stockyard":
						if r.StockyardCount > 0 {
							stageMatch = true
						}
					case "dispatch":
						if r.DispatchCount > 0 {
							stageMatch = true
						}
					case "erection":
						if r.ErectionCount > 0 {
							stageMatch = true
						}
					case "request":
						if r.InRequestCount > 0 {
							stageMatch = true
						}
					}
				}
				if !stageMatch {
					continue
				}
			}

			results = append(results, r)
		}

		// Check for any errors encountered during iteration
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error during rows iteration",
				"details": err.Error(),
			})
			return
		}

		// Generate PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Set font for header with styling
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 18)
		pdf.CellFormat(190, 12, "Element Types Summary Report", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Project information
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(190, 8, fmt.Sprintf("Project: %s (ID: %d)", projectName, projectID))
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(190, 6, fmt.Sprintf("Generated by: %s", userName))
		pdf.Ln(4)
		pdf.Cell(190, 6, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))
		pdf.Ln(10)

		// Summary statistics with styled heading
		pdf.SetFillColor(240, 240, 240) // Light gray background
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 10, "Summary Statistics", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.Ln(5)
		// Calculate totals
		var totalProduction, totalStockyard, totalDispatch, totalErection, totalRequest int
		for _, item := range results {
			totalProduction += item.ProductionCount
			totalStockyard += item.StockyardCount
			totalDispatch += item.DispatchCount
			totalErection += item.ErectionCount
			totalRequest += item.InRequestCount
		}

		// Summary statistics table - 4 columns (Stages, Count, Stages, Count), 3 rows
		pdf.SetFillColor(200, 220, 240) // Light blue/gray background for header
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(47.5, 8, "Stages", "1", 0, "C", true, 0, "")
		pdf.CellFormat(47.5, 8, "Count", "1", 0, "C", true, 0, "")
		pdf.CellFormat(47.5, 8, "Stages", "1", 0, "C", true, 0, "")
		pdf.CellFormat(47.5, 8, "Count", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black

		// Row 1: Total Element Types (8), Total in Dispatch (0)
		pdf.SetFont("Arial", "", 9)
		pdf.CellFormat(47.5, 8, "Total Element Types", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", len(results)), "1", 0, "C", false, 0, "")
		pdf.CellFormat(47.5, 8, "Total in Dispatch", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", totalDispatch), "1", 1, "C", false, 0, "")

		// Row 2: Total in Production (80), Total in Erection (1)
		pdf.CellFormat(47.5, 8, "Total in Production", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", totalProduction), "1", 0, "C", false, 0, "")
		pdf.CellFormat(47.5, 8, "Total in Erection", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", totalErection), "1", 1, "C", false, 0, "")

		// Row 3: Total in Stockyard (0), Total in Request (0)
		pdf.CellFormat(47.5, 8, "Total in Stockyard", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", totalStockyard), "1", 0, "C", false, 0, "")
		pdf.CellFormat(47.5, 8, "Total in Request", "1", 0, "L", false, 0, "")
		pdf.CellFormat(47.5, 8, fmt.Sprintf("%d", totalRequest), "1", 1, "C", false, 0, "")

		pdf.Ln(10)

		// Element types table with black section heading
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 10, "Element Types Details", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(5)

		// Table header with styling
		pdf.SetFillColor(50, 50, 50)    // Dark gray background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(20, 8, "Type ID", "1", 0, "C", true, 0, "")
		pdf.CellFormat(25, 8, "Element Type", "1", 0, "C", true, 0, "")
		pdf.CellFormat(35, 8, "Element Name", "1", 0, "C", true, 0, "")
		pdf.CellFormat(20, 8, "Floor", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Qty", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Prod", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Stock", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Disp", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Erect", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Req", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black

		// Table data
		pdf.SetFont("Arial", "", 8)
		for _, item := range results {
			// Check if we need a new page
			if pdf.GetY() > 250 {
				pdf.AddPage()
				// Repeat header with styling
				pdf.SetFillColor(50, 50, 50)    // Dark gray background
				pdf.SetTextColor(255, 255, 255) // White text
				pdf.SetFont("Arial", "B", 9)
				pdf.CellFormat(20, 8, "Type ID", "1", 0, "C", true, 0, "")
				pdf.CellFormat(25, 8, "Element Type", "1", 0, "C", true, 0, "")
				pdf.CellFormat(35, 8, "Element Name", "1", 0, "C", true, 0, "")
				pdf.CellFormat(20, 8, "Floor", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Qty", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Prod", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Stock", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Disp", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Erect", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Req", "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255) // Reset to white
				pdf.SetTextColor(0, 0, 0)       // Reset to black
				pdf.SetFont("Arial", "", 8)
			}

			// Truncate long strings for table display
			elementType := item.ElementType
			if len(elementType) > 20 {
				elementType = elementType[:17] + "..."
			}
			elementName := item.ElementTypeName
			if len(elementName) > 30 {
				elementName = elementName[:27] + "..."
			}
			floorName := item.FloorName
			if len(floorName) > 15 {
				floorName = floorName[:12] + "..."
			}

			pdf.CellFormat(20, 6, fmt.Sprintf("%d", item.ElementTypeID), "1", 0, "C", false, 0, "")
			pdf.CellFormat(25, 6, elementType, "1", 0, "L", false, 0, "")
			pdf.CellFormat(35, 6, elementName, "1", 0, "L", false, 0, "")
			pdf.CellFormat(20, 6, floorName, "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.Quantity), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.ProductionCount), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.StockyardCount), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.DispatchCount), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.ErectionCount), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%d", item.InRequestCount), "1", 1, "C", false, 0, "")
		}

		// Add detailed specifications section with black heading
		pdf.Ln(10)
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 10, "Element Specifications", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(5)

		pdf.SetFillColor(50, 50, 50)    // Dark gray background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(25, 8, "Element Type", "1", 0, "C", true, 0, "")
		pdf.CellFormat(40, 8, "Element Name", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Length", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Width", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Height", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Thickness", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Volume", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Mass", "1", 0, "C", true, 0, "")
		pdf.CellFormat(15, 8, "Area", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black

		pdf.SetFont("Arial", "", 8)
		for _, item := range results {
			if pdf.GetY() > 250 {
				pdf.AddPage()
				// Repeat header with styling
				pdf.SetFillColor(50, 50, 50)    // Dark gray background
				pdf.SetTextColor(255, 255, 255) // White text
				pdf.SetFont("Arial", "B", 9)
				pdf.CellFormat(25, 8, "Element Type", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 8, "Element Name", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Length", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Width", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Height", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Thickness", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Volume", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Mass", "1", 0, "C", true, 0, "")
				pdf.CellFormat(15, 8, "Area", "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255) // Reset to white
				pdf.SetTextColor(0, 0, 0)       // Reset to black
				pdf.SetFont("Arial", "", 8)
			}

			elementType := item.ElementType
			if len(elementType) > 20 {
				elementType = elementType[:17] + "..."
			}
			elementName := item.ElementTypeName
			if len(elementName) > 35 {
				elementName = elementName[:32] + "..."
			}

			pdf.CellFormat(25, 6, elementType, "1", 0, "L", false, 0, "")
			pdf.CellFormat(40, 6, elementName, "1", 0, "L", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Length), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Width), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Height), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Thickness), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Volume), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Mass), "1", 0, "C", false, 0, "")
			pdf.CellFormat(15, 6, fmt.Sprintf("%.2f", item.Area), "1", 1, "C", false, 0, "")
		}

		// Footer
		pdf.SetY(-20)
		pdf.SetFont("Arial", "I", 8)
		pdf.Cell(190, 6, "This is a computer-generated report. Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

		// Output PDF
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=element_types_summary_%s_%d.pdf", projectName, projectID))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}

		// Log activity
		activityLog := models.ActivityLog{
			EventContext: "PDF Generation",
			EventName:    "Element Types Summary",
			Description:  "Generated PDF summary for element types",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			log.Printf("Failed to log PDF generation activity: %v", logErr)
		}
	}
}

// GenerateElementDetailsPDF generates a PDF report for specific element details
// @Summary Generate PDF report for element details by type and location
// @Description Generate a comprehensive PDF report containing detailed element information
// @Tags PDF
// @Accept json
// @Produce application/pdf
// @Param element_type_id body int true "Element Type ID"
// @Param target_location body int false "Target Location"
// @Param project_id body int true "Project ID"
// @Success 200 {file} file "PDF file"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/element_details_pdf [get]
func GenerateElementDetailsPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Define the request structure
		var req struct {
			ElementTypeID  int `json:"element_type_id" binding:"required"`
			TargetLocation int `json:"target_location"`
			ProjectID      int `json:"project_id" binding:"required"`
		}

		// Bind JSON input
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid input",
				"details": err.Error(),
			})
			return
		}

		// Fetch project name
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project details"})
			return
		}

		// Initialize element with empty arrays
		element := models.ElementTypeInterface{
			DrawingType: []models.DrawingType{},
			BOM:         []models.BOMItem{},
			Stages:      []models.StageDetails{},
		}

		// Step 1: Get base element_type data
		var countQuery string
		var countArgs []interface{}

		if req.TargetLocation > 0 {
			countQuery = `(SELECT COUNT(*) FROM element WHERE element_type_id = $1 AND target_location = $2) as total_quantity`
			countArgs = []interface{}{req.ElementTypeID, req.TargetLocation}
		} else {
			countQuery = `(SELECT COUNT(*) FROM element WHERE element_type_id = $1) as total_quantity`
			countArgs = []interface{}{req.ElementTypeID}
		}

		query := fmt.Sprintf(`
			SELECT 
				et.element_type_id,
				et.element_type,
				et.element_type_version,
				et.thickness,
				et.length,
				et.height,
				et.volume, et.mass, et.area, et.width,
				%s
			FROM element_type et
			WHERE et.element_type_id = $1`, countQuery)

		var volume, mass, area, width float64
		err = db.QueryRow(query, countArgs...).Scan(
			&element.ID,
			&element.ElementType,
			&element.ElementTypeVersion,
			&element.Thickness,
			&element.Length,
			&element.Height,
			&volume, &mass, &area, &width,
			&element.TotalQuantity,
		)
		element.Volume = volume
		element.Mass = mass
		element.Area = area
		element.Width = width
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "Element type not found",
					"details": fmt.Sprintf("No element type found with ID %d", req.ElementTypeID),
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database error",
				"details": err.Error(),
			})
			return
		}

		// Step 2: Fetch Drawings & Revisions
		drawingRows, err := db.Query(`
            SELECT 
                d.drawing_id,
                d.current_version,
                d.file,
                dt.drawing_type_name,
				d.created_at,
				d.update_at
            FROM drawings d
            JOIN drawing_type dt ON d.drawing_type_id = dt.drawing_type_id
            WHERE d.element_type_id = $1`, req.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch drawings",
				"details": err.Error(),
			})
			return
		}
		defer drawingRows.Close()

		for drawingRows.Next() {
			var drawing models.DrawingType
			var createdAt, updatedAt time.Time
			var drawingID int
			var currentVersion, file, drawingTypeName string
			err := drawingRows.Scan(
				&drawingID,
				&currentVersion,
				&file,
				&drawingTypeName,
				&createdAt,
				&updatedAt,
			)
			if err != nil {
				log.Printf("Error scanning drawing: %v", err)
				continue
			}
			drawing.Name = drawingTypeName
			drawing.Version = currentVersion
			drawing.FilePath = file
			drawing.CreatedAt = createdAt
			drawing.UpdatedAt = updatedAt
			element.DrawingType = append(element.DrawingType, drawing)
		}

		// Step 3: Fetch BOM Items
		bomRows, err := db.Query(`
            SELECT 
                etb.product_id,
                etb.product_name,
                etb.quantity
            FROM element_type_bom etb
            WHERE etb.element_type_id = $1`, req.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch BOM",
				"details": err.Error(),
			})
			return
		}
		defer bomRows.Close()

		for bomRows.Next() {
			var bomItem models.BOMItem
			var productID int
			var productName string
			var quantity float64
			err := bomRows.Scan(
				&productID,
				&productName,
				&quantity,
			)
			if err != nil {
				log.Printf("Error scanning BOM: %v", err)
				continue
			}
			bomItem.MaterialID = productID
			bomItem.Name = productName
			bomItem.Quantity = quantity
			element.BOM = append(element.BOM, bomItem)
		}

		// Step 4: Fetch Stages
		stageRows, err := db.Query(`
            SELECT 
                s.stage_id,
                s.stage_name,
                s.stage_order
            FROM stages s
            JOIN element_type_stages ets ON s.stage_id = ets.stage_id
            WHERE ets.element_type_id = $1
            ORDER BY s.stage_order`, req.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch stages",
				"details": err.Error(),
			})
			return
		}
		defer stageRows.Close()

		for stageRows.Next() {
			var stage models.StageDetails
			var stageID int
			var stageName string
			var stageOrder int
			err := stageRows.Scan(
				&stageID,
				&stageName,
				&stageOrder,
			)
			if err != nil {
				log.Printf("Error scanning stage: %v", err)
				continue
			}
			stage.StageID = stageID
			stage.StageName = stageName
			element.Stages = append(element.Stages, stage)
		}

		// Generate PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Header with styling
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 18)
		pdf.CellFormat(190, 12, "Element Details Report", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Project information
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(190, 8, fmt.Sprintf("Project: %s (ID: %d)", projectName, req.ProjectID))
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(190, 6, fmt.Sprintf("Generated by: %s", userName))
		pdf.Ln(4)
		pdf.Cell(190, 6, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))
		pdf.Ln(10)

		// Element Type Information
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 10, "Element Type Information", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(5)

		// Element details table
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(60, 8, "Property", "1", 0, "C", true, 0, "")
		pdf.CellFormat(130, 8, "Value", "1", 1, "C", true, 0, "")
		pdf.SetFont("Arial", "", 10)

		details := map[string]interface{}{
			"Element Type ID": element.ID,
			"Element Type":    element.ElementType,
			"Version":         element.ElementTypeVersion,
			"Thickness":       element.Thickness,
			"Length":          element.Length,
			"Height":          element.Height,
			"Volume":          element.Volume,
			"Mass":            element.Mass,
			"Area":            element.Area,
			"Width":           element.Width,
			"Total Quantity":  element.TotalQuantity,
		}

		for key, value := range details {
			pdf.CellFormat(60, 8, key, "1", 0, "L", false, 0, "")
			pdf.CellFormat(130, 8, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
		}

		pdf.Ln(10)

		// Drawings section
		if len(element.DrawingType) > 0 {
			pdf.SetFillColor(0, 0, 0)       // Black background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 12)
			pdf.CellFormat(190, 10, "Drawings", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(5)

			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(30, 8, "Drawing ID", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 8, "Type", "1", 0, "C", true, 0, "")
			pdf.CellFormat(30, 8, "Version", "1", 0, "C", true, 0, "")
			pdf.CellFormat(50, 8, "File", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 8, "Created", "1", 1, "C", true, 0, "")

			pdf.SetFont("Arial", "", 8)
			for _, drawing := range element.DrawingType {
				file := drawing.FilePath
				if len(file) > 40 {
					file = file[:37] + "..."
				}
				pdf.CellFormat(30, 6, "N/A", "1", 0, "C", false, 0, "")
				pdf.CellFormat(40, 6, drawing.Name, "1", 0, "L", false, 0, "")
				pdf.CellFormat(30, 6, drawing.Version, "1", 0, "C", false, 0, "")
				pdf.CellFormat(50, 6, file, "1", 0, "L", false, 0, "")
				pdf.CellFormat(40, 6, drawing.CreatedAt.Format("2006-01-02"), "1", 1, "C", false, 0, "")
			}
			pdf.Ln(10)
		}

		// BOM section
		if len(element.BOM) > 0 {
			pdf.SetFillColor(0, 0, 0)       // Black background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 12)
			pdf.CellFormat(190, 10, "Bill of Materials (BOM)", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(5)

			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(30, 8, "Product ID", "1", 0, "C", true, 0, "")
			pdf.CellFormat(100, 8, "Product Name", "1", 0, "C", true, 0, "")
			pdf.CellFormat(60, 8, "Quantity", "1", 1, "C", true, 0, "")

			pdf.SetFont("Arial", "", 8)
			for _, bom := range element.BOM {
				pdf.CellFormat(30, 6, fmt.Sprintf("%d", bom.MaterialID), "1", 0, "C", false, 0, "")
				pdf.CellFormat(100, 6, bom.Name, "1", 0, "L", false, 0, "")
				pdf.CellFormat(60, 6, fmt.Sprintf("%.2f", bom.Quantity), "1", 1, "C", false, 0, "")
			}
			pdf.Ln(10)
		}

		// Stages section
		if len(element.Stages) > 0 {
			pdf.SetFillColor(0, 0, 0)       // Black background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 12)
			pdf.CellFormat(190, 10, "Production Stages", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(5)

			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(30, 8, "Stage ID", "1", 0, "C", true, 0, "")
			pdf.CellFormat(100, 8, "Stage Name", "1", 0, "C", true, 0, "")
			pdf.CellFormat(60, 8, "Order", "1", 1, "C", true, 0, "")

			pdf.SetFont("Arial", "", 8)
			for _, stage := range element.Stages {
				pdf.CellFormat(30, 6, fmt.Sprintf("%d", stage.StageID), "1", 0, "C", false, 0, "")
				pdf.CellFormat(100, 6, stage.StageName, "1", 0, "L", false, 0, "")
				pdf.CellFormat(60, 6, fmt.Sprintf("%d", stage.Quantity), "1", 1, "C", false, 0, "")
			}
		}

		// Footer
		pdf.SetY(-20)
		pdf.SetFont("Arial", "I", 8)
		pdf.Cell(190, 6, "This is a computer-generated report. Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

		// Output PDF
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=element_details_%s_%d.pdf", element.ElementType, req.ElementTypeID))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}

		// Log activity
		activityLog := models.ActivityLog{
			EventContext: "PDF Generation",
			EventName:    "Element Details",
			Description:  "Generated PDF report for element details",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			log.Printf("Failed to log PDF generation activity: %v", logErr)
		}
	}
}

// GenerateElementsWithDrawingsPDF generates a PDF report for elements with drawings by project
// @Summary Generate PDF report for elements with drawings by project
// @Description Generate a comprehensive PDF report containing all elements with their drawings for a specific project
// @Tags PDF
// @Accept json
// @Produce application/pdf
// @Param project_id path int true "Project ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(25)
// @Success 200 {file} file "PDF file"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/elements_with_drawings_pdf/{project_id} [get]
func GenerateElementsWithDrawingsPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		projectId, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Project ID"})
			return
		}

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "25")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 25
		}

		offset := (page - 1) * limit

		// Fetch project name
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectId).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project details"})
			return
		}

		// First get total count
		countQuery := `
		SELECT COUNT(DISTINCT e.id)
		FROM element e
		WHERE e.project_id = $1
		`
		var totalElements int
		err = db.QueryRow(countQuery, projectId).Scan(&totalElements)
		if err != nil {
			log.Printf("Count query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get total count"})
			return
		}

		// Calculate total pages
		totalPages := (totalElements + limit - 1) / limit

		// Step 1: page element IDs to avoid join duplication
		idRows, err := db.Query(`
			SELECT e.id
			FROM element e
			WHERE e.project_id = $1
			ORDER BY e.id
			LIMIT $2 OFFSET $3
		`, projectId, limit, offset)
		if err != nil {
			log.Printf("Database id query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database query failed",
				"details": err.Error(),
			})
			return
		}
		defer idRows.Close()

		pagedIDs := make([]int, 0, limit)
		for idRows.Next() {
			var id int
			if err := idRows.Scan(&id); err != nil {
				log.Println("Scan id error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan id", "details": err.Error()})
				return
			}
			pagedIDs = append(pagedIDs, id)
		}

		if len(pagedIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No elements found for this project"})
			return
		}

		// Build placeholders for IN clause
		placeholders := make([]string, len(pagedIDs))
		args := make([]interface{}, 0, len(pagedIDs)+1)
		args = append(args, projectId)
		for i, id := range pagedIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, id)
		}

		query := fmt.Sprintf(`
	        SELECT e.id, e.element_id, e.element_name, e.project_id, e.element_type_version, e.element_type_id,
		       et.element_type_name, et.thickness, et.length, et.height, et.volume, et.mass, et.area, et.width,
	               COALESCE(d.drawing_id, 0) AS drawing_id, COALESCE(d.current_version, '') AS current_version,
	               COALESCE(d.drawing_type_id, 0) AS drawing_type_id, COALESCE(d.comments, '') AS comments,
	               COALESCE(d.file, '') AS file,
	               COALESCE(dr.version, '') AS version, COALESCE(dr.drawing_type_id, 0) AS revision_drawing_type_id,
	               COALESCE(dr.comments, '') AS revision_comments, COALESCE(dr.file, '') AS revision_file,
	               COALESCE(dr.drawing_revision_id, 0) AS drawing_revision_id,
			   e.target_location
	        FROM element e
	        LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
	        LEFT JOIN drawings d ON e.element_type_id = d.element_type_id
	        LEFT JOIN drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
	        WHERE e.project_id = $1 AND e.id IN (%s)
	        ORDER BY e.id
	        `, strings.Join(placeholders, ","))

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("Database query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database query failed",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		elements := map[int]*models.ElementResponse{}

		for rows.Next() {
			var element models.ElementResponse
			var drawing models.DrawingResponse
			var revision models.DrawingsRevisionResponse

			var elementTypeName sql.NullString
			var thickness sql.NullFloat64
			var length sql.NullFloat64
			var height sql.NullFloat64
			var volume sql.NullFloat64
			var mass sql.NullFloat64
			var area sql.NullFloat64
			var width sql.NullFloat64
			var targetLocation sql.NullInt64

			err := rows.Scan(
				&element.ID, &element.ElementID, &element.ElementName, &element.ProjectID,
				&element.ElementTypeVersion, &element.ElementTypeID, &elementTypeName,
				&thickness, &length, &height, &volume, &mass, &area, &width,
				&drawing.DrawingId, &drawing.CurrentVersion, &drawing.DrawingTypeId,
				&drawing.Comments, &drawing.File,
				&revision.Version, &revision.DrawingTypeId, &revision.Comments,
				&revision.File, &revision.DrawingRevisionId, &targetLocation,
			)
			if err != nil {
				log.Println("Scan error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			element.Thickness = thickness.Float64
			element.Length = length.Float64
			element.Height = height.Float64
			element.Volume = volume.Float64
			element.Mass = mass.Float64
			element.Area = area.Float64
			element.Width = width.Float64

			element.Status = getElementStatus(db, element.ID)

			if targetLocation.Valid {
				tower, floor, err := getTowerAndFloor(db, targetLocation.Int64)
				if err != nil {
					log.Printf("Error fetching tower/floor: %v", err)
				} else {
					element.Tower = tower
					element.Floor = floor
				}
			}

			element.ElementTypeName = elementTypeName.String

			if _, exists := elements[element.ID]; !exists {
				element.Drawings = []models.DrawingResponse{}
				elements[element.ID] = &element
			}

			if drawing.DrawingId != 0 {
				e := elements[element.ID]
				found := false

				for i := range e.Drawings {
					if e.Drawings[i].DrawingId == drawing.DrawingId {
						e.Drawings[i].DrawingsRevision = append(e.Drawings[i].DrawingsRevision, revision)
						found = true
						break
					}
				}

				if !found {
					drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}
					if revision.DrawingRevisionId != 0 {
						drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
					}
					e.Drawings = append(e.Drawings, drawing)
				}
			}
		}

		var result []models.ElementResponse
		for _, element := range elements {
			result = append(result, *element)
		}

		// Generate PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Header with styling
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 18)
		pdf.CellFormat(190, 12, "Elements with Drawings Report", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Project information
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(190, 8, fmt.Sprintf("Project: %s (ID: %d)", projectName, projectId))
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(190, 6, fmt.Sprintf("Generated by: %s", userName))
		pdf.Ln(4)
		pdf.Cell(190, 6, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))
		pdf.Ln(4)
		pdf.Cell(190, 6, fmt.Sprintf("Page: %d of %d | Total Elements: %d", page, totalPages, totalElements))
		pdf.Ln(10)

		// Elements section
		pdf.SetFillColor(0, 0, 0)       // Black background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 10, "Elements with Drawings", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(5)

		// Process each element
		for _, element := range result {
			// Check if we need a new page
			if pdf.GetY() > 250 {
				pdf.AddPage()
			}

			// Element header
			pdf.SetFillColor(50, 50, 50)    // Dark gray background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 11)
			pdf.CellFormat(190, 8, fmt.Sprintf("Element: %s (%s)", element.ElementName, element.ElementID), "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(3)

			// Element details table
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(40, 6, "Property", "1", 0, "C", true, 0, "")
			pdf.CellFormat(50, 6, "Value", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 6, "Property", "1", 0, "C", true, 0, "")
			pdf.CellFormat(60, 6, "Value", "1", 1, "C", true, 0, "")
			pdf.SetFont("Arial", "", 8)

			// Element details
			details := map[string]interface{}{
				"Element ID":   element.ID,
				"Element Type": element.ElementTypeName,
				"Version":      element.ElementTypeVersion,
				"Status":       element.Status,
				"Tower":        element.Tower,
				"Floor":        element.Floor,
				"Thickness":    element.Thickness,
				"Length":       element.Length,
				"Height":       element.Height,
				"Volume":       element.Volume,
				"Mass":         element.Mass,
				"Area":         element.Area,
				"Width":        element.Width,
			}

			keys := []string{"Element ID", "Element Type", "Version", "Status", "Tower", "Floor", "Thickness", "Length", "Height", "Volume", "Mass", "Area", "Width"}
			for i := 0; i < len(keys); i += 2 {
				key1 := keys[i]
				value1 := fmt.Sprintf("%v", details[key1])
				key2 := ""
				value2 := ""
				if i+1 < len(keys) {
					key2 = keys[i+1]
					value2 = fmt.Sprintf("%v", details[key2])
				}
				pdf.CellFormat(40, 6, key1, "1", 0, "L", false, 0, "")
				pdf.CellFormat(50, 6, value1, "1", 0, "L", false, 0, "")
				pdf.CellFormat(40, 6, key2, "1", 0, "L", false, 0, "")
				pdf.CellFormat(60, 6, value2, "1", 1, "L", false, 0, "")
			}

			pdf.Ln(5)

			// Drawings section for this element
			if len(element.Drawings) > 0 {
				pdf.SetFillColor(0, 0, 0)       // Black background
				pdf.SetTextColor(255, 255, 255) // White text
				pdf.SetFont("Arial", "B", 10)
				pdf.CellFormat(190, 8, fmt.Sprintf("Drawings (%d)", len(element.Drawings)), "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255) // Reset to white
				pdf.SetTextColor(0, 0, 0)       // Reset to black
				pdf.Ln(3)

				// Drawings table header
				pdf.SetFont("Arial", "B", 8)
				pdf.CellFormat(30, 6, "Drawing ID", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 6, "Version", "1", 0, "C", true, 0, "")
				pdf.CellFormat(30, 6, "Type ID", "1", 0, "C", true, 0, "")
				pdf.CellFormat(50, 6, "File", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 6, "Comments", "1", 1, "C", true, 0, "")

				pdf.SetFont("Arial", "", 7)
				for _, drawing := range element.Drawings {
					file := drawing.File
					if len(file) > 40 {
						file = file[:37] + "..."
					}
					comments := drawing.Comments
					if len(comments) > 30 {
						comments = comments[:27] + "..."
					}
					pdf.CellFormat(30, 5, fmt.Sprintf("%d", drawing.DrawingId), "1", 0, "C", false, 0, "")
					pdf.CellFormat(40, 5, drawing.CurrentVersion, "1", 0, "C", false, 0, "")
					pdf.CellFormat(30, 5, fmt.Sprintf("%d", drawing.DrawingTypeId), "1", 0, "C", false, 0, "")
					pdf.CellFormat(50, 5, file, "1", 0, "L", false, 0, "")
					pdf.CellFormat(40, 5, comments, "1", 1, "L", false, 0, "")

					// Drawing revisions
					if len(drawing.DrawingsRevision) > 0 {
						pdf.SetFont("Arial", "B", 7)
						pdf.CellFormat(190, 5, "Revisions:", "0", 1, "L", false, 0, "")
						pdf.SetFont("Arial", "", 6)
						for _, revision := range drawing.DrawingsRevision {
							revFile := revision.File
							if len(revFile) > 50 {
								revFile = revFile[:47] + "..."
							}
							pdf.CellFormat(30, 4, fmt.Sprintf("Rev: %s", revision.Version), "1", 0, "L", false, 0, "")
							pdf.CellFormat(50, 4, revFile, "1", 0, "L", false, 0, "")
							pdf.CellFormat(30, 4, fmt.Sprintf("Type: %d", revision.DrawingTypeId), "1", 0, "C", false, 0, "")
							pdf.CellFormat(80, 4, revision.Comments, "1", 1, "L", false, 0, "")
						}
					}
				}
			} else {
				pdf.SetFont("Arial", "I", 9)
				pdf.Cell(190, 6, "No drawings available for this element")
			}

			pdf.Ln(10)
		}

		// Footer
		pdf.SetY(-20)
		pdf.SetFont("Arial", "I", 8)
		pdf.Cell(190, 6, "This is a computer-generated report. Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

		// Output PDF
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=elements_with_drawings_%s_%d.pdf", projectName, projectId))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}

		// Log activity
		activityLog := models.ActivityLog{
			EventContext: "PDF Generation",
			EventName:    "Elements with Drawings",
			Description:  "Generated PDF report for elements with drawings",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			log.Printf("Failed to log PDF generation activity: %v", logErr)
		}
	}
}

// GenerateElementByIDPDF generates a PDF report for a specific element by ID
// @Summary Generate PDF report for element by ID
// @Description Generate a comprehensive PDF report containing complete element details, lifecycle, drawings, BOM, and QC answers
// @Tags PDF
// @Accept json
// @Produce application/pdf
// @Param id path int true "Element ID"
// @Success 200 {file} file "PDF file"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/element_by_id_pdf/{id} [get]
func GenerateElementByIDPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Set default user name for PDF generation
		userName := "System"

		elementIDStr := c.Param("id")
		var err error
		elementID, convErr := strconv.Atoi(elementIDStr)
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID format"})
			return
		}

		var element models.Element

		// Fetch element by ID
		query := `
		SELECT id, element_type_id, element_id, element_name, project_id, created_by, created_at,
		       status, element_type_version, update_at, target_location, disable
		FROM element
		WHERE id = $1
	`
		var statusStr string
		err = db.QueryRow(query, elementID).Scan(
			&element.Id,
			&element.ElementTypeID,
			&element.ElementId,
			&element.ElementName,
			&element.ProjectID,
			&element.CreatedBy,
			&element.CreatedAt,
			&statusStr,
			&element.ElementTypeVersion,
			&element.UpdateAt,
			&element.TargetLocation,
			&element.Disable,
		)
		// Convert status string to int, handling potential whitespace
		if statusStr != "" {
			statusStr = strings.TrimSpace(statusStr)
			if statusInt, parseErr := strconv.Atoi(statusStr); parseErr == nil {
				element.Status = statusInt
			} else {
				element.Status = 0 // Default value if parsing fails
			}
		}
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch element",
					"details": err.Error(),
				})
			}
			return
		}

		// Fetch project information with error handling
		var projectName, projectDescription string
		var projectCreatedAt time.Time
		var projectCreatedBy int

		// Try to fetch project details, handle missing fields gracefully
		err = db.QueryRow(`
			SELECT name, 
			       COALESCE(description, '') as description, 
			       COALESCE(created_at, NOW()) as created_at, 
			       COALESCE(created_by, 0) as created_by 
			FROM project 
			WHERE project_id = $1`, element.ProjectID).Scan(&projectName, &projectDescription, &projectCreatedAt, &projectCreatedBy)
		if err != nil {
			log.Printf("Failed to fetch project details: %v", err)
			// Set default values if project not found
			projectName = fmt.Sprintf("Project %d", element.ProjectID)
			projectDescription = "Project details not available"
			projectCreatedAt = time.Now()
			projectCreatedBy = 0
		}

		// Fetch project statistics with error handling
		var totalElements, totalElementTypes int
		err = db.QueryRow(`
			SELECT 
				COALESCE(COUNT(DISTINCT e.id), 0) as total_elements,
				COALESCE(COUNT(DISTINCT e.element_type_id), 0) as total_element_types
			FROM element e 
			WHERE e.project_id = $1`, element.ProjectID).Scan(&totalElements, &totalElementTypes)
		if err != nil {
			log.Printf("Failed to fetch project statistics: %v", err)
			totalElements = 0
			totalElementTypes = 0
		}

		// Fetch associated element type details
		var elementType models.ElementTypeR
		elementTypeQuery := `
		SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width, created_by,
		       created_at, update_at, element_type_id, project_id, element_type_version,
		       total_count_element
		FROM element_type
		WHERE element_type_id = $1
	`
		var volume, mass, area, width float64
		err = db.QueryRow(elementTypeQuery, element.ElementTypeID).Scan(
			&elementType.ElementType,
			&elementType.ElementTypeName,
			&elementType.Thickness,
			&elementType.Length,
			&elementType.Height,
			&volume, &mass, &area, &width,
			&elementType.CreatedBy,
			&elementType.CreatedAt,
			&elementType.UpdatedAt,
			&elementType.ElementTypeId,
			&elementType.ProjectID,
			&elementType.ElementTypeVersion,
			&elementType.TotalCountElement,
		)
		elementType.Volume = volume
		elementType.Mass = mass
		elementType.Area = area
		elementType.Width = width
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch element type",
				"details": err.Error(),
			})
			return
		}

		// Fetch hierarchy information for this specific element
		hierarchyQuery := `
			SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
			       p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix,
			       parent.name as tower_name
			FROM element_type_hierarchy_quantity hq
			JOIN precast p ON hq.hierarchy_id = p.id
			LEFT JOIN precast parent ON p.parent_id = parent.id
			WHERE hq.element_type_id = $1 AND hq.hierarchy_id = $2
		`
		hierarchyRows, err := db.Query(hierarchyQuery, element.ElementTypeID, element.TargetLocation)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err)})
			return
		}
		defer hierarchyRows.Close()

		var hierarchyResponseList []models.HierarchyResponce
		for hierarchyRows.Next() {
			var hierarchyData models.HierarchyResponce
			var hierarchyId, quantity int
			var namingConvention string
			var parentID sql.NullInt64

			err := hierarchyRows.Scan(
				&hierarchyId, &quantity, &namingConvention,
				&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
				&hierarchyData.Description, &parentID, &hierarchyData.Prefix,
				&hierarchyData.TowerName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan hierarchy data: %v", err)})
				return
			}

			if parentID.Valid {
				hierarchyData.ParentID = int(parentID.Int64)
			} else {
				hierarchyData.ParentID = 0
			}

			hierarchyData.Quantity = quantity
			hierarchyData.NamingConvention = namingConvention
			hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
		}

		// Store hierarchy data for PDF generation
		_ = hierarchyResponseList

		// Fetch drawings
		drawingsQuery := `
		SELECT d.drawing_id, d.current_version, d.created_at, d.created_by, d.drawing_type_id, dt.drawing_type_name, d.update_at, d.updated_by,
       d.comments, d.file, d.element_type_id
FROM drawings d
JOIN drawing_type dt ON d.drawing_type_id = dt.drawing_type_id 
WHERE d.element_type_id = $1
ORDER BY d.created_at DESC
	`
		drawingRows, err := db.Query(drawingsQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawings", "details": err.Error()})
			return
		}
		defer drawingRows.Close()

		var drawings []models.DrawingsR
		for drawingRows.Next() {
			var drawing models.DrawingsR
			err := drawingRows.Scan(
				&drawing.DrawingsId,
				&drawing.CurrentVersion,
				&drawing.CreatedAt,
				&drawing.CreatedBy,
				&drawing.DrawingTypeId,
				&drawing.DrawingTypeName,
				&drawing.UpdatedAt,
				&drawing.UpdatedBy,
				&drawing.Comments,
				&drawing.File,
				&drawing.ElementTypeID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing", "details": err.Error()})
				return
			}

			// Fetch drawing revisions
			revisionQuery := `
			SELECT dr.parent_drawing_id, dr.version, dr.created_at, dr.created_by, dr.drawing_type_id, 
			       dt.drawing_type_name, dr.comments, dr.file, dr.drawing_revision_id, dr.element_type_id
			FROM drawings_revision dr
			JOIN drawing_type dt ON dr.drawing_type_id = dt.drawing_type_id
			WHERE dr.parent_drawing_id = $1
			ORDER BY dr.created_at DESC
		`
			revisionRows, err := db.Query(revisionQuery, drawing.DrawingsId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revisions", "details": err.Error()})
				return
			}
			defer revisionRows.Close()

			for revisionRows.Next() {
				var revision models.DrawingsRevisionR
				err := revisionRows.Scan(
					&revision.ParentDrawingsId,
					&revision.Version,
					&revision.CreatedAt,
					&revision.CreatedBy,
					&revision.DrawingsTypeId,
					&revision.DrawingTypeName,
					&revision.Comments,
					&revision.File,
					&revision.DrawingsRevisionId,
					&revision.ElementTypeID,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing revision", "details": err.Error()})
					return
				}
				revision.CreatedAtFormatted = formatDateTime(revision.CreatedAt)
				drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
			}
			drawings = append(drawings, drawing)
		}

		// Store drawings data for PDF generation
		_ = drawings

		// Fetch BOM products
		productsQuery := `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`
		productRows, err := db.Query(productsQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
			return
		}
		defer productRows.Close()

		var products []models.ProductR
		for productRows.Next() {
			var product models.ProductR
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := productRows.Scan(
				&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
				return
			}
			products = append(products, product)
		}

		// Store products data for PDF generation
		_ = products

		// Fetch QC answers
		answersQuery := `
			SELECT 
				qa.id, qa.project_id, qa.qc_id, qa.question_id, qa.option_id, qa.task_id, qa.stage_id,
				qa.comment, qa.image_path, qa.created_at, qa.updated_at, qa.element_id,
				CONCAT(u.first_name, ' ', u.last_name) as qc_name, o.option_text, q.question_text,
				q.paper_id, p.name as paper_name, ps.name as stage_name
			FROM qc_answers qa
			LEFT JOIN users u ON qa.qc_id = u.id
			LEFT JOIN options o ON qa.option_id = o.id
			LEFT JOIN questions q ON qa.question_id = q.id
			LEFT JOIN papers p ON q.paper_id = p.id
			LEFT JOIN project_stages ps ON qa.stage_id = ps.id
			WHERE qa.element_id = $1
			ORDER BY qa.stage_id, qa.created_at DESC
		`
		answerRows, err := db.Query(answersQuery, elementID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch answers", "details": err.Error()})
			return
		}
		defer answerRows.Close()

		// Map to group answers by stage_id
		stageGroups := make(map[int][]map[string]interface{})

		for answerRows.Next() {
			var (
				id, projectID, qcID, questionID, taskID, stageID, elementID int
				optionID                                                    *int
				comment, imagePath                                          *string
				createdAt, updatedAt                                        time.Time
				qcName, optionText, questionText, paperName, stageName      *string
				paperID                                                     *int
			)

			err := answerRows.Scan(
				&id, &projectID, &qcID, &questionID, &optionID, &taskID, &stageID,
				&comment, &imagePath, &createdAt, &updatedAt, &elementID,
				&qcName, &optionText, &questionText, &paperID, &paperName, &stageName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan answer", "details": err.Error()})
				return
			}

			answer := map[string]interface{}{
				"id": id, "project_id": projectID, "qc_id": qcID, "question_id": questionID,
				"option_id": optionID, "task_id": taskID, "stage_id": stageID,
				"comment": comment, "image_path": imagePath, "created_at": createdAt,
				"updated_at": updatedAt, "element_id": elementID, "qc_name": qcName,
				"option_text": optionText, "question_text": questionText, "paper_id": paperID,
				"paper_name": paperName, "stage_name": stageName,
			}

			stageGroups[stageID] = append(stageGroups[stageID], answer)
		}

		// Convert map to slice for PDF
		var submittedAnswers []map[string]interface{}
		for stageID, answers := range stageGroups {
			stageGroup := map[string]interface{}{
				"stage_id":   stageID,
				"stage_name": getStageNameFromAnswers(answers),
				"answers":    answers,
			}
			submittedAnswers = append(submittedAnswers, stageGroup)
		}

		// Generate QR code for element
		qrData := struct {
			ElementID   int    `json:"element_id"`
			ElementName string `json:"element_name"`
			ProjectID   int    `json:"project_id"`
		}{
			ElementID:   elementID,
			ElementName: element.ElementName,
			ProjectID:   element.ProjectID,
		}

		qrCodeBytes, err := generateQRCodeImage(qrData)
		if err != nil {
			log.Printf("Failed to generate QR code: %v", err)
			// Try alternative QR code generation
			qrCodeBytes, err = generateQRCodeImage(fmt.Sprintf("Element ID: %d, Name: %s", elementID, element.ElementName))
			if err != nil {
				log.Printf("Failed to generate alternative QR code: %v", err)
			}
		}

		// Generate PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Header with beautiful styling
		pdf.SetFillColor(25, 25, 112)   // Dark blue background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 20)
		pdf.CellFormat(190, 15, "ELEMENT DETAILS REPORT", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(10)

		// Project and element information with QR code - Better spacing and box
		pdf.SetFillColor(240, 248, 255) // Light blue background
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 14)
		// Use project name or fallback
		displayProjectName := projectName
		if displayProjectName == "" {
			displayProjectName = fmt.Sprintf("Project %d", element.ProjectID)
		}
		pdf.CellFormat(100, 12, fmt.Sprintf("Project: %s", displayProjectName), "1", 0, "L", true, 0, "")
		pdf.Ln(12) // Increased spacing between Project and Element
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(100, 10, fmt.Sprintf("Element: %s (%s)", element.ElementName, element.ElementId), "1", 0, "L", true, 0, "")
		pdf.Ln(10)                      // Increased spacing after Element line
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetFont("Arial", "", 10)
		pdf.CellFormat(100, 8, fmt.Sprintf("Generated by: %s", userName), "0", 0, "L", false, 0, "")
		pdf.Ln(8) // Increased spacing
		pdf.CellFormat(100, 8, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")), "0", 0, "L", false, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white

		// Add QR code to the right side - Fixed positioning
		if qrCodeBytes != nil {
			// Create a temporary file for the QR code
			imageName := fmt.Sprintf("qr_%d", elementID)
			pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{ImageType: "JPEG"}, bytes.NewReader(qrCodeBytes))

			// Position QR code on the right side - Better positioning
			pdf.SetXY(120, 30) // X=120mm, Y=30mm from top
			pdf.ImageOptions(imageName, 120, 30, 30, 30, false, gofpdf.ImageOptions{ImageType: "JPEG"}, 0, "")

			// Add QR code label - Better positioning
			pdf.SetXY(120, 65) // Position below QR code
			pdf.SetFont("Arial", "B", 8)
			pdf.Cell(30, 4, "Scan for Details")
		}

		pdf.Ln(15) // Increased spacing between project info and next section

		// Element Information section - Beautiful styling
		pdf.SetFillColor(70, 130, 180)  // Steel blue background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 14)
		pdf.CellFormat(190, 12, "ELEMENT INFORMATION", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Element details table - Optimized formatting to prevent page breaks
		// Check if we need a new page before starting the table
		if pdf.GetY() > 250 {
			pdf.AddPage()
		}

		pdf.SetFillColor(220, 220, 220) // Light gray background for header
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 11)
		pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
		pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetFont("Arial", "", 9)

		// Get current element status and stage information
		currentStatus := getElementStatus(db, elementID)

		// Get stage completion information
		var stageCompletionInfo string
		stageQuery := `
			SELECT ps.name, ps.stage_order, 
			       CASE WHEN a.element_id IS NOT NULL THEN 'Completed' ELSE 'Pending' END as status
			FROM project_stages ps
			LEFT JOIN activity a ON ps.id = a.stage_id AND a.element_id = $1
			WHERE ps.project_id = $2
			ORDER BY ps.stage_order
		`
		stageRows, err := db.Query(stageQuery, elementID, element.ProjectID)
		if err == nil {
			defer stageRows.Close()
			var stages []string
			for stageRows.Next() {
				var stageName, stageStatus string
				var stageOrder int
				err := stageRows.Scan(&stageName, &stageOrder, &stageStatus)
				if err == nil {
					stages = append(stages, fmt.Sprintf("%s: %s", stageName, stageStatus))
				}
			}
			if len(stages) > 0 {
				stageCompletionInfo = strings.Join(stages, ", ")
			}
		}

		elementDetails := map[string]interface{}{
			"Element ID":           element.Id,
			"Element Name":         element.ElementName,
			"Element Type ID":      element.ElementTypeID,
			"Element Type Version": element.ElementTypeVersion,
			"Project ID":           element.ProjectID,
			"Created By":           element.CreatedBy,
			"Created At":           element.CreatedAt.Format("2006-01-02 15:04:05"),
			"Current Status":       currentStatus,
			"Target Location":      element.TargetLocation,
			"Disabled":             element.Disable,
		}

		for key, value := range elementDetails {
			// Check if we need a new page before adding each row
			if pdf.GetY() > 270 {
				pdf.AddPage()
				// Repeat header
				pdf.SetFillColor(220, 220, 220)
				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 11)
				pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
				pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255)
				pdf.SetFont("Arial", "", 9)
			}
			pdf.CellFormat(60, 8, key, "1", 0, "L", false, 0, "")
			pdf.CellFormat(130, 8, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
		}

		pdf.Ln(12) // Increased spacing

		// Project Information section - Beautiful styling
		pdf.SetFillColor(255, 165, 0)   // Orange background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 14)
		pdf.CellFormat(190, 12, "PROJECT INFORMATION", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Project details table - Optimized formatting
		// Check if we need a new page before starting the table
		if pdf.GetY() > 250 {
			pdf.AddPage()
		}

		pdf.SetFillColor(220, 220, 220) // Light gray background for header
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 11)
		pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
		pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetFont("Arial", "", 9)

		projectDetails := map[string]interface{}{
			"Project ID":          element.ProjectID,
			"Project Name":        projectName,
			"Description":         projectDescription,
			"Created At":          projectCreatedAt.Format("2006-01-02 15:04:05"),
			"Created By":          projectCreatedBy,
			"Total Elements":      totalElements,
			"Total Element Types": totalElementTypes,
		}

		for key, value := range projectDetails {
			// Check if we need a new page before adding each row
			if pdf.GetY() > 270 {
				pdf.AddPage()
				// Repeat header
				pdf.SetFillColor(220, 220, 220)
				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 11)
				pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
				pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255)
				pdf.SetFont("Arial", "", 9)
			}
			pdf.CellFormat(60, 8, key, "1", 0, "L", false, 0, "")
			pdf.CellFormat(130, 8, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
		}

		pdf.Ln(12) // Increased spacing

		// Stage Information section - Beautiful styling
		if stageCompletionInfo != "" {
			pdf.SetFillColor(34, 139, 34)   // Forest green background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 14)
			pdf.CellFormat(190, 12, "STAGE INFORMATION", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(8)

			// Stage details table - Optimized formatting
			// Check if we need a new page before starting the table
			if pdf.GetY() > 250 {
				pdf.AddPage()
			}

			pdf.SetFillColor(220, 220, 220) // Light gray background for header
			pdf.SetTextColor(0, 0, 0)       // Black text
			pdf.SetFont("Arial", "B", 11)
			pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
			pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetFont("Arial", "", 9)

			stageDetails := map[string]interface{}{
				"Current Status": currentStatus,
				"Stage Progress": stageCompletionInfo,
			}

			for key, value := range stageDetails {
				// Check if we need a new page before adding each row
				if pdf.GetY() > 270 {
					pdf.AddPage()
					// Repeat header
					pdf.SetFillColor(220, 220, 220)
					pdf.SetTextColor(0, 0, 0)
					pdf.SetFont("Arial", "B", 11)
					pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
					pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
					pdf.SetFillColor(255, 255, 255)
					pdf.SetFont("Arial", "", 9)
				}
				pdf.CellFormat(60, 8, key, "1", 0, "L", false, 0, "")
				pdf.CellFormat(130, 8, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
			}
			pdf.Ln(12) // Increased spacing
		}

		// Element Type Information section - Beautiful styling
		pdf.SetFillColor(128, 0, 128)   // Purple background
		pdf.SetTextColor(255, 255, 255) // White text
		pdf.SetFont("Arial", "B", 14)
		pdf.CellFormat(190, 12, "ELEMENT TYPE INFORMATION", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetTextColor(0, 0, 0)       // Reset to black
		pdf.Ln(8)

		// Element type details table - Optimized formatting
		// Check if we need a new page before starting the table
		if pdf.GetY() > 250 {
			pdf.AddPage()
		}

		pdf.SetFillColor(220, 220, 220) // Light gray background for header
		pdf.SetTextColor(0, 0, 0)       // Black text
		pdf.SetFont("Arial", "B", 11)
		pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
		pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255) // Reset to white
		pdf.SetFont("Arial", "", 9)

		elementTypeDetails := map[string]interface{}{
			"Element Type":        elementType.ElementType,
			"Element Type Name":   elementType.ElementTypeName,
			"Thickness":           elementType.Thickness,
			"Length":              elementType.Length,
			"Height":              elementType.Height,
			"Volume":              elementType.Volume,
			"Mass":                elementType.Mass,
			"Area":                elementType.Area,
			"Width":               elementType.Width,
			"Total Count Element": elementType.TotalCountElement,
		}

		for key, value := range elementTypeDetails {
			// Check if we need a new page before adding each row
			if pdf.GetY() > 270 {
				pdf.AddPage()
				// Repeat header
				pdf.SetFillColor(220, 220, 220)
				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 11)
				pdf.CellFormat(60, 10, "Property", "1", 0, "C", true, 0, "")
				pdf.CellFormat(130, 10, "Value", "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255)
				pdf.SetFont("Arial", "", 9)
			}
			pdf.CellFormat(60, 8, key, "1", 0, "L", false, 0, "")
			pdf.CellFormat(130, 8, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
		}

		pdf.Ln(10)

		// Hierarchy Information section - Beautiful styling
		if len(hierarchyResponseList) > 0 {
			pdf.SetFillColor(255, 140, 0)   // Orange background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 14)
			pdf.CellFormat(190, 12, "HIERARCHY INFORMATION", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(8)

			// Hierarchy table header - Optimized formatting
			// Check if we need a new page before starting the table
			if pdf.GetY() > 250 {
				pdf.AddPage()
			}

			pdf.SetFillColor(220, 220, 220) // Light gray background for header
			pdf.SetTextColor(0, 0, 0)       // Black text
			pdf.SetFont("Arial", "B", 10)
			pdf.CellFormat(30, 8, "Hierarchy ID", "1", 0, "C", true, 0, "")
			pdf.CellFormat(50, 8, "Name", "1", 0, "C", true, 0, "")
			pdf.CellFormat(30, 8, "Quantity", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 8, "Tower Name", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 8, "Naming Convention", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white

			pdf.SetFont("Arial", "", 8)
			for _, hierarchy := range hierarchyResponseList {
				// Check if we need a new page before adding each row
				if pdf.GetY() > 270 {
					pdf.AddPage()
					// Repeat header
					pdf.SetFillColor(220, 220, 220)
					pdf.SetTextColor(0, 0, 0)
					pdf.SetFont("Arial", "B", 10)
					pdf.CellFormat(30, 8, "Hierarchy ID", "1", 0, "C", true, 0, "")
					pdf.CellFormat(50, 8, "Name", "1", 0, "C", true, 0, "")
					pdf.CellFormat(30, 8, "Quantity", "1", 0, "C", true, 0, "")
					pdf.CellFormat(40, 8, "Tower Name", "1", 0, "C", true, 0, "")
					pdf.CellFormat(40, 8, "Naming Convention", "1", 1, "C", true, 0, "")
					pdf.SetFillColor(255, 255, 255)
					pdf.SetFont("Arial", "", 8)
				}
				pdf.CellFormat(30, 6, fmt.Sprintf("%d", hierarchy.HierarchyID), "1", 0, "C", false, 0, "")
				pdf.CellFormat(50, 6, hierarchy.Name, "1", 0, "L", false, 0, "")
				pdf.CellFormat(30, 6, fmt.Sprintf("%d", hierarchy.Quantity), "1", 0, "C", false, 0, "")
				towerName := "N/A"
				if hierarchy.TowerName != nil {
					towerName = *hierarchy.TowerName
				}
				pdf.CellFormat(40, 6, towerName, "1", 0, "L", false, 0, "")
				pdf.CellFormat(40, 6, hierarchy.NamingConvention, "1", 1, "L", false, 0, "")
			}
			pdf.Ln(10)
		}

		// Drawings section - Beautiful styling
		if len(drawings) > 0 {
			pdf.SetFillColor(0, 100, 200)   // Blue background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 14)
			pdf.CellFormat(190, 12, "DRAWINGS", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(8)

			for _, drawing := range drawings {
				// Drawing header
				pdf.SetFillColor(50, 50, 50)    // Dark gray background
				pdf.SetTextColor(255, 255, 255) // White text
				pdf.SetFont("Arial", "B", 10)
				pdf.CellFormat(190, 8, fmt.Sprintf("Drawing ID: %d - %s", drawing.DrawingsId, drawing.DrawingTypeName), "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255) // Reset to white
				pdf.SetTextColor(0, 0, 0)       // Reset to black
				pdf.Ln(3)

				// Drawing details
				pdf.SetFont("Arial", "B", 9)
				pdf.CellFormat(40, 6, "Property", "1", 0, "C", true, 0, "")
				pdf.CellFormat(150, 6, "Value", "1", 1, "C", true, 0, "")
				pdf.SetFont("Arial", "", 8)

				drawingDetails := map[string]interface{}{
					"Version":    drawing.CurrentVersion,
					"Created By": drawing.CreatedBy,
					"Created At": drawing.CreatedAt.Format("2006-01-02 15:04:05"),
					"Updated By": drawing.UpdatedBy,
					"Updated At": drawing.UpdatedAt.Format("2006-01-02 15:04:05"),
					"Comments":   drawing.Comments,
					"File":       drawing.File,
				}

				for key, value := range drawingDetails {
					pdf.CellFormat(40, 6, key, "1", 0, "L", false, 0, "")
					pdf.CellFormat(150, 6, fmt.Sprintf("%v", value), "1", 1, "L", false, 0, "")
				}

				// Drawing revisions
				if len(drawing.DrawingsRevision) > 0 {
					pdf.Ln(3)
					pdf.SetFont("Arial", "B", 9)
					pdf.Cell(190, 6, "Revisions:")
					pdf.Ln(3)

					pdf.SetFont("Arial", "B", 8)
					pdf.CellFormat(30, 6, "Version", "1", 0, "C", true, 0, "")
					pdf.CellFormat(40, 6, "Created By", "1", 0, "C", true, 0, "")
					pdf.CellFormat(50, 6, "Created At", "1", 0, "C", true, 0, "")
					pdf.CellFormat(70, 6, "Comments", "1", 1, "C", true, 0, "")

					pdf.SetFont("Arial", "", 7)
					for _, revision := range drawing.DrawingsRevision {
						pdf.CellFormat(30, 5, revision.Version, "1", 0, "C", false, 0, "")
						pdf.CellFormat(40, 5, revision.CreatedBy, "1", 0, "L", false, 0, "")
						pdf.CellFormat(50, 5, revision.CreatedAt.Format("2006-01-02 15:04:05"), "1", 0, "C", false, 0, "")
						pdf.CellFormat(70, 5, revision.Comments, "1", 1, "L", false, 0, "")
					}
				}
				pdf.Ln(10)
			}
		}

		// BOM Products section - Beautiful styling
		if len(products) > 0 {
			pdf.SetFillColor(220, 20, 60)   // Crimson background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 14)
			pdf.CellFormat(190, 12, "BILL OF MATERIALS (BOM)", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(8)

			// BOM table header
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(30, 8, "Product ID", "1", 0, "C", true, 0, "")
			pdf.CellFormat(80, 8, "Product Name", "1", 0, "C", true, 0, "")
			pdf.CellFormat(30, 8, "Quantity", "1", 0, "C", true, 0, "")
			pdf.CellFormat(50, 8, "Unit", "1", 1, "C", true, 0, "")

			pdf.SetFont("Arial", "", 8)
			for _, product := range products {
				pdf.CellFormat(30, 6, fmt.Sprintf("%d", product.ProductID), "1", 0, "C", false, 0, "")
				pdf.CellFormat(80, 6, product.ProductName, "1", 0, "L", false, 0, "")
				pdf.CellFormat(30, 6, fmt.Sprintf("%.2f", product.Quantity), "1", 0, "C", false, 0, "")
				pdf.CellFormat(50, 6, "N/A", "1", 1, "C", false, 0, "")
			}
			pdf.Ln(10)
		}

		// QC Answers section - Beautiful styling
		if len(submittedAnswers) > 0 {
			pdf.SetFillColor(50, 205, 50)   // Lime green background
			pdf.SetTextColor(255, 255, 255) // White text
			pdf.SetFont("Arial", "B", 14)
			pdf.CellFormat(190, 12, "✅ QUALITY CONTROL ANSWERS", "1", 1, "C", true, 0, "")
			pdf.SetFillColor(255, 255, 255) // Reset to white
			pdf.SetTextColor(0, 0, 0)       // Reset to black
			pdf.Ln(8)

			for _, stageGroup := range submittedAnswers {
				stageName := stageGroup["stage_name"].(string)
				answers := stageGroup["answers"].([]map[string]interface{})

				// Stage header
				pdf.SetFillColor(50, 50, 50)    // Dark gray background
				pdf.SetTextColor(255, 255, 255) // White text
				pdf.SetFont("Arial", "B", 10)
				pdf.CellFormat(190, 8, fmt.Sprintf("Stage: %s", stageName), "1", 1, "C", true, 0, "")
				pdf.SetFillColor(255, 255, 255) // Reset to white
				pdf.SetTextColor(0, 0, 0)       // Reset to black
				pdf.Ln(3)

				// Answers table
				pdf.SetFont("Arial", "B", 8)
				pdf.CellFormat(30, 6, "Question", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 6, "Answer", "1", 0, "C", true, 0, "")
				pdf.CellFormat(30, 6, "QC Name", "1", 0, "C", true, 0, "")
				pdf.CellFormat(50, 6, "Comment", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 6, "Date", "1", 1, "C", true, 0, "")

				pdf.SetFont("Arial", "", 7)
				for _, answer := range answers {
					questionText := "N/A"
					if answer["question_text"] != nil {
						questionText = *answer["question_text"].(*string)
					}
					optionText := "N/A"
					if answer["option_text"] != nil {
						optionText = *answer["option_text"].(*string)
					}
					qcName := "N/A"
					if answer["qc_name"] != nil {
						qcName = *answer["qc_name"].(*string)
					}
					comment := "N/A"
					if answer["comment"] != nil {
						comment = *answer["comment"].(*string)
					}
					createdAt := answer["created_at"].(time.Time).Format("2006-01-02 15:04:05")

					pdf.CellFormat(30, 5, questionText, "1", 0, "L", false, 0, "")
					pdf.CellFormat(40, 5, optionText, "1", 0, "L", false, 0, "")
					pdf.CellFormat(30, 5, qcName, "1", 0, "L", false, 0, "")
					pdf.CellFormat(50, 5, comment, "1", 0, "L", false, 0, "")
					pdf.CellFormat(40, 5, createdAt, "1", 1, "C", false, 0, "")
				}
				pdf.Ln(10)
			}
		}

		// Footer
		pdf.SetY(-20)
		pdf.SetFont("Arial", "I", 8)
		pdf.Cell(190, 6, "This is a computer-generated report. Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

		// Output PDF
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=element_details_%s_%d.pdf", element.ElementName, elementID))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}

		// Log activity
		activityLog := models.ActivityLog{
			EventContext: "PDF Generation",
			EventName:    "Element by ID",
			Description:  "Generated PDF report for element by ID",
			UserName:     userName,
			HostName:     "System",
			IPAddress:    "127.0.0.1",
			CreatedAt:    time.Now(),
			ProjectID:    element.ProjectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			log.Printf("Failed to log PDF generation activity: %v", logErr)
		}
	}
}

package handlers

import (
	"backend/storage"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// Helper function to capitalize first letter of each word

// DownloadBOMTemplate downloads an empty BOM CSV template
func DownloadBOMTemplate(c *gin.Context) {
	// Set headers for CSV template download
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=bom_template.csv")

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header - matching the import function requirements
	header := []string{"ProductName", "ProductType", "Unit", "Rate"}
	if err := writer.Write(header); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV header"})
		return
	}

	// Add sample data row for reference
	sampleRow := []string{"Sample Product", "Sample Type", "PCS", "100.00"}
	if err := writer.Write(sampleRow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing sample row"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "BOM template downloaded successfully"})
}

// ExportCSVBOM godoc
// @Summary      Export BOM as CSV
// @Tags         export
// @Produce      text/csv
// @Success      200  {file}  file  "CSV file"
// @Failure      400  {object}  object
// @Router       /api/export_csv_bom [get]
func ExportCSVBOM(c *gin.Context) {
	db := storage.GetDB()

	// Get project_id from route parameter
	projectID := c.Param("project_id")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	// Convert projectID to int
	projectIDInt, err := strconv.Atoi(projectID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	// Fetch project name for summary and filename
	var projectName string
	if err := db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectIDInt).Scan(&projectName); err != nil {
		projectName = "project"
	}

	// Set headers for CSV file download
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=bom_export.csv")

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header - matching the import function requirements
	header := []string{"ProductName", "ProductType", "Unit", "Rate"}
	if err := writer.Write(header); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV header"})
		return
	}

	// Query BOM data from database
	query := `
		SELECT product_name, product_type, unit, name_id
		FROM inv_bom 
		WHERE project_id = $1
		ORDER BY product_name, product_type
	`
	rows, err := db.Query(query, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching BOM data"})
		return
	}
	defer rows.Close()

	// Write data rows
	for rows.Next() {
		var productName, productType, unit, nameId string
		if err := rows.Scan(&productName, &productType, &unit, &nameId); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning BOM data"})
			return
		}

		// Create row data - Rate column is empty as it's not stored in the database
		row := []string{productName, productType, unit, ""}
		if err := writer.Write(row); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV row"})
			return
		}
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating BOM data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "BOM CSV export successful"})
}

// DownloadPrecastTemplate downloads an empty Precast CSV template
func DownloadPrecastTemplate(c *gin.Context) {
	// Set headers for CSV template download
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=precast_template.csv")

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header - matching the import function requirements
	header := []string{"ID", "ProjectID", "Name", "Description", "ParentID", "Prefix", "Path", "NamingConvention"}
	if err := writer.Write(header); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV header"})
		return
	}

	// Add sample data row for reference
	sampleRow := []string{"1", "1", "Sample Precast", "Sample Description", "", "PREFIX", "PATH", "NAMING_CONVENTION"}
	if err := writer.Write(sampleRow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing sample row"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Precast template downloaded successfully"})
}

// ExportCSVPrecast godoc
// @Summary      Export precast as CSV
// @Tags         export
// @Produce      text/csv
// @Success      200  {file}  file  "CSV file"
// @Router       /api/export_csv_precast [get]
func ExportCSVPrecast(c *gin.Context) {
	db := storage.GetDB()

	// Get project_id from route parameter (optional for precast export)
	projectID := c.Param("project_id")
	var projectIDInt int
	var err error

	if projectID != "" {
		projectIDInt, err = strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
	}

	// Set headers for CSV file download
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=precast_export.csv")

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header - matching the import function requirements
	header := []string{"ID", "ProjectID", "Name", "Description", "ParentID", "Prefix", "Path", "NamingConvention"}
	if err := writer.Write(header); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV header"})
		return
	}

	// Query precast data from database
	var query string
	var args []interface{}

	if projectID != "" {
		query = `
			SELECT id, project_id, name, description, parent_id, prefix, path, naming_convention
			FROM precast 
			WHERE project_id = $1
			ORDER BY id
		`
		args = []interface{}{projectIDInt}
	} else {
		query = `
			SELECT id, project_id, name, description, parent_id, prefix, path, naming_convention
			FROM precast 
			ORDER BY project_id, id
		`
		args = []interface{}{}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching precast data"})
		return
	}
	defer rows.Close()

	// Write data rows
	for rows.Next() {
		var id, projectID int
		var name, description, prefix, path, namingConvention string
		var parentID *int

		if err := rows.Scan(&id, &projectID, &name, &description, &parentID, &prefix, &path, &namingConvention); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning precast data"})
			return
		}

		// Handle nullable ParentID
		parentIDStr := ""
		if parentID != nil {
			parentIDStr = strconv.Itoa(*parentID)
		}

		// Create row data
		row := []string{
			strconv.Itoa(id),
			strconv.Itoa(projectID),
			name,
			description,
			parentIDStr,
			prefix,
			path,
			namingConvention,
		}

		if err := writer.Write(row); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV row"})
			return
		}
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating precast data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Precast CSV export successful"})
}

// Request structure for the JSON body
type ExportElementTypeRequest struct {
	Hierarchy []HierarchyData `json:"hierarchy"`
	BOM       []BOMData       `json:"bom"`
}

type HierarchyData struct {
	HierarchyID int    `json:"hierarchy_id"`
	ParentID    int    `json:"parent_id"`
	Name        string `json:"name"`
	TowerName   string `json:"tower_name"`
}

type BOMData struct {
	BOMID   int    `json:"bom_id"`
	BOMName string `json:"bom_name"`
	BOMType string `json:"bom_type"`
}

// ExportExcellementType godoc
// @Summary      Export element type to Excel
// @Tags         export
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {file}   file  "Excel file"
// @Failure      400         {object}  object
// @Router       /api/export_excel_element_type/{project_id} [post]
func ExportExcellementType(c *gin.Context) {
	db := storage.GetDB()

	// Get project_id from route parameter
	projectID := c.Param("project_id")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	// Convert projectID to int
	projectIDInt, err := strconv.Atoi(projectID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	// Parse JSON request body
	var requestData ExportElementTypeRequest

	// Check if request has JSON content
	contentType := c.GetHeader("Content-Type")
	if contentType == "application/json" || strings.Contains(contentType, "application/json") {
		// Bind JSON data from Postman or other clients
		if err := c.ShouldBindJSON(&requestData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid JSON format",
				"details": err.Error(),
				"expected_format": map[string]interface{}{
					"hierarchy": []map[string]interface{}{
						{
							"hierarchy_id": "integer",
							"parent_id":    "integer",
							"name":         "string",
							"tower_name":   "string",
						},
					},
					"bom": []map[string]interface{}{
						{
							"bom_id":   "integer",
							"bom_name": "string",
							"bom_type": "string",
						},
					},
				},
			})
			return
		}
	} else {
		// If no JSON content type, initialize with empty data
		requestData.Hierarchy = []HierarchyData{}
		requestData.BOM = []BOMData{}
	}

	// Ensure arrays are initialized even if empty
	if requestData.Hierarchy == nil {
		requestData.Hierarchy = []HierarchyData{}
	}
	if requestData.BOM == nil {
		requestData.BOM = []BOMData{}
	}

	// Log parsed data for debugging (optional - remove in production)
	fmt.Printf("Parsed JSON Data - Hierarchy: %+v, BOM: %+v\n", requestData.Hierarchy, requestData.BOM)
	fmt.Printf("Hierarchy count: %d, BOM count: %d\n", len(requestData.Hierarchy), len(requestData.BOM))

	// Skip fetching element types - create empty template only
	// No database queries for element types

	// Get all drawing types for the project
	drawingTypesQuery := `
		SELECT drawing_type_name 
		FROM drawing_type 
		WHERE project_id = $1
		ORDER BY drawing_type_name
	`
	drawingTypeRows, err := db.Query(drawingTypesQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching drawing types"})
		return
	}
	defer drawingTypeRows.Close()

	var drawingTypes []string
	for drawingTypeRows.Next() {
		var drawingType string
		if err := drawingTypeRows.Scan(&drawingType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning drawing type"})
			return
		}
		drawingTypes = append(drawingTypes, drawingType)
	}

	// Group drawing types (for now, we'll treat each as individual, but can be grouped later if needed)
	drawingGroups := make(map[string][]string)
	drawingGroups["Drawing Types"] = drawingTypes

	// Group BOM data by bom_name (main heading) and bom_type (sub-heading)
	bomGroups := make(map[string][]string)
	for _, bom := range requestData.BOM {
		bomGroups[bom.BOMName] = append(bomGroups[bom.BOMName], bom.BOMType)
	}

	// If no BOM data provided in JSON, fetch default BOM data from database
	if len(bomGroups) == 0 {
		// Get all BOM products for the project
		bomQuery := `
			SELECT DISTINCT product_name, product_type
			FROM element_type_bom 
			WHERE project_id = $1
			ORDER BY product_name, product_type
		`
		bomRows, err := db.Query(bomQuery, projectIDInt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching BOM data"})
			return
		}
		defer bomRows.Close()

		// Group BOM data by product_name (main heading) and product_type (sub-heading)
		for bomRows.Next() {
			var productName, productType string
			if err := bomRows.Scan(&productName, &productType); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning BOM data"})
				return
			}
			bomGroups[productName] = append(bomGroups[productName], productType)
		}
	}

	// Get all paths from project_stages table for the project
	stageQuery := `
		SELECT name
FROM (
  SELECT name, "order"
  FROM project_stages
  WHERE project_id = $1
    AND name IS NOT NULL 
    AND name != ''
  ORDER BY "order"
) AS sub
GROUP BY name
ORDER BY MIN("order");
	`
	stageRows, err := db.Query(stageQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching paths"})
		return
	}
	defer stageRows.Close()

	var stages []string
	for stageRows.Next() {
		var stageName string
		if err := stageRows.Scan(&stageName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning stage"})
			return
		}
		stages = append(stages, stageName)
	}

	// Group paths (for now, we'll treat each as individual, but can be grouped later if needed)
	stageGroups := make(map[string][]string)
	stageGroups["Stages"] = stages

	// Group hierarchy data by tower_name (main heading) and name (sub-heading)
	towerGroups := make(map[string][]string)
	for _, hierarchy := range requestData.Hierarchy {
		towerGroups[hierarchy.TowerName] = append(towerGroups[hierarchy.TowerName], hierarchy.Name)
	}

	// If no hierarchy data provided in JSON, fetch default hierarchy data from database
	if len(towerGroups) == 0 {
		// Get all hierarchy data for the project
		hierarchyQuery := `
			SELECT DISTINCT tower_name, name
			FROM precast 
			WHERE project_id = $1 AND tower_name IS NOT NULL AND name IS NOT NULL
			ORDER BY tower_name, name
		`
		hierarchyRows, err := db.Query(hierarchyQuery, projectIDInt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching hierarchy data"})
			return
		}
		defer hierarchyRows.Close()

		// Group hierarchy data by tower_name (main heading) and name (sub-heading)
		for hierarchyRows.Next() {
			var towerName, name string
			if err := hierarchyRows.Scan(&towerName, &name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning hierarchy data"})
				return
			}
			towerGroups[towerName] = append(towerGroups[towerName], name)
		}
	}

	// Create stage names and tower mapping for merged headers
	var stageNames []string
	var towerHeaderRanges []struct {
		TowerName string
		StartCol  int
		EndCol    int
	}

	// Create drawing names and drawing mapping for merged headers
	var drawingTypesList []string
	var drawingHeaderRanges []struct {
		DrawingName string
		StartCol    int
		EndCol      int
	}

	// Create path names and path mapping for merged headers
	var pathTypesList []string
	var pathHeaderRanges []struct {
		PathName string
		StartCol int
		EndCol   int
	}

	// Create BOM names and BOM mapping for merged headers
	var bomTypes []string
	var bomHeaderRanges []struct {
		BOMName  string
		StartCol int
		EndCol   int
	}

	currentCol := 11 // Start after base columns A-J (11)

	// 1. Process drawing groups first (start after base columns)
	for drawingName, drawingTypesFromGroup := range drawingGroups {
		startCol := currentCol
		for _, drawingType := range drawingTypesFromGroup {
			drawingTypesList = append(drawingTypesList, drawingType)
			currentCol++
		}
		endCol := currentCol - 1
		drawingHeaderRanges = append(drawingHeaderRanges, struct {
			DrawingName string
			StartCol    int
			EndCol      int
		}{DrawingName: drawingName, StartCol: startCol, EndCol: endCol})
	}

	// 2. Process tower groups (stages from database)
	for towerName, floors := range towerGroups {
		startCol := currentCol
		for _, floor := range floors {
			stageNames = append(stageNames, floor)
			currentCol++
		}
		endCol := currentCol - 1
		towerHeaderRanges = append(towerHeaderRanges, struct {
			TowerName string
			StartCol  int
			EndCol    int
		}{TowerName: towerName, StartCol: startCol, EndCol: endCol})
	}

	// 3. Process hierarchy groups (hierarchy from JSON)
	for stageName, stageTypesFromGroup := range stageGroups {
		startCol := currentCol
		for _, pathType := range stageTypesFromGroup {
			pathTypesList = append(pathTypesList, pathType)
			currentCol++
		}
		endCol := currentCol - 1
		pathHeaderRanges = append(pathHeaderRanges, struct {
			PathName string
			StartCol int
			EndCol   int
		}{PathName: stageName, StartCol: startCol, EndCol: endCol})
	}

	// 4. Process BOM groups (BOM from JSON)
	for bomName, bomTypesList := range bomGroups {
		startCol := currentCol
		for _, bomType := range bomTypesList {
			bomTypes = append(bomTypes, bomType)
			currentCol++
		}
		endCol := currentCol - 1
		bomHeaderRanges = append(bomHeaderRanges, struct {
			BOMName  string
			StartCol int
			EndCol   int
		}{BOMName: bomName, StartCol: startCol, EndCol: endCol})
	}

	// Debug logging for column creation
	fmt.Printf("Tower Groups: %+v\n", towerGroups)
	fmt.Printf("Stage Names: %+v\n", stageNames)
	fmt.Printf("BOM Groups: %+v\n", bomGroups)
	fmt.Printf("BOM Types: %+v\n", bomTypes)
	fmt.Printf("Drawing Groups: %+v\n", drawingGroups)
	fmt.Printf("Drawing Types List: %+v\n", drawingTypesList)
	fmt.Printf("Path Groups: %+v\n", stageGroups)
	fmt.Printf("Path Types List: %+v\n", pathTypesList)

	// Create a new Excel file
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error closing Excel file"})
		}
	}()

	// Create Summary Sheet
	summarySheet := "Summary"
	index, err := f.NewSheet(summarySheet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating summary sheet"})
		return
	}
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1") // Delete default sheet
	var projectName string
	if err := db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectIDInt).Scan(&projectName); err != nil {
		projectName = "project"
	}
	// Add summary information
	f.SetCellValue(summarySheet, "A1", "Element Type Export Summary")
	f.SetCellValue(summarySheet, "A2", "Project ID")
	f.SetCellValue(summarySheet, "B2", projectID)
	f.SetCellValue(summarySheet, "A3", "Project Name")
	f.SetCellValue(summarySheet, "B3", projectName)
	// Use the same logic as Column Layout Reference section
	// 1. Drawing types range (start after base columns)
	drawingStartCol := 11 // K
	drawingEndCol := 10 + len(drawingTypesList)
	drawingStartCell, _ := excelize.CoordinatesToCellName(drawingStartCol, 1)
	drawingEndCell, _ := excelize.CoordinatesToCellName(drawingEndCol, 1)
	f.SetCellValue(summarySheet, "A4", "Total Drawing Types") // For drawings
	f.SetCellValue(summarySheet, "B4", len(drawingTypesList))
	f.SetCellValue(summarySheet, "C4", fmt.Sprintf("Range: %s-%s", drawingStartCell, drawingEndCell))

	// 2. Stages range (after drawings)
	stageStartCol := drawingEndCol + 1
	stageEndCol := stageStartCol + len(stageNames) - 1
	stageStartCell, _ := excelize.CoordinatesToCellName(stageStartCol, 1)
	stageEndCell, _ := excelize.CoordinatesToCellName(stageEndCol, 1)
	f.SetCellValue(summarySheet, "A5", "Total Hierarchy") //For hierarchy
	f.SetCellValue(summarySheet, "B5", len(stageNames))
	f.SetCellValue(summarySheet, "C5", fmt.Sprintf("Range: %s-%s", stageStartCell, stageEndCell))

	// 3. Hierarchy range (after stages)
	pathStartCol := stageEndCol + 1
	pathEndCol := pathStartCol + len(pathTypesList) - 1
	pathStartCell, _ := excelize.CoordinatesToCellName(pathStartCol, 1)
	pathEndCell, _ := excelize.CoordinatesToCellName(pathEndCol, 1)
	f.SetCellValue(summarySheet, "A6", "Total Stages") // For stages
	f.SetCellValue(summarySheet, "B6", len(pathTypesList))
	f.SetCellValue(summarySheet, "C6", fmt.Sprintf("Range: %s-%s", pathStartCell, pathEndCell))

	// 4. BOM types range (after hierarchy)
	bomStartCol := pathEndCol + 1
	bomEndCol := bomStartCol + len(bomTypes) - 1
	bomStartCell, _ := excelize.CoordinatesToCellName(bomStartCol, 1)
	bomEndCell, _ := excelize.CoordinatesToCellName(bomEndCol, 1)
	f.SetCellValue(summarySheet, "A7", "Total BOM Types") // For BOM
	f.SetCellValue(summarySheet, "B7", len(bomTypes))
	f.SetCellValue(summarySheet, "C7", fmt.Sprintf("Range: %s-%s", bomStartCell, bomEndCell))

	// 5. Base columns (always A1-J1)
	baseStartCell, _ := excelize.CoordinatesToCellName(1, 1) // A1
	baseEndCell, _ := excelize.CoordinatesToCellName(10, 1)  // J1 for 10 base columns
	f.SetCellValue(summarySheet, "A8", "Base Columns")       // For fixed columns element type
	f.SetCellValue(summarySheet, "B8", "10")
	f.SetCellValue(summarySheet, "C8", fmt.Sprintf("Range: %s-%s", baseStartCell, baseEndCell))

	// Add Column Layout Reference Section with dynamic ranges
	f.SetCellValue(summarySheet, "A10", "Column Layout Reference:")

	// Base columns (always A1-J1)
	f.SetCellValue(summarySheet, "A11", "A1-J1: Base Columns (10)")

	// Use actual column positions from column processing
	// 1. Drawing types range (K1 or beyond based on actual count)
	drawingStartCol = 11 // K (after base columns A1-J1)
	drawingEndCol = 10 + len(drawingTypesList)
	drawingStartCell, _ = excelize.CoordinatesToCellName(drawingStartCol, 1)
	drawingEndCell, _ = excelize.CoordinatesToCellName(drawingEndCol, 1)
	f.SetCellValue(summarySheet, "A12", fmt.Sprintf("%s-%s: Drawing Types (%d)", drawingStartCell, drawingEndCell, len(drawingTypesList)))

	// 2. Stages range (after drawings)
	stageStartCol = drawingEndCol + 1
	stageEndCol = stageStartCol + len(stageNames) - 1
	stageStartCell, _ = excelize.CoordinatesToCellName(stageStartCol, 1)
	stageEndCell, _ = excelize.CoordinatesToCellName(stageEndCol, 1)
	f.SetCellValue(summarySheet, "A13", fmt.Sprintf("%s-%s: Stages (%d)", stageStartCell, stageEndCell, len(stageNames)))

	// 3. Hierarchy range (after stages)
	pathStartCol = stageEndCol + 1
	pathEndCol = pathStartCol + len(pathTypesList) - 1
	pathStartCell, _ = excelize.CoordinatesToCellName(pathStartCol, 1)
	pathEndCell, _ = excelize.CoordinatesToCellName(pathEndCol, 1)
	f.SetCellValue(summarySheet, "A14", fmt.Sprintf("%s-%s: Hierarchy (%d)", pathStartCell, pathEndCell, len(pathTypesList)))

	// 4. BOM types range (after hierarchy)
	bomStartCol = pathEndCol + 1
	bomEndCol = bomStartCol + len(bomTypes) - 1
	bomStartCell, _ = excelize.CoordinatesToCellName(bomStartCol, 1)
	bomEndCell, _ = excelize.CoordinatesToCellName(bomEndCol, 1)
	f.SetCellValue(summarySheet, "A15", fmt.Sprintf("%s-%s: BOM Types (%d)", bomStartCell, bomEndCell, len(bomTypes)))

	// Style summary sheet
	titleStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   14,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#4472C4"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating title style"})
		return
	}

	baseStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#4472C4"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating base style"})
		return
	}

	stageStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#70AD47"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage style"})
		return
	}

	drawingStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#ED7D31"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing style"})
		return
	}

	pathStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#5B9BD5"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path style"})
		return
	}

	productStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#9E480E"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating product style"})
		return
	}

	// Apply styles to summary sheet
	f.SetCellStyle(summarySheet, "A1", "C1", titleStyle)
	f.SetCellStyle(summarySheet, "A2", "B2", baseStyle)
	f.SetCellStyle(summarySheet, "A3", "C3", drawingStyle)
	f.SetCellStyle(summarySheet, "A4", "C4", drawingStyle)
	f.SetCellStyle(summarySheet, "A5", "C5", productStyle)
	f.SetCellStyle(summarySheet, "A6", "C6", pathStyle)
	f.SetCellStyle(summarySheet, "A7", "C7", stageStyle)
	f.SetCellStyle(summarySheet, "A8", "C8", baseStyle)

	// Style the reference sections
	f.SetCellStyle(summarySheet, "A10", "A10", titleStyle) // Column Layout Reference title
	f.SetCellStyle(summarySheet, "A17", "A17", titleStyle) // Dynamic Example title

	// Set column widths for summary sheet
	f.SetColWidth(summarySheet, "A", "A", 25)
	f.SetColWidth(summarySheet, "B", "B", 20)
	f.SetColWidth(summarySheet, "C", "C", 35)

	// Set row heights for summary sheet
	f.SetRowHeight(summarySheet, 1, 30) // Title row
	for i := 2; i <= 25; i++ {
		f.SetRowHeight(summarySheet, i, 22) // Data rows including reference section
	}

	// Create main data sheet
	sheetName := "Element Types"
	index, err = f.NewSheet(sheetName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating sheet"})
		return
	}
	f.SetActiveSheet(index)

	// Create header with base columns
	baseHeaders := []string{
		"Element Type",
		"Element Type Name",
		"Height",
		"Length",
		"Thickness",
		"Mass",
		"Volume",
		"Area",
		"Width",
		"Element Type Version",
	}

	// Write base headers in row 1
	for i, col := range baseHeaders {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, col)
	}

	// 1. Write drawing type headers (merged) in row 1 - grouped by "Drawing Types"
	for _, drawingRange := range drawingHeaderRanges {
		startCell, err := excelize.CoordinatesToCellName(drawingRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing start cell name"})
			return
		}
		endCell, err := excelize.CoordinatesToCellName(drawingRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing end cell name"})
			return
		}
		f.SetCellValue(sheetName, startCell, drawingRange.DrawingName)
		f.MergeCell(sheetName, startCell, endCell)
	}

	// 2. Write tower headers (merged) in row 1 - stages from database
	for _, towerRange := range towerHeaderRanges {
		startCell, err := excelize.CoordinatesToCellName(towerRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating start cell name"})
			return
		}
		endCell, err := excelize.CoordinatesToCellName(towerRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating end cell name"})
			return
		}
		f.SetCellValue(sheetName, startCell, towerRange.TowerName)
		f.MergeCell(sheetName, startCell, endCell)
	}

	// 3. Write path headers (merged) in row 1 - hierarchy from JSON
	for _, pathRange := range pathHeaderRanges {
		startCell, err := excelize.CoordinatesToCellName(pathRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path start cell name"})
			return
		}
		endCell, err := excelize.CoordinatesToCellName(pathRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path end cell name"})
			return
		}
		f.SetCellValue(sheetName, startCell, pathRange.PathName)
		f.MergeCell(sheetName, startCell, endCell)
	}

	// 4. Write BOM headers (merged) in row 1 - BOM from JSON
	for _, bomRange := range bomHeaderRanges {
		startCell, err := excelize.CoordinatesToCellName(bomRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM start cell name"})
			return
		}
		endCell, err := excelize.CoordinatesToCellName(bomRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM end cell name"})
			return
		}
		f.SetCellValue(sheetName, startCell, bomRange.BOMName)
		f.MergeCell(sheetName, startCell, endCell)
	}

	// Write sub-headers in row 2
	// Base columns (empty sub-headers)
	for i := 1; i <= 10; i++ {
		cell, err := excelize.CoordinatesToCellName(i, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating base sub-header cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, "")
	}

	// 1. Drawing type sub-headers (start after base columns)
	for i, drawingType := range drawingTypesList {
		cell, err := excelize.CoordinatesToCellName(11+i, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing sub-header cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, drawingType)
	}

	// 2. Stage sub-headers - stages from database
	drawingStartCol = 11 + len(drawingTypesList)
	for i, floorName := range stageNames {
		cell, err := excelize.CoordinatesToCellName(drawingStartCol+i, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage sub-header cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, floorName)
	}

	// 3. Hierarchy sub-headers - hierarchy from JSON
	stageStartCol = drawingStartCol + len(stageNames)
	for i, pathType := range pathTypesList {
		cell, err := excelize.CoordinatesToCellName(stageStartCol+i, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating hierarchy sub-header cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, pathType)
	}

	// 4. BOM type sub-headers - BOM from JSON
	pathStartCol = stageStartCol + len(pathTypesList)
	for i, bomType := range bomTypes {
		cell, err := excelize.CoordinatesToCellName(pathStartCol+i, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM sub-header cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, bomType)
	}

	// Create different styles for different sections
	baseHeaderStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#4472C4"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating base header style"})
		return
	}

	stageHeaderStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#70AD47"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage header style"})
		return
	}

	drawingHeaderStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#ED7D31"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing header style"})
		return
	}

	pathHeaderStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#5B9BD5"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path header style"})
		return
	}

	productHeaderStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Family: "Arial",
			Color:  "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#9E480E"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating product header style"})
		return
	}

	// Apply styles to different sections - Row 1 (Main headers)
	// Base columns (1-10) - Row 1
	startCell, err := excelize.CoordinatesToCellName(1, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating start cell name"})
		return
	}
	endCell, err := excelize.CoordinatesToCellName(10, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating end cell name"})
		return
	}
	f.SetCellStyle(sheetName, startCell, endCell, baseHeaderStyle)

	// Tower headers (merged) - Row 1
	for _, towerRange := range towerHeaderRanges {
		startCell, err = excelize.CoordinatesToCellName(towerRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating tower start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(towerRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating tower end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, stageHeaderStyle)
	}

	// Drawing type headers (merged) - Row 1
	for _, drawingRange := range drawingHeaderRanges {
		startCell, err = excelize.CoordinatesToCellName(drawingRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(drawingRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, drawingHeaderStyle)
	}

	// Path headers (merged) - Row 1
	for _, pathRange := range pathHeaderRanges {
		startCell, err = excelize.CoordinatesToCellName(pathRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(pathRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating path end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, pathHeaderStyle)
	}

	// BOM headers (merged) - Row 1 - grouped by BOM name
	for _, bomRange := range bomHeaderRanges {
		startCell, err = excelize.CoordinatesToCellName(bomRange.StartCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(bomRange.EndCol, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, productHeaderStyle)
	}

	// Apply styles to Row 2 (Sub-headers)
	// Base columns (1-10) - Row 2 (empty sub-headers)
	startCell, err = excelize.CoordinatesToCellName(1, 2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating base sub-header start cell name"})
		return
	}
	endCell, err = excelize.CoordinatesToCellName(10, 2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating base sub-header end cell name"})
		return
	}
	f.SetCellStyle(sheetName, startCell, endCell, baseHeaderStyle)

	// 1. Drawing type sub-headers - Row 2 (start after base columns)
	if len(drawingTypesList) > 0 {
		startCell, err = excelize.CoordinatesToCellName(11, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing sub-header start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(10+len(drawingTypesList), 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing sub-header end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, drawingHeaderStyle)
	}

	// 2. Stage sub-headers - Row 2
	if len(stageNames) > 0 {
		drawingStartCol = 11 + len(drawingTypesList)
		startCell, err = excelize.CoordinatesToCellName(drawingStartCol, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage sub-header start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(drawingStartCol+len(stageNames)-1, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage sub-header end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, stageHeaderStyle)
	}

	// 3. Hierarchy sub-headers - Row 2
	if len(pathTypesList) > 0 {
		stageStartCol = drawingStartCol + len(stageNames)
		startCell, err = excelize.CoordinatesToCellName(stageStartCol, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating hierarchy sub-header start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(stageStartCol+len(pathTypesList)-1, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating hierarchy sub-header end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, pathHeaderStyle)
	}

	// 4. BOM sub-headers - Row 2
	if len(bomTypes) > 0 {
		pathStartCol = stageStartCol + len(pathTypesList)
		startCell, err = excelize.CoordinatesToCellName(pathStartCol, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM sub-header start cell name"})
			return
		}
		endCell, err = excelize.CoordinatesToCellName(pathStartCol+len(bomTypes)-1, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM sub-header end cell name"})
			return
		}
		f.SetCellStyle(sheetName, startCell, endCell, productHeaderStyle)
	}

	// Set default column widths to 20 units for all columns
	totalColumns := 10 + len(drawingTypesList) + len(stageNames) + len(pathTypesList) + len(bomTypes)
	for i := 1; i <= totalColumns; i++ {
		col, err := excelize.CoordinatesToCellName(i, 1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating column name"})
			return
		}
		// Set all columns to default width of 20 units
		f.SetColWidth(sheetName, col, col, 25)
	}

	// Set default row heights
	// Set header rows (1-2) to be taller
	f.SetRowHeight(sheetName, 1, 25) // Main headers
	f.SetRowHeight(sheetName, 2, 20) // Sub-headers

	// Set default height for data rows (starting from row 3)
	for i := 3; i <= 1000; i++ { // Set for first 1000 rows
		f.SetRowHeight(sheetName, i, 18)
	}

	// Make top 3 rows non-editable: unlock rows 4+ and protect the sheet
	unlockedStyle, err := f.NewStyle(&excelize.Style{Protection: &excelize.Protection{Locked: false}})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating unlocked style"})
		return
	}
	unlockStartCell, err := excelize.CoordinatesToCellName(1, 4) // A4
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating unlock start cell name"})
		return
	}
	unlockEndCell, err := excelize.CoordinatesToCellName(totalColumns, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating unlock end cell name"})
		return
	}
	if err := f.SetCellStyle(sheetName, unlockStartCell, unlockEndCell, unlockedStyle); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error applying unlocked style"})
		return
	}
	if err := f.ProtectSheet(sheetName, &excelize.SheetProtectionOptions{
		SelectLockedCells:   true,
		SelectUnlockedCells: true,
		FormatCells:         false,
		FormatColumns:       true, // Allow column width changes
		FormatRows:          false,
		InsertColumns:       false,
		InsertRows:          false,
		DeleteColumns:       false,
		DeleteRows:          false,
		Sort:                false,
		AutoFilter:          true,
		PivotTables:         false,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error protecting sheet"})
		return
	}

	// Add default data in one row to demonstrate the structure
	row := 3 // Start from row 3 (after headers)

	// Base columns data (10 columns)
	f.SetCellValue(sheetName, "A3", "ET001")       // Element Type
	f.SetCellValue(sheetName, "B3", "Beam Type A") // Element Type Name
	f.SetCellValue(sheetName, "C3", "300")         // Height
	f.SetCellValue(sheetName, "D3", "2500")        // Length
	f.SetCellValue(sheetName, "E3", "6000")        // Thickness
	f.SetCellValue(sheetName, "F3", "2400")        // Mass
	f.SetCellValue(sheetName, "G3", "1.50")        // Volume
	f.SetCellValue(sheetName, "H3", "5.25")        // Area
	f.SetCellValue(sheetName, "I3", "200")         // Width
	f.SetCellValue(sheetName, "J3", "V1.0")        // Element Type Version

	// Drawing types data (start after base)
	drawingCol := 11 // Start from column K
	for i, drawingType := range drawingTypesList {
		cell, err := excelize.CoordinatesToCellName(drawingCol+i, row)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating drawing cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, fmt.Sprintf("Drw_%s", drawingType))
	}

	// Stages data
	stageCol := 11 + len(drawingTypesList) // Start after drawings
	for i, stageName := range stageNames {
		cell, err := excelize.CoordinatesToCellName(stageCol+i, row)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating stage cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, fmt.Sprintf("Qty_%s", stageName))
	}

	// Hierarchy data
	hierarchyCol := 11 + len(drawingTypesList) + len(stageNames) // Start after stages
	for i, pathType := range pathTypesList {
		cell, err := excelize.CoordinatesToCellName(hierarchyCol+i, row)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating hierarchy cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, fmt.Sprintf("Hierarchy_%s", pathType))
	}

	// BOM types data
	bomCol := 11 + len(drawingTypesList) + len(stageNames) + len(pathTypesList) // Start after hierarchy
	for i, bomType := range bomTypes {
		cell, err := excelize.CoordinatesToCellName(bomCol+i, row)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating BOM cell name"})
			return
		}
		f.SetCellValue(sheetName, cell, fmt.Sprintf("BOM_%s", bomType))
	}

	// Set the response headers for Excel file download
	// Build filename with project name and id (sanitize and support UTF-8)
	safeProjectName := projectName
	safeProjectName = strings.TrimSpace(safeProjectName)
	safeProjectName = strings.ReplaceAll(safeProjectName, " ", "_")
	safeProjectName = strings.ReplaceAll(safeProjectName, "/", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "\\", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, ":", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "*", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "?", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "\"", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "<", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, ">", "-")
	safeProjectName = strings.ReplaceAll(safeProjectName, "|", "-")

	filename := fmt.Sprintf("element_type_export_%s_%d.xlsx", safeProjectName, projectIDInt)
	escaped := url.PathEscape(filename)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", filename, escaped))

	// Write the Excel file to the response
	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing Excel file"})
		return
	}
}

// // ExportCSVElementTypePreField exports element types with pre-field data to CSV format
// func ExportCSVElementTypePreField(c *gin.Context) {
// 	db := storage.GetDB()

// 	projectID := c.Param("project_id")
// 	if projectID == "" {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
// 		return
// 	}
// 	projectIDInt, err := strconv.Atoi(projectID)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
// 		return
// 	}

// 	// Fetch drawing types
// 	drawingTypes := []string{}
// 	dtRows, err := db.Query(`SELECT drawing_type_name FROM drawing_type WHERE project_id = $1 ORDER BY drawing_type_name`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching drawing types",
// 			"details":    err.Error(),
// 			"operation":  "fetch_drawing_types",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer dtRows.Close()
// 	for dtRows.Next() {
// 		var dt string
// 		if err := dtRows.Scan(&dt); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":      "Error scanning drawing type",
// 				"details":    err.Error(),
// 				"operation":  "scan_drawing_type",
// 				"project_id": projectIDInt,
// 			})
// 			return
// 		}
// 		drawingTypes = append(drawingTypes, dt)
// 	}

// 	// Fetch BOM product names
// 	bomProducts := []string{}
// 	bomRows, err := db.Query(`SELECT name_id FROM inv_bom WHERE project_id = $1`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching BOM products",
// 			"details":    err.Error(),
// 			"operation":  "fetch_bom_products",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer bomRows.Close()
// 	for bomRows.Next() {
// 		var name string
// 		if err := bomRows.Scan(&name); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":      "Error scanning BOM product",
// 				"details":    err.Error(),
// 				"operation":  "scan_bom_product",
// 				"project_id": projectIDInt,
// 			})
// 			return
// 		}
// 		bomProducts = append(bomProducts, name)
// 	}

// 	// Fetch paths
// 	paths := []string{}
// 	pathRows, err := db.Query(`SELECT DISTINCT path FROM precast WHERE project_id = $1 AND path IS NOT NULL AND path != '' ORDER BY path`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching paths",
// 			"details":    err.Error(),
// 			"operation":  "fetch_paths",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer pathRows.Close()
// 	for pathRows.Next() {
// 		var path string
// 		if err := pathRows.Scan(&path); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":      "Error scanning path",
// 				"details":    err.Error(),
// 				"operation":  "scan_path",
// 				"project_id": projectIDInt,
// 			})
// 			return
// 		}
// 		paths = append(paths, path)
// 	}

// 	// Fetch stage names
// 	stageNames := []string{}
// 	stageRows, err := db.Query(`SELECT name FROM project_stages WHERE project_id = $1 ORDER BY "order"`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching stage names",
// 			"details":    err.Error(),
// 			"operation":  "fetch_stage_names",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer stageRows.Close()
// 	for stageRows.Next() {
// 		var s string
// 		if err := stageRows.Scan(&s); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":      "Error scanning stage name",
// 				"details":    err.Error(),
// 				"operation":  "scan_stage_name",
// 				"project_id": projectIDInt,
// 			})
// 			return
// 		}
// 		stageNames = append(stageNames, s)
// 	}

// 	// Setup CSV
// 	c.Header("Content-Type", "text/csv")
// 	c.Header("Content-Disposition", "attachment;filename=element_type_export.csv")

// 	writer := csv.NewWriter(c.Writer)
// 	defer writer.Flush()

// 	// Header
// 	header := []string{
// 		"Element Type",
// 		"Element Type Name",
// 		"Height",
// 		"Weight",
// 		"Length",
// 		"Thickness",
// 		"Project ID",
// 		"Element Type Version",
// 	}
// 	header = append(header, stageNames...)
// 	header = append(header, drawingTypes...)
// 	header = append(header, paths...)
// 	header = append(header, bomProducts...)

// 	if err := writer.Write(header); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error writing CSV header",
// 			"details":    err.Error(),
// 			"operation":  "write_csv_header",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}

// 	// Pre-fetch all drawings for this project into a map
// 	drawingMap := make(map[string]map[string]bool)
// 	drawingRows, err := db.Query(`SELECT element_type, drawing_type FROM drawings WHERE project_id = $1`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching drawings",
// 			"details":    err.Error(),
// 			"operation":  "fetch_drawings",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer drawingRows.Close()
// 	for drawingRows.Next() {
// 		var et, dt string
// 		if err := drawingRows.Scan(&et, &dt); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":      "Error scanning drawing data",
// 				"details":    err.Error(),
// 				"operation":  "scan_drawing_data",
// 				"project_id": projectIDInt,
// 			})
// 			return
// 		}
// 		if drawingMap[et] == nil {
// 			drawingMap[et] = make(map[string]bool)
// 		}
// 		drawingMap[et][dt] = true
// 	}

// 	// Fetch all element types
// 	elemRows, err := db.Query(`
// 		SELECT element_type, element_type_name, height, weight, length, thickness, project_id, version, hierarchy
// 		FROM element_type
// 		WHERE project_id = $1`, projectIDInt)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{
// 			"error":      "Error fetching element types",
// 			"details":    err.Error(),
// 			"operation":  "fetch_element_types",
// 			"project_id": projectIDInt,
// 		})
// 		return
// 	}
// 	defer elemRows.Close()

// 	for elemRows.Next() {
// 		var (
// 			et, etName, version, hierarchyJSON string
// 			height, weight, length, thickness  float64
// 			projID                             int
// 		)
// 		if err := elemRows.Scan(&et, &etName, &height, &weight, &length, &thickness, &projID, &version, &hierarchyJSON); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":        "Error scanning element type data",
// 				"details":      err.Error(),
// 				"operation":    "scan_element_type_data",
// 				"project_id":   projectIDInt,
// 				"element_type": et,
// 			})
// 			return
// 		}

// 		// Parse hierarchy JSON (paths)
// 		var pathList []string
// 		if err := json.Unmarshal([]byte(hierarchyJSON), &pathList); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":        "Error parsing hierarchy JSON",
// 				"details":      err.Error(),
// 				"operation":    "parse_hierarchy_json",
// 				"project_id":   projectIDInt,
// 				"element_type": et,
// 			})
// 			return
// 		}
// 		pathSet := make(map[string]bool)
// 		for _, p := range pathList {
// 			pathSet[p] = true
// 		}

// 		record := []string{
// 			et,
// 			etName,
// 			fmt.Sprintf("%.2f", height),
// 			fmt.Sprintf("%.2f", weight),
// 			fmt.Sprintf("%.2f", length),
// 			fmt.Sprintf("%.2f", thickness),
// 			strconv.Itoa(projID),
// 			version,
// 		}

// 		// Stages — placeholder empty (or pull if stored)
// 		for range stageNames {
// 			record = append(record, "") // Or fetch actual values if stored
// 		}

// 		// Drawing type flags
// 		for _, dt := range drawingTypes {
// 			if drawingMap[et][dt] {
// 				record = append(record, "Yes")
// 			} else {
// 				record = append(record, "")
// 			}
// 		}

// 		// Paths
// 		for _, path := range paths {
// 			if pathSet[path] {
// 				record = append(record, "Yes")
// 			} else {
// 				record = append(record, "")
// 			}
// 		}

// 		// BOM Products - Add empty values since we don't have BOM data anymore
// 		for range bomProducts {
// 			record = append(record, "")
// 		}

// 		if err := writer.Write(record); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":        "Error writing CSV record",
// 				"details":      err.Error(),
// 				"operation":    "write_csv_record",
// 				"project_id":   projectIDInt,
// 				"element_type": et,
// 			})
// 			return
// 		}
// 	}
// }

// ExportBlankCSVElementType exports a blank CSV file with headers for element types
func ExportCSVElementType(c *gin.Context) {
	db := storage.GetDB()

	// Get project_id from query parameter
	projectID := c.Param("project_id")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	// Convert projectID to int
	projectIDInt, err := strconv.Atoi(projectID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	// Get all drawing types for the project
	drawingTypesQuery := `
		SELECT drawing_type_name 
		FROM drawing_type 
		WHERE project_id = $1
		ORDER BY drawing_type_name
	`
	drawingTypeRows, err := db.Query(drawingTypesQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching drawing types"})
		return
	}
	defer drawingTypeRows.Close()

	var drawingTypes []string
	for drawingTypeRows.Next() {
		var drawingType string
		if err := drawingTypeRows.Scan(&drawingType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning drawing type"})
			return
		}
		drawingTypes = append(drawingTypes, drawingType)
	}

	// Get all BOM products for the project
	bomQuery := `
		SELECT name_id 
		FROM inv_bom 
		WHERE project_id = $1
	`
	bomRows, err := db.Query(bomQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching BOM products"})
		return
	}
	defer bomRows.Close()

	var bomProducts []string
	for bomRows.Next() {
		var productName string
		if err := bomRows.Scan(&productName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning BOM product"})
			return
		}
		bomProducts = append(bomProducts, productName)
	}

	// Get all paths from precast table for the project
	pathQuery := `
		SELECT DISTINCT path 
		FROM precast 
		WHERE project_id = $1 AND path IS NOT NULL AND path != ''
		ORDER BY path
	`
	pathRows, err := db.Query(pathQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching paths"})
		return
	}
	defer pathRows.Close()

	var paths []string
	for pathRows.Next() {
		var path string
		if err := pathRows.Scan(&path); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning path"})
			return
		}
		paths = append(paths, path)
	}

	// Get all stage names from project_stages table
	stageQuery := `
		SELECT name 
		FROM project_stages 
		WHERE project_id = $1
		ORDER BY "order"
	`
	stageRows, err := db.Query(stageQuery, projectIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching stage names"})
		return
	}
	defer stageRows.Close()

	var stageNames []string
	for stageRows.Next() {
		var stageName string
		if err := stageRows.Scan(&stageName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning stage name"})
			return
		}
		stageNames = append(stageNames, stageName)
	}

	// Set up CSV writer
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=blank_element_type_export.csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Create header with base columns
	header := []string{
		"Element Type",
		"Element Type Name",
		"Height",
		"Length",
		"Thickness",
		"Mass",
		"Volume",
		"Area",
		"Width",
		"Element Type Version",
	}

	// Add stage names as columns
	header = append(header, stageNames...)

	// Add drawing type columns, paths, and BOM product columns
	header = append(header, drawingTypes...)
	header = append(header, paths...)
	header = append(header, bomProducts...)

	// Write header row
	if err := writer.Write(header); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error writing CSV header"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Element Type CSV export successful"})
}

// ExportElementTypeExcelHandler handles the export of element types to Excel format

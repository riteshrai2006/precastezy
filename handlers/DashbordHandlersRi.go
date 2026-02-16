package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// MaterialType defines the type of material to calculate
type MaterialType string

const (
	MaterialTypeConcrete MaterialType = "concrete"
	MaterialTypeSteel    MaterialType = "steel"
)

// getMeshMouldStageID gets the Mesh & Mould stage ID for a project
func getMeshMouldStageID(db *sql.DB, projectID int) (int, error) {
	var stageID int
	err := db.QueryRow(`
		SELECT id FROM project_stages 
		WHERE project_id = $1 AND name ILIKE '%Mesh%Mould%'
	`, projectID).Scan(&stageID)
	if err != nil {
		return 0, fmt.Errorf("failed to get Mesh & Mould stage ID: %v", err)
	}
	return stageID, nil
}

// calculateMaterialUsageByGrade calculates material usage by grade for a specific date range
func calculateMaterialUsageByGrade(db *sql.DB, projectID int, startDate, endDate time.Time, materialType MaterialType) (map[string]float64, error) {
	// For concrete, calculate based on element volumes and production data
	if materialType == MaterialTypeConcrete {
		return calculateConcreteUsageByVolume(db, projectID, startDate, endDate)
	}

	// For steel, use the original BOM-based approach
	return calculateSteelUsageByBOM(db, projectID, startDate, endDate)
}

// getCompletedElementsCount gets the count of elements that completed Mesh & Mould stage in a specific date range
func getCompletedElementsCount(db *sql.DB, projectID int, startDate, endDate time.Time) (int, error) {
	// First, get the Mesh & Mould stage ID
	meshMouldStageID, err := getMeshMouldStageID(db, projectID)
	if err != nil {
		log.Printf("WARNING: Mesh & Mould stage not found for project %d: %v. Using alternative approach.", projectID, err)
		// Try to get any stage ID for this project as fallback
		var fallbackStageID int
		fallbackErr := db.QueryRow(`
			SELECT id FROM project_stages 
			WHERE project_id = $1 
			ORDER BY id LIMIT 1
		`, projectID).Scan(&fallbackStageID)
		if fallbackErr != nil {
			log.Printf("No stages found for project %d: %v", projectID, fallbackErr)
			return 0, fmt.Errorf("no stages found for project: %v", fallbackErr)
		}
		meshMouldStageID = fallbackStageID
		log.Printf("DEBUG: Using fallback stage ID: %d for element completion count in project %d", meshMouldStageID, projectID)
	} else {
		log.Printf("DEBUG: Using Mesh & Mould stage ID: %d for element completion count in project %d", meshMouldStageID, projectID)
	}

	// Count elements that completed Mesh & Mould stage in the date range
	query := `
		SELECT COUNT(DISTINCT cp.element_id) 
		FROM complete_production cp
		WHERE cp.project_id = $1 
			AND cp.stage_id = $2
			AND cp.started_at >= $3 
			AND cp.started_at <= $4
	`

	var count int
	err = db.QueryRow(query, projectID, meshMouldStageID, startDate, endDate).Scan(&count)
	if err != nil {
		log.Printf("Error counting completed elements in Mesh & Mould stage: %v", err)
		return 0, err
	}

	log.Printf("DEBUG: Found %d elements that completed Mesh & Mould stage for project %d between %s and %s", count, projectID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	return count, nil
}

// calculateConcreteUsageByVolume calculates concrete usage based on elements that completed Mesh & Mould stage
func calculateConcreteUsageByVolume(db *sql.DB, projectID int, startDate, endDate time.Time) (map[string]float64, error) {
	// First, get the count of elements that completed Mesh & Mould stage in this period
	completedElements, err := getCompletedElementsCount(db, projectID, startDate, endDate)
	if err != nil {
		log.Printf("Error getting completed elements count: %v", err)
		return map[string]float64{"M-40": 0.0, "M-60": 0.0}, err
	}

	// If no elements completed Mesh & Mould stage, return zero usage
	if completedElements == 0 {
		log.Printf("No elements completed Mesh & Mould stage in this period, returning zero usage")
		return map[string]float64{"M-40": 0.0, "M-60": 0.0}, nil
	}

	// Get concrete usage per element from BOM data
	concreteQuery := `
		SELECT 
			etb.product_name AS material_name,
			AVG(etb.quantity) AS avg_quantity_per_element
		FROM element_type_bom etb
		WHERE etb.project_id = $1 
			AND (
				etb.product_name ILIKE '%concrete%' 
				OR etb.product_name ILIKE '%cement%'
				OR etb.product_name ILIKE '%M10%'
				OR etb.product_name ILIKE '%M15%'
				OR etb.product_name ILIKE '%M20%'
				OR etb.product_name ILIKE '%M25%'
				OR etb.product_name ILIKE '%M30%'
				OR etb.product_name ILIKE '%M35%'
				OR etb.product_name ILIKE '%M40%'
				OR etb.product_name ILIKE '%M45%'
				OR etb.product_name ILIKE '%M50%'
				OR etb.product_name ILIKE '%M60%'
				OR etb.product_name ILIKE '%M80%'
				OR etb.product_name ILIKE '%grade%'
				OR etb.product_name ILIKE '%mix%'
			)
		GROUP BY etb.product_name
		ORDER BY etb.product_name
	`

	rows, err := db.Query(concreteQuery, projectID)
	if err != nil {
		log.Printf("Error querying concrete BOM data: %v", err)
		return map[string]float64{"M-40": 0.0, "M-60": 0.0}, err
	}
	defer rows.Close()

	usageByGrade := make(map[string]float64)
	rowCount := 0

	for rows.Next() {
		var materialName string
		var avgQuantityPerElement float64
		if err := rows.Scan(&materialName, &avgQuantityPerElement); err != nil {
			log.Printf("Error scanning concrete BOM row: %v", err)
			continue
		}

		// Calculate total usage: average per element * number of completed elements
		totalUsage := avgQuantityPerElement * float64(completedElements)
		usageByGrade[materialName] = totalUsage
		rowCount++

		log.Printf("DEBUG: Concrete material - Name: %s, Avg per element: %f, Completed elements: %d, Total usage: %f",
			materialName, avgQuantityPerElement, completedElements, totalUsage)
	}

	log.Printf("DEBUG: Found %d concrete materials from BOM, calculated usage for %d completed elements", rowCount, completedElements)

	// If no BOM data found, try broader search
	if len(usageByGrade) == 0 {
		log.Printf("No concrete BOM data found, trying broader search...")
		broadQuery := `
			SELECT 
				etb.product_name AS material_name,
				AVG(etb.quantity) AS avg_quantity_per_element
			FROM element_type_bom etb
			WHERE etb.project_id = $1 
			AND (
				etb.product_name ILIKE '%M%' 
				OR etb.product_name ILIKE '%grade%'
			)
			GROUP BY etb.product_name
			ORDER BY etb.product_name
		`

		broadRows, broadErr := db.Query(broadQuery, projectID)
		if broadErr == nil {
			defer broadRows.Close()
			for broadRows.Next() {
				var materialName string
				var avgQuantityPerElement float64
				if err := broadRows.Scan(&materialName, &avgQuantityPerElement); err == nil {
					totalUsage := avgQuantityPerElement * float64(completedElements)
					usageByGrade[materialName] = totalUsage
					log.Printf("DEBUG: Found potential concrete material - Name: %s, Total usage: %f", materialName, totalUsage)
				}
			}
		}
	}

	// Ensure we have at least default grades
	if len(usageByGrade) == 0 {
		log.Printf("WARNING: No concrete materials found for project %d. Using default values.", projectID)
		usageByGrade["M-40"] = 0.0
		usageByGrade["M-60"] = 0.0
	}

	return usageByGrade, nil
}

// calculateSteelUsageByBOM calculates steel usage based on elements that completed Mesh & Mould stage
func calculateSteelUsageByBOM(db *sql.DB, projectID int, startDate, endDate time.Time) (map[string]float64, error) {
	// First, get the count of elements that completed Mesh & Mould stage in this period
	completedElements, err := getCompletedElementsCount(db, projectID, startDate, endDate)
	if err != nil {
		log.Printf("Error getting completed elements count: %v", err)
		return map[string]float64{"TMT Steel": 0.0}, err
	}

	// If no elements completed Mesh & Mould stage, return zero usage
	if completedElements == 0 {
		log.Printf("No elements completed Mesh & Mould stage in this period, returning zero steel usage")
		return map[string]float64{"TMT Steel": 0.0}, nil
	}

	// Get steel usage per element from BOM data
	steelQuery := `
		SELECT 
			etb.product_name AS material_name,
			AVG(etb.quantity) AS avg_quantity_per_element
		FROM element_type_bom etb
		WHERE etb.project_id = $1 
			AND (
				etb.product_name ILIKE '%steel%' 
				OR etb.product_name ILIKE '%bar%'
				OR etb.product_name ILIKE '%rod%'
				OR etb.product_name ILIKE '%TMT%'
				OR etb.product_name ILIKE '%rebar%'
				OR etb.product_name ILIKE '%mm%'
				OR etb.product_name ILIKE '%Fe%'
				OR etb.product_name ILIKE '%iron%'
			)
		GROUP BY etb.product_name
		ORDER BY etb.product_name
	`

	rows, err := db.Query(steelQuery, projectID)
	if err != nil {
		log.Printf("Error querying steel BOM data: %v", err)
		return map[string]float64{"TMT Steel": 0.0}, err
	}
	defer rows.Close()

	usageByGrade := make(map[string]float64)
	rowCount := 0

	for rows.Next() {
		var materialName string
		var avgQuantityPerElement float64
		if err := rows.Scan(&materialName, &avgQuantityPerElement); err != nil {
			log.Printf("Error scanning steel BOM row: %v", err)
			continue
		}

		// Calculate total usage: average per element * number of completed elements
		totalUsage := avgQuantityPerElement * float64(completedElements)
		usageByGrade[materialName] = totalUsage
		rowCount++

		log.Printf("DEBUG: Steel material - Name: %s, Avg per element: %f, Completed elements: %d, Total usage: %f",
			materialName, avgQuantityPerElement, completedElements, totalUsage)
	}

	log.Printf("DEBUG: Found %d steel materials from BOM, calculated usage for %d completed elements", rowCount, completedElements)

	// If no BOM data found, try broader search
	if len(usageByGrade) == 0 {
		log.Printf("No steel BOM data found, trying broader search...")
		broadQuery := `
			SELECT 
				etb.product_name AS material_name,
				AVG(etb.quantity) AS avg_quantity_per_element
			FROM element_type_bom etb
			WHERE etb.project_id = $1 
			AND (
				etb.product_name ILIKE '%mm%' 
				OR etb.product_name ILIKE '%bar%'
			)
			GROUP BY etb.product_name
			ORDER BY etb.product_name
		`

		broadRows, broadErr := db.Query(broadQuery, projectID)
		if broadErr == nil {
			defer broadRows.Close()
			for broadRows.Next() {
				var materialName string
				var avgQuantityPerElement float64
				if err := broadRows.Scan(&materialName, &avgQuantityPerElement); err == nil {
					totalUsage := avgQuantityPerElement * float64(completedElements)
					usageByGrade[materialName] = totalUsage
					log.Printf("DEBUG: Found potential steel material - Name: %s, Total usage: %f", materialName, totalUsage)
				}
			}
		}
	}

	// Ensure we have at least default steel type
	if len(usageByGrade) == 0 {
		log.Printf("WARNING: No steel materials found for project %d. Using default values.", projectID)
		usageByGrade["TMT Steel"] = 0.0
	}

	return usageByGrade, nil
}

// calculateConcreteUsageByGradeRi calculates concrete usage by grade for a specific date range
func calculateConcreteUsageByGradeRi(db *sql.DB, projectID int, startDate, endDate time.Time) (map[string]float64, error) {
	return calculateMaterialUsageByGrade(db, projectID, startDate, endDate, MaterialTypeConcrete)
}

// calculateSteelUsageByGradeRi calculates steel usage by grade for a specific date range
func calculateSteelUsageByGradeRi(db *sql.DB, projectID int, startDate, endDate time.Time) (map[string]float64, error) {
	return calculateMaterialUsageByGrade(db, projectID, startDate, endDate, MaterialTypeSteel)
}

// getProjectConcreteGradesRi gets all concrete grades used in a project
func getProjectConcreteGradesRi(db *sql.DB, projectID int) ([]string, error) {
	// Get concrete grades from element_type_bom table (new format)
	query := `
		SELECT DISTINCT product_name AS grade
		FROM element_type_bom 
		WHERE project_id = $1 
		AND (
			product_name ILIKE '%concrete%' 
			OR product_name ILIKE '%cement%'
			OR product_name ILIKE '%M10%'
			OR product_name ILIKE '%M15%'
			OR product_name ILIKE '%M20%'
			OR product_name ILIKE '%M25%'
			OR product_name ILIKE '%M30%'
			OR product_name ILIKE '%M35%'
			OR product_name ILIKE '%M40%'
			OR product_name ILIKE '%M45%'
			OR product_name ILIKE '%M50%'
			OR product_name ILIKE '%M60%'
			OR product_name ILIKE '%M80%'
			OR product_name ILIKE '%grade%'
			OR product_name ILIKE '%mix%'
		)
		ORDER BY grade;
	`

	rows, err := db.Query(query, projectID)
	if err != nil {
		log.Printf("Error querying concrete grades from element_type_bom: %v", err)
		return []string{"M-40", "M-60"}, nil
	}
	defer rows.Close()

	var grades []string
	for rows.Next() {
		var grade string
		if err := rows.Scan(&grade); err != nil {
			log.Printf("Error scanning concrete grade: %v", err)
			continue
		}
		grades = append(grades, grade)
	}

	// If no grades found, try a broader search
	if len(grades) == 0 {
		log.Printf("No concrete grades found with specific patterns, trying broader search...")
		broadQuery := `
			SELECT DISTINCT product_name AS grade
			FROM element_type_bom 
			WHERE project_id = $1 
			AND (
				product_name ILIKE '%M%' 
				OR product_name ILIKE '%grade%'
			)
			ORDER BY grade;
		`

		broadRows, broadErr := db.Query(broadQuery, projectID)
		if broadErr == nil {
			defer broadRows.Close()
			for broadRows.Next() {
				var grade string
				if err := broadRows.Scan(&grade); err == nil {
					grades = append(grades, grade)
				}
			}
		}
	}

	// If still no grades found, return default grades
	if len(grades) == 0 {
		log.Printf("No concrete grades found for project %d, using defaults", projectID)
		grades = []string{"M-40", "M-60"}
	} else {
		log.Printf("Found concrete grades for project %d: %v", projectID, grades)
	}

	return grades, nil
}

// getProjectSteelGradesRi gets all steel grades used in a project
func getProjectSteelGradesRi(db *sql.DB, projectID int) ([]string, error) {
	// Get steel grades from element_type_bom table (new format)
	query := `
		SELECT DISTINCT product_name AS grade
		FROM element_type_bom 
		WHERE project_id = $1 
		AND (
			product_name ILIKE '%steel%' 
			OR product_name ILIKE '%bar%'
			OR product_name ILIKE '%rod%'
			OR product_name ILIKE '%TMT%'
			OR product_name ILIKE '%rebar%'
			OR product_name ILIKE '%mm%'
			OR product_name ILIKE '%Fe%'
			OR product_name ILIKE '%iron%'
		)
		ORDER BY grade;
	`

	rows, err := db.Query(query, projectID)
	if err != nil {
		log.Printf("Error querying steel grades from inv_bom: %v", err)
		return []string{"TMT Steel"}, nil
	}
	defer rows.Close()

	var grades []string
	for rows.Next() {
		var grade string
		if err := rows.Scan(&grade); err != nil {
			log.Printf("Error scanning steel grade: %v", err)
			continue
		}
		grades = append(grades, grade)
	}

	// If no grades found, return default grades
	if len(grades) == 0 {
		grades = []string{"TMT Steel"}
	}

	return grades, nil
}

// GetConcreteUsageReportsRi godoc
// @Summary      Get concrete material usage reports
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Param        type        query string  false  "yearly|monthly|daily"
// @Param        year        query string  false  "Year"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/material_usage_reports_concrete/{project_id} [get]
func GetConcreteUsageReportsRi(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Authentication
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Parse project ID
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Step 3: Parse query parameters
		reportType := c.DefaultQuery("type", "yearly")
		yearStr := c.DefaultQuery("year", strconv.Itoa(time.Now().Year()))
		monthStr := c.Query("month")
		dateStr := c.Query("date") // Parameter for weekly reports

		// For weekly reports, validate month and date are provided
		if reportType == "weekly" && (monthStr == "" || dateStr == "") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Month and date required for weekly reports"})
			return
		}

		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 2020 || year > 2030 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid year"})
			return
		}

		// For monthly reports, set default month if not provided
		if monthStr == "" {
			monthStr = strconv.Itoa(int(time.Now().Month()))
		}

		month, err := strconv.Atoi(monthStr)
		if err != nil || month < 1 || month > 12 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid month"})
			return
		}

		// Step 4: Generate date ranges with appropriate naming
		var dateRanges []DateRangeRi
		location := time.Now().Location()

		switch reportType {
		case "yearly":
			now := time.Now()
			isCurrentYear := now.Year() == year

			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)
				monthName := start.Month().String()
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  monthName,
					Start: start,
					End:   end,
				})
			}
		case "monthly":
			// For monthly reports, divide the month into 5-day periods
			monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Second)
			daysInMonth := monthEnd.Day()

			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				periodName := fmt.Sprintf("%d-%d", startRange.Day(), endRange.Day())
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  periodName,
					Start: startRange,
					End:   endRange,
				})
			}
		case "weekly":
			// For weekly reports, show last 7 days data from given date
			date, err := strconv.Atoi(dateStr)
			if err != nil || date < 1 || date > 31 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format. Use DD (1-31)"})
				return
			}

			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dateStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				dayEnd := currentDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				dayName := currentDate.Format("2006-01-02")
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  dayName,
					Start: currentDate,
					End:   dayEnd,
				})
			}
		}

		// Step 5: Get available concrete grades for the project (with fallback)
		concreteGrades := []string{"M-40", "M-60"} // Default grades
		if grades, err := getProjectConcreteGradesRi(db, projectID); err == nil && len(grades) > 0 {
			concreteGrades = grades
		}
		log.Printf("DEBUG: Using concrete grades: %v", concreteGrades)

		// Step 6: Calculate concrete usage for each date range
		var results []map[string]interface{}
		for _, dateRange := range dateRanges {
			result := map[string]interface{}{
				"name": dateRange.Name,
			}

			// Add date range for weekly reports
			if reportType == "weekly" {
				result["name"] = dateRange.Start.Format("2006-01-02")
			}

			// Get count of elements that completed Mesh & Mould stage in this period
			completedElements, err := getCompletedElementsCount(db, projectID, dateRange.Start, dateRange.End)
			if err != nil {
				log.Printf("Error getting completed elements count for %s: %v", dateRange.Name, err)
				completedElements = 0
			}
			result["completed_elements"] = completedElements

			// Calculate concrete usage by grade for this date range
			concreteUsageByGrade, err := calculateConcreteUsageByGradeRi(db, projectID, dateRange.Start, dateRange.End)
			if err != nil {
				log.Printf("Error calculating concrete usage for %s: %v", dateRange.Name, err)
				// Set default values if calculation fails
				for _, grade := range concreteGrades {
					result[grade] = 0.0
				}
			} else {
				log.Printf("DEBUG: Concrete usage for %s: %v", dateRange.Name, concreteUsageByGrade)
				// Add concrete usage by grade to result
				for _, grade := range concreteGrades {
					if usage, exists := concreteUsageByGrade[grade]; exists {
						result[grade] = usage
					} else {
						result[grade] = 0.0
					}
				}
			}

			results = append(results, result)
		}

		// Step 7: Return formatted data
		c.JSON(http.StatusOK, results)

		// Step 8: Log activity
		activityLog := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetched %s concrete usage reports", reportType),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		SaveActivityLog(db, activityLog)
	}
}

// GetSteelUsageReportsRi godoc
// @Summary      Get steel material usage reports
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Param        type        query string  false  "yearly|monthly|daily"
// @Param        year        query string  false  "Year"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/material_usage_reports_steel/{project_id} [get]
func GetSteelUsageReportsRi(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Authentication
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Parse project ID
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Step 3: Parse query parameters
		reportType := c.DefaultQuery("type", "yearly")
		yearStr := c.DefaultQuery("year", strconv.Itoa(time.Now().Year()))
		monthStr := c.Query("month")
		dateStr := c.Query("date") // Parameter for weekly reports

		// For weekly reports, validate month and date are provided
		if reportType == "weekly" && (monthStr == "" || dateStr == "") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Month and date required for weekly reports"})
			return
		}

		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 2020 || year > 2030 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid year"})
			return
		}

		// For monthly reports, set default month if not provided
		if monthStr == "" {
			monthStr = strconv.Itoa(int(time.Now().Month()))
		}

		month, err := strconv.Atoi(monthStr)
		if err != nil || month < 1 || month > 12 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid month"})
			return
		}

		// Step 4: Generate date ranges (using same logic as original function)
		var dateRanges []DateRangeRi
		location := time.Now().Location()

		switch reportType {
		case "yearly":
			now := time.Now()
			isCurrentYear := now.Year() == year

			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)
				monthName := start.Month().String()
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  monthName,
					Start: start,
					End:   end,
				})
			}
		case "monthly":
			// For monthly reports, divide the month into 5-day periods
			monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Second)
			daysInMonth := monthEnd.Day()

			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				periodName := fmt.Sprintf("%d-%d", startRange.Day(), endRange.Day())
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  periodName,
					Start: startRange,
					End:   endRange,
				})
			}
		case "weekly":
			// For weekly reports, show last 7 days data from given date
			date, err := strconv.Atoi(dateStr)
			if err != nil || date < 1 || date > 31 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format. Use DD (1-31)"})
				return
			}

			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dateStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				dayEnd := currentDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				dayName := currentDate.Format("2006-01-02")
				dateRanges = append(dateRanges, DateRangeRi{
					Name:  dayName,
					Start: currentDate,
					End:   dayEnd,
				})
			}
		}

		// Step 5: Get available steel grades for the project (with fallback)
		steelGrades := []string{"TMT Steel"} // Default grades
		if grades, err := getProjectSteelGradesRi(db, projectID); err == nil && len(grades) > 0 {
			steelGrades = grades
		}
		log.Printf("DEBUG: Using steel grades: %v", steelGrades)

		// Step 6: Calculate steel usage for each date range
		var results []map[string]interface{}
		for _, dateRange := range dateRanges {
			result := map[string]interface{}{
				"name": dateRange.Name,
			}

			// Add date range for weekly reports
			if reportType == "weekly" {
				result["name"] = dateRange.Start.Format("2006-01-02")
			}

			// Get count of elements that completed Mesh & Mould stage in this period
			completedElements, err := getCompletedElementsCount(db, projectID, dateRange.Start, dateRange.End)
			if err != nil {
				log.Printf("Error getting completed elements count for %s: %v", dateRange.Name, err)
				completedElements = 0
			}
			result["completed_elements"] = completedElements

			// Calculate steel usage by grade for this date range
			steelUsageByGrade, err := calculateSteelUsageByGradeRi(db, projectID, dateRange.Start, dateRange.End)
			if err != nil {
				log.Printf("Error calculating steel usage for %s: %v", dateRange.Name, err)
				// Set default values if calculation fails
				for _, grade := range steelGrades {
					result[grade] = 0.0
				}
			} else {
				log.Printf("DEBUG: Steel usage for %s: %v", dateRange.Name, steelUsageByGrade)
				// Add steel usage by grade to result
				for _, grade := range steelGrades {
					if usage, exists := steelUsageByGrade[grade]; exists {
						result[grade] = usage
					} else {
						result[grade] = 0.0
					}
				}
			}

			results = append(results, result)
		}

		// Step 6: Return steel data
		c.JSON(http.StatusOK, results)

		// Step 7: Log activity
		activityLog := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetched %s steel usage reports", reportType),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		SaveActivityLog(db, activityLog)
	}
}

// Helper structures for material usage calculation
type DateRangeRi struct {
	Name  string
	Start time.Time
	End   time.Time
}

type MaterialUsageResultRi struct {
	Period        string                     `json:"period"`
	ConcreteUsage map[string]ConcreteGradeRi `json:"concrete_usage"`
	SteelUsage    map[string]SteelSizeRi     `json:"steel_usage"`
}

type ConcreteGradeRi struct {
	Grade    string  `json:"grade"`
	Volume   float64 `json:"volume_m3"`
	Weight   float64 `json:"weight_kg"`
	Elements int     `json:"elements_count"`
}

type SteelSizeRi struct {
	Diameter string  `json:"diameter"`
	Weight   float64 `json:"weight_kg"`
	Length   float64 `json:"length_m"`
	Elements int     `json:"elements_count"`
}

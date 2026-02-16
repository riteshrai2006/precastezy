package handlers

import (
	"backend/models"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// GetElementReportAccordingToStage godoc
// @Summary      Get element report by stage
// @Tags         reports
// @Success      200  {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/get_element_report/ [get]
func GetElementReportAccordingToStage(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

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

		// Query to fetch all distinct stages with their IDs from the project_stage table
		stageQuery := `SELECT DISTINCT id, name FROM project_stages`
		rows, err := db.Query(stageQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stages"})
			return
		}
		defer rows.Close()

		// Store fetched stages
		type Stage struct {
			ID   int    `json:"stage_id"`
			Name string `json:"stage_name"`
		}

		var stages []Stage
		for rows.Next() {
			var stage Stage
			if err := rows.Scan(&stage.ID, &stage.Name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan stage"})
				return
			}
			stages = append(stages, stage)
		}

		// Prepare response structure
		type StageReport struct {
			StageID       int    `json:"stage_id"`
			StageName     string `json:"stage_name"`
			TotalElements int    `json:"total_elements"`
		}

		var stageReports []StageReport

		// Count elements for each stage using stage_id
		for _, stage := range stages {
			var count int
			countQuery := `SELECT COUNT(*) FROM activity WHERE stage_id = $1`
			err := db.QueryRow(countQuery, stage.ID).Scan(&count)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count elements for stage"})
				return
			}

			stageReports = append(stageReports, StageReport{
				StageID:       stage.ID,
				StageName:     stage.Name,
				TotalElements: count,
			})
		}

		// Return JSON response
		c.JSON(http.StatusOK, stageReports)

		log := models.ActivityLog{
			EventContext: "Report",
			EventName:    "Get",
			Description:  "Get Element Report According to Stage",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetPrecastReport godoc
// @Summary      Get precast report by project (stage-wise counts)
// @Tags         reports
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_precast_report/{project_id} [get]
func GetPrecastReport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Get project_id from query parameters and convert it to an integer
		projectID, convErr := strconv.Atoi(c.Param("project_id")) // Get BOMPro ID from the request URL
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid BOMPro ID"})
			return
		}

		// Query to fetch stages for the given project ID
		stageQuery := `SELECT DISTINCT id, name FROM project_stages WHERE project_id = $1`
		rows, err := db.Query(stageQuery, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stages"})
			return
		}
		defer rows.Close()

		// Store fetched stages
		type Stage struct {
			ID   int    `json:"stage_id"`
			Name string `json:"stage_name"`
		}

		var stages []Stage
		for rows.Next() {
			var stage Stage
			if err := rows.Scan(&stage.ID, &stage.Name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan stage"})
				return
			}
			stages = append(stages, stage)
		}

		// Prepare response structure
		type StageReport struct {
			StageID       int    `json:"stage_id"`
			StageName     string `json:"stage_name"`
			TotalElements int    `json:"total_elements"`
		}

		var stageReports []StageReport

		// Count elements for each stage using stage_id and project_id
		for _, stage := range stages {
			var count int
			countQuery := `SELECT COUNT(*) FROM activity WHERE stage_id = $1 AND project_id = $2`
			err := db.QueryRow(countQuery, stage.ID, projectID).Scan(&count)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count elements for stage"})
				return
			}

			stageReports = append(stageReports, StageReport{
				StageID:       stage.ID,
				StageName:     stage.Name,
				TotalElements: count,
			})
		}

		// Return JSON response
		c.JSON(http.StatusOK, stageReports)

		log := models.ActivityLog{
			EventContext: "Element",
			EventName:    "Get",
			Description:  "Get Element Report according to stage",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetWeeklyStageReport godoc
// @Summary      Get weekly stage report by project
// @Tags         reports
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_weekly_report/{project_id} [get]
func GetWeeklyStageReport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Get project_id from query parameters and convert it to an integer
		projectID, convErr := strconv.Atoi(c.Param("project_id")) // Get BOMPro ID from the request URL
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid BOMPro ID"})
			return
		}

		// SQL query to fetch weekly grouped data
		query := `
			SELECT 
    DATE_TRUNC('week', start_date)::DATE AS week_start,
    (DATE_TRUNC('week', start_date) + INTERVAL '6 days')::DATE AS week_end,
    ps.id AS stage_id,
    ps.name AS stage_name,
    COUNT(*) AS total_elements
FROM activity a
JOIN project_stages ps ON a.stage_id = ps.id
WHERE a.project_id = $1
GROUP BY week_start, week_end, ps.id, ps.name
ORDER BY week_start, ps.id;

		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch weekly report"})
			return
		}
		defer rows.Close()

		// Define response structure
		type StageData struct {
			StageID       int    `json:"stage_id"`
			StageName     string `json:"stage_name"`
			TotalElements int    `json:"total_elements"`
		}

		type WeekReport struct {
			WeekStart string      `json:"week_start"`
			WeekEnd   string      `json:"week_end"`
			Stages    []StageData `json:"stages"`
		}

		// Temporary storage
		weekMap := make(map[string]*WeekReport)

		// Iterate over rows
		for rows.Next() {
			var weekStart, weekEnd, stageName string
			var stageID, totalElements int

			if err := rows.Scan(&weekStart, &weekEnd, &stageID, &stageName, &totalElements); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan weekly data"})
				return
			}

			// Create a unique key for each week
			weekKey := weekStart + "_" + weekEnd

			// If the week entry does not exist, create it
			if _, exists := weekMap[weekKey]; !exists {
				weekMap[weekKey] = &WeekReport{
					WeekStart: weekStart,
					WeekEnd:   weekEnd,
					Stages:    []StageData{},
				}
			}

			// Append stage data to the corresponding week
			weekMap[weekKey].Stages = append(weekMap[weekKey].Stages, StageData{
				StageID:       stageID,
				StageName:     stageName,
				TotalElements: totalElements,
			})
		}

		// Convert map to slice
		var weekReports []WeekReport
		for _, report := range weekMap {
			weekReports = append(weekReports, *report)
		}

		// Return JSON response
		c.JSON(http.StatusOK, weekReports)

		log := models.ActivityLog{
			EventContext: "Report",
			EventName:    "Get",
			Description:  "Get Weekly Stage Report",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetMonthlyStageReport godoc
// @Summary      Get monthly stage report by project
// @Tags         reports
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_monthly_report/{project_id} [get]
func GetMonthlyStageReport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Get project_id from URL parameter ("/report/:project_id") or change to Query if needed
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			log.Println("Invalid project_id:", c.Param("project_id")) // Debugging
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id, must be an integer"})
			return
		}

		log.Println("Fetching Monthly Report for Project ID:", projectID) // Debugging

		// SQL query to fetch monthly grouped data
		query := `
			SELECT 
				TO_CHAR(a.start_date, 'YYYY-MM') AS month,
				ps.id AS stage_id,
				ps.name AS stage_name,
				COUNT(*) AS total_elements
			FROM activity a
			JOIN project_stages ps ON a.stage_id = ps.id
			WHERE a.project_id = $1
			GROUP BY month, ps.id, ps.name
			ORDER BY month, ps.id;
		`

		// Execute query
		rows, err := db.Query(query, projectID)
		if err != nil {
			log.Println("Query Execution Error:", err) // Debugging
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch monthly report"})
			return
		}
		defer rows.Close()

		// Define response structure
		type StageData struct {
			StageID       int    `json:"stage_id"`
			StageName     string `json:"stage_name"`
			TotalElements int    `json:"total_elements"`
		}

		type MonthlyReport struct {
			Month  string      `json:"month"`
			Stages []StageData `json:"stages"`
		}

		// Temporary storage
		monthMap := make(map[string]*MonthlyReport)

		// Process rows correctly
		for rows.Next() {
			var month, stageName string
			var stageID, totalElements int

			if err := rows.Scan(&month, &stageID, &stageName, &totalElements); err != nil {
				log.Println("Row Scan Error:", err) // Debugging
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan monthly data"})
				return
			}

			// If the month entry does not exist, create it
			if _, exists := monthMap[month]; !exists {
				monthMap[month] = &MonthlyReport{
					Month:  month,
					Stages: []StageData{},
				}
			}

			// Append stage data to the corresponding month
			monthMap[month].Stages = append(monthMap[month].Stages, StageData{
				StageID:       stageID,
				StageName:     stageName,
				TotalElements: totalElements,
			})
		}

		// Convert map to slice
		var monthReports []MonthlyReport
		for _, report := range monthMap {
			monthReports = append(monthReports, *report)
		}

		// Debugging: Print fetched data
		log.Printf("Fetched %d months of data\n", len(monthReports))

		// If no data found, return an empty array instead of an error
		if len(monthReports) == 0 {
			c.JSON(http.StatusOK, gin.H{"monthly_report": []MonthlyReport{}})
			return
		}

		// Return JSON response
		c.JSON(http.StatusOK, gin.H{"monthly_report": monthReports})

		log := models.ActivityLog{
			EventContext: "Report",
			EventName:    "Get",
			Description:  "Get Monthly Stage Report",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetDailyStageReport godoc
// @Summary      Get daily stage report by project
// @Tags         reports
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_daily_report/{project_id} [get]
func GetDailyStageReport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Get project_id from query parameters and convert it to an integer
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id, must be an integer"})
			return
		}

		// SQL query to fetch daily grouped data
		query := `
			SELECT 
				a.start_date::DATE AS day,
				ps.id AS stage_id,
				ps.name AS stage_name,
				COUNT(*) AS total_elements
			FROM activity a
			JOIN project_stages ps ON a.stage_id = ps.id
			WHERE a.project_id = $1
			GROUP BY day, ps.id, ps.name
			ORDER BY day, ps.id;
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch daily report"})
			return
		}
		defer rows.Close()

		// Define response structure
		type StageData struct {
			StageID       int    `json:"stage_id"`
			StageName     string `json:"stage_name"`
			TotalElements int    `json:"total_elements"`
		}

		type DailyReport struct {
			Day    string      `json:"day"`
			Stages []StageData `json:"stages"`
		}

		// Temporary storage
		dayMap := make(map[string]*DailyReport)

		// Iterate over rows
		for rows.Next() {
			var day, stageName string
			var stageID, totalElements int

			if err := rows.Scan(&day, &stageID, &stageName, &totalElements); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan daily data"})
				return
			}

			// If the day entry does not exist, create it
			if _, exists := dayMap[day]; !exists {
				dayMap[day] = &DailyReport{
					Day:    day,
					Stages: []StageData{},
				}
			}

			// Append stage data to the corresponding day
			dayMap[day].Stages = append(dayMap[day].Stages, StageData{
				StageID:       stageID,
				StageName:     stageName,
				TotalElements: totalElements,
			})
		}

		// Convert map to slice
		var dayReports []DailyReport
		for _, report := range dayMap {
			dayReports = append(dayReports, *report)
		}

		// Return JSON response
		c.JSON(http.StatusOK, gin.H{"daily_report": dayReports})

		log := models.ActivityLog{
			EventContext: "Report",
			EventName:    "Get",
			Description:  "Get Daily Stage Report",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

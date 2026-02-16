package handlers

import (
	"backend/models"
	"backend/utils"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"github.com/lib/pq"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// func GetProjectStats(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		statuses := []string{"Cancelled", "Completed", "Critical", "Inactive", "Ongoing"}
// 		projectCount := make(map[string]int)
// 		projectGroups := make(map[string][]models.Projects)

// 		// Initialize count and groups with empty values
// 		for _, status := range statuses {
// 			projectCount[status] = 0
// 			projectGroups[status] = []models.Projects{}
// 		}

// 		rows, err := db.Query(`
//         SELECT
//             project_id, name, priority, project_status, start_date,
//             end_date, logo, description, client_id, budget
//         FROM project
//     `)
// 		if err != nil {
// 			log.Printf("Error fetching projects: %v", err)
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects"})
// 			return
// 		}
// 		defer rows.Close()

// 		// Parse rows
// 		for rows.Next() {
// 			var project models.Projects
// 			var startDate, endDate models.DateOnly
// 			var status string

// 			err := rows.Scan(
// 				&project.ProjectId, &project.Name, &project.Priority, &status,
// 				&startDate, &endDate, &project.Logo, &project.Description,
// 				&project.ClientId, &project.Budget,
// 			)
// 			if err != nil {
// 				log.Printf("Error scanning row: %v", err)
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process project data"})
// 				return
// 			}

// 			project.ProjectStatus = status
// 			project.StartDate = startDate
// 			project.EndDate = endDate

// 			// Increment count and add project to the appropriate group
// 			if _, exists := projectGroups[status]; exists {
// 				projectCount[status]++
// 				projectGroups[status] = append(projectGroups[status], project)
// 			}
// 		}

// 		// Create the final response
// 		response := gin.H{
// 			"count":    projectCount,
// 			"projects": projectGroups,
// 		}

// 		c.JSON(http.StatusOK, response)
// 	}
// }

// func GetElementStats(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// Struct for storing response
// 		type ElementResponse struct {
// 			Counts   map[string]int              `json:"counts"`
// 			Elements map[string][]models.Element `json:"elements"`
// 		}

// 		response := ElementResponse{
// 			Counts:   make(map[string]int),
// 			Elements: make(map[string][]models.Element),
// 		}

// 		// Fetch counts and details for each status
// 		query := `
// 			SELECT s.status_name, COUNT(e.id) AS element_count
// 			FROM status s
// 			LEFT JOIN element e ON e.status = s.status_id
// 			GROUP BY s.status_name
// 		`

// 		rows, err := db.Query(query)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch status counts", "details": err.Error()})
// 			return
// 		}
// 		defer rows.Close()

// 		// Populate counts
// 		for rows.Next() {
// 			var statusName string
// 			var count int
// 			err := rows.Scan(&statusName, &count)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse status counts", "details": err.Error()})
// 				return
// 			}
// 			response.Counts[statusName] = count
// 		}

// 		// Fetch all elements grouped by status
// 		elementQuery := `
// 			SELECT e.id, e.element_type_id, e.element_id, e.element_name, e.project_id,
// 			       e.created_by, e.created_at, e.status, e.element_type_version, e.update_at, e.target_location, s.status_name
// 			FROM element e
// 			LEFT JOIN status s ON e.status = s.status_id
// 		`
// 		elementRows, err := db.Query(elementQuery)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch elements", "details": err.Error()})
// 			return
// 		}
// 		defer elementRows.Close()

// 		// Populate elements grouped by status
// 		for elementRows.Next() {
// 			var element models.Element
// 			var statusName string

// 			err := elementRows.Scan(
// 				&element.Id,
// 				&element.ElementTypeID,
// 				&element.ElementId,
// 				&element.ElementName,
// 				&element.ProjectID,
// 				&element.CreatedBy,
// 				&element.CreatedAt,
// 				&element.Status,
// 				&element.ElementTypeVersion,
// 				&element.UpdateAt,
// 				&element.TargetLocation,
// 				&statusName,
// 			)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse elements", "details": err.Error()})
// 				return
// 			}

// 			response.Elements[statusName] = append(response.Elements[statusName], element)
// 		}

// 		// Send the combined response
// 		c.JSON(http.StatusOK, response)
// 	}
// }

// // Define DayStats at the package level
// type DayStats struct {
// 	Date      string `json:"date"`
// 	TaskCount int    `json:"task_count"`
// }

// func GetLast7DaysTaskStats(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		rows, err := db.Query(`
// 			SELECT
// 				TO_CHAR(start_date, 'YYYY-MM-DD') AS assigned_date,
// 				COUNT(*) AS task_count
// 			FROM
// 				activity
// 			WHERE
// 				start_date >= NOW() - INTERVAL '7 days'
// 			GROUP BY
// 				assigned_date
// 			ORDER BY
// 				assigned_date ASC
// 		`)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task statistics"})
// 			return
// 		}
// 		defer rows.Close()

// 		var stats []DayStats

// 		for rows.Next() {
// 			var stat DayStats
// 			if err := rows.Scan(&stat.Date, &stat.TaskCount); err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing task statistics"})
// 				return
// 			}
// 			stats = append(stats, stat)
// 		}

// 		if err := rows.Err(); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading task statistics"})
// 			return
// 		}

// 		// Ensure days with no tasks are included
// 		result := fillMissingDays(stats)

// 		// Send response
// 		c.JSON(http.StatusOK, result)
// 	}
// }

// // Fill missing days with 0 task count
// func fillMissingDays(stats []DayStats) []DayStats {
// 	daysMap := make(map[string]int)
// 	for _, stat := range stats {
// 		daysMap[stat.Date] = stat.TaskCount
// 	}

// 	var result []DayStats
// 	today := time.Now()
// 	for i := 6; i >= 0; i-- {
// 		day := today.AddDate(0, 0, -i).Format("2006-01-02")
// 		result = append(result, DayStats{
// 			Date:      day,
// 			TaskCount: daysMap[day],
// 		})
// 	}

// 	return result
// }

// // Define response structure
// type RolewiseUserData map[string][]models.User

// func GetRolewiseUserData(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {

// 		// Query to fetch user details with role names
// 		rows, err := db.Query(`
// 			SELECT
// 				u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name,
// 				u.created_at, u.updated_at, u.first_access, u.last_access,
// 				u.profile_picture, u.is_admin, u.address, u.city, u.state,
// 				u.country, u.zip_code, u.phone_no, u.role_id,
// 				r.role_name
// 			FROM
// 				users u
// 			INNER JOIN
// 				roles r ON u.role_id = r.role_id
// 			ORDER BY
// 				r.role_name
// 		`)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user data", "err": err.Error()})
// 			return
// 		}
// 		defer rows.Close()

// 		// Create a map to store role-wise user data
// 		rolewiseData := make(RolewiseUserData)

// 		// Iterate over query results
// 		for rows.Next() {
// 			var user models.User
// 			var firstAccess, lastAccess sql.NullTime
// 			var employeeID sql.NullString

// 			var roleName string
// 			if err := rows.Scan(
// 				&user.ID, &employeeID, &user.Email, &user.Password,
// 				&user.FirstName, &user.LastName, &user.CreatedAt, &user.UpdatedAt,
// 				&firstAccess, &lastAccess, &user.ProfilePic, &user.IsAdmin,
// 				&user.Address, &user.City, &user.State, &user.Country,
// 				&user.ZipCode, &user.PhoneNo, &user.RoleID, &roleName,
// 			); err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing user data", "err": err.Error()})
// 				return
// 			}

// 			// Handle sql.NullTime for FirstAccess and LastAccess
// 			user.FirstAccess = firstAccess.Time
// 			if !firstAccess.Valid {
// 				user.FirstAccess = time.Time{} // Zero value of time.Time
// 			}

// 			user.LastAccess = lastAccess.Time
// 			if !lastAccess.Valid {
// 				user.LastAccess = time.Time{} // Zero value of time.Time
// 			}

// 			// Handle sql.NullString for EmployeeID
// 			if employeeID.Valid {
// 				user.EmployeeId = employeeID.String
// 			} else {
// 				user.EmployeeId = "" // Do not include EmployeeId if it is NULL
// 			}

// 			// Append user to the corresponding role name
// 			rolewiseData[roleName] = append(rolewiseData[roleName], user)
// 		}

// 		// Check for errors after iteration
// 		if err := rows.Err(); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading user data"})
// 			return
// 		}

// 		// Send response
// 		c.JSON(http.StatusOK, rolewiseData)
// 	}
// }

// func GetPrecastDashboardStats(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// Initialize response
// 		response := models.DashboardResponse{
// 			ProjectName: "uddating",
// 			Towers:      make(map[string]map[string]models.DashboardFloorStats),
// 		}

// 		// Query to get element counts by status, tower, floor, and element type
// 		query := `
// 			SELECT
// 				t.tower_name,
// 				f.floor_name,
// 				et.element_type_name,
// 				COUNT(*) as total_elements,
// 				SUM(CASE WHEN e.status = 'production' THEN 1 ELSE 0 END) as production_count,
// 				SUM(CASE WHEN e.status = 'stockyard' THEN 1 ELSE 0 END) as stockyard_count,
// 				SUM(CASE WHEN e.status = 'erection' THEN 1 ELSE 0 END) as erection_count,
// 				SUM(CASE WHEN e.status = 'not_in_production' THEN 1 ELSE 0 END) as not_in_production_count
// 			FROM
// 				elements e
// 				JOIN towers t ON e.tower_id = t.id
// 				JOIN floors f ON e.floor_id = f.id
// 				JOIN element_types et ON e.element_type_id = et.id
// 			GROUP BY
// 				t.tower_name, f.floor_name, et.element_type_name
// 		`

// 		rows, err := db.Query(query)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch dashboard statistics"})
// 			return
// 		}
// 		defer rows.Close()

// 		// Process the results
// 		for rows.Next() {
// 			var towerName, floorName, elementType string
// 			var total, production, stockyard, erection, notInProduction int

// 			err := rows.Scan(
// 				&towerName, &floorName, &elementType,
// 				&total, &production, &stockyard, &erection, &notInProduction,
// 			)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing dashboard statistics"})
// 				return
// 			}

// 			// Initialize tower if not exists
// 			if _, exists := response.Towers[towerName]; !exists {
// 				response.Towers[towerName] = make(map[string]models.DashboardFloorStats)
// 			}

// 			// Initialize floor if not exists
// 			if _, exists := response.Towers[towerName][floorName]; !exists {
// 				response.Towers[towerName][floorName] = models.DashboardFloorStats{}
// 			}

// 			// Update element stats based on type
// 			floorStats := response.Towers[towerName][floorName]
// 			elementStats := models.DashboardElementStats{
// 				TotalElement:    total,
// 				Production:      production,
// 				Stockyard:       stockyard,
// 				Erection:        erection,
// 				NotInProduction: notInProduction,
// 			}

// 			switch elementType {
// 			case "Beam":
// 				floorStats.Beam = elementStats
// 			case "Balcony":
// 				floorStats.Balcony = elementStats
// 			case "Solid Slab":
// 				floorStats.SolidSlab = elementStats
// 			}

// 			response.Towers[towerName][floorName] = floorStats

// 			// Update total counts
// 			response.TotalElement += total
// 			response.Production += production
// 			response.Stockyard += stockyard
// 			response.Erection += erection
// 			response.NotInProduction += notInProduction
// 		}

// 		if err := rows.Err(); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading dashboard statistics"})
// 			return
// 		}

// 		c.JSON(http.StatusOK, response)
// 	}
// }

// GetProductionSummary godoc
// @Summary      Get production summary for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/projects/{project_id}/production-summary [get]
func GetProductionSummary(db *sql.DB) gin.HandlerFunc {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		layout := "2006-01-02"
		today := time.Now().Truncate(24 * time.Hour)
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		var startDate, endDate time.Time
		hasStartDate := startDateStr != ""
		hasEndDate := endDateStr != ""

		switch {
		case hasStartDate && hasEndDate:
			startDate, _ = time.Parse(layout, startDateStr)
			endDate, _ = time.Parse(layout, endDateStr)
			// Add one day to endDate to make it inclusive of the entire end date
			endDate = endDate.AddDate(0, 0, 1)
		case hasStartDate:
			startDate, _ = time.Parse(layout, startDateStr)
			endDate = today.AddDate(0, 0, 1) // Add one day to include the entire today
		case hasEndDate:
			endDate, _ = time.Parse(layout, endDateStr)
			endDate = endDate.AddDate(0, 0, 1) // Add one day to make it inclusive
			startDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		}

		// Step 1: Fetch planned elements from activity
		query := `
			SELECT DISTINCT a.element_id 
			FROM activity a 
			JOIN element e ON a.element_id = e.id 
			WHERE a.project_id = $1
			AND a.completed = false`
		args := []interface{}{projectID}

		if hasStartDate || hasEndDate {
			query += " AND a.start_date >= $2 AND a.start_date < $3"
			args = append(args, startDate, endDate)
		}

		actRows, _ := db.Query(query, args...)
		defer actRows.Close()

		plannedSet := make(map[int]struct{})
		for actRows.Next() {
			var eid int
			_ = actRows.Scan(&eid)
			plannedSet[eid] = struct{}{}
		}

		var totalCount int
		// Step 1: Count total elements from precast_stock
		totalQuery := `
				SELECT COUNT(*)
				FROM element
				WHERE project_id = $1`
		_ = db.QueryRow(totalQuery, projectID).Scan(&totalCount)

		var plannedCount int
		var countQuery string
		if hasStartDate || hasEndDate {
			countQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM activity
				WHERE project_id = $1
		AND start_date >= $2 AND start_date < $3`
			_ = db.QueryRow(countQuery, projectID, startDate, endDate).Scan(&plannedCount)
		} else {
			countQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM activity
				WHERE project_id = $1`
			_ = db.QueryRow(countQuery, projectID).Scan(&plannedCount)
		}

		// Step 3: Count casted elements from precast_stock
		var castedCount int
		var castedQuery string

		if hasStartDate || hasEndDate {
			castedQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM precast_stock
				WHERE project_id = $1
				AND created_at >= $2 AND created_at < $3`
			_ = db.QueryRow(castedQuery, projectID, startDate, endDate).Scan(&castedCount)
		} else {
			castedQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM precast_stock
				WHERE project_id = $1`
			_ = db.QueryRow(castedQuery, projectID).Scan(&castedCount)
		}

		var erectedCount int
		var erectedQuery string

		if hasStartDate || hasEndDate {
			erectedQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM precast_stock
				WHERE project_id = $1
				AND erected = true
				AND updated_at >= $2 AND updated_at < $3`
			_ = db.QueryRow(erectedQuery, projectID, startDate, endDate).Scan(&erectedCount)
		} else {
			erectedQuery = `
				SELECT COUNT(DISTINCT element_id)
				FROM precast_stock
				WHERE project_id = $1
				AND erected = true`
			_ = db.QueryRow(erectedQuery, projectID).Scan(&erectedCount)
		}

		c.JSON(http.StatusOK, gin.H{
			"Total":        totalCount,
			"InProduction": plannedCount,
			"Casted":       castedCount,
			"Erected":      erectedCount,
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetched production summary for project %d", projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

	}
}

// GetQCSummary godoc
// @Summary      Get QC history summary for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/qc-history/{project_id} [get]
func GetQCSummary(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Session validation ---
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

		// --- Project ID ---
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// --- Date range ---
		layout := "2006-01-02"
		today := time.Now().Truncate(24 * time.Hour)

		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		var startDate, endDate time.Time
		if startDateStr != "" {
			startDate, _ = time.Parse(layout, startDateStr)
		} else {
			startDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		if endDateStr != "" {
			endDate, _ = time.Parse(layout, endDateStr)
		} else {
			endDate = today
		}

		// --- Optimized Query ---
		query := `
			SELECT cp.status, COUNT(*) 
			FROM complete_production cp
			JOIN project_members pm ON cp.user_id = pm.user_id AND pm.project_id = cp.project_id
			JOIN roles r ON pm.role_id = r.role_id
			WHERE cp.project_id = $1
			  AND cp.updated_at BETWEEN $2 AND $3
			  AND r.role_name IN ('QA', 'QC', 'QA/QC', 'MEP QC')
			  AND cp.status IN ('completed', 'rejected', 'hold')
			GROUP BY cp.status
		`

		ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancel()

		rows, err := db.QueryContext(ctx, query, projectID, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching QC summary", "details": err.Error()})
			return
		}
		defer rows.Close()

		// --- Initialize defaults ---
		statusCounts := map[string]int{
			"completed": 0,
			"rejected":  0,
			"hold":      0,
		}

		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err == nil {
				statusCounts[status] = count
			}
		}

		// --- Response ---
		c.JSON(http.StatusOK, gin.H{
			"approved": statusCounts["completed"],
			"rejected": statusCounts["rejected"],
			"hold":     statusCounts["hold"],
		})

		// --- Activity Log ---
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetched QC summary for project %d", projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		})
	}
}

// GetProjectStatusCounts godoc
// @Summary      Get project status counts
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/project_status [get]
func GetProjectStatusCounts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get session_id
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user_id from session
		ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
		defer cancel()

		var userID int
		err = db.QueryRowContext(ctx, "SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get role_id from users table
		var roleID int
		err = db.QueryRowContext(ctx, "SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}

		// Step 4: Get role name from roles table
		var roleName string
		err = db.QueryRowContext(ctx, "SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		var rows *sql.Rows

		switch roleName {
		case "superadmin":
			// Fetch all project counts
			query := `
				SELECT 
					COUNT(*) AS total,
					COUNT(*) FILTER (WHERE p.suspend = false AND (CURRENT_DATE >= p.start_date AND CURRENT_DATE <= p.end_date)) AS ongoing_count,
					COUNT(*) FILTER (WHERE p.suspend = true) AS hold_count,
					COUNT(*) FILTER (WHERE CURRENT_DATE > p.end_date AND p.suspend = false) AS completed_count,
					COUNT(*) FILTER (WHERE CURRENT_DATE < p.start_date AND p.suspend = false) AS tostart_count
				FROM project p;
			`
			rows, err = db.QueryContext(ctx, query)

		case "admin":
			// Fetch client_ids for the admin
			query := `
SELECT 
    COUNT(*) AS total,
    COUNT(*) FILTER (
        WHERE p.suspend = false AND (CURRENT_DATE >= p.start_date AND CURRENT_DATE <= p.end_date)) AS ongoing_count,
    COUNT(*) FILTER (
        WHERE p.suspend = true) AS hold_count,
    COUNT(*) FILTER (
        WHERE CURRENT_DATE > p.end_date AND p.suspend = false) AS completed_count,
    COUNT(*) FILTER (
        WHERE CURRENT_DATE < p.start_date AND p.suspend = false) AS tostart_count
FROM project p
WHERE p.client_id IN (
    SELECT id FROM end_client 
    WHERE client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
);`
			rows, err = db.QueryContext(ctx, query, userID)

		default:
			// Other users - Fetch projects they are assigned to in project_members
			query := `
				SELECT 
					COUNT(*) AS total,
					COUNT(*) FILTER (WHERE p.suspend = false AND (CURRENT_DATE >= p.start_date AND CURRENT_DATE <= p.end_date)) AS ongoing_count,
					COUNT(*) FILTER (WHERE p.suspend = true) AS hold_count,
					COUNT(*) FILTER (WHERE CURRENT_DATE > p.end_date AND p.suspend = false) AS completed_count,
					COUNT(*) FILTER (WHERE CURRENT_DATE < p.start_date AND p.suspend = false) AS tostart_count
				FROM project p
				WHERE p.project_id IN (SELECT project_id FROM project_members WHERE user_id = $1);
			`
			rows, err = db.QueryContext(ctx, query, userID)
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project data"})
			return
		}
		defer rows.Close()

		var total, ongoing, hold, completed, tostart int
		if rows.Next() {
			err = rows.Scan(&total, &ongoing, &hold, &completed, &tostart)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan result"})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"total_projects": total,
			"active":         ongoing,
			"suspended":      hold,
			"closed":         completed,
			"inactive":       tostart,
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched project status counts graph",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetElementStatusCountsPerProject godoc
// @Summary      Get element status counts per project
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_status_project [get]
func GetElementStatusCountsPerProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		if err := db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role ID"})
			return
		}
		if err := db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Use the same project filter logic as your working queries
		var projectFilterQuery string
		var args []interface{}
		switch roleName {
		case "superadmin":
			projectFilterQuery = "WHERE p.suspend = false"
		case "admin":
			projectFilterQuery = `
				WHERE p.client_id IN (
    SELECT id FROM end_client 
    WHERE client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
) AND p.suspend = false
`
			args = append(args, userID)
		default:
			projectFilterQuery = `
				WHERE p.project_id IN (
					SELECT pm.project_id FROM project_members pm
					JOIN project p2 ON pm.project_id = p2.id
					WHERE pm.user_id = $1 AND p2.suspend = false
				)`
			args = append(args, userID)
		}

		// Get all projects visible to user (even if no elements)
		ctxProjects, cancelProjects := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelProjects()

		projectRows, err := db.QueryContext(ctxProjects, fmt.Sprintf(`
			SELECT p.project_id, p.name, p.suspend
			FROM project p
			%s
		`, projectFilterQuery), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects", "details": err.Error()})
			return
		}
		defer projectRows.Close()

		projectStats := make(map[string]map[string]int)

		for projectRows.Next() {
			var projectID int
			var projectName string
			var suspend bool
			if err := projectRows.Scan(&projectID, &projectName, &suspend); err != nil {
				continue
			}

			var castedElements, erected, inStock, inProduction int

			// Casted
			err = db.QueryRowContext(ctxProjects, `
				SELECT COUNT(*) 
				FROM precast_stock ps
				JOIN element e ON ps.element_id = e.id
				WHERE e.project_id = $1 AND ps.stockyard = true AND ps.erected = false AND ps.dispatch_status = false
			`, projectID).Scan(&castedElements)
			if err != nil {
				castedElements = 0
			}

			// Erected
			err = db.QueryRowContext(ctxProjects, `
				SELECT COUNT(*) 
				FROM precast_stock ps
				JOIN element e ON ps.element_id = e.id
				WHERE e.project_id = $1 AND ps.stockyard = true AND ps.erected = true AND ps.dispatch_status = true 
			`, projectID).Scan(&erected)
			if err != nil {
				erected = 0
			}

			// In stock
			err = db.QueryRowContext(ctxProjects, `
				SELECT COUNT(*) 
				FROM precast_stock ps
				JOIN element e ON ps.element_id = e.id
				WHERE e.project_id = $1 AND ps.erected = false AND ps.dispatch_status = false 
				AND ps.stockyard = true 
			`, projectID).Scan(&inStock)
			if err != nil {
				inStock = 0
			}

			// In production
			err = db.QueryRowContext(ctxProjects, `
				SELECT COUNT(*) 
				FROM activity a
				WHERE a.project_id = $1 
				AND a.completed = false
			`, projectID).Scan(&inProduction)
			if err != nil {
				inProduction = 0
			}

			projectStats[projectName] = map[string]int{
				"casted_elements":  castedElements,
				"erected_elements": erected,
				"in_production":    inProduction,
				"in_stock":         inStock,
			}
		}

		c.JSON(http.StatusOK, projectStats)

		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched current month's element status counts per project",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetElementStatusCounts godoc
// @Summary      Get element status counts
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_status [get]
func GetElementStatusCounts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		if err := db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role ID"})
			return
		}
		if err := db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		// Get current month start and end dates (keeping for future use)
		// location := time.Now().Location()
		// now := time.Now().In(location)
		// year, month := now.Year(), now.Month()
		// start := time.Date(year, month, 1, 0, 0, 0, 0, location)
		// end := start.AddDate(0, 1, 0).Add(-time.Second) // Commented out for now

		var projectFilterQuery string
		var args []interface{}

		// Build project filter based on role and specific project
		if specificProjectID != nil {
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, *specificProjectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
    SELECT id FROM end_client 
    WHERE client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
) AND p.suspend = false
`
				args = append(args, userID)
			default:
				projectFilterQuery = `
					WHERE p.project_id IN (
						SELECT pm.project_id FROM project_members pm
						JOIN project p2 ON pm.project_id = p2.project_id
						WHERE pm.user_id = $1 AND p2.suspend = false
					)`
				args = append(args, userID)
			}
		}

		var inProduction, castedElements, inStock, erected int

		ctxStats, cancelStats := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelStats()

		// First, let's check if there's any data at all
		var totalElements int
		err = db.QueryRowContext(ctxStats, "SELECT COUNT(*) FROM element").Scan(&totalElements)
		if err != nil {
			fmt.Printf("Error counting total elements: %v\n", err)
		} else {
			fmt.Printf("Total elements in database: %d\n", totalElements)
		}

		// Casted elements - current month (relaxed date filter)
		castedQuery := fmt.Sprintf(`
			SELECT COUNT(DISTINCT ps.element_id)
			FROM precast_stock ps
			JOIN element e ON ps.element_id = e.id
			JOIN project p ON e.project_id = p.project_id
			%s AND ps.production_date IS NOT NULL
		`, projectFilterQuery)
		castedParams := args

		// Debug logging
		fmt.Printf("Casted Query: %s\n", castedQuery)
		fmt.Printf("Casted Params: %v\n", castedParams)

		err = db.QueryRowContext(ctxStats, castedQuery, castedParams...).Scan(&castedElements)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch casted elements count", "details": err.Error()})
			return
		}

		fmt.Printf("Casted Elements Count: %d\n", castedElements)

		// Erected elements - current month (relaxed date filter)
		erectedQuery := fmt.Sprintf(`
			SELECT COUNT(DISTINCT ps.element_id)
			FROM precast_stock ps
			JOIN element e ON ps.element_id = e.id
			JOIN project p ON e.project_id = p.project_id
			%s AND ps.erected = true
		`, projectFilterQuery)
		erectedParams := args
		err = db.QueryRowContext(ctxStats, erectedQuery, erectedParams...).Scan(&erected)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch erected elements count", "details": err.Error()})
			return
		}

		// In stock - current month (relaxed date filter)
		inStockQuery := fmt.Sprintf(`
			SELECT COUNT(DISTINCT ps.element_id)
			FROM precast_stock ps
			JOIN element e ON ps.element_id = e.id
			JOIN project p ON e.project_id = p.project_id
			%s AND e.disable = false AND ps.erected = false AND ps.dispatch_status = false AND ps.stockyard = true
		`, projectFilterQuery)
		inStockParams := args
		err = db.QueryRowContext(ctxStats, inStockQuery, inStockParams...).Scan(&inStock)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch in stock count", "details": err.Error()})
			return
		}

		// In production - current month (relaxed date filter)
		inProductionQuery := fmt.Sprintf(`
			SELECT COUNT(DISTINCT a.element_id)
			FROM activity a
			JOIN project p ON a.project_id = p.project_id
			%s AND a.start_date IS NOT NULL
		`, projectFilterQuery)
		inProductionParams := args
		err = db.QueryRowContext(ctxStats, inProductionQuery, inProductionParams...).Scan(&inProduction)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch in production count", "details": err.Error()})
			return
		}

		response := gin.H{
			"casted_elements":  castedElements,
			"erected_elements": erected,
			"in_stock":         inStock,
			"in_production":    inProduction,
			"debug": gin.H{
				"project_filter": projectFilterQuery,
				"args_count":     len(args),
				"role_name":      roleName,
				"user_id":        userID,
				"total_elements": totalElements,
			},
		}

		c.JSON(http.StatusOK, response)

		// Log activity
		logErr := SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched current month's element status counts",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
		if logErr != nil {
			// Log error but don't fail the request
			log.Printf("Failed to save activity log: %v", logErr)
		}
	}
}

// GetStageWiseStatsHandler godoc
// @Summary      Get stage-wise stats (element stages graph)
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_stages_graph [get]
func GetStageWiseStatsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user ID
		ctxUser, cancelUser := utils.GetFastQueryContext(c.Request.Context())
		defer cancelUser()

		var userID int
		err = db.QueryRowContext(ctxUser, "SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get role
		var roleID int
		err = db.QueryRowContext(ctxUser, "SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role ID"})
			return
		}

		var roleName string
		err = db.QueryRowContext(ctxUser, "SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Step 4: Prepare activity filter
		var activityFilter string
		args := []interface{}{}

		switch roleName {
		case "superadmin":
			activityFilter = "1=1" // no filtering
		case "admin":
			activityFilter = `
				activity.project_id IN (
    SELECT project_id FROM project 
    WHERE endclient_id IN (
        SELECT id FROM end_client 
        WHERE client_id IN (
            SELECT client_id FROM client WHERE user_id = $1
        )
    )
)
`
			args = append(args, userID)
		default:
			activityFilter = `
				activity.project_id IN (
					SELECT project_id FROM project_members WHERE user_id = $1
				)`
			args = append(args, userID)
		}

		// Step 5: Final Query with filter injected
		query := fmt.Sprintf(`
			WITH stage_counts AS (
				SELECT 
					s.name AS stage_name,
					COUNT(a.id) AS count
				FROM stages s
				LEFT JOIN project_stages ps ON ps.name = s.name
				LEFT JOIN activity a ON a.stage_id = ps.id AND %s
				GROUP BY s.name
			),
			rejected_count AS (
				SELECT COUNT(*) AS count
				FROM activity
				WHERE (%s) AND (
					status = 'Rejected' OR 
					qc_status = 'Rejected' OR 
					mesh_mold_status = 'Rejected' OR 
					reinforcement_status = 'Rejected' OR 
					meshmold_qc_status = 'Rejected' OR 
					reinforcement_qc_status = 'Rejected'
					AND completed = false
				)
			),
			hold_count AS (
				SELECT COUNT(*) AS count
				FROM activity
				WHERE (%s) AND (
					status = 'Hold' OR 
					qc_status = 'Hold' OR 
					mesh_mold_status = 'Hold' OR 
					reinforcement_status = 'Hold' OR 
					meshmold_qc_status = 'Hold' OR 
					reinforcement_qc_status = 'Hold'
					AND completed = false
				)
			)
			SELECT json_object_agg(key, value) AS result
			FROM (
				SELECT stage_name AS key, count::int AS value FROM stage_counts
				UNION ALL
				SELECT 'rejected', count FROM rejected_count
				UNION ALL
				SELECT 'on_hold', count FROM hold_count
			) AS all_counts;
		`, activityFilter, activityFilter, activityFilter)

		ctxStage, cancelStage := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelStage()

		var result []byte
		err = db.QueryRowContext(ctxStage, query, args...).Scan(&result)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data", "details": err.Error()})
			return
		}

		c.Data(http.StatusOK, "application/json", result)

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched stage-wise stats",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetElementProductionGraphDayWise godoc
// @Summary      Get element production graph (day-wise)
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_graph [get]
func GetElementProductionGraphDayWise(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Authenticate
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

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role ID"})
			return
		}

		var roleName string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		// Step 2: Build project filter
		var projectFilter string
		args := []interface{}{}

		// If specific project_id is provided, filter for that project only
		if specificProjectID != nil {
			projectFilter = "p.project_id = $1 AND p.suspend = false"
			args = append(args, *specificProjectID)
		} else {
			// Use role-based filtering for all projects
			switch roleName {
			case "superadmin":
				projectFilter = "p.suspend = false"
			case "admin":
				projectFilter = `
					p.client_id IN (
    SELECT id FROM end_client 
    WHERE client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
) AND p.suspend = false
`
				args = append(args, userID)
			default:
				projectFilter = `
					p.project_id IN (
						SELECT project_id FROM project_members WHERE user_id = $1
					) AND p.suspend = false`
				args = append(args, userID)
			}
		}

		// Step 3: Run main query
		query := fmt.Sprintf(`
			WITH dates AS (
				SELECT generate_series(CURRENT_DATE - INTERVAL '6 days', CURRENT_DATE, '1 day')::date AS date
			),
			projects AS (
				SELECT p.project_id, p.name AS project_name
				FROM project p
				WHERE %s
			),
			production AS (
				SELECT 
					ps.project_id,
					ps.production_date::date AS date,
					COUNT(DISTINCT ps.element_id) AS count
				FROM precast_stock ps
				INNER JOIN projects pr ON ps.project_id = pr.project_id
				WHERE ps.production_date >= CURRENT_DATE - INTERVAL '6 days'
				GROUP BY ps.project_id, ps.production_date::date
			)
			SELECT 
				d.date::text,
				pr.project_name,
				COALESCE(prod.count, 0) AS produced_count
			FROM dates d
			CROSS JOIN projects pr
			LEFT JOIN production prod 
				ON prod.project_id = pr.project_id AND prod.date = d.date
			ORDER BY d.date, pr.project_name;
		`, projectFilter)

		type Result struct {
			Name        string
			ProjectName string
			Count       int
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Step 4: Build graph map
		graphData := make(map[string]map[string]int) // date -> project -> count
		projectSet := make(map[string]bool)

		for rows.Next() {
			var res Result
			if err := rows.Scan(&res.Name, &res.ProjectName, &res.Count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Scan failed", "details": err.Error()})
				return
			}
			if _, ok := graphData[res.Name]; !ok {
				graphData[res.Name] = make(map[string]int)
			}
			graphData[res.Name][res.ProjectName] = res.Count
			projectSet[res.ProjectName] = true
		}

		// Step 5: Build date list for current month
		now := time.Now()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
		dates := []string{}
		dateToDay := make(map[string]string)
		for d := firstOfMonth; !d.After(lastOfMonth); d = d.AddDate(0, 0, 1) {
			dateStr := d.Format("2006-01-02")
			dates = append(dates, dateStr)
			dateToDay[dateStr] = d.Weekday().String()
		}

		projectKeys := []string{}
		for p := range projectSet {
			projectKeys = append(projectKeys, p)
		}
		sort.Strings(projectKeys)

		// Step 7: Build final result
		result := []gin.H{}
		for _, date := range dates {
			row := gin.H{
				"day":  date,
				"name": dateToDay[date], // include weekday name
			}
			for _, proj := range projectKeys {
				row[proj] = graphData[date][proj] // 0 if not present
			}
			result = append(result, row)
		}

		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched element production graph day-wise",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetTotalWorkersHandler godoc
// @Summary      Get total workers
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/totalworkers [get]
func GetTotalWorkersHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role ID"})
			return
		}

		var roleName string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name"})
			return
		}

		var count int
		switch roleName {
		case "superadmin":
			err = db.QueryRow(`SELECT COUNT(*) FROM project_members`).Scan(&count)

		case "admin":
			query := `
				SELECT COUNT(*) 
FROM project_members pm
WHERE pm.project_id IN (
    SELECT p.project_id 
    FROM project p
    JOIN end_client ec ON p.client_id = ec.id
    WHERE ec.client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
)

			`
			err = db.QueryRow(query, userID).Scan(&count)

		default:
			query := `
				SELECT COUNT(*) 
				FROM project_members 
				WHERE project_id IN (
					SELECT project_id FROM project_members WHERE user_id = $1
				)
			`
			err = db.QueryRow(query, userID).Scan(&count)
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch worker count", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"total_workers": count,
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched total workers count",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetAverageDailyCastingHandler godoc
// @Summary      Get average daily casting
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/average_casted [get]
func GetAverageDailyCastingHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role ID"})
			return
		}

		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		// Step 1: Get the first and last day of the current month
		now := time.Now()
		location := now.Location()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		today := now.Truncate(24 * time.Hour)

		var projectFilter string
		var args []interface{}
		argIndex := 2 // index for date args (we'll use $3 and $4 for dates)

		// If specific project_id is provided, filter for that project only
		if specificProjectID != nil {
			projectFilter = "WHERE e.project_id = $1"
			args = append(args, *specificProjectID)
			argIndex = 2
		} else {
			// Use role-based filtering for all projects
			switch roleName {
			case "superadmin":
				projectFilter = "WHERE e.project_id IN (SELECT project_id FROM project WHERE suspend = false)"
				args = []interface{}{} // Clear any previously added userID
				argIndex = 1           // date args will be $1 and $2
			case "admin":
				projectFilter = `
					WHERE e.project_id IN (
    SELECT p.project_id 
    FROM project p
    JOIN end_client ec ON p.client_id = ec.id
    WHERE ec.client_id IN (
        SELECT client_id FROM client WHERE user_id = $1
    )
    AND p.suspend = false
)
`
				args = append(args, userID)
				argIndex = 2
			default:
				projectFilter = `
					WHERE e.project_id IN (
						SELECT pm.project_id FROM project_members pm
						JOIN project p ON pm.project_id = p.project_id
						WHERE pm.user_id = $1 AND p.suspend = false
					)`
				args = append(args, userID)
				argIndex = 2
			}
		}

		// Step 2: Add production_date filter to projectFilter
		if projectFilter == "" {
			projectFilter = "WHERE"
		} else {
			projectFilter += " AND"
		}
		projectFilter += ` ps.production_date >= $` + fmt.Sprint(argIndex)
		projectFilter += ` AND ps.production_date <= $` + fmt.Sprint(argIndex+1)

		// Add date params to args
		args = append(args, firstOfMonth, today)

		// Step 3: Prepare the query
		query := fmt.Sprintf(`
			SELECT 
				DATE(ps.production_date) AS day, 
				COUNT(DISTINCT ps.element_id) 
			FROM precast_stock ps
			JOIN element e ON ps.element_id = e.id
			%s  
			GROUP BY day
		`, projectFilter)

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		var totalCasted int
		for rows.Next() {
			var day time.Time
			var count int
			if err := rows.Scan(&day, &count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Scan failed", "details": err.Error()})
				return
			}
			totalCasted += count
		}

		// Step 4: Calculate the number of days elapsed so far in the current month
		daysElapsed := int(today.Sub(firstOfMonth).Hours()/24) + 1 // include today

		average := 0.0
		if daysElapsed > 0 {
			average = float64(totalCasted) / float64(daysElapsed)
		}

		c.JSON(http.StatusOK, gin.H{
			"average_casted_elements": average,
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched average daily casting for the current month",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetTotalRejectionsHandler godoc
// @Summary      Get total rejections
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/total_rejections [get]
func GetTotalRejectionsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role ID"})
			return
		}
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		var filter string
		args := []interface{}{}

		// If specific project_id is provided, filter for that project only
		if specificProjectID != nil {
			filter = "project_id = $1"
			args = append(args, *specificProjectID)
		} else {
			// Use role-based filtering for all projects
			switch roleName {
			case "superadmin":
				filter = "1=1"
			case "admin":
				filter = `project_id IN (
        SELECT p.project_id 
        FROM project p
        JOIN end_client ec ON p.client_id = ec.id
        JOIN client c ON ec.client_id = c.client_id
        WHERE c.user_id = $1
    )`
				args = append(args, userID)

			default:
				filter = `project_id IN (
					SELECT project_id FROM project_members WHERE user_id = $1
				)`
				args = append(args, userID)
			}
		}

		query := fmt.Sprintf(`
			SELECT COUNT(*) FROM activity
			WHERE (%s) AND (
				status = 'Rejected' OR 
				qc_status = 'Rejected' OR 
				mesh_mold_status = 'Rejected' OR 
				reinforcement_status = 'Rejected' OR 
				meshmold_qc_status = 'Rejected' OR 
				reinforcement_qc_status = 'Rejected'
				AND completed = false
			)
		`, filter)

		var totalRejections int
		err = db.QueryRow(query, args...).Scan(&totalRejections)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch total rejections", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"total_rejections": totalRejections})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched total rejections count",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetMonthlyRejectionsHandler godoc
// @Summary      Get monthly rejections
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/monthly_rejections [get]
func GetMonthlyRejectionsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role ID"})
			return
		}
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		var filter string
		args := []interface{}{}

		// If specific project_id is provided, filter for that project only
		if specificProjectID != nil {
			filter = "project_id = $1"
			args = append(args, *specificProjectID)
		} else {
			// Use role-based filtering for all projects
			switch roleName {
			case "superadmin":
				filter = "1=1"
			case "admin":
				filter = `project_id IN (
        SELECT p.project_id 
        FROM project p
        JOIN end_client ec ON p.client_id = ec.id
        JOIN client c ON ec.client_id = c.client_id
        WHERE c.user_id = $1
    )`
				args = append(args, userID)

			default:
				filter = `project_id IN (
					SELECT project_id FROM project_members WHERE user_id = $1
				)`
				args = append(args, userID)
			}
		}

		query := fmt.Sprintf(`
			SELECT COUNT(*) FROM activity
			WHERE (%s) 
			AND DATE_TRUNC('month', start_date) = DATE_TRUNC('month', CURRENT_DATE)
			AND (
				status = 'Rejected' OR 
				qc_status = 'Rejected' OR 
				mesh_mold_status = 'Rejected' OR 
				reinforcement_status = 'Rejected' OR 
				meshmold_qc_status = 'Rejected' OR 
				reinforcement_qc_status = 'Rejected'
				AND completed = false
			)
		`, filter)

		var monthlyRejections int
		err = db.QueryRow(query, args...).Scan(&monthlyRejections)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch monthly rejections", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"monthly_rejections": monthlyRejections})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched monthly rejections count",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// Projects struct
type Projects struct {
	ProjectId     int       `json:"project_id"`
	Name          string    `json:"name"`
	Priority      string    `json:"priority"`
	ProjectStatus string    `json:"project_status"`
	StartDate     time.Time `json:"start_date"`
	EndDate       time.Time `json:"end_date"`
	Logo          string    `json:"logo"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastUpdated   time.Time `json:"last_updated"`
	LastUpdatedBy string    `json:"last_updated_by"`
	ClientId      int       `json:"client_id"`
	Budget        string    `json:"budget"`
	TemplateID    int       `json:"template_id"`
	Suspend       bool      `json:"suspend"`
}

// GetProjectsOverviewHandler godoc
// @Summary      Get projects overview
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/projects_overview [get]
func GetProjectsOverviewHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		/* ---------------- PAGINATION ---------------- */
		pageStr := c.Query("page")
		limitStr := c.Query("page_size")

		usePagination := pageStr != "" || limitStr != ""

		page := 1
		limit := 10

		if usePagination {
			page, _ = strconv.Atoi(pageStr)
			limit, _ = strconv.Atoi(limitStr)

			if page < 1 {
				page = 1
			}
			if limit < 1 || limit > 100 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		/* ---------------- SESSION ---------------- */
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		/* ---------------- USER + ROLE ---------------- */
		var userID int
		var roleName string

		err = db.QueryRow(`
			SELECT s.user_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		/* ---------------- FILTER TYPE ---------------- */
		filterType := c.DefaultQuery("type", "all")
		validTypes := map[string]bool{
			"all": true, "active": true, "suspend": true, "inactive": true, "closed": true,
		}
		if !validTypes[filterType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filter type"})
			return
		}

		/* ---------------- SEARCH PARAMS ---------------- */
		name := c.Query("name")
		projectID := c.Query("project_id")
		clientID := c.Query("client_id")
		stockyardID := c.Query("stockyard_id")
		startDate := c.Query("start_date")
		endDate := c.Query("end_date")
		subStart := c.Query("subscription_start_date")
		subEnd := c.Query("subscription_end_date")

		currentDate := time.Now().Format("2006-01-02")

		var conditions []string
		var args []interface{}
		argIndex := 1

		/* ---------------- ACCESS CONTROL ---------------- */
		switch roleName {
		case "superadmin":
			conditions = append(conditions, "1=1")

		case "admin":
			conditions = append(conditions, fmt.Sprintf(`
				p.client_id IN (
					SELECT ec.id
					FROM end_client ec
					JOIN client c ON ec.client_id = c.client_id
					WHERE c.user_id = $%d
				)
			`, argIndex))
			args = append(args, userID)
			argIndex++

		default:
			conditions = append(conditions, fmt.Sprintf(`
				EXISTS (
					SELECT 1 FROM project_members pm
					WHERE pm.project_id = p.project_id AND pm.user_id = $%d
				)
			`, argIndex))
			args = append(args, userID)
			argIndex++
		}

		/* ---------------- STATUS FILTER ---------------- */
		switch filterType {
		case "active":
			conditions = append(conditions,
				fmt.Sprintf("p.start_date <= $%d AND p.end_date >= $%d AND p.suspend = false", argIndex, argIndex+1))
			args = append(args, currentDate, currentDate)
			argIndex += 2

		case "inactive":
			conditions = append(conditions, fmt.Sprintf("p.start_date > $%d", argIndex))
			args = append(args, currentDate)
			argIndex++

		case "closed":
			conditions = append(conditions, fmt.Sprintf("p.end_date < $%d", argIndex))
			args = append(args, currentDate)
			argIndex++

		case "suspend":
			conditions = append(conditions, "p.suspend = true")
		}

		/* ---------------- ADVANCED SEARCH ---------------- */
		if name != "" {
			conditions = append(conditions, fmt.Sprintf("p.name ILIKE $%d", argIndex))
			args = append(args, "%"+name+"%")
			argIndex++
		}

		if projectID != "" {
			conditions = append(conditions, fmt.Sprintf("p.project_id = $%d", argIndex))
			args = append(args, projectID)
			argIndex++
		}

		if clientID != "" {
			conditions = append(conditions, fmt.Sprintf("p.client_id = $%d", argIndex))
			args = append(args, clientID)
			argIndex++
		}

		if stockyardID != "" {
			conditions = append(conditions, fmt.Sprintf(`
				p.project_id IN (
					SELECT project_id FROM project_stockyard WHERE stockyard_id = $%d
				)
			`, argIndex))
			args = append(args, stockyardID)
			argIndex++
		}

		if startDate != "" {
			conditions = append(conditions, fmt.Sprintf("p.start_date >= $%d", argIndex))
			args = append(args, startDate)
			argIndex++
		}

		if endDate != "" {
			conditions = append(conditions, fmt.Sprintf("p.end_date <= $%d", argIndex))
			args = append(args, endDate)
			argIndex++
		}

		if subStart != "" {
			conditions = append(conditions, fmt.Sprintf("p.subscription_start_date >= $%d", argIndex))
			args = append(args, subStart)
			argIndex++
		}

		if subEnd != "" {
			conditions = append(conditions, fmt.Sprintf("p.subscription_end_date <= $%d", argIndex))
			args = append(args, subEnd)
			argIndex++
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT ---------------- */
		var total int
		if err := db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM project p WHERE %s`, whereClause),
			args...,
		).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY + METRICS ---------------- */
		query := fmt.Sprintf(`
			WITH project_metrics AS (
				SELECT 
					p.project_id, p.name, p.priority, p.project_status,
					p.start_date, p.end_date, p.logo, p.description,
					p.created_at, p.updated_at, p.client_id, p.budget,
					p.template_id, p.suspend,
					p.subscription_start_date, p.subscription_end_date,
					COALESCE(e.total_elements, 0),
					COALESCE(ps.casted_elements, 0),
					COALESCE(ps.in_stock, 0),
					COALESCE(a.in_production, 0),
					COALESCE(et.element_type_count, 0),
					COALESCE(pm.project_members_count, 0),
					COALESCE(ps.erected_elements, 0)
				FROM project p
				LEFT JOIN (SELECT project_id, COUNT(*) total_elements FROM element GROUP BY project_id) e ON p.project_id=e.project_id
				LEFT JOIN (
					SELECT e.project_id,
						COUNT(*) FILTER (WHERE ps.stockyard=true AND ps.order_by_erection=false AND ps.erected=false AND ps.dispatch_status=false) casted_elements,
						COUNT(*) FILTER (WHERE ps.stockyard=true) in_stock,
						COUNT(*) FILTER (WHERE ps.erected=true) erected_elements
					FROM precast_stock ps
					JOIN element e ON ps.element_id=e.id
					GROUP BY e.project_id
				) ps ON p.project_id=ps.project_id
				LEFT JOIN (SELECT project_id, COUNT(*) in_production FROM activity WHERE completed=false GROUP BY project_id) a ON p.project_id=a.project_id
				LEFT JOIN (SELECT project_id, COUNT(*) element_type_count FROM element_type GROUP BY project_id) et ON p.project_id=et.project_id
				LEFT JOIN (SELECT project_id, COUNT(*) project_members_count FROM project_members GROUP BY project_id) pm ON p.project_id=pm.project_id
				WHERE %s
				ORDER BY p.project_id
		`, whereClause)
		
		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)
		}
		
		query += `
			)
			SELECT * FROM project_metrics
		`

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var (
			totalElements, castedElements, inStock,
			inProduction, notInProduction,
			elementTypeCount, projectMembersCount int
		)

		var projects []models.ProjectMetricsWithDetails

		for rows.Next() {
			var pm models.ProjectMetricsWithDetails
			if err := rows.Scan(
				&pm.ProjectID, &pm.Name, &pm.Priority, &pm.ProjectStatus,
				&pm.StartDate, &pm.EndDate, &pm.Logo, &pm.Description,
				&pm.CreatedAt, &pm.UpdatedAt, &pm.ClientId, &pm.Budget,
				&pm.TemplateID, &pm.Suspend,
				&pm.SubscriptionStartDate, &pm.SubscriptionEndDate,
				&pm.TotalElements, &pm.CastedElements,
				&pm.InStock, &pm.InProduction,
				&pm.ElementTypeCount, &pm.ProjectMembersCount,
				&pm.ErectedElements,
			); err != nil {
				continue
			}

			totalElements += pm.TotalElements
			castedElements += pm.CastedElements
			inStock += pm.InStock
			inProduction += pm.InProduction
			notInProduction += pm.TotalElements - pm.InProduction
			elementTypeCount += pm.ElementTypeCount
			projectMembersCount += pm.ProjectMembersCount

			projects = append(projects, pm)
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"projects": projects,
			"aggregates": gin.H{
				"total_elements":        totalElements,
				"casted_elements":       castedElements,
				"in_stock":              inStock,
				"in_production":         inProduction,
				"not_in_production":     notInProduction,
				"element_type_count":    elementTypeCount,
				"project_members_count": projectMembersCount,
			},
		}

		// Only include pagination if pagination parameters were provided
		if usePagination {
			response["pagination"] = gin.H{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": int(math.Ceil(float64(total) / float64(limit))),
			}
		}

		c.JSON(http.StatusOK, response)

		go SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Search",
			Description:  "Fetched project overview with aggregates",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

// GetElementStatusBreakdown godoc
// @Summary Get element status breakdown
// @Description Counts elements in different statuses: production, stockyard, dispatch, erection, and not in production, categorized by project and floor
// @Tags Dashboard
// @Accept json
// @Produce json
// @Param Authorization header string true "Session ID"
// @Param project_id path int true "Project ID"
// @Success 200 {object} map[string]interface{} "Element status breakdown by project and floor"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /api/element_status_breakdown/{project_id} [get]
func GetElementStatusBreakdown(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user ID
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get role name
		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}

		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Step 4: Get project ID
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Step 5: Check project access based on role
		hasAccess := false
		if strings.EqualFold(roleName, "superadmin") {
			hasAccess = true
		} else if strings.EqualFold(roleName, "admin") {
			// Check if admin is associated with the project's client via end_client
			var clientID int
			err = db.QueryRow(`
        SELECT c.client_id
        FROM project p
        JOIN end_client ec ON p.client_id = ec.id
        JOIN client c ON ec.client_id = c.client_id
        WHERE p.project_id = $1
    `, projectID).Scan(&clientID)

			if err == nil {
				var count int
				err = db.QueryRow(`
            SELECT COUNT(*) 
            FROM users 
            WHERE id = $1 AND client_id = $2
        `, userID, clientID).Scan(&count)
				hasAccess = (err == nil && count > 0)
			}
		} else {
			// Check if user is a project member
			var count int
			err = db.QueryRow("SELECT COUNT(*) FROM project_members WHERE user_id = $1 AND project_id = $2", userID, projectID).Scan(&count)
			hasAccess = (err == nil && count > 0)
		}

		if !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this project"})
			return
		}

		// Step 6: Get all hierarchies for the project with context timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, `
			SELECT 
				id, project_id, name, description, parent_id, 
				prefix, naming_convention
			FROM precast 
			WHERE project_id = $1
			ORDER BY parent_id NULLS FIRST, name ASC
		`, projectID)
		if err != nil {
			log.Printf("Failed to fetch hierarchies for project %d: %v", projectID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project hierarchies"})
			return
		}
		defer rows.Close()

		// Step 7: Build response
		response := make(map[string]interface{})

		// Initialize project totals
		projectTotals := map[string]interface{}{
			"totalelement":      0,
			"production":        0,
			"stockyard":         0,
			"erection":          0,
			"notinproduction":   0,
			"concrete_required": 0.0,
			"concrete_used":     0.0,
			"concrete_balance":  0.0,
		}

		// Track towers and floors separately
		towers := make(map[string]interface{})
		floors := make(map[string]interface{})

		for rows.Next() {
			var hierarchyID, projectID int
			var name, description, prefix, namingConvention string
			var parentID sql.NullInt64

			err := rows.Scan(
				&hierarchyID, &projectID, &name, &description, &parentID,
				&prefix, &namingConvention,
			)
			if err != nil {
				log.Printf("Failed to scan hierarchy row: %v", err)
				continue
			}

			isTower := !parentID.Valid
			log.Printf("Processing hierarchy %d (%s): isTower=%v", hierarchyID, name, isTower)

			if isTower {
				// OPTIMIZED: Fetch all child floors in one query with context timeout
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				childRows, err := db.QueryContext(ctx, `
					SELECT id, name FROM precast 
					WHERE project_id = $1 AND parent_id = $2
					ORDER BY name ASC
				`, projectID, hierarchyID)
				if err != nil {
					log.Printf("Failed to fetch child hierarchies for tower %d: %v", hierarchyID, err)
					continue
				}

				var childHierarchyIDs []int
				var childHierarchyNames []string
				for childRows.Next() {
					var childID int
					var childName string
					if err := childRows.Scan(&childID, &childName); err == nil {
						childHierarchyIDs = append(childHierarchyIDs, childID)
						childHierarchyNames = append(childHierarchyNames, childName)
					}
				}
				childRows.Close()

				log.Printf("Tower %d (%s) has %d child floors", hierarchyID, name, len(childHierarchyIDs))

				// Create tower structure - we'll calculate totals from floor data
				towerStructure := map[string]interface{}{
					"totalelement":      0,
					"production":        0,
					"stockyard":         0,
					"erection":          0,
					"notinproduction":   0,
					"concrete_required": 0.0,
					"concrete_used":     0.0,
					"concrete_balance":  0.0,
				}

				// OPTIMIZED: Fetch all element breakdowns for all floors in this tower in bulk
				bulkBreakdowns := getElementTypeBreakdownByHierarchiesBulk(db, projectID, childHierarchyIDs)

				// Add floors directly to the tower structure (without "floors" wrapper)
				for i, childID := range childHierarchyIDs {
					floorName := childHierarchyNames[i]

					// Get element type breakdown from bulk results
					elementTypeBreakdown := bulkBreakdowns[childID]
					if elementTypeBreakdown == nil {
						elementTypeBreakdown = make(map[string]interface{})
					}

					// Calculate floor totals from element type breakdowns to ensure consistency
					var floorTotal, floorProduction, floorStockyard, floorErection, floorNotInProduction int
					var floorConcreteRequired, floorConcreteUsed, floorConcreteBalance float64

					for _, elementTypeData := range elementTypeBreakdown {
						if elementData, ok := elementTypeData.(map[string]interface{}); ok {
							floorTotal += elementData["totalelement"].(int)
							floorProduction += elementData["production"].(int)
							floorStockyard += elementData["stockyard"].(int)
							floorErection += elementData["erection"].(int)
							floorNotInProduction += elementData["notinproduction"].(int)

							// Map concrete values from function return format
							totalelementConcrete := elementData["totalelement_concrete"].(float64)
							productionConcrete := elementData["production_concrete"].(float64)

							floorConcreteRequired += totalelementConcrete
							floorConcreteUsed += productionConcrete
							floorConcreteBalance += (totalelementConcrete - productionConcrete)
						}
					}

					// Create floor data with floor-level totals
					floorData := map[string]interface{}{
						"totalelement":      floorTotal,
						"production":        floorProduction,
						"stockyard":         floorStockyard,
						"erection":          floorErection,
						"notinproduction":   floorNotInProduction,
						"concrete_required": floorConcreteRequired,
						"concrete_used":     floorConcreteUsed,
						"concrete_balance":  floorConcreteBalance,
					}

					// Add element types directly to the floor object
					for elementTypeKey, elementTypeData := range elementTypeBreakdown {
						floorData[elementTypeKey] = elementTypeData
					}

					towerStructure[floorName] = floorData
					floors[floorName] = floorData // Also add to main floors map for backward compatibility

					// Accumulate tower totals from floor data
					towerStructure["totalelement"] = towerStructure["totalelement"].(int) + floorTotal
					towerStructure["production"] = towerStructure["production"].(int) + floorProduction
					towerStructure["stockyard"] = towerStructure["stockyard"].(int) + floorStockyard
					towerStructure["erection"] = towerStructure["erection"].(int) + floorErection
					towerStructure["notinproduction"] = towerStructure["notinproduction"].(int) + floorNotInProduction
					towerStructure["concrete_required"] = towerStructure["concrete_required"].(float64) + floorConcreteRequired
					towerStructure["concrete_used"] = towerStructure["concrete_used"].(float64) + floorConcreteUsed
					towerStructure["concrete_balance"] = towerStructure["concrete_balance"].(float64) + floorConcreteBalance
				}

				// Accumulate project totals from tower totals
				projectTotals["totalelement"] = projectTotals["totalelement"].(int) + towerStructure["totalelement"].(int)
				projectTotals["production"] = projectTotals["production"].(int) + towerStructure["production"].(int)
				projectTotals["stockyard"] = projectTotals["stockyard"].(int) + towerStructure["stockyard"].(int)
				projectTotals["erection"] = projectTotals["erection"].(int) + towerStructure["erection"].(int)
				projectTotals["notinproduction"] = projectTotals["notinproduction"].(int) + towerStructure["notinproduction"].(int)
				projectTotals["concrete_required"] = projectTotals["concrete_required"].(float64) + towerStructure["concrete_required"].(float64)
				projectTotals["concrete_used"] = projectTotals["concrete_used"].(float64) + towerStructure["concrete_used"].(float64)
				projectTotals["concrete_balance"] = projectTotals["concrete_balance"].(float64) + towerStructure["concrete_balance"].(float64)

				towers[name] = towerStructure

				// We'll accumulate project totals after calculating floor totals

				log.Printf("Added tower %s to response with %d floors", name, len(childHierarchyIDs))

			} else {
				// Single floor (not under any tower)
				// OPTIMIZED: Use bulk function even for single floor (it handles single items efficiently)
				bulkBreakdowns := getElementTypeBreakdownByHierarchiesBulk(db, projectID, []int{hierarchyID})
				elementTypeBreakdown := bulkBreakdowns[hierarchyID]
				if elementTypeBreakdown == nil {
					elementTypeBreakdown = make(map[string]interface{})
				}

				// Calculate floor totals from element type breakdowns to ensure consistency
				var floorTotal, floorProduction, floorStockyard, floorErection, floorNotInProduction int
				var floorConcreteRequired, floorConcreteUsed, floorConcreteBalance float64

				for _, elementTypeData := range elementTypeBreakdown {
					if elementData, ok := elementTypeData.(map[string]interface{}); ok {
						floorTotal += elementData["totalelement"].(int)
						floorProduction += elementData["production"].(int)
						floorStockyard += elementData["stockyard"].(int)
						floorErection += elementData["erection"].(int)
						floorNotInProduction += elementData["notinproduction"].(int)

						// Map concrete values from function return format
						totalelementConcrete := elementData["totalelement_concrete"].(float64)
						productionConcrete := elementData["production_concrete"].(float64)

						floorConcreteRequired += totalelementConcrete
						floorConcreteUsed += productionConcrete
						floorConcreteBalance += (totalelementConcrete - productionConcrete)
					}
				}

				floorData := map[string]interface{}{
					"totalelement":      floorTotal,
					"production":        floorProduction,
					"stockyard":         floorStockyard,
					"erection":          floorErection,
					"notinproduction":   floorNotInProduction,
					"concrete_required": floorConcreteRequired,
					"concrete_used":     floorConcreteUsed,
					"concrete_balance":  floorConcreteBalance,
				}

				// Add element types directly to the floor object
				for elementTypeKey, elementTypeData := range elementTypeBreakdown {
					floorData[elementTypeKey] = elementTypeData
				}

				floors[name] = floorData

				// Accumulate project totals
				projectTotals["totalelement"] = projectTotals["totalelement"].(int) + floorTotal
				projectTotals["production"] = projectTotals["production"].(int) + floorProduction
				projectTotals["stockyard"] = projectTotals["stockyard"].(int) + floorStockyard
				projectTotals["erection"] = projectTotals["erection"].(int) + floorErection
				projectTotals["notinproduction"] = projectTotals["notinproduction"].(int) + floorNotInProduction
				projectTotals["concrete_required"] = projectTotals["concrete_required"].(float64) + floorConcreteRequired
				projectTotals["concrete_used"] = projectTotals["concrete_used"].(float64) + floorConcreteUsed
				projectTotals["concrete_balance"] = projectTotals["concrete_balance"].(float64) + floorConcreteBalance

				log.Printf("Added floor %s to response", name)
			}
		}

		// Build final response structure
		response["totalelement"] = projectTotals["totalelement"]
		response["production"] = projectTotals["production"]
		response["stockyard"] = projectTotals["stockyard"]
		response["erection"] = projectTotals["erection"]
		response["notinproduction"] = projectTotals["notinproduction"]
		response["concrete_required"] = projectTotals["concrete_required"]
		response["concrete_used"] = projectTotals["concrete_used"]
		response["concrete_balance"] = projectTotals["concrete_balance"]

		// Add towers and floors to response
		if len(towers) > 0 {
			response["towers"] = towers
		}

		// Add individual floors (for backward compatibility and single floors)
		for floorName, floorData := range floors {
			response[floorName] = floorData
		}

		log.Printf("Final response has %d entries, %d towers, %d floors", len(response), len(towers), len(floors))
		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched element status breakdown by project and floor",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetProductionReports godoc
// @Summary      Get production reports for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/production_reports/{project_id} [get]
func GetProductionReports(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // default to yearly
		yearStr := c.Query("year")
		monthStr := c.Query("month")

		layout := "2006-01-02"
		location := time.Now().Location()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			now := time.Now()
			isCurrentYear := now.Year() == year

			monthlyData := make([]map[string]interface{}, 0)
			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)

				counts := fetchCountsForRange(db, projectID, start, end)
				monthlyData = append(monthlyData, gin.H{
					"name": start.Month().String(),
					// "name":       year,
					"planned":           counts.Planned,
					"casted":            counts.Casted,
					"stockyard":         counts.Stockyard,
					"dispatch":          counts.Dispatch,
					"erected":           counts.Erected,
					"concrete_required": counts.ConcreteRequired,
					"concrete_used":     counts.ConcreteUsed,
					"concrete_balance":  counts.ConcreteBalance,
				})
			}
			c.JSON(http.StatusOK, monthlyData)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			daysInMonth := start.AddDate(0, 1, -1).Day()

			rangeData := make([]map[string]interface{}, 0)
			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				counts := fetchCountsForRange(db, projectID, startRange, endRange)
				rangeData = append(rangeData, gin.H{
					"name":              fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
					"planned":           counts.Planned,
					"casted":            counts.Casted,
					"stockyard":         counts.Stockyard,
					"dispatch":          counts.Dispatch,
					"erected":           counts.Erected,
					"concrete_required": counts.ConcreteRequired,
					"concrete_used":     counts.ConcreteUsed,
					"concrete_balance":  counts.ConcreteBalance,
				})
			}
			c.JSON(http.StatusOK, rangeData)

		case "weekly":
			yearStr := c.Query("year")
			monthStr := c.Query("month")
			dayStr := c.Query("date")

			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}

			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))

			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			weekData := make([]map[string]interface{}, 0)
			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				counts := fetchCountsForRange(db, projectID, currentDate, currentDate)
				weekData = append(weekData, gin.H{
					"name":              currentDate.Format(layout),
					"planned":           counts.Planned,
					"casted":            counts.Casted,
					"stockyard":         counts.Stockyard,
					"dispatch":          counts.Dispatch,
					"erected":           counts.Erected,
					"concrete_required": counts.ConcreteRequired,
					"concrete_used":     counts.ConcreteUsed,
					"concrete_balance":  counts.ConcreteBalance,
				})
			}
			c.JSON(http.StatusOK, weekData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
		}

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched production reports",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

func padZero(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

type SummaryCounts struct {
	Planned          int
	Casted           int
	Stockyard        int
	Dispatch         int
	Erected          int
	ConcreteRequired float64
	ConcreteUsed     float64
	ConcreteBalance  float64
}

func fetchCountsForRange(db *sql.DB, projectID int, start, end time.Time) SummaryCounts {
	var planned, casted, stockyard, erected, dispatch int
	var concreteRequired, concreteUsed, concreteBalance float64

	// Planned from activity + precast_stock
	plannedQuery := `
		SELECT COUNT(DISTINCT element_id)
		FROM activity
		WHERE project_id = $1 AND start_date BETWEEN $2 AND $3`
	_ = db.QueryRow(plannedQuery, projectID, start, end).Scan(&planned)

	castedQuery := `
		SELECT COUNT(DISTINCT element_id)
		FROM precast_stock
		WHERE project_id = $1 AND production_date BETWEEN $2 AND $3`
	_ = db.QueryRow(castedQuery, projectID, start, end).Scan(&casted)

	dispatchQuery := `
		SELECT COUNT(DISTINCT element_id)
		FROM precast_stock
		WHERE project_id = $1 AND dispatch_status = true AND dispatch_start >= $2 AND dispatch_end <= $3`
	_ = db.QueryRow(dispatchQuery, projectID, start, end).Scan(&dispatch)

	// Erected - use DATE() function to match date ranges properly
	erectedQuery := `
		SELECT COUNT(DISTINCT element_id)
		FROM precast_stock
		WHERE project_id = $1 AND erected = true AND DATE(updated_at) BETWEEN DATE($2) AND DATE($3)`
	_ = db.QueryRow(erectedQuery, projectID, start, end).Scan(&erected)

	stockyard = int(math.Abs(float64(casted - erected)))

	return SummaryCounts{Planned: planned, Casted: casted, Stockyard: stockyard, Dispatch: dispatch, Erected: erected, ConcreteRequired: concreteRequired, ConcreteUsed: concreteUsed, ConcreteBalance: concreteBalance}
}

// func GetQCReports(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		sessionID := c.GetHeader("Authorization")
// 		if sessionID == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
// 			return
// 		}
// 		session, userName, err := GetSessionDetails(db, sessionID)
// 		if err != nil {
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
// 			return
// 		}

// 		projectID, err := strconv.Atoi(c.Param("project_id"))
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
// 			return
// 		}

// 		summaryType := c.DefaultQuery("type", "yearly")
// 		yearStr := c.Query("year")
// 		monthStr := c.Query("month")

// 		layout := "2006-01-02"
// 		location := time.Now().Location()

// 		// Fetch QA/QC user IDs
// 		qcUserIDs := []int{}
// 		qcQuery := `
// 			SELECT pm.user_id
// 			FROM project_members pm
// 			JOIN roles r ON pm.role_id = r.role_id
// 			WHERE pm.project_id = $1
// 			AND r.role_name IN ('QA', 'QC', 'QA/QC', 'MEP QC')`
// 		rows, err := db.Query(qcQuery, projectID)
// 		if err == nil {
// 			defer rows.Close()
// 			for rows.Next() {
// 				var uid int
// 				if err := rows.Scan(&uid); err == nil {
// 					qcUserIDs = append(qcUserIDs, uid)
// 				}
// 			}
// 		}

// 		if len(qcUserIDs) == 0 {
// 			c.JSON(http.StatusOK, gin.H{"message": "No QA/QC users found", "data": []interface{}{}})
// 			return
// 		}

// 		// Utility for counting statuses within a time range
// 		getStatusCounts := func(start, end time.Time) map[string]int {
// 			statusCounts := map[string]int{
// 				"completed": 0,
// 				"rejected":  0,
// 				"hold":      0,
// 			}
// 			params := []interface{}{projectID, start, end}
// 			placeholders := []string{}
// 			for i, uid := range qcUserIDs {
// 				params = append(params, uid)
// 				placeholders = append(placeholders, fmt.Sprintf("$%d", i+4))
// 			}

// 			query := fmt.Sprintf(`
// 				SELECT status, COUNT(*)
// 				FROM complete_production
// 				WHERE project_id = $1
// 				AND updated_at BETWEEN $2 AND $3
// 				AND user_id IN (%s)
// 				AND status IN ('completed', 'rejected', 'hold')
// 				GROUP BY status`, strings.Join(placeholders, ","))

// 			rows, err := db.Query(query, params...)
// 			if err == nil {
// 				defer rows.Close()
// 				for rows.Next() {
// 					var status string
// 					var count int
// 					_ = rows.Scan(&status, &count)
// 					statusCounts[status] = count
// 				}
// 			}
// 			return statusCounts
// 		}

// 		// Helper struct and function for concrete counts
// 		type ConcreteCounts struct {
// 			Required float64
// 			Used     float64
// 			Balance  float64
// 		}

// 		getConcreteCounts := func(start, end time.Time) ConcreteCounts {
// 			var required, used, balance float64
// 			query := `
// 				WITH element_status AS (
// 					SELECT
// 						e.id,
// 						et.thickness,
// 						et.length,
// 						et.height,
// 						CASE
// 							WHEN a.id IS NOT NULL THEN 'production'
// 							WHEN ps.id IS NOT NULL AND ps.stockyard = true AND ps.dispatch_status = false AND ps.erected = false THEN 'stockyard'
// 							WHEN ps.id IS NOT NULL AND ps.erected = true THEN 'erection'
// 							ELSE 'notinproduction'
// 						END AS element_status
// 					FROM element e
// 					JOIN element_type et ON e.element_type_id = et.element_type_id
// 					LEFT JOIN activity a ON e.id = a.element_id
// 					LEFT JOIN precast_stock ps ON e.id = ps.element_id
// 					WHERE e.project_id = $1 AND ((a.start_date BETWEEN $2 AND $3) OR (ps.created_at BETWEEN $2 AND $3))
// 				)
// 				SELECT
// 					COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as total_concrete_required,
// 					COALESCE(SUM(CASE
// 						WHEN element_status IN ('production', 'stockyard', 'erection')
// 						THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000
// 						ELSE 0
// 					END), 0) as total_concrete_used,
// 					COALESCE(SUM(CASE
// 						WHEN element_status = 'notinproduction'
// 						THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000
// 						ELSE 0
// 					END), 0) as total_concrete_balance
// 				FROM element_status`
// 			db.QueryRow(query, projectID, start, end).Scan(&required, &used, &balance)
// 			return ConcreteCounts{required, used, balance}
// 		}

// 		now := time.Now()
// 		switch summaryType {
// 		case "yearly":
// 			year, _ := strconv.Atoi(yearStr)
// 			isCurrentYear := now.Year() == year

// 			results := []gin.H{}
// 			for m := 1; m <= 12; m++ {
// 				if isCurrentYear && m > int(now.Month()) {
// 					break
// 				}
// 				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
// 				end := start.AddDate(0, 1, -1)
// 				counts := getStatusCounts(start, end)
// 				concrete := getConcreteCounts(start, end)
// 				results = append(results, gin.H{
// 					"name":                    start.Month().String(),
// 					"approved":                counts["completed"],
// 					"rejected":                counts["rejected"],
// 					"hold":                    counts["hold"],
// 					"total_concrete_required": concrete.Required,
// 					"total_concrete_used":     concrete.Used,
// 					"total_concrete_balance":  concrete.Balance,
// 				})
// 			}
// 			c.JSON(http.StatusOK, results)

// 		case "monthly":
// 			year, _ := strconv.Atoi(yearStr)
// 			month, _ := strconv.Atoi(monthStr)
// 			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
// 			daysInMonth := start.AddDate(0, 1, -1).Day()

// 			results := []gin.H{}
// 			for day := 1; day <= daysInMonth; day += 5 {
// 				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
// 				endRange := startRange.AddDate(0, 0, 4)
// 				if endRange.Day() > daysInMonth {
// 					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
// 				}
// 				counts := getStatusCounts(startRange, endRange)
// 				concrete := getConcreteCounts(startRange, endRange)
// 				results = append(results, gin.H{
// 					"name":                    fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
// 					"approved":                counts["completed"],
// 					"rejected":                counts["rejected"],
// 					"hold":                    counts["hold"],
// 					"total_concrete_required": concrete.Required,
// 					"total_concrete_used":     concrete.Used,
// 					"total_concrete_balance":  concrete.Balance,
// 				})
// 			}
// 			c.JSON(http.StatusOK, results)

// 		case "weekly":
// 			yearStr := c.Query("year")
// 			monthStr := c.Query("month")
// 			dayStr := c.Query("date")

// 			if yearStr == "" || monthStr == "" || dayStr == "" {
// 				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
// 				return
// 			}

// 			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))

// 			startDate, err := time.Parse("2006-01-02", startDateStr)
// 			if err != nil {
// 				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
// 				return
// 			}

// 			// Get 7-day window ending at startDate
// 			weekStart := startDate.AddDate(0, 0, -6)

// 			layout := "2006-01-02"
// 			results := []gin.H{}

// 			for i := 0; i < 7; i++ {
// 				currentDate := weekStart.AddDate(0, 0, i)
// 				if currentDate.Month() != startDate.Month() {
// 					break
// 				}

// 				counts := getStatusCounts(currentDate, currentDate)
// 				concrete := getConcreteCounts(currentDate, currentDate)

// 				results = append(results, gin.H{
// 					"name":                    currentDate.Format(layout),
// 					"approved":                counts["completed"],
// 					"rejected":                counts["rejected"],
// 					"hold":                    counts["hold"],
// 					"total_concrete_required": concrete.Required,
// 					"total_concrete_used":     concrete.Used,
// 					"total_concrete_balance":  concrete.Balance,
// 				})
// 			}

// 			c.JSON(http.StatusOK, results)

// 		default:
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid summary type"})
// 		}

// 		log := models.ActivityLog{
// 			EventContext: "Dashboard",
// 			EventName:    "Get",
// 			Description:  "Fetched qc status counts graph",
// 			UserName:     userName,
// 			HostName:     session.HostName,
// 			IPAddress:    session.IPAddress,
// 			CreatedAt:    time.Now(),
// 			ProjectID:    0, // No specific project ID for this operation
// 		}

// 		// Step 5: Insert activity log
// 		if logErr := SaveActivityLog(db, log); logErr != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":   "Failed to log activity",
// 				"details": logErr.Error(),
// 			})
// 			return
// 		}
// 	}
// }

// GetQCReports godoc
// @Summary      Get QC reports for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/qc_reports/{project_id} [get]
func GetQCReports(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")

		location := time.Now().Location()

		// ================= QC USERS =================

		var qcUserIDs []int
		rows, err := db.Query(`
			SELECT pm.user_id
			FROM project_members pm
			JOIN roles r ON pm.role_id = r.role_id
			WHERE pm.project_id = $1
			AND r.role_name IN ('QA','QC','QA/QC','MEP QC')
		`, projectID)

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			rows.Scan(&id)
			qcUserIDs = append(qcUserIDs, id)
		}

		if len(qcUserIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
			return
		}

		// ================= STATUS COUNTER =================

		getCounts := func(start, end time.Time) (approved int, pending int) {

			// ----- APPROVED / REJECTED / HOLD -----

			rows, _ := db.Query(`
				SELECT LOWER(status), COUNT(*)
				FROM complete_production
				WHERE project_id = $1
				AND updated_at BETWEEN $2 AND $3
				AND user_id = ANY($4)
				GROUP BY status
			`, projectID, start, end, pq.Array(qcUserIDs))

			for rows.Next() {
				var status string
				var count int
				rows.Scan(&status, &count)

				switch status {
				case "completed":
					approved = count
				}
			}
			rows.Close()

			// ----- QC PENDING (FROM ACTIVITY) -----

			db.QueryRow(`
				SELECT COUNT(*)
				FROM activity a
				WHERE a.project_id = $1
				AND a.start_date BETWEEN $2 AND $3
				AND (
					(a.mesh_mold_status = 'completed' AND a.meshmold_qc_status != 'completed')
					OR
					(a.reinforcement_status = 'completed' AND a.reinforcement_qc_status != 'completed')
					OR
					(a.status = 'completed' AND a.qc_status != 'completed')
				)
			`, projectID, start, end).Scan(&pending)

			return
		}

		// ================= RESPONSE BUILDER =================

		now := time.Now()
		results := []gin.H{}

		switch summaryType {

		// ---------- YEARLY ----------

		case "yearly":
			year, _ := strconv.Atoi(yearStr)

			for m := 1; m <= 12; m++ {

				if year == now.Year() && m > int(now.Month()) {
					break
				}

				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

				approved, pending := getCounts(start, end)

				results = append(results, gin.H{
					"name":     start.Month().String(),
					"approved": approved,
					"pending":  pending,
				})
			}

		// ---------- MONTHLY ----------

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)

			startMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			days := startMonth.AddDate(0, 1, -1).Day()

			for d := 1; d <= days; d += 5 {

				start := time.Date(year, time.Month(month), d, 0, 0, 0, 0, location)
				end := start.AddDate(0, 0, 4)

				if end.Day() > days {
					end = time.Date(year, time.Month(month), days, 23, 59, 59, 0, location)
				}

				approved, pending := getCounts(start, end)

				results = append(results, gin.H{
					"name":     fmt.Sprintf("%s to %s", start.Format("2006-01-02"), end.Format("2006-01-02")),
					"approved": approved,
					"pending":  pending,
				})
			}

		// ---------- WEEKLY ----------

		case "weekly":

			dateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(c.Query("date")))
			endDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid date"})
				return
			}

			startWeek := endDate.AddDate(0, 0, -6)

			for i := 0; i < 7; i++ {

				current := startWeek.AddDate(0, 0, i)

				approved, pending := getCounts(current, current)

				results = append(results, gin.H{
					"name":     current.Format("2006-01-02"),
					"approved": approved,
					"pending":  pending,
				})
			}

		default:
			c.JSON(400, gin.H{"error": "Invalid type"})
			return
		}

		c.JSON(http.StatusOK, results)

		// ================= ACTIVITY LOG =================

		SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched QC summary graph",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

// GetQCReportsStageWise godoc
// @Summary      Get QC reports stage-wise for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/qc_reports_stagewise/{project_id} [get]
func GetQCReportsStageWise(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		log.Println("\n========== GetQCReportsStageWise START ==========")

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		if _, _, err := GetSessionDetails(db, sessionID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		location := time.Now().Location()

		// ================= QC USERS =================

		var qcUserIDs []int

		rows, err := db.Query(`
			SELECT pm.user_id
			FROM project_members pm
			JOIN roles r ON pm.role_id = r.role_id
			WHERE pm.project_id = $1
			AND r.role_name IN ('QA','QC','QA/QC','MEP QC')
		`, projectID)

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			rows.Scan(&id)
			qcUserIDs = append(qcUserIDs, id)
		}

		if len(qcUserIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
			return
		}

		// ================= TIME RANGE =================

		var start, end time.Time

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

		case "weekly":
			dateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			endDate, _ := time.Parse("2006-01-02", dateStr)
			start = endDate.AddDate(0, 0, -6)
			end = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		}

		type StageStatusCount struct {
			Stage  string `json:"stage"`
			Status string `json:"status"`
			Count  int    `json:"count"`
		}

		results := []StageStatusCount{}

		// ================= APPROVED =================

		rows, err = db.Query(`
			SELECT ps.name, COUNT(*)
			FROM complete_production cp
			JOIN project_stages ps ON ps.id = cp.stage_id
			WHERE cp.project_id = $1
			AND cp.started_at BETWEEN $2 AND $3
			AND cp.user_id = ANY($4)
			AND LOWER(cp.status) = 'completed'
			GROUP BY ps.name
		`, projectID, start, end, pq.Array(qcUserIDs))

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var stage string
			var count int
			rows.Scan(&stage, &count)

			results = append(results, StageStatusCount{
				Stage:  normalize(stage),
				Status: "approved",
				Count:  count,
			})
		}

		// ================= PENDING =================

		rows, err = db.Query(`
			SELECT ps.name, COUNT(*)
			FROM activity a
			JOIN project_stages ps ON ps.id = a.stage_id
			WHERE a.project_id = $1
			AND a.start_date BETWEEN $2 AND $3
			AND (
				(a.mesh_mold_status = 'completed' AND a.meshmold_qc_status != 'completed')
				OR
				(a.reinforcement_status = 'completed' AND a.reinforcement_qc_status != 'completed')
				OR
				(a.status = 'completed' AND a.qc_status != 'completed')
			)
			GROUP BY ps.name
		`, projectID, start, end)

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var stage string
			var count int
			rows.Scan(&stage, &count)

			results = append(results, StageStatusCount{
				Stage:  normalize(stage),
				Status: "pending",
				Count:  count,
			})
		}

		// ================= LOAD ALL STAGES =================

		stageRows, err := db.Query(`
			SELECT name
			FROM project_stages
			WHERE project_id = $1
			ORDER BY "order"
		`, projectID)

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer stageRows.Close()

		var allStages []string
		for stageRows.Next() {
			var s string
			stageRows.Scan(&s)
			allStages = append(allStages, normalize(s))
		}

		// ================= ZERO FILL =================

		countMap := map[string]int{}
		for _, r := range results {
			countMap[r.Stage+"|"+r.Status] = r.Count
		}

		finalResults := []StageStatusCount{}
		statuses := []string{"approved", "pending"}

		for _, stage := range allStages {
			for _, status := range statuses {
				key := stage + "|" + status
				finalResults = append(finalResults, StageStatusCount{
					Stage:  stage,
					Status: status,
					Count:  countMap[key],
				})
			}
		}

		c.JSON(http.StatusOK, gin.H{"data": finalResults})
	}
}

// ================= NORMALIZER =================

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// GetElementTypeStatusBreakdownByMultipleHierarchies godoc
// @Summary Get element type status breakdown for multiple hierarchies
// @Description Fetches element type status breakdown for multiple hierarchy IDs in a project
// @Tags Dashboard
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param hierarchy_ids query string true "Comma-separated hierarchy IDs"
// @Param Authorization header string true "Session ID"
// @Success 200 {object} map[string]interface{} "Element type status breakdown"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /api/element_type_status_breakdown_multiple/{project_id} [get]

// elementTypeBreakdownResponseOrdered marshals as JSON with summary keys first, then floor keys in numeric order (Floor 1, Floor 2, ... Floor 10, ...).
type elementTypeBreakdownResponseOrdered struct {
	Data map[string]interface{}
}

// summaryKeysOrder is the desired order for aggregate/summary keys in the response.
var elementTypeBreakdownSummaryKeysOrder = []string{
	"totalelement", "production", "balance", "dispatch", "stockyard", "erected", "erection", "notinproduction", "erectedbalance",
	"totalelement_concrete", "production_concrete", "balance_concrete", "dispatch_concrete", "stockyard_concrete",
	"erected_concrete", "erection_concrete", "notinproduction_concrete", "erectedbalance_concrete",
}

func (r elementTypeBreakdownResponseOrdered) MarshalJSON() ([]byte, error) {
	summarySet := make(map[string]bool)
	for _, k := range elementTypeBreakdownSummaryKeysOrder {
		summarySet[k] = true
	}
	var floorKeys []string
	var otherKeys []string
	for k := range r.Data {
		if summarySet[k] {
			continue
		}
		if strings.HasPrefix(k, "Floor ") {
			floorKeys = append(floorKeys, k)
		} else {
			otherKeys = append(otherKeys, k)
		}
	}
	// Sort floor keys by numeric part: "Floor 1", "Floor 2", ... "Floor 10", "Floor 11"
	sort.Slice(floorKeys, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(floorKeys[i], "Floor")))
		nj, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(floorKeys[j], "Floor")))
		return ni < nj
	})
	// Build ordered key list: summary in fixed order, then floors, then others
	ordered := make([]string, 0, len(r.Data))
	for _, k := range elementTypeBreakdownSummaryKeysOrder {
		if _, ok := r.Data[k]; ok {
			ordered = append(ordered, k)
		}
	}
	ordered = append(ordered, floorKeys...)
	ordered = append(ordered, otherKeys...)
	// Marshal as ordered object
	buf := strings.Builder{}
	buf.WriteByte('{')
	for i, k := range ordered {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(k)
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := json.Marshal(r.Data[k])
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return []byte(buf.String()), nil
}

func GetElementTypeStatusBreakdownByMultipleHierarchies(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user ID and role in a single query
		var userID, roleID int
		var roleName string
		err = db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name 
			FROM session s 
			JOIN users u ON u.id = s.user_id
			JOIN roles r ON r.role_id = u.role_id
			WHERE s.session_id = $1
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get project ID
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Step 4: Check project access based on role (optimized with single query)
		hasAccess := false
		if strings.EqualFold(roleName, "superadmin") {
			hasAccess = true
		} else {
			var count int
			if strings.EqualFold(roleName, "admin") {
				err = db.QueryRow(`
        SELECT COUNT(*) 
        FROM project p
        JOIN end_client ec ON p.client_id = ec.id
        JOIN client c ON ec.client_id = c.client_id
        JOIN users u ON u.id = c.user_id
        WHERE p.project_id = $1 AND u.id = $2
    `, projectID, userID).Scan(&count)
			} else {
				err = db.QueryRow(`
					SELECT COUNT(*) FROM project_members 
					WHERE user_id = $1 AND project_id = $2
				`, userID, projectID).Scan(&count)
			}
			hasAccess = (err == nil && count > 0)
		}

		if !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this project"})
			return
		}

		// Step 5: Parse hierarchy IDs
		hierarchyIDsStr := c.Query("hierarchy_ids")
		if hierarchyIDsStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing hierarchy_ids parameter"})
			return
		}

		hierarchyIDStrs := strings.Split(hierarchyIDsStr, ",")
		var hierarchyIDs []int
		for _, idStr := range hierarchyIDStrs {
			id, err := strconv.Atoi(strings.TrimSpace(idStr))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid hierarchy ID: " + idStr})
				return
			}
			hierarchyIDs = append(hierarchyIDs, id)
		}

		// Step 6: Batch fetch all hierarchy details at once
		placeholders := make([]string, len(hierarchyIDs))
		args := make([]interface{}, len(hierarchyIDs)+1)
		args[0] = projectID
		for i, id := range hierarchyIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args[i+1] = id
		}

		hierarchyQuery := fmt.Sprintf(`
			SELECT id, name, description, parent_id, prefix, naming_convention
			FROM precast 
			WHERE project_id = $1 AND id IN (%s) ORDER BY id ASC
		`, strings.Join(placeholders, ","))

		rows, err := db.Query(hierarchyQuery, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch hierarchies"})
			return
		}
		defer rows.Close()

		type HierarchyInfo struct {
			ID               int
			Name             string
			Description      string
			ParentID         sql.NullInt64
			Prefix           string
			NamingConvention string
			IsTower          bool
		}

		hierarchies := make(map[int]HierarchyInfo)
		var towerIDs []int

		for rows.Next() {
			var h HierarchyInfo
			err := rows.Scan(&h.ID, &h.Name, &h.Description, &h.ParentID, &h.Prefix, &h.NamingConvention)
			if err != nil {
				continue
			}
			h.IsTower = !h.ParentID.Valid
			hierarchies[h.ID] = h
			if h.IsTower {
				towerIDs = append(towerIDs, h.ID)
			}
		}

		// Step 7: Batch fetch all child hierarchies for towers
		var allChildHierarchies map[int][]HierarchyInfo
		if len(towerIDs) > 0 {
			allChildHierarchies = make(map[int][]HierarchyInfo)

			childPlaceholders := make([]string, len(towerIDs))
			childArgs := make([]interface{}, len(towerIDs)+1)
			childArgs[0] = projectID
			for i, towerID := range towerIDs {
				childPlaceholders[i] = fmt.Sprintf("$%d", i+2)
				childArgs[i+1] = towerID
			}

			childQuery := fmt.Sprintf(`
				SELECT id, name, parent_id
				FROM precast 
				WHERE project_id = $1 AND parent_id IN (%s)
				ORDER BY id, name ASC
			`, strings.Join(childPlaceholders, ","))

			childRows, err := db.Query(childQuery, childArgs...)
			if err == nil {
				defer childRows.Close()
				for childRows.Next() {
					var childID int
					var childName string
					var parentID int
					if childRows.Scan(&childID, &childName, &parentID) == nil {
						child := HierarchyInfo{ID: childID, Name: childName, IsTower: false}
						allChildHierarchies[parentID] = append(allChildHierarchies[parentID], child)
					}
				}
			}
		}

		// Step 8: Batch fetch all element counts and breakdowns
		response := make(map[string]interface{})

		// Get all hierarchy IDs including children for bulk processing
		allHierarchyIDs := make([]int, 0)
		for _, h := range hierarchies {
			allHierarchyIDs = append(allHierarchyIDs, h.ID)
			if children, exists := allChildHierarchies[h.ID]; exists {
				for _, child := range children {
					allHierarchyIDs = append(allHierarchyIDs, child.ID)
				}
			}
		}
		//this is for getting the element counts for all hierarchies
		// Batch get element counts for all hierarchies
		elementCounts := getBatchElementCounts(db, projectID, allHierarchyIDs)

		// Batch get element type breakdowns for all hierarchies
		elementBreakdowns := getBatchElementTypeBreakdowns(db, projectID, allHierarchyIDs)

		// Step 9: Process results efficiently
		for _, h := range hierarchies {
			if h.IsTower {
				// Process tower with children
				childHierarchies := allChildHierarchies[h.ID]

				// Calculate tower totals
				towerHierarchyIDs := []int{h.ID}
				for _, child := range childHierarchies {
					towerHierarchyIDs = append(towerHierarchyIDs, child.ID)
				}

				towerTotals := aggregateElementCounts(elementCounts, towerHierarchyIDs)

				// Add tower totals to response root
				response["totalelement"] = towerTotals["total"]
				response["production"] = towerTotals["production"]
				response["balance"] = towerTotals["balance"]
				response["dispatch"] = towerTotals["dispatch"]
				response["stockyard"] = towerTotals["stockyard"]
				response["erected"] = towerTotals["erected"]
				response["erection"] = towerTotals["erection"]
				response["notinproduction"] = towerTotals["notinproduction"]
				response["erectedbalance"] = towerTotals["erectedbalance"]
				response["totalelement_concrete"] = towerTotals["totalelement_concrete"]
				response["production_concrete"] = towerTotals["production_concrete"]
				response["balance_concrete"] = towerTotals["balance_concrete"]
				response["dispatch_concrete"] = towerTotals["dispatch_concrete"]
				response["stockyard_concrete"] = towerTotals["stockyard_concrete"]
				response["erected_concrete"] = towerTotals["erected_concrete"]
				response["erection_concrete"] = towerTotals["erection_concrete"]
				response["notinproduction_concrete"] = towerTotals["notinproduction_concrete"]
				response["erectedbalance_concrete"] = towerTotals["erectedbalance_concrete"]

				// Add floors to response
				for _, child := range childHierarchies {
					floorCounts := elementCounts[child.ID]
					floorBreakdown := elementBreakdowns[child.ID]

					floorResponse := map[string]interface{}{
						"totalelement":             floorCounts["total"],
						"production":               floorCounts["production"],
						"balance":                  floorCounts["balance"],
						"dispatch":                 floorCounts["dispatch"],
						"stockyard":                floorCounts["stockyard"],
						"erected":                  floorCounts["erected"],
						"erection":                 floorCounts["erection"],
						"notinproduction":          floorCounts["notinproduction"],
						"erectedbalance":           floorCounts["erectedbalance"],
						"totalelement_concrete":    floorCounts["totalelement_concrete"],
						"production_concrete":      floorCounts["production_concrete"],
						"balance_concrete":         floorCounts["balance_concrete"],
						"dispatch_concrete":        floorCounts["dispatch_concrete"],
						"stockyard_concrete":       floorCounts["stockyard_concrete"],
						"erected_concrete":         floorCounts["erected_concrete"],
						"erection_concrete":        floorCounts["erection_concrete"],
						"notinproduction_concrete": floorCounts["notinproduction_concrete"],
						"erectedbalance_concrete":  floorCounts["erectedbalance_concrete"],
					}

					// Add element types directly to floor
					for elementType, data := range floorBreakdown {
						floorResponse[elementType] = data
					}

					response[child.Name] = floorResponse
				}

			} else {
				// Single floor
				floorCounts := elementCounts[h.ID]
				floorBreakdown := elementBreakdowns[h.ID]

				floorResponse := map[string]interface{}{
					"totalelement":             floorCounts["total"],
					"production":               floorCounts["production"],
					"balance":                  floorCounts["balance"],
					"dispatch":                 floorCounts["dispatch"],
					"stockyard":                floorCounts["stockyard"],
					"erected":                  floorCounts["erected"],
					"erection":                 floorCounts["erection"],
					"notinproduction":          floorCounts["notinproduction"],
					"erectedbalance":           floorCounts["erectedbalance"],
					"totalelement_concrete":    floorCounts["totalelement_concrete"],
					"production_concrete":      floorCounts["production_concrete"],
					"balance_concrete":         floorCounts["balance_concrete"],
					"dispatch_concrete":        floorCounts["dispatch_concrete"],
					"stockyard_concrete":       floorCounts["stockyard_concrete"],
					"erected_concrete":         floorCounts["erected_concrete"],
					"erection_concrete":        floorCounts["erection_concrete"],
					"notinproduction_concrete": floorCounts["notinproduction_concrete"],
					"erectedbalance_concrete":  floorCounts["erectedbalance_concrete"],
					"element_types":            floorBreakdown,
				}

				response[h.Name] = floorResponse
			}
		}

		// Use ordered wrapper so JSON has summary keys first, then Floor 1, Floor 2, ... Floor 10, ...
		c.JSON(http.StatusOK, elementTypeBreakdownResponseOrdered{Data: response})

		// Log activity
		activityLog := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched element type status breakdown for multiple hierarchies",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			// Don't fail the request for logging errors, just log them
			log.Printf("Failed to save activity log: %v", logErr)
		}
	}
}

// getElementTypeBreakdownByHierarchiesBulk fetches element type breakdown for multiple hierarchies in a single query.
// This is an optimized version that reduces N+1 query problems.
// Returns a map where key is hierarchyID and value is the breakdown map.
func getElementTypeBreakdownByHierarchiesBulk(db *sql.DB, projectID int, hierarchyIDs []int) map[int]map[string]interface{} {
	if len(hierarchyIDs) == 0 {
		return make(map[int]map[string]interface{})
	}

	// Build query with IN clause for multiple hierarchies
	query := `
		WITH element_status AS (
			SELECT 
				e.id,
				e.target_location,
				et.element_type_id,
				et.element_type,
				et.element_type_name,
				et.thickness,
				et.length,
				et.height,
				CASE 
					WHEN a.id IS NOT NULL THEN 'production'
					WHEN ps.id IS NOT NULL AND ps.stockyard = true AND ps.dispatch_status = false AND ps.erected = false THEN 'stockyard'
					WHEN ps.id IS NOT NULL AND ps.erected = true THEN 'erection'
					ELSE 'notinproduction'
				END AS element_status
			FROM element e
			JOIN element_type et ON e.element_type_id = et.element_type_id
			LEFT JOIN activity a ON e.id = a.element_id
			LEFT JOIN precast_stock ps ON e.id = ps.element_id
			WHERE e.project_id = $1 
			AND e.target_location = ANY($2)
		)
		SELECT 
			target_location as hierarchy_id,
			element_type_id,
			element_type,
			element_type_name,
			COUNT(*) as total_elements,
			COUNT(CASE WHEN element_status = 'production' THEN 1 END) as production_count,
			COUNT(CASE WHEN element_status = 'stockyard' THEN 1 END) as stockyard_count,
			COUNT(CASE WHEN element_status = 'erection' THEN 1 END) as erection_count,
			COUNT(CASE WHEN element_status = 'notinproduction' THEN 1 END) as notinproduction_count,
			COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as total_concrete,
			COALESCE(SUM(CASE 
				WHEN element_status = 'production' 
				THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
				ELSE 0 
			END), 0) as production_concrete,
			COALESCE(SUM(CASE 
				WHEN element_status = 'stockyard' 
				THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
				ELSE 0 
			END), 0) as stockyard_concrete,
			COALESCE(SUM(CASE 
				WHEN element_status = 'erection' 
				THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
				ELSE 0 
			END), 0) as erection_concrete,
			COALESCE(SUM(CASE 
				WHEN element_status = 'notinproduction' 
				THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
				ELSE 0 
			END), 0) as notinproduction_concrete
		FROM element_status
		GROUP BY target_location, element_type_id, element_type, element_type_name
		ORDER BY target_location, element_type_id
	`

	// Add context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, projectID, pq.Array(hierarchyIDs))
	if err != nil {
		log.Printf("Failed to get bulk element type breakdown: %v", err)
		return make(map[int]map[string]interface{})
	}
	defer rows.Close()

	// Group results by hierarchy_id
	result := make(map[int]map[string]interface{})
	for rows.Next() {
		var hierarchyID, elementTypeID int
		var elementType, elementTypeName string
		var totalElements, productionCount, stockyardCount, erectionCount, notinproductionCount int
		var totalConcrete, productionConcrete, stockyardConcrete, erectionConcrete, notinproductionConcrete float64

		err := rows.Scan(
			&hierarchyID, &elementTypeID, &elementType, &elementTypeName, &totalElements,
			&productionCount, &stockyardCount, &erectionCount, &notinproductionCount,
			&totalConcrete, &productionConcrete, &stockyardConcrete, &erectionConcrete, &notinproductionConcrete,
		)
		if err != nil {
			log.Printf("Failed to scan bulk element type breakdown: %v", err)
			continue
		}

		// Initialize hierarchy map if not exists
		if result[hierarchyID] == nil {
			result[hierarchyID] = make(map[string]interface{})
		}

		// Use element_type (short code) as the key
		result[hierarchyID][elementType] = map[string]interface{}{
			"element_type_id":          elementTypeID,
			"element_type":             elementType,
			"element_type_name":        elementTypeName,
			"totalelement":             totalElements,
			"production":               productionCount,
			"stockyard":                stockyardCount,
			"erection":                 erectionCount,
			"notinproduction":          notinproductionCount,
			"totalelement_concrete":    totalConcrete,
			"production_concrete":      productionConcrete,
			"stockyard_concrete":       stockyardConcrete,
			"erection_concrete":        erectionConcrete,
			"notinproduction_concrete": notinproductionConcrete,
		}
	}

	return result
}

// GetDashboardTrends godoc
// @Summary      Get dashboard trends
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/dashboard_trends [get]
func GetDashboardTrends(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user ID
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get role name
		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}

		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		// Step 2: Determine accessible project IDs
		var projectIDs []int

		// If specific project_id is provided, use only that project
		if specificProjectID != nil {
			projectIDs = append(projectIDs, *specificProjectID)
		} else {
			// Use role-based filtering for all projects
			if strings.EqualFold(roleName, "superadmin") {
				// All projects
				rows, err := db.Query("SELECT project_id FROM project WHERE suspend = false")
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project IDs", "details": err.Error()})
					return
				}
				defer rows.Close()

				for rows.Next() {
					var id int
					if err := rows.Scan(&id); err == nil {
						projectIDs = append(projectIDs, id)
					}
				}

			} else if strings.EqualFold(roleName, "admin") {
				// Projects where user is part of the client
				query := `
					SELECT p.project_id
					FROM project p
					JOIN end_client ec ON p.client_id = ec.id
					JOIN client c ON ec.client_id = c.client_id
					JOIN users u ON u.id = c.user_id
					WHERE u.id = $1 AND p.suspend = false
				`
				rows, err := db.Query(query, userID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch admin projects", "details": err.Error()})
					return
				}
				defer rows.Close()

				for rows.Next() {
					var id int
					if err := rows.Scan(&id); err == nil {
						projectIDs = append(projectIDs, id)
					}
				}

			} else {
				// Projects where user is a member
				query := `
					SELECT pm.project_id
					FROM project_members pm
					JOIN project p ON pm.project_id = p.project_id
					WHERE pm.user_id = $1 AND p.suspend = false
				`
				rows, err := db.Query(query, userID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch member projects", "details": err.Error()})
					return
				}
				defer rows.Close()

				for rows.Next() {
					var id int
					if err := rows.Scan(&id); err == nil {
						projectIDs = append(projectIDs, id)
					}
				}
			}
		}

		if len(projectIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"current_month_count":  0,
				"previous_month_count": 0,
				"difference":           0,
				"days_left_in_month":   0,
			})
			return
		}

		// Step 3: Date ranges
		now := time.Now()
		currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		nextMonthStart := currentMonthStart.AddDate(0, 1, 0)
		prevMonthStart := currentMonthStart.AddDate(0, -1, 0)
		prevMonthEnd := currentMonthStart

		endOfMonth := nextMonthStart.AddDate(0, 0, -1) // last day of month
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		daysLeft := int(endOfMonth.Sub(today).Hours() / 24)

		// Step 4: Aggregate counts for selected projects
		var currentCount, previousCount int

		// Helper function to format the IN clause
		placeholders := []string{}
		args := []interface{}{}
		for i, id := range projectIDs {
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
			args = append(args, id)
		}
		inClause := strings.Join(placeholders, ",")

		// Count for current month
		queryCurrent := fmt.Sprintf(`
			SELECT COUNT(*) FROM precast_stock
			WHERE project_id IN (%s) AND created_at >= $%d AND created_at < $%d
		`, inClause, len(args)+1, len(args)+2)

		argsCurrent := append(args, currentMonthStart, nextMonthStart)
		err = db.QueryRow(queryCurrent, argsCurrent...).Scan(&currentCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get current month count", "details": err.Error()})
			return
		}

		// Count for previous month
		queryPrevious := fmt.Sprintf(`
			SELECT COUNT(*) FROM precast_stock
			WHERE project_id IN (%s) AND created_at >= $%d AND created_at < $%d
		`, inClause, len(args)+1, len(args)+2)

		argsPrevious := append(args, prevMonthStart, prevMonthEnd)
		err = db.QueryRow(queryPrevious, argsPrevious...).Scan(&previousCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get previous month count", "details": err.Error()})
			return
		}

		difference := currentCount - previousCount

		// Step 5: Response
		c.JSON(http.StatusOK, gin.H{
			"current_month_count":  currentCount,
			"previous_month_count": previousCount,
			"difference":           fmt.Sprintf("%d (%d days left in month)", difference, daysLeft),
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched dashboard trends",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

	}
}

type TotalCounts struct {
	Production int
	Casted     int
}

func (t TotalCounts) Add(other TotalCounts) TotalCounts {
	return TotalCounts{
		Production: t.Production + other.Production,
		Casted:     t.Casted + other.Casted,
	}
}

// GetPlannedVsCastedElements godoc
// @Summary      Get planned vs casted elements
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/planned_casted [get]
func GetPlannedVsCastedElements(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		if err := db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}
		if err := db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_name"})
			return
		}

		// Project access logic
		var projectIDs []int
		projectIDStr := c.Query("project_id")
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
				return
			}
			projectIDs = append(projectIDs, projectID)
		} else {
			var rows *sql.Rows
			switch strings.ToLower(roleName) {
			case "superadmin":
				rows, err = db.Query("SELECT project_id FROM project")
			case "admin":
				rows, err = db.Query(`
					SELECT p.project_id
FROM project p
JOIN end_client ec ON p.client_id = ec.id
JOIN users u ON u.client_id = ec.client_id
WHERE u.id = $1
`, userID)
			default:
				rows, err = db.Query(`SELECT project_id FROM project_members WHERE user_id = $1`, userID)
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects"})
				return
			}
			defer rows.Close()
			for rows.Next() {
				var id int
				if err := rows.Scan(&id); err == nil {
					projectIDs = append(projectIDs, id)
				}
			}
		}

		if len(projectIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No projects found", "data": []interface{}{}})
			return
		}

		// Summary logic
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")
		layout := "2006-01-02"
		location := time.Now().Location()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			now := time.Now()
			isCurrentYear := now.Year() == year

			monthlyData := make([]map[string]interface{}, 0)
			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, 0).Add(-time.Second)

				var counts TotalCounts
				for _, pid := range projectIDs {
					c := fetchCountsForRangee(db, pid, start, end)
					counts = counts.Add(c)
				}

				monthlyData = append(monthlyData, gin.H{
					"name":    start.Month().String(),
					"planned": counts.Production,
					"casted":  counts.Casted,
				})
			}
			c.JSON(http.StatusOK, monthlyData)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			daysInMonth := start.AddDate(0, 1, -1).Day()

			rangeData := make([]map[string]interface{}, 0)
			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				var counts TotalCounts
				for _, pid := range projectIDs {
					c := fetchCountsForRangee(db, pid, startRange, endRange)
					counts = counts.Add(c)
				}

				rangeData = append(rangeData, gin.H{
					"name":    fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
					"planned": counts.Production,
					"casted":  counts.Casted,
				})
			}
			c.JSON(http.StatusOK, rangeData)

		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			weekData := make([]map[string]interface{}, 0)
			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				var counts TotalCounts
				for _, pid := range projectIDs {
					c := fetchCountsForRangee(db, pid, currentDate, currentDate)
					counts = counts.Add(c)
				}

				weekData = append(weekData, gin.H{
					"name":    currentDate.Format(layout),
					"planned": counts.Production,
					"casted":  counts.Casted,
				})
			}
			c.JSON(http.StatusOK, weekData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
			return
		}

		// Save Activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Planned vs Casted Elements",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

func fetchCountsForRangee(db *sql.DB, projectID int, from, to time.Time) TotalCounts {
	var planned, casted int
	db.QueryRow(`SELECT COUNT(DISTINCT element_id) FROM activity WHERE project_id = $1 AND start_date BETWEEN $2 AND $3`, projectID, from, to).Scan(&planned)
	db.QueryRow(`SELECT COUNT(DISTINCT element_id) FROM precast_stock WHERE project_id = $1 AND production_date BETWEEN $2 AND $3`, projectID, from, to).Scan(&casted)

	return TotalCounts{
		Production: planned,
		Casted:     casted,
	}
}

// Batch helper functions for optimized performance
func getBatchElementCounts(db *sql.DB, projectID int, hierarchyIDs []int) map[int]map[string]interface{} {
	if len(hierarchyIDs) == 0 {
		return make(map[int]map[string]interface{})
	}

	result := make(map[int]map[string]interface{})

	// Initialize all hierarchies with zero counts
	for _, hierarchyID := range hierarchyIDs {
		result[hierarchyID] = map[string]interface{}{
			"total":                    0,
			"production":               0,
			"stockyard":                0,
			"dispatch":                 0,
			"erected":                  0,
			"erection":                 0,
			"notinproduction":          0,
			"balance":                  0,
			"erectedbalance":           0,
			"totalelement_concrete":    0.0,
			"production_concrete":      0.0,
			"stockyard_concrete":       0.0,
			"dispatch_concrete":        0.0,
			"erected_concrete":         0.0,
			"erection_concrete":        0.0,
			"notinproduction_concrete": 0.0,
			"balance_concrete":         0.0,
			"erectedbalance_concrete":  0.0,
		}
	}

	// Build placeholders for SQL query
	placeholders := make([]string, len(hierarchyIDs))
	args := make([]interface{}, len(hierarchyIDs)+1)
	args[0] = projectID
	for i, id := range hierarchyIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		WITH element_data AS (
			SELECT 
				e.id,
				e.target_location,
				et.thickness,
				et.length,
				et.height,
				CASE 
					WHEN ps.id IS NOT NULL AND ps.erected = true THEN 'erected'
					WHEN ps.id IS NOT NULL AND ps.dispatch_status = true THEN 'dispatch'
					WHEN a.id IS NOT NULL THEN 'production'
					WHEN ps.id IS NOT NULL AND ps.stockyard = true AND ps.dispatch_status = false AND ps.erected = false THEN 'stockyard'
					ELSE 'notinproduction'
				END AS element_status,
				CASE WHEN a.id IS NOT NULL OR ps.id IS NOT NULL THEN 1 ELSE 0 END AS is_produced
			FROM element e
			JOIN element_type et ON e.element_type_id = et.element_type_id
			LEFT JOIN activity a ON e.id = a.element_id
			LEFT JOIN precast_stock ps ON e.id = ps.element_id
			WHERE e.project_id = $1 AND e.target_location IN (%s)
		),
		status_counts AS (
			SELECT 
				target_location,
				element_status,
				COUNT(*) as count,
				COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
			FROM element_data
			GROUP BY target_location, element_status
		),
		production_counts AS (
			SELECT 
				target_location,
				COUNT(*) as count,
				COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
			FROM element_data
			WHERE is_produced = 1
			GROUP BY target_location
		)
		SELECT 
			target_location,
			element_status,
			count,
			concrete_sum
		FROM status_counts
		UNION ALL
		SELECT 
			target_location,
			'production' as element_status,
			count,
			concrete_sum
		FROM production_counts
	`, strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var targetLocation int
		var status string
		var count int
		var concreteSum float64

		if err := rows.Scan(&targetLocation, &status, &count, &concreteSum); err != nil {
			continue
		}

		if _, exists := result[targetLocation]; !exists {
			continue
		}

		// Update counts
		result[targetLocation]["total"] = result[targetLocation]["total"].(int) + count
		result[targetLocation]["totalelement_concrete"] = result[targetLocation]["totalelement_concrete"].(float64) + concreteSum

		// Update status-specific counts
		switch status {
		case "production":
			result[targetLocation]["production"] = result[targetLocation]["production"].(int) + count
			result[targetLocation]["production_concrete"] = result[targetLocation]["production_concrete"].(float64) + concreteSum
		case "dispatch":
			result[targetLocation]["dispatch"] = result[targetLocation]["dispatch"].(int) + count
			result[targetLocation]["dispatch_concrete"] = result[targetLocation]["dispatch_concrete"].(float64) + concreteSum
		case "erected":
			result[targetLocation]["erected"] = result[targetLocation]["erected"].(int) + count
			result[targetLocation]["erected_concrete"] = result[targetLocation]["erected_concrete"].(float64) + concreteSum
			// Also update erection for backward compatibility
			result[targetLocation]["erection"] = result[targetLocation]["erected"].(int)
			result[targetLocation]["erection_concrete"] = result[targetLocation]["erected_concrete"].(float64)
		case "stockyard":
			result[targetLocation]["stockyard"] = result[targetLocation]["stockyard"].(int) + count
			result[targetLocation]["stockyard_concrete"] = result[targetLocation]["stockyard_concrete"].(float64) + concreteSum
		default:
			result[targetLocation]["notinproduction"] = result[targetLocation]["notinproduction"].(int) + count
			result[targetLocation]["notinproduction_concrete"] = result[targetLocation]["notinproduction_concrete"].(float64) + concreteSum
		}
	}

	// Calculate derived fields: stockyard, balance, erectedbalance (same logic as ExportDashboardPDF)
	for location := range result {
		dispatch := result[location]["dispatch"].(int)
		produced := result[location]["production"].(int)
		erected := result[location]["erected"].(int)

		// Recalculate stockyard = produced - dispatch (with minimum 0)
		stockyard := produced - dispatch
		if stockyard < 0 {
			stockyard = 0
		}
		result[location]["stockyard"] = stockyard

		dispatchConcrete := result[location]["dispatch_concrete"].(float64)
		producedConcrete := result[location]["production_concrete"].(float64)
		erectedConcrete := result[location]["erected_concrete"].(float64)
		stockyardConcrete := producedConcrete - dispatchConcrete
		if stockyardConcrete < 0 {
			stockyardConcrete = 0
		}
		result[location]["stockyard_concrete"] = stockyardConcrete

		// Calculate balance = total - produced (same as notinproduction)
		total := result[location]["total"].(int)
		balance := total - produced
		result[location]["balance"] = balance
		result[location]["notinproduction"] = balance

		totalConcrete := result[location]["totalelement_concrete"].(float64)
		balanceConcrete := totalConcrete - producedConcrete
		result[location]["balance_concrete"] = balanceConcrete
		result[location]["notinproduction_concrete"] = balanceConcrete

		// Calculate erected balance = dispatch - erected (with minimum 0)
		erectedBalance := dispatch - erected
		if erectedBalance < 0 {
			erectedBalance = 0
		}
		result[location]["erectedbalance"] = erectedBalance

		erectedBalanceConcrete := dispatchConcrete - erectedConcrete
		if erectedBalanceConcrete < 0 {
			erectedBalanceConcrete = 0
		}
		result[location]["erectedbalance_concrete"] = erectedBalanceConcrete
	}

	return result
}

func getBatchElementTypeBreakdowns(db *sql.DB, projectID int, hierarchyIDs []int) map[int]map[string]interface{} {
	if len(hierarchyIDs) == 0 {
		return make(map[int]map[string]interface{})
	}

	result := make(map[int]map[string]interface{})

	// Initialize all hierarchies
	for _, hierarchyID := range hierarchyIDs {
		result[hierarchyID] = make(map[string]interface{})
	}

	// Build placeholders for SQL query
	placeholders := make([]string, len(hierarchyIDs))
	args := make([]interface{}, len(hierarchyIDs)+1)
	args[0] = projectID
	for i, id := range hierarchyIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		WITH element_data AS (
			SELECT 
				e.id,
				e.target_location,
				et.element_type_id,
				et.element_type,
				et.element_type_name,
				et.thickness,
				et.length,
				et.height,
				CASE 
					WHEN ps.id IS NOT NULL AND ps.erected = true THEN 'erected'
					WHEN ps.id IS NOT NULL AND ps.dispatch_status = true THEN 'dispatch'
					WHEN a.id IS NOT NULL THEN 'production'
					WHEN ps.id IS NOT NULL AND ps.stockyard = true AND ps.dispatch_status = false AND ps.erected = false THEN 'stockyard'
					ELSE 'notinproduction'
				END AS element_status,
				CASE WHEN a.id IS NOT NULL OR ps.id IS NOT NULL THEN 1 ELSE 0 END AS is_produced
			FROM element e
			JOIN element_type et ON e.element_type_id = et.element_type_id
			LEFT JOIN activity a ON e.id = a.element_id
			LEFT JOIN precast_stock ps ON e.id = ps.element_id
			WHERE e.project_id = $1 AND e.target_location IN (%s)
		),
		status_counts AS (
			SELECT 
				target_location,
				element_type,
				element_status,
				COUNT(*) as count,
				COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
			FROM element_data
			GROUP BY target_location, element_type, element_status
		),
		production_counts AS (
			SELECT 
				target_location,
				element_type,
				COUNT(*) as count,
				COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
			FROM element_data
			WHERE is_produced = 1
			GROUP BY target_location, element_type
		)
		SELECT 
			target_location,
			element_type,
			element_status,
			count,
			concrete_sum
		FROM status_counts
		UNION ALL
		SELECT 
			target_location,
			element_type,
			'production' as element_status,
			count,
			concrete_sum
		FROM production_counts
		ORDER BY target_location, element_type
	`, strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return result
	}
	defer rows.Close()

	// Temporary storage for element type data
	tempData := make(map[int]map[string]map[string]interface{})

	for rows.Next() {
		var targetLocation int
		var elementType, status string
		var count int
		var concreteSum float64

		if err := rows.Scan(&targetLocation, &elementType, &status, &count, &concreteSum); err != nil {
			continue
		}

		if _, exists := tempData[targetLocation]; !exists {
			tempData[targetLocation] = make(map[string]map[string]interface{})
		}
		if _, exists := tempData[targetLocation][elementType]; !exists {
			tempData[targetLocation][elementType] = map[string]interface{}{
				"totalelement":             0,
				"production":               0,
				"stockyard":                0,
				"dispatch":                 0,
				"erected":                  0,
				"erection":                 0,
				"notinproduction":          0,
				"balance":                  0,
				"erectedbalance":           0,
				"totalelement_concrete":    0.0,
				"production_concrete":      0.0,
				"stockyard_concrete":       0.0,
				"dispatch_concrete":        0.0,
				"erected_concrete":         0.0,
				"erection_concrete":        0.0,
				"notinproduction_concrete": 0.0,
				"balance_concrete":         0.0,
				"erectedbalance_concrete":  0.0,
			}
		}

		elementData := tempData[targetLocation][elementType]

		// Update totals
		elementData["totalelement"] = elementData["totalelement"].(int) + count
		elementData["totalelement_concrete"] = elementData["totalelement_concrete"].(float64) + concreteSum

		// Update status-specific counts
		switch status {
		case "production":
			elementData["production"] = elementData["production"].(int) + count
			elementData["production_concrete"] = elementData["production_concrete"].(float64) + concreteSum
		case "dispatch":
			elementData["dispatch"] = elementData["dispatch"].(int) + count
			elementData["dispatch_concrete"] = elementData["dispatch_concrete"].(float64) + concreteSum
		case "erected":
			elementData["erected"] = elementData["erected"].(int) + count
			elementData["erected_concrete"] = elementData["erected_concrete"].(float64) + concreteSum
			// Also update erection for backward compatibility
			elementData["erection"] = elementData["erected"].(int)
			elementData["erection_concrete"] = elementData["erected_concrete"].(float64)
		case "stockyard":
			elementData["stockyard"] = elementData["stockyard"].(int) + count
			elementData["stockyard_concrete"] = elementData["stockyard_concrete"].(float64) + concreteSum
		default:
			elementData["notinproduction"] = elementData["notinproduction"].(int) + count
			elementData["notinproduction_concrete"] = elementData["notinproduction_concrete"].(float64) + concreteSum
		}
	}

	// Calculate derived fields: stockyard, balance, erectedbalance (same logic as ExportDashboardPDF)
	for _, elementTypes := range tempData {
		for _, elementData := range elementTypes {
			dispatch := elementData["dispatch"].(int)
			produced := elementData["production"].(int)
			erected := elementData["erected"].(int)

			// Recalculate stockyard = produced - dispatch (with minimum 0)
			stockyard := produced - dispatch
			if stockyard < 0 {
				stockyard = 0
			}
			elementData["stockyard"] = stockyard

			dispatchConcrete := elementData["dispatch_concrete"].(float64)
			producedConcrete := elementData["production_concrete"].(float64)
			erectedConcrete := elementData["erected_concrete"].(float64)
			stockyardConcrete := producedConcrete - dispatchConcrete
			if stockyardConcrete < 0 {
				stockyardConcrete = 0
			}
			elementData["stockyard_concrete"] = stockyardConcrete

			// Calculate balance = total - produced (same as notinproduction)
			total := elementData["totalelement"].(int)
			balance := total - produced
			elementData["balance"] = balance
			elementData["notinproduction"] = balance

			totalConcrete := elementData["totalelement_concrete"].(float64)
			balanceConcrete := totalConcrete - producedConcrete
			elementData["balance_concrete"] = balanceConcrete
			elementData["notinproduction_concrete"] = balanceConcrete

			// Calculate erected balance = dispatch - erected (with minimum 0)
			erectedBalance := dispatch - erected
			if erectedBalance < 0 {
				erectedBalance = 0
			}
			elementData["erectedbalance"] = erectedBalance

			erectedBalanceConcrete := dispatchConcrete - erectedConcrete
			if erectedBalanceConcrete < 0 {
				erectedBalanceConcrete = 0
			}
			elementData["erectedbalance_concrete"] = erectedBalanceConcrete
		}
	}

	// Convert to final format
	for targetLocation, elementTypes := range tempData {
		result[targetLocation] = make(map[string]interface{})
		for elementType, data := range elementTypes {
			result[targetLocation][elementType] = data
		}
	}

	return result
}

func aggregateElementCounts(elementCounts map[int]map[string]interface{}, hierarchyIDs []int) map[string]interface{} {
	aggregated := map[string]interface{}{
		"total":                    0,
		"production":               0,
		"stockyard":                0,
		"dispatch":                 0,
		"erected":                  0,
		"erection":                 0,
		"notinproduction":          0,
		"balance":                  0,
		"erectedbalance":           0,
		"totalelement_concrete":    0.0,
		"production_concrete":      0.0,
		"stockyard_concrete":       0.0,
		"dispatch_concrete":        0.0,
		"erected_concrete":         0.0,
		"erection_concrete":        0.0,
		"notinproduction_concrete": 0.0,
		"balance_concrete":         0.0,
		"erectedbalance_concrete":  0.0,
	}

	for _, hierarchyID := range hierarchyIDs {
		if counts, exists := elementCounts[hierarchyID]; exists {
			aggregated["total"] = aggregated["total"].(int) + counts["total"].(int)
			aggregated["production"] = aggregated["production"].(int) + counts["production"].(int)
			aggregated["stockyard"] = aggregated["stockyard"].(int) + counts["stockyard"].(int)
			aggregated["dispatch"] = aggregated["dispatch"].(int) + counts["dispatch"].(int)
			aggregated["erected"] = aggregated["erected"].(int) + counts["erected"].(int)
			aggregated["erection"] = aggregated["erection"].(int) + counts["erection"].(int)
			aggregated["notinproduction"] = aggregated["notinproduction"].(int) + counts["notinproduction"].(int)
			aggregated["balance"] = aggregated["balance"].(int) + counts["balance"].(int)
			aggregated["erectedbalance"] = aggregated["erectedbalance"].(int) + counts["erectedbalance"].(int)
			aggregated["totalelement_concrete"] = aggregated["totalelement_concrete"].(float64) + counts["totalelement_concrete"].(float64)
			aggregated["production_concrete"] = aggregated["production_concrete"].(float64) + counts["production_concrete"].(float64)
			aggregated["stockyard_concrete"] = aggregated["stockyard_concrete"].(float64) + counts["stockyard_concrete"].(float64)
			aggregated["dispatch_concrete"] = aggregated["dispatch_concrete"].(float64) + counts["dispatch_concrete"].(float64)
			aggregated["erected_concrete"] = aggregated["erected_concrete"].(float64) + counts["erected_concrete"].(float64)
			aggregated["erection_concrete"] = aggregated["erection_concrete"].(float64) + counts["erection_concrete"].(float64)
			aggregated["notinproduction_concrete"] = aggregated["notinproduction_concrete"].(float64) + counts["notinproduction_concrete"].(float64)
			aggregated["balance_concrete"] = aggregated["balance_concrete"].(float64) + counts["balance_concrete"].(float64)
			aggregated["erectedbalance_concrete"] = aggregated["erectedbalance_concrete"].(float64) + counts["erectedbalance_concrete"].(float64)
		}
	}

	return aggregated
}

// GetConcreteUsageReports - Separate API for concrete materials only

// // getProjectConcreteGrades gets all concrete grades used in a project
// func getProjectConcreteGrades(db *sql.DB, projectID int) ([]string, error) {
// 	query := `
// 		SELECT DISTINCT
// 			CASE
// 				WHEN concrete_grade IS NOT NULL AND concrete_grade != '' THEN concrete_grade
// 				ELSE 'M-40'
// 			END as grade
// 		FROM element_type
// 		WHERE project_id = $1
// 		ORDER BY grade
// 	`

// 	rows, err := db.Query(query, projectID)
// 	if err != nil {
// 		// If concrete_grade column doesn't exist, return default grades
// 		log.Printf("Concrete grade column may not exist, using defaults: %v", err)
// 		return []string{"M-40", "M-60"}, nil
// 	}
// 	defer rows.Close()

// 	var grades []string
// 	for rows.Next() {
// 		var grade string
// 		if err := rows.Scan(&grade); err != nil {
// 			log.Printf("Error scanning concrete grade, using defaults: %v", err)
// 			return []string{"M-40", "M-60"}, nil
// 		}
// 		grades = append(grades, grade)
// 	}

// 	// If no grades found, return default grades
// 	if len(grades) == 0 {
// 		grades = []string{"M-40", "M-60"}
// 	}

// 	return grades, nil
// }

// GetStockyardReports godoc
// @Summary      Get stockyard reports for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/stockyard_reports/{project_id} [get]
func GetStockyardReports(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // default to yearly
		yearStr := c.Query("year")
		monthStr := c.Query("month")

		layout := "2006-01-02"
		location := time.Now().Location()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			now := time.Now()
			isCurrentYear := now.Year() == year

			monthlyData := make([]map[string]interface{}, 0)
			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)

				counts := fetchCountsForRange(db, projectID, start, end)
				monthlyData = append(monthlyData, gin.H{
					"name":        start.Month().String(),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, monthlyData)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			daysInMonth := start.AddDate(0, 1, -1).Day()

			rangeData := make([]map[string]interface{}, 0)
			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				counts := fetchCountsForRange(db, projectID, startRange, endRange)
				rangeData = append(rangeData, gin.H{
					"name":        fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, rangeData)

		case "weekly":
			yearStr := c.Query("year")
			monthStr := c.Query("month")
			dayStr := c.Query("date")

			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}

			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))

			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			weekData := make([]map[string]interface{}, 0)
			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				counts := fetchCountsForRange(db, projectID, currentDate, currentDate)
				weekData = append(weekData, gin.H{
					"name":        currentDate.Format(layout),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, weekData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
		}

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched production reports",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetElementTypeReports godoc
// @Summary      Get element type reports for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_type_reports/{project_id} [get]
func GetElementTypeReports(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")
		location := time.Now().Location()

		var start, end time.Time
		now := time.Now()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			if year == now.Year() {
				end = now
			} else {
				end = time.Date(year, 12, 31, 23, 59, 59, 0, location)
			}

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, 0).Add(-time.Second)

		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			day, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}
			start = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
			end = start.AddDate(0, 0, 1).Add(-time.Second)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type parameter"})
			return
		}

		// Fetch data for the calculated range
		counts, err := fetchElementTypeCounts(db, projectID, start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, defaultEmptyArray(counts))

		// Save activity log
		log := models.ActivityLog{
			EventContext: "ElementTypeReport",
			EventName:    "Get",
			Description:  "Fetched element type wise stock report",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		_ = SaveActivityLog(db, log)
	}
}

func defaultEmptyArray(data []map[string]interface{}) []map[string]interface{} {
	if data == nil {
		return []map[string]interface{}{}
	}
	return data
}

func fetchElementTypeCounts(db *sql.DB, projectID int, start, end time.Time) ([]map[string]any, error) {
	query := `
		SELECT et.element_type, COUNT(DISTINCT ps.element_id)
		FROM precast_stock ps
		JOIN element e ON ps.element_id = e.id
		JOIN element_type et ON e.element_type_id = et.element_type_id
		WHERE ps.project_id = $1 AND ps.created_at BETWEEN $2 AND $3
		GROUP BY et.element_type
		ORDER BY et.element_type
	`

	rows, err := db.Query(query, projectID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		result = append(result, gin.H{
			"element_type": name,
			"count":        count,
		})
	}
	return result, nil
}

// GetAverageDailyErectedHandler godoc
// @Summary      Get average daily erected
// @Tags         dashboard
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/average_erected [get]
func GetAverageDailyErectedHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role ID"})
			return
		}

		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name"})
			return
		}

		// Check for project_id query parameter
		projectIDStr := c.Query("project_id")
		var specificProjectID *int
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			specificProjectID = &projectID
		}

		// Step 1: Get the first and last day of the current month
		now := time.Now()
		location := now.Location()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		today := now.Truncate(24 * time.Hour)

		var projectFilter string
		var args []interface{}
		argIndex := 2 // index for date args (we'll use $3 and $4 for dates)

		// If specific project_id is provided, filter for that project only
		if specificProjectID != nil {
			projectFilter = "WHERE e.project_id = $1"
			args = append(args, *specificProjectID)
			argIndex = 2
		} else {
			// Use role-based filtering for all projects
			switch roleName {
			case "superadmin":
				projectFilter = "WHERE e.project_id IN (SELECT project_id FROM project WHERE suspend = false)"
				args = []interface{}{} // Clear any previously added userID
				argIndex = 1           // date args will be $1 and $2
			case "admin":
				projectFilter = `
					WHERE e.project_id IN (
						SELECT p.project_id FROM project p
						JOIN end_client ec ON p.client_id = ec.id
    JOIN client cl ON ec.client_id = cl.client_id
    WHERE cl.user_id = $1
      AND p.suspend = false
						AND p.suspend = false
					)`
				args = append(args, userID)
				argIndex = 2
			default:
				projectFilter = `
					WHERE e.project_id IN (
						SELECT pm.project_id FROM project_members pm
						JOIN project p ON pm.project_id = p.project_id
						WHERE pm.user_id = $1 AND p.suspend = false
					)`
				args = append(args, userID)
				argIndex = 2
			}
		}

		// Step 2: Add updated_at filter to projectFilter
		if projectFilter == "" {
			projectFilter = "WHERE"
		} else {
			projectFilter += " AND"
		}
		projectFilter += ` DATE(ps.updated_at) >= DATE($` + fmt.Sprint(argIndex) + `)`
		projectFilter += ` AND DATE(ps.updated_at) <= DATE($` + fmt.Sprint(argIndex+1) + `)`

		// Add date params to args
		args = append(args, firstOfMonth, today)

		// Step 3: Prepare the query
		query := fmt.Sprintf(`
			SELECT 
				DATE(ps.updated_at) AS day, 
				COUNT(DISTINCT ps.element_id) 
			FROM precast_stock ps
			JOIN element e ON ps.element_id = e.id
			%s AND ps.erected = true 
			GROUP BY day
		`, projectFilter)

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		var totalCasted int
		for rows.Next() {
			var day time.Time
			var count int
			if err := rows.Scan(&day, &count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Scan failed", "details": err.Error()})
				return
			}
			totalCasted += count
		}

		// Step 4: Calculate the number of days elapsed so far in the current month
		daysElapsed := int(today.Sub(firstOfMonth).Hours()/24) + 1 // include today

		average := 0.0
		if daysElapsed > 0 {
			average = float64(totalCasted) / float64(daysElapsed)
		}

		c.JSON(http.StatusOK, gin.H{
			"average_erected_elements": average,
		})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched average daily erected elements for the current month",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// ExportDashboardPDF godoc
// @Summary      Export dashboard as PDF
// @Tags         dashboard
// @Param        project_id  query     int  true   "Project ID"
// @Param        tower_id    query     int  false  "Tower ID"
// @Param        type        query     string  false  "yearly/monthly/weekly"
// @Success      200         {file}    file  "PDF file"
// @Failure      400         {object}  models.ErrorResponse
// @Router       /api/dashboard_pdf [get]
func ExportDashboardPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := strconv.Atoi(c.Query("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var projectName string
		err = db.QueryRow(`SELECT name FROM project WHERE project_id = $1`, projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		towerIDStr := c.Query("tower_id")
		var towerID *int
		if towerIDStr != "" {
			tid, err := strconv.Atoi(towerIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tower ID"})
				return
			}
			towerID = &tid
		}

		// Parse date range based on type (yearly/monthly/weekly/custom)
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		// view = all | production | stockyard | dispatch | erected
		viewMode := strings.ToLower(c.DefaultQuery("view", "all"))
		switch viewMode {
		case "production", "stockyard", "dispatch", "erected":
		default:
			viewMode = "all"
		}

		layout := "2006-01-02"
		location := time.Now().Location()

		var startDate, endDate time.Time
		var dateRangeStr string

		switch summaryType {
		case "yearly":
			if yearStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year parameter"})
				return
			}
			year, _ := strconv.Atoi(yearStr)
			startDate = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			endDate = time.Date(year, 12, 31, 23, 59, 59, 0, location)
			dateRangeStr = fmt.Sprintf("Year %d(%s to %s)", year, startDate.Format("2-Jan-2006"), endDate.Format("2-Jan-2006"))

		case "monthly":
			if yearStr == "" || monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year or month parameter"})
				return
			}
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			startDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			endDate = startDate.AddDate(0, 1, -1)
			endDate = time.Date(year, time.Month(month), endDate.Day(), 23, 59, 59, 0, location)
			dateRangeStr = fmt.Sprintf("%s to %s", startDate.Format("2-Jan-2006"), endDate.Format("2-Jan-2006"))

		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date parameter"})
				return
			}
			weekStartDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			parsedDate, err := time.Parse(layout, weekStartDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
				return
			}
			weekStart := parsedDate.AddDate(0, 0, -6)
			startDate = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, location)
			endDate = time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 23, 59, 59, 0, location)
			dateRangeStr = fmt.Sprintf("%s to %s", startDate.Format(layout), endDate.Format(layout))

		case "custom":
			if startDateStr == "" || endDateStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing start_date or end_date parameter"})
				return
			}
			parsedStartDate, err := time.Parse(layout, startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format. Expected YYYY-MM-DD"})
				return
			}
			parsedEndDate, err := time.Parse(layout, endDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format. Expected YYYY-MM-DD"})
				return
			}
			if parsedEndDate.Before(parsedStartDate) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "end_date must be after or equal to start_date"})
				return
			}
			startDate = time.Date(parsedStartDate.Year(), parsedStartDate.Month(), parsedStartDate.Day(), 0, 0, 0, 0, location)
			endDate = time.Date(parsedEndDate.Year(), parsedEndDate.Month(), parsedEndDate.Day(), 23, 59, 59, 0, location)
			dateRangeStr = fmt.Sprintf("%s to %s", startDate.Format("2-Jan-2006"), endDate.Format("2-Jan-2006"))

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type. Must be yearly, monthly, weekly, or custom"})
			return
		}

		// Pre-fetch element types
		elementTypeMap := make(map[int]string)
		etRows, _ := db.Query(`SELECT element_type_id, element_type FROM element_type WHERE project_id = $1`, projectID)
		for etRows.Next() {
			var etID int
			var etName string
			if err := etRows.Scan(&etID, &etName); err == nil {
				elementTypeMap[etID] = etName
			}
		}
		etRows.Close()

		// ------------ Helpers for view modes (headers / cells) ------------

		// Build headers & widths
		buildStatusHeaders := func(viewMode string, includeNameCol bool, nameHeader string, pageWidth float64) ([]string, []float64) {
			var baseHeaders []string
			switch viewMode {
			case "production":
				baseHeaders = []string{
					"Total\n(nos./ cum.)",
					"Produced\n(nos./ cum.)",
					"Balance\n(nos./ cum.)",
				}
			case "stockyard":
				// Balance removed for stockyard view
				baseHeaders = []string{
					"Total\n(nos./ cum.)",
					"Dispatched\n(nos./ cum.)",
					"Stockyard\n(nos./ cum.)",
				}
			case "dispatch":
				// Balance removed for dispatch view
				baseHeaders = []string{
					"Total\n(nos./ cum.)",
					"Dispatched\n(nos./ cum.)",
				}
			case "erected":
				baseHeaders = []string{
					"Total\n(nos./ cum.)",
					"Erected\n(nos./ cum.)",
					"Er. Balance\n(nos./ cum.)",
				}
			default: // all
				baseHeaders = []string{
					"Total\n(nos./ cum.)",
					"Produced\n(nos./ cum.)",
					"Balance\n(nos./ cum.)",
					"Dispatched\n(nos./ cum.)",
					"Stockyard\n(nos./ cum.)",
					"Erected\n(nos./ cum.)",
					"Er. Balance\n(nos./ cum.)",
				}
			}

			var headers []string
			if includeNameCol {
				headers = make([]string, 0, len(baseHeaders)+1)
				headers = append(headers, nameHeader)
				headers = append(headers, baseHeaders...)
			} else {
				headers = baseHeaders
			}

			widths := make([]float64, len(headers))
			colWidth := pageWidth / float64(len(headers))
			for i := range widths {
				widths[i] = colWidth
			}
			if includeNameCol && len(widths) > 0 {
				widths[0] = colWidth * 1.2
			}
			return headers, widths
		}

		// Write one row based on viewMode (no Balance col for stockyard/dispatch now)
		writeStatusRow := func(pdf *gofpdf.Fpdf, viewMode string, widths []float64, startIdx int,
			totalNos int, totalCum float64,
			prodNos int, prodCum float64,
			dispatchNos int, dispatchCum float64,
			stockNos int, stockCum float64,
			erectNos int, erectCum float64,
		) {
			balanceProdNos := totalNos - prodNos
			balanceProdCum := totalCum - prodCum

			erectedBalanceNos := dispatchNos - erectNos
			if erectedBalanceNos < 0 {
				erectedBalanceNos = 0
			}
			erectedBalanceCum := dispatchCum - erectCum
			if erectedBalanceCum < 0 {
				erectedBalanceCum = 0
			}

			col := startIdx
			switch viewMode {
			case "production":
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", totalNos, totalCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", prodNos, prodCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", balanceProdNos, balanceProdCum), "1", 0, "C", false, 0, "")

			case "stockyard":
				// Only Total, Dispatched, Stockyard
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", totalNos, totalCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", dispatchNos, dispatchCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", stockNos, stockCum), "1", 0, "C", false, 0, "")

			case "dispatch":
				// Only Total, Dispatched
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", totalNos, totalCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", dispatchNos, dispatchCum), "1", 0, "C", false, 0, "")

			case "erected":
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", totalNos, totalCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", erectNos, erectCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", erectedBalanceNos, erectedBalanceCum), "1", 0, "C", false, 0, "")

			default: // all
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", totalNos, totalCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", prodNos, prodCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", balanceProdNos, balanceProdCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", dispatchNos, dispatchCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", stockNos, stockCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", erectNos, erectCum), "1", 0, "C", false, 0, "")
				col++
				pdf.CellFormat(widths[col], 14, fmt.Sprintf("%d / %.2f", erectedBalanceNos, erectedBalanceCum), "1", 0, "C", false, 0, "")
			}
		}

		// Row labels for yearly/monthly breakdown
		getBreakdownRowLabels := func(viewMode string) []string {
			switch viewMode {
			case "production":
				return []string{"Total", "Produced", "Balance"}
			case "stockyard":
				// Balance row removed for stockyard view
				return []string{"Total", "Dispatched", "Stockyard"}
			case "dispatch":
				// Balance row removed for dispatch view
				return []string{"Total", "Dispatched"}
			case "erected":
				return []string{"Total", "Erected", "Er. Balance"}
			default:
				return []string{"Total", "Produced", "Balance", "Dispatched", "Stockyard", "Erected", "Er. Balance"}
			}
		}

		getBreakdownValue := func(viewMode, rowLabel string, data map[string]interface{}) string {
			totalNos := data["total"].(int)
			totalCum := data["totalelement_concrete"].(float64)
			prodNos := data["production"].(int)
			prodCum := data["production_concrete"].(float64)
			dispatchNos := data["dispatch"].(int)
			dispatchCum := data["dispatch_concrete"].(float64)
			stockNos := data["stockyard"].(int)
			stockCum := data["stockyard_concrete"].(float64)
			erectNos := data["erected"].(int)
			erectCum := data["erected_concrete"].(float64)

			balanceProdNos := totalNos - prodNos
			balanceProdCum := totalCum - prodCum
			balanceDispatchNos := totalNos - dispatchNos
			balanceDispatchCum := totalCum - dispatchCum
			balanceStockNos := totalNos - stockNos
			balanceStockCum := totalCum - stockCum

			erectedBalanceNos := dispatchNos - erectNos
			if erectedBalanceNos < 0 {
				erectedBalanceNos = 0
			}
			erectedBalanceCum := dispatchCum - erectCum
			if erectedBalanceCum < 0 {
				erectedBalanceCum = 0
			}

			switch rowLabel {
			case "Total":
				return fmt.Sprintf("%d / %.2f", totalNos, totalCum)
			case "Produced":
				return fmt.Sprintf("%d / %.2f", prodNos, prodCum)
			case "Dispatched":
				return fmt.Sprintf("%d / %.2f", dispatchNos, dispatchCum)
			case "Stockyard":
				return fmt.Sprintf("%d / %.2f", stockNos, stockCum)
			case "Erected":
				return fmt.Sprintf("%d / %.2f", erectNos, erectCum)
			case "Balance":
				switch viewMode {
				case "production", "all":
					return fmt.Sprintf("%d / %.2f", balanceProdNos, balanceProdCum)
				case "dispatch":
					return fmt.Sprintf("%d / %.2f", balanceDispatchNos, balanceDispatchCum)
				case "stockyard":
					return fmt.Sprintf("%d / %.2f", balanceStockNos, balanceStockCum)
				default:
					return ""
				}
			case "Er. Balance":
				return fmt.Sprintf("%d / %.2f", erectedBalanceNos, erectedBalanceCum)
			default:
				return ""
			}
		}

		// ------------- DB helpers (unchanged logic) -------------

		getBatchElementCountsWithDate := func(hierarchyIDs []int) map[int]map[string]interface{} {
			if len(hierarchyIDs) == 0 {
				return make(map[int]map[string]interface{})
			}

			result := make(map[int]map[string]interface{})
			for _, hierarchyID := range hierarchyIDs {
				result[hierarchyID] = map[string]interface{}{
					"total":                    0,
					"production":               0,
					"stockyard":                0,
					"dispatch":                 0,
					"erected":                  0,
					"notinproduction":          0,
					"erectedbalance":           0,
					"totalelement_concrete":    0.0,
					"production_concrete":      0.0,
					"stockyard_concrete":       0.0,
					"dispatch_concrete":        0.0,
					"erected_concrete":         0.0,
					"notinproduction_concrete": 0.0,
					"erectedbalance_concrete":  0.0,
				}
			}

			placeholders := make([]string, len(hierarchyIDs))
			args := make([]interface{}, len(hierarchyIDs)+3)
			args[0] = projectID
			args[1] = startDate
			args[2] = endDate
			for i, id := range hierarchyIDs {
				placeholders[i] = fmt.Sprintf("$%d", i+4)
				args[i+3] = id
			}

			query := fmt.Sprintf(`
				WITH element_data AS (
					SELECT 
						e.id,
						e.target_location,
						et.thickness,
						et.length,
						et.height,
						CASE WHEN ps.id IS NOT NULL AND ps.erected = true 
							AND DATE(ps.updated_at) BETWEEN DATE($2) AND DATE($3) THEN 'erected'
							WHEN ps.id IS NOT NULL AND ps.dispatch_status = true 
							AND ps.dispatch_start IS NOT NULL AND ps.dispatch_start BETWEEN $2 AND $3 THEN 'dispatch'
							ELSE 'notinproduction'
						END AS element_status,
						CASE WHEN ps.id IS NOT NULL AND ps.production_date BETWEEN $2 AND $3 THEN 1 ELSE 0 END AS is_produced
					FROM element e
					JOIN element_type et ON e.element_type_id = et.element_type_id
					LEFT JOIN precast_stock ps ON e.id = ps.element_id
					WHERE e.project_id = $1 AND e.target_location IN (%s)
				),
				status_counts AS (
					SELECT 
						target_location,
						element_status,
						COUNT(*) as count,
						COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
					FROM element_data
					GROUP BY target_location, element_status
				),
				production_counts AS (
					SELECT 
						target_location,
						COUNT(*) as count,
						COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
					FROM element_data
					WHERE is_produced = 1
					GROUP BY target_location
				)
				SELECT 
					target_location,
					element_status,
					count,
					concrete_sum
				FROM status_counts
				UNION ALL
				SELECT 
					target_location,
					'production' as element_status,
					count,
					concrete_sum
				FROM production_counts
			`, strings.Join(placeholders, ","))

			rows, err := db.Query(query, args...)
			if err != nil {
				return result
			}
			defer rows.Close()

			for rows.Next() {
				var targetLocation int
				var status string
				var count int
				var concreteSum float64

				if err := rows.Scan(&targetLocation, &status, &count, &concreteSum); err != nil {
					continue
				}

				if _, exists := result[targetLocation]; !exists {
					continue
				}

				result[targetLocation]["total"] = result[targetLocation]["total"].(int) + count
				result[targetLocation]["totalelement_concrete"] = result[targetLocation]["totalelement_concrete"].(float64) + concreteSum

				switch status {
				case "production":
					result[targetLocation]["production"] = result[targetLocation]["production"].(int) + count
					result[targetLocation]["production_concrete"] = result[targetLocation]["production_concrete"].(float64) + concreteSum
				case "dispatch":
					result[targetLocation]["dispatch"] = result[targetLocation]["dispatch"].(int) + count
					result[targetLocation]["dispatch_concrete"] = result[targetLocation]["dispatch_concrete"].(float64) + concreteSum
				case "erected":
					result[targetLocation]["erected"] = result[targetLocation]["erected"].(int) + count
					result[targetLocation]["erected_concrete"] = result[targetLocation]["erected_concrete"].(float64) + concreteSum
				default:
					result[targetLocation]["notinproduction"] = result[targetLocation]["notinproduction"].(int) + count
					result[targetLocation]["notinproduction_concrete"] = result[targetLocation]["notinproduction_concrete"].(float64) + concreteSum
				}
			}

			// stockyard, balances, erected balance
			for location := range result {
				dispatch := result[location]["dispatch"].(int)
				produced := result[location]["production"].(int)
				erected := result[location]["erected"].(int)
				stockyard := produced - dispatch
				if stockyard < 0 {
					stockyard = 0
				}
				result[location]["stockyard"] = stockyard

				dispatchConcrete := result[location]["dispatch_concrete"].(float64)
				producedConcrete := result[location]["production_concrete"].(float64)
				erectedConcrete := result[location]["erected_concrete"].(float64)
				stockyardConcrete := producedConcrete - dispatchConcrete
				if stockyardConcrete < 0 {
					stockyardConcrete = 0
				}
				result[location]["stockyard_concrete"] = stockyardConcrete

				total := result[location]["total"].(int)
				balance := total - produced
				result[location]["notinproduction"] = balance

				totalConcrete := result[location]["totalelement_concrete"].(float64)
				balanceConcrete := totalConcrete - producedConcrete
				result[location]["notinproduction_concrete"] = balanceConcrete

				erectedBalance := dispatch - erected
				if erectedBalance < 0 {
					erectedBalance = 0
				}
				result[location]["erectedbalance"] = erectedBalance

				erectedBalanceConcrete := dispatchConcrete - erectedConcrete
				if erectedBalanceConcrete < 0 {
					erectedBalanceConcrete = 0
				}
				result[location]["erectedbalance_concrete"] = erectedBalanceConcrete
			}

			return result
		}

		getBatchElementTypeBreakdownsWithDate := func(hierarchyIDs []int) map[int]map[string]interface{} {
			if len(hierarchyIDs) == 0 {
				return make(map[int]map[string]interface{})
			}

			result := make(map[int]map[string]interface{})
			for _, hierarchyID := range hierarchyIDs {
				result[hierarchyID] = make(map[string]interface{})
			}

			placeholders := make([]string, len(hierarchyIDs))
			args := make([]interface{}, len(hierarchyIDs)+3)
			args[0] = projectID
			args[1] = startDate
			args[2] = endDate
			for i, id := range hierarchyIDs {
				placeholders[i] = fmt.Sprintf("$%d", i+4)
				args[i+3] = id
			}

			query := fmt.Sprintf(`
				WITH element_data AS (
					SELECT 
						e.id,
						e.target_location,
						et.element_type_id,
						et.element_type,
						et.element_type_name,
						et.thickness,
						et.length,
						et.height,
						CASE WHEN ps.id IS NOT NULL AND ps.erected = true 
							AND DATE(ps.updated_at) BETWEEN DATE($2) AND DATE($3) THEN 'erected'
							WHEN ps.id IS NOT NULL AND ps.dispatch_status = true 
							AND ps.dispatch_start IS NOT NULL AND ps.dispatch_start BETWEEN $2 AND $3 THEN 'dispatch'
							ELSE 'notinproduction'
						END AS element_status,
						CASE WHEN ps.id IS NOT NULL AND ps.production_date BETWEEN $2 AND $3 THEN 1 ELSE 0 END AS is_produced
					FROM element e
					JOIN element_type et ON e.element_type_id = et.element_type_id
					LEFT JOIN precast_stock ps ON e.id = ps.element_id
					WHERE e.project_id = $1 AND e.target_location IN (%s)
				),
				status_counts AS (
					SELECT 
						target_location,
						element_type,
						element_status,
						COUNT(*) as count,
						COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
					FROM element_data
					GROUP BY target_location, element_type, element_status
				),
				production_counts AS (
					SELECT 
						target_location,
						element_type,
						COUNT(*) as count,
						COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
					FROM element_data
					WHERE is_produced = 1
					GROUP BY target_location, element_type
				)
				SELECT 
					target_location,
					element_type,
					element_status,
					count,
					concrete_sum
				FROM status_counts
				UNION ALL
				SELECT 
					target_location,
					element_type,
					'production' as element_status,
					count,
					concrete_sum
				FROM production_counts
				ORDER BY target_location, element_type
			`, strings.Join(placeholders, ","))

			rows, err := db.Query(query, args...)
			if err != nil {
				return result
			}
			defer rows.Close()

			tempData := make(map[int]map[string]map[string]interface{})

			for rows.Next() {
				var targetLocation int
				var elementType, status string
				var count int
				var concreteSum float64

				if err := rows.Scan(&targetLocation, &elementType, &status, &count, &concreteSum); err != nil {
					continue
				}

				if _, exists := tempData[targetLocation]; !exists {
					tempData[targetLocation] = make(map[string]map[string]interface{})
				}
				if _, exists := tempData[targetLocation][elementType]; !exists {
					tempData[targetLocation][elementType] = map[string]interface{}{
						"totalelement":             0,
						"production":               0,
						"stockyard":                0,
						"dispatch":                 0,
						"erected":                  0,
						"notinproduction":          0,
						"erectedbalance":           0,
						"totalelement_concrete":    0.0,
						"production_concrete":      0.0,
						"stockyard_concrete":       0.0,
						"dispatch_concrete":        0.0,
						"erected_concrete":         0.0,
						"notinproduction_concrete": 0.0,
						"erectedbalance_concrete":  0.0,
					}
				}

				elementData := tempData[targetLocation][elementType]
				elementData["totalelement"] = elementData["totalelement"].(int) + count
				elementData["totalelement_concrete"] = elementData["totalelement_concrete"].(float64) + concreteSum

				switch status {
				case "production":
					elementData["production"] = elementData["production"].(int) + count
					elementData["production_concrete"] = elementData["production_concrete"].(float64) + concreteSum
				case "dispatch":
					elementData["dispatch"] = elementData["dispatch"].(int) + count
					elementData["dispatch_concrete"] = elementData["dispatch_concrete"].(float64) + concreteSum
				case "erected":
					elementData["erected"] = elementData["erected"].(int) + count
					elementData["erected_concrete"] = elementData["erected_concrete"].(float64) + concreteSum
				default:
					elementData["notinproduction"] = elementData["notinproduction"].(int) + count
					elementData["notinproduction_concrete"] = elementData["notinproduction_concrete"].(float64) + concreteSum
				}
			}

			for _, elementTypes := range tempData {
				for _, elementData := range elementTypes {
					dispatch := elementData["dispatch"].(int)
					produced := elementData["production"].(int)
					erected := elementData["erected"].(int)
					stockyard := produced - dispatch
					if stockyard < 0 {
						stockyard = 0
					}
					elementData["stockyard"] = stockyard

					dispatchConcrete := elementData["dispatch_concrete"].(float64)
					producedConcrete := elementData["production_concrete"].(float64)
					erectedConcrete := elementData["erected_concrete"].(float64)
					stockyardConcrete := producedConcrete - dispatchConcrete
					if stockyardConcrete < 0 {
						stockyardConcrete = 0
					}
					elementData["stockyard_concrete"] = stockyardConcrete

					total := elementData["totalelement"].(int)
					balance := total - produced
					elementData["notinproduction"] = balance

					totalConcrete := elementData["totalelement_concrete"].(float64)
					balanceConcrete := totalConcrete - producedConcrete
					elementData["notinproduction_concrete"] = balanceConcrete

					erectedBalance := dispatch - erected
					if erectedBalance < 0 {
						erectedBalance = 0
					}
					elementData["erectedbalance"] = erectedBalance

					erectedBalanceConcrete := dispatchConcrete - erectedConcrete
					if erectedBalanceConcrete < 0 {
						erectedBalanceConcrete = 0
					}
					elementData["erectedbalance_concrete"] = erectedBalanceConcrete
				}
			}

			for location, elementTypes := range tempData {
				converted := make(map[string]interface{})
				for et, data := range elementTypes {
					converted[et] = data
				}
				result[location] = converted
			}

			return result
		}

		getCumulativeConcreteVolumesByElementType := func(hierarchyIDs []int) map[int]map[int]map[string]float64 {
			result := make(map[int]map[int]map[string]float64)
			for _, hierarchyID := range hierarchyIDs {
				result[hierarchyID] = make(map[int]map[string]float64)
			}

			if len(hierarchyIDs) == 0 {
				return result
			}

			placeholders := make([]string, len(hierarchyIDs))
			args := make([]interface{}, len(hierarchyIDs)+1)
			args[0] = projectID
			for i, id := range hierarchyIDs {
				placeholders[i] = fmt.Sprintf("$%d", i+2)
				args[i+1] = id
			}

			query := fmt.Sprintf(`
				WITH element_status AS (
					SELECT 
						e.target_location,
						e.element_type_id,
						et.thickness,
						et.length,
						et.height,
						CASE 
							WHEN ps.id IS NOT NULL THEN 'production'
							WHEN ps.id IS NOT NULL AND ps.dispatch_status = true AND ps.erected = false THEN 'dispatch'
							WHEN ps.id IS NOT NULL AND ps.erected = true THEN 'erected'
							ELSE 'none'
						END AS element_status
					FROM element e
					JOIN element_type et ON e.element_type_id = et.element_type_id
					LEFT JOIN precast_stock ps ON e.id = ps.element_id
					WHERE e.project_id = $1 AND e.target_location IN (%s)
				)
				SELECT 
					target_location,
					element_type_id,
					element_status,
					COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_volume
				FROM element_status
				GROUP BY target_location, element_type_id, element_status
			`, strings.Join(placeholders, ","))

			rows, _ := db.Query(query, args...)
			for rows.Next() {
				var location, etID int
				var status string
				var concreteVolume float64
				if err := rows.Scan(&location, &etID, &status, &concreteVolume); err == nil {
					if _, exists := result[location]; !exists {
						result[location] = make(map[int]map[string]float64)
					}
					if _, exists := result[location][etID]; !exists {
						result[location][etID] = map[string]float64{
							"production": 0.0,
							"stockyard":  0.0,
							"dispatch":   0.0,
							"erected":    0.0,
							"total":      0.0,
						}
					}
					switch status {
					case "production":
						result[location][etID]["production"] = concreteVolume
					case "stockyard":
						result[location][etID]["stockyard"] = concreteVolume
					case "dispatch":
						result[location][etID]["dispatch"] = concreteVolume
					case "erected":
						result[location][etID]["erected"] = concreteVolume
					}
					result[location][etID]["total"] += concreteVolume
				}
			}
			rows.Close()

			return result
		}

		titleCaser := cases.Title(language.Und)

		// PDF init
		pdf := gofpdf.New("L", "mm", "A4", "")
		pdf.AddPage()
		pdf.SetMargins(25, 25, 25)
		pdf.SetFont("Arial", "", 10)

		// ----------- Theme (colors only; no functionality changes) -----------
		// Matches the provided reference screenshot: dark blue headers + white text + light borders.
		headerBlue := struct{ R, G, B int }{R: 40, G: 60, B: 110}
		borderGray := struct{ R, G, B int }{R: 210, G: 210, B: 210}

		setTableHeaderStyle := func(pdf *gofpdf.Fpdf) {
			pdf.SetFillColor(headerBlue.R, headerBlue.G, headerBlue.B)
			pdf.SetTextColor(255, 255, 255)
			pdf.SetDrawColor(borderGray.R, borderGray.G, borderGray.B)
		}
		setTableBodyStyle := func(pdf *gofpdf.Fpdf) {
			pdf.SetFillColor(255, 255, 255)
			pdf.SetTextColor(0, 0, 0)
			pdf.SetDrawColor(borderGray.R, borderGray.G, borderGray.B)
		}

		// Header
		pdf.Ln(10)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Arial", "B", 24)
		reportTypeTitle := titleCaser.String(summaryType) + " Report"
		title := fmt.Sprintf("%s - %s", reportTypeTitle, projectName)
		if towerID != nil {
			var towerName string
			_ = db.QueryRow(`SELECT name FROM precast WHERE id = $1 AND project_id = $2`, *towerID, projectID).Scan(&towerName)
			title = fmt.Sprintf("%s - %s (Tower: %s)", reportTypeTitle, projectName, towerName)
		}
		pdf.CellFormat(0, 20, title, "0", 0, "C", false, 0, "")
		pdf.Ln(15)

		pdf.SetFont("Arial", "B", 13)
		pdf.CellFormat(0, 10, fmt.Sprintf("Period: %s", dateRangeStr), "0", 0, "C", false, 0, "")
		pdf.Ln(10)

		pdf.SetFont("Arial", "I", 9)
		pdf.CellFormat(0, 8, fmt.Sprintf("Generated On: %s", time.Now().Format("02-Jan-2006 15:04:05")), "0", 0, "C", false, 0, "")
		pdf.Ln(18)

		// Aggregator
		aggregateCounts := func(counts map[int]map[string]interface{}, hierarchyIDs []int) map[string]interface{} {
			aggregated := map[string]interface{}{
				"total":                    0,
				"production":               0,
				"stockyard":                0,
				"dispatch":                 0,
				"erected":                  0,
				"notinproduction":          0,
				"totalelement_concrete":    0.0,
				"production_concrete":      0.0,
				"stockyard_concrete":       0.0,
				"dispatch_concrete":        0.0,
				"erected_concrete":         0.0,
				"notinproduction_concrete": 0.0,
			}

			for _, hierarchyID := range hierarchyIDs {
				if data, exists := counts[hierarchyID]; exists {
					aggregated["total"] = aggregated["total"].(int) + data["total"].(int)
					aggregated["production"] = aggregated["production"].(int) + data["production"].(int)
					aggregated["stockyard"] = aggregated["stockyard"].(int) + data["stockyard"].(int)
					aggregated["dispatch"] = aggregated["dispatch"].(int) + data["dispatch"].(int)
					aggregated["erected"] = aggregated["erected"].(int) + data["erected"].(int)
					aggregated["notinproduction"] = aggregated["notinproduction"].(int) + data["notinproduction"].(int)
					aggregated["totalelement_concrete"] = aggregated["totalelement_concrete"].(float64) + data["totalelement_concrete"].(float64)
					aggregated["production_concrete"] = aggregated["production_concrete"].(float64) + data["production_concrete"].(float64)
					aggregated["stockyard_concrete"] = aggregated["stockyard_concrete"].(float64) + data["stockyard_concrete"].(float64)
					aggregated["dispatch_concrete"] = aggregated["dispatch_concrete"].(float64) + data["dispatch_concrete"].(float64)
					aggregated["erected_concrete"] = aggregated["erected_concrete"].(float64) + data["erected_concrete"].(float64)
					aggregated["notinproduction_concrete"] = aggregated["notinproduction_concrete"].(float64) + data["notinproduction_concrete"].(float64)
				}
			}

			dispatch := aggregated["dispatch"].(int)
			erected := aggregated["erected"].(int)
			erectedBalance := dispatch - erected
			if erectedBalance < 0 {
				erectedBalance = 0
			}
			aggregated["erectedbalance"] = erectedBalance

			dispatchConcrete := aggregated["dispatch_concrete"].(float64)
			erectedConcrete := aggregated["erected_concrete"].(float64)
			erectedBalanceConcrete := dispatchConcrete - erectedConcrete
			if erectedBalanceConcrete < 0 {
				erectedBalanceConcrete = 0
			}
			aggregated["erectedbalance_concrete"] = erectedBalanceConcrete

			return aggregated
		}

		if towerID == nil {
			// ---------------- Project-level report -------------------
			allHierarchyIDs := []int{}
			rows, _ := db.Query(`SELECT id FROM precast WHERE project_id = $1`, projectID)
			for rows.Next() {
				var id int
				if err := rows.Scan(&id); err == nil {
					allHierarchyIDs = append(allHierarchyIDs, id)
				}
			}
			rows.Close()

			projectCounts := getBatchElementCountsWithDate(allHierarchyIDs)
			projectTotals := aggregateCounts(projectCounts, allHierarchyIDs)

			// Project Totals
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "B", 16)
			pdf.CellFormat(0, 12, "Project Aggregate", "B", 1, "L", false, 0, "")

			pdf.SetFont("Arial", "B", 8.3)
			pageWidth := 247.0
			headers, widths := buildStatusHeaders(viewMode, false, "", pageWidth)
			setTableHeaderStyle(pdf)
			for i, h := range headers {
				pdf.CellFormat(widths[i], 16, h, "1", 0, "C", true, 0, "")
			}
			pdf.Ln(-1)

			setTableBodyStyle(pdf)
			pdf.SetFont("Arial", "", 8.3)
			writeStatusRow(
				pdf, viewMode, widths, 0,
				projectTotals["total"].(int), projectTotals["totalelement_concrete"].(float64),
				projectTotals["production"].(int), projectTotals["production_concrete"].(float64),
				projectTotals["dispatch"].(int), projectTotals["dispatch_concrete"].(float64),
				projectTotals["stockyard"].(int), projectTotals["stockyard_concrete"].(float64),
				projectTotals["erected"].(int), projectTotals["erected_concrete"].(float64),
			)
			pdf.Ln(14)

			// Helper for monthly/daily breakdown
			getElementCountsForDateRange := func(rangeStartDate, rangeEndDate time.Time) map[string]interface{} {
				placeholders := make([]string, len(allHierarchyIDs))
				args := make([]interface{}, len(allHierarchyIDs)+3)
				args[0] = projectID
				args[1] = rangeStartDate
				args[2] = rangeEndDate
				for i, id := range allHierarchyIDs {
					placeholders[i] = fmt.Sprintf("$%d", i+4)
					args[i+3] = id
				}

				query := fmt.Sprintf(`
					WITH element_data AS (
						SELECT 
							e.id,
							e.target_location,
							et.thickness,
							et.length,
							et.height,
							CASE WHEN ps.id IS NOT NULL AND ps.erected = true 
								AND DATE(ps.updated_at) BETWEEN DATE($2) AND DATE($3) THEN 'erected'
								WHEN ps.id IS NOT NULL AND ps.dispatch_status = true 
								AND ps.dispatch_start IS NOT NULL AND ps.dispatch_start BETWEEN $2 AND $3 THEN 'dispatch'
								ELSE 'notinproduction'
							END AS element_status,
							CASE WHEN ps.id IS NOT NULL AND ps.production_date BETWEEN $2 AND $3 THEN 1 ELSE 0 END AS is_produced
						FROM element e
						JOIN element_type et ON e.element_type_id = et.element_type_id
						LEFT JOIN precast_stock ps ON e.id = ps.element_id
						WHERE e.project_id = $1 AND e.target_location IN (%s)
					),
					status_counts AS (
						SELECT 
							element_status,
							COUNT(*) as count,
							COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
						FROM element_data
						GROUP BY element_status
					),
					production_counts AS (
						SELECT 
							COUNT(*) as count,
							COALESCE(SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000), 0) as concrete_sum
						FROM element_data
						WHERE is_produced = 1
					)
					SELECT 
						element_status,
						count,
						concrete_sum
					FROM status_counts
					UNION ALL
					SELECT 
						'production' as element_status,
						count,
						concrete_sum
					FROM production_counts
				`, strings.Join(placeholders, ","))

				result := map[string]interface{}{
					"total":                    0,
					"production":               0,
					"stockyard":                0,
					"dispatch":                 0,
					"erected":                  0,
					"notinproduction":          0,
					"erectedbalance":           0,
					"totalelement_concrete":    0.0,
					"production_concrete":      0.0,
					"stockyard_concrete":       0.0,
					"dispatch_concrete":        0.0,
					"erected_concrete":         0.0,
					"notinproduction_concrete": 0.0,
					"erectedbalance_concrete":  0.0,
				}

				rows, err := db.Query(query, args...)
				if err != nil {
					return result
				}
				defer rows.Close()

				for rows.Next() {
					var status string
					var count int
					var concreteSum float64

					if err := rows.Scan(&status, &count, &concreteSum); err != nil {
						continue
					}

					result["total"] = result["total"].(int) + count
					result["totalelement_concrete"] = result["totalelement_concrete"].(float64) + concreteSum

					switch status {
					case "production":
						result["production"] = result["production"].(int) + count
						result["production_concrete"] = result["production_concrete"].(float64) + concreteSum
					case "dispatch":
						result["dispatch"] = result["dispatch"].(int) + count
						result["dispatch_concrete"] = result["dispatch_concrete"].(float64) + concreteSum
					case "erected":
						result["erected"] = result["erected"].(int) + count
						result["erected_concrete"] = result["erected_concrete"].(float64) + concreteSum
					default:
						result["notinproduction"] = result["notinproduction"].(int) + count
						result["notinproduction_concrete"] = result["notinproduction_concrete"].(float64) + concreteSum
					}
				}

				dispatch := result["dispatch"].(int)
				produced := result["production"].(int)
				erected := result["erected"].(int)
				stockyard := produced - dispatch
				if stockyard < 0 {
					stockyard = 0
				}
				result["stockyard"] = stockyard

				dispatchConcrete := result["dispatch_concrete"].(float64)
				producedConcrete := result["production_concrete"].(float64)
				erectedConcrete := result["erected_concrete"].(float64)
				stockyardConcrete := producedConcrete - dispatchConcrete
				if stockyardConcrete < 0 {
					stockyardConcrete = 0
				}
				result["stockyard_concrete"] = stockyardConcrete

				total := result["total"].(int)
				balance := total - produced
				result["notinproduction"] = balance

				totalConcrete := result["totalelement_concrete"].(float64)
				balanceConcrete := totalConcrete - producedConcrete
				result["notinproduction_concrete"] = balanceConcrete

				erectedBalance := dispatch - erected
				if erectedBalance < 0 {
					erectedBalance = 0
				}
				result["erectedbalance"] = erectedBalance

				erectedBalanceConcrete := dispatchConcrete - erectedConcrete
				if erectedBalanceConcrete < 0 {
					erectedBalanceConcrete = 0
				}
				result["erectedbalance_concrete"] = erectedBalanceConcrete

				return result
			}

			// Yearly: Monthly breakdown
			if summaryType == "yearly" {
				year, _ := strconv.Atoi(yearStr)

				monthData := make(map[string]map[string]interface{})
				monthNames := []string{}
				for month := 1; month <= 12; month++ {
					monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
					monthEnd := monthStart.AddDate(0, 1, -1)
					monthEnd = time.Date(year, time.Month(month), monthEnd.Day(), 23, 59, 59, 0, location)

					monthTotals := getElementCountsForDateRange(monthStart, monthEnd)
					monthName := time.Month(month).String()
					monthNames = append(monthNames, monthName)
					monthData[monthName] = monthTotals
				}

				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 16)
				pdf.CellFormat(0, 12, "Monthly Breakdown", "B", 1, "L", false, 0, "")

				pageWidth := 247.0
				rowLabelWidth := 30.0
				availableWidth := pageWidth - rowLabelWidth
				colWidth := availableWidth / float64(len(monthNames))

				pdf.SetFont("Arial", "B", 8.3)
				setTableHeaderStyle(pdf)
				pdf.CellFormat(rowLabelWidth, 16, "", "1", 0, "C", true, 0, "")
				for _, monthName := range monthNames {
					pdf.CellFormat(colWidth, 16, monthName, "1", 0, "C", true, 0, "")
				}
				pdf.Ln(-1)

				rowLabels := getBreakdownRowLabels(viewMode)
				setTableBodyStyle(pdf)
				pdf.SetFont("Arial", "", 8.3)
				for _, rowLabel := range rowLabels {
					pdf.CellFormat(rowLabelWidth, 14, rowLabel, "1", 0, "L", false, 0, "")
					for _, monthName := range monthNames {
						data := monthData[monthName]
						value := getBreakdownValue(viewMode, rowLabel, data)
						pdf.CellFormat(colWidth, 14, value, "1", 0, "C", false, 0, "")
					}
					pdf.Ln(-1)
				}
				pdf.Ln(12)
			}

			// Monthly: Daily breakdown
			if summaryType == "monthly" {
				year, _ := strconv.Atoi(yearStr)
				month, _ := strconv.Atoi(monthStr)
				monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
				monthEnd := monthStart.AddDate(0, 1, -1)
				daysInMonth := monthEnd.Day()

				dateData := make(map[int]map[string]interface{})
				for day := 1; day <= daysInMonth; day++ {
					dayStart := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
					dayEnd := time.Date(year, time.Month(month), day, 23, 59, 59, 0, location)
					dayTotals := getElementCountsForDateRange(dayStart, dayEnd)
					dateData[day] = dayTotals
				}

				renderDateRangeTable := func(startDay, endDay int, title string) {
					pdf.SetTextColor(0, 0, 0)
					pdf.SetFont("Arial", "B", 16)
					pdf.CellFormat(0, 12, title, "B", 1, "L", false, 0, "")

					pageWidth := 247.0
					rowLabelWidth := 20.0
					availableWidth := pageWidth - rowLabelWidth
					numDays := endDay - startDay + 1
					colWidth := availableWidth / float64(numDays)

					pdf.SetFont("Arial", "B", 8.3)
					setTableHeaderStyle(pdf)
					pdf.CellFormat(rowLabelWidth, 18, "", "1", 0, "C", true, 0, "")
					for day := startDay; day <= endDay; day++ {
						pdf.CellFormat(colWidth, 18, fmt.Sprintf("%d", day), "1", 0, "C", true, 0, "")
					}
					pdf.Ln(-1)

					rowLabels := getBreakdownRowLabels(viewMode)
					setTableBodyStyle(pdf)
					pdf.SetFont("Arial", "", 8.3)
					for _, rowLabel := range rowLabels {
						pdf.CellFormat(rowLabelWidth, 16, rowLabel, "1", 0, "L", false, 0, "")
						for day := startDay; day <= endDay; day++ {
							data := dateData[day]
							value := getBreakdownValue(viewMode, rowLabel, data)
							pdf.CellFormat(colWidth, 16, value, "1", 0, "C", false, 0, "")
						}
						pdf.Ln(-1)
					}
					pdf.Ln(12)
				}

				renderDateRangeTable(1, minInt(10, daysInMonth), "Daily Breakdown (Days 1-10)")
				if daysInMonth > 10 {
					endDay := minInt(20, daysInMonth)
					renderDateRangeTable(11, endDay, fmt.Sprintf("Daily Breakdown (Days 11-%d)", endDay))
				}
				if daysInMonth > 20 {
					renderDateRangeTable(21, daysInMonth, fmt.Sprintf("Daily Breakdown (Days 21-%d)", daysInMonth))
				}
			}

			// -------- Tower + Floor + ET breakdowns (project-level) --------

			type TowerFloorData struct {
				TowerID                   int
				TowerName                 string
				FloorIDs                  []int
				FloorCounts               map[int]map[string]interface{}
				FloorBreakdowns           map[int]map[string]interface{}
				CumulativeConcreteVolByET map[int]map[int]map[string]float64
			}
			towerFloorDataList := []TowerFloorData{}

			rows, err = db.Query(`
				SELECT id, name FROM precast 
				WHERE project_id = $1 AND parent_id IS NULL
				ORDER BY name ASC
			`, projectID)
			if err == nil {
				defer rows.Close()

				for rows.Next() {
					var towerIDLocal int
					var towerName string
					if err := rows.Scan(&towerIDLocal, &towerName); err != nil {
						continue
					}

					floorIDs := []int{}
					floorRowsForTower, _ := db.Query(`
						SELECT id FROM precast 
						WHERE project_id = $1 AND parent_id = $2
					`, projectID, towerIDLocal)
					for floorRowsForTower.Next() {
						var floorID int
						if err := floorRowsForTower.Scan(&floorID); err == nil {
							floorIDs = append(floorIDs, floorID)
						}
					}
					floorRowsForTower.Close()

					floorCounts := getBatchElementCountsWithDate(floorIDs)
					towerTotals := aggregateCounts(floorCounts, floorIDs)
					floorBreakdowns := getBatchElementTypeBreakdownsWithDate(floorIDs)
					cumulativeConcreteVolByET := getCumulativeConcreteVolumesByElementType(floorIDs)

					towerFloorDataList = append(towerFloorDataList, TowerFloorData{
						TowerID:                   towerIDLocal,
						TowerName:                 towerName,
						FloorIDs:                  floorIDs,
						FloorCounts:               floorCounts,
						FloorBreakdowns:           floorBreakdowns,
						CumulativeConcreteVolByET: cumulativeConcreteVolByET,
					})

					// Tower totals
					pdf.SetTextColor(0, 0, 0)
					pdf.SetFont("Arial", "B", 15)
					pdf.CellFormat(0, 12, fmt.Sprintf("Tower: %s", towerName), "B", 1, "L", false, 0, "")

					pdf.SetFont("Arial", "B", 8.3)
					pageWidth := 247.0
					towerHeaders, towerWidths := buildStatusHeaders(viewMode, false, "", pageWidth)
					setTableHeaderStyle(pdf)
					for i, h := range towerHeaders {
						pdf.CellFormat(towerWidths[i], 16, h, "1", 0, "C", true, 0, "")
					}
					pdf.Ln(-1)

					setTableBodyStyle(pdf)
					pdf.SetFont("Arial", "", 8.3)
					writeStatusRow(
						pdf, viewMode, towerWidths, 0,
						towerTotals["total"].(int), towerTotals["totalelement_concrete"].(float64),
						towerTotals["production"].(int), towerTotals["production_concrete"].(float64),
						towerTotals["dispatch"].(int), towerTotals["dispatch_concrete"].(float64),
						towerTotals["stockyard"].(int), towerTotals["stockyard_concrete"].(float64),
						towerTotals["erected"].(int), towerTotals["erected_concrete"].(float64),
					)
					pdf.Ln(16)

					// Floors list
					floorRows, err := db.Query(`
						SELECT id, name FROM precast 
						WHERE project_id = $1 AND parent_id = $2
						ORDER BY name ASC
					`, projectID, towerIDLocal)
					if err == nil {
						pdf.SetFont("Arial", "B", 12)
						pdf.SetTextColor(0, 0, 0)
						pdf.Cell(0, 10, "Floors:")
						pdf.Ln(16)

						pdf.SetFont("Arial", "B", 7)
						fHeaders, fWidths := buildStatusHeaders(viewMode, true, "Floor Name", pageWidth)
						setTableHeaderStyle(pdf)
						for i, h := range fHeaders {
							pdf.CellFormat(fWidths[i], 16, h, "1", 0, "C", true, 0, "")
						}
						pdf.Ln(-1)

						setTableBodyStyle(pdf)
						pdf.SetFont("Arial", "", 8.3)
						for floorRows.Next() {
							var floorID int
							var floorName string
							if err := floorRows.Scan(&floorID, &floorName); err != nil {
								continue
							}
							floorData := floorCounts[floorID]

							floorTotal := floorData["total"].(int)
							floorTotalCum := floorData["totalelement_concrete"].(float64)
							floorProd := floorData["production"].(int)
							floorProdCum := floorData["production_concrete"].(float64)
							floorDispatch := floorData["dispatch"].(int)
							floorDispatchCum := floorData["dispatch_concrete"].(float64)
							floorStock := floorData["stockyard"].(int)
							floorStockCum := floorData["stockyard_concrete"].(float64)
							floorErect := floorData["erected"].(int)
							floorErectCum := floorData["erected_concrete"].(float64)

							pdf.CellFormat(fWidths[0], 14, floorName, "1", 0, "L", false, 0, "")
							writeStatusRow(
								pdf, viewMode, fWidths, 1,
								floorTotal, floorTotalCum,
								floorProd, floorProdCum,
								floorDispatch, floorDispatchCum,
								floorStock, floorStockCum,
								floorErect, floorErectCum,
							)
							pdf.Ln(-1)
						}
						floorRows.Close()
					}
					pdf.Ln(10)
				}
			}

			// Single floors not under any tower
			singleFloorIDs := []int{}
			singleFloorMap := make(map[int]string)
			var singleFloorBreakdowns map[int]map[string]interface{}
			var singleCumulativeConcreteVolByET map[int]map[int]map[string]float64

			singleFloorRows, err := db.Query(`
				SELECT id, name FROM precast 
				WHERE project_id = $1 AND parent_id IS NOT NULL
				AND id NOT IN (SELECT id FROM precast WHERE project_id = $1 AND parent_id IS NOT NULL AND parent_id IN (SELECT id FROM precast WHERE project_id = $1 AND parent_id IS NULL))
				ORDER BY name ASC
			`, projectID)
			if err == nil {
				defer singleFloorRows.Close()
				for singleFloorRows.Next() {
					var floorID int
					var floorName string
					if err := singleFloorRows.Scan(&floorID, &floorName); err != nil {
						continue
					}
					singleFloorIDs = append(singleFloorIDs, floorID)
					singleFloorMap[floorID] = floorName
				}

				if len(singleFloorIDs) > 0 {
					singleFloorCounts := getBatchElementCountsWithDate(singleFloorIDs)
					singleFloorBreakdowns = getBatchElementTypeBreakdownsWithDate(singleFloorIDs)
					singleCumulativeConcreteVolByET = getCumulativeConcreteVolumesByElementType(singleFloorIDs)

					pageWidth := 247.0
					for _, floorID := range singleFloorIDs {
						floorName := singleFloorMap[floorID]
						floorData := singleFloorCounts[floorID]

						pdf.SetTextColor(0, 0, 0)
						pdf.SetFont("Arial", "B", 15)
						pdf.CellFormat(0, 12, fmt.Sprintf("Floor: %s", floorName), "B", 0, "L", false, 0, "")
						pdf.Ln(10)

						pdf.SetFont("Arial", "B", 7)
						headers, widths := buildStatusHeaders(viewMode, false, "", pageWidth)
						setTableHeaderStyle(pdf)
						for i, h := range headers {
							pdf.CellFormat(widths[i], 16, h, "1", 0, "C", true, 0, "")
						}
						pdf.Ln(-1)

						setTableBodyStyle(pdf)
						pdf.SetFont("Arial", "", 7)
						floorTotal := floorData["total"].(int)
						floorTotalCum := floorData["totalelement_concrete"].(float64)
						floorProd := floorData["production"].(int)
						floorProdCum := floorData["production_concrete"].(float64)
						floorDispatch := floorData["dispatch"].(int)
						floorDispatchCum := floorData["dispatch_concrete"].(float64)
						floorStock := floorData["stockyard"].(int)
						floorStockCum := floorData["stockyard_concrete"].(float64)
						floorErect := floorData["erected"].(int)
						floorErectCum := floorData["erected_concrete"].(float64)

						writeStatusRow(
							pdf, viewMode, widths, 0,
							floorTotal, floorTotalCum,
							floorProd, floorProdCum,
							floorDispatch, floorDispatchCum,
							floorStock, floorStockCum,
							floorErect, floorErectCum,
						)
						pdf.Ln(12)
					}
				}
			}

			// ---------- Element Type Breakdown section (project-level) ----------

			{
				pdf.Ln(15)
				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 18)
				pdf.CellFormat(0, 14, "Element Type Breakdown", "B", 1, "C", false, 0, "")

				pageWidth := 247.0

				// For each tower
				for _, towerData := range towerFloorDataList {
					pdf.SetTextColor(0, 0, 0)
					pdf.SetFont("Arial", "B", 15)
					pdf.CellFormat(0, 12, fmt.Sprintf("Tower: %s - Element Type Breakdown", towerData.TowerName), "B", 1, "L", false, 0, "")

					// Tower aggregate by element type
					towerAggregate := make(map[string]map[string]interface{})
					for _, floorID := range towerData.FloorIDs {
						floorBreakdown := towerData.FloorBreakdowns[floorID]
						for elementType, typeData := range floorBreakdown {
							etData := typeData.(map[string]interface{})
							if _, exists := towerAggregate[elementType]; !exists {
								towerAggregate[elementType] = map[string]interface{}{
									"totalelement":          0,
									"production":            0,
									"stockyard":             0,
									"dispatch":              0,
									"erected":               0,
									"totalelement_concrete": 0.0,
									"production_concrete":   0.0,
									"stockyard_concrete":    0.0,
									"dispatch_concrete":     0.0,
									"erected_concrete":      0.0,
								}
							}
							towerAggregate[elementType]["totalelement"] = towerAggregate[elementType]["totalelement"].(int) + etData["totalelement"].(int)
							towerAggregate[elementType]["production"] = towerAggregate[elementType]["production"].(int) + etData["production"].(int)
							towerAggregate[elementType]["stockyard"] = towerAggregate[elementType]["stockyard"].(int) + etData["stockyard"].(int)
							towerAggregate[elementType]["dispatch"] = towerAggregate[elementType]["dispatch"].(int) + etData["dispatch"].(int)
							towerAggregate[elementType]["erected"] = towerAggregate[elementType]["erected"].(int) + etData["erected"].(int)
							towerAggregate[elementType]["totalelement_concrete"] = towerAggregate[elementType]["totalelement_concrete"].(float64) + etData["totalelement_concrete"].(float64)
							towerAggregate[elementType]["production_concrete"] = towerAggregate[elementType]["production_concrete"].(float64) + etData["production_concrete"].(float64)
							towerAggregate[elementType]["stockyard_concrete"] = towerAggregate[elementType]["stockyard_concrete"].(float64) + etData["stockyard_concrete"].(float64)
							towerAggregate[elementType]["dispatch_concrete"] = towerAggregate[elementType]["dispatch_concrete"].(float64) + etData["dispatch_concrete"].(float64)
							towerAggregate[elementType]["erected_concrete"] = towerAggregate[elementType]["erected_concrete"].(float64) + etData["erected_concrete"].(float64)
						}
					}

					if len(towerAggregate) > 0 {
						pdf.SetFont("Arial", "B", 12)
						pdf.SetTextColor(0, 0, 0)
						pdf.CellFormat(0, 9, fmt.Sprintf("  Tower Aggregate - %s:", towerData.TowerName), "0", 0, "L", false, 0, "")
						pdf.Ln(8)

						pdf.SetFont("Arial", "B", 7)
						etHeaders, etWidths := buildStatusHeaders(viewMode, true, "Element Type", pageWidth)
						setTableHeaderStyle(pdf)
						for i, h := range etHeaders {
							pdf.CellFormat(etWidths[i], 16, h, "1", 0, "C", true, 0, "")
						}
						pdf.Ln(-1)

						setTableBodyStyle(pdf)
						pdf.SetFont("Arial", "", 8.3)
						for elementType, etData := range towerAggregate {
							var etID int
							for eid, ename := range elementTypeMap {
								if ename == elementType {
									etID = eid
									break
								}
							}
							aggCumConcreteVol := map[string]float64{
								"production": 0.0,
								"stockyard":  0.0,
								"dispatch":   0.0,
								"erected":    0.0,
								"total":      0.0,
							}
							for _, floorID := range towerData.FloorIDs {
								if floorVol, exists := towerData.CumulativeConcreteVolByET[floorID][etID]; exists {
									aggCumConcreteVol["production"] += floorVol["production"]
									aggCumConcreteVol["stockyard"] += floorVol["stockyard"]
									aggCumConcreteVol["dispatch"] += floorVol["dispatch"]
									aggCumConcreteVol["erected"] += floorVol["erected"]
									aggCumConcreteVol["total"] += floorVol["total"]
								}
							}

							etTotal := etData["totalelement"].(int)
							if etTotal <= 0 {
								continue
							}
							etTotalCum := etData["totalelement_concrete"].(float64)
							etProd := etData["production"].(int)
							etProdCum := aggCumConcreteVol["production"]
							etDispatch := etData["dispatch"].(int)
							etDispatchCum := aggCumConcreteVol["dispatch"]
							etStock := etData["stockyard"].(int)
							etStockCum := aggCumConcreteVol["stockyard"]
							etErect := etData["erected"].(int)
							etErectCum := aggCumConcreteVol["erected"]

							pdf.CellFormat(etWidths[0], 14, elementType, "1", 0, "L", false, 0, "")
							writeStatusRow(
								pdf, viewMode, etWidths, 1,
								etTotal, etTotalCum,
								etProd, etProdCum,
								etDispatch, etDispatchCum,
								etStock, etStockCum,
								etErect, etErectCum,
							)
							pdf.Ln(-1)
						}
						pdf.Ln(10)
					}

					// Per-floor ET breakdown for tower
					floorRows, err := db.Query(`
						SELECT id, name FROM precast 
						WHERE project_id = $1 AND parent_id = $2
						ORDER BY name ASC
					`, projectID, towerData.TowerID)
					if err == nil {
						for floorRows.Next() {
							var floorID int
							var floorName string
							if err := floorRows.Scan(&floorID, &floorName); err != nil {
								continue
							}
							floorBreakdown := towerData.FloorBreakdowns[floorID]
							if len(floorBreakdown) == 0 {
								continue
							}
							pdf.Ln(8)
							pdf.SetFont("Arial", "B", 11)
							pdf.SetTextColor(0, 0, 0)
							pdf.CellFormat(0, 9, fmt.Sprintf("  Element Type Breakdown for %s:", floorName), "0", 1, "L", false, 0, "")

							pdf.SetFont("Arial", "B", 7)
							etHeaders, etWidths := buildStatusHeaders(viewMode, true, "Element Type", pageWidth)
							setTableHeaderStyle(pdf)
							for i, h := range etHeaders {
								pdf.CellFormat(etWidths[i], 16, h, "1", 0, "C", true, 0, "")
							}
							pdf.Ln(-1)

							setTableBodyStyle(pdf)
							pdf.SetFont("Arial", "", 7)
							for elementType, typeData := range floorBreakdown {
								etData := typeData.(map[string]interface{})

								var etID int
								for eid, ename := range elementTypeMap {
									if ename == elementType {
										etID = eid
										break
									}
								}
								etCumConcreteVol, exists := towerData.CumulativeConcreteVolByET[floorID][etID]
								if !exists {
									etCumConcreteVol = map[string]float64{
										"production": 0.0,
										"stockyard":  0.0,
										"dispatch":   0.0,
										"erected":    0.0,
										"total":      0.0,
									}
								}

								etTotal := etData["totalelement"].(int)
								if etTotal <= 0 {
									continue
								}
								etTotalCum := etData["totalelement_concrete"].(float64)
								etProd := etData["production"].(int)
								etProdCum := etCumConcreteVol["production"]
								etDispatch := etData["dispatch"].(int)
								etDispatchCum := etCumConcreteVol["dispatch"]
								etStock := etData["stockyard"].(int)
								etStockCum := etCumConcreteVol["stockyard"]
								etErect := etData["erected"].(int)
								etErectCum := etCumConcreteVol["erected"]

								pdf.CellFormat(etWidths[0], 14, elementType, "1", 0, "L", false, 0, "")
								writeStatusRow(
									pdf, viewMode, etWidths, 1,
									etTotal, etTotalCum,
									etProd, etProdCum,
									etDispatch, etDispatchCum,
									etStock, etStockCum,
									etErect, etErectCum,
								)
								pdf.Ln(-1)
							}
							pdf.SetTextColor(0, 0, 0)
							pdf.Ln(6)
						}
						floorRows.Close()
					}
					pdf.Ln(10)
				}

				// ET breakdown for single floors
				if len(singleFloorIDs) > 0 && singleFloorBreakdowns != nil {
					for _, floorID := range singleFloorIDs {
						floorName := singleFloorMap[floorID]
						floorBreakdown := singleFloorBreakdowns[floorID]
						if len(floorBreakdown) == 0 {
							continue
						}

						pdf.SetTextColor(0, 0, 0)
						pdf.SetFont("Arial", "B", 15)
						pdf.CellFormat(0, 12, fmt.Sprintf("Floor: %s - Element Type Breakdown", floorName), "B", 1, "L", false, 0, "")

						pdf.SetFont("Arial", "B", 11)
						pdf.SetTextColor(0, 0, 0)
						pdf.CellFormat(0, 9, fmt.Sprintf("  Element Type Breakdown for %s:", floorName), "0", 0, "L", false, 0, "")
						pdf.Ln(6)

						pdf.SetFont("Arial", "B", 7)
						etHeaders, etWidths := buildStatusHeaders(viewMode, true, "Element Type", pageWidth)
						setTableHeaderStyle(pdf)
						for i, h := range etHeaders {
							pdf.CellFormat(etWidths[i], 16, h, "1", 0, "C", true, 0, "")
						}
						pdf.Ln(-1)

						setTableBodyStyle(pdf)
						pdf.SetFont("Arial", "", 7)
						for elementType, typeData := range floorBreakdown {
							etData := typeData.(map[string]interface{})

							var etID int
							for eid, ename := range elementTypeMap {
								if ename == elementType {
									etID = eid
									break
								}
							}
							etCumConcreteVol, exists := singleCumulativeConcreteVolByET[floorID][etID]
							if !exists {
								etCumConcreteVol = map[string]float64{
									"production": 0.0,
									"stockyard":  0.0,
									"dispatch":   0.0,
									"erected":    0.0,
									"total":      0.0,
								}
							}

							etTotal := etData["totalelement"].(int)
							if etTotal <= 0 {
								continue
							}
							etTotalCum := etData["totalelement_concrete"].(float64)
							etProd := etData["production"].(int)
							etProdCum := etCumConcreteVol["production"]
							etDispatch := etData["dispatch"].(int)
							etDispatchCum := etCumConcreteVol["dispatch"]
							etStock := etData["stockyard"].(int)
							etStockCum := etCumConcreteVol["stockyard"]
							etErect := etData["erected"].(int)
							etErectCum := etCumConcreteVol["erected"]

							pdf.CellFormat(etWidths[0], 14, elementType, "1", 0, "L", false, 0, "")
							writeStatusRow(
								pdf, viewMode, etWidths, 1,
								etTotal, etTotalCum,
								etProd, etProdCum,
								etDispatch, etDispatchCum,
								etStock, etStockCum,
								etErect, etErectCum,
							)
							pdf.Ln(-1)
						}
						pdf.SetTextColor(0, 0, 0)
						pdf.Ln(10)
					}
				}
			}

		} else {
			// ---------------- Tower-specific report -------------------

			var towerName string
			err = db.QueryRow(`SELECT name FROM precast WHERE id = $1 AND project_id = $2`, *towerID, projectID).Scan(&towerName)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Tower not found"})
				return
			}

			floorIDs := []int{}
			floorRowsForTower, _ := db.Query(`
				SELECT id FROM precast 
				WHERE project_id = $1 AND parent_id = $2
			`, projectID, *towerID)
			for floorRowsForTower.Next() {
				var floorID int
				if err := floorRowsForTower.Scan(&floorID); err == nil {
					floorIDs = append(floorIDs, floorID)
				}
			}
			floorRowsForTower.Close()

			floorCounts := getBatchElementCountsWithDate(floorIDs)
			towerTotals := aggregateCounts(floorCounts, floorIDs)
			floorBreakdowns := getBatchElementTypeBreakdownsWithDate(floorIDs)
			cumulativeConcreteVolByET := getCumulativeConcreteVolumesByElementType(floorIDs)

			pageWidth := 247.0
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "B", 16)
			pdf.CellFormat(0, 12, fmt.Sprintf("Tower Totals: %s", towerName), "B", 0, "L", false, 0, "")
			pdf.Ln(10)

			pdf.SetFont("Arial", "B", 7)
			headers, widths := buildStatusHeaders(viewMode, false, "", pageWidth)
			setTableHeaderStyle(pdf)
			for i, h := range headers {
				pdf.CellFormat(widths[i], 16, h, "1", 0, "C", true, 0, "")
			}
			pdf.Ln(-1)

			setTableBodyStyle(pdf)
			pdf.SetFont("Arial", "", 7)
			writeStatusRow(
				pdf, viewMode, widths, 0,
				towerTotals["total"].(int), towerTotals["totalelement_concrete"].(float64),
				towerTotals["production"].(int), towerTotals["production_concrete"].(float64),
				towerTotals["dispatch"].(int), towerTotals["dispatch_concrete"].(float64),
				towerTotals["stockyard"].(int), towerTotals["stockyard_concrete"].(float64),
				towerTotals["erected"].(int), towerTotals["erected_concrete"].(float64),
			)
			pdf.Ln(12)

			// Floors
			floorRows, err := db.Query(`
				SELECT id, name FROM precast 
				WHERE project_id = $1 AND parent_id = $2
				ORDER BY name ASC
			`, projectID, *towerID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch floors"})
				return
			}
			defer floorRows.Close()

			pdf.SetFont("Arial", "B", 12)
			pdf.SetTextColor(0, 0, 0)
			pdf.Cell(0, 10, "Floors:")
			pdf.Ln(16)

			pdf.SetFont("Arial", "B", 7)
			fHeaders, fWidths := buildStatusHeaders(viewMode, true, "Floor Name", pageWidth)
			setTableHeaderStyle(pdf)
			for i, h := range fHeaders {
				pdf.CellFormat(fWidths[i], 16, h, "1", 0, "C", true, 0, "")
			}
			pdf.Ln(-1)

			setTableBodyStyle(pdf)
			pdf.SetFont("Arial", "", 7)
			for floorRows.Next() {
				var floorID int
				var floorName string
				if err := floorRows.Scan(&floorID, &floorName); err != nil {
					continue
				}

				floorData := floorCounts[floorID]
				floorTotal := floorData["total"].(int)
				floorTotalCum := floorData["totalelement_concrete"].(float64)
				floorProd := floorData["production"].(int)
				floorProdCum := floorData["production_concrete"].(float64)
				floorDispatch := floorData["dispatch"].(int)
				floorDispatchCum := floorData["dispatch_concrete"].(float64)
				floorStock := floorData["stockyard"].(int)
				floorStockCum := floorData["stockyard_concrete"].(float64)
				floorErect := floorData["erected"].(int)
				floorErectCum := floorData["erected_concrete"].(float64)

				pdf.CellFormat(fWidths[0], 14, floorName, "1", 0, "L", false, 0, "")
				writeStatusRow(
					pdf, viewMode, fWidths, 1,
					floorTotal, floorTotalCum,
					floorProd, floorProdCum,
					floorDispatch, floorDispatchCum,
					floorStock, floorStockCum,
					floorErect, floorErectCum,
				)
				pdf.Ln(-1)
			}

			// Tower ET breakdown
			{
				pdf.Ln(15)
				pdf.SetTextColor(0, 0, 0)
				pdf.SetFont("Arial", "B", 18)
				pdf.CellFormat(0, 14, "Element Type Breakdown", "B", 0, "C", false, 0, "")
				pdf.Ln(12)

				pdf.SetFont("Arial", "B", 15)
				pdf.CellFormat(0, 12, fmt.Sprintf("Tower: %s - Element Type Breakdown", towerName), "B", 0, "L", false, 0, "")
				pdf.Ln(10)

				floorRows, err = db.Query(`
					SELECT id, name FROM precast 
					WHERE project_id = $1 AND parent_id = $2
					ORDER BY name ASC
				`, projectID, *towerID)
				if err == nil {
					for floorRows.Next() {
						var floorID int
						var floorName string
						if err := floorRows.Scan(&floorID, &floorName); err != nil {
							continue
						}

						floorBreakdown := floorBreakdowns[floorID]
						if len(floorBreakdown) == 0 {
							continue
						}

						pdf.SetFont("Arial", "B", 11)
						pdf.SetTextColor(0, 0, 0)
						pdf.CellFormat(0, 9, fmt.Sprintf("  Element Type Breakdown for %s:", floorName), "0", 0, "L", false, 0, "")
						pdf.Ln(6)

						pdf.SetFont("Arial", "B", 7)
						etHeaders, etWidths := buildStatusHeaders(viewMode, true, "Element Type", pageWidth)
						setTableHeaderStyle(pdf)
						for i, h := range etHeaders {
							pdf.CellFormat(etWidths[i], 16, h, "1", 0, "C", true, 0, "")
						}
						pdf.Ln(-1)

						setTableBodyStyle(pdf)
						pdf.SetFont("Arial", "", 7)
						for elementType, typeData := range floorBreakdown {
							etData := typeData.(map[string]interface{})

							var etID int
							for eid, ename := range elementTypeMap {
								if ename == elementType {
									etID = eid
									break
								}
							}
							etCumConcreteVol, exists := cumulativeConcreteVolByET[floorID][etID]
							if !exists {
								etCumConcreteVol = map[string]float64{
									"production": 0.0,
									"stockyard":  0.0,
									"dispatch":   0.0,
									"erected":    0.0,
									"total":      0.0,
								}
							}

							etTotal := etData["totalelement"].(int)
							if etTotal <= 0 {
								continue
							}
							etTotalCum := etData["totalelement_concrete"].(float64)
							etProd := etData["production"].(int)
							etProdCum := etCumConcreteVol["production"]
							etDispatch := etData["dispatch"].(int)
							etDispatchCum := etCumConcreteVol["dispatch"]
							etStock := etData["stockyard"].(int)
							etStockCum := etCumConcreteVol["stockyard"]
							etErect := etData["erected"].(int)
							etErectCum := etCumConcreteVol["erected"]

							pdf.CellFormat(etWidths[0], 14, elementType, "1", 0, "L", false, 0, "")
							writeStatusRow(
								pdf, viewMode, etWidths, 1,
								etTotal, etTotalCum,
								etProd, etProdCum,
								etDispatch, etDispatchCum,
								etStock, etStockCum,
								etErect, etErectCum,
							)
							pdf.Ln(-1)
						}
						pdf.SetFont("Arial", "", 7)
						pdf.SetTextColor(0, 0, 0)
						pdf.Ln(6)
					}
					floorRows.Close()
				}
			}
		}

		// Footer
		pdf.Ln(10)
		pdf.SetFont("Arial", "I", 9)
		pdf.Cell(0, 8, "This is a system-generated report. No signature required.")

		filename := fmt.Sprintf("dashboard_export_project_%d", projectID)
		if towerID != nil {
			filename = fmt.Sprintf("dashboard_export_project_%d_tower_%d", projectID, *towerID)
		}

		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.pdf", filename))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}
	}
}

// helper for monthly rendering
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetStockyardReportsByStockyards godoc
// @Summary      Get stockyard reports by stockyards for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/stockyard_reports_by_stockyards/{project_id} [get]
func GetStockyardReportsByStockyards(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		var userID int
		err = db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // default to yearly
		yearStr := c.Query("year")
		monthStr := c.Query("month")

		layout := "2006-01-02"
		location := time.Now().Location()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			now := time.Now()
			isCurrentYear := now.Year() == year

			monthlyData := make([]map[string]interface{}, 0)
			for m := 1; m <= 12; m++ {
				if isCurrentYear && m > int(now.Month()) {
					break
				}
				start := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)

				counts := fetchCountsForRangeViaManager(db, projectID, start, end, userID)
				monthlyData = append(monthlyData, gin.H{
					"name":        start.Month().String(),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, monthlyData)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			daysInMonth := start.AddDate(0, 1, -1).Day()

			rangeData := make([]map[string]interface{}, 0)
			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				counts := fetchCountsForRangeViaManager(db, projectID, startRange, endRange, userID)
				rangeData = append(rangeData, gin.H{
					"name":        fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, rangeData)

		case "weekly":
			yearStr := c.Query("year")
			monthStr := c.Query("month")
			dayStr := c.Query("date")

			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}

			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))

			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date combination. Use valid year, month, and date"})
				return
			}

			// Get 7-day window ending at startDate
			weekStart := startDate.AddDate(0, 0, -6)

			weekData := make([]map[string]interface{}, 0)
			for i := 0; i < 7; i++ {
				currentDate := weekStart.AddDate(0, 0, i)
				counts := fetchCountsForRangeViaManager(db, projectID, currentDate, currentDate, userID)
				weekData = append(weekData, gin.H{
					"name":        currentDate.Format(layout),
					"checkins":    counts.Casted,
					"checkouts":   counts.Erected,
					"adjustments": 0,
				})
			}
			c.JSON(http.StatusOK, weekData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
		}

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched production reports",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

func fetchCountsForRangeViaManager(
	db *sql.DB,
	projectID int,
	start, end time.Time,
	userID int,
) SummaryCounts {

	var planned, casted, stockyard, erected, dispatch int
	var concreteRequired, concreteUsed, concreteBalance float64

	// 1. Planned (activities are project-wide, not stockyard-specific)
	plannedQuery := `
		SELECT COUNT(DISTINCT element_id)
		FROM activity
		WHERE project_id = $1
		  AND start_date BETWEEN $2 AND $3
	`
	_ = db.QueryRow(plannedQuery, projectID, start, end).Scan(&planned)

	// 2. Casted (ONLY stockyards assigned to this manager)
	castedQuery := `
		SELECT COUNT(DISTINCT ps.element_id)
		FROM precast_stock ps
		JOIN project_stockyard psy
		  ON psy.stockyard_id = ps.stockyard_id
		WHERE ps.project_id = $1
		  AND psy.user_id = $2
		  AND ps.production_date BETWEEN $3 AND $4
	`
	_ = db.QueryRow(castedQuery, projectID, userID, start, end).Scan(&casted)

	// 3. Dispatch (manager's stockyards only)
	dispatchQuery := `
		SELECT COUNT(DISTINCT ps.element_id)
		FROM precast_stock ps
		JOIN project_stockyard psy
		  ON psy.stockyard_id = ps.stockyard_id
		WHERE ps.project_id = $1
		  AND psy.user_id = $2
		  AND ps.dispatch_status = true
		  AND ps.dispatch_start >= $3
		  AND ps.dispatch_end <= $4
	`
	_ = db.QueryRow(dispatchQuery, projectID, userID, start, end).Scan(&dispatch)

	// 4. Erected (manager's stockyards only)
	erectedQuery := `
		SELECT COUNT(DISTINCT ps.element_id)
		FROM precast_stock ps
		JOIN project_stockyard psy
		  ON psy.stockyard_id = ps.stockyard_id
		WHERE ps.project_id = $1
		  AND psy.user_id = $2
		  AND ps.erected = true
		  AND DATE(ps.updated_at) BETWEEN DATE($3) AND DATE($4)
	`
	_ = db.QueryRow(erectedQuery, projectID, userID, start, end).Scan(&erected)

	// 5. Stockyard balance = casted - erected
	stockyard = casted - erected
	if stockyard < 0 {
		stockyard = 0
	}

	return SummaryCounts{
		Planned:          planned,
		Casted:           casted,
		Stockyard:        stockyard,
		Dispatch:         dispatch,
		Erected:          erected,
		ConcreteRequired: concreteRequired,
		ConcreteUsed:     concreteUsed,
		ConcreteBalance:  concreteBalance,
	}
}

// GetElementTypeReportsviaManager godoc
// @Summary      Get element type reports (assigned to manager)
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_type_reports_assigned/{project_id} [get]
func GetElementTypeReportsviaManager(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		var userID int
		err = db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")
		location := time.Now().Location()

		var start, end time.Time
		now := time.Now()

		switch summaryType {
		case "yearly":
			year, _ := strconv.Atoi(yearStr)
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			if year == now.Year() {
				end = now
			} else {
				end = time.Date(year, 12, 31, 23, 59, 59, 0, location)
			}

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, 0).Add(-time.Second)

		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			day, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}
			start = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
			end = start.AddDate(0, 0, 1).Add(-time.Second)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type parameter"})
			return
		}

		// Fetch data for the calculated range
		counts, err := fetchElementTypeCountsviaManager(db, projectID, start, end, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, defaultEmptyArray(counts))

		// Save activity log
		log := models.ActivityLog{
			EventContext: "ElementTypeReport",
			EventName:    "Get",
			Description:  "Fetched element type wise stock report",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		_ = SaveActivityLog(db, log)
	}
}

func fetchElementTypeCountsviaManager(
	db *sql.DB,
	projectID int,
	start, end time.Time,
	userID int,
) ([]map[string]any, error) {

	query := `
		SELECT
			et.element_type,
			COUNT(DISTINCT ps.element_id) AS count
		FROM precast_stock ps
		JOIN project_stockyard psy
			ON psy.stockyard_id = ps.stockyard_id
		JOIN element e
			ON ps.element_id = e.id
		JOIN element_type et
			ON e.element_type_id = et.element_type_id
		WHERE ps.project_id = $1
		  AND psy.user_id = $2
		  AND ps.created_at BETWEEN $3 AND $4
		GROUP BY et.element_type
		ORDER BY et.element_type
	`

	rows, err := db.Query(query, projectID, userID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}

		result = append(result, gin.H{
			"element_type": name,
			"count":        count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

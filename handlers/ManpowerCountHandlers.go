package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// ==================== MANPOWER COUNT CRUD OPERATIONS ====================

// CreateManpowerCountBulk creates multiple manpower count entries for different people, skill types, and skills
// @Summary Create bulk manpower count
// @Description Create multiple manpower count entries for different people, skill types, and skills with individual quantities
// @Tags ManpowerCount
// @Accept json
// @Produce json
// @Param request body models.ManpowerCountBulkCreate true "Bulk manpower count creation request with nested people, skill types, and skills structure"
// @Success 201 {object} models.ManpowerCountBulkResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/manpower-count/bulk [post]
func CreateManpowerCountBulk(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var bulkRequest models.ManpowerCountBulkCreate
		if err := c.ShouldBindJSON(&bulkRequest); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", bulkRequest.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if tower exists (if provided)
		if bulkRequest.TowerID != nil {
			var towerExists bool
			err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM precast WHERE id = $1)", *bulkRequest.TowerID).Scan(&towerExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
			if !towerExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Tower does not exist"})
				return
			}
		}

		// Validate all people, skill types, and skills exist
		for _, peopleWithSkillTypes := range bulkRequest.Skills {
			// Check if people exists
			var peopleExists bool
			err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM people WHERE id = $1)", peopleWithSkillTypes.PeopleID).Scan(&peopleExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
			if !peopleExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "People with ID " + strconv.Itoa(int(peopleWithSkillTypes.PeopleID)) + " does not exist"})
				return
			}

			// Check skill types and skills for this person
			for _, skillTypeWithCount := range peopleWithSkillTypes.SkillTypes {
				// Check if skill type exists
				var skillTypeExists bool
				err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skill_types WHERE id = $1)", skillTypeWithCount.SkillTypeID).Scan(&skillTypeExists)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
					return
				}
				if !skillTypeExists {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Skill type with ID " + strconv.Itoa(int(skillTypeWithCount.SkillTypeID)) + " does not exist"})
					return
				}

				// Check if all skills exist
				for _, skillWithQuantity := range skillTypeWithCount.Count {
					var skillExists bool
					err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skills WHERE id = $1)", skillWithQuantity.SkillID).Scan(&skillExists)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
						return
					}
					if !skillExists {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Skill with ID " + strconv.Itoa(int(skillWithQuantity.SkillID)) + " does not exist"})
						return
					}
				}
			}
		}

		// Insert multiple manpower count entries
		var skillsResponse []models.PeopleWithSkillTypesResponse
		now := time.Now()

		for _, peopleWithSkillTypes := range bulkRequest.Skills {
			var skillTypesResponse []models.SkillTypeWithCountResponse

			for _, skillTypeWithCount := range peopleWithSkillTypes.SkillTypes {
				var countResponse []models.SkillWithQuantityResponse

				for _, skillWithQuantity := range skillTypeWithCount.Count {
					var id int
					err = db.QueryRow(
						"INSERT INTO manpower_count (project_id, tower_id, people_id, skill_type_id, skill_id, date, count, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id",
						bulkRequest.ProjectID, bulkRequest.TowerID, peopleWithSkillTypes.PeopleID, skillTypeWithCount.SkillTypeID, skillWithQuantity.SkillID, bulkRequest.Date, skillWithQuantity.Quantity, now, now,
					).Scan(&id)

					if err != nil {
						if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
							c.JSON(http.StatusConflict, gin.H{"error": "Manpower count entry already exists for people ID " + strconv.Itoa(int(peopleWithSkillTypes.PeopleID)) + ", skill type ID " + strconv.Itoa(int(skillTypeWithCount.SkillTypeID)) + ", skill ID " + strconv.Itoa(int(skillWithQuantity.SkillID)) + " on this date"})
							return
						}
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create manpower count entry for people ID " + strconv.Itoa(int(peopleWithSkillTypes.PeopleID)) + ", skill type ID " + strconv.Itoa(int(skillTypeWithCount.SkillTypeID)) + ", skill ID " + strconv.Itoa(int(skillWithQuantity.SkillID))})
						return
					}

					// Add to count response array
					countResponse = append(countResponse, models.SkillWithQuantityResponse(skillWithQuantity))
				}

				// Add to skill types response array
				skillTypesResponse = append(skillTypesResponse, models.SkillTypeWithCountResponse{
					SkillTypeID: skillTypeWithCount.SkillTypeID,
					Count:       countResponse,
				})
			}

			// Add to skills response array
			skillsResponse = append(skillsResponse, models.PeopleWithSkillTypesResponse{
				PeopleID:   peopleWithSkillTypes.PeopleID,
				SkillTypes: skillTypesResponse,
			})
		}

		// Create the grouped response data
		bulkData := models.ManpowerCountBulkData{
			ProjectID: bulkRequest.ProjectID,
			TowerID:   bulkRequest.TowerID,
			Date:      bulkRequest.Date,
			Skills:    skillsResponse,
		}

		c.JSON(http.StatusCreated, models.ManpowerCountBulkResponse{
			Success: true,
			Message: "Manpower count entries created successfully",
			Data:    &bulkData,
		})
	}
}

// CreateManpowerCount creates a new manpower count entry
// @Summary Create manpower count
// @Description Create a new manpower count entry
// @Tags ManpowerCount
// @Accept json
// @Produce json
// @Param request body models.ManpowerCount true "Manpower count creation request"
// @Success 201 {object} models.ManpowerCountResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/manpower-count [post]
func CreateManpowerCount(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var manpowerCount models.ManpowerCount
		if err := c.ShouldBindJSON(&manpowerCount); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", manpowerCount.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if people exists
		var peopleExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM people WHERE id = $1)", manpowerCount.PeopleID).Scan(&peopleExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !peopleExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "People does not exist"})
			return
		}

		// Check if skill type exists
		var skillTypeExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skill_types WHERE id = $1)", manpowerCount.SkillTypeID).Scan(&skillTypeExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !skillTypeExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill type does not exist"})
			return
		}

		// Check if skill exists
		var skillExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skills WHERE id = $1)", manpowerCount.SkillID).Scan(&skillExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !skillExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill does not exist"})
			return
		}

		// Check if tower exists (if provided)
		if manpowerCount.TowerID != nil {
			var towerExists bool
			err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM precast WHERE id = $1)", *manpowerCount.TowerID).Scan(&towerExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
			if !towerExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Tower does not exist"})
				return
			}
		}

		// // Check if entry already exists for the same combination
		// var existingID int
		// err = db.QueryRow("SELECT id FROM manpower_count WHERE project_id = $1 AND people_id = $2 AND skill_type_id = $3 AND skill_id = $4 AND date = $5",
		// 	manpowerCount.ProjectID, manpowerCount.PeopleID, manpowerCount.SkillTypeID, manpowerCount.SkillID, manpowerCount.Date).Scan(&existingID)
		// if err == nil {
		// 	c.JSON(http.StatusConflict, gin.H{"error": "Manpower count entry already exists for this combination on this date"})
		// 	return
		// } else if err != sql.ErrNoRows {
		// 	c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		// 	return
		// }

		// Insert new manpower count entry
		var id int
		err = db.QueryRow(
			"INSERT INTO manpower_count (project_id, tower_id, people_id, skill_type_id, skill_id, date, count, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id",
			manpowerCount.ProjectID, manpowerCount.TowerID, manpowerCount.PeopleID, manpowerCount.SkillTypeID, manpowerCount.SkillID, manpowerCount.Date, manpowerCount.Count, time.Now(), time.Now(),
		).Scan(&id)

		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Manpower count entry already exists for this combination on this date"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create manpower count entry"})
			return
		}

		manpowerCount.ID = uint(id)
		c.JSON(http.StatusCreated, models.ManpowerCountResponse{
			Success: true,
			Message: "Manpower count entry created successfully",
			Data:    &manpowerCount,
		})
	}
}

// GetManpowerCounts retrieves all manpower count entries
// @Summary Get all manpower counts
// @Description Retrieve all manpower count entries
// @Tags ManpowerCount
// @Produce json
// @Success 200 {object} models.ManpowerCountListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/manpower-count [get]

func GetManpowerCounts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// ✅ Validate session and get role details
		var userID, roleID int
		var roleName string

		err := db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session"})
			return
		}

		// ✅ Apply role-based access
		var accessCondition string
		var accessArgs []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			accessCondition = "1=1" // No restriction

		case "admin":
			accessCondition = `
				p.client_id IN (
					SELECT ec.id 
					FROM end_client ec
					JOIN client c ON ec.client_id = c.client_id
					WHERE c.user_id = $` + fmt.Sprint(argIndex) + `
				)
			`
			accessArgs = append(accessArgs, userID)
			argIndex++

		default:
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "No permission to view manpower count entries",
			})
			return
		}

		// ✅ Get filter query parameters
		projectID := c.Query("project_id")
		towerID := c.Query("tower_id")
		peopleID := c.Query("people_id")
		departmentID := c.Query("department_id")
		categoryID := c.Query("category_id")
		skillTypeID := c.Query("skill_type_id")
		skillID := c.Query("skill_id")
		date := c.Query("date")
		minCount := c.Query("min_count")
		maxCount := c.Query("max_count")
		searchTerm := c.Query("search")

		/* ---------------- PAGINATION ---------------- */
		pageStr := c.Query("page")
		limitStr := c.Query("page_size")

		usePagination := pageStr != "" || limitStr != ""

		page := 1
		limit := 10

		if usePagination {
			page, err = strconv.Atoi(pageStr)
			if err != nil || page < 1 {
				page = 1
			}
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit < 1 || limit > 100 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		// ✅ Base query
		baseQuery := `
			SELECT 
				mc.id, 
				mc.project_id, 
				p.name as project_name,
				mc.tower_id, 
				t.name as tower_name,
				mc.people_id, 
				pe.name as people_name,
				pe.department_id,
				d.name as department_name,
				pe.category_id,
				c.name as category_name,
				mc.skill_type_id, 
				st.name as skill_type_name,
				mc.skill_id, 
				s.name as skill_name,
				mc.date, 
				mc.count 
			FROM manpower_count mc
			JOIN project p ON mc.project_id = p.project_id
			LEFT JOIN precast t ON mc.tower_id = t.id
			JOIN people pe ON mc.people_id = pe.id
			JOIN departments d ON pe.department_id = d.id
			JOIN categories c ON pe.category_id = c.id
			JOIN skill_types st ON mc.skill_type_id = st.id
			JOIN skills s ON mc.skill_id = s.id
		`

		// ✅ Build dynamic WHERE conditions
		var conditions []string
		var args []interface{}
		argIndex = 1

		// Include access condition
		conditions = append(conditions, accessCondition)
		args = append(args, accessArgs...)

		if projectID != "" {
			conditions = append(conditions, fmt.Sprintf("mc.project_id = $%d", argIndex+len(args)-len(accessArgs)))
			args = append(args, projectID)
		}
		if towerID != "" {
			conditions = append(conditions, fmt.Sprintf("mc.tower_id = $%d", len(args)+1))
			args = append(args, towerID)
		}
		if peopleID != "" {
			conditions = append(conditions, fmt.Sprintf("mc.people_id = $%d", len(args)+1))
			args = append(args, peopleID)
		}
		if departmentID != "" {
			conditions = append(conditions, fmt.Sprintf("pe.department_id = $%d", len(args)+1))
			args = append(args, departmentID)
		}
		if categoryID != "" {
			conditions = append(conditions, fmt.Sprintf("pe.category_id = $%d", len(args)+1))
			args = append(args, categoryID)
		}
		if skillTypeID != "" {
			conditions = append(conditions, fmt.Sprintf("mc.skill_type_id = $%d", len(args)+1))
			args = append(args, skillTypeID)
		}
		if skillID != "" {
			conditions = append(conditions, fmt.Sprintf("mc.skill_id = $%d", len(args)+1))
			args = append(args, skillID)
		}
		if date != "" {
			conditions = append(conditions, fmt.Sprintf("mc.date = $%d", len(args)+1))
			args = append(args, date)
		}
		if minCount != "" {
			conditions = append(conditions, fmt.Sprintf("mc.count >= $%d", len(args)+1))
			args = append(args, minCount)
		}
		if maxCount != "" {
			conditions = append(conditions, fmt.Sprintf("mc.count <= $%d", len(args)+1))
			args = append(args, maxCount)
		}
		if searchTerm != "" {
			searchCondition := fmt.Sprintf(`(
				LOWER(p.name) LIKE LOWER($%d) OR 
				LOWER(t.name) LIKE LOWER($%d) OR 
				LOWER(pe.name) LIKE LOWER($%d) OR 
				LOWER(d.name) LIKE LOWER($%d) OR 
				LOWER(c.name) LIKE LOWER($%d) OR 
				LOWER(st.name) LIKE LOWER($%d) OR 
				LOWER(s.name) LIKE LOWER($%d)
			)`, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1)
			conditions = append(conditions, searchCondition)
			args = append(args, "%"+searchTerm+"%")
		}

		// ✅ Final query with pagination
		query := baseQuery
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		query += " ORDER BY mc.date DESC, mc.id DESC"
		
		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
			args = append(args, limit, offset)
		}

		// ✅ Count query for pagination metadata
		countQuery := `
			SELECT COUNT(*)
			FROM manpower_count mc
			JOIN project p ON mc.project_id = p.project_id
			LEFT JOIN precast t ON mc.tower_id = t.id
			JOIN people pe ON mc.people_id = pe.id
			JOIN departments d ON pe.department_id = d.id
			JOIN categories c ON pe.category_id = c.id
			JOIN skill_types st ON mc.skill_type_id = st.id
			JOIN skills s ON mc.skill_id = s.id
		`
		if len(conditions) > 0 {
			countQuery += " WHERE " + strings.Join(conditions, " AND ")
		}

		var totalCount int
		var countArgs []interface{}
		if usePagination {
			countArgs = args[:len(args)-2] // Remove limit and offset for count
		} else {
			countArgs = args
		}
		err = db.QueryRow(countQuery, countArgs...).Scan(&totalCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get total count", "details": err.Error()})
			return
		}

		// ✅ Fetch data
		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch manpower count entries", "details": err.Error()})
			return
		}
		defer rows.Close()

		var manpowerCountList []models.ManpowerCountWithDetails
		for rows.Next() {
			var manpowerCount models.ManpowerCountWithDetails
			var towerID sql.NullInt64
			var towerName sql.NullString

			if err := rows.Scan(
				&manpowerCount.ID,
				&manpowerCount.ProjectID,
				&manpowerCount.ProjectName,
				&towerID,
				&towerName,
				&manpowerCount.PeopleID,
				&manpowerCount.PeopleName,
				&manpowerCount.DepartmentID,
				&manpowerCount.DepartmentName,
				&manpowerCount.CategoryID,
				&manpowerCount.CategoryName,
				&manpowerCount.SkillTypeID,
				&manpowerCount.SkillTypeName,
				&manpowerCount.SkillID,
				&manpowerCount.SkillName,
				&manpowerCount.Date,
				&manpowerCount.Count,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan manpower count data", "details": err.Error()})
				return
			}

			if towerID.Valid {
				towerIDUint := uint(towerID.Int64)
				manpowerCount.TowerID = &towerIDUint
			}
			if towerName.Valid {
				manpowerCount.TowerName = &towerName.String
			}

			manpowerCountList = append(manpowerCountList, manpowerCount)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating manpower count entries", "details": err.Error()})
			return
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"success": true,
			"message": "Manpower count entries retrieved successfully",
			"data":    manpowerCountList,
		}

		// Only include pagination if pagination parameters were provided
		if usePagination {
			totalPages := (totalCount + limit - 1) / limit
			hasNext := page < totalPages
			hasPrev := page > 1
			response["pagination"] = gin.H{
				"page":         page,
				"limit":        limit,
				"total":        totalCount,
				"total_pages":  totalPages,
				"has_next":     hasNext,
				"has_prev":     hasPrev,
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

// GetManpowerCountsByProject retrieves all manpower count entries for a specific project
// @Summary Get manpower counts by project
// @Description Retrieve all manpower count entries for a specific project
// @Tags ManpowerCount
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {object} models.ManpowerCountListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/projects/{project_id}/manpower-count [get]
func GetManpowerCountsByProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		rows, err := db.Query("SELECT id, project_id, tower_id, people_id, skill_type_id, skill_id, date, count FROM manpower_count WHERE project_id = $1 ORDER BY date DESC, id DESC", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch manpower count entries"})
			return
		}
		defer rows.Close()

		var manpowerCountList []models.ManpowerCount
		for rows.Next() {
			var manpowerCount models.ManpowerCount
			var towerID sql.NullInt64
			if err := rows.Scan(&manpowerCount.ID, &manpowerCount.ProjectID, &towerID, &manpowerCount.PeopleID, &manpowerCount.SkillTypeID, &manpowerCount.SkillID, &manpowerCount.Date, &manpowerCount.Count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan manpower count data"})
				return
			}
			if towerID.Valid {
				towerIDUint := uint(towerID.Int64)
				manpowerCount.TowerID = &towerIDUint
			}
			manpowerCountList = append(manpowerCountList, manpowerCount)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating manpower count entries"})
			return
		}

		c.JSON(http.StatusOK, models.ManpowerCountListResponse{
			Success: true,
			Message: "Manpower count entries retrieved successfully",
			Data:    manpowerCountList,
		})
	}
}

// GetManpowerCountsByDate retrieves all manpower count entries for a specific date
// @Summary Get manpower counts by date
// @Description Retrieve all manpower count entries for a specific date
// @Tags ManpowerCount
// @Produce json
// @Param date path string true "Date (YYYY-MM-DD)"
// @Success 200 {object} models.ManpowerCountListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/manpower-count/date/{date} [get]
func GetManpowerCountsByDate(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		dateStr := c.Param("date")
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format. Use YYYY-MM-DD"})
			return
		}

		rows, err := db.Query("SELECT id, project_id, tower_id, people_id, skill_type_id, skill_id, date, count FROM manpower_count WHERE date = $1 ORDER BY id DESC", date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch manpower count entries"})
			return
		}
		defer rows.Close()

		var manpowerCountList []models.ManpowerCount
		for rows.Next() {
			var manpowerCount models.ManpowerCount
			var towerID sql.NullInt64
			if err := rows.Scan(&manpowerCount.ID, &manpowerCount.ProjectID, &towerID, &manpowerCount.PeopleID, &manpowerCount.SkillTypeID, &manpowerCount.SkillID, &manpowerCount.Date, &manpowerCount.Count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan manpower count data"})
				return
			}
			if towerID.Valid {
				towerIDUint := uint(towerID.Int64)
				manpowerCount.TowerID = &towerIDUint
			}
			manpowerCountList = append(manpowerCountList, manpowerCount)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating manpower count entries"})
			return
		}

		c.JSON(http.StatusOK, models.ManpowerCountListResponse{
			Success: true,
			Message: "Manpower count entries retrieved successfully",
			Data:    manpowerCountList,
		})
	}
}

// GetManpowerCount retrieves a specific manpower count entry by ID
// @Summary Get manpower count by ID
// @Description Retrieve a specific manpower count entry by its ID
// @Tags ManpowerCount
// @Produce json
// @Param id path int true "Manpower Count ID"
// @Success 200 {object} models.ManpowerCountResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/manpower-count/{id} [get]
func GetManpowerCount(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid manpower count ID"})
			return
		}

		var manpowerCount models.ManpowerCount
		var towerID sql.NullInt64
		err = db.QueryRow("SELECT id, project_id, tower_id, people_id, skill_type_id, skill_id, date, count FROM manpower_count WHERE id = $1", id).Scan(&manpowerCount.ID, &manpowerCount.ProjectID, &towerID, &manpowerCount.PeopleID, &manpowerCount.SkillTypeID, &manpowerCount.SkillID, &manpowerCount.Date, &manpowerCount.Count)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Manpower count entry not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch manpower count entry"})
			return
		}

		if towerID.Valid {
			towerIDUint := uint(towerID.Int64)
			manpowerCount.TowerID = &towerIDUint
		}

		c.JSON(http.StatusOK, models.ManpowerCountResponse{
			Success: true,
			Message: "Manpower count entry retrieved successfully",
			Data:    &manpowerCount,
		})
	}
}

// UpdateManpowerCount updates an existing manpower count entry
// @Summary Update manpower count
// @Description Update an existing manpower count entry
// @Tags ManpowerCount
// @Accept json
// @Produce json
// @Param id path int true "Manpower Count ID"
// @Param request body models.ManpowerCount true "Manpower count update request"
// @Success 200 {object} models.ManpowerCountResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/manpower-count/{id} [put]
func UpdateManpowerCount(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid manpower count ID"})
			return
		}

		var manpowerCount models.ManpowerCount
		if err := c.ShouldBindJSON(&manpowerCount); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if manpower count entry exists
		var existingID int
		err = db.QueryRow("SELECT id FROM manpower_count WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Manpower count entry not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", manpowerCount.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if people exists
		var peopleExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM people WHERE id = $1)", manpowerCount.PeopleID).Scan(&peopleExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !peopleExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "People does not exist"})
			return
		}

		// Check if skill type exists
		var skillTypeExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skill_types WHERE id = $1)", manpowerCount.SkillTypeID).Scan(&skillTypeExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !skillTypeExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill type does not exist"})
			return
		}

		// Check if skill exists
		var skillExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM skills WHERE id = $1)", manpowerCount.SkillID).Scan(&skillExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !skillExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill does not exist"})
			return
		}

		// Check if tower exists (if provided)
		if manpowerCount.TowerID != nil {
			var towerExists bool
			err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM precast WHERE id = $1)", *manpowerCount.TowerID).Scan(&towerExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
			if !towerExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Tower does not exist"})
				return
			}
		}

		// Check if another entry with same combination exists
		var conflictingID int
		err = db.QueryRow("SELECT id FROM manpower_count WHERE project_id = $1 AND people_id = $2 AND skill_type_id = $3 AND skill_id = $4 AND date = $5 AND id != $6",
			manpowerCount.ProjectID, manpowerCount.PeopleID, manpowerCount.SkillTypeID, manpowerCount.SkillID, manpowerCount.Date, id).Scan(&conflictingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Manpower count entry already exists for this combination on this date"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Update manpower count entry
		result, err := db.Exec(
			"UPDATE manpower_count SET project_id = $1, tower_id = $2, people_id = $3, skill_type_id = $4, skill_id = $5, date = $6, count = $7, updated_at = $8 WHERE id = $9",
			manpowerCount.ProjectID, manpowerCount.TowerID, manpowerCount.PeopleID, manpowerCount.SkillTypeID, manpowerCount.SkillID, manpowerCount.Date, manpowerCount.Count, time.Now(), id,
		)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Manpower count entry already exists for this combination on this date"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update manpower count entry"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check update result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Manpower count entry not found"})
			return
		}

		manpowerCount.ID = uint(id)
		c.JSON(http.StatusOK, models.ManpowerCountResponse{
			Success: true,
			Message: "Manpower count entry updated successfully",
			Data:    &manpowerCount,
		})
	}
}

// DeleteManpowerCount deletes a manpower count entry
// @Summary Delete manpower count
// @Description Delete a manpower count entry by its ID
// @Tags ManpowerCount
// @Produce json
// @Param id path int true "Manpower Count ID"
// @Success 200 {object} models.ManpowerCountResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/manpower-count/{id} [delete]
func DeleteManpowerCount(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid manpower count ID"})
			return
		}

		// Check if manpower count entry exists
		var existingID int
		err = db.QueryRow("SELECT id FROM manpower_count WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Manpower count entry not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Delete manpower count entry
		result, err := db.Exec("DELETE FROM manpower_count WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete manpower count entry"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check delete result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Manpower count entry not found"})
			return
		}

		c.JSON(http.StatusOK, models.ManpowerCountResponse{
			Success: true,
			Message: "Manpower count entry deleted successfully",
		})
	}
}

// GetManpowerCountDashboard godoc
// @Summary      Get manpower count dashboard
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower-count/dashboard [get]
func GetManpowerCountDashboard(db *sql.DB) gin.HandlerFunc {
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

		// Get user details for role-based access
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

		// Build project filter based on role and specific project
		var projectFilterQuery string
		var args []interface{}

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
                SELECT ec.id
                FROM end_client ec
                JOIN client cl ON ec.client_id = cl.client_id
                WHERE cl.user_id = $1
            )
            AND p.suspend = false
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

		// Get date parameters - default to monthly for current month
		summaryType := c.DefaultQuery("type", "currentMonth")
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

				skillCounts := fetchManpowerCountsForRange(db, projectFilterQuery, args, start, end)
				entry := gin.H{"name": start.Month().String()}
				for skillName, count := range skillCounts {
					entry[skillName] = count
				}
				monthlyData = append(monthlyData, entry)
			}
			c.JSON(http.StatusOK, monthlyData)

		case "monthly":
			// Default to current year and month if not provided
			now := time.Now()
			year := now.Year()
			month := int(now.Month())

			if yearStr != "" {
				if y, err := strconv.Atoi(yearStr); err == nil {
					year = y
				}
			}
			if monthStr != "" {
				if m, err := strconv.Atoi(monthStr); err == nil {
					month = m
				}
			}

			// Start of the month
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			daysInMonth := start.AddDate(0, 1, -1).Day()

			rangeData := make([]map[string]interface{}, 0)

			// Iterate in 5-day ranges
			for day := 1; day <= daysInMonth; day += 5 {
				startRange := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endRange := startRange.AddDate(0, 0, 4)

				// Ensure we don’t go past the month’s last day
				if endRange.Day() > daysInMonth {
					endRange = time.Date(year, time.Month(month), daysInMonth, 23, 59, 59, 0, location)
				}

				// Fetch manpower counts for the range
				skillCounts := fetchManpowerCountsForRange(db, projectFilterQuery, args, startRange, endRange)

				// Prepare entry for response
				entry := gin.H{
					"name": fmt.Sprintf("%s to %s", startRange.Format(layout), endRange.Format(layout)),
				}
				for skillName, count := range skillCounts {
					entry[skillName] = count
				}

				rangeData = append(rangeData, entry)
			}

			// Return aggregated result
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
				skillCounts := fetchManpowerCountsForRange(db, projectFilterQuery, args, currentDate, currentDate)
				entry := gin.H{"name": currentDate.Format(layout)}
				for skillName, count := range skillCounts {
					entry[skillName] = count
				}
				weekData = append(weekData, entry)
			}
			c.JSON(http.StatusOK, weekData)

		case "currentMonth":
			layout := "2006-01-02"
			rangeData := make([]map[string]interface{}, 0)

			now := time.Now()
			// First day of current month
			firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			// Last day of current month
			lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
			daysInMonth := lastOfMonth.Day()

			// Loop through each day of current month
			for day := 1; day <= daysInMonth; day++ {
				currentDate := time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location())

				// Fetch manpower counts for this day
				skillCounts := fetchManpowerCountsForRange(db, projectFilterQuery, args, currentDate, currentDate)

				// Prepare entry
				entry := gin.H{
					"name": currentDate.Format(layout),
					"day":  currentDate.Weekday().String(), // e.g. Monday
				}
				for skillName, count := range skillCounts {
					entry[skillName] = count
				}

				rangeData = append(rangeData, entry)
			}

			c.JSON(http.StatusOK, rangeData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
		}

		// Log activity
		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// fetchManpowerCountsForRange fetches manpower counts for a specific date range
func fetchManpowerCountsForRange(db *sql.DB, projectFilterQuery string, args []interface{}, start, end time.Time) map[string]int {
	// Add date range to args
	dateArgs := append(args, start, end)

	query := fmt.Sprintf(`
		SELECT 
			s.name as skill_name,
			st.name as skill_type_name,
			COALESCE(COUNT(mc.id), 0) as total_count,
			COALESCE(SUM(COALESCE(mc.count, 0)), 0) as total_manpower
		FROM skills s
		JOIN skill_types st ON s.skill_type_id = st.id
		LEFT JOIN (
			SELECT mc.skill_id, mc.count, mc.id
			FROM manpower_count mc
			JOIN project p ON mc.project_id = p.project_id
			%s AND mc.date BETWEEN $%d AND $%d
		) mc ON s.id = mc.skill_id
		GROUP BY s.id, s.name, st.id, st.name
		ORDER BY st.name, s.name
	`, projectFilterQuery, len(args)+1, len(args)+2)

	rows, err := db.Query(query, dateArgs...)
	if err != nil {
		fmt.Printf("Error fetching manpower counts: %v\n", err)
		return map[string]int{}
	}
	defer rows.Close()

	skillCounts := make(map[string]int)

	// Always build the skills map with all skills
	for rows.Next() {
		var skillName, skillTypeName string
		var totalCount, totalManpower int

		if err := rows.Scan(&skillName, &skillTypeName, &totalCount, &totalManpower); err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}

		// Map skill name to manpower count (including 0)
		skillCounts[skillName] = totalManpower
	}

	return skillCounts
}

// GetManpowerDashboard godoc
// @Summary      Get manpower dashboard
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower/dashboard [get]
func GetManpowerDashboard(db *sql.DB) gin.HandlerFunc {
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

		// Get user details for role-based access
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

		// --- Project filter ---
		projectIDStr := c.Query("project_id")
		var projectFilterQuery string
		var args []interface{}
		argIndex := 1

		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, projectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default:
				projectFilterQuery = `
					WHERE p.project_id IN (
						SELECT pm.project_id 
						FROM project_members pm
						WHERE pm.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			}
		}

		// --- Date filter ---
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		now := time.Now()
		location := now.Location()

		var startDate, endDate time.Time
		switch summaryType {
		case "yearly":
			if yearStr == "" {
				yearStr = strconv.Itoa(now.Year())
			}
			year, _ := strconv.Atoi(yearStr)
			startDate = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			endDate = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if yearStr == "" || monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year or month"})
				return
			}
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			startDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			endDate = startDate.AddDate(0, 1, -1)

		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			endDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}
			startDate = endDate.AddDate(0, 0, -6) // past 7 rolling days

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
			return
		}

		// --- Add date filter to queries ---
		argsWithDates := append(args, startDate, endDate)
		dateFilter := " AND mc.date BETWEEN $" + strconv.Itoa(len(args)+1) + " AND $" + strconv.Itoa(len(args)+2)

		// ---- Top Cards ----
		var totalManpower, totalVendors, totalSkills int

		query := `
	SELECT COALESCE(SUM(mc.count), 0)
	FROM manpower_count mc
	JOIN project p ON p.project_id = mc.project_id
`

		// Append dynamic filters safely
		if projectFilterQuery != "" {
			query += " " + projectFilterQuery + " "
		}
		if dateFilter != "" {
			query += " " + dateFilter + " "
		}

		log.Println("Final manpower query:", query, argsWithDates)

		err = db.QueryRow(query, argsWithDates...).Scan(&totalManpower)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch total manpower",
				"details": err.Error(),
			})
			return
		}

		err = db.QueryRow(`
			SELECT COUNT(DISTINCT people_id)
			FROM manpower_count mc
			JOIN project p ON p.project_id = mc.project_id
			`+projectFilterQuery+dateFilter, argsWithDates...).Scan(&totalVendors)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch total vendors"})
			return
		}

		err = db.QueryRow(`
			SELECT COUNT(DISTINCT skill_id)
			FROM manpower_count mc
			JOIN project p ON p.project_id = mc.project_id
			`+projectFilterQuery+dateFilter, argsWithDates...).Scan(&totalSkills)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch total skills"})
			return
		}

		// ---- Final Response ----
		c.JSON(http.StatusOK, gin.H{
			"total_manpower": totalManpower,
			"total_vendors":  totalVendors,
			"total_skills":   totalSkills,
			"start_date":     startDate.Format("2006-01-02"),
			"end_date":       endDate.Format("2006-01-02"),
			"type":           summaryType,
		})

		// ---- Log activity ----
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetTotalSkillTypeCount godoc
// @Summary      Get total skill type count (manpower skills dashboard)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower/skills/dashboard [get]
func GetTotalSkillTypeCount(db *sql.DB) gin.HandlerFunc {
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

		// --- Role-based Access ---
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

		// --- Project filter ---
		projectIDStr := c.Query("project_id")
		var projectFilterQuery string
		var args []interface{}
		argIndex := 1

		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, projectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default:
				projectFilterQuery = `WHERE p.project_id IN (
					SELECT pm.project_id FROM project_members pm
					JOIN project p2 ON pm.project_id = p2.project_id
					WHERE pm.user_id = $1 AND p2.suspend = false
				)`
				args = append(args, userID)
			}
		}

		// --- Get all skills list ---
		allSkills := []string{}
		skillRows, err := db.Query("SELECT name FROM skills ORDER BY id")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skills"})
			return
		}
		defer skillRows.Close()
		for skillRows.Next() {
			var s string
			if err := skillRows.Scan(&s); err == nil {
				allSkills = append(allSkills, s)
			}
		}

		// --- Date inputs ---
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		now := time.Now()
		location := now.Location()
		if summaryType == "yearly" && yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}

		// --- Fetch helper ---
		fetchCounts := func(start, end time.Time) (map[time.Time]map[string]int, error) {
			query := `
				SELECT mc.date::DATE, s.name, SUM(mc.count) AS labour_count
				FROM manpower_count mc
				JOIN project p ON p.project_id = mc.project_id
				JOIN skills s ON s.id = mc.skill_id
				` + projectFilterQuery + `
				AND mc.date BETWEEN $` + strconv.Itoa(len(args)+1) + ` AND $` + strconv.Itoa(len(args)+2) + `
				GROUP BY mc.date::DATE, s.name
				ORDER BY mc.date
			`
			rows, err := db.Query(query, append(args, start, end)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			data := make(map[time.Time]map[string]int)
			for rows.Next() {
				var d time.Time
				var skill string
				var count int
				if err := rows.Scan(&d, &skill, &count); err != nil {
					return nil, err
				}
				if data[d] == nil {
					data[d] = make(map[string]int)
				}
				data[d][skill] += count
			}
			return data, nil
		}

		// --- Example: Yearly summary ---
		if summaryType == "yearly" {
			year, _ := strconv.Atoi(yearStr)
			start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end := time.Date(year, 12, 31, 23, 59, 59, 0, location)

			allData, err := fetchCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
				return
			}

			monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
			yearData := []map[string]interface{}{}
			for m := 1; m <= 12; m++ {
				if year == now.Year() && m > int(now.Month()) {
					break
				}
				startM := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				endM := startM.AddDate(0, 1, -1)

				// initialize row with all skills = 0
				row := map[string]interface{}{"name": monthNames[m-1]}
				for _, s := range allSkills {
					row[s] = 0
				}

				// fill actual data
				for d, skills := range allData {
					if !d.Before(startM) && !d.After(endM) {
						for skill, count := range skills {
							row[skill] = row[skill].(int) + count
						}
					}
				}
				yearData = append(yearData, row)
			}
			c.JSON(http.StatusOK, yearData)
			return
		}

		switch summaryType {
		case "weekly":
			if yearStr == "" || monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}

			weekStart := startDate.AddDate(0, 0, -6)
			allData, err := fetchCounts(weekStart, startDate)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
				return
			}

			weekData := []map[string]interface{}{}
			for i := 0; i < 7; i++ {
				current := weekStart.AddDate(0, 0, i)

				// initialize row with all skills = 0
				row := map[string]interface{}{"name": current.Format("2006-01-02")}
				for _, s := range allSkills {
					row[s] = 0
				}

				// fill actual data
				for skill, count := range allData[current] {
					row[skill] = row[skill].(int) + count
				}

				weekData = append(weekData, row)
			}
			c.JSON(http.StatusOK, weekData)

		case "monthly":
			year, _ := strconv.Atoi(yearStr)
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end := start.AddDate(0, 1, -1)

			allData, err := fetchCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
				return
			}

			rangeData := []map[string]interface{}{}
			daysInMonth := end.Day()
			for day := 1; day <= daysInMonth; day += 5 {
				startR := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endR := startR.AddDate(0, 0, 4)
				if endR.After(end) {
					endR = end
				}

				// initialize row with all skills = 0
				row := map[string]interface{}{
					"name": fmt.Sprintf("%s to %s", startR.Format("2006-01-02"), endR.Format("2006-01-02")),
				}
				for _, s := range allSkills {
					row[s] = 0
				}

				// fill actual data
				for d, skills := range allData {
					if !d.Before(startR) && !d.After(endR) {
						for skill, count := range skills {
							row[skill] = row[skill].(int) + count
						}
					}
				}
				rangeData = append(rangeData, row)
			}
			c.JSON(http.StatusOK, rangeData)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
			return

		}

		// Log activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate vendor count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetTotalSkillTypeTypeCount godoc
// @Summary      Get total skill type type count (manpower dashboard)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower/skill_type/dashboard [get]
func GetTotalSkillTypeTypeCount(db *sql.DB) gin.HandlerFunc {
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

		// --- Role-based Access ---
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

		// --- Project filter ---
		projectIDStr := c.Query("project_id")
		var projectFilterQuery string
		var args []interface{}
		argIndex := 1

		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id parameter"})
				return
			}
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, projectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default:
				projectFilterQuery = `WHERE p.project_id IN (
					SELECT pm.project_id FROM project_members pm
					JOIN project p2 ON pm.project_id = p2.project_id
					WHERE pm.user_id = $1 AND p2.suspend = false
				)`
				args = append(args, userID)
			}
		}

		// --- Date inputs ---
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")
		now := time.Now()
		location := now.Location()
		if summaryType == "yearly" && yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}

		// --- Check for skill_type_id ---
		skillTypeIDStr := c.Query("skill_type_id")
		if skillTypeIDStr != "" {
			// -------------------- Mode 2: Group by skills of given skill_type --------------------
			skillTypeID, err := strconv.Atoi(skillTypeIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skill_type_id"})
				return
			}

			// get all skills under that skill_type
			allSkills := []string{}
			rows, err := db.Query("SELECT name FROM skills WHERE skill_type_id = $1 ORDER BY id", skillTypeID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skills"})
				return
			}
			defer rows.Close()
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err == nil {
					allSkills = append(allSkills, s)
				}
			}

			// fetch helper for skills
			fetchSkillCounts := func(start, end time.Time) (map[time.Time]map[string]int, error) {
				query := `
					SELECT mc.date::DATE, s.name, SUM(mc.count) AS labour_count
					FROM manpower_count mc
					JOIN project p ON p.project_id = mc.project_id
					JOIN skills s ON s.id = mc.skill_id
					` + projectFilterQuery + `
					AND s.skill_type_id = $` + strconv.Itoa(len(args)+1) + `
					AND mc.date BETWEEN $` + strconv.Itoa(len(args)+2) + ` AND $` + strconv.Itoa(len(args)+3) + `
					GROUP BY mc.date::DATE, s.name
					ORDER BY mc.date
				`
				rows, err := db.Query(query, append(args, skillTypeID, start, end)...)
				if err != nil {
					return nil, err
				}
				defer rows.Close()

				data := make(map[time.Time]map[string]int)
				for rows.Next() {
					var d time.Time
					var skill string
					var count int
					if err := rows.Scan(&d, &skill, &count); err != nil {
						return nil, err
					}
					if data[d] == nil {
						data[d] = make(map[string]int)
					}
					data[d][skill] += count
				}
				return data, nil
			}

			// --- summaryType logic (reuse from above, but use allSkills instead of allSkillTypes) ---
			returnSkillSummary(c, summaryType, yearStr, monthStr, dayStr, now, location, allSkills, fetchSkillCounts)
			return
		}

		// -------------------- Mode 1: Default (group by skill types) --------------------
		allSkillTypes := []string{}
		skillTypeRows, err := db.Query("SELECT name FROM skill_types ORDER BY id")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skill types"})
			return
		}
		defer skillTypeRows.Close()
		for skillTypeRows.Next() {
			var s string
			if err := skillTypeRows.Scan(&s); err == nil {
				allSkillTypes = append(allSkillTypes, s)
			}
		}

		// fetch helper for skill types
		fetchCounts := func(start, end time.Time) (map[time.Time]map[string]int, error) {
			query := `
				SELECT mc.date::DATE, st.name, SUM(mc.count) AS labour_count
				FROM manpower_count mc
				JOIN project p ON p.project_id = mc.project_id
				JOIN skills s ON s.id = mc.skill_id
				JOIN skill_types st ON st.id = s.skill_type_id
				` + projectFilterQuery + `
				AND mc.date BETWEEN $` + strconv.Itoa(len(args)+1) + ` AND $` + strconv.Itoa(len(args)+2) + `
				GROUP BY mc.date::DATE, st.name
				ORDER BY mc.date
			`
			rows, err := db.Query(query, append(args, start, end)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			data := make(map[time.Time]map[string]int)
			for rows.Next() {
				var d time.Time
				var skillType string
				var count int
				if err := rows.Scan(&d, &skillType, &count); err != nil {
					return nil, err
				}
				if data[d] == nil {
					data[d] = make(map[string]int)
				}
				data[d][skillType] += count
			}
			return data, nil
		}

		// summaryType logic (default)
		returnSkillSummary(c, summaryType, yearStr, monthStr, dayStr, now, location, allSkillTypes, fetchCounts)

		// log
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate vendor count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

func returnSkillSummary(
	c *gin.Context,
	summaryType, yearStr, monthStr, dayStr string,
	now time.Time,
	location *time.Location,
	allLabels []string,
	fetch func(start, end time.Time) (map[time.Time]map[string]int, error),
) {
	// --- Yearly ---
	if summaryType == "yearly" {
		year, _ := strconv.Atoi(yearStr)
		start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
		end := time.Date(year, 12, 31, 23, 59, 59, 0, location)

		allData, err := fetch(start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
			return
		}

		monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
		yearData := []map[string]interface{}{}
		for m := 1; m <= 12; m++ {
			if year == now.Year() && m > int(now.Month()) {
				break
			}
			startM := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
			endM := startM.AddDate(0, 1, -1)

			row := map[string]interface{}{"name": monthNames[m-1]}
			for _, s := range allLabels {
				row[s] = 0
			}
			for d, labels := range allData {
				if !d.Before(startM) && !d.After(endM) {
					for label, count := range labels {
						row[label] = row[label].(int) + count
					}
				}
			}
			yearData = append(yearData, row)
		}
		c.JSON(http.StatusOK, yearData)
		return
	}

	// --- Weekly ---
	if summaryType == "weekly" {
		if yearStr == "" || monthStr == "" || dayStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing year, month, or date"})
			return
		}
		startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
		startDate, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
			return
		}

		weekStart := startDate.AddDate(0, 0, -6)
		allData, err := fetch(weekStart, startDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
			return
		}

		weekData := []map[string]interface{}{}
		for i := 0; i < 7; i++ {
			current := weekStart.AddDate(0, 0, i)
			row := map[string]interface{}{"name": current.Format("2006-01-02")}
			for _, s := range allLabels {
				row[s] = 0
			}
			for label, count := range allData[current] {
				row[label] = row[label].(int) + count
			}
			weekData = append(weekData, row)
		}
		c.JSON(http.StatusOK, weekData)
		return
	}

	// --- Monthly ---
	if summaryType == "monthly" {
		year, _ := strconv.Atoi(yearStr)
		month, _ := strconv.Atoi(monthStr)
		start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
		end := start.AddDate(0, 1, -1)

		allData, err := fetch(start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching data", "details": err.Error()})
			return
		}

		rangeData := []map[string]interface{}{}
		daysInMonth := end.Day()
		for day := 1; day <= daysInMonth; day += 5 {
			startR := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
			endR := startR.AddDate(0, 0, 4)
			if endR.After(end) {
				endR = end
			}

			row := map[string]interface{}{
				"name": fmt.Sprintf("%s to %s", startR.Format("2006-01-02"), endR.Format("2006-01-02")),
			}
			for _, s := range allLabels {
				row[s] = 0
			}
			for d, labels := range allData {
				if !d.Before(startR) && !d.After(endR) {
					for label, count := range labels {
						row[label] = row[label].(int) + count
					}
				}
			}
			rangeData = append(rangeData, row)
		}
		c.JSON(http.StatusOK, rangeData)
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
}

// GetVendorCount godoc
// @Summary      Get vendor count (manpower dashboard)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower/vendor/dashboard [get]
func GetVendorCount(db *sql.DB) gin.HandlerFunc {
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

		// get user id
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		// ---- Project filter ----
		projectIDStr := c.Query("project_id")
		var args []interface{}
		argIndex := 1
		var projectFilterQuery string
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
				return
			}
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, projectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default:
				projectFilterQuery = `WHERE p.project_id IN (
					SELECT pm.project_id FROM project_members pm
					JOIN project p2 ON pm.project_id = p2.project_id
					WHERE pm.user_id = $1 AND p2.suspend = false
				)`
				args = append(args, userID)
			}
		}

		// ---- Get all vendors (to always include with 0) ----
		allVendors := []string{}
		rows, err := db.Query("SELECT DISTINCT name FROM people ORDER BY name")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching vendors"})
			return
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err == nil {
				allVendors = append(allVendors, v)
			}
		}
		rows.Close()

		// ---- Date inputs ----
		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		now := time.Now()
		location := now.Location()
		if yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}
		year, _ := strconv.Atoi(yearStr)

		// ---- Fetch helper ----
		fetchAllCounts := func(start, end time.Time) (map[time.Time]map[string]int, error) {
			query := `
				SELECT mc.date, pe.name, SUM(mc.count) 
				FROM manpower_count mc
				JOIN people pe ON mc.people_id = pe.id
				JOIN project p ON p.project_id = mc.project_id
				` + projectFilterQuery + `
				AND mc.date BETWEEN $` + strconv.Itoa(len(args)+1) + ` AND $` + strconv.Itoa(len(args)+2) + `
				GROUP BY mc.date, pe.name
			`
			rows, err := db.Query(query, append(args, start, end)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			data := make(map[time.Time]map[string]int)
			for rows.Next() {
				var d time.Time
				var vendor string
				var cnt int
				if err := rows.Scan(&d, &vendor, &cnt); err != nil {
					return nil, err
				}
				if _, ok := data[d]; !ok {
					data[d] = make(map[string]int)
				}
				data[d][vendor] = cnt
			}
			return data, nil
		}

		// ---- Response ----
		response := []map[string]interface{}{}

		switch summaryType {
		case "yearly":
			start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end := time.Date(year, 12, 31, 23, 59, 59, 0, location)
			allData, err := fetchAllCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			for m := 1; m <= 12; m++ {
				if year == now.Year() && m > int(now.Month()) {
					break
				}
				startM := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				endM := startM.AddDate(0, 1, -1)

				// init record
				rec := map[string]interface{}{}
				for _, v := range allVendors {
					rec[v] = 0
				}
				rec["name"] = startM.Month().String()
				rec["other"] = 0

				// accumulate
				for d, vendorCounts := range allData {
					if !d.Before(startM) && !d.After(endM) {
						for v, cnt := range vendorCounts {
							if _, ok := rec[v]; ok {
								rec[v] = rec[v].(int) + cnt
							} else {
								rec["other"] = rec["other"].(int) + cnt
							}
						}
					}
				}
				response = append(response, rec)
			}

		case "monthly":
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end := start.AddDate(0, 1, -1)
			allData, err := fetchAllCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			daysInMonth := end.Day()
			for day := 1; day <= daysInMonth; day += 5 {
				startR := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endR := startR.AddDate(0, 0, 4)
				if endR.After(end) {
					endR = end
				}
				rec := map[string]interface{}{}
				for _, v := range allVendors {
					rec[v] = 0
				}
				rec["name"] = fmt.Sprintf("%s to %s", startR.Format("2006-01-02"), endR.Format("2006-01-02"))
				rec["other"] = 0

				for d, vendorCounts := range allData {
					if !d.Before(startR) && !d.After(endR) {
						for v, cnt := range vendorCounts {
							if _, ok := rec[v]; ok {
								rec[v] = rec[v].(int) + cnt
							} else {
								rec["other"] = rec["other"].(int) + cnt
							}
						}
					}
				}
				response = append(response, rec)
			}

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing month or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}
			weekStart := startDate.AddDate(0, 0, -6)
			allData, err := fetchAllCounts(weekStart, startDate)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			for i := 0; i < 7; i++ {
				current := weekStart.AddDate(0, 0, i)
				rec := map[string]interface{}{}
				for _, v := range allVendors {
					rec[v] = 0
				}
				rec["name"] = current.Format("2006-01-02")
				rec["other"] = 0

				if vendorCounts, ok := allData[current]; ok {
					for v, cnt := range vendorCounts {
						if _, ok2 := rec[v]; ok2 {
							rec[v] = rec[v].(int) + cnt
						} else {
							rec["other"] = rec["other"].(int) + cnt
						}
					}
				}
				response = append(response, rec)
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
			return
		}

		c.JSON(http.StatusOK, response)

		// log
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate vendor count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetVendorShare godoc
// @Summary      Get vendor share (manpower dashboard)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower/share/dashboard [get]
func GetVendorShare(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ---- Validate session ----
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

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		// ---- Project filter ----
		projectIDStr := c.Query("project_id")
		var args []interface{}
		argIndex := 1
		var projectFilterQuery string
		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
				return
			}
			projectFilterQuery = "WHERE p.project_id = $1 AND p.suspend = false"
			args = append(args, projectID)
		} else {
			switch roleName {
			case "superadmin":
				projectFilterQuery = "WHERE p.suspend = false"
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default:
				projectFilterQuery = `WHERE p.project_id IN (
					SELECT pm.project_id FROM project_members pm
					JOIN project p2 ON pm.project_id = p2.project_id
					WHERE pm.user_id = $1 AND p2.suspend = false
				)`
				args = append(args, userID)
			}
		}

		// ---- All vendors (for zero-fill consistency) ----
		allVendors := []string{}
		rows, err := db.Query("SELECT DISTINCT name FROM people ORDER BY name")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed fetching vendors"})
			return
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err == nil {
				allVendors = append(allVendors, v)
			}
		}
		rows.Close()

		// ---- Date inputs ----
		summaryType := c.DefaultQuery("type", "yearly") // daily, weekly, monthly, yearly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		now := time.Now()
		location := now.Location()
		if yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}
		year, _ := strconv.Atoi(yearStr)

		// ---- Helper: Fetch counts ----
		fetchAllCounts := func(start, end time.Time) (map[time.Time]map[string]int, error) {
			query := `
				SELECT mc.date, pe.name, SUM(mc.count) 
				FROM manpower_count mc
				JOIN people pe ON mc.people_id = pe.id
				JOIN project p ON p.project_id = mc.project_id
				` + projectFilterQuery + `
				AND mc.date BETWEEN $` + strconv.Itoa(len(args)+1) + ` AND $` + strconv.Itoa(len(args)+2) + `
				GROUP BY mc.date, pe.name
			`
			rows, err := db.Query(query, append(args, start, end)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			data := make(map[time.Time]map[string]int)
			for rows.Next() {
				var d time.Time
				var vendor string
				var cnt int
				if err := rows.Scan(&d, &vendor, &cnt); err != nil {
					return nil, err
				}
				if _, ok := data[d]; !ok {
					data[d] = make(map[string]int)
				}
				data[d][vendor] = cnt
			}
			return data, nil
		}

		// ---- Response ----
		response := []map[string]interface{}{}

		switch summaryType {
		case "daily":
			// default: last 7 days
			end := now
			start := now.AddDate(0, 0, -6)
			allData, err := fetchAllCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			for i := 0; i < 7; i++ {
				day := start.AddDate(0, 0, i)
				rec := map[string]interface{}{"name": day.Format("2006-01-02")}
				total := 0

				// init vendors
				for _, v := range allVendors {
					rec[v] = 0.0
				}

				// count
				if vendorCounts, ok := allData[day]; ok {
					for v, cnt := range vendorCounts {
						if _, ok2 := rec[v]; ok2 {
							rec[v] = float64(cnt)
						}
						total += cnt
					}
				}

				// compute %
				if total > 0 {
					for _, v := range allVendors {
						val := rec[v].(float64)
						share := (val / float64(total)) * 100
						rec[v] = math.Round(share*100) / 100
					}
				}
				response = append(response, rec)
			}

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Missing month or date"})
				return
			}
			startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
				return
			}
			weekStart := startDate.AddDate(0, 0, -6)
			allData, err := fetchAllCounts(weekStart, startDate)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			for i := 0; i < 7; i++ {
				current := weekStart.AddDate(0, 0, i)
				rec := map[string]interface{}{"name": current.Format("2006-01-02")}
				total := 0

				for _, v := range allVendors {
					rec[v] = 0.0
				}
				if vendorCounts, ok := allData[current]; ok {
					for v, cnt := range vendorCounts {
						if _, ok2 := rec[v]; ok2 {
							rec[v] = float64(cnt)
						}
						total += cnt
					}
				}
				if total > 0 {
					for _, v := range allVendors {
						val := rec[v].(float64)
						rec[v] = math.Round((val/float64(total))*100*100) / 100
					}
				}
				response = append(response, rec)
			}

		case "monthly":
			month, _ := strconv.Atoi(monthStr)
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end := start.AddDate(0, 1, -1)
			allData, err := fetchAllCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			daysInMonth := end.Day()
			for day := 1; day <= daysInMonth; day += 5 {
				startR := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
				endR := startR.AddDate(0, 0, 4)
				if endR.After(end) {
					endR = end
				}
				rec := map[string]interface{}{"name": fmt.Sprintf("%s to %s", startR.Format("2006-01-02"), endR.Format("2006-01-02"))}
				total := 0

				for _, v := range allVendors {
					rec[v] = 0.0
				}
				for d, vendorCounts := range allData {
					if !d.Before(startR) && !d.After(endR) {
						for v, cnt := range vendorCounts {
							if _, ok := rec[v]; ok {
								rec[v] = rec[v].(float64) + float64(cnt)
							}
							total += cnt
						}
					}
				}
				if total > 0 {
					for _, v := range allVendors {
						val := rec[v].(float64)
						rec[v] = math.Round((val/float64(total))*100*100) / 100
					}
				}
				response = append(response, rec)
			}

		case "yearly":
			start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end := time.Date(year, 12, 31, 23, 59, 59, 0, location)
			allData, err := fetchAllCounts(start, end)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
				return
			}

			for m := 1; m <= 12; m++ {
				if year == now.Year() && m > int(now.Month()) {
					break
				}
				startM := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
				endM := startM.AddDate(0, 1, -1)
				rec := map[string]interface{}{"name": startM.Month().String()}
				total := 0

				for _, v := range allVendors {
					rec[v] = 0.0
				}
				for d, vendorCounts := range allData {
					if !d.Before(startM) && !d.After(endM) {
						for v, cnt := range vendorCounts {
							if _, ok := rec[v]; ok {
								rec[v] = rec[v].(float64) + float64(cnt)
							}
							total += cnt
						}
					}
				}
				if total > 0 {
					for _, v := range allVendors {
						val := rec[v].(float64)
						rec[v] = math.Round((val/float64(total))*100*100) / 100
					}
				}
				response = append(response, rec)
			}

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
			return
		}

		c.JSON(http.StatusOK, response)

		// log activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower %share dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetProjectManpowerHandler godoc
// @Summary      Get manpower project summary
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary [get]
func GetProjectManpowerHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		projectID := c.Query("project_id")
		summaryType := c.DefaultQuery("type", "yearly") // daily, weekly, monthly, yearly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		type ProjectSummary struct {
			ProjectID   int    `json:"project_id"`
			ProjectName string `json:"name"`
			TotalCount  int    `json:"total_count"`
		}
		var summaries []ProjectSummary

		// --------------------------
		// build project query filter
		// --------------------------
		var rows *sql.Rows
		var args []interface{}
		argIndex := 1

		if projectID != "" {
			rows, err = db.Query(`SELECT p.project_id, p.name 
				FROM project p 
				WHERE p.project_id = $1 AND p.suspend = false`, projectID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project", "details": err.Error()})
				return
			}
		} else {
			var projectFilterQuery string
			switch roleName {
			case "superadmin":
				projectFilterQuery = `WHERE p.suspend = false`
			case "admin":
				projectFilterQuery = `
					WHERE p.client_id IN (
						SELECT ec.id
						FROM end_client ec
						JOIN client cl ON ec.client_id = cl.client_id
						WHERE cl.user_id = $` + strconv.Itoa(argIndex) + `
					) AND p.suspend = false
				`
				args = append(args, userID)
				argIndex++
			default: // member or normal user
				projectFilterQuery = `WHERE p.project_id IN (
						SELECT pm.project_id 
						FROM project_members pm
						JOIN project p2 ON pm.project_id = p2.project_id
						WHERE pm.user_id = $1 AND p2.suspend = false
					)`
				args = append(args, userID)
			}

			query := fmt.Sprintf(`SELECT p.project_id, p.name FROM project p %s`, projectFilterQuery)
			rows, err = db.Query(query, args...)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects", "details": err.Error()})
				return
			}
		}
		defer rows.Close()

		projects := []struct {
			ID   int
			Name string
		}{}
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Scan failed", "details": err.Error()})
				return
			}
			projects = append(projects, struct {
				ID   int
				Name string
			}{id, name})
		}

		// --------------------------
		// iterate projects + counts
		// --------------------------
		for _, proj := range projects {
			var totalCount int

			switch summaryType {
			case "yearly":
				start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
				end := time.Date(year, 12, 31, 23, 59, 59, 0, location)
				err = db.QueryRow(`
					SELECT COALESCE(SUM(count), 0) 
					FROM manpower_count
					WHERE project_id = $1 AND date BETWEEN $2 AND $3`,
					proj.ID, start, end).Scan(&totalCount)

			case "monthly":
				month, _ := strconv.Atoi(monthStr)
				start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)
				err = db.QueryRow(`
					SELECT COALESCE(SUM(count), 0) 
					FROM manpower_count 
					WHERE project_id = $1 AND date BETWEEN $2 AND $3`,
					proj.ID, start, end).Scan(&totalCount)

			case "weekly":
				if monthStr == "" || dayStr == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Missing month or date"})
					return
				}
				startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
				startDate, err2 := time.Parse("2006-01-02", startDateStr)
				if err2 != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
					return
				}
				weekStart := startDate.AddDate(0, 0, -6)
				err = db.QueryRow(`
					SELECT COALESCE(SUM(count), 0) 
					FROM manpower_count
					WHERE project_id = $1 AND date BETWEEN $2 AND $3`,
					proj.ID, weekStart, startDate).Scan(&totalCount)

			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid summary_type"})
				return
			}

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Count fetch failed", "details": err.Error()})
				return
			}

			summaries = append(summaries, ProjectSummary{
				ProjectID:   proj.ID,
				ProjectName: proj.Name,
				TotalCount:  totalCount,
			})
		}

		c.JSON(http.StatusOK, summaries)

		// log activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower %share dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

type CategorySummary struct {
	CategoryID   uint   `json:"category_id"`
	CategoryName string `json:"category_name"`
	TotalCount   int    `json:"total_count"`
}

type DepartmentSummary struct {
	DepartmentID   uint   `json:"department_id"`
	DepartmentName string `json:"department_name"`
	TotalCount     int    `json:"total_count"`
}

type TowerSummary struct {
	TowerID    uint   `json:"tower_id"`
	TowerName  string `json:"tower_name"`
	TotalCount int    `json:"total_count"`
}

type ManpowerBreakdown struct {
	Categories  []CategorySummary   `json:"categories"`
	Departments []DepartmentSummary `json:"departments"`
	Towers      []TowerSummary      `json:"towers"`
}

// GetManpowerBreakdown godoc
// @Summary      Get manpower breakdown (h1)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary/h1 [get]
func GetManpowerBreakdown(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		projectID := c.Query("project_id")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		var start, end time.Time

		switch summaryType {
		case "yearly":
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month is required for monthly summary"})
				return
			}
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1)

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month and day are required for weekly summary"})
				return
			}
			startDateStr := fmt.Sprintf("%d-%s-%s", year, padZero(monthStr), padZero(dayStr))
			startDate, err2 := time.Parse("2006-01-02", startDateStr)
			if err2 != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
				return
			}
			end = startDate
			start = startDate.AddDate(0, 0, -6)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid summary_type"})
			return
		}

		var breakdown ManpowerBreakdown

		// -------------------------
		// 1. Categories Summary
		// -------------------------
		catRows, err := db.Query(`
			SELECT c.id, c.name, COALESCE(SUM(mc.count), 0) AS total_count
			FROM categories c
			LEFT JOIN people p ON p.category_id = c.id 
			LEFT JOIN manpower_count mc ON mc.people_id = p.id AND mc.project_id = $1
				AND mc.date BETWEEN $2 AND $3
			WHERE c.project_id = $1
			GROUP BY c.id, c.name
		`, projectID, start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch categories", "details": err.Error()})
			return
		}
		defer catRows.Close()

		for catRows.Next() {
			var cs CategorySummary
			if err := catRows.Scan(&cs.CategoryID, &cs.CategoryName, &cs.TotalCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan category failed", "details": err.Error()})
				return
			}
			breakdown.Categories = append(breakdown.Categories, cs)
		}

		// -------------------------
		// 2. Departments Summary
		// -------------------------
		deptRows, err := db.Query(`
			SELECT d.id, d.name, COALESCE(SUM(mc.count), 0) AS total_count
			FROM departments d
			LEFT JOIN people p ON p.department_id = d.id
			LEFT JOIN manpower_count mc ON mc.people_id = p.id AND mc.project_id = p.project_id
				AND mc.date BETWEEN $2 AND $3
			WHERE p.project_id = $1
			GROUP BY d.id, d.name
		`, projectID, start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch departments", "details": err.Error()})
			return
		}
		defer deptRows.Close()

		for deptRows.Next() {
			var ds DepartmentSummary
			if err := deptRows.Scan(&ds.DepartmentID, &ds.DepartmentName, &ds.TotalCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan department failed", "details": err.Error()})
				return
			}
			breakdown.Departments = append(breakdown.Departments, ds)
		}

		// -------------------------
		// 3. Towers Summary
		// -------------------------
		towerRows, err := db.Query(`
			SELECT t.id, t.name, COALESCE(SUM(mc.count), 0) AS total_count
			FROM precast t
			LEFT JOIN manpower_count mc ON mc.tower_id = t.id AND mc.project_id = t.project_id
				AND mc.date BETWEEN $2 AND $3
			WHERE t.project_id = $1 AND t.parent_id IS NULL
			GROUP BY t.id, t.name
		`, projectID, start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch towers", "details": err.Error()})
			return
		}
		defer towerRows.Close()

		for towerRows.Next() {
			var ts TowerSummary
			if err := towerRows.Scan(&ts.TowerID, &ts.TowerName, &ts.TotalCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan tower failed", "details": err.Error()})
				return
			}
			breakdown.Towers = append(breakdown.Towers, ts)
		}

		// Final JSON response
		c.JSON(http.StatusOK, breakdown)

		// log activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower %share dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

type DepartmentCountSummary struct {
	DepartmentID   int    `json:"department_id"`
	DepartmentName string `json:"department_name"`
	TotalCount     int    `json:"total_count"`
}

type DepartmentBreakdown struct {
	Departments []DepartmentCountSummary `json:"departments"`
}

// GetCategoryDepartmentBreakdown godoc
// @Summary      Get category department breakdown (h2)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary/h2 [get]
func GetCategoryDepartmentBreakdown(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		categoryID := c.Query("category_id")
		if categoryID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "category_id is required"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		var start, end time.Time

		switch summaryType {
		case "yearly":
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month is required for monthly summary"})
				return
			}
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1)

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month and day are required for weekly summary"})
				return
			}
			startDateStr := fmt.Sprintf("%d-%s-%s", year, padZero(monthStr), padZero(dayStr))
			startDate, err2 := time.Parse("2006-01-02", startDateStr)
			if err2 != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
				return
			}
			end = startDate
			start = startDate.AddDate(0, 0, -6)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid summary_type"})
			return
		}

		// -------------------------
		// Fetch departments & counts
		// -------------------------
		rows, err := db.Query(`
			SELECT d.id, d.name, COALESCE(SUM(mc.count), 0) AS total_count
			FROM departments d
			LEFT JOIN people p ON p.department_id = d.id AND p.category_id = $1
			LEFT JOIN manpower_count mc ON mc.people_id = p.id 
				AND mc.date BETWEEN $2 AND $3
			GROUP BY d.id, d.name
			ORDER BY d.id
		`, categoryID, start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch department breakdown", "details": err.Error()})
			return
		}
		defer rows.Close()

		var breakdown DepartmentBreakdown
		for rows.Next() {
			var ds DepartmentCountSummary
			if err := rows.Scan(&ds.DepartmentID, &ds.DepartmentName, &ds.TotalCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan department failed", "details": err.Error()})
				return
			}
			breakdown.Departments = append(breakdown.Departments, ds)
		}

		c.JSON(http.StatusOK, breakdown)

		// ---- Log activity ----
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

type PeopleSummary struct {
	PeopleID   int    `json:"people_id"`
	PeopleName string `json:"people_name"`
	TotalCount int    `json:"total_count"`
}

type PeopleBreakdown struct {
	People []PeopleSummary `json:"people"`
}

// GetDepartmentPeopleBreakdown godoc
// @Summary      Get department people breakdown (h3)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary/h3 [get]
func GetDepartmentPeopleBreakdown(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		projectIDStr := c.Query("project_id")
		departmentID := c.Query("department_id")
		if departmentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "department_id is required"})
			return
		}

		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		var start, end time.Time

		switch summaryType {
		case "yearly":
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month is required for monthly summary"})
				return
			}
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1)

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month and day are required for weekly summary"})
				return
			}
			startDateStr := fmt.Sprintf("%d-%s-%s", year, padZero(monthStr), padZero(dayStr))
			startDate, err2 := time.Parse("2006-01-02", startDateStr)
			if err2 != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
				return
			}
			end = startDate
			start = startDate.AddDate(0, 0, -6)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid summary_type"})
			return
		}

		// -------------------------
		// Fetch people & counts
		// -------------------------
		rows, err := db.Query(`
	SELECT p.id, p.name, COALESCE(SUM(mc.count), 0) AS total_count
	FROM people p
	LEFT JOIN manpower_count mc ON mc.people_id = p.id 
		AND mc.created_at BETWEEN $2 AND $3
	WHERE p.department_id = $1 AND p.project_id = $4
	GROUP BY p.id, p.name
	ORDER BY p.id
`, departmentID, start, end, projectIDStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch people breakdown", "details": err.Error()})
			return
		}

		defer rows.Close()

		var breakdown PeopleBreakdown
		for rows.Next() {
			var ps PeopleSummary
			if err := rows.Scan(&ps.PeopleID, &ps.PeopleName, &ps.TotalCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan people failed", "details": err.Error()})
				return
			}
			breakdown.People = append(breakdown.People, ps)
		}

		c.JSON(http.StatusOK, breakdown)

		// ---- Log activity ----
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

type SkillTypeSummary struct {
	SkillTypeID   uint   `json:"skill_type_id"`
	SkillTypeName string `json:"skill_type_name"`
	TotalCount    int    `json:"total_count"`
}

// GetSkillTypeSummaryHandler godoc
// @Summary      Get skill type summary (h4)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary/h4 [get]
func GetSkillTypeSummaryHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		projectIDStr := c.Query("project_id")
		peopleID := c.Query("people_id")
		if peopleID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "people_id is required"})
			return
		}

		// Get summary_type and date filters
		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		var start, end time.Time

		switch summaryType {
		case "yearly":
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month is required for monthly summary"})
				return
			}
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1)

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month and day are required for weekly summary"})
				return
			}
			startDateStr := fmt.Sprintf("%d-%s-%s", year, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
				return
			}
			end = startDate
			start = startDate.AddDate(0, 0, -6)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid summary_type"})
			return
		}

		// Fetch all skill types
		rows, err := db.Query(`SELECT id, name FROM skill_types`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch skill types"})
			return
		}
		defer rows.Close()

		var summaries []SkillTypeSummary

		for rows.Next() {
			var skillTypeID uint
			var skillTypeName string
			if err := rows.Scan(&skillTypeID, &skillTypeName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan skill type"})
				return
			}

			// Count manpower for this skill_type_id + people_id within date range
			var totalCount int
			err = db.QueryRow(`
				SELECT COALESCE(SUM(count), 0)
				FROM manpower_count
				WHERE skill_type_id = $1 AND people_id = $2 AND project_id = $3 AND date BETWEEN $4 AND $5
			`, skillTypeID, peopleID, projectIDStr, start, end).Scan(&totalCount)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch manpower count"})
				return
			}

			summaries = append(summaries, SkillTypeSummary{
				SkillTypeID:   skillTypeID,
				SkillTypeName: skillTypeName,
				TotalCount:    totalCount,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"skill_types": summaries,
		})

		// ---- Log activity ----
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

type SkillSummary struct {
	SkillID    uint   `json:"skill_id"`
	SkillName  string `json:"skill_name"`
	TotalCount int    `json:"total_count"`
}

// GetSkillSummaryHandler godoc
// @Summary      Get skill summary (h5)
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/manpower_project/summary/h5 [get]
func GetSkillSummaryHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		// Query params
		skillTypeIDStr := c.Query("skill_type_id")
		projectIDStr := c.Query("project_id")
		if skillTypeIDStr == "" || projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "skill_type_id and project_id are required"})
			return
		}

		skillTypeID, _ := strconv.Atoi(skillTypeIDStr)
		projectID, _ := strconv.Atoi(projectIDStr)

		// Summary type and date filters
		summaryType := c.DefaultQuery("type", "yearly")
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		year := time.Now().Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		}
		location := time.Now().Location()

		var start, end time.Time

		switch summaryType {
		case "yearly":
			start = time.Date(year, 1, 1, 0, 0, 0, 0, location)
			end = time.Date(year, 12, 31, 23, 59, 59, 0, location)

		case "monthly":
			if monthStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month is required for monthly summary"})
				return
			}
			month, _ := strconv.Atoi(monthStr)
			start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
			end = start.AddDate(0, 1, -1)

		case "weekly":
			if monthStr == "" || dayStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "month and day are required for weekly summary"})
				return
			}
			startDateStr := fmt.Sprintf("%d-%s-%s", year, padZero(monthStr), padZero(dayStr))
			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
				return
			}
			end = startDate
			start = startDate.AddDate(0, 0, -6)

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid summary_type"})
			return
		}

		// Fetch all skills for this skill_type_id
		rows, err := db.Query(`SELECT id, name FROM skills WHERE skill_type_id = $1`, skillTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch skills"})
			return
		}
		defer rows.Close()

		var summaries []SkillSummary

		for rows.Next() {
			var skillID uint
			var skillName string
			if err := rows.Scan(&skillID, &skillName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan skill"})
				return
			}

			// Count manpower for this skill_id + skill_type_id + project_id within date range
			var totalCount int
			err = db.QueryRow(`
				SELECT COALESCE(SUM(count), 0)
				FROM manpower_count
				WHERE skill_id = $1 AND skill_type_id = $2 AND project_id = $3 AND date BETWEEN $4 AND $5
			`, skillID, skillTypeID, projectID, start, end).Scan(&totalCount)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch manpower count"})
				return
			}

			summaries = append(summaries, SkillSummary{
				SkillID:    skillID,
				SkillName:  skillName,
				TotalCount: totalCount,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"skills": summaries,
		})

		// ---- Log activity ----
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower aggregate count dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

// GetProjectManHandler godoc
// @Summary      Get project manpower dashboard
// @Tags         manpower
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/project_manpower/dashboard [get]
func GetProjectManHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is required"})
			return
		}

		// get session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session", "details": err.Error()})
			return
		}

		// ---- Get user id ----
		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ---- Get role ----
		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name FROM users u 
			JOIN roles r ON u.role_id = r.role_id 
			WHERE u.id = $1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		projectID := c.Query("project_id")
		summaryType := c.DefaultQuery("type", "yearly") // yearly, monthly, weekly
		yearStr := c.Query("year")
		monthStr := c.Query("month")
		dayStr := c.Query("date")

		now := time.Now()
		location := now.Location()
		if yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}
		year, _ := strconv.Atoi(yearStr)

		// --------------------------
		// Fetch projects based on role
		// --------------------------
		var rows *sql.Rows
		var args []interface{}
		if projectID != "" {
			rows, err = db.Query(`SELECT p.project_id, p.name 
				FROM project p 
				WHERE p.project_id = $1 AND p.suspend = false`, projectID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project", "details": err.Error()})
				return
			}
		} else {
			var projectFilterQuery string
			switch roleName {
			case "superadmin":
				projectFilterQuery = `WHERE p.suspend = false`
			case "admin":
				projectFilterQuery = `WHERE p.client_id IN (
						SELECT ec.id 
			FROM end_client ec
			JOIN client cl ON ec.client_id = cl.client_id
			WHERE cl.user_id = $1
		) AND p.suspend = false`
				args = append(args, userID)
			default:
				projectFilterQuery = `WHERE p.project_id IN (
						SELECT pm.project_id 
						FROM project_members pm
						JOIN project p2 ON pm.project_id = p2.project_id
						WHERE pm.user_id = $1 AND p2.suspend = false
					)`
				args = append(args, userID)
			}
			query := fmt.Sprintf(`SELECT p.project_id, p.name FROM project p %s`, projectFilterQuery)
			rows, err = db.Query(query, args...)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects", "details": err.Error()})
				return
			}
		}
		defer rows.Close()

		projects := []struct {
			ID   int
			Name string
		}{}
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Scan failed", "details": err.Error()})
				return
			}
			projects = append(projects, struct {
				ID   int
				Name string
			}{id, name})
		}

		// --------------------------
		// helper: fetch manpower counts per date
		// --------------------------
		fetchAllCounts := func(projectID int, start, end time.Time) (map[time.Time]int, error) {
			query := `
				SELECT mc.date, SUM(mc.count)
				FROM manpower_count mc
				WHERE mc.project_id = $1 AND mc.date BETWEEN $2 AND $3
				GROUP BY mc.date
			`
			rows, err := db.Query(query, projectID, start, end)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			data := make(map[time.Time]int)
			for rows.Next() {
				var d time.Time
				var cnt int
				if err := rows.Scan(&d, &cnt); err != nil {
					return nil, err
				}
				data[d] = cnt
			}
			return data, nil
		}

		// --------------------------
		// Final response aggregation
		// --------------------------
		periodData := make(map[string]map[string]int)

		for _, proj := range projects {
			switch summaryType {
			case "yearly":
				start := time.Date(year, 1, 1, 0, 0, 0, 0, location)
				end := time.Date(year, 12, 31, 23, 59, 59, 0, location)
				allData, err := fetchAllCounts(proj.ID, start, end)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
					return
				}
				for m := 1; m <= 12; m++ {
					if year == now.Year() && m > int(now.Month()) {
						break
					}
					startM := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, location)
					endM := startM.AddDate(0, 1, -1)

					total := 0
					for d, cnt := range allData {
						if !d.Before(startM) && !d.After(endM) {
							total += cnt
						}
					}

					periodName := startM.Month().String()
					if _, ok := periodData[periodName]; !ok {
						periodData[periodName] = make(map[string]int)
					}
					periodData[periodName][proj.Name] = total
				}

			case "monthly":
				month, _ := strconv.Atoi(monthStr)
				start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
				end := start.AddDate(0, 1, -1)
				allData, err := fetchAllCounts(proj.ID, start, end)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
					return
				}

				daysInMonth := end.Day()
				for day := 1; day <= daysInMonth; day += 5 {
					startR := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
					endR := startR.AddDate(0, 0, 4)
					if endR.After(end) {
						endR = end
					}

					total := 0
					for d, cnt := range allData {
						if !d.Before(startR) && !d.After(endR) {
							total += cnt
						}
					}

					periodName := fmt.Sprintf("%d-%d", startR.Day(), endR.Day())
					if _, ok := periodData[periodName]; !ok {
						periodData[periodName] = make(map[string]int)
					}
					periodData[periodName][proj.Name] = total
				}

			case "weekly":
				if monthStr == "" || dayStr == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Missing month or date"})
					return
				}
				startDateStr := fmt.Sprintf("%s-%s-%s", yearStr, padZero(monthStr), padZero(dayStr))
				startDate, err := time.Parse("2006-01-02", startDateStr)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
					return
				}
				weekStart := startDate.AddDate(0, 0, -6)
				allData, err := fetchAllCounts(proj.ID, weekStart, startDate)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Fetch failed", "details": err.Error()})
					return
				}

				for i := 0; i < 7; i++ {
					current := weekStart.AddDate(0, 0, i)
					total := 0
					if cnt, ok := allData[current]; ok {
						total = cnt
					}
					periodName := current.Format("2006-01-02")
					if _, ok := periodData[periodName]; !ok {
						periodData[periodName] = make(map[string]int)
					}
					periodData[periodName][proj.Name] = total
				}

			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type"})
				return
			}
		}

		// --------------------------
		// Convert to final JSON response
		// --------------------------
		response := []map[string]interface{}{}
		for period, projCounts := range periodData {
			row := map[string]interface{}{
				"name": period,
			}
			for projName, count := range projCounts {
				row[projName] = count
			}
			response = append(response, row)
		}

		c.JSON(http.StatusOK, response)

		// log activity
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Fetched manpower breakdown dashboard",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		})
	}
}

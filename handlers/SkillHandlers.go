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
	"github.com/lib/pq"
)

// ==================== SKILL TYPE CRUD OPERATIONS ====================

// CreateSkillType creates a new skill type
// @Summary Create skill type
// @Description Create a new skill type
// @Tags SkillTypes
// @Accept json
// @Produce json
// @Param request body models.SkillType true "Skill type creation request"
// @Success 201 {object} models.SkillTypeResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/skill-types [post]
func CreateSkillType(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// Validate session and fetch role info
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var clientID int

		// Parse request body
		var skillTypeWithSkills struct {
			Name     string   `json:"name"`
			ClientID *int     `json:"client_id,omitempty"`
			Skills   []string `json:"skills"`
		}

		if err := c.ShouldBindJSON(&skillTypeWithSkills); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// --- Role based logic ---
		switch roleName {
		case "superadmin":
			// Superadmin must send client_id
			if skillTypeWithSkills.ClientID == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required for superadmin"})
				return
			}
			clientID = *skillTypeWithSkills.ClientID

		case "admin":
			// Admin → automatically fetch client_id using user_id
			err = db.QueryRow(`SELECT client_id FROM client WHERE user_id = $1 LIMIT 1`, userID).Scan(&clientID)
			if err != nil {
				if err == sql.ErrNoRows {
					c.JSON(http.StatusBadRequest, gin.H{"error": "No client linked to this admin"})
				} else {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch client_id"})
				}
				return
			}

		default:
			// Other users not allowed
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission to create skill type"})
			return
		}

		// --- Check for duplicate skill type ---
		var existingID int
		err = db.QueryRow(`SELECT id FROM skill_types WHERE name = $1 AND client_id = $2`, skillTypeWithSkills.Name, clientID).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Skill type with this name already exists for this client"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error checking skill type"})
			return
		}

		// --- Start transaction ---
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// --- Insert new skill type ---
		var skillTypeID int
		now := time.Now()

		err = tx.QueryRow(`
			INSERT INTO skill_types (name, client_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, skillTypeWithSkills.Name, clientID, now, now).Scan(&skillTypeID)

		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Skill type with this name already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create skill type"})
			return
		}

		// --- Insert skills ---
		var createdSkills []models.Skill
		for _, skillName := range skillTypeWithSkills.Skills {
			var existingSkillID int
			err = tx.QueryRow(`SELECT id FROM skills WHERE name = $1 AND skill_type_id = $2`, skillName, skillTypeID).Scan(&existingSkillID)
			if err == nil {
				continue // Skill already exists
			} else if err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error while checking existing skills"})
				return
			}

			var skillID int
			err = tx.QueryRow(`
				INSERT INTO skills (name, skill_type_id, created_at, updated_at)
				VALUES ($1, $2, $3, $4)
				RETURNING id
			`, skillName, skillTypeID, now, now).Scan(&skillID)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create skill: " + skillName})
				return
			}

			createdSkills = append(createdSkills, models.Skill{
				ID:          uint(skillID),
				Name:        skillName,
				SkillTypeID: uint(skillTypeID),
			})
		}

		// --- Commit transaction ---
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		skillType := models.SkillType{
			ID:        uint(skillTypeID),
			Name:      skillTypeWithSkills.Name,
			ClientID:  clientID,
			CreatedAt: now,
			UpdatedAt: now,
		}

		c.JSON(http.StatusCreated, models.SkillTypeWithSkillsResponse{
			Success: true,
			Message: "Skill type and skills created successfully",
			Data:    &skillType,
			Skills:  createdSkills,
		})
	}
}

// GetSkillTypes retrieves all skill types
// @Summary Get all skill types
// @Description Retrieve all skill types
// @Tags SkillTypes
// @Produce json
// @Success 200 {object} models.SkillTypeListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/skill-types [get]

func GetSkillTypes(db *sql.DB) gin.HandlerFunc {
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

		log.Printf("User %s (ID: %d) is fetching departments", userName, session.UserID)

		// ✅ OPTIMIZED: Single query for user + role info
		var userID, roleID int
		var roleName string

		err = db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var query string
		var args []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			// ✅ Superadmin can see all skill types and skills
			query = `
				SELECT 
					st.id AS skill_type_id, 
					st.name AS skill_type_name, 
					st.created_at AS skill_type_created_at,
					st.updated_at AS skill_type_updated_at,
					s.id AS skill_id, 
					s.name AS skill_name,
					s.created_at AS skill_created_at,
					s.updated_at AS skill_updated_at,
					st.client_id AS client_id,
					c.organization AS client_name
				FROM skill_types st
				LEFT JOIN skills s ON st.id = s.skill_type_id
				LEFT JOIN client c ON st.client_id = c.client_id
				ORDER BY st.name, s.name
			`
		case "admin":
			// Fetch client_id for this admin
			var clientID int
			err := db.QueryRow(`SELECT client_id FROM client WHERE user_id = $1 LIMIT 1`, userID).Scan(&clientID)
			if err != nil {
				if err == sql.ErrNoRows {
					c.JSON(http.StatusForbidden, gin.H{"error": "No client associated with this admin"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch client ID"})
				return
			}

			// Admin sees only skill types for their client, then get associated skills
			query = fmt.Sprintf(`
        SELECT 
            st.id AS skill_type_id, 
            st.name AS skill_type_name, 
            st.created_at AS skill_type_created_at,
            st.updated_at AS skill_type_updated_at,
            s.id AS skill_id, 
            s.name AS skill_name,
            s.created_at AS skill_created_at,
            s.updated_at AS skill_updated_at,
			st.client_id AS client_id,
			c.organization AS client_name
        FROM skill_types st
        LEFT JOIN skills s ON st.id = s.skill_type_id
		LEFT JOIN client c ON st.client_id = c.client_id
        WHERE st.client_id = $%d
        ORDER BY st.name, s.name
    `, argIndex)
			args = append(args, clientID)

		default:
			// ❌ No permission for other roles
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission to view skill types"})
			return
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skill types", "details": err.Error()})
			return
		}
		defer rows.Close()

		// ✅ Map to group skills by skill type
		skillTypeMap := make(map[uint]*models.SkillType)
		var skillTypeOrder []uint

		for rows.Next() {
			var skillTypeID uint
			var skillTypeName string
			var skillTypeCreatedAt, skillTypeUpdatedAt time.Time
			var skillID sql.NullInt64
			var skillName sql.NullString
			var skillCreatedAt, skillUpdatedAt sql.NullTime
			var clientID sql.NullInt64
			var clientName sql.NullString

			if err := rows.Scan(
				&skillTypeID, &skillTypeName, &skillTypeCreatedAt, &skillTypeUpdatedAt,
				&skillID, &skillName, &skillCreatedAt, &skillUpdatedAt, &clientID, &clientName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan skill type data"})
				return
			}

			// Initialize skill type if not already added
			if _, exists := skillTypeMap[skillTypeID]; !exists {

				var cid int
				var cname string

				if clientID.Valid {
					cid = int(clientID.Int64)
				}
				if clientName.Valid {
					cname = clientName.String
				}

				skillTypeMap[skillTypeID] = &models.SkillType{
					ID:         skillTypeID,
					Name:       skillTypeName,
					CreatedAt:  skillTypeCreatedAt,
					UpdatedAt:  skillTypeUpdatedAt,
					ClientID:   cid,
					ClientName: cname,
					Skills:     []models.Skill{},
				}
				skillTypeOrder = append(skillTypeOrder, skillTypeID)
			}

			// Add skill if it exists
			if skillID.Valid && skillName.Valid {
				skill := models.Skill{
					ID:          uint(skillID.Int64),
					Name:        skillName.String,
					SkillTypeID: skillTypeID,
				}
				skillTypeMap[skillTypeID].Skills = append(skillTypeMap[skillTypeID].Skills, skill)
			}
		}

		// ✅ Convert map to ordered slice
		var skillTypes []models.SkillType
		for _, skillTypeID := range skillTypeOrder {
			skillTypes = append(skillTypes, *skillTypeMap[skillTypeID])
		}

		c.JSON(http.StatusOK, models.SkillTypeListResponse{
			Success: true,
			Message: "Skill types with skills retrieved successfully",
			Data:    skillTypes,
		})
	}
}

// GetSkillType retrieves a specific skill type by ID
// @Summary Get skill type by ID
// @Description Retrieve a specific skill type by ID
// @Tags SkillTypes
// @Produce json
// @Param id path int true "Skill type ID"
// @Success 200 {object} models.SkillTypeResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/skill-types/{id} [get]
func GetSkillType(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		// First, check if skill type exists
		var skillTypeName string
		var clientID int
		var skillTypeCreatedAt, skillTypeUpdatedAt time.Time
		err = db.QueryRow("SELECT name, created_at, updated_at, client_id FROM skill_types WHERE id = $1", id).Scan(&skillTypeName, &skillTypeCreatedAt, &skillTypeUpdatedAt, &clientID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Query to get skill type with all its skills
		query := `
			SELECT 
				s.id as skill_id, 
				s.name as skill_name,
				s.created_at as skill_created_at,
				s.updated_at as skill_updated_at
			FROM skills s
			WHERE s.skill_type_id = $1
			ORDER BY s.name
		`

		rows, err := db.Query(query, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skills"})
			return
		}
		defer rows.Close()

		var skills []models.Skill
		for rows.Next() {
			var skillID uint
			var skillName string

			err := rows.Scan(&skillID, &skillName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan skill data"})
				return
			}

			skill := models.Skill{
				ID:          skillID,
				Name:        skillName,
				SkillTypeID: uint(id),
			}
			skills = append(skills, skill)
		}

		skillType := models.SkillType{
			ID:         uint(id),
			Name:       skillTypeName,
			CreatedAt:  skillTypeCreatedAt,
			UpdatedAt:  skillTypeUpdatedAt,
			ClientID:   clientID,
			ClientName: getClientName(db, clientID),
			Skills:     skills,
		}

		c.JSON(http.StatusOK, models.SkillTypeResponse{
			Success: true,
			Message: "Skill type with skills retrieved successfully",
			Data:    &skillType,
		})
	}
}

func getClientName(db *sql.DB, clientID int) string {
	var clientName string
	err := db.QueryRow("SELECT organization FROM client WHERE client_id = $1", clientID).Scan(&clientName)
	if err != nil {
		return ""
	}
	return clientName
}

// UpdateSkillType updates a skill type
// @Summary Update skill type
// @Description Update an existing skill type
// @Tags SkillTypes
// @Accept json
// @Produce json
// @Param id path int true "Skill type ID"
// @Param request body models.SkillType true "Skill type update request"
// @Success 200 {object} models.SkillTypeResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/skill-types/{id} [put]
func UpdateSkillType(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		var skillTypeUpdate models.SkillTypeUpdateWithSkills
		if err := c.ShouldBindJSON(&skillTypeUpdate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if skill type exists
		var existingID int
		err = db.QueryRow("SELECT id FROM skill_types WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if name already exists for different skill type
		var conflictID int
		err = db.QueryRow("SELECT id FROM skill_types WHERE name = $1 AND id != $2", skillTypeUpdate.Name, id).Scan(&conflictID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Skill type with this name already exists"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// Update skill type
		now := time.Now()
		query := "UPDATE skill_types SET name = $1, updated_at = $2"
		args := []interface{}{skillTypeUpdate.Name, now}

		if skillTypeUpdate.ClientID != nil {
			query += ", client_id = $3 WHERE id = $4"
			args = append(args, *skillTypeUpdate.ClientID, id)
		} else {
			query += " WHERE id = $3"
			args = append(args, id)
		}

		_, err = tx.Exec(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update skill type"})
			return
		}

		// Delete existing skills for this skill type
		_, err = tx.Exec("DELETE FROM skills WHERE skill_type_id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing skills"})
			return
		}

		// Create new skills for this skill type
		var updatedSkills []models.Skill
		for _, skillName := range skillTypeUpdate.Skills {
			// Insert new skill
			var skillID int
			err = tx.QueryRow(
				"INSERT INTO skills (name, skill_type_id, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id",
				skillName, id, now, now,
			).Scan(&skillID)

			if err != nil {
				if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
					c.JSON(http.StatusConflict, gin.H{"error": "Skill with name '" + skillName + "' already exists for this skill type"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create skill: " + skillName})
				return
			}

			updatedSkills = append(updatedSkills, models.Skill{
				ID:          uint(skillID),
				Name:        skillName,
				SkillTypeID: uint(id),
			})
		}

		// Commit transaction
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		skillType := models.SkillType{
			ID:        uint(id),
			Name:      skillTypeUpdate.Name,
			UpdatedAt: now,
		}

		c.JSON(http.StatusOK, models.SkillTypeWithSkillsResponse{
			Success: true,
			Message: "Skill type and skills updated successfully",
			Data:    &skillType,
			Skills:  updatedSkills,
		})
	}
}

// DeleteSkillType deletes a skill type
// @Summary Delete skill type
// @Description Delete a skill type by ID
// @Tags SkillTypes
// @Produce json
// @Param id path int true "Skill type ID"
// @Success 200 {object} models.SkillTypeResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/skill-types/{id} [delete]
func DeleteSkillType(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		// Check if skill type exists
		var existingID int
		err = db.QueryRow("SELECT id FROM skill_types WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if skills are associated with this skill type
		var skillCount int
		err = db.QueryRow("SELECT COUNT(*) FROM skills WHERE skill_type_id = $1", id).Scan(&skillCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		if skillCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Cannot delete skill type. %d skills are associated with it", skillCount)})
			return
		}

		// Delete skill type
		result, err := db.Exec("DELETE FROM skill_types WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete skill type"})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill type not found"})
			return
		}

		c.JSON(http.StatusOK, models.SkillTypeResponse{
			Success: true,
			Message: "Skill type deleted successfully",
		})
	}
}

// ==================== SKILL CRUD OPERATIONS ====================

// CreateSkill creates multiple skills for a skill type
// @Summary Create skills
// @Description Create multiple skills for a skill type
// @Tags Skills
// @Accept json
// @Produce json
// @Param request body SkillBulkCreateRequest true "Bulk skill creation request"
// @Success 201 {object} SkillBulkCreateResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/skills [post]
// SkillBulkCreateRequest represents the request for creating multiple skills for a skill type
type SkillBulkCreateRequest struct {
	SkillTypeID uint     `json:"skill_type_id" binding:"required"`
	Skills      []string `json:"skills" binding:"required"`
}

// SkillBulkCreateResponse represents the response for bulk skill creation
type SkillBulkCreateResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []string `json:"data,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func CreateSkill(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// Validate session and fetch role info
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var request SkillBulkCreateRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if skill type exists
		var skillTypeID int
		err = db.QueryRow("SELECT id FROM skill_types WHERE id = $1", request.SkillTypeID).Scan(&skillTypeID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Validate that skills array is not empty
		if len(request.Skills) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skills array cannot be empty"})
			return
		}

		// Check for duplicate skill names in the request
		skillMap := make(map[string]bool)
		for _, skillName := range request.Skills {
			if skillName == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Skill name cannot be empty"})
				return
			}
			if skillMap[skillName] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Duplicate skill names in request"})
				return
			}
			skillMap[skillName] = true
		}

		// Check if any skills already exist in this skill type
		var existingSkills []string
		for _, skillName := range request.Skills {
			var existingID int
			err = db.QueryRow("SELECT id FROM skills WHERE name = $1 AND skill_type_id = $2", skillName, request.SkillTypeID).Scan(&existingID)
			if err == nil {
				existingSkills = append(existingSkills, skillName)
			} else if err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
		}

		if len(existingSkills) > 0 {
			c.JSON(http.StatusConflict, gin.H{
				"error":           "Some skills already exist in this skill type",
				"existing_skills": existingSkills,
			})
			return
		}

		// Insert all skills in a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
			return
		}
		defer tx.Rollback()

		var createdSkills []string
		for _, skillName := range request.Skills {
			var id int
			err = tx.QueryRow(
				"INSERT INTO skills (name, skill_type_id, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id",
				skillName, request.SkillTypeID, time.Now(), time.Now(),
			).Scan(&id)

			if err != nil {
				if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
					c.JSON(http.StatusConflict, gin.H{"error": "Skill with this name already exists in this skill type"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create skill"})
				return
			}
			createdSkills = append(createdSkills, skillName)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusCreated, SkillBulkCreateResponse{
			Success: true,
			Message: fmt.Sprintf("Successfully created %d skills", len(createdSkills)),
			Data:    createdSkills,
		})
	}
}

// GetSkills retrieves all skills
// @Summary Get all skills
// @Description Retrieve all skills with optional filtering
// @Tags Skills
// @Produce json
// @Param skill_type_id query int false "Filter by skill type ID"
// @Success 200 {object} models.SkillListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/skills [get]
func GetSkills(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// Validate session and fetch role info
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		skillTypeID := c.Query("skill_type_id")
		var query string
		var args []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			// Superadmin can view all skills
			if skillTypeID != "" {
				query = fmt.Sprintf(`
					SELECT s.id, s.name, s.skill_type_id, st.name AS skill_type_name,
					       s.created_at, s.updated_at, st.client_id AS client_id, c.organization AS client_name
					FROM skills s
					JOIN skill_types st ON s.skill_type_id = st.id
					LEFT JOIN client c ON st.client_id = c.client_id
					WHERE s.skill_type_id = $%d
					ORDER BY s.name
				`, argIndex)
				args = append(args, skillTypeID)
			} else {
				query = `
					SELECT s.id, s.name, s.skill_type_id, st.name AS skill_type_name,
					       s.created_at, s.updated_at
					FROM skills s
					JOIN skill_types st ON s.skill_type_id = st.id
					ORDER BY s.name
				`
			}

		case "admin":
			// Admin can view only skills for skill_types belonging to their client
			var clientID int
			err := db.QueryRow(`SELECT client_id FROM client WHERE user_id = $1 LIMIT 1`, userID).Scan(&clientID)
			if err != nil {
				if err == sql.ErrNoRows {
					c.JSON(http.StatusForbidden, gin.H{"error": "No client associated with this admin"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch client ID"})
				return
			}

			if skillTypeID != "" {
				query = fmt.Sprintf(`
					SELECT s.id, s.name, s.skill_type_id, st.name AS skill_type_name,
					       s.created_at, s.updated_at
					FROM skills s
					JOIN skill_types st ON s.skill_type_id = st.id
					WHERE st.client_id = $%d AND s.skill_type_id = $%d
					ORDER BY s.name
				`, argIndex, argIndex+1)
				args = append(args, clientID, skillTypeID)
			} else {
				query = fmt.Sprintf(`
					SELECT s.id, s.name, s.skill_type_id, st.name AS skill_type_name,
					       s.created_at, s.updated_at
					FROM skills s
					JOIN skill_types st ON s.skill_type_id = st.id
					WHERE st.client_id = $%d
					ORDER BY s.name
				`, argIndex)
				args = append(args, clientID)
			}

		default:
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission to view skills"})
			return
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch skills", "details": err.Error()})
			return
		}
		defer rows.Close()

		var skills []models.Skill
		for rows.Next() {
			var skill models.Skill
			if err := rows.Scan(
				&skill.ID,
				&skill.Name,
				&skill.SkillTypeID,
				&skill.SkillTypeName,
				&skill.CreatedAt,
				&skill.UpdatedAt,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan skill", "details": err.Error()})
				return
			}
			skills = append(skills, skill)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating skills", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.SkillListResponse{
			Success: true,
			Message: "Skills retrieved successfully",
			Data:    skills,
		})
	}
}

// GetSkill retrieves a specific skill by ID
// @Summary Get skill by ID
// @Description Retrieve a specific skill by ID
// @Tags Skills
// @Produce json
// @Param id path int true "Skill ID"
// @Success 200 {object} models.SkillResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/skills/{id} [get]
func GetSkill(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		var skill models.Skill
		var createdAt, updatedAt time.Time
		err = db.QueryRow("SELECT id, name, skill_type_id, created_at, updated_at FROM skills WHERE id = $1", id).Scan(
			&skill.ID, &skill.Name, &skill.SkillTypeID, &createdAt, &updatedAt,
		)

		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		c.JSON(http.StatusOK, models.SkillResponse{
			Success: true,
			Message: "Skill retrieved successfully",
			Data:    &skill,
		})
	}
}

// GetSkillBySkillTypeID retrieves all skills by skill type ID
// @Summary Get skills by skill type ID
// @Description Retrieve all skills for a given skill type ID
// @Tags Skill
// @Produce json
// @Param id path int true "Skill Type ID"
// @Success 200 {object} models.SkillListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/skills/skill-type/{id} [get]
func GetSkillBySkillTypeID(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		// Fetch all skills for the given skill type ID
		rows, err := db.Query("SELECT id, name, skill_type_id, created_at, updated_at FROM skills WHERE skill_type_id = $1 ORDER BY name", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		defer rows.Close()

		var skills []models.Skill
		for rows.Next() {
			var skill models.Skill
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&skill.ID, &skill.Name, &skill.SkillTypeID, &createdAt, &updatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan skill data"})
				return
			}
			skills = append(skills, skill)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating skills"})
			return
		}

		if len(skills) == 0 {
			c.JSON(http.StatusOK, gin.H{"error": "No skills found for this skill type ID"})
			return
		}

		c.JSON(http.StatusOK, models.SkillListResponse{
			Success: true,
			Message: "Skills retrieved successfully",
			Data:    skills,
		})
	}
}

// UpdateSkill updates a skill
// @Summary Update skill
// @Description Update an existing skill
// @Tags Skills
// @Accept json
// @Produce json
// @Param id path int true "Skill ID"
// @Param request body models.Skill true "Skill update request"
// @Success 200 {object} models.SkillResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/skills/{id} [put]
func UpdateSkill(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		var skill models.Skill
		if err := c.ShouldBindJSON(&skill); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if skill exists
		var existingID int
		err = db.QueryRow("SELECT id FROM skills WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if skill type exists
		var skillTypeID int
		err = db.QueryRow("SELECT id FROM skill_types WHERE id = $1", skill.SkillTypeID).Scan(&skillTypeID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Skill type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if name already exists for different skill in same skill type
		var conflictID int
		err = db.QueryRow("SELECT id FROM skills WHERE name = $1 AND skill_type_id = $2 AND id != $3",
			skill.Name, skill.SkillTypeID, id).Scan(&conflictID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Skill with this name already exists in this skill type"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Update skill
		_, err = db.Exec("UPDATE skills SET name = $1, skill_type_id = $2, updated_at = $3 WHERE id = $4",
			skill.Name, skill.SkillTypeID, time.Now(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update skill"})
			return
		}

		skill.ID = uint(id)
		c.JSON(http.StatusOK, models.SkillResponse{
			Success: true,
			Message: "Skill updated successfully",
			Data:    &skill,
		})
	}
}

// DeleteSkill deletes a skill
// @Summary Delete skill
// @Description Delete a skill by ID
// @Tags Skills
// @Produce json
// @Param id path int true "Skill ID"
// @Success 200 {object} models.SkillResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/skills/{id} [delete]
func DeleteSkill(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
			return
		}

		// Check if skill exists
		var existingID int
		err = db.QueryRow("SELECT id FROM skills WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Delete skill
		result, err := db.Exec("DELETE FROM skills WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete skill"})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Skill not found"})
			return
		}

		c.JSON(http.StatusOK, models.SkillResponse{
			Success: true,
			Message: "Skill deleted successfully",
		})
	}
}

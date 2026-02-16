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

// ==================== DEPARTMENT CRUD OPERATIONS ====================

// CreateDepartment creates a new department
// @Summary Create department
// @Description Create a new department
// @Tags Departments
// @Accept json
// @Produce json
// @Param request body models.Department true "Department creation request"
// @Success 201 {object} models.DepartmentResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/departments [post]
func CreateDepartment(db *sql.DB) gin.HandlerFunc {
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

		// --- Parse request body ---
		var department struct {
			Name     string `json:"name"`
			ClientID *int   `json:"client_id,omitempty"` // only superadmin sends this
		}

		if err := c.ShouldBindJSON(&department); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// --- Role-based client_id logic ---
		switch roleName {
		case "superadmin":
			if department.ClientID == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required for superadmin"})
				return
			}
			clientID = *department.ClientID

		case "admin":
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
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission to create department"})
			return
		}

		// --- Check for duplicate department ---
		var existingID int
		err = db.QueryRow(`SELECT id FROM departments WHERE name = $1 AND client_id = $2`, department.Name, clientID).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Department with this name already exists for this client"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error checking department"})
			return
		}

		// --- Insert new department ---
		var id int
		now := time.Now()
		err = db.QueryRow(
			`INSERT INTO departments (name, client_id, created_at, updated_at)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id`,
			department.Name, clientID, now, now,
		).Scan(&id)

		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Department with this name already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create department"})
			return
		}

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", clientID).Scan(&clientName)
		if err != nil {
			log.Printf("Failed to fetch client name: %v", err)
			clientName = fmt.Sprintf("Client %d", clientID)
		}

		c.JSON(http.StatusCreated, gin.H{
			"success": true,
			"message": "Department created successfully",
			"data": gin.H{
				"id":         id,
				"name":       department.Name,
				"client_id":  clientID,
				"created_at": now,
				"updated_at": now,
			},
		})

		// Send notification to the user who created the department
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New department created: %s for client: %s", department.Name, clientName),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/departments",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = db.Exec(`
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		if err != nil {
			log.Printf("Failed to insert notification: %v", err)
		}

		// Send notifications to all client and end_client users
		sendClientNotifications(db, clientID,
			fmt.Sprintf("New department created: %s for client: %s", department.Name, clientName),
			"https://precastezy.blueinvent.com/departments")
	}
}

// GetDepartments retrieves all departments
// @Summary Get all departments
// @Description Retrieve all departments
// @Tags Departments
// @Produce json
// @Success 200 {object} models.DepartmentListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/departments [get]

func GetDepartments(db *sql.DB) gin.HandlerFunc {
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
			// ✅ Superadmin sees all departments
			query = `SELECT d.id, d.name, d.created_at, d.updated_at, d.client_id, c.organization AS client_name
			  FROM departments d
			  JOIN client c ON d.client_id = c.client_id
			  ORDER BY name`

		case "admin":
			// ✅ Fetch client_id for this admin
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

			// ✅ Admin sees only their client’s departments
			query = fmt.Sprintf(`
				SELECT d.id, d.name, d.created_at, d.updated_at, d.client_id, c.organization AS client_name
				FROM departments d
				JOIN client c ON d.client_id = c.client_id
				WHERE d.client_id = $%d
				ORDER BY name
			`, argIndex)
			args = append(args, clientID)

		default:
			// ❌ Other roles have no permission
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission to view departments"})
			return
		}

		// ✅ Execute the query
		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch departments",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		var departments []models.Department
		for rows.Next() {
			var dept models.Department
			if err := rows.Scan(&dept.ID, &dept.Name, &dept.CreatedAt, &dept.UpdatedAt, &dept.ClientID, &dept.ClientName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan department data"})
				return
			}
			departments = append(departments, dept)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating departments"})
			return
		}

		c.JSON(http.StatusOK, models.DepartmentListResponse{
			Success: true,
			Message: "Departments retrieved successfully",
			Data:    departments,
		})
	}
}

// GetDepartment retrieves a specific department by ID
// @Summary Get department by ID
// @Description Retrieve a specific department by its ID
// @Tags Departments
// @Produce json
// @Param id path int true "Department ID"
// @Success 200 {object} models.DepartmentResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/departments/{id} [get]
func GetDepartment(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		// Validate session
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Parse department ID from URL param
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid department ID"})
			return
		}

		// Fetch department along with client info
		var department models.Department
		err = db.QueryRow(`
			SELECT d.id, d.name, d.client_id, c.organization
			FROM departments d
			JOIN client c ON d.client_id = c.client_id
			WHERE d.id = $1
		`, id).Scan(&department.ID, &department.Name, &department.ClientID, &department.ClientName)

		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Department not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch department", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.DepartmentResponse{
			Success: true,
			Message: "Department retrieved successfully",
			Data:    &department,
		})
	}
}

// UpdateDepartment updates an existing department
// @Summary Update department
// @Description Update an existing department
// @Tags Departments
// @Accept json
// @Produce json
// @Param id path int true "Department ID"
// @Param request body models.Department true "Department update request"
// @Success 200 {object} models.DepartmentResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/departments/{id} [put]
func UpdateDepartment(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid department ID"})
			return
		}

		var department models.Department
		if err := c.ShouldBindJSON(&department); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if department exists
		var existingID int
		err = db.QueryRow("SELECT id FROM departments WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Department not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Get current client_id from database before update
		var currentClientID int
		err = db.QueryRow("SELECT client_id FROM departments WHERE id = $1", id).Scan(&currentClientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch department client_id"})
			return
		}

		// Use the client_id from the update if provided, otherwise use current
		notificationClientID := currentClientID
		if department.ClientID != nil {
			notificationClientID = *department.ClientID
		}

		// Check for duplicate name
		var conflictingID int
		err = db.QueryRow("SELECT id FROM departments WHERE name = $1 AND id != $2", department.Name, id).Scan(&conflictingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Department with this name already exists"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Build dynamic query
		query := "UPDATE departments SET name = $1, updated_at = $2"
		args := []interface{}{department.Name, time.Now()}

		if department.ClientID != nil {
			query += ", client_id = $3 WHERE id = $4"
			args = append(args, department.ClientID, id)
		} else {
			query += " WHERE id = $3"
			args = append(args, id)
		}

		result, err := db.Exec(query, args...)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Department with this name already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update department"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check update result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Department not found"})
			return
		}

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", notificationClientID).Scan(&clientName)
		if err != nil {
			log.Printf("Failed to fetch client name: %v", err)
			clientName = fmt.Sprintf("Client %d", notificationClientID)
		}

		department.ID = uint(id)
		c.JSON(http.StatusOK, models.DepartmentResponse{
			Success: true,
			Message: "Department updated successfully",
			Data:    &department,
		})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the department
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Department updated: %s for client: %s", department.Name, clientName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/departments",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all client and end_client users
		sendClientNotifications(db, notificationClientID,
			fmt.Sprintf("Department updated: %s for client: %s", department.Name, clientName),
			"https://precastezy.blueinvent.com/departments")
	}
}

// DeleteDepartment deletes a department
// @Summary Delete department
// @Description Delete a department by its ID
// @Tags Departments
// @Produce json
// @Param id path int true "Department ID"
// @Success 200 {object} models.DepartmentResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/departments/{id} [delete]
func DeleteDepartment(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid department ID"})
			return
		}

		// Fetch department info before deletion for notifications
		var departmentName string
		var clientID int
		err = db.QueryRow("SELECT name, client_id FROM departments WHERE id = $1", id).Scan(&departmentName, &clientID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Department not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Delete department
		result, err := db.Exec("DELETE FROM departments WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete department"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check delete result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Department not found"})
			return
		}

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", clientID).Scan(&clientName)
		if err != nil {
			log.Printf("Failed to fetch client name: %v", err)
			clientName = fmt.Sprintf("Client %d", clientID)
		}

		c.JSON(http.StatusOK, models.DepartmentResponse{
			Success: true,
			Message: "Department deleted successfully",
		})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the department
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Department deleted: %s from client: %s", departmentName, clientName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/departments",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all client and end_client users
		sendClientNotifications(db, clientID,
			fmt.Sprintf("Department deleted: %s from client: %s", departmentName, clientName),
			"https://precastezy.blueinvent.com/departments")
	}
}

// sendClientNotifications sends notifications to all users associated with a client (client users and end_client users)
func sendClientNotifications(db *sql.DB, clientID int, message string, action string) {
	// Get all user IDs associated with the client:
	// 1. Client users (from client table where user_id is set)
	// 2. End_client users (through end_client -> client relationship)
	query := `
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.id IN (
			-- Client users
			SELECT cl.user_id
			FROM client cl
			WHERE cl.client_id = $1 AND cl.user_id IS NOT NULL
			
			UNION
			
			-- End_client users (through client -> end_client)
			SELECT cl.user_id
			FROM end_client ec
			JOIN client cl ON ec.client_id = cl.client_id
			WHERE ec.client_id = $1 AND cl.user_id IS NOT NULL
		)
	`

	rows, err := db.Query(query, clientID)
	if err != nil {
		log.Printf("Failed to fetch client stakeholders: %v", err)
		return
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			log.Printf("Failed to scan user ID: %v", err)
			continue
		}
		userIDs = append(userIDs, userID)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over user IDs: %v", err)
		return
	}

	// Send notification to each user
	now := time.Now()
	for _, userID := range userIDs {
		notif := models.Notification{
			UserID:    userID,
			Message:   message,
			Status:    "unread",
			Action:    action,
			CreatedAt: now,
			UpdatedAt: now,
		}

		_, err = db.Exec(`
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		if err != nil {
			log.Printf("Failed to insert notification for user %d: %v", userID, err)
		}
	}
}

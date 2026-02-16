package handlers

import (
	"backend/models"
	"backend/repository"
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateWarehouse godoc
// @Summary Create a new warehouse
// @Description Adds a new warehouse. Request body: name, location, contact_number, email, capacity, project_id, etc. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param warehouse body models.Warehouse true "Warehouse data"
// @Success 201 {object} models.Warehouse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/create_warehouses [post]
func CreateWarehouse(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		var warehouse models.Warehouse
		if err := c.ShouldBindJSON(&warehouse); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON input", "details": err.Error()})
			return
		}

		// Set timestamps
		warehouse.CreatedAt = time.Now()
		warehouse.UpdatedAt = time.Now()
		warehouse.ID = repository.GenerateRandomNumber()

		// Adjusted query if project_id is included in the Warehouse model
		query := `
		INSERT INTO inv_warehouse (id,name, location, contact_number, email, capacity, used_capacity, description, created_at, updated_at, project_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
		`

		err = db.QueryRow(query, warehouse.ID, warehouse.Name, warehouse.Location, warehouse.ContactNumber, warehouse.Email,
			warehouse.Capacity, warehouse.UsedCapacity, warehouse.Description, warehouse.CreatedAt, warehouse.UpdatedAt, warehouse.ProjectID).Scan(&warehouse.ID)
		if err != nil {
			log.Printf("Error inserting warehouse: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert warehouse", "details": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New warehouse created: %s", warehouse.Name),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/warehouses", // example route for frontend
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

		// Return the created warehouse
		c.JSON(http.StatusCreated, warehouse)

		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "Create",
			Description:  "Create Warehouse",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    warehouse.ProjectID,
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

// UpdateWarehouse updates a warehouse by ID.
// @Summary Update warehouse
// @Description Update warehouse by id. Send warehouse fields in body. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param id path int true "Warehouse ID"
// @Param body body models.Warehouse true "Warehouse data"
// @Success 200 {object} models.MessageResponse "message: Warehouse updated successfully"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/update_warehouses/{id} [put]
func UpdateWarehouse(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from Authorization header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		var warehouse models.Warehouse
		if err := c.ShouldBindJSON(&warehouse); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Parse warehouse ID from the URL
		warehouseIDStr := c.Param("id")
		warehouseID, err := strconv.Atoi(warehouseIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid warehouse ID"})
			return
		}

		// Check if the warehouse exists
		var existingWarehouseID int
		err = db.QueryRow("SELECT id FROM inv_warehouse WHERE id = $1", warehouseID).Scan(&existingWarehouseID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Warehouse not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var updates []string
		var fields []interface{}
		placeholderIndex := 1

		// Fields to update
		if warehouse.Name != "" {
			updates = append(updates, fmt.Sprintf("name = $%d", placeholderIndex))
			fields = append(fields, warehouse.Name)
			placeholderIndex++
		}
		if warehouse.Location != "" {
			updates = append(updates, fmt.Sprintf("location = $%d", placeholderIndex))
			fields = append(fields, warehouse.Location)
			placeholderIndex++
		}
		if warehouse.ContactNumber != "" {
			updates = append(updates, fmt.Sprintf("contact_number = $%d", placeholderIndex))
			fields = append(fields, warehouse.ContactNumber)
			placeholderIndex++
		}
		if warehouse.Email != "" {
			updates = append(updates, fmt.Sprintf("email = $%d", placeholderIndex))
			fields = append(fields, warehouse.Email)
			placeholderIndex++
		}
		if warehouse.Capacity != 0 {
			updates = append(updates, fmt.Sprintf("capacity = $%d", placeholderIndex))
			fields = append(fields, warehouse.Capacity)
			placeholderIndex++
		}
		if warehouse.UsedCapacity != 0 {
			updates = append(updates, fmt.Sprintf("used_capacity = $%d", placeholderIndex))
			fields = append(fields, warehouse.UsedCapacity)
			placeholderIndex++
		}
		if warehouse.Description != "" {
			updates = append(updates, fmt.Sprintf("description = $%d", placeholderIndex))
			fields = append(fields, warehouse.Description)
			placeholderIndex++
		}
		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
			return
		}

		// Update `updated_at` field to the current timestamp
		updates = append(updates, fmt.Sprintf("updated_at = $%d", placeholderIndex))
		fields = append(fields, time.Now())
		placeholderIndex++

		// Build the SQL query
		sqlStatement := fmt.Sprintf("UPDATE inv_warehouse SET %s WHERE id = $%d", strings.Join(updates, ", "), placeholderIndex)
		fields = append(fields, warehouseID)

		// Execute the query
		_, err = db.Exec(sqlStatement, fields...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var projectId int
		err = db.QueryRow(`SELECT project_id FROM inv_warehouse where id = $1`, existingWarehouseID).Scan(&projectId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("warehouse updated: %s", warehouse.Name),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/warehouses", // example route for frontend
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

		c.JSON(http.StatusOK, gin.H{"message": "Warehouse updated successfully"})

		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "Update",
			Description:  "Update Warehouse",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// DeleteWarehouse deletes a warehouse by ID.
// @Summary Delete warehouse
// @Description Delete warehouse by id. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param id path int true "Warehouse ID"
// @Success 200 {object} models.MessageResponse "message: Warehouse deleted successfully"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/delete_warehouses/{id} [delete]
func DeleteWarehouse(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		id := c.Param("id")

		// Get warehouse name before deleting
		var warehouseName string
		var projectId int
		err = db.QueryRow(`SELECT name, project_id FROM inv_warehouse where id=$1`, id).Scan(&warehouseName, &projectId)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Warehouse not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch warehouse", "details": err.Error()})
			}
			return
		}

		query := `DELETE FROM inv_warehouse WHERE id = $1`
		result, err := db.Exec(query, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete warehouse", "details": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Warehouse not found"})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Warehouse deleted: %s", warehouseName),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/warehouses", // example route for frontend
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

		c.JSON(http.StatusOK, gin.H{"message": "Warehouse deleted successfully"})

		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "DELETE",
			Description:  "DELETE Warehouse",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// GetWarehouseById returns a single warehouse by ID.
// @Summary Get warehouse by ID
// @Description Returns one warehouse by id. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param id path int true "Warehouse ID"
// @Success 200 {object} models.Warehouse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_warehouses/{id} [get]
func GetWarehouseById(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		id := c.Param("id")

		query := `SELECT id, name, location, contact_number, email, capacity, used_capacity, description, created_at, updated_at, project_id FROM inv_warehouse WHERE id = $1`
		row := db.QueryRow(query, id)

		var warehouse models.Warehouse
		if err := row.Scan(&warehouse.ID, &warehouse.Name, &warehouse.Location, &warehouse.ContactNumber, &warehouse.Email, &warehouse.Capacity, &warehouse.UsedCapacity, &warehouse.Description, &warehouse.CreatedAt, &warehouse.UpdatedAt, &warehouse.ProjectID); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Warehouse not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch warehouse", "details": err.Error()})
			}
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Warehouse viewed: %s", warehouse.Name),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/warehouses",
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

		c.JSON(http.StatusOK, warehouse)

		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "GET",
			Description:  "GET Warehouse" + warehouse.Name,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    warehouse.ProjectID,
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

// GetWarehouses returns all warehouses with optional pagination.
// @Summary Get all warehouses
// @Description Returns all warehouses. Optional query: page, page_size. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param page query int false "Page number"
// @Param page_size query int false "Page size"
// @Success 200 {array} models.Warehouse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_warehouses [get]
func GetWarehouses(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		/* ---------------- PAGINATION ---------------- */
		pageStr := c.Query("page")
		limitStr := c.Query("page_size")

		usePagination := pageStr != "" || limitStr != ""

		page := 1
		limit := 10

		if usePagination {
			var parseErr error
			page, parseErr = strconv.Atoi(pageStr)
			if parseErr != nil || page < 1 {
				page = 1
			}
			limit, parseErr = strconv.Atoi(limitStr)
			if parseErr != nil || limit < 1 || limit > 100 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		// Query the database
		query := `
			SELECT 
				w.id, w.name, w.location, w.contact_number, w.email, w.capacity, w.used_capacity, 
				w.description, w.created_at, w.updated_at
			FROM 
				inv_warehouse w
			ORDER BY 
				w.id
		`

		var rows *sql.Rows
		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", 1, 2)
			rows, err = db.Query(query, limit, offset)
		} else {
			rows, err = db.Query(query)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch warehouses",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		var warehouses []models.Warehouse
		for rows.Next() {
			var warehouse models.Warehouse
			if err := rows.Scan(
				&warehouse.ID,
				&warehouse.Name,
				&warehouse.Location,
				&warehouse.ContactNumber,
				&warehouse.Email,
				&warehouse.Capacity,
				&warehouse.UsedCapacity,
				&warehouse.Description,
				&warehouse.CreatedAt,
				&warehouse.UpdatedAt,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to scan warehouse",
					"details": err.Error(),
				})
				return
			}
			warehouses = append(warehouses, warehouse)
		}

		// Check for errors during iteration
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error occurred during rows iteration",
				"details": err.Error(),
			})
			return
		}

		// // Create database notification for the admin
		// notif := models.Notification{
		// 	UserID:    userID,
		// 	Message:   "All warehouses viewed",
		// 	Status:    "unread",
		// 	Action:    "https://precastezy.blueinvent.com/warehouses",
		// 	CreatedAt: time.Now(),
		// 	UpdatedAt: time.Now(),
		// }

		// _, err = db.Exec(`
		// INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
		// VALUES ($1, $2, $3, $4, $5, $6)
		// `, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		// if err != nil {
		// 	log.Printf("Failed to insert notification: %v", err)
		// }

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": warehouses,
		}

		// Only include pagination if pagination parameters were provided
		if usePagination {
			response["pagination"] = gin.H{
				"page":        page,
				"limit":       limit,
				"total":       len(warehouses),
				"total_pages": int(math.Ceil(float64(len(warehouses)) / float64(limit))),
			}
		}

		c.JSON(http.StatusOK, response)

		/* ---------------- ACTIVITY LOG ---------------- */
		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "GET",
			Description:  "GET All Warehouses",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
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

// GetWarehousesProjectId returns warehouses for a project.
// @Summary Get warehouses by project ID
// @Description Returns all warehouses for the given project_id. Requires Authorization header.
// @Tags Warehouses
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.Warehouse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/fetch_warehouses/{project_id} [get]
func GetWarehousesProjectId(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		// Extract `project_id` from the request URL
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// SQL query to fetch warehouses filtered by `project_id`
		query := `
			SELECT 
				id, name, location, contact_number, email, capacity, used_capacity, 
				description, created_at, updated_at, project_id 
			FROM 
				inv_warehouse
			WHERE 
				project_id = $1`

		// Execute query with the provided `project_id`
		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch warehouses",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		// Prepare the result list
		var warehouses []models.Warehouse
		for rows.Next() {
			var warehouse models.Warehouse
			if err := rows.Scan(
				&warehouse.ID,
				&warehouse.Name,
				&warehouse.Location,
				&warehouse.ContactNumber,
				&warehouse.Email,
				&warehouse.Capacity,
				&warehouse.UsedCapacity,
				&warehouse.Description,
				&warehouse.CreatedAt,
				&warehouse.UpdatedAt,
				&warehouse.ProjectID,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to scan warehouse",
					"details": err.Error(),
				})
				return
			}
			warehouses = append(warehouses, warehouse)
		}

		// Check for errors during iteration
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error occurred during rows iteration",
				"details": err.Error(),
			})
			return
		}

		var projectName string
		_ = db.QueryRow(`SELECT name FROM project where project_id = $1`, projectID).Scan(&projectName)

		// // Create database notification for the admin
		// notif := models.Notification{
		// 	UserID:    userID,
		// 	Message:   fmt.Sprintf("Warehouses viewed for project: %s", projectName),
		// 	Status:    "unread",
		// 	Action:    "https://precastezy.blueinvent.com/warehouses",
		// 	CreatedAt: time.Now(),
		// 	UpdatedAt: time.Now(),
		// }

		// _, err = db.Exec(`
		// INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
		// VALUES ($1, $2, $3, $4, $5, $6)
		// `, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		// if err != nil {
		// 	log.Printf("Failed to insert notification: %v", err)
		// }

		// Return the result as JSON
		if len(warehouses) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No warehouses found for the given project ID"})
			return
		}

		c.JSON(http.StatusOK, warehouses)

		log := models.ActivityLog{
			EventContext: "Warehouse",
			EventName:    "GET",
			Description:  "GET Warehouses of project" + projectName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
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

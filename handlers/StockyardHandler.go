package handlers // import "backend/handlers"

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

// sendAllProjectStakeholdersNotifications sends notifications to all users who are project members, clients, or end_clients across all projects
func sendAllProjectStakeholdersNotifications(db *sql.DB, message string, action string) {
	// Get all user IDs who are:
	// 1. Project members (from any project)
	// 2. Client users (from any client)
	query := `
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.id IN (
			-- Project members (from any project)
			SELECT pm.user_id
			FROM project_members pm
			
			UNION
			
			-- Client users (from any client)
			SELECT cl.user_id
			FROM client cl
			WHERE cl.user_id IS NOT NULL
			
			UNION
			
			-- End_client users (through end_client -> client)
			SELECT cl.user_id
			FROM end_client ec
			JOIN client cl ON ec.client_id = cl.client_id
			WHERE cl.user_id IS NOT NULL
		)
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Failed to fetch all project stakeholders: %v", err)
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

// GetStockyard returns all stockyards.
// @Summary Get all stockyards
// @Description Returns all stockyards. Requires Authorization header.
// @Tags Stockyards
// @Accept json
// @Produce json
// @Success 200 {array} models.Stockyard
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/stockyards [get]
func GetStockyard(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query(`
			SELECT id, yard_name, location, created_at, updated_at, carpet_area 
			FROM stockyard
		`)
		if err != nil {
			log.Printf("Database Query Error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyard data", "details": err.Error()})
			return
		}
		defer rows.Close()

		stockyards := []models.Stockyard{}

		for rows.Next() {
			var stockyard models.Stockyard
			if err := rows.Scan(
				&stockyard.ID,
				&stockyard.YardName,
				&stockyard.Location,
				&stockyard.CreatedAt,
				&stockyard.UpdatedAt,
				&stockyard.CarpetArea,
			); err != nil {
				log.Printf("Row Scan Error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing stockyard data", "details": err.Error()})
				return
			}
			stockyards = append(stockyards, stockyard)
		}

		c.JSON(http.StatusOK, stockyards)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "GET",
			Description:  "Get All Stockyards",
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

// CreateStockyard creates a new stockyard.
// @Summary Create stockyard
// @Description Creates a new stockyard. Body: yard_name, location, carpet_area. Requires Authorization header.
// @Tags Stockyards
// @Accept json
// @Produce json
// @Param body body models.Stockyard true "Stockyard data"
// @Success 201 {object} models.Stockyard
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/stockyards [post]
func CreateStockyard(db *sql.DB) gin.HandlerFunc {
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

		var stockyard models.Stockyard

		// Bind and validate the incoming JSON
		if err := c.ShouldBindJSON(&stockyard); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid request data",
				"details": err.Error(),
			})
			return
		}

		// Prepare the SQL query
		query := `
			INSERT INTO stockyard (
				yard_name, location, carpet_area, created_at, updated_at
			) 
			VALUES ($1, $2, $3, $4, $5) 
			RETURNING id
		`

		// Execute the query
		err = db.QueryRow(
			query,
			stockyard.YardName,
			stockyard.Location,
			stockyard.CarpetArea,
			time.Now(),
			time.Now(),
		).Scan(&stockyard.ID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to create stockyard",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusCreated, stockyard)

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who created the stockyard
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("New stockyard created: %s", stockyard.YardName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/stockyard",
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

		// Send notifications to all project members, clients, and end_clients
		sendAllProjectStakeholdersNotifications(db,
			fmt.Sprintf("New stockyard created: %s", stockyard.YardName),
			"https://precastezy.blueinvent.com/stockyard")

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Create",
			Description:  "Create Stockyard" + stockyard.YardName,
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

// DeleteStockyard deletes a stockyard by ID.
// @Summary Delete stockyard
// @Description Deletes stockyard by id. Requires Authorization header.
// @Tags Stockyards
// @Accept json
// @Produce json
// @Param id path int true "Stockyard ID"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/stockyards/{id} [delete]
func DeleteStockyard(db *sql.DB) gin.HandlerFunc {
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

		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stockyard ID"})
			return
		}

		var name string
		_ = db.QueryRow(`SELECT yard_name FROM stockyard WHERE id = $1`, id).Scan(&name)

		query := `DELETE FROM stockyard WHERE id=$1`
		_, err = db.Exec(query, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stockyard"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Stockyard deleted successfully"})

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who deleted the stockyard
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Stockyard deleted: %s", name),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/stockyard",
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

		// Send notifications to all project members, clients, and end_clients
		sendAllProjectStakeholdersNotifications(db,
			fmt.Sprintf("Stockyard deleted: %s", name),
			"https://precastezy.blueinvent.com/stockyard")

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Delete",
			Description:  "Delete Stockyard" + name,
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
// GetStockyardByID returns a single stockyard by ID.
// @Summary Get stockyard by ID
// @Description Returns one stockyard by id. Requires Authorization header.
// @Tags Stockyards
// @Accept json
// @Produce json
// @Param id path int true "Stockyard ID"
// @Success 200 {object} models.Stockyard
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/stockyard/{id} [get]
func GetStockyardByID(db *sql.DB) gin.HandlerFunc {
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

		// Get and validate ID param
		idParam := c.Param("id")
		id, err := strconv.Atoi(idParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stockyard ID"})
			return
		}

		var stockyard models.Stockyard

		// Prepare and execute the query
		query := `
			SELECT id, yard_name, location, created_at, updated_at, carpet_area 
			FROM stockyard 
			WHERE id = $1
		`
		err = db.QueryRow(query, id).Scan(
			&stockyard.ID,
			&stockyard.YardName,
			&stockyard.Location,
			&stockyard.CreatedAt,
			&stockyard.UpdatedAt,
			&stockyard.CarpetArea,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Stockyard not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyard", "details": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, stockyard)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "GET",
			Description:  "Get Stockyard" + stockyard.YardName,
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

// UpdateStockyard updates a stockyard by ID.
// @Summary Update stockyard
// @Description Updates stockyard by id. Body: yard_name, location, carpet_area. Requires Authorization header.
// @Tags Stockyards
// @Accept json
// @Produce json
// @Param id path int true "Stockyard ID"
// @Param body body models.Stockyard true "Stockyard data"
// @Success 200 {object} models.Stockyard
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/stockyards/{id} [put]
func UpdateStockyard(db *sql.DB) gin.HandlerFunc {
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

		// Get stockyard ID from URL parameter
		stockyardID := c.Param("id")
		if stockyardID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Stockyard ID is required"})
			return
		}

		// Parse stockyard ID to integer
		id, err := strconv.Atoi(stockyardID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stockyard ID"})
			return
		}

		var stockyard models.Stockyard
		if err := c.ShouldBindJSON(&stockyard); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid request data",
				"details": err.Error(),
			})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Get the old stockyard data for comparison
		var oldStockyard models.Stockyard
		err = tx.QueryRow(`
			SELECT id, yard_name, location, carpet_area 
			FROM stockyard WHERE id = $1
		`, id).Scan(
			&oldStockyard.ID,
			&oldStockyard.YardName,
			&oldStockyard.Location,
			&oldStockyard.CarpetArea,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Stockyard not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyard"})
			}
			return
		}

		// Update the stockyard
		query := `
			UPDATE stockyard 
			SET yard_name = $1, 
				location = $2, 
				carpet_area = $3,
				updated_at = $4
			WHERE id = $5
			RETURNING id, yard_name, location, carpet_area, created_at, updated_at
		`

		err = tx.QueryRow(
			query,
			stockyard.YardName,
			stockyard.Location,
			stockyard.CarpetArea,
			time.Now(),
			id,
		).Scan(
			&stockyard.ID,
			&stockyard.YardName,
			&stockyard.Location,
			&stockyard.CarpetArea,
			&stockyard.CreatedAt,
			&stockyard.UpdatedAt,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to update stockyard",
				"details": err.Error(),
			})
			return
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Stockyard updated successfully",
			"data":    stockyard,
		})

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who updated the stockyard
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Stockyard updated: %s", stockyard.YardName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/stockyard",
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

		// Send notifications to all project members, clients, and end_clients
		sendAllProjectStakeholdersNotifications(db,
			fmt.Sprintf("Stockyard updated: %s", stockyard.YardName),
			"https://precastezy.blueinvent.com/stockyard")

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Update",
			Description:  "Update Stockyard" + stockyard.YardName,
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

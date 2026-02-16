package handlers

import (
	"backend/models"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type StockErectionRequest map[string][]struct {
	ElementTypeID int `json:"element_type_id" binding:"required"`
	Quantity      int `json:"quantity" binding:"required"`
}

// RageStockRequestByErection godoc
// @Summary      Stock erection request
// @Tags         erection
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "Stock erection request (map of element_type_id to quantity)"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/stock_erection [post]
func RageStockRequestByErection(db *sql.DB) gin.HandlerFunc {
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

		var req StockErectionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
			return
		}

		// Fetch user ID from session
		var userID int
		err = db.QueryRow(`
			SELECT user_id 
			FROM session 
			WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Track unique project IDs for notifications
		projectIDs := make(map[int]bool)

		for floorID, elements := range req {
			for _, element := range elements {
				query := `SELECT id, element_type, element_id, project_id
                          FROM precast_stock
                          WHERE element_type_id = $1 AND target_location = $2 AND order_by_erection = FALSE
                          ORDER BY id ASC LIMIT $3`

				rows, err := db.Query(query, element.ElementTypeID, floorID, element.Quantity)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch elements", "details": err.Error()})
					return
				}
				defer rows.Close()

				for rows.Next() {
					var id, elementID, projectID int
					var elementType string
					if err := rows.Scan(&id, &elementType, &elementID, &projectID); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element", "details": err.Error()})
						return
					}

					// Track project ID for notifications
					projectIDs[projectID] = true

					log.Println("Processing ID:", id)
					orderAt := time.Now()

					// Start a transaction
					tx, err := db.Begin()
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
						return
					}

					// Insert into stock_erected
					var stockErectedID int
					err = tx.QueryRow(`
						INSERT INTO stock_erected (precast_stock_id, element_id, project_id, order_at) 
						VALUES ($1, $2, $3, $4) 
						RETURNING id`, id, elementID, projectID, orderAt).Scan(&stockErectedID)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stock erection", "details": err.Error()})
						return
					}

					// Insert into stock_erected_logs
					_, err = tx.Exec(`
						INSERT INTO stock_erected_logs 
						(stock_erected_id, element_id, status, acted_by, comments) 
						VALUES ($1, $2, $3, $4, $5)`,
						stockErectedID,
						elementID,
						"Pending",
						userID,
						"Initial erection request",
					)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert log entry", "details": err.Error()})
						return
					}

					// Update precast_stock to mark as ordered by erection
					_, err = tx.Exec(`
						UPDATE precast_stock 
						SET order_by_erection = TRUE 
						WHERE id = $1`, id)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update precast stock", "details": err.Error()})
						return
					}

					// Commit the transaction
					if err = tx.Commit(); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
						return
					}
				}

				if err := rows.Err(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating over rows", "details": err.Error()})
					return
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "Stock erection request successfully processed"})

		// Send notifications for each unique project
		for projectID := range projectIDs {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", projectID)
			}

			// Send notification to the user who created the erection request
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Stock erection request created for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", projectID),
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

			// Send notifications to all project members, clients, and end_clients
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Stock erection request created for project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", projectID))
		}

		activityLog := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "POST",
			Description:  "Stock Erection Request proceesed",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

func GetErectionOrderData(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
            SELECT
    se.precast_stock_id,
    se.element_id,
    ps.element_type_id,
    et.element_type_name,
    ps.element_type,
    e.element_id,
    COALESCE(p.name, 'Unknown Floor') AS floor_name,  
    COALESCE(pp.name, 'Unknown Tower') AS tower_name,
    COALESCE(p.id, -1) AS floor_id,
    BOOL_OR(e.disable) AS disable
FROM stock_erected se  
JOIN precast_stock ps ON se.element_id = ps.element_id  
JOIN element_type et ON ps.element_type_id = et.element_type_id
JOIN precast p ON ps.target_location = p.id  
LEFT JOIN precast pp ON p.parent_id = pp.id
LEFT JOIN element e ON se.element_id = e.id
-- Removed the second JOIN to "element" to avoid duplicate alias
WHERE se.erected = 'false' AND se.recieve_in_erection = 'false' AND se.approved_status = 'false' AND ps.project_id = $1 AND e.disable = 'false'
GROUP BY
    se.precast_stock_id,
    se.element_id,
    ps.element_type_id,
    et.element_type_name,
    ps.element_type,
    e.element_id,
    p.name,
    pp.name,
    p.id;
        `

		rows, err := db.Query(query, projectID)
		if err != nil {
			log.Printf("Query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch erection order data"})
			return
		}
		defer rows.Close()

		items := make([]models.ErectionOrderResponce, 0)
		for rows.Next() {
			var item models.ErectionOrderResponce
			var disable bool
			if err := rows.Scan(
				&item.PrecastStockID,
				&item.ElementID,
				&item.ElementTypeID,
				&item.ElementTypeName,
				&item.ElementType,
				&item.ElementName,
				&item.FloorName,
				&item.TowerName,
				&item.FloorID,
				&disable,
			); err != nil {
				log.Printf("Scan error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse result"})
				return
			}
			item.Disable = disable
			items = append(items, item)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading data"})
			return
		}

		c.JSON(http.StatusOK, items)

		log := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "Get",
			Description:  "Get Erection Order Data",
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

func GetApprovedErectionOrderData(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
            SELECT
    se.precast_stock_id,
    se.element_id,
    ps.element_type_id,
    et.element_type_name,
    ps.element_type,
    COALESCE(ps.weight, 0) AS element_type_weight,
    e.element_id,
    COALESCE(p.name, 'Unknown Floor') AS floor_name,
    COALESCE(pp.name, 'Unknown Tower') AS tower_name,
    COALESCE(p.id, -1) AS floor_id,
    BOOL_OR(e.disable) AS disable,
    latest_log.status
FROM stock_erected se
JOIN precast_stock ps ON se.element_id = ps.element_id
JOIN element_type et ON ps.element_type_id = et.element_type_id
JOIN precast p ON ps.target_location = p.id
LEFT JOIN precast pp ON p.parent_id = pp.id
LEFT JOIN element e ON se.element_id = e.id
LEFT JOIN LATERAL (
    SELECT status
    FROM stock_erected_logs
    WHERE stock_erected_id = se.id
    AND status IN ('Approved', 'Rejected','Erected','Received')
    ORDER BY action_timestamp DESC
    LIMIT 1
) latest_log ON true
WHERE se.approved_status = 'true'
    AND ps.project_id = $1
    AND e.disable = 'false'
    AND latest_log.status IS NOT NULL
GROUP BY
    se.precast_stock_id,
    se.element_id,
    ps.element_type_id,
    et.element_type_name,
    ps.element_type,
    ps.weight,
    e.element_id,
    p.name,
    pp.name,
    p.id,
    latest_log.status;
        `

		rows, err := db.Query(query, projectID)
		if err != nil {
			log.Printf("Query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch approved erection order data"})
			return
		}
		defer rows.Close()

		items := make([]models.ErectionOrderResponce, 0)
		for rows.Next() {
			var item models.ErectionOrderResponce
			var disable bool
			var status sql.NullString
			if err := rows.Scan(
				&item.PrecastStockID,
				&item.ElementID,
				&item.ElementTypeID,
				&item.ElementTypeName,
				&item.ElementType,
				&item.ElementTypeWeight,
				&item.ElementName,
				&item.FloorName,
				&item.TowerName,
				&item.FloorID,
				&disable,
				&status,
			); err != nil {
				log.Printf("Scan error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse result"})
				return
			}
			item.Disable = disable
			if status.Valid {
				item.Status = status.String
			} else {
				item.Status = ""
			}
			items = append(items, item)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading data"})
			return
		}

		c.JSON(http.StatusOK, items)

		log := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "Get",
			Description:  "Get Approved Erection Order Data",
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

// UpdateStockByPlaning godoc
// @Summary      Update stock by planning
// @Tags         erection
// @Accept       json
// @Produce      json
// @Param        body  body  array   true  "Update stock requests"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/update_stock [put]
func UpdateStockByPlaning(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req []models.UpdateStockRequest

		// Bind JSON payload to struct
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("JSON binding error: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
			return
		}

		if len(req) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Request body cannot be empty"})
			return
		}

		// Validate comments for rejected items
		for _, item := range req {
			if !item.ApprovedStatus && item.Comments == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":      "Comments are required when rejecting an item",
					"element_id": item.ElementID,
				})
				return
			}
		}

		// Extract all element IDs from the request
		var elementIDs []interface{}
		for _, item := range req {
			elementIDs = append(elementIDs, item.ElementID)
		}

		// Generate placeholders ($1, $2, ...)
		placeholders := make([]string, len(req))
		for i := range req {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}

		// Correct query construction using strings.Join
		query := fmt.Sprintf("SELECT element_id FROM public.stock_erected WHERE element_id IN (%s)",
			strings.Join(placeholders, ", "))

		// Execute query
		rows, err := db.Query(query, elementIDs...)
		if err != nil {
			log.Printf("Database error while checking element IDs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database check failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Store existing element IDs in a map for quick lookup
		existingIDs := make(map[int]bool)
		for rows.Next() {
			var elementID int
			if err := rows.Scan(&elementID); err != nil {
				log.Printf("Error scanning element_id: %v", err)
				continue
			}
			existingIDs[elementID] = true
		}

		// Filter out requests where element_id does not exist
		var validUpdates []models.UpdateStockRequest
		var missingIDs []int

		for _, item := range req {
			if existingIDs[item.ElementID] {
				validUpdates = append(validUpdates, item)
			} else {
				missingIDs = append(missingIDs, item.ElementID)
			}
		}

		// If no valid updates, return error
		if len(validUpdates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid element IDs found", "missing_ids": missingIDs})
			return
		}

		// Prepare bulk update query with comments and timestamps
		updateQuery := `
            UPDATE stock_erected 
            SET approved_status = CASE
        `
		var updateParams []interface{}
		paramIndex := 1

		for _, item := range validUpdates {
			updateQuery += fmt.Sprintf(" WHEN element_id = $%d THEN $%d::BOOLEAN", paramIndex, paramIndex+1)
			updateParams = append(updateParams, item.ElementID, item.ApprovedStatus)
			paramIndex += 2
		}

		updateQuery += " END,"
		updateQuery += " comments = CASE"

		for _, item := range validUpdates {
			if item.ApprovedStatus {
				updateQuery += fmt.Sprintf(" WHEN element_id = $%d THEN 'Approved'", paramIndex)
				updateParams = append(updateParams, item.ElementID)
			} else {
				updateQuery += fmt.Sprintf(" WHEN element_id = $%d THEN $%d", paramIndex, paramIndex+1)
				updateParams = append(updateParams, item.ElementID, item.Comments)
				paramIndex++
			}
			paramIndex++
		}

		updateQuery += " END,"
		updateQuery += " action_approve_or_reject = CASE"

		for _, item := range validUpdates {
			updateQuery += fmt.Sprintf(" WHEN element_id = $%d THEN CURRENT_TIMESTAMP", paramIndex)
			updateParams = append(updateParams, item.ElementID)
			paramIndex++
		}

		updateQuery += " END WHERE element_id IN ("
		for i, item := range validUpdates {
			updateQuery += fmt.Sprintf("$%d", paramIndex)
			updateParams = append(updateParams, item.ElementID)
			if i < len(validUpdates)-1 {
				updateQuery += ", "
			}
			paramIndex++
		}
		updateQuery += ")"

		// Execute update query
		_, err = db.Exec(updateQuery, updateParams...)
		if err != nil {
			log.Printf("Database error while updating stock: %v", err)
			log.Printf("Update query: %s", updateQuery)
			log.Printf("Update parameters: %v", updateParams)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database update failed",
				"details": err.Error(),
				"query":   updateQuery,
				"params":  updateParams,
			})
			return
		}

		// Get session ID from header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user ID and name from session and user tables
		var userID int
		var firstName, lastName string
		err = db.QueryRow(`
			SELECT u.id, u.first_name, u.last_name 
			FROM session s 
			JOIN users u ON s.user_id = u.id 
			WHERE s.session_id = $1`, sessionID).Scan(&userID, &firstName, &lastName)
		if err != nil {
			log.Printf("Error fetching user details: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Combine first and last name
		fullName := strings.TrimSpace(firstName + " " + lastName)

		// Update precast_stock only when approved_status is true
		for _, item := range validUpdates {
			if item.ApprovedStatus {
				// Set order_by_erection to TRUE only when item is approved
				_, err = db.Exec(`UPDATE precast_stock SET order_by_erection = TRUE WHERE element_id = $1`, item.ElementID)
				if err != nil {
					log.Printf("Error updating precast_stock for approved item: %v", err)
					continue
				}
			}
			// No action taken when approved_status is false
		}

		// Insert logs for each update
		for _, item := range validUpdates {
			// Get stock_erected_id for the element
			var stockErectedID int
			err := db.QueryRow("SELECT id FROM stock_erected WHERE element_id = $1", item.ElementID).Scan(&stockErectedID)
			if err != nil {
				log.Printf("Error getting stock_erected_id: %v", err)
				continue
			}

			if item.ApprovedStatus {
				// Update existing log entry for approval
				_, err = db.Exec(`
					UPDATE stock_erected_logs 
					SET status = 'Approved',
						acted_by = $1,
						comments = 'Approved',
						action_timestamp = CURRENT_TIMESTAMP
					WHERE stock_erected_id = $2 
					AND element_id = $3 
					AND status = 'Pending'`,
					userID,
					stockErectedID,
					item.ElementID,
				)
				if err != nil {
					log.Printf("Error updating log for approval: %v", err)
					continue
				}
			} else {
				// Insert new log entry for rejection
				_, err = db.Exec(`
					INSERT INTO stock_erected_logs 
					(stock_erected_id, element_id, status, acted_by, comments) 
					VALUES ($1, $2, 'Rejected', $3, $4)`,
					stockErectedID,
					item.ElementID,
					userID,
					item.Comments,
				)
				if err != nil {
					log.Printf("Error inserting log for rejection: %v", err)
					continue
				}
			}
		}

		// Return response with updated and missing IDs
		c.JSON(http.StatusOK, gin.H{
			"message":       "Stock status updated successfully",
			"updated_count": len(validUpdates),
			"missing_ids":   missingIDs,
			"acted_by":      fullName,
		})

		// Get unique project IDs from updated elements
		projectIDs := make(map[int]bool)
		for _, item := range validUpdates {
			var projectID int
			err = db.QueryRow("SELECT project_id FROM stock_erected WHERE element_id = $1", item.ElementID).Scan(&projectID)
			if err == nil {
				projectIDs[projectID] = true
			}
		}

		// Send notifications for each unique project
		for projectID := range projectIDs {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", projectID)
			}

			// Send notification to the user who updated the stock
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Stock erection approval status updated for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", projectID),
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

			// Send notifications to all project members, clients, and end_clients
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Stock erection approval status updated for project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", projectID))
		}

		activityLog := models.ActivityLog{
			EventContext: "Stock",
			EventName:    "PUT",
			Description:  "Update Stock By Planning",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

func GetApprovedErectedStockSummary(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		// Convert project_id to integer
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}

		// SQL query to fetch the stock summary
		query := `
		SELECT
    ps.element_type,
    ps.element_type_id,
    et.element_type_name,
    ps.element_id AS stock_element_id,
    e.id AS element_table_id,
    e.element_id AS element_element_id,
    COALESCE(pp.name, 'Unknown Tower') AS tower_name,
    COALESCE(p.name, 'Unknown Floor') AS floor_name,
    COALESCE(p.id, -1) AS floor_id,
    ROUND(COALESCE(ps.weight, 0)::numeric, 2) AS weight,
    BOOL_OR(e.disable) AS disable
FROM precast_stock ps
JOIN precast p ON ps.target_location = p.id
LEFT JOIN precast pp ON p.parent_id = pp.id
LEFT JOIN element_type et ON ps.element_type_id = et.element_type_id
LEFT JOIN element e ON ps.element_id::integer = e.id  -- ✅ keep only one JOIN
INNER JOIN stock_erected se ON se.precast_stock_id = ps.id AND se.approved_status = 'true'
WHERE ps.project_id = $1
  AND ps.stockyard = 'true'
  AND ps.dispatch_status = 'false'
GROUP BY
    ps.element_type,
    ps.element_type_id,
    et.element_type_name,
    ps.element_id,
    e.id,
    e.element_id,
    pp.name,
    p.name,
    p.id,
    ps.weight
ORDER BY
    pp.name,
    p.name,
    et.element_type_name;

		`

		// Execute the query
		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var results []models.StockSummaryResponce

		// Iterate through rows and scan into the results slice
		for rows.Next() {
			var r models.StockSummaryResponce
			if err := rows.Scan(
				&r.ElementType,
				&r.ElementTypeID,
				&r.ElementTypeName,
				&r.StockElementID,
				&r.ElementTableID,
				&r.ElementElementID,
				&r.TowerName,
				&r.FloorName,
				&r.FloorID,
				&r.Weight,
				&r.Disable,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			results = append(results, r)
		}

		// Return the results as JSON
		c.JSON(http.StatusOK, results)

		log := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "Get",
			Description:  "Get Approved Erected Stock Summary",
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

func GetStockErectedLogs(db *sql.DB) gin.HandlerFunc {
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

		// Get project ID from route parameter
		projectID := c.Param("project_id")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id parameter is required"})
			return
		}

		// Convert project ID to int
		projectIDInt, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id format"})
			return
		}

		query := `
			SELECT 
				sel.id,
				sel.stock_erected_id,
				sel.element_id,
				sel.status,
				sel.acted_by,
				sel.comments,
				sel.action_timestamp,
				u.first_name,
				u.last_name,
				ps.element_type_id,
				et.element_type_name,
				COALESCE(e.element_id, '') AS element_name,
				COALESCE(NULLIF(substring(ps.dimensions FROM 'Thickness: ([0-9\.]+)mm'), '')::NUMERIC, 0) AS thickness,
				COALESCE(NULLIF(substring(ps.dimensions FROM 'Length: ([0-9\.]+)mm'), '')::NUMERIC, 0) AS length,
				COALESCE(ps.weight, 0) AS weight,
				COALESCE(pp.name, 'Unknown Tower') AS tower_name,
				COALESCE(p.name, 'Unknown Floor') AS floor_name
			FROM 
				stock_erected_logs sel
			JOIN 
				users u ON sel.acted_by = u.id
			JOIN
				stock_erected se ON sel.stock_erected_id = se.id
			JOIN
				precast_stock ps ON se.precast_stock_id = ps.id
			LEFT JOIN
				element e ON se.element_id = e.id
			LEFT JOIN
				element_type et ON ps.element_type_id = et.element_type_id
			LEFT JOIN
				precast p ON ps.target_location = p.id
			LEFT JOIN
				precast pp ON p.parent_id = pp.id
			WHERE ps.project_id = $1
			ORDER BY 
				sel.action_timestamp DESC`

		rows, err := db.QueryContext(context.Background(), query, projectIDInt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
			return
		}
		defer rows.Close()

		var logs []models.StockErectedLog
		for rows.Next() {
			var stockLog models.StockErectedLog
			var firstName, lastName string
			var status string
			var elementTypeID sql.NullInt64
			var elementTypeName sql.NullString
			var towerName, floorName sql.NullString

			if err := rows.Scan(
				&stockLog.ID,
				&stockLog.StockErectedID,
				&stockLog.ElementID,
				&status,
				&stockLog.ActedBy,
				&stockLog.Comments,
				&stockLog.CreatedAt,
				&firstName,
				&lastName,
				&elementTypeID,
				&elementTypeName,
				&stockLog.ElementName,
				&stockLog.Thickness,
				&stockLog.Length,
				&stockLog.Weight,
				&towerName,
				&floorName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan log data"})
				return
			}

			// Handle NULL values
			if elementTypeID.Valid {
				stockLog.ElementTypeID = int(elementTypeID.Int64)
			}
			if elementTypeName.Valid {
				stockLog.ElementTypeName = elementTypeName.String
			}
			if towerName.Valid {
				stockLog.TowerName = towerName.String
			}
			if floorName.Valid {
				stockLog.FloorName = floorName.String
			}

			// Use the status directly from the database
			stockLog.Status = status

			stockLog.ActedByName = strings.TrimSpace(firstName + " " + lastName)
			logs = append(logs, stockLog)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating log rows"})
			return
		}

		c.JSON(http.StatusOK, logs)

		log := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "Get",
			Description:  "Get Stock Erected Logs",
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

func GetReceivedErectedStock(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
			SELECT
				se.id,
				se.precast_stock_id,
				se.element_id,
				se.erected,
				se.approved_status,
				se.project_id,
				se.order_at,
				se.action_approve_or_reject,
				se.comments,
				ps.element_type,
				ps.element_type_id,
				et.element_type_name,
				COALESCE(pp.name, 'Unknown Tower') AS tower_name,
				COALESCE(p.name, 'Unknown Floor') AS floor_name,
				COALESCE(p.id, -1) AS floor_id,
				COALESCE(e.disable, false) AS deceble
			FROM
				stock_erected se
			JOIN
				precast_stock ps ON se.precast_stock_id = ps.id
			LEFT JOIN
				element_type et ON ps.element_type_id = et.element_type_id
			LEFT JOIN
				precast p ON ps.target_location = p.id
			LEFT JOIN
				precast pp ON p.parent_id = pp.id
			LEFT JOIN
				element e ON se.element_id = e.id
			WHERE
				se.project_id = $1 
				
				AND se.approved_status = true
			ORDER BY
				ps.element_type, pp.name, p.name;
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch received erected stock data"})
			return
		}
		defer rows.Close()

		var stockErectedItems []models.StockErectedResponseDetails
		dataFound := false

		for rows.Next() {
			dataFound = true
			var item models.StockErectedResponseDetails
			var orderAt, actionApproveReject sql.NullTime
			var comments sql.NullString
			var elementTypeName sql.NullString
			var towerName, floorName sql.NullString
			var floorID sql.NullInt32
			var deceble sql.NullBool

			if err := rows.Scan(
				&item.ID,
				&item.PrecastStockID,
				&item.ElementID,
				&item.Erected,
				&item.ApprovedStatus,
				&item.ProjectID,
				&orderAt,
				&actionApproveReject,
				&comments,
				&item.ElementType,
				&item.ElementTypeID,
				&elementTypeName,
				&towerName,
				&floorName,
				&floorID,
				&deceble,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing received erected stock data"})
				return
			}

			// Handle NULL values
			if orderAt.Valid {
				item.OrderAt = orderAt.Time
			}
			if actionApproveReject.Valid {
				item.ActionApproveReject = actionApproveReject.Time
			}
			if comments.Valid {
				item.Comments = comments.String
			}
			if elementTypeName.Valid {
				item.ElementTypeName = elementTypeName.String
			}
			if towerName.Valid {
				item.TowerName = towerName.String
			}
			if floorName.Valid {
				item.FloorName = floorName.String
			}
			if floorID.Valid {
				item.FloorID = int(floorID.Int32)
			}
			if deceble.Valid {
				item.Deceble = deceble.Bool
			}

			stockErectedItems = append(stockErectedItems, item)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading received erected stock data"})
			return
		}

		// If no data was found, return an empty array
		if !dataFound {
			stockErectedItems = []models.StockErectedResponseDetails{}
		}

		c.JSON(http.StatusOK, stockErectedItems)

		log := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "Get",
			Description:  "Get Received Erected Stock",
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

// UpdateErectedStatus godoc
// @Summary      Update erected status
// @Tags         erection
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Erection status update"
// @Success      200   {object}  object
// @Router       /api/erection_stock/update [post]
func UpdateErectedStatus(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.UpdateErectedStatusRequest

		// Bind JSON payload to struct
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
			return
		}

		if len(req.ElementIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element IDs array cannot be empty"})
			return
		}

		// Get session ID from header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID is required"})
			return
		}

		session, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user ID and name from session
		var userID int
		var userName string
		err = db.QueryRow(`
			SELECT u.id, u.first_name || ' ' || u.last_name AS full_name
			FROM users u
			JOIN session s ON u.id = s.user_id
			WHERE s.session_id = $1`, sessionID).Scan(&userID, &userName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Process each element ID
		for _, elementID := range req.ElementIDs {
			// Get dispatch order number for the element
			var orderNumber string
			err = tx.QueryRow(`
				SELECT do.order_number
				FROM dispatch_orders do
				JOIN dispatch_order_items doi ON do.id = doi.dispatch_order_id
				WHERE doi.element_id = $1`, elementID).Scan(&orderNumber)
			if err != nil && err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch dispatch order"})
				return
			}

			// Update stock_erected table (remove non-existent erected_at/erected_by)
			_, err = tx.Exec(`
				UPDATE stock_erected 
				SET recieve_in_erection = true
				WHERE element_id = $1 
				AND project_id = $2
				AND approved_status = true`,
				elementID,
				req.ProjectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update erection status"})
				return
			}

			// Insert into stock_erected_logs
			_, err = tx.Exec(`
				INSERT INTO stock_erected_logs 
				(stock_erected_id, element_id, status, acted_by) 
				SELECT 
					id, 
					$1, 
					'Received', 
					$2
				FROM stock_erected 
				WHERE element_id = $1 
				AND project_id = $3`,
				elementID,
				userID,
				req.ProjectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert log entry"})
				return
			}

			// If element has a dispatch order, update dispatch tracking log
			if orderNumber != "" {
				_, err = tx.Exec(`
					INSERT INTO dispatch_tracking_logs (
						order_number,
						status,
						location,
						remarks,
						status_timestamp,
						created_at,
						updated_at
					) VALUES ($1, $2, $3, $4, $5, $5, $5)`,
					orderNumber,
					"Received",
					"Received in Erection Site",
					fmt.Sprintf("Element %d erected by %s", elementID, userName),
					time.Now(),
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update dispatch tracking log"})
					return
				}

				_, err = tx.Exec(`
					UPDATE element
					SET status = 'Dispatch'
					WHERE id = $1`, elementID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update dispatch order status"})
					return
				}
			}
		}

		// Commit the transaction
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Erection status updated successfully",
			"count":   len(req.ElementIDs),
		})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", req.ProjectID)
		}

		// Send notification to the user who updated the erection status
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Elements received in erection site for project: %s", projectName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", req.ProjectID),
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

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, req.ProjectID,
			fmt.Sprintf("Elements received in erection site for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", req.ProjectID))

		activityLog := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "PUT",
			Description:  "Update Erected Status",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

func GetStockApprovalLogs(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
			SELECT 
				sal.id,
				sal.precat_stock_id,
				sal.element_id,
				sal.status,
				sal.acted_by,
				sal.comments,
				sal.action_timestamp,
				sal.element_type,
				sal.element_type_name,
				u.first_name,
				u.last_name
			FROM stock_approval_logs sal
			JOIN users u ON sal.acted_by = u.id
			JOIN stock_erected se ON sal.precat_stock_id = se.id
			WHERE se.project_id = $1
			ORDER BY sal.action_timestamp DESC
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			log.Printf("Database error while fetching logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch approval logs"})
			return
		}
		defer rows.Close()

		var logs []models.StockApprovalLog
		for rows.Next() {
			var logEntry models.StockApprovalLog
			var firstName, lastName string
			err := rows.Scan(
				&logEntry.ID,
				&logEntry.PrecatStockID,
				&logEntry.ElementID,
				&logEntry.Status,
				&logEntry.ActedBy,
				&logEntry.Comments,
				&logEntry.ActionTimestamp,
				&logEntry.ElementType,
				&logEntry.ElementTypeName,
				&firstName,
				&lastName,
			)
			if err != nil {
				log.Printf("Error scanning log row: %v", err)
				continue
			}
			logEntry.ActedByName = strings.TrimSpace(firstName + " " + lastName)
			logs = append(logs, logEntry)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Error iterating log rows: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading approval logs"})
			return
		}

		c.JSON(http.StatusOK, logs)

		log := models.ActivityLog{
			EventContext: "Stock",
			EventName:    "Get",
			Description:  "Get Stock Approval Logs",
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

// UpdateStockErectedWhenErected godoc
// @Summary      Update stock erected when erected
// @Tags         erection
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Update payload"
// @Success      200   {object}  object
// @Router       /api/erection_stock/update_when_erected [put]
func UpdateStockErectedWhenErected(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.UpdateStockErectedRequest

		// Bind JSON payload to struct
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
			return
		}

		if len(req.ElementIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element IDs array cannot be empty"})
			return
		}

		// Get session ID from header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user ID from session
		var userID int
		err = db.QueryRow(`
			SELECT user_id 
			FROM session 
			WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Process each element ID
		for _, elementID := range req.ElementIDs {
			// First, check if the record exists and is in the correct state
			var exists bool
			err = tx.QueryRow(`
				SELECT EXISTS(
					SELECT 1 FROM stock_erected 
					WHERE element_id = $1 
					AND project_id = $2 
					AND approved_status = true
				)`, elementID, req.ProjectID).Scan(&exists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to check stock_erected record",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			if !exists {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":      "No approved stock_erected record found for element",
					"element_id": elementID,
					"project_id": req.ProjectID,
				})
				return
			}

			// Update stock_erected table (columns erected_at/erected_by do not exist)
			result, err := tx.Exec(`
				UPDATE stock_erected 
				SET erected = true,
					comments = $1
				WHERE element_id = $2 
				AND project_id = $3
				AND approved_status = true`,
				req.Comments,
				elementID,
				req.ProjectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to update stock_erected status",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			// Check if any rows were actually updated
			rowsAffected, err := result.RowsAffected()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to get rows affected",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			if rowsAffected == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":      "No rows updated - element may not be in approved status",
					"element_id": elementID,
					"project_id": req.ProjectID,
				})
				return
			}

			// Update precast_stock table (columns erected_at/erected_by likely do not exist)
			result2, err := tx.Exec(`
				UPDATE precast_stock 
				SET erected = true
				WHERE element_id = $1 
				AND project_id = $2`,
				elementID,
				req.ProjectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to update precast_stock status",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			// Check if precast_stock was updated
			rowsAffected2, err := result2.RowsAffected()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to get precast_stock rows affected",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			if rowsAffected2 == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":      "No precast_stock record found for element",
					"element_id": elementID,
					"project_id": req.ProjectID,
				})
				return
			}

			// Update element status
			result3, err := tx.Exec(`
				UPDATE element
				SET status = 'Erected'
				WHERE id = $1`, elementID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to update element status",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			// Check if element was updated
			rowsAffected3, err := result3.RowsAffected()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to get element rows affected",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}

			if rowsAffected3 == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":      "No element record found",
					"element_id": elementID,
				})
				return
			}

			// Insert into stock_erected_logs
			_, err = tx.Exec(`
				INSERT INTO stock_erected_logs 
				(stock_erected_id, element_id, status, acted_by, comments) 
				SELECT 
					id, 
					$1, 
					'Erected', 
					$2,
					$3
				FROM stock_erected 
				WHERE element_id = $1 
				AND project_id = $4`,
				elementID,
				userID,
				req.Comments,
				req.ProjectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":      "Failed to insert log entry",
					"details":    err.Error(),
					"element_id": elementID,
				})
				return
			}
		}

		// Commit the transaction
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Stock status updated successfully",
			"count":   len(req.ElementIDs),
		})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", req.ProjectID)
		}

		// Send notification to the user who erected the elements
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Elements erected for project: %s", projectName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", req.ProjectID),
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

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, req.ProjectID,
			fmt.Sprintf("Elements erected for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/elementinerectionsite", req.ProjectID))

		activityLog := models.ActivityLog{
			EventContext: "Erection",
			EventName:    "PUT",
			Description:  "Elements Erected",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

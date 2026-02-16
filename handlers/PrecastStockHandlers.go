package handlers

import (
	"backend/models"

	"backend/utils"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

func CreatePrecastStock(db *sql.DB, elementID int, projectID int, stockyardID int) (int, error) {
	var elementTypeID, targetLocation int
	var elementName string
	var disable bool

	// Fetch element details from the element table
	query := "SELECT element_type_id, element_name, target_location, disable FROM element WHERE id = $1 AND project_id = $2 LIMIT 1"
	err := db.QueryRow(query, elementID, projectID).Scan(&elementTypeID, &elementName, &targetLocation, &disable)
	if err != nil {
		return 0, fmt.Errorf("element not found or database error: %w", err)
	}

	// Fetch element type details
	var thickness, length, height, weight, density float32
	var elementTypeName, elementType string
	query = "SELECT element_type, element_type_name, thickness, length, height, mass, density FROM element_type WHERE element_type_id = $1 AND project_id = $2 LIMIT 1"
	err = db.QueryRow(query, elementTypeID, projectID).Scan(&elementType, &elementTypeName, &thickness, &length, &height, &weight, &density)
	if err != nil {
		return 0, fmt.Errorf("element type not found or database error: %w", err)
	}

	// Correct format for dimensions
	dimensions := fmt.Sprintf("Thickness: %.2fmm, Length: %.2fmm, Height: %.2fmm", thickness, length, height)

	// Correct volume calculation
	volume := (thickness * length * height) / 1000000000.0
	elementWeight := volume * density

	// Define necessary values
	productionDate := time.Now() // Assign a valid timestamp
	storageLocation := "default_location"
	dispatchStatus := false

	// Insert into precast_stock table
	query = `
			INSERT INTO precast_stock (
				element_id, element_type, element_type_id, stockyard_id, dimensions, weight,
				production_date, storage_location, dispatch_status, created_at, updated_at,
				project_id, target_location
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10, $11)
			RETURNING id;
		`

	var insertedID int
	err = db.QueryRow(query, elementID, elementType, elementTypeID, stockyardID,
		dimensions, elementWeight, productionDate, storageLocation,
		dispatchStatus, projectID, targetLocation).Scan(&insertedID)

	if err != nil {
		return 0, fmt.Errorf("failed to create precast stock: %w", err)
	}

	var projectName string
	err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
	if err != nil {
		log.Printf("Failed to fetch project name: %v", err)
		projectName = fmt.Sprintf("Project %d", projectID)
	}

	var endclientId int
	err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", projectID).Scan(&endclientId)
	if err != nil {
		log.Printf("Failed to fetch client_id for notification: %v", err)
	}

	var clientId int
	err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
	if err != nil {
		log.Printf("Failed to fetch client_id for notification: %v", err)
	}

	var clientUserId int
	err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
	if err != nil {
		log.Printf("Failed to fetch client_user_id for notification: %v", err)
	}

	log.Printf("Attempting to send push notification to user %d for project creation", clientUserId)
	SendNotificationHelper(db, clientUserId,
		"Precast Stock Created",
		fmt.Sprintf("New project created: %s", projectName),
		map[string]string{
			"project_name": projectName,
			"element_type": elementType,
			"action":       "precast_stock_created",
		},
		"project_created")

	return insertedID, nil
}

// InPrecastStock godoc
// @Summary      Get in-stockyards
// @Tags         precast-stock
// @Success      200  {object}  object
// @Router       /api/in_stockyards [get]
func InPrecastStock(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query(`
			SELECT
    ps.id, ps.element_id, ps.element_type, ps.element_type_id, ps.stockyard_id,
    ps.dimensions, ps.weight, ps.production_date, ps.storage_location, ps.dispatch_status,
    ps.created_at, ps.updated_at, ps.stockyard, ps.project_id, ps.target_location,
    BOOL_OR(e.disable) AS disable
FROM
    precast_stock ps
JOIN
    element e ON e.id = ps.element_id
WHERE
    ps.project_id = $1 AND ps.stockyard = 'true' AND ps.dispatch_status = 'false'
GROUP BY
    ps.id, ps.element_id, ps.element_type, ps.element_type_id, ps.stockyard_id,
    ps.dimensions, ps.weight, ps.production_date, ps.storage_location, ps.dispatch_status,
    ps.created_at, ps.updated_at, ps.stockyard, ps.project_id, ps.target_location
`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch precast stock data"})
			return
		}
		defer rows.Close()

		groupedPrecastStocks := make(map[string][]models.PrecastStock)

		for rows.Next() {
			var precastStock models.PrecastStock
			var disable bool
			if err := rows.Scan(
				&precastStock.ID, &precastStock.ElementID, &precastStock.ElementType, &precastStock.ElementTypeID,
				&precastStock.StockyardID, &precastStock.Dimensions, &precastStock.Mass, &precastStock.ProductionDate,
				&precastStock.StorageLocation, &precastStock.DispatchStatus, &precastStock.CreatedAt,
				&precastStock.UpdatedAt, &precastStock.Stockyard, &precastStock.ProjectID, &precastStock.TargetLocation, &disable,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing precast stock data"})
				return
			}
			precastStock.Disable = disable
			groupedPrecastStocks[precastStock.ElementType] = append(groupedPrecastStocks[precastStock.ElementType], precastStock)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading precast stock data"})
			return
		}

		c.JSON(http.StatusOK, groupedPrecastStocks)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get Elements in Stockyard",
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

// parseDimensions parses the dimensions string and returns individual dimension values
func parseDimensions(dimensions string) (string, string, string) {
	// Split by comma and space
	parts := strings.Split(dimensions, ", ")
	if len(parts) == 1 {
		// If no comma found, try splitting by comma only
		parts = strings.Split(dimensions, ",")
	}

	var thickness, length, height string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(strings.ToLower(part), "thickness") {
			// Extract only the value after "Thickness: "
			if strings.Contains(part, ":") {
				parts := strings.Split(part, ":")
				if len(parts) > 1 {
					thickness = strings.TrimSpace(parts[1])
				}
			}
		} else if strings.Contains(strings.ToLower(part), "length") {
			// Extract only the value after "Length: "
			if strings.Contains(part, ":") {
				parts := strings.Split(part, ":")
				if len(parts) > 1 {
					length = strings.TrimSpace(parts[1])
				}
			}
		} else if strings.Contains(strings.ToLower(part), "height") {
			// Extract only the value after "Height: "
			if strings.Contains(part, ":") {
				parts := strings.Split(part, ":")
				if len(parts) > 1 {
					height = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	return thickness, length, height
}

// ReceivedPrecastStock godoc
// @Summary      Get received stockyards by project
// @Tags         precast-stock
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Router       /api/{project_id}/received_stockyards [get]
func ReceivedPrecastStock(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query(`
			SELECT
    ps.id, ps.element_id, ps.element_type, ps.element_type_id, ps.stockyard_id,
    ps.dimensions, ps.weight, ps.production_date, ps.storage_location, ps.dispatch_status,
    ps.created_at, ps.updated_at, ps.stockyard, ps.project_id, ps.target_location,
    BOOL_OR(e.disable) AS disable
FROM
    precast_stock ps
JOIN
    element e ON e.id = ps.element_id
WHERE
    ps.project_id = $1 AND ps.stockyard = 'false' AND ps.dispatch_status = 'false'
GROUP BY
    ps.id, ps.element_id, ps.element_type, ps.element_type_id, ps.stockyard_id,
    ps.dimensions, ps.weight, ps.production_date, ps.storage_location, ps.dispatch_status,
    ps.created_at, ps.updated_at, ps.stockyard, ps.project_id, ps.target_location`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch precast stock data",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		groupedPrecastStocks := make(map[string][]models.PrecastStock)

		for rows.Next() {
			var precastStock models.PrecastStock
			var disable bool
			if err := rows.Scan(
				&precastStock.ID, &precastStock.ElementID, &precastStock.ElementType, &precastStock.ElementTypeID,
				&precastStock.StockyardID, &precastStock.Dimensions, &precastStock.Mass, &precastStock.ProductionDate,
				&precastStock.StorageLocation, &precastStock.DispatchStatus, &precastStock.CreatedAt,
				&precastStock.UpdatedAt, &precastStock.Stockyard, &precastStock.ProjectID, &precastStock.TargetLocation, &disable,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing precast stock data"})
				return
			}

			// Parse dimensions into separate fields
			precastStock.Thickness, precastStock.Length, precastStock.Height = parseDimensions(precastStock.Dimensions)
			precastStock.Disable = disable
			// Append stock data under the corresponding element_type
			groupedPrecastStocks[precastStock.ElementType] = append(groupedPrecastStocks[precastStock.ElementType], precastStock)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading precast stock data"})
			return
		}

		c.JSON(http.StatusOK, groupedPrecastStocks)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Received Precast Stock",
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

// UpdateStockyardReceived godoc
// @Summary      Update stockyard receive element
// @Tags         precast-stock
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Receive element update"
// @Success      200   {object}  object
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/update_stockyard/recieve_element [put]
func UpdateStockyardReceived(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from JWT token
		token := c.GetHeader("Authorization")
		if token == "" {
			log.Printf("Authorization token missing in request")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization token is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		parsedToken, err := utils.ValidateJWT(token)
		if err != nil {
			log.Printf("Token validation failed: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
			return
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			log.Printf("Failed to parse token claims")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		email, ok := claims["email"].(string)
		if !ok {
			log.Printf("Email not found in token claims")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Email not found in token"})
			return
		}

		// Get user ID from email
		var actedBy int
		err = db.QueryRow("SELECT id FROM users WHERE email = $1", email).Scan(&actedBy)
		if err != nil {
			log.Printf("Failed to get user ID for email %s: %v", email, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "details": err.Error()})
			return
		}

		// Parse JSON request body
		var req models.UpdateReceivedRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("Failed to parse request body: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON input", "details": err.Error()})
			return
		}

		// Validate input
		if len(req.ElementIDs) == 0 || req.ProjectID == 0 {
			log.Printf("Invalid request parameters - ElementIDs: %v, ProjectID: %v", req.ElementIDs, req.ProjectID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing element_ids or project_id"})
			return
		}

		// Start transaction with context
		ctx := c.Request.Context()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("Failed to start transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to start transaction",
				"details": err.Error(),
				"context": "Database connection issue",
			})
			return
		}
		defer func() {
			if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("Error rolling back transaction: %v", rbErr)
				}
			}
		}()

		// Update precast stock and collect updated records
		updateQuery := `
			UPDATE precast_stock
			SET stockyard = TRUE, updated_at = $1
			WHERE element_id = ANY($2) AND project_id = $3
			RETURNING id, element_id, element_type, element_type_id
		`

		// First, collect all updated records
		var updatedRecords []struct {
			StockID       int
			ElementID     int
			ElementType   string
			ElementTypeID int
		}

		log.Printf("Executing update query with parameters: project_id=%v, element_ids=%v", req.ProjectID, req.ElementIDs)
		rows, err := tx.QueryContext(ctx, updateQuery, time.Now(), pq.Array(req.ElementIDs), req.ProjectID)
		if err != nil {
			log.Printf("DB Error in update query: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to update stockyard received status",
				"details": err.Error(),
				"query":   updateQuery,
				"params": map[string]interface{}{
					"project_id":  req.ProjectID,
					"element_ids": req.ElementIDs,
				},
			})
			return
		}

		// Collect all records first
		for rows.Next() {
			var record struct {
				StockID       int
				ElementID     int
				ElementType   string
				ElementTypeID int
			}
			if err := rows.Scan(&record.StockID, &record.ElementID, &record.ElementType, &record.ElementTypeID); err != nil {
				log.Printf("Error scanning updated stock: %v", err)
				continue
			}
			updatedRecords = append(updatedRecords, record)
		}

		if err = rows.Err(); err != nil {
			log.Printf("Error iterating rows: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error processing updated stocks",
				"details": err.Error(),
				"context": "Row iteration error",
			})
			return
		}
		rows.Close()

		_, err = tx.Exec(`
				UPDATE element
				SET status = 'In Stockyard'
				WHERE id = ANY($1) 
				`, pq.Array(req.ElementIDs))
		if err != nil {
			log.Printf("Failed to update element status: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to update element status",
				"details": err.Error(),
				"context": "Element status update error",
			})
			return
		}

		log.Printf("Successfully collected %d updated records", len(updatedRecords))

		// Now process the collected records
		logQuery := `
			INSERT INTO precast_stock_approval_logs (
				precat_stock_id, element_id, status, acted_by, 
				comments, action_timestamp, element_type, element_type_name,project_id
			) VALUES ($1, $2, 'Approved', $3, $4, $5, $6, $7, $8)
		`

		var logErrors []string
		for _, record := range updatedRecords {
			// Get element type name
			var elementTypeName string
			err = tx.QueryRowContext(ctx, "SELECT element_type_name FROM element_type WHERE element_type_id = $1", record.ElementTypeID).Scan(&elementTypeName)
			if err != nil {
				log.Printf("Error getting element type name for element_type_id %d: %v", record.ElementTypeID, err)
				logErrors = append(logErrors, fmt.Sprintf("Element type name error for ID %d: %v", record.ElementTypeID, err))
				continue
			}

			// Insert log entry
			_, err = tx.ExecContext(ctx, logQuery,
				record.StockID, record.ElementID, actedBy,
				"Stock received in stockyard", time.Now(), record.ElementType, elementTypeName, req.ProjectID)
			if err != nil {
				log.Printf("Error inserting log for stock_id %d: %v", record.StockID, err)
				logErrors = append(logErrors, fmt.Sprintf("Log insertion error for stock ID %d: %v", record.StockID, err))
				continue
			}
		}

		// Commit transaction
		if err = tx.Commit(); err != nil {
			log.Printf("Failed to commit transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "Failed to commit transaction",
				"details":    err.Error(),
				"context":    "Transaction commit error",
				"log_errors": logErrors,
			})
			return
		}

		if len(logErrors) > 0 {
			log.Printf("Completed with %d errors during log creation", len(logErrors))
			c.JSON(http.StatusOK, gin.H{
				"message":    "Stockyard received status updated with some log creation errors",
				"log_errors": logErrors,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Stockyard received status updated successfully"})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", req.ProjectID)
		}

		// Send notification to the user who received the stock in stockyard
		notif := models.Notification{
			UserID:    actedBy,
			Message:   fmt.Sprintf("Elements received in stockyard for project: %s (%d elements)", projectName, len(updatedRecords)),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/reciving", req.ProjectID),
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
			fmt.Sprintf("Elements received in stockyard for project: %s (%d elements)", projectName, len(updatedRecords)),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/reciving", req.ProjectID))

		activityLog := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "PUT",
			Description:  "Elements Received in Stockyard",
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

func GetPrecastStockForStockyard(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query(`
			SELECT
				id, element_id, element_type, element_type_id,
				dimensions, weight, production_date, storage_location,
				project_id, target_location,
				BOOL_OR(e.disable) AS disable
			FROM
				precast_stock ps
			JOIN element e ON e.id = ps.element_id
			WHERE
				ps.project_id = $1 AND ps.stockyard = 'false' AND ps.dispatch_status = 'false'`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch precast stock data"})
			return
		}
		defer rows.Close()

		var precastStocks []models.PrecastStockResponse

		for rows.Next() {
			var precastStock models.PrecastStockResponse
			var disable bool
			if err := rows.Scan(
				&precastStock.ID, &precastStock.ElementID, &precastStock.ElementType, &precastStock.ElementTypeID,
				&precastStock.Dimensions, &precastStock.Mass, &precastStock.ProductionDate,
				&precastStock.StorageLocation, &precastStock.ProjectID, &precastStock.TargetLocation, &disable,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing precast stock data"})
				return
			}
			precastStock.Disable = disable
			precastStocks = append(precastStocks, precastStock)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading precast stock data"})
			return
		}

		c.JSON(http.StatusOK, precastStocks)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get Precast Stock",
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
func UpdatePrecastStock(db *sql.DB) gin.HandlerFunc {
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

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stock ID"})
			return
		}

		var requestBody struct {
			ProjectID       int    `json:"project_id"`
			StockyardID     int    `json:"stockyard_id"`
			StorageLocation string `json:"storage_location"`
		}

		if err := c.ShouldBindJSON(&requestBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		query := "UPDATE precast_stock SET stockyard_id = $1, stockyard = TRUE, storage_location = $2 WHERE id = $3 AND project_id = $4"
		_, err = db.Exec(query, requestBody.StockyardID, requestBody.StorageLocation, id, requestBody.ProjectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update precast stock"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Precast stock updated successfully"})

		// Get userID from session
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", requestBody.ProjectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", requestBody.ProjectID)
			}

			// Send notification to the user who updated the precast stock
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Precast stock updated for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/reciving", requestBody.ProjectID),
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
			sendProjectNotifications(db, requestBody.ProjectID,
				fmt.Sprintf("Precast stock updated for project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/reciving", requestBody.ProjectID))
		}

		activityLog := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "PUT",
			Description:  "Updated Precast Stock",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    requestBody.ProjectID,
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

func GetElementlistFromStockYard(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
		SELECT
    ps.element_type,
    ps.element_type_id,
    et.element_type_name,
    COALESCE(pp.name, 'Unknown Tower') AS tower_name,
    COALESCE(p.name, 'Unknown Floor') AS floor_name,
    COALESCE(p.id, -1) AS floor_id,
    COUNT(*) AS total_elements,
    COALESCE(erection_count.total_erection_elements, 0) AS total_erection_elements
FROM precast_stock ps
JOIN precast p ON ps.target_location = p.id
LEFT JOIN precast pp ON p.parent_id = pp.id
LEFT JOIN element_type et ON ps.element_type_id = et.element_type_id
LEFT JOIN element e ON ps.element_id = e.id
LEFT JOIN (
    -- Subquery to count total erection elements
    SELECT ps2.element_type_id, ps2.target_location, COUNT(*) AS total_erection_elements
    FROM precast_stock ps2
    JOIN element e2 ON ps2.element_id = e2.id
    WHERE ps2.project_id = $1
        AND ps2.stockyard = 'true'
        AND ps2.order_by_erection = 'true'
        AND e2.disable = 'false'
    GROUP BY ps2.element_type_id, ps2.target_location
) erection_count ON ps.element_type_id = erection_count.element_type_id
                AND ps.target_location = erection_count.target_location
WHERE ps.project_id = $1
    AND ps.stockyard = 'true'
    AND ps.order_by_erection = 'false'  -- Keep only 'false' elements
    AND e.disable = 'false'  -- Filter out disabled elements
GROUP BY ps.element_type, ps.element_type_id, et.element_type_name, pp.name, p.name, p.id, erection_count.total_erection_elements
ORDER BY pp.name, p.name, et.element_type_name;
`

		rows, err := db.QueryContext(c.Request.Context(), query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to execute query: %v", err)})
			return
		}
		defer rows.Close()

		// Define response structure
		towerData := make(map[string]map[string]map[string]models.ElementCountResponse)
		dataFound := false

		for rows.Next() {
			var elementType, elementTypeName, towerName, floorName string
			var elementTypeID, totalElements, totalErectionElements, floorId int

			// Scan row data into variables
			err := rows.Scan(
				&elementType, &elementTypeID, &elementTypeName, &towerName, &floorName, &floorId,
				&totalElements, &totalErectionElements)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Ensure tower exists
			if _, exists := towerData[towerName]; !exists {
				towerData[towerName] = make(map[string]map[string]models.ElementCountResponse)
			}

			// Ensure floor exists under the tower
			if _, exists := towerData[towerName][floorName]; !exists {
				towerData[towerName][floorName] = make(map[string]models.ElementCountResponse)
			}

			// Store element count data under element_type (NO nested element_type_name)
			towerData[towerName][floorName][elementType] = models.ElementCountResponse{
				ElementType:      elementType,
				ElementTypeID:    elementTypeID,
				ElementTypeName:  elementTypeName,
				BalancelElements: totalElements,
				Leftelements:     totalErectionElements,
				FloorID:          floorId,
			}

			dataFound = true
		}

		// Check for iteration errors
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error while iterating rows: %v", err)})
			return
		}

		// Return empty response if no data found
		if !dataFound {
			c.JSON(http.StatusOK, gin.H{})
			return
		}

		// Return structured response
		c.JSON(http.StatusOK, towerData)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get Element List from Stockyard",
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

func GetAllPrecastStock(db *sql.DB) gin.HandlerFunc {
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

		var userID int
		if err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": "could not resolve user"})
			return
		}

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// If user is a stock manager for this project, restrict to their assigned stockyard(s) only
		var managerStockyardIDs []int
		managerRows, err := db.Query(
			"SELECT stockyard_id FROM project_stockyard WHERE project_id = $1 AND user_id = $2",
			projectID, userID,
		)
		if err != nil {
			log.Printf("Database error fetching stockyard assignments in GetAllPrecastStock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to resolve stockyard access",
				"details": err.Error(),
			})
			return
		}
		for managerRows.Next() {
			var sid int
			if err := managerRows.Scan(&sid); err != nil {
				managerRows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stockyard assignment", "details": err.Error()})
				return
			}
			managerStockyardIDs = append(managerStockyardIDs, sid)
		}
		managerRows.Close()

		// Get the type query parameter
		typeParam := c.Query("type")
		isArchived := typeParam == "archived"

		// Build the WHERE clause conditionally
		whereConditions := `
	ps.project_id = $1
	AND ps.stockyard = 'true'
	AND e.disable = 'false'`

		if !isArchived {
			whereConditions += `
	AND ps.order_by_erection = 'false'
	AND ps.dispatch_status = 'false'
	AND ps.erected = 'false'
	AND ps.recieve_in_erection = 'false'`
		}

		// Stock manager: only show stock for their assigned stockyard(s); others see all
		if len(managerStockyardIDs) > 0 {
			whereConditions += `
	AND ps.stockyard_id = ANY($2)`
		}

		query := `
			SELECT
	ps.id,
	e.element_id AS element_name,
	ps.element_type,
	ps.element_id,
	ps.element_type_id,
	et.element_type_name,
	ps.stockyard_id,
	NULLIF(substring(ps.dimensions FROM 'Thickness: ([0-9\.]+)mm'), '')::NUMERIC AS thickness,
	NULLIF(substring(ps.dimensions FROM 'Length: ([0-9\.]+)mm'), '')::NUMERIC AS length,
	NULLIF(substring(ps.dimensions FROM 'Height: ([0-9\.]+)mm'), '')::NUMERIC AS height,
	ps.weight,
	ps.production_date,
	ps.storage_location,
	ps.dispatch_status,
	ps.created_at,
	ps.updated_at,
	ps.stockyard,
	ps.project_id,
	ps.target_location,
	COALESCE(pp.name, 'Unknown Tower') AS tower_name,
	COALESCE(p.name, 'Unknown Floor') AS floor_name,
	COALESCE(p.id, -1) AS floor_id,
	BOOL_OR(e.disable) AS disable
FROM
	precast_stock ps
JOIN
	precast p ON ps.target_location = p.id
LEFT JOIN
	precast pp ON p.parent_id = pp.id
LEFT JOIN
	element e ON e.id = ps.element_id
LEFT JOIN
	element_type et ON ps.element_type_id = et.element_type_id
WHERE` + whereConditions + `
GROUP BY
	ps.id, e.element_id, ps.element_type, ps.element_type_id, et.element_type_name, ps.stockyard_id,
	ps.dimensions, ps.production_date, ps.storage_location, 
	ps.dispatch_status, ps.created_at, ps.updated_at, ps.stockyard, ps.project_id, 
	ps.target_location, pp.name, p.name, p.id
ORDER BY
	ps.element_type, pp.name, p.name;
		`

		var rows *sql.Rows
		if len(managerStockyardIDs) > 0 {
			rows, err = db.Query(query, projectID, pq.Array(managerStockyardIDs))
		} else {
			rows, err = db.Query(query, projectID)
		}
		if err != nil {
			log.Printf("Database error in GetAllPrecastStock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch precast stock data",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		var precastStocks []models.PrecastStockResponseDetails
		dataFound := false

		for rows.Next() {
			dataFound = true
			var precastStock models.PrecastStockResponseDetails
			var towerName, floorName string
			var floorID int
			var disable bool
			var thickness, length, height sql.NullFloat64

			if err := rows.Scan(
				&precastStock.ID, &precastStock.ElementName, &precastStock.ElementType, &precastStock.ElementID, &precastStock.ElementTypeID, &precastStock.ElementTypeName,
				&precastStock.StockyardID, &thickness, &length, &height,
				&precastStock.Mass,
				&precastStock.ProductionDate, &precastStock.StorageLocation,
				&precastStock.DispatchStatus, &precastStock.CreatedAt, &precastStock.UpdatedAt,
				&precastStock.Stockyard, &precastStock.ProjectID, &precastStock.TargetLocation,
				&towerName, &floorName, &floorID, &disable,
			); err != nil {
				log.Printf("Error scanning row in GetAllPrecastStock: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Error parsing precast stock data",
					"details": err.Error(),
				})
				return
			}

			// Convert NullFloat64 to float64 for the response
			if thickness.Valid {
				precastStock.Thickness = thickness.Float64
			}
			if length.Valid {
				precastStock.Length = length.Float64
			}
			if height.Valid {
				precastStock.Height = height.Float64
			}

			// Add tower and floor information to the response
			precastStock.TowerName = towerName
			precastStock.FloorName = floorName
			precastStock.FloorID = floorID
			precastStock.Disable = disable

			precastStocks = append(precastStocks, precastStock)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Error iterating rows in GetAllPrecastStock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error reading precast stock data",
				"details": err.Error(),
			})
			return
		}

		// If no data was found, return an empty array
		if !dataFound {
			precastStocks = []models.PrecastStockResponseDetails{}
		}

		c.JSON(http.StatusOK, precastStocks)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get All Precast Stock",
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

func GetPrecastStockApprovalLogs(db *sql.DB) gin.HandlerFunc {
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

		query := `
			SELECT 
				sal.id,
				sal.precat_stock_id,
				sal.element_id,
				sal.status,
				sal.comments,
				sal.action_timestamp,
				sal.element_type,
				sal.element_type_name,
				(u.first_name || ' ' || u.last_name) AS acted_by_name
			FROM precast_stock_approval_logs sal
			INNER JOIN users u ON sal.acted_by = u.id
			ORDER BY sal.action_timestamp DESC
		`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch approval logs"})
			return
		}
		defer rows.Close()

		type StockApprovalLog struct {
			ID              int       `json:"id"`
			PrecatStockID   int       `json:"precat_stock_id"`
			ElementID       int       `json:"element_id"`
			Status          string    `json:"status"`
			Comments        string    `json:"comments"`
			ActionTimestamp time.Time `json:"action_timestamp"`
			ElementType     string    `json:"element_type"`
			ElementTypeName string    `json:"element_type_name"`
			ActedByName     string    `json:"acted_by_name"`
		}

		var logs []StockApprovalLog

		for rows.Next() {
			var logEntry StockApprovalLog
			err := rows.Scan(
				&logEntry.ID,
				&logEntry.PrecatStockID,
				&logEntry.ElementID,
				&logEntry.Status,
				&logEntry.Comments,
				&logEntry.ActionTimestamp,
				&logEntry.ElementType,
				&logEntry.ElementTypeName,
				&logEntry.ActedByName,
			)
			if err != nil {
				// Log scan error but continue processing other rows
				continue
			}
			logs = append(logs, logEntry)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading approval logs"})
			return
		}

		c.JSON(http.StatusOK, logs)

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get Precast Stock Approval Logs",
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

func GetPendingApprovalRequests(db *sql.DB) gin.HandlerFunc {
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

		// Query to get all pending approval requests with relevant details
		rows, err := db.Query(`
			SELECT 
				ps.id,
				ps.element_id,
				ps.element_type,
				ps.element_type_id,
				ps.dimensions,
				ps.weight,
				ps.production_date,
				ps.storage_location,
				ps.created_at,
				et.element_type_name,
				e.element_name,
				COALESCE(psal.status, 'Pending') as approval_status,
				COALESCE(psal.comments, '') as comments,
				COALESCE(psal.action_timestamp, ps.created_at) as last_action_timestamp,
				COALESCE(u.first_name || ' ' || u.last_name, '') as acted_by_name
			FROM precast_stock ps
			LEFT JOIN element_type et ON ps.element_type_id = et.element_type_id
			LEFT JOIN element e ON ps.element_id = e.id
			LEFT JOIN precast_stock_approval_logs psal ON ps.id = psal.precat_stock_id
			LEFT JOIN users u ON psal.acted_by = u.id
			WHERE ps.project_id = $1
			AND (
				psal.id IS NULL 
				OR psal.id IN (
					SELECT MAX(id) 
					FROM precast_stock_approval_logs 
					GROUP BY precat_stock_id
				)
			)
			AND (
				psal.status IS NULL 
				OR psal.status = 'Pending'
			)
			ORDER BY ps.created_at DESC`, projectID)
		if err != nil {
			log.Printf("Error fetching pending approval requests: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch pending approval requests",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		var pendingRequests []models.PendingApprovalRequest
		for rows.Next() {
			var request models.PendingApprovalRequest
			if err := rows.Scan(
				&request.ID,
				&request.ElementID,
				&request.ElementType,
				&request.ElementTypeID,
				&request.Dimensions,
				&request.Weight,
				&request.ProductionDate,
				&request.StorageLocation,
				&request.CreatedAt,
				&request.ElementTypeName,
				&request.ElementName,
				&request.ApprovalStatus,
				&request.Comments,
				&request.LastActionTimestamp,
				&request.ActedByName,
			); err != nil {
				log.Printf("Error scanning pending approval request: %v", err)
				continue
			}
			pendingRequests = append(pendingRequests, request)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Error iterating pending approval requests: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error processing pending approval requests"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"project_id": projectID,
			"requests":   pendingRequests,
			"count":      len(pendingRequests),
		})

		log := models.ActivityLog{
			EventContext: "Stockyard",
			EventName:    "Get",
			Description:  "Get Pending Approval Requests",
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

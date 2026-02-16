package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateDrawingType godoc
// @Summary      Create drawing type
// @Description  Create a new drawing type for a project
// @Tags         drawing-types
// @Accept       json
// @Produce      json
// @Param        body  body      models.DrawingsType  true  "Drawing type data"
// @Success      200   {object}  models.MessageResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/drawingtype_create [post]
func CreateDrawingType(db *sql.DB) gin.HandlerFunc {
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
		// Retrieve and log the origin of the request
		origin := c.Request.Header.Get("Origin")
		log.Printf("Request Origin: %s", origin)

		// Handle OPTIONS requests (CORS preflight)
		if c.Request.Method == http.MethodOptions {
			c.JSON(http.StatusOK, gin.H{"origin": origin})
			return
		}

		// Parse the JSON body into the drawingType struct
		var drawingType models.DrawingsType
		if err := c.ShouldBindJSON(&drawingType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Generate a random ID for the new drawing type
		drawingType.DrawingsTypeId = repository.GenerateRandomNumber()

		// Insert the drawing type into the database
		sqlStatement := `
			INSERT INTO drawing_type ( 
				drawing_type_id,
				drawing_type_name,
				project_id
			) VALUES ($1, $2, $3) RETURNING drawing_type_id`

		err = db.QueryRow(sqlStatement,
			drawingType.DrawingsTypeId,
			drawingType.DrawingTypeName,
			drawingType.ProjectId).Scan(&drawingType.DrawingsTypeId)

		// Handle any database insertion errors
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Send a success response
		c.JSON(http.StatusOK, gin.H{
			"message": "Drawing Type Added successfully",
			"origin":  origin,
		})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", drawingType.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", drawingType.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the drawing type
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New drawing type created: %s for project: %s", drawingType.DrawingTypeName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingType.ProjectId),
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
		sendProjectNotifications(db, drawingType.ProjectId,
			fmt.Sprintf("New drawing type created: %s for project: %s", drawingType.DrawingTypeName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingType.ProjectId))

		activityLog := models.ActivityLog{
			EventContext: "Drawing Type",
			EventName:    "Create",
			Description:  "Drawing Type created successfully" + drawingType.DrawingTypeName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawingType.ProjectId,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// UpdateDrawingType godoc
// @Summary      Update drawing type
// @Description  Update an existing drawing type by ID
// @Tags         drawing-types
// @Accept       json
// @Produce      json
// @Param        id    path      int                  true  "Drawing type ID"
// @Param        body  body      models.DrawingsType  true  "Drawing type data"
// @Success      200   {object}  models.MessageResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/drawingtype_update/{id} [put]
func UpdateDrawingType(db *sql.DB) gin.HandlerFunc {
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

		// Extract and convert the drawing type ID from the URL parameter
		idParam := c.Param("id")
		id, err := strconv.Atoi(idParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid drawing type ID"})
			return
		}

		var drawingType models.DrawingsType
		// Bind the incoming JSON payload to the `drawingType` struct
		if err := c.ShouldBindJSON(&drawingType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data: " + err.Error()})
			return
		}

		// Get a reference to the database
		db := storage.InitDB()

		// SQL query to update the drawing type record
		sqlStatement := `
		UPDATE drawing_type 
		SET drawing_type_name = $1, project_id = $2, updated_at = now() 
		WHERE drawing_type_id = $3
	`

		// Execute the SQL query
		_, err = db.Exec(sqlStatement,
			drawingType.DrawingTypeName,
			drawingType.ProjectId,
			id,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update drawing type: " + err.Error()})
			return
		}

		// Respond with success
		c.JSON(http.StatusOK, gin.H{"message": "Drawing Type updated successfully"})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", drawingType.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", drawingType.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the drawing type
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Drawing type updated: %s for project: %s", drawingType.DrawingTypeName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingType.ProjectId),
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
		sendProjectNotifications(db, drawingType.ProjectId,
			fmt.Sprintf("Drawing type updated: %s for project: %s", drawingType.DrawingTypeName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingType.ProjectId))

		activityLog := models.ActivityLog{
			EventContext: "Drawing Type",
			EventName:    "Update",
			Description:  "Drawing Type updated successfully: " + drawingType.DrawingTypeName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawingType.ProjectId,
		}
		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetAllDrawingType godoc
// @Summary      Get all drawing types
// @Description  Get all drawing types (no project filter)
// @Tags         drawing-types
// @Accept       json
// @Produce      json
// @Success      200  {array}  models.DrawingsType
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/drawingtype [get]
func GetAllDrawingType(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query(`
	SELECT 
	drawing_type_id,
	drawing_type_name,
	project_id from drawing_type`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element: " + err.Error()})
			return
		}
		defer rows.Close()

		var getDrawingType []models.DrawingsType
		for rows.Next() {
			var drawingType models.DrawingsType
			err := rows.Scan(
				&drawingType.DrawingsTypeId,
				&drawingType.DrawingTypeName,
				&drawingType.ProjectId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan elements: " + err.Error()})
				return
			}

			getDrawingType = append(getDrawingType, drawingType)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, getDrawingType)

		log := models.ActivityLog{
			EventContext: "Drawing Type",
			EventName:    "Get",
			Description:  "Retrieved all drawing types successfully",
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

// GetAllDrawingTypeByprojectid godoc
// @Summary      Get drawing types by project
// @Description  Get all drawing types for a project
// @Tags         drawing-types
// @Accept       json
// @Produce      json
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {array}   models.DrawingsType
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Failure      500         {object}  models.ErrorResponse
// @Router       /api/drawingtype/{project_id} [get]
func GetAllDrawingTypeByprojectid(db *sql.DB) gin.HandlerFunc {
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

		// Convert project_id to integer
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Query the drawing_type table filtered by project_id
		rows, err := db.Query(`
        SELECT 
            drawing_type_id,
            drawing_type_name,
            project_id
        FROM drawing_type
        WHERE project_id = $1`, projectID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawing types: " + err.Error()})
			return
		}
		defer rows.Close()

		var getDrawingType []models.DrawingsType
		for rows.Next() {
			var drawingType models.DrawingsType
			err := rows.Scan(
				&drawingType.DrawingsTypeId,
				&drawingType.DrawingTypeName,
				&drawingType.ProjectId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing types: " + err.Error()})
				return
			}

			getDrawingType = append(getDrawingType, drawingType)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
			return
		}

		// Return the filtered drawing types as JSON
		c.JSON(http.StatusOK, getDrawingType)

		log := models.ActivityLog{
			EventContext: "Drawing Type",
			EventName:    "Get",
			Description:  "Retrieved all drawing types for project ID " + strconv.Itoa(projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID, // Use the project ID for logging
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

// GetDrawingTypeByID godoc
// @Summary      Get drawing type by ID
// @Description  Get a single drawing type by ID (route has no /api prefix in main)
// @Tags         drawing-types
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Drawing type ID"
// @Success      200  {object}  models.DrawingsType
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /drawing-type/{id} [get]
func GetDrawingTypeByID(db *sql.DB) gin.HandlerFunc {
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
		if idStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "drawing_type_id is required"})
			return
		}

		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid drawing_type_id"})
			return
		}

		var drawingType models.DrawingsType
		err = db.QueryRow(`
		SELECT 
			drawing_type_id,
			drawing_type_name,
			project_id 
		FROM drawing_type 
		WHERE drawing_type_id = $1`, id).Scan(
			&drawingType.DrawingsTypeId,
			&drawingType.DrawingTypeName,
			&drawingType.ProjectId,
		)

		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Drawing type not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawing type: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, drawingType)
		log := models.ActivityLog{
			EventContext: "Drawing Type",
			EventName:    "Get",
			Description:  "Retrieved drawing type successfully with ID: " + idStr,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawingType.ProjectId, // Use the project ID from the retrieved drawing type
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

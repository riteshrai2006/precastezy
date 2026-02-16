package handlers

import (
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func CreateDrawingRevision(db *sql.DB) gin.HandlerFunc {
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

		var drawingsRev models.DrawingsRevision
		if err := c.ShouldBindJSON(&drawingsRev); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		drawingsRev.CreatedAt = time.Now()

		db := storage.GetDB()
		sqlStatement := `INSERT INTO drawings_revision ( 
	parent_drawing_id,
	project_id,
	version_number,
	element_type_id,
	file,
	version,
	created_at,
	created_by)
	 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING parent_drawing_id`
		err = db.QueryRow(sqlStatement,
			drawingsRev.ParentDrawingsId,
			drawingsRev.ProjectId,

			drawingsRev.File,
			drawingsRev.Version,
			drawingsRev.CreatedAt,
			drawingsRev.CreatedBy).Scan(&drawingsRev.ParentDrawingsId)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, drawingsRev)

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", drawingsRev.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", drawingsRev.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the drawing revision
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New drawing revision created for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingsRev.ProjectId),
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
		sendProjectNotifications(db, drawingsRev.ProjectId,
			fmt.Sprintf("New drawing revision created for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawingsRev.ProjectId))

		activityLog := models.ActivityLog{
			EventContext: "Drawing Revision",
			EventName:    "Create",
			Description:  "Created a new drawing revision" + strconv.Itoa(drawingsRev.ParentDrawingsId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawingsRev.ProjectId,
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

func UpdateDrawingRevision(db *sql.DB) gin.HandlerFunc {
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

		ParentDrawingsId, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid  ID"})
			return
		}

		updatedrawingRev := map[string]interface{}{}
		if err := c.ShouldBindJSON(&updatedrawingRev); err != nil || len(updatedrawingRev) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or empty input"})
			return
		}

		setClauses := []string{}
		args := []interface{}{}
		for key, value := range updatedrawingRev {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, len(args)+1))
			args = append(args, value)
		}
		args = append(args, ParentDrawingsId)

		query := fmt.Sprintf(`UPDATE drawings_revision SET %s WHERE parent_drawing_id = $%d RETURNING parent_drawing_id, project_id, version_number, element_type_id, file, version, created_at, created_by`,
			strings.Join(setClauses, ", "), len(args))
		row := storage.GetDB().QueryRow(query, args...)

		var updateDrawingRev models.DrawingsRevision
		if err := row.Scan(&updateDrawingRev.ParentDrawingsId, &updateDrawingRev.ProjectId, &updateDrawingRev.File, &updateDrawingRev.Version, &updateDrawingRev.CreatedAt, &updateDrawingRev.CreatedBy); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, updateDrawingRev)

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", updateDrawingRev.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", updateDrawingRev.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the drawing revision
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Drawing revision updated for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", updateDrawingRev.ProjectId),
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
		sendProjectNotifications(db, updateDrawingRev.ProjectId,
			fmt.Sprintf("Drawing revision updated for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", updateDrawingRev.ProjectId))

		activityLog := models.ActivityLog{
			EventContext: "Drawing Revision",
			EventName:    "Update",
			Description:  fmt.Sprintf("Updated drawing revision with ID %d", updateDrawingRev.ParentDrawingsId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    updateDrawingRev.ProjectId,
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

func DeleteDrawingRevision(db *sql.DB) gin.HandlerFunc {
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

		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid DrawingRev ID"})
			return
		}

		db := storage.GetDB()

		// Fetch drawing revision info before deletion for notifications
		var projectID int
		err = db.QueryRow("SELECT project_id FROM drawings_revision WHERE drawing_revision_id = $1", id).Scan(&projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Drawing revision not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revision details"})
			return
		}

		result, err := db.Exec("DELETE FROM drawings_revision WHERE drawing_revision_id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete drawingsRev"})
			return
		}

		if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "drawingsRev not found"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		c.JSON(http.StatusOK, gin.H{"message": "DrawingsRev successfully deleted"})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the drawing revision
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Drawing revision deleted from project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", projectID),
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
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Drawing revision deleted from project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", projectID))

		activityLog := models.ActivityLog{
			EventContext: "Drawing Revision",
			EventName:    "Delete",
			Description:  fmt.Sprintf("Deleted drawing revision with ID %d", id),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
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

// GetDrawingRevisionByProjectID returns drawing revisions for a project.
// @Summary Get drawing revisions by project ID
// @Description Returns drawing revisions for the given project_id. Requires Authorization header.
// @Tags Drawing Revisions
// @Accept json
// @Produce json
// @Param project_id query string true "Project ID"
// @Success 200 {array} models.DrawingsRevision
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_revision_fetch/{project_id} [get]
func GetDrawingRevisionByProjectID(c *gin.Context) {
	db := storage.GetDB()

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

	// Get project_id from the query parameters
	projectId := c.Query("project_id")
	if projectId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
		return
	}

	// Prepare the query with a WHERE clause for project_id
	rows, err := db.Query(`
    SELECT 
        parent_drawing_id,
        project_id,
        version,
        drawing_type_id,
        comments,
        file,
        drawing_revision_id,
        element_type_id 
    FROM drawing_type
    WHERE project_id = $1`, projectId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element: " + err.Error()})
		return
	}
	defer rows.Close()

	var GetAllDrawingRevision []models.DrawingsRevision
	for rows.Next() {
		var drawingRevision models.DrawingsRevision
		err := rows.Scan(
			&drawingRevision.ParentDrawingsId,
			&drawingRevision.ProjectId,
			&drawingRevision.Version,
			&drawingRevision.DrawingsTypeId,
			&drawingRevision.Comments,
			&drawingRevision.File,
			&drawingRevision.DrawingsRevisionId,
			&drawingRevision.ElementTypeID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan elements: " + err.Error()})
			return
		}

		GetAllDrawingRevision = append(GetAllDrawingRevision, drawingRevision)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
		return
	}

	// If no drawings are found, return a message
	if len(GetAllDrawingRevision) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No drawings found for this project."})
		return
	}

	c.JSON(http.StatusOK, GetAllDrawingRevision)
	log := models.ActivityLog{
		EventContext: "Drawing Revision",
		EventName:    "Get",
		Description:  fmt.Sprintf("Retrieved drawing revisions for project ID %s", projectId),
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

// GetDrawingRevisionByRevisionId returns a single drawing revision by ID.
// @Summary Get drawing revision by ID
// @Description Returns one drawing revision by drawing_revision_id. Requires Authorization header.
// @Tags Drawing Revisions
// @Accept json
// @Produce json
// @Param drawing_revision_id path int true "Drawing Revision ID"
// @Success 200 {object} models.DrawingsRevision
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_revision_get/{drawing_revision_id} [get]
func GetDrawingRevisionByRevisionId(c *gin.Context) {
	db := storage.GetDB()

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

	// Get drawing_revision_id from the query parameters and convert to int
	drawingRevisionIdStr := c.Query("drawing_revision_id")
	if drawingRevisionIdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Drawing Revision ID is required"})
		return
	}

	drawingRevisionId, err := strconv.Atoi(drawingRevisionIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Drawing Revision ID"})
		return
	}

	// Prepare the query with a WHERE clause for drawing_revision_id
	row := db.QueryRow(`
    SELECT 
        parent_drawing_id,
        project_id,
        version,
        drawing_type_id,
        comments,
        file,
        drawing_revision_id,
        element_type_id 
    FROM drawing_type
    WHERE drawing_revision_id = $1`, drawingRevisionId)

	var drawingRevision models.DrawingsRevision
	err = row.Scan(
		&drawingRevision.ParentDrawingsId,
		&drawingRevision.ProjectId,
		&drawingRevision.Version,
		&drawingRevision.DrawingsTypeId,
		&drawingRevision.Comments,
		&drawingRevision.File,
		&drawingRevision.DrawingsRevisionId,
		&drawingRevision.ElementTypeID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusOK, gin.H{"message": "No drawing found with the specified revision ID."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element: " + err.Error()})
		}
		return
	}

	// Return the found drawing revision
	c.JSON(http.StatusOK, drawingRevision)
	log := models.ActivityLog{
		EventContext: "Drawing Revision",
		EventName:    "Get",
		Description:  fmt.Sprintf("Retrieved drawing revision with ID %d", drawingRevisionId),
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    drawingRevision.ProjectId, // No specific project ID for this operation
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

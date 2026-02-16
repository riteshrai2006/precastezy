package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// UpdateDrawingHandler updates a drawing.
// @Summary Update drawing
// @Description Updates drawing. Body: Drawings model (element_type_id, drawing_type_id, project_id, file, comments, etc.). Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Param body body models.Drawings true "Drawing data"
// @Success 200 {object} models.MessageResponse "message: Drawing updated successfully"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_update [put]
func UpdateDrawingHandler(db *sql.DB) gin.HandlerFunc {
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

		var drawing models.Drawings

		// Bind JSON data to the drawing struct
		if err := c.ShouldBindJSON(&drawing); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		_, updateErr := UpdateDrawing(c, drawing)
		if updateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": updateErr.Error()})
			return
		}

		// Get project ID from the drawing (it's fetched in UpdateDrawing function)
		// We need to fetch it again to ensure we have the correct project ID
		var projectID int
		err = db.QueryRow(`
			SELECT project_id FROM drawings 
			WHERE element_type_id = $1 AND drawing_type_id = $2`,
			drawing.ElementTypeID, drawing.DrawingTypeId).Scan(&projectID)
		if err != nil {
			log.Printf("Failed to fetch project ID: %v", err)
			projectID = drawing.ProjectId // Fallback to the provided project ID
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Drawing updated successfully"})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the drawing
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Drawing updated for project: %s", projectName),
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
			fmt.Sprintf("Drawing updated for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", projectID))

		activityLog := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Update",
			Description:  "Update Drawing" + drawing.DrawingTypeName,
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

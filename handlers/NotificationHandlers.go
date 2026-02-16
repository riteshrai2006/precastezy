package handlers

import (
	"backend/models"
	"backend/services"
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func CreateNotificationHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		var notif models.Notification
		if err := c.BindJSON(&notif); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		notif.UserID = userID
		notif.Status = "unread"
		now := time.Now()
		notif.CreatedAt = now
		notif.UpdatedAt = now

		query := `
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		result, err := db.Exec(query, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert notification"})
			return
		}

		insertedID, _ := result.LastInsertId()
		notif.ID = int(insertedID)

		c.JSON(http.StatusOK, notif)
	}
}

// MarkNotificationAsReadHandler marks a notification as read.
// @Summary Mark notification as read
// @Description Marks notification by id as read. Requires Authorization header.
// @Tags Notifications
// @Accept json
// @Produce json
// @Param id path int true "Notification ID"
// @Success 200 {object} models.MessageResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/notifications/{id}/read [put]
func MarkNotificationAsReadHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		notifID := c.Param("id")

		_, err := db.Exec(`
			UPDATE notifications SET status = 'read', updated_at = $1 WHERE id = $2
		`, time.Now(), notifID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update notification"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Notification marked as read"})
	}
}

// MarkAllNotificationsAsReadHandler marks all notifications as read for the current user.
// @Summary Mark all notifications as read
// @Description Marks all notifications for the current user as read. Requires Authorization header.
// @Tags Notifications
// @Accept json
// @Produce json
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/notifications/read-all [put]
func MarkAllNotificationsAsReadHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is required"})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		// Update all notifications for this user to 'read' status
		result, err := db.Exec(`
			UPDATE notifications 
			SET status = 'read', updated_at = $1 
			WHERE user_id = $2 AND status = 'unread'
		`, time.Now(), userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update notifications"})
			return
		}

		// Get the number of rows affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get rows affected"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "All notifications marked as read",
			"rows_affected": rowsAffected,
		})
	}
}

func DeleteNotificationHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		notifID := c.Param("id")

		_, err := db.Exec("DELETE FROM notifications WHERE id = ?", notifID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notification"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Notification deleted"})
	}
}

// GetMyNotificationsHandler returns notifications for the current user.
// @Summary Get my notifications
// @Description Returns all notifications for the current user (from session). Requires Authorization header.
// @Tags Notifications
// @Accept json
// @Produce json
// @Success 200 {array} models.Notification
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/notifications [get]
func GetMyNotificationsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		rows, err := db.Query(`
			SELECT id, user_id, message, status, action, created_at, updated_at
			FROM notifications
			WHERE user_id = $1
			ORDER BY created_at DESC
		`, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notifications"})
			return
		}
		defer rows.Close()

		// Initialize slice to empty (ensures [] instead of null)
		notifications := []models.Notification{}

		for rows.Next() {
			var n models.Notification
			if err := rows.Scan(&n.ID, &n.UserID, &n.Message, &n.Status, &n.Action, &n.CreatedAt, &n.UpdatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning notification"})
				return
			}
			notifications = append(notifications, n)
		}

		c.JSON(http.StatusOK, notifications)
	}
}

// RegisterFCMTokenHandler handles FCM token registration/update
// RegisterFCMTokenHandler registers FCM token for push notifications.
// @Summary Register FCM token
// @Description Registers FCM token for the current user. Body: fcm_token. Requires Authorization header.
// @Tags Notifications
// @Accept json
// @Produce json
// @Param body body object true "fcm_token"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/fcm/register-token [post]
func RegisterFCMTokenHandler(db *sql.DB, fcmService *services.FCMService) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is required"})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		var request struct {
			Token string `json:"token" binding:"required"`
		}

		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request. Token is required."})
			return
		}

		if fcmService != nil {
			if err := fcmService.SaveFCMToken(userID, request.Token); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save FCM token: " + err.Error()})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "FCM token registered successfully"})
	}
}

// RemoveFCMTokenHandler handles FCM token removal
// RemoveFCMTokenHandler removes FCM token for the current user.
// @Summary Remove FCM token
// @Description Removes FCM token. Body: fcm_token. Requires Authorization header.
// @Tags Notifications
// @Accept json
// @Produce json
// @Param body body object true "fcm_token"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/fcm/remove-token [delete]
func RemoveFCMTokenHandler(db *sql.DB, fcmService *services.FCMService) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is required"})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		if fcmService != nil {
			if err := fcmService.RemoveFCMToken(userID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove FCM token: " + err.Error()})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "FCM token removed successfully"})
	}
}

// SendPushNotification is a helper function to send push notifications from any handler
func SendPushNotification(db *sql.DB, fcmService *services.FCMService, userID int, title, body string, data map[string]string, action string) {
	if fcmService == nil {
		log.Printf("FCM service is nil, cannot send push notification to user %d", userID)
		return
	}

	ctx := context.Background()
	err := fcmService.SendNotificationWithDB(ctx, userID, title, body, data, action)
	if err != nil {
		log.Printf("Error in SendPushNotification for user %d: %v", userID, err)
	}
}

// SendPushNotificationToUsers sends push notifications to multiple users
func SendPushNotificationToUsers(db *sql.DB, fcmService *services.FCMService, userIDs []int, title, body string, data map[string]string) {
	if fcmService == nil {
		log.Printf("FCM service is nil, cannot send push notifications to users: %v", userIDs)
		return
	}

	if len(userIDs) == 0 {
		log.Printf("No user IDs provided for push notification")
		return
	}

	ctx := context.Background()
	err := fcmService.SendNotificationToUsers(ctx, userIDs, title, body, data)
	if err != nil {
		log.Printf("Error in SendPushNotificationToUsers for users %v: %v", userIDs, err)
	}

	// Save notifications to database for each user
	for _, userID := range userIDs {
		_, err = db.Exec(`
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, 'unread', $3, NOW(), NOW())
		`, userID, body, data["action"])
		if err != nil {
			// Log error but continue
			_ = err
		}
	}
}

package handlers

import (
	"backend/services"
	"database/sql"
	"log"
)

// Global FCM service - will be set from main.go
var GlobalFCMService *services.FCMService

// SetFCMService sets the global FCM service
func SetFCMService(fcmService *services.FCMService) {
	GlobalFCMService = fcmService
}

// SendNotificationHelper is a convenience function to send push notifications
// This can be called from any handler without needing to pass fcmService
func SendNotificationHelper(db *sql.DB, userID int, title, body string, data map[string]string, action string) {
	SendPushNotification(db, GlobalFCMService, userID, title, body, data, action)
}

// SendNotificationToUsersHelper sends notifications to multiple users
func SendNotificationToUsersHelper(db *sql.DB, userIDs []int, title, body string, data map[string]string) {
	SendPushNotificationToUsers(db, GlobalFCMService, userIDs, title, body, data)
}

// SendNotificationToProjectMembers sends notifications to all members of a project
func SendNotificationToProjectMembers(db *sql.DB, projectID int, title, body string, data map[string]string) {
	rows, err := db.Query(`SELECT user_id FROM project_members WHERE project_id = $1`, projectID)
	if err != nil {
		log.Printf("Error fetching project members for notification: %v", err)
		return
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			log.Printf("Error scanning user ID: %v", err)
			continue
		}
		userIDs = append(userIDs, userID)
	}

	if len(userIDs) > 0 {
		SendNotificationToUsersHelper(db, userIDs, title, body, data)
	}
}

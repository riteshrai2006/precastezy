package handlers

import (
	"backend/models"
	"backend/storage"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/services"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"github.com/lib/pq"
)

// CreateMilestoneHandler godoc
// @Summary      Create milestone
// @Tags         milestones
// @Accept       json
// @Produce      json
// @Param        body  body  models.Milestone  true  "Milestone"
// @Success      201   {object}  models.Milestone
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/create_milestone [post]
func CreateMilestoneHandler(db *sql.DB) gin.HandlerFunc {
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

		var milestone models.Milestone

		if err := c.ShouldBindJSON(&milestone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		query := `INSERT INTO milestone (project_id, milestone_name, milestone_start_date, milestone_end_date, status) 
				  VALUES ($1, $2, $3, $4, $5) RETURNING id`
		err = db.QueryRow(query, milestone.ProjectID, milestone.MilestoneName, time.Now(), time.Now(), milestone.Status).Scan(&milestone.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create milestone", "details": err.Error()})
			return
		}

		// Get project members and admins to notify
		queryUsers := `
			SELECT DISTINCT u.id 
			FROM users u
			LEFT JOIN project_members pm ON u.id = pm.user_id
			WHERE pm.project_id = $1 OR u.is_admin = true
		`
		rows, err := db.Query(queryUsers, milestone.ProjectID)
		if err != nil {
			log.Printf("Error fetching users to notify: %v", err)
		} else {
			defer rows.Close()
			var userIDs []int
			for rows.Next() {
				var userID int
				if err := rows.Scan(&userID); err != nil {
					log.Printf("Error scanning user: %v", err)
					continue
				}
				userIDs = append(userIDs, userID)
			}

			// Send notifications to all collected users
			for _, userID := range userIDs {
				notif := models.Notification{
					UserID:    userID,
					Message:   fmt.Sprintf("New milestone '%s' created for project %d", milestone.MilestoneName, milestone.ProjectID),
					Status:    "unread",
					Action:    fmt.Sprintf("/api/projects/%d/milestones", milestone.ProjectID),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
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

		c.JSON(http.StatusCreated, milestone)

		log := models.ActivityLog{
			EventContext: "Milestone",
			EventName:    "POST",
			Description:  "Create Milestone",
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

// GetMilestoneHandler godoc
// @Summary      Get milestone by ID
// @Tags         milestones
// @Param        project_id  path  int  true  "Project ID"
// @Param        id         path  int  true  "Milestone ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_milestone/{project_id}/{id} [get]
func GetMilestoneHandler(db *sql.DB) gin.HandlerFunc {
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

		id := c.Param("id")
		projectID := c.Param("project_id")

		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
		}

		var milestone models.Milestone

		query := `SELECT id, project_id, milestone_name, milestone_start_date, milestone_end_date, status 
				  FROM milestone WHERE id = $1 AND project_id = $2`
		err = db.QueryRow(query, id, projectID).Scan(&milestone.ID, &milestone.ProjectID, &milestone.MilestoneName, &milestone.MilestoneStartDate, &milestone.MilestoneEndDate, &milestone.Status)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Milestone not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, milestone)

		log := models.ActivityLog{
			EventContext: "Milestone",
			EventName:    "Get",
			Description:  "Get Milestone",
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

// GetAllMilestonesHandler godoc
// @Summary      Get all milestones for project
// @Tags         milestones
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_milestones/{project_id} [get]
func GetAllMilestonesHandler(db *sql.DB) gin.HandlerFunc {
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

		projectID := c.Param("project_id")

		// Validate project ID
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		var milestones []models.Milestone

		// Query to fetch milestones for the specified project
		query := `SELECT id, project_id, milestone_name, milestone_start_date, milestone_end_date, status 
			FROM milestone WHERE project_id = $1`
		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch milestones"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var milestone models.Milestone
			if err := rows.Scan(&milestone.ID, &milestone.ProjectID, &milestone.MilestoneName, &milestone.MilestoneStartDate, &milestone.MilestoneEndDate, &milestone.Status); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan milestone"})
				return
			}
			milestones = append(milestones, milestone)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during rows iteration"})
			return
		}

		c.JSON(http.StatusOK, milestones)

		log := models.ActivityLog{
			EventContext: "Milestone",
			EventName:    "Get",
			Description:  "Get All Milestone",
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

// UpdateMilestoneHandler godoc
// @Summary      Update milestone
// @Tags         milestones
// @Param        id    path  int  true  "Milestone ID"
// @Param        body  body  models.Milestone  true  "Milestone"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_milestone/{id} [put]
func UpdateMilestoneHandler(db *sql.DB) gin.HandlerFunc {
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

		id := c.Param("id")
		var milestone models.Milestone

		if err := c.ShouldBindJSON(&milestone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		query := `UPDATE milestone SET project_id = $1, milestone_name = $2, milestone_start_date = $3, milestone_end_date = $4, status = $5 
				  WHERE id = $6`
		_, err = db.Exec(query, milestone.ProjectID, milestone.MilestoneName, milestone.MilestoneStartDate, milestone.MilestoneEndDate, milestone.Status, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update milestone"})
			return
		}

		// Get project members and admins to notify
		queryUsers := `
			SELECT DISTINCT u.id 
			FROM users u
			LEFT JOIN project_members pm ON u.id = pm.user_id
			WHERE pm.project_id = $1 OR u.is_admin = true
		`
		rows, err := db.Query(queryUsers, milestone.ProjectID)
		if err != nil {
			log.Printf("Error fetching users to notify: %v", err)
		} else {
			defer rows.Close()
			var userIDs []int
			for rows.Next() {
				var userID int
				if err := rows.Scan(&userID); err != nil {
					log.Printf("Error scanning user: %v", err)
					continue
				}
				userIDs = append(userIDs, userID)
			}

			// Send notifications to all collected users
			for _, userID := range userIDs {
				notif := models.Notification{
					UserID:    userID,
					Message:   fmt.Sprintf("Milestone '%s' updated for project %d", milestone.MilestoneName, milestone.ProjectID),
					Status:    "unread",
					Action:    fmt.Sprintf("/api/projects/%d/milestones", milestone.ProjectID),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
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

		c.JSON(http.StatusOK, gin.H{"message": "Milestone updated successfully"})

		log := models.ActivityLog{
			EventContext: "Milestone",
			EventName:    "PUT",
			Description:  "Update Milestone Handler",
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

// DeleteMilestoneHandler godoc
// @Summary      Delete milestone
// @Tags         milestones
// @Param        id  path  int  true  "Milestone ID"
// @Success      200  {object}  object
// @Failure      401  {object}  object
// @Router       /api/delete_milestone/{id} [delete]
func DeleteMilestoneHandler(db *sql.DB) gin.HandlerFunc {
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

		id := c.Param("id")

		// Get milestone details before deletion for notification
		var milestone models.Milestone
		err = db.QueryRow(`SELECT project_id, milestone_name FROM milestone WHERE id = $1`, id).Scan(&milestone.ProjectID, &milestone.MilestoneName)
		if err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch milestone details"})
			return
		}

		// Get project members and admins to notify
		queryUsers := `
			SELECT DISTINCT u.id 
			FROM users u
			LEFT JOIN project_members pm ON u.id = pm.user_id
			WHERE pm.project_id = $1 OR u.is_admin = true
		`
		rows, err := db.Query(queryUsers, milestone.ProjectID)
		if err != nil {
			log.Printf("Error fetching users to notify: %v", err)
		} else {
			defer rows.Close()
			var userIDs []int
			for rows.Next() {
				var userID int
				if err := rows.Scan(&userID); err != nil {
					log.Printf("Error scanning user: %v", err)
					continue
				}
				userIDs = append(userIDs, userID)
			}

			// Send notifications to all collected users
			for _, userID := range userIDs {
				notif := models.Notification{
					UserID:    userID,
					Message:   fmt.Sprintf("Milestone '%s' deleted from project %d", milestone.MilestoneName, milestone.ProjectID),
					Status:    "unread",
					Action:    fmt.Sprintf("/api/projects/%d/milestones", milestone.ProjectID),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
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

		query := `DELETE FROM milestone WHERE id = $1`
		_, err = db.Exec(query, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete milestone"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Milestone deleted successfully"})

		log := models.ActivityLog{
			EventContext: "Delete",
			EventName:    "Delete",
			Description:  "Delete Milestone",
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

// Task Type ------------------------------------------------------------------------------------------------------------------------------
// -------------------------------------------------------------------------------------------------------------------------------------------------
// --------------------------------------------------------------------------------------------------------------------------------------------------

// GetAllTaskTypesHandler godoc
// @Summary      Get all task types for project
// @Tags         task-types
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  models.TaskType
// @Failure      400  {object}  object
// @Router       /api/get_tasktypes/{project_id} [get]
func GetAllTaskTypesHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("project_id") // Retrieve the project_id from the URL parameter

		// Validate project_id (you can extend validation if necessary)
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		var taskTypes []models.TaskType

		query := `SELECT id, project_id, name, color_code FROM task_type WHERE project_id = $1`
		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task types"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var taskType models.TaskType
			if err := rows.Scan(&taskType.ID, &taskType.ProjectID, &taskType.Name, &taskType.ColorCode); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan task type"})
				return
			}
			taskTypes = append(taskTypes, taskType)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during rows iteration"})
			return
		}

		c.JSON(http.StatusOK, taskTypes)
	}
}

// GetTaskTypeHandler godoc
// @Summary      Get task type by ID
// @Tags         task-types
// @Param        project_id  path  int  true  "Project ID"
// @Param        id          path  int  true  "Task type ID"
// @Success      200  {object}  models.TaskType
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/get_tasktype/{project_id}/{id} [get]
func GetTaskTypeHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		projectID := c.Param("project_id") // Retrieve the project_id from the URL parameter

		// Validate project_id (you can extend validation if necessary)
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}

		var taskType models.TaskType
		query := `SELECT id, project_id, name, color_code FROM task_type WHERE id = $1 AND project_id = $2`
		if err := db.QueryRow(query, id, projectID).Scan(&taskType.ID, &taskType.ProjectID, &taskType.Name, &taskType.ColorCode); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task type"})
			}
			return
		}

		c.JSON(http.StatusOK, taskType)
	}
}

// CreateTaskTypeHandler godoc
// @Summary      Create task type
// @Tags         task-types
// @Accept       json
// @Produce      json
// @Param        body  body  models.TaskType  true  "Task type"
// @Success      201   {object}  models.TaskType
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/create_tasktype/ [post]
func CreateTaskTypeHandler(db *sql.DB) gin.HandlerFunc {
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
		var taskType models.TaskType

		if err := c.ShouldBindJSON(&taskType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
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

		query := `INSERT INTO task_type (project_id, name, color_code) VALUES ($1, $2, $3) RETURNING id`
		if err := db.QueryRow(query, taskType.ProjectID, taskType.Name, taskType.ColorCode).Scan(&taskType.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to create task type",
				"details": err.Error(), // Include detailed error message
			})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New task type created: %s", taskType.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d/task-types", taskType.ProjectID),
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

		c.JSON(http.StatusCreated, taskType)

		log := models.ActivityLog{
			EventContext: "Tags",
			EventName:    "Create",
			Description:  "Create Task Type",
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

// UpdateTaskTypeHandler godoc
// @Summary      Update task type
// @Tags         task-types
// @Param        id    path  int  true  "Task type ID"
// @Param        body  body  models.TaskType  true  "Task type"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_tasktype/{id} [put]
func UpdateTaskTypeHandler(db *sql.DB) gin.HandlerFunc {
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
		var taskType models.TaskType

		if err := c.ShouldBindJSON(&taskType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		query := `UPDATE task_type SET project_id = $1, name = $2, color_code = $3 WHERE id = $4`
		if _, err := db.Exec(query, taskType.ProjectID, taskType.Name, taskType.ColorCode, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task type", "detail": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Task type updated: %s", taskType.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d/task-types", taskType.ProjectID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Task type updated successfully"})

		log := models.ActivityLog{
			EventContext: "Tags",
			EventName:    "PUT",
			Description:  fmt.Sprintf("Update Task Type %s", id),
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

// DeleteTaskTypeHandler godoc
// @Summary      Delete task type
// @Tags         task-types
// @Param        id  path  int  true  "Task type ID"
// @Success      200  {object}  object
// @Failure      401  {object}  object
// @Router       /api/delete_tasktype/{id} [delete]
func DeleteTaskTypeHandler(db *sql.DB) gin.HandlerFunc {
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

		// Get task type name before deleting
		var taskTypeName string
		var projectID int
		err = db.QueryRow(`SELECT name, project_id FROM task_type WHERE id = $1`, id).Scan(&taskTypeName, &projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task type", "details": err.Error()})
			}
			return
		}

		query := `DELETE FROM task_type WHERE id = $1`
		if _, err := db.Exec(query, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task type"})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Task type deleted: %s", taskTypeName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d/task-types", projectID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Task type deleted successfully"})

		log := models.ActivityLog{
			EventContext: "Tags",
			EventName:    "DELETE",
			Description:  "Delete Task Type",
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

//

// TASK ----------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------
// -------------------------------------------------------------------------------------------------------------

// GetAllTasks godoc
// @Summary      Get all tasks for project
// @Tags         tasks
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Router       /api/get_alltasks/{project_id} [get]
func GetAllTasks(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("project_id") // Retrieve the project_id from the URL parameter

		// Validate project_id (you can extend validation if necessary)
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}

		// Modify query to fetch tasks for the given project_id
		query := `
			SELECT task_id, project_id, stage_id, name, description, priority,
				   file_attachments, assigned_to, estimated_effort_in_hrs, start_date, end_date,
				   status, color_code, task_type_id
			FROM task WHERE project_id = $1`
		rows, err := db.Query(query, projectID) // Pass project_id as parameter to filter tasks by project
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching tasks", "details": err.Error()})
			return
		}
		defer rows.Close()

		var tasks []models.Task

		for rows.Next() {
			var task models.Task
			var fileAttachments string

			err := rows.Scan(
				&task.TaskID, &task.ProjectID, &task.StageID, &task.Name, &task.Desc, &task.Priority,
				&fileAttachments, &task.AssignedTo, &task.EstimatedEffortInHrs, &task.StartDate,
				&task.EndDate, &task.Status, &task.ColorCode, &task.TaskTypeId,
			)
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
				return
			} else if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching task details", "details": err.Error()})
				return
			}

			// Deserialize CSV arrays for file attachments
			task.FileAttachments = strings.Split(fileAttachments, ",")

			// Fetch only members assigned to the task at the current stage
			if task.AssignedTo > 0 {
				var user models.User
				var firstAccess, lastAccess sql.NullTime
				var employeeID sql.NullString // To handle employee_id being NULL
				userQuery := `
				SELECT 
				u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name, 
				u.created_at, u.updated_at, u.first_access, u.last_access, 
				u.profile_picture, u.is_admin, u.address, u.city, u.state, 
				u.country, u.zip_code, u.phone_no, u.role_id, r.role_name
				FROM users u
				JOIN roles r ON u.role_id = r.role_id
				WHERE u.id = $1`
				err = db.QueryRow(userQuery, task.AssignedTo).Scan(
					&user.ID,
					&employeeID,
					&user.Email,
					&user.Password,
					&user.FirstName,
					&user.LastName,
					&user.CreatedAt,
					&user.UpdatedAt,
					&firstAccess,
					&lastAccess,
					&user.ProfilePic,
					&user.IsAdmin,
					&user.Address,
					&user.City,
					&user.State,
					&user.Country,
					&user.ZipCode,
					&user.PhoneNo,
					&user.RoleID,
					&user.RoleName)
				if err != nil && err != sql.ErrNoRows {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching user details", "details": err.Error()})
					return
				}

				// Handle sql.NullTime for FirstAccess and LastAccess
				user.FirstAccess = firstAccess.Time
				if !firstAccess.Valid {
					user.FirstAccess = time.Time{} // Zero value of time.Time
				}

				user.LastAccess = lastAccess.Time
				if !lastAccess.Valid {
					user.LastAccess = time.Time{} // Zero value of time.Time
				}

				// Handle sql.NullString for EmployeeID
				if employeeID.Valid {
					user.EmployeeId = employeeID.String
				} else {
					user.EmployeeId = "" // Do not include EmployeeId if it is NULL
				}

				// task.AssignedToUsers = append(task.AssignedToUsers, user)
			}

			// Append task to tasks slice
			tasks = append(tasks, task)
		}

		// Handle any iteration error
		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating tasks", "details": err.Error()})
			return
		}

		// Directly return the array of tasks (without an outer object)
		c.JSON(http.StatusOK, tasks)
	}
}

// GetTaskHandler godoc
// @Summary      Get task by ID
// @Tags         tasks
// @Param        project_id  path  int  true  "Project ID"
// @Param        task_id     path  int  true  "Task ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/get_task/{project_id}/{task_id} [get]
func GetTaskHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("project_id")
		taskID := c.Param("task_id")

		// Convert taskID and projectID to integers
		taskIDInt, err := strconv.Atoi(taskID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID format"})
			return
		}
		projectIDInt, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID format"})
			return
		}

		var task models.Task
		var fileAttachments string
		var blockedBy sql.NullString

		query := `
			SELECT task_id, project_id, stage_id, name, description, priority,
				   file_attachments, assigned_to, estimated_effort_in_hrs, start_date, end_date,
				   status, color_code, blocked_by, task_type_id
			FROM task WHERE task_id = $1 AND project_id = $2`
		err = db.QueryRow(query, taskIDInt, projectIDInt).Scan(
			&task.TaskID, &task.ProjectID, &task.StageID, &task.Name, &task.Desc, &task.Priority,
			&fileAttachments, &task.AssignedTo, &task.EstimatedEffortInHrs, &task.StartDate,
			&task.EndDate, &task.Status, &task.ColorCode, &blockedBy, &task.TaskTypeId,
		)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching task details", "details": err.Error()})
			return
		}

		// Deserialize fileAttachments
		task.FileAttachments = strings.Split(fileAttachments, ",")

		// Fetch assigned user details
		var user models.User
		var firstAccess, lastAccess sql.NullTime
		var employeeID sql.NullString // To handle employee_id being NULL
		userQuery := `
	SELECT 
			u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name, 
			u.created_at, u.updated_at, u.first_access, u.last_access, 
			u.profile_picture, u.is_admin, u.address, u.city, u.state, 
			u.country, u.zip_code, u.phone_no, u.role_id, r.role_name
	FROM users u
	JOIN roles r ON u.role_id = r.role_id
	WHERE u.id = $1`
		err = db.QueryRow(userQuery, task.AssignedTo).Scan(
			&user.ID,
			&employeeID,
			&user.Email,
			&user.Password,
			&user.FirstName,
			&user.LastName,
			&user.CreatedAt,
			&user.UpdatedAt,
			&firstAccess,
			&lastAccess,
			&user.ProfilePic,
			&user.IsAdmin,
			&user.Address,
			&user.City,
			&user.State,
			&user.Country,
			&user.ZipCode,
			&user.PhoneNo,
			&user.RoleID,
			&user.RoleName)
		if err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching user details", "details": err.Error()})
			return
		}

		// Handle sql.NullTime for FirstAccess and LastAccess
		user.FirstAccess = firstAccess.Time
		if !firstAccess.Valid {
			user.FirstAccess = time.Time{} // Zero value of time.Time
		}

		user.LastAccess = lastAccess.Time
		if !lastAccess.Valid {
			user.LastAccess = time.Time{} // Zero value of time.Time
		}

		// Handle sql.NullString for EmployeeID
		if employeeID.Valid {
			user.EmployeeId = employeeID.String
		} else {
			user.EmployeeId = "" // Do not include EmployeeId if it is NULL
		}

		// task.AssignedToUsers = append(task.AssignedToUsers, user)
		// Get activity count
		activityCountQuery := `SELECT COUNT(*) FROM activity WHERE task_id = $1`
		err = db.QueryRow(activityCountQuery, task.TaskID).Scan(&task.Activities)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error counting activities for task", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, task)
	}
}

// CreateTaskHandler godoc
// @Summary      Create task
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Task"
// @Success      201   {object}  object
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/create_task/ [post]
func CreateTaskHandler(db *sql.DB) gin.HandlerFunc {
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

		var input struct {
			ProjectID            int                `json:"project_id"`
			TaskTypeId           int                `json:"task_type_id"`
			Name                 string             `json:"name"`
			StageID              int                `json:"stage_id"`
			Desc                 string             `json:"description"`
			Priority             string             `json:"priority"`
			AssignedTo           int                `json:"assigned_to"`
			EstimatedEffortInHrs int                `json:"estimated_effort_in_hrs"`
			StartDate            models.DateOnly    `json:"start_date"`
			EndDate              models.DateOnly    `json:"end_date"`
			Status               string             `json:"status"`
			ColorCode            string             `json:"color_code"`
			Selection            map[int][]struct { // Maps floor_id to an array of element tasks
				Quantity      int   `json:"quantity"`
				ElementTypeID int   `json:"element_type_id"`
				StockYardID   int   `json:"stockyard_id"`
				Billable      *bool `json:"billable"`
			} `json:"Selection"`
		}

		// Bind JSON input
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}
		defer tx.Rollback()

		createdTaskIDs := []int{}

		// Iterate over `Selection` map
		for floorID, tasks := range input.Selection {
			for _, task := range tasks {
				// Fetch `assigned_to`, `qc_assign`, `qc_id`, and `paper_id` from `project_stages`
				var assignedTo int
				var qcAssign bool
				var qcID, paperID *int
				var stageName string

				// 2. Fetch the stage path using the `element_type_id`
				var stagePath string
				err = tx.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, task.ElementTypeID).Scan(&stagePath)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stage path", "details": err.Error()})
					return
				}

				// 3. Convert `stage_path` to an array
				stagePath = strings.Trim(stagePath, "{}") // Remove braces
				stagePathArray := strings.Split(stagePath, ",")

				// 3. Check if `stagePathArray` is not empty
				if len(stagePathArray) == 0 {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Stage path is empty"})
					return
				}

				// 4. Convert the first element to `stage_id`
				stageID, err := strconv.Atoi(strings.TrimSpace(stagePathArray[0])) // Trim spaces and convert
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid stage ID format", "details": err.Error()})
					return
				}

				log.Print(stageID)

				queryStage := `SELECT assigned_to, qc_assign, qc_id, paper_id, name FROM project_stages WHERE id = $1`
				err = tx.QueryRow(queryStage, stageID).Scan(&assignedTo, &qcAssign, &qcID, &paperID, &stageName)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "Stage not found or no assigned members"})
					return
				}

				// Insert task
				queryInsertTask := `
					INSERT INTO task (project_id, task_type_id, name, stage_id, description, priority, 
					   assigned_to, estimated_effort_in_hrs, start_date, end_date, status, 
					   color_code, element_type_id, floor_id) 
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) 
					RETURNING task_id
				`
				var taskID int
				err = tx.QueryRow(queryInsertTask,
					input.ProjectID, // Assuming project_id is 0 or needs to be retrieved elsewhere
					input.TaskTypeId, input.Name, stageID,
					input.Desc, input.Priority, input.AssignedTo, input.EstimatedEffortInHrs,
					input.StartDate.ToTime(), input.EndDate.ToTime(), input.Status, input.ColorCode,
					task.ElementTypeID, floorID).Scan(&taskID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task", "details": err.Error()})
					return
				}

				createdTaskIDs = append(createdTaskIDs, taskID)

				// Fetch and update `left_quantity`
				queryFetchQuantities := `SELECT quantity, left_quantity FROM element_type_hierarchy_quantity WHERE element_type_id = $1 AND hierarchy_id= $2`
				var totalQuantity, leftQuantity int
				err = tx.QueryRow(queryFetchQuantities, task.ElementTypeID, floorID).Scan(&totalQuantity, &leftQuantity)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch quantity details", "details": err.Error()})
					return
				}

				// Compute new `left_quantity`
				newLeftQuantity := leftQuantity + task.Quantity

				queryUpdateLeftQuantity := `UPDATE element_type_hierarchy_quantity SET left_quantity = $1 WHERE element_type_id = $2 AND hierarchy_id = $3`
				_, err = tx.Exec(queryUpdateLeftQuantity, newLeftQuantity, task.ElementTypeID, floorID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update left quantity", "details": err.Error()})
					return
				}

				// Fetch `quantity` number of elements from `element` table
				queryElements := `
					SELECT id, element_name FROM element 
					WHERE element_type_id = $1 AND target_location = $2 AND instage = FALSE 
					ORDER BY id ASC LIMIT $3`

				rows, err := tx.Query(queryElements, task.ElementTypeID, floorID, task.Quantity)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch elements", "details": err.Error()})
					return
				}
				defer rows.Close()

				var selectedElements []struct {
					ID   int
					Name string
				}

				for rows.Next() {
					var element struct {
						ID   int
						Name string
					}
					if err := rows.Scan(&element.ID, &element.Name); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading elements", "details": err.Error()})
						return
					}
					selectedElements = append(selectedElements, element)
				}

				// Ensure enough elements are available
				if len(selectedElements) < task.Quantity {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Not enough available elements"})
					return
				}

				var pname string
				err = db.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, stageID).Scan(&pname)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
					return
				}

				billable := true
				if task.Billable != nil {
					billable = *task.Billable
				}

				queryUpdateInStage := `
							UPDATE element 
							SET instage = TRUE,
    						status = $1,
    						billable = $2
							WHERE id = ANY($3)
						`
				_, err = tx.Exec(queryUpdateInStage, pname, billable, pq.Array(getElementIDs(selectedElements)))
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update elements", "details": err.Error()})
					return
				}

				// Insert activities for selected elements
				queryInsertActivity := `
					INSERT INTO activity (task_id, project_id, name, stage_id, status, element_id, assigned_to, start_date, end_date, priority, qc_id, paper_id, stockyard_id)
					VALUES ($1, $2, $3, $4, 'Inprogress', $5, $6, $7, $8, $9, $10, $11, $12)`

				for _, element := range selectedElements {
					_, err := tx.Exec(queryInsertActivity,
						taskID,
						input.ProjectID, // Assuming project_id is 0
						element.Name,
						stageID,
						element.ID,
						assignedTo,
						input.StartDate.ToTime(),
						input.EndDate.ToTime(),
						input.Priority,
						qcID,
						paperID,
						task.StockYardID,
					)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create activity", "details": err.Error()})
						return
					}

					// Get the last inserted activity ID
					var activityID int
					err = tx.QueryRow(
						`SELECT id FROM activity 
	 					WHERE task_id = $1 AND element_id = $2 
	 					ORDER BY id DESC LIMIT 1`,
						taskID, element.ID).Scan(&activityID)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activity ID", "details": err.Error()})
						return
					}

					// Insert into complete_production
					queryInsertCompleteProduction := `
						INSERT INTO complete_production (
            task_id, activity_id, project_id, element_id, element_type_id, floor_id,
            stage_id, user_id, started_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
						`

					_, err = tx.Exec(queryInsertCompleteProduction,
						taskID,
						activityID,
						input.ProjectID,
						element.ID,
						task.ElementTypeID,
						floorID,
						stageID,
						assignedTo, // user_id
						time.Now(),
					)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert complete_production record", "details": err.Error()})
						return
					}

				}

			}
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    input.AssignedTo,
			Message:   fmt.Sprintf("New task created for project %d and name: %s", input.ProjectID, input.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", input.ProjectID), // example route for frontend
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

		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", input.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", input.ProjectID)
		}

		// Send push notification to the user who created the project
		log.Printf("Attempting to send push notification to user %d for project creation", input.AssignedTo)
		SendNotificationHelper(db, input.AssignedTo,
			"Task Created",
			fmt.Sprintf("New project created: %s", input.Name),
			map[string]string{
				"project_name": projectName,
				"task_name":    input.Name,
				"action":       "task_created",
			},
			"project_created")

		// Commit the transaction if all tasks are created successfully
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Tasks created successfully", "task_ids": createdTaskIDs})

		log := models.ActivityLog{
			EventContext: "Task",
			EventName:    "Create",
			Description:  "Create Task Handler",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    input.ProjectID,
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

// Helper function to extract IDs from selected elements
func getElementIDs(elements []struct {
	ID   int
	Name string
}) []int {
	ids := make([]int, len(elements))
	for i, element := range elements {
		ids[i] = element.ID
	}
	return ids
}

// GetAllListTask godoc
// @Summary      Get all list tasks for project
// @Tags         tasks
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_alllist_tasks/{project_id} [get]
func GetAllListTask(db *sql.DB) gin.HandlerFunc {
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

		// Retrieve project_id from query parameters or some other source
		projectID := c.Param("project_id")

		// Query for tasks related to the provided project_id
		queryTasks := `
            SELECT task_id, project_id, task_type_id, name, stage_id, description, priority, 
                   file_attachments, assigned_to, estimated_effort_in_hrs, start_date, 
                   end_date, status, color_code, element_type_id, floor_id
            FROM task 
            WHERE project_id = $1`

		rows, err := db.Query(queryTasks, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tasks", "details": err.Error()})
			return
		}
		defer rows.Close()

		var tasks []models.Task
		for rows.Next() {
			var task models.Task

			err := rows.Scan(
				&task.TaskID,
				&task.ProjectID,
				&task.TaskTypeId,
				&task.Name,
				&task.StageID,
				&task.Desc,
				&task.Priority,
				pq.Array(&task.FileAttachments),
				&task.AssignedTo,
				&task.EstimatedEffortInHrs,
				&task.StartDate,
				&task.EndDate,
				&task.Status,
				&task.ColorCode,
				&task.ElementTypeID,
				&task.FloorID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading tasks", "details": err.Error()})
				return
			}

			tasks = append(tasks, task)
		}

		// For each task, fetch its activities
		for i, task := range tasks {
			queryActivities := `
                SELECT id, task_id, project_id, name, priority, stage_id, assigned_to, start_date, 
				   end_date, status, element_id, qc_id, paper_id, qc_status, mesh_mold_status , reinforcement_status, concrete_status
			FROM activity 
                WHERE task_id = $1`

			activityRows, err := db.Query(queryActivities, task.TaskID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
				return
			}
			defer activityRows.Close()

			var activities []models.Activity
			for activityRows.Next() {
				var activity models.Activity
				var paperID sql.NullInt64
				var qcID sql.NullInt64
				var qcStatus sql.NullString

				err := activityRows.Scan(
					&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &activity.Priority, &activity.StageID,
					&activity.AssignedTo, &activity.StartDate, &activity.EndDate, &activity.Status, &activity.ElementID,
					&qcID, &paperID, &qcStatus, &activity.MeshMoldStatus, &activity.ReinforcementStatus,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading activities", "details": err.Error()})
					return
				}

				// Check for NULL values before assigning
				if qcID.Valid {
					activity.QCID = int(qcID.Int64)
				} else {
					activity.QCID = 0 // or another default value
				}

				if qcStatus.Valid {
					activity.QCStatus = qcStatus.String
				} else {
					activity.QCStatus = "" // or another default value
				}

				activities = append(activities, activity)
			}

			// Assign the activities to the task
			tasks[i].Activities = activities
		}

		// Return the tasks along with their activities
		c.JSON(http.StatusOK, tasks)

		projectId, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		log := models.ActivityLog{
			EventContext: "Task",
			EventName:    "Get",
			Description:  "Get All List Task",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// UpdateTaskHandler godoc
// @Summary      Update task
// @Tags         tasks
// @Param        id    path  int  true  "Task ID"
// @Param        body  body  object  true  "Task"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_task/{id} [put]
func UpdateTaskHandler(db *sql.DB) gin.HandlerFunc {
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

		var task models.Task

		// Bind JSON input to task model
		if err := c.ShouldBindJSON(&task); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Get task ID from URL param
		taskID := c.Param("task_id")

		// Update the task's core data
		query := `
			UPDATE task SET
				project_id = $1,
				task_type_id = $2,
				name = $3,
				description = $4,
				priority = $5,
				file_attachments = $6,
				estimated_effort_in_hrs = $7,
				start_date = $8,
				end_date = $9,
				status = $10,
				color_code = $11,
				blocked_by = $12,
				stage_id = $13,
				target_location = $14
			WHERE task_id = $15
		`
		_, err = db.Exec(query,
			task.ProjectID,
			task.TaskTypeId,
			task.Name,
			task.Desc,
			task.Priority,
			pq.Array(task.FileAttachments),
			task.EstimatedEffortInHrs,
			task.StartDate.ToTime(),
			task.EndDate.ToTime(),
			task.Status,
			task.ColorCode,
			task.StageID,
			taskID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task", "details": err.Error()})
			return
		}

		// Update the `AssignedTo` field (members assigned to this task)
		// First, delete existing assignments
		deleteAssignedQuery := `
			DELETE FROM task_assigned_to WHERE task_id = $1
		`
		_, err = db.Exec(deleteAssignedQuery, taskID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing assignments", "details": err.Error()})
			return
		}

		// Delete existing activities related to this task
		deleteActivitiesQuery := `
			DELETE FROM activity WHERE task_id = $1
		`
		_, err = db.Exec(deleteActivitiesQuery, taskID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing activities", "details": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Task updated: %s", task.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d/tasks/%s", task.ProjectID, taskID),
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

		// Return the updated task as JSON response
		c.JSON(http.StatusOK, gin.H{
			"message": "Task updated successfully",
			"task_id": taskID,
			// "num_activities": len(elements),
		})

		// Send push notification to assigned user
		// Get element type name
		var elementTypeName string
		err = db.QueryRow(`SELECT element_type FROM element_type WHERE element_type_id = $1`, task.ElementTypeID).Scan(&elementTypeName)
		if err != nil {
			log.Printf("Error fetching element type name: %v", err)
			return
		}

		log := models.ActivityLog{
			EventContext: "Task",
			EventName:    "PUT",
			Description:  fmt.Sprintf("Update Task %d", task.TaskID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    task.ProjectID,
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

// DeleteTaskHandler godoc
// @Summary      Delete task
// @Tags         tasks
// @Param        id  path  int  true  "Task ID"
// @Success      200  {object}  object
// @Failure      401  {object}  object
// @Router       /api/delete_task/{id} [delete]
func DeleteTaskHandler(db *sql.DB) gin.HandlerFunc {
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

		taskID := c.Param("id")

		// Convert taskID to an integer
		id, err := strconv.Atoi(taskID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID format"})
			return
		}

		// Get task name and project_id before deleting
		var taskName string
		var projectID int
		err = db.QueryRow(`SELECT name, project_id FROM task WHERE task_id = $1`, id).Scan(&taskName, &projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task", "details": err.Error()})
			}
			return
		}

		// Begin a transaction to ensure consistency
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}
		defer tx.Rollback() // Ensure the transaction is rolled back in case of an error

		// Delete the assigned_to records first
		_, err = tx.Exec("DELETE FROM task_assigned_to WHERE task_id = $1", taskID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete assigned members"})
			return
		}

		// Get all activity IDs related to the task
		rows, err := tx.Query("SELECT activity_id FROM activity WHERE task_id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve related activities", "details": err.Error()})
			return
		}
		defer rows.Close()

		var activityIDs []int
		for rows.Next() {
			var activityID int
			if err := rows.Scan(&activityID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan activity IDs", "details": err.Error()})
				return
			}
			activityIDs = append(activityIDs, activityID)
		}

		// Delete activity_assigned_to records for each related activity
		for _, activityID := range activityIDs {
			_, err = tx.Exec("DELETE FROM activity_assigned_to WHERE activity_id = $1", activityID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete activity assignments", "details": err.Error()})
				return
			}
		}

		// Delete all related activities
		_, err = tx.Exec(`DELETE FROM activity WHERE task_id = $1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related activities", "details": err.Error()})
			return
		}

		// Now delete the task
		_, err = tx.Exec(`DELETE FROM task WHERE task_id = $1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task", "details": err.Error()})
			return
		}

		// Commit the transaction if everything goes well
		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Task deleted: %s", taskName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d/tasks", projectID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Task, related activities, and assignments deleted successfully"})

		log := models.ActivityLog{
			EventContext: "Task",
			EventName:    "Delete",
			Description:  fmt.Sprintf("Delete Task and Activities %d", id),
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

// GetActivityHandlerByAssignee godoc
// @Summary      Get activity by assignee (project)
// @Tags         activity
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_activity/{project_id} [get]
func GetActivityHandlerByAssignee(db *sql.DB) gin.HandlerFunc {
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

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}
		userID := user.ID

		projectIDParam := c.Param("project_id")
		if projectIDParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		projectID, err := strconv.Atoi(projectIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID", "details": err.Error()})
			return
		}

		// Fetch activities with stage names
		query := `
			SELECT 
    a.id, 
    a.task_id, 
    a.project_id, 
    a.name, 
    a.priority, 
    a.stage_id, 
    s.name AS stage_name, 
    a.assigned_to, 
    a.start_date, 
    a.end_date, 
    a.status, 
    a.element_id, 
    a.qc_id, 
    a.paper_id, 
    a.qc_status, 
    a.mesh_mold_status, 
    a.reinforcement_status,
    t.element_type_id, 
    e.element_name
FROM activity a
LEFT JOIN task t ON a.task_id = t.task_id
LEFT JOIN project_stages s ON a.stage_id = s.id
LEFT JOIN element e ON a.element_id = e.id
WHERE 
    a.project_id = $1
	AND a.completed = false
    AND (
        a.assigned_to = $2

        OR (
            a.qc_id = $2
            AND s.name NOT IN ('Mesh & Mould', 'Reinforcement')
            AND a.status = 'completed'
			AND a.paper_id IS NOT NULL
        )

        OR (
            a.qc_id = $2
            AND s.name IN ('Mesh & Mould')
            AND (a.mesh_mold_status = 'completed' )
            AND a.paper_id IS NOT NULL
        )

		OR (
			a.qc_id = $2
			AND s.name IN ('Reinforcement')
			AND a.reinforcement_status = 'completed'
			AND a.paper_id IS NOT NULL
		)

        OR (
            s.name IN ('Mesh & Mould', 'Reinforcement')
            AND EXISTS (
                SELECT 1 
                FROM project_stages ps
                WHERE ps.name = 'Reinforcement' AND ps.assigned_to = $2
            )
        )
    )`
		rows, err := db.Query(query, projectID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
			return
		}
		defer rows.Close()

		taskMap := make(map[int][]models.Activity)
		taskIDs := make(map[int]struct{})

		for rows.Next() {
			var activity models.Activity
			var paperID, qcID sql.NullInt64
			var elemTypeID int
			var elementName string
			var qcStatus sql.NullString
			var startDate, endDate sql.NullTime
			var stageName sql.NullString

			err = rows.Scan(&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &activity.Priority,
				&activity.StageID, &stageName, &activity.AssignedTo, &startDate, &endDate, &activity.Status,
				&activity.ElementID, &qcID, &paperID, &qcStatus, &activity.MeshMoldStatus, &activity.ReinforcementStatus, &elemTypeID, &elementName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning activities", "details": err.Error()})
				return
			}

			// Assign nullable values
			activity.PaperID = int(paperID.Int64)
			activity.QCID = int(qcID.Int64)
			activity.QCStatus = qcStatus.String
			activity.StageName = stageName.String

			// Store in map
			taskMap[activity.TaskID] = append(taskMap[activity.TaskID], activity)
			taskIDs[activity.TaskID] = struct{}{}
		}

		if len(taskMap) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No tasks found for the given project and user."})
			return
		}

		// Fetch assigned users for Mesh & Mold and Reinforcement
		var meshMoldAssignedTo, reinforcementAssignedTo int
		_ = db.QueryRow(`SELECT assigned_to FROM project_stages WHERE name = $1 AND project_id = $2`, strings.TrimSpace("Mesh & Mould"), projectID).Scan(&meshMoldAssignedTo)
		if err != nil {
			log.Println("Error fetching meshmold assigned_to:", err)
		}
		_ = db.QueryRow(`SELECT assigned_to FROM project_stages WHERE name = $1 AND project_id = $2`, strings.TrimSpace("Reinforcement"), projectID).Scan(&reinforcementAssignedTo)
		if err != nil {
			log.Println("Error fetching reinforcement assigned_to:", err)
		}

		// Structure response
		var response []gin.H
		for taskID := range taskIDs {
			task, err := getTaskByID(db, taskID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching tasks", "details": err.Error()})
				return
			}

			// Fetch element type
			elementType, err := getElementTypeByID(db, task.ElementTypeID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching element type", "details": err.Error()})
				return
			}
			task.ElementType = elementType

			// Fetch activities for the task
			taskActivities := taskMap[task.TaskID]
			var formattedActivities []gin.H

			for _, act := range taskActivities {

				meshMold := userID == meshMoldAssignedTo
				reinforcement := userID == reinforcementAssignedTo
				other := !meshMold && !reinforcement

				// Append activity with stage type
				formattedActivities = append(formattedActivities, gin.H{
					"id":                   act.ID,
					"task_id":              act.TaskID,
					"project_id":           act.ProjectID,
					"name":                 act.Name,
					"priority":             act.Priority,
					"stage_id":             act.StageID,
					"stage_name":           act.StageName,
					"assigned_to":          act.AssignedTo,
					"start_date":           act.StartDate,
					"end_date":             act.EndDate,
					"status":               act.Status,
					"element_id":           act.ElementID,
					"qc_id":                act.QCID,
					"paper_id":             act.PaperID,
					"qc_status":            act.QCStatus,
					"qc":                   act.QCID == userID,
					"mesh_mold_status":     act.MeshMoldStatus,
					"reinforcement_status": act.ReinforcementStatus,
					"MeshMold":             meshMold,
					"Reinforcement":        reinforcement,
					"Other":                other,
				})
			}

			response = append(response, gin.H{
				"task_id":         task.TaskID,
				"project_id":      task.ProjectID,
				"element_type_id": task.ElementTypeID,
				"element_type":    task.ElementType,
				"activities":      formattedActivities,
			})
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Activity",
			EventName:    "Get",
			Description:  "Get Activity",
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

func getElementTypeByID(db *sql.DB, elementTypeID int) (string, error) {
	var elementType string
	err := db.QueryRow("SELECT element_type_name FROM element_type WHERE element_type_id = $1", elementTypeID).Scan(&elementType)
	if err != nil {
		return "", err
	}
	return elementType, nil
}

func getTaskByID(db *sql.DB, taskID int) (models.Task, error) {
	query := `
		SELECT task_id, project_id, task_type_id, name, stage_id, description, priority, 
		       assigned_to, estimated_effort_in_hrs, start_date, end_date, status, 
		       color_code, element_type_id, floor_id
		FROM task WHERE task_id = $1 LIMIT 1`

	var task models.Task

	err := db.QueryRow(query, taskID).Scan(
		&task.TaskID, &task.ProjectID, &task.TaskTypeId, &task.Name, &task.StageID,
		&task.Desc, &task.Priority, &task.AssignedTo, &task.EstimatedEffortInHrs,
		&task.StartDate, &task.EndDate, &task.Status, &task.ColorCode,
		&task.ElementTypeID, &task.FloorID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return task, fmt.Errorf("task not found")
		}
		return task, err
	}

	return task, nil
}

// UpdateActivityStatusHandler godoc
// @Summary      Update activity status
// @Tags         activity
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Activity status payload"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_activity_status [put]
func UpdateActivityStatusHandler(db *sql.DB) gin.HandlerFunc {
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

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}
		userID := user.ID

		var req struct {
			ActivityID int    `json:"activity_id"`
			Status     string `json:"status"`
			QCStatus   string `json:"qc_status,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
			return
		}

		var activity models.Activity
		var qcID sql.NullInt64
		var qcStatus sql.NullString

		query := `
			SELECT id, task_id, name, element_id, assigned_to, qc_id, stage_id, project_id, status, qc_status, mesh_mold_status, reinforcement_status, stockyard_id, meshmold_qc_status, reinforcement_qc_status
			FROM activity WHERE id = $1`
		err = db.QueryRow(query, req.ActivityID).Scan(
			&activity.ID,
			&activity.TaskID,
			&activity.Name,
			&activity.ElementID,
			&activity.AssignedTo,
			&qcID,
			&activity.StageID,
			&activity.ProjectID,
			&activity.Status,
			&qcStatus,
			&activity.MeshMoldStatus,
			&activity.ReinforcementStatus,
			&activity.StockyardID,
			&activity.MeshMoldQCStatus,
			&activity.ReinforcementQCStatus,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Activity not found", "details": err.Error()})
			return
		}

		query = `
			SELECT task_id, project_id, element_type_id, floor_id
			FROM task WHERE task_id = $1`
		var task models.Task
		err = db.QueryRow(query, activity.TaskID).Scan(
			&task.TaskID,
			&task.ProjectID,
			&task.ElementTypeID,
			&task.FloorID,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found", "details": err.Error()})
			return
		}

		if qcID.Valid {
			activity.QCID = int(qcID.Int64)
		}

		var currStage string
		err = db.QueryRow(`SELECT name FROM project_stages WHERE id = $1 AND project_id = $2`, activity.StageID, activity.ProjectID).Scan(&currStage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch current stage", "details": err.Error()})
			return
		}

		// Get Reinforcement and Mesh & Mold stage user info
		var ReinforcementAssignedTo, ReinforcementQC, ReinforcementID int
		err = db.QueryRow(`SELECT assigned_to FROM project_stages WHERE name = 'Reinforcement' AND project_id = $1`, activity.ProjectID).Scan(&ReinforcementAssignedTo)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Reinforcement assigned user", "details": err.Error()})
			return
		}
		err = db.QueryRow(`SELECT qc_id FROM project_stages WHERE name = 'Reinforcement' AND project_id = $1`, activity.ProjectID).Scan(&ReinforcementQC)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Reinforcement QC", "details": err.Error()})
			return
		}
		err = db.QueryRow(`SELECT id FROM project_stages WHERE name = 'Reinforcement' AND project_id = $1`, activity.ProjectID).Scan(&ReinforcementID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Reinforcement stage ID", "details": err.Error()})
			return
		}

		var MeshMoldAssignedTo, MeshMoldQC int
		err = db.QueryRow(`SELECT assigned_to FROM project_stages WHERE name = 'Mesh & Mould' AND project_id = $1`, activity.ProjectID).Scan(&MeshMoldAssignedTo)
		if err != nil {
			if err == sql.ErrNoRows {
				// Stage not found, set default values
				MeshMoldAssignedTo = 0
				MeshMoldQC = 0
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Mesh & Mould assigned user", "details": err.Error()})
				return
			}
		} else {
			// If stage exists, fetch QC
			err = db.QueryRow(`SELECT qc_id FROM project_stages WHERE name = 'Mesh & Mould' AND project_id = $1`, activity.ProjectID).Scan(&MeshMoldQC)
			if err != nil && err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Mesh & Mould QC", "details": err.Error()})
				return
			}
		}

		// Check user permissions
		isAssignee := userID == activity.AssignedTo
		if !isAssignee && ReinforcementAssignedTo != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized to update this activity"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Update Reinforcement status (only if not already completed)
		if userID == ReinforcementAssignedTo && strings.ToLower(activity.ReinforcementStatus) != "completed" {
			_, err = tx.Exec(`UPDATE activity SET reinforcement_status = $1 WHERE id = $2`, req.Status, req.ActivityID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Reinforcement status"})
				return
			}

			var ReinforcementStageId int
			err = db.QueryRow(`SELECT id FROM project_stages WHERE name = 'Reinforcement' AND project_id = $1`, activity.ProjectID).Scan(&ReinforcementStageId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Reinforcement stage ID", "details": err.Error()})
				return
			}

			queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

			_, err = tx.Exec(queryInsertCompleteProduction,
				activity.TaskID,
				req.ActivityID,
				activity.ProjectID,
				activity.ElementID,
				task.ElementTypeID,
				task.FloorID,
				ReinforcementStageId,
				activity.AssignedTo,
				time.Now(),
				time.Now(),
				req.Status,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert complete_production record", "details": err.Error()})
				return
			}

			if req.Status == "completed" {

				if activity.MeshMoldStatus == "completed" && activity.MeshMoldQCStatus == "completed" && activity.ReinforcementStatus == "completed" {
					if activity.QCID == 0 {
						nextStageID, err := getNextStageID(tx, activity.StageID, activity.TaskID)
						if err != nil {
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get next stage ID", "details": err.Error()})
							return
						}

						if nextStageID == 0 {
							// Last stage, move to stockyard
							if _, err = CreatePrecastStock(db, activity.ElementID, activity.ProjectID, activity.StockyardID); err != nil {
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to move to stockyard", "details": err.Error()})
								return
							}

							// Delete activity
							if _, err = tx.Exec("UPDATE activity SET completed = true WHERE id = $1", activity.ID); err != nil {
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove activity", "details": err.Error()})
								return
							}
						} else {
							// Move activity to next stage
							moveToNextStage(tx, activity.ID, nextStageID)
						}
					}
				}

			}
		}

		if currStage == "Mesh & Mould" && userID == MeshMoldAssignedTo {
			_, err = tx.Exec(`UPDATE activity SET mesh_mold_status = $1 WHERE id = $2`, req.Status, req.ActivityID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Mesh Mold Status status"})
				return
			}

			queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

			_, err = tx.Exec(queryInsertCompleteProduction,
				activity.TaskID,
				req.ActivityID,
				activity.ProjectID,
				activity.ElementID,
				task.ElementTypeID,
				task.FloorID,
				activity.StageID,
				activity.AssignedTo,
				time.Now(),
				time.Now(),
				req.Status,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert complete_production record", "details": err.Error()})
				return
			}

			if req.Status == "completed" {

				if activity.ReinforcementStatus == "completed" && activity.ReinforcementQCStatus == "completed" && ReinforcementQC != 0 {
					if activity.QCID == 0 {
						nextStageID, err := getNextStageID(tx, ReinforcementID, activity.TaskID)
						if err != nil {
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get next stage ID", "details": err.Error()})
							return
						}

						if nextStageID == 0 {
							// Last stage, move to stockyard
							if _, err = CreatePrecastStock(db, activity.ElementID, activity.ProjectID, activity.StockyardID); err != nil {
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to move to stockyard", "details": err.Error()})
								return
							}

							// Delete activity
							if _, err = tx.Exec("DELETE FROM activity WHERE id = $1", activity.ID); err != nil {
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove activity", "details": err.Error()})
								return
							}
						} else {
							// Move activity to next stage
							moveToNextStage(tx, activity.ID, nextStageID)
						}
					}
				}

			}

		}

		if currStage != "Reinforcement" && currStage != "Mesh & Mould" && userID == activity.AssignedTo {
			_, err = tx.Exec(`UPDATE activity SET status = $1 WHERE id = $2`, req.Status, req.ActivityID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update activity status", "details": err.Error()})
				return
			}

			queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

			_, err = tx.Exec(queryInsertCompleteProduction,
				activity.TaskID,
				req.ActivityID,
				activity.ProjectID,
				activity.ElementID,
				task.ElementTypeID,
				task.FloorID,
				activity.StageID,
				activity.AssignedTo,
				time.Now(),
				time.Now(),
				req.Status,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert complete_production record", "details": err.Error()})
				return
			}

			if req.Status == "completed" {

				if activity.QCID == 0 {

					nextStageID, err := getNextStageID(tx, activity.StageID, activity.TaskID)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get next stage ID", "details": err.Error()})
						return
					}

					if nextStageID == 0 {
						// Last stage, move to stockyard
						if _, err = CreatePrecastStock(db, activity.ElementID, activity.ProjectID, activity.StockyardID); err != nil {
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to move to stockyard", "details": err.Error()})
							return
						}

						// Delete activity
						if _, err = tx.Exec("UPDATE activity SET completed = true WHERE id = $1", activity.ID); err != nil {
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove activity", "details": err.Error()})
							return
						}
					} else {
						moveToNextStage(tx, activity.ID, nextStageID)
					}

				}

			}
		}

		var ProjectName string
		err = db.QueryRow(`SELECT name FROM project WHERE project_id = $1`, activity.ProjectID).Scan(&ProjectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "project name not present", "error": err.Error()})
			return
		}

		var endclientID int
		err = tx.QueryRow(`SELECT client_id from project WHERE project_id = $1`, task.ProjectID).Scan(&endclientID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project member", "details": err.Error()})
			return
		}

		var clientID int
		err = tx.QueryRow(`SELECT client_id from end_client WHERE id = $1`, endclientID).Scan(&clientID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project member", "details": err.Error()})
			return
		}

		var organization string
		err = tx.QueryRow(`SELECT organization from client WHERE client_id = $1`, clientID).Scan(&organization)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project member", "details": err.Error()})
			return
		}

		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed"})
			return
		}

		// Initialize email service
		emailService := services.NewEmailService(db)

		// Fetch next assignee (based on next stage)
		var nextAssigneeEmail, nextAssigneeName string
		err = db.QueryRow(`
    SELECT u.email, u.first_name || ' ' || u.last_name AS full_name
    FROM users u
	WHERE u.id = $1
`, qcID).Scan(&nextAssigneeEmail, &nextAssigneeName)
		if err != nil {
			log.Printf("Failed to fetch next assignee email: %v", err)
		} else {
			// Prepare email data
			emailData := models.EmailData{
				Email:        nextAssigneeEmail,
				Role:         "Update Activity",            // You can enhance based on role table
				Organization: "",                           // Add if needed
				ProjectName:  ProjectName,                  // Or fetch project name from project table
				ProjectID:    strconv.Itoa(task.ProjectID), // Convert int → string
				CompanyName:  organization,
				SupportEmail: "support@blueinvent.com",
				LoginURL:     "https://precastezy.blueinvent.com/login",
				AdminName:    userName, // User who performed the update
				UserName:     nextAssigneeName,
			}

			// Choose the template
			templateType := "Activity Update"
			var templateID int = 6 // Example: your DB template ID

			// Send email
			if err := emailService.SendTemplatedEmail(templateType, emailData, &templateID); err != nil {
				log.Printf("Failed to send notification email: %v", err)
			}
		}

		// Get QC ID from current stage if activity.QCID is 0
		var qcUserID int = activity.QCID
		if qcUserID == 0 {
			var stageQCID sql.NullInt64
			err = db.QueryRow(`SELECT qc_id FROM project_stages WHERE id = $1 AND project_id = $2`, activity.StageID, activity.ProjectID).Scan(&stageQCID)
			if err == nil && stageQCID.Valid {
				qcUserID = int(stageQCID.Int64)
			}
		}

		// Create database notification for the QC user
		if qcUserID > 0 {
			notif := models.Notification{
				UserID:    qcUserID,
				Message:   fmt.Sprintf("Activity status updated to %s for element: %s (ID: %d)", req.Status, activity.Name, activity.ElementID),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", activity.ProjectID),
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

			// Send push notification to QC user
			log.Printf("Attempting to send push notification to QC user %d for activity status update", qcUserID)
			SendNotificationHelper(db, qcUserID,
				"Activity Status Updated",
				fmt.Sprintf("Activity '%s' status updated to %s", activity.Name, req.Status),
				map[string]string{
					"project_id":    strconv.Itoa(activity.ProjectID),
					"project_name":  ProjectName,
					"activity_id":   strconv.Itoa(activity.ID),
					"activity_name": activity.Name,
					"element_id":    strconv.Itoa(activity.ElementID),
					"status":        req.Status,
					"action":        "activity_status_updated",
				},
				"activity_status_updated")
		} else {
			log.Printf("No QC user ID found for activity %d, skipping push notification", activity.ID)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Activity status updated successfully"})

		log := models.ActivityLog{
			EventContext: "Activity",
			EventName:    "PUT",
			Description:  fmt.Sprintf("Update Activity %d Status %s", req.ActivityID, req.Status),
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

func getNextStageID(tx *sql.Tx, currentStageID, taskID int) (int, error) {
	// Fetch element type ID
	var elementTypeID int
	err := tx.QueryRow(`SELECT element_type_id FROM task WHERE task_id = $1`, taskID).Scan(&elementTypeID)
	if err != nil {
		return 0, err
	}

	// Fetch stage path
	var stagePath string
	err = tx.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
	if err != nil {
		return 0, err
	}

	// Convert stage path to array
	stagePath = strings.Trim(stagePath, "{}")
	stagePathArray := strings.Split(stagePath, ",")
	for i, stage := range stagePathArray {
		stageInt, _ := strconv.Atoi(stage)
		if stageInt == currentStageID && i+1 < len(stagePathArray) {
			nextStage, _ := strconv.Atoi(stagePathArray[i+1])
			return nextStage, nil
		}
	}

	return 0, nil
}

func moveToNextStage(tx *sql.Tx, activityID, nextStageID int) error {

	var newAssignedTo, newQCID, newPaperID sql.NullInt64
	query := `SELECT assigned_to, qc_id, paper_id FROM project_stages WHERE id = $1 LIMIT 1`
	err := tx.QueryRow(query, nextStageID).Scan(&newAssignedTo, &newQCID, &newPaperID)
	if err != nil {
		return err
	}

	var elementID int
	err = tx.QueryRow(`SELECT element_id FROM activity WHERE id= $1`, activityID).Scan(&elementID)
	if err != nil {
		return err
	}

	var name string
	err = tx.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, nextStageID).Scan(&name)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE element SET status = $1 WHERE element_id = $2`, name, elementID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE activity
		SET stage_id = $1, status = 'InProgress', qc_status = 'InProgress',
		    assigned_to = $2, qc_id = $3, paper_id = $4
		WHERE id = $5`,
		nextStageID, newAssignedTo.Int64, newQCID.Int64, newPaperID.Int64, activityID)
	return err
}

//-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

// CreateTemplateHandler godoc
// @Summary      Create template with stages
// @Tags         templates
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "template_name, stages[]"
// @Success      201   {object}  object
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/create_template [post]
func CreateTemplateHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
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

		var req struct {
			TemplateName string         `json:"template_name"`
			Stages       []models.Stage `json:"stages"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		// Insert template
		var templateID int
		err = db.QueryRow("INSERT INTO templates (name) VALUES ($1) RETURNING id", req.TemplateName).Scan(&templateID)
		if err != nil {
			log.Println("Error inserting template:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template"})
			return
		}

		// Insert stages
		query := `
			INSERT INTO stages (name, qc_assign, template_id, "order", completion_stage, inventory_deduction) 
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		for _, stage := range req.Stages {
			_, err := db.Exec(query, stage.Name, stage.QCAssign, templateID, stage.Order, stage.CompletionStage, stage.InventoryDeduction)
			if err != nil {
				log.Println("Error inserting stage:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stage"})
				return
			}
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New template created: %s", req.TemplateName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/templates/%d", templateID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Template and stages created successfully", "template_id": templateID})
	}
}

// UpdateTemplateHandler godoc
// @Summary      Update template
// @Tags         templates
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "Template ID"
// @Param        body  body  object  true  "template_name, stages[]"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_template/{id} [put]
func UpdateTemplateHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
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

		templateIDStr := c.Param("id")
		templateID, err := strconv.Atoi(templateIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		var req struct {
			TemplateName string         `json:"template_name"`
			Stages       []models.Stage `json:"stages"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
			return
		}

		// Update template name
		_, err = tx.Exec(`UPDATE templates SET name = $1 WHERE id = $2`, req.TemplateName, templateID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update template"})
			return
		}

		// Delete old stages
		_, err = tx.Exec(`DELETE FROM stages WHERE template_id = $1`, templateID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete old stages"})
			return
		}

		// Insert new stages
		stageQuery := `
			INSERT INTO stages (name, qc_assign, template_id, "order", completion_stage, inventory_deduction)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		for _, stage := range req.Stages {
			_, err := tx.Exec(stageQuery, stage.Name, stage.QCAssign, templateID, stage.Order, stage.CompletionStage, stage.InventoryDeduction)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stage"})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit changes"})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Template updated: %s", req.TemplateName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/templates/%d", templateID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Template updated successfully"})
	}
}

// DeleteTemplateHandler godoc
// @Summary      Delete template
// @Tags         templates
// @Param        id  path  int  true  "Template ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      404  {object}  object
// @Router       /api/delete_template/{id} [delete]
func DeleteTemplateHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
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

		templateIDStr := c.Param("id")
		templateID, err := strconv.Atoi(templateIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		// Get template name before deleting
		var templateName string
		err = db.QueryRow(`SELECT name FROM templates WHERE id = $1`, templateID).Scan(&templateName)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch template", "details": err.Error()})
			}
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
			return
		}

		// Delete stages first
		_, err = tx.Exec(`DELETE FROM stages WHERE template_id = $1`, templateID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stages"})
			return
		}

		// Delete template
		_, err = tx.Exec(`DELETE FROM templates WHERE id = $1`, templateID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete template"})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit deletion"})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Template deleted: %s", templateName),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/templates",
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

		c.JSON(http.StatusOK, gin.H{"message": "Template deleted successfully"})
	}
}

// GetTemplateByIDHandler godoc
// @Summary      Get template by ID (with stages)
// @Tags         templates
// @Param        id  path  int  true  "Template ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/get_template/{id} [get]
func GetTemplateByIDHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		templateIDStr := c.Param("id")
		templateID, err := strconv.Atoi(templateIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		var templateName string
		err = db.QueryRow(`SELECT name FROM templates WHERE id = $1`, templateID).Scan(&templateName)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch template"})
			}
			return
		}

		rows, err := db.Query(`SELECT id, name, qc_assign, "order", completion_stage, inventory_deduction FROM stages WHERE template_id = $1 ORDER BY "order"`, templateID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages"})
			return
		}
		defer rows.Close()

		var stages []models.Stage
		for rows.Next() {
			var stage models.Stage
			if err := rows.Scan(&stage.ID, &stage.Name, &stage.QCAssign, &stage.Order, &stage.CompletionStage, &stage.InventoryDeduction); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan stage"})
				return
			}
			stages = append(stages, stage)
		}

		c.JSON(http.StatusOK, gin.H{
			"template_id":   templateID,
			"template_name": templateName,
			"stages":        stages,
		})
	}
}

// GetTemplateHandler godoc
// @Summary      Get template stages by template ID
// @Tags         templates
// @Param        template_id  path  int  true  "Template ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/get_templatestages/{template_id} [get]
func GetTemplateHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get template_id from URL params and validate it
		templateID, err := strconv.Atoi(c.Param("template_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template_id"})
			return
		}
		// Fetch the template details
		var template models.Template
		err = db.QueryRow("SELECT id, name FROM templates WHERE id = $1", templateID).
			Scan(&template.ID, &template.Name)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
				return
			}
			log.Println("Error fetching template:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch template", "details": err.Error()})
			return
		}

		// Fetch associated stages
		rows, err := db.Query(`
			SELECT id, name, qc_assign, template_id, "order", completion_stage, inventory_deduction
			FROM stages WHERE template_id = $1 ORDER BY "order"`, templateID)
		if err != nil {
			log.Println("Error fetching stages:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages", "details": err.Error()})
			return
		}
		defer rows.Close()

		var stages []models.Stage
		for rows.Next() {
			var stage models.Stage
			if err := rows.Scan(&stage.ID, &stage.Name, &stage.QCAssign, &stage.TemplateID, &stage.Order, &stage.CompletionStage, &stage.InventoryDeduction); err != nil {
				log.Println("Error scanning stage:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse stages", "details": err.Error()})
				return
			}
			stages = append(stages, stage)
		}

		c.JSON(http.StatusOK, gin.H{
			"template": gin.H{
				"id":   template.ID,
				"name": template.Name,
			},
			"stages": stages,
		})
	}
}

// GetAllTemplatesHandler godoc
// @Summary      Get all templates with stages
// @Tags         templates
// @Success      200  {object}  object
// @Failure      500  {object}  object
// @Router       /api/get_alltemplatestages [get]
func GetAllTemplatesHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Fetch all templates
		rows, err := db.Query("SELECT id, name FROM templates ORDER BY id")
		if err != nil {
			log.Println("Error fetching templates:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch templates"})
			return
		}
		defer rows.Close()

		var templates []models.Template
		for rows.Next() {
			var template models.Template
			if err := rows.Scan(&template.ID, &template.Name); err != nil {
				log.Println("Error scanning template:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse templates"})
				return
			}

			// Fetch associated stages for each template
			stageRows, err := db.Query(`
				SELECT id, name, qc_assign, template_id, "order", completion_stage, inventory_deduction 
				FROM stages WHERE template_id = $1 ORDER BY "order"`, template.ID)
			if err != nil {
				log.Println("Error fetching stages for template:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages"})
				return
			}

			var stages []models.Stage
			for stageRows.Next() {
				var stage models.Stage
				if err := stageRows.Scan(&stage.ID, &stage.Name, &stage.QCAssign, &stage.TemplateID, &stage.Order, &stage.CompletionStage, &stage.InventoryDeduction); err != nil {
					log.Println("Error scanning stage:", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse stages"})
					return
				}
				stages = append(stages, stage)
			}
			stageRows.Close()

			template.Stages = stages
			templates = append(templates, template)
		}

		c.JSON(http.StatusOK, gin.H{"templates": templates})
	}
}

// GetAllTemplates godoc
// @Summary      Get all templates (id, name only)
// @Tags         templates
// @Success      200  {object}  object
// @Failure      500  {object}  object
// @Router       /api/get_template [get]
func GetAllTemplates(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Fetch all templates
		rows, err := db.Query("SELECT id, name FROM templates ORDER BY id")
		if err != nil {
			log.Println("Error fetching templates:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch templates"})
			return
		}
		defer rows.Close()

		var templates []models.Template
		for rows.Next() {
			var template models.Template
			if err := rows.Scan(&template.ID, &template.Name); err != nil {
				log.Println("Error scanning template:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse templates"})
				return
			}
			templates = append(templates, template)
		}

		c.JSON(http.StatusOK, templates)
	}
}

// CreateProjectStage godoc
// @Summary      Create project stage
// @Tags         project-stages
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Project stage"
// @Success      201   {object}  object
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/create_projectstage/ [post]
func CreateProjectStage(db *sql.DB) gin.HandlerFunc {
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

		var stage models.ProjectStages

		// Bind incoming JSON to the struct
		if err := c.ShouldBindJSON(&stage); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Prepare nullable values
		var templateID, qcID, paperID, assignedTo interface{}

		if stage.TemplateID == 0 {
			templateID = nil
		} else {
			templateID = stage.TemplateID
		}

		if stage.QCID == 0 {
			qcID = nil
		} else {
			qcID = stage.QCID
		}

		if stage.PaperID == 0 {
			paperID = nil
		} else {
			paperID = stage.PaperID
		}

		if stage.AssignedTo == 0 {
			assignedTo = nil
		} else {
			assignedTo = stage.AssignedTo
		}

		// SQL Query
		query := `
			INSERT INTO project_stages 
			(name, project_id, assigned_to, qc_assign, qc_id, paper_id, template_id, "order", completion_stage, inventory_deduction) 
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) 
			RETURNING id`

		// Execute query and scan returned ID
		err = db.QueryRow(
			query,
			stage.Name, stage.ProjectID, assignedTo, stage.QCAssign,
			qcID, paperID, templateID, stage.Order,
			stage.CompletionStage, stage.InventoryDeduction,
		).Scan(&stage.ID)

		// Handle errors
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert project stage", "details": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New project stage created: %s", stage.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/stage", stage.ProjectID),
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

		// Return success response
		c.JSON(http.StatusCreated, gin.H{"message": "Project stage created successfully", "stage": stage})

		log := models.ActivityLog{
			EventContext: "Project Stage",
			EventName:    "Create",
			Description:  "Create Project Stage",
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

// UpdateProjectStage godoc
// @Summary      Update project stage
// @Tags         project-stages
// @Param        id    path  int  true  "Project stage ID"
// @Param        body  body  object  true  "Project stage"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/update_project_stage/{id} [put]
func UpdateProjectStage(db *sql.DB) gin.HandlerFunc {
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

		var stage models.ProjectStages
		stageID := c.Param("id")

		if err := c.ShouldBindJSON(&stage); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// If template_id is missing, set it to NULL
		var templateID interface{} = nil
		if stage.TemplateID != 0 { // Assuming 0 means it's not set
			templateID = stage.TemplateID
		}

		query := `
			UPDATE project_stages 
			SET name = $1, project_id = $2, assigned_to = $3, qc_assign = $4, 
				qc_id = $5, paper_id = $6, template_id = $7, "order" = $8, 
				completion_stage = $9, inventory_deduction = $10
			WHERE id = $11`

		_, err = db.Exec(
			query,
			stage.Name, stage.ProjectID, stage.AssignedTo, stage.QCAssign,
			stage.QCID, stage.PaperID, templateID, stage.Order,
			stage.CompletionStage, stage.InventoryDeduction, stageID,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project stage", "details": err.Error()})
			return
		}

		// Update related activities where stage_id = stageID
		activityUpdateQuery := `
			UPDATE activity 
			SET assigned_to = $1, qc_id = $2, paper_id = $3
			WHERE stage_id = $4`

		_, err = db.Exec(
			activityUpdateQuery,
			stage.AssignedTo, stage.QCID, stage.PaperID, stageID,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update activities", "details": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Project stage updated: %s", stage.Name),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/stage", stage.ProjectID),
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

		c.JSON(http.StatusOK, gin.H{"message": "Project stage updated successfully"})

		log := models.ActivityLog{
			EventContext: "Stage",
			EventName:    "Update",
			Description:  "Update Project Stage",
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

// GetAllStagesByProjectID godoc
// @Summary      Get all stages for project
// @Tags         project-stages
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_allstages/{project_id} [get]
func GetAllStagesByProjectID(db *sql.DB) gin.HandlerFunc {
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

		projectID := c.Param("project_id")

		projectId, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		type StageResponse struct {
			models.ProjectStages
			AssigneeName string `json:"assignee_name"`
			QCName       string `json:"qc_name"`
		}

		query := `
			SELECT 
				ps.id, ps.name, ps.project_id, ps.assigned_to, ps.qc_assign, ps.qc_id, 
				ps.paper_id, ps.template_id, ps."order", ps.completion_stage, ps.inventory_deduction,
				COALESCE(TRIM(COALESCE(assignee.first_name, '') || ' ' || COALESCE(assignee.last_name, '')), '') AS assignee_name,
				COALESCE(TRIM(COALESCE(qc.first_name, '') || ' ' || COALESCE(qc.last_name, '')), '') AS qc_name
			FROM project_stages ps
			LEFT JOIN users assignee ON ps.assigned_to = assignee.id
			LEFT JOIN users qc ON ps.qc_id = qc.id
			WHERE ps.project_id = $1
			ORDER BY ps."order"`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve project stages", "details": err.Error()})
			return
		}
		defer rows.Close()

		var stages []StageResponse

		for rows.Next() {
			var stage StageResponse
			var templateID, qcID, paperID, assignedTo sql.NullInt64 // Handle NULL values correctly

			err := rows.Scan(
				&stage.ID, &stage.Name, &stage.ProjectID, &assignedTo,
				&stage.QCAssign, &qcID, &paperID, &templateID,
				&stage.Order, &stage.CompletionStage, &stage.InventoryDeduction,
				&stage.AssigneeName, &stage.QCName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning project stages", "details": err.Error()})
				return
			}

			// Convert sql.NullInt64 to int (if Valid) or set it to zero
			if assignedTo.Valid {
				stage.AssignedTo = int(assignedTo.Int64)
			} else {
				stage.AssignedTo = 0
			}

			if qcID.Valid {
				stage.QCID = int(qcID.Int64)
			} else {
				stage.QCID = 0
			}

			if paperID.Valid {
				stage.PaperID = int(paperID.Int64)
			} else {
				stage.PaperID = 0
			}

			if templateID.Valid {
				stage.TemplateID = int(templateID.Int64)
			} else {
				stage.TemplateID = 0
			}

			stages = append(stages, stage)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving project stages", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, stages)

		log := models.ActivityLog{
			EventContext: "Stages",
			EventName:    "Get",
			Description:  "Get All Stages",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// GetStageByID godoc
// @Summary      Get stage by ID
// @Tags         project-stages
// @Param        stage_id  path  int  true  "Stage ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/get_stage/{stage_id} [get]
func GetStageByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		stageID := c.Param("stage_id")

		query := `
			SELECT id, name, project_id, assigned_to, qc_assign, qc_id, 
			       paper_id, template_id, "order", completion_stage, inventory_deduction
			FROM project_stages 
			WHERE id = $1
			LIMIT 1`

		var stage models.ProjectStages
		var templateID, qcID, paperID, assignedTo sql.NullInt64 // Handle NULL values correctly

		err := db.QueryRow(query, stageID).Scan(
			&stage.ID, &stage.Name, &stage.ProjectID, &assignedTo,
			&stage.QCAssign, &qcID, &paperID, &templateID,
			&stage.Order, &stage.CompletionStage, &stage.InventoryDeduction,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Stage not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve stage", "details": err.Error()})
			}
			return
		}

		// Convert sql.NullInt64 to int (if Valid) or set it to zero
		if assignedTo.Valid {
			stage.AssignedTo = int(assignedTo.Int64)
		} else {
			stage.AssignedTo = 0
		}

		if qcID.Valid {
			stage.QCID = int(qcID.Int64)
		} else {
			stage.QCID = 0
		}

		if paperID.Valid {
			stage.PaperID = int(paperID.Int64)
		} else {
			stage.PaperID = 0
		}

		if templateID.Valid {
			stage.TemplateID = int(templateID.Int64)
		} else {
			stage.TemplateID = 0
		}

		c.JSON(http.StatusOK, stage)
	}
}

func GetAllView(db *sql.DB) gin.HandlerFunc {
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

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}
		userID := user.ID

		projectIDParam := c.Param("project_id")
		if projectIDParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		projectID, err := strconv.Atoi(projectIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID", "details": err.Error()})
			return
		}

		// Fetch project stages for the given project_id
		queryStages := `SELECT id, name, project_id, assigned_to, qc_assign, qc_id, paper_id, template_id, 
		                "order", completion_stage, inventory_deduction 
		                FROM project_stages WHERE project_id = $1 ORDER BY "order"`

		stageRows, err := db.Query(queryStages, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages", "details": err.Error()})
			return
		}
		defer stageRows.Close()

		// Create a map of all project stages for easy lookup
		stageMap := make(map[int]models.ProjectStages)
		for stageRows.Next() {
			var stage models.ProjectStages
			var templateID, qcID, paperID, assignedTo sql.NullInt64

			err := stageRows.Scan(
				&stage.ID, &stage.Name, &stage.ProjectID, &assignedTo,
				&stage.QCAssign, &qcID, &paperID, &templateID,
				&stage.Order, &stage.CompletionStage, &stage.InventoryDeduction,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stages", "details": err.Error()})
				return
			}

			// Convert sql.NullInt64 to int (if Valid) or set it to zero
			if assignedTo.Valid {
				stage.AssignedTo = int(assignedTo.Int64)
			} else {
				stage.AssignedTo = 0
			}

			if qcID.Valid {
				stage.QCID = int(qcID.Int64)
			} else {
				stage.QCID = 0
			}

			if paperID.Valid {
				stage.PaperID = int(paperID.Int64)
			} else {
				stage.PaperID = 0
			}

			if templateID.Valid {
				stage.TemplateID = int(templateID.Int64)
			} else {
				stage.TemplateID = 0
			}

			stageMap[stage.ID] = stage
		}

		// Fetch tasks
		queryTasks := `SELECT t.task_id, t.project_id, t.task_type_id, t.name, t.stage_id, t.description, t.priority, 
                        t.file_attachments, t.assigned_to, t.estimated_effort_in_hrs, t.start_date, 
                        t.end_date, t.status, t.color_code, t.element_type_id, t.floor_id, et.element_type_name
                FROM task t
                LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
                WHERE t.project_id = $1`

		rows, err := db.Query(queryTasks, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tasks", "details": err.Error()})
			return
		}
		defer rows.Close()

		var tasks []models.Task
		for rows.Next() {
			var task models.Task
			var elementType sql.NullString
			err := rows.Scan(
				&task.TaskID, &task.ProjectID, &task.TaskTypeId, &task.Name, &task.StageID, &task.Desc,
				&task.Priority, pq.Array(&task.FileAttachments), &task.AssignedTo, &task.EstimatedEffortInHrs,
				&task.StartDate, &task.EndDate, &task.Status, &task.ColorCode, &task.ElementTypeID, &task.FloorID, &elementType,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading tasks", "details": err.Error()})
				return
			}
			task.ElementType = elementType.String

			tasks = append(tasks, task)
		}

		// Fetch activities and process stages
		for i, task := range tasks {
			queryActivities := `SELECT a.id, a.task_id, a.project_id, a.name, a.priority, a.stage_id, s.name as stage_name, 
				   				a.assigned_to, a.start_date, a.end_date, a.status, a.element_id, 
				   				a.qc_id, a.paper_id, a.qc_status, a.mesh_mold_status, a.reinforcement_status, a.meshmold_qc_status, a.reinforcement_qc_status
								FROM activity a
								LEFT JOIN project_stages s ON a.stage_id = s.id
								WHERE a.task_id = $1 AND a.completed = false`

			activityRows, err := db.Query(queryActivities, task.TaskID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
				return
			}
			defer activityRows.Close()

			var activities []models.Activity
			for activityRows.Next() {
				var activity models.Activity
				var paperID sql.NullInt64
				var qcID sql.NullInt64
				var qcStatus, MeshMoldStatus, ReinforcementStatus sql.NullString
				var stageName sql.NullString
				var priority sql.NullString

				err := activityRows.Scan(
					&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &priority,
					&activity.StageID, &stageName, &activity.AssignedTo, &activity.StartDate, &activity.EndDate,
					&activity.Status, &activity.ElementID, &qcID, &paperID, &qcStatus, &MeshMoldStatus, &ReinforcementStatus, &activity.MeshMoldQCStatus, &activity.ReinforcementQCStatus,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading activities", "details": err.Error()})
					return
				}

				activity.StageName = stageName.String
				activity.Priority = priority.String

				activity.MeshMoldStatus = MeshMoldStatus.String
				activity.ReinforcementStatus = ReinforcementStatus.String

				activity.PaperID = int(paperID.Int64)
				// Set QC ID & Status if present
				if qcID.Valid {
					activity.QCID = int(qcID.Int64)
				} else {
					activity.QCID = 0
				}
				if qcStatus.Valid {
					activity.QCStatus = qcStatus.String
				} else {
					activity.QCStatus = ""
				}

				// Fetch element_type_id and stage_path for the current task
				var elementTypeID int
				err = db.QueryRow(`SELECT element_type_id FROM task WHERE task_id = $1`, activity.TaskID).Scan(&elementTypeID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type", "details": err.Error()})
					return
				}

				var stagePath string
				err = db.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stage path", "details": err.Error()})
					return
				}

				// Convert stage_path to array
				stagePath = strings.Trim(stagePath, "{}")
				stageIDs := make([]int, 0)
				for _, idStr := range strings.Split(stagePath, ",") {
					if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil {
						stageIDs = append(stageIDs, id)
					}
				}

				// Find special stages (Mesh & Mould, Reinforcement)
				meshMouldStageID := -1
				reinforcementStageID := -1
				for id, stage := range stageMap {
					if strings.EqualFold(stage.Name, "Mesh & Mould") {
						meshMouldStageID = id
					} else if strings.EqualFold(stage.Name, "Reinforcement") {
						reinforcementStageID = id
					}
				}

				// Build activity stages based on stage path
				var activityStages []models.ProjectStages
				for _, stageID := range stageIDs {
					if stage, exists := stageMap[stageID]; exists {
						stageCopy := stage

						// Set stage status
						if stageID < activity.StageID {
							stageCopy.Status = "completed"
							stageCopy.QCStatus = "completed"
						} else if stageID == activity.StageID {
							if activity.Status == "completed" {
								stageCopy.Status = "completed"
								stageCopy.QCStatus = "inprogress"
							} else {
								stageCopy.Status = "inprogress"
								stageCopy.QCStatus = "inprogress"
							}
						} else {
							stageCopy.Status = "pending"
							stageCopy.QCStatus = "pending"
						}

						// Handle editability
						stageCopy.Editable = "no"
						stageCopy.QCEditable = "no"

						// Special handling for Mesh & Mould and Reinforcement stages
						if (stageID == meshMouldStageID || stageID == reinforcementStageID) &&
							(stage.AssignedTo == userID || getNextStageAssignee(db, stageID, activity.TaskID) == userID) {
							stageCopy.Editable = "yes"
						} else if stageID == activity.StageID && stage.AssignedTo == userID {
							stageCopy.Editable = "yes"
						}

						// QC editability
						if stage.QCAssign && stage.QCID == userID {
							stageCopy.QCEditable = "yes"
						}

						activityStages = append(activityStages, stageCopy)
					}
				}

				// Convert ProjectStages to []string
				stageNames := make([]string, len(activityStages))
				for i, stage := range activityStages {
					stageNames[i] = stage.Name
				}
				activity.Stages = stageNames
				activities = append(activities, activity)
			}

			tasks[i].Activities = activities
		}

		// Return the tasks with activities and stage statuses
		c.JSON(http.StatusOK, tasks)

		log := models.ActivityLog{
			EventContext: "Kanban",
			EventName:    "Get",
			Description:  "Get All Kanban View",
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

func getNextStageAssignee(db *sql.DB, currentStageID int, taskID int) int {
	// Step 1: Get element_type_id from the task
	var elementTypeID int
	err := db.QueryRow(`SELECT element_type_id FROM task WHERE task_id = $1`, taskID).Scan(&elementTypeID)
	if err != nil {
		log.Printf("Error fetching element_type_id: %v", err)
		return 0
	}

	// Step 2: Get stage_path from element_type_path
	var stagePath string
	err = db.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
	if err != nil {
		log.Printf("Error fetching stage_path: %v", err)
		return 0
	}

	// Step 3: Process stage_path to find the next stage
	stagePath = strings.Trim(stagePath, "{}") // Remove braces
	stagePathArray := strings.Split(stagePath, ",")

	foundCurrentStage := false
	var nextStageID int

	for _, stage := range stagePathArray {
		stageInt, err := strconv.Atoi(strings.TrimSpace(stage))
		if err != nil {
			log.Printf("Error converting stage to int: %v", err)
			continue
		}

		if foundCurrentStage {
			// The next stage is immediately after the current one
			nextStageID = stageInt
			break
		}

		if stageInt == currentStageID {
			foundCurrentStage = true
		}
	}

	// Step 4: If no next stage is found, return 0
	if nextStageID == 0 {
		log.Println("No next stage found.")
		return 0
	}

	// Step 5: Return the next stage's assignee (assuming you have logic to fetch it)
	var nextStageAssignee int
	err = db.QueryRow(`SELECT assignee_id FROM project_stages WHERE stage_id = $1`, nextStageID).Scan(&nextStageAssignee)
	if err != nil {
		log.Printf("Error fetching assignee for stage %d: %v", nextStageID, err)
		return 0
	}

	return nextStageAssignee
}

// GetActivityHandlerWithElement godoc
// @Summary      Get all activity with element for project
// @Tags         activity
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_allactivity/{project_id} [get]
func GetActivityHandlerWithElement(db *sql.DB) gin.HandlerFunc {
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

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}
		userID := user.ID

		projectIDParam := c.Param("project_id")
		if projectIDParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		projectID, err := strconv.Atoi(projectIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID", "details": err.Error()})
			return
		}

		query := `
			SELECT a.id, a.task_id, a.project_id, a.name, a.priority, a.stage_id, s.name as stage_name, 
				   a.assigned_to, a.start_date, a.end_date, a.status, a.element_id, 
				   a.qc_id, a.paper_id, a.qc_status, a.mesh_mold_status, a.reinforcement_status t.element_type_id, t.floor_id, e.element_type
			FROM activity a
			LEFT JOIN project_stages s ON a.stage_id = s.id
			LEFT JOIN task t ON a.task_id = t.task_id
			LEFT JOIN element_type e ON t.element_type_id = e.element_type_id
			WHERE a.project_id = $1 
			AND a.completed = false
			AND (
				a.assigned_to = $2
				OR (a.qc_id = $2 AND a.status = 'completed' AND a.paper_id IS NOT NULL)
				OR (s.name IN ('Mesh & Mould', 'Reinforcement') AND EXISTS (
					SELECT 1 FROM project_stages ps
					WHERE ps.name = 'Reinforcement' AND ps.assigned_to = $2
				))
			)`

		rows, err := db.Query(query, projectID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Grouping data by element_type_id
		elementTypeMap := make(map[int]gin.H)

		for rows.Next() {
			var activity models.Activity
			var elementTypeID int
			var floorID int
			var elementType string
			var paperID sql.NullInt64
			var qcID sql.NullInt64
			var qcStatus sql.NullString
			var startDate, endDate sql.NullTime
			var stageName sql.NullString

			err = rows.Scan(&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &activity.Priority,
				&activity.StageID, &stageName, &activity.AssignedTo, &startDate, &endDate, &activity.Status,
				&activity.ElementID, &qcID, &paperID, &qcStatus, &activity.MeshMoldStatus, &activity.ReinforcementStatus, &elementTypeID, &floorID, &elementType)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning activities", "details": err.Error()})
				return
			}

			// Assign nullable values
			activity.PaperID = int(paperID.Int64)
			activity.QCID = int(qcID.Int64)
			activity.QCStatus = qcStatus.String
			activity.StageName = stageName.String

			// Format activity response
			formattedActivity := gin.H{
				"id":                   activity.ID,
				"task_id":              activity.TaskID,
				"project_id":           activity.ProjectID,
				"name":                 activity.Name,
				"priority":             activity.Priority,
				"stage_id":             activity.StageID,
				"stage_name":           activity.StageName,
				"assigned_to":          activity.AssignedTo,
				"start_date":           activity.StartDate,
				"end_date":             activity.EndDate,
				"status":               activity.Status,
				"element_id":           activity.ElementID,
				"qc_id":                activity.QCID,
				"paper_id":             activity.PaperID,
				"qc_status":            activity.QCStatus,
				"qc":                   activity.QCID == userID,
				"mesh_mold_status":     activity.MeshMoldStatus,
				"reinforcement_status": activity.ReinforcementStatus,
			}

			// Check if the element type group already exists
			if _, exists := elementTypeMap[elementTypeID]; !exists {
				elementTypeMap[elementTypeID] = gin.H{
					"element_type_id": elementTypeID,
					"element_type":    elementType,
					"floor_id":        floorID,
					"activities":      []gin.H{},
				}
			}

			// Append activity to the appropriate group
			elementTypeMap[elementTypeID]["activities"] = append(elementTypeMap[elementTypeID]["activities"].([]gin.H), formattedActivity)
		}

		// Convert map to slice for JSON response
		var response []gin.H
		for _, groupedData := range elementTypeMap {
			response = append(response, groupedData)
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Activity",
			EventName:    "Get",
			Description:  "Get Activity With Element",
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

// func GetAllViews(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		sessionID := c.GetHeader("Authorization")
// 		if sessionID == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
// 			return
// 		}
// 		session, userName, err := GetSessionDetails(db, sessionID)
// 		if err != nil {
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
// 			return
// 		}

// 		user, err := storage.GetUserBySessionID(db, sessionID)
// 		if err != nil || user == nil {
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
// 			return
// 		}
// 		userID := user.ID

// 		projectIDParam := c.Param("project_id")
// 		if projectIDParam == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
// 			return
// 		}
// 		projectID, err := strconv.Atoi(projectIDParam)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID", "details": err.Error()})
// 			return
// 		}

// 		// Fetch project stages for the given project_id
// 		queryStages := `SELECT id, name, project_id, assigned_to, qc_assign, qc_id, paper_id, template_id,
// 		                "order", completion_stage, inventory_deduction
// 		                FROM project_stages WHERE project_id = $1 ORDER BY "order"`

// 		stageRows, err := db.Query(queryStages, projectID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages", "details": err.Error()})
// 			return
// 		}
// 		defer stageRows.Close()

// 		// Create a map of all project stages for easy lookup
// 		stageMap := make(map[int]models.ProjectStages)
// 		for stageRows.Next() {
// 			var stage models.ProjectStages
// 			var templateID, qcID, paperID, assignedTo sql.NullInt64

// 			err := stageRows.Scan(
// 				&stage.ID, &stage.Name, &stage.ProjectID, &assignedTo,
// 				&stage.QCAssign, &qcID, &paperID, &templateID,
// 				&stage.Order, &stage.CompletionStage, &stage.InventoryDeduction,
// 			)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stages", "details": err.Error()})
// 				return
// 			}

// 			// Convert sql.NullInt64 to int (if Valid) or set it to zero
// 			if assignedTo.Valid {
// 				stage.AssignedTo = int(assignedTo.Int64)
// 			} else {
// 				stage.AssignedTo = 0
// 			}

// 			if qcID.Valid {
// 				stage.QCID = int(qcID.Int64)
// 			} else {
// 				stage.QCID = 0
// 			}

// 			if paperID.Valid {
// 				stage.PaperID = int(paperID.Int64)
// 			} else {
// 				stage.PaperID = 0
// 			}

// 			if templateID.Valid {
// 				stage.TemplateID = int(templateID.Int64)
// 			} else {
// 				stage.TemplateID = 0
// 			}

// 			stageMap[stage.ID] = stage
// 		}

// 		// Fetch tasks
// 		queryTasks := `SELECT t.project_id, t.element_type_id, et.element_type_name, t.floor_id
//                 FROM task t
//                 LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
//                 WHERE t.project_id = $1 AND t.disable = false`

// 		rows, err := db.Query(queryTasks, projectID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tasks", "details": err.Error()})
// 			return
// 		}
// 		defer rows.Close()

// 		var tasks []models.ResponseTask
// 		for rows.Next() {
// 			var task models.ResponseTask
// 			var elementType sql.NullString
// 			err := rows.Scan(
// 				&task.ProjectID, &task.ElementTypeID, &elementType, &task.FloorID,
// 			)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading tasks", "details": err.Error()})
// 				return
// 			}
// 			task.ElementType = elementType.String

// 			queryActivities := `SELECT COUNT(*) FROM activity WHERE task_id IN (SELECT task_id FROM task WHERE element_type_id = $1 AND floor_id = $2 AND disable != true AND completed = false) `
// 			var activityCount int
// 			err = db.QueryRow(queryActivities, task.ElementTypeID, task.FloorID).Scan(&activityCount)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error checking activities", "details": err.Error()})
// 				return
// 			}

// 			if activityCount > 0 {
// 				tasks = append(tasks, task)
// 			}
// 		}

// 		// Fetch activities and process stages
// 		for i, task := range tasks {
// 			queryActivities := `SELECT a.id, a.task_id, a.project_id, a.name, a.priority, a.stage_id, s.name as stage_name,
// 				   				a.assigned_to, a.start_date, a.end_date, a.status, a.element_id,
// 				   				a.qc_id, a.paper_id, a.qc_status, a.mesh_mold_status, a.reinforcement_status, a.meshmold_qc_status, a.reinforcement_qc_status, a.stockyard_id,
// 								t.element_type_id , t.floor_id
// 								FROM activity a
// 								LEFT JOIN task t ON a.task_id = t.task_id
// 								LEFT JOIN project_stages s ON a.stage_id = s.id
// 								WHERE t.element_type_id = $1 AND t.floor_id = $2 AND a.completed = false`

// 			activityRows, err := db.Query(queryActivities, task.ElementTypeID, task.FloorID)
// 			log.Print(task.ElementTypeID)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
// 				return
// 			}
// 			defer activityRows.Close()

// 			var activities []models.Activity
// 			for activityRows.Next() {
// 				var activity models.Activity
// 				var paperID sql.NullInt64
// 				var elemTypeID int
// 				var floorID int
// 				var qcID sql.NullInt64
// 				var qcStatus, MeshMoldStatus, ReinforcementStatus sql.NullString
// 				var stageName sql.NullString
// 				var priority sql.NullString

// 				err := activityRows.Scan(
// 					&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &priority,
// 					&activity.StageID, &stageName, &activity.AssignedTo, &activity.StartDate, &activity.EndDate,
// 					&activity.Status, &activity.ElementID, &qcID, &paperID, &qcStatus, &MeshMoldStatus, &ReinforcementStatus, &activity.MeshMoldQCStatus, &activity.ReinforcementQCStatus, &activity.StockyardID, &elemTypeID, &floorID,
// 				)
// 				if err != nil {
// 					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading activities", "details": err.Error()})
// 					return
// 				}

// 				activity.StageName = stageName.String
// 				activity.Priority = priority.String

// 				activity.MeshMoldStatus = MeshMoldStatus.String
// 				activity.ReinforcementStatus = ReinforcementStatus.String

// 				activity.PaperID = int(paperID.Int64)
// 				// Set QC ID & Status if present
// 				if qcID.Valid {
// 					activity.QCID = int(qcID.Int64)
// 				} else {
// 					activity.QCID = 0
// 				}
// 				if qcStatus.Valid {
// 					activity.QCStatus = qcStatus.String
// 				} else {
// 					activity.QCStatus = ""
// 				}

// 				// Fetch element_type_id and stage_path for the current task
// 				var elementTypeID int
// 				err = db.QueryRow(`SELECT element_type_id FROM task WHERE task_id = $1`, activity.TaskID).Scan(&elementTypeID)
// 				if err != nil {
// 					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type", "details": err.Error()})
// 					return
// 				}

// 				var stagePath string
// 				err = db.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
// 				if err != nil {
// 					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stage path", "details": err.Error()})
// 					return
// 				}

// 				// Convert stage_path to array
// 				stagePath = strings.Trim(stagePath, "{}")
// 				stageIDs := make([]int, 0)
// 				for _, idStr := range strings.Split(stagePath, ",") {
// 					if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil {
// 						stageIDs = append(stageIDs, id)
// 					}
// 				}

// 				// Find special stages (Mesh & Mould, Reinforcement)
// 				meshMouldStageID := -1
// 				reinforcementStageID := -1
// 				for id, stage := range stageMap {
// 					if strings.EqualFold(stage.Name, "Mesh & Mould") {
// 						meshMouldStageID = id
// 					} else if strings.EqualFold(stage.Name, "Reinforcement") {
// 						reinforcementStageID = id
// 					}
// 				}

// 				// Build activity stages based on stage path
// 				var activityStages []models.ProjectStages
// 				for _, stageID := range stageIDs {
// 					if stage, exists := stageMap[stageID]; exists {
// 						stageCopy := stage

// 						// Set stage status
// 						if stageID < activity.StageID {
// 							stageCopy.Status = "completed"
// 							stageCopy.QCStatus = "completed"
// 						} else if stageID == activity.StageID {
// 							if activity.Status == "completed" {
// 								stageCopy.Status = "completed"
// 								stageCopy.QCStatus = "inprogress"
// 							} else {
// 								stageCopy.Status = "inprogress"
// 								stageCopy.QCStatus = "inprogress"
// 							}
// 						} else {
// 							stageCopy.Status = "pending"
// 							stageCopy.QCStatus = "pending"
// 						}

// 						// Handle editability
// 						stageCopy.Editable = "no"
// 						stageCopy.QCEditable = "no"

// 						// Special handling for Mesh & Mould and Reinforcement stages
// 						if (stageID == meshMouldStageID || stageID == reinforcementStageID) &&
// 							(stage.AssignedTo == userID || getNextStageAssignee(db, stageID, activity.TaskID) == userID) {
// 							stageCopy.Editable = "yes"
// 						} else if stageID == activity.StageID && stage.AssignedTo == userID {
// 							stageCopy.Editable = "yes"
// 						}

// 						// QC editability
// 						if stage.QCAssign && stage.QCID == userID {
// 							stageCopy.QCEditable = "yes"
// 						}

// 						activityStages = append(activityStages, stageCopy)
// 					}
// 				}

// 				// Convert ProjectStages to []string
// 				stageNames := make([]string, len(activityStages))
// 				for i, stage := range activityStages {
// 					stageNames[i] = stage.Name
// 				}
// 				activity.Stages = stageNames
// 				activities = append(activities, activity)
// 			}

// 			tasks[i].Activities = activities
// 		}

// 		// Return the tasks with activities and stage statuses
// 		c.JSON(http.StatusOK, tasks)

// 		log := models.ActivityLog{
// 			EventContext: "Kanban",
// 			EventName:    "Get",
// 			Description:  "Get All Kanban Views",
// 			UserName:     userName,
// 			HostName:     session.HostName,
// 			IPAddress:    session.IPAddress,
// 			CreatedAt:    time.Now(),
// 			ProjectID:    projectID,
// 		}
// 		if logErr := SaveActivityLog(db, log); logErr != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{
// 				"error":   "Project deleted but failed to log activity",
// 				"details": logErr.Error(),
// 			})
// 			return
// 		}
// 	}
// }

func GetAllViews(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ✅ Check session
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

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}
		userID := user.ID

		projectIDParam := c.Param("project_id")
		if projectIDParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}
		projectID, err := strconv.Atoi(projectIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID", "details": err.Error()})
			return
		}

		// ✅ Fetch project stages once
		queryStages := `SELECT id, name, project_id, assigned_to, qc_assign, qc_id, paper_id, template_id,
		                "order", completion_stage, inventory_deduction 
		                FROM project_stages WHERE project_id = $1 ORDER BY "order"`
		stageRows, err := db.Query(queryStages, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stages", "details": err.Error()})
			return
		}
		defer stageRows.Close()

		stageMap := make(map[int]models.ProjectStages)
		var meshMouldStageID, reinforcementStageID int
		for stageRows.Next() {
			var stage models.ProjectStages
			var templateID, qcID, paperID, assignedTo sql.NullInt64

			if err := stageRows.Scan(
				&stage.ID, &stage.Name, &stage.ProjectID, &assignedTo,
				&stage.QCAssign, &qcID, &paperID, &templateID,
				&stage.Order, &stage.CompletionStage, &stage.InventoryDeduction,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stages", "details": err.Error()})
				return
			}

			if assignedTo.Valid {
				stage.AssignedTo = int(assignedTo.Int64)
			}
			if qcID.Valid {
				stage.QCID = int(qcID.Int64)
			}
			if paperID.Valid {
				stage.PaperID = int(paperID.Int64)
			}
			if templateID.Valid {
				stage.TemplateID = int(templateID.Int64)
			}

			// cache special stage IDs
			if strings.EqualFold(stage.Name, "Mesh & Mould") {
				meshMouldStageID = stage.ID
			} else if strings.EqualFold(stage.Name, "Reinforcement") {
				reinforcementStageID = stage.ID
			}

			stageMap[stage.ID] = stage
		}

		// ✅ Fetch tasks + activity count in one go
		queryTasks := `
			SELECT t.project_id, t.element_type_id, et.element_type_name, t.floor_id, COUNT(a.id) AS activity_count
			FROM task t
			LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
			LEFT JOIN activity a ON a.task_id = t.task_id AND a.completed = false
			WHERE t.project_id = $1 AND t.disable = false
			GROUP BY t.project_id, t.element_type_id, et.element_type_name, t.floor_id
		`
		rows, err := db.Query(queryTasks, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tasks", "details": err.Error()})
			return
		}
		defer rows.Close()

		var tasks []models.ResponseTask
		for rows.Next() {
			var task models.ResponseTask
			var elementType sql.NullString
			var activityCount int

			if err := rows.Scan(&task.ProjectID, &task.ElementTypeID, &elementType, &task.FloorID, &activityCount); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading tasks", "details": err.Error()})
				return
			}

			task.ElementType = elementType.String
			if activityCount > 0 {
				tasks = append(tasks, task)
			}
		}

		// ✅ OPTIMIZED: Fetch all activities in a single query instead of N+1 queries
		// Build task filter conditions for IN clause
		if len(tasks) == 0 {
			c.JSON(http.StatusOK, tasks)
			return
		}

		// Create task key map for efficient lookup
		taskMap := make(map[string]int) // key: "element_type_id:floor_id" -> task index
		taskKeys := make([]string, len(tasks))
		for i, task := range tasks {
			key := fmt.Sprintf("%d:%d", task.ElementTypeID, task.FloorID)
			taskMap[key] = i
			taskKeys[i] = key
		}

		// Build WHERE clause with all task combinations
		var taskConditions []string
		var activityArgs []interface{}
		argIndex := 1
		for _, task := range tasks {
			taskConditions = append(taskConditions, fmt.Sprintf("(t.element_type_id = $%d AND t.floor_id = $%d)", argIndex, argIndex+1))
			activityArgs = append(activityArgs, task.ElementTypeID, task.FloorID)
			argIndex += 2
		}

		// Single query to fetch all activities for all tasks
		queryActivities := fmt.Sprintf(`
			SELECT a.id, a.task_id, a.project_id, a.name, a.priority, a.stage_id, s.name as stage_name,
				   a.assigned_to, a.start_date, a.end_date, a.status, a.element_id, 
				   a.qc_id, a.paper_id, a.qc_status, a.mesh_mold_status, a.reinforcement_status, 
				   a.meshmold_qc_status, a.reinforcement_qc_status, a.stockyard_id,
				   t.element_type_id, t.floor_id, etp.stage_path
			FROM activity a
			LEFT JOIN task t ON a.task_id = t.task_id
			LEFT JOIN project_stages s ON a.stage_id = s.id
			LEFT JOIN element_type_path etp ON t.element_type_id = etp.element_type_id
			WHERE a.completed = false 
			  AND t.project_id = $%d
			  AND (%s)
		`, argIndex, strings.Join(taskConditions, " OR "))
		activityArgs = append(activityArgs, projectID)

		// Use request context with timeout
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		activityRows, err := db.QueryContext(ctx, queryActivities, activityArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
			return
		}
		defer activityRows.Close()

		// Group activities by task key
		activitiesByTask := make(map[string][]models.Activity)
		for activityRows.Next() {
			var activity models.Activity
			var paperID, qcID sql.NullInt64
			var qcStatus, meshMoldStatus, reinforcementStatus sql.NullString
			var stageName, priority sql.NullString
			var elemTypeID, floorID int
			var stagePath sql.NullString

			if err := activityRows.Scan(
				&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &priority,
				&activity.StageID, &stageName, &activity.AssignedTo, &activity.StartDate, &activity.EndDate,
				&activity.Status, &activity.ElementID, &qcID, &paperID, &qcStatus,
				&meshMoldStatus, &reinforcementStatus, &activity.MeshMoldQCStatus,
				&activity.ReinforcementQCStatus, &activity.StockyardID,
				&elemTypeID, &floorID, &stagePath,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading activities", "details": err.Error()})
				return
			}

			// safe null handling
			activity.Priority = priority.String
			activity.StageName = stageName.String
			activity.MeshMoldStatus = meshMoldStatus.String
			activity.ReinforcementStatus = reinforcementStatus.String
			if paperID.Valid {
				activity.PaperID = int(paperID.Int64)
			}
			if qcID.Valid {
				activity.QCID = int(qcID.Int64)
			}
			if qcStatus.Valid {
				activity.QCStatus = qcStatus.String
			}

			// ✅ Convert stage_path once
			stageIDs := make([]int, 0)
			if stagePath.Valid {
				stageStr := strings.Trim(stagePath.String, "{}")
				for _, idStr := range strings.Split(stageStr, ",") {
					if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil {
						stageIDs = append(stageIDs, id)
					}
				}
			}

			// ✅ Build stage progression
			var activityStages []models.ProjectStages
			for _, stageID := range stageIDs {
				if stage, exists := stageMap[stageID]; exists {
					stageCopy := stage

					if stageID < activity.StageID {
						stageCopy.Status = "completed"
						stageCopy.QCStatus = "completed"
					} else if stageID == activity.StageID {
						if activity.Status == "completed" {
							stageCopy.Status = "completed"
							stageCopy.QCStatus = "inprogress"
						} else {
							stageCopy.Status = "inprogress"
							stageCopy.QCStatus = "inprogress"
						}
					} else {
						stageCopy.Status = "pending"
						stageCopy.QCStatus = "pending"
					}

					stageCopy.Editable = "no"
					stageCopy.QCEditable = "no"

					// Special handling
					if (stageID == meshMouldStageID || stageID == reinforcementStageID) &&
						(stage.AssignedTo == userID || getNextStageAssignee(db, stageID, activity.TaskID) == userID) {
						stageCopy.Editable = "yes"
					} else if stageID == activity.StageID && stage.AssignedTo == userID {
						stageCopy.Editable = "yes"
					}
					if stage.QCAssign && stage.QCID == userID {
						stageCopy.QCEditable = "yes"
					}

					activityStages = append(activityStages, stageCopy)
				}
			}

			// ✅ Collect stage names
			stageNames := make([]string, len(activityStages))
			for i, s := range activityStages {
				stageNames[i] = s.Name
			}
			activity.Stages = stageNames

			// Group activity by task key
			taskKey := fmt.Sprintf("%d:%d", elemTypeID, floorID)
			if _, exists := taskMap[taskKey]; exists {
				activitiesByTask[taskKey] = append(activitiesByTask[taskKey], activity)
			}
		}

		// Check for errors during iteration
		if err := activityRows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating activities", "details": err.Error()})
			return
		}

		// Assign activities to tasks
		for i, task := range tasks {
			taskKey := fmt.Sprintf("%d:%d", task.ElementTypeID, task.FloorID)
			if activities, exists := activitiesByTask[taskKey]; exists {
				tasks[i].Activities = activities
			} else {
				tasks[i].Activities = []models.Activity{} // Empty slice if no activities
			}
		}

		// ✅ Final response
		c.JSON(http.StatusOK, tasks)

		// ✅ Log event
		log := models.ActivityLog{
			EventContext: "Kanban",
			EventName:    "Get",
			Description:  "Get All Kanban Views",
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

// GetAllCompleteProduction godoc
// @Summary      Get production history for project
// @Tags         production
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/production_history/{project_id} [get]
func GetAllCompleteProduction(db *sql.DB) gin.HandlerFunc {
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

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer func() {
			if p := recover(); p != nil {
				tx.Rollback()
				panic(p)
			} else if err != nil {
				tx.Rollback()
			} else {
				err = tx.Commit()
			}
		}()

		user, err := storage.GetUserBySessionID(db, sessionID) // Updated to use tx
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}
		userID := user.ID

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		query := `
			SELECT 
				cp.id, cp.task_id, cp.activity_id, cp.project_id, cp.element_id, cp.element_type_id,
				cp.user_id, COALESCE(cp.stage_id, 0) AS stage_id, cp.started_at, cp.updated_at,
				COALESCE(cp.status, '') AS status, COALESCE(cp.floor_id, 0) AS floor_id,


				COALESCE(et.element_type, '') AS element_type,
				COALESCE(et.element_type_name, '') AS element_type_name, 
				COALESCE(et.element_type_version, '') AS element_type_version,
				COALESCE(e.element_name, '') AS element_name,
				COALESCE(ps.name, '') AS stage_name,

				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN COALESCE(hierarchyPrecast.id, 0)
					ELSE COALESCE(hierarchyPrecast.parent_id, 0)
				END AS tower,
				COALESCE(towerPrecast.name, hierarchyPrecast.name, '') AS tower_name,
				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN 'common'
					ELSE COALESCE(NULLIF(hierarchyPrecast.name, ''), 'common')
				END AS floor_name
			FROM complete_production cp
			LEFT JOIN element_type et ON cp.element_type_id = et.element_type_id
			LEFT JOIN element e ON cp.element_id = e.id
			LEFT JOIN project_stages ps ON cp.stage_id = ps.id
			LEFT JOIN element_type_hierarchy_quantity ethq ON cp.element_type_id = ethq.element_type_id AND cp.floor_id = ethq.hierarchy_id
			LEFT JOIN precast hierarchyPrecast ON cp.floor_id = hierarchyPrecast.id
			LEFT JOIN precast towerPrecast ON hierarchyPrecast.parent_id = towerPrecast.id
			WHERE cp.user_id = $1 AND cp.project_id = $2 AND cp.updated_at IS NOT NULL
			ORDER BY cp.updated_at DESC;
		`

		rows, err := tx.Query(query, userID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch records", "details": err.Error()})
			return
		}
		defer rows.Close()

		results := []models.CompleteProductionResponse{}

		for rows.Next() {
			var cp models.CompleteProduction
			var response models.CompleteProductionResponse

			err := rows.Scan(
				&cp.ID, &cp.TaskID, &cp.ActivityID, &cp.ProjectID, &cp.ElementID, &cp.ElementTypeID,
				&cp.UserID, &cp.StageID, &cp.StartedAt, &cp.UpdatedAt, &cp.Status, &cp.FloorID,
				&response.ElementType, &response.ElementTypeName, &response.ElementTypeVersion,
				&response.ElementName, &response.StageName,
				&response.TowerID, &response.TowerName, &response.FloorName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read record", "details": err.Error()})
				return
			}

			if response.ElementType == "" {
				log.Printf("[DEBUG] Missing element_type for element_type_id: %d (element_id: %d, project_id: %d)", cp.ElementTypeID, cp.ElementID, cp.ProjectID)
			}

			response.UpdatedAt = ""
			if cp.UpdatedAt != nil {
				response.UpdatedAt = cp.UpdatedAt.Format(time.RFC3339)
			}

			response.ID = cp.ID
			response.TaskID = cp.TaskID
			response.ActivityID = cp.ActivityID
			response.ProjectID = cp.ProjectID
			response.ElementID = cp.ElementID
			response.ElementTypeID = cp.ElementTypeID
			response.UserID = cp.UserID
			response.StageID = cp.StageID
			response.Status = cp.Status
			response.FloorID = cp.FloorID
			response.StartedAt = cp.StartedAt.Format(time.RFC3339)
			response.UpdatedAt = ""
			if cp.UpdatedAt != nil {
				response.UpdatedAt = cp.UpdatedAt.Format(time.RFC3339)
			}

			results = append(results, response)
		}

		c.JSON(http.StatusOK, results)

		log := models.ActivityLog{
			EventContext: "Element",
			EventName:    "Get",
			Description:  "Get All Complete Element Production History",
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

// ExportAllViewsPDF godoc
// @Summary      Export project views as PDF
// @Tags         export
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {file}    file  "PDF file"
// @Failure      400         {object}  models.ErrorResponse
// @Router       /api/export_project_views/{project_id} [get]
func ExportAllViewsPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var projectName string
		err = db.QueryRow(`SELECT name FROM project WHERE project_id = $1`, projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		type StageStatus struct {
			Name   string
			Status string
		}

		type ElementView struct {
			ElementType string
			Date        time.Time
			ElementName string
			Stages      []StageStatus
		}

		var views []ElementView

		rows, err := db.Query(`
			SELECT 
				t.element_type_id, et.element_type_name, COUNT(t.task_id) AS total_tasks
			FROM task t
			LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
			WHERE t.project_id = $1 AND t.disable = false
			GROUP BY t.element_type_id, et.element_type_name
			ORDER BY et.element_type_name
		`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var elementTypeID int
			var elementTypeName string
			var total int
			if err := rows.Scan(&elementTypeID, &elementTypeName, &total); err != nil {
				continue
			}

			var stagePath pq.Int64Array
			err = db.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
			if err != nil {
				continue
			}

			stageNames := []string{}
			for _, sid := range stagePath {
				var name string
				err = db.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, sid).Scan(&name)
				if err == nil {
					stageNames = append(stageNames, name)
				}
			}

			taskRows, err := db.Query(`
				SELECT 
					t.name, t.start_date,
					a.element_id,
					a.mesh_mold_status, a.meshmold_qc_status,
					a.reinforcement_status, a.reinforcement_qc_status,
					a.status, a.qc_status
				FROM task t
				LEFT JOIN activity a 
					ON a.task_id = t.task_id AND a.project_id = t.project_id AND a.completed = false
				WHERE t.element_type_id = $1 AND t.project_id = $2
			`, elementTypeID, projectID)
			if err != nil {
				continue
			}

			for taskRows.Next() {
				var taskName string
				var startDate time.Time
				var elementId sql.NullInt64
				var mesh, meshQc, reinf, reinfQc, status, qc sql.NullString

				if err := taskRows.Scan(&taskName, &startDate, &elementId, &mesh, &meshQc, &reinf, &reinfQc, &status, &qc); err != nil {
					continue
				}

				elementName := "-"
				if elementId.Valid {
					_ = db.QueryRow(`SELECT element_id FROM element WHERE id = $1`, elementId.Int64).Scan(&elementName)
				}

				stages := []StageStatus{}
				meshStage := "Not Started"
				reinfStage := "Not Started"

				if mesh.String == "completed" && reinf.String == "completed" && meshQc.String == "completed" && reinfQc.String == "completed" {
					meshStage, reinfStage = "Completed", "Completed"
				} else if mesh.String == "completed" && reinf.String == "completed" && meshQc.String == "completed" && reinfQc.String != "completed" {
					meshStage, reinfStage = "Completed", "QC In Progress"
				} else if mesh.String == "completed" && reinf.String == "completed" && meshQc.String != "completed" {
					meshStage, reinfStage = "QC In Progress", "QC In Progress"
				} else if mesh.String == "completed" && meshQc.String != "completed" && reinf.String != "completed" {
					meshStage, reinfStage = "QC In Progress", "In Progress"
				} else if mesh.String != "completed" && reinf.String != "completed" {
					meshStage, reinfStage = "In Progress", "In Progress"
				}

				stages = append(stages, StageStatus{"Mesh & Mould", meshStage})
				stages = append(stages, StageStatus{"Reinforcement", reinfStage})

				for i, name := range stageNames {
					if i < 2 {
						continue
					}
					s := "Not Started"
					if status.String == "completed" && qc.String != "completed" {
						s = "In Progress"
					} else if status.String == "completed" && qc.String == "completed" {
						s = "Completed"
					}
					stages = append(stages, StageStatus{name, s})
				}

				views = append(views, ElementView{
					ElementType: elementTypeName,
					Date:        startDate,
					ElementName: elementName,
					Stages:      stages,
				})
			}
			taskRows.Close()
		}

		if len(views) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"message": "No elements found"})
			return
		}

		// --- ✅ Generate PDF ---
		pdf := gofpdf.New("L", "mm", "A4", "")
		pdf.AddPage()
		pdf.SetMargins(10, 10, 10)
		pdf.SetFont("Arial", "B", 16)

		// --- ✅ Title + Generated Time in same line ---
		title := fmt.Sprintf("Production View Report (Project: %s)", projectName)
		generatedOn := fmt.Sprintf("Generated On: %s", time.Now().Format("02-Jan-2006 15:04:05"))

		pageWidth, _ := pdf.GetPageSize()
		rightMargin, _, _, _ := pdf.GetMargins()
		stringWidth := pdf.GetStringWidth(generatedOn)

		// Left title
		pdf.CellFormat(0, 10, title, "", 0, "L", false, 0, "")
		// Move to right for generated time
		pdf.SetXY(pageWidth-rightMargin-stringWidth, 10)
		pdf.CellFormat(stringWidth, 10, generatedOn, "", 1, "R", false, 0, "")

		pdf.Ln(8)
		pdf.SetFont("Arial", "", 11)

		currentType := ""
		for _, v := range views {
			if v.ElementType != currentType {
				if currentType != "" {
					pdf.Ln(8)
				}
				currentType = v.ElementType
				pdf.SetFont("Arial", "B", 13)
				pdf.Cell(0, 8, fmt.Sprintf("Element Type: %s", currentType))
				pdf.Ln(8)

				// Table header
				pdf.SetFont("Arial", "B", 11)
				pdf.SetFillColor(230, 230, 230)
				pdf.CellFormat(40, 8, "Date", "1", 0, "C", true, 0, "")
				pdf.CellFormat(40, 8, "ElementID", "1", 0, "C", true, 0, "")
				for _, s := range v.Stages {
					pdf.CellFormat(40, 8, s.Name, "1", 0, "C", true, 0, "")
				}
				pdf.Ln(-1)
			}

			// ✅ Center-align Date and Element Name
			pdf.SetFont("Arial", "", 10)
			pdf.SetTextColor(0, 0, 0)
			pdf.CellFormat(40, 8, v.Date.Format("02-Jan-2006"), "1", 0, "C", false, 0, "")
			pdf.CellFormat(40, 8, v.ElementName, "1", 0, "C", false, 0, "")
			for _, s := range v.Stages {
				switch s.Status {
				case "QC In Progress":
					pdf.SetTextColor(0, 0, 255)
				case "Completed":
					pdf.SetTextColor(0, 128, 0)
				default:
					pdf.SetTextColor(0, 0, 0)
				}
				pdf.CellFormat(40, 8, s.Status, "1", 0, "C", false, 0, "")
			}
			pdf.Ln(-1)
			pdf.SetTextColor(0, 0, 0)
		}

		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=project_%s_all_views.pdf", projectName))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}
	}
}

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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GetRectificationHandler godoc
// @Summary      Get rectification elements by project
// @Tags         rectification
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/rectification/{project_id} [get]
func GetRectificationHandler(db *sql.DB) gin.HandlerFunc {
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

		projectID := c.Param("project_id")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		query := `
			SELECT 
				e.id, e.element_type_id, e.element_id, e.element_name, 
				e.project_id, e.created_by, e.created_at, COALESCE(e.status, '') AS status, 
				e.element_type_version, e.update_at, e.target_location,
				CASE WHEN ps.id IS NOT NULL THEN true ELSE false END AS in_stockyard,
				CASE WHEN se.id IS NOT NULL THEN true ELSE false END AS in_erection
			FROM element e
			LEFT JOIN precast_stock ps ON ps.element_id = e.id
			LEFT JOIN stock_erected se ON se.element_id = e.id
			WHERE e.disable = true AND e.project_id = $1
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data", "details": err.Error()})
			return
		}
		defer rows.Close()

		type RectificationElement struct {
			ID                 int       `json:"id"`
			ElementTypeID      int       `json:"element_type_id"`
			ElementId          string    `json:"element_id"`
			ElementName        string    `json:"element_name"`
			ProjectID          int       `json:"project_id"`
			CreatedBy          string    `json:"created_by"`
			CreatedAt          time.Time `json:"created_at"`
			Status             string    `json:"status"`
			StatusText         string    `json:"status_text"`
			ElementTypeVersion string    `json:"element_type_version"`
			UpdateAt           time.Time `json:"update_at"`
			TargetLocation     int       `json:"target_location"`
			InStockyard        bool      `json:"in_stockyard"`
			InErection         bool      `json:"in_erection"`
		}

		elements := make([]RectificationElement, 0)

		for rows.Next() {
			var el RectificationElement
			err := rows.Scan(
				&el.ID, &el.ElementTypeID, &el.ElementId, &el.ElementName,
				&el.ProjectID, &el.CreatedBy, &el.CreatedAt, &el.Status,
				&el.ElementTypeVersion, &el.UpdateAt, &el.TargetLocation,
				&el.InStockyard, &el.InErection,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse result", "details": err.Error()})
				return
			}

			// Set human-readable status
			if el.InErection {
				el.StatusText = "Erected"
			} else if el.InStockyard {
				el.StatusText = "InStockyard"
			} else {
				el.StatusText = "Pending"
			}

			elements = append(elements, el)
		}

		var name string
		_ = db.QueryRow(`SELECT name FROM project Where project_id=$1`, projectID).Scan(&name)

		projectid, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		c.JSON(http.StatusOK, elements)

		log := models.ActivityLog{
			EventContext: "Rectification",
			EventName:    "Get",
			Description:  "Get rectification in" + name,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectid,
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

// UpdateRectificationHandler godoc
// @Summary      Update rectification
// @Tags         rectification
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Rectification update"
// @Success      200   {object}  object
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/update_rectification [put]
func UpdateRectificationHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"err": err.Error()})
			return
		}

		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var input []models.RectificationInput
		if err := c.BindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// Track unique project IDs for notifications
		projectIDs := make(map[int]bool)
		for _, el := range input {
			projectIDs[el.ProjectID] = true
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		for _, el := range input {
			// Save rectification info
			_, err := tx.Exec(`
				INSERT INTO element_rectification (element_id, project_id, user_id, comments, image, status)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, el.ElementID, el.ProjectID, user.ID, el.Comments, el.Image, el.Status)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save rectification", "details": err.Error()})
				return
			}

			if strings.ToLower(el.Status) == "approved" {
				_, err := tx.Exec("UPDATE element SET disable = false WHERE id = $1", el.ElementID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update element", "details": err.Error()})
					return
				}
			} else if strings.ToLower(el.Status) == "rejected" {
				// Step 1: Fetch old element
				var oldElement models.Element
				query := `SELECT id, element_type_id, element_id, element_name, project_id, created_by, created_at, element_type_version, update_at, target_location, disable FROM element WHERE id = $1`
				err := tx.QueryRow(query, el.ElementID).Scan(
					&oldElement.Id, &oldElement.ElementTypeID, &oldElement.ElementId, &oldElement.ElementName,
					&oldElement.ProjectID, &oldElement.CreatedBy, &oldElement.CreatedAt, &oldElement.ElementTypeVersion,
					&oldElement.UpdateAt, &oldElement.TargetLocation, &oldElement.Disable)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch old element", "details": err.Error()})
					return
				}

				// Step 2: Save to deleted_element
				_, err = tx.Exec(`INSERT INTO deleted_element (id, element_type_id, element_id, element_name, project_id, created_by, created_at, status, element_type_version, update_at, target_location, deleted_by)
								VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
					oldElement.Id, oldElement.ElementTypeID, oldElement.ElementId, oldElement.ElementName,
					oldElement.ProjectID, oldElement.CreatedBy, oldElement.CreatedAt, oldElement.Status,
					oldElement.ElementTypeVersion, oldElement.UpdateAt, oldElement.TargetLocation, user.ID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to archive element", "details": err.Error()})
					return
				}

				// Step 3: Delete old element
				_, err = tx.Exec("DELETE FROM element WHERE id = $1", el.ElementID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete element", "details": err.Error()})
					return
				}

				// Step 4: Generate new element_id
				var elementType, namingConvention string

				err = tx.QueryRow("SELECT naming_convention FROM precast WHERE id = $1",
					oldElement.TargetLocation).Scan(&namingConvention)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type", "details": err.Error()})
					return
				}

				var totalCount int
				err = tx.QueryRow("SELECT element_type, total_count_element FROM element_type WHERE element_type_id = $1",
					oldElement.ElementTypeID).Scan(&elementType, &totalCount)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type", "details": err.Error()})
					return
				}

				newElementID := repository.GenerateElementID(elementType, namingConvention, totalCount+1)
				newElement := oldElement
				newElement.Id = repository.GenerateRandomNumber()
				newElement.ElementId = newElementID
				newElement.CreatedAt = time.Now()
				newElement.UpdateAt = time.Now()
				newElement.Status = 1 // fresh

				// Step 5: Insert new element
				_, err = tx.Exec(`INSERT INTO element (id, element_type_id, element_id, element_name, project_id, created_by, created_at, status, element_type_version, update_at, target_location)
							VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
					newElement.Id, newElement.ElementTypeID, newElement.ElementId, newElement.ElementName,
					newElement.ProjectID, newElement.CreatedBy, newElement.CreatedAt, newElement.Status,
					newElement.ElementTypeVersion, newElement.UpdateAt, newElement.TargetLocation)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create new element", "details": err.Error()})
					return
				}

				// Step 6: Update total_count_element
				_, err = tx.Exec(`UPDATE element_type SET total_count_element = total_count_element + 1 WHERE element_type_id = $1`, oldElement.ElementTypeID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update element count", "details": err.Error()})
					return
				}
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status. Must be 'approved' or 'rejected'"})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Rectification updated successfully"})

		// Send notifications for each unique project
		for projectID := range projectIDs {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", projectID)
			}

			// Send notification to the admin user who updated the rectification
			notif := models.Notification{
				UserID:    user.ID,
				Message:   fmt.Sprintf("Rectification updated in project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/retification", projectID),
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
				fmt.Sprintf("Rectification updated in project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/retification", projectID))
		}

		// Use first project ID for activity log (or 0 if no projects)
		activityLogProjectID := 0
		for projectID := range projectIDs {
			activityLogProjectID = projectID
			break
		}

		log := models.ActivityLog{
			EventContext: "Rectification",
			EventName:    "Update",
			Description:  "Update rectification",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    activityLogProjectID,
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

// GetDeletedElementsWithDrawings godoc
// @Summary      Get deleted elements with drawings by project
// @Tags         rectification
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/deleted_elements/{project_id} [get]
func GetDeletedElementsWithDrawings(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// ----------- FETCH DELETED ELEMENTS -----------
		deletedQuery := `
			SELECT 
				de.id, 
				de.element_type_id, 
				de.element_id, 
				de.element_name, 
				de.project_id, 
				de.created_by, 
				de.created_at, 
				de.status, 
				de.element_type_version, 
				de.update_at, 
				de.deleted_by,
				de.target_location, 
				COALESCE(u.first_name || ' ' || u.last_name, '') AS deleted_by_name
			FROM 
				deleted_element de
			LEFT JOIN 
				users u ON de.deleted_by = u.id
			WHERE 
				de.project_id = $1
		`

		rows, err := db.Query(deletedQuery, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query deleted elements", "details": err.Error()})
			return
		}
		defer rows.Close()

		var result []models.DeletedElementWithDrawings

		for rows.Next() {
			var d models.DeletedElementWithDrawings

			err := rows.Scan(
				&d.ID,
				&d.ElementTypeID,
				&d.ElementID,
				&d.ElementName,
				&d.ProjectID,
				&d.CreatedBy,
				&d.CreatedAt,
				&d.Status,
				&d.ElementTypeVersion,
				&d.UpdateAt,
				&d.TargetLocation,
				&d.DeletedBy,
				&d.DeletedByName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan deleted element", "details": err.Error()})
				return
			}

			// Fetch drawings for this element
			d.Drawings = fetchDrawingsWithRevisions(db, d.ElementTypeID)

			result = append(result, d)
		}

		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Rectification",
			EventName:    "Get",
			Description:  "Get deleted elements",
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

func fetchDrawingsWithRevisions(db *sql.DB, elementTypeID int) []models.DrawingResponse {
	query := `
		SELECT 
			d.drawing_id, d.current_version, d.drawing_type_id, d.comments, d.file,
			dr.drawing_revision_id, dr.version, dr.drawing_type_id, dr.comments, dr.file
		FROM 
			drawings d
		LEFT JOIN 
			drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
		WHERE 
			d.element_type_id = $1
	`

	rows, err := db.Query(query, elementTypeID)
	if err != nil {
		log.Printf("Error fetching drawings: %v", err)
		return nil
	}
	defer rows.Close()

	drawingMap := make(map[int]*models.DrawingResponse)

	for rows.Next() {
		var drawingID int
		var drawing models.DrawingResponse
		var revision models.DrawingsRevisionResponse

		err := rows.Scan(
			&drawingID,
			&drawing.CurrentVersion,
			&drawing.DrawingTypeId,
			&drawing.Comments,
			&drawing.File,
			&revision.DrawingRevisionId,
			&revision.Version,
			&revision.DrawingTypeId,
			&revision.Comments,
			&revision.File,
		)
		if err != nil {
			log.Printf("Scan error in drawings: %v", err)
			continue
		}

		if _, exists := drawingMap[drawingID]; !exists {
			drawing.DrawingId = drawingID
			drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}
			drawingMap[drawingID] = &drawing
		}

		if revision.DrawingRevisionId != 0 {
			drawingMap[drawingID].DrawingsRevision = append(drawingMap[drawingID].DrawingsRevision, revision)
		}
	}

	var drawings []models.DrawingResponse
	for _, d := range drawingMap {
		drawings = append(drawings, *d)
	}

	return drawings
}

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

func FetchElementTypeName(elementTypeID int) (string, error) {
	db := storage.GetDB()

	var elementTypeName string
	query := `
		SELECT element_type_name 
		FROM element_type 
		WHERE element_type_id = $1
	`
	err := db.QueryRow(query, elementTypeID).Scan(&elementTypeName)
	if err != nil {
		return "", fmt.Errorf("call failed to fetch element type name: %v", err)
	}

	return elementTypeName, nil
}

func CreateDrawing(db *sql.DB) gin.HandlerFunc {
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
		var drawing models.Drawings
		if err := c.ShouldBindJSON(&drawing); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		drawing.CreatedAt = time.Now()

		query := `
        INSERT INTO drawings (
             project_id, current_version, created_at, created_by,
            drawing_type_id, update_at, comments, file,element_type_id
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        RETURNING drawing_id`
		err = storage.GetDB().QueryRow(query,

			drawing.ProjectId,
			drawing.CurrentVersion,
			drawing.CreatedAt,
			drawing.CreatedBy,
			drawing.DrawingTypeId,
			time.Now(),
			drawing.Comments,
			drawing.File,
			drawing.ElementTypeID,
		).Scan(&drawing.DrawingsId)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("database insert error: %v", err)})
			return
		}

		c.JSON(http.StatusOK, drawing)

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", drawing.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", drawing.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the drawing
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New drawing created for project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawing.ProjectId),
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
		sendProjectNotifications(db, drawing.ProjectId,
			fmt.Sprintf("New drawing created for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", drawing.ProjectId))

		activityLog := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Create",
			Description:  "Create Drwaing " + strconv.Itoa(drawing.DrawingsId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawing.ProjectId,
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

func UpdateDrawing(c *gin.Context, drawing models.Drawings) (int, error) {

	db := storage.GetDB()

	// Query to check if the drawing exists based on element_type_id and drawing_type_id
	err := db.QueryRow(
		`SELECT drawing_id FROM drawings WHERE element_type_id = $1 AND drawing_type_id = $2`,
		drawing.ElementTypeID, drawing.DrawingTypeId,
	).Scan(&drawing.DrawingsId)

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("drawing not found with given element_type_id and drawing_type_id")
		}
		return 0, fmt.Errorf("database error while retrieving drawing: %v", err)
	}

	// Fetch the current drawing details
	var currentDrawing models.Drawings
	err = db.QueryRow(
		`SELECT drawing_id, project_id, current_version, created_at, created_by, drawing_type_id, update_at, comments, file, element_type_id 
		FROM drawings WHERE drawing_id = $1 ORDER BY created_at DESC`, drawing.DrawingsId).Scan(
		&currentDrawing.DrawingsId,
		&currentDrawing.ProjectId,
		&currentDrawing.CurrentVersion,
		&currentDrawing.CreatedAt,
		&currentDrawing.CreatedBy,
		&currentDrawing.DrawingTypeId,
		&currentDrawing.UpdateAt,
		&currentDrawing.Comments,
		&currentDrawing.File,
		&currentDrawing.ElementTypeID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("drawing not found")
		}
		return 0, fmt.Errorf("failed to fetch current drawing: %v", err)
	}

	// Insert into the drawings_revision table
	var drawingRevision models.DrawingsRevision
	drawingRevision.DrawingsRevisionId = repository.GenerateRandomNumber()

	_, err = db.Exec(
		`INSERT INTO drawings_revision 
		(parent_drawing_id, project_id, version, created_at, created_by, drawing_type_id, comments, file, drawing_revision_id, element_type_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		currentDrawing.DrawingsId, currentDrawing.ProjectId, currentDrawing.CurrentVersion, currentDrawing.CreatedAt,
		currentDrawing.CreatedBy, currentDrawing.DrawingTypeId, currentDrawing.Comments, currentDrawing.File,
		drawingRevision.DrawingsRevisionId, currentDrawing.ElementTypeID,
	)

	if err != nil {
		return 0, fmt.Errorf("failed to insert into drawings_revision: %v", err)
	}

	// Update drawing details dynamically
	drawing.UpdateAt = time.Now()
	drawing.CurrentVersion = repository.GenerateVersionCode(currentDrawing.CurrentVersion)

	var updates []string
	var fields []interface{}
	placeholderIndex := 1

	// Dynamically add fields for update
	if drawing.CurrentVersion != "" {
		updates = append(updates, fmt.Sprintf("current_version = $%d", placeholderIndex))
		fields = append(fields, drawing.CurrentVersion)
		placeholderIndex++
	}
	if drawing.Comments != "" {
		updates = append(updates, fmt.Sprintf("comments = $%d", placeholderIndex))
		fields = append(fields, drawing.Comments)
		placeholderIndex++
	}
	if drawing.File != "" {
		updates = append(updates, fmt.Sprintf("file = $%d", placeholderIndex))
		fields = append(fields, drawing.File)
		placeholderIndex++
	}
	if !drawing.UpdateAt.IsZero() {
		updates = append(updates, fmt.Sprintf("update_at = $%d", placeholderIndex))
		fields = append(fields, drawing.UpdateAt)
		placeholderIndex++
	}

	// Ensure there is something to update
	if len(updates) == 0 {
		return 0, fmt.Errorf("no valid fields to update")
	}

	// Execute the update query on the drawings table
	sqlStatement := fmt.Sprintf("UPDATE drawings SET %s WHERE drawing_id = $%d", strings.Join(updates, ", "), placeholderIndex)
	fields = append(fields, currentDrawing.DrawingsId)

	_, err = db.Exec(sqlStatement, fields...)
	if err != nil {
		return 0, fmt.Errorf("failed to update drawing: %v", err)
	}

	return drawingRevision.DrawingsRevisionId, nil

}

// DeleteDrawing deletes a drawing by ID.
// @Summary Delete drawing
// @Description Deletes drawing by id. Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Param id path int true "Drawing ID"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_delete/{id} [delete]
func DeleteDrawing(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Drawing ID"})
			return
		}

		// Fetch drawing info before deletion for notifications
		var projectID int
		err = db.QueryRow("SELECT project_id FROM drawings WHERE drawing_id = $1", id).Scan(&projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Drawing not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing details"})
			return
		}

		result, err := db.Exec("DELETE FROM drawings WHERE drawings_id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete drawings"})
			return
		}

		if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "drawings not found"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Drawings successfully deleted"})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the drawing
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Drawing deleted from project: %s", projectName),
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
			fmt.Sprintf("Drawing deleted from project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/drawing", projectID))

		activityLog := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Delete",
			Description:  "Delete Drawing with ID " + strconv.Itoa(id),
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
// GetAllDrawings returns all drawings.
// @Summary Get all drawings
// @Description Returns all drawings. Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Success 200 {array} models.DrawingResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing [get]
func GetAllDrawings(db *sql.DB) gin.HandlerFunc {
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

		// Query to get all drawings from the 'drawings' table
		rows, err := db.Query(`
        SELECT 
            drawing_id,
            project_id,
            current_version,
            drawing_type_id,
            comments,
            file,
            element_type_id 
        FROM drawings order by drawing_id`)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawings: " + err.Error()})
			return
		}

		var drawingResponses []models.DrawingResponse

		// Iterate through each drawing row
		for rows.Next() {
			var drawing models.Drawings
			err := rows.Scan(
				&drawing.DrawingsId,
				&drawing.ProjectId,
				&drawing.CurrentVersion,
				&drawing.DrawingTypeId,
				&drawing.Comments,
				&drawing.File,
				&drawing.ElementTypeID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing: " + err.Error()})
				return
			}

			// Fetch the DrawingTypeName for the drawing
			DrawingTypeName, err := FetchDrawingTypeName(drawing.DrawingTypeId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing type name: " + err.Error()})
				return
			}
			drawing.DrawingTypeName = DrawingTypeName

			// Fetch revisions for the current drawing
			revisionsQuery := `
		SELECT version, drawing_type_id, comments, file, drawing_revision_id
		FROM drawings_revision
		WHERE parent_drawing_id = $1
		ORDER BY version DESC`

			revisionRows, err := db.Query(revisionsQuery, drawing.DrawingsId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revisions: " + err.Error()})
				return
			}
			defer revisionRows.Close()

			var revisions []models.DrawingsRevisionResponse

			// Iterate through the revisions and map them to the response struct
			for revisionRows.Next() {
				var revision models.DrawingsRevision
				err := revisionRows.Scan(
					&revision.Version,
					&revision.DrawingsTypeId,
					&revision.Comments,
					&revision.File,
					&revision.DrawingsRevisionId,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing revision: " + err.Error()})
					return
				}

				// Fetch the DrawingTypeName for the revision
				DrawingTypeName, err := FetchDrawingTypeName(revision.DrawingsTypeId)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing type name: " + err.Error()})
					return
				}
				revision.DrawingTypeName = DrawingTypeName
				// Map to the response struct
				revisionResponse := models.DrawingsRevisionResponse{
					Version:         revision.Version,
					DrawingTypeId:   revision.DrawingsTypeId,
					DrawingTypeName: revision.DrawingTypeName,

					Comments:          revision.Comments,
					File:              revision.File,
					DrawingRevisionId: revision.DrawingsRevisionId,
				}

				revisions = append(revisions, revisionResponse)
			}

			// Ensure DrawingsRevision is always an empty array if no revisions are found
			if len(revisions) == 0 {
				revisions = []models.DrawingsRevisionResponse{}
			}
			ElementTypeName, err := FetchElementTypeName(drawing.ElementTypeID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type name: " + err.Error()})
				return
			}
			// Populate the response struct
			drawingResponse := models.DrawingResponse{
				DrawingId:        drawing.DrawingsId,
				ProjectId:        drawing.ProjectId,
				CurrentVersion:   drawing.CurrentVersion,
				DrawingTypeName:  drawing.DrawingTypeName,
				DrawingTypeId:    drawing.DrawingTypeId,
				Comments:         drawing.Comments,
				File:             drawing.File,
				ElementTypeID:    drawing.ElementTypeID,
				ElementTypeName:  ElementTypeName,
				DrawingsRevision: revisions,
			}

			// Append to the response list
			drawingResponses = append(drawingResponses, drawingResponse)
		}

		// Check if no drawings were found
		if len(drawingResponses) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"message": "No drawings found"})
			return
		}

		// Return the drawings in the custom response format
		c.JSON(http.StatusOK, drawingResponses)

		log := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Get",
			Description:  "Fetched all drawings",
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

// GetDrawingByDrawingID returns a single drawing by drawing_id.
// @Summary Get drawing by ID
// @Description Returns one drawing by drawing_id. Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Param drawing_id path int true "Drawing ID"
// @Success 200 {object} models.DrawingResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_get/{drawing_id} [get]
func GetDrawingByDrawingID(db *sql.DB) gin.HandlerFunc {
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

		// Retrieve the drawing_id from the query parameters and convert it to an integer

		drawingID, err := strconv.Atoi(c.Param("drawing_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Query to get the drawing from the 'drawings' table by drawing_id
		row := db.QueryRow(`
        SELECT 
            drawing_id,
            project_id,
            current_version,
            drawing_type_id,
            comments,
            file,
            element_type_id 
        FROM drawings
        WHERE drawing_id = $1`, drawingID)

		var drawing models.Drawings

		// Scan the drawing details into the model
		err = row.Scan(
			&drawing.DrawingsId,
			&drawing.ProjectId,
			&drawing.CurrentVersion,
			&drawing.DrawingTypeId,
			&drawing.Comments,
			&drawing.File,
			&drawing.ElementTypeID,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"message": "No drawing found for drawing_id: " + strconv.Itoa(drawingID)})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawing: " + err.Error()})
			}
			return
		}

		// Fetch the DrawingTypeName for the drawing
		DrawingTypeName, err := FetchDrawingTypeName(drawing.DrawingTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing type name: " + err.Error()})
			return
		}
		drawing.DrawingTypeName = DrawingTypeName

		// Fetch revisions for the drawing
		revisionsQuery := `
	SELECT version, drawing_type_id, comments, file, drawing_revision_id
		FROM drawings_revision
		WHERE parent_drawing_id = $1
		ORDER BY version DESC`

		revisionRows, err := db.Query(revisionsQuery, drawing.DrawingsId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revisions: " + err.Error()})
			return
		}
		defer revisionRows.Close()

		var revisions []models.DrawingsRevisionResponse

		// Iterate through the revisions and map them to the response struct
		for revisionRows.Next() {
			var revision models.DrawingsRevision
			err := revisionRows.Scan(
				&revision.Version,
				&revision.DrawingsTypeId,
				&revision.Comments,
				&revision.File,
				&revision.DrawingsRevisionId,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing revision: " + err.Error()})
				return
			}

			// Fetch the DrawingTypeName for the revision
			DrawingTypeName, err := FetchDrawingTypeName(revision.DrawingsTypeId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing type name: " + err.Error()})
				return
			}
			revision.DrawingTypeName = DrawingTypeName
			// Map to the response struct
			revisionResponse := models.DrawingsRevisionResponse{
				Version:         revision.Version,
				DrawingTypeId:   revision.DrawingsTypeId,
				DrawingTypeName: revision.DrawingTypeName,

				Comments:          revision.Comments,
				File:              revision.File,
				DrawingRevisionId: revision.DrawingsRevisionId,
			}

			revisions = append(revisions, revisionResponse)
		}

		// Ensure DrawingsRevision is always an empty array if no revisions are found
		if len(revisions) == 0 {
			revisions = []models.DrawingsRevisionResponse{}
		}
		ElementTypeName, err := FetchElementTypeName(drawing.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Element type name: " + err.Error()})
			return
		}
		// Populate the response struct
		drawingResponse := models.DrawingResponse{
			DrawingId:        drawing.DrawingsId,
			ProjectId:        drawing.ProjectId,
			CurrentVersion:   drawing.CurrentVersion,
			DrawingTypeName:  drawing.DrawingTypeName,
			DrawingTypeId:    drawing.DrawingTypeId,
			Comments:         drawing.Comments,
			File:             drawing.File,
			ElementTypeID:    drawing.ElementTypeID,
			ElementTypeName:  ElementTypeName,
			DrawingsRevision: revisions, // Include the revisions in the response
		}

		// Return the drawing in the custom response format
		c.JSON(http.StatusOK, drawingResponse)
		log := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Get",
			Description:  "Fetched drawing with ID " + strconv.Itoa(drawingID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    drawing.ProjectId, // Use the project ID from the drawing
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

// GetDrawingsByProjectID returns drawings for a project.
// @Summary Get drawings by project ID
// @Description Returns all drawings for the given project_id. Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.DrawingResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawing_fetch/{project_id} [get]
func GetDrawingsByProjectID(db *sql.DB) gin.HandlerFunc {
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

		// Parse pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "10")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 100 {
			limit = 10
		}

		offset := (page - 1) * limit

		// Get total count for pagination
		var totalCount int
		countQuery := `SELECT COUNT(*) FROM drawings WHERE project_id = $1`
		err = db.QueryRow(countQuery, projectID).Scan(&totalCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get total count: " + err.Error()})
			return
		}

		// Cache for drawing type names
		drawingTypeCache := make(map[int]string)

		// Pre-fetch all drawing types for this project
		drawingTypeRows, err := db.Query(`
		SELECT drawing_type_id, drawing_type_name 
		FROM drawing_type 
		WHERE project_id = $1`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing types"})
			return
		}
		defer drawingTypeRows.Close()

		for drawingTypeRows.Next() {
			var id int
			var name string
			if err := drawingTypeRows.Scan(&id, &name); err != nil {
				continue
			}
			drawingTypeCache[id] = name
		}

		// Query to get paginated drawings from the 'drawings' table by project_id
		rows, err := db.Query(`
        SELECT 
            drawing_id,
            project_id,
            current_version,
            drawing_type_id,
            comments,
            file,
            element_type_id 
        FROM drawings
        WHERE project_id = $1
        ORDER BY drawing_id
        LIMIT $2 OFFSET $3`, projectID, limit, offset)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawings: " + err.Error()})
			return
		}
		defer rows.Close()

		var drawingResponses []models.DrawingResponse

		// Iterate through each drawing row
		for rows.Next() {
			var drawing models.Drawings
			err := rows.Scan(
				&drawing.DrawingsId,
				&drawing.ProjectId,
				&drawing.CurrentVersion,
				&drawing.DrawingTypeId,
				&drawing.Comments,
				&drawing.File,
				&drawing.ElementTypeID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing: " + err.Error()})
				return
			}

			// Get drawing type name from cache
			drawing.DrawingTypeName = drawingTypeCache[drawing.DrawingTypeId]

			// Fetch revisions for the current drawing
			revisionsQuery := `
		SELECT version, drawing_type_id, comments, file, drawing_revision_id
		FROM drawings_revision
		WHERE parent_drawing_id = $1
		ORDER BY version DESC`

			revisionRows, err := db.Query(revisionsQuery, drawing.DrawingsId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revisions: " + err.Error()})
				return
			}
			defer revisionRows.Close()

			var revisions []models.DrawingsRevisionResponse

			// Iterate through the revisions and map them to the response struct
			for revisionRows.Next() {
				var revision models.DrawingsRevision
				err := revisionRows.Scan(
					&revision.Version,
					&revision.DrawingsTypeId,
					&revision.Comments,
					&revision.File,
					&revision.DrawingsRevisionId,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing revision: " + err.Error()})
					return
				}

				// Get drawing type name from cache
				revision.DrawingTypeName = drawingTypeCache[revision.DrawingsTypeId]

				revisionResponse := models.DrawingsRevisionResponse{
					Version:           revision.Version,
					DrawingTypeId:     revision.DrawingsTypeId,
					DrawingTypeName:   revision.DrawingTypeName,
					Comments:          revision.Comments,
					File:              revision.File,
					DrawingRevisionId: revision.DrawingsRevisionId,
				}

				revisions = append(revisions, revisionResponse)
			}

			if len(revisions) == 0 {
				revisions = []models.DrawingsRevisionResponse{}
			}

			ElementTypeName, err := FetchElementTypeName(drawing.ElementTypeID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type name: " + err.Error()})
				return
			}

			drawingResponse := models.DrawingResponse{
				DrawingId:        drawing.DrawingsId,
				ProjectId:        drawing.ProjectId,
				CurrentVersion:   drawing.CurrentVersion,
				DrawingTypeName:  drawing.DrawingTypeName,
				DrawingTypeId:    drawing.DrawingTypeId,
				Comments:         drawing.Comments,
				File:             drawing.File,
				ElementTypeID:    drawing.ElementTypeID,
				ElementTypeName:  ElementTypeName,
				DrawingsRevision: revisions,
			}

			drawingResponses = append(drawingResponses, drawingResponse)
		}

		// Calculate pagination metadata
		totalPages := (totalCount + limit - 1) / limit
		hasNext := page < totalPages
		hasPrev := page > 1

		// Prepare response with pagination metadata
		response := gin.H{
			"data": drawingResponses,
			"pagination": gin.H{
				"current_page": page,
				"limit":        limit,
				"total_count":  totalCount,
				"total_pages":  totalPages,
				"has_next":     hasNext,
				"has_prev":     hasPrev,
			},
		}

		if len(drawingResponses) == 0 {
			response["message"] = "No drawings found for project_id"
		}

		c.JSON(http.StatusOK, response)
		log := models.ActivityLog{
			EventContext: "Drawing",
			EventName:    "Get",
			Description:  "Fetched drawings for project_id " + strconv.Itoa(projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID, // Use the project ID from the request
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

// GetDrawingsByElementType returns drawings for an element type.
// @Summary Get drawings by element type
// @Description Returns drawings for the given element_type_id. Requires Authorization header.
// @Tags Drawings
// @Accept json
// @Produce json
// @Param element_type_id path int true "Element Type ID"
// @Success 200 {array} object
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/drawings_by_element_type/{element_type_id} [get]
func GetDrawingsByElementType(c *gin.Context) {
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

	// Get element_type_id from route parameter
	elementTypeID := c.Param("element_type_id")
	if elementTypeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "element_type_id parameter is required"})
		return
	}

	// Query to fetch drawings with their revisions for a specific element type
	query := `
	SELECT 
		d.drawing_id,
		d.element_type_id,
		d.current_version,
		d.created_by,
		d.drawing_type_id,
		dt.drawing_type_name,
		d.update_at,
		d.comments,
		d.file,
		dr.parent_drawing_id,
		dr.version as revision_version,
		dr.created_by as revision_created_by,
		dr.drawing_type_id as revision_drawing_type_id,
		dt2.drawing_type_name as revision_drawing_type_name,
		dr.comments as revision_comments,
		dr.file as revision_file,
		dr.drawing_revision_id,
		dr.element_type_id as revision_element_type_id,
		dr.created_at as revision_created_at,
		dr.created_at as revision_updated_at
	FROM drawings d
	LEFT JOIN drawing_type dt ON dt.drawing_type_id = d.drawing_type_id
	LEFT JOIN drawings_revision dr ON dr.parent_drawing_id = d.drawing_id
	LEFT JOIN drawing_type dt2 ON dt2.drawing_type_id = dr.drawing_type_id
	WHERE d.element_type_id = $1
	ORDER BY d.drawing_id, dr.drawing_revision_id`

	rows, err := db.Query(query, elementTypeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve drawings: " + err.Error()})
		return
	}
	defer rows.Close()

	// Map to store drawings and their revisions
	drawingsMap := make(map[int]*DrawingResponse)

	for rows.Next() {
		var (
			drawingID, elementTypeID, drawingTypeID    int
			currentVersion, createdBy, drawingTypeName string
			updatedAt, comments, file                  string
			parentDrawingID                            sql.NullInt64
			revisionVersion, revisionCreatedBy         sql.NullString
			revisionDrawingTypeID                      sql.NullInt64
			revisionDrawingTypeName                    sql.NullString
			revisionComments, revisionFile             sql.NullString
			drawingRevisionID                          sql.NullInt64
			revisionElementTypeID                      sql.NullInt64
			revisionCreatedAt, revisionUpdatedAt       sql.NullTime
		)

		err := rows.Scan(
			&drawingID, &elementTypeID, &currentVersion, &createdBy, &drawingTypeID,
			&drawingTypeName, &updatedAt, &comments, &file,
			&parentDrawingID, &revisionVersion, &revisionCreatedBy, &revisionDrawingTypeID,
			&revisionDrawingTypeName, &revisionComments, &revisionFile,
			&drawingRevisionID, &revisionElementTypeID, &revisionCreatedAt, &revisionUpdatedAt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing data: " + err.Error()})
			return
		}

		// Create or get existing drawing
		if _, exists := drawingsMap[drawingID]; !exists {
			drawingsMap[drawingID] = &DrawingResponse{
				DrawingID:        drawingID,
				ElementTypeID:    elementTypeID,
				CurrentVersion:   currentVersion,
				CreatedBy:        createdBy,
				DrawingTypeID:    drawingTypeID,
				DrawingTypeName:  drawingTypeName,
				UpdatedAt:        updatedAt,
				Comments:         comments,
				File:             file,
				DrawingsRevision: []DrawingRevision{},
			}
		}

		// Add revision if it exists
		if parentDrawingID.Valid && drawingRevisionID.Valid {
			revision := DrawingRevision{
				ParentDrawingID:    int(parentDrawingID.Int64),
				Version:            revisionVersion.String,
				CreatedBy:          revisionCreatedBy.String,
				DrawingTypeID:      int(revisionDrawingTypeID.Int64),
				DrawingTypeName:    revisionDrawingTypeName.String,
				Comments:           revisionComments.String,
				File:               revisionFile.String,
				DrawingRevisionID:  int(drawingRevisionID.Int64),
				ElementTypeID:      int(revisionElementTypeID.Int64),
				CreatedAtFormatted: formatTime(revisionCreatedAt),
				UpdatedAtFormatted: formatTime(revisionUpdatedAt),
			}
			drawingsMap[drawingID].DrawingsRevision = append(drawingsMap[drawingID].DrawingsRevision, revision)
		}
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
		return
	}

	// Convert map to slice
	var result []DrawingResponse
	for _, drawing := range drawingsMap {
		result = append(result, *drawing)
	}

	c.JSON(http.StatusOK, result)

	log := models.ActivityLog{
		EventContext: "Drawing Revision",
		EventName:    "Get",
		Description:  fmt.Sprintf("Retrieved drawings for element type ID: %s", elementTypeID),
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    0,
	}

	if logErr := SaveActivityLog(db, log); logErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to log activity",
			"details": logErr.Error(),
		})
		return
	}
}

// Helper function to format time
func formatTime(t sql.NullTime) string {
	if t.Valid {
		return t.Time.Format("2006-01-02 15:04")
	}
	return ""
}

// Response structures for drawings
type DrawingResponse struct {
	DrawingID        int               `json:"drawing_id"`
	ElementTypeID    int               `json:"Element_type_id"`
	CurrentVersion   string            `json:"current_version"`
	CreatedBy        string            `json:"created_by"`
	DrawingTypeID    int               `json:"drawing_type_id"`
	DrawingTypeName  string            `json:"drawing_type_name"`
	UpdatedAt        string            `json:"updated_at"`
	Comments         string            `json:"comments"`
	File             string            `json:"file"`
	DrawingsRevision []DrawingRevision `json:"drawingsRevision"`
}

type DrawingRevision struct {
	ParentDrawingID    int    `json:"parent_drawing_id"`
	Version            string `json:"version"`
	CreatedBy          string `json:"created_by"`
	DrawingTypeID      int    `json:"drawing_type_id"`
	DrawingTypeName    string `json:"drawing_type_name"`
	Comments           string `json:"comments"`
	File               string `json:"file"`
	DrawingRevisionID  int    `json:"drawing_revision_id"`
	ElementTypeID      int    `json:"Element_type_id"`
	CreatedAtFormatted string `json:"created_at_formatted"`
	UpdatedAtFormatted string `json:"updated_at_formatted"`
}

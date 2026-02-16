package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

// Element godoc
// @Summary      Create elements
// @Description  Create multiple elements for an element type in a project
// @Tags         elements
// @Accept       json
// @Produce      json
// @Param        body  body      models.ElementInput  true  "Element creation input"
// @Success      200   {object}  models.MessageResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/element_create [post]
func Element(c *gin.Context) {
	var jsondata models.ElementInput
	if err := c.ShouldBindJSON(&jsondata); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Call CreateElements with jsondata
	CreateElements(c, jsondata)
}
func CreateElements(c *gin.Context, jsondata models.ElementInput) {
	log.Println("CreateElements called")
	db := storage.GetDB()

	createdBy := "Admin" // replace with actual user if needed

	stmtInsert, err := db.Prepare(`
        INSERT INTO element (
            element_type_id, element_id, element_name, project_id,
            created_by, created_at, status, element_type_version,
            update_at, target_location
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    `)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare insert statement", "details": err.Error()})
		return
	}
	defer stmtInsert.Close()

	// Insert elements
	for i := 1; i <= jsondata.Quantity; i++ {
		elementID := repository.GenerateElementID(jsondata.ElementType, jsondata.NamingConvention, jsondata.TotalCountElement+i)
		_, err := stmtInsert.Exec(
			jsondata.ElementTypeID, elementID, jsondata.ElementTypeName, jsondata.ProjectID,
			createdBy, time.Now(), 1, jsondata.ElementTypeVersion,
			time.Now(), jsondata.HierarchyId, // Make sure the field name matches your struct
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   fmt.Sprintf("Failed to insert element (i=%d)", i),
				"details": err.Error(),
			})
			return
		}
	}

	// Update the total_count_element
	_, err = db.Exec(`UPDATE element_type SET total_count_element = $1 WHERE element_type_id = $2`, jsondata.TotalCountElement+jsondata.Quantity, jsondata.ElementTypeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update element_type count", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Elements inserted successfully"})

	// Get session ID from header for notifications
	sessionID := c.GetHeader("Authorization")
	if sessionID != "" {
		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", jsondata.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", jsondata.ProjectID)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the elements
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New elements created (%d) for project: %s", jsondata.Quantity, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", jsondata.ProjectID),
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
		sendProjectNotifications(db, jsondata.ProjectID,
			fmt.Sprintf("New elements created (%d) for project: %s", jsondata.Quantity, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", jsondata.ProjectID))
	}

}

func UpdtateElements(c *gin.Context, jsondata models.ElementInput) {
	log.Println("Element Function was called")
	db := storage.GetDB()

	var createdBy string

	fmt.Println("createdBy:", createdBy)
	createdBy = "Admin"
	stmtInsert, err := db.Prepare(`INSERT INTO element ( element_type_id,
		 element_id, element_name, project_id, created_by, created_at, status, element_type_version, update_at,target_location)
                               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`)
	if err != nil {
		fmt.Println("Failed to prepare SQL statement:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare SQL statement: " + err.Error()})
		return
	}
	defer stmtInsert.Close()

	queryLastValue := `SELECT element_id FROM element WHERE element_type_id = $1 ORDER BY element_id DESC LIMIT 1`
	rowlast := db.QueryRow(queryLastValue, jsondata.ElementTypeID)

	var lastvalue string
	var lastValue int

	if err := rowlast.Scan(&lastvalue); err != nil {
		if err == sql.ErrNoRows {
			// Initialize to 0 for first-time insertion
			lastValue = 0
			fmt.Println("No previous elements found. Starting with element ID 1")
		} else {
			fmt.Println("Error querying element_type table:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element_type data"})
			return
		}
	} else {
		// Strip letters from lastvalue
		lastvalue = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) {
				return -1 // Remove the letter
			}
			return r // Keep non-letters
		}, lastvalue)

		if len(lastvalue) >= 4 {
			lastvalue = lastvalue[len(lastvalue)-4:]
			fmt.Println("Last 4 digits:", lastvalue)
		} else {
			fmt.Println("Not enough digits available.")
			lastValue = 0 // Initialize to 0 if not enough digits
		}

		var err error
		lastValue, err = strconv.Atoi(lastvalue)
		if err != nil {
			fmt.Println("Error converting to integer:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to convert element_id to integer"})
			return
		}
	}

	query := `SELECT element_type,element_type_name, project_id, element_type_version, total_count_element FROM element_type WHERE element_type_id = $1`
	row := db.QueryRow(query, jsondata.ElementTypeID)
	fmt.Println("Element Type Id", jsondata.ElementTypeID)
	var elementName string
	var ElementType string
	var projectId, TotalCountElement int
	var elementTypeVersion string
	if err := row.Scan(&ElementType, &elementName, &projectId, &elementTypeVersion, &TotalCountElement); err != nil {
		fmt.Println("Error querying element_type table:", err) // Log the error to the console
		fmt.Println("Element Type Id", jsondata.ElementTypeID)
		// Log the error details for debugging
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Fail to retrieve element_type",
			"details": err.Error(), // Include the database error details in the response
		})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
		return
	}
	defer tx.Rollback()

	for i := 1; i <= jsondata.Quantity; i++ {
		elementID := repository.GenerateElementID(ElementType, jsondata.NamingConvention, lastValue+i)
		TotalCountElement++
		element := models.Element{

			ElementTypeID:      jsondata.ElementTypeID,
			ElementId:          elementID,
			ElementName:        elementName,
			ProjectID:          projectId,
			CreatedBy:          createdBy,
			CreatedAt:          time.Now(),
			Status:             1,
			ElementTypeVersion: elementTypeVersion,
			UpdateAt:           time.Now(),
			TargetLocation:     jsondata.HierarchyId,
		}

		_, err := tx.Stmt(stmtInsert).Exec(element.ElementTypeID, element.ElementId, element.ElementName, element.ProjectID, element.CreatedBy, element.CreatedAt, element.Status, element.ElementTypeVersion, element.UpdateAt, element.TargetLocation)
		if err != nil {
			fmt.Println("Error inserting element:", err) // Log the error for debugging
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert element: " + err.Error()})
			return
		}
		fmt.Print("i=", i)
	}

	if err := tx.Commit(); err != nil {
		fmt.Println("Failed to commit transaction:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Update the total_count_element
	if _, err := db.Exec("UPDATE element_type SET total_count_element = $1 WHERE element_type_id = $2", TotalCountElement, jsondata.ElementTypeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update element type count"})
		return
	}

	// Get session ID from header for notifications
	sessionID := c.GetHeader("Authorization")
	if sessionID != "" {
		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the elements
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New elements created (%d) for project: %s", jsondata.Quantity, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectId),
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
		sendProjectNotifications(db, projectId,
			fmt.Sprintf("New elements created (%d) for project: %s", jsondata.Quantity, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectId))
	}
}

// DeleteElement removes an element by ID.
// DeleteElement godoc
// @Summary      Delete element
// @Description  Delete an element by ID (requires session)
// @Tags         elements
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Element ID"
// @Success      200  {object}  models.MessageResponse
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/element_delete/{id} [delete]
func DeleteElement(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Element ID"})
			return
		}

		var projectID int
		var elementName string
		err = db.QueryRow(`SELECT project_id, element_name from element WHERE id=$1`, id).Scan(&projectID, &elementName)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		result, err := db.Exec("DELETE FROM element WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete Element"})
			return
		}

		if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Element successfully deleted"})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the element
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Element deleted: %s from project: %s", elementName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID),
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
			fmt.Sprintf("Element deleted: %s from project: %s", elementName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID))

		activityLog := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Delete",
			Description:  fmt.Sprintf("Element Deleted %d", id),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
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

// GetElementsWithDrawingsByProjectId returns paginated elements with drawings for a project.
// GetElementsWithDrawingsByProjectId godoc
// @Summary      Get elements with drawings by project
// @Description  Get paginated elements with drawing info for a project (query: page, limit)
// @Tags         elements
// @Accept       json
// @Produce      json
// @Param        project_id  path      int     true   "Project ID"
// @Param        page       query     int     false  "Page (default 1)"
// @Param        limit      query     int     false  "Limit (default 25)"
// @Success      200        {object}  object
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      500        {object}  models.ErrorResponse
// @Router       /api/element_get/{project_id} [get]
func GetElementsWithDrawingsByProjectId(db *sql.DB) gin.HandlerFunc {
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

		projectId, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Project ID"})
			return
		}

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "25")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 25
		}

		// Read filter query params (comma-separated supported)
		parseList := func(q string) map[string]struct{} {
			out := make(map[string]struct{})
			if q == "" {
				return out
			}
			parts := strings.Split(q, ",")
			for _, p := range parts {
				v := strings.TrimSpace(strings.ToLower(p))
				if v != "" {
					out[v] = struct{}{}
				}
			}
			return out
		}

		// Parse hierarchy_id as integers
		parseIntList := func(q string) map[int]struct{} {
			out := make(map[int]struct{})
			if q == "" {
				return out
			}
			parts := strings.Split(q, ",")
			for _, p := range parts {
				v := strings.TrimSpace(p)
				if v != "" {
					if id, err := strconv.Atoi(v); err == nil {
						out[id] = struct{}{}
					}
				}
			}
			return out
		}

		elementTypeNameFilters := parseList(c.Query("element_type_name"))
		statusFilters := parseList(c.Query("status"))
		hierarchyIdFilters := parseIntList(c.Query("hierarchy_id"))
		elementIdFilters := parseList(c.Query("element_id"))

		// Step 1: Collect all candidate element IDs and apply filters
		baseRows, err := db.Query(`
            SELECT e.id, COALESCE(e.element_id, '') AS element_id, COALESCE(et.element_type_name, '') AS element_type_name, COALESCE(e.target_location, 0) AS target_location
            FROM element e
            LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
            WHERE e.project_id = $1
            ORDER BY e.id
        `, projectId)
		if err != nil {
			log.Printf("Database base query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed", "details": err.Error()})
			return
		}
		defer baseRows.Close()

		type candidate struct {
			id              int
			elementId       string
			elementTypeName string
			targetLocation  sql.NullInt64
		}

		candidates := make([]candidate, 0, 256)
		for baseRows.Next() {
			var cnd candidate
			var etn string
			if err := baseRows.Scan(&cnd.id, &cnd.elementId, &etn, &cnd.targetLocation); err != nil {
				log.Println("Scan base row error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}
			cnd.elementTypeName = etn
			candidates = append(candidates, cnd)
		}

		// Helper for set membership
		inSet := func(set map[string]struct{}, v string) bool {
			if len(set) == 0 {
				return true
			}
			_, ok := set[strings.ToLower(v)]
			return ok
		}

		filteredIDs := make([]int, 0, len(candidates))
		// Cache status to avoid duplicate DB hits
		statusCache := make(map[int]string)

		// Helper for integer set membership
		inIntSet := func(set map[int]struct{}, v int) bool {
			if len(set) == 0 {
				return true
			}
			_, ok := set[v]
			return ok
		}

		for _, cnd := range candidates {
			// element_id filter
			if len(elementIdFilters) > 0 && !inSet(elementIdFilters, cnd.elementId) {
				continue
			}

			// element_type_name filter
			if len(elementTypeNameFilters) > 0 && !inSet(elementTypeNameFilters, cnd.elementTypeName) {
				continue
			}

			// hierarchy_id filter
			if len(hierarchyIdFilters) > 0 {
				if !cnd.targetLocation.Valid || cnd.targetLocation.Int64 == 0 {
					// No target location; cannot match hierarchy_id filters
					continue
				}
				if !inIntSet(hierarchyIdFilters, int(cnd.targetLocation.Int64)) {
					continue
				}
			}

			// status filter
			if len(statusFilters) > 0 {
				st, ok := statusCache[cnd.id]
				if !ok {
					st = getElementStatus(db, cnd.id)
					statusCache[cnd.id] = st
				}
				if !inSet(statusFilters, st) {
					continue
				}
			}

			filteredIDs = append(filteredIDs, cnd.id)
		}

		// Pagination over filtered IDs
		totalElements := len(filteredIDs)
		totalPages := 0
		if limit > 0 {
			totalPages = (totalElements + limit - 1) / limit
		}
		start := (page - 1) * limit
		end := start + limit
		if start > totalElements {
			start = totalElements
		}
		if end > totalElements {
			end = totalElements
		}
		pagedIDs := filteredIDs[start:end]

		if len(pagedIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"Elements": []models.ElementResponse{},
				"pagination": gin.H{
					"total_elements": totalElements,
					"total_pages":    totalPages,
					"current_page":   page,
					"limit":          limit,
					"has_next":       page < totalPages,
					"has_prev":       page > 1,
				},
			})
			return
		}

		// Build placeholders for IN clause
		placeholders := make([]string, len(pagedIDs))
		args := make([]interface{}, 0, len(pagedIDs)+1)
		args = append(args, projectId)
		for i, id := range pagedIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, id)
		}

		query := fmt.Sprintf(`
	        SELECT e.id, e.element_id, e.element_name, e.project_id, e.element_type_version, e.element_type_id,
		       et.element_type_name, et.thickness, et.length, et.height, et.volume, et.mass, et.area, et.width,
	               COALESCE(d.drawing_id, 0) AS drawing_id, COALESCE(d.current_version, '') AS current_version,
	               COALESCE(d.drawing_type_id, 0) AS drawing_type_id, COALESCE(d.comments, '') AS comments,
	               COALESCE(d.file, '') AS file,
	               COALESCE(dr.version, '') AS version, COALESCE(dr.drawing_type_id, 0) AS revision_drawing_type_id,
	               COALESCE(dr.comments, '') AS revision_comments, COALESCE(dr.file, '') AS revision_file,
	               COALESCE(dr.drawing_revision_id, 0) AS drawing_revision_id,
			   e.target_location
	        FROM element e
	        LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
	        LEFT JOIN drawings d ON e.element_type_id = d.element_type_id
	        LEFT JOIN drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
	        WHERE e.project_id = $1 AND e.id IN (%s)
	        ORDER BY e.id
	        `, strings.Join(placeholders, ","))

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("Database query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database query failed",
				"details": err.Error(),
			})
			return
		}

		elements := map[int]*models.ElementResponse{}

		for rows.Next() {
			var element models.ElementResponse
			var drawing models.DrawingResponse
			var revision models.DrawingsRevisionResponse

			var elementTypeName sql.NullString
			var thickness sql.NullFloat64
			var length sql.NullFloat64
			var height sql.NullFloat64
			var volume sql.NullFloat64
			var mass sql.NullFloat64
			var area sql.NullFloat64
			var width sql.NullFloat64
			var targetLocation sql.NullInt64

			err := rows.Scan(
				&element.ID, &element.ElementID, &element.ElementName, &element.ProjectID,
				&element.ElementTypeVersion, &element.ElementTypeID, &elementTypeName,
				&thickness, &length, &height, &volume, &mass, &area, &width,
				&drawing.DrawingId, &drawing.CurrentVersion, &drawing.DrawingTypeId,
				&drawing.Comments, &drawing.File,
				&revision.Version, &revision.DrawingTypeId, &revision.Comments,
				&revision.File, &revision.DrawingRevisionId, &targetLocation,
			)
			if err != nil {
				log.Println("Scan error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			element.Thickness = thickness.Float64
			element.Length = length.Float64
			element.Height = height.Float64
			element.Volume = volume.Float64
			element.Mass = mass.Float64
			element.Area = area.Float64
			element.Width = width.Float64

			element.Status = getElementStatus(db, element.ID)

			if targetLocation.Valid {
				tower, floor, err := getTowerAndFloor(db, targetLocation.Int64)
				if err != nil {
					log.Printf("Error fetching tower/floor: %v", err)
				} else {
					element.Tower = tower
					element.Floor = floor
				}
			}

			element.ElementTypeName = elementTypeName.String

			if _, exists := elements[element.ID]; !exists {
				element.Drawings = []models.DrawingResponse{}
				elements[element.ID] = &element
			}

			if drawing.DrawingId != 0 {
				e := elements[element.ID]
				found := false

				for i := range e.Drawings {
					if e.Drawings[i].DrawingId == drawing.DrawingId {
						e.Drawings[i].DrawingsRevision = append(e.Drawings[i].DrawingsRevision, revision)
						found = true
						break
					}
				}

				if !found {
					drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}
					if revision.DrawingRevisionId != 0 {
						drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
					}
					e.Drawings = append(e.Drawings, drawing)
				}
			}
		}

		var result []models.ElementResponse
		for _, element := range elements {
			result = append(result, *element)
		}

		c.JSON(http.StatusOK, gin.H{
			"Elements": result,
			"pagination": gin.H{
				"total_elements": totalElements,
				"total_pages":    totalPages,
				"current_page":   page,
				"limit":          limit,
				"has_next":       page < totalPages,
				"has_prev":       page > 1,
			},
		})

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  "Get Elements With Drawings",
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

func getElementStatus(db *sql.DB, elementID int) string {
	var stageID int
	var stageName string

	// 1. Check in activity table for Production status
	err := db.QueryRow(`SELECT stage_id FROM activity WHERE element_id = $1 LIMIT 1`, elementID).Scan(&stageID)
	if err == nil {
		err = db.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, stageID).Scan(&stageName)
		if err == nil {
			return fmt.Sprintf("In Production (%s)", stageName)
		}
		return "In Production"
	}

	// 2. Check in precast_stock for stockyard/dispatch/erection status
	var dispatchStatus, orderByErection, erected bool
	err = db.QueryRow(`SELECT dispatch_status, order_by_erection, erected FROM precast_stock WHERE element_id = $1 LIMIT 1`, elementID).Scan(&dispatchStatus, &orderByErection, &erected)
	if err == nil {
		switch {
		case erected && orderByErection && dispatchStatus:
			return "In Erection (Erected)"
		case !erected && orderByErection && dispatchStatus:
			return "In Stockyard (Ordered)"
		case dispatchStatus && !erected && !orderByErection:
			return "In Dispatch"
		default:
			return "In Stockyard"
		}
	}

	return "Not Started"
}

func getTowerAndFloor(db *sql.DB, targetLocationID int64) (tower string, floor string, err error) {
	var name sql.NullString
	var parentID sql.NullInt64

	// Fetch hierarchy details (could be a tower or floor)
	err = db.QueryRow(`SELECT name, parent_id FROM precast WHERE id = $1`, targetLocationID).Scan(&name, &parentID)
	if err != nil {
		return "", "", err
	}

	// If parent_id exists, this hierarchy is a floor, parent is the tower
	if parentID.Valid && parentID.Int64 != 0 {
		// This is a floor - parent is the tower
		floor = name.String
		if floor == "" {
			floor = "common"
		}

		// Fetch tower name from parent
		err = db.QueryRow(`SELECT name FROM precast WHERE id = $1`, parentID.Int64).Scan(&name)
		if err != nil {
			return "", "", err
		}
		tower = name.String
	} else {
		// No parent - this hierarchy is a tower itself
		tower = name.String
		floor = "common" // Default floor name when no floor exists
	}

	return tower, floor, nil
}

// GetElementsWithDrawingsByElementId returns element(s) with drawings by element ID.
// GetElementsWithDrawingsByElementId godoc
// @Summary      Get elements with drawings by element ID
// @Description  Get element(s) with drawing info by element_id (numeric or string)
// @Tags         elements
// @Accept       json
// @Produce      json
// @Param        element_id  path      string  true  "Element ID"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Failure      500         {object}  models.ErrorResponse
// @Router       /api/element_fetch/{element_id} [get]
func GetElementsWithDrawingsByElementId(db *sql.DB) gin.HandlerFunc {
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

		elementId := c.Param("element_id") // Get the element_id from the request parameters

		// Debug logging
		log.Printf("Received request for element ID: '%s'", elementId)
		log.Printf("All URL parameters: %v", c.Params)

		if elementId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element ID is required"})
			return
		}

		// Convert string ID to integer
		id, err := strconv.Atoi(elementId)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
			return
		}

		// Fetch the element using the provided id (primary key)
		elementQuery := `
        SELECT id, element_id, element_name, project_id, element_type_version, element_type_id, created_by, created_at, status, target_location
        FROM element WHERE id = $1
    `

		var element models.ElementResponse
		var targetLocation sql.NullInt64
		err = db.QueryRow(elementQuery, id).Scan(
			&element.ID,
			&element.ElementID,
			&element.ElementName,
			&element.ProjectID,
			&element.ElementTypeVersion,
			&element.ElementTypeID,
			&element.CreatedBy,
			&element.CreatedAt,
			&element.Status,
			&targetLocation,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching element data"})
			return
		}

		// Initialize the Drawings slice to ensure it's an empty array if no drawings are found
		element.Drawings = []models.DrawingResponse{}

		// Fetch tower and floor information if target_location is available
		if targetLocation.Valid && targetLocation.Int64 != 0 {
			tower, floor, err := getTowerAndFloor(db, targetLocation.Int64)
			if err != nil {
				log.Printf("Error fetching tower and floor: %v", err)
				// Don't fail the entire request, just set empty values
				element.Tower = ""
				element.Floor = ""
			} else {
				element.Tower = tower
				element.Floor = floor
			}
		} else {
			element.Tower = ""
			element.Floor = ""
		}

		// Fetch element type details
		elementTypeQuery := `
		SELECT element_type_name, thickness, length, height, volume, mass, area, width, created_by, created_at
        FROM element_type WHERE element_type_id = $1 ORDER BY created_at DESC
    `

		log.Printf("Fetching element type for ID: %d", element.ElementTypeID)

		err = db.QueryRow(elementTypeQuery, element.ElementTypeID).Scan(
			&element.ElementTypeName,
			&element.Thickness,
			&element.Length,
			&element.Height,
			&element.Volume, &element.Mass, &element.Area, &element.Width,
			&element.CreatedBy,
			&element.CreatedAt,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element type not found"})
				return
			}
			log.Printf("Error fetching element type: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching element type: %v", err)})
			return
		}

		// Fetch drawings associated with this element
		drawingsQuery := `
        SELECT drawing_id, project_id, current_version, drawing_type_id, comments, file
        FROM drawings WHERE element_type_id = $1
    `

		drawingRows, err := db.Query(drawingsQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching drawings"})
			return
		}
		defer drawingRows.Close()

		for drawingRows.Next() {
			var drawing models.DrawingResponse
			drawing.DrawingsRevision = []models.DrawingsRevisionResponse{} // Initialize empty array for drawing revisions
			err := drawingRows.Scan(
				&drawing.DrawingId,
				&drawing.ProjectId,
				&drawing.CurrentVersion,
				&drawing.DrawingTypeId,
				&drawing.Comments,
				&drawing.File,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning drawing data"})
				return
			}
			drawingTypeName, err := FetchDrawingTypeName(drawing.DrawingTypeId)
			if err != nil {
				log.Printf("Failed to fetch drawing type name for drawing: %v", err)
				drawing.DrawingTypeName = "" // Default value if the fetch fails
			} else {
				drawing.DrawingTypeName = drawingTypeName
			}

			// Fetch revisions for this drawing
			revisionsQuery := `
             SELECT version, comments, file, drawing_revision_id
            FROM drawings_revision
            WHERE parent_drawing_id = $1 ORDER BY created_at DESC`

			revisionRows, err := db.Query(revisionsQuery, drawing.DrawingId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching drawing revisions"})
				return
			}
			defer revisionRows.Close()

			for revisionRows.Next() {
				var revision models.DrawingsRevisionResponse
				err := revisionRows.Scan(
					&revision.Version,
					&revision.Comments,
					&revision.File,
					&revision.DrawingRevisionId,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning drawing revision data"})
					return
				}

				// Fetch DrawingTypeName for the revision
				revisionDrawingTypeName, err := FetchDrawingTypeName(revision.DrawingTypeId)
				if err != nil {
					log.Printf("Failed to fetch drawing type name for revision: %v", err)
					revision.DrawingTypeName = "" // Default value if the fetch fails
				} else {
					revision.DrawingTypeName = revisionDrawingTypeName
				}

				drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
			}

			// Append the drawing to the element's Drawings slice
			element.Drawings = append(element.Drawings, drawing)
		}

		// Return the element with drawings
		c.JSON(http.StatusOK, element)

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Element details of id %s with drawings", elementId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    element.ProjectID,
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

// GetAllElementsWithDrawings returns all elements with drawing info (no project filter).
// GetAllElementsWithDrawings godoc
// @Summary      Get all elements with drawings
// @Description  Get all elements with drawing info across projects
// @Tags         elements
// @Accept       json
// @Produce      json
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/element [get]
func GetAllElementsWithDrawings(db *sql.DB) gin.HandlerFunc {
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
	 SELECT e.id, e.element_id, e.element_name, e.project_id, e.element_type_version, e.element_type_id, 
		 et.element_type_name, et.thickness, et.length, et.height, et.volume, et.mass, et.area, et.width, 
		       COALESCE(d.drawing_id, 0) AS drawing_id, COALESCE(d.current_version, '') AS current_version, 
		       COALESCE(d.drawing_type_id, 0) AS drawing_type_id, COALESCE(d.comments, '') AS comments, 
		       COALESCE(d.file, '') AS file,
		       COALESCE(dr.version, '') AS version, COALESCE(dr.drawing_type_id, 0) AS revision_drawing_type_id, 
		       COALESCE(dr.comments, '') AS revision_comments, COALESCE(dr.file, '') AS revision_file, 
		       COALESCE(dr.drawing_revision_id, 0) AS drawing_revision_id
		FROM element e
		LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
		LEFT JOIN drawings d ON e.element_type_id = d.element_type_id
		LEFT JOIN drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
		ORDER BY e.id
	`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
			return
		}
		defer rows.Close()

		elements := map[int]*models.ElementResponse{}

		for rows.Next() {
			var element models.ElementResponse
			var drawing models.DrawingResponse
			var revision models.DrawingsRevisionResponse
			var elementTypeName sql.NullString // Change this to handle NULL values

			var thickness sql.NullFloat64 // Handle NULL values for thickness
			var length sql.NullFloat64
			var height sql.NullFloat64
			var volume sql.NullFloat64
			var mass sql.NullFloat64
			var area sql.NullFloat64
			var width sql.NullFloat64

			err := rows.Scan(
				&element.ID, &element.ElementID, &element.ElementName, &element.ProjectID,
				&element.ElementTypeVersion, &element.ElementTypeID, &elementTypeName,
				&thickness, &length, &height, &volume, &mass, &area, &width, // Use sql.NullFloat64
				&drawing.DrawingId, &drawing.CurrentVersion, &drawing.DrawingTypeId,
				&drawing.Comments, &drawing.File,
				&revision.Version, &revision.DrawingTypeId, &revision.Comments,
				&revision.File, &revision.DrawingRevisionId,
			)
			if err != nil {
				log.Println("Scan error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			// Convert sql.NullFloat64 to float64 (assign 0 if NULL)
			element.Thickness = thickness.Float64
			element.Length = length.Float64
			element.Height = height.Float64
			element.Volume = volume.Float64
			element.Mass = mass.Float64
			element.Area = area.Float64
			element.Width = width.Float64

			// Convert sql.NullString to standard string (assign empty string if NULL)
			element.ElementTypeName = elementTypeName.String

			// Initialize element if not already present
			if _, exists := elements[element.ID]; !exists {
				element.Drawings = []models.DrawingResponse{}
				elements[element.ID] = &element
			}

			// Add drawing to element
			if drawing.DrawingId != 0 {
				e := elements[element.ID]
				found := false

				for i := range e.Drawings {
					if e.Drawings[i].DrawingId == drawing.DrawingId {
						// Add revision to existing drawing
						e.Drawings[i].DrawingsRevision = append(e.Drawings[i].DrawingsRevision, revision)
						found = true
						break
					}
				}

				if !found {
					// Ensure revisions field is always initialized as an empty array
					drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}

					// Append the revision if available
					if revision.DrawingRevisionId != 0 {
						drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
					}

					e.Drawings = append(e.Drawings, drawing)
				}
			}
		}

		// Prepare response
		var result []models.ElementResponse
		for _, element := range elements {
			result = append(result, *element)
		}

		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  "Get All Elements With Drawings",
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

// GetElementsByElementTypeID returns elements with drawings for an element type.
// GetElementsByElementTypeID godoc
// @Summary      Get elements by element type ID
// @Description  Get elements with drawing info for a given element_type_id
// @Tags         elements
// @Accept       json
// @Produce      json
// @Param        element_type_id  path      int  true  "Element Type ID"
// @Success      200              {object}  object
// @Failure      400              {object}  models.ErrorResponse
// @Failure      401              {object}  models.ErrorResponse
// @Failure      500              {object}  models.ErrorResponse
// @Router       /api/element/{element_type_id} [get]
func GetElementsByElementTypeID(db *sql.DB) gin.HandlerFunc {
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

		elementTypeId, err := strconv.Atoi(c.Param("element_type_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Element Type ID"})
			return
		}

		var projectID int
		err = db.QueryRow(`SELECT project_id FROM element_type WHERE element_type_id = $1`, elementTypeId).Scan(&projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		query := `
	 SELECT e.id, e.element_id, e.element_name, e.project_id, e.element_type_version, e.element_type_id, 
		 et.element_type_name, et.thickness, et.length, et.height, et.volume, et.mass, et.area, et.width, 
		       COALESCE(d.drawing_id, 0) AS drawing_id, COALESCE(d.current_version, '') AS current_version, 
		       COALESCE(d.drawing_type_id, 0) AS drawing_type_id, COALESCE(d.comments, '') AS comments, 
		       COALESCE(d.file, '') AS file,
		       COALESCE(dr.version, '') AS version, COALESCE(dr.drawing_type_id, 0) AS revision_drawing_type_id, 
		       COALESCE(dr.comments, '') AS revision_comments, COALESCE(dr.file, '') AS revision_file, 
		       COALESCE(dr.drawing_revision_id, 0) AS drawing_revision_id
		FROM element e
		LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
		LEFT JOIN drawings d ON e.element_type_id = d.element_type_id
		LEFT JOIN drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
		WHERE e.element_type_id = $1
		ORDER BY e.id
	`

		rows, err := db.Query(query, elementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		elements := map[int]*models.ElementResponse{}

		for rows.Next() {
			var element models.ElementResponse
			var drawing models.DrawingResponse
			var revision models.DrawingsRevisionResponse

			err := rows.Scan(
				&element.ID, &element.ElementID, &element.ElementName, &element.ProjectID,
				&element.ElementTypeVersion, &element.ElementTypeID, &element.ElementTypeName,
				&element.Thickness, &element.Length, &element.Height, &element.Volume, &element.Mass, &element.Area, &element.Width,
				&drawing.DrawingId, &drawing.CurrentVersion, &drawing.DrawingTypeId,
				&drawing.Comments, &drawing.File,
				&revision.Version, &revision.DrawingTypeId, &revision.Comments,
				&revision.File, &revision.DrawingRevisionId,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			// Initialize element if not already present
			if _, exists := elements[element.ID]; !exists {
				element.Drawings = []models.DrawingResponse{}
				elements[element.ID] = &element
			}

			// Add drawing to element
			if drawing.DrawingId != 0 {
				e := elements[element.ID]
				found := false

				for i := range e.Drawings {
					if e.Drawings[i].DrawingId == drawing.DrawingId {
						// Add revision to existing drawing
						e.Drawings[i].DrawingsRevision = append(e.Drawings[i].DrawingsRevision, revision)
						found = true
						break
					}
				}

				if !found {
					drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}

					// Append the revision if available
					if revision.DrawingRevisionId != 0 {
						drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
					}

					e.Drawings = append(e.Drawings, drawing)
				}
			}
		}

		// Prepare response
		var result []models.ElementResponse
		for _, element := range elements {
			result = append(result, *element)
		}

		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Elements By ElementTypeID %d", projectID),
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

// GetAllElements returns all elements with drawing info (simplified list).
// GetAllElements godoc
// @Summary      Get all elements
// @Description  Get all elements list (with drawing info)
// @Tags         elements
// @Accept       json
// @Produce      json
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/elements [get]
func GetAllElements(db *sql.DB) gin.HandlerFunc {
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

		// Base query with joins to fetch element, element type, and related drawings and revisions
		query := `
		SELECT e.id, e.element_id, e.element_name, e.project_id, e.element_type_version, e.element_type_id, 
		       et.element_type_name, et.thickness, et.length, et.height, et.volume, et.mass, et.area, et.width, 
		       COALESCE(d.drawing_id, 0) AS drawing_id, COALESCE(d.current_version, '') AS current_version, 
		       COALESCE(d.drawing_type_id, 0) AS drawing_type_id, COALESCE(d.comments, '') AS comments, 
		       COALESCE(d.file, '') AS file,
		       COALESCE(dr.version, '') AS version, COALESCE(dr.drawing_type_id, 0) AS revision_drawing_type_id, 
		       COALESCE(dr.comments, '') AS revision_comments, COALESCE(dr.file, '') AS revision_file, 
		       COALESCE(dr.drawing_revision_id, 0) AS drawing_revision_id
		FROM element e
		LEFT JOIN element_type et ON e.element_type_id = et.element_type_id
		LEFT JOIN drawings d ON e.element_type_id = d.element_type_id
		LEFT JOIN drawings_revision dr ON d.drawing_id = dr.parent_drawing_id
		
	`

		var conditions []string
		var args []interface{}

		// Helper function to add query conditions
		addCondition := func(field, operator, value string) {
			conditions = append(conditions, fmt.Sprintf("%s %s $%d", field, operator, len(args)+1))
			args = append(args, value)
		}

		// Dynamically build filters for all fields
		if id := c.Query("id"); id != "" {
			addCondition("e.id", "=", id)
		}
		if elementID := c.Query("element_id"); elementID != "" {
			addCondition("e.element_id", "=", elementID)
		}
		if elementName := c.Query("element_name"); elementName != "" {
			addCondition("e.element_name", "ILIKE", "%"+elementName+"%")
		}
		if projectID := c.Query("project_id"); projectID != "" {
			addCondition("e.project_id", "=", projectID)
		}
		if elementTypeVersion := c.Query("element_type_version"); elementTypeVersion != "" {
			addCondition("e.element_type_version", "=", elementTypeVersion)
		}
		if elementTypeID := c.Query("element_type_id"); elementTypeID != "" {
			addCondition("e.element_type_id", "=", elementTypeID)
		}

		// Append conditions to the query
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		query += " ORDER BY e.id, d.drawing_id, dr.parent_drawing_id"

		// Execute the optimized query
		rows, err := db.Query(query, args...)
		if err != nil {
			log.Println("Database Query Error:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error while retrieving elements", "details": err.Error()})
			return
		}
		defer rows.Close()

		elementMap := make(map[int]*models.ElementResponse)
		for rows.Next() {
			var (
				drawingID        sql.NullInt64
				drawingTypeID    sql.NullInt64
				revisionParentID sql.NullInt64
			)

			element := models.ElementResponse{}
			drawing := models.DrawingResponse{}
			revision := models.DrawingsRevisionResponse{}
			var elementTypeName sql.NullString
			var thickness sql.NullFloat64 // Handle NULL values for thickness
			var length sql.NullFloat64
			var height sql.NullFloat64
			var volume sql.NullFloat64
			var mass sql.NullFloat64
			var area sql.NullFloat64
			var width sql.NullFloat64

			err := rows.Scan(
				&element.ID, &element.ElementID, &element.ElementName, &element.ProjectID,
				&element.ElementTypeVersion, &element.ElementTypeID, &elementTypeName,
				&thickness, &length, &height, &volume, &mass, &area, &width, // Use sql.NullFloat64
				&drawing.DrawingId, &drawing.CurrentVersion, &drawing.DrawingTypeId,
				&drawing.Comments, &drawing.File,
				&revision.Version, &revision.DrawingTypeId, &revision.Comments,
				&revision.File, &revision.DrawingRevisionId,
			)
			if err != nil {
				log.Println("Scan error:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			// Convert sql.NullFloat64 to float64 (assign 0 if NULL)
			element.Thickness = thickness.Float64
			element.Length = length.Float64
			element.Height = height.Float64
			element.Volume = volume.Float64
			element.Mass = mass.Float64
			element.Area = area.Float64
			element.Width = width.Float64

			// Convert sql.NullString to standard string (assign empty string if NULL)
			element.ElementTypeName = elementTypeName.String

			// Retrieve existing element from map or create a new one
			existingElement, exists := elementMap[element.ID]
			if !exists {
				existingElement = &element
				existingElement.Drawings = []models.DrawingResponse{}
				elementMap[element.ID] = existingElement
			}

			// Append drawings and revisions if they exist
			if drawingID.Valid {
				drawing.DrawingId = int(drawingID.Int64)
				drawing.DrawingTypeId = int(drawingTypeID.Int64)
				drawing.DrawingsRevision = []models.DrawingsRevisionResponse{}

				// Check if the drawing already exists to avoid duplicate entries
				var existingDrawing *models.DrawingResponse
				for i := range existingElement.Drawings {
					if existingElement.Drawings[i].DrawingId == drawing.DrawingId {
						existingDrawing = &existingElement.Drawings[i]
						break
					}
				}

				if existingDrawing == nil {
					existingElement.Drawings = append(existingElement.Drawings, drawing)
					existingDrawing = &existingElement.Drawings[len(existingElement.Drawings)-1]
				}

				if revisionParentID.Valid {
					existingDrawing.DrawingsRevision = append(existingDrawing.DrawingsRevision, revision)
				}
			}
		}

		if len(elementMap) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "No elements found"})
			return
		}

		// Convert map to slice for response
		elements := make([]models.ElementResponse, 0, len(elementMap))
		for _, element := range elementMap {
			elements = append(elements, *element)
		}

		c.JSON(http.StatusOK, gin.H{"data": elements})

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  "Get Element Life Cycle",
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

// GetElementLifecycleHandler godoc
// @Summary      Get element lifecycle
// @Tags         elements
// @Param        element_id  path  int  true  "Element ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/element_lifecycle/{element_id} [get]
func GetElementLifecycleHandler(db *sql.DB) gin.HandlerFunc {
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

		elementIDStr := c.Param("element_id")
		elementID, err := strconv.Atoi(elementIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element_id"})
			return
		}

		type Event struct {
			Label     string    `json:"label"`
			Timestamp time.Time `json:"timestamp"`
			Duration  string    `json:"duration,omitempty"`
		}

		var lifecycle []Event

		// Step 1: Element created
		var createdAt time.Time
		var elementName string
		var projectID int
		err = db.QueryRow(`SELECT created_at, element_name, project_id FROM element WHERE id = $1`, elementID).Scan(&createdAt, &elementName, &projectID)
		if err != nil {
			log.Printf("Error fetching element: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Element not found", "details": err.Error()})
			return
		}
		lifecycle = append(lifecycle, Event{"Element Created", createdAt, ""})

		// Step 2: Task Assigned?
		var instage bool
		err = db.QueryRow(`SELECT instage FROM element WHERE id = $1`, elementID).Scan(&instage)
		if err != nil {
			log.Printf("Error fetching instage: %v", err)
			// Continue execution even if this fails
		} else if instage {
			var taskTime time.Time
			err = db.QueryRow(`SELECT MIN(start_date) FROM activity WHERE element_id = $1`, elementID).Scan(&taskTime)
			if err != nil {
				log.Printf("Error fetching task time: %v", err)
			} else if !taskTime.IsZero() {
				lifecycle = append(lifecycle, Event{"Task Assigned", taskTime, ""})
			}
		}

		// Step 3: Completed Stages
		stageQuery := `
			SELECT ps.name, cp.started_at
			FROM complete_production cp
			JOIN project_stages ps ON cp.stage_id = ps.id
			WHERE cp.element_id = $1
			ORDER BY cp.started_at`
		rows, err := db.Query(stageQuery, elementID)
		if err != nil {
			log.Printf("Error fetching completed stages: %v", err)
		} else {
			for rows.Next() {
				var stageName string
				var startedAt time.Time
				err := rows.Scan(&stageName, &startedAt)
				if err != nil {
					log.Printf("Error scanning stage row: %v", err)
					continue
				}
				lifecycle = append(lifecycle, Event{"Stage: " + stageName, startedAt, ""})
			}
			rows.Close()
		}

		// Step 4: Rectification
		rectQuery := `
			SELECT status, comments, created_at
			FROM element_rectification
			WHERE element_id = $1`
		rows, err = db.Query(rectQuery, elementID)
		if err != nil {
			log.Printf("Error fetching rectification: %v", err)
		} else {
			for rows.Next() {
				var status, comments string
				var at time.Time
				err := rows.Scan(&status, &comments, &at)
				if err != nil {
					log.Printf("Error scanning rectification row: %v", err)
					continue
				}
				label := fmt.Sprintf("Rectification %s - %s", status, comments)
				lifecycle = append(lifecycle, Event{label, at, ""})
			}
			rows.Close()
		}

		// Step 5: Precast Stock - stockyard, dispatch, erected, received
		var ps models.PrecastStock2
		err = db.QueryRow(`
			SELECT stockyard, dispatch_status, erected, dispatch_start, dispatch_end, created_at, updated_at, recieve_in_erection
			FROM precast_stock WHERE element_id = $1`, elementID).
			Scan(&ps.Stockyard, &ps.DispatchStatus, &ps.Erected, &ps.DispatchStart, &ps.DispatchEnd, &ps.CreatedAt, &ps.UpdatedAt, &ps.ReceiveInErection)

		if err != nil {
			log.Printf("Error fetching precast stock for element_id %d: %v", elementID, err)
			// Check if element exists in precast_stock table
			var count int
			err = db.QueryRow(`SELECT COUNT(*) FROM precast_stock WHERE element_id = $1`, elementID).Scan(&count)
			if err != nil {
				log.Printf("Error checking precast_stock count: %v", err)
			} else {
				log.Printf("Found %d records in precast_stock for element_id %d", count, elementID)
			}
		} else {
			log.Printf("Precast stock data for element_id %d: stockyard=%v, dispatch_status=%v, erected=%v, receive_in_erection=%v",
				elementID, ps.Stockyard, ps.DispatchStatus, ps.Erected, ps.ReceiveInErection)

			if ps.Stockyard {
				lifecycle = append(lifecycle, Event{"Received in Stockyard", ps.CreatedAt, ""})
			}
			if ps.DispatchStatus {
				// Dispatch duration
				duration := ps.DispatchEnd.Sub(ps.DispatchStart).String()
				lifecycle = append(lifecycle, Event{"Dispatched", ps.DispatchStart, duration})
			}
			if ps.Erected {
				lifecycle = append(lifecycle, Event{"Erected", ps.UpdatedAt, ""})
			}
			if ps.ReceiveInErection {
				lifecycle = append(lifecycle, Event{"Received at Site", ps.UpdatedAt, ""})
			}
		}

		// Sort lifecycle by timestamp
		sort.Slice(lifecycle, func(i, j int) bool {
			return lifecycle[i].Timestamp.Before(lifecycle[j].Timestamp)
		})

		c.JSON(http.StatusOK, gin.H{
			"element_id":   elementID,
			"element_name": elementName,
			"project_id":   projectID,
			"lifecycle":    lifecycle,
		})

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Element Life Cycle %d", projectID),
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

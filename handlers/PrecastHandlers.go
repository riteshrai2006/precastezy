package handlers

import (
	"backend/models"
	"backend/storage"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func BuildFullNamingConcention(db *sql.DB, parentID *int64) (string, error) {
	// Base case: if there's no parent, return an empty string
	if parentID == nil {
		return "", nil
	}

	// Query to fetch the prefix and parent_id of the current entry
	var prefix string
	var parent *int64
	err := db.QueryRow(`SELECT prefix, parent_id FROM precast WHERE id = $1`, parentID).Scan(&prefix, &parent)
	if err != nil {
		return "", err
	}

	// Recursively build the naming convention for the parent
	parentPath, err := BuildFullNamingConcention(db, parent)
	if err != nil {
		return "", err
	}

	// Concatenate the current prefix to the parent path with an underscore
	if parentPath == "" {
		return prefix, nil
	}
	return parentPath + "_" + prefix, nil
}

// BuildFullPath recursively builds the path based on parent_id
func BuildFullPath(db *sql.DB, parentID *int64) (string, error) {
	// Base case: if there's no parent, return an empty string
	if parentID == nil {
		return "", nil
	}

	// Query to fetch the parent details (assuming table 'precast' has name and parent_id fields)
	var name string
	var parent *int64
	err := db.QueryRow(`SELECT name, parent_id FROM precast WHERE id = $1`, parentID).Scan(&name, &parent)
	if err != nil {
		return "", err
	}

	// Recursively build the path for the parent
	parentPath, err := BuildFullPath(db, parent)
	if err != nil {
		return "", err
	}

	// Concatenate the current name to the parent path
	if parentPath == "" {
		return name, nil
	}
	return parentPath + "." + name, nil
}

var pathRegex = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

// InsertPrecast creates a new precast hierarchy entry
// @Summary Create precast
// @Description Create a new precast hierarchy entry
// @Tags Precast
// @Accept json
// @Produce json
// @Param request body models.Precast true "Precast creation request"
// @Success 201 {object} models.PrecastResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/create_precast [post]
func InsertPrecast(c *gin.Context) {
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

	// Expected JSON: { "project_id": 123, "parent_id": 5, "records": [ {...}, {...} ] }
	var input struct {
		ProjectID int64            `json:"project_id"`
		ParentID  *int64           `json:"parent_id"`
		Records   []models.Precast `json:"records"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	// Track project ID for notifications (all records have the same projectID)
	projectID := int(input.ProjectID)

	for _, rec := range input.Records {
		rec.ProjectID = int(input.ProjectID)
		rec.ParentID = input.ParentID

		// Build naming convention
		namingConvention, err := BuildFullNamingConcention(db, rec.ParentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build naming convention", "details": err.Error()})
			return
		}
		if namingConvention != "" {
			namingConvention += "_" + rec.Prefix
		} else {
			namingConvention = rec.Prefix
		}
		rec.NamingConvention = namingConvention

		// Build path
		path, err := BuildFullPath(db, rec.ParentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build path", "details": err.Error()})
			return
		}

		if path != "" {
			path += "." + rec.Name
		} else {
			path = rec.Name
		}
		fmt.Print("Raw path before sanitization:", path)
		// Sanitize path for ltree
		path = sanitizePathForLtree(path)

		// Validate ltree path
		if !pathRegex.MatchString(path) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid path format",
				"details": fmt.Sprintf("Path '%s' doesn't match ltree pattern", path),
			})
			return
		}

		// Insert into DB
		_, err = db.ExecContext(context.Background(),
			`INSERT INTO precast (project_id, name, description, parent_id, prefix, path, naming_convention, others)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			rec.ProjectID, rec.Name, rec.Description, rec.ParentID, rec.Prefix, path, rec.NamingConvention, rec.Others,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert record", "details": err.Error()})
			return
		}

		// Activity log
		log := models.ActivityLog{
			EventContext: "Precast",
			EventName:    "Create",
			Description:  "Insert Precast",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    rec.ProjectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "All precast records inserted successfully"})

	// Get userID from session
	var userID int
	err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
	if err != nil {
		log.Printf("Failed to fetch user_id for notification: %v", err)
	} else {
		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Send notification to the user who created the precast structure
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Precast structure created for project: %s", projectName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/structure", projectID),
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
			fmt.Sprintf("Precast structure created for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/structure", projectID))
	}
}

func sanitizePathForLtree(path string) string {
	// Convert to lowercase
	path = strings.ToLower(path)
	// Replace spaces and hyphens with underscores
	path = strings.ReplaceAll(path, " ", "_")
	path = strings.ReplaceAll(path, "-", "_")
	return path
}

// UpdatePrecast updates an existing precast hierarchy entry
// @Summary Update precast
// @Description Update an existing precast hierarchy entry
// @Tags Precast
// @Accept json
// @Produce json
// @Param id path int true "Precast ID"
// @Param request body models.Precast true "Precast update request"
// @Success 200 {object} models.PrecastResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/update_precast/{id} [put]
func UpdatePrecast(c *gin.Context) {
	var precast models.Precast
	var updates []string
	var fields []interface{}
	placeholderIndex := 1

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

	// Extract the ID from the URL and convert it to an integer
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	// Get projectID from the precast record before updating
	var projectID int
	err = db.QueryRow("SELECT project_id FROM precast WHERE id = $1", id).Scan(&projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Precast record not found"})
		return
	}

	// Bind JSON input to the Precast model
	if err := c.ShouldBindJSON(&precast); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	// Handle ParentID if provided
	if precast.ParentID != nil {
		// Update ParentID field
		updates = append(updates, fmt.Sprintf("parent_id = $%d", placeholderIndex))
		fields = append(fields, *precast.ParentID)
		placeholderIndex++

		// Call BuildFullPath to automatically build the path based on ParentID
		path, err := BuildFullPath(storage.GetDB(), precast.ParentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to build path",
				"details": err.Error(),
			})
			return
		}

		// Sanitize the path for ltree
		path = sanitizePathForLtree(path)

		// Debugging: Print the sanitized path to check if it's valid for ltree
		fmt.Println("Sanitized path:", path)

		// Append the sanitized path to the update query
		updates = append(updates, fmt.Sprintf("path = $%d", placeholderIndex))
		fields = append(fields, path)
		placeholderIndex++

		// Call BuildFullNamingConvention to automatically build the naming convention
		namingConvention, err := BuildFullNamingConcention(storage.GetDB(), precast.ParentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to build naming convention",
				"details": err.Error(),
			})
			return
		}

		// Append the naming convention to the update query
		updates = append(updates, fmt.Sprintf("naming_convention = $%d", placeholderIndex))
		fields = append(fields, namingConvention)
		placeholderIndex++
	}

	// Handle Others (boolean, always updatable)
	updates = append(updates, fmt.Sprintf("others = $%d", placeholderIndex))
	fields = append(fields, precast.Others)
	placeholderIndex++

	// Ensure there are fields to update
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
		return
	}

	// Build the SQL query dynamically
	sqlStatement := fmt.Sprintf("UPDATE precast SET %s WHERE id = $%d", strings.Join(updates, ", "), placeholderIndex)
	fields = append(fields, id)

	// Execute the update query
	_, err = storage.GetDB().ExecContext(context.Background(), sqlStatement, fields...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   fmt.Sprintf("Failed to update precast: %v", err),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Data updated successfully"})

	// Get userID from session
	var userID int
	err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
	if err != nil {
		log.Printf("Failed to fetch user_id for notification: %v", err)
	} else {
		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Send notification to the user who updated the precast structure
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Precast structure updated for project: %s", projectName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/structure", projectID),
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
			fmt.Sprintf("Precast structure updated for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/structure", projectID))
	}

	activityLog := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "Update",
		Description:  "Update Precast",
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

// DeletePrecast deletes a precast hierarchy row by ID
// @Summary Delete precast by ID
// @Description Delete a precast hierarchy entry by ID. Fails if the node has children.
// @Tags Precast
// @Accept json
// @Produce json
// @Param id path int true "Precast ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/delete_precast/{id} [delete]
func DeletePrecast(c *gin.Context) {
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

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var projectID int
	err = db.QueryRow("SELECT project_id FROM precast WHERE id = $1", id).Scan(&projectID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Precast record not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check precast record", "details": err.Error()})
		return
	}

	var childCount int
	err = db.QueryRow("SELECT COUNT(*) FROM precast WHERE parent_id = $1", id).Scan(&childCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check child nodes", "details": err.Error()})
		return
	}
	if childCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Cannot delete: this node has child nodes. Delete or move children first.",
		})
		return
	}

	result, err := db.ExecContext(context.Background(), "DELETE FROM precast WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete precast", "details": err.Error()})
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Precast record not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Precast deleted successfully"})

	activityLog := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "Delete",
		Description:  "Delete Precast",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    projectID,
	}
	if logErr := SaveActivityLog(db, activityLog); logErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to log activity",
			"details": logErr.Error(),
		})
		return
	}
}

// GetHierarchy retrieves the complete precast hierarchy
// @Summary Get precast hierarchy
// @Description Retrieve the complete precast hierarchy
// @Tags Precast
// @Accept json
// @Produce json
// @Success 200 {array} models.PrecastResponce
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/precast [get]
func GetHierarchy(c *gin.Context) {
	db := storage.GetDB() // Assume this returns *sql.DB

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

	rows, err := db.QueryContext(context.Background(),
		`WITH RECURSIVE hierarchy AS (
            SELECT 
                id, project_id, name, description, parent_id, prefix, COALESCE(naming_convention, '') as naming_convention, COALESCE(others, false) as others
            FROM 
                precast
            WHERE 
                parent_id IS NULL
            UNION ALL
            SELECT 
                p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix, COALESCE(p.naming_convention, '') as naming_convention, COALESCE(p.others, false) as others
            FROM 
                precast p
            JOIN 
                hierarchy h ON p.parent_id = h.id
        )
        SELECT id, project_id, name, description, parent_id, prefix, naming_convention, others
        FROM hierarchy
        ORDER BY id;`)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch hierarchy",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	var flatHierarchy []models.PrecastResponce

	for rows.Next() {
		var precast models.PrecastResponce
		if err := rows.Scan(
			&precast.ID,
			&precast.ProjectID,
			&precast.Name,
			&precast.Description,
			&precast.ParentID,
			&precast.Prefix,
			&precast.NamingConvention,
			&precast.Others,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan row",
				"details": err.Error(),
			})
			return
		}
		flatHierarchy = append(flatHierarchy, precast)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error during row iteration",
			"details": err.Error(),
		})
		return
	}

	hierarchyMap := make(map[int]*models.PrecastResponce)
	var roots []*models.PrecastResponce

	for _, item := range flatHierarchy {
		hierarchyMap[item.ID] = &item // Store reference to the item
		if item.ParentID == nil {     // Root items
			roots = append(roots, &item)
		} else { // Child items
			parent := hierarchyMap[*item.ParentID]
			if parent != nil {
				// Ensure parent.Children is initialized
				if parent.Children == nil {
					parent.Children = make([]*models.PrecastResponce, 0)
				}
				parent.Children = append(parent.Children, &item) // Add child to parent's children
			}
		}
	}

	c.JSON(http.StatusOK, roots) // Return the roots as JSON

	log := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "Get",
		Description:  "Get hierarchy",
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

// GetHierarchyByID retrieves precast hierarchy by ID
// @Summary Get precast hierarchy by ID
// @Description Retrieve precast hierarchy by specific ID
// @Tags Precast
// @Accept json
// @Produce json
// @Param id path int true "Precast ID"
// @Success 200 {array} models.PrecastResponce
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_precast/{id} [get]
func GetHierarchyByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
		return
	}

	db := storage.GetDB() // Assume this returns *sql.DB

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

	// Recursive CTE to fetch the hierarchy from the specified ID
	query := `
        WITH RECURSIVE hierarchy AS (
            SELECT 
                id, project_id, name, description, parent_id, prefix, COALESCE(naming_convention, '') as naming_convention, COALESCE(others, false) as others
            FROM 
                precast
            WHERE 
                id = $1
            UNION ALL
            SELECT 
                p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix, COALESCE(p.naming_convention, '') as naming_convention, COALESCE(p.others, false) as others
            FROM 
                precast p
            JOIN 
                hierarchy h ON p.parent_id = h.id
        )
        SELECT id, project_id, name, description, parent_id, prefix, naming_convention, others
        FROM hierarchy
        ORDER BY id;`

	// Execute the query with the provided ID
	rows, err := db.QueryContext(context.Background(), query, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to fetch hierarchy",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	// Collect rows into a flat hierarchy slice
	var flatHierarchy []models.PrecastResponce
	for rows.Next() {
		var precast models.PrecastResponce
		precast.Children = []*models.PrecastResponce{} // Initialize Children as an empty slice
		if err := rows.Scan(
			&precast.ID,
			&precast.ProjectID,
			&precast.Name,
			&precast.Description,
			&precast.ParentID,
			&precast.Prefix,
			&precast.NamingConvention,
			&precast.Others,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to scan row",
				"details": err.Error(),
			})
			return
		}
		flatHierarchy = append(flatHierarchy, precast)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "error during row iteration",
			"details": err.Error(),
		})
		return
	}

	// Build the hierarchy structure
	hierarchyMap := make(map[int]*models.PrecastResponce)
	var roots []*models.PrecastResponce

	for i := range flatHierarchy {
		item := &flatHierarchy[i]
		hierarchyMap[item.ID] = item
		if item.ParentID == nil {
			roots = append(roots, item)
		} else if parent := hierarchyMap[*item.ParentID]; parent != nil {
			if parent.Children == nil {
				parent.Children = []*models.PrecastResponce{}
			}
			parent.Children = append(parent.Children, item)
		}
	}

	c.JSON(http.StatusOK, roots)

	log := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "GET",
		Description:  "GET Precast hierarchy",
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

// GetHierarchyByProjectID retrieves precast hierarchy by project ID
// @Summary Get precast hierarchy by project ID
// @Description Retrieve precast hierarchy for a specific project
// @Tags Precast
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.PrecastResponce
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_precast_project/{project_id} [get]
func GetHierarchyByProjectID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
		return
	}

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

	// Recursive CTE to fetch the hierarchy from the specified ID
	query := `
        WITH RECURSIVE hierarchy AS (
            SELECT 
                id, project_id, name, description, parent_id, prefix, COALESCE(naming_convention, '') as naming_convention, COALESCE(others, false) as others
            FROM 
                precast
            WHERE 
                parent_id IS NULL
                AND project_id = $1
            UNION ALL
            SELECT 
                p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix, COALESCE(p.naming_convention, '') as naming_convention, COALESCE(p.others, false) as others
            FROM 
                precast p
            JOIN 
                hierarchy h ON p.parent_id = h.id
                AND p.project_id = $2
        )
        SELECT 
            id, project_id, name, description, parent_id, prefix, naming_convention, others
        FROM 
            hierarchy
        ORDER BY 
            id;`

	// Execute the query with the provided project ID
	rows, err := db.QueryContext(context.Background(), query, id, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch hierarchy",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	// Collect rows into a flat hierarchy slice
	var flatHierarchy []models.PrecastResponce
	for rows.Next() {
		var precast models.PrecastResponce
		precast.Children = []*models.PrecastResponce{} // Initialize Children as an empty slice
		if err := rows.Scan(
			&precast.ID,
			&precast.ProjectID,
			&precast.Name,
			&precast.Description,
			&precast.ParentID,
			&precast.Prefix,
			&precast.NamingConvention,
			&precast.Others,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan row",
				"details": err.Error(),
			})
			return
		}
		flatHierarchy = append(flatHierarchy, precast)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error during row iteration",
			"details": err.Error(),
		})
		return
	}

	if len(flatHierarchy) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No hierarchy found for the given project ID"})
		return
	}

	// Build the hierarchy structure
	hierarchyMap := make(map[int]*models.PrecastResponce)
	var roots []*models.PrecastResponce

	for i := range flatHierarchy {
		item := &flatHierarchy[i]
		hierarchyMap[item.ID] = item
		if item.ParentID == nil {
			roots = append(roots, item)
		} else if parent, exists := hierarchyMap[*item.ParentID]; exists {
			if parent.Children == nil {
				parent.Children = []*models.PrecastResponce{}
			}
			parent.Children = append(parent.Children, item)
		}
	}

	c.JSON(http.StatusOK, roots)

	log := models.ActivityLog{
		EventContext: "Precast Hierarchy",
		EventName:    "GET",
		Description:  "Get Heirarchy By project id",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    id, // No specific project ID for this operation
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

// GetPrecastNamesWithNullParent retrieves precast names with null parent
// @Summary Get precast names with null parent
// @Description Retrieve precast names that have no parent (root level)
// @Tags Precast
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} string
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/precast/parents_names/{project_id} [get]
func GetPrecastNamesWithNullParent(c *gin.Context) {
	projectID, err := strconv.Atoi(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
		return
	}

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

	rows, err := db.QueryContext(context.Background(),
		`SELECT name 
		FROM precast 
		WHERE parent_id IS NULL AND project_id = $1
		ORDER BY id`, projectID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch precast names"})
		return
	}
	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan row",
				"details": err.Error(),
			})
			return
		}
		names = append(names, name)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error during row iteration",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, names)

	log := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "GET",
		Description:  "GET Precast With Null Parent",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    projectID, // No specific project ID for this operation
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

func GetPrecastNamesByParentID(c *gin.Context) {
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
	projectID := c.Param("project_id")
	parentID := c.Param("parent_id")

	// Validate project_id
	projectIDInt, err := strconv.Atoi(projectID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
		return
	}

	// Validate parent_id
	parentIDInt, err := strconv.Atoi(parentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
		return
	}

	// Query to get only precast names where parent_id matches
	query := `
		SELECT name 
		FROM precast
		WHERE project_id = $1 AND parent_id = $2
		ORDER BY name`

	rows, err := storage.GetDB().Query(query, projectIDInt, parentIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch precast names"})
		return
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan precast data",
				"details": err.Error(),
			})
			return
		}
		names = append(names, name)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error iterating precast rows",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, names)

	ProjectID, err := strconv.Atoi("projectID")
	if err != nil {
		return
	}

	log := models.ActivityLog{
		EventContext: "Precast",
		EventName:    "GET",
		Description:  "Get Precast Names By ParentID",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    ProjectID, // No specific project ID for this operation
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

type Tower struct {
	ID          int    `json:"id"`
	ProjectID   int    `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Others      bool   `json:"others"`
	ChildCount  int    `json:"child_count"`
}

// GetTowersList godoc
// @Summary      Get dashboard towers list for project
// @Tags         dashboard
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/dashboard/towers/{project_id} [get]
func GetTowersList(db *sql.DB) gin.HandlerFunc {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		query := `
			SELECT 
				p1.id,
				p1.project_id,
				p1.name,
				p1.description,
				COALESCE(p1.others, false) AS others,
				COUNT(p2.id) AS child_count
			FROM precast p1
			LEFT JOIN precast p2 ON p2.parent_id = p1.id AND p2.project_id = p1.project_id
			WHERE p1.project_id = $1 AND p1.parent_id IS NULL
			GROUP BY p1.id, p1.project_id, p1.name, p1.description, p1.others
			ORDER BY p1.name ASC;
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch towers", "details": err.Error()})
			return
		}
		defer rows.Close()

		var towers []Tower
		for rows.Next() {
			var tower Tower
			err := rows.Scan(
				&tower.ID,
				&tower.ProjectID,
				&tower.Name,
				&tower.Description,
				&tower.Others,
				&tower.ChildCount,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan tower data", "details": err.Error()})
				return
			}
			towers = append(towers, tower)
		}

		c.JSON(http.StatusOK, gin.H{
			"towers":       towers,
			"total_towers": len(towers),
		})

		log := models.ActivityLog{
			EventContext: "Tower",
			EventName:    "GET",
			Description:  "GET Tower List",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID, // No specific project ID for this operation
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

// GetFloorNamesWithIDs retrieves floor names with IDs for a specific project and parent
// @Summary Get floor names with IDs
// @Description Retrieve floor names with IDs for a specific project and parent
// @Tags Precast
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param parent_id path int true "Parent ID"
// @Success 200 {array} models.FloorInfoResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/precast/floors/{project_id}/{parent_id} [get]
func GetFloorNamesWithIDs(c *gin.Context) {
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

	projectID, err := strconv.Atoi(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
		return
	}

	parentID, err := strconv.Atoi(c.Param("parent_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
		return
	}

	// Query to get floors for a specific project and parent_id with tower name
	// Order by numeric part of name (e.g. Floor 1, 2, ... 10, 11) not alphabetically
	query := `
		SELECT 
			f.id, 
			f.name,
			f.parent_id,
			f.description,
			COALESCE(t.name, '') as tower_name,
			COALESCE(f.others, false) as others
		FROM precast f
		LEFT JOIN precast t ON t.id = f.parent_id
		WHERE f.project_id = $1 
		AND f.parent_id = $2
		ORDER BY (COALESCE((regexp_match(f.name, '[0-9]+'))[1], '0')::int) ASC, f.name ASC`

	rows, err := db.QueryContext(context.Background(), query, projectID, parentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch floors", "details": err.Error()})
		return
	}
	defer rows.Close()

	var floors []models.FloorInfoResponse
	for rows.Next() {
		var floor models.FloorInfoResponse
		var parentID sql.NullInt64

		if err := rows.Scan(
			&floor.ID,
			&floor.Name,
			&parentID,
			&floor.Description,
			&floor.TowerName,
			&floor.Others,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan floor data", "details": err.Error()})
			return
		}

		// Set ParentID to 0 if it's null in database
		if parentID.Valid {
			floor.ParentID = int(parentID.Int64)
		} else {
			floor.ParentID = 0
		}

		floors = append(floors, floor)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during row iteration", "details": err.Error()})
		return
	}

	if len(floors) == 0 {
		c.JSON(http.StatusOK, []models.FloorInfoResponse{})
		return
	}

	c.JSON(http.StatusOK, floors)

	log := models.ActivityLog{
		EventContext: "Floor",
		EventName:    "GET",
		Description:  "GET Floor Name With ID",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    projectID, // No specific project ID for this operation
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

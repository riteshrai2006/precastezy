package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// ==================== CATEGORY CRUD OPERATIONS ====================

// CreateCategory creates a new category
// @Summary Create category
// @Description Create a new category
// @Tags Categories
// @Accept json
// @Produce json
// @Param request body models.Category true "Category creation request"
// @Success 201 {object} models.CategoryResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/categories [post]
func CreateCategory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var category models.Category
		if err := c.ShouldBindJSON(&category); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", category.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if category with same name already exists in the project
		var existingID int
		err = db.QueryRow("SELECT id FROM categories WHERE name = $1 AND project_id = $2", category.Name, category.ProjectID).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Category with this name already exists in this project"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Insert new category
		var id int
		err = db.QueryRow(
			"INSERT INTO categories (name, project_id, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id",
			category.Name, category.ProjectID, time.Now(), time.Now(),
		).Scan(&id)

		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Category with this name already exists in this project"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
			return
		}

		category.ID = uint(id)

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", category.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", category.ProjectID)
		}

		c.JSON(http.StatusCreated, models.CategoryResponse{
			Success: true,
			Message: "Category created successfully",
			Data:    &category,
		})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the category
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New category created: %s in project: %s", category.Name, projectName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/departments",
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
		sendProjectNotifications(db, int(category.ProjectID),
			fmt.Sprintf("New category created: %s in project: %s", category.Name, projectName),
			"https://precastezy.blueinvent.com/departments")
	}
}

// GetCategories retrieves all categories
// @Summary Get all categories
// @Description Retrieve all categories
// @Tags Categories
// @Produce json
// @Success 200 {object} models.CategoryListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/categories [get]
func GetCategories(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// Validate session and get user + role info
		var userID, roleID int
		var roleName string

		err := db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session"})
			return
		}

		// ✅ Build access control condition dynamically
		var accessCondition string
		var args []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			accessCondition = "1=1" // No restriction
		case "admin":
			accessCondition = `
				p.client_id IN (
					SELECT ec.id 
					FROM end_client ec
					JOIN client c ON ec.client_id = c.client_id
					WHERE c.user_id = $` + fmt.Sprint(argIndex) + `
				)
			`
			args = append(args, userID)
			argIndex++
		default:
			// Normal user: only categories linked to projects they created
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "No permission to view categories",
			})
			return
		}

		// ✅ Query categories with project join and access control
		query := fmt.Sprintf(`
			SELECT 
				c.id, 
				c.name, 
				c.project_id, 
				p.name AS project_name, 
				c.created_at, 
				c.updated_at
			FROM categories c
			LEFT JOIN project p ON c.project_id = p.project_id
			WHERE %s
			ORDER BY c.name
		`, accessCondition)

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch categories", "details": err.Error()})
			return
		}
		defer rows.Close()

		var categories []models.Category
		for rows.Next() {
			var cat models.Category
			if err := rows.Scan(&cat.ID, &cat.Name, &cat.ProjectID, &cat.ProjectName, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan category data", "details": err.Error()})
				return
			}
			categories = append(categories, cat)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating categories", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.CategoryListResponse{
			Success: true,
			Message: "Categories retrieved successfully",
			Data:    categories,
		})
	}
}

// GetCategoriesByProject retrieves all categories for a specific project
// @Summary Get categories by project
// @Description Retrieve all categories for a specific project
// @Tags Categories
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {object} models.CategoryListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/projects/{project_id}/categories [get]
func GetCategoriesByProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		rows, err := db.Query("SELECT id, name, project_id FROM categories WHERE project_id = $1 ORDER BY name", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch categories"})
			return
		}
		defer rows.Close()

		var categories []models.Category
		for rows.Next() {
			var cat models.Category
			if err := rows.Scan(&cat.ID, &cat.Name, &cat.ProjectID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan category data"})
				return
			}
			categories = append(categories, cat)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating categories"})
			return
		}

		c.JSON(http.StatusOK, models.CategoryListResponse{
			Success: true,
			Message: "Categories retrieved successfully",
			Data:    categories,
		})
	}
}

// GetCategory retrieves a specific category by ID
// @Summary Get category by ID
// @Description Retrieve a specific category by its ID
// @Tags Categories
// @Produce json
// @Param id path int true "Category ID"
// @Success 200 {object} models.CategoryResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/categories/{id} [get]
func GetCategory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
			return
		}

		var category models.Category
		err = db.QueryRow("SELECT id, name, project_id FROM categories WHERE id = $1", id).Scan(&category.ID, &category.Name, &category.ProjectID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch category"})
			return
		}

		c.JSON(http.StatusOK, models.CategoryResponse{
			Success: true,
			Message: "Category retrieved successfully",
			Data:    &category,
		})
	}
}

// UpdateCategory updates an existing category
// @Summary Update category
// @Description Update an existing category
// @Tags Categories
// @Accept json
// @Produce json
// @Param id path int true "Category ID"
// @Param request body models.Category true "Category update request"
// @Success 200 {object} models.CategoryResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/categories/{id} [put]
func UpdateCategory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
			return
		}

		var category models.Category
		if err := c.ShouldBindJSON(&category); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", category.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if another category with same name exists in the same project
		var conflictingID int
		err = db.QueryRow("SELECT id FROM categories WHERE name = $1 AND project_id = $2 AND id != $3", category.Name, category.ProjectID, id).Scan(&conflictingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Category with this name already exists in this project"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Update category
		result, err := db.Exec(
			"UPDATE categories SET name = $1, project_id = $2, updated_at = $3 WHERE id = $4",
			category.Name, category.ProjectID, time.Now(), id,
		)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Category with this name already exists in this project"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check update result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
			return
		}

		category.ID = uint(id)

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", category.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", category.ProjectID)
		}

		c.JSON(http.StatusOK, models.CategoryResponse{
			Success: true,
			Message: "Category updated successfully",
			Data:    &category,
		})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the category
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Category updated: %s in project: %s", category.Name, projectName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/departments",
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
		sendProjectNotifications(db, int(category.ProjectID),
			fmt.Sprintf("Category updated: %s in project: %s", category.Name, projectName),
			"https://precastezy.blueinvent.com/departments")
	}
}

// DeleteCategory deletes a category
// @Summary Delete category
// @Description Delete a category by its ID
// @Tags Categories
// @Produce json
// @Param id path int true "Category ID"
// @Success 200 {object} models.CategoryResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/categories/{id} [delete]
func DeleteCategory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
			return
		}

		// Fetch category info before deletion for notifications
		var categoryName string
		var projectID int
		err = db.QueryRow("SELECT name, project_id FROM categories WHERE id = $1", id).Scan(&categoryName, &projectID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Delete category
		result, err := db.Exec("DELETE FROM categories WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check delete result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		c.JSON(http.StatusOK, models.CategoryResponse{
			Success: true,
			Message: "Category deleted successfully",
		})

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the category
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Category deleted: %s from project: %s", categoryName, projectName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/departments",
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
			fmt.Sprintf("Category deleted: %s from project: %s", categoryName, projectName),
			"https://precastezy.blueinvent.com/departments")
	}
}

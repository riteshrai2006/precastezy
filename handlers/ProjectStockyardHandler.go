package handlers

import (
	"backend/models"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AssignStockyardManager godoc
// @Summary      Assign stockyard manager
// @Tags         project-stockyards
// @Accept       json
// @Produce      json
// @Param        id    path      int  true  "Project stockyard ID"
// @Param        body  body      object  true  "{\"user_id\": int}"
// @Success      200   {object}  object
// @Failure      400   {object}  object
// @Failure      404   {object}  object
// @Router       /api/project-stockyards/{id}/manager [put]
func AssignStockyardManager(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
			return
		}

		type AssignStockyardManagerRequest struct {
			UserID int `json:"user_id" binding:"required"`
		}

		// 2. Bind JSON { "user_id": ... }
		var req AssignStockyardManagerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
			return
		}

		// 3. Update project_stockyard
		query := `
			UPDATE project_stockyard
			SET user_id = $1,
			    updated_at = NOW()
			WHERE id = $2
			RETURNING id, project_id, stockyard_id, created_at, updated_at, user_id
		`

		var ps models.ProjectStockyard
		err = db.QueryRow(query, req.UserID, id).Scan(
			&ps.ID,
			&ps.ProjectID,
			&ps.StockyardID,
			&ps.CreatedAt,
			&ps.UpdatedAt,
			&ps.UserID,
		)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project_stockyard not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign stockyard manager", "details": err.Error()})
			return
		}

		// 4. Send response
		c.JSON(http.StatusOK, gin.H{
			"message": "stockyard manager assigned successfully",
			"data":    ps,
		})
	}
}

// GetProjectStockyards godoc
// @Summary      Get project stockyards
// @Tags         project-stockyards
// @Param        project_id  path  int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400         {object}  object
// @Router       /api/projects/{project_id}/stockyards [get]
func GetProjectStockyards(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id parameter"})
			return
		}

		query := `
			SELECT 
				ps.id,
				ps.project_id,
				p.name,           
				ps.stockyard_id,
				s.yard_name,
				ps.user_id,
				COALESCE(u.first_name || ' ' || u.last_name, '') AS user_name,
				ps.created_at,
				ps.updated_at
			FROM project_stockyard ps
			JOIN project p ON ps.project_id = p.project_id
			JOIN stockyard s ON ps.stockyard_id = s.id
			LEFT JOIN users u ON ps.user_id = u.id
			WHERE ps.project_id = $1
			ORDER BY s.yard_name
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project stockyards", "details": err.Error()})
			return
		}
		defer rows.Close()

		var result []models.ProjectStockyardDetail

		for rows.Next() {
			var ps models.ProjectStockyardDetail
			err := rows.Scan(
				&ps.ID,
				&ps.ProjectID,
				&ps.ProjectName,
				&ps.StockyardID,
				&ps.YardName,
				&ps.UserID,
				&ps.UserName,
				&ps.CreatedAt,
				&ps.UpdatedAt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan row", "details": err.Error()})
				return
			}
			result = append(result, ps)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "stockyards fetched successfully",
			"data":    result,
		})
	}
}

// GetProjectStockyard godoc
// @Summary      Get single project stockyard
// @Tags         project-stockyards
// @Param        project_id    path  int  true  "Project ID"
// @Param        stockyard_id  path  int  true  "Stockyard ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/projects/{project_id}/stockyards/{stockyard_id} [get]
func GetProjectStockyard(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectIDStr := c.Param("project_id")
		stockyardIDStr := c.Param("stockyard_id")

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id parameter"})
			return
		}

		stockyardID, err := strconv.Atoi(stockyardIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stockyard_id parameter"})
			return
		}

		query := `
			SELECT 
				ps.id,
				ps.project_id,
				p.name,           
				ps.stockyard_id,
				s.yard_name,
				ps.user_id,
				COALESCE(u.first_name || ' ' || u.last_name, '') AS user_name,
				ps.created_at,
				ps.updated_at
			FROM project_stockyard ps
			JOIN project p ON ps.project_id = p.project_id
			JOIN stockyard s ON ps.stockyard_id = s.id
			LEFT JOIN users u ON ps.user_id = u.id
			WHERE ps.project_id = $1 AND ps.stockyard_id = $2
			LIMIT 1
		`

		var psd models.ProjectStockyardDetail

		err = db.QueryRow(query, projectID, stockyardID).Scan(
			&psd.ID,
			&psd.ProjectID,
			&psd.ProjectName,
			&psd.StockyardID,
			&psd.YardName,
			&psd.UserID,
			&psd.UserName,
			&psd.CreatedAt,
			&psd.UpdatedAt,
		)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project stockyard not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project stockyard", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "project stockyard fetched successfully",
			"data":    psd,
		})
	}
}

// AssignElementToStockyard godoc
// @Summary      Assign element to stockyard
// @Tags         project-stockyards
// @Accept       json
// @Produce      json
// @Param        project_id   path  int  true  "Project ID"
// @Param        element_id  path  int  true  "Element ID"
// @Param        body        body  object  true  "{\"stockyard_id\": int}"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      403  {object}  object
// @Failure      404  {object}  object
// @Failure      409  {object}  object
// @Router       /api/projects/{project_id}/assign-stockyard/{element_id} [put]
func AssignElementToStockyard(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		_ = session
		_ = userName

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var userID int
		err = db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		projectIDStr := c.Param("project_id")
		elementIDStr := c.Param("element_id")

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		elementID, err := strconv.Atoi(elementIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element_id"})
			return
		}

		type AssignElementToStockyardRequest struct {
			StockyardID int `json:"stockyard_id" binding:"required"`
		}

		var req AssignElementToStockyardRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
			return
		}
		stockyardID := req.StockyardID

		var (
			exists      bool
			inStockyard bool
		)

		err = db.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM precast_stock WHERE element_id = $1
			)
		`, elementID).Scan(&exists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Element not present in precast stock"})
			return
		}

		err = db.QueryRow(`
			SELECT stockyard FROM precast_stock WHERE element_id = $1
		`, elementID).Scan(&inStockyard)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stockyard status"})
			return
		}

		if inStockyard {
			c.JSON(http.StatusConflict, gin.H{"error": "Element already in stockyard"})
			return
		}

		var isManager bool
		err = db.QueryRow(`
			SELECT EXISTS (
				SELECT 1
				FROM project_stockyard
				WHERE project_id = $1
				  AND stockyard_id = $2
				  AND user_id = $3
			)
		`, projectID, stockyardID, userID).Scan(&isManager)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify stockyard manager", "details": err.Error()})
			return
		}

		if !isManager {
			c.JSON(http.StatusForbidden, gin.H{"error": "User is not assigned as stockyard manager for this project and stockyard"})
			return
		}

		_, err = db.Exec(`
			UPDATE precast_stock
			SET stockyard_id = $1,
			    stockyard = TRUE,
			    updated_at = NOW()
			WHERE element_id = $2
		`, stockyardID, elementID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update element stockyard", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Element assigned to stockyard successfully",
			"data": gin.H{
				"element_id":   elementID,
				"project_id":   projectID,
				"stockyard_id": stockyardID,
				"assigned_by":  userID,
			},
		})
	}
}

// GetMyProjectStockyards godoc
// @Summary      Get my project stockyards (for current user)
// @Tags         project-stockyards
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/projects/{project_id}/my-stockyards [get]
func GetMyProjectStockyards(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Session & user
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		_ = session
		_ = userName

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var userID int
		err = db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// 2. project_id from route
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id parameter"})
			return
		}

		// 3. Same query as GetProjectStockyards but filtered by user_id
		query := `
			SELECT 
				ps.id,
				ps.project_id,
				p.name,           
				ps.stockyard_id,
				s.yard_name,
				ps.user_id,
				COALESCE(u.first_name || ' ' || u.last_name, '') AS user_name,
				ps.created_at,
				ps.updated_at
			FROM project_stockyard ps
			JOIN project p ON ps.project_id = p.project_id
			JOIN stockyard s ON ps.stockyard_id = s.id
			LEFT JOIN users u ON ps.user_id = u.id
			WHERE ps.project_id = $1
			  AND ps.user_id = $2
			ORDER BY s.yard_name
		`

		rows, err := db.Query(query, projectID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project stockyards", "details": err.Error()})
			return
		}
		defer rows.Close()

		var result []models.ProjectStockyardDetail

		for rows.Next() {
			var ps models.ProjectStockyardDetail
			err := rows.Scan(
				&ps.ID,
				&ps.ProjectID,
				&ps.ProjectName,
				&ps.StockyardID,
				&ps.YardName,
				&ps.UserID,
				&ps.UserName,
				&ps.CreatedAt,
				&ps.UpdatedAt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan row", "details": err.Error()})
				return
			}
			result = append(result, ps)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error", "details": err.Error()})
			return
		}

		// 4. Response (empty array is fine if user has no stockyards)
		c.JSON(http.StatusOK, gin.H{
			"message": "stockyards for current user fetched successfully",
			"data":    result,
		})
	}
}

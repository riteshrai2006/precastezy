package handlers

import (
	"backend/models"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// GetProjectStatus godoc
// @Summary      Get project element status
// @Description  Returns status of all elements in a project (towers, floors, element types)
// @Tags         project-status
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Router       /api/project/{project_id}/status [get]
func GetProjectStatus(db *sql.DB) gin.HandlerFunc {
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

		// Get project name
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project name"})
			return
		}

		// Initialize project status
		projectStatus := models.ProjectStatus{
			ProjectName: projectName,
			Towers:      make(map[string]models.ElementTower),
		}

		// Query to get element counts by status, tower, floor, and element type
		query := `
		WITH element_status AS (
	SELECT 
		e.element_id,
		e.element_type_id,
		et.element_type,
		et.element_type_name,
		p.name AS floor_name,
		COALESCE(pf.name, '') AS tower_name,
		et.thickness,
		et.length,
		et.height,
		CASE 
			WHEN e.instage = true THEN 'production'
			WHEN ps.id IS NOT NULL THEN 'stockyard'
			WHEN se.id IS NOT NULL THEN 'erection'
			ELSE 'not_in_production'
		END AS element_status
	FROM element e
	JOIN element_type et ON e.element_type_id = et.element_type_id
	JOIN precast p ON e.target_location = p.id
	LEFT JOIN precast pf ON p.parent_id = pf.id
	LEFT JOIN activity a ON e.id = a.element_id
	LEFT JOIN precast_stock ps ON e.id = ps.element_id
	LEFT JOIN stock_erected se ON ps.id = se.precast_stock_id
	WHERE e.project_id = $1
)
SELECT 
	tower_name,
	floor_name,
	element_type,
	COUNT(*) AS total_count,
	COUNT(CASE WHEN element_status = 'production' THEN 1 END) AS production_count,
	COUNT(CASE WHEN element_status = 'stockyard' THEN 1 END) AS stockyard_count,
	COUNT(CASE WHEN element_status = 'erection' THEN 1 END) AS erection_count,
	COUNT(CASE WHEN element_status = 'not_in_production' THEN 1 END) AS not_in_production_count,
	SUM((thickness::numeric * length::numeric * height::numeric) / 1000000000) AS total_concrete_required,
	SUM(CASE 
			WHEN element_status IN ('production', 'stockyard', 'erection') 
			THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
			ELSE 0 
		END) AS total_concrete_used,
	SUM(CASE 
			WHEN element_status = 'not_in_production' 
			THEN (thickness::numeric * length::numeric * height::numeric) / 1000000000 
			ELSE 0 
		END) AS total_concrete_balance
FROM element_status
GROUP BY floor_name, tower_name, element_type
ORDER BY tower_name, floor_name, element_type;

	`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element status: " + err.Error()})
			return
		}
		defer rows.Close()

		// Process the results
		for rows.Next() {
			var towerName, floorName, elementType string
			var total, production, stockyard, erection, notInProduction int
			var totalConcreteRequired, totalConcreteUsed, totalConcreteBalance float64

			err := rows.Scan(
				&towerName,
				&floorName,
				&elementType,
				&total,
				&production,
				&stockyard,
				&erection,
				&notInProduction,
				&totalConcreteRequired,
				&totalConcreteUsed,
				&totalConcreteBalance,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element status: " + err.Error()})
				return
			}

			// Update project totals
			projectStatus.TotalElement += total
			projectStatus.Production += production
			projectStatus.Stockyard += stockyard
			projectStatus.Erection += erection
			projectStatus.NotInProduction += notInProduction
			projectStatus.TotalConcreteRequired += totalConcreteRequired
			projectStatus.TotalConcreteUsed += totalConcreteUsed
			projectStatus.TotalConcreteBalance += totalConcreteBalance

			// Initialize tower if it doesn't exist
			if _, exists := projectStatus.Towers[towerName]; !exists {
				projectStatus.Towers[towerName] = models.ElementTower{
					Floors: make(map[string]models.ElementFloor),
				}
			}

			// Initialize floor if it doesn't exist
			if _, exists := projectStatus.Towers[towerName].Floors[floorName]; !exists {
				projectStatus.Towers[towerName].Floors[floorName] = models.ElementFloor{
					ElementTypes: make(map[string]models.ElementStatus),
				}
			}

			// Update element type status
			status := models.ElementStatus{
				TotalElement:          total,
				Production:            production,
				Stockyard:             stockyard,
				Erection:              erection,
				NotInProduction:       notInProduction,
				TotalConcreteRequired: totalConcreteRequired,
				TotalConcreteUsed:     totalConcreteUsed,
				TotalConcreteBalance:  totalConcreteBalance,
			}

			// Add element type status to the floor
			floor := projectStatus.Towers[towerName].Floors[floorName]
			floor.ElementTypes[elementType] = status
			projectStatus.Towers[towerName].Floors[floorName] = floor
		}

		// Create the response structure
		response := make(map[string]interface{})

		// Add project-level information
		response["ProjectName"] = projectStatus.ProjectName
		response["TotalElement"] = projectStatus.TotalElement
		response["TotalElementInProduction"] = projectStatus.Production
		response["TotalElementInStockyard"] = projectStatus.Stockyard
		response["TotalElementInErection"] = projectStatus.Erection
		response["TotalElementBalance"] = projectStatus.NotInProduction
		response["TotalConcreteRequired"] = float64(int64(projectStatus.TotalConcreteRequired*100)) / 100
		response["TotalConcreteUsed"] = float64(int64(projectStatus.TotalConcreteUsed*100)) / 100
		response["TotalConcreteBalance"] = float64(int64(projectStatus.TotalConcreteBalance*100)) / 100

		// Add tower information directly to the response
		// Process only the first tower
		var firstTowerName string
		var firstTower models.ElementTower
		for towerName, tower := range projectStatus.Towers {
			firstTowerName = towerName
			firstTower = tower
			break // Only process the first tower
		}

		if firstTowerName != "" {
			towerData := make(map[string]interface{})

			// Process each floor in the tower
			for floorName, floor := range firstTower.Floors {
				floorData := make(map[string]interface{})

				// Calculate floor totals
				var floorTotal, floorProduction, floorStockyard, floorErection, floorNotInProduction int
				for _, elementStatus := range floor.ElementTypes {
					floorTotal += elementStatus.TotalElement
					floorProduction += elementStatus.Production
					floorStockyard += elementStatus.Stockyard
					floorErection += elementStatus.Erection
					floorNotInProduction += elementStatus.NotInProduction
				}

				// Add element types directly to floor data
				for elementType, status := range floor.ElementTypes {
					floorData[elementType] = map[string]interface{}{
						"totalElement":          status.TotalElement,
						"production":            status.Production,
						"stockyard":             status.Stockyard,
						"erection":              status.Erection,
						"notInProduction":       status.NotInProduction,
						"totalConcreteRequired": float64(int64(status.TotalConcreteRequired*100)) / 100,
						"totalConcreteUsed":     float64(int64(status.TotalConcreteUsed*100)) / 100,
						"totalConcreteBalance":  float64(int64(status.TotalConcreteBalance*100)) / 100,
					}
				}

				// Add basic_details
				floorData["basic_details"] = map[string]interface{}{
					"totalElement":    floorTotal,
					"production":      floorProduction,
					"stockyard":       floorStockyard,
					"erection":        floorErection,
					"notInProduction": floorNotInProduction,
				}

				towerData[floorName] = floorData
			}

			response[firstTowerName] = towerData
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Get",
			Description:  "Get Project Status",
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

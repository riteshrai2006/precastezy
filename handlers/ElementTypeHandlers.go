package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"backend/utils"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// CreateElementType creates a new element type
// @Summary Create element type
// @Description Create a new element type with stages and hierarchy
// @Tags ElementTypes
// @Accept json
// @Produce json
// @Param request body models.ElementType true "Element type creation request"
// @Success 201 {object} models.ElementTypeResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/elementtype_create [post]
func CreateElementType(db *sql.DB) gin.HandlerFunc {
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

		var element models.ElementType
		var rawBody map[string]json.RawMessage

		// Read body once
		bodyBytes, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}

		// Unmarshal to raw map for extracting stages
		if err := json.Unmarshal(bodyBytes, &rawBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}

		// Unmarshal to struct
		if err := json.Unmarshal(bodyBytes, &element); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Extract stage IDs
		var stageIDs []int
		if stagesRaw, ok := rawBody["stages"]; ok {
			var stagesArr []map[string]interface{}
			if err := json.Unmarshal(stagesRaw, &stagesArr); err == nil {
				for _, s := range stagesArr {
					if idFloat, ok := s["stages_id"].(float64); ok {
						stageIDs = append(stageIDs, int(idFloat))
					}
				}
			}
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
			return
		}

		// Defer rollback in case of error
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// // Verify that all stage IDs exist in project_stages for this project
		// if len(stageIDs) > 0 {
		// 	// Create a query to check if all stage IDs exist in project_stages for this project
		// 	query := `
		// 		SELECT COUNT(*)
		// 		FROM project_stages
		// 		WHERE id = ANY($1) AND project_id = $2
		// 	`
		// 	var count int
		// 	err = tx.QueryRow(query, pq.Array(stageIDs), element.ProjectID).Scan(&count)
		// 	if err != nil {
		// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify stage IDs: " + err.Error()})
		// 		return
		// 	}

		// 	// If count doesn't match the number of stage IDs, some stages don't exist
		// 	if count != len(stageIDs) {
		// 		c.JSON(http.StatusBadRequest, gin.H{"error": "one or more stage IDs do not exist in project stages"})
		// 		return
		// 	}
		// }

		// Get created_by from session
		err = tx.QueryRow(`SELECT host_name FROM session WHERE session_id = $1`, sessionID).Scan(&element.CreatedBy)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session ID: user not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error while retrieving session"})
			return
		}

		element.CreatedAt = time.Now()
		element.UpdatedAt = time.Now()
		element.ElementTypeId = repository.GenerateRandomNumber()
		element.TotalCountElement = 0
		element.ElementTypeVersion = repository.GenerateVersionCode("")

		// Calculate density if mass and volume provided
		if element.Volume > 0 {
			element.Density = element.Mass / element.Volume
		}

		sqlStatement := `INSERT INTO element_type ( 
			element_type,
			element_type_name,
			thickness,
			length,
			height,
			volume,
			mass,
			area,
			width,
				created_by,
				created_at,
				update_at,
				project_id,
				element_type_version,
				total_count_element,
				density
		    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING element_type_id`

		err = tx.QueryRow(sqlStatement,
			element.ElementType,
			element.ElementTypeName,
			element.Thickness,
			element.Length,
			element.Height,
			element.Volume,
			element.Mass,
			element.Area,
			element.Width,
			element.CreatedBy,
			element.CreatedAt,
			element.UpdatedAt,
			element.ProjectID,
			element.ElementTypeVersion,
			element.TotalCountElement,
			element.Density).Scan(&element.ElementTypeId)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to insert element type: " + err.Error()})
			return
		}

		// Insert stage path
		if len(stageIDs) > 0 {
			_, err := tx.Exec(`INSERT INTO element_type_path (element_type_id, stage_path) VALUES ($1, $2)`, element.ElementTypeId, pq.Array(stageIDs))
			if err != nil {
				log.Printf("failed to insert stage path: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to insert stage path: " + err.Error()})
				return
			}
		}

		// Insert Drawings
		for _, drawing := range element.Drawings {
			drawing.DrawingsId = repository.GenerateRandomNumber()
			drawing.ProjectId = element.ProjectID
			drawing.ElementTypeID = element.ElementTypeId

			if err := CreateDrawingWithTx(c, drawing, tx); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create drawing: " + err.Error()})
				return
			}
		}

		// Insert hierarchy quantities into new table
		for _, HQ := range element.HierarchyQ {
			// Get naming convention from precast table
			var namingConvention string
			err := tx.QueryRow(`SELECT naming_convention FROM precast WHERE id = $1`, HQ.HierarchyId).Scan(&namingConvention)
			if err != nil {
				if err == sql.ErrNoRows {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("HierarchyId %d not found in precast", HQ.HierarchyId)})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "database error while retrieving naming convention"})
				return
			}

			// Insert into element_type_hierarchy_quantity table (updated columns)
			_, err = tx.Exec(`INSERT INTO element_type_hierarchy_quantity (
				element_type_id, hierarchy_id, quantity, naming_convention,
				element_type_name, element_type, left_quantity, project_id
			) VALUES ($1, $2, $3, $4, $5, $6, 0, $7)`,
				element.ElementTypeId, HQ.HierarchyId, HQ.Quantity, namingConvention,
				element.ElementTypeName, element.ElementType, element.ProjectID)

			if err != nil {
				log.Printf("Error inserting ElementTypeHierarchyQuantity: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error: %v", err)})
				return
			}
		}

		// Process hierarchy data
		jsondata := models.ElementInput{
			ElementTypeID:      element.ElementTypeId,
			SessionID:          sessionID,
			ProjectID:          element.ProjectID,
			ElementType:        element.ElementType,
			ElementTypeName:    element.ElementTypeName,
			ElementTypeVersion: element.ElementTypeVersion,
			TotalCountElement:  element.TotalCountElement,
		}
		if err := ProcessHierarchyDataWithTx(c, element.HierarchyQ, jsondata, tx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process hierarchy data: " + err.Error()})
			return
		}

		// Insert BOM products individually
		for _, product := range element.Products {
			// Get product name from inv_bom table
			var productName string
			productQuery := `SELECT product_name FROM inv_bom WHERE id = $1`
			err := tx.QueryRow(productQuery, product.ProductID).Scan(&productName)
			if err != nil {
				if err == sql.ErrNoRows {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Product ID %d not found in inventory", product.ProductID)})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error while fetching product name: %v", err)})
				return
			}

			// Insert individual BOM record
			_, err = tx.Exec(`INSERT INTO element_type_bom (element_type_id, project_id, product_id, product_name, quantity, created_at, created_by, updated_at, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				element.ElementTypeId, element.ProjectID, product.ProductID, productName, product.Quantity,
				element.CreatedAt, element.CreatedBy, element.UpdatedAt, element.CreatedBy)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error inserting BOM product: %v", err)})
				return
			}
		}

		// Commit the transaction
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit transaction: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Element Type, Drawings, Element, BOM created successfully",
			"id":      element.ElementTypeId,
		})

		// Get project name for notification
		ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
		defer cancel()

		var projectName string
		err = db.QueryRowContext(ctx, "SELECT name FROM project WHERE project_id = $1", element.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", element.ProjectID)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRowContext(ctx, "SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the element type
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New element type created: %s for project: %s", element.ElementTypeName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", element.ProjectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.ExecContext(ctx, `
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, element.ProjectID,
			fmt.Sprintf("New element type created: %s for project: %s", element.ElementTypeName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", element.ProjectID))

		activityLog := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "Create",
			Description:  fmt.Sprintf("Create Element Type %d", element.ElementTypeId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    element.ProjectID,
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

// CreateDrawingWithTx creates a drawing within a transaction
func CreateDrawingWithTx(c *gin.Context, drawing models.Drawings, tx *sql.Tx) error {
	// First verify that the drawing type ID exists for this project
	query := `
		SELECT COUNT(*) 
		FROM drawing_type 
		WHERE drawing_type_id = $1 AND project_id = $2
	`
	var count int
	err := tx.QueryRow(query, drawing.DrawingTypeId, drawing.ProjectId).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to verify drawing type ID: %v", err)
	}
	if count == 0 {
		return fmt.Errorf("drawing type ID %d does not exist for project %d", drawing.DrawingTypeId, drawing.ProjectId)
	}
	drawing.CreatedAt = time.Now()
	drawing.UpdateAt = time.Now()

	// If verification passes, proceed with insertion
	sqlStatement := `INSERT INTO drawings (
		
		current_version,
		created_at,
		created_by,
		drawing_type_id,
		update_at,
		updated_by,
		comments,
		file,
		element_type_id,
		project_id
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err = tx.Exec(sqlStatement,

		drawing.CurrentVersion,
		drawing.CreatedAt,
		drawing.CreatedBy,
		drawing.DrawingTypeId,
		drawing.UpdateAt,
		drawing.CreatedBy,
		drawing.Comments,
		drawing.File,
		drawing.ElementTypeID,
		drawing.ProjectId,
	)

	return err
}

// ProcessHierarchyDataWithTx processes hierarchy data within a transaction
func ProcessHierarchyDataWithTx(c *gin.Context, hierarchyQ []models.HierarchyQuantity, inputData models.ElementInput, tx *sql.Tx) error {
	log.Println("Process HierarchyData was called")

	for _, hq := range hierarchyQ {
		var hierarchy models.HierarchyQuantity
		query := `SELECT naming_convention FROM precast WHERE id = $1`

		err := tx.QueryRow(query, hq.HierarchyId).Scan(&hierarchy.NamingConvention)
		if err != nil {
			return fmt.Errorf("error fetching data for hierarchy_id %d: %v", hq.HierarchyId, err)
		}

		elementData := models.ElementInput{
			HierarchyId:        hq.HierarchyId,
			Quantity:           hq.Quantity,
			NamingConvention:   hierarchy.NamingConvention,
			SessionID:          inputData.SessionID,
			ElementTypeID:      inputData.ElementTypeID,
			ProjectID:          inputData.ProjectID,
			ElementType:        inputData.ElementType,
			ElementTypeName:    inputData.ElementTypeName,
			ElementTypeVersion: inputData.ElementTypeVersion,
			TotalCountElement:  inputData.TotalCountElement,
		}

		jsonBytes, err := json.MarshalIndent(elementData, "", "  ")
		if err != nil {
			return fmt.Errorf("error marshalling to JSON: %v", err)
		}

		fmt.Println(string(jsonBytes))

		CreateElements(c, elementData)
	}

	return nil
}

func ProcessHierarchyData(c *gin.Context, hierarchyQ []models.HierarchyQuantity, inputData models.ElementInput) {
	db := storage.GetDB()

	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	log.Println("Process HierarchyData was called")

	// Print inputData fields
	log.Printf("InputData: HierarchyId=%d, Quantity=%d, NamingConvention=%s,ElementTypeID=%d,SessionID=%s\n",
		inputData.HierarchyId, inputData.Quantity, inputData.NamingConvention, inputData.ElementTypeID, inputData.SessionID)

	// Print each element in hierarchyQ
	for i, hq := range hierarchyQ {
		log.Printf("HierarchyQuantity[%d]: HierarchyId=%d, Quantity=%d, NamingConvention=%s\n",
			i, hq.HierarchyId, hq.Quantity, hq.NamingConvention)
	}

	for _, hq := range hierarchyQ {
		var hierarchy models.HierarchyQuantity
		query := `SELECT naming_convention FROM precast WHERE id = $1`

		// Fetch data from the database for each hierarchy_id
		err := tx.QueryRow(query, hq.HierarchyId).Scan(&hierarchy.NamingConvention)
		if err != nil {
			log.Printf("Error fetching data for hierarchy_id %d: %v\n", hq.HierarchyId, err)
			continue
		}

		// Create a new ElementInput instance with the fetched data
		elementData := models.ElementInput{
			HierarchyId:        hq.HierarchyId,
			Quantity:           hq.Quantity,
			NamingConvention:   hierarchy.NamingConvention,
			SessionID:          inputData.SessionID,
			ElementTypeID:      inputData.ElementTypeID,
			ProjectID:          inputData.ProjectID,
			ElementType:        inputData.ElementType,
			ElementTypeName:    inputData.ElementTypeName,
			ElementTypeVersion: inputData.ElementTypeVersion,
			TotalCountElement:  inputData.TotalCountElement,
		}

		// Marshal elementData to JSON
		jsonBytes, err := json.MarshalIndent(elementData, "", "  ")
		if err != nil {
			log.Printf("Error marshalling to JSON: %v", err)
			continue
		}

		// Print JSON as string
		fmt.Println(string(jsonBytes))

		// Call CreateElements function with context and elementData
		CreateElements(c, elementData)
	}
}

// UpdateElementType updates an existing element type
// @Summary Update element type
// @Description Update an existing element type
// @Tags ElementTypes
// @Accept json
// @Produce json
// @Param element_type_id path int true "Element Type ID"
// @Param request body models.ElementType true "Element type update request"
// @Success 200 {object} models.ElementTypeResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/elementtype_update/{element_type_id} [put]
func UpdateElementType(db *sql.DB) gin.HandlerFunc {
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

		var elementType models.ElementType

		// Bind JSON data to elementType struct
		if err := c.ShouldBindJSON(&elementType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Get element type ID from URL parameter
		elementTypeID := c.Param("element_type_id")

		// Retrieve the current element type
		ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
		defer cancel()

		var currentElementType models.ElementType
		err = db.QueryRowContext(ctx,
			`SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width,
		created_by, created_at, update_at, element_type_id, project_id, element_type_version, total_count_element,
		density
		FROM element_type 
		WHERE element_type_id = $1`, elementTypeID).Scan(
			&currentElementType.ElementType,
			&currentElementType.ElementTypeName,
			&currentElementType.Thickness,
			&currentElementType.Length,
			&currentElementType.Height,
			&currentElementType.Volume,
			&currentElementType.Mass,
			&currentElementType.Area,
			&currentElementType.Width,
			&currentElementType.CreatedBy,
			&currentElementType.CreatedAt,
			&currentElementType.UpdatedAt,
			&currentElementType.ElementTypeId,
			&currentElementType.ProjectID,
			&currentElementType.ElementTypeVersion,
			&currentElementType.TotalCountElement,
			&currentElementType.Density,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element type not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to fetch current element type: %v", err)})
			return
		}

		elementType.UpdatedAt = time.Now()
		var updates []string
		var fields []interface{}
		placeholderIndex := 1

		// Update the element type version
		elementType.ElementTypeVersion = repository.GenerateVersionCode(currentElementType.ElementTypeVersion)

		// Dynamically add fields for update
		if elementType.ElementTypeName != "" {
			updates = append(updates, fmt.Sprintf("element_type_name = $%d", placeholderIndex))
			fields = append(fields, elementType.ElementTypeName)
			placeholderIndex++
		}
		if elementType.Thickness != 0 {
			updates = append(updates, fmt.Sprintf("thickness = $%d", placeholderIndex))
			fields = append(fields, elementType.Thickness)
			placeholderIndex++
		}
		if elementType.Length != 0 {
			updates = append(updates, fmt.Sprintf("length = $%d", placeholderIndex))
			fields = append(fields, elementType.Length)
			placeholderIndex++
		}
		if elementType.Height != 0 {
			updates = append(updates, fmt.Sprintf("height = $%d", placeholderIndex))
			fields = append(fields, elementType.Height)
			placeholderIndex++
		}
		if elementType.Volume != 0 {
			updates = append(updates, fmt.Sprintf("volume = $%d", placeholderIndex))
			fields = append(fields, elementType.Volume)
			placeholderIndex++
		}
		if elementType.Mass != 0 {
			updates = append(updates, fmt.Sprintf("mass = $%d", placeholderIndex))
			fields = append(fields, elementType.Mass)
			placeholderIndex++
		}
		if elementType.Area != 0 {
			updates = append(updates, fmt.Sprintf("area = $%d", placeholderIndex))
			fields = append(fields, elementType.Area)
			placeholderIndex++
		}
		if elementType.Width != 0 {
			updates = append(updates, fmt.Sprintf("width = $%d", placeholderIndex))
			fields = append(fields, elementType.Width)
			placeholderIndex++
		}
		if elementType.ElementTypeVersion != "" {
			updates = append(updates, fmt.Sprintf("element_type_version = $%d", placeholderIndex))
			fields = append(fields, elementType.ElementTypeVersion)
			placeholderIndex++
		}
		if elementType.Density != 0 {
			updates = append(updates, fmt.Sprintf("density = $%d", placeholderIndex))
			fields = append(fields, elementType.Density)
			placeholderIndex++
		}
		if !elementType.UpdatedAt.IsZero() {
			updates = append(updates, fmt.Sprintf("update_at = $%d", placeholderIndex))
			fields = append(fields, elementType.UpdatedAt)
			placeholderIndex++
		}

		// Ensure there is something to update
		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
			return
		}

		// Create the SQL update query dynamically
		sqlStatement := fmt.Sprintf("UPDATE element_type SET %s WHERE element_type_id = $%d", strings.Join(updates, ", "), placeholderIndex)
		fields = append(fields, elementTypeID)

		// Execute the SQL statement to update element_type
		ctxUpdate, cancelUpdate := utils.GetFastQueryContext(c.Request.Context())
		defer cancelUpdate()

		_, err = db.ExecContext(ctxUpdate, sqlStatement, fields...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to update element type: %v", err)})
			return
		}

		// Process hierarchy data with smart quantity management
		if len(elementType.HierarchyQ) > 0 {
			// Validate hierarchy IDs first
			for _, hq := range elementType.HierarchyQ {
				if hq.HierarchyId <= 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid HierarchyId %d - must be greater than 0", hq.HierarchyId)})
					return
				}
			}

			// Get current hierarchy quantities for comparison
			currentHierarchyData := make(map[int]int) // hierarchy_id -> current_quantity
			ctxHierarchy, cancelHierarchy := utils.GetDefaultQueryContext(c.Request.Context())
			defer cancelHierarchy()

			currentRows, err := db.QueryContext(ctxHierarchy, `SELECT hierarchy_id, quantity FROM element_type_hierarchy_quantity WHERE element_type_id = $1`, currentElementType.ElementTypeId)
			if err != nil {
				log.Printf("Error fetching current hierarchy data: %v", err)
			} else {
				defer currentRows.Close()
				for currentRows.Next() {
					var hierarchyId, quantity int
					if err := currentRows.Scan(&hierarchyId, &quantity); err == nil {
						currentHierarchyData[hierarchyId] = quantity
					}
				}
			}

			// Process each hierarchy quantity change
			elementInputData := models.ElementInput{
				ElementTypeID:      currentElementType.ElementTypeId,
				SessionID:          elementType.SessionID,
				ProjectID:          currentElementType.ProjectID,
				ElementType:        currentElementType.ElementType,
				ElementTypeName:    currentElementType.ElementTypeName,
				ElementTypeVersion: currentElementType.ElementTypeVersion,
				TotalCountElement:  currentElementType.TotalCountElement,
			}

			for _, newHQ := range elementType.HierarchyQ {
				// Get naming convention from precast table
				var namingConvention string
				err := db.QueryRowContext(ctxHierarchy, `SELECT naming_convention FROM precast WHERE id = $1`, newHQ.HierarchyId).Scan(&namingConvention)
				if err != nil {
					if err == sql.ErrNoRows {
						c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("HierarchyId %d not found in precast", newHQ.HierarchyId)})
						return
					}
					c.JSON(http.StatusInternalServerError, gin.H{"error": "database error while retrieving naming convention"})
					return
				}

				// Get current quantity for this hierarchy
				currentQuantity := currentHierarchyData[newHQ.HierarchyId]
				quantityChange := newHQ.Quantity - currentQuantity

				log.Printf("Hierarchy %d: Current=%d, New=%d, Change=%d", newHQ.HierarchyId, currentQuantity, newHQ.Quantity, quantityChange)

				if quantityChange > 0 {
					// Quantity increased - create extra elements
					log.Printf("Quantity increased by %d for hierarchy_id %d - creating extra elements", quantityChange, newHQ.HierarchyId)
					elementData := models.ElementInput{
						HierarchyId:        newHQ.HierarchyId,
						Quantity:           quantityChange,
						NamingConvention:   namingConvention,
						SessionID:          elementInputData.SessionID,
						ElementTypeID:      elementInputData.ElementTypeID,
						ProjectID:          elementInputData.ProjectID,
						ElementType:        elementInputData.ElementType,
						ElementTypeName:    elementInputData.ElementTypeName,
						ElementTypeVersion: elementInputData.ElementTypeVersion,
						TotalCountElement:  elementInputData.TotalCountElement,
					}
					CreateElements(c, elementData)
				} else if quantityChange < 0 {
					// Quantity decreased - delete extra elements
					log.Printf("Quantity decreased by %d for hierarchy_id %d - deleting extra elements", -quantityChange, newHQ.HierarchyId)
					elementData := models.ElementInput{
						HierarchyId:        newHQ.HierarchyId,
						Quantity:           -quantityChange,
						NamingConvention:   namingConvention,
						SessionID:          elementInputData.SessionID,
						ElementTypeID:      elementInputData.ElementTypeID,
						ProjectID:          elementInputData.ProjectID,
						ElementType:        elementInputData.ElementType,
						ElementTypeName:    elementInputData.ElementTypeName,
						ElementTypeVersion: elementInputData.ElementTypeVersion,
						TotalCountElement:  elementInputData.TotalCountElement,
					}
					DeleteElements(c, elementData)
				} else {
					log.Printf("No quantity change for hierarchy_id %d - skipping", newHQ.HierarchyId)
				}

				// Update or insert hierarchy quantity record
				// First check if record exists
				var existingQuantity int
				err = db.QueryRowContext(ctxHierarchy, `SELECT quantity FROM element_type_hierarchy_quantity WHERE element_type_id = $1 AND hierarchy_id = $2`,
					currentElementType.ElementTypeId, newHQ.HierarchyId).Scan(&existingQuantity)

				switch err {
				case sql.ErrNoRows:
					// Record doesn't exist, insert new one
					_, err = db.ExecContext(ctxHierarchy, `INSERT INTO element_type_hierarchy_quantity (element_type_id, hierarchy_id, quantity, naming_convention)
						VALUES ($1, $2, $3, $4)`,
						currentElementType.ElementTypeId, newHQ.HierarchyId, newHQ.Quantity, namingConvention)
				case nil:
					// Record exists, update it
					_, err = db.ExecContext(ctxHierarchy, `UPDATE element_type_hierarchy_quantity 
						SET quantity = $1, naming_convention = $2 
						WHERE element_type_id = $3 AND hierarchy_id = $4`,
						newHQ.Quantity, namingConvention, currentElementType.ElementTypeId, newHQ.HierarchyId)
				}

				if err != nil {
					log.Printf("Error updating hierarchy quantity for hierarchy_id %d: %v", newHQ.HierarchyId, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error updating hierarchy quantity: %v", err)})
					return
				}
			}

			// Update total count element
			totalQuantity := 0
			for _, hq := range elementType.HierarchyQ {
				totalQuantity += hq.Quantity
			}

			// Update element_type with new total count
			_, err = db.ExecContext(ctxHierarchy, `UPDATE element_type SET total_count_element = $1 WHERE element_type_id = $2`,
				totalQuantity, currentElementType.ElementTypeId)
			if err != nil {
				log.Printf("Error updating total count element: %v", err)
			}

			log.Printf("Successfully updated hierarchy quantities for element_type_id: %d", currentElementType.ElementTypeId)
		}
		// Handle BOM products update with proper revision tracking
		// Step 1: ALWAYS fetch all existing BOM data and save to revision table BEFORE any updates
		// SaveBOMToRevisionTable handles all verification internally, so we just call it and check for errors
		_, err = SaveBOMToRevisionTable(db, currentElementType.ElementTypeId, currentElementType.ProjectID, userName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save BOM to revision table: %v", err)})
			return
		}

		// Step 2: Only update BOM if new products are provided
		if len(elementType.Products) > 0 {
			// Delete existing BOM records for this element type
			ctxBOM, cancelBOM := utils.GetFastQueryContext(c.Request.Context())
			defer cancelBOM()

			_, err = db.ExecContext(ctxBOM, `DELETE FROM element_type_bom WHERE element_type_id = $1 AND project_id = $2`,
				currentElementType.ElementTypeId, currentElementType.ProjectID)
			if err != nil {
				log.Printf("Error deleting existing BOM records: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing BOM records"})
				return
			}

			// Step 4: Insert new BOM products into element_type_bom table
			for _, product := range elementType.Products {
				// Get product name from inv_bom table
				var productName string
				productQuery := `SELECT product_name FROM inv_bom WHERE id = $1`
				err := db.QueryRowContext(ctxBOM, productQuery, product.ProductID).Scan(&productName)
				if err != nil {
					if err == sql.ErrNoRows {
						c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Product ID %d not found in inventory", product.ProductID)})
						return
					}
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error while fetching product name: %v", err)})
					return
				}

				// Insert individual BOM record with unit and rate
				_, err = db.ExecContext(ctxBOM, `INSERT INTO element_type_bom (element_type_id, project_id, product_id, product_name, quantity, unit, rate, created_at, created_by, updated_at, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
					currentElementType.ElementTypeId, currentElementType.ProjectID, product.ProductID, productName, product.Quantity,
					product.Unit, product.Rate,
					time.Now(), userName, time.Now(), userName)

				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database error inserting BOM product: %v", err)})
					return
				}
			}

		}

		// Update related drawings and capture revision IDs
		var createdDrawingRevisionIDs []int
		for _, drawing := range elementType.Drawings {
			drawing.ElementTypeID = currentElementType.ElementTypeId
			drawingRevisionID, err := UpdateDrawing(c, drawing)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update drawing: " + err.Error()})
				return
			}

			// Capture the drawing revision ID that was just created
			if drawingRevisionID > 0 {
				createdDrawingRevisionIDs = append(createdDrawingRevisionIDs, drawingRevisionID)
				log.Printf("Captured drawing revision ID: %d for drawing type: %d", drawingRevisionID, drawing.DrawingTypeId)
			}
		}

		// Fetch all elements related to this element type that are in "postpore" stage and have null revision IDs
		var affectedElements []int
		fetchAffectedElementsQuery := `
		SELECT a.element_id 
FROM activity a 
JOIN element e 
    ON e.id = a.element_id
WHERE a.project_id = $2
  AND e.instage = true
  AND e.element_type_id = $1
  AND a.completed = true
  AND (e.drawing_revision_id IS NULL);

		`
		ctxAffected, cancelAffected := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelAffected()

		affectedRows, affectedErr := db.QueryContext(ctxAffected, fetchAffectedElementsQuery, currentElementType.ElementTypeId, currentElementType.ProjectID)
		if affectedErr != nil {
			log.Printf("Error fetching affected elements: %v", affectedErr)
			// Don't return error here, just log it as this might be a new feature
		} else {
			defer affectedRows.Close()
			for affectedRows.Next() {
				var elementID int
				if err := affectedRows.Scan(&elementID); err == nil {
					affectedElements = append(affectedElements, elementID)
				}
			}
		}

		// Update drawing_revision_id for all affected elements
		if len(affectedElements) > 0 {
			// Since we're using direct BOM updates now, we don't have BOM revision IDs

			// Use the captured drawing revision IDs from when they were created
			var drawingRevisionIDToUse interface{}
			if len(createdDrawingRevisionIDs) > 0 {
				drawingRevisionIDToUse = createdDrawingRevisionIDs[0]
				log.Printf("Using captured drawing revision ID: %d", createdDrawingRevisionIDs[0])
			} else {
				drawingRevisionIDToUse = nil
			}

			// Check if we have drawing revision IDs to update
			if len(createdDrawingRevisionIDs) > 0 {
				// Update all affected elements with the captured revision IDs
				for _, elementID := range affectedElements {
					// Build dynamic update query based on available revision IDs
					var updateFields []string
					var updateValues []interface{}
					placeholderIndex := 1

					// Add drawing revision ID
					updateFields = append(updateFields, fmt.Sprintf("drawing_revision_id = $%d", placeholderIndex))
					updateValues = append(updateValues, drawingRevisionIDToUse)
					placeholderIndex++

					// Always update the update_at timestamp
					updateFields = append(updateFields, fmt.Sprintf("update_at = $%d", placeholderIndex))
					updateValues = append(updateValues, time.Now())
					placeholderIndex++

					// Add element ID for WHERE clause
					updateValues = append(updateValues, elementID)

					// Build and execute the update query
					updateElementQuery := fmt.Sprintf(`
					UPDATE element 
					SET %s
					WHERE id = $%d
					`, strings.Join(updateFields, ", "), placeholderIndex)

					_, updateErr := db.ExecContext(ctxAffected, updateElementQuery, updateValues...)

					if updateErr != nil {
						log.Printf("Error updating revision IDs for element %d: %v", elementID, updateErr)
					} else {
						log.Printf("Successfully updated revision IDs for element %d (Drawing: %v)",
							elementID, drawingRevisionIDToUse)
					}
				}
				log.Printf("Updated revision IDs for %d affected elements using captured revision IDs", len(affectedElements))
			}
		}
		// Disable elements for this element type
		// Disable if: not in activity table OR in activity table with completed = true
		// Keep enabled if: in activity table with completed = false
		disableElementsQuery := `
	UPDATE element e
	SET disable = true, update_at = now() 
	WHERE e.element_type_id = $1 
	AND e.project_id = $2 
	AND e.instage = true
	AND (
		e.id NOT IN (SELECT a.element_id FROM activity a WHERE a.element_id = e.id)
		OR
		EXISTS (SELECT 1 FROM activity a WHERE a.element_id = e.id AND a.completed = true)
	)
`
		ctxDisable, cancelDisable := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelDisable()

		result, err := db.ExecContext(ctxDisable, disableElementsQuery, currentElementType.ElementTypeId, currentElementType.ProjectID)
		if err != nil {
			log.Printf("Error disabling elements: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable related elements"})
			return
		}

		// Get rows affected (for internal use, not logging)
		_, err = result.RowsAffected()
		if err != nil {
			log.Printf("Error getting rows affected: %v", err)
		}

		// Step 1: Get matching element_ids that exist in activity table
		elementIDs := []int{}

		// Check if there are any elements for this element_type_id at all
		var totalElements int
		err = db.QueryRowContext(ctxDisable, `SELECT COUNT(*) FROM element WHERE element_type_id = $1 AND project_id = $2`,
			currentElementType.ElementTypeId, currentElementType.ProjectID).Scan(&totalElements)
		if err != nil {
			log.Printf("Error counting total elements: %v", err)
		}

		// Check how many elements are in activity table
		var elementsInActivity int
		err = db.QueryRowContext(ctxDisable, `SELECT COUNT(*) FROM element e JOIN activity a ON a.element_id = e.id WHERE e.element_type_id = $1 AND e.project_id = $2`,
			currentElementType.ElementTypeId, currentElementType.ProjectID).Scan(&elementsInActivity)
		if err != nil {
			log.Printf("Error counting elements in activity: %v", err)
		}

		query := `
		SELECT e.id 
		FROM element e
		JOIN activity a ON a.element_id = e.id
		WHERE e.element_type_id = $1 AND e.project_id = $2 AND e.instage = true
	`
		rows, err := db.QueryContext(ctxDisable, query, currentElementType.ElementTypeId, currentElementType.ProjectID)
		if err != nil {
			log.Printf("Error fetching element IDs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch affected elements"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err == nil {
				elementIDs = append(elementIDs, id)
			}
		}

		var stagePathStr string
		stagePathQuery := `SELECT stage_path FROM element_type_path WHERE element_type_id = $1`
		err = db.QueryRowContext(ctxDisable, stagePathQuery, currentElementType.ElementTypeId).Scan(&stagePathStr)
		if err != nil {
			log.Printf("Error fetching stage path: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve stage path"})
			return
		}
		stagePath := parseStagePath(stagePathStr)
		if len(stagePath) == 0 {
			log.Printf("Stage path is empty for element_type_id: %d", currentElementType.ElementTypeId)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Stage path is empty"})
			return
		}

		firstStageID := stagePath[0]

		var newAssignedTo, newQCID, newPaperID int
		// Try to fetch stage details, but handle gracefully if columns don't exist
		stageQuery := `SELECT COALESCE(assigned_to, 0), COALESCE(qc_id, 0), COALESCE(paper_id, 0) FROM project_stages WHERE id = $1 LIMIT 1`
		err = db.QueryRowContext(ctxDisable, stageQuery, firstStageID).Scan(&newAssignedTo, &newQCID, &newPaperID)
		if err != nil {
			log.Printf("Error fetching stage details for stage_id %d: %v", firstStageID, err)
			// If the query fails, use default values
			newAssignedTo = 0
			newQCID = 0
			newPaperID = 0
			log.Printf("Using default values: assigned_to=%d, qc_id=%d, paper_id=%d", newAssignedTo, newQCID, newPaperID)
		}

		// Step 3: Update elements whose current stage is not the first stage
		updatedCount := 0
		for _, elementID := range elementIDs {
			var currentStageID int
			err := db.QueryRowContext(ctxDisable, `SELECT stage_id FROM activity WHERE element_id = $1`, elementID).Scan(&currentStageID)
			if err != nil {
				log.Printf("Error getting stage_id for element %d: %v", elementID, err)
				continue
			}

			if currentStageID != firstStageID {
				log.Printf("Updating stage_id and all status fields to Inprogress for element %d", elementID)
				log.Printf("New assigned_to: %d, new QC ID: %d, new paper ID: %d", newAssignedTo, newQCID, newPaperID)
				// Update stage_id and all status fields to "Inprogress"
				_, err := db.ExecContext(ctxDisable, `
					UPDATE activity 
					SET stage_id = $1,
						status = 'Inprogress',
						qc_status = 'Inprogress',
						mesh_mold_status = 'Inprogress',
						reinforcement_status = 'Inprogress',
						meshmold_qc_status = 'Inprogress',
						reinforcement_qc_status = 'Inprogress',
						assigned_to = $2,
						qc_id = $3,
						paper_id = $4
					WHERE element_id = $5 AND completed = false
				`, firstStageID, newAssignedTo, newQCID, newPaperID, elementID)
				if err != nil {
					log.Printf("Error updating stage and status for element %d: %v", elementID, err)
					continue
				}
				updatedCount++
			}
		}

		// Check if any elements are in production
		var productionCount int
		productionQuery := `SELECT COUNT(*) FROM element WHERE element_type_id = $1 AND project_id = $2 AND instage = true`
		err = db.QueryRowContext(ctxDisable, productionQuery, currentElementType.ElementTypeId, currentElementType.ProjectID).Scan(&productionCount)
		if err != nil {
			log.Printf("Error checking production status: %v", err)
			// Default to false if there's an error
			productionCount = 0
		}

		// Determine if any elements are in production
		inProduction := productionCount > 0

		// Send success response with production status
		c.JSON(http.StatusOK, gin.H{
			"message":    "Element type updated successfully and related elements status reset to Inprogress",
			"production": inProduction,
		})

		// Get project name for notification
		ctxNotif, cancelNotif := utils.GetFastQueryContext(c.Request.Context())
		defer cancelNotif()

		var projectName string
		err = db.QueryRowContext(ctxNotif, "SELECT name FROM project WHERE project_id = $1", currentElementType.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", currentElementType.ProjectID)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRowContext(ctxNotif, "SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the element type
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Element type updated: %s for project: %s", currentElementType.ElementTypeName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", currentElementType.ProjectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.ExecContext(ctxNotif, `
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, currentElementType.ProjectID,
			fmt.Sprintf("Element type updated: %s for project: %s", currentElementType.ElementTypeName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", currentElementType.ProjectID))

		activityLog := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "PUT",
			Description:  fmt.Sprintf("Update Element Type %d", currentElementType.ElementTypeId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    currentElementType.ProjectID,
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

// SaveBOMToRevisionTable saves all existing BOM records to the revision table before updates
// This ensures we always preserve the old BOM data in the revision table
func SaveBOMToRevisionTable(db *sql.DB, elementTypeID int, projectID int, userName string) (int, error) {
	// Fetch all current BOM data from element_type_bom table
	ctx, cancel := utils.GetDefaultQueryContext(context.Background())
	defer cancel()

	currentBOMQuery := `SELECT id, product_id, product_name, quantity, unit, rate FROM element_type_bom WHERE element_type_id = $1 AND project_id = $2 ORDER BY id`
	currentBOMRows, err := db.QueryContext(ctx, currentBOMQuery, elementTypeID, projectID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch current BOM data: %v", err)
	}
	defer currentBOMRows.Close()

	revisionCount := 0
	insertQuery := `INSERT INTO element_type_revision_bom
			(element_type_bom_id, element_type_id, project_id, product_id, product_name, quantity, units, rate, changed_at, changed_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING revision_id`

	// Insert current BOM data into revision table
	for currentBOMRows.Next() {
		var bomID int
		var productID int
		var productName string
		var quantity int
		var unit sql.NullString
		var rate sql.NullFloat64

		if err := currentBOMRows.Scan(&bomID, &productID, &productName, &quantity, &unit, &rate); err != nil {
			continue
		}

		// Prepare NULL-safe values for unit and rate
		var unitValue interface{}
		if unit.Valid {
			unitValue = unit.String
		}

		var rateValue interface{}
		if rate.Valid {
			rateValue = rate.Float64
		}

		var revisionID int
		if err := db.QueryRowContext(ctx, insertQuery,
			bomID, elementTypeID, projectID,
			productID, productName, quantity,
			unitValue, rateValue,
			time.Now(), userName).Scan(&revisionID); err != nil {
			return revisionCount, fmt.Errorf("failed to insert BOM revision for product %d (BOM ID: %d): %v", productID, bomID, err)
		}

		revisionCount++
	}

	// Check for errors from iterating rows
	if err = currentBOMRows.Err(); err != nil {
		return revisionCount, fmt.Errorf("error iterating BOM rows: %v", err)
	}

	return revisionCount, nil
}

// Add this helper function at the top (after imports):
func parseStagePath(stagePathStr string) []int {
	stagePathStr = strings.Trim(stagePathStr, "{}")
	var stagePath []int
	for _, s := range strings.Split(stagePathStr, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			stagePath = append(stagePath, n)
		}
	}
	return stagePath
}

// DeleteElements deletes individual elements from element table with hierarchy_id condition
func DeleteElements(c *gin.Context, elementData models.ElementInput) {
	db := storage.GetDB()

	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
		return
	}
	defer tx.Rollback()

	// Delete elements with element_type_id and hierarchy_id conditions
	// Limit deletion to the specified quantity (delete last/newest elements first)
	_, err = tx.Exec(`
		DELETE FROM element 
		WHERE element_type_id = $1 AND target_location = $2 
		AND id IN (
			SELECT id FROM element 
			WHERE element_type_id = $1 AND target_location = $2 
			ORDER BY id DESC 
			LIMIT $3
		)`, elementData.ElementTypeID, elementData.HierarchyId, elementData.Quantity)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete elements", "details": err.Error()})
		return
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
		return
	}

	log.Printf("Successfully deleted %d elements for element_type_id: %d, hierarchy_id: %d",
		elementData.Quantity, elementData.ElementTypeID, elementData.HierarchyId)
}

// DeleteElementType deletes an element type
// @Summary Delete element type
// @Description Delete an element type by ID
// @Tags ElementTypes
// @Accept json
// @Produce json
// @Param id path int true "Element Type ID"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/elementtype_delete/{id} [delete]
func DeleteElementType(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Element ID"})
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

	// Fetch element type info before deletion for notifications
	ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
	defer cancel()

	var projectID int
	var elementTypeName string
	err = db.QueryRowContext(ctx, "SELECT project_id, element_type_name FROM element_type WHERE element_type_id = $1", id).Scan(&projectID, &elementTypeName)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Element type not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element type info"})
		return
	}

	// Start a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// Delete the element type
	_, err = tx.ExecContext(ctx, "DELETE FROM element_type WHERE element_type_id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete element type"})
		return
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Element type deleted successfully"})

	// Get project name for notification
	ctxNotif, cancelNotif := utils.GetFastQueryContext(c.Request.Context())
	defer cancelNotif()

	var projectName string
	err = db.QueryRowContext(ctxNotif, "SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
	if err != nil {
		log.Printf("Failed to fetch project name: %v", err)
		projectName = fmt.Sprintf("Project %d", projectID)
	}

	// Fetch user_id from the session table
	var userID int
	err = db.QueryRowContext(ctxNotif, "SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
	if err != nil {
		log.Printf("Failed to fetch user_id for notification: %v", err)
	} else {
		// Send notification to the user who deleted the element type
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Element type deleted: %s from project: %s", elementTypeName, projectName),
			Status:    "unread",
			Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = db.ExecContext(ctxNotif, `
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		if err != nil {
			log.Printf("Failed to insert notification: %v", err)
		}
	}

	// Send notifications to all project members, clients, and end_clients
	sendProjectNotifications(db, projectID,
		fmt.Sprintf("Element type deleted: %s from project: %s", elementTypeName, projectName),
		fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID))

	activityLog := models.ActivityLog{
		EventContext: "Element Type",
		EventName:    "Delete",
		Description:  fmt.Sprintf("Delete Element Type %d", id),
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

func formatDateTime(input time.Time) string {
	// Convert to desired format: Date-20-Nov-2024 Time-09:23:23AM
	return input.Format("Date-02-Jan-2006 Time-03:04:05PM")
}

func FetchDrawingTypeName(drawingTypeID int) (string, error) {
	db := storage.GetDB()
	ctx, cancel := utils.GetFastQueryContext(context.Background())
	defer cancel()

	var drawingTypeName string
	err := db.QueryRowContext(ctx, "SELECT drawing_type_name FROM drawing_type WHERE drawing_type_id = $1", drawingTypeID).Scan(&drawingTypeName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch drawing type name: %v", err)
	}

	return drawingTypeName, nil
}

func FetchElementTypeAndDrawingByProjectID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Call the logic function, passing db and c
		if err := FetchElementType(c, db, projectID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch element types",
				"details": err.Error(),
			})
			return
		}
	}
}

// FetchElementType handles the actual DB query
func FetchElementType(c *gin.Context, db *sql.DB, projectID int) error {
	query := `
		SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width, created_by, created_at, update_at, 
			   element_type_id, project_id, element_type_version, total_count_element, density 
		FROM element_type WHERE project_id = $1 ORDER BY created_at DESC
	`

	// Execute the query
	ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
	defer cancel()

	rows, err := db.QueryContext(ctx, query, projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch element types: %v", err)})
		return nil
	}
	defer rows.Close()

	var elementTypes []models.ElementTypeR

	// Iterate through the rows
	for rows.Next() {
		var elementType models.ElementTypeR

		// Scan the row into variables
		var density float64
		err := rows.Scan(
			&elementType.ElementType, &elementType.ElementTypeName, &elementType.Thickness, &elementType.Length,
			&elementType.Height, &elementType.Volume, &elementType.Mass, &elementType.Area, &elementType.Width, &elementType.CreatedBy, &elementType.CreatedAt, &elementType.UpdatedAt,
			&elementType.ElementTypeId, &elementType.ProjectID, &elementType.ElementTypeVersion,
			&elementType.TotalCountElement, &density,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan element type: %v", err)})
			return nil
		}

		// Fetch hierarchy quantities from element_type_hierarchy_quantity table
		hierarchyQuery := `
            SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
                   p.id, p.project_id, p.name AS floor_name, p.description, p.parent_id, p.prefix,
                   parent.name as tower_name
            FROM element_type_hierarchy_quantity hq
            JOIN precast p ON hq.hierarchy_id = p.id
            LEFT JOIN precast parent ON p.parent_id = parent.id
            WHERE hq.element_type_id = $1
        `

		hierarchyRows, err := db.QueryContext(ctx, hierarchyQuery, elementType.ElementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err)})
			return nil
		}
		defer hierarchyRows.Close()

		var hierarchyResponseList []models.HierarchyResponce
		for hierarchyRows.Next() {
			var hierarchyData models.HierarchyResponce
			var hierarchyId, quantity int
			var namingConvention string

			err := hierarchyRows.Scan(
				&hierarchyId, &quantity, &namingConvention,
				&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
				&hierarchyData.Description, &hierarchyData.ParentID, &hierarchyData.Prefix,
				&hierarchyData.TowerName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan hierarchy data: %v", err)})
				return nil
			}

			hierarchyData.Quantity = quantity
			hierarchyData.NamingConvention = namingConvention
			hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
		}

		elementType.HierarchyResponce = hierarchyResponseList

		// Fetch associated drawings for each element type
		drawingsQuery := `
			SELECT drawing_id,  current_version, created_at, created_by, drawing_type_id, 
				  update_at, updated_by, comments, file, element_type_id
			FROM drawings
			WHERE element_type_id = $1 ORDER BY created_at DESC
		`
		drawingRows, err := db.QueryContext(ctx, drawingsQuery, elementType.ElementTypeId)
		if err != nil {
			return fmt.Errorf("failed to fetch drawings: %v", err)
		}
		defer drawingRows.Close()

		for drawingRows.Next() {
			var drawing models.DrawingsR
			err := drawingRows.Scan(
				&drawing.DrawingsId,
				&drawing.CurrentVersion,
				&drawing.CreatedAt,
				&drawing.CreatedBy,
				&drawing.DrawingTypeId,
				&drawing.UpdatedAt,
				&drawing.UpdatedBy,
				&drawing.Comments,
				&drawing.File,
				&drawing.ElementTypeID,
			)
			if err != nil {
				return fmt.Errorf("failed to scan drawing: %v", err)
			}

			// Fetch DrawingTypeName
			DrawingTypeName, err := FetchDrawingTypeName(drawing.DrawingTypeId)
			if err != nil {
				return fmt.Errorf("failed to fetch drawing type name for drawing: %v", err)
			}
			// Assign the fetched drawing type name
			drawing.DrawingTypeName = DrawingTypeName

			// Fetch associated drawing revisions for this drawing
			revisionsQuery := `
				 SELECT parent_drawing_id, version, created_at, created_by, drawing_type_id, 
                   comments, file, drawing_revision_id, element_type_id
            FROM drawings_revision
            WHERE parent_drawing_id = $1 ORDER BY created_at DESC`
			revisionRows, err := db.QueryContext(ctx, revisionsQuery, drawing.DrawingsId)
			if err != nil {
				return fmt.Errorf("failed to fetch drawing revisions: %v", err)
			}
			defer revisionRows.Close()

			for revisionRows.Next() {
				var revision models.DrawingsRevisionR
				err := revisionRows.Scan(
					&revision.ParentDrawingsId,

					&revision.Version,
					&revision.CreatedAt,
					&revision.CreatedBy,
					&revision.DrawingsTypeId,
					&revision.Comments,
					&revision.File,
					&revision.DrawingsRevisionId,
					&revision.ElementTypeID,
				)
				if err != nil {
					return fmt.Errorf("failed to scan drawing revision: %v", err)
				}
				RevisionDrawingTypeName, err := FetchDrawingTypeName(revision.DrawingsTypeId)
				if err != nil {
					return fmt.Errorf("failed to fetch drawing type name for revision: %v", err)
				}
				// Assign the fetched drawing type name to the revision
				revision.DrawingTypeName = RevisionDrawingTypeName
				// Fetch DrawingTypeName for the revision

				drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
			}
			if len(drawing.DrawingsRevision) == 0 {

				drawing.DrawingsRevision = []models.DrawingsRevisionR{}
			}

			elementType.Drawings = append(elementType.Drawings, drawing)
		}
		// Fetch BOM products from element_type_bom table
		productsQuery := `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`

		productRows, err := db.QueryContext(ctx, productsQuery, elementType.ElementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
			return nil
		}
		defer productRows.Close()

		var products []models.ProductR
		for productRows.Next() {
			var product models.ProductR
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := productRows.Scan(
				&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
				return nil
			}

			// Handle nullable fields if needed in the future
			_ = unit
			_ = rateFloat

			products = append(products, product)
		}

		elementType.Products = products
		elementTypes = append(elementTypes, elementType)
	}

	// Return the elementTypes as JSON
	c.JSON(http.StatusOK, elementTypes)
	return nil
}

//-----------------------------------------------------------------------------------------------------

// FetchElementTypeById retrieves an element type by its ID along with hierarchy, drawings, and product information

func GetElementTypeHandler(db *sql.DB, elementTypeId int, hierarchyID int, session models.Session, userName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Test database connection before proceeding
		if err := db.Ping(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database connection lost", "details": err.Error()})
			return
		}

		// Query to fetch element type details
		ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
		defer cancel()

		query := `
	SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width, created_by,
		   created_at, update_at, element_type_id, project_id, element_type_version,
		   total_count_element
	FROM element_type
	WHERE element_type_id = $1
`
		row := db.QueryRowContext(ctx, query, elementTypeId)

		var elementType models.ElementTypeR

		// Scan the element type details
		var volume, mass, area, width float64
		err := row.Scan(
			&elementType.ElementType,
			&elementType.ElementTypeName,
			&elementType.Thickness,
			&elementType.Length,
			&elementType.Height,
			&volume,
			&mass,
			&area,
			&width,
			&elementType.CreatedBy,
			&elementType.CreatedAt,
			&elementType.UpdatedAt,
			&elementType.ElementTypeId,
			&elementType.ProjectID,
			&elementType.ElementTypeVersion,
			&elementType.TotalCountElement,
		)
		elementType.Volume = volume
		elementType.Mass = mass
		elementType.Area = area
		elementType.Width = width
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusOK, gin.H{"message": "No element type found with the specified ID."})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch element type from the database.",
				"details": err.Error(),
			})
			return
		}

		// Fetch hierarchy quantities from element_type_hierarchy_quantity table
		hierarchyQuery := `
			SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
			       p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix,
			       parent.name as tower_name
			FROM element_type_hierarchy_quantity hq
			JOIN precast p ON hq.hierarchy_id = p.id
			LEFT JOIN precast parent ON p.parent_id = parent.id
			WHERE hq.element_type_id = $1 and hq.hierarchy_id = $2
		`

		hierarchyRows, err := db.QueryContext(ctx, hierarchyQuery, elementTypeId, hierarchyID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err),
			})
			return
		}
		defer hierarchyRows.Close()

		var hierarchyResponseList []models.HierarchyResponce
		for hierarchyRows.Next() {
			var hierarchyData models.HierarchyResponce
			var hierarchyId, quantity int
			var namingConvention string

			// Handle nullable DB fields safely
			var (
				description sql.NullString
				parentID    sql.NullInt64
				prefix      sql.NullString
				towerName   sql.NullString
			)

			err := hierarchyRows.Scan(
				&hierarchyId, &quantity, &namingConvention,
				&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
				&description, &parentID, &prefix,
				&towerName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("failed to scan hierarchy data: %v", err),
				})
				return
			}

			// Map nullable fields to concrete types with sane defaults
			if description.Valid {
				hierarchyData.Description = description.String
			} else {
				hierarchyData.Description = ""
			}
			if parentID.Valid {
				hierarchyData.ParentID = int(parentID.Int64)
			} else {
				hierarchyData.ParentID = 0
			}
			if prefix.Valid {
				hierarchyData.Prefix = prefix.String
			} else {
				hierarchyData.Prefix = ""
			}
			if towerName.Valid {
				tn := towerName.String
				hierarchyData.TowerName = &tn
			} else {
				hierarchyData.TowerName = nil
			}

			hierarchyData.Quantity = quantity
			hierarchyData.NamingConvention = namingConvention
			hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
		}

		elementType.HierarchyResponce = hierarchyResponseList

		// Fetch associated drawings for the element type
		if err := fetchDrawingsAndRevisions(db, &elementType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Fetch BOM products from element_type_bom table
		productsQuery := `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`

		productRows, err := db.QueryContext(ctx, productsQuery, elementType.ElementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
			return
		}
		defer productRows.Close()

		var products []models.ProductR
		for productRows.Next() {
			var product models.ProductR
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := productRows.Scan(
				&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
				return
			}

			// Handle nullable fields if needed in the future
			_ = unit
			_ = rateFloat

			products = append(products, product)
		}

		elementType.Products = products

		// Format createdAt and updateAt for elementType
		elementType.CreatedAtFormatted = formatDateTime(elementType.CreatedAt)
		elementType.UpdatedAtFormatted = formatDateTime(elementType.UpdatedAt)

		// Fetch tower and floor names from hierarchy data
		elementType.TowerName = ""
		elementType.FloorName = ""

		// Get tower and floor names from the hierarchy data
		if len(hierarchyResponseList) > 0 {
			// Use the first hierarchy item to get tower and floor names
			hierarchy := hierarchyResponseList[0]
			if hierarchy.TowerName != nil && *hierarchy.TowerName != "" {
				elementType.TowerName = *hierarchy.TowerName
			}
			// Floor name is the hierarchy name itself
			if hierarchy.Name != "" {
				elementType.FloorName = hierarchy.Name
			}
		}

		// Return the element type and associated data as JSON
		c.JSON(http.StatusOK, elementType)

		// Log the activity
		log := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Element Type Details for ID %d", elementTypeId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    elementType.ProjectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			// Log the error but don't fail the request
			fmt.Printf("Failed to log activity: %v\n", logErr)
		}
	}
}

// fetchDrawingsAndRevisions fetches drawings and revisions for the element type
func fetchDrawingsAndRevisions(db *sql.DB, elementType *models.ElementTypeR) error {
	elementType.Drawings = []models.DrawingsR{}

	ctx, cancel := utils.GetDefaultQueryContext(context.Background())
	defer cancel()

	query := `
	    SELECT drawing_id, current_version, created_at, created_by, updated_by, drawing_type_id, 
	           update_at, comments, file, element_type_id
	    FROM drawings
	    WHERE element_type_id = $1 ORDER BY created_at DESC
	`
	rows, err := db.QueryContext(ctx, query, elementType.ElementTypeId)
	if err != nil {
		return fmt.Errorf("failed to fetch drawings: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var drawing models.DrawingsR
		err := rows.Scan(
			&drawing.DrawingsId,
			&drawing.CurrentVersion,
			&drawing.CreatedAt,
			&drawing.CreatedBy,
			&drawing.UpdatedBy,
			&drawing.DrawingTypeId,
			&drawing.UpdatedAt,
			&drawing.Comments,
			&drawing.File,
			&drawing.ElementTypeID,
		)
		if err != nil {
			return fmt.Errorf("failed to scan drawing: %v", err)
		}

		DrawingTypeName, err := FetchDrawingTypeName(drawing.DrawingTypeId)
		if err != nil {
			return fmt.Errorf("failed to fetch drawing type name: %v", err)
		}
		drawing.DrawingTypeName = DrawingTypeName

		// Fetch revisions for this drawing
		drawing.DrawingsRevision, err = fetchDrawingRevisions(db, drawing.DrawingsId)
		if err != nil {
			return err
		}

		elementType.Drawings = append(elementType.Drawings, drawing)
	}
	return nil
}

// fetchDrawingRevisions fetches drawing revisions for a specific drawing
func fetchDrawingRevisions(db *sql.DB, parentDrawingId int) ([]models.DrawingsRevisionR, error) {
	var revisions []models.DrawingsRevisionR

	ctx, cancel := utils.GetDefaultQueryContext(context.Background())
	defer cancel()

	query := `
	    SELECT parent_drawing_id, version, created_at, created_by, drawing_type_id, 
	           comments, file, drawing_revision_id, element_type_id
	    FROM drawings_revision
	    WHERE parent_drawing_id = $1 ORDER BY created_at DESC
	`
	rows, err := db.QueryContext(ctx, query, parentDrawingId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch drawing revisions: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var revision models.DrawingsRevisionR
		err := rows.Scan(
			&revision.ParentDrawingsId,
			&revision.Version,
			&revision.CreatedAt,
			&revision.CreatedBy,
			&revision.DrawingsTypeId,
			&revision.Comments,
			&revision.File,
			&revision.DrawingsRevisionId,
			&revision.ElementTypeID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan drawing revision: %v", err)
		}

		RevisionDrawingTypeName, err := FetchDrawingTypeName(revision.DrawingsTypeId)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch drawing type name for revision: %v", err)
		}
		revision.DrawingTypeName = RevisionDrawingTypeName

		revisions = append(revisions, revision)
	}
	return revisions, nil
}

// FetchElementTypeByID godoc
// @Summary      Get element type by ID
// @Tags         element-types
// @Param        element_type_id  path      int  true  "Element type ID"
// @Success      200              {object}  object
// @Failure      400              {object}  models.ErrorResponse
// @Failure      401              {object}  models.ErrorResponse
// @Router       /api/elementtype_get/{element_type_id} [get]
func FetchElementTypeByID(c *gin.Context) {
	// Validate session
	sessionID := c.GetHeader("Authorization")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
		return
	}

	db := storage.GetDB()

	// Test database connection before proceeding
	if err := db.Ping(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database connection failed", "details": err.Error()})
		return
	}

	session, userName, err := GetSessionDetails(db, sessionID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
		return
	}

	ElementTypeId, err := strconv.Atoi(c.Param("element_type_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element type ID"})
		return
	}

	// Sanitize and parse hierarchy_id
	hierarchyIDStr := strings.TrimSpace(strings.Trim(c.Query("hierarchy_id"), `"`)) // strip quotes and whitespace
	var hierarchyID int
	if hierarchyIDStr != "" {
		hierarchyID, err = strconv.Atoi(hierarchyIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid hierarchy ID"})
			return
		}
	}

	GetElementTypeHandler(db, ElementTypeId, hierarchyID, session, userName)(c)
}

func GetProduct(elementTypeID int) (models.ElementTypeR, error) {
	db := storage.GetDB()
	ctx, cancel := utils.GetDefaultQueryContext(context.Background())
	defer cancel()

	var product models.ElementTypeR

	// SQL query to fetch all products for this element type
	query := `
        SELECT product_id, product_name, quantity, unit, rate
        FROM element_type_bom 
        WHERE element_type_id = $1`

	// Fetch data from the database
	rows, err := db.QueryContext(ctx, query, elementTypeID)
	if err != nil {
		return product, err
	}
	defer rows.Close()

	var products []models.ProductR
	for rows.Next() {
		var productItem models.ProductR
		var unit sql.NullString
		var rate sql.NullFloat64

		err := rows.Scan(
			&productItem.ProductID,
			&productItem.ProductName,
			&productItem.Quantity,
			&unit,
			&rate,
		)
		if err != nil {
			return product, err
		}

		// Handle nullable fields if needed in the future
		_ = unit
		_ = rate

		products = append(products, productItem)
	}

	product.Products = products
	return product, nil
}

func FetchElementTypes(c *gin.Context) {
	db := storage.GetDB()
	ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
	defer cancel()

	// Get all element types without project filter
	query := `
		SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width, created_by, created_at, update_at, 
			   element_type_id, project_id, element_type_version, total_count_element, density 
		FROM element_type ORDER BY created_at DESC
	`

	// Execute the query
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch element types: %v", err)})
		return
	}
	defer rows.Close()

	var elementTypes []models.ElementTypeR

	// Iterate through the rows
	for rows.Next() {
		var elementType models.ElementTypeR

		// Scan the row into variables
		var density float64
		err := rows.Scan(
			&elementType.ElementType, &elementType.ElementTypeName, &elementType.Thickness, &elementType.Length,
			&elementType.Height, &elementType.Volume, &elementType.Mass, &elementType.Area, &elementType.Width, &elementType.CreatedBy, &elementType.CreatedAt, &elementType.UpdatedAt,
			&elementType.ElementTypeId, &elementType.ProjectID, &elementType.ElementTypeVersion,
			&elementType.TotalCountElement, &density,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan element type: %v", err)})
			return
		}

		// Fetch hierarchy quantities from element_type_hierarchy_quantity table
		hierarchyQuery := `
			SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
			       p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix,
			       parent.name as tower_name
			FROM element_type_hierarchy_quantity hq
			JOIN precast p ON hq.hierarchy_id = p.id
			LEFT JOIN precast parent ON p.parent_id = parent.id
			WHERE hq.element_type_id = $1
		`

		hierarchyRows, err := db.QueryContext(ctx, hierarchyQuery, elementType.ElementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err)})
			return
		}
		defer hierarchyRows.Close()

		var hierarchyResponseList []models.HierarchyResponce
		for hierarchyRows.Next() {
			var hierarchyData models.HierarchyResponce
			var hierarchyId, quantity int
			var namingConvention string

			err := hierarchyRows.Scan(
				&hierarchyId, &quantity, &namingConvention,
				&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
				&hierarchyData.Description, &hierarchyData.ParentID, &hierarchyData.Prefix,
				&hierarchyData.TowerName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan hierarchy data: %v", err)})
				return
			}

			hierarchyData.Quantity = quantity
			hierarchyData.NamingConvention = namingConvention
			hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
		}

		elementType.HierarchyResponce = hierarchyResponseList

		// Fetch BOM products from element_type_bom table
		productsQuery := `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`

		productRows, err := db.QueryContext(ctx, productsQuery, elementType.ElementTypeId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
			return
		}
		defer productRows.Close()

		var products []models.ProductR
		for productRows.Next() {
			var product models.ProductR
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := productRows.Scan(
				&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
				return
			}

			// Handle nullable fields if needed in the future
			_ = unit
			_ = rateFloat

			products = append(products, product)
		}

		elementType.Products = products
		elementTypes = append(elementTypes, elementType)
	}

	// Return the elementTypes as JSON
	c.JSON(http.StatusOK, elementTypes)
}

// FetchElementTypesName godoc
// @Summary      Get element type names
// @Tags         element-types
// @Success      200  {array}  models.ElementTypeName
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/elementtype_name [get]
func FetchElementTypesName(c *gin.Context) {
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

	// Query to fetch all element types
	ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
	defer cancel()

	query := `
		SELECT element_type, element_type_name,
		       element_type_id
		FROM element_type
	`

	// Execute the query
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch element types: %v", err)})
		return
	}
	defer rows.Close()

	var elementtypes []models.ElementTypeName

	// Iterate through the result set
	for rows.Next() {
		var elementType models.ElementTypeName
		err := rows.Scan(
			&elementType.ElementType,
			&elementType.ElementTypeName,
			&elementType.ElementTypeID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan element type: %v", err)})
			return
		}

		// Append the element type to the slice
		elementtypes = append(elementtypes, elementType)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error iterating over rows: %v", err)})
		return
	}

	// Return the result as JSON
	c.JSON(http.StatusOK, elementtypes)

	log := models.ActivityLog{
		EventContext: "Element Type",
		EventName:    "Get",
		Description:  "Get Element Type Names",
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

// CreateElementTypeName godoc
// @Summary      Create element type name
// @Tags         element-types
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Element type name"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/element_type_name_create [post]
func CreateElementTypeName(c *gin.Context) {
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

	var element models.ElementTypename

	// Bind JSON input for a single record
	if err := c.ShouldBindJSON(&element); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Prepare and execute the insert query
	ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
	defer cancel()

	query := `INSERT INTO element_type_name (id, "Element_type_name", project_id) 
                  VALUES ($1, $2, $3) RETURNING id`
	var insertedID int
	err = db.QueryRowContext(ctx, query, element.ID, element.ElementTypeName, element.ProjectID).Scan(&insertedID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return the inserted ID
	c.JSON(http.StatusOK, gin.H{
		"message":     "ElementTypeName inserted successfully",
		"inserted_id": insertedID,
	})

	log := models.ActivityLog{
		EventContext: "Element Type",
		EventName:    "Create",
		Description:  "Create Element Type Name",
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
func GetAllElementTypeNames(c *gin.Context) {
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

	// Query to fetch all element type names
	ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
	defer cancel()

	query := `SELECT id, element_type, project_id FROM element_type_name`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element type names: " + err.Error()})
		return
	}
	defer rows.Close()

	var elementTypeNames []models.ElementTypename
	for rows.Next() {
		var elementTypeName models.ElementTypename
		err := rows.Scan(
			&elementTypeName.ID,
			&elementTypeName.ElementTypeName,
			&elementTypeName.ProjectID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element type names: " + err.Error()})
			return
		}
		elementTypeNames = append(elementTypeNames, elementTypeName)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
		return
	}

	// Return the result as a JSON response
	c.JSON(http.StatusOK, gin.H{"data": elementTypeNames})

	log := models.ActivityLog{
		EventContext: "Element Type",
		EventName:    "Get",
		Description:  "Get All Element Type Names",
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

// GetAllelementType godoc
// @Summary      Fetch all element type names
// @Tags         element-types
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/fetch_element_type_name [get]
func GetAllelementType(c *gin.Context) {
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

	ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
	defer cancel()

	rows, err := db.QueryContext(ctx, `
	SELECT 
	id, element_type, project_id FROM element_type_name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element: " + err.Error()})
		return
	}
	defer rows.Close()

	var getDrawingType []models.ElementTypename
	for rows.Next() {
		var drawingType models.ElementTypename
		err := rows.Scan(
			&drawingType.ID,
			&drawingType.ElementTypeName,
			&drawingType.ProjectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan elements: " + err.Error()})
			return
		}

		getDrawingType = append(getDrawingType, drawingType)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, getDrawingType)

	log := models.ActivityLog{
		EventContext: "Element Type",
		EventName:    "Get",
		Description:  "Get All Element Type",
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

// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// GetElementTypeQuantity retrieves element type quantities by project
// @Summary Get element type quantities
// @Description Retrieve element type quantities for a specific project
// @Tags ElementTypes
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {object} models.Building
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_element_type_quantity/{project_id} [get]
func GetElementTypeQuantity(db *sql.DB) gin.HandlerFunc {
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

		ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancel()

		rows, err := db.QueryContext(ctx, `
			SELECT 
				ethq.id, 
				COALESCE(towerPrecast.name, hierarchyPrecast.name, '') AS tower_name, 
				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN 'common'
					ELSE COALESCE(NULLIF(hierarchyPrecast.name, ''), 'common')
				END AS floor_name, 
				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN 0
					ELSE ethq.hierarchy_id
				END AS floor_id, 
				COALESCE(ethq.element_type_name, et.element_type_name) AS element_type_name, 
				COALESCE(ethq.element_type, et.element_type) AS element_type, 
				ethq.element_type_id, 
				ethq.quantity AS total_quantity,
				COALESCE(ethq.left_quantity, 0) AS left_quantity
			FROM element_type_hierarchy_quantity ethq
			JOIN element_type et ON ethq.element_type_id = et.element_type_id
			LEFT JOIN precast hierarchyPrecast ON ethq.hierarchy_id = hierarchyPrecast.id
			LEFT JOIN precast towerPrecast ON hierarchyPrecast.parent_id = towerPrecast.id
			WHERE COALESCE(ethq.project_id, et.project_id) = $1
			ORDER BY 
				CASE WHEN COALESCE(towerPrecast.name, hierarchyPrecast.name, '') = '' THEN 1 ELSE 0 END,
				COALESCE(towerPrecast.name, hierarchyPrecast.name, ''), 
				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN 1
					WHEN COALESCE(NULLIF(hierarchyPrecast.name, ''), 'common') = 'common' THEN 1
					ELSE 0
				END,
				CASE 
					WHEN hierarchyPrecast.parent_id IS NULL THEN 'common'
					ELSE COALESCE(NULLIF(hierarchyPrecast.name, ''), 'common')
				END, 
				COALESCE(ethq.element_type_name, et.element_type_name);
		`, projectID)

		if err != nil {
			log.Printf("Error fetching element type quantity data for project_id %d: %v", projectID, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database query failed",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		building := make(models.Building)

		for rows.Next() {
			var id, floorID, elementTypeID, totalQuantity, leftQuantity int
			var towerName, floorName, elementTypeName, elementType sql.NullString

			if err := rows.Scan(
				&id, &towerName, &floorName, &floorID, &elementTypeName, &elementType,
				&elementTypeID, &totalQuantity, &leftQuantity,
			); err != nil {
				log.Println("Error scanning row:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading data"})
				return
			}

			// Convert NULL values to empty strings
			tower := towerName.String
			floor := floorName.String
			elemTypeName := elementTypeName.String
			elemType := elementType.String

			// **Skip this item if totalQuantity == 0**
			if totalQuantity == 0 {
				continue
			}

			// **Skip if tower is empty (element must belong to a tower)**
			if tower == "" {
				continue
			}
			// Create an Item
			item := models.Item{
				FloorID:         floorID,
				ElementType:     elemType,
				ElementTypeId:   elementTypeID,
				ElementTypeName: elemTypeName,
				TotalQuantity:   totalQuantity,
				BalanceQuantity: totalQuantity - leftQuantity, // Since left_quantity is 0, balance equals total
			}

			// Initialize maps
			if _, ok := building[tower]; !ok {
				building[tower] = make(models.Tower)
			}
			if _, ok := building[tower][floor]; !ok {
				building[tower][floor] = make(models.Floor)
			}
			if _, ok := building[tower][floor][elemType]; !ok {
				building[tower][floor][elemType] = []models.Item{}
			}

			// Append item
			building[tower][floor][elemType] = append(building[tower][floor][elemType], item)
		}

		c.JSON(http.StatusOK, building)

		log := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "Get",
			Description:  "Get Element Type Quantity",
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

// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------------------------------------------------------------------
// ELEMENT TYPE VERSION
// CreateElementTypeVersion creates multiple element type versions in the database
func CreateElementTypeVersion(db *sql.DB) gin.HandlerFunc {
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

		var elementTypes []models.ElementTypeVersion

		// Bind JSON input (array of element types with project_id and name)
		if err := c.ShouldBindJSON(&elementTypes); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON input"})
			return
		}

		// Prepare the SQL statement for batch insert
		var values []string
		var args []interface{}

		// Construct the values and arguments for the insert query
		for i, elementType := range elementTypes {
			values = append(values, "($"+strconv.Itoa(i*2+1)+", $"+strconv.Itoa(i*2+2)+")")
			args = append(args, elementType.ProjectID, elementType.Name)
		}

		// Create the query string dynamically based on the number of element types
		query := "INSERT INTO element_types (project_id, name) VALUES " + strings.Join(values, ", ") + " RETURNING id"

		// Insert into the database
		ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancel()

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create element types", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Extract IDs for created element types
		var createdElementTypes []models.ElementTypeVersion
		for i := range elementTypes {
			var createdElementType models.ElementTypeVersion
			if rows.Next() {
				err := rows.Scan(&createdElementType.ID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element type ID", "details": err.Error()})
					return
				}
				createdElementType.Name = elementTypes[i].Name
				createdElementType.ProjectID = elementTypes[i].ProjectID
				createdElementTypes = append(createdElementTypes, createdElementType)
			}
		}

		// Respond with the created element types
		c.JSON(http.StatusCreated, gin.H{
			"message":       "Element types created successfully",
			"element_types": createdElementTypes,
		})

		log := models.ActivityLog{
			EventContext: "Element Type Version",
			EventName:    "Create",
			Description:  "Create Element Type Version",
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

// GetAllElementTypes retrieves all element types from the database
func GetAllElementTypeVersion(db *sql.DB) gin.HandlerFunc {
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
		ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancel()

		query := `SELECT id, name FROM element_types WHERE project_id = $1 ORDER BY id`

		rows, err := db.QueryContext(ctx, query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve element types", "details": err.Error()})
			return
		}
		defer rows.Close()

		var elementTypes []models.ElementTypeVersion

		for rows.Next() {
			var elementType models.ElementTypeVersion
			if err := rows.Scan(&elementType.ID, &elementType.Name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning element types", "details": err.Error()})
				return
			}
			elementTypes = append(elementTypes, elementType)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving element types", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"element_types": elementTypes})

		log := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "Get",
			Description:  "Get Element Type Version",
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

// GetElementTypesByProjectWith retrieves element types by project with pagination
// @Summary Get element types by project
// @Description Retrieve element types for a specific project with pagination
// @Tags ElementTypes
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(10)
// @Success 200 {object} models.ElementTypePaginationResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/elementtype_fetch/{project_id} [get]
func GetElementTypesByProjectWith(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}

		// Get query parameters - essential ones
		searchTerm := c.Query("search")
		hierarchyID := c.Query("hierarchy_id")
		elementType := c.Query("element_type")
		elementTypeName := c.Query("element_type_name")
		// Always fetch drawings with element types
		// Note: These parameters are for reference/filtering purposes
		// They can be used to filter results based on element counts in different states
		// _totalElements := c.Query("total_elements") // Total elements count filter
		// _inProduction := c.Query("in_production")   // Elements in production filter
		// _inStockyard := c.Query("in_stockyard")     // Elements in stockyard filter
		// _inDispatch := c.Query("in_dispatch")       // Elements in dispatch filter
		// _inErection := c.Query("in_erection")       // Elements in erection filter
		// _inRequest := c.Query("in_request")         // Elements in request filter

		// Get pagination parameters from query string
		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("page_size", "10")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 10
		}

		// Check if stage filtering is applied - if so, disable pagination
		stages := c.QueryArray("stage")
		hasStageFilter := len(stages) > 0

		// Calculate offset - use 0 if stage filtering is applied
		offset := 0
		if !hasStageFilter {
			offset = (page - 1) * pageSize
		}

		// Build WHERE conditions - only project_id and search
		var conditions []string
		var args []interface{}
		argIndex := 1

		// Base condition for project_id
		conditions = append(conditions, fmt.Sprintf("et.project_id = $%d", argIndex))
		args = append(args, projectID)
		argIndex++

		// Add search condition if provided
		if searchTerm != "" {
			conditions = append(conditions, fmt.Sprintf("(et.element_type ILIKE $%d OR et.element_type_name ILIKE $%d)", argIndex, argIndex))
			args = append(args, "%"+searchTerm+"%")
			argIndex++
		}

		// Add element type filter
		if elementType != "" {
			conditions = append(conditions, fmt.Sprintf("et.element_type ILIKE $%d", argIndex))
			args = append(args, "%"+elementType+"%")
			argIndex++
		}

		// Add element type name filter
		if elementTypeName != "" {
			conditions = append(conditions, fmt.Sprintf("et.element_type_name ILIKE $%d", argIndex))
			args = append(args, "%"+elementTypeName+"%")
			argIndex++
		}

		// Add hierarchy ID filter - will be applied in the main query CTE
		var hierarchyIDInt int
		if hierarchyID != "" {
			var err error
			hierarchyIDInt, err = strconv.Atoi(hierarchyID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hierarchy_id"})
				return
			}
			log.Printf("Filtering by hierarchy_id: %d", hierarchyIDInt)
		}

		// Build WHERE clause
		whereClause := ""
		if len(conditions) > 0 {
			whereClause = "WHERE " + strings.Join(conditions, " AND ")
		}

		// Debug: Check what hierarchy IDs exist for this project
		if hierarchyID != "" {
			debugQuery := `
			SELECT DISTINCT ethq.hierarchy_id
			FROM element_type et
			JOIN element_type_hierarchy_quantity ethq ON et.element_type_id = ethq.element_type_id
			WHERE et.project_id = $1
			ORDER BY hierarchy_id`

			ctxDebug, cancelDebug := utils.GetFastQueryContext(c.Request.Context())
			defer cancelDebug()

			debugRows, err := db.QueryContext(ctxDebug, debugQuery, projectID)
			if err != nil {
				log.Printf("Debug query error: %v", err)
			} else {
				defer debugRows.Close()
				var availableHierarchyIDs []int
				for debugRows.Next() {
					var hid int
					if err := debugRows.Scan(&hid); err == nil {
						availableHierarchyIDs = append(availableHierarchyIDs, hid)
					}
				}
				log.Printf("Available hierarchy IDs for project %d: %v", projectID, availableHierarchyIDs)
			}
		}

		// Debug: Check element counts by floor to understand the data structure
		debugElementQuery := `
		SELECT 
			e.element_type_id,
			e.target_location,
			COUNT(e.id) as element_count,
			COUNT(cp.element_id) as production_count
		FROM element e
		LEFT JOIN complete_production cp ON cp.element_id = e.id AND cp.status IS NULL
		WHERE e.project_id = $1
		GROUP BY e.element_type_id, e.target_location
		ORDER BY e.element_type_id, e.target_location
		LIMIT 10`

		ctxDebugElem, cancelDebugElem := utils.GetFastQueryContext(c.Request.Context())
		defer cancelDebugElem()

		debugElementRows, err := db.QueryContext(ctxDebugElem, debugElementQuery, projectID)
		if err == nil {
			defer debugElementRows.Close()
			log.Printf("DEBUG: Element counts by floor:")
			for debugElementRows.Next() {
				var elementTypeID, targetLocation, elementCount, productionCount int
				if err := debugElementRows.Scan(&elementTypeID, &targetLocation, &elementCount, &productionCount); err == nil {
					log.Printf("DEBUG: ElementTypeID: %d, Floor: %d, Elements: %d, Production: %d",
						elementTypeID, targetLocation, elementCount, productionCount)
				}
			}
		}

		// // Debug: Check for duplicate production records
		// debugDuplicateQuery := `
		// SELECT
		// 	cp.element_id,
		// 	COUNT(*) as duplicate_count
		// FROM complete_production cp
		// JOIN element e ON e.id = cp.element_id
		// WHERE e.project_id = $1 AND cp.status IS NULL
		// GROUP BY cp.element_id
		// HAVING COUNT(*) > 1
		// LIMIT 10`

		// debugDuplicateRows, err := db.Query(debugDuplicateQuery, projectID)
		// if err == nil {
		// 	defer debugDuplicateRows.Close()
		// 	log.Printf("DEBUG: Duplicate production records:")
		// 	for debugDuplicateRows.Next() {
		// 		var elementID, duplicateCount int
		// 		if err := debugDuplicateRows.Scan(&elementID, &duplicateCount); err == nil {
		// 			log.Printf("DEBUG: ElementID: %d has %d production records", elementID, duplicateCount)
		// 		}
		// 	}
		// }

		// Ultra-fast simplified count query
		countQuery := `
		SELECT COUNT(*)
		FROM element_type et
		JOIN element_type_hierarchy_quantity ethq ON et.element_type_id = ethq.element_type_id
		WHERE et.project_id = $1`

		// Add WHERE conditions to count query
		if whereClause != "" {
			filterClause := strings.TrimPrefix(whereClause, "WHERE ")
			countQuery += " AND " + filterClause
		}

		// Add hierarchy ID filter to count query
		if hierarchyID != "" {
			countQuery += fmt.Sprintf(" AND ethq.hierarchy_id = %d", hierarchyIDInt)
		}

		ctxCount, cancelCount := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelCount()

		var totalRecords int
		err = db.QueryRowContext(ctxCount, countQuery, args...).Scan(&totalRecords)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count records", "details": err.Error()})
			return
		}
		// Calculate total pages
		totalPages := (totalRecords + pageSize - 1) / pageSize

		// Fast query with efficient count calculations
		query := `
WITH element_counts AS (
    SELECT 
        e.element_type_id,
        e.target_location AS hierarchy_id,
        COUNT(DISTINCT e.id) AS total_elements,

        -- Production count (only if production row exists and status is NULL) - FLOOR SPECIFIC
        COUNT(DISTINCT 
            CASE 
                WHEN cp.element_id IS NOT NULL 
                     AND cp.status IS NULL 
                THEN cp.element_id 
                ELSE NULL
            END
        ) AS production_count,

        -- Stockyard count (only when precast_stock row exists and stockyard is FALSE) - FLOOR SPECIFIC
        COUNT(DISTINCT 
            CASE 
                WHEN ps.element_id IS NOT NULL 
                     AND ps.stockyard = TRUE 
					 AND ps.order_by_erection = FALSE 
                THEN ps.element_id 
                ELSE NULL
            END
        ) AS stockyard_count,

        -- Dispatch count (only when precast_stock row exists and dispatch_status is FALSE) - FLOOR SPECIFIC
        COUNT(DISTINCT 
            CASE 
                WHEN ps.element_id IS NOT NULL 
                     AND ps.dispatch_status = True 
					 AND ps.recieve_in_erection = FALSE 
                THEN ps.element_id 
                ELSE NULL
            END
        ) AS dispatch_count,

        -- Erection count (only when precast_stock row exists and erected is FALSE) - FLOOR SPECIFIC
        COUNT(DISTINCT 
            CASE 
                WHEN ps.element_id IS NOT NULL 
                     AND ps.erected = true 
					 AND ps.recieve_in_erection = TRUE 
                THEN ps.element_id 
                ELSE NULL
            END
        ) AS erection_count,

        -- In request count (only when precast_stock row exists and matches conditions) - FLOOR SPECIFIC
        COUNT(DISTINCT 
            CASE 
                WHEN ps.element_id IS NOT NULL
                     AND ps.dispatch_status = FALSE 
                     AND ps.order_by_erection = TRUE 
                     AND ps.recieve_in_erection = FALSE 
					 AND ps.erected = false
                THEN ps.element_id 
                ELSE NULL
            END
        ) AS in_request_count

    FROM element e
    LEFT JOIN complete_production cp 
        ON cp.element_id = e.id 
       AND cp.status IS NULL
    LEFT JOIN precast_stock ps 
        ON ps.element_id = e.id
    WHERE e.project_id = $1
    GROUP BY e.element_type_id, e.target_location
),
paginated_data AS (
    SELECT 
        et.element_type_id,
        et.element_type,
        et.element_type_name,
        et.thickness,
        et.length,
        et.height,
	et.volume, et.mass, et.area, et.width,
        et.project_id,
        et.element_type_version,
        ethq.quantity,
        ethq.hierarchy_id,
        CASE 
            WHEN p.parent_id IS NULL THEN ''
            ELSE COALESCE(p.name, '')
        END AS floor_name,
        CASE 
            WHEN p.parent_id IS NULL THEN COALESCE(p.name, '')
            ELSE COALESCE(tower.name, '')
        END AS tower_name,
        COALESCE(ethq.naming_convention, '') AS naming_convention,
        COALESCE(ec.total_elements, 0) AS total_elements,
        COALESCE(ec.production_count, 0) AS production_count,
        COALESCE(ec.stockyard_count, 0) AS stockyard_count,
        COALESCE(ec.dispatch_count, 0) AS dispatch_count,
        COALESCE(ec.erection_count, 0) AS erection_count,
        COALESCE(ec.in_request_count, 0) AS in_request_count,
        ROW_NUMBER() OVER (ORDER BY et.element_type_id, ethq.hierarchy_id) as rn
    FROM element_type et
    JOIN element_type_hierarchy_quantity ethq 
        ON et.element_type_id = ethq.element_type_id
    LEFT JOIN precast p 
        ON ethq.hierarchy_id = p.id
    LEFT JOIN precast tower 
        ON p.parent_id = tower.id
    LEFT JOIN element_counts ec 
        ON ec.element_type_id = et.element_type_id 
       AND ec.hierarchy_id = ethq.hierarchy_id
    WHERE et.project_id = $1`

		// Add WHERE conditions to paginated_data CTE
		if whereClause != "" {
			filterClause := strings.TrimPrefix(whereClause, "WHERE ")
			query += " AND " + filterClause
		}

		// Add hierarchy ID filter to paginated_data CTE
		if hierarchyID != "" {
			query += fmt.Sprintf(" AND ethq.hierarchy_id = %d", hierarchyIDInt)
		}

		query += fmt.Sprintf(`
)
SELECT * FROM paginated_data
WHERE rn > $%d AND rn <= $%d
ORDER BY element_type_id, hierarchy_id`, argIndex, argIndex+1)
		args = append(args, offset, offset+pageSize)

		// Add context timeout to prevent hanging queries
		ctx, cancel := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancel()

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Query execution failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		var results []models.ElementTypeWithHierarchyResponse

		for rows.Next() {
			var r models.ElementTypeWithHierarchyResponse
			var rn int // ROW_NUMBER column for pagination

			var totalElements int // Temporary variable for total_elements
			err := rows.Scan(
				&r.ElementTypeID,
				&r.ElementType,
				&r.ElementTypeName,
				&r.Thickness,
				&r.Length,
				&r.Height,
				&r.Volume,
				&r.Mass,
				&r.Area,
				&r.Width,
				&r.ProjectID,
				&r.ElementTypeVersion,
				&r.Quantity,
				&r.HierarchyID,
				&r.FloorName,
				&r.TowerName,
				&r.NamingConvention,
				&totalElements, // Store in temporary variable
				&r.ProductionCount,
				&r.StockyardCount,
				&r.DispatchCount,
				&r.ErectionCount,
				&r.InRequestCount,
				&rn, // ROW_NUMBER column
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan row", "details": err.Error()})
				return
			}

			// Initialize empty drawings array since we removed drawing functionality
			r.Drawings = make([]models.DrawingsR, 0)

			results = append(results, r)
		}

		// Check for any errors encountered during iteration
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error during rows iteration",
				"details": err.Error(),
			})
			return
		}

		// === Apply UI filters AFTER computing counts, BEFORE pagination ===
		// Get stage filters from query parameters (already declared above)

		var filtered []models.ElementTypeWithHierarchyResponse
		for _, item := range results {

			// Stage filtering - only show elements with count > 0 for specified stages
			if hasStageFilter {
				stageMatch := false
				for _, stage := range stages {
					switch stage {
					case "production":
						if item.ProductionCount > 0 {
							stageMatch = true
						}
					case "stockyard":
						if item.StockyardCount > 0 {
							stageMatch = true
						}
					case "dispatch":
						if item.DispatchCount > 0 {
							stageMatch = true
						}
					case "erection":
						if item.ErectionCount > 0 {
							stageMatch = true
						}
					case "request":
						if item.InRequestCount > 0 {
							stageMatch = true
						}
					}
				}
				if !stageMatch {
					continue
				}
			}

			filtered = append(filtered, item)
		}

		uiFilterApplied := hasStageFilter
		finalResults := results
		finalTotalRecords := totalRecords
		finalTotalPages := totalPages
		if uiFilterApplied {
			// When stage filtering is applied, return all filtered results without pagination
			finalResults = filtered
			finalTotalRecords = len(filtered)
			finalTotalPages = 1
		}

		// Ensure finalResults is never nil - use empty array instead
		if finalResults == nil {
			finalResults = make([]models.ElementTypeWithHierarchyResponse, 0)
		}

		// Create pagination response
		pagination := models.Pagination{
			CurrentPage:  page,
			PageSize:     pageSize,
			TotalRecords: finalTotalRecords,
			TotalPages:   finalTotalPages,
			HasNext:      page < finalTotalPages,
			HasPrev:      page > 1,
		}

		response := models.PaginatedResponse{
			Data:       finalResults,
			Pagination: pagination,
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Element Type",
			EventName:    "Get",
			Description:  "Get Element Type",
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

// GetElementDetailsByTypeAndLocation godoc
// @Summary      Get element details by type and location
// @Tags         element-types
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "element_type_id, target_location, project_id"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/element_details [post]
func GetElementDetailsByTypeAndLocation(db *sql.DB) gin.HandlerFunc {
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

		// Define the request structure
		var req struct {
			ElementTypeID  int `json:"element_type_id" binding:"required"`
			TargetLocation int `json:"target_location"`
			ProjectID      int `json:"project_id" binding:"required"`
		}

		// Bind JSON input
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid input",
				"details": err.Error(),
			})
			return
		}

		// Initialize element with empty arrays
		element := models.ElementTypeInterface{
			DrawingType: []models.DrawingType{},
			BOM:         []models.BOMItem{},
			Stages:      []models.StageDetails{},
		}

		// Step 1: Get base element_type data
		var countQuery string
		var countArgs []interface{}

		if req.TargetLocation > 0 {
			countQuery = `(SELECT COUNT(*) FROM element WHERE element_type_id = $1 AND target_location = $2) as total_quantity`
			countArgs = []interface{}{req.ElementTypeID, req.TargetLocation}
		} else {
			countQuery = `(SELECT COUNT(*) FROM element WHERE element_type_id = $1) as total_quantity`
			countArgs = []interface{}{req.ElementTypeID}
		}

		query := fmt.Sprintf(`
			SELECT 
				et.element_type_id,
				et.element_type,
				et.element_type_version,
				et.thickness,
				et.length,
				et.height,
				et.volume, et.mass, et.area, et.width,
				%s
			FROM element_type et
			WHERE et.element_type_id = $1`, countQuery)

		ctx, cancel := utils.GetFastQueryContext(c.Request.Context())
		defer cancel()

		var volume, mass, area, width float64
		err = db.QueryRowContext(ctx, query, countArgs...).Scan(
			&element.ID,
			&element.ElementType,
			&element.ElementTypeVersion,
			&element.Thickness,
			&element.Length,
			&element.Height,
			&volume, &mass, &area, &width,
			&element.TotalQuantity,
		)
		element.Volume = volume
		element.Mass = mass
		element.Area = area
		element.Width = width
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "Element type not found",
					"details": fmt.Sprintf("No element type found with ID %d", req.ElementTypeID),
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database error",
				"details": err.Error(),
			})
			return
		}

		// Step 2: Fetch Drawings & Revisions
		ctxDrawings, cancelDrawings := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelDrawings()

		drawingRows, err := db.QueryContext(ctxDrawings, `
            SELECT 
                d.drawing_id,
                d.current_version,
                d.file,
                dt.drawing_type_name,
				d.created_at,
				d.update_at
            FROM drawings d
            JOIN drawing_type dt ON d.drawing_type_id = dt.drawing_type_id
            WHERE d.element_type_id = $1`, req.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch drawings",
				"details": err.Error(),
			})
			return
		}
		defer drawingRows.Close()

		for drawingRows.Next() {
			var drawing models.DrawingType
			var filePath string
			var drawingID int
			if err := drawingRows.Scan(&drawingID, &drawing.Version, &filePath, &drawing.Name, &drawing.CreatedAt, &drawing.UpdatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to scan drawing",
					"details": err.Error(),
				})
				return
			}
			drawing.FilePath = filePath
			drawing.Revision = []models.DrawingRevision{} // Initialize empty array for revisions

			// Fetch revisions for this drawing
			revisionRows, err := db.QueryContext(ctxDrawings, `
                SELECT 
                    dr.version,
                    dr.file,
                    dt.drawing_type_name
                FROM drawings_revision dr
                JOIN drawing_type dt ON dr.drawing_type_id = dt.drawing_type_id
                WHERE dr.parent_drawing_id = $1`, drawingID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch revisions",
					"details": err.Error(),
				})
				return
			}
			defer revisionRows.Close()

			for revisionRows.Next() {
				var revision models.DrawingRevision
				var revFilePath string
				if err := revisionRows.Scan(&revision.Version, &revFilePath, &revision.Name); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error":   "Failed to scan revision",
						"details": err.Error(),
					})
					return
				}
				revision.FilePath = revFilePath
				drawing.Revision = append(drawing.Revision, revision)
			}
			element.DrawingType = append(element.DrawingType, drawing)
		}

		// Step 3: Fetch BOM Products from element_type_bom table
		bomRows, err := db.QueryContext(ctxDrawings, `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`, req.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch BOM products",
				"details": err.Error(),
			})
			return
		}
		defer bomRows.Close()

		// Initialize BOM array
		element.BOM = make([]models.BOMItem, 0)

		for bomRows.Next() {
			var productID int
			var productName string
			var quantity float64
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := bomRows.Scan(
				&productID, &productName, &quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to scan BOM product",
					"details": err.Error(),
				})
				return
			}

			// Handle nullable fields if needed in the future
			_ = unit
			_ = rateFloat

			// Convert to BOMItem
			element.BOM = append(element.BOM, models.BOMItem{
				MaterialID: productID,
				Name:       productName,
				Quantity:   quantity,
				Unit:       unit.String,
				Rate:       rateFloat.Float64,
			})
		}

		// Step 4: Fetch stages
		// First get all stages for the project
		stageRows, err := db.QueryContext(ctxDrawings, `
            SELECT 
                s.id,
                s.name
            FROM project_stages s
            WHERE s.project_id = $1
            ORDER BY s.id`, req.ProjectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch stages",
				"details": err.Error(),
			})
			return
		}
		defer stageRows.Close()

		// Initialize stages array
		element.Stages = make([]models.StageDetails, 0)

		for stageRows.Next() {
			var stage models.StageDetails
			if err := stageRows.Scan(&stage.StageID, &stage.StageName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to scan stage",
					"details": err.Error(),
				})
				return
			}

			// Handle different stage types
			var qcCount, productionCount int
			var statusField string

			switch stage.StageName {
			case "Mesh & Mold":
				statusField = "mesh_mold_status"
			case "Reinforcement":
				statusField = "reinforcement_status"
			default:
				statusField = "status"
			}

			// Count QC activities (where status is 'completed')
			var qcQuery string
			var qcArgs []interface{}
			if req.TargetLocation > 0 {
				qcQuery = fmt.Sprintf(`
					SELECT COUNT(a.id)
					FROM activity a
					LEFT JOIN task t ON a.task_id = t.task_id
					WHERE a.stage_id = $1 
					AND t.element_type_id = $2 
					AND t.floor_id = $3
					AND a.%s = 'completed'`, statusField)
				qcArgs = []interface{}{stage.StageID, req.ElementTypeID, req.TargetLocation}
			} else {
				qcQuery = fmt.Sprintf(`
					SELECT COUNT(a.id)
					FROM activity a
					LEFT JOIN task t ON a.task_id = t.task_id
					WHERE a.stage_id = $1 
					AND t.element_type_id = $2 
					AND a.%s = 'completed'`, statusField)
				qcArgs = []interface{}{stage.StageID, req.ElementTypeID}
			}
			err = db.QueryRowContext(ctxDrawings, qcQuery, qcArgs...).Scan(&qcCount)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to count QC activities",
					"details": err.Error(),
				})
				return
			}

			// Count production activities (where status is not 'completed')
			var prodQuery string
			var prodArgs []interface{}
			if req.TargetLocation > 0 {
				prodQuery = fmt.Sprintf(`
					SELECT COUNT(a.id)
					FROM activity a
					LEFT JOIN task t ON a.task_id = t.task_id
					WHERE a.stage_id = $1 
					AND t.element_type_id = $2 
					AND t.floor_id = $3
					AND a.%s != 'completed'`, statusField)
				prodArgs = []interface{}{stage.StageID, req.ElementTypeID, req.TargetLocation}
			} else {
				prodQuery = fmt.Sprintf(`
					SELECT COUNT(a.id)
					FROM activity a
					LEFT JOIN task t ON a.task_id = t.task_id
					WHERE a.stage_id = $1 
					AND t.element_type_id = $2 
					AND a.%s != 'completed'`, statusField)
				prodArgs = []interface{}{stage.StageID, req.ElementTypeID}
			}
			err = db.QueryRowContext(ctxDrawings, prodQuery, prodArgs...).Scan(&productionCount)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to count production activities",
					"details": err.Error(),
				})
				return
			}

			stage.Quantity = qcCount + productionCount
			stage.QC = qcCount
			stage.Production = productionCount

			element.Stages = append(element.Stages, stage)
		}

		// Return the complete element details
		c.JSON(http.StatusOK, element)

		log := models.ActivityLog{
			EventContext: "Elements",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Element details of Element Type %d", req.ElementTypeID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
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

// SearchElementTypes godoc
// @Summary      Search element types
// @Tags         element-types
// @Param        search   query  string  false  "Search term"
// @Param        project_id  query  int  false  "Project ID"
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/element-types/search [get]
func SearchElementTypes(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get search parameters from query string
		searchTerm := c.Query("search")
		dateFromStr := c.Query("date_from")
		dateToStr := c.Query("date_to")
		sortBy := c.DefaultQuery("sort_by", "created_at")
		sortOrder := c.DefaultQuery("sort_order", "desc")

		projectIDStr := c.Query("project_id")
		elementTypeStr := c.Query("element_type")
		elementTypeNameStr := c.Query("element_type_name")
		hierarchyIDStr := c.Query("hierarchy_id")
		statusFilter := c.Query("status") // production, stockyard, dispatch, erection, in_request

		// Get dimension search parameters
		thicknessStr := c.Query("thickness")
		lengthStr := c.Query("length")
		heightStr := c.Query("height")
		// weight parameter deprecated; use mass
		massStr := c.Query("mass")
		weightStr := c.Query("weight")

		// // Debug logging
		// log.Printf("SearchElementTypes - Received parameters:")
		// log.Printf("  searchTerm: '%s'", searchTerm)
		// log.Printf("  projectIDStr: '%s'", projectIDStr)
		// log.Printf("  elementTypeStr: '%s'", elementTypeStr)
		// log.Printf("  elementTypeNameStr: '%s'", elementTypeNameStr)
		// log.Printf("  hierarchyIDStr: '%s'", hierarchyIDStr)
		// log.Printf("  statusFilter: '%s'", statusFilter)
		// log.Printf("  thicknessStr: '%s'", thicknessStr)
		// log.Printf("  lengthStr: '%s'", lengthStr)
		// log.Printf("  heightStr: '%s'", heightStr)
		// log.Printf("  weightStr: '%s'", weightStr)
		// log.Printf("  dateFromStr: '%s'", dateFromStr)
		// log.Printf("  dateToStr: '%s'", dateToStr)

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("page_size", "10")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 10
		}

		offset := (page - 1) * pageSize

		// Validate sort parameters
		validSortFields := map[string]string{
			"created_at":        "created_at",
			"updated_at":        "update_at",
			"element_type":      "element_type",
			"element_type_name": "element_type_name",
			"project_id":        "project_id",
		}

		sortField, exists := validSortFields[sortBy]
		if !exists {
			sortField = "created_at"
		}

		if sortOrder != "asc" && sortOrder != "desc" {
			sortOrder = "desc"
		}

		// Build the advanced query with CTE
		baseQuery := `
		WITH element_hierarchy_combinations AS (
			SELECT 
				et.element_type_id,
				et.element_type,
				et.element_type_name,
				et.thickness,
				et.length,
				et.height,
				et.volume,
				et.mass,
				et.area,
				et.width,
				et.created_by,
				et.created_at,
				et.update_at,
				et.project_id,
				et.element_type_version,
				et.total_count_element,
				ethq.quantity,
				ethq.hierarchy_id,
                -- Get tower and floor names
                COALESCE(tower_p.name, '') AS tower_name,
                COALESCE(floor_p.name, '') AS floor_name,
                ROW_NUMBER() OVER (ORDER BY et.element_type_id, ethq.hierarchy_id) as rn
            FROM element_type et
            JOIN element_type_hierarchy_quantity ethq ON et.element_type_id = ethq.element_type_id
            LEFT JOIN precast floor_p ON ethq.hierarchy_id = floor_p.id
			LEFT JOIN precast tower_p ON floor_p.parent_id = tower_p.id
			WHERE 1=1
		`

		// Build count query
		countQuery := `
		SELECT COUNT(*)
		FROM element_type et
		JOIN element_type_hierarchy_quantity ethq ON et.element_type_id = ethq.element_type_id
		WHERE 1=1
		`

		var args []interface{}
		var countArgs []interface{}
		argIndex := 1

		// Debug: Track argument sources
		var argSources []string

		// Add search conditions
		if searchTerm != "" {
			searchCondition := fmt.Sprintf(" AND (et.element_type ILIKE $%d OR et.element_type_name ILIKE $%d)", argIndex, argIndex)
			baseQuery += searchCondition
			countQuery += searchCondition
			args = append(args, "%"+searchTerm+"%")
			countArgs = append(countArgs, "%"+searchTerm+"%")
			argSources = append(argSources, fmt.Sprintf("searchTerm: %s", searchTerm))
			argIndex++
		}

		if projectIDStr != "" {
			projectID, err := strconv.Atoi(projectIDStr)
			if err == nil {
				projectCondition := fmt.Sprintf(" AND et.project_id = $%d", argIndex)
				baseQuery += projectCondition
				countQuery += projectCondition
				args = append(args, projectID)
				countArgs = append(countArgs, projectID)
				argSources = append(argSources, fmt.Sprintf("projectID: %d", projectID))
				argIndex++
			}
		}

		if elementTypeStr != "" {
			elementTypeCondition := fmt.Sprintf(" AND et.element_type ILIKE $%d", argIndex)
			baseQuery += elementTypeCondition
			countQuery += elementTypeCondition
			args = append(args, "%"+elementTypeStr+"%")
			countArgs = append(countArgs, "%"+elementTypeStr+"%")
			argIndex++
		}

		if elementTypeNameStr != "" {
			elementTypeNameCondition := fmt.Sprintf(" AND et.element_type_name ILIKE $%d", argIndex)
			baseQuery += elementTypeNameCondition
			countQuery += elementTypeNameCondition
			args = append(args, "%"+elementTypeNameStr+"%")
			countArgs = append(countArgs, "%"+elementTypeNameStr+"%")
			argIndex++
		}

		if hierarchyIDStr != "" {
			hierarchyID, err := strconv.Atoi(hierarchyIDStr)
			if err == nil {
				hierarchyCondition := fmt.Sprintf(" AND ethq.hierarchy_id = $%d", argIndex)
				baseQuery += hierarchyCondition
				countQuery += hierarchyCondition
				args = append(args, hierarchyID)
				countArgs = append(countArgs, hierarchyID)
				argSources = append(argSources, fmt.Sprintf("hierarchyID: %d", hierarchyID))
				argIndex++
			}
		}

		// Add dimension filters
		if thicknessStr != "" {
			thickness, err := strconv.ParseFloat(thicknessStr, 64)
			if err == nil {
				thicknessCondition := fmt.Sprintf(" AND et.thickness = $%d", argIndex)
				baseQuery += thicknessCondition
				countQuery += thicknessCondition
				args = append(args, thickness)
				countArgs = append(countArgs, thickness)
				argIndex++
			}
		}

		if lengthStr != "" {
			length, err := strconv.ParseFloat(lengthStr, 64)
			if err == nil {
				lengthCondition := fmt.Sprintf(" AND et.length = $%d", argIndex)
				baseQuery += lengthCondition
				countQuery += lengthCondition
				args = append(args, length)
				countArgs = append(countArgs, length)
				argIndex++
			}
		}

		if heightStr != "" {
			height, err := strconv.ParseFloat(heightStr, 64)
			if err == nil {
				heightCondition := fmt.Sprintf(" AND et.height = $%d", argIndex)
				baseQuery += heightCondition
				countQuery += heightCondition
				args = append(args, height)
				countArgs = append(countArgs, height)
				argIndex++
			}
		}

		// Support both mass and legacy weight query param (weight will be treated as mass)
		if massStr == "" && weightStr != "" {
			massStr = weightStr
		}
		if massStr != "" {
			mass, err := strconv.ParseFloat(massStr, 64)
			if err == nil {
				massCondition := fmt.Sprintf(" AND et.mass = $%d", argIndex)
				baseQuery += massCondition
				countQuery += massCondition
				args = append(args, mass)
				countArgs = append(countArgs, mass)
				argIndex++
			}
		}

		// Add date range filters
		if dateFromStr != "" {
			dateFromCondition := fmt.Sprintf(" AND et.created_at >= $%d", argIndex)
			baseQuery += dateFromCondition
			countQuery += dateFromCondition
			args = append(args, dateFromStr)
			countArgs = append(countArgs, dateFromStr)
			argIndex++
		}

		if dateToStr != "" {
			dateToCondition := fmt.Sprintf(" AND et.created_at <= $%d", argIndex)
			baseQuery += dateToCondition
			countQuery += dateToCondition
			args = append(args, dateToStr)
			countArgs = append(countArgs, dateToStr)
			argIndex++
		}

		// Add status filter
		if statusFilter != "" {
			statusCondition := ""
			switch statusFilter {
			case "production":
				statusCondition = ` AND EXISTS (
					SELECT 1 FROM element e 
					WHERE e.element_type_id = et.element_type_id 
					AND e.instage = true
				)`
			case "stockyard":
				statusCondition = ` AND EXISTS (
					SELECT 1 FROM precast_stock ps 
					WHERE ps.element_type_id = et.element_type_id 
					AND ps.dispatch_status = false 
					AND ps.erected = false
				)`
			case "dispatch":
				statusCondition = ` AND EXISTS (
					SELECT 1 FROM precast_stock ps 
					WHERE ps.element_type_id = et.element_type_id 
					AND ps.dispatch_status = true 
					AND ps.order_by_erection = true 
					AND ps.erected = false
				)`
			case "erection":
				statusCondition = ` AND EXISTS (
					SELECT 1 FROM precast_stock ps 
					WHERE ps.element_type_id = et.element_type_id 
					AND ps.dispatch_status = true 
					AND ps.order_by_erection = true 
					AND ps.erected = true
				)`
			case "in_request":
				statusCondition = ` AND EXISTS (
					SELECT 1 FROM precast_stock ps 
					WHERE ps.element_type_id = et.element_type_id 
					AND ps.dispatch_status = false 
					AND ps.order_by_erection = true 
					AND ps.erected = false
				)`
			}
			if statusCondition != "" {
				baseQuery += statusCondition
				countQuery += statusCondition
			}
		}

		// Complete the base query with CTE
		baseQuery += `
		)
		SELECT 
			element_type_id,
			element_type,
			element_type_name,
			thickness,
			length,
			height,
			volume, mass, area, width,
			created_by,
			created_at,
			update_at,
			project_id,
			element_type_version,
			total_count_element,
			quantity,
			hierarchy_id,
			tower_name,
			floor_name
		FROM element_hierarchy_combinations
		WHERE rn > $` + strconv.Itoa(argIndex) + ` AND rn <= $` + strconv.Itoa(argIndex+1) + `
		ORDER BY ` + sortField + ` ` + strings.ToUpper(sortOrder) + `, element_type_id, hierarchy_id
		LIMIT $` + strconv.Itoa(argIndex+2) + ` OFFSET $` + strconv.Itoa(argIndex+3)

		args = append(args, offset, offset+pageSize, pageSize, offset)
		argSources = append(argSources, "offset", "offset+pageSize", "pageSize", "offset")

		// Debug: Show argument mapping
		log.Printf("=== ARGUMENT MAPPING ===")
		for i, arg := range args {
			source := "unknown"
			if i < len(argSources) {
				source = argSources[i]
			}
			log.Printf("$%d = %v (type: %T) [%s]", i+1, arg, arg, source)
		}
		log.Printf("Total arguments: %d", len(args))
		log.Printf("========================")

		// Debug: Check total element types in database
		ctxDebug, cancelDebug := utils.GetFastQueryContext(c.Request.Context())
		defer cancelDebug()

		var totalElementTypes int
		err = db.QueryRowContext(ctxDebug, "SELECT COUNT(*) FROM element_type").Scan(&totalElementTypes)
		if err != nil {
			log.Printf("Error checking total element types: %v", err)
		} else {
			log.Printf("Total element types in database: %d", totalElementTypes)
		}

		// Get total count
		var totalRecords int
		ctxCount, cancelCount := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelCount()

		err = db.QueryRowContext(ctxCount, countQuery, countArgs...).Scan(&totalRecords)
		if err != nil {
			log.Printf("Count query error: %v", err)
			log.Printf("Count query: %s", countQuery)
			log.Printf("Count args: %v", countArgs)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count records: " + err.Error()})
			return
		}

		// Calculate total pages
		totalPages := (totalRecords + pageSize - 1) / pageSize

		// Debug: Test simple query without filters
		var simpleCount int
		simpleQuery := "SELECT COUNT(*) FROM element_type et JOIN element_type_hierarchy_quantity ethq ON et.element_type_id = ethq.element_type_id"
		err = db.QueryRowContext(ctxDebug, simpleQuery).Scan(&simpleCount)
		if err != nil {
			log.Printf("Simple query error: %v", err)
		} else {
			log.Printf("Simple query count: %d", simpleCount)
		}

		// // Print the final query for debugging
		// fmt.Println("=== FINAL QUERY ===")
		// fmt.Println("Base Query:")
		// fmt.Println(baseQuery)
		// fmt.Println("Count Query:")
		// fmt.Println(countQuery)
		// fmt.Println("Arguments:", args)
		// fmt.Println("Count Arguments:", countArgs)
		// fmt.Println("Total Records:", totalRecords)
		// fmt.Println("Simple Count:", simpleCount)
		// fmt.Println("Page:", page, "PageSize:", pageSize, "Offset:", offset)
		// fmt.Println("==================")

		// Execute the main query with context timeout
		ctxMain, cancelMain := utils.GetDefaultQueryContext(c.Request.Context())
		defer cancelMain()

		rows, err := db.QueryContext(ctxMain, baseQuery, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element types: " + err.Error()})
			return
		}
		defer rows.Close()

		var elementTypes []models.ElementTypeR

		// Iterate through the rows
		for rows.Next() {
			var elementType models.ElementTypeR
			var quantity int
			var hierarchyID int

			// Scan the row into variables
			err := rows.Scan(
				&elementType.ElementTypeId,
				&elementType.ElementType,
				&elementType.ElementTypeName,
				&elementType.Thickness,
				&elementType.Length,
				&elementType.Height,
				&elementType.Volume,
				&elementType.Mass,
				&elementType.Area,
				&elementType.Width,
				&elementType.CreatedBy,
				&elementType.CreatedAt,
				&elementType.UpdatedAt,
				&elementType.ProjectID,
				&elementType.ElementTypeVersion,
				&elementType.TotalCountElement,
				&quantity,
				&hierarchyID,
				&elementType.TowerName,
				&elementType.FloorName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan element type: %v", err)})
				return
			}

			// Fetch hierarchy quantities from element_type_hierarchy_quantity table
			hierarchyQuery := `
				SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
				       p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix,
				       parent.name as tower_name
				FROM element_type_hierarchy_quantity hq
				JOIN precast p ON hq.hierarchy_id = p.id
				LEFT JOIN precast parent ON p.parent_id = parent.id
				WHERE hq.element_type_id = $1
			`

			hierarchyRows, err := db.QueryContext(ctxMain, hierarchyQuery, elementType.ElementTypeId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err)})
				return
			}
			defer hierarchyRows.Close()

			var hierarchyResponseList []models.HierarchyResponce
			for hierarchyRows.Next() {
				var hierarchyData models.HierarchyResponce
				var hierarchyId, quantity int
				var namingConvention string

				err := hierarchyRows.Scan(
					&hierarchyId, &quantity, &namingConvention,
					&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
					&hierarchyData.Description, &hierarchyData.ParentID, &hierarchyData.Prefix,
					&hierarchyData.TowerName,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan hierarchy data: %v", err)})
					return
				}

				hierarchyData.Quantity = quantity
				hierarchyData.NamingConvention = namingConvention
				hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
			}

			elementType.HierarchyResponce = hierarchyResponseList

			// Fetch associated drawings
			drawingsQuery := `
				SELECT drawing_id, current_version, created_at, created_by, drawing_type_id, update_at, updated_by, comments, file, element_type_id
				FROM drawings
				WHERE element_type_id = $1 ORDER BY created_at DESC
			`
			drawingRows, err := db.QueryContext(ctxMain, drawingsQuery, elementType.ElementTypeId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch drawings: %v", err)})
				return
			}
			defer drawingRows.Close()

			var drawings []models.DrawingsR
			for drawingRows.Next() {
				var drawing models.DrawingsR
				err := drawingRows.Scan(
					&drawing.DrawingsId, &drawing.CurrentVersion, &drawing.CreatedAt, &drawing.CreatedBy, &drawing.DrawingTypeId,
					&drawing.UpdatedAt, &drawing.UpdatedBy, &drawing.Comments, &drawing.File, &drawing.ElementTypeID,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan drawing: %v", err)})
					return
				}

				// Fetch associated drawing revisions
				revisionsQuery := `
					SELECT parent_drawing_id, version, created_at, created_by, drawing_type_id, comments, file, drawing_revision_id, element_type_id
					FROM drawings_revision
					WHERE parent_drawing_id = $1 ORDER BY created_at DESC
				`
				revisionRows, err := db.QueryContext(ctxMain, revisionsQuery, drawing.DrawingsId)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch drawing revisions: %v", err)})
					return
				}
				defer revisionRows.Close()

				for revisionRows.Next() {
					var revision models.DrawingsRevisionR
					err := revisionRows.Scan(
						&revision.ParentDrawingsId,
						&revision.Version,
						&revision.CreatedAt,
						&revision.CreatedBy,
						&revision.DrawingsTypeId,
						&revision.Comments,
						&revision.File,
						&revision.DrawingsRevisionId,
						&revision.ElementTypeID,
					)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan drawing revision: %v", err)})
						return
					}

					revision.CreatedAtFormatted = formatDateTime(revision.CreatedAt)
					drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
				}
				drawings = append(drawings, drawing)
			}

			elementType.Drawings = drawings

			// Format createdAt and updateAt for elementType
			elementType.CreatedAtFormatted = formatDateTime(elementType.CreatedAt)
			elementType.UpdatedAtFormatted = formatDateTime(elementType.UpdatedAt)

			// Fetch BOM products from element_type_bom table
			productsQuery := `
				SELECT product_id, product_name, quantity, unit, rate
				FROM element_type_bom
				WHERE element_type_id = $1
			`

			productRows, err := db.QueryContext(ctxMain, productsQuery, elementType.ElementTypeId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
				return
			}
			defer productRows.Close()

			var products []models.ProductR
			for productRows.Next() {
				var product models.ProductR
				var unit sql.NullString
				var rateFloat sql.NullFloat64

				err := productRows.Scan(
					&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
					return
				}

				// Handle nullable fields if needed in the future
				_ = unit
				_ = rateFloat

				products = append(products, product)
			}

			elementType.Products = products

			// Add advanced statistics if hierarchy_id is provided
			if hierarchyID > 0 {
				// Get element IDs for this element type and hierarchy
				elementIDsQuery := `SELECT id FROM element WHERE element_type_id = $1 AND target_location = $2`
				elementRows, err := db.QueryContext(ctxMain, elementIDsQuery, elementType.ElementTypeId, hierarchyID)
				if err == nil {
					defer elementRows.Close()

					var elementIDs []int
					for elementRows.Next() {
						var eid int
						if err := elementRows.Scan(&eid); err == nil {
							elementIDs = append(elementIDs, eid)
						}
					}

					if len(elementIDs) > 0 {
						// Get Production Count
						var productionCount int
						productionCountQuery := `SELECT COUNT(*) FROM activity WHERE element_id = ANY($1)`
						err = db.QueryRowContext(ctxMain, productionCountQuery, pq.Array(elementIDs)).Scan(&productionCount)
						if err == nil {
							// Add production count to response (you might need to extend the model)
							log.Printf("Production count for element_type_id %d, hierarchy_id %d: %d", elementType.ElementTypeId, hierarchyID, productionCount)
						}

						// Get Stockyard Count
						var stockyardCount int
						stockyardCountQuery := `
							SELECT COUNT(*) 
							FROM precast_stock 
							WHERE element_id = ANY($1) 
							  AND dispatch_status IS false  
							  AND erected IS false`
						err = db.QueryRowContext(ctxMain, stockyardCountQuery, pq.Array(elementIDs)).Scan(&stockyardCount)
						if err == nil {
							log.Printf("Stockyard count for element_type_id %d, hierarchy_id %d: %d", elementType.ElementTypeId, hierarchyID, stockyardCount)
						}

						// Get In Request Count
						var inRequestCount int
						inRequestCountQuery := `
							SELECT COUNT(*)
							FROM precast_stock
							WHERE element_id = ANY($1)
							  AND dispatch_status IS false
							  AND order_by_erection IS true
							  AND erected IS false`
						err = db.QueryRowContext(ctxMain, inRequestCountQuery, pq.Array(elementIDs)).Scan(&inRequestCount)
						if err == nil {
							log.Printf("In request count for element_type_id %d, hierarchy_id %d: %d", elementType.ElementTypeId, hierarchyID, inRequestCount)
						}

						// Get Erection Count
						var erectionCount int
						erectionCountQuery := `
							SELECT COUNT(*) 
							FROM precast_stock 
							WHERE element_id = ANY($1) 
							  AND dispatch_status IS true 
							  AND order_by_erection IS true 
							  AND erected IS true`
						err = db.QueryRowContext(ctxMain, erectionCountQuery, pq.Array(elementIDs)).Scan(&erectionCount)
						if err == nil {
							log.Printf("Erection count for element_type_id %d, hierarchy_id %d: %d", elementType.ElementTypeId, hierarchyID, erectionCount)
						}

						// Get Dispatch Count
						var dispatchCount int
						dispatchCountQuery := `
							SELECT COUNT(*) 
							FROM precast_stock 
							WHERE element_id = ANY($1) 
							  AND dispatch_status IS true 
							  AND order_by_erection IS true 
							  AND erected IS false`
						err = db.QueryRowContext(ctxMain, dispatchCountQuery, pq.Array(elementIDs)).Scan(&dispatchCount)
						if err == nil {
							log.Printf("Dispatch count for element_type_id %d, hierarchy_id %d: %d", elementType.ElementTypeId, hierarchyID, dispatchCount)
						}
					}
				}
			}

			elementTypes = append(elementTypes, elementType)
		}

		// Create pagination response
		pagination := models.Pagination{
			CurrentPage:  page,
			PageSize:     pageSize,
			TotalRecords: totalRecords,
			TotalPages:   totalPages,
			HasNext:      page < totalPages,
			HasPrev:      page > 1,
		}

		response := models.PaginatedResponse{
			Data:       elementTypes,
			Pagination: pagination,
		}

		c.JSON(http.StatusOK, response)
	}
}

// GetElementByID godoc
// @Summary      Scan element by ID (get element details)
// @Tags         elements
// @Param        id   path  int  true  "Element ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      404  {object}  object
// @Router       /api/scan_element/{id} [get]
func GetElementByID(db *sql.DB) gin.HandlerFunc {
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

		elementIDStr := c.Param("id")

		// Convert string ID to integer
		elementID, convErr := strconv.Atoi(elementIDStr)
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID format"})
			return
		}

		var element models.Element

		// Fetch element by ID
		query := `
		SELECT id, element_type_id, element_id, element_name, project_id, created_by, created_at,
		       status, element_type_version, update_at, target_location, disable
		FROM element
		WHERE id = $1
	`
		ctxElement, cancelElement := utils.GetFastQueryContext(c.Request.Context())
		defer cancelElement()

		var statusStr string
		err = db.QueryRowContext(ctxElement, query, elementID).Scan(
			&element.Id,
			&element.ElementTypeID,
			&element.ElementId,
			&element.ElementName,
			&element.ProjectID,
			&element.CreatedBy,
			&element.CreatedAt,
			&statusStr,
			&element.ElementTypeVersion,
			&element.UpdateAt,
			&element.TargetLocation,
			&element.Disable,
		)
		// Convert status string to int, handling potential whitespace
		if statusStr != "" {
			statusStr = strings.TrimSpace(statusStr)
			if statusInt, parseErr := strconv.Atoi(statusStr); parseErr == nil {
				element.Status = statusInt
			} else {
				element.Status = 0 // Default value if parsing fails
			}
		}
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Element not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch element",
					"details": err.Error(),
				})
			}
			return
		}

		// Fetch associated element type details
		var elementType models.ElementTypeR

		elementTypeQuery := `
	SELECT element_type, element_type_name, thickness, length, height, volume, mass, area, width, created_by,
		       created_at, update_at, element_type_id, project_id, element_type_version,
		       total_count_element
		FROM element_type
		WHERE element_type_id = $1
	`
		var volume, mass, area, width float64
		err = db.QueryRowContext(ctxElement, elementTypeQuery, element.ElementTypeID).Scan(
			&elementType.ElementType,
			&elementType.ElementTypeName,
			&elementType.Thickness,
			&elementType.Length,
			&elementType.Height,
			&volume, &mass, &area, &width,
			&elementType.CreatedBy,
			&elementType.CreatedAt,
			&elementType.UpdatedAt,
			&elementType.ElementTypeId,
			&elementType.ProjectID,
			&elementType.ElementTypeVersion,
			&elementType.TotalCountElement,
		)
		elementType.Volume = volume
		elementType.Mass = mass
		elementType.Area = area
		elementType.Width = width
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch element type",
				"details": err.Error(),
			})
			return
		}

		// Fetch hierarchy quantities from element_type_hierarchy_quantity table
		hierarchyQuery := `
			SELECT hq.hierarchy_id, hq.quantity, hq.naming_convention, 
			       p.id, p.project_id, p.name, p.description, p.parent_id, p.prefix,
			       parent.name as tower_name
			FROM element_type_hierarchy_quantity hq
			JOIN precast p ON hq.hierarchy_id = p.id
			LEFT JOIN precast parent ON p.parent_id = parent.id
			WHERE hq.element_type_id = $1
		`

		hierarchyRows, err := db.QueryContext(ctxElement, hierarchyQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch hierarchy quantities: %v", err)})
			return
		}
		defer hierarchyRows.Close()

		var hierarchyResponseList []models.HierarchyResponce
		for hierarchyRows.Next() {
			var hierarchyData models.HierarchyResponce
			var hierarchyId, quantity int
			var namingConvention string
			var parentID sql.NullInt64

			err := hierarchyRows.Scan(
				&hierarchyId, &quantity, &namingConvention,
				&hierarchyData.HierarchyID, &hierarchyData.ProjectID, &hierarchyData.Name,
				&hierarchyData.Description, &parentID, &hierarchyData.Prefix,
				&hierarchyData.TowerName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan hierarchy data: %v", err)})
				return
			}

			// Handle NULL parent_id
			if parentID.Valid {
				hierarchyData.ParentID = int(parentID.Int64)
			} else {
				hierarchyData.ParentID = 0 // or -1, depending on your business logic
			}

			hierarchyData.Quantity = quantity
			hierarchyData.NamingConvention = namingConvention
			// Ensure floor_name is exposed separately
			hierarchyData.FloorName = hierarchyData.Name
			hierarchyResponseList = append(hierarchyResponseList, hierarchyData)
		}

		elementType.HierarchyResponce = hierarchyResponseList

		// If available, set top-level tower_name and floor_name on elementType from first hierarchy entry
		if len(hierarchyResponseList) > 0 {
			elementType.FloorName = hierarchyResponseList[0].FloorName
			if hierarchyResponseList[0].TowerName != nil {
				elementType.TowerName = *hierarchyResponseList[0].TowerName
			}
		}

		// Fetch drawings for this specific element's element type
		drawingsQuery := `
		SELECT d.drawing_id, d.current_version, d.created_at, d.created_by, d.drawing_type_id, dt.drawing_type_name, d.update_at, d.updated_by,
       d.comments, d.file, d.element_type_id
FROM drawings d
JOIN drawing_type dt ON d.drawing_type_id = dt.drawing_type_id 
WHERE d.element_type_id = $1
ORDER BY d.created_at DESC
	`
		drawingRows, err := db.QueryContext(ctxElement, drawingsQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawings", "details": err.Error()})
			return
		}
		defer drawingRows.Close()

		var drawings []models.DrawingsR
		for drawingRows.Next() {
			var drawing models.DrawingsR
			err := drawingRows.Scan(
				&drawing.DrawingsId,
				&drawing.CurrentVersion,
				&drawing.CreatedAt,
				&drawing.CreatedBy,
				&drawing.DrawingTypeId,
				&drawing.DrawingTypeName,
				&drawing.UpdatedAt,
				&drawing.UpdatedBy,
				&drawing.Comments,
				&drawing.File,
				&drawing.ElementTypeID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing", "details": err.Error()})
				return
			}

			// Fetch drawing revisions
			revisionQuery := `
			SELECT dr.parent_drawing_id, dr.version, dr.created_at, dr.created_by, dr.drawing_type_id, 
			       dt.drawing_type_name, dr.comments, dr.file, dr.drawing_revision_id, dr.element_type_id
			FROM drawings_revision dr
			JOIN drawing_type dt ON dr.drawing_type_id = dt.drawing_type_id
			WHERE dr.parent_drawing_id = $1
			ORDER BY dr.created_at DESC
		`
			revisionRows, err := db.QueryContext(ctxElement, revisionQuery, drawing.DrawingsId)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch drawing revisions", "details": err.Error()})
				return
			}
			defer revisionRows.Close()

			for revisionRows.Next() {
				var revision models.DrawingsRevisionR
				err := revisionRows.Scan(
					&revision.ParentDrawingsId,
					&revision.Version,
					&revision.CreatedAt,
					&revision.CreatedBy,
					&revision.DrawingsTypeId,
					&revision.DrawingTypeName,
					&revision.Comments,
					&revision.File,
					&revision.DrawingsRevisionId,
					&revision.ElementTypeID,
				)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan drawing revision", "details": err.Error()})
					return
				}
				revision.CreatedAtFormatted = formatDateTime(revision.CreatedAt)
				drawing.DrawingsRevision = append(drawing.DrawingsRevision, revision)
			}
			drawings = append(drawings, drawing)
		}

		elementType.Drawings = drawings

		elementType.CreatedAtFormatted = formatDate(elementType.CreatedAt)
		elementType.UpdatedAtFormatted = formatDate(elementType.UpdatedAt)

		// Fetch BOM products from element_type_bom table
		productsQuery := `
			SELECT product_id, product_name, quantity, unit, rate
			FROM element_type_bom
			WHERE element_type_id = $1
		`

		productRows, err := db.QueryContext(ctxElement, productsQuery, element.ElementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch BOM products: %v", err)})
			return
		}
		defer productRows.Close()

		var products []models.ProductR
		for productRows.Next() {
			var product models.ProductR
			var unit sql.NullString
			var rateFloat sql.NullFloat64

			err := productRows.Scan(
				&product.ProductID, &product.ProductName, &product.Quantity, &unit, &rateFloat,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to scan product: %v", err)})
				return
			}

			// Handle nullable fields if needed in the future
			_ = unit
			_ = rateFloat

			products = append(products, product)
		}

		elementType.Products = products

		// Fetch submitted answers for this element with all related data
		answersQuery := `
			SELECT 
				qa.id,
				qa.project_id,
				qa.qc_id,
				qa.question_id,
				qa.option_id,
				qa.task_id,
				qa.stage_id,
				qa.comment,
				qa.image_path,
				qa.created_at,
				qa.updated_at,
				qa.element_id,
				CONCAT(u.first_name, ' ', u.last_name) as qc_name,
				o.option_text,
				q.question_text,
				q.paper_id,
				p.name as paper_name,
				ps.name as stage_name
			FROM 
				qc_answers qa
			LEFT JOIN 
				users u ON qa.qc_id = u.id
			LEFT JOIN 
				options o ON qa.option_id = o.id
			LEFT JOIN 
				questions q ON qa.question_id = q.id
			LEFT JOIN 
				papers p ON q.paper_id = p.id
			LEFT JOIN 
				project_stages ps ON qa.stage_id = ps.id
			WHERE 
				qa.element_id = $1
			ORDER BY 
				qa.stage_id, qa.created_at DESC
		`

		answerRows, err := db.QueryContext(ctxElement, answersQuery, elementID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch answers", "details": err.Error()})
			return
		}
		defer answerRows.Close()

		// Map to group answers by stage_id
		stageGroups := make(map[int][]map[string]interface{})

		// Iterate over the answer rows and populate the map
		for answerRows.Next() {
			var (
				id           int
				projectID    int
				qcID         int
				questionID   int
				optionID     *int
				taskID       int
				stageID      int
				comment      *string
				imagePath    *string
				createdAt    time.Time
				updatedAt    time.Time
				elementID    int
				qcName       *string
				optionText   *string
				questionText *string
				paperID      *int
				paperName    *string
				stageName    *string
			)

			err := answerRows.Scan(
				&id, &projectID, &qcID, &questionID, &optionID, &taskID, &stageID,
				&comment, &imagePath, &createdAt, &updatedAt, &elementID,
				&qcName, &optionText, &questionText, &paperID, &paperName, &stageName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan answer", "details": err.Error()})
				return
			}

			// Create answer object
			answer := map[string]interface{}{
				"id":            id,
				"project_id":    projectID,
				"qc_id":         qcID,
				"question_id":   questionID,
				"option_id":     optionID,
				"task_id":       taskID,
				"stage_id":      stageID,
				"comment":       comment,
				"image_path":    imagePath,
				"created_at":    createdAt,
				"updated_at":    updatedAt,
				"element_id":    elementID,
				"qc_name":       qcName,
				"option_text":   optionText,
				"question_text": questionText,
				"paper_id":      paperID,
				"paper_name":    paperName,
				"stage_name":    stageName,
			}

			// Group by stage_id
			stageGroups[stageID] = append(stageGroups[stageID], answer)
		}

		// Convert map to slice for JSON response
		var submittedAnswers []map[string]interface{}
		for stageID, answers := range stageGroups {
			stageGroup := map[string]interface{}{
				"stage_id":   stageID,
				"stage_name": getStageNameFromAnswers(answers),
				"answers":    answers,
			}
			submittedAnswers = append(submittedAnswers, stageGroup)
		}

		// Fetch element lifecycle events with embedded answers
		type Event struct {
			Label     string                   `json:"label"`
			Timestamp time.Time                `json:"timestamp,omitempty"` // Use interface{} to allow empty string
			Duration  string                   `json:"duration,omitempty"`
			StageID   *int                     `json:"stage_id,omitempty"`
			Answers   []map[string]interface{} `json:"answers,omitempty"`
		}

		var lifecycle []Event

		// Step 1: Element created (already have this data)
		lifecycle = append(lifecycle, Event{"Element Created", element.CreatedAt, "", nil, nil})

		// Step 2: Task Assigned?
		var instage bool
		err = db.QueryRowContext(ctxElement, `SELECT instage FROM element WHERE id = $1`, elementID).Scan(&instage)
		if err != nil {
			log.Printf("Error fetching instage: %v", err)
			// Continue execution even if this fails
		} else if instage {
			var taskTime time.Time
			err = db.QueryRowContext(ctxElement, `SELECT MIN(start_date) FROM activity WHERE element_id = $1`, elementID).Scan(&taskTime)
			if err != nil {
				log.Printf("Error fetching task time: %v", err)
			} else if !taskTime.IsZero() {
				lifecycle = append(lifecycle, Event{"Task Assigned", taskTime, "", nil, nil})
			}
		}

		// Step 3: Get stages from element_type_path in correct order
		log.Printf("Fetching stages for element_type_id: %d, project_id: %d", element.ElementTypeID, element.ProjectID)

		// Try to get stage_path from element_type table
		var stagePathStr string
		stagePathQuery := `SELECT stage_path FROM element_type_path WHERE element_type_id = $1`
		err = db.QueryRowContext(ctxElement, stagePathQuery, element.ElementTypeID).Scan(&stagePathStr)
		if err != nil {
			log.Printf("Error fetching stage_path: %v", err)
			// Try alternative: check if stage_path column exists
			var columnExists bool
			checkColumnQuery := `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns 
					WHERE table_name = 'element_type_path' AND column_name = 'stage_path'
				)`
			err = db.QueryRowContext(ctxElement, checkColumnQuery).Scan(&columnExists)
			if err != nil {
				log.Printf("Error checking if stage_path column exists: %v", err)
			} else {
				log.Printf("stage_path column exists: %v", columnExists)
			}

			// If stage_path doesn't exist, use the hardcoded stage path you provided
			if !columnExists || stagePathStr == "" {
				log.Printf("Using hardcoded stage path: {76,75,74,73,77}")
				stagePathStr = "{76,75,74,73,77}"
			}
		}

		log.Printf("Stage path: %s", stagePathStr)

		// Parse stage_path string to get stage IDs in order
		stageIDs := parseStagePath(stagePathStr)
		log.Printf("Parsed stage IDs: %v", stageIDs)

		if len(stageIDs) == 0 {
			log.Printf("No stage IDs parsed, using hardcoded stages")
			stageIDs = []int{76, 75, 74, 73, 77}
		}

		// Create a map to track completed stages with their timestamps
		completedStages := make(map[int]time.Time)

		// Get completed stages with timestamps - handle NULL values properly
		// First, let's check what columns exist in complete_production table
		log.Printf("Checking complete_production table structure for element_id %d", elementID)
		
		// Try alternative column names if started_at doesn't work
		completedStagesQuery := `
			SELECT cp.stage_id, cp.started_at
			FROM complete_production cp
			WHERE cp.element_id = $1`
		log.Printf("Executing completed stages query for element_id %d: %s", elementID, completedStagesQuery)
		completedRows, err := db.QueryContext(ctxElement, completedStagesQuery, elementID)
		if err != nil {
			log.Printf("Error fetching completed stages with started_at: %v", err)
			// Try alternative column names
			alternativeQueries := []string{
				"SELECT cp.stage_id, cp.created_at FROM complete_production cp WHERE cp.element_id = $1",
				"SELECT cp.stage_id, cp.completion_date FROM complete_production cp WHERE cp.element_id = $1",
				"SELECT cp.stage_id, cp.timestamp FROM complete_production cp WHERE cp.element_id = $1",
			}
			
			for i, altQuery := range alternativeQueries {
				log.Printf("Trying alternative query %d: %s", i+1, altQuery)
				completedRows, err = db.QueryContext(ctxElement, altQuery, elementID)
				if err == nil {
					log.Printf("Alternative query %d succeeded", i+1)
					break
				} else {
					log.Printf("Alternative query %d failed: %v", i+1, err)
				}
			}
		}
		
		if err != nil {
			log.Printf("All queries failed, unable to fetch completed stages: %v", err)
		} else {
			defer completedRows.Close()
			for completedRows.Next() {
				var stageID int
				var startedAt sql.NullTime
				err := completedRows.Scan(&stageID, &startedAt)
				if err != nil {
					log.Printf("Error scanning completed stage row: %v", err)
					continue
				}
				var timestamp time.Time
				if startedAt.Valid {
					timestamp = startedAt.Time
					log.Printf("Found completed stage: stage_id=%d, started_at=%v (is_zero=%v)", stageID, timestamp, timestamp.IsZero())
				} else {
					log.Printf("Found completed stage: stage_id=%d, started_at=NULL", stageID)
					// Use current time as fallback for NULL timestamps
					timestamp = time.Now()
				}
				completedStages[stageID] = timestamp
			}
			log.Printf("Total completed stages found: %d", len(completedStages))
			for stageID, timestamp := range completedStages {
				log.Printf("Completed stage %d at %v", stageID, timestamp)
			}
		}

		// Process stages in the order from stage_path
		for _, stageID := range stageIDs {
			// Get stage name
			var stageName string
			stageNameQuery := `SELECT name FROM project_stages WHERE id = $1`
			err := db.QueryRowContext(ctxElement, stageNameQuery, stageID).Scan(&stageName)
			if err != nil {
				log.Printf("Error fetching stage name for stage_id %d: %v", stageID, err)
				stageName = fmt.Sprintf("Stage %d", stageID)
			}

			// Check if this stage is completed
			if startedAt, isCompleted := completedStages[stageID]; isCompleted {
				// Stage is completed - get answers and use actual timestamp
				log.Printf("Stage %d (%s) is completed at %v", stageID, stageName, startedAt)
				var stageAnswers []map[string]interface{}
				if answers, exists := stageGroups[stageID]; exists {
					stageAnswers = answers
					log.Printf("Found %d answers for stage %d", len(stageAnswers), stageID)
				} else {
					log.Printf("No answers found for completed stage %d", stageID)
				}
				lifecycle = append(lifecycle, Event{"Stage: " + stageName, startedAt, "", &stageID, stageAnswers})
			} else {
				// Stage is not completed - add with stage name and empty timestamp
				log.Printf("Stage %d (%s) is not completed", stageID, stageName)
				lifecycle = append(lifecycle, Event{"Stage: " + stageName, time.Time{}, "", &stageID, nil})
			}
		}

		// Step 4: Rectification
		rectQuery := `
			SELECT status, comments, created_at
			FROM element_rectification
			WHERE element_id = $1`
		rectRows, err := db.QueryContext(ctxElement, rectQuery, elementID)
		if err != nil {
			log.Printf("Error fetching rectification: %v", err)
		} else {
			defer rectRows.Close()
			for rectRows.Next() {
				var status, comments string
				var at time.Time
				err := rectRows.Scan(&status, &comments, &at)
				if err != nil {
					log.Printf("Error scanning rectification row: %v", err)
					continue
				}
				label := fmt.Sprintf("Rectification %s - %s", status, comments)
				lifecycle = append(lifecycle, Event{label, at, "", nil, nil})
			}
		}

		// Step 5: Precast Stock - stockyard, dispatch, erected, received
		log.Printf("Fetching precast stock data for element_id %d", elementID)
		var ps models.PrecastStock2
		var stockyard sql.NullBool
		var dispatchStatus sql.NullBool
		var erected sql.NullBool
		var receiveInErection sql.NullBool
		var dispatchStart, dispatchEnd, createdAt, updatedAt sql.NullTime
		
		err = db.QueryRowContext(ctxElement, `
			SELECT stockyard, dispatch_status, erected, dispatch_start, dispatch_end, created_at, updated_at, recieve_in_erection
			FROM precast_stock WHERE element_id = $1`, elementID).
			Scan(&stockyard, &dispatchStatus, &erected, &dispatchStart, &dispatchEnd, &createdAt, &updatedAt, &receiveInErection)

		if err != nil {
			log.Printf("Error fetching precast stock for element_id %d: %v", elementID, err)
			if err == sql.ErrNoRows {
				log.Printf("No precast stock record found for element_id %d", elementID)
			}
			// Check if element exists in precast_stock table
			var count int
			err = db.QueryRowContext(ctxElement, `SELECT COUNT(*) FROM precast_stock WHERE element_id = $1`, elementID).Scan(&count)
			if err != nil {
				log.Printf("Error checking precast_stock count: %v", err)
			} else {
				log.Printf("Found %d records in precast_stock for element_id %d", count, elementID)
			}
		} else {
			// Convert NullBool to bool
			ps.Stockyard = stockyard.Valid && stockyard.Bool
			ps.DispatchStatus = dispatchStatus.Valid && dispatchStatus.Bool
			ps.Erected = erected.Valid && erected.Bool
			ps.ReceiveInErection = receiveInErection.Valid && receiveInErection.Bool
			
			// Convert NullTime to Time
			if dispatchStart.Valid {
				ps.DispatchStart = dispatchStart.Time
			}
			if dispatchEnd.Valid {
				ps.DispatchEnd = dispatchEnd.Time
			}
			if createdAt.Valid {
				ps.CreatedAt = createdAt.Time
			}
			if updatedAt.Valid {
				ps.UpdatedAt = updatedAt.Time
			}
			
			log.Printf("Precast stock data for element_id %d: stockyard=%v, dispatch_status=%v, erected=%v, receive_in_erection=%v, created_at=%v, updated_at=%v",
				elementID, ps.Stockyard, ps.DispatchStatus, ps.Erected, ps.ReceiveInErection, ps.CreatedAt, ps.UpdatedAt)

			if ps.Stockyard {
				log.Printf("Adding 'Received in Stockyard' event with timestamp %v", ps.CreatedAt)
				lifecycle = append(lifecycle, Event{
					Label:     "Received in Stockyard",
					Timestamp: ps.CreatedAt,
				})
			} else {
				log.Printf("Stockyard is false, not adding 'Received in Stockyard' event")
			}

			if ps.DispatchStatus {
				// Dispatch duration
				duration := ps.DispatchEnd.Sub(ps.DispatchStart).String()
				log.Printf("Adding 'Dispatched' event with start time %v and duration %s", ps.DispatchStart, duration)
				lifecycle = append(lifecycle, Event{"Dispatched", ps.DispatchStart, duration, nil, nil})
			}
			if ps.Erected {
				log.Printf("Adding 'Erected' event with timestamp %v", ps.UpdatedAt)
				lifecycle = append(lifecycle, Event{"Erected", ps.UpdatedAt, "", nil, nil})
			}
			if ps.ReceiveInErection {
				log.Printf("Adding 'Received at Site' event with timestamp %v", ps.UpdatedAt)
				lifecycle = append(lifecycle, Event{"Received at Site", ps.UpdatedAt, "", nil, nil})
			}
		}

		// Sort lifecycle: Element Created first, then stages in order, then other events
		sort.Slice(lifecycle, func(i, j int) bool {
			// Element Created should always be first
			if lifecycle[i].Label == "Element Created" {
				return true
			}
			if lifecycle[j].Label == "Element Created" {
				return false
			}

			// For stages, maintain the order from stage_path
			if lifecycle[i].StageID != nil && lifecycle[j].StageID != nil {
				// Get the order from stage_path
				stageOrder := map[int]int{76: 1, 75: 2, 74: 3, 73: 4, 77: 5}
				orderI := stageOrder[*lifecycle[i].StageID]
				orderJ := stageOrder[*lifecycle[j].StageID]
				return orderI < orderJ
			}

			// For non-stage events, sort by timestamp
			if lifecycle[i].Timestamp.IsZero() && lifecycle[j].Timestamp.IsZero() {
				return false
			}
			if lifecycle[i].Timestamp.IsZero() {
				return false
			}
			if lifecycle[j].Timestamp.IsZero() {
				return true
			}

			return lifecycle[i].Timestamp.Before(lifecycle[j].Timestamp)
		})

		// Debug: Log the complete lifecycle before returning
		log.Printf("Final lifecycle for element_id %d:", elementID)
		for i, event := range lifecycle {
			log.Printf("  %d: %s - %v (StageID: %v)", i, event.Label, event.Timestamp, event.StageID)
		}

		currentStatus := "Unknown"
		if ps.Erected {
			currentStatus = "Erected"
		} else if ps.DispatchStatus {
			currentStatus = "Dispatch"
		} else if ps.Stockyard {
			currentStatus = "Stockyard"
		} else {
			// Find last completed stage, or first incomplete stage
			found := false
			for _, stageID := range stageIDs {
				if _, completed := completedStages[stageID]; completed {
					var stageName string
					_ = db.QueryRowContext(ctxElement, `SELECT name FROM project_stages WHERE id = $1`, stageID).Scan(&stageName)
					currentStatus = "Stage: " + stageName
					found = true
				}
			}
			if !found && len(stageIDs) > 0 {
				var stageName string
				_ = db.QueryRowContext(ctxElement, `SELECT name FROM project_stages WHERE id = $1`, stageIDs[0]).Scan(&stageName)
				currentStatus = "Stage: " + stageName
			}
		}

		// Create element with embedded element type data and lifecycle (answers embedded in lifecycle)
		elementWithDetails := gin.H{
			"id":                   element.Id,
			"element_type_id":      element.ElementTypeID,
			"element_id":           element.ElementId,
			"element_name":         element.ElementName,
			"project_id":           element.ProjectID,
			"created_by":           element.CreatedBy,
			"created_at":           element.CreatedAt,
			"status":               element.Status,
			"element_type_version": element.ElementTypeVersion,
			"update_at":            element.UpdateAt,
			"target_location":      element.TargetLocation,
			"disable":              element.Disable,
			"element_type":         elementType,
			"lifecycle":            lifecycle,
			"CurrentStatus":        currentStatus,
			"submitted_answers":    submittedAnswers,
		}

		// Return element with embedded element type details, submitted answers, and lifecycle
		c.JSON(http.StatusOK, elementWithDetails)

		log := models.ActivityLog{
			EventContext: "Element",
			EventName:    "Get",
			Description:  "Get Complete Element Details",
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

// Helper function to get stage name from answers
func getStageNameFromAnswers(answers []map[string]interface{}) string {
	if len(answers) > 0 {
		if stageName, ok := answers[0]["stage_name"].(*string); ok && stageName != nil {
			return *stageName
		}
	}
	return "Unknown Stage"
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05") // Example format
}

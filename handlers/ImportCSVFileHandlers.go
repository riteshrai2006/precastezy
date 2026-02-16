package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"html"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// ImportCSVBOM godoc
// @Summary      Import BOM from CSV
// @Tags         import
// @Accept       multipart/form-data
// @Produce      json
// @Param        project_id  path    int   true  "Project ID"
// @Param        file        formData  file  true  "CSV file"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/import_csv_bom/{project_id} [post]
func ImportCSVBOM(c *gin.Context) {
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

	project_id := c.Param("project_id")
	if project_id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file not found"})
		return
	}

	ProjectID, err := strconv.Atoi(project_id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to open file"})
		return
	}
	defer src.Close()

	// Read and process the CSV file
	reader := csv.NewReader(src)

	// Read CSV Header
	header, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read CSV header: %v", err)})
		return
	}

	// Decode header names
	for i := range header {
		header[i] = decodeWeirdEncodedText(header[i])
	}

	// Create a map of column names to their indices
	columnIndices := make(map[string]int)
	for i, col := range header {
		columnIndices[col] = i
	}

	// Validate header for necessary columns
	requiredColumns := []string{"ProductName", "ProductType", "Unit", "Rate"}
	for _, col := range requiredColumns {
		if _, exists := columnIndices[col]; !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("missing required column: %s", col)})
			return
		}
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var successCount, errorCount int
	var errors []string

	// Start reading the rows
	for {
		row, err := reader.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			errors = append(errors, fmt.Sprintf("failed to read row: %v", err))
			continue
		}

		// Validate row length
		if len(row) < len(header) {
			errors = append(errors, fmt.Sprintf("row has insufficient columns: %v", row))
			errorCount++
			continue
		}

		// Map the row data to BOMProduct struct
		var bomProduct models.BOMProduct

		// Get values using column indices
		bomProduct.ProductName = strings.TrimSpace(row[columnIndices["ProductName"]])
		bomProduct.ProductType = strings.TrimSpace(row[columnIndices["ProductType"]])
		bomProduct.Unit = strings.TrimSpace(row[columnIndices["Unit"]])

		// Validate required fields
		if bomProduct.ProductName == "" || bomProduct.ProductType == "" || bomProduct.Unit == "" {
			errors = append(errors, fmt.Sprintf("row has empty required fields: %v", row))
			errorCount++
			continue
		}

		bomProduct.NameId = fmt.Sprintf("%s_%s", bomProduct.ProductName, bomProduct.ProductType)
		bomProduct.CreatedAt = time.Now()
		bomProduct.UpdatedAt = time.Now()

		// Check for duplicate name_id
		var exists bool
		err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM inv_bom WHERE project_id = $1 AND name_id = $2)",
			ProjectID, bomProduct.NameId).Scan(&exists)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to check for duplicate name_id: %v", err))
			errorCount++
			continue
		}
		if exists {
			errors = append(errors, fmt.Sprintf("duplicate name_id found: %s", bomProduct.NameId))
			errorCount++
			continue
		}

		// Insert into the database
		query := `
            INSERT INTO inv_bom (project_id, product_name, product_type, unit, created_at, updated_at, name_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8);`
		_, err = tx.Exec(query,
			ProjectID,
			bomProduct.ProductName,
			bomProduct.ProductType,
			bomProduct.Unit,
			bomProduct.CreatedAt,
			bomProduct.UpdatedAt,

			bomProduct.NameId,
		)
		if err != nil {
			errors = append(errors, fmt.Sprintf("database insertion failed: %v", err))
			errorCount++
			continue
		}

		successCount++
	}

	// Commit transaction if no errors
	if errorCount == 0 {
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit transaction"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": fmt.Sprintf("Successfully imported %d records", successCount),
		})

		// Get userID from session
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", ProjectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", ProjectID)
			}

			// Send notification to the user who imported the BOM
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("BOM imported successfully for project: %s (%d records)", projectName, successCount),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", ProjectID),
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
			sendProjectNotifications(db, ProjectID,
				fmt.Sprintf("BOM imported successfully for project: %s (%d records)", projectName, successCount),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", ProjectID))
		}
	} else {
		// Rollback transaction if there were errors
		tx.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   fmt.Sprintf("Import failed with %d errors. %d records imported successfully.", errorCount, successCount),
			"details": errors,
		})
	}

	// Log activity
	activityLog := models.ActivityLog{
		EventContext: "Import BOM",
		EventName:    "Import",
		Description:  "User imported BOM from CSV",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    ProjectID,
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

// ImportCSVPrecast godoc
// @Summary      Import precast from CSV
// @Tags         import
// @Accept       multipart/form-data
// @Produce      json
// @Param        project_id  path    int   true  "Project ID"
// @Param        file        formData  file  true  "CSV file"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/import_csv/{project_id} [post]
func ImportCSVPrecast(c *gin.Context) {
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

	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file not found"})
		return
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to open file"})
		return
	}
	defer src.Close()

	// Read and process the CSV file
	reader := csv.NewReader(src)

	// Read CSV Header
	header, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read CSV header: %v", err)})
		return
	}

	// Decode header names
	for i := range header {
		header[i] = decodeWeirdEncodedText(header[i])
	}

	// Print header
	log.Printf("CSV Header: %v", header)

	// Validate header for necessary columns
	requiredColumns := []string{"ID", "ProjectID", "Name", "Description", "ParentID", "Prefix", "Path", "NamingConvention"}
	for _, col := range requiredColumns {
		if !contains(header, col) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("missing required column: %s", col)})
			return
		}
	}

	// Track unique project IDs for notifications
	projectIDs := make(map[int]bool)

	// Start reading the rows
	rowCount := 0
	for {
		row, err := reader.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read row: %v", err)})
			return
		}

		rowCount++
		log.Printf("Row %d: %v", rowCount, row)

		// Map the row data to your Precast struct
		var precast models.Precast

		precast.ID, err = strconv.Atoi(row[0])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ID: %v", err)})
			return
		}

		precast.ProjectID, err = strconv.Atoi(row[1])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ProjectID: %v", err)})
			return
		}

		// Track project ID for notifications
		projectIDs[precast.ProjectID] = true

		precast.Name = row[2]
		precast.Description = row[3]

		// Handle nullable ParentID
		if row[4] == "" {
			precast.ParentID = nil
		} else {
			parentID, err := strconv.ParseInt(row[4], 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ParentID: %v", err)})
				return
			}
			precast.ParentID = &parentID
		}

		precast.Prefix = row[5]
		precast.Path = row[6]
		// Set default naming convention if not provided
		precast.NamingConvention = row[7]

		// Insert into the database
		query := `
            INSERT INTO precast (id, project_id, name, description, parent_id, prefix, path, naming_convention)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		_, err = db.Exec(query,
			precast.ID,
			precast.ProjectID,
			precast.Name,
			precast.Description,
			precast.ParentID,
			precast.Prefix,
			precast.Path,
			precast.NamingConvention,
		)
		if err != nil {
			log.Printf("Database insertion failed for ID %d: %v", precast.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("database insertion failed: %v", err)})
			return
		}
		log.Printf("Successfully inserted precast with ID: %d, Name: %s", precast.ID, precast.Name)
	}

	// Success response
	log.Printf("Total rows processed: %d", rowCount)
	c.JSON(http.StatusOK, gin.H{"message": "file imported successfully"})

	// Get userID from session
	var userID int
	err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
	if err != nil {
		log.Printf("Failed to fetch user_id for notification: %v", err)
	} else {
		// Send notifications for each unique project
		for projectID := range projectIDs {
			// Get project name for notification
			var projectName string
			err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
			if err != nil {
				log.Printf("Failed to fetch project name: %v", err)
				projectName = fmt.Sprintf("Project %d", projectID)
			}

			// Send notification to the user who imported the precast data
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Precast structure imported successfully for project: %s", projectName),
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
				fmt.Sprintf("Precast structure imported successfully for project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/structure", projectID))
		}
	}

	// Log activity
	activityLog := models.ActivityLog{
		EventContext: "Import Precast",
		EventName:    "Import",
		Description:  "User imported precast data from CSV",
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    0,
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

func InsertMultipleElements(c *gin.Context, elements []models.ElementType) error {
	return InsertMultipleElementsInBatches(c, elements, 15) // Default batch size of 5
}

// InsertMultipleElementsInBatches processes elements in batches to avoid timeout errors
func InsertMultipleElementsInBatches(c *gin.Context, elements []models.ElementType, batchSize int) error {
	return InsertMultipleElementsInBatchesConcurrent(c, elements, batchSize, 15) // Default to 15 concurrent batches
}

// InsertMultipleElementsInBatchesConcurrent processes multiple batches concurrently
func InsertMultipleElementsInBatchesConcurrent(c *gin.Context, elements []models.ElementType, batchSize int, maxConcurrentBatches int) error {
	totalElements := len(elements)
	totalBatches := (totalElements + batchSize - 1) / batchSize

	log.Printf("Starting concurrent batch processing of %d elements with batch size %d and %d concurrent batches",
		totalElements, batchSize, maxConcurrentBatches)

	// Adaptive throttling based on database load
	// Reduce concurrent batches if we're processing a large dataset
	if totalElements > 1000 {
		adaptiveBatches := maxConcurrentBatches
		if adaptiveBatches > 6 {
			adaptiveBatches = 6
			log.Printf("Reduced concurrent batches to %d for large dataset (%d elements)", adaptiveBatches, totalElements)
		}
		maxConcurrentBatches = adaptiveBatches
	}

	// Create a semaphore to limit concurrent batches
	semaphore := make(chan struct{}, maxConcurrentBatches)

	// Create channels for results and errors
	resultChan := make(chan batchResult, totalBatches)
	errorChan := make(chan error, totalBatches)

	// Create a WaitGroup to wait for all goroutines to complete
	var wg sync.WaitGroup

	// Process elements in batches concurrently
	for i := 0; i < totalElements; i += batchSize {
		end := i + batchSize
		if end > totalElements {
			end = totalElements
		}

		batch := elements[i:end]
		batchNumber := (i / batchSize) + 1

		wg.Add(1)
		go func(batch []models.ElementType, batchNum int) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("Starting batch %d/%d with %d elements", batchNum, totalBatches, len(batch))

			startTime := time.Now()
			if err := processBatch(c, batch); err != nil {
				log.Printf("Error processing batch %d: %v", batchNum, err)
				errorChan <- fmt.Errorf("failed to process batch %d: %w", batchNum, err)
				return
			}

			duration := time.Since(startTime)
			log.Printf("Successfully completed batch %d/%d in %v", batchNum, totalBatches, duration)

			resultChan <- batchResult{
				batchNumber:  batchNum,
				duration:     duration,
				elementCount: len(batch),
			}
		}(batch, batchNumber)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(resultChan)
	close(errorChan)

	// Check for any errors
	select {
	case err := <-errorChan:
		return err
	default:
		// No errors, collect results
		var results []batchResult
		for result := range resultChan {
			results = append(results, result)
		}

		// Sort results by batch number for consistent logging
		sort.Slice(results, func(i, j int) bool {
			return results[i].batchNumber < results[j].batchNumber
		})

		// Log summary
		var totalDuration time.Duration
		var totalElementsProcessed int
		for _, result := range results {
			totalDuration += result.duration
			totalElementsProcessed += result.elementCount
		}

		log.Printf("All %d elements processed successfully in %d concurrent batches in %v",
			totalElementsProcessed, totalBatches, totalDuration)

		return nil
	}
}

// batchResult holds the result of processing a batch
type batchResult struct {
	batchNumber  int
	duration     time.Duration
	elementCount int
}

// processBatch handles a single batch of elements
func processBatch(c *gin.Context, batch []models.ElementType) error {
	db := storage.GetDB()

	for _, element := range batch {
		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Failed to begin transaction: %v", err)
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		// Prepare default values
		element.CreatedAt = time.Now()
		element.UpdatedAt = time.Now()
		element.TotalCountElement = 0
		element.ElementTypeVersion = repository.GenerateVersionCode("")

		// Serialize HierarchyQuantity to JSON
		HierarchyQuantityJSON, err := json.Marshal(element.HierarchyQ)
		if err != nil {
			log.Printf("Failed to marshal hierarchy quantity: %v", err)
			return fmt.Errorf("failed to marshal hierarchy quantity: %w", err)
		}

		// Define the SQL Query
		elementQuery := `
		INSERT INTO element_type (
			element_type, element_type_name, thickness, length, height, volume, mass, area, width,
			created_by, created_at, update_at, project_id, element_type_version, total_count_element, hierarchy_quantity, density
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING element_type_id;
	`

		// Execute Query
		err = tx.QueryRow(elementQuery,
			element.ElementType, element.ElementTypeName, element.Thickness, element.Length, element.Height,
			element.Volume, element.Mass, element.Area, element.Width, element.CreatedBy, element.CreatedAt, element.UpdatedAt,
			element.ProjectID, element.ElementTypeVersion, element.TotalCountElement, HierarchyQuantityJSON,
			element.Density,
		).Scan(&element.ElementTypeId)

		if err != nil {
			log.Printf("Failed to insert element type. Element: %+v, Error: %v", element, err)
			return fmt.Errorf("failed to insert element type: %w", err)
		}

		if len(element.Stage) > 0 {
			stage := element.Stage[0]
			stageQuery := `INSERT INTO element_type_path (element_type_id, stage_path) VALUES ($1, $2)`
			_, err := tx.Exec(stageQuery, element.ElementTypeId, pq.Array(stage.StagePath))
			if err != nil {
				log.Printf("Failed to insert stage path. ElementTypeID: %d, StagePath: %v, Error: %v",
					element.ElementTypeId, stage.StagePath, err)
				return fmt.Errorf("failed to insert stage path: %w", err)
			}
		}

		// Insert Drawings
		for _, drawing := range element.Drawings {
			drawing.ProjectId = element.ProjectID
			drawing.ElementTypeID = element.ElementTypeId
			drawing.CreatedBy = element.CreatedBy
			drawing.UpdatedBy = element.CreatedBy
			drawing.ProjectId = element.ProjectID

			err := CreateDrawingWithTx(c, drawing, tx)
			if err != nil {
				log.Printf("Failed to insert drawing. Drawing: %+v, Error: %v", drawing, err)
				return fmt.Errorf("failed to insert drawing: %w", err)
			}
		}

		for _, HQ := range element.HierarchyQ {
			// Get naming convention from precast table
			var namingConvention sql.NullString
			err := tx.QueryRow(`SELECT naming_convention FROM precast WHERE id = $1`, HQ.HierarchyId).Scan(&namingConvention)
			if err != nil {
				if err == sql.ErrNoRows {
					log.Printf("HierarchyId %d not found in precast", HQ.HierarchyId)
					return fmt.Errorf("hierarchy ID %d not found in precast", HQ.HierarchyId)
				}
				log.Printf("Database error while retrieving naming convention. HierarchyID: %d, Error: %v",
					HQ.HierarchyId, err)
				return fmt.Errorf("database error while retrieving naming convention: %w", err)
			}

			// Use empty string if naming_convention is NULL
			namingConventionValue := ""
			if namingConvention.Valid {
				namingConventionValue = namingConvention.String
			}

			HqQuery := `
				INSERT INTO element_type_hierarchy_quantity (
					element_type_id, hierarchy_id, quantity, naming_convention,
					element_type_name, element_type, left_quantity, project_id
				) VALUES ($1, $2, $3, $4, $5, $6, 0, $7)
			`

			_, err = tx.Exec(HqQuery,
				element.ElementTypeId,
				HQ.HierarchyId,
				HQ.Quantity,
				namingConventionValue,
				element.ElementTypeName,
				element.ElementType,
				element.ProjectID,
			)

			if err != nil {
				log.Printf("Failed to insert hierarchy quantity. HQ: %+v, Error: %v", HQ, err)
				return fmt.Errorf("failed to insert hierarchy quantity: %w", err)
			}
		}

		// Prepare BOMPro Data
		productJSON, err := json.Marshal(element.Products)
		if err != nil {
			log.Printf("Failed to marshal products. Products: %+v, Error: %v", element.Products, err)
			return fmt.Errorf("failed to marshal products: %w", err)
		}

		bomproQuery := `
			INSERT INTO element_type_bom (
				element_type_id, project_id, created_at, created_by, 
				updated_at, updated_by, product
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`

		_, err = tx.Exec(bomproQuery,
			element.ElementTypeId,
			element.ProjectID,
			time.Now(),
			element.CreatedBy,
			time.Now(),
			element.CreatedBy,
			string(productJSON),
		)

		if err != nil {
			log.Printf("Failed to insert BOM. ElementTypeID: %d, Error: %v", element.ElementTypeId, err)
			return fmt.Errorf("failed to insert BOM: %w", err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit transaction. ElementTypeID: %d, Error: %v", element.ElementTypeId, err)
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		// Process hierarchy data
		jsondata := models.ElementInput{
			ElementTypeID:      element.ElementTypeId,
			SessionID:          element.SessionID,
			ProjectID:          element.ProjectID,
			ElementType:        element.ElementType,
			ElementTypeName:    element.ElementTypeName,
			ElementTypeVersion: element.ElementTypeVersion,
			TotalCountElement:  element.TotalCountElement,
		}
		ProcessHierarchyData(c, element.HierarchyQ, jsondata)
	}

	return nil
}

func decodeWeirdEncodedText(input string) string {
	replacer := strings.NewReplacer(
		"+ACY-", "&",
		"+AF8-", "_",
		"+ACo-", "*",
		"+AC0-", "-",
		"+ACs-", "'",
		"+ACQ-", "\"",
		"+ACU-", "%",
		"+ACg-", "(",
		"+ACk-", ")",
		"+ACo-", "+",
		"+ACs-", ",",
		"+AC0-", ".",
		"+ACU-", "/",
		"+ACg-", ":",
		"+ACk-", ";",
		"+ACo-", "<",
		"+ACs-", "=",
		"+AC0-", ">",
		"+ACU-", "?",
		"+ACg-", "@",
		"+ACk-", "[",
		"+ACo-", "\\",
		"+ACs-", "]",
		"+AC0-", "^",
		"+ACU-", "`",
		"+ACg-", "{",
		"+ACk-", "|",
		"+ACo-", "}",
		"+ACs-", "~",
		"+", " ", // Replace any remaining + with space
	)
	decoded := html.UnescapeString(input)
	return replacer.Replace(decoded)
}

// ImportElementTypeCSVHandler godoc
// @Summary      Import element types from CSV
// @Tags         import
// @Accept       multipart/form-data
// @Produce      json
// @Param        project_id  path    int   true  "Project ID"
// @Param        file        formData  file  true  "CSV file"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/import_csv_element_type/{project_id} [post]
func ImportElementTypeCSVHandler(c *gin.Context) {
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

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20) // 10 MB

	// Get project_id from URL parameter
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

	// Get initial table counts before processing CSV
	var drawingTypeCount, stageCount, precastCount, invBomCount int

	// Count drawing types
	err = db.QueryRow("SELECT COUNT(*) FROM drawing_type WHERE project_id = $1", projectID).Scan(&drawingTypeCount)
	if err != nil {
		log.Printf("Error counting drawing types: %v", err)
	}
	log.Printf("Drawing Type Count: %d", drawingTypeCount)

	// Count stages
	err = db.QueryRow("SELECT COUNT(*) FROM project_stages WHERE project_id = $1", projectID).Scan(&stageCount)
	if err != nil {
		log.Printf("Error counting stages: %v", err)
	}
	log.Printf("Stage Count: %d", stageCount)

	// Count precast fields
	err = db.QueryRow("SELECT COUNT(*) FROM precast WHERE project_id = $1", projectID).Scan(&precastCount)
	if err != nil {
		log.Printf("Error counting precast fields: %v", err)
	}
	log.Printf("Precast Count: %d", precastCount)

	// Count inv_bom fields
	err = db.QueryRow("SELECT COUNT(*) FROM inv_bom WHERE project_id = $1", projectID).Scan(&invBomCount)
	if err != nil {
		log.Printf("Error counting inv_bom fields: %v", err)
	}
	log.Printf("Inv BOM Count: %d", invBomCount)

	// Now process the CSV file
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File upload failed"})
		return
	}

	openedFile, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to open file"})
		return
	}
	defer openedFile.Close()

	reader := csv.NewReader(openedFile)
	header, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV header"})
		return
	}

	// Decode header names
	for i := range header {
		header[i] = decodeWeirdEncodedText(header[i])
	}

	if len(header) < 14 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid CSV format, insufficient columns"})
		return
	}

	// sessionID := c.GetHeader("Authorization")
	// if sessionID == "" {
	// 	c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
	// 	return
	// }

	var elementTypes []models.ElementType

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 14 {
			continue
		}

		// Decode record data
		for i := range record {
			record[i] = decodeWeirdEncodedText(record[i])
		}

		height, _ := strconv.ParseFloat(record[2], 64)
		length, _ := strconv.ParseFloat(record[3], 64)
		thickness, _ := strconv.ParseFloat(record[4], 64)
		mass, _ := strconv.ParseFloat(record[5], 64)
		volume, _ := strconv.ParseFloat(record[6], 64)
		area, _ := strconv.ParseFloat(record[7], 64)
		width, _ := strconv.ParseFloat(record[8], 64)

		heightM := height / 1000
		lengthM := length / 1000
		thicknessM := thickness / 1000
		volumeM3 := heightM * lengthM * thicknessM

		var density float64
		if volumeM3 > 0.000001 { // Add minimum threshold to prevent division by very small numbers
			density = mass / volumeM3
			// Add maximum density limit (e.g., 10000 kg/m³)
			if density > 10000 {
				density = 10000
			}
		} else {
			density = 0
		}

		// Validate other numeric fields
		if height > 1000000 || mass > 1000000 || length > 1000000 || thickness > 1000000 || volume > 1000000 || area > 1000000 || width > 1000000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "numeric values exceed maximum allowed limits"})
			return
		}

		// ---- Stage fields (from index 10 onwards until stageCount)
		stageCache := make(map[string][]int)
		var allStageIDs []int
		var Stages []models.Stages

		stageStart := 9
		stageEnd := stageStart + stageCount
		drawingStart := stageEnd
		drawingEnd := drawingStart + drawingTypeCount

		for i := stageStart; i < stageEnd && i < len(header); i++ {
			stageName := html.UnescapeString(header[i])
			cellValue := strings.TrimSpace(strings.ToLower(record[i]))
			if cellValue != "yes" && cellValue != "1" {
				continue
			}

			var stageIDs []int
			if cached, ok := stageCache[stageName]; ok {
				stageIDs = cached
			} else {
				rows, err := db.Query("SELECT id FROM project_stages WHERE name = $1 AND project_id = $2", stageName, projectID)
				if err != nil {
					log.Printf("Error querying stage '%s' for project %d: %v", stageName, projectID, err)
					continue
				}
				defer rows.Close()
				for rows.Next() {
					var id int
					if err := rows.Scan(&id); err == nil {
						stageIDs = append(stageIDs, id)
					}
				}
				stageCache[stageName] = stageIDs
			}
			allStageIDs = append(allStageIDs, stageIDs...)
		}

		if len(allStageIDs) > 0 {
			Stages = append(Stages, models.Stages{
				StagePath: allStageIDs,
			})
		}

		// ---- Drawing fields (from drawingStart to drawingEnd)
		drawings := []models.Drawings{}
		drawingTypeCache := map[string]int{}

		for i := drawingStart; i < drawingEnd && i < len(header) && i < len(record); i++ {
			drawingTypeName := strings.TrimSpace(header[i])
			// Decode the drawing type name to handle special characters
			drawingTypeName = decodeWeirdEncodedText(drawingTypeName)
			file := strings.TrimSpace(record[i])

			if file == "" {
				continue
			}

			// Get drawing_type_id from cache or DB
			drawingTypeID, found := drawingTypeCache[drawingTypeName]
			if !found {
				err := db.QueryRow(
					"SELECT drawing_type_id FROM drawing_type WHERE project_id = $1 AND drawing_type_name = $2",
					projectID, drawingTypeName,
				).Scan(&drawingTypeID)

				if err != nil {
					log.Printf("Drawing type not found: %s (project %d): %v", drawingTypeName, projectID, err)
					continue
				}
				drawingTypeCache[drawingTypeName] = drawingTypeID
			}

			drawings = append(drawings, models.Drawings{
				DrawingsId:      repository.GenerateRandomNumber(),
				CreatedAt:       time.Now(),
				UpdateAt:        time.Now(),
				ProjectId:       projectID,
				CurrentVersion:  "VR-1",
				File:            file,
				DrawingTypeId:   drawingTypeID,
				DrawingTypeName: drawingTypeName,
				ElementTypeID:   0, // To be set later
			})
		}

		// ---- Hierarchy fields (from drawingEnd to drawingEnd + precastCount)
		hierarchyStart := drawingEnd
		fmt.Println("hierarchyStart=", hierarchyStart)
		hierarchyEnd := hierarchyStart + precastCount
		hierarchyQuantity := []models.HierarchyQuantity{}

		for i := hierarchyStart; i < hierarchyEnd && i+1 < len(record); i++ {
			quantity, err2 := strconv.Atoi(record[i])
			if err2 != nil {
				continue
			}

			rawHeaderName := header[i]
			decodedHeaderName := decodeWeirdEncodedText(rawHeaderName)

			// Fetch hierarchyID from the precast table based on decodedHeaderName and projectID
			var hierarchyID int
			query := `SELECT id FROM precast WHERE path = $1 AND project_id = $2 LIMIT 1`
			err := db.QueryRow(query, decodedHeaderName, projectID).Scan(&hierarchyID)
			if err != nil {
				log.Printf("Failed to find precast ID for name: %s, project_id: %d, err: %v", decodedHeaderName, projectID, err)
				continue
			}
			fmt.Println("Hierarchy ID:", hierarchyID)
			hierarchyQuantity = append(hierarchyQuantity, models.HierarchyQuantity{
				HierarchyId:      hierarchyID,
				Quantity:         quantity,
				NamingConvention: fmt.Sprintf("%s-%d", decodedHeaderName, hierarchyID),
			})
		}

		// Process Products dynamically
		products := []models.Product{}
		invBomCache := make(map[string]int) // Cache for cleaned name_id to productID

		for i := hierarchyEnd; i < len(record); i++ {
			rawNameID := header[i]

			// Decode and clean nameID
			cleanNameID := decodeWeirdEncodedText(rawNameID)

			// Parse quantity
			quantity, err := strconv.ParseFloat(record[i], 64)
			if err != nil || quantity == 0 {
				continue // Skip invalid or zero values
			}

			// Lookup in cache or database
			productID, found := invBomCache[cleanNameID]
			if !found {
				err = db.QueryRow("SELECT id FROM inv_bom WHERE project_id = $1 AND name_id = $2", projectID, cleanNameID).Scan(&productID)
				if err != nil {
					if err == sql.ErrNoRows {
						log.Printf("Product not found for name_id: %s", cleanNameID)
					} else {
						log.Printf("DB error for name_id %s: %v", cleanNameID, err)
					}
					continue
				}
				invBomCache[cleanNameID] = productID
			}

			// Append to products
			products = append(products, models.Product{
				ProductID:   productID,
				ProductName: cleanNameID,
				Quantity:    quantity,
			})
		}

		// ---- Final element type
		elementType := models.ElementType{
			ElementType:        record[0],
			ElementTypeName:    record[1],
			Height:             height,
			Length:             length,
			Thickness:          thickness,
			Mass:               mass,
			Volume:             volume,
			Area:               area,
			Width:              width,
			ProjectID:          projectID,
			CreatedBy:          "admin",
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			ElementTypeVersion: "VR-1",
			SessionID:          sessionID,
			TotalCountElement:  len(hierarchyQuantity),
			HierarchyQ:         hierarchyQuantity,
			Density:            density,
			Stage:              Stages,
			Drawings:           drawings,
			Products:           products,
		}

		elementTypes = append(elementTypes, elementType)
	}
	// Get batch size from query parameter, default to 30
	batchSizeStr := c.DefaultQuery("batch_size", "30")
	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil || batchSize < 1 || batchSize > 50 {
		batchSize = 30 // Default to 30 if invalid
	}

	// Get concurrent batches from query parameter, default to 15
	concurrentBatchesStr := c.DefaultQuery("concurrent_batches", "15")
	concurrentBatches, err := strconv.Atoi(concurrentBatchesStr)
	if err != nil || concurrentBatches < 1 || concurrentBatches > 20 {
		concurrentBatches = 15 // Default to 15 if invalid
	}

	// Create job manager
	jobManager := NewGormJobManager()

	// Create a new import job and get job ID
	jobID, err := jobManager.CreateImportJobAndGetID(c, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	// Start background processing
	go func() {
		// Create a cancellable context for this job
		ctx, cancel := context.WithCancel(context.Background())
		jobManager.registerJob(jobID, cancel)

		// Ensure job is unregistered when it completes
		defer func() {
			jobManager.unregisterJob(jobID)
		}()

		// Use enhanced cancellation method for better goroutine cleanup
		jobManager.ProcessElementTypeImportJobWithEnhancedCancellation(ctx, jobID, projectID, elementTypes, batchSize, concurrentBatches)
	}()

	// Return job ID immediately
	c.JSON(http.StatusOK, gin.H{
		"message":            "Import job started successfully",
		"job_id":             jobID,
		"status":             "pending",
		"total_elements":     len(elementTypes),
		"batch_size":         batchSize,
		"concurrent_batches": concurrentBatches,
	})

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

		// Send notification to the user who imported the element types
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Element types import started for project: %s (%d elements)", projectName, len(elementTypes)),
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

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Element types import started for project: %s (%d elements)", projectName, len(elementTypes)),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID))
	}

	// Log activity
	activityLog := models.ActivityLog{
		EventContext: "Import Element Types",
		EventName:    "Import",
		Description:  "User imported element types from CSV",
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

// ImportElementTypeExcelHandler handles the import of element types from Excel.
func ImportElementTypeExcelHandler(c *gin.Context) {
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

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20) // 10 MB

	// Get project_id from URL parameter
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

	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File upload failed"})
		return
	}

	// Upload file to server first using existing upload function
	// Try multiple directory options for better compatibility
	importDir := "/var/www/dataprecast/"

	// Ensure the import directory exists with proper permissions
	if err := EnsureDirectoryExists(importDir); err != nil {
		// Fallback to local directory if server directory fails
		fallbackDir := "./imports/"
		if err := EnsureDirectoryExists(fallbackDir); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":          "Failed to create import directories",
				"details":        err.Error(),
				"attempted_dirs": []string{importDir, fallbackDir},
			})
			return
		}
		importDir = fallbackDir
	}

	filePath, err := UploadFileToDirectory(file, importDir, 10<<20) // 10 MB limit
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     "Failed to upload file to server",
			"details":   err.Error(),
			"directory": importDir,
		})
		return
	}

	// Create job manager
	jobManager := NewGormJobManager()

	// Create a new import job and get job ID with file path
	jobID, err := jobManager.CreateImportJobAndGetID(c, &filePath)
	if err != nil {
		// Clean up uploaded file if job creation fails
		//CleanupFile(filePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	// Get batch size from query parameter, default to 30
	batchSizeStr := c.DefaultQuery("batch_size", "30")
	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil || batchSize < 1 || batchSize > 50 {
		batchSize = 30 // Default to 30 if invalid
	}

	// Get concurrent batches from query parameter, default to 15
	concurrentBatchesStr := c.DefaultQuery("concurrent_batches", "15")
	concurrentBatches, err := strconv.Atoi(concurrentBatchesStr)
	if err != nil || concurrentBatches < 1 || concurrentBatches > 20 {
		concurrentBatches = 15 // Default to 15 if invalid
	}

	// Return job ID immediately to prevent timeout
	c.JSON(http.StatusOK, gin.H{
		"message":            "Import job started successfully",
		"job_id":             jobID,
		"status":             "pending",
		"batch_size":         batchSize,
		"concurrent_batches": concurrentBatches,
		"file_path":          filePath,
	})

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

		// Send notification to the user who imported the element types
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("Element types import started for project: %s", projectName),
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

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Element types import started for project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/element", projectID))
	}

	// Log activity in background to avoid blocking response
	go func() {
		activityLog := models.ActivityLog{
			EventContext: "Import Element Type",
			EventName:    "Import",
			Description:  "Import Element Type Excel",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			log.Printf("Failed to log activity: %v", logErr)
		}
	}()

	// Start background processing after response is sent
	go func() {
		// Create a cancellable context for this job
		ctx, cancel := context.WithCancel(context.Background())
		jobManager.registerJob(jobID, cancel)

		// Ensure job is unregistered when it completes
		defer func() {
			jobManager.unregisterJob(jobID)
		}()

		// Use enhanced cancellation method for better goroutine cleanup
		jobManager.ProcessElementTypeExcelImportJobFromPathWithEnhancedCancellation(ctx, jobID, projectID, filePath, batchSize, concurrentBatches, userName, session)
	}()
}

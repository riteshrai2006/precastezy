package handlers

import (
	"backend/models"
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
)

// CreateBOMProduct creates one or more BOM products.
// @Summary Create BOM products
// @Description Create BOM products. Send an array of products (bom_name, bom_type, unit, project_id). Requires Authorization header.
// @Tags BOM
// @Accept json
// @Produce json
// @Param body body []models.BOMProduct true "Array of BOM products to create"
// @Success 201 {object} models.CreateBOMProductResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/create_bom_products [post]
func CreateBOMProduct(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var products []models.BOMProduct

		// Bind JSON input to array of BOMProduct structs
		if err := c.ShouldBindJSON(&products); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var createdProducts []models.BOMProduct
		var projectID int

		// Process each BOM product
		for _, product := range products {
			// Generate name_id automatically as bom_name_bom_type
			product.NameId = fmt.Sprintf("%s_%s", product.ProductName, product.ProductType)

			// SQL Query for insertion
			query := `
				INSERT INTO inv_bom (
					product_name, product_type, unit, created_at, updated_at, project_id, name_id, master_bom_id
				) VALUES (
					$1, $2, $3, now(), now(), $4, $5, $6
				) RETURNING id, created_at, updated_at`

			// Execute the query and scan the returned fields
			err = db.QueryRow(
				query,
				product.ProductName, // Maps to product_name column
				product.ProductType, // Maps to product_type column
				product.Unit,        // Maps to unit column
				product.ProjectId,   // Maps to project_id column
				product.NameId,      // Auto-generated name_id (bom_name_bom_type)
				product.MasterBomId, // May be nil → NULL
			).Scan(&product.ID, &product.CreatedAt, &product.UpdatedAt)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create BOM product %s: %v", product.ProductName, err)})
				return
			}

			createdProducts = append(createdProducts, product)
			projectID = product.ProjectId
		}

		// Return the inserted products as a JSON response
		c.JSON(http.StatusCreated, gin.H{
			"message":  "BOM products created successfully",
			"products": createdProducts,
			"count":    len(createdProducts),
		})

		// Get project name for logging
		var name string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve project name", "details": err.Error()})
			return
		}

		// Log the activity
		activityLog := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Create",
			Description:  fmt.Sprintf("Created %d BOM products for project %s (ID: %d)", len(createdProducts), name, projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "BOM products created but failed to log activity", "details": logErr.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who created the BOM products
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Created %d BOM product(s) for project: %s", len(createdProducts), name),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", projectID),
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
			fmt.Sprintf("Created %d BOM product(s) for project: %s", len(createdProducts), name),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", projectID))
	}
}

// GetAllBOMProducts returns all BOM products.
// @Summary Get all BOM products
// @Description Returns all BOM products. Requires Authorization header.
// @Tags BOM
// @Accept json
// @Produce json
// @Success 200 {array} models.BOMProduct
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_bom_products [get]
func GetAllBOMProducts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
		}

		// Define a slice to hold all BOM products
		var products []models.BOMProduct

		// Query to select all products
		query := `SELECT id, product_name, product_type, unit, created_at, updated_at, project_id FROM inv_bom`
		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		// Iterate through the rows and scan each product into the slice
		for rows.Next() {
			var product models.BOMProduct
			err := rows.Scan(
				&product.ID,
				&product.ProductName,
				&product.ProductType,
				&product.Unit,
				&product.CreatedAt,
				&product.UpdatedAt,
				&product.ProjectId,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			products = append(products, product)
		}

		// Check for errors after iterating through rows
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return all BOM products
		c.JSON(http.StatusOK, products)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  "Retrieved all BOM products",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // Assuming 0 for global context, adjust as needed
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}
	}

}

// GetAllBOMProductsProjectId returns BOM products for a project.
// @Summary Get BOM products by project ID
// @Description Returns all BOM products for the given project_id. Requires Authorization header.
// @Tags BOM
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.BOMProduct
// @Success 204 "No products found"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/fetch_bom_products/{project_id} [get]
func GetAllBOMProductsProjectId(db *sql.DB) gin.HandlerFunc {
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

		// Get the project_id from the request query parameters
		ProjectID := c.Param("project_id")
		if ProjectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}
		projectID, err := strconv.Atoi(ProjectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id must be a valid integer"})
			return
		}
		// Define a slice to hold all BOM products
		var products []models.BOMProduct

		// Query to select BOM products for the given project_id
		query := `SELECT id, product_name, product_type, unit, created_at, updated_at, project_id, name_id, master_bom_id
		          FROM inv_bom WHERE project_id = $1
				  ORDER BY product_name ASC, product_type ASC`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		// Iterate through the rows and scan each product into the slice
		for rows.Next() {
			var product models.BOMProduct
			var master sql.NullInt32
			err := rows.Scan(
				&product.ID,
				&product.ProductName,
				&product.ProductType,
				&product.Unit,
				&product.CreatedAt,
				&product.UpdatedAt,
				&product.ProjectId,
				&product.NameId,
				&master,
			)
			if master.Valid {
				v := int(master.Int32)
				product.MasterBomId = &v
			} else {
				product.MasterBomId = nil
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			products = append(products, product)
		}

		// Check for errors after iterating through rows
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return an empty array with status 200 if no products are found
		if len(products) == 0 {
			c.Status(http.StatusNoContent)
			return
		}

		// Return all BOM products for the specified project
		c.JSON(http.StatusOK, products)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  fmt.Sprintf("Retrieved BOM products for project ID %d", projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}

	}
}

// GetBOMProductByID returns a single BOM product by ID.
// @Summary Get BOM product by ID
// @Description Returns one BOM product by id. Requires Authorization header.
// @Tags BOM
// @Accept json
// @Produce json
// @Param id path int true "BOM product ID"
// @Success 200 {object} models.BOMProduct
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_bom_products/{id} [get]
func GetBOMProductByID(db *sql.DB) gin.HandlerFunc {
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

		// Extract the 'id' from the URL path parameter
		idStr := c.Param("id")
		if idStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}

		// Convert 'id' from string to integer
		ID, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a valid integer"})
			return
		}

		// Define a variable to hold the retrieved product
		var product models.BOMProduct

		// Query to fetch the product by ID
		query := `SELECT id, product_name, product_type, unit, created_at, updated_at, project_id
                  FROM inv_bom WHERE id = $1`

		// Execute the query
		err = db.QueryRow(query, ID).Scan(
			&product.ID,
			&product.ProductName,
			&product.ProductType,
			&product.Unit,
			&product.CreatedAt,
			&product.UpdatedAt,
			&product.ProjectId,
		)

		// Handle query errors
		if err == sql.ErrNoRows {
			// No product found
			c.JSON(http.StatusNotFound, gin.H{"error": "No BOM product found with the given ID"})
			return
		} else if err != nil {
			// Other errors
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return the retrieved product
		c.JSON(http.StatusOK, product)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  fmt.Sprintf("Retrieved BOM product with ID %d", ID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    product.ProjectId, // Assuming the product has a ProjectId field
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}
	}
}

// UpdateBOMProduct updates an existing BOM product by ID.
// @Summary Update BOM product
// @Description Update a BOM product by ID. Send the product fields in the request body. Requires Authorization header with session ID.
// @Tags BOM
// @Accept json
// @Produce json
// @Param id path int true "BOM product ID (e.g. 1371)"
// @Param body body models.BOMProduct true "BOM product data to update"
// @Success 200 {object} models.UpdateBOMProductResponse "Returns success message"
// @Failure 400 {object} models.ErrorResponse "Missing session, invalid request data, or missing required fields (bom_name, bom_type, unit)"
// @Failure 401 {object} models.ErrorResponse "Invalid session"
// @Failure 500 {object} models.ErrorResponse "Update failed or activity log error"
// @Router /api/update_bom_products/{id} [put]
func UpdateBOMProduct(db *sql.DB) gin.HandlerFunc {
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

		id := c.Param("id") // Extract ID from the URL parameter

		var product models.BOMProduct
		// Bind JSON payload to the `product` struct
		if err := c.ShouldBindJSON(&product); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data: " + err.Error()})
			return
		}

		// Validate required fields
		if product.ProductName == "" || product.ProductType == "" || product.Unit == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required fields: product_name, product_type, or unit"})
			return
		}

		// Generate name_id automatically as bom_name_bom_type
		product.NameId = fmt.Sprintf("%s_%s", product.ProductName, product.ProductType)

		// SQL Query for updating the record
		query := `
			UPDATE inv_bom 
			SET product_name = $1, product_type = $2, unit = $3, project_id = $4, name_id = $5, master_bom_id = $6, updated_at = now() 
			WHERE id = $7
		`

		// Execute the query with data from the struct
		_, err = db.Exec(query,
			product.ProductName,
			product.ProductType,
			product.Unit,
			product.ProjectId,
			product.NameId,
			product.MasterBomId,
			id,
		)

		// Handle errors during query execution
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product: " + err.Error()})
			return
		}

		// Respond with success message
		c.JSON(http.StatusOK, gin.H{"message": "Product updated successfully"})

		activityLog := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Update",
			Description:  fmt.Sprintf("Updated BOM product with ID %s", id),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    product.ProjectId, // Assuming the product has a ProjectId field
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", product.ProjectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", product.ProjectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the BOM product
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Updated BOM product: %s in project: %s", product.ProductName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", product.ProjectId),
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
		sendProjectNotifications(db, product.ProjectId,
			fmt.Sprintf("Updated BOM product: %s in project: %s", product.ProductName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", product.ProjectId))
	}
}

// DeleteBOMProduct deletes a BOM product by ID.
// @Summary Delete BOM product
// @Description Deletes the BOM product by id. Requires Authorization header.
// @Tags BOM
// @Accept json
// @Produce json
// @Param id path int true "BOM product ID"
// @Success 200 {object} models.MessageResponse "Returns message: Product deleted"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/delete_bom_products/{id} [delete]
func DeleteBOMProduct(db *sql.DB) gin.HandlerFunc {
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

		// Fetch product info before deletion for notifications
		var productName string
		var projectID int
		err = db.QueryRow("SELECT product_name, project_id FROM inv_bom WHERE id = $1", id).Scan(&productName, &projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "BOM product not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch product info", "details": err.Error()})
			return
		}

		query := "DELETE FROM inv_bom WHERE id=$1"
		_, err = db.Exec(query, id)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Product deleted"})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		activityLog := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Delete",
			Description:  fmt.Sprintf("Deleted BOM product with ID %s", id),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the BOM product
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Deleted BOM product: %s from project: %s", productName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", projectID),
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
			fmt.Sprintf("Deleted BOM product: %s from project: %s", productName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/bom", projectID))
	}
}

// GetAllBOMMasterProducts fetches all BOM master products
// @Summary Get all BOM master products
// @Description Retrieve all BOM master products from bom_master table
// @Tags BOM Master
// @Accept json
// @Produce json
// @Param Authorization header string true "Session ID"
// @Success 200 {array} models.BOMMaster
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_bom_master_products [get]
func GetAllBOMMasterProducts(db *sql.DB) gin.HandlerFunc {
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

		// Query to select all BOM master products
		query := `SELECT master_bom_id, bom_name, bom_type, unit 
		          FROM bom_master ORDER BY bom_name ASC`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var products []models.BOMMaster
		for rows.Next() {
			var product models.BOMMaster
			err := rows.Scan(
				&product.MasterID,
				&product.BOMName,
				&product.BOMType,
				&product.Unit,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			products = append(products, product)
		}

		// Check for errors after iterating through rows
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return an empty array with status 200 if no products are found
		if len(products) == 0 {
			c.Status(http.StatusNoContent)
			return
		}

		// Return all BOM master products
		c.JSON(http.StatusOK, products)

		log := models.ActivityLog{
			EventContext: "BOM Master",
			EventName:    "Get",
			Description:  "Retrieved all BOM master products",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project for master data
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity", "details": logErr.Error()})
			return
		}
	}
}

// BOMUsesListPDF generates a PDF report of BOM usage for completed post-pour elements
func BOMUsesListPDF(db *sql.DB) gin.HandlerFunc {
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

		// Get project_id from route parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Get project name
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project name"})
			return
		}

		// Query completed post-pour elements with their BOM usage
		query := `
			SELECT 
				cp.element_id,
				cp.element_type_id,
				et.element_type_name,
				e.element_name,
				etb.product_id,
				etb.product_name,
				etb.quantity,
				etb.unit,
				cp.started_at,
				cp.updated_at
			FROM complete_production cp
			INNER JOIN element_type et ON cp.element_type_id = et.element_type_id
			INNER JOIN element e ON cp.element_id = e.id
			INNER JOIN element_type_bom etb ON cp.element_type_id = etb.element_type_id
			WHERE cp.project_id = $1 
				AND cp.status = 'completed'
				AND cp.updated_at IS NOT NULL
			ORDER BY cp.updated_at DESC, et.element_type_name, etb.product_name
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch BOM usage data", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Collect BOM usage data
		type BOMUsage struct {
			ElementID       int       `json:"element_id"`
			ElementTypeID   int       `json:"element_type_id"`
			ElementTypeName string    `json:"element_type_name"`
			ElementName     string    `json:"element_name"`
			ProductID       int       `json:"product_id"`
			ProductName     string    `json:"product_name"`
			Quantity        float64   `json:"quantity"`
			Unit            string    `json:"unit"`
			StartedAt       time.Time `json:"started_at"`
			UpdatedAt       time.Time `json:"updated_at"`
		}

		var bomUsages []BOMUsage
		for rows.Next() {
			var usage BOMUsage
			err := rows.Scan(
				&usage.ElementID,
				&usage.ElementTypeID,
				&usage.ElementTypeName,
				&usage.ElementName,
				&usage.ProductID,
				&usage.ProductName,
				&usage.Quantity,
				&usage.Unit,
				&usage.StartedAt,
				&usage.UpdatedAt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan BOM usage data", "details": err.Error()})
				return
			}
			bomUsages = append(bomUsages, usage)
		}

		if len(bomUsages) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "No completed post-pour elements found for this project"})
			return
		}

		// Create PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Set margins similar to Dispatch PDF
		pdf.SetMargins(10, 10, 10)

		// Header band
		pdf.SetFont("Arial", "B", 24)
		pdf.SetFillColor(240, 240, 240)
		pdf.Rect(10, 10, 190, 15, "F")
		pdf.SetXY(10, 12)
		pdf.Cell(190, 10, "BOM Uses List")
		pdf.Ln(20)

		// Project information box
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(245, 245, 245)
		pdf.Rect(10, pdf.GetY(), 190, 10, "F")
		pdf.SetXY(10, pdf.GetY()+2)
		pdf.Cell(190, 8, "Project Details")
		pdf.Ln(12)

		pdf.SetFont("Arial", "", 11)
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(40, 7, "Project:")
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(70, 7, fmt.Sprintf("%s (ID: %d)", projectName, projectID))
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 11)
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(40, 7, "Generated on:")
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(70, 7, time.Now().Format("2006-01-02 15:04:05"))
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 11)
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(40, 7, "Generated by:")
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(70, 7, userName)
		pdf.Ln(12)

		// Group by element type and calculate totals
		elementTypeTotals := make(map[string]map[string]float64)
		elementTypeDetails := make(map[string][]BOMUsage)

		for _, usage := range bomUsages {
			key := usage.ElementTypeName
			if elementTypeTotals[key] == nil {
				elementTypeTotals[key] = make(map[string]float64)
			}
			if elementTypeDetails[key] == nil {
				elementTypeDetails[key] = []BOMUsage{}
			}

			productKey := fmt.Sprintf("%s (%s)", usage.ProductName, usage.Unit)
			elementTypeTotals[key][productKey] += usage.Quantity
			elementTypeDetails[key] = append(elementTypeDetails[key], usage)
		}

		// Summary section header band
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(245, 245, 245)
		pdf.Rect(10, pdf.GetY(), 190, 10, "F")
		pdf.SetXY(10, pdf.GetY()+2)
		pdf.Cell(190, 8, "Summary by Element Type")
		pdf.Ln(12)

		// Summary table header (shaded)
		pdf.SetFillColor(230, 230, 230)
		pdf.SetFont("Arial", "B", 10)
		pdf.Rect(10, pdf.GetY(), 190, 8, "F")
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(60, 8, "Element Type")
		pdf.Cell(80, 8, "Product")
		pdf.Cell(30, 8, "Total Quantity")
		pdf.Cell(20, 8, "Unit")
		pdf.Ln(8)

		// Summary table content
		pdf.SetFont("Arial", "", 9)
		for elementType, totals := range elementTypeTotals {
			firstRow := true
			for product, totalQty := range totals {
				if firstRow {
					pdf.Cell(60, 6, elementType)
					firstRow = false
				} else {
					pdf.Cell(60, 6, "")
				}
				pdf.Cell(80, 6, product)
				pdf.Cell(30, 6, fmt.Sprintf("%.2f", totalQty))
				pdf.Cell(20, 6, "pcs")
				pdf.Ln(6)
			}
			pdf.Ln(3)
		}

		// Add new page for detailed breakdown
		pdf.AddPage()

		// Detailed section header band
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(245, 245, 245)
		pdf.Rect(10, pdf.GetY(), 190, 10, "F")
		pdf.SetXY(10, pdf.GetY()+2)
		pdf.Cell(190, 8, "Detailed Breakdown by Element")
		pdf.Ln(12)

		// Detailed table header (shaded)
		pdf.SetFillColor(230, 230, 230)
		pdf.SetFont("Arial", "B", 9)
		pdf.Rect(10, pdf.GetY(), 190, 8, "F")
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(25, 8, "Element ID")
		pdf.Cell(40, 8, "Element Type")
		pdf.Cell(30, 8, "Element Name")
		pdf.Cell(50, 8, "Product")
		pdf.Cell(20, 8, "Quantity")
		pdf.Cell(15, 8, "Unit")
		pdf.Cell(20, 8, "Completed")
		pdf.Ln(8)

		// Detailed table content
		pdf.SetFont("Arial", "", 8)
		for _, usage := range bomUsages {
			pdf.Cell(25, 6, fmt.Sprintf("%d", usage.ElementID))
			pdf.Cell(40, 6, usage.ElementTypeName)
			pdf.Cell(30, 6, usage.ElementName)
			pdf.Cell(50, 6, usage.ProductName)
			pdf.Cell(20, 6, fmt.Sprintf("%.2f", usage.Quantity))
			pdf.Cell(15, 6, usage.Unit)
			pdf.Cell(20, 6, usage.UpdatedAt.Format("2006-01-02"))
			pdf.Ln(6)
		}

		// Footer similar to dispatch PDF
		pdf.SetY(-35)
		footerY := pdf.GetY()
		pdf.SetFont("Arial", "I", 8)
		pdf.SetXY(10, footerY+4)
		pdf.Cell(190, 6, "Generated on: "+time.Now().Format("2006-01-02 15:04:05"))
		pdf.SetXY(10, footerY+8)
		pdf.Cell(190, 6, fmt.Sprintf("Page: %d", pdf.PageNo()))

		// Generate PDF bytes
		var buf bytes.Buffer
		err = pdf.Output(&buf)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF", "details": err.Error()})
			return
		}

		// Set response headers
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=bom_uses_list_%s_%d.pdf", projectName, projectID))
		c.Data(http.StatusOK, "application/pdf", buf.Bytes())

		// Log the activity
		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "PDF Export",
			Description:  fmt.Sprintf("Generated BOM uses list PDF for project %s (ID: %d) with %d completed elements", projectName, projectID, len(bomUsages)),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			// Log error but don't fail the request
			fmt.Printf("Failed to log activity: %v\n", logErr)
		}
	}
}

package handlers

import (
	"backend/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func UpdateBOMPro(db *sql.DB) gin.HandlerFunc {
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

		// Parse the incoming JSON request body
		var userUpdates []models.Product
		if err := c.ShouldBindJSON(&userUpdates); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		// Get element_type_id from query parameters
		elementTypeID := c.Query("element_type_id")
		if elementTypeID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "element_type_id is required"})
			return
		}

		var projectID int
		err = db.QueryRow(`SELECT project_id FROM element_type WHERE element_type_id= $1`, elementTypeID).Scan(&projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		// Fetch data from BOMPro based on element_type_id
		query := `SELECT id, element_type_id, project_id, created_at, created_by, product FROM element_type_bom WHERE element_type_id = $1`
		rows, err := db.Query(query, elementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching data from bompro"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var bomPro models.BOMPro

			// Scan the data into BOMPro struct
			if err := rows.Scan(&bomPro.ID, &bomPro.ElementTypeID, &bomPro.ProjectId, &bomPro.CreatedAt, &bomPro.CreatedBy, &bomPro.Product); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning bompro row"})
				return
			}

			// Parse Product JSON
			var products []models.Product
			if err := json.Unmarshal([]byte(bomPro.Product), &products); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error unmarshalling product json"})
				return
			}

			// Match user-provided product IDs and update the quantity
			for _, update := range userUpdates {
				found := false
				for i := range products {
					if products[i].ProductID == update.ProductID {
						// Update quantity if ProductID is found
						products[i].Quantity = update.Quantity
						found = true
						break
					}
				}
				// If ProductID is not found, add as new product
				if !found {
					newProduct := models.Product{
						ProductID:   update.ProductID,
						ProductName: update.ProductName, // Use ProductName from the user's input
						Quantity:    update.Quantity,
					}
					products = append(products, newProduct)
				}
			}

			// Insert into BOMRevision
			insertRevisionQuery := `INSERT INTO bom_revision (bompro_id, element_type_id, project_id, created_at, created_by, product) 
		                        VALUES ($1, $2, $3, $4, $5, $6)`
			productJSON, err := json.Marshal(products)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error marshalling products to json"})
				return
			}

			_, err = db.Exec(insertRevisionQuery, bomPro.ID, bomPro.ElementTypeID, bomPro.ProjectId, bomPro.CreatedAt, bomPro.CreatedBy, productJSON)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error inserting into bom_revision"})
				return
			}

			// Update BOMPro with the updated product data
			updateQuery := `UPDATE element_type_bom SET product = $1, updated_at = $2, updated_by = $3 WHERE id = $4`
			_, err = db.Exec(updateQuery, productJSON, time.Now(), "new_user", bomPro.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating bompro"})
				return
			}

			fmt.Printf("Moved BOMPro ID: %d to BOMRevision and updated BOMPro\n", bomPro.ID)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating through bompro rows"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Data moved and updated successfully"})

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetched QC summary for project %d", projectID),
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

// GetBOMPro godoc
// @Summary      Get BOM by ID
// @Tags         element-type-bom
// @Param        id   path      int  true  "BOM ID"
// @Success      200  {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/get_bom/{id} [get]
func GetBOMPro(db *sql.DB) gin.HandlerFunc {
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

		id, convErr := strconv.Atoi(c.Param("id")) // Get BOMPro ID from the request URL
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Project ID"})
			return
		}

		var bomPro models.BOMResponse
		var productData sql.NullString // Handle potential NULL values in the product field

		// SQL query to fetch data from the BOMPro table
		query := `
        SELECT id, element_type_id, project_id, created_at, created_by, updated_at, updated_by, product 
        FROM element_type_bom 
        WHERE id = $1`

		// Fetch data from the database
		row := db.QueryRow(query, id)
		err = row.Scan(&bomPro.ID, &bomPro.ElementTypeID, &bomPro.ProjectId, &bomPro.CreatedAt, &bomPro.CreatedBy, &bomPro.UpdatedAt, &bomPro.UpdatedBy, &productData)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "BOMPro not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Check if productData is not NULL
		if productData.Valid {
			// Parse the JSONB product data into the Product slice
			if err := json.Unmarshal([]byte(productData.String), &bomPro.Product); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse product data"})
				return
			}

			// Fetch product name for each product_id in bomPro.Product
			for i, product := range bomPro.Product {
				var ProductName string
				var ProductType string
				productQuery := `SELECT product_name,product_type FROM inv_bom WHERE id = $1`
				err = db.QueryRow(productQuery, product.ProductID).Scan(&ProductName, &ProductType)
				if err != nil {
					if err == sql.ErrNoRows {
						bomPro.Product[i].ProductName = "Product not found"
					} else {
						log.Printf("Error fetching product name for id %d: %v", product.ProductID, err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching product name for product_id %d", product.ProductID)})
						return
					}
				} else {
					bomPro.Product[i].ProductName = ProductName + "-" + ProductType
				}
			}

		} else {
			// Handle NULL product field by setting an empty Product slice
			bomPro.Product = []models.ProductResponse{} // Empty slice
		}

		// Return the BOMPro data as JSON
		c.JSON(http.StatusOK, bomPro)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  "Get BOM Products",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    id,
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

// GetBOMProByProjectId godoc
// @Summary      Get BOM by project ID
// @Tags         element-type-bom
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/bom_get_fetch/{project_id} [get]
func GetBOMProByProjectId(db *sql.DB) gin.HandlerFunc {
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

		projectId, convErr := strconv.Atoi(c.Param("project_id")) // Get BOMPro ID from the request URL
		if convErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid BOMPro ID"})
			return
		}

		var bomPro models.BOMResponse
		var productData sql.NullString // Handle potential NULL values in the product field

		// SQL query to fetch data from the BOMPro table
		query := `
        SELECT id, element_type_id, project_id, created_at, created_by, updated_at, updated_by, product 
        FROM element_type_bom 
        WHERE project_id = $1`

		// Fetch data from the database
		row := db.QueryRow(query, projectId)
		err = row.Scan(&bomPro.ID, &bomPro.ElementTypeID, &bomPro.ProjectId, &bomPro.CreatedAt, &bomPro.CreatedBy, &bomPro.UpdatedAt, &bomPro.UpdatedBy, &productData)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "BOMPro not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Check if productData is not NULL
		if productData.Valid {
			// Parse the JSONB product data into the Product slice
			if err := json.Unmarshal([]byte(productData.String), &bomPro.Product); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse product data"})
				return
			}
			// Fetch product name for each product_id in bomPro.Product
			for i, product := range bomPro.Product {
				var ProductName string
				var ProductType string
				productQuery := `SELECT product_name,product_type FROM inv_bom WHERE id = $1`
				err = db.QueryRow(productQuery, product.ProductID).Scan(&ProductName, &ProductType)
				if err != nil {
					if err == sql.ErrNoRows {
						bomPro.Product[i].ProductName = "Product not found"
					} else {
						log.Printf("Error fetching product name for id %d: %v", product.ProductID, err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching product name for product_id %d", product.ProductID)})
						return
					}
				} else {
					bomPro.Product[i].ProductName = ProductName + "-" + ProductType
				}
			}
		} else {
			// Handle NULL product field by setting an empty Product slice
			bomPro.Product = []models.ProductResponse{} // Empty slice
		}

		// Return the BOMPro data as JSON
		c.JSON(http.StatusOK, bomPro)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  "GET Bom Products",
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

// GetBOMProByElementTypeID godoc
// @Summary      Get BOM by element type ID
// @Tags         element-type-bom
// @Param        element_type_id  path      int  true  "Element type ID"
// @Success      200              {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/bom_fetch/{element_type_id} [get]
func GetBOMProByElementTypeID(db *sql.DB) gin.HandlerFunc {
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

		ElementTypeID := c.Param("element_type_id") // Get BOMPro ID from the request URL

		var bomPro models.BOMResponse
		var productData sql.NullString // Handle potential NULL values in the product field

		// SQL query to fetch data from the BOMPro table
		query := `
        SELECT id, element_type_id, project_id, created_at, created_by, updated_at, updated_by, product 
        FROM element_type_bom 
        WHERE element_type_id = $1`

		// Fetch data from the database
		row := db.QueryRow(query, ElementTypeID)
		err = row.Scan(&bomPro.ID, &bomPro.ElementTypeID, &bomPro.ProjectId, &bomPro.CreatedAt, &bomPro.CreatedBy, &bomPro.UpdatedAt, &bomPro.UpdatedBy, &productData)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "BOMPro not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Check if productData is not NULL
		if productData.Valid {
			// Parse the JSONB product data into the Product slice
			if err := json.Unmarshal([]byte(productData.String), &bomPro.Product); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse product data"})
				return
			}
			// Fetch product name for each product_id in bomPro.Product
			for i, product := range bomPro.Product {
				var ProductName string
				var ProductType string
				productQuery := `SELECT product_name,product_type FROM inv_bo WHERE id = $1`
				err = db.QueryRow(productQuery, product.ProductID).Scan(&ProductName, &ProductType)
				if err != nil {
					if err == sql.ErrNoRows {
						bomPro.Product[i].ProductName = "Product not found"
					} else {
						log.Printf("Error fetching product name for id %d: %v", product.ProductID, err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching product name for product_id %d", product.ProductID)})
						return
					}
				} else {
					bomPro.Product[i].ProductName = ProductName + "-" + ProductType
				}
			}
		} else {
			// Handle NULL product field by setting an empty Product slice
			bomPro.Product = []models.ProductResponse{} // Empty slice
		}

		// Return the BOMPro data as JSON
		c.JSON(http.StatusOK, bomPro)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  "Get BOM Products",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    bomPro.ProjectId,
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

// GetAllBOMPros godoc
// @Summary      Get all BOM (element type BOM)
// @Tags         element-type-bom
// @Success      200  {array}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/get_bom [get]
func GetAllBOMPros(db *sql.DB) gin.HandlerFunc {
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

		var bomPros []models.BOMResponse

		query := `
        SELECT id, element_type_id, project_id, created_at, created_by, updated_at, updated_by, product
        FROM element_type_bom`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var bomPro models.BOMResponse
			var productData sql.NullString // Use sql.NullString to handle potential NULL values

			err := rows.Scan(&bomPro.ID, &bomPro.ElementTypeID, &bomPro.ProjectId, &bomPro.CreatedAt, &bomPro.CreatedBy, &bomPro.UpdatedAt, &bomPro.UpdatedBy, &productData)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Check if productData is not NULL
			if productData.Valid {
				// Parse the JSONB product data into the Product slice
				if err := json.Unmarshal([]byte(productData.String), &bomPro.Product); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse product data"})
					return
				}
				// Fetch product name for each product_id in bomPro.Product
				for i, product := range bomPro.Product {
					var ProductName string
					var ProductType string
					productQuery := `SELECT product_name,product_type FROM inv_bom WHERE id = $1`
					err = db.QueryRow(productQuery, product.ProductID).Scan(&ProductName, &ProductType)
					if err != nil {
						if err == sql.ErrNoRows {
							bomPro.Product[i].ProductName = "Product not found"
						} else {
							log.Printf("Error fetching product name for id %d: %v", product.ProductID, err)
							c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching product name for product_id %d", product.ProductID)})
							return
						}
					} else {
						bomPro.Product[i].ProductName = ProductName + "-" + ProductType
					}
				}
			} else {
				// Handle NULL product field by setting an empty Product slice
				bomPro.Product = []models.ProductResponse{} // Empty slice
			}

			bomPros = append(bomPros, bomPro) // Append the BOMPro record to the list
		}

		// Check for any errors from iterating over rows
		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return the list of BOMPro records as JSON
		c.JSON(http.StatusOK, bomPros)

		log := models.ActivityLog{
			EventContext: "BOM",
			EventName:    "Get",
			Description:  "Get All BOM Products",
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

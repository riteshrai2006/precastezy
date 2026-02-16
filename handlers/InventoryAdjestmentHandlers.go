package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"backend/models"

	"github.com/gin-gonic/gin"
)

// GetElementTypesWithUpdatedBOMAndCompletedElements fetches element types that have updated BOMs and their elements with completed post-pre stages
func GetElementTypesWithUpdatedBOMAndCompletedElements(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
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

		// Get project_id from path parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id path parameter is required"})
			return
		}

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id must be a valid integer"})
			return
		}

		// Get element types with version updates (RV-2 and higher indicate updates)
		query := `
			SELECT 
				et.element_type_id,
				et.element_type_name,
				et.project_id,
				et.created_by AS element_type_created_by,
				et.element_type_version,
				et.update_at AS element_type_updated_at,
				e.id AS element_id,
				e.element_id AS element_code,
				e.bom_revision_id,
				e.drawing_revision_id,
				e.update_at AS element_updated_at,

				-- Cleaned BOM Product (remove exact matches with revision) from normalized tables
				(
					SELECT COALESCE(
						jsonb_agg(
							jsonb_build_object(
								'product_id', etb2.product_id,
								'product_name', etb2.product_name,
								'quantity', etb2.quantity
							)
						), '[]'::jsonb)
					FROM element_type_bom etb2
					LEFT JOIN element_type_revision_bom etbr2 
						ON etbr2.element_type_bom_id = etb2.id
						AND etbr2.revision_id = e.bom_revision_id
						AND COALESCE(etbr2.quantity,0) = COALESCE(etb2.quantity,0)
					WHERE etb2.element_type_id = et.element_type_id
						AND etb2.project_id = et.project_id
						AND etbr2.element_type_bom_id IS NULL
				) AS bom_product,

				-- Cleaned BOM Revision Product (remove exact matches with main BOM) from normalized tables
				(
					SELECT COALESCE(
						jsonb_agg(
							jsonb_build_object(
								'product_id', etbr3.product_id,
								'product_name', etbr3.product_name,
								'quantity', etbr3.quantity
							)
						), '[]'::jsonb)
					FROM element_type_revision_bom etbr3
					LEFT JOIN element_type_bom etb3 
						ON etb3.id = etbr3.element_type_bom_id
						AND COALESCE(etb3.quantity,0) = COALESCE(etbr3.quantity,0)
					WHERE etbr3.element_type_id = et.element_type_id
						AND etbr3.revision_id = e.bom_revision_id
						AND etbr3.project_id = et.project_id
						AND etb3.id IS NULL
				) AS bom_revision_product,

				-- BOM Required Adjustment (newly added products OR products with quantity changes)
				(
					SELECT COALESCE(
						jsonb_agg(
							jsonb_build_object(
								'product_id', etb4.product_id,
								'product_name', etb4.product_name,
								'quantity', etb4.quantity,
								'revision_quantity', COALESCE(etbr4.quantity, 0),
								'quantity_change', etb4.quantity - COALESCE(etbr4.quantity, 0)
							)
						), '[]'::jsonb
					)
					FROM element_type_bom etb4
					LEFT JOIN element_type_revision_bom etbr4 ON etbr4.element_type_bom_id = etb4.id
						AND etbr4.revision_id = e.bom_revision_id
					WHERE etb4.element_type_id = et.element_type_id
						AND etb4.project_id = et.project_id
						AND (
							-- Newly added: product in main BOM but not in revision
							etbr4.element_type_bom_id IS NULL
							OR
							-- Quantity changed: product exists in both but quantities differ
							(etbr4.element_type_bom_id IS NOT NULL AND COALESCE(etb4.quantity, 0) != COALESCE(etbr4.quantity, 0))
						)
				) AS bom_required_adjustment

            FROM element_type et
            INNER JOIN element e ON et.element_type_id = e.element_type_id
            INNER JOIN activity a ON e.id = a.element_id
            WHERE et.project_id = $1
                AND et.element_type_version NOT IN ('RV-1', 'VR-1','RV-01')
                AND e.instage = true
                AND a.completed = true
                AND e.inv_adjust = false
                AND et.inv_adjust = false
            ORDER BY et.element_type_name, e.element_id
        `

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element types with version updates", "details": err.Error()})
			return
		}
		defer rows.Close()

		elementTypeMap := make(map[int]*models.ElementTypeWithBOM)
		for rows.Next() {
			var et struct {
				ElementTypeID         int        `json:"element_type_id"`
				ElementTypeName       string     `json:"element_type_name"`
				ProjectID             int        `json:"project_id"`
				ElementTypeCreatedBy  string     `json:"element_type_created_by"`
				ElementTypeVersion    string     `json:"element_type_version"`
				ElementTypeUpdatedAt  *time.Time `json:"element_type_updated_at"`
				ElementID             *int       `json:"element_id"`
				ElementCode           *string    `json:"element_code"`
				BOMRevisionID         *int       `json:"bom_revision_id"`
				DrawingRevisionID     *int       `json:"drawing_revision_id"`
				ElementUpdatedAt      *time.Time `json:"element_updated_at"`
				BOMProduct            *string    `json:"bom_product"`
				BOMRevisionProduct    *string    `json:"bom_revision_product"`
				BOMRequiredAdjustment *string    `json:"bom_required_adjustment"`
			}

			err := rows.Scan(
				&et.ElementTypeID,
				&et.ElementTypeName,
				&et.ProjectID,
				&et.ElementTypeCreatedBy,
				&et.ElementTypeVersion,
				&et.ElementTypeUpdatedAt,
				&et.ElementID,
				&et.ElementCode,
				&et.BOMRevisionID,
				&et.DrawingRevisionID,
				&et.ElementUpdatedAt,
				&et.BOMProduct,
				&et.BOMRevisionProduct,
				&et.BOMRequiredAdjustment,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element type data", "details": err.Error()})
				return
			}

			// Create or update element type entry
			if _, exists := elementTypeMap[et.ElementTypeID]; !exists {
				// Parse BOM product JSON
				var bomProductArray interface{}
				if et.BOMProduct != nil && *et.BOMProduct != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMProduct), &bomProductArray); err != nil {
						// If parsing fails, set as empty array
						bomProductArray = []interface{}{}
					}
				} else {
					bomProductArray = []interface{}{}
				}

				// Parse BOM revision product JSON
				var bomRevisionProductArray interface{}
				if et.BOMRevisionProduct != nil && *et.BOMRevisionProduct != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMRevisionProduct), &bomRevisionProductArray); err != nil {
						// If parsing fails, set as empty array
						bomRevisionProductArray = []interface{}{}
					}
				} else {
					bomRevisionProductArray = []interface{}{}
				}

				// Parse BOM required adjustment JSON
				var bomRequiredAdjustmentArray interface{}
				if et.BOMRequiredAdjustment != nil && *et.BOMRequiredAdjustment != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMRequiredAdjustment), &bomRequiredAdjustmentArray); err != nil {
						// If parsing fails, set as empty array
						bomRequiredAdjustmentArray = []interface{}{}
					}
				} else {
					bomRequiredAdjustmentArray = []interface{}{}
				}

				elementTypeMap[et.ElementTypeID] = &models.ElementTypeWithBOM{
					BOMRevisionProduct:    bomRevisionProductArray,
					ElementTypeCreatedBy:  et.ElementTypeCreatedBy,
					ElementTypeID:         et.ElementTypeID,
					ElementTypeName:       et.ElementTypeName,
					ElementTypeUpdatedAt:  et.ElementTypeUpdatedAt,
					ElementTypeVersion:    et.ElementTypeVersion,
					ProjectID:             et.ProjectID,
					BOMProduct:            bomProductArray,
					BOMRequiredAdjustment: bomRequiredAdjustmentArray,
					Elements:              []models.ElementWithRevision{},
				}
			}

			// Add element if it exists
			if et.ElementID != nil {
				// Print BOM revision ID and drawing revision ID for debugging
				if et.BOMRevisionID != nil {
					log.Printf("Element ID: %d, BOM Revision ID: %d", *et.ElementID, *et.BOMRevisionID)
				} else {
					log.Printf("Element ID: %d, BOM Revision ID: null", *et.ElementID)
				}

				if et.DrawingRevisionID != nil {
					log.Printf(", Drawing Revision ID: %d", *et.DrawingRevisionID)
				} else {
					log.Printf(", Drawing Revision ID: null")
				}

				elementType := elementTypeMap[et.ElementTypeID]
				elementType.Elements = append(elementType.Elements, models.ElementWithRevision{
					BOMRevisionID:     et.BOMRevisionID,
					DrawingRevisionID: et.DrawingRevisionID,
					ElementCode:       et.ElementCode,
					ElementID:         et.ElementID,
					ElementUpdatedAt:  et.ElementUpdatedAt,
				})
			}
		}

		// Convert map to slice
		var elementTypes []*models.ElementTypeWithBOM
		for _, elementType := range elementTypeMap {
			elementTypes = append(elementTypes, elementType)
		}

		// If no element types found, return empty array
		if len(elementTypes) == 0 {
			c.JSON(http.StatusOK, []*models.ElementTypeWithBOM{})
			return
		}

		// Return element types with version updates
		c.JSON(http.StatusOK, elementTypes)
	}
}

// GetInventoryAdjustmentLogs retrieves inventory adjustment logs for a specific project
// @Summary Get inventory adjustment logs by project
// @Description Retrieve inventory adjustment logs for a specific project
// @Tags InventoryAdjustment
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.InventoryAdjustmentLog
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/inventory-adjustment-logs/{project_id} [get]
func GetInventoryAdjustmentLogs(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
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

		// Get project_id from path parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id path parameter is required"})
			return
		}

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id must be a valid integer"})
			return
		}

		// Query to get adjustment logs for specific project
		query := `
			SELECT 
				id,
				element_type_id,
				product_id,
				quantity,
				reason,
				adjusted_by,
				adjusted_at,
				project_id,
				element_caunt
			FROM inv_adjustment
			WHERE project_id = $1
			ORDER BY adjusted_at DESC, id DESC
		`

		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory adjustment logs", "details": err.Error()})
			return
		}
		defer rows.Close()

		var adjustmentLogs []models.InventoryAdjustmentLog
		for rows.Next() {
			var log models.InventoryAdjustmentLog
			var elementTypeID sql.NullInt64
			var productID sql.NullInt64
			var projectID sql.NullInt64
			var elementCount sql.NullInt64

			if err := rows.Scan(
				&log.ID,
				&elementTypeID,
				&productID,
				&log.Quantity,
				&log.Reason,
				&log.AdjustedBy,
				&log.AdjustedAt,
				&projectID,
				&elementCount,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan adjustment log data", "details": err.Error()})
				return
			}

			// Handle nullable fields
			if elementTypeID.Valid {
				log.ElementTypeID = int(elementTypeID.Int64)
			}
			if productID.Valid {
				log.ProductID = int(productID.Int64)
			}
			if projectID.Valid {
				log.ProjectID = int(projectID.Int64)
			}
			if elementCount.Valid {
				log.ElementCount = int(elementCount.Int64)
			}

			adjustmentLogs = append(adjustmentLogs, log)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating adjustment logs", "details": err.Error()})
			return
		}

		// If no logs found, return empty array
		if len(adjustmentLogs) == 0 {
			c.JSON(http.StatusOK, []models.InventoryAdjustmentLog{})
			return
		}

		c.JSON(http.StatusOK, adjustmentLogs)
	}
}

// InventoryAdjustmentRequest represents the request structure for inventory adjustment with BOM operations
type InventoryAdjustmentRequest struct {
	ElementTypeID int                    `json:"element_type_id" binding:"required"`
	ElementCount  int                    `json:"element_count" binding:"required"`
	ProjectID     int                    `json:"project_id" binding:"required"`
	BOM           []BOMAdjustmentRequest `json:"bom" binding:"required"`
}

// BOMAdjustmentRequest represents individual BOM adjustment operations
type BOMAdjustmentRequest struct {
	BOMID     int    `json:"bom_id" binding:"required"`
	Quantity  int    `json:"quantity" binding:"required"`
	Operation string `json:"operation" binding:"required"` // "add" or "subtract"
}

// InventoryAdjustmentResponse represents the response structure for inventory adjustment
type InventoryAdjustmentResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	AdjustmentID int    `json:"adjustment_id,omitempty"`
}

// CreateInventoryAdjustmentWithBOM creates inventory adjustments with BOM operations
// @Summary Create inventory adjustment with BOM operations
// @Description Create inventory adjustments for multiple BOM items with add/subtract operations
// @Tags InventoryAdjustment
// @Accept json
// @Produce json
// @Param request body InventoryAdjustmentRequest true "Inventory adjustment request with BOM operations"
// @Success 201 {object} InventoryAdjustmentResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/inventory-adjustment [post]
func CreateInventoryAdjustmentWithBOM(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var request InventoryAdjustmentRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
			return
		}

		// Validate project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", request.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Validate element type exists
		var elementTypeExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM element_type WHERE element_type_id = $1)", request.ElementTypeID).Scan(&elementTypeExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !elementTypeExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element type does not exist"})
			return
		}

		// Validate BOM items exist
		for _, bomItem := range request.BOM {
			var bomExists bool
			err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM inv_bom WHERE id = $1)", bomItem.BOMID).Scan(&bomExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
				return
			}
			if !bomExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("BOM item with ID %d does not exist", bomItem.BOMID)})
				return
			}

			// Validate operation
			if bomItem.Operation != "add" && bomItem.Operation != "subtract" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid operation '%s' for BOM ID %d. Must be 'add' or 'subtract'", bomItem.Operation, bomItem.BOMID)})
				return
			}
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Process each BOM adjustment
		for _, bomItem := range request.BOM {
			// Get current inventory for this BOM item
			var currentQuantity float64
			err = tx.QueryRow(`
				SELECT COALESCE(bom_qty, 0) 
				FROM inv_track 
				WHERE project_id = $1 AND bom_id = $2
			`, request.ProjectID, bomItem.BOMID).Scan(&currentQuantity)
			if err != nil && err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get current inventory"})
				return
			}

			// Calculate new quantity based on operation
			var newQuantity float64
			switch bomItem.Operation {
			case "add":
				newQuantity = currentQuantity + float64(bomItem.Quantity)
			case "subtract":
				newQuantity = currentQuantity - float64(bomItem.Quantity)
				if newQuantity < 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Cannot subtract %d from BOM ID %d. Insufficient inventory (current: %.2f)", bomItem.Quantity, bomItem.BOMID, currentQuantity)})
					return
				}
			}

			// Update or insert inventory track
			if err == sql.ErrNoRows {
				// Insert new record
				_, err = tx.Exec(`
					INSERT INTO inv_track (project_id, bom_id, bom_qty, last_updated)
					VALUES ($1, $2, $3, $4)
				`, request.ProjectID, bomItem.BOMID, newQuantity, time.Now())
			} else {
				// Update existing record
				_, err = tx.Exec(`
					UPDATE inv_track 
					SET bom_qty = $1, last_updated = $2
					WHERE project_id = $3 AND bom_id = $4
				`, newQuantity, time.Now(), request.ProjectID, bomItem.BOMID)
			}

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory"})
				return
			}

			// Insert adjustment log
			var adjustmentID int
			err = tx.QueryRow(`
				INSERT INTO inv_adjustment (
					element_type_id, product_id, quantity, reason, 
					adjusted_by, adjusted_at, project_id, element_caunt
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) 
				RETURNING id
			`, request.ElementTypeID, bomItem.BOMID, bomItem.Quantity,
				fmt.Sprintf("%s operation for BOM ID %d", bomItem.Operation, bomItem.BOMID),
				userName, time.Now(), request.ProjectID, request.ElementCount).Scan(&adjustmentID)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create adjustment log"})
				return
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusCreated, InventoryAdjustmentResponse{
			Success: true,
			Message: "Inventory adjustments created successfully",
		})
	}
}

// GetElementTypesWithUpdatedBOMByProjectAndElementType fetches element types with updated BOMs for specific project and element type
// @Summary Get element types with updated BOM by project and element type
// @Description Retrieve element types with updated BOMs for a specific project and element type ID
// @Tags InventoryAdjustment
// @Produce json
// @Param project_id path int true "Project ID"
// @Param element_type_id path int true "Element Type ID"
// @Success 200 {array} models.ElementTypeWithBOM
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/element_types_with_updated_bom/{project_id}/{element_type_id} [get]
func GetElementTypesWithUpdatedBOMByProjectAndElementType(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session validation
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

		// Get project_id from path parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id path parameter is required"})
			return
		}

		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id must be a valid integer"})
			return
		}

		// Get element_type_id from path parameter
		elementTypeIDStr := c.Param("element_type_id")
		if elementTypeIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "element_type_id path parameter is required"})
			return
		}

		elementTypeID, err := strconv.Atoi(elementTypeIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "element_type_id must be a valid integer"})
			return
		}

		// Get element types with version updates for specific project and element type
		query := `
			WITH latest_revisions AS (
				SELECT revision_id
				FROM element_type_revision_bom
				WHERE element_type_id = $2
					AND project_id = $1
					AND changed_at >= (
						SELECT MAX(changed_at) - INTERVAL '1 second'
						FROM element_type_revision_bom
						WHERE element_type_id = $2 AND project_id = $1
					)
			),
			max_revision AS (
				SELECT MAX(revision_id) AS revision_id FROM latest_revisions
			),
			bom_main AS (
				SELECT etb.product_id, etb.product_name, etb.quantity
				FROM element_type_bom etb
				LEFT JOIN element_type_revision_bom etbr 
					ON etbr.element_type_bom_id = etb.id
					AND etbr.revision_id IN (SELECT revision_id FROM latest_revisions)
					AND COALESCE(etbr.quantity,0) = COALESCE(etb.quantity,0)
				WHERE etb.element_type_id = $2
					AND etb.project_id = $1
					AND etbr.element_type_bom_id IS NULL
			),
			bom_revision AS (
				SELECT etbr.product_id, etbr.product_name, etbr.quantity
				FROM element_type_revision_bom etbr
				LEFT JOIN element_type_bom etb 
					ON etb.id = etbr.element_type_bom_id
					AND COALESCE(etb.quantity,0) = COALESCE(etbr.quantity,0)
				WHERE etbr.element_type_id = $2
					AND etbr.revision_id IN (SELECT revision_id FROM latest_revisions)
					AND etbr.project_id = $1
					AND etb.id IS NULL
			)
			SELECT 
				et.element_type_id,
				et.element_type_name,
				et.project_id,
				et.created_by AS element_type_created_by,
				et.element_type_version,
				et.update_at AS element_type_updated_at,
				e.id AS element_id,
				e.element_id AS element_code,
				mr.revision_id AS bom_revision_id,
				e.drawing_revision_id,
				e.update_at AS element_updated_at,

				-- Cleaned BOM Product
				(
					SELECT COALESCE(jsonb_agg(
						jsonb_build_object('product_id', product_id, 'product_name', product_name, 'quantity', quantity)
					), '[]'::jsonb)
					FROM bom_main
				) AS bom_product,

				-- Cleaned BOM Revision Product
				(
					SELECT COALESCE(jsonb_agg(
						jsonb_build_object('product_id', product_id, 'product_name', product_name, 'quantity', quantity)
					), '[]'::jsonb)
					FROM bom_revision
				) AS bom_revision_product,

				-- BOM Required Adjustment
				(
					SELECT COALESCE(jsonb_agg(
						jsonb_build_object(
							'product_id', adj.product_id,
							'product_name', adj.product_name,
							'quantity', adj.main_quantity,
							'revision_quantity', adj.revision_quantity,
							'quantity_change', adj.main_quantity - adj.revision_quantity
						)
					), '[]'::jsonb)
					FROM (
						-- Products only in bom_main
						SELECT product_id, product_name, quantity AS main_quantity, 0 AS revision_quantity
						FROM bom_main
						WHERE product_id NOT IN (SELECT product_id FROM bom_revision)
						
						UNION ALL
						
						-- Products only in bom_revision
						SELECT product_id, product_name, 0 AS main_quantity, quantity AS revision_quantity
						FROM bom_revision
						WHERE product_id NOT IN (SELECT product_id FROM bom_main)
						
						UNION ALL
						
						-- Products in both with different quantities
						SELECT bm.product_id, bm.product_name, bm.quantity AS main_quantity, br.quantity AS revision_quantity
						FROM bom_main bm
						INNER JOIN bom_revision br ON br.product_id = bm.product_id
						WHERE bm.quantity != br.quantity
					) adj
				) AS bom_required_adjustment

            FROM element_type et
            INNER JOIN element e ON et.element_type_id = e.element_type_id
            INNER JOIN activity a ON e.id = a.element_id
            CROSS JOIN max_revision mr
            WHERE et.project_id = $1
                AND et.element_type_id = $2
                AND et.element_type_version NOT IN ('RV-1', 'VR-1','RV-01')
                AND e.instage = true
                AND a.completed = true
                AND e.inv_adjust = false
                AND et.inv_adjust = false
            ORDER BY et.element_type_name, e.element_id
        `

		rows, err := db.Query(query, projectID, elementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element types with version updates", "details": err.Error()})
			return
		}
		defer rows.Close()

		elementTypeMap := make(map[int]*models.ElementTypeWithBOM)
		for rows.Next() {
			var et struct {
				ElementTypeID         int        `json:"element_type_id"`
				ElementTypeName       string     `json:"element_type_name"`
				ProjectID             int        `json:"project_id"`
				ElementTypeCreatedBy  string     `json:"element_type_created_by"`
				ElementTypeVersion    string     `json:"element_type_version"`
				ElementTypeUpdatedAt  *time.Time `json:"element_type_updated_at"`
				ElementID             *int       `json:"element_id"`
				ElementCode           *string    `json:"element_code"`
				BOMRevisionID         *int       `json:"bom_revision_id"`
				DrawingRevisionID     *int       `json:"drawing_revision_id"`
				ElementUpdatedAt      *time.Time `json:"element_updated_at"`
				BOMProduct            *string    `json:"bom_product"`
				BOMRevisionProduct    *string    `json:"bom_revision_product"`
				BOMRequiredAdjustment *string    `json:"bom_required_adjustment"`
			}

			err := rows.Scan(
				&et.ElementTypeID,
				&et.ElementTypeName,
				&et.ProjectID,
				&et.ElementTypeCreatedBy,
				&et.ElementTypeVersion,
				&et.ElementTypeUpdatedAt,
				&et.ElementID,
				&et.ElementCode,
				&et.BOMRevisionID,
				&et.DrawingRevisionID,
				&et.ElementUpdatedAt,
				&et.BOMProduct,
				&et.BOMRevisionProduct,
				&et.BOMRequiredAdjustment,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan element type data", "details": err.Error()})
				return
			}

			// Create or update element type entry
			if _, exists := elementTypeMap[et.ElementTypeID]; !exists {
				// Parse BOM product JSON
				var bomProductArray interface{}
				if et.BOMProduct != nil && *et.BOMProduct != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMProduct), &bomProductArray); err != nil {
						// If parsing fails, set as empty array
						bomProductArray = []interface{}{}
					}
				} else {
					bomProductArray = []interface{}{}
				}

				// Parse BOM revision product JSON
				var bomRevisionProductArray interface{}
				if et.BOMRevisionProduct != nil && *et.BOMRevisionProduct != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMRevisionProduct), &bomRevisionProductArray); err != nil {
						// If parsing fails, set as empty array
						bomRevisionProductArray = []interface{}{}
					}
				} else {
					bomRevisionProductArray = []interface{}{}
				}

				// Parse BOM required adjustment JSON
				var bomRequiredAdjustmentArray interface{}
				if et.BOMRequiredAdjustment != nil && *et.BOMRequiredAdjustment != "" {
					// Parse the JSON string into array
					if err := json.Unmarshal([]byte(*et.BOMRequiredAdjustment), &bomRequiredAdjustmentArray); err != nil {
						// If parsing fails, set as empty array
						bomRequiredAdjustmentArray = []interface{}{}
					}
				} else {
					bomRequiredAdjustmentArray = []interface{}{}
				}

				elementTypeMap[et.ElementTypeID] = &models.ElementTypeWithBOM{
					BOMRevisionProduct:    bomRevisionProductArray,
					ElementTypeCreatedBy:  et.ElementTypeCreatedBy,
					ElementTypeID:         et.ElementTypeID,
					ElementTypeName:       et.ElementTypeName,
					ElementTypeUpdatedAt:  et.ElementTypeUpdatedAt,
					ElementTypeVersion:    et.ElementTypeVersion,
					ProjectID:             et.ProjectID,
					BOMProduct:            bomProductArray,
					BOMRequiredAdjustment: bomRequiredAdjustmentArray,
					Elements:              []models.ElementWithRevision{},
				}
			}

			// Add element if it exists
			if et.ElementID != nil {
				elementType := elementTypeMap[et.ElementTypeID]
				elementType.Elements = append(elementType.Elements, models.ElementWithRevision{
					BOMRevisionID:     et.BOMRevisionID,
					DrawingRevisionID: et.DrawingRevisionID,
					ElementCode:       et.ElementCode,
					ElementID:         et.ElementID,
					ElementUpdatedAt:  et.ElementUpdatedAt,
				})
			}
		}

		// Get the first (and only) element type from map
		var result *models.ElementTypeWithBOM
		for _, elementType := range elementTypeMap {
			result = elementType
			break
		}

		// If no element type found, return null
		if result == nil {
			c.JSON(http.StatusOK, nil)
			return
		}

		// Return single element type object (not array)
		c.JSON(http.StatusOK, result)
	}
}

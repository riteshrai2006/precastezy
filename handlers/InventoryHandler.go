package handlers

import (
	"backend/models"
	"backend/repository"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CreatePurchase godoc
// @Summary      Create inventory purchase
// @Description  Create a new inventory purchase with BOM line items
// @Tags         inventory
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "Purchase with PurchaseBOM"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/inventory_create [post]
func CreatePurchase(db *sql.DB) gin.HandlerFunc {
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

		var purchase models.InvPurchase
		purchase.PurchaseID = repository.GenerateRandomNumber()

		// Parse and validate input JSON
		if err := c.ShouldBindJSON(&purchase); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid JSON input",
				"details": err.Error(),
			})
			return
		}

		// Check BOM item availability
		var unavailableItems []int
		for _, bom := range purchase.PurchaseBOM {
			var count int
			query := `SELECT COUNT(*) FROM inv_Bom WHERE id = $1`
			if err := db.QueryRow(query, bom.BomID).Scan(&count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Error checking BOM item availability",
					"details": err.Error(),
				})
				return
			}
			if count == 0 {
				unavailableItems = append(unavailableItems, bom.BomID)
			}
		}

		if len(unavailableItems) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Some BOM items are not available",
				"details": gin.H{
					"unavailable_bom_ids": unavailableItems,
				},
			})
			return
		}

		purchase.Timedatestamp = time.Now()
		totalCost := 0.0

		// Process BOM items
		var transactionID int
		for _, bom := range purchase.PurchaseBOM {
			var bomRate float64
			query := `SELECT rate FROM inv_Bom WHERE id = $1`
			if err := db.QueryRow(query, bom.BomID).Scan(&bomRate); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch BOM rate",
					"details": err.Error(),
				})
				return
			}

			subTotal := bom.BomQty * bomRate
			totalCost += subTotal

			// Insert into line items
			query = `
			INSERT INTO inv_line_items (items_id, purchase_id, bom_id, bom_qty, bom_rate, sub_total)
			VALUES (DEFAULT, $1, $2, $3, $4, $5)`
			if _, err := db.Exec(query, purchase.PurchaseID, bom.BomID, bom.BomQty, bomRate, subTotal); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to insert line item",
					"details": err.Error(),
				})
				return
			}

			// Insert transaction
			query = `
				INSERT INTO inv_transaction (inv_transaction_id, purchase_id, warehouse_id, project_id, task_id, bom_id, bom_qty, status, time_date)
				VALUES (DEFAULT, $1, $2, $3, DEFAULT, $4, $5, 'Added', $6) RETURNING inv_transaction_id`
			if err := db.QueryRow(query, purchase.PurchaseID, purchase.WarehouseID, purchase.ProjectID, bom.BomID, bom.BomQty, time.Now()).Scan(&transactionID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to insert new transaction",
					"details": err.Error(),
				})
				return
			}

			// Update or insert track record
			var existingTrackQty float64
			query = `
			SELECT bom_qty FROM inv_track
			WHERE project_id = $1 AND bom_id = $2 AND warehouse_id = $3`
			err := db.QueryRow(query, purchase.ProjectID, bom.BomID, purchase.WarehouseID).Scan(&existingTrackQty)

			switch err {
			case sql.ErrNoRows:
				query = `
				INSERT INTO inv_track (inv_track_id, project_id, bom_id, bom_qty, warehouse_id, last_updated, last_inv_transactionid)
				VALUES (DEFAULT, $1, $2, $3, $4, $5, $6)`
				if _, err := db.Exec(query, purchase.ProjectID, bom.BomID, bom.BomQty, purchase.WarehouseID, time.Now(), transactionID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error":   "Failed to insert into inv_track",
						"details": err.Error(),
					})
					return
				}
			case nil:
				updatedTrackQty := existingTrackQty + bom.BomQty
				query = `
				UPDATE inv_track
				SET bom_qty = $1, last_updated = $2
				WHERE project_id = $3 AND bom_id = $4 AND warehouse_id = $5`
				if _, err := db.Exec(query, updatedTrackQty, time.Now(), purchase.ProjectID, bom.BomID, purchase.WarehouseID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error":   "Failed to update inv_track",
						"details": err.Error(),
					})
					return
				}
			default:
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch track record",
					"details": err.Error(),
				})
				return
			}
		}

		// Insert into purchase table
		purchase.SubTotal = totalCost
		gstAmount := (totalCost * purchase.Tax) / 100

		query := `
		INSERT INTO inv_purchase (
		    purchase_id, description, project_id, vendor_id, warehouse_id, purchase_date, delivered_date,
		    sub_total, tax, total_cost, payment_mode, status, customer_note,
		    timedatestamp, updated_by, created_by
		) VALUES (
		    $1, $2, $3, $4, $5, $6,
		    $7, $8, $9, $10, $11, $12,
		    $13, $14, $15,$16
		) RETURNING purchase_id`

		var purchaseID int
		err = db.QueryRow(query,
			purchase.PurchaseID,
			purchase.Description,
			purchase.ProjectID,
			purchase.VendorID,
			purchase.WarehouseID,
			purchase.PurchaseDate,
			purchase.DeliveredDate,
			purchase.SubTotal,
			purchase.Tax,
			totalCost+gstAmount,
			purchase.PaymentMode,
			purchase.Status,
			purchase.CustomerNote,
			purchase.Timedatestamp,
			purchase.UpdatedBy,
			purchase.CreatedBy,
		).Scan(&purchaseID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to insert purchase",
				"details": err.Error(),
			})
			return
		}

		// Return the created purchase record
		purchase.PurchaseID = purchaseID
		purchase.TotalCost = totalCost + gstAmount
		c.JSON(http.StatusCreated, purchase)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Create",
			Description:  "Create Inventory Purchase",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    purchase.ProjectID, // No specific project ID for this operation
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

// FetchAllInvPurchases godoc
// @Summary      List all inventory purchases
// @Tags         inventory
// @Produce      json
// @Success      200  {array}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/inv_purchases [get]
func FetchAllInvPurchases(db *sql.DB) gin.HandlerFunc {
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
		SELECT 
			p.purchase_id, p.description, p.project_id, p.vendor_id, p.warehouse_id, 
			p.purchase_date, p.delivered_date, p.sub_total, p.tax, p.total_cost, 
			p.payment_mode, p.status, p.customer_note, p.timedatestamp, p.updated_by, p.created_by,
			v.name as vendor_name, w.name as warehouse_name
		FROM inv_purchase p
		LEFT JOIN inv_vendors v ON p.vendor_id = v.vendor_id
		LEFT JOIN inv_warehouse w ON p.warehouse_id = w.id`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer rows.Close()

		var purchases []models.InvPurchase
		for rows.Next() {
			var purchase models.InvPurchase
			var vendorName, warehouseName sql.NullString
			err := rows.Scan(
				&purchase.PurchaseID, &purchase.Description, &purchase.ProjectID, &purchase.VendorID,
				&purchase.WarehouseID, &purchase.PurchaseDate, &purchase.DeliveredDate, &purchase.SubTotal,
				&purchase.Tax, &purchase.TotalCost, &purchase.PaymentMode, &purchase.Status,
				&purchase.CustomerNote, &purchase.Timedatestamp, &purchase.UpdatedBy, &purchase.CreatedBy,
				&vendorName, &warehouseName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning data", "details": err.Error()})
				return
			}

			// Add names to the response
			purchase.VendorName = vendorName.String
			purchase.WarehouseName = warehouseName.String
			purchases = append(purchases, purchase)
		}

		// If no records found, return an empty list with 200 OK
		if len(purchases) == 0 {
			c.JSON(http.StatusOK, []models.InvPurchase{})
			return
		}

		// Return the purchases as JSON response
		c.JSON(http.StatusOK, purchases)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Fetch All Inventory Purchases",
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

// FetchInvPurchaseByID godoc
// @Summary      Get inventory purchase by ID
// @Tags         inventory
// @Param        id   path      int  true  "Purchase ID"
// @Success      200  {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Router       /api/inv_purchases/{id} [get]
func FetchInvPurchaseByID(db *sql.DB) gin.HandlerFunc {
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

		purchaseID := c.Param("id") // Get the purchase_id from the URL parameter

		query := `
		SELECT 
			p.purchase_id, p.description, p.project_id, p.vendor_id, p.warehouse_id, 
			p.purchase_date, p.delivered_date, p.sub_total, p.tax, p.total_cost, 
			p.payment_mode, p.status, p.customer_note, p.timedatestamp, p.updated_by, p.created_by,
			v.name as vendor_name, w.name as warehouse_name
		FROM inv_purchase p
		LEFT JOIN inv_vendors v ON p.vendor_id = v.vendor_id
		LEFT JOIN inv_warehouse w ON p.warehouse_id = w.id
		WHERE p.purchase_id = ?`

		var purchase models.InvPurchase
		var vendorName, warehouseName sql.NullString
		err = db.QueryRow(query, purchaseID).Scan(
			&purchase.PurchaseID, &purchase.Description, &purchase.ProjectID, &purchase.VendorID,
			&purchase.WarehouseID, &purchase.PurchaseDate, &purchase.DeliveredDate, &purchase.SubTotal,
			&purchase.Tax, &purchase.TotalCost, &purchase.PaymentMode, &purchase.Status,
			&purchase.CustomerNote, &purchase.Timedatestamp, &purchase.UpdatedBy, &purchase.CreatedBy,
			&vendorName, &warehouseName,
		)
		if err == sql.ErrNoRows {
			// If no rows are found, return 200 OK with a message
			c.JSON(http.StatusOK, gin.H{"message": "No data found"})
			return
		} else if err != nil {
			// For other errors, return a 500 status
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// Add names to the response
		purchase.VendorName = vendorName.String
		purchase.WarehouseName = warehouseName.String

		// Return the fetched purchase record
		c.JSON(http.StatusOK, purchase)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  fmt.Sprintf("Fetch Inventory Purchase of %s", purchaseID),
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

// FetchAllInvLineItems godoc
// @Summary      List all inventory line items
// @Tags         inventory
// @Success      200  {array}  object
// @Router       /api/invlineitems [get]
func FetchAllInvLineItems(db *sql.DB) gin.HandlerFunc {
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
		SELECT 
			l.items_id, l.purchase_id, l.bom_id, l.bom_qty, l.bom_rate, l.sub_total,
			b.product_name as bom_name
		FROM inv_line_items l
		LEFT JOIN inv_bom b ON l.bom_id = b.id`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer rows.Close()

		var lineItems []models.InvLineItem
		for rows.Next() {
			var lineItem models.InvLineItem
			var bomName sql.NullString
			err := rows.Scan(
				&lineItem.ItemsID, &lineItem.PurchaseID, &lineItem.BomID,
				&lineItem.BomQty, &lineItem.BomRate, &lineItem.SubTotal,
				&bomName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning data", "details": err.Error()})
				return
			}

			// Add BOM name to the response
			lineItem.BomName = bomName.String
			lineItems = append(lineItems, lineItem)
		}

		// If no records found, return an empty list with 200 OK
		if len(lineItems) == 0 {
			c.JSON(http.StatusOK, []models.InvLineItem{})
			return
		}

		// Return the line items as JSON response
		c.JSON(http.StatusOK, lineItems)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "fetch All Inventory Line Items",
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

// FetchInvLineItemByID godoc
// @Summary      Get inventory line item by ID
// @Tags         inventory
// @Param        id   path      int  true  "Line item ID"
// @Success      200  {object}  object
// @Router       /api/invlineitems/{id} [get]
func FetchInvLineItemByID(db *sql.DB) gin.HandlerFunc {
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

		itemsID := c.Param("id") // Get the items_id from the URL parameter

		query := `
		SELECT 
			l.items_id, l.purchase_id, l.bom_id, l.bom_qty, l.bom_rate, l.sub_total,
			b.product_name as bom_name
		FROM inv_line_items l
		LEFT JOIN inv_bom b ON l.bom_id = b.id
		WHERE l.items_id = ?`

		var lineItem models.InvLineItem
		var bomName sql.NullString
		err = db.QueryRow(query, itemsID).Scan(
			&lineItem.ItemsID, &lineItem.PurchaseID, &lineItem.BomID,
			&lineItem.BomQty, &lineItem.BomRate, &lineItem.SubTotal,
			&bomName,
		)
		if err == sql.ErrNoRows {
			// If no rows are found, return 200 OK with a message
			c.JSON(http.StatusOK, gin.H{"message": "No data found"})
			return
		} else if err != nil {
			// For other errors, return a 500 status
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// Add BOM name to the response
		lineItem.BomName = bomName.String

		// Return the fetched line item
		c.JSON(http.StatusOK, lineItem)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Inventory Line Item %s", itemsID),
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

// FetchAllInvTransactions godoc
// @Summary      List all inventory transactions
// @Tags         inventory
// @Success      200  {array}  object
// @Router       /api/invtransactions [get]
func FetchAllInvTransactions(db *sql.DB) gin.HandlerFunc {
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
		SELECT 
			t.inv_transaction_id, t.purchase_id, t.warehouse_id, t.project_id, t.task_id, 
			t.bom_id, t.bom_qty, t.status, t.time_date,
			b.product_name as bom_name, w.name as warehouse_name
		FROM inv_transaction t
		LEFT JOIN inv_bom b ON t.bom_id = b.id
		LEFT JOIN inv_warehouse w ON t.warehouse_id = w.id`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer rows.Close()

		var transactions []models.InvTransaction
		for rows.Next() {
			var transaction models.InvTransaction
			var purchaseID, taskID sql.NullInt64
			var bomName, warehouseName sql.NullString
			err := rows.Scan(
				&transaction.TransactionID, &purchaseID, &transaction.WarehouseID,
				&transaction.ProjectID, &taskID, &transaction.BomID,
				&transaction.BomQty, &transaction.Status, &transaction.TimeDate,
				&bomName, &warehouseName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning data", "details": err.Error()})
				return
			}

			// Handle NULL values
			if purchaseID.Valid {
				transaction.PurchaseID = int(purchaseID.Int64)
			} else {
				transaction.PurchaseID = 0
			}

			if taskID.Valid {
				transaction.TaskID = int(taskID.Int64)
			} else {
				transaction.TaskID = 0
			}

			// Add names to the response
			transaction.BomName = bomName.String
			transaction.WarehouseName = warehouseName.String
			transactions = append(transactions, transaction)
		}

		// If no records found, return an empty list with 200 OK
		if len(transactions) == 0 {
			c.JSON(http.StatusOK, []models.InvTransaction{})
			return
		}

		// Return the transactions as JSON response
		c.JSON(http.StatusOK, transactions)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Fetch All Inventory Transactions",
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

// FetchInvTransactionByID godoc
// @Summary      Get inventory transaction by ID
// @Tags         inventory
// @Param        id   path      int  true  "Transaction ID"
// @Success      200  {object}  object
// @Router       /api/invtransactions/{id} [get]
func FetchInvTransactionByID(db *sql.DB) gin.HandlerFunc {
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

		transactionID := c.Param("id") // Get the transaction_id from the URL parameter

		query := `
		SELECT 
			inv_transaction_id, purchase_id, warehouse_id, project_id, task_id, 
			bom_id, bom_qty, status, time_date
		FROM inv_transaction
		WHERE inv_transaction_id = ?`

		var transaction models.InvTransaction
		var purchaseID, taskID sql.NullInt64
		err = db.QueryRow(query, transactionID).Scan(
			&transaction.TransactionID, &purchaseID, &transaction.WarehouseID,
			&transaction.ProjectID, &taskID, &transaction.BomID,
			&transaction.BomQty, &transaction.Status, &transaction.TimeDate,
		)
		if err == sql.ErrNoRows {
			// If no rows are found, return 200 OK with a message
			c.JSON(http.StatusOK, gin.H{"message": "No data found"})
			return
		} else if err != nil {
			// For other errors, return a 500 status
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// Handle NULL values
		if purchaseID.Valid {
			transaction.PurchaseID = int(purchaseID.Int64)
		} else {
			transaction.PurchaseID = 0
		}

		if taskID.Valid {
			transaction.TaskID = int(taskID.Int64)
		} else {
			transaction.TaskID = 0
		}

		// Return the fetched transaction
		c.JSON(http.StatusOK, transaction)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Inventory Transaction of %s", transactionID),
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

// FetchAllInvTracks godoc
// @Summary      List all inventory tracks
// @Tags         inventory
// @Success      200  {array}  object
// @Router       /api/invtracks [get]
func FetchAllInvTracks(db *sql.DB) gin.HandlerFunc {
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
        SELECT 
            inv_track_id, project_id, bom_id, bom_qty, warehouse_id, last_updated, last_inv_transactionID
        FROM inv_track`

		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer rows.Close()

		var tracks []models.InvTrack
		for rows.Next() {
			var track models.InvTrack
			var lastInvTransactionID sql.NullInt64
			err := rows.Scan(
				&track.TrackID, &track.ProjectID, &track.BomID,
				&track.BomQty, &track.WarehouseID, &track.LastUpdated,
				&lastInvTransactionID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning data", "details": err.Error()})
				return
			}

			// Handle NULL values
			if lastInvTransactionID.Valid {
				track.LastInvTransactionID = int(lastInvTransactionID.Int64)
			} else {
				track.LastInvTransactionID = 0
			}
			tracks = append(tracks, track)
		}

		if len(tracks) == 0 {
			c.JSON(http.StatusOK, []models.InvTrack{})
			return
		}

		c.JSON(http.StatusOK, tracks)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Get All Inventory Tracks",
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

// FetchInvTrackByID godoc
// @Summary      Get inventory track by ID
// @Tags         inventory
// @Param        id   path      int  true  "Track ID"
// @Success      200  {object}  object
// @Router       /api/invtracks/{id} [get]
func FetchInvTrackByID(db *sql.DB) gin.HandlerFunc {
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

		trackID := c.Param("id") // Get the track_id from the URL parameter

		query := `
		SELECT 
			inv_track_id, project_id, bom_id, bom_qty, warehouse_id, last_updated, last_inv_transactionID
		FROM inv_track
		WHERE inv_track_id = ?`

		var track models.InvTrack
		var lastInvTransactionID sql.NullInt64
		err = db.QueryRow(query, trackID).Scan(
			&track.TrackID, &track.ProjectID, &track.BomID,
			&track.BomQty, &track.WarehouseID, &track.LastUpdated,
			&lastInvTransactionID,
		)
		if err == sql.ErrNoRows {
			// If no rows are found, return 200 OK with a message
			c.JSON(http.StatusOK, gin.H{"message": "No data found"})
			return
		} else if err != nil {
			// For other errors, return a 500 status
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// Handle NULL values
		if lastInvTransactionID.Valid {
			track.LastInvTransactionID = int(lastInvTransactionID.Int64)
		} else {
			track.LastInvTransactionID = 0
		}

		// Return the fetched track
		c.JSON(http.StatusOK, track)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  fmt.Sprintf("Get Inventory Track by %s", trackID),
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

type INVtask struct {
	ElementID int `json:"element_id"` // ID of the product
	TaskID    int `json:"task_id"`
}

func CreateInvTransactionByTask(db *sql.DB, elementID int, taskID int, projectID int) error {
	var transaction models.InvTransaction

	// Fetch element type ID using elementID
	var elementTypeID int
	query := `SELECT element_type_id FROM element WHERE id = $1`
	err := db.QueryRow(query, elementID).Scan(&elementTypeID)
	if err != nil {
		return fmt.Errorf("failed to fetch element type: %w", err)
	}

	// Fetch BOM rows from element_type_bom for this element type
	rows, err := db.Query(`
        SELECT product_id, quantity
        FROM element_type_bom
        WHERE element_type_id = $1
    `, elementTypeID)
	if err != nil {
		return fmt.Errorf("failed to fetch BOM rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var productID int
		var productQty float64
		if err := rows.Scan(&productID, &productQty); err != nil {
			return fmt.Errorf("failed to scan BOM row: %w", err)
		}

		transaction = models.InvTransaction{
			BomID:       productID,
			BomQty:      productQty,
			PurchaseID:  0,
			ProjectID:   projectID,
			WarehouseID: 0, // Initialize with default value
			Status:      "Subtract",
			TimeDate:    time.Now(),
			TaskID:      taskID,
		}

		// Fetch warehouse_id for the product
		query = `
            SELECT warehouse_id FROM inv_transaction
            WHERE project_id = $1 AND bom_id = $2`
		err = db.QueryRow(query, transaction.ProjectID, transaction.BomID).Scan(&transaction.WarehouseID)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("no warehouse found for project_id: %d and bom_id: %d", transaction.ProjectID, transaction.BomID)
			}
			return fmt.Errorf("failed to fetch warehouse: %w", err)
		}

		// Insert the transaction into inv_transactions table
		query = `
            INSERT INTO inv_transaction (purchase_id, warehouse_id, project_id, task_id, bom_id, bom_qty, status, time_date)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            RETURNING inv_transaction_id`
		var transactionID int
		err = db.QueryRow(query, transaction.PurchaseID, transaction.WarehouseID, transaction.ProjectID, transaction.TaskID,
			transaction.BomID, transaction.BomQty, transaction.Status, transaction.TimeDate).Scan(&transactionID)
		if err != nil {
			return fmt.Errorf("failed to insert transaction: %w", err)
		}

		// Fetch existing inventory track record
		var existingTrackQty float64
		query = `
            SELECT bom_qty FROM inv_track
            WHERE project_id = $1 AND bom_id = $2 AND warehouse_id = $3`
		err = db.QueryRow(query, transaction.ProjectID, transaction.BomID, transaction.WarehouseID).Scan(&existingTrackQty)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("no inventory record found to update for project_id: %d, bom_id: %d, warehouse_id: %d",
					transaction.ProjectID, transaction.BomID, transaction.WarehouseID)
			}
			return fmt.Errorf("failed to fetch inventory track record: %w", err)
		}

		// Update inventory track quantity
		updatedTrackQty := existingTrackQty - transaction.BomQty
		if updatedTrackQty < 0 {
			return fmt.Errorf("insufficient inventory: cannot subtract more than available quantity")
		}

		query = `
            UPDATE inv_track
            SET bom_qty = $1, last_updated = $2
            WHERE project_id = $3 AND bom_id = $4 AND warehouse_id = $5`
		_, err = db.Exec(query, updatedTrackQty, time.Now(), transaction.ProjectID, transaction.BomID, transaction.WarehouseID)
		if err != nil {
			return fmt.Errorf("failed to update inventory track: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating BOM rows: %w", err)
	}

	return nil
}

// InventoryView godoc
// @Summary      Inventory view (all)
// @Tags         inventory
// @Success      200  {object}  object
// @Router       /api/invatory_view [get]
func InventoryView(db *sql.DB) gin.HandlerFunc {
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

		// Fetch all data from inv_track, including warehouse_id
		trackQuery := `SELECT bom_id, bom_qty, warehouse_id FROM inv_track`
		trackRows, err := db.Query(trackQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer trackRows.Close()

		// Aggregating bom_qty for each bom_id and storing unique warehouse_ids
		trackData := make(map[int]struct {
			BomQty       int
			WarehouseIDs map[int]bool // Using a map to store unique warehouse_ids
		}) // map[bom_id]struct{ bom_qty, unique_warehouse_ids }

		for trackRows.Next() {
			var bomID, bomQty, warehouseID int
			if err := trackRows.Scan(&bomID, &bomQty, &warehouseID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning track data", "details": err.Error()})
				return
			}

			// Aggregate quantities and store unique warehouse_id for each bom_id
			track := trackData[bomID]
			track.BomQty += bomQty
			if track.WarehouseIDs == nil {
				track.WarehouseIDs = make(map[int]bool)
			}
			track.WarehouseIDs[warehouseID] = true // Ensure unique warehouse_id
			trackData[bomID] = track
		}

		// Fetch bom names and product types from inv_bom
		bomQuery := `SELECT id, product_name, product_type FROM inv_bom`
		bomRows, err := db.Query(bomQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer bomRows.Close()

		// Map bom_id to product name and product type
		bomData := make(map[int]string) // map[bom_id]product_name and product_type
		for bomRows.Next() {
			var bomID int
			var productName, productType string
			if err := bomRows.Scan(&bomID, &productName, &productType); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning bom data", "details": err.Error()})
				return
			}
			// Concatenate product_name and product_type with space
			bomData[bomID] = productName + " " + productType
		}

		// Fetch warehouse names based on warehouse_id (this fetches each warehouse only once)
		warehouseQuery := `SELECT id, name FROM inv_warehouse`
		warehouseRows, err := db.Query(warehouseQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer warehouseRows.Close()

		// Map warehouse_ids to warehouse names
		warehouseNames := make(map[int]string) // map[warehouse_id]warehouse_name
		for warehouseRows.Next() {
			var warehouseID int
			var warehouseName string
			if err := warehouseRows.Scan(&warehouseID, &warehouseName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning warehouse data", "details": err.Error()})
				return
			}
			warehouseNames[warehouseID] = warehouseName
		}

		// Prepare the final result
		var result []models.InventoryViewResponse
		for bomID, track := range trackData {
			// Collect unique warehouse names based on warehouse_ids
			var warehouseNamesList []string
			for warehouseID := range track.WarehouseIDs { // Iterate over unique warehouse_ids
				if name, exists := warehouseNames[warehouseID]; exists {
					warehouseNamesList = append(warehouseNamesList, name)
				}
			}
			warehouseNamesStr := strings.Join(warehouseNamesList, ",") // Join warehouse names with comma

			response := models.InventoryViewResponse{
				BomId:          bomID,
				BomQty:         track.BomQty,
				BomName:        bomData[bomID], // Merged product_name and product_type
				WarehouseNames: warehouseNamesStr,
			}
			result = append(result, response)
		}

		// Return the result
		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Get Inventory With BOM",
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

// InventoryViewProjectId godoc
// @Summary      Inventory view by project
// @Tags         inventory
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Router       /api/invatory_view/{project_id} [get]
func InventoryViewProjectId(db *sql.DB) gin.HandlerFunc {
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

		// Fetch the project_id from the request query parameters
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Project ID"})
			return
		}

		// Fetch all data from inv_track for the specific project_id
		trackQuery := `SELECT bom_id, bom_qty, warehouse_id FROM inv_track WHERE project_id = $1`
		trackRows, err := db.Query(trackQuery, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer trackRows.Close()

		// Aggregating bom_qty for each bom_id and storing unique warehouse_ids
		trackData := make(map[int]struct {
			BomQty       int
			WarehouseIDs map[int]bool // Using a map to store unique warehouse_ids
		}) // map[bom_id]struct{ bom_qty, unique_warehouse_ids }

		for trackRows.Next() {
			var bomID, bomQty, warehouseID int
			if err := trackRows.Scan(&bomID, &bomQty, &warehouseID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning track data", "details": err.Error()})
				return
			}

			// Aggregate quantities and store unique warehouse_id for each bom_id
			track := trackData[bomID]
			track.BomQty += bomQty
			if track.WarehouseIDs == nil {
				track.WarehouseIDs = make(map[int]bool)
			}
			track.WarehouseIDs[warehouseID] = true // Ensure unique warehouse_id
			trackData[bomID] = track
		}

		// Fetch bom names and product types from inv_bom
		bomQuery := `SELECT id, product_name, product_type FROM inv_bom`
		bomRows, err := db.Query(bomQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer bomRows.Close()

		// Map bom_id to product name and product type
		bomData := make(map[int]string) // map[bom_id]product_name and product_type
		for bomRows.Next() {
			var bomID int
			var productName, productType string
			if err := bomRows.Scan(&bomID, &productName, &productType); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning bom data", "details": err.Error()})
				return
			}
			// Concatenate product_name and product_type with space
			bomData[bomID] = productName + " " + productType
		}

		// Fetch warehouse names based on warehouse_id (this fetches each warehouse only once)
		warehouseQuery := `SELECT id, name FROM inv_warehouse`
		warehouseRows, err := db.Query(warehouseQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer warehouseRows.Close()

		// Map warehouse_ids to warehouse names
		warehouseNames := make(map[int]string) // map[warehouse_id]warehouse_name
		for warehouseRows.Next() {
			var warehouseID int
			var warehouseName string
			if err := warehouseRows.Scan(&warehouseID, &warehouseName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning warehouse data", "details": err.Error()})
				return
			}
			warehouseNames[warehouseID] = warehouseName
		}

		// Prepare the final result
		var result []models.InventoryViewResponse
		for bomID, track := range trackData {
			// Collect unique warehouse names based on warehouse_ids
			var warehouseNamesList []string
			for warehouseID := range track.WarehouseIDs { // Iterate over unique warehouse_ids
				if name, exists := warehouseNames[warehouseID]; exists {
					warehouseNamesList = append(warehouseNamesList, name)
				}
			}
			warehouseNamesStr := strings.Join(warehouseNamesList, ",") // Join warehouse names with comma

			response := models.InventoryViewResponse{
				BomId:          bomID,
				BomQty:         track.BomQty,
				BomName:        bomData[bomID], // Merged product_name and product_type
				WarehouseNames: warehouseNamesStr,
			}
			result = append(result, response)
		}

		// Return the result
		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Get Inventory With BOM",
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

// InventoryViewEachBOM godoc
// @Summary      Inventory view by BOM
// @Tags         inventory
// @Param        bom_id  path      int  true  "BOM ID"
// @Success      200     {object}  object
// @Router       /api/invatory_view_each_bom/{bom_id} [get]
func InventoryViewEachBOM(db *sql.DB) gin.HandlerFunc {
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

		// Fetch the project_id from the request query parameters
		BOMID, err := strconv.Atoi(c.Param("bom_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid BOM ID"})
			return
		}

		// Fetch all data from inv_track for the specific bom_id
		trackQuery := `SELECT bom_id, bom_qty, warehouse_id FROM inv_track WHERE bom_id = $1`
		trackRows, err := db.Query(trackQuery, BOMID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer trackRows.Close()

		// Aggregating bom_qty for each bom_id and warehouse_id
		trackData := make(map[int]map[int]int) // map[bom_id]map[warehouse_id]bom_qty
		for trackRows.Next() {
			var bomID, bomQty, warehouseID int
			if err := trackRows.Scan(&bomID, &bomQty, &warehouseID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning track data", "details": err.Error()})
				return
			}

			// Aggregate quantities for each bom_id and warehouse_id
			if trackData[bomID] == nil {
				trackData[bomID] = make(map[int]int)
			}
			trackData[bomID][warehouseID] += bomQty // Aggregate quantities for each warehouse
		}

		// Fetch bom names and product types from inv_bom
		bomQuery := `SELECT id, product_name, product_type FROM inv_bom`
		bomRows, err := db.Query(bomQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer bomRows.Close()

		// Map bom_id to product name and product type
		bomData := make(map[int]string) // map[bom_id]product_name and product_type
		for bomRows.Next() {
			var bomID int
			var productName, productType string
			if err := bomRows.Scan(&bomID, &productName, &productType); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning bom data", "details": err.Error()})
				return
			}
			// Concatenate product_name and product_type with space
			bomData[bomID] = productName + " " + productType
		}

		// Fetch warehouse names based on warehouse_id
		warehouseQuery := `SELECT id, name FROM inv_warehouse`
		warehouseRows, err := db.Query(warehouseQuery)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer warehouseRows.Close()

		// Map warehouse_ids to warehouse names
		warehouseNames := make(map[int]string) // map[warehouse_id]warehouse_name
		for warehouseRows.Next() {
			var warehouseID int
			var warehouseName string
			if err := warehouseRows.Scan(&warehouseID, &warehouseName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning warehouse data", "details": err.Error()})
				return
			}
			warehouseNames[warehouseID] = warehouseName
		}

		// Prepare the final result where each bom is listed with its warehouses and quantities
		var result []models.BomInventoryResponse // Changed from InventoryViewResponse to BomInventoryResponse
		for bomID, warehouseQtyMap := range trackData {
			// Create a list of warehouse and bom_qty pairings
			var warehouseDetails []models.WarehouseDetails
			for warehouseID, qty := range warehouseQtyMap {
				warehouseDetails = append(warehouseDetails, models.WarehouseDetails{
					WarehouseName: warehouseNames[warehouseID],
					BomQty:        qty,
				})
			}

			// Merged data for the same bom_id
			response := models.BomInventoryResponse{ // Changed from InventoryViewResponse to BomInventoryResponse
				BomId:         bomID,
				BomQty:        0,                // Total bom_qty can be calculated for all warehouses if needed
				BomName:       bomData[bomID],   // Merged product_name and product_type
				WarehouseData: warehouseDetails, // Show warehouse name with corresponding bom_qty
			}

			result = append(result, response)
		}

		// Return the result
		c.JSON(http.StatusOK, result)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Get Inventory With BOM",
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

// InventoryShortageRequest represents the request structure for checking inventory shortages
type InventoryShortageRequest struct {
	ProjectID int     `json:"project_id" binding:"required"`
	MinQty    float64 `json:"min_qty" binding:"required,gt=0"`
}

// InventoryShortageResponse represents the response structure for inventory shortages
type InventoryShortageResponse struct {
	BomID         int     `json:"bom_id"`
	BomName       string  `json:"bom_name"`
	CurrentQty    float64 `json:"current_qty"`
	MinQty        float64 `json:"min_qty"`
	ShortageQty   float64 `json:"shortage_qty"`
	WarehouseID   int     `json:"warehouse_id"`
	WarehouseName string  `json:"warehouse_name"`
	LastUpdated   string  `json:"last_updated"`
}

// PurchaseRequest represents the structure for generating purchase requests
type PurchaseRequest struct {
	ProjectID    int                   `json:"project_id" binding:"required"`
	VendorID     int                   `json:"vendor_id" binding:"required"`
	WarehouseID  int                   `json:"warehouse_id" binding:"required"`
	Description  string                `json:"description" binding:"required"`
	RequestedBy  string                `json:"requested_by" binding:"required"`
	Priority     string                `json:"priority"` // High, Medium, Low
	ExpectedDate string                `json:"expected_date"`
	Items        []PurchaseRequestItem `json:"items" binding:"required"`
}

// PurchaseRequestItem represents individual items in a purchase request
type PurchaseRequestItem struct {
	BomID        int     `json:"bom_id" binding:"required"`
	BomName      string  `json:"bom_name"`
	RequestedQty float64 `json:"requested_qty" binding:"required,gt=0"`
	MinQty       float64 `json:"min_qty"`
	CurrentQty   float64 `json:"current_qty"`
	Priority     string  `json:"priority"` // High, Medium, Low
}

// CheckInventoryShortage godoc
// @Summary      Check inventory shortage
// @Tags         inventory
// @Accept       json
// @Produce      json
// @Success      200  {object}  object
// @Router       /api/inventory_check_shortage [post]
func CheckInventoryShortage(db *sql.DB) gin.HandlerFunc {
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

		var request InventoryShortageRequest

		// Parse and validate input JSON
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid JSON input",
				"details": err.Error(),
			})
			return
		}

		// Validate that the project exists
		var projectExists bool
		query := `SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)`
		if err := db.QueryRow(query, request.ProjectID).Scan(&projectExists); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error checking project existence",
				"details": err.Error(),
			})
			return
		}

		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Project not found",
				"details": gin.H{
					"project_id": request.ProjectID,
				},
			})
			return
		}

		// Query for inventory items below minimum quantity
		query = `
			SELECT 
				t.bom_id,
				t.bom_qty as current_qty,
				t.warehouse_id,
				t.last_updated,
				w.name as warehouse_name
			FROM inv_track t
			JOIN inv_warehouse w ON t.warehouse_id = w.id
			WHERE t.project_id = $1 AND t.bom_qty < $2
			ORDER BY t.bom_qty ASC`

		rows, err := db.Query(query, request.ProjectID, request.MinQty)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database error",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		var shortages []InventoryShortageResponse
		for rows.Next() {
			var shortage InventoryShortageResponse
			var lastUpdated time.Time

			err := rows.Scan(
				&shortage.BomID,
				&shortage.CurrentQty,
				&shortage.WarehouseID,
				&lastUpdated,
				&shortage.WarehouseName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Error scanning data",
					"details": err.Error(),
				})
				return
			}

			shortage.MinQty = request.MinQty
			shortage.ShortageQty = request.MinQty - shortage.CurrentQty
			shortage.LastUpdated = lastUpdated.Format("2006-01-02 15:04:05")

			// Try to get BOM name if inv_bom table exists
			var bomName string
			bomQuery := `SELECT product_name FROM inv_bom WHERE id = $1`
			if err := db.QueryRow(bomQuery, shortage.BomID).Scan(&bomName); err != nil {
				shortage.BomName = fmt.Sprintf("BOM Item %d", shortage.BomID)
			} else {
				shortage.BomName = bomName
			}

			shortages = append(shortages, shortage)
		}

		// Prepare response
		response := gin.H{
			"project_id":      request.ProjectID,
			"min_qty":         request.MinQty,
			"total_shortages": len(shortages),
			"shortages":       shortages,
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Dashboard",
			EventName:    "Get",
			Description:  "Check Inventory Shortage",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    request.ProjectID,
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

// GeneratePurchaseRequest godoc
// @Summary      Generate purchase request for shortages
// @Tags         inventory
// @Accept       json
// @Produce      json
// @Success      200  {object}  object
// @Router       /api/inventory_generate_purchase_request [post]
func GeneratePurchaseRequest(db *sql.DB) gin.HandlerFunc {
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

		var request PurchaseRequest

		// Parse and validate input JSON
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid JSON input",
				"details": err.Error(),
			})
			return
		}

		// Validate that the project exists
		var projectExists bool
		query := `SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)`
		if err := db.QueryRow(query, request.ProjectID).Scan(&projectExists); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error checking project existence",
				"details": err.Error(),
			})
			return
		}

		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Project not found",
				"details": gin.H{
					"project_id": request.ProjectID,
				},
			})
			return
		}

		// Validate that the vendor exists
		var vendorExists bool
		query = `SELECT EXISTS(SELECT 1 FROM inv_vendors WHERE vendor_id = $1)`
		if err := db.QueryRow(query, request.VendorID).Scan(&vendorExists); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error checking vendor existence",
				"details": err.Error(),
			})
			return
		}

		if !vendorExists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Vendor not found",
				"details": gin.H{
					"vendor_id": request.VendorID,
				},
			})
			return
		}

		// Validate that the warehouse exists
		var warehouseExists bool
		query = `SELECT EXISTS(SELECT 1 FROM inv_warehouse WHERE id = $1)`
		if err := db.QueryRow(query, request.WarehouseID).Scan(&warehouseExists); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Error checking warehouse existence",
				"details": err.Error(),
			})
			return
		}

		if !warehouseExists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Warehouse not found",
				"details": gin.H{
					"warehouse_id": request.WarehouseID,
				},
			})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to start transaction",
				"details": err.Error(),
			})
			return
		}
		defer tx.Rollback()

		// Generate purchase request ID
		purchaseRequestID := repository.GenerateRandomNumber()

		// Insert purchase request record
		query = `
			INSERT INTO inv_purchase (
				purchase_id, description, project_id, vendor_id, warehouse_id,
				purchase_date, delivered_date, sub_total, tax, total_cost,
				payment_mode, status, customer_note, timedatestamp, updated_by, created_by
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
			) RETURNING purchase_id`

		var purchaseID int
		expectedDate := time.Now().AddDate(0, 0, 7) // Default to 7 days from now
		if request.ExpectedDate != "" {
			if parsedDate, err := time.Parse("2006-01-02", request.ExpectedDate); err == nil {
				expectedDate = parsedDate
			}
		}

		err = tx.QueryRow(query,
			purchaseRequestID,
			request.Description,
			request.ProjectID,
			request.VendorID,
			request.WarehouseID,
			time.Now(),   // purchase_date
			expectedDate, // delivered_date
			0.0,          // sub_total (will be calculated)
			0.0,          // tax
			0.0,          // total_cost (will be calculated)
			"Pending",    // payment_mode
			"Requested",  // status
			fmt.Sprintf("Auto-generated purchase request. Priority: %s", request.Priority),
			time.Now(), // timedatestamp
			request.RequestedBy,
			request.RequestedBy,
		).Scan(&purchaseID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to insert purchase request",
				"details": err.Error(),
			})
			return
		}

		// Process each item in the purchase request
		totalCost := 0.0
		for _, item := range request.Items {
			// Get current inventory for this item
			var currentQty float64
			query = `
				SELECT COALESCE(bom_qty, 0) FROM inv_track
				WHERE project_id = $1 AND bom_id = $2 AND warehouse_id = $3`
			err := tx.QueryRow(query, request.ProjectID, item.BomID, request.WarehouseID).Scan(&currentQty)
			if err != nil && err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to fetch current inventory",
					"details": err.Error(),
				})
				return
			}

			// Get BOM rate if available
			var bomRate float64
			bomQuery := `SELECT rate FROM inv_bom WHERE id = $1`
			if err := tx.QueryRow(bomQuery, item.BomID).Scan(&bomRate); err != nil {
				bomRate = 0.0 // Default rate if not found
			}

			subTotal := item.RequestedQty * bomRate
			totalCost += subTotal

			// Insert line item
			query = `
				INSERT INTO inv_line_items (items_id, purchase_id, bom_id, bom_qty, bom_rate, sub_total)
				VALUES (DEFAULT, $1, $2, $3, $4, $5)`
			if _, err := tx.Exec(query, purchaseID, item.BomID, item.RequestedQty, bomRate, subTotal); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to insert line item",
					"details": err.Error(),
				})
				return
			}
		}

		// Update purchase request with calculated totals
		query = `
			UPDATE inv_purchase
			SET sub_total = $1, total_cost = $2
			WHERE purchase_id = $3`
		if _, err := tx.Exec(query, totalCost, totalCost, purchaseID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to update purchase totals",
				"details": err.Error(),
			})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to commit transaction",
				"details": err.Error(),
			})
			return
		}

		// Fetch vendor and warehouse details for response
		var vendorName, warehouseName string
		query = `SELECT name FROM inv_vendors WHERE vendor_id = $1`
		if err := db.QueryRow(query, request.VendorID).Scan(&vendorName); err != nil {
			vendorName = "Unknown Vendor"
		}

		query = `SELECT name FROM inv_warehouse WHERE id = $1`
		if err := db.QueryRow(query, request.WarehouseID).Scan(&warehouseName); err != nil {
			warehouseName = "Unknown Warehouse"
		}

		// Prepare response
		response := gin.H{
			"message": "Purchase request generated successfully",
			"data": gin.H{
				"purchase_id":    purchaseID,
				"description":    request.Description,
				"project_id":     request.ProjectID,
				"vendor_id":      request.VendorID,
				"vendor_name":    vendorName,
				"warehouse_id":   request.WarehouseID,
				"warehouse_name": warehouseName,
				"priority":       request.Priority,
				"expected_date":  expectedDate.Format("2006-01-02"),
				"total_cost":     totalCost,
				"items_count":    len(request.Items),
				"requested_by":   request.RequestedBy,
				"status":         "Requested",
				"created_at":     time.Now(),
			},
		}

		c.JSON(http.StatusCreated, response)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Generate Purchase Request",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    request.ProjectID,
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

// GetInventoryShortageSummary godoc
// @Summary      Get inventory shortage summary
// @Tags         inventory
// @Success      200  {object}  object
// @Router       /api/inventory_shortage_summary [get]
func GetInventoryShortageSummary(db *sql.DB) gin.HandlerFunc {
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

		// Get minimum quantity from query parameter or use default
		minQtyStr := c.DefaultQuery("min_qty", "10")
		minQty, err := strconv.ParseFloat(minQtyStr, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid min_qty parameter",
				"details": "min_qty must be a valid number",
			})
			return
		}

		// Query for all inventory shortages across projects
		query := `
			SELECT 
				t.project_id,
				p.name as project_name,
				t.bom_id,
				t.bom_qty as current_qty,
				t.warehouse_id,
				w.name as warehouse_name,
				t.last_updated
			FROM inv_track t
			JOIN project p ON t.project_id = p.project_id
			JOIN inv_warehouse w ON t.warehouse_id = w.id
			WHERE t.bom_qty < $1
			ORDER BY t.project_id, t.bom_qty ASC`

		rows, err := db.Query(query, minQty)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database error",
				"details": err.Error(),
			})
			return
		}
		defer rows.Close()

		// Group shortages by project
		projectShortages := make(map[int]gin.H)
		totalShortages := 0

		for rows.Next() {
			var projectID int
			var projectName string
			var bomID int
			var currentQty float64
			var warehouseID int
			var warehouseName string
			var lastUpdated time.Time

			err := rows.Scan(
				&projectID,
				&projectName,
				&bomID,
				&currentQty,
				&warehouseID,
				&warehouseName,
				&lastUpdated,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Error scanning data",
					"details": err.Error(),
				})
				return
			}

			// Get BOM name if available
			var bomName string
			bomQuery := `SELECT product_name FROM inv_bom WHERE id = $1`
			if err := db.QueryRow(bomQuery, bomID).Scan(&bomName); err != nil {
				bomName = fmt.Sprintf("BOM Item %d", bomID)
			}

			shortage := gin.H{
				"bom_id":         bomID,
				"bom_name":       bomName,
				"current_qty":    currentQty,
				"min_qty":        minQty,
				"shortage_qty":   minQty - currentQty,
				"warehouse_id":   warehouseID,
				"warehouse_name": warehouseName,
				"last_updated":   lastUpdated.Format("2006-01-02 15:04:05"),
			}

			if projectShortages[projectID] == nil {
				projectShortages[projectID] = gin.H{
					"project_id":   projectID,
					"project_name": projectName,
					"shortages":    []gin.H{},
				}
			}

			projectData := projectShortages[projectID]
			shortages := projectData["shortages"].([]gin.H)
			shortages = append(shortages, shortage)
			projectData["shortages"] = shortages
			projectShortages[projectID] = projectData

			totalShortages++
		}

		// Convert map to slice for response
		var projects []gin.H
		for _, projectData := range projectShortages {
			projects = append(projects, projectData)
		}

		// Prepare response
		response := gin.H{
			"min_qty":         minQty,
			"total_projects":  len(projects),
			"total_shortages": totalShortages,
			"projects":        projects,
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Inventory",
			EventName:    "Get",
			Description:  "Get Inventory Shortage Summary",
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

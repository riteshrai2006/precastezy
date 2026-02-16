package handlers

import (
	"backend/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// CreateInvoice godoc
// @Summary      Create invoice
// @Description  Create a new invoice for a work order
// @Tags         invoices
// @Accept       json
// @Produce      json
// @Param        body  body      models.Invoice  true  "Invoice data"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/invoices [post]
func CreateInvoice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- check session ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var createdBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&createdBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- bind JSON ---
		var inv models.Invoice
		if err := c.ShouldBindJSON(&inv); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- get revision number ---
		var revisionNo int
		err = tx.QueryRow(
			`SELECT COALESCE(MAX(revision_no), 0) + 1 FROM invoice WHERE work_order_id=$1`,
			inv.WorkOrderID,
		).Scan(&revisionNo)
		if err != nil {
			// If no rows found (sql.ErrNoRows), default to revision 0
			if err == sql.ErrNoRows {
				revisionNo = 0
			} else {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"details": "1", "error": err.Error()})
				return
			}
		}

		// --- fetch endclient_id and project_id from work_order ---
		var endClientID, projectID int
		err = tx.QueryRow(`SELECT endclient_id, project_id FROM work_order WHERE id=$1`, inv.WorkOrderID).Scan(&endClientID, &projectID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch work order details: " + err.Error()})
			return
		}

		// --- fetch endclient name and project name ---
		var endClientName, projectName string
		err = tx.QueryRow(`SELECT abbreviation FROM end_client WHERE id=$1`, endClientID).Scan(&endClientName)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch end client name: " + err.Error()})
			return
		}

		err = tx.QueryRow(`SELECT abbreviation FROM project WHERE project_id=$1`, projectID).Scan(&projectName)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project name: " + err.Error()})
			return
		}

		// --- create invoice name ---
		name := fmt.Sprintf("%s-%s-%d", endClientName, projectName, revisionNo)

		// --- insert invoice ---
		var invoiceID int
		err = tx.QueryRow(`
			INSERT INTO invoice (work_order_id, created_by, revision_no, billing_address, shipping_address, name, total_amount) 
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			inv.WorkOrderID, createdBy, revisionNo, inv.BillingAddress, inv.ShippingAddress, name, inv.TotalAmount,
		).Scan(&invoiceID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- insert items & update volume_used ---
		for _, item := range inv.Items {
			// Insert invoice item
			_, err := tx.Exec(`
				INSERT INTO invoice_item (invoice_id, item_id, volume) 
				VALUES ($1,$2,$3)`,
				invoiceID, item.ItemID, item.Volume,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Update volume_used in workorder_material
			_, err = tx.Exec(`
				UPDATE work_order_material
				SET volume_used = volume_used + $1 , hsn_code = $4
				WHERE work_order_id = $2 AND id = $3`,
				item.Volume, inv.WorkOrderID, item.ItemID, item.HSNCode,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message":     "Invoice created successfully",
			"id":          invoiceID,
			"revision_no": revisionNo,
		})
	}
}

// GetAllInvoicesByWorkOrderId godoc
// @Summary      Get all invoices by work order ID
// @Tags         invoices
// @Param        id   path  int  true  "Work order ID"
// @Success      200  {array}  models.Invoice
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/allinvoices/{id} [get]
func GetAllInvoicesByWorkOrderId(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth check ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var userID int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- get work_order_id from URL ---
		workOrderID := c.Param("id")
		if workOrderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing work_order_id"})
			return
		}

		// --- fetch all invoices for this work order ---
		rows, err := db.Query(`
			SELECT i.id, i.work_order_id, i.created_by, i.revision_no, i.billing_address, i.shipping_address, i.created_at, CONCAT(u.first_name, ' ', u.last_name) AS created_by_name, i.name,
			i.payment_status, i.total_paid, i.total_amount, i.updated_by, i.updated_at
			FROM invoice i
			JOIN users u ON i.created_by = u.id
			WHERE work_order_id = $1
			ORDER BY revision_no ASC
		`, workOrderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var invoices []models.Invoice

		for rows.Next() {
			var inv models.Invoice

			var totalPaid, totalAmount sql.NullFloat64
			var updatedBy sql.NullInt64
			var updatedAt sql.NullTime

			if err := rows.Scan(
				&inv.ID,
				&inv.WorkOrderID,
				&inv.CreatedBy,
				&inv.RevisionNo,
				&inv.BillingAddress,
				&inv.ShippingAddress,
				&inv.CreatedAt,
				&inv.CreatedByName,
				&inv.Name,
				&inv.PaymentStatus,
				&totalPaid,
				&totalAmount,
				&updatedBy,
				&updatedAt,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if totalPaid.Valid {
				inv.TotalPaid = totalPaid.Float64
			} else {
				inv.TotalPaid = 0.0
			}

			if totalAmount.Valid {
				inv.TotalAmount = totalAmount.Float64
			} else {
				inv.TotalAmount = 0.0
			}

			if updatedBy.Valid {
				inv.UpdatedBy = int(updatedBy.Int64)
			} else {
				inv.UpdatedBy = 0 // Or use a pointer type if you want to represent null
			}

			if updatedAt.Valid {
				inv.UpdatedAt = updatedAt.Time
			}

			invoices = append(invoices, inv)

		}

		// --- ensure we always return an array (even if empty) ---
		if invoices == nil {
			invoices = []models.Invoice{}
		}

		c.JSON(http.StatusOK, invoices)
	}
}

// GetInvoice godoc
// @Summary      Get invoice by ID
// @Tags         invoices
// @Param        id   path  int  true  "Invoice ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      404  {object}  object
// @Router       /api/invoice/{id} [get]
func GetInvoice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Auth check ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var userID int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- Get invoice_id from URL ---
		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		// --- Fetch invoice details ---
		var inv models.GetInvoice

		var totalPaid, totalAmount sql.NullFloat64
		var updatedBy sql.NullInt64
		var updatedAt sql.NullTime

		var paymentTermRaw sql.NullString

		err := db.QueryRow(`
			SELECT 
				i.id, i.work_order_id, i.created_by, i.revision_no, i.billing_address, 
				i.shipping_address, i.created_at, i.payment_status, i.total_paid, i.total_amount, i.updated_by, i.updated_at,
				CONCAT(u.first_name, ' ', u.last_name) AS created_by_name, i.name,
				wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value, 
				wo.contact_person, wo.payment_term::text, wo.wo_description,
				wo.endclient_id, ec.contact_person AS endclient_name,
				wo.project_id, p.name AS project_name, 
				wo.contact_email, wo.contact_number, wo.phone_code, 
				pc.phone_code AS phone_code_name, wo.revision
			FROM invoice i
			JOIN users u ON i.created_by = u.id
			JOIN work_order wo ON i.work_order_id = wo.id
			JOIN end_client ec ON wo.endclient_id = ec.id
			JOIN project p ON wo.project_id = p.project_id
			JOIN phone_code pc ON wo.phone_code = pc.id
			WHERE i.id = $1
		`, invoiceID).Scan(
			&inv.ID,
			&inv.WorkOrderID,
			&inv.CreatedBy,
			&inv.RevisionNo,
			&inv.BillingAddress,
			&inv.ShippingAddress,
			&inv.CreatedAt,
			&inv.PaymentStatus,
			&totalPaid,
			&totalAmount,
			&updatedBy,
			&updatedAt,
			&inv.CreatedByName,
			&inv.Name,
			&inv.WONumber,
			&inv.WODate,
			&inv.WOValidate,
			&inv.TotalValue,
			&inv.ContactPerson,
			&paymentTermRaw,
			&inv.WODescription,
			&inv.EndClientID,
			&inv.EndClient,
			&inv.ProjectID,
			&inv.ProjectName,
			&inv.ContactEmail,
			&inv.ContactNumber,
			&inv.PhoneCode,
			&inv.PhoneCodeName,
			&inv.WORevision,
		)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Unmarshal payment_term JSONB
		if paymentTermRaw.Valid && paymentTermRaw.String != "" {
			var pt map[string]float64
			if err := json.Unmarshal([]byte(paymentTermRaw.String), &pt); err == nil {
				inv.PaymentTerm = pt
			} else {
				inv.PaymentTerm = make(map[string]float64)
			}
		} else {
			inv.PaymentTerm = make(map[string]float64)
		}

		if totalPaid.Valid {
			inv.TotalPaid = totalPaid.Float64
		} else {
			inv.TotalPaid = 0.0
		}

		if totalAmount.Valid {
			inv.TotalAmount = totalAmount.Float64
		} else {
			inv.TotalAmount = 0.0
		}

		if updatedBy.Valid {
			inv.UpdatedBy = int(updatedBy.Int64)
		} else {
			inv.UpdatedBy = 0 // Or use a pointer type if you want to represent null
		}

		if updatedAt.Valid {
			inv.UpdatedAt = updatedAt.Time
		}

		// ✅ Append revision number to wo_number if not zero
		if inv.WORevision != 0 {
			inv.WONumber = fmt.Sprintf("%s - %d", inv.WONumber, inv.RevisionNo)
		}

		// --- Fetch associated items ---
		rows, err := db.Query(`
			SELECT i.id, i.invoice_id, i.item_id, i.volume, wom.hsn_code, wom.tower_id, wom.floor_id, wom.item_name, wom.unit_rate, wom.tax, wom.volume_used
			FROM invoice_item i
			LEFT JOIN work_order_material wom ON i.item_id = wom.id
			WHERE invoice_id = $1
		`, invoiceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var items []models.GetInvoiceItem
		for rows.Next() {
			var item models.GetInvoiceItem
			var towerID sql.NullInt64
			var floorIDs pq.Int64Array

			if err := rows.Scan(&item.ID, &item.InvoiceID, &item.ItemID, &item.Volume, &item.HSNCode, &towerID, &floorIDs, &item.ItemName, &item.UnitRate, &item.Tax, &item.VolumeUsed); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			item.Balance = item.Volume - item.VolumeUsed

			// tower_id
			if towerID.Valid {
				tid := int(towerID.Int64)
				item.TowerID = &tid
				var towerName string
				db.QueryRow(`SELECT name FROM precast WHERE id=$1`, tid).Scan(&towerName)
				item.TowerName = towerName
			} else {
				tid := 0
				item.TowerID = &tid
				item.TowerName = ""
			}

			// floor_id & floor_name
			if len(floorIDs) > 0 {
				item.FloorID = []int{}
				item.FloorName = []string{}
				for _, fid := range floorIDs {
					id := int(fid)
					item.FloorID = append(item.FloorID, id)
					var floorName string
					db.QueryRow(`SELECT name FROM precast WHERE id=$1`, id).Scan(&floorName)
					item.FloorName = append(item.FloorName, floorName)
				}
			} else {
				item.FloorID = []int{0}
				item.FloorName = []string{""}
			}

			items = append(items, item)
		}

		if items == nil {
			items = []models.GetInvoiceItem{}
		}

		inv.Items = items

		// --- Stage-wise and element_type-wise breakdown using element_invoice_history ---
		// Build payment term map (lowercase keys)
		paymentTermPct := map[string]float64{}
		for k, v := range inv.PaymentTerm {
			paymentTermPct[strings.ToLower(k)] = v
		}

		hRows, err := db.Query(`
			SELECT h.stage,
			       et.element_type,
			       COALESCE(e.target_location, 0) AS floor_id,
			       COALESCE(pc.parent_id, 0) AS tower_id,
			       COALESCE(pc.name, '') AS floor_name,
			       COALESCE(pct.name, '') AS tower_name,
			       COALESCE(SUM(h.volume),0) AS total_volume,
			       COALESCE(wom.unit_rate, 0) AS unit_rate,
			       COALESCE(wom.tax, 0) AS tax
			FROM element_invoice_history h
			JOIN element e ON e.id = h.element_id
			LEFT JOIN precast pc ON pc.id = e.target_location
			LEFT JOIN precast pct ON pct.id = pc.parent_id
			JOIN element_type et ON et.element_type_id = e.element_type_id
			LEFT JOIN work_order_material wom 
			  ON wom.work_order_id = $2
			 AND LOWER(wom.item_name) = LOWER(et.element_type)
			WHERE h.invoice_id = $1
			GROUP BY h.stage, et.element_type, COALESCE(e.target_location, 0), COALESCE(pc.parent_id, 0), COALESCE(pc.name, ''), COALESCE(pct.name, ''), wom.unit_rate, wom.tax
		`, invoiceID, inv.WorkOrderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer hRows.Close()

		type ElementTypeBreakdown struct {
			ElementType string  `json:"element_type"`
			TowerID     int     `json:"tower_id"`
			FloorID     int     `json:"floor_id"`
			TowerName   string  `json:"tower_name"`
			FloorName   string  `json:"floor_name"`
			TotalVolume float64 `json:"total_volume"`
			UnitRate    float64 `json:"unit_rate"`
			Tax         float64 `json:"tax"`
			Amount      float64 `json:"amount"`
		}
		type StageSummary struct {
			Stage              string                 `json:"stage"`
			PaymentTermPercent float64                `json:"payment_term_percent"`
			TotalAmount        float64                `json:"total_amount"`
			AmountPaidByTerm   float64                `json:"amount_paid_by_payment_term"`
			ElementTypes       []ElementTypeBreakdown `json:"element_types"`
		}

		stageToSummary := map[string]*StageSummary{}

		for hRows.Next() {
			var stageRaw, etName string
			var towerID, floorID int
			var floorName, towerName string
			var volume, unitRate, tax float64
			if err := hRows.Scan(&stageRaw, &etName, &floorID, &towerID, &floorName, &towerName, &volume, &unitRate, &tax); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			stageKey := strings.ToLower(stageRaw)
			if stageKey == "dispatched" {
				stageKey = "dispatch"
			}

			sum, ok := stageToSummary[stageKey]
			if !ok {
				sum = &StageSummary{Stage: stageKey, PaymentTermPercent: paymentTermPct[stageKey]}
				stageToSummary[stageKey] = sum
			}

			amount := unitRate * volume
			if tax > 0 {
				amount = amount + (amount * tax / 100.0)
			}
			sum.TotalAmount += amount
			sum.ElementTypes = append(sum.ElementTypes, ElementTypeBreakdown{
				ElementType: etName,
				TowerID:     towerID,
				FloorID:     floorID,
				TowerName:   towerName,
				FloorName:   floorName,
				TotalVolume: volume,
				UnitRate:    unitRate,
				Tax:         tax,
				Amount:      amount,
			})
		}

		if err := hRows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		stageSummaries := []StageSummary{}
		for _, key := range []string{"casted", "dispatch", "erection", "handover"} {
			if sum, ok := stageToSummary[key]; ok {
				sum.AmountPaidByTerm = sum.TotalAmount * (sum.PaymentTermPercent / 100.0)
				stageSummaries = append(stageSummaries, *sum)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"invoice":       inv,
			"stage_summary": stageSummaries,
		})
	}
}

// GetAllInvoices godoc
// @Summary      Get all invoices (paginated, role-based)
// @Tags         invoices
// @Param        page       query  int  false  "Page"
// @Param        page_size  query  int  false  "Page size"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      403  {object}  object
// @Router       /api/invoices [get]
func GetAllInvoices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		/* ---------------- PAGINATION ---------------- */
		pageStr := c.Query("page")
		limitStr := c.Query("page_size")

		usePagination := pageStr != "" || limitStr != ""

		page := 1
		limit := 10

		if usePagination {
			page, _ = strconv.Atoi(pageStr)
			limit, _ = strconv.Atoi(limitStr)

			if page < 1 {
				page = 1
			}
			if limit < 1 || limit > 100 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		// Validate session and fetch role details
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 2: Get user_id from session
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Step 3: Get role_id from users table
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}

		// Step 4: Get role name from roles table
		var roleName string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		// Build access control condition
		var accessCondition string
		var args []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			accessCondition = "1=1" // No restriction

		case "admin":
			accessCondition = `
				wo.endclient_id IN (
					SELECT ec.id 
					FROM end_client ec
					JOIN client c ON ec.client_id = c.client_id
					WHERE c.user_id = $` + fmt.Sprint(argIndex) + `
				)
			`
			args = append(args, userID)
			argIndex++

		default:
			// No permission for other roles
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "No permission to view invoices",
			})
			return
		}

		// Query invoices with access control
		query := fmt.Sprintf(`
			SELECT 
				i.id, i.work_order_id, i.created_by, i.revision_no, i.billing_address, 
				i.shipping_address, i.created_at, i.name, i.payment_status, i.total_paid, i.total_amount, i.updated_by, i.updated_at,
				CONCAT(u.first_name, ' ', u.last_name) AS created_by_name,
				wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value, 
				wo.contact_person, wo.payment_term::text, wo.wo_description,
				wo.endclient_id, ec.contact_person AS endclient_name,
				wo.project_id, p.name AS project_name, 
				wo.contact_email, wo.contact_number, wo.phone_code, 
				pc.phone_code AS phone_code_name, wo.revision
			FROM invoice i
			JOIN users u ON i.created_by = u.id
			JOIN work_order wo ON i.work_order_id = wo.id
			JOIN end_client ec ON wo.endclient_id = ec.id
			JOIN project p ON wo.project_id = p.project_id
			JOIN phone_code pc ON wo.phone_code = pc.id
			WHERE i.indraft = false AND %s
			ORDER BY i.created_at DESC
		`, accessCondition)
		
		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)
		}

		// Get total count for pagination
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*) 
			FROM invoice i
			JOIN work_order wo ON i.work_order_id = wo.id
			JOIN end_client ec ON wo.endclient_id = ec.id
			JOIN client c ON ec.client_id = c.client_id
			WHERE i.indraft = false AND %s
		`, accessCondition)

		var totalCount int
		var countArgs []interface{}
		if usePagination {
			countArgs = args[:len(args)-2] // Remove limit and offset for count
		} else {
			countArgs = args
		}
		err = db.QueryRow(countQuery, countArgs...).Scan(&totalCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "count", "error": err.Error()})
			return
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var invoices []models.GetInvoice

		for rows.Next() {
			var inv models.GetInvoice

			var totalPaid, totalAmount sql.NullFloat64
			var updatedBy sql.NullInt64
			var updatedAt sql.NullTime
			var paymentTermRaw sql.NullString

			if err := rows.Scan(
				&inv.ID,
				&inv.WorkOrderID,
				&inv.CreatedBy,
				&inv.RevisionNo,
				&inv.BillingAddress,
				&inv.ShippingAddress,
				&inv.CreatedAt,
				&inv.Name,          // <- FIX: 'name' comes before payment_status
				&inv.PaymentStatus, // <- FIX: then payment_status
				&totalPaid,
				&totalAmount,
				&updatedBy,
				&updatedAt,
				&inv.CreatedByName,
				&inv.WONumber,
				&inv.WODate,
				&inv.WOValidate,
				&inv.TotalValue,
				&inv.ContactPerson,
				&paymentTermRaw,
				&inv.WODescription,
				&inv.EndClientID,
				&inv.EndClient,
				&inv.ProjectID,
				&inv.ProjectName,
				&inv.ContactEmail,
				&inv.ContactNumber,
				&inv.PhoneCode,
				&inv.PhoneCodeName,
				&inv.WORevision,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Unmarshal payment_term JSONB
			if paymentTermRaw.Valid && paymentTermRaw.String != "" {
				var pt map[string]float64
				if err := json.Unmarshal([]byte(paymentTermRaw.String), &pt); err == nil {
					inv.PaymentTerm = pt
				} else {
					inv.PaymentTerm = make(map[string]float64)
				}
			} else {
				inv.PaymentTerm = make(map[string]float64)
			}

			if totalPaid.Valid {
				inv.TotalPaid = totalPaid.Float64
			} else {
				inv.TotalPaid = 0.0
			}

			if totalAmount.Valid {
				inv.TotalAmount = totalAmount.Float64
			} else {
				inv.TotalAmount = 0.0
			}

			if updatedBy.Valid {
				inv.UpdatedBy = int(updatedBy.Int64)
			} else {
				inv.UpdatedBy = 0 // Or use a pointer type if you want to represent null
			}

			if updatedAt.Valid {
				inv.UpdatedAt = updatedAt.Time
			}

			// ✅ Append revision number to wo_number if not zero
			if inv.WORevision != 0 {
				inv.WONumber = fmt.Sprintf("%s - %d", inv.WONumber, inv.RevisionNo)
			}

			// --- Fetch items for this invoice ---
			itemRows, err := db.Query(`
				SELECT i.id, i.invoice_id, i.item_id, i.volume, wom.hsn_code, wom.tower_id, wom.floor_id, wom.item_name, wom.unit_rate, wom.tax
				FROM invoice_item i
				LEFT JOIN work_order_material wom ON i.item_id = wom.id
				WHERE i.invoice_id = $1
			`, inv.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var items []models.GetInvoiceItem
			for itemRows.Next() {
				var item models.GetInvoiceItem

				var hsnCode sql.NullInt64
				var floorIDs pq.Int64Array
				var name sql.NullString
				var unitRate sql.NullFloat64
				var tax sql.NullFloat64

				if err := itemRows.Scan(
					&item.ID,
					&item.InvoiceID,
					&item.ItemID,
					&item.Volume,
					&hsnCode,
					&item.TowerID,
					&floorIDs,
					&name,
					&unitRate,
					&tax,
				); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}

				if hsnCode.Valid {
					item.HSNCode = int(hsnCode.Int64)
				} else {
					item.HSNCode = 0 // or leave empty / default value
				}

				if name.Valid {
					item.ItemName = name.String
				} else {
					item.ItemName = ""
				}

				if unitRate.Valid {
					item.UnitRate = unitRate.Float64
				} else {
					item.UnitRate = 0.0
				}

				if tax.Valid {
					item.Tax = tax.Float64
				} else {
					item.Tax = 0.0
				}

				items = append(items, item)
			}
			itemRows.Close()

			if items == nil {
				items = []models.GetInvoiceItem{}
			}

			inv.Items = items
			invoices = append(invoices, inv)
		}

		if invoices == nil {
			invoices = []models.GetInvoice{}
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": invoices,
		}

		// Only include pagination if pagination parameters were provided
		if usePagination {
			totalPages := (totalCount + limit - 1) / limit
			response["pagination"] = gin.H{
				"current_page": page,
				"per_page":     limit,
				"total":        totalCount,
				"total_pages":  totalPages,
			}
		}

		c.JSON(http.StatusOK, response)

		log := models.ActivityLog{
			EventContext: "Invoice",
			EventName:    "Get",
			Description:  "Fetched all invoices",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}
		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Invoices fetched but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// UpdateInvoice godoc
// @Summary      Update invoice
// @Tags         invoices
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "Invoice ID"
// @Param        body  body  models.Invoice  true  "Invoice data"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/invoices/{id} [put]
func UpdateInvoice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var updatedBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&updatedBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- get invoice id from params ---
		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		// --- bind JSON body ---
		var inv models.Invoice
		if err := c.ShouldBindJSON(&inv); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// --- start transaction ---
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- update invoice details ---
		_, err = tx.Exec(`
			UPDATE invoice 
			SET billing_address = $1, 
			    shipping_address = $2,
			    updated_at = NOW(),
			    updated_by = $3
			WHERE id = $4`,
			inv.BillingAddress, inv.ShippingAddress, updatedBy, invoiceID,
		)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- update invoice items ---
		for _, item := range inv.Items {

			var volume float64
			err = tx.QueryRow(`
					  SELECT volume FROM invoice_item
					  WHERE invoice_id = $1 AND item_id = $2`,
				invoiceID, item.ItemID,
			).Scan(&volume)
			if err == sql.ErrNoRows {
				tx.Rollback()
				c.JSON(http.StatusBadRequest, gin.H{"error": "Item not found in this invoice"})
				return
			} else if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			// update invoice_item table
			_, err = tx.Exec(`
				UPDATE invoice_item
				SET volume = $1
				WHERE invoice_id = $2 AND item_id = $3`,
				item.Volume, invoiceID, item.ItemID,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var volumeUsed float64
			err = tx.QueryRow(`
				SELECT volume_used FROM work_order_material
				WHERE work_order_id = (SELECT work_order_id FROM invoice WHERE id = $1) AND id = $2
			`, invoiceID, item.ItemID).Scan(&volumeUsed)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// volume_used - item.Volume + volume
			newVolume := volumeUsed - volume + item.Volume

			// update work_order_material table for volume_used & HSN code
			_, err = tx.Exec(`
				UPDATE work_order_material
				SET volume_used = volume_used + $1, hsn_code = $4
				WHERE work_order_id = $2 AND id = $3`,
				newVolume, inv.WorkOrderID, item.ItemID, item.HSNCode,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		// --- commit transaction ---
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "Invoice updated successfully",
			"invoiceID": invoiceID,
			"updatedBy": updatedBy,
		})
	}
}

// SubmitInvoice godoc
// @Summary      Submit invoice (mark as not draft)
// @Tags         invoices
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "Invoice ID"
// @Param        body  body  object  true  "billing_address, shipping_address"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      404  {object}  object
// @Router       /api/invoice/{id}/submit [put]
func SubmitInvoice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var updatedBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&updatedBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		// --- bind JSON body ---
		var req struct {
			BillingAddress  string `json:"billing_address"`
			ShippingAddress string `json:"shipping_address"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		res, err := db.Exec(`
			UPDATE invoice
			SET indraft = FALSE,
			    billing_address = $2,
			    shipping_address = $3,
			    updated_at = NOW(),
			    updated_by = $4
			WHERE id = $1`, invoiceID, req.BillingAddress, req.ShippingAddress, updatedBy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		affected, _ := res.RowsAffected()
		if affected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Invoice submitted", "invoice_id": invoiceID})
	}
}

// UpdateInvoicePayment godoc
// @Summary      Add/update invoice payment
// @Tags         invoices
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "Invoice ID"
// @Param        body  body  object  true  "Payment (amount_paid, utr_number, payment_date, etc.)"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      404  {object}  object
// @Router       /api/update_invoice_payment/{id} [put]
func UpdateInvoicePayment(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Check session ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var updatedBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&updatedBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- Get invoice ID from URL parameter ---
		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		// --- Bind JSON request ---
		var req struct {
			UTRNumber     string  `json:"utr_number" binding:"required"`
			PaymentStatus string  `json:"payment_status" binding:"required,oneof=fully_paid partial_paid"`
			AmountPaid    float64 `json:"amount_paid" binding:"required,gt=0"`
			PaymentDate   string  `json:"payment_date"`
			PaymentMode   string  `json:"payment_mode"`
			Remarks       string  `json:"remarks"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// --- Validate payment status ---
		if req.PaymentStatus != "fully_paid" && req.PaymentStatus != "partial_paid" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payment_status. Must be 'fully_paid' or 'partial_paid'"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- Check if invoice exists and get total amount ---
		var totalAmount float64
		var currentStatus string
		err = tx.QueryRow(`
			SELECT COALESCE(i.total_amount, 0), COALESCE(i.payment_status, 'unpaid')
			FROM invoice i
			WHERE i.id = $1
		`, invoiceID).Scan(&totalAmount, &currentStatus)

		if err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch invoice: " + err.Error()})
			}
			return
		}

		// --- Validate amount for fully paid ---
		if req.PaymentStatus == "fully_paid" && req.AmountPaid < totalAmount {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Amount paid (%.2f) is less than total amount (%.2f). Use 'partial_paid' status", req.AmountPaid, totalAmount),
			})
			tx.Rollback()
			return
		}

		// --- Insert payment record ---
		var paymentID int
		err = tx.QueryRow(`
			INSERT INTO invoice_payment 
			(invoice_id, utr_number, payment_status, amount_paid, payment_date, payment_mode, remarks, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`, invoiceID, req.UTRNumber, req.PaymentStatus, req.AmountPaid, req.PaymentDate, req.PaymentMode, req.Remarks, updatedBy).Scan(&paymentID)

		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create payment record: " + err.Error()})
			return
		}

		// --- Calculate total paid amount ---
		var totalPaid float64
		err = tx.QueryRow(`
			SELECT COALESCE(SUM(amount_paid), 0) 
			FROM invoice_payment 
			WHERE invoice_id = $1
		`, invoiceID).Scan(&totalPaid)

		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate total paid: " + err.Error()})
			return
		}

		// --- Determine final payment status ---
		finalStatus := "partial_paid"
		if totalPaid >= totalAmount {
			finalStatus = "fully_paid"
		}

		// --- Update invoice payment status ---
		_, err = tx.Exec(`
			UPDATE invoice 
			SET payment_status = $1, 
			    total_paid = $2,
			    updated_at = NOW(),
			    updated_by = $3
			WHERE id = $4
		`, finalStatus, totalPaid, updatedBy, invoiceID)

		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update invoice: " + err.Error()})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":        "Payment updated successfully",
			"payment_id":     paymentID,
			"invoice_id":     invoiceID,
			"payment_status": finalStatus,
			"total_paid":     totalPaid,
			"total_amount":   totalAmount,
			"balance":        totalAmount - totalPaid,
		})
	}
}

// GetInvoicePayments godoc
// @Summary      Get payments for an invoice
// @Tags         invoices
// @Param        id   path  int  true  "Invoice ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/get_invoice_payment/{id} [get]
func GetInvoicePayments(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Auth check ---
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var userID int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// --- Get invoice ID from URL parameter ---
		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		// --- Query all payments for this invoice ---
		rows, err := db.Query(`
			SELECT 
				ip.id,
				ip.invoice_id,
				ip.utr_number,
				ip.payment_status,
				ip.amount_paid,
				COALESCE(TO_CHAR(ip.payment_date, 'YYYY-MM-DD'), '') AS payment_date,
				COALESCE(ip.payment_mode, '') AS payment_mode,
				COALESCE(ip.remarks, '') AS remarks,
				ip.created_by,
				CONCAT(u.first_name, ' ', u.last_name) AS created_by_name,
				ip.created_at
			FROM invoice_payment ip
			LEFT JOIN users u ON u.id = ip.created_by
			WHERE ip.invoice_id = $1
			ORDER BY ip.created_at DESC
		`, invoiceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch payments", "details": err.Error()})
			return
		}
		defer rows.Close()

		// --- Build response list ---
		type Payment struct {
			ID            int     `json:"id"`
			InvoiceID     int     `json:"invoice_id"`
			UTRNumber     string  `json:"utr_number"`
			PaymentStatus string  `json:"payment_status"`
			AmountPaid    float64 `json:"amount_paid"`
			PaymentDate   string  `json:"payment_date"`
			PaymentMode   string  `json:"payment_mode"`
			Remarks       string  `json:"remarks"`
			CreatedBy     int     `json:"created_by"`
			CreatedByName string  `json:"created_by_name"`
			CreatedAt     string  `json:"created_at"`
		}

		var payments []Payment
		for rows.Next() {
			var p Payment
			if err := rows.Scan(
				&p.ID, &p.InvoiceID, &p.UTRNumber, &p.PaymentStatus,
				&p.AmountPaid, &p.PaymentDate, &p.PaymentMode,
				&p.Remarks, &p.CreatedBy, &p.CreatedByName, &p.CreatedAt,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse payment record", "details": err.Error()})
				return
			}
			payments = append(payments, p)
		}

		if len(payments) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No payments found for this invoice", "payments": []Payment{}})
			return
		}

		// --- Calculate total paid & remaining ---
		var totalAmount, totalPaid float64
		err = db.QueryRow(`SELECT COALESCE(total_amount,0), COALESCE(total_paid,0) FROM invoice WHERE id=$1`, invoiceID).Scan(&totalAmount, &totalPaid)
		if err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch invoice summary", "details": err.Error()})
			return
		}

		// --- Send final response ---
		c.JSON(http.StatusOK, gin.H{
			"invoice_id":   invoiceID,
			"total_amount": totalAmount,
			"total_paid":   totalPaid,
			"balance":      totalAmount - totalPaid,
			"payments":     payments,
		})
	}
}

// SearchInvoices godoc
// @Summary      Search invoices (query params)
// @Tags         invoices
// @Param        page   query  int  false  "Page"
// @Param        limit  query  int  false  "Limit"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/invoices/search [get]
func SearchInvoices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		/* ---------------- PAGINATION ---------------- */
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 10
		}
		offset := (page - 1) * limit

		/* ---------------- SESSION ---------------- */
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var userID int
		if err := db.QueryRow(
			`SELECT user_id FROM session WHERE session_id=$1`,
			sessionID,
		).Scan(&userID); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleName string
		if err := db.QueryRow(`
			SELECT r.role_name
			FROM users u JOIN roles r ON u.role_id=r.role_id
			WHERE u.id=$1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		/* ---------------- ACCESS CONTROL ---------------- */
		var args []interface{}
		var conditions []string
		argIndex := 1

		conditions = append(conditions, "i.indraft = false")

		switch roleName {
		case "superadmin":
			conditions = append(conditions, "1=1")

		case "admin":
			conditions = append(conditions, fmt.Sprintf(`
				wo.endclient_id IN (
					SELECT ec.id
					FROM end_client ec
					JOIN client c ON ec.client_id=c.client_id
					WHERE c.user_id=$%d
				)
			`, argIndex))
			args = append(args, userID)
			argIndex++

		default:
			c.JSON(http.StatusForbidden, gin.H{"error": "No permission"})
			return
		}

		/* ---------------- QUERY PARAMS ---------------- */
		projectID := c.Query("project_id")
		workOrderID := c.Query("workorderid")
		contactPerson := c.Query("contact_person")
		contactEmail := c.Query("contact_email")
		contactNumber := c.Query("contact_number")
		paymentStatus := c.Query("payment_status")

		totalPaid := c.Query("total_paid")
		totalPaidFilter := c.Query("total_paid_filter_type") // eq|gt|lt

		totalValue := c.Query("total_value")
		totalValueFilter := c.Query("total_value_filter_type") // eq|gt|lt

		billingAddress := c.Query("billing_address")
		shippingAddress := c.Query("shipping_address")

		woDate := c.Query("wo_date")
		woValidate := c.Query("wo_validate")

		/* ---------------- FILTERS ---------------- */

		if projectID != "" {
			conditions = append(conditions, fmt.Sprintf("wo.project_id=$%d", argIndex))
			args = append(args, projectID)
			argIndex++
		}

		if workOrderID != "" {
			conditions = append(conditions, fmt.Sprintf("wo.id=$%d", argIndex))
			args = append(args, workOrderID)
			argIndex++
		}

		if contactPerson != "" {
			conditions = append(conditions, fmt.Sprintf("wo.contact_person ILIKE $%d", argIndex))
			args = append(args, "%"+contactPerson+"%")
			argIndex++
		}

		if contactEmail != "" {
			conditions = append(conditions, fmt.Sprintf("wo.contact_email ILIKE $%d", argIndex))
			args = append(args, "%"+contactEmail+"%")
			argIndex++
		}

		if contactNumber != "" {
			conditions = append(conditions, fmt.Sprintf("wo.contact_number ILIKE $%d", argIndex))
			args = append(args, "%"+contactNumber+"%")
			argIndex++
		}

		if paymentStatus != "" {
			conditions = append(conditions, fmt.Sprintf("i.payment_status=$%d", argIndex))
			args = append(args, paymentStatus)
			argIndex++
		}

		if billingAddress != "" {
			conditions = append(conditions, fmt.Sprintf("i.billing_address ILIKE $%d", argIndex))
			args = append(args, "%"+billingAddress+"%")
			argIndex++
		}

		if shippingAddress != "" {
			conditions = append(conditions, fmt.Sprintf("i.shipping_address ILIKE $%d", argIndex))
			args = append(args, "%"+shippingAddress+"%")
			argIndex++
		}

		if woDate != "" {
			conditions = append(conditions, fmt.Sprintf("wo.wo_date=$%d", argIndex))
			args = append(args, woDate)
			argIndex++
		}

		if woValidate != "" {
			conditions = append(conditions, fmt.Sprintf("wo.wo_validate=$%d", argIndex))
			args = append(args, woValidate)
			argIndex++
		}

		/* -------- NUMERIC FILTERS (FIXED) -------- */

		if totalPaid != "" && totalPaidFilter != "" {
			var op string
			switch totalPaidFilter {
			case "gt":
				op = ">"
			case "lt":
				op = "<"
			default: // "eq"
				op = "="
			}

			conditions = append(conditions,
				fmt.Sprintf("i.total_paid %s $%d", op, argIndex))
			args = append(args, totalPaid)
			argIndex++
		}

		if totalValue != "" && totalValueFilter != "" {
			var op string
			switch totalValueFilter {
			case "gt":
				op = ">"
			case "lt":
				op = "<"
			default: // "eq"
				op = "="
			}

			conditions = append(conditions,
				fmt.Sprintf("wo.total_value %s $%d", op, argIndex))
			args = append(args, totalValue)
			argIndex++
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT ---------------- */
		var total int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM invoice i
			JOIN work_order wo ON i.work_order_id=wo.id
			JOIN end_client ec ON wo.endclient_id=ec.id
			WHERE %s
		`, whereClause)

		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY ---------------- */
		query := fmt.Sprintf(`
			SELECT 
				i.id, i.work_order_id, i.created_by, i.revision_no,
				i.billing_address, i.shipping_address, i.created_at,
				i.name, i.payment_status, i.total_paid, i.total_amount,
				i.updated_by, i.updated_at,
				CONCAT(u.first_name,' ',u.last_name),
				wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value,
				wo.contact_person, wo.payment_term::text, wo.wo_description,
				wo.endclient_id, ec.contact_person,
				wo.project_id, p.name,
				wo.contact_email, wo.contact_number, wo.phone_code,
				pc.phone_code, wo.revision
			FROM invoice i
			JOIN users u ON i.created_by=u.id
			JOIN work_order wo ON i.work_order_id=wo.id
			JOIN end_client ec ON wo.endclient_id=ec.id
			JOIN project p ON wo.project_id=p.project_id
			JOIN phone_code pc ON wo.phone_code=pc.id
			WHERE %s
			ORDER BY i.created_at DESC
			LIMIT $%d OFFSET $%d
		`, whereClause, argIndex, argIndex+1)

		args = append(args, limit, offset)

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var invoices []models.GetInvoice

		for rows.Next() {
			var inv models.GetInvoice
			var ptRaw sql.NullString
			var tp, ta sql.NullFloat64
			var ub sql.NullInt64
			var ua sql.NullTime

			rows.Scan(
				&inv.ID, &inv.WorkOrderID, &inv.CreatedBy, &inv.RevisionNo,
				&inv.BillingAddress, &inv.ShippingAddress, &inv.CreatedAt,
				&inv.Name, &inv.PaymentStatus, &tp, &ta,
				&ub, &ua, &inv.CreatedByName,
				&inv.WONumber, &inv.WODate, &inv.WOValidate, &inv.TotalValue,
				&inv.ContactPerson, &ptRaw, &inv.WODescription,
				&inv.EndClientID, &inv.EndClient,
				&inv.ProjectID, &inv.ProjectName,
				&inv.ContactEmail, &inv.ContactNumber,
				&inv.PhoneCode, &inv.PhoneCodeName, &inv.WORevision,
			)

			if ptRaw.Valid {
				_ = json.Unmarshal([]byte(ptRaw.String), &inv.PaymentTerm)
			}
			if tp.Valid {
				inv.TotalPaid = tp.Float64
			}
			if ta.Valid {
				inv.TotalAmount = ta.Float64
			}
			if ub.Valid {
				inv.UpdatedBy = int(ub.Int64)
			}
			if ua.Valid {
				inv.UpdatedAt = ua.Time
			}

			// ✅ Append revision number to wo_number if not zero
			if inv.WORevision != 0 {
				inv.WONumber = fmt.Sprintf("%s - %d", inv.WONumber, inv.RevisionNo)
			}

			// --- Fetch items for this invoice ---
			itemRows, err := db.Query(`
				SELECT i.id, i.invoice_id, i.item_id, i.volume, wom.hsn_code, wom.tower_id, wom.floor_id, wom.item_name, wom.unit_rate, wom.tax
				FROM invoice_item i
				LEFT JOIN work_order_material wom ON i.item_id = wom.id
				WHERE i.invoice_id = $1
			`, inv.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var items []models.GetInvoiceItem
			for itemRows.Next() {
				var item models.GetInvoiceItem

				var hsnCode sql.NullInt64
				var floorIDs pq.Int64Array
				var name sql.NullString
				var unitRate sql.NullFloat64
				var tax sql.NullFloat64

				if err := itemRows.Scan(
					&item.ID,
					&item.InvoiceID,
					&item.ItemID,
					&item.Volume,
					&hsnCode,
					&item.TowerID,
					&floorIDs,
					&name,
					&unitRate,
					&tax,
				); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}

				if hsnCode.Valid {
					item.HSNCode = int(hsnCode.Int64)
				} else {
					item.HSNCode = 0
				}

				if name.Valid {
					item.ItemName = name.String
				} else {
					item.ItemName = ""
				}

				if unitRate.Valid {
					item.UnitRate = unitRate.Float64
				} else {
					item.UnitRate = 0.0
				}

				if tax.Valid {
					item.Tax = tax.Float64
				} else {
					item.Tax = 0.0
				}

				items = append(items, item)
			}
			itemRows.Close()

			if items == nil {
				items = []models.GetInvoiceItem{}
			}

			inv.Items = items
			invoices = append(invoices, inv)
		}

		c.JSON(http.StatusOK, gin.H{
			"data": invoices,
			"pagination": gin.H{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": (total + limit - 1) / limit,
			},
		})

		go SaveActivityLog(db, models.ActivityLog{
			EventContext: "Invoice",
			EventName:    "Search",
			Description:  "Advanced invoice search",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

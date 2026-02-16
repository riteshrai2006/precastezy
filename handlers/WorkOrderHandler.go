package handlers

import (
	"backend/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// CreateWorkOrder godoc
// @Summary      Create work order
// @Tags         work-orders
// @Accept       json
// @Produce      json
// @Param        body  body      models.WorkOrder  true  "Work order"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/workorders [post]
func CreateWorkOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		// Get created_by from session
		var createdBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&createdBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var wo models.WorkOrder
		if err := c.ShouldBindJSON(&wo); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var revisionNo int
		err = tx.QueryRow(
			`SELECT COALESCE(MAX(revision), -1) + 1 FROM work_order WHERE wo_number=$1`,
			wo.WONumber,
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

		recurrenceJSON, _ := json.Marshal(wo.RecurrencePatterns)

		// marshal payment_term map to JSON for DB storage
		paymentTermJSON, _ := json.Marshal(wo.PaymentTerm)

		var workOrderID int
		err = tx.QueryRow(`
			INSERT INTO work_order 
			(wo_number, wo_date, wo_validate, total_value, contact_person, payment_term, wo_description, created_by, endclient_id, project_id, contact_email, contact_number, phone_code, shipped_address, billed_address, revision, recurrence_patterns) 
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
			wo.WONumber, wo.WODate, wo.WOValidate, wo.TotalValue, wo.ContactPerson,
			string(paymentTermJSON), wo.WODescription, createdBy, wo.EndClientID, wo.ProjectID, wo.ContactEmail, wo.ContactNumber, wo.PhoneCode, wo.ShippedAddress, wo.BilledAddress, revisionNo, recurrenceJSON).Scan(&workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"details": "2", "error": err.Error()})
			return
		}

		// insert materials
		for _, m := range wo.Material {
			_, err := tx.Exec(`INSERT INTO work_order_material 
				(work_order_id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id) 
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				workOrderID, m.ItemName, m.UnitRate, m.Volume, m.Tax, m.HsnCode, m.TowerID, pq.Array(m.FloorID))
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"details": "3", "error": err.Error()})
				return
			}
		}

		// insert attachments
		for _, file := range wo.Attachments {
			_, err := tx.Exec(`INSERT INTO work_order_attachment (work_order_id, file_url) VALUES ($1,$2)`, workOrderID, file)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"details": "4", "error": err.Error()})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Work Order created successfully", "id": workOrderID})
	}
}

// GetWorkOrder godoc
// @Summary      Get work order by ID
// @Tags         work-orders
// @Param        id   path      int  true  "Work order ID"
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/workorders/{id} [get]
func GetWorkOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Fetch current work order
		var wo models.WorkOrder
		err := db.QueryRow(`
			SELECT 
				wo.id, wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value, 
				wo.contact_person, wo.payment_term, wo.wo_description,
				wo.created_at, wo.updated_at, wo.created_by, CONCAT(u.first_name, ' ', u.last_name) AS created_by_name,
				wo.endclient_id, ec.contact_person AS endclient_name,
				wo.project_id, p.name AS project_name, wo.contact_email, wo.contact_number, wo.phone_code, pc.phone_code AS phone_code_name, wo.shipped_address, wo.billed_address, wo.recurrence_patterns
			FROM work_order wo
			LEFT JOIN end_client ec ON wo.endclient_id = ec.id
			LEFT JOIN project p ON wo.project_id = p.project_id
			LEFT JOIN users u ON wo.created_by = u.id::text
			LEFT JOIN phone_code pc ON wo.phone_code = pc.id
			WHERE wo.id = $1
		`, id).Scan(
			&wo.ID, &wo.WONumber, &wo.WODate, &wo.WOValidate, &wo.TotalValue,
			&wo.ContactPerson, new(sql.NullString), &wo.WODescription,
			&wo.CreatedAt, &wo.UpdatedAt, &wo.CreatedBy, &wo.CreatedByName,
			&wo.EndClientID, &wo.EndClient,
			&wo.ProjectID, &wo.ProjectName, &wo.ContactEmail, &wo.ContactNumber, &wo.PhoneCode, &wo.PhoneCodeName, &wo.ShippedAddress, &wo.BilledAddress, new(sql.NullString),
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Work Order not found"})
			return
		}

		// fetch payment_term separately to unmarshal JSON
		var paymentTermRaw sql.NullString
		if err := db.QueryRow(`SELECT payment_term FROM work_order WHERE id=$1`, id).Scan(&paymentTermRaw); err == nil && paymentTermRaw.Valid {
			var pt map[string]float64
			if json.Unmarshal([]byte(paymentTermRaw.String), &pt) == nil {
				wo.PaymentTerm = pt
			}
		}

		// fetch recurrence_patterns separately to unmarshal JSON
		var recurrencePatternsRaw sql.NullString
		if err := db.QueryRow(`SELECT recurrence_patterns FROM work_order WHERE id=$1`, id).Scan(&recurrencePatternsRaw); err == nil && recurrencePatternsRaw.Valid {
			var rp []models.RecurrencePattern
			if json.Unmarshal([]byte(recurrencePatternsRaw.String), &rp) == nil {
				wo.RecurrencePatterns = rp
			}
		}

		// ✅ Append revision number if not zero
		if wo.RevisionNo != 0 {
			wo.WONumber = fmt.Sprintf("%s - %d", wo.WONumber, wo.RevisionNo)
		}

		matRows, err := db.Query(`
		SELECT 
			wom.id, wom.item_name, wom.unit_rate, wom.volume, wom.tax, wom.volume_used, wom.hsn_code,
			wom.tower_id,
			pct.name AS tower_name,
			wom.floor_id,
			COALESCE(ARRAY_AGG(pf.name) FILTER (WHERE pf.id IS NOT NULL), '{}'::text[]) AS floor_names
		FROM work_order_material wom
		LEFT JOIN precast pct ON pct.id = wom.tower_id
		LEFT JOIN precast pf ON pf.id = ANY(wom.floor_id)
		WHERE wom.work_order_id=$1
		GROUP BY wom.id, wom.item_name, wom.unit_rate, wom.volume, wom.tax, wom.volume_used, wom.hsn_code, wom.tower_id, pct.name, wom.floor_id
		`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		for matRows.Next() {
			var m models.WorkOrderMaterial
			var towerID sql.NullInt64
			var towerName sql.NullString
			var floorIDs pq.Int64Array
			var floorNames pq.StringArray

			if err := matRows.Scan(&m.ID, &m.ItemName, &m.UnitRate, &m.Volume, &m.Tax, &m.VolumeUsed, &m.HsnCode, &towerID, &towerName, &floorIDs, &floorNames); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			m.Balance = m.Volume - m.VolumeUsed

			// tower_id & tower_name
			if towerID.Valid {
				tid := int(towerID.Int64)
				m.TowerID = &tid
				if towerName.Valid && towerName.String != "" {
					m.TowerName = towerName.String
				} else {
					// Fallback: fetch tower name directly if join returned empty
					var tname string
					_ = db.QueryRow(`SELECT name FROM precast WHERE id=$1`, tid).Scan(&tname)
					m.TowerName = tname
				}
			} else {
				tid := 0
				m.TowerID = &tid
				m.TowerName = ""
			}

			// floor_id & floor_name
			if len(floorIDs) > 0 {
				m.FloorID = []int{}
				for _, fid := range floorIDs {
					m.FloorID = append(m.FloorID, int(fid))
				}
				// map floorNames if present, else fallback per id
				if len(floorNames) > 0 {
					m.FloorName = []string(floorNames)
				} else {
					m.FloorName = []string{}
					for _, fid := range m.FloorID {
						var fname string
						_ = db.QueryRow(`SELECT name FROM precast WHERE id=$1`, fid).Scan(&fname)
						m.FloorName = append(m.FloorName, fname)
					}
				}
			} else {
				m.FloorID = []int{0}
				m.FloorName = []string{""}
			}

			wo.Material = append(wo.Material, m)
		}
		matRows.Close()

		// fetch attachments
		attRows, err := db.Query(`SELECT file_url FROM work_order_attachment WHERE work_order_id=$1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for attRows.Next() {
			var file string
			if err := attRows.Scan(&file); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			wo.Attachments = append(wo.Attachments, file)
		}
		attRows.Close()

		// --- after fetching attachments ---
		attRows.Close()

		c.JSON(http.StatusOK, wo)
	}
}

// GetAllWorkOrders godoc
// @Summary      List work orders
// @Tags         work-orders
// @Success      200  {array}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/workorders [get]
func GetAllWorkOrders(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get pagination parameters
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 10
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
				"message": "No permission to view work orders",
			})
			return
		}

		// Query work orders with access control
		query := fmt.Sprintf(`
			SELECT 
				wo.id, wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value, 
				wo.contact_person, wo.payment_term, wo.wo_description, 
				wo.created_at, wo.updated_at, wo.created_by, CONCAT(u.first_name, ' ', u.last_name) AS created_by_name,
				wo.endclient_id, ec.contact_person AS endclient_name,
				wo.project_id, p.name AS project_name, wo.contact_email, wo.contact_number, wo.phone_code, pc.phone_code AS phone_code_name, wo.shipped_address, wo.billed_address
			FROM work_order wo
			LEFT JOIN end_client ec ON wo.endclient_id = ec.id
			LEFT JOIN phone_code pc ON wo.phone_code = pc.id
			LEFT JOIN users u ON wo.created_by = u.id::text
			LEFT JOIN project p ON wo.project_id = p.project_id
			WHERE %s
			ORDER BY wo.created_at DESC
			LIMIT $%d OFFSET $%d
		`, accessCondition, argIndex, argIndex+1)

		// Add pagination args
		args = append(args, limit, offset)

		// Get total count for pagination
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*) 
			FROM work_order wo
			LEFT JOIN end_client ec ON wo.endclient_id = ec.id
			LEFT JOIN client c ON ec.client_id = c.client_id
			WHERE %s
		`, accessCondition)

		var totalCount int
		countArgs := args[:len(args)-2] // Remove limit and offset for count
		err = db.QueryRow(countQuery, countArgs...).Scan(&totalCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "count", "error": err.Error()})
			return
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "1", "error": err.Error()})
			return
		}
		defer rows.Close()

		var workOrders []models.WorkOrder
		for rows.Next() {
			var wo models.WorkOrder
			var paymentTermRaw sql.NullString
			rows.Scan(&wo.ID, &wo.WONumber, &wo.WODate, &wo.WOValidate, &wo.TotalValue, &wo.ContactPerson,
				&paymentTermRaw, &wo.WODescription, &wo.CreatedAt, &wo.UpdatedAt, &wo.CreatedBy, &wo.CreatedByName, &wo.EndClientID, &wo.EndClient,
				&wo.ProjectID, &wo.ProjectName, &wo.ContactEmail, &wo.ContactNumber, &wo.PhoneCode, &wo.PhoneCodeName, &wo.ShippedAddress, &wo.BilledAddress)

			if paymentTermRaw.Valid {
				var pt map[string]float64
				if json.Unmarshal([]byte(paymentTermRaw.String), &pt) == nil {
					wo.PaymentTerm = pt
				}
			}

			// ✅ Append revision number if not zero
			if wo.RevisionNo != 0 {
				wo.WONumber = fmt.Sprintf("%s - %d", wo.WONumber, wo.RevisionNo)
			}

			// fetch materials
			matRows, err := db.Query(`SELECT id, item_name, unit_rate, volume, tax, volume_used, hsn_code, tower_id, floor_id FROM work_order_material WHERE work_order_id=$1`, wo.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"details": "2", "error": err.Error()})
				return
			}

			for matRows.Next() {
				var m models.WorkOrderMaterial
				var towerID sql.NullInt64
				var floorID sql.NullString

				// Initialize defaults
				m.TowerID = new(int)
				*m.TowerID = 0
				m.TowerName = ""
				m.FloorID = []int{0} // default to 0 if NULL
				m.FloorName = []string{""}

				if err := matRows.Scan(&m.ID, &m.ItemName, &m.UnitRate, &m.Volume, &m.Tax, &m.VolumeUsed, &m.HsnCode, &towerID, &floorID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"details": "3", "error": err.Error()})
					return
				}

				m.Balance = m.Volume - m.VolumeUsed

				// Fetch tower name
				if towerID.Valid {
					var towerName string
					if err := db.QueryRow(`SELECT name FROM precast WHERE id=$1`, towerID.Int64).Scan(&towerName); err == nil {
						m.TowerName = towerName
					}
				}

				// Fetch floor names
				if floorID.Valid {
					m.FloorID = []int{}
					m.FloorName = []string{}
					for _, f := range strings.Split(floorID.String, ",") {
						f = strings.TrimSpace(f)
						if fid, err := strconv.Atoi(f); err == nil {
							var floorName string
							if err := db.QueryRow(`SELECT name FROM precast WHERE id=$1`, fid).Scan(&floorName); err == nil {
								m.FloorID = append(m.FloorID, fid)
								m.FloorName = append(m.FloorName, floorName)
							}
						}
					}
				}

				wo.Material = append(wo.Material, m)
			}
			matRows.Close()

			// fetch attachments
			attRows, _ := db.Query(`SELECT file_url FROM work_order_attachment WHERE work_order_id=$1`, wo.ID)
			for attRows.Next() {
				var file string
				attRows.Scan(&file)
				wo.Attachments = append(wo.Attachments, file)
			}
			attRows.Close()

			workOrders = append(workOrders, wo)
		}

		totalPages := (totalCount + limit - 1) / limit
		c.JSON(http.StatusOK, gin.H{
			"data": workOrders,
			"pagination": gin.H{
				"current_page": page,
				"per_page":     limit,
				"total":        totalCount,
				"total_pages":  totalPages,
			},
		})

		log := models.ActivityLog{
			EventContext: "Work Order",
			EventName:    "Get",
			Description:  "Fetched all work orders",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}
		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// DeleteWorkOrder godoc
// @Summary      Delete work order
// @Tags         work-orders
// @Param        id   path      int  true  "Work order ID"
// @Success      200  {object}  object
// @Router       /api/workorders/{id} [delete]
func DeleteWorkOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Delete attachments
		_, err = tx.Exec(`DELETE FROM work_order_attachment WHERE work_order_id=$1`, id)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Delete materials
		_, err = tx.Exec(`DELETE FROM work_order_material WHERE work_order_id=$1`, id)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Delete work order
		_, err = tx.Exec(`DELETE FROM work_order WHERE id=$1`, id)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Commit transaction
		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Work Order and related records deleted successfully"})
	}
}

// UpdateWorkOrder godoc
// @Summary      Update work order
// @Tags         work-orders
// @Param        id     path      int  true  "Work order ID"
// @Param        body   body      object  true  "Work order fields"
// @Success      200    {object}  object
// @Router       /api/workorders/{id} [put]
func UpdateWorkOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		var updatedBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&updatedBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		workOrderID := c.Param("id")
		if workOrderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing work_order_id"})
			return
		}

		var wo models.WorkOrder
		if err := c.ShouldBindJSON(&wo); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "details": "0"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "details": "1"})
			return
		}

		// --- Get latest revision number ---
		var lastRevision int
		_ = tx.QueryRow(`SELECT COALESCE(MAX(revision_no), 0) FROM work_order_revision WHERE work_order_id = $1`, workOrderID).Scan(&lastRevision)
		newRevision := lastRevision + 1

		// --- Fetch current work order for backup ---
		var currentWO models.WorkOrder
		err = tx.QueryRow(`
			SELECT wo_number, wo_date, wo_validate, total_value, contact_person, payment_term, wo_description,
				   endclient_id, project_id, contact_email, contact_number, phone_code, shipped_address, billed_address, created_by
			FROM work_order WHERE id = $1
		`, workOrderID).Scan(
			&currentWO.WONumber, &currentWO.WODate, &currentWO.WOValidate, &currentWO.TotalValue,
			&currentWO.ContactPerson, new(sql.NullString), &currentWO.WODescription,
			&currentWO.EndClientID, &currentWO.ProjectID, &currentWO.ContactEmail, &currentWO.ContactNumber,
			&currentWO.PhoneCode, &currentWO.ShippedAddress, &currentWO.BilledAddress, &currentWO.CreatedBy,
		)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch existing work order"})
			return
		}

		// --- Insert into work_order_revision ---
		var revisionID int
		err = tx.QueryRow(`
			INSERT INTO work_order_revision 
				(work_order_id, revision_no, wo_number, wo_date, wo_validate, total_value, contact_person, 
				payment_term, wo_description, endclient_id, project_id, contact_email, contact_number, 
				phone_code, shipped_address, billed_address, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			RETURNING id
		`, workOrderID, newRevision, currentWO.WONumber, currentWO.WODate, currentWO.WOValidate, currentWO.TotalValue,
			currentWO.ContactPerson, (func() string {
				var s string
				_ = db.QueryRow(`SELECT payment_term FROM work_order WHERE id=$1`, workOrderID).Scan(&s)
				return s
			})(), currentWO.WODescription, currentWO.EndClientID, currentWO.ProjectID,
			currentWO.ContactEmail, currentWO.ContactNumber, currentWO.PhoneCode, currentWO.ShippedAddress, currentWO.BilledAddress, updatedBy).Scan(&revisionID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save work order revision", "detail": err.Error()})
			return
		}

		// --- Backup materials ---
		rows, err := tx.Query(`SELECT id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id FROM work_order_material WHERE work_order_id = $1`, workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch materials for revision", "details": err.Error()})
			return
		}

		// Collect all materials first
		var materials []struct {
			Material models.WorkOrderMaterial
			FloorIDs pq.Int32Array
		}

		for rows.Next() {
			var m models.WorkOrderMaterial
			var floorIDs pq.Int32Array

			if err := rows.Scan(&m.ID, &m.ItemName, &m.UnitRate, &m.Volume, &m.Tax, &m.HsnCode, &m.TowerID, &floorIDs); err != nil {
				rows.Close()
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan material", "details": err.Error()})
				return
			}

			m.FloorID = make([]int, len(floorIDs))
			for i, v := range floorIDs {
				m.FloorID[i] = int(v)
			}

			materials = append(materials, struct {
				Material models.WorkOrderMaterial
				FloorIDs pq.Int32Array
			}{m, floorIDs})
		}
		rows.Close() // Close rows before executing inserts

		if err := rows.Err(); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating materials", "details": err.Error()})
			return
		}

		// Now insert all materials
		for _, mat := range materials {
			_, err := tx.Exec(`
        INSERT INTO work_order_material_revision
        (work_order_revision_id, work_order_id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, revisionID, workOrderID, mat.Material.ItemName, mat.Material.UnitRate, mat.Material.Volume,
				mat.Material.Tax, mat.Material.HsnCode, mat.Material.TowerID, mat.FloorIDs)

			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save material revision", "details": err.Error()})
				return
			}
		}

		// --- Backup attachments ---
		attRows, err := tx.Query(`SELECT file_url FROM work_order_attachment WHERE work_order_id = $1`, workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch attachments", "details": err.Error()})
			return
		}

		// Collect all file URLs first
		var fileURLs []string
		for attRows.Next() {
			var file string
			if err := attRows.Scan(&file); err != nil {
				attRows.Close()
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan attachment", "details": err.Error()})
				return
			}
			fileURLs = append(fileURLs, file)
		}
		attRows.Close() // Close the rows before executing inserts

		// Check for iteration errors
		if err := attRows.Err(); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating attachments", "details": err.Error()})
			return
		}

		// Now insert all attachments
		for _, file := range fileURLs {
			_, err := tx.Exec(`INSERT INTO work_order_attachment_revision (work_order_revision_id, work_order_id, file_url) VALUES ($1,$2,$3)`, revisionID, workOrderID, file)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save attachment revision", "details": err.Error()})
				return
			}
		}

		// --- Continue with your existing UPDATE logic here ---
		// (You can paste your update logic from your message here unchanged)
		// ===============================
		// 🟢 NOW UPDATE MAIN TABLES
		// ===============================

		// --- Update main work_order ---
		// marshal new payment_term to JSON for update
		newPaymentTermJSON, _ := json.Marshal(wo.PaymentTerm)

		_, err = tx.Exec(`
			UPDATE work_order 
			SET wo_number=$1, wo_date=$2, wo_validate=$3, total_value=$4, contact_person=$5, payment_term=$6::jsonb, 
				wo_description=$7, endclient_id=$8, project_id=$9, contact_email=$10, contact_number=$11, 
				phone_code=$12, shipped_address=$13, billed_address=$14, updated_at=NOW(), created_by=$15
			WHERE id=$16
		`, wo.WONumber, wo.WODate, wo.WOValidate, wo.TotalValue, wo.ContactPerson, string(newPaymentTermJSON), wo.WODescription,
			wo.EndClientID, wo.ProjectID, wo.ContactEmail, wo.ContactNumber, wo.PhoneCode, wo.ShippedAddress,
			wo.BilledAddress, updatedBy, workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update work order", "details": err.Error()})
			return
		}

		// --- Delete old materials before inserting updated ones ---
		_, err = tx.Exec(`DELETE FROM work_order_material WHERE work_order_id=$1`, workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old materials", "details": err.Error()})
			return
		}

		// --- Insert updated materials ---
		for _, mat := range wo.Material {
			_, err := tx.Exec(`
				INSERT INTO work_order_material 
				(work_order_id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			`, workOrderID, mat.ItemName, mat.UnitRate, mat.Volume, mat.Tax, mat.HsnCode, mat.TowerID, pq.Array(mat.FloorID))
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update materials", "details": err.Error()})
				return
			}
		}

		// --- Delete old attachments before inserting updated ones ---
		_, err = tx.Exec(`DELETE FROM work_order_attachment WHERE work_order_id=$1`, workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old attachments", "details": err.Error()})
			return
		}

		// --- Insert updated attachments ---
		for _, att := range wo.Attachments {
			_, err := tx.Exec(`INSERT INTO work_order_attachment (work_order_id, file_url) VALUES ($1, $2)`, workOrderID, att)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update attachments", "details": err.Error()})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Work Order updated successfully",
			"work_order_id": workOrderID,
			"revision_no":   newRevision,
			"updated_by":    updatedBy,
		})
	}
}

// GetWorkOrderRevisions godoc
// @Summary      Get work order revisions
// @Tags         work-orders
// @Param        id   path      int  true  "Work order ID"
// @Success      200  {object}  object
// @Router       /api/wo_revisions/{id} [get]
func GetWorkOrderRevisions(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		workOrderID := c.Param("id")

		// --- Fetch all revisions ---
		rows, err := db.Query(`
			SELECT 
				wor.id, wor.work_order_id, wor.revision_no, wor.wo_number, wor.wo_date, wor.wo_validate, wor.total_value,
				wor.contact_person, wor.payment_term::text, wor.wo_description,
				wor.created_by, wor.updated_at, 
				CONCAT(u.first_name, ' ', u.last_name) AS created_by_name,
				wor.endclient_id, ec.contact_person AS endclient_name,
				wor.project_id, p.name AS project_name, wor.contact_email, wor.contact_number, 
				wor.phone_code, pc.phone_code AS phone_code_name, wor.shipped_address, wor.billed_address
			FROM work_order_revision wor
			LEFT JOIN end_client ec ON wor.endclient_id = ec.id
			LEFT JOIN project p ON wor.project_id = p.project_id
			LEFT JOIN users u ON wor.created_by = u.id
			LEFT JOIN phone_code pc ON wor.phone_code = pc.id
			WHERE wor.work_order_id = $1
			ORDER BY wor.revision_no DESC
		`, workOrderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var revisions []models.WorkOrderRevision

		for rows.Next() {
			var r models.WorkOrderRevision
			var paymentTermRaw sql.NullString
			err := rows.Scan(
				&r.ID, &r.WorkOrderID, &r.RevisionNo, &r.WONumber, &r.WODate, &r.WOValidate, &r.TotalValue,
				&r.ContactPerson, &paymentTermRaw, &r.WODescription,
				&r.CreatedBy, &r.UpdatedAt, &r.CreatedByName,
				&r.EndClientID, &r.EndClient,
				&r.ProjectID, &r.ProjectName,
				&r.ContactEmail, &r.ContactNumber,
				&r.PhoneCode, &r.PhoneCodeName,
				&r.ShippedAddress, &r.BilledAddress,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"details": "row-scan", "error": err.Error()})
				return
			}

			// Unmarshal payment_term JSONB
			if paymentTermRaw.Valid && paymentTermRaw.String != "" {
				var pt map[string]float64
				if err := json.Unmarshal([]byte(paymentTermRaw.String), &pt); err == nil {
					r.PaymentTerm = pt
				} else {
					r.PaymentTerm = make(map[string]float64)
				}
			} else {
				r.PaymentTerm = make(map[string]float64)
			}

			// --- Fetch materials for this revision ---
			matRows, err := db.Query(`
				SELECT id, work_order_revision_id, work_order_id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id 
				FROM work_order_material_revision 
				WHERE work_order_revision_id = $1
			`, r.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"details": "materials", "error": err.Error()})
				return
			}

			for matRows.Next() {
				var m models.WorkOrderMaterialRevision
				var towerID sql.NullInt64
				var floorIDs pq.Int64Array
				if err := matRows.Scan(&m.ID, &m.WorkOrderRevisionID, &m.WorkOrderID, &m.ItemName, &m.UnitRate,
					&m.Volume, &m.Tax, &m.HsnCode, &towerID, &floorIDs); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"details": "mat-scan", "error": err.Error()})
					return
				}
				// tower_id
				if towerID.Valid {
					tid := int(towerID.Int64)
					m.TowerID = &tid
					var towerName string
					db.QueryRow(`SELECT name FROM precast WHERE id=$1`, tid).Scan(&towerName)
					m.TowerName = towerName
				} else {
					tid := 0
					m.TowerID = &tid
					m.TowerName = ""
				}

				// floor_id & floor_name
				if len(floorIDs) > 0 {
					m.FloorID = []int{}
					m.FloorName = []string{}
					for _, fid := range floorIDs {
						id := int(fid)
						m.FloorID = append(m.FloorID, id)
						var floorName string
						db.QueryRow(`SELECT name FROM precast WHERE id=$1`, id).Scan(&floorName)
						m.FloorName = append(m.FloorName, floorName)
					}
				} else {
					m.FloorID = []int{0}
					m.FloorName = []string{""}
				}
				r.Material = append(r.Material, m)
			}
			matRows.Close()

			// --- Fetch attachments for this revision ---
			attRows, err := db.Query(`
				SELECT id, work_order_revision_id, work_order_id, file_url
				FROM work_order_attachment_revision
				WHERE work_order_revision_id = $1
			`, r.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"details": "attachments", "error": err.Error()})
				return
			}

			for attRows.Next() {
				var a models.WorkOrderAttachmentRevision
				if err := attRows.Scan(&a.ID, &a.WorkOrderRevisionID, &a.WorkOrderID, &a.FileURL); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"details": "att-scan", "error": err.Error()})
					return
				}
				r.Attachments = append(r.Attachments, a)
			}
			attRows.Close()

			revisions = append(revisions, r)
		}

		c.JSON(http.StatusOK, revisions)
	}
}

// CreateWorkOrderAmendment godoc
// @Summary      Create work order amendment
// @Tags         work-orders
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Amendment data"
// @Success      200   {object}  object
// @Router       /api/workorders_amendment [post]
func CreateWorkOrderAmendment(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing session_id"})
			return
		}

		// Get created_by from session
		var createdBy int
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&createdBy); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var wo models.WorkOrder
		if err := c.ShouldBindJSON(&wo); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var revisionNo int
		err = tx.QueryRow(
			`SELECT COALESCE(MAX(revision), 0) + 1 FROM invoice WHERE wo_number=$1`,
			wo.WONumber,
		).Scan(&revisionNo)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var workOrderID int
		paymentTermJSON2, _ := json.Marshal(wo.PaymentTerm)
		err = tx.QueryRow(`
			INSERT INTO work_order 
			(wo_number, wo_date, wo_validate, total_value, contact_person, payment_term, wo_description, created_by, endclient_id, project_id, contact_email, contact_number, phone_code, shipped_address, billed_address, revision) 
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			wo.WONumber, wo.WODate, wo.WOValidate, wo.TotalValue, wo.ContactPerson,
			string(paymentTermJSON2), wo.WODescription, createdBy, wo.EndClientID, wo.ProjectID, wo.ContactEmail, wo.ContactNumber, wo.PhoneCode, wo.ShippedAddress, wo.BilledAddress, revisionNo).Scan(&workOrderID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// insert materials
		for _, m := range wo.Material {
			_, err := tx.Exec(`INSERT INTO work_order_material 
				(work_order_id, item_name, unit_rate, volume, tax, hsn_code, tower_id, floor_id) 
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				workOrderID, m.ItemName, m.UnitRate, m.Volume, m.Tax, m.HsnCode, m.TowerID, pq.Array(m.FloorID))
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		// insert attachments
		for _, file := range wo.Attachments {
			_, err := tx.Exec(`INSERT INTO work_order_attachment (work_order_id, file_url) VALUES ($1,$2)`, workOrderID, file)
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

		c.JSON(http.StatusCreated, gin.H{"message": "Work Order created successfully", "id": workOrderID})
	}
}

// GetPendingInvoices godoc
// @Summary      Get pending invoices
// @Tags         work-orders
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/pending-invoices [get]
func GetPendingInvoices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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
			accessCondition = "1=1"

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
				"message": "No permission to view pending invoices",
			})
			return
		}

		/* ---------------- PAGINATION ---------------- */
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 10
		}
		offset := (page - 1) * limit

		/* ---------------- COUNT ---------------- */
		var total int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM invoice i
			JOIN work_order wo ON i.work_order_id=wo.id
			WHERE i.indraft=true AND %s
		`, accessCondition)

		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Query pending invoices with access control
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
			WHERE i.indraft = true AND %s
			ORDER BY i.created_at DESC
			LIMIT $%d OFFSET $%d
		`, accessCondition, argIndex, argIndex+1)

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
				&inv.Name,
				&inv.PaymentStatus,
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

		c.JSON(http.StatusOK, gin.H{
			"data": invoices,
			"pagination": gin.H{
				"page":          page,
				"limit":         limit,
				"total_records": total,
				"total_pages":   int(math.Ceil(float64(total) / float64(limit))),
			},
		})

		log := models.ActivityLog{
			EventContext: "Pending Invoice",
			EventName:    "Get",
			Description:  "Fetched all pending invoices",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}
		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Pending invoices fetched but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// SearchWorkOrders godoc
// @Summary      Search work orders
// @Tags         work-orders
// @Param        q  query  string  false  "Search query"
// @Success      200  {object}  object
// @Router       /api/work-orders/search [get]
func SearchWorkOrders(db *sql.DB) gin.HandlerFunc {
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

		// Get user_id from session
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Get role_id from users table
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
			return
		}

		// Get role name from roles table
		var roleName string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
			return
		}

		/* ---------------- ACCESS CONTROL ---------------- */
		var conditions []string
		var args []interface{}
		argIndex := 1

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
			c.JSON(http.StatusForbidden, gin.H{"message": "No permission"})
			return
		}

		/* ---------------- FILTER PARAMS (ONLY REQUESTED) ---------------- */
		project := c.Query("project_id")
		woNumber := c.Query("wo_number")
		contactPerson := c.Query("contact_person")
		revisionNo := c.Query("revision_no")
		totalValue := c.Query("total_value")
		totalValueFilter := c.Query("total_value_filter_type") // gt | lt | eq
		woDate := c.Query("wo_date")
		woValidate := c.Query("wo_validate")
		createdAt := c.Query("created_at")
		endClient := c.Query("end_client_id")

		/* ---------------- APPLY FILTERS ---------------- */

		if woNumber != "" {
			conditions = append(conditions, fmt.Sprintf("wo.wo_number ILIKE $%d", argIndex))
			args = append(args, "%"+woNumber+"%")
			argIndex++
		}

		if contactPerson != "" {
			conditions = append(conditions, fmt.Sprintf("wo.contact_person ILIKE $%d", argIndex))
			args = append(args, "%"+contactPerson+"%")
			argIndex++
		}

		if endClient != "" {
			conditions = append(conditions, fmt.Sprintf("wo.endclient_id = $%d", argIndex))
			args = append(args, endClient)
			argIndex++
		}

		if project != "" {
			conditions = append(conditions, fmt.Sprintf("wo.project_id = $%d", argIndex))
			args = append(args, project)
			argIndex++
		}

		if revisionNo != "" {
			conditions = append(conditions, fmt.Sprintf("wo.revision = $%d", argIndex))
			args = append(args, revisionNo)
			argIndex++
		}

		if woValidate != "" {
			conditions = append(conditions, fmt.Sprintf("wo.wo_validate = $%d", argIndex))
			args = append(args, woValidate)
			argIndex++
		}

		if woDate != "" {
			conditions = append(conditions, fmt.Sprintf("DATE(wo.wo_date) = $%d", argIndex))
			args = append(args, woDate)
			argIndex++
		}

		if createdAt != "" {
			conditions = append(conditions, fmt.Sprintf("DATE(wo.created_at) = $%d", argIndex))
			args = append(args, createdAt)
			argIndex++
		}

		if totalValue != "" && totalValueFilter != "" {
			switch strings.ToLower(totalValueFilter) {
			case "gt":
				conditions = append(conditions, fmt.Sprintf("wo.total_value > $%d", argIndex))
			case "lt":
				conditions = append(conditions, fmt.Sprintf("wo.total_value < $%d", argIndex))
			case "eq":
				conditions = append(conditions, fmt.Sprintf("wo.total_value = $%d", argIndex))
			}
			args = append(args, totalValue)
			argIndex++
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT ---------------- */
		var totalCount int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM work_order wo
			LEFT JOIN end_client ec ON wo.endclient_id=ec.id
			LEFT JOIN project p ON wo.project_id=p.project_id
			WHERE %s
		`, whereClause)

		if err := db.QueryRow(countQuery, args...).Scan(&totalCount); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY ---------------- */
		query := fmt.Sprintf(`
			SELECT 
				wo.id, wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value,
				wo.contact_person, wo.payment_term, wo.wo_description,
				wo.created_at, wo.updated_at, wo.created_by,
				CONCAT(u.first_name,' ',u.last_name),
				wo.endclient_id, ec.contact_person,
				wo.project_id, p.name,
				wo.contact_email, wo.contact_number,
				wo.phone_code, pc.phone_code,
				wo.shipped_address, wo.billed_address
			FROM work_order wo
			LEFT JOIN users u ON wo.created_by=u.id::text
			LEFT JOIN end_client ec ON wo.endclient_id=ec.id
			LEFT JOIN project p ON wo.project_id=p.project_id
			LEFT JOIN phone_code pc ON wo.phone_code=pc.id
			WHERE %s
			ORDER BY wo.created_at DESC
		`, whereClause)
		
		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "1", "error": err.Error()})
			return
		}
		defer rows.Close()

		var workOrders []models.WorkOrder
		for rows.Next() {
			var wo models.WorkOrder
			var paymentTermRaw sql.NullString
			rows.Scan(&wo.ID, &wo.WONumber, &wo.WODate, &wo.WOValidate, &wo.TotalValue, &wo.ContactPerson,
				&paymentTermRaw, &wo.WODescription, &wo.CreatedAt, &wo.UpdatedAt, &wo.CreatedBy, &wo.CreatedByName, &wo.EndClientID, &wo.EndClient,
				&wo.ProjectID, &wo.ProjectName, &wo.ContactEmail, &wo.ContactNumber, &wo.PhoneCode, &wo.PhoneCodeName, &wo.ShippedAddress, &wo.BilledAddress)

			if paymentTermRaw.Valid {
				var pt map[string]float64
				if json.Unmarshal([]byte(paymentTermRaw.String), &pt) == nil {
					wo.PaymentTerm = pt
				}
			}

			// ✅ Append revision number if not zero
			if wo.RevisionNo != 0 {
				wo.WONumber = fmt.Sprintf("%s - %d", wo.WONumber, wo.RevisionNo)
			}

			// fetch materials
			matRows, err := db.Query(`SELECT id, item_name, unit_rate, volume, tax, volume_used, hsn_code, tower_id, floor_id FROM work_order_material WHERE work_order_id=$1`, wo.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"details": "2", "error": err.Error()})
				return
			}

			for matRows.Next() {
				var m models.WorkOrderMaterial
				var towerID sql.NullInt64
				var floorID sql.NullString

				// Initialize defaults
				m.TowerID = new(int)
				*m.TowerID = 0
				m.TowerName = ""
				m.FloorID = []int{0}
				m.FloorName = []string{""}

				if err := matRows.Scan(&m.ID, &m.ItemName, &m.UnitRate, &m.Volume, &m.Tax, &m.VolumeUsed, &m.HsnCode, &towerID, &floorID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"details": "3", "error": err.Error()})
					return
				}

				m.Balance = m.Volume - m.VolumeUsed

				// Fetch tower name
				if towerID.Valid {
					var towerName string
					if err := db.QueryRow(`SELECT name FROM precast WHERE id=$1`, towerID.Int64).Scan(&towerName); err == nil {
						m.TowerName = towerName
					}
				}

				// Fetch floor names
				if floorID.Valid {
					m.FloorID = []int{}
					m.FloorName = []string{}
					for _, f := range strings.Split(floorID.String, ",") {
						f = strings.TrimSpace(f)
						if fid, err := strconv.Atoi(f); err == nil {
							var floorName string
							if err := db.QueryRow(`SELECT name FROM precast WHERE id=$1`, fid).Scan(&floorName); err == nil {
								m.FloorID = append(m.FloorID, fid)
								m.FloorName = append(m.FloorName, floorName)
							}
						}
					}
				}

				wo.Material = append(wo.Material, m)
			}
			matRows.Close()

			// fetch attachments
			attRows, _ := db.Query(`SELECT file_url FROM work_order_attachment WHERE work_order_id=$1`, wo.ID)
			for attRows.Next() {
				var file string
				attRows.Scan(&file)
				wo.Attachments = append(wo.Attachments, file)
			}
			attRows.Close()

			workOrders = append(workOrders, wo)
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": workOrders,
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
			EventContext: "Work Order",
			EventName:    "Search",
			Description:  "Searched work orders with advanced filters",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}
		// Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Work orders searched but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// SearchPendingInvoices godoc
// @Summary      Search pending invoices
// @Tags         work-orders
// @Param        page   query  int  false  "Page"
// @Param        limit  query  int  false  "Limit"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/search-pending-invoice [get]
func SearchPendingInvoices(db *sql.DB) gin.HandlerFunc {
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
			FROM users u
			JOIN roles r ON u.role_id=r.role_id
			WHERE u.id=$1`, userID).Scan(&roleName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role"})
			return
		}

		/* ---------------- CONDITIONS ---------------- */
		var conditions []string
		var args []interface{}
		argIndex := 1

		// pending invoices ONLY
		conditions = append(conditions, "i.indraft = true")

		/* ---------------- ACCESS CONTROL ---------------- */
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
		totalPaidFilter := c.Query("total_paid_filter_type")

		totalValue := c.Query("total_value")
		totalValueFilter := c.DefaultQuery("total_value_filter_type",
			c.Query("total_value_filter_Type")) // handle wrong casing

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

		if paymentStatus != "" {
			conditions = append(conditions, fmt.Sprintf("i.payment_status=$%d", argIndex))
			args = append(args, paymentStatus)
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

		/* -------- NUMERIC FILTERS -------- */

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
				wo.contact_email, wo.contact_number,
				wo.phone_code, pc.phone_code,
				wo.revision
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
				&inv.PhoneCode, &inv.PhoneCodeName,
				&inv.WORevision,
			)

			if ptRaw.Valid {
				json.Unmarshal([]byte(ptRaw.String), &inv.PaymentTerm)
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

			invoices = append(invoices, inv)
		}

		c.JSON(http.StatusOK, gin.H{
			"data": invoices,
			"pagination": gin.H{
				"page":          page,
				"limit":         limit,
				"total_records": total,
				"total_pages":   (total + limit - 1) / limit,
			},
		})

		go SaveActivityLog(db, models.ActivityLog{
			EventContext: "Pending Invoice",
			EventName:    "Search",
			Description:  "Advanced search pending invoices",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

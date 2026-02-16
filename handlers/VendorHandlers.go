package handlers

import (
	"backend/models"
	"backend/repository"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateVendor creates a new vendor.
// @Summary Create vendor
// @Description Creates a new vendor. Request body: name, email, phone, address, status, vendor_type, project_id, etc. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Param body body models.Vendor true "Vendor data"
// @Success 201 {object} models.Vendor
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/create_Vendor [post]
func CreateVendor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var vendor models.Vendor
		if err = c.ShouldBindJSON(&vendor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON input", "details": err.Error()})
			return
		}

		// Set timestamps
		vendor.CreatedAt = time.Now()
		vendor.UpdatedAt = time.Now()
		vendor.VendorID = repository.GenerateRandomNumber()

		// Insert query
		query := `
			INSERT INTO inv_vendors (vendor_id,name, email, phone, address, status, vendor_type, created_at, updated_at, created_by, updated_by, project_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,$12)
			RETURNING vendor_id
		`

		err = db.QueryRow(query,
			vendor.VendorID,
			vendor.Name,
			vendor.Email,
			vendor.Phone,
			vendor.Address,
			vendor.Status,
			vendor.VendorType,
			vendor.CreatedAt,
			vendor.UpdatedAt,
			vendor.CreatedBy,
			vendor.UpdatedBy,
			vendor.ProjectID, // Corrected this line
		).Scan(&vendor.VendorID)
		if err != nil {
			log.Printf("Error inserting vendor: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert vendor", "details": err.Error()})
			return
		}

		// Return the created vendor
		c.JSON(http.StatusCreated, vendor)

		log := models.ActivityLog{
			EventContext:      "Vendor",
			EventName:         "Create",
			Description:       "Create Vendor",
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         vendor.ProjectID,
			AffectedUserName:  vendor.Name,
			AffectedUserEmail: vendor.Email,
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

// UpdateVendor updates a vendor by ID.
// @Summary Update vendor
// @Description Updates vendor by id. Send vendor fields in body; id in path. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Param id path int true "Vendor ID"
// @Param body body models.Vendor true "Vendor data"
// @Success 200 {object} models.Vendor
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/update_Vendor/{id} [put]
func UpdateVendor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from Authorization header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var vendor models.Vendor
		if err := c.ShouldBindJSON(&vendor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Parse vendor ID from the URL
		vendorIDStr := c.Param("id")
		vendorID, err := strconv.Atoi(vendorIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid vendor ID"})
			return
		}

		// Check if the vendor exists
		var existingVendorID int
		err = db.QueryRow("SELECT vendor_id FROM inv_vendors WHERE vendor_id = $1", vendorID).Scan(&existingVendorID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Vendor not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var updates []string
		var fields []interface{}
		placeholderIndex := 1

		// Fields to update
		if vendor.Name != "" {
			updates = append(updates, fmt.Sprintf("name = $%d", placeholderIndex))
			fields = append(fields, vendor.Name)
			placeholderIndex++
		}
		if vendor.Email != "" {
			updates = append(updates, fmt.Sprintf("email = $%d", placeholderIndex))
			fields = append(fields, vendor.Email)
			placeholderIndex++
		}
		if vendor.Phone != "" {
			updates = append(updates, fmt.Sprintf("phone = $%d", placeholderIndex))
			fields = append(fields, vendor.Phone)
			placeholderIndex++
		}
		if vendor.Address != "" {
			updates = append(updates, fmt.Sprintf("address = $%d", placeholderIndex))
			fields = append(fields, vendor.Address)
			placeholderIndex++
		}
		if vendor.Status != "" {
			updates = append(updates, fmt.Sprintf("status = $%d", placeholderIndex))
			fields = append(fields, vendor.Status)
			placeholderIndex++
		}
		if vendor.VendorType != "" {
			updates = append(updates, fmt.Sprintf("vendor_type = $%d", placeholderIndex))
			fields = append(fields, vendor.VendorType)
			placeholderIndex++
		}
		if vendor.UpdatedBy != "" {
			updates = append(updates, fmt.Sprintf("updated_by = $%d", placeholderIndex))
			fields = append(fields, vendor.UpdatedBy)
			placeholderIndex++
		}
		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
			return
		}

		updates = append(updates, fmt.Sprintf("updated_at = $%d", placeholderIndex))
		fields = append(fields, time.Now())
		placeholderIndex++

		sqlStatement := fmt.Sprintf("UPDATE inv_vendors SET %s WHERE vendor_id = $%d", strings.Join(updates, ", "), placeholderIndex)
		fields = append(fields, vendorID)

		_, err = db.Exec(sqlStatement, fields...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Vendor updated successfully"})

		log := models.ActivityLog{
			EventContext:      "Vendor",
			EventName:         "UPDATE",
			Description:       "Update Vendor",
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         vendor.ProjectID,
			AffectedUserName:  vendor.Name,
			AffectedUserEmail: vendor.Email,
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

// DeleteVendor deletes a vendor by ID.
// @Summary Delete vendor
// @Description Delete vendor by id. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Param id path int true "Vendor ID"
// @Success 200 {object} models.MessageResponse "message: Vendor deleted successfully"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/delete_Vendor/{id} [delete]
func DeleteVendor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		id := c.Param("id")

		var name, email string
		var project_id int
		err = db.QueryRow(`SELECT name , email, project_id FROM inv_vendors WHERE vendor_id = $1`, id).Scan(&name, &email, &project_id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		query := `DELETE FROM inv_vendors WHERE vendor_id = $1`
		result, err := db.Exec(query, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete vendor", "details": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Vendor not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Vendor deleted successfully"})

		log := models.ActivityLog{
			EventContext:      "Vendor",
			EventName:         "DELETE",
			Description:       "DELETE Vendor",
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         project_id,
			AffectedUserName:  name,
			AffectedUserEmail: email,
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

// GetVendorByID returns a single vendor by ID.
// @Summary Get vendor by ID
// @Description Returns one vendor by id. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Param id path int true "Vendor ID"
// @Success 200 {object} models.Vendor
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_vendor/{id} [get]
func GetVendorByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		id := c.Param("id")
		query := `
			SELECT vendor_id, name, email, phone, address, status, vendor_type, created_at, updated_at, created_by, updated_by, project_id
			FROM inv_vendors WHERE vendor_id = $1
		`
		row := db.QueryRow(query, id)
		var vendor models.Vendor
		if err := row.Scan(&vendor.VendorID, &vendor.Name, &vendor.Email, &vendor.Phone, &vendor.Address,
			&vendor.Status, &vendor.VendorType, &vendor.CreatedAt, &vendor.UpdatedAt, &vendor.CreatedBy, &vendor.UpdatedBy, &vendor.ProjectID); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Vendor not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vendor", "details": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, vendor)

		log := models.ActivityLog{
			EventContext: "Vendor",
			EventName:    "GET",
			Description:  "GET Vendor" + vendor.Name,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    vendor.ProjectID,
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

// GetVendors returns all vendors.
// @Summary Get all vendors
// @Description Returns all vendors. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Success 200 {array} models.Vendor
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/get_vendor [get]
func GetVendors(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		query := `
			SELECT vendor_id, name, email, phone, address, status, vendor_type, created_at, updated_at, created_by, updated_by, project_id
			FROM inv_vendors
		`
		rows, err := db.Query(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vendors", "details": err.Error()})
			return
		}
		defer rows.Close()

		var vendors []models.Vendor
		for rows.Next() {
			var vendor models.Vendor
			if err := rows.Scan(&vendor.VendorID, &vendor.Name, &vendor.Email, &vendor.Phone, &vendor.Address,
				&vendor.Status, &vendor.VendorType, &vendor.CreatedAt, &vendor.UpdatedAt, &vendor.CreatedBy, &vendor.UpdatedBy, &vendor.ProjectID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan vendor", "details": err.Error()})
				return
			}
			vendors = append(vendors, vendor)
		}

		c.JSON(http.StatusOK, vendors)

		log := models.ActivityLog{
			EventContext: "Vendor",
			EventName:    "GET",
			Description:  "GET All Vendors",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
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

// GetVendorsProjectId returns vendors for a project.
// @Summary Get vendors by project ID
// @Description Returns all vendors for the given project_id. Requires Authorization header.
// @Tags Vendors
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.Vendor
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/fetch_vendor/{project_id} [get]
func GetVendorsProjectId(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Extract the project_id from the query parameters
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
			return
		}

		// Convert project_id to an integer
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id must be a valid integer"})
			return
		}

		// Query to fetch vendors by project_id
		query := `
			SELECT vendor_id, name, email, phone, address, status, vendor_type, created_at, updated_at, created_by, updated_by, project_id
			FROM inv_vendors
			WHERE project_id = $1
		`

		// Execute the query
		rows, err := db.Query(query, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vendors", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Parse the result into a slice of Vendor models
		var vendors []models.Vendor
		for rows.Next() {
			var vendor models.Vendor
			if err := rows.Scan(
				&vendor.VendorID,
				&vendor.Name,
				&vendor.Email,
				&vendor.Phone,
				&vendor.Address,
				&vendor.Status,
				&vendor.VendorType,
				&vendor.CreatedAt,
				&vendor.UpdatedAt,
				&vendor.CreatedBy,
				&vendor.UpdatedBy,
				&vendor.ProjectID,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan vendor", "details": err.Error()})
				return
			}
			vendors = append(vendors, vendor)
		}

		// Check for errors during iteration
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error occurred during rows iteration", "details": err.Error()})
			return
		}

		// If no vendors are found, return a 202 status and an empty array
		if len(vendors) == 0 {
			c.JSON(http.StatusAccepted, []models.Vendor{})
			return
		}

		// Return the list of vendors
		c.JSON(http.StatusOK, vendors)

		var project_name string
		_ = db.QueryRow(`SELECT name FROM project where project_id = $1`, projectID).Scan(&project_name)

		log := models.ActivityLog{
			EventContext: "Vendor",
			EventName:    "GET",
			Description:  "GET All Vendors of project" + project_name,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
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

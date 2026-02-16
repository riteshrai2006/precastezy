package handlers

import (
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GetQCStatus returns a single QC status by element id.
// @Summary Get QC status by ID
// @Description Returns QC status by element id. Requires Authorization header.
// @Tags QC
// @Accept json
// @Produce json
// @Param id path int true "Element ID (QC status id)"
// @Success 200 {object} models.QCStatus
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/qcstatuses/{id} [get]
func GetQCStatus(db *sql.DB) gin.HandlerFunc {
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

		idStr := c.Param("id")
		if idStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Element ID is required in the URL"})
			return
		}

		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
			return
		}

		status, err := getQCStatusByID(id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "QC Status not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve QC Status: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, status)

		log := models.ActivityLog{
			EventContext: "QC",
			EventName:    "Get",
			Description:  "Get QC Status",
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

func getQCStatusByID(id int) (models.QCStatus, error) {
	db := storage.GetDB()

	var status models.QCStatus
	query := `
		SELECT element_id, project_id, design_id, qc_status, remarks, status, picture_gallery, date_created, date_assingned, completion_date, date_completed
		FROM qc_status WHERE element_id = $1`
	err := db.QueryRow(query, id).Scan(
		&status.ElementID, &status.ProjectID, &status.DesignID, &status.QCStatus, &status.Remarks, &status.Status, &status.PictureGallery, &status.DateCreated, &status.DateAssigned, &status.CompletionDate, &status.DateCompleted)

	return status, err
}

// GetAllQCStatuses returns all QC statuses.
// @Summary Get all QC statuses
// @Description Returns all QC statuses. Requires Authorization header.
// @Tags QC
// @Accept json
// @Produce json
// @Success 200 {array} models.QCStatus
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/qcstatuses_fetch [get]
func GetAllQCStatuses(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		rows, err := db.Query(`
		SELECT element_id, project_id, design_id, qc_status, remarks, status, picture_gallery, date_created, date_assingned, completion_date, date_completed
		FROM qc_status`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve QC Statuses: " + err.Error()})
			return
		}
		defer rows.Close()

		var statuses []models.QCStatus
		for rows.Next() {
			var status models.QCStatus
			err := rows.Scan(
				&status.ElementID, &status.ProjectID, &status.DesignID, &status.QCStatus, &status.Remarks, &status.Status, &status.PictureGallery, &status.DateCreated, &status.DateAssigned, &status.CompletionDate, &status.DateCompleted)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan QC Status: " + err.Error()})
				return
			}
			statuses = append(statuses, status)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, statuses)

		log := models.ActivityLog{
			EventContext: "QC",
			EventName:    "Get",
			Description:  "Get All QC Status",
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

// CreateQCStatus creates a new QC status.
// @Summary Create QC status
// @Description Creates a new QC status. Body: project_id, design_id, qc_status, remarks, status, etc. Requires Authorization header.
// @Tags QC
// @Accept json
// @Produce json
// @Param body body models.QCStatus true "QC status data"
// @Success 201 {object} models.QCStatus
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/qcstatuses_create [post]
func CreateQCStatus(db *sql.DB) gin.HandlerFunc {
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

		var status models.QCStatus

		if err := c.ShouldBindJSON(&status); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		status.DateCreated = time.Now()
		status.DateAssigned = time.Now()

		sqlStatement := `
		INSERT INTO qc_status (project_id, design_id, qc_status, remarks, status, picture_gallery, date_created, date_assingned, completion_date, date_completed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING element_id`

		var id int
		err = db.QueryRow(sqlStatement, status.ProjectID, status.DesignID, status.QCStatus, status.Remarks, status.Status, status.PictureGallery, status.DateCreated, status.DateAssigned, status.CompletionDate, status.DateCompleted).Scan(&id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		status.ElementID = id
		c.JSON(http.StatusCreated, status)

		log := models.ActivityLog{
			EventContext: "QC",
			EventName:    "POST",
			Description:  "Create QC Status",
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

// UpdateQCStatus updates a QC status by id.
// @Summary Update QC status
// @Description Updates QC status by id. Body: QC status fields. Requires Authorization header.
// @Tags QC
// @Accept json
// @Produce json
// @Param id path int true "Element/QC status ID"
// @Param body body models.QCStatus true "QC status data"
// @Success 200 {object} models.QCStatus
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/qcstatuses_update/{id} [put]
func UpdateQCStatus(db *sql.DB) gin.HandlerFunc {
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

		var status models.QCStatus

		if err := c.ShouldBindJSON(&status); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		elementIDStr := c.Param("id")
		elementID, err := strconv.Atoi(elementIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
			return
		}

		var updates []string
		var fields []interface{}
		placeholderIndex := 1

		if status.QCStatus != "" {
			updates = append(updates, fmt.Sprintf("qc_status = $%d", placeholderIndex))
			fields = append(fields, status.QCStatus)
			placeholderIndex++
		}
		if status.Remarks != "" {
			updates = append(updates, fmt.Sprintf("remarks = $%d", placeholderIndex))
			fields = append(fields, status.Remarks)
			placeholderIndex++
		}
		if status.Status != "" {
			updates = append(updates, fmt.Sprintf("status = $%d", placeholderIndex))
			fields = append(fields, status.Status)
			placeholderIndex++
		}
		if status.PictureGallery != "" {
			updates = append(updates, fmt.Sprintf("picture_gallery = $%d", placeholderIndex))
			fields = append(fields, status.PictureGallery)
			placeholderIndex++
		}
		if !status.DateAssigned.IsZero() {
			updates = append(updates, fmt.Sprintf("date_assigned = $%d", placeholderIndex))
			fields = append(fields, status.DateAssigned)
			placeholderIndex++
		}
		if !status.DateCreated.IsZero() {
			updates = append(updates, fmt.Sprintf("date_created = $%d", placeholderIndex))
			fields = append(fields, status.DateCreated)
			placeholderIndex++
		}
		if !status.CompletionDate.IsZero() {
			updates = append(updates, fmt.Sprintf("completion_date = $%d", placeholderIndex))
			fields = append(fields, status.CompletionDate)
			placeholderIndex++
		}
		if !status.DateCompleted.IsZero() {
			updates = append(updates, fmt.Sprintf("date_completed = $%d", placeholderIndex))
			fields = append(fields, status.DateCompleted)
			placeholderIndex++
		}

		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
			return
		}

		sqlStatement := fmt.Sprintf("UPDATE qc_status SET %s WHERE element_id = $%d", strings.Join(updates, ", "), placeholderIndex)
		fields = append(fields, elementID)

		_, err = db.Exec(sqlStatement, fields...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "QC Status updated successfully"})

		log := models.ActivityLog{
			EventContext: "QC",
			EventName:    "Update",
			Description:  "Update QC Status of elementID" + elementIDStr,
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

// DeleteQCStatus deletes a QC status by id.
// @Summary Delete QC status
// @Description Deletes QC status by id. Requires Authorization header.
// @Tags QC
// @Accept json
// @Produce json
// @Param id path int true "Element/QC status ID"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/qcstatuses_delete/{id} [delete]
func DeleteQCStatus(db *sql.DB) gin.HandlerFunc {
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

		// Extract element ID from the URL
		elementIDStr := c.Param("id")
		elementID, err := strconv.Atoi(elementIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid element ID"})
			return
		}

		// Attempt to delete the QC Status from the database
		err = deleteQCStatusFromDB(elementID)
		if err != nil {
			if err.Error() == "no rows affected" {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("No QC Status found with ID %d", elementID)})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete QC Status"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("QC Status with ID %d successfully deleted", elementID)})

		log := models.ActivityLog{
			EventContext: "QC",
			EventName:    "Delete",
			Description:  "Delete QC Status of elementID:" + elementIDStr,
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

func deleteQCStatusFromDB(id int) error {
	db := storage.GetDB()
	if db == nil {
		return fmt.Errorf("database connection is not available")
	}

	// Prepare the delete query
	query := "DELETE FROM qc_status WHERE element_id = $1"
	result, err := db.Exec(query, id)
	if err != nil {
		return err
	}

	// Check the number of rows affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no rows affected")
	}

	return nil
}

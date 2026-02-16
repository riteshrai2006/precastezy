package handlers

import (
	"backend/models"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// SQL query constants for transporter operations
const (
	// createTransporterTableSQL creates the transporter table if it doesn't exist
	createTransporterTableSQL = `
		CREATE TABLE IF NOT EXISTS transporter (
			id SERIAL PRIMARY KEY,
			name        TEXT NOT NULL,
			address     TEXT,
			phone_no    VARCHAR(20),
			gst_no      VARCHAR(20),
			emergency_contact_no VARCHAR(20),
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`

	// insertTransporterSQL inserts a new transporter and returns the created record
	insertTransporterSQL = `
		INSERT INTO transporter (name, address, phone_no, gst_no, emergency_contact_no)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, address, phone_no, gst_no, emergency_contact_no, created_at, updated_at`

	// selectTransporterByIDSQL retrieves a transporter by its ID
	selectTransporterByIDSQL = `
		SELECT id, name, address, phone_no, gst_no, emergency_contact_no, created_at, updated_at
		FROM transporter
		WHERE id = $1`

	// selectAllTransportersSQL retrieves all transporters ordered by ID
	selectAllTransportersSQL = `
		SELECT id, name, address, phone_no, gst_no, emergency_contact_no, created_at, updated_at
		FROM transporter
		ORDER BY id ASC`

	// updateTransporterSQL updates a transporter and returns the updated record
	updateTransporterSQL = `
		UPDATE transporter
		SET name = $1, address = $2, phone_no = $3, emergency_contact_no = $4, gst_no = $5, updated_at = NOW()
		WHERE id = $6
		RETURNING id, name, address, phone_no, emergency_contact_no, gst_no, created_at, updated_at`

	// deleteTransporterSQL deletes a transporter by ID
	deleteTransporterSQL = `DELETE FROM transporter WHERE id = $1`
)

// Error message constants for transporter operations
const (
	errInvalidTransporterID = "invalid transporter id"
	errTransporterNotFound  = "transporter not found"
	errFailedToEnsureTable  = "failed to ensure transporter table"
)

// transporterRequest represents the request payload for creating/updating a transporter
type transporterRequest struct {
	Name               string `json:"name" binding:"required"`
	Address            string `json:"address"`
	PhoneNo            string `json:"phone_no"`
	EmergencyContactNo string `json:"emergency_contact_no"`
	GstNo              string `json:"gst_no"`
}

// ensureTransporterTable ensures the transporter table exists in the database.
// It creates the table if it doesn't exist, otherwise does nothing.
func ensureTransporterTable(db *sql.DB) error {
	_, err := db.Exec(createTransporterTableSQL)
	return err
}

// parseTransporterID extracts and parses the transporter ID from the request parameters.
// Returns the parsed ID or an error if the ID is invalid.
func parseTransporterID(c *gin.Context) (int, error) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// respondWithError sends a JSON error response with the specified status code and message.
func respondWithTransporterError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{"error": message})
}

// respondWithTransporterInternalError sends a JSON error response for internal server errors.
// It includes both the error message and the underlying error details.
func respondWithTransporterInternalError(c *gin.Context, message string, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": message + ": " + err.Error()})
}

// scanTransporter scans database rows into a Transporter model.
// It maps the database columns to the transporter struct fields.
func scanTransporter(rows *sql.Rows, transporter *models.Transporter) error {
	return rows.Scan(
		&transporter.ID,
		&transporter.Name,
		&transporter.Address,
		&transporter.PhoneNo,
		&transporter.GstNo,
		&transporter.EmergencyContactNo,
		&transporter.CreatedAt,
		&transporter.UpdatedAt,
	)
}

// CreateTransporter godoc
// @Summary      Create transporter
// @Tags         transporters
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "Transporter (name, address, phone_no, gst_no, emergency_contact_no)"
// @Success      201   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/transporters [post]
func CreateTransporter(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the transporter table exists
		if err := ensureTransporterTable(db); err != nil {
			respondWithTransporterInternalError(c, errFailedToEnsureTable, err)
			return
		}

		// Parse and validate the request body
		var req transporterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondWithTransporterError(c, http.StatusBadRequest, err.Error())
			return
		}

		// Insert the transporter into the database
		var transporter models.Transporter
		err := db.QueryRow(
			insertTransporterSQL,
			req.Name,
			req.Address,
			req.PhoneNo,
			req.GstNo,
			req.EmergencyContactNo,
		).Scan(
			&transporter.ID,
			&transporter.Name,
			&transporter.Address,
			&transporter.PhoneNo,
			&transporter.GstNo,
			&transporter.EmergencyContactNo,
			&transporter.CreatedAt,
			&transporter.UpdatedAt,
		)

		if err != nil {
			respondWithTransporterInternalError(c, "failed to create transporter", err)
			return
		}

		// Return success response with created transporter data
		c.JSON(http.StatusCreated, gin.H{
			"message": "Transporter created successfully",
			"data":    transporter,
		})
	}
}

// GetTransporterByID handles GET /transporters/:id requests.
// It retrieves a single transporter by its ID.
// Returns: 200 OK with transporter data, 404 Not Found if transporter doesn't exist, or an error response.
// GetTransporterByID godoc
// @Summary      Get transporter by ID
// @Tags         transporters
// @Param        id   path      int  true  "Transporter ID"
// @Success      200  {object}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/transporters/{id} [get]
func GetTransporterByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the transporter table exists
		if err := ensureTransporterTable(db); err != nil {
			respondWithTransporterInternalError(c, errFailedToEnsureTable, err)
			return
		}

		// Parse and validate the transporter ID from URL parameter
		id, err := parseTransporterID(c)
		if err != nil {
			respondWithTransporterError(c, http.StatusBadRequest, errInvalidTransporterID)
			return
		}

		// Query the database for the transporter
		var transporter models.Transporter
		err = db.QueryRow(selectTransporterByIDSQL, id).Scan(
			&transporter.ID,
			&transporter.Name,
			&transporter.Address,
			&transporter.PhoneNo,
			&transporter.GstNo,
			&transporter.EmergencyContactNo,
			&transporter.CreatedAt,
			&transporter.UpdatedAt,
		)

		// Handle not found error
		if err == sql.ErrNoRows {
			respondWithTransporterError(c, http.StatusNotFound, errTransporterNotFound)
			return
		}
		// Handle other database errors
		if err != nil {
			respondWithTransporterInternalError(c, "failed to retrieve transporter", err)
			return
		}

		// Return success response with transporter data
		c.JSON(http.StatusOK, gin.H{"data": transporter})
	}
}

// GetAllTransporters handles GET /transporters requests.
// It retrieves all transporters from the database, ordered by ID.
// Returns: 200 OK with an array of transporter data, or an error response.
// GetAllTransporters godoc
// @Summary      List transporters
// @Tags         transporters
// @Success      200  {array}  object
// @Router       /api/transporters [get]
func GetAllTransporters(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the transporter table exists
		if err := ensureTransporterTable(db); err != nil {
			respondWithTransporterInternalError(c, errFailedToEnsureTable, err)
			return
		}

		// Query all transporters from the database
		rows, err := db.Query(selectAllTransportersSQL)
		if err != nil {
			respondWithTransporterInternalError(c, "failed to fetch transporters", err)
			return
		}
		defer rows.Close()

		// Scan all rows into a slice of transporters
		var transporters []models.Transporter
		for rows.Next() {
			var transporter models.Transporter
			if err := scanTransporter(rows, &transporter); err != nil {
				respondWithTransporterInternalError(c, "failed to parse transporter data", err)
				return
			}
			transporters = append(transporters, transporter)
		}

		// Check for errors during row iteration
		if err := rows.Err(); err != nil {
			respondWithTransporterInternalError(c, "failed to process transporters", err)
			return
		}

		// Return success response with all transporters
		c.JSON(http.StatusOK, gin.H{"data": transporters})
	}
}

// UpdateTransporter handles PUT /transporters/:id requests.
// It updates an existing transporter record in the database.
// Expected request body: { "name": string (required), "address": string, "phone_no": string, "emergency_contact_no": string, "gst_no": string }
// Returns: 200 OK with updated transporter data, 404 Not Found if transporter doesn't exist, or an error response.
// UpdateTransporter godoc
// @Summary      Update transporter
// @Tags         transporters
// @Param        id     path      int  true  "Transporter ID"
// @Param        body   body      object  true  "Transporter fields"
// @Success      200    {object}  object
// @Router       /api/transporters/{id} [put]
func UpdateTransporter(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the transporter table exists
		if err := ensureTransporterTable(db); err != nil {
			respondWithTransporterInternalError(c, errFailedToEnsureTable, err)
			return
		}

		// Parse and validate the transporter ID from URL parameter
		id, err := parseTransporterID(c)
		if err != nil {
			respondWithTransporterError(c, http.StatusBadRequest, errInvalidTransporterID)
			return
		}

		// Parse and validate the request body
		var req transporterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondWithTransporterError(c, http.StatusBadRequest, err.Error())
			return
		}

		// Update the transporter in the database
		var transporter models.Transporter
		err = db.QueryRow(
			updateTransporterSQL,
			req.Name,
			req.Address,
			req.PhoneNo,
			req.EmergencyContactNo,
			req.GstNo,
			id,
		).Scan(
			&transporter.ID,
			&transporter.Name,
			&transporter.Address,
			&transporter.PhoneNo,
			&transporter.EmergencyContactNo,
			&transporter.GstNo,
			&transporter.CreatedAt,
			&transporter.UpdatedAt,
		)

		// Handle not found error
		if err == sql.ErrNoRows {
			respondWithTransporterError(c, http.StatusNotFound, errTransporterNotFound)
			return
		}
		// Handle other database errors
		if err != nil {
			respondWithTransporterInternalError(c, "failed to update transporter", err)
			return
		}

		// Return success response with updated transporter data
		c.JSON(http.StatusOK, gin.H{
			"message": "Transporter updated successfully",
			"data":    transporter,
		})
	}
}

// DeleteTransporter handles DELETE /transporters/:id requests.
// It deletes a transporter record from the database by ID.
// Returns: 200 OK on successful deletion, 404 Not Found if transporter doesn't exist, or an error response.
// DeleteTransporter godoc
// @Summary      Delete transporter
// @Tags         transporters
// @Param        id   path      int  true  "Transporter ID"
// @Success      200  {object}  object
// @Router       /api/transporters/{id} [delete]
func DeleteTransporter(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the transporter table exists
		if err := ensureTransporterTable(db); err != nil {
			respondWithTransporterInternalError(c, errFailedToEnsureTable, err)
			return
		}

		// Parse and validate the transporter ID from URL parameter
		id, err := parseTransporterID(c)
		if err != nil {
			respondWithTransporterError(c, http.StatusBadRequest, errInvalidTransporterID)
			return
		}

		// Execute the delete operation
		result, err := db.Exec(deleteTransporterSQL, id)
		if err != nil {
			respondWithTransporterInternalError(c, "failed to delete transporter", err)
			return
		}

		// Check if any rows were affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			respondWithTransporterInternalError(c, "failed to get delete result", err)
			return
		}

		// Handle case where transporter was not found
		if rowsAffected == 0 {
			respondWithTransporterError(c, http.StatusNotFound, errTransporterNotFound)
			return
		}

		// Return success response
		c.JSON(http.StatusOK, gin.H{"message": "Transporter deleted successfully"})
	}
}

package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"backend/models"

	"github.com/gin-gonic/gin"
)

// Constants for vehicle table and default values
const (
	vehicleDefaultStatus = "active"
	authorizationHeader  = "Authorization"
)

// SQL query constants for vehicle operations
const (
	// insertVehicleSQL inserts a new vehicle and returns the created record's ID and timestamps
	insertVehicleSQL = `
		INSERT INTO vehicle_details (vehicle_number, status, driver_name, truck_type, driver_contact_no, transporter_id, capacity)
		VALUES ($1, $2, $3, $4, $5, $6, $7) 
		RETURNING id, created_at, updated_at`

	// selectAllVehiclesSQL retrieves all vehicles from the database
	selectAllVehiclesSQL = `
		SELECT id, vehicle_number, status, created_at, updated_at, driver_name, truck_type, driver_contact_no, transporter_id, capacity 
		FROM vehicle_details`

	// selectVehicleByIDSQL retrieves a vehicle by its ID
	selectVehicleByIDSQL = `
		SELECT id, vehicle_number, status, created_at, updated_at, driver_name, truck_type, driver_contact_no, transporter_id, capacity 
		FROM vehicle_details 
		WHERE id = $1`

	// updateVehicleSQL updates a vehicle record and sets the updated_at timestamp
	updateVehicleSQL = `
		UPDATE vehicle_details 
		SET vehicle_number = $1, status = $2, driver_name = $3, truck_type = $4, driver_contact_no = $5, transporter_id = $6, capacity = $7, updated_at = NOW()
		WHERE id = $8`

	// deleteVehicleSQL deletes a vehicle by ID
	deleteVehicleSQL = `DELETE FROM vehicle_details WHERE id = $1`

	// selectVehicleNumberSQL retrieves a vehicle number by ID (used for logging after deletion)
	selectVehicleNumberSQL = `SELECT vehicle_number FROM vehicle_details WHERE id = $1`

	// selectVehicleByNumberSQL retrieves a vehicle by vehicle_number (for upsert lookup)
	selectVehicleByNumberSQL = `
		SELECT id, vehicle_number, status, created_at, updated_at, driver_name, truck_type, driver_contact_no, transporter_id, capacity
		FROM vehicle_details
		WHERE vehicle_number = $1`
)

// Error message constants for vehicle operations
const (
	errSessionHeaderRequired  = "session-id header is required"
	errInvalidSession         = "Invalid session"
	errInvalidRequestBody     = "Invalid request body"
	errVehicleNotFound        = "Vehicle not found"
	errFailedToLogActivity    = "Failed to log activity"
	errDuplicateVehicleNumber = "pq: duplicate key value violates unique constraint \"vehicle_details_vehicle_number_key\""
)

// Activity log event constants
const (
	eventContextVehicle  = "Vehicle"
	eventContextVehicles = "Vehicles"
	eventNameCreate      = "Create"
	eventNameGet         = "GET"
	eventNameUpdate      = "UPDATE"
	eventNameDelete      = "Delete"
)

// validateSession validates the session from the Authorization header.
// Returns the session, username, and a boolean indicating success.
// If validation fails, it sends an error response and returns false.
func validateSession(c *gin.Context, db *sql.DB) (models.Session, string, bool) {
	sessionID := c.GetHeader(authorizationHeader)
	if sessionID == "" {
		respondWithVehicleError(c, http.StatusBadRequest, errSessionHeaderRequired)
		return models.Session{}, "", false
	}

	session, userName, err := GetSessionDetails(db, sessionID)
	if err != nil {
		respondWithVehicleErrorDetails(c, http.StatusUnauthorized, errInvalidSession, err)
		return models.Session{}, "", false
	}

	return session, userName, true
}

// respondWithVehicleError sends a JSON error response with the specified status code and message.
func respondWithVehicleError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{"error": message})
}

// respondWithVehicleErrorDetails sends a JSON error response with status code, message, and error details.
func respondWithVehicleErrorDetails(c *gin.Context, statusCode int, message string, err error) {
	c.JSON(statusCode, gin.H{"error": message, "details": err.Error()})
}

// respondWithVehicleInternalError sends a JSON error response for internal server errors.
// It includes both the error message and the underlying error details.
func respondWithVehicleInternalError(c *gin.Context, message string, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": message, "details": err.Error()})
}

// logActivitySafely logs an activity and handles errors gracefully.
// If logging fails, it sends an error response but doesn't interrupt the main flow.
func logActivitySafely(c *gin.Context, db *sql.DB, log models.ActivityLog) {
	if err := SaveActivityLog(db, log); err != nil {
		respondWithVehicleInternalError(c, errFailedToLogActivity, err)
	}
}

// createActivityLog creates an ActivityLog model with the provided parameters.
// It sets default values for ProjectID and CreatedAt timestamp.
func createActivityLog(eventContext, eventName, description, userName string, session models.Session) models.ActivityLog {
	return models.ActivityLog{
		EventContext: eventContext,
		EventName:    eventName,
		Description:  description,
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    0,
	}
}

// scanVehicle scans database rows into a VehicleDetails model.
// It maps the database columns to the vehicle struct fields in the correct order.
func scanVehicle(rows *sql.Rows, vehicle *models.VehicleDetails) error {
	return rows.Scan(
		&vehicle.ID,
		&vehicle.VehicleNumber,
		&vehicle.Status,
		&vehicle.CreatedAt,
		&vehicle.UpdatedAt,
		&vehicle.DriverName,
		&vehicle.TruckType,
		&vehicle.DriverContactNo,
		&vehicle.TransporterID,
		&vehicle.Capacity,
	)
}

// ensureDefaultStatus sets the vehicle status to the default value if it's empty.
// This ensures all vehicles have a valid status when created.
func ensureDefaultStatus(vehicle *models.VehicleDetails) {
	if vehicle.Status == "" {
		vehicle.Status = vehicleDefaultStatus
	}
}

// handleDuplicateVehicleError checks if the error is a duplicate vehicle number error.
// If so, it sends a conflict response and returns true. Otherwise returns false.
func handleDuplicateVehicleError(c *gin.Context, err error) bool {
	if err.Error() == errDuplicateVehicleNumber {
		respondWithVehicleError(c, http.StatusConflict, "Vehicle number already exists")
		return true
	}
	return false
}

// CreateVehicleDetails handles POST /api/vehicles. Creates or updates a vehicle by vehicle_number.
// @Summary Create or update vehicle
// @Description Creates a new vehicle or updates existing by vehicle_number. Body: vehicle_number, status, driver_name, truck_type, driver_contact_no, transporter_id, capacity. Requires Authorization header.
// @Tags Vehicles
// @Accept json
// @Produce json
// @Param body body models.VehicleDetails true "Vehicle data"
// @Success 201 {object} models.VehicleDetails
// @Success 200 {object} models.VehicleDetails "When updating existing by vehicle_number"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/vehicles [post]
func CreateVehicleDetails(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session and get user information
		session, userName, ok := validateSession(c, db)
		if !ok {
			return
		}

		// Parse and validate the request body
		var vehicle models.VehicleDetails
		if err := c.ShouldBindJSON(&vehicle); err != nil {
			respondWithVehicleErrorDetails(c, http.StatusBadRequest, errInvalidRequestBody, err)
			return
		}

		// Set default status if not provided
		ensureDefaultStatus(&vehicle)

		// Look up existing row by vehicle_number
		var existing models.VehicleDetails
		err := db.QueryRow(selectVehicleByNumberSQL, vehicle.VehicleNumber).Scan(
			&existing.ID,
			&existing.VehicleNumber,
			&existing.Status,
			&existing.CreatedAt,
			&existing.UpdatedAt,
			&existing.DriverName,
			&existing.TruckType,
			&existing.DriverContactNo,
			&existing.TransporterID,
			&existing.Capacity,
		)
		if err == nil {
			// Path A — Update existing vehicle
			_, err = db.Exec(
				updateVehicleSQL,
				vehicle.VehicleNumber,
				vehicle.Status,
				vehicle.DriverName,
				vehicle.TruckType,
				vehicle.DriverContactNo,
				vehicle.TransporterID,
				vehicle.Capacity,
				existing.ID,
			)
			if err != nil {
				respondWithVehicleInternalError(c, "Failed to update vehicle record", err)
				return
			}
			// Fetch updated row for response (updated_at)
			err = db.QueryRow(selectVehicleByIDSQL, existing.ID).Scan(
				&vehicle.ID,
				&vehicle.VehicleNumber,
				&vehicle.Status,
				&vehicle.CreatedAt,
				&vehicle.UpdatedAt,
				&vehicle.DriverName,
				&vehicle.TruckType,
				&vehicle.DriverContactNo,
				&vehicle.TransporterID,
				&vehicle.Capacity,
			)
			if err != nil {
				respondWithVehicleInternalError(c, "Failed to fetch updated vehicle", err)
				return
			}
			c.JSON(http.StatusOK, vehicle)
			log := createActivityLog(
				eventContextVehicle,
				eventNameUpdate,
				"Update Vehicle Details (by vehicle number)",
				userName,
				session,
			)
			logActivitySafely(c, db, log)
			return
		}
		if err != sql.ErrNoRows {
			respondWithVehicleInternalError(c, "Failed to lookup vehicle", err)
			return
		}

		// Path B — Insert new vehicle
		err = db.QueryRow(
			insertVehicleSQL,
			vehicle.VehicleNumber,
			vehicle.Status,
			vehicle.DriverName,
			vehicle.TruckType,
			vehicle.DriverContactNo,
			vehicle.TransporterID,
			vehicle.Capacity,
		).Scan(&vehicle.ID, &vehicle.CreatedAt, &vehicle.UpdatedAt)

		if err != nil {
			if handleDuplicateVehicleError(c, err) {
				return
			}
			respondWithVehicleInternalError(c, "Failed to create vehicle record", err)
			return
		}

		c.JSON(http.StatusCreated, vehicle)
		log := createActivityLog(
			eventContextVehicle,
			eventNameCreate,
			"Create Vehicle Details",
			userName,
			session,
		)
		logActivitySafely(c, db, log)
	}
}

// GetAllVehicles handles GET /vehicles requests.
// It retrieves all vehicles from the database.
// Requires: Authorization header with valid session ID
// Returns: 200 OK with an array of vehicle data, or an error response.
// GetAllVehicles returns all vehicles.
// @Summary Get all vehicles
// @Description Returns all vehicles. Requires Authorization header.
// @Tags Vehicles
// @Accept json
// @Produce json
// @Success 200 {array} models.VehicleDetails
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/vehicles [get]
func GetAllVehicles(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session and get user information
		session, userName, ok := validateSession(c, db)
		if !ok {
			return
		}

		// Query all vehicles from the database
		rows, err := db.Query(selectAllVehiclesSQL)
		if err != nil {
			respondWithVehicleInternalError(c, "Failed to fetch vehicles", err)
			return
		}
		defer rows.Close()

		// Scan all rows into a slice of vehicles
		var vehicles []models.VehicleDetails
		for rows.Next() {
			var vehicle models.VehicleDetails
			if err := scanVehicle(rows, &vehicle); err != nil {
				respondWithVehicleInternalError(c, "Failed to parse vehicle data", err)
				return
			}
			vehicles = append(vehicles, vehicle)
		}

		// Check for errors during row iteration
		if err := rows.Err(); err != nil {
			respondWithVehicleInternalError(c, "Failed to process vehicles", err)
			return
		}

		// Return success response with all vehicles
		c.JSON(http.StatusOK, vehicles)

		// Log the activity
		log := createActivityLog(
			eventContextVehicles,
			eventNameGet,
			"GET All Vehicles",
			userName,
			session,
		)
		logActivitySafely(c, db, log)
	}
}

// GetVehicleByID handles GET /vehicles/:id requests.
// It retrieves a single vehicle by its ID.
// Requires: Authorization header with valid session ID
// Returns: 200 OK with vehicle data, 404 Not Found if vehicle doesn't exist, or an error response.
// GetVehicleByID returns a single vehicle by ID.
// @Summary Get vehicle by ID
// @Description Returns one vehicle by id. Requires Authorization header.
// @Tags Vehicles
// @Accept json
// @Produce json
// @Param id path int true "Vehicle ID"
// @Success 200 {object} models.VehicleDetails
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/vehicles/{id} [get]
func GetVehicleByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session and get user information
		session, userName, ok := validateSession(c, db)
		if !ok {
			return
		}

		// Get vehicle ID from URL parameter
		id := c.Param("id")
		var vehicle models.VehicleDetails

		// Query the database for the vehicle
		err := db.QueryRow(selectVehicleByIDSQL, id).Scan(
			&vehicle.ID,
			&vehicle.VehicleNumber,
			&vehicle.Status,
			&vehicle.CreatedAt,
			&vehicle.UpdatedAt,
			&vehicle.DriverName,
			&vehicle.TruckType,
			&vehicle.DriverContactNo,
			&vehicle.TransporterID,
			&vehicle.Capacity,
		)

		// Handle not found error
		if err == sql.ErrNoRows {
			respondWithVehicleError(c, http.StatusNotFound, errVehicleNotFound)
			return
		}
		// Handle other database errors
		if err != nil {
			respondWithVehicleInternalError(c, "Failed to retrieve vehicle", err)
			return
		}

		// Return success response with vehicle data
		c.JSON(http.StatusOK, vehicle)

		// Log the activity
		log := createActivityLog(
			eventContextVehicle,
			eventNameGet,
			fmt.Sprintf("GET Vehicle %s", vehicle.VehicleNumber),
			userName,
			session,
		)
		logActivitySafely(c, db, log)
	}
}

// UpdateVehicleDetails handles PUT /vehicles/:id requests.
// It updates an existing vehicle record in the database.
// Requires: Authorization header with valid session ID
// Expected request body: VehicleDetails model with fields to update
// Returns: 200 OK on successful update, 404 Not Found if vehicle doesn't exist, or an error response.
// UpdateVehicleDetails updates a vehicle by ID.
// @Summary Update vehicle
// @Description Updates vehicle by id. Body: vehicle_number, status, driver_name, truck_type, etc. Requires Authorization header.
// @Tags Vehicles
// @Accept json
// @Produce json
// @Param id path int true "Vehicle ID"
// @Param body body models.VehicleDetails true "Vehicle data"
// @Success 200 {object} models.VehicleDetails
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/vehicles/{id} [put]
func UpdateVehicleDetails(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session and get user information
		session, userName, ok := validateSession(c, db)
		if !ok {
			return
		}

		// Get vehicle ID from URL parameter
		id := c.Param("id")
		var vehicle models.VehicleDetails

		// Parse and validate the request body
		if err := c.ShouldBindJSON(&vehicle); err != nil {
			respondWithVehicleErrorDetails(c, http.StatusBadRequest, errInvalidRequestBody, err)
			return
		}

		// Update the vehicle in the database
		result, err := db.Exec(
			updateVehicleSQL,
			vehicle.VehicleNumber,
			vehicle.Status,
			vehicle.DriverName,
			vehicle.TruckType,
			vehicle.DriverContactNo,
			vehicle.TransporterID,
			vehicle.Capacity,
			id,
		)

		if err != nil {
			respondWithVehicleInternalError(c, "Failed to update vehicle record", err)
			return
		}

		// Check if any rows were affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			respondWithVehicleInternalError(c, "Failed to get update result", err)
			return
		}

		// Handle case where vehicle was not found
		if rowsAffected == 0 {
			respondWithVehicleError(c, http.StatusNotFound, errVehicleNotFound)
			return
		}

		// Return success response
		c.JSON(http.StatusOK, gin.H{"message": "Vehicle updated successfully"})

		// Log the activity
		log := createActivityLog(
			eventContextVehicle,
			eventNameUpdate,
			fmt.Sprintf("Update Vehicles details of vehicle number %s", vehicle.VehicleNumber),
			userName,
			session,
		)
		logActivitySafely(c, db, log)
	}
}

// DeleteVehicleDetails handles DELETE /vehicles/:id requests.
// It deletes a vehicle record from the database by ID.
// Requires: Authorization header with valid session ID
// Returns: 200 OK on successful deletion, 404 Not Found if vehicle doesn't exist, or an error response.
// DeleteVehicleDetails deletes a vehicle by ID.
// @Summary Delete vehicle
// @Description Deletes vehicle by id. Requires Authorization header.
// @Tags Vehicles
// @Accept json
// @Produce json
// @Param id path int true "Vehicle ID"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/vehicles/{id} [delete]
func DeleteVehicleDetails(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session and get user information
		session, userName, ok := validateSession(c, db)
		if !ok {
			return
		}

		// Get vehicle ID from URL parameter
		id := c.Param("id")

		// Execute the delete operation
		result, err := db.Exec(deleteVehicleSQL, id)
		if err != nil {
			respondWithVehicleInternalError(c, "Failed to delete vehicle record", err)
			return
		}

		// Check if any rows were affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			respondWithVehicleInternalError(c, "Failed to get delete result", err)
			return
		}

		// Handle case where vehicle was not found
		if rowsAffected == 0 {
			respondWithVehicleError(c, http.StatusNotFound, errVehicleNotFound)
			return
		}

		// Retrieve vehicle number for logging (ignore errors if query fails)
		var vehicleNumber string
		_ = db.QueryRow(selectVehicleNumberSQL, id).Scan(&vehicleNumber)

		// Return success response
		c.JSON(http.StatusOK, gin.H{"message": "Vehicle deleted successfully"})

		// Log the activity
		log := createActivityLog(
			eventContextVehicle,
			eventNameDelete,
			fmt.Sprintf("Delete Vehicle Details of %s", vehicleNumber),
			userName,
			session,
		)
		logActivitySafely(c, db, log)
	}
}

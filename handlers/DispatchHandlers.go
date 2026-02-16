// Package handlers provides HTTP handlers for dispatch order management operations.
// This package handles dispatch-related operations including:
//   - Creating and saving dispatch orders with items and details
//   - Retrieving dispatch orders by project ID or all orders
//   - Updating and deleting dispatch orders
//   - Generating PDF documents for dispatch orders
//   - Receiving dispatch orders at erection site
//   - Updating dispatch status to in-transit
//   - Tracking dispatch order logs

package handlers

import (
	"backend/models"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"github.com/lib/pq"
)

// Dispatch status constants
const (
	StatusDispatched  = "Dispatched"
	StatusAccepted    = "Accepted"
	StatusInTransit   = "In Transit"
	StatusReceived    = "Received"
	LocationStockyard = "Stockyard"
	LocationTruck     = "Truck"
)

// Dispatch log remarks constants
const (
	DispatchLogRemarks          = "Element dispatched from stockyard"
	DispatchLogRemarksInTransit = "Items loaded in truck by %s"
	DispatchLogRemarksReceived  = "Dispatch order received: %s for project: %s"
)

// Notification action URL template
const (
	NotificationActionTemplate = "https://precastezy.blueinvent.com/project/%d/dispatchlog"
)

// generateRandomOrderNumber generates a unique random order number for dispatch orders.
// The format is "ORD" followed by a random 6-digit number.
//
// Returns:
//   - A string containing the generated order number (e.g., "ORD123456")
func generateRandomOrderNumber() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("ORD%d", r.Intn(1000000))
}

// validateAndGetSession validates the session from the Authorization header and retrieves session information.
// It performs the following validations:
//  1. Checks if Authorization header exists
//  2. Validates session using GetSessionDetails
//
// Parameters:
//   - c: Gin context containing the HTTP request
//   - db: Database connection
//
// Returns:
//   - session: Validated session object
//   - userName: Username associated with the session
//   - err: Error if validation fails at any step
func validateAndGetSession(c *gin.Context, db *sql.DB) (session models.Session, userName string, err error) {
	sessionID := c.GetHeader("Authorization")
	if sessionID == "" {
		return session, "", fmt.Errorf("session_id header is missing")
	}

	session, userName, err = GetSessionDetails(db, sessionID)
	if err != nil {
		return session, "", fmt.Errorf("invalid session: %w", err)
	}

	return session, userName, nil
}

// getUserIDFromSession retrieves the user ID from a valid session.
// This helper function abstracts session validation and user ID retrieval.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - db: Database connection
//   - sessionID: Session identifier from Authorization header
//
// Returns:
//   - userID: User ID associated with the session
//   - err: Error if session is invalid, expired, or database query fails
func getUserIDFromSession(ctx context.Context, db *sql.DB, sessionID string) (int, error) {
	if sessionID == "" {
		return 0, errors.New("authorization token is required")
	}
	var userID int
	err := db.QueryRowContext(ctx, "SELECT user_id FROM session WHERE session_id = $1 AND expires_at > NOW()", sessionID).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, errors.New("invalid or expired session")
		}
		return 0, fmt.Errorf("database error validating session: %w", err)
	}
	return userID, nil
}

// getUserDetailsFromSession retrieves user ID and full name from session.
// This function joins the users and session tables to get complete user information.
//
// Parameters:
//   - db: Database connection
//   - sessionID: Session identifier from Authorization header
//
// Returns:
//   - userID: User ID associated with the session
//   - userName: Full name of the user (first_name + last_name)
//   - err: Error if session is invalid or database query fails
func getUserDetailsFromSession(db *sql.DB, sessionID string) (userID int, userName string, err error) {
	err = db.QueryRow(`
		SELECT u.id,
		       u.first_name || ' ' || u.last_name AS full_name
		FROM users u
		JOIN session s ON u.id = s.user_id
		WHERE s.session_id = $1`, sessionID).Scan(&userID, &userName)
	if err != nil {
		return 0, "", fmt.Errorf("invalid session: %w", err)
	}
	return userID, userName, nil
}

// getProjectName retrieves the project name by project ID.
// If the query fails, it returns a default formatted project name.
//
// Parameters:
//   - db: Database connection
//   - projectID: Project ID to fetch name for
//
// Returns:
//   - projectName: Project name or default formatted name if not found
func getProjectName(db *sql.DB, projectID int) string {
	var projectName string
	err := db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
	if err != nil {
		log.Printf("Failed to fetch project name: %v", err)
		return fmt.Sprintf("Project %d", projectID)
	}
	return projectName
}

// sendUserNotification creates and sends a notification to a specific user.
// The notification is inserted into the notifications table with unread status.
//
// Parameters:
//   - db: Database connection
//   - userID: ID of the user to send notification to
//   - message: Notification message content
//   - action: URL action for the notification
func sendUserNotification(db *sql.DB, userID int, message, action string) {
	notif := models.Notification{
		UserID:    userID,
		Message:   message,
		Status:    "unread",
		Action:    action,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := db.Exec(`
		INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

	if err != nil {
		log.Printf("Failed to insert notification: %v", err)
	}
}

// logDispatchActivity logs user activity to the activity log table for dispatch operations.
// This function is used to track user actions related to dispatch orders.
//
// Parameters:
//   - db: Database connection
//   - session: User session information
//   - userName: Username performing the action
//   - projectID: Project ID associated with the action (0 if not applicable)
//   - eventName: Type of event (Create, Update, Delete, Get, Generate, Receive)
//   - description: Description of the activity being logged
func logDispatchActivity(db *sql.DB, session models.Session, userName string, projectID int, eventName, description string) {
	activityLog := models.ActivityLog{
		EventContext: "Dispatch",
		EventName:    eventName,
		Description:  description,
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    projectID,
	}

	if err := SaveActivityLog(db, activityLog); err != nil {
		log.Printf("Failed to save activity log: %v", err)
	}
}

// updatePrecastStockForDispatch updates precast stock records to mark them as dispatched.
// It updates the dispatch_status to true and sets dispatch_start timestamp for items
// that are available (dispatch_status = false, stockyard = true) in the specified project.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - projectID: Project ID to filter stock items
//   - elementIDs: Array of element IDs to update
//   - timestamp: Timestamp to set for dispatch_start
//
// Returns:
//   - successfullyUpdated: Map of element IDs that were successfully updated
//   - err: Error if database query fails
func updatePrecastStockForDispatch(ctx context.Context, tx *sql.Tx, projectID int, elementIDs []int, timestamp time.Time) (map[int]bool, error) {
	updateStockQuery := `
		UPDATE precast_stock
		SET dispatch_status = true, dispatch_start = $1
		WHERE project_id = $2
		  AND dispatch_status = false
		  AND stockyard = true
		  AND element_id = ANY($3)
		RETURNING element_id;`

	rows, err := tx.QueryContext(ctx, updateStockQuery, timestamp, projectID, pq.Array(elementIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to update precast stock: %w", err)
	}
	defer rows.Close()

	successfullyUpdated := make(map[int]bool)
	for rows.Next() {
		var itemID int
		if err := rows.Scan(&itemID); err != nil {
			return nil, fmt.Errorf("failed to scan updated item ID: %w", err)
		}
		successfullyUpdated[itemID] = true
	}

	return successfullyUpdated, nil
}

// validateItemsAvailability checks if all requested items are available for dispatch.
// It compares the successfully updated items with the requested items and returns
// an error with unavailable element IDs if any items are missing.
//
// Parameters:
//   - requestedItems: Array of element IDs that were requested for dispatch
//   - successfullyUpdated: Map of element IDs that were successfully updated
//
// Returns:
//   - err: ErrItemsUnavailable error if any items are not available, nil otherwise
func validateItemsAvailability(requestedItems []int, successfullyUpdated map[int]bool) error {
	if len(successfullyUpdated) != len(requestedItems) {
		var unavailableElements []int
		for _, itemID := range requestedItems {
			if !successfullyUpdated[itemID] {
				unavailableElements = append(unavailableElements, itemID)
			}
		}
		return &models.ErrItemsUnavailable{ItemIDs: unavailableElements}
	}
	return nil
}

// insertDispatchOrder creates a new dispatch order record in the database.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - orderNumber: Unique order number for the dispatch order
//   - projectID: Project ID associated with the dispatch order
//   - userID: User ID who created the order
//   - timestamp: Timestamp for dispatch_date, created_at, and updated_at
//
// Returns:
//   - orderID: ID of the newly created dispatch order
//   - err: Error if database insert fails
func insertDispatchOrder(ctx context.Context, tx *sql.Tx, orderNumber string, projectID, userID int, timestamp time.Time) (int, error) {
	var orderID int
	err := tx.QueryRowContext(ctx, `
		INSERT INTO dispatch_orders (order_number, project_id, dispatch_date, created_at, updated_at, recieve_by)
		VALUES ($1, $2, $3, $3, $3, $4) RETURNING id`,
		orderNumber, projectID, timestamp, userID).Scan(&orderID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert dispatch order: %w", err)
	}
	return orderID, nil
}

// insertDispatchDetails creates a new dispatch details record in the database.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - orderID: ID of the dispatch order
//   - vehicleID: ID of the vehicle assigned to the dispatch
//   - driverName: Name of the driver
//   - timestamp: Timestamp for departure_time, created_at, and updated_at
//
// Returns:
//   - dispatchID: ID of the newly created dispatch details record
//   - err: Error if database insert fails
func insertDispatchDetails(ctx context.Context, tx *sql.Tx, orderID, vehicleID int, driverName string, timestamp time.Time) (int, error) {
	var dispatchID int
	err := tx.QueryRowContext(ctx, `
		INSERT INTO dispatch_details (dispatch_order_id, vehicle_id, driver_name, departure_time, current_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $4, $4) RETURNING id`,
		orderID, vehicleID, driverName, timestamp, StatusDispatched).Scan(&dispatchID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert dispatch details: %w", err)
	}
	return dispatchID, nil
}

// insertDispatchOrderItems bulk inserts dispatch order items into the database.
// This function uses a prepared statement for better performance when inserting multiple items.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - orderID: ID of the dispatch order
//   - elementIDs: Array of element IDs to add to the dispatch order
//   - timestamp: Timestamp for created_at and updated_at
//
// Returns:
//   - err: Error if database insert fails for any item
func insertDispatchOrderItems(ctx context.Context, tx *sql.Tx, orderID int, elementIDs []int, timestamp time.Time) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO dispatch_order_items (dispatch_order_id, element_id, created_at, updated_at)
		VALUES ($1, $2, $3, $3)`)
	if err != nil {
		return fmt.Errorf("failed to prepare item insert statement: %w", err)
	}
	defer stmt.Close()

	for _, itemID := range elementIDs {
		if _, err := stmt.ExecContext(ctx, orderID, itemID, timestamp); err != nil {
			return fmt.Errorf("failed to insert order item %d: %w", itemID, err)
		}
	}
	return nil
}

// insertDispatchTrackingLog creates a new tracking log entry for a dispatch order.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - orderNumber: Order number for the tracking log
//   - status: Status of the dispatch (e.g., "Dispatched", "In Transit")
//   - location: Location of the dispatch (e.g., "Stockyard", "Truck")
//   - remarks: Remarks for the tracking log entry
//   - timestamp: Timestamp for status_timestamp, created_at, and updated_at
//
// Returns:
//   - err: Error if database insert fails
func insertDispatchTrackingLog(ctx context.Context, tx *sql.Tx, orderNumber, status, location, remarks string, timestamp time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO dispatch_tracking_logs (order_number, status, location, remarks, status_timestamp, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5, $5)`,
		orderNumber, status, location, remarks, timestamp)
	if err != nil {
		return fmt.Errorf("failed to insert tracking log: %w", err)
	}
	return nil
}

// findVehicleByNumber retrieves a vehicle ID by vehicle number from the database.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - vehicleNumber: Vehicle number to search for
//
// Returns:
//   - vehicleID: ID of the vehicle if found, 0 if not found
//   - err: Error if database query fails
func findVehicleByNumber(ctx context.Context, tx *sql.Tx, vehicleNumber string) (int, error) {
	var vehicleID int
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM vehicle_details WHERE vehicle_number = $1`, vehicleNumber).Scan(&vehicleID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // Vehicle not found, return 0 without error
		}
		return 0, fmt.Errorf("failed to find vehicle: %w", err)
	}
	return vehicleID, nil
}

// createOrUpdateVehicle creates a new vehicle or updates an existing one based on vehicle number.
// If a vehicle with the given vehicle_number exists, it updates it; otherwise, it creates a new one.
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - tx: Database transaction
//   - vehicleNumber: Vehicle number (unique identifier)
//   - driverName: Name of the driver
//   - driverPhoneNo: Driver's phone number
//   - emergencyContactPhoneNo: Emergency contact phone number
//   - capacity: Vehicle capacity
//   - transporterID: ID of the transporter
//   - truckType: Type of truck
//   - timestamp: Timestamp for created_at and updated_at
//
// Returns:
//   - vehicleID: ID of the created or updated vehicle
//   - err: Error if database operation fails
func createOrUpdateVehicle(ctx context.Context, tx *sql.Tx, vehicleNumber, driverName, driverPhoneNo, emergencyContactPhoneNo string, capacity, transporterID int, truckType string, timestamp time.Time) (int, error) {
	// First, try to find existing vehicle
	vehicleID, err := findVehicleByNumber(ctx, tx, vehicleNumber)
	if err != nil {
		return 0, err
	}

	// Convert capacity to string for database (as per VehicleDetails model)
	capacityStr := fmt.Sprintf("%d", capacity)

	if vehicleID > 0 {
		// Vehicle exists, update it
		// Note: If emergency_contact_phone_no column doesn't exist in vehicle_details table,
		// you may need to add it to the table schema first
		_, err = tx.ExecContext(ctx, `
			UPDATE vehicle_details 
			SET driver_name = $1, 
				driver_contact_no = $2, 
				emergency_contact_phone_no = $3,
				capacity = $4, 
				transporter_id = $5, 
				truck_type = $6, 
				status = 'active',
				updated_at = $7
			WHERE id = $8`,
			driverName, driverPhoneNo, emergencyContactPhoneNo, capacityStr, transporterID, truckType, timestamp, vehicleID)
		if err != nil {
			return 0, fmt.Errorf("failed to update vehicle: %w", err)
		}
		return vehicleID, nil
	}

	// Vehicle doesn't exist, create new one
	err = tx.QueryRowContext(ctx, `
		INSERT INTO vehicle_details (vehicle_number, status, driver_name, truck_type, driver_contact_no, emergency_contact_phone_no, transporter_id, capacity, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9) 
		RETURNING id`,
		vehicleNumber, "active", driverName, truckType, driverPhoneNo, emergencyContactPhoneNo, transporterID, capacityStr, timestamp).Scan(&vehicleID)
	if err != nil {
		return 0, fmt.Errorf("failed to create vehicle: %w", err)
	}

	return vehicleID, nil
}

// createDispatchTransaction handles all database operations for creating a dispatch order within a single atomic transaction.
// This function ensures data consistency by performing all related database operations in one transaction.
// It performs the following operations:
//  1. Creates or updates vehicle details based on vehicle_number
//  2. Updates precast stock to mark items as dispatched
//  3. Validates that all requested items are available
//  4. Inserts dispatch order record
//  5. Inserts dispatch details record
//  6. Inserts dispatch order items
//  7. Inserts tracking log entry
//  8. Commits the transaction
//
// Parameters:
//   - ctx: Request context for timeout and cancellation propagation
//   - db: Database connection
//   - req: Dispatch order request containing project ID, vehicle details, driver info, and items
//   - userID: User ID who is creating the dispatch order
//
// Returns:
//   - response: Success response containing order details
//   - err: Error if any step fails, including ErrItemsUnavailable if items are not available
func createDispatchTransaction(ctx context.Context, db *sql.DB, req *models.DispatchOrderRequest, userID int) (gin.H, error) {
	// Start a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Defer a rollback. If the transaction is successfully committed, this will be a no-op.
	defer tx.Rollback()

	// Use a single timestamp for all operations in this transaction for consistency
	now := time.Now()
	orderNumber := generateRandomOrderNumber()

	// Determine vehicle ID - use vehicle_id if provided (backward compatibility), otherwise create/update by vehicle_number
	var vehicleID int
	if req.VehicleId > 0 {
		// Legacy mode: use provided vehicle_id
		vehicleID = req.VehicleId
		log.Printf("Creating dispatch order - ProjectID: %d, VehicleID: %d, DriverName: %s, Items: %v",
			req.ProjectID, req.VehicleId, req.DriverName, req.Items)
	} else {
		// New mode: create or update vehicle by vehicle_number
		vehicleID, err = createOrUpdateVehicle(ctx, tx, req.VehicleNumber, req.DriverName, req.DriverPhoneNo,
			req.EmergencyContactPhoneNo, req.Capacity, req.TransporterID, req.TruckType, now)
		if err != nil {
			log.Printf("Error creating/updating vehicle: %v", err)
			return nil, fmt.Errorf("failed to create or update vehicle: %w", err)
		}
		log.Printf("Creating dispatch order - ProjectID: %d, VehicleNumber: %s, VehicleID: %d, DriverName: %s, Items: %v",
			req.ProjectID, req.VehicleNumber, vehicleID, req.DriverName, req.Items)
	}

	// Step 1: Update precast stock and validate availability
	successfullyUpdated, err := updatePrecastStockForDispatch(ctx, tx, req.ProjectID, req.Items, now)
	if err != nil {
		log.Printf("Error updating precast stock: %v", err)
		return nil, err
	}

	// Step 2: Validate that all requested items are available
	if err := validateItemsAvailability(req.Items, successfullyUpdated); err != nil {
		return nil, err
	}

	// Step 3: Insert the parent order record
	orderID, err := insertDispatchOrder(ctx, tx, orderNumber, req.ProjectID, userID, now)
	if err != nil {
		log.Printf("Error inserting dispatch order: %v", err)
		return nil, err
	}

	// Step 4: Insert dispatch details record
	dispatchID, err := insertDispatchDetails(ctx, tx, orderID, vehicleID, req.DriverName, now)
	if err != nil {
		log.Printf("Error inserting dispatch details: %v", err)
		return nil, err
	}

	// Step 5: Bulk insert order items
	if err := insertDispatchOrderItems(ctx, tx, orderID, req.Items, now); err != nil {
		return nil, err
	}

	// Step 6: Insert tracking log entry
	if err := insertDispatchTrackingLog(ctx, tx, orderNumber, StatusDispatched, LocationStockyard, DispatchLogRemarks, now); err != nil {
		return nil, err
	}

	// Step 7: Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Step 8: Prepare success response
	response := gin.H{
		"message":      "Order created successfully!",
		"dispatch_id":  dispatchID,
		"order_id":     orderID,
		"order_number": orderNumber,
		"project_id":   req.ProjectID,
		"vehicle_id":   vehicleID,
	}
	return response, nil
}

// CreateAndSaveDispatchOrder handles the creation of a dispatch order with items and details.
// This is the main HTTP handler that manages the request/response lifecycle.
// It performs the following operations:
//  1. Validates session and user authentication
//  2. Decodes and validates the request payload
//  3. Executes the dispatch creation transaction
//  4. Sends notifications to relevant users
//  5. Logs the activity
//
// The endpoint expects a JSON payload with:
//   - project_id: ID of the project
//   - vehicle_id: ID of the vehicle
//   - driver_name: Name of the driver
//   - items: Array of element IDs to dispatch
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// CreateAndSaveDispatchOrder godoc
// @Summary      Create dispatch order
// @Description  Create and save a new dispatch order with items and vehicle details
// @Tags         dispatch
// @Accept       json
// @Produce      json
// @Param        body  body      models.DispatchOrderRequest  true  "Dispatch order request"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/dispatch_order [post]
func CreateAndSaveDispatchOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Use request context for timeout and cancellation propagation
		ctx := c.Request.Context()

		// Decode and validate request
		var req models.DispatchOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("JSON Binding Error: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
			return
		}

		// Process nested vehicle_details if present
		req.AfterUnmarshal()

		// Validate required fields after processing nested structure
		if req.VehicleNumber == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "vehicle_number is required (can be provided directly or in vehicle_details)"})
			return
		}
		if req.Capacity == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "capacity is required (can be provided directly or in vehicle_details)"})
			return
		}
		if req.TransporterID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "transporter_id is required (can be provided directly or in vehicle_details)"})
			return
		}
		if req.TruckType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "truck_type is required (can be provided directly or in vehicle_details)"})
			return
		}
		if req.DriverName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "driver_name is required (can be provided directly or in vehicle_details)"})
			return
		}
		if req.DriverPhoneNo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "driver_phone_no is required (can be provided directly or in vehicle_details)"})
			return
		}

		// Authenticate user
		userID, err := getUserIDFromSession(ctx, db, c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		// Execute core business logic within a transaction
		resp, err := createDispatchTransaction(ctx, db, &req, userID)
		if err != nil {
			// Check for a specific, known error type for unavailable items
			var unavailableErr *models.ErrItemsUnavailable
			if errors.As(err, &unavailableErr) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":                "Some elements are not available for dispatch",
					"unavailable_elements": unavailableErr.ItemIDs,
				})
				return
			}
			// For all other errors, log the details and return a more specific error message
			log.Printf("Transaction failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "An internal error occurred while creating the dispatch order",
				"details": err.Error(),
			})
			return
		}

		// Success response
		c.JSON(http.StatusOK, resp)

		// Get project name for notification
		projectName := getProjectName(db, req.ProjectID)

		// Get order number from response
		orderNumber, _ := resp["order_number"].(string)
		if orderNumber == "" {
			orderNumber = "N/A"
		}

		// Send notification to the user who created the dispatch order
		sendUserNotification(db, userID,
			fmt.Sprintf("New dispatch order created: %s for project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, req.ProjectID))

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, req.ProjectID,
			fmt.Sprintf("New dispatch order created: %s for project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, req.ProjectID))

		// Log activity
		logDispatchActivity(db, session, userName, req.ProjectID, "Create", "Create and save dispatch order")
	}
}

// buildDispatchOrderQuery builds the SQL query for retrieving dispatch orders by project ID.
// The query joins dispatch_orders with dispatch_details and project tables to get complete order information.
//
// Returns:
//   - A complete SQL SELECT query string
func buildDispatchOrderQuery() string {
	return `
		SELECT 
			d_order.id,
			d_order.order_number, 
			d_order.project_id, 
			d_order.dispatch_date,
			dd.vehicle_id,
			dd.driver_name,
			dd.current_status,
			p.name AS project_name
		FROM dispatch_orders d_order
		LEFT JOIN (
			SELECT DISTINCT ON (dispatch_order_id)
				dispatch_order_id,
				vehicle_id,
				driver_name,
				current_status
			FROM dispatch_details
			ORDER BY dispatch_order_id, updated_at DESC
		) dd ON dd.dispatch_order_id::INTEGER = d_order.id
		LEFT JOIN project p ON d_order.project_id = p.project_id
		WHERE d_order.project_id = $1
		ORDER BY d_order.dispatch_date DESC`
}

// buildDispatchOrderItemsQuery builds the SQL query for retrieving dispatch order items.
// The query joins dispatch_order_items with precast_stock and element_type tables.
//
// Returns:
//   - A complete SQL SELECT query string
func buildDispatchOrderItemsQuery() string {
	return `
		SELECT 
			doi.element_id,
			ps.element_type,
			ps.weight,
			et.element_type_name
		FROM dispatch_order_items doi
		LEFT JOIN precast_stock ps ON doi.element_id = ps.element_id
		LEFT JOIN element_type et ON ps.element_type_id = et.element_type_id
		LEFT JOIN dispatch_orders d_order ON doi.dispatch_order_id = d_order.id
		WHERE d_order.order_number = $1`
}

// scanDispatchOrderRow scans a single row from the dispatch orders result set.
//
// Parameters:
//   - rows: Database result set
//
// Returns:
//   - order: Scanned dispatch order response
//   - err: Error if scanning fails
func scanDispatchOrderRow(rows *sql.Rows) (models.DispatchOrderResponse, error) {
	var order models.DispatchOrderResponse
	var projectName sql.NullString

	if err := rows.Scan(
		&order.ID,
		&order.OrderNumber,
		&order.ProjectID,
		&order.DispatchDate,
		&order.VehicleId,
		&order.DriverName,
		&order.CurrentStatus,
		&projectName,
	); err != nil {
		return models.DispatchOrderResponse{}, fmt.Errorf("error scanning orders: %w", err)
	}

	// Handle nullable fields
	order.ProjectName = projectName.String
	return order, nil
}

// scanDispatchItemRow scans a single row from the dispatch items result set.
//
// Parameters:
//   - rows: Database result set
//
// Returns:
//   - item: Scanned dispatch item response
//   - err: Error if scanning fails
func scanDispatchItemRow(rows *sql.Rows) (models.DispatchItemResponse, error) {
	var item models.DispatchItemResponse
	var elementType, elementTypeName sql.NullString
	var weight sql.NullFloat64

	if err := rows.Scan(
		&item.ElementID,
		&elementType,
		&weight,
		&elementTypeName,
	); err != nil {
		return models.DispatchItemResponse{}, fmt.Errorf("error scanning items: %w", err)
	}

	// Handle nullable fields
	if elementType.Valid {
		item.ElementType = elementType.String
	}
	if weight.Valid {
		item.Weight = weight.Float64
	}
	if elementTypeName.Valid {
		item.ElementTypeName = elementTypeName.String
	}

	return item, nil
}

// fetchDispatchOrderItems retrieves all items associated with a dispatch order.
//
// Parameters:
//   - db: Database connection
//   - orderNumber: Order number to fetch items for
//
// Returns:
//   - items: Array of dispatch item responses
//   - err: Error if database query fails
func fetchDispatchOrderItems(db *sql.DB, orderNumber string) ([]models.DispatchItemResponse, error) {
	itemRows, err := db.Query(buildDispatchOrderItemsQuery(), orderNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch order items: %w", err)
	}
	defer itemRows.Close()

	var items []models.DispatchItemResponse
	for itemRows.Next() {
		item, err := scanDispatchItemRow(itemRows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

// GetDispatchOrdersByProjectID retrieves all dispatch orders for a specific project.
// The endpoint returns dispatch orders with their associated items, vehicle details, and project information.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// GetDispatchOrdersByProjectID godoc
// @Summary      Get dispatch orders by project
// @Description  Get all dispatch orders for a project
// @Tags         dispatch
// @Accept       json
// @Produce      json
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {array}  models.DispatchOrderResponse
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Failure      500         {object}  models.ErrorResponse
// @Router       /api/dispatch_order/{project_id} [get]
func GetDispatchOrdersByProjectID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Convert project_id from string to int
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Fetch dispatch orders with details for the given project_id
		rows, err := db.Query(buildDispatchOrderQuery(), projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch orders"})
			return
		}
		defer rows.Close()

		// Store results in a slice (empty slice so JSON returns [] not null when no data)
		orders := []models.DispatchOrderResponse{}
		for rows.Next() {
			order, err := scanDispatchOrderRow(rows)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Fetch dispatch order items for each order
			items, err := fetchDispatchOrderItems(db, order.OrderNumber)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Assign items to the order
			order.Items = items
			orders = append(orders, order)
		}

		// Send response
		c.JSON(http.StatusOK, orders)

		// Log activity
		logDispatchActivity(db, session, userName, projectID, "Get", "Get dispatch orders by project ID")
	}
}

// UpdateDispatchOrder updates an existing dispatch order in the database.
// The endpoint allows updating order number, project ID, and dispatch date.
// After updating, it sends notifications to relevant users and logs the activity.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
func UpdateDispatchOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		orderID := c.Param("id")
		var order models.DispatchOrder
		if err := c.ShouldBindJSON(&order); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Update dispatch order
		_, err = db.Exec(`UPDATE dispatch_orders
			SET order_number=$1, project_id=$2, dispatch_date=$3, updated_at=NOW()
			WHERE id=$4`,
			order.OrderNumber, order.ProjectID, order.DispatchDate, orderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order"})
			return
		}

		// Fetch project ID
		var projectID int
		err = db.QueryRow(`SELECT project_id FROM dispatch_orders WHERE id=$1`, orderID).Scan(&projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project ID"})
			return
		}

		// Get project name and order number for notification
		var projectName string
		var orderNumber string
		err = db.QueryRow(`SELECT p.name, d.order_number FROM dispatch_orders d 
			JOIN project p ON d.project_id = p.project_id WHERE d.id=$1`, orderID).Scan(&projectName, &orderNumber)
		if err != nil {
			log.Printf("Failed to fetch project name or order number: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
			orderNumber = "N/A"
		}

		c.JSON(http.StatusOK, gin.H{"message": "Order updated successfully!"})

		// Fetch user_id from the session table
		userID, _, err := getUserDetailsFromSession(db, c.GetHeader("Authorization"))
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the dispatch order
			sendUserNotification(db, userID,
				fmt.Sprintf("Dispatch order updated: %s for project: %s", orderNumber, projectName),
				fmt.Sprintf(NotificationActionTemplate, projectID))
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Dispatch order updated: %s for project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, projectID))

		// Log activity
		logDispatchActivity(db, session, userName, projectID, "Update", "Update dispatch order with id "+orderID)
	}
}

// DeleteDispatchOrder deletes a dispatch order and all its associated items from the database.
// The deletion is performed within a transaction to ensure data consistency.
// After deletion, it sends notifications to relevant users and logs the activity.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
func DeleteDispatchOrder(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		orderID := c.Param("id")

		// Fetch order info before deletion for notifications
		var projectID int
		var orderNumber string
		err = db.QueryRow(`SELECT project_id, order_number FROM dispatch_orders WHERE id=$1`, orderID).Scan(&projectID, &orderNumber)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Dispatch order not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch order details"})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction error"})
			return
		}
		defer tx.Rollback()

		// Delete order items first (foreign key constraint)
		_, err = tx.Exec(`DELETE FROM dispatch_order_items WHERE dispatch_order_id = $1`, orderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete order items"})
			return
		}

		// Delete dispatch order
		_, err = tx.Exec(`DELETE FROM dispatch_orders WHERE id = $1`, orderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete order"})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed"})
			return
		}

		// Get project name for notification
		projectName := getProjectName(db, projectID)

		c.JSON(http.StatusOK, gin.H{"message": "Order deleted successfully!"})

		// Fetch user_id from the session table
		userID, _, err := getUserDetailsFromSession(db, c.GetHeader("Authorization"))
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the dispatch order
			sendUserNotification(db, userID,
				fmt.Sprintf("Dispatch order deleted: %s from project: %s", orderNumber, projectName),
				fmt.Sprintf(NotificationActionTemplate, projectID))
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Dispatch order deleted: %s from project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, projectID))

		// Log activity
		logDispatchActivity(db, session, userName, projectID, "Delete", "Delete dispatch order")
	}
}

// GetAllDispatchOrders retrieves all dispatch orders from the database.
// This endpoint returns a list of all dispatch orders regardless of project.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
func GetAllDispatchOrders(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Fetch all dispatch orders
		rows, err := db.Query(`SELECT id, order_number, project_id, dispatch_date FROM dispatch_orders`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch orders"})
			return
		}
		defer rows.Close()

		var orders []models.DispatchOrder
		for rows.Next() {
			var order models.DispatchOrder
			if err := rows.Scan(&order.ID, &order.OrderNumber, &order.ProjectID, &order.DispatchDate); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning orders"})
				return
			}
			orders = append(orders, order)
		}

		c.JSON(http.StatusOK, orders)

		// Log activity
		logDispatchActivity(db, session, userName, 0, "Get", "Get All dispatch order")
	}
}

// buildDispatchOrderPDFQuery builds the SQL query for retrieving dispatch order details for PDF generation.
// The query joins dispatch_orders with dispatch_details, vehicle_details, and project tables.
//
// Returns:
//   - A complete SQL SELECT query string
func buildDispatchOrderPDFQuery() string {
	return `
		SELECT 
			d.id,
			d.order_number,
			d.project_id,
			d.dispatch_date,
			dd.vehicle_id,
			dd.driver_name,
			dd.current_status,
			v.vehicle_number,
			COALESCE(v.truck_type, '') AS model,
			'' AS manufacturer,
			COALESCE(v.capacity::text, '') AS capacity,
			p.name
		FROM dispatch_orders d
		JOIN dispatch_details dd ON dd.dispatch_order_id = d.id::text
		JOIN vehicle_details v ON dd.vehicle_id = v.id
		JOIN project p ON d.project_id = p.project_id
		WHERE d.id = $1`
}

// buildDispatchItemsPDFQuery builds the SQL query for retrieving dispatch items for PDF generation.
//
// Returns:
//   - A complete SQL SELECT query string
func buildDispatchItemsPDFQuery() string {
	return `
		SELECT 
			doi.element_id,
			ps.element_type,
			ps.dimensions,
			ps.weight,
			et.element_type_name
		FROM dispatch_order_items doi
		JOIN precast_stock ps ON doi.element_id = ps.element_id
		LEFT JOIN element_type et ON ps.element_type_id = et.element_type_id
		WHERE doi.dispatch_order_id = $1`
}

// fetchDispatchOrderForPDF retrieves dispatch order details for PDF generation.
//
// Parameters:
//   - db: Database connection
//   - orderID: ID of the dispatch order
//
// Returns:
//   - order: Dispatch order PDF data
//   - err: Error if database query fails
func fetchDispatchOrderForPDF(db *sql.DB, orderID int) (models.DispatchOrderPDF, error) {
	var order models.DispatchOrderPDF

	err := db.QueryRow(buildDispatchOrderPDFQuery(), orderID).Scan(
		&order.ID,
		&order.OrderNumber,
		&order.ProjectID,
		&order.DispatchDate,
		&order.VehicleID,
		&order.DriverName,
		&order.CurrentStatus,
		&order.VehicleNumber,
		&order.Model,
		&order.Manufacturer,
		&order.Capacity,
		&order.ProjectName,
	)
	if err != nil {
		return models.DispatchOrderPDF{}, fmt.Errorf("failed to fetch dispatch order: %w", err)
	}

	return order, nil
}

// fetchDispatchItemsForPDF retrieves dispatch items for PDF generation.
//
// Parameters:
//   - db: Database connection
//   - orderID: ID of the dispatch order
//
// Returns:
//   - items: Array of dispatch items
//   - err: Error if database query fails
func fetchDispatchItemsForPDF(db *sql.DB, orderID int) ([]models.DispatchItem, error) {
	rows, err := db.Query(buildDispatchItemsPDFQuery(), orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch dispatch items: %w", err)
	}
	defer rows.Close()

	var items []models.DispatchItem
	for rows.Next() {
		var item models.DispatchItem
		if err := rows.Scan(
			&item.ElementID,
			&item.ElementType,
			&item.Dimensions,
			&item.Weight,
			&item.ElementTypeName,
		); err != nil {
			log.Printf("Error scanning item: %v", err)
			continue
		}
		items = append(items, item)
	}

	return items, nil
}

// generatePDFHeader creates the header section of the dispatch order PDF.
//
// Parameters:
//   - pdf: PDF document object
func generatePDFHeader(pdf *gofpdf.Fpdf) {
	pdf.SetFont("Arial", "B", 24)
	pdf.SetFillColor(240, 240, 240)
	pdf.Rect(10, 10, 190, 15, "F")
	pdf.SetXY(10, 12)
	pdf.Cell(190, 10, "Dispatch Order")
	pdf.Ln(20)
}

// generatePDFVehicleDetails creates the vehicle details section of the dispatch order PDF.
//
// Parameters:
//   - pdf: PDF document object
//   - order: Dispatch order data containing vehicle information
func generatePDFVehicleDetails(pdf *gofpdf.Fpdf, order models.DispatchOrderPDF) {
	// Left side - Vehicle details with border
	pdf.SetFont("Arial", "B", 14)
	pdf.SetFillColor(245, 245, 245)
	pdf.Rect(10, 30, 90, 10, "F")
	pdf.SetXY(10, 32)
	pdf.Cell(90, 8, "Vehicle Details")
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(10, pdf.GetY())
	pdf.Cell(45, 8, "Vehicle Number:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.VehicleNumber)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(10, pdf.GetY())
	pdf.Cell(45, 8, "Driver Name:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.DriverName)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(10, pdf.GetY())
	pdf.Cell(45, 8, "Vehicle Model:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.Model)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(10, pdf.GetY())
	pdf.Cell(45, 8, "Manufacturer:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.Manufacturer)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(10, pdf.GetY())
	pdf.Cell(45, 8, "Capacity:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.Capacity)
	pdf.Ln(8)
}

// generatePDFOrderDetails creates the order details section of the dispatch order PDF.
//
// Parameters:
//   - pdf: PDF document object
//   - order: Dispatch order data containing order information
func generatePDFOrderDetails(pdf *gofpdf.Fpdf, order models.DispatchOrderPDF) {
	// Right side - Order details with border
	pdf.SetFont("Arial", "B", 14)
	pdf.SetFillColor(245, 245, 245)
	pdf.Rect(105, 30, 90, 10, "F")
	pdf.SetXY(105, 32)
	pdf.Cell(90, 8, "Order Details")
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(105, pdf.GetY())
	pdf.Cell(45, 8, "Order Number:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.OrderNumber)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(105, pdf.GetY())
	pdf.Cell(45, 8, "Project Name:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.ProjectName)
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(105, pdf.GetY())
	pdf.Cell(45, 8, "Dispatch Date:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.DispatchDate.Format("2006-01-02 15:04:05"))
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	pdf.SetXY(105, pdf.GetY())
	pdf.Cell(45, 8, "Status:")
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(45, 8, order.CurrentStatus)
	pdf.Ln(8)
}

// generatePDFItemsTable creates the dispatch items table section of the dispatch order PDF.
//
// Parameters:
//   - pdf: PDF document object
//   - items: Array of dispatch items to display in the table
func generatePDFItemsTable(pdf *gofpdf.Fpdf, items []models.DispatchItem) {
	// Move back to left margin for items table
	pdf.SetY(85)

	// Dispatch items with border
	pdf.SetFont("Arial", "B", 14)
	pdf.SetFillColor(245, 245, 245)
	pdf.Rect(10, pdf.GetY(), 190, 10, "F")
	pdf.SetXY(10, pdf.GetY()+2)
	pdf.Cell(190, 8, "Dispatch Items")
	pdf.Ln(12)

	if len(items) == 0 {
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(190, 8, "No dispatch items found.")
		pdf.Ln(10)
	} else {
		// Table header with background
		pdf.SetFillColor(230, 230, 230)
		pdf.SetFont("Arial", "B", 10)
		pdf.Rect(10, pdf.GetY(), 190, 8, "F")
		pdf.SetXY(10, pdf.GetY())
		pdf.Cell(15, 8, "Check")
		pdf.Cell(40, 8, "Element ID")
		pdf.Cell(65, 8, "Element Type")
		pdf.Cell(70, 8, "Comments")
		pdf.Ln(8)

		pdf.SetFont("Arial", "", 10)
		var totalWeight float64
		for _, item := range items {
			// Draw checkbox with border
			pdf.Rect(12, pdf.GetY()+1, 5, 5, "D")
			pdf.Cell(15, 8, "")
			pdf.Cell(40, 8, fmt.Sprintf("%d", item.ElementID))
			pdf.Cell(65, 8, item.ElementTypeName)
			// Comment box
			pdf.Rect(120, pdf.GetY()+1, 70, 5, "D")
			pdf.Cell(70, 8, "")
			pdf.Ln(8)
			totalWeight += item.Weight
		}

		// Total weight with border
		pdf.Ln(5)
		pdf.SetFillColor(240, 240, 240)
		pdf.Rect(10, pdf.GetY(), 190, 10, "F")
		pdf.SetFont("Arial", "B", 12)
		pdf.SetXY(10, pdf.GetY()+2)
		pdf.Cell(190, 8, "Total Weight: "+fmt.Sprintf("%.2f kg", totalWeight))
		pdf.Ln(15)

		// Signature boxes with borders
		pdf.SetFont("Arial", "", 10)
		signatureY := pdf.GetY()

		// First signature box
		pdf.Rect(20, signatureY, 80, 25, "D")
		pdf.SetXY(20, signatureY+5)
		pdf.Cell(80, 8, "Dispatcher Signature")
		pdf.SetXY(20, signatureY+15)
		pdf.Cell(80, 8, "Name: _________________")

		// Second signature box
		pdf.Rect(110, signatureY, 80, 25, "D")
		pdf.SetXY(110, signatureY+5)
		pdf.Cell(80, 8, "Receiver Signature")
		pdf.SetXY(110, signatureY+15)
		pdf.Cell(80, 8, "Name: _________________")
	}
}

// generatePDFFooter creates the footer section of the dispatch order PDF.
//
// Parameters:
//   - pdf: PDF document object
func generatePDFFooter(pdf *gofpdf.Fpdf) {
	// Footer
	pdf.SetY(-45)
	footerY := pdf.GetY()

	// Footer content
	pdf.SetFont("Arial", "I", 8)

	// First row: Generated on
	pdf.SetXY(10, footerY+4)
	pdf.Cell(190, 6, "Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

	// Second row: Page number
	pdf.SetXY(10, footerY+8)
	pdf.Cell(190, 6, "Page: "+fmt.Sprintf("%d", pdf.PageNo()))

	// Third row: notice text
	pdf.SetXY(10, footerY+12)
	pdf.Cell(190, 6, "This is a computer-generated document. No signature is required.")
}

// GenerateDispatchPDF generates a PDF document for a dispatch order.
// The PDF includes order details, vehicle information, dispatch items, and signature sections.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function that returns a PDF file
//
// GenerateDispatchPDF godoc
// @Summary      Generate dispatch order PDF
// @Description  Generate PDF for a dispatch order
// @Tags         dispatch
// @Produce      application/pdf
// @Param        order_id  path      int  true  "Dispatch order ID"
// @Success      200       {file}    file "PDF file"
// @Failure      400       {object}  models.ErrorResponse
// @Failure      401       {object}  models.ErrorResponse
// @Failure      500       {object}  models.ErrorResponse
// @Router       /api/dispatch_order/pdf/{order_id} [get]
func GenerateDispatchPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Parse and validate order_id parameter
		orderId := c.Param("order_id")
		if orderId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order_id is required"})
			return
		}

		orderID, err := strconv.Atoi(orderId)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
			return
		}

		// Fetch dispatch order details
		order, err := fetchDispatchOrderForPDF(db, orderID)
		if err != nil {
			log.Printf("Error fetching dispatch order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":       "Failed to fetch dispatch order",
				"details":     err.Error(),
				"suggestion":  "Please check if the dispatch order ID is valid or contact support.",
				"error_stage": "Query: Fetch dispatch order",
			})
			return
		}

		// Fetch dispatch items
		items, err := fetchDispatchItemsForPDF(db, orderID)
		if err != nil {
			log.Printf("Error fetching dispatch items: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch dispatch items"})
			return
		}

		// Create PDF
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()

		// Set margins
		pdf.SetMargins(10, 10, 10)

		// Generate PDF sections
		generatePDFHeader(pdf)
		generatePDFVehicleDetails(pdf, order)
		generatePDFOrderDetails(pdf, order)
		generatePDFItemsTable(pdf, items)
		generatePDFFooter(pdf)

		// Output PDF
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=dispatch_order_%s.pdf", order.OrderNumber))
		err = pdf.Output(c.Writer)
		if err != nil {
			log.Printf("Error generating PDF: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}

		// Log activity
		logDispatchActivity(db, session, userName, order.ProjectID, "Generate", "Generate dispatch PDF for order ID "+orderId)
	}
}

// fetchElementIDsFromDispatchOrder retrieves all element IDs associated with a dispatch order.
//
// Parameters:
//   - tx: Database transaction
//   - orderID: ID of the dispatch order
//
// Returns:
//   - elementIDs: Array of element IDs
//   - err: Error if database query fails
func fetchElementIDsFromDispatchOrder(tx *sql.Tx, orderID string) ([]int, error) {
	rows, err := tx.Query(`
		SELECT element_id 
		FROM dispatch_order_items 
		WHERE dispatch_order_id = $1`, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch dispatch items: %w", err)
	}
	defer rows.Close()

	var elementIDs []int
	for rows.Next() {
		var elementID int
		if err := rows.Scan(&elementID); err != nil {
			return nil, fmt.Errorf("error scanning element ID: %w", err)
		}
		elementIDs = append(elementIDs, elementID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return elementIDs, nil
}

// updatePrecastStockForReceipt updates precast stock records when a dispatch order is received.
//
// Parameters:
//   - tx: Database transaction
//   - elementIDs: Array of element IDs to update
//
// Returns:
//   - err: Error if database update fails
func updatePrecastStockForReceipt(tx *sql.Tx, elementIDs []int) error {
	_, err := tx.Exec(`
		UPDATE precast_stock 
		SET recieve_in_erection = true,
			updated_at = NOW(),
			dispatch_end = NOW()
		WHERE element_id = ANY($1)`,
		pq.Array(elementIDs))
	if err != nil {
		return fmt.Errorf("bulk update failed for precast_stock: %w", err)
	}
	return nil
}

// updateStockErectedForReceipt updates stock_erected records when a dispatch order is received.
//
// Parameters:
//   - tx: Database transaction
//   - elementIDs: Array of element IDs to update
//
// Returns:
//   - err: Error if database update fails
func updateStockErectedForReceipt(tx *sql.Tx, elementIDs []int) error {
	_, err := tx.Exec(`
		UPDATE stock_erected 
		SET recieve_in_erection = true,
			action_approve_or_reject = NOW()
		WHERE element_id = ANY($1)`,
		pq.Array(elementIDs))
	if err != nil {
		return fmt.Errorf("bulk update failed for stock_erected: %w", err)
	}
	return nil
}

// updateStockErectedLogsForReceipt updates stock_erected_logs records when a dispatch order is received.
//
// Parameters:
//   - tx: Database transaction
//   - elementIDs: Array of element IDs to update
//
// Returns:
//   - err: Error if database update fails
func updateStockErectedLogsForReceipt(tx *sql.Tx, elementIDs []int) error {
	_, err := tx.Exec(`
		UPDATE stock_erected_logs 
		SET status = 'Received',
			action_timestamp = NOW()
		WHERE element_id = ANY($1) 
		AND stock_erected_id IN (
			SELECT id FROM stock_erected WHERE element_id = ANY($1)
		)`,
		pq.Array(elementIDs))
	if err != nil {
		return fmt.Errorf("bulk update failed for stock_erected_logs: %w", err)
	}
	return nil
}

// updateElementsForReceipt updates element status when a dispatch order is received.
//
// Parameters:
//   - tx: Database transaction
//   - elementIDs: Array of element IDs to update
//
// Returns:
//   - err: Error if database update fails
func updateElementsForReceipt(tx *sql.Tx, elementIDs []int) error {
	_, err := tx.Exec(`
		UPDATE element
		SET status = 'In Erection'
		WHERE id = ANY($1)`,
		pq.Array(elementIDs))
	if err != nil {
		return fmt.Errorf("failed to update elements: %w", err)
	}
	return nil
}

// ReceiveDispatchOrderByErection handles the receipt of a dispatch order at the erection site.
// This endpoint updates multiple database tables to reflect that the dispatch order has been received:
//   - Updates dispatch_orders and dispatch_details status to "Accepted"
//   - Updates precast_stock to mark items as received
//   - Updates stock_erected and stock_erected_logs
//   - Updates element status to "In Erection"
//   - Sends notifications to relevant users
//   - Logs the activity
//
// All database operations are performed within a single transaction for data consistency.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// ReceiveDispatchOrderByErection godoc
// @Summary      Receive dispatch order at erection
// @Description  Mark dispatch order as received at erection site
// @Tags         dispatch
// @Accept       json
// @Produce      json
// @Param        order_id  path      string  true  "Dispatch order ID"
// @Success      200       {object}  object
// @Failure      400       {object}  models.ErrorResponse
// @Failure      401       {object}  models.ErrorResponse
// @Failure      500       {object}  models.ErrorResponse
// @Router       /api/dispatch_order/{order_id}/receive [post]
func ReceiveDispatchOrderByErection(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userNamee, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Parse and validate order_id parameter
		orderID := c.Param("order_id")
		if orderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order_id is required"})
			return
		}

		// Fetch user details from session
		userID, userName, err := getUserDetailsFromSession(db, c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction Start Error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Ensure rollback if commit doesn't succeed
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		// Update dispatch order status
		_, err = tx.Exec(`
			UPDATE dispatch_orders 
			SET status = $1,
				recieve_at = NOW(),
				recieve_by = $2,
				updated_at = NOW()
			WHERE id = $3`,
			StatusAccepted, userName, orderID)
		if err != nil {
			log.Printf("Failed to update dispatch order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update dispatch order"})
			return
		}

		// Update dispatch details status
		_, err = tx.Exec(`
			UPDATE dispatch_details 
			SET current_status = $1,
				updated_at = NOW()
			WHERE dispatch_order_id = $2`,
			StatusAccepted, orderID)
		if err != nil {
			log.Printf("Failed to update dispatch details: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update dispatch details"})
			return
		}

		// Fetch all element IDs from dispatch order
		elementIDs, err := fetchElementIDsFromDispatchOrder(tx, orderID)
		if err != nil {
			log.Printf("Failed to fetch dispatch items: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch dispatch items"})
			return
		}

		// Bulk update all related tables if there are elements
		if len(elementIDs) > 0 {
			// Update precast_stock
			if err := updatePrecastStockForReceipt(tx, elementIDs); err != nil {
				log.Printf("Bulk update failed for precast_stock: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    "Failed to update precast stock",
					"details":  err.Error(),
					"code":     "PRECAST_STOCK_BULK_UPDATE_ERROR",
					"order_id": orderID,
				})
				return
			}

			// Update stock_erected
			if err := updateStockErectedForReceipt(tx, elementIDs); err != nil {
				log.Printf("Bulk update failed for stock_erected: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    "Failed to update stock erected",
					"details":  err.Error(),
					"code":     "STOCK_ERECTED_BULK_UPDATE_ERROR",
					"order_id": orderID,
				})
				return
			}

			// Update stock_erected_logs
			if err := updateStockErectedLogsForReceipt(tx, elementIDs); err != nil {
				log.Printf("Bulk update failed for stock_erected_logs: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    "Failed to update stock erected logs",
					"details":  err.Error(),
					"code":     "STOCK_ERECTED_LOGS_BULK_UPDATE_ERROR",
					"order_id": orderID,
				})
				return
			}

			// Update element status
			if err := updateElementsForReceipt(tx, elementIDs); err != nil {
				log.Printf("Failed to update elements: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update elements"})
				return
			}
		}

		// Get project ID and order number before commit
		var projectID int
		var orderNumber string
		err = tx.QueryRow(`SELECT d.project_id, d.order_number FROM dispatch_orders d WHERE d.id = $1`, orderID).Scan(&projectID, &orderNumber)
		if err != nil {
			log.Printf("Failed to fetch project ID or order number: %v", err)
			projectID = 0
			orderNumber = "N/A"
		}

		// Commit transaction
		if err = tx.Commit(); err != nil {
			log.Printf("Transaction Commit Failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}
		committed = true

		// Success response
		c.JSON(http.StatusOK, gin.H{
			"message":     "Order received successfully!",
			"order_id":    orderID,
			"received_by": userName,
		})

		// Get project name for notification
		var projectName string
		if projectID > 0 {
			projectName = getProjectName(db, projectID)

			// Send notification to the user who received the dispatch order
			sendUserNotification(db, userID,
				fmt.Sprintf("Dispatch order received: %s for project: %s", orderNumber, projectName),
				fmt.Sprintf(NotificationActionTemplate, projectID))

			// Send notifications to all project members, clients, and end_clients
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Dispatch order received: %s for project: %s", orderNumber, projectName),
				fmt.Sprintf(NotificationActionTemplate, projectID))
		}

		// Log activity
		logDispatchActivity(db, session, userNamee, projectID, "Receive", "Receive dispatch order with ID "+orderID)
	}
}

// UpdateDispatchToInTransit updates a dispatch order status to "In Transit".
// This endpoint updates the dispatch_details status and creates a tracking log entry.
// It also sends notifications to relevant users and logs the activity.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// UpdateDispatchToInTransit godoc
// @Summary      Update dispatch to in-transit
// @Description  Mark dispatch order as in transit
// @Tags         dispatch
// @Accept       json
// @Produce      json
// @Param        order_id  path      string  true  "Dispatch order ID"
// @Success      200       {object}  object
// @Failure      400       {object}  models.ErrorResponse
// @Failure      401       {object}  models.ErrorResponse
// @Failure      500       {object}  models.ErrorResponse
// @Router       /api/dispatch_order/{order_id}/in-transit [post]
func UpdateDispatchToInTransit(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Parse and validate order_id parameter
		orderID := c.Param("order_id")
		if orderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order_id is required"})
			return
		}

		// Validate session
		session, userNamee, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID is required"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Fetch user details from session
		userID, userName, err := getUserDetailsFromSession(db, c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction Start Error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Get order number and project ID for tracking log and notifications
		var orderNumber string
		var projectID int
		err = tx.QueryRow(`
			SELECT order_number, project_id 
			FROM dispatch_orders 
			WHERE id = $1`, orderID).Scan(&orderNumber, &projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch order details"})
			return
		}

		// Update dispatch details status
		_, err = tx.Exec(`
			UPDATE dispatch_details 
			SET current_status = $1,
				updated_at = NOW()
			WHERE dispatch_order_id = $2`,
			StatusInTransit, orderID)
		if err != nil {
			log.Printf("Failed to update dispatch details: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update dispatch status"})
			return
		}

		// Insert tracking log entry
		remarks := fmt.Sprintf(DispatchLogRemarksInTransit, userName)
		if err := insertDispatchTrackingLog(context.Background(), tx, orderNumber, StatusInTransit, LocationTruck, remarks, time.Now()); err != nil {
			log.Printf("Failed to insert tracking log: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tracking log"})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Transaction Commit Failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		// Get project name for notification
		projectName := getProjectName(db, projectID)

		// Success response
		c.JSON(http.StatusOK, gin.H{
			"message": "Dispatch status updated to in-transit successfully!",
		})

		// Send notification to the user who updated the dispatch status
		sendUserNotification(db, userID,
			fmt.Sprintf("Dispatch order in transit: %s for project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, projectID))

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Dispatch order in transit: %s for project: %s", orderNumber, projectName),
			fmt.Sprintf(NotificationActionTemplate, projectID))

		// Log activity
		logDispatchActivity(db, session, userNamee, projectID, "Update", "Update dispatch order status to in-transit for order ID "+orderID)
	}
}

// buildDispatchTrackingLogsQuery builds the SQL query for retrieving dispatch tracking logs.
//
// Returns:
//   - A complete SQL SELECT query string
func buildDispatchTrackingLogsQuery() string {
	return `
		SELECT 
			dtl.id,
			dtl.order_number,
			dtl.status,
			dtl.location,
			dtl.remarks,
			dtl.status_timestamp,
			dtl.created_at,
			dtl.updated_at
		FROM dispatch_tracking_logs dtl
		JOIN dispatch_orders do ON dtl.order_number = do.order_number
		WHERE do.project_id = $1
		ORDER BY dtl.status_timestamp DESC`
}

// scanDispatchTrackingLogRow scans a single row from the dispatch tracking logs result set.
//
// Parameters:
//   - rows: Database result set
//
// Returns:
//   - trackingLog: Scanned dispatch tracking log
//   - err: Error if scanning fails
func scanDispatchTrackingLogRow(rows *sql.Rows) (models.DispatchTrackingLog, error) {
	var trackingLog models.DispatchTrackingLog
	if err := rows.Scan(
		&trackingLog.ID,
		&trackingLog.OrderNumber,
		&trackingLog.Status,
		&trackingLog.Location,
		&trackingLog.Remarks,
		&trackingLog.StatusTimestamp,
		&trackingLog.CreatedAt,
		&trackingLog.UpdatedAt,
	); err != nil {
		return models.DispatchTrackingLog{}, fmt.Errorf("error scanning tracking log: %w", err)
	}
	return trackingLog, nil
}

// GetDispatchTrackingLogs retrieves all tracking logs for dispatch orders in a specific project.
// The logs are ordered by status_timestamp in descending order (most recent first).
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// GetDispatchTrackingLogs godoc
// @Summary      Get dispatch tracking logs
// @Description  Get tracking logs for dispatch orders by project
// @Tags         dispatch
// @Accept       json
// @Produce      json
// @Param        project_id  path      string  true  "Project ID"
// @Success      200         {array}  models.DispatchTrackingLog
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Failure      500         {object}  models.ErrorResponse
// @Router       /api/dispatch_order/logs/{project_id} [get]
func GetDispatchTrackingLogs(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		session, userName, err := validateAndGetSession(c, db)
		if err != nil {
			if err.Error() == "session_id header is missing" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			}
			return
		}

		// Parse and validate project_id parameter
		projectID := c.Param("project_id")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID is required"})
			return
		}

		// Fetch tracking logs
		rows, err := db.Query(buildDispatchTrackingLogsQuery(), projectID)
		if err != nil {
			log.Printf("Error fetching tracking logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tracking logs"})
			return
		}
		defer rows.Close()

		var logs []models.DispatchTrackingLog
		for rows.Next() {
			trackingLog, err := scanDispatchTrackingLogRow(rows)
			if err != nil {
				log.Printf("Error scanning tracking log: %v", err)
				continue
			}
			logs = append(logs, trackingLog)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Error iterating tracking logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error processing tracking logs"})
			return
		}

		// Send response
		c.JSON(http.StatusOK, gin.H{
			"project_id": projectID,
			"logs":       logs,
			"count":      len(logs),
		})

		// Log activity
		logDispatchActivity(db, session, userName, 0, "Get", "Get dispatch tracking logs for project ID "+projectID)
	}
}

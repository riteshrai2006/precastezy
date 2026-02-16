package handlers

import (
	"backend/models"
	"backend/services"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type ClientWithUser struct {
	models.Client
	User models.User `json:"user"`
}

// SearchClients searches clients by query params (client_id, user_id, organization, email, etc.).
// @Summary Search clients
// @Description Search clients with optional query filters. Returns array of clients with user details.
// @Tags Clients
// @Accept json
// @Produce json
// @Param client_id query string false "Client ID"
// @Param user_id query string false "User ID"
// @Param organization query string false "Organization"
// @Param email query string false "Email"
// @Success 200 {array} models.Client
// @Failure 500 {object} models.ErrorResponse
// @Router /api/client_search [get]
func SearchClients(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseQuery := `
		SELECT c.client_id, c.user_id, c.organization,
		       u.id, u.employee_id, u.email, u.first_name, u.last_name, u.created_at, u.updated_at,
		       u.first_access, u.last_access, u.profile_picture, u.is_admin, u.address, u.city, 
		       u.state, u.country, u.zip_code, u.phone_no, u.role
		FROM client c
		LEFT JOIN users u ON c.user_id = u.id
		WHERE 1=1`

		queryConditions := []string{}
		queryParams := []interface{}{}
		paramIndex := 1

		// Map of query parameters to columns
		fieldMap := map[string]string{
			"client_id":    "c.client_id",
			"user_id":      "c.user_id",
			"organization": "c.organization",
			"email":        "u.email",
			"first_name":   "u.first_name",
			"last_name":    "u.last_name",
			"phone_no":     "u.phone_no",
			"role_id":      "u.role_id",
		}

		// Build query conditions dynamically
		for field, column := range fieldMap {
			if value := c.Query(field); value != "" {
				queryConditions = append(queryConditions, fmt.Sprintf("%s = $%d", column, paramIndex))
				queryParams = append(queryParams, value)
				paramIndex++
			}
		}

		// Append conditions if present
		if len(queryConditions) > 0 {
			baseQuery += " AND " + strings.Join(queryConditions, " AND ")
		}

		rows, err := db.Query(baseQuery, queryParams...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search clients", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Collect results
		var clients []ClientWithUser

		for rows.Next() {
			var client ClientWithUser
			err := rows.Scan(
				&client.ClientID, &client.UserID, &client.Organization,
				&client.User.ID, &client.User.EmployeeId, &client.User.Email, &client.User.FirstName, &client.User.LastName,
				&client.User.CreatedAt, &client.User.UpdatedAt, &client.User.FirstAccess, &client.User.LastAccess,
				&client.User.ProfilePic, &client.User.IsAdmin, &client.User.Address, &client.User.City, &client.User.State,
				&client.User.Country, &client.User.ZipCode, &client.User.PhoneNo, &client.User.RoleID,
			)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning client details", "details": err.Error()})
				return
			}

			clients = append(clients, client)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, clients)
	}
}

// GetAllClient returns all clients with optional pagination and filters.
// @Summary Get all clients
// @Description Returns clients list with optional pagination (page, page_size) and filters. Requires Authorization header.
// @Tags Clients
// @Accept json
// @Produce json
// @Param page query int false "Page number"
// @Param page_size query int false "Page size"
// @Param client_id query string false "Filter by client ID"
// @Param organization query string false "Filter by organization"
// @Success 200 {object} models.PaginatedResponse "data: clients, pagination when page/page_size provided"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/client [get]
func GetAllClient(db *sql.DB) gin.HandlerFunc {
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
			if limit < 1 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		/* ---------------- SESSION ---------------- */
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		/* ---------------- FILTER PARAMS ---------------- */
		clientID := c.Query("client_id")
		userID := c.Query("user_id")
		organization := c.Query("organization")
		employeeID := c.Query("employee_id")
		email := c.Query("email")
		firstName := c.Query("first_name")
		lastName := c.Query("last_name")
		isAdmin := c.Query("is_admin")
		address := c.Query("address")
		city := c.Query("city")
		state := c.Query("state")
		country := c.Query("country")
		zipCode := c.Query("zip_code")
		phoneNo := c.Query("phone_no")
		roleID := c.Query("role_id")
		projectID := c.Query("project_id")

		var conditions []string
		var args []interface{}
		argIndex := 1

		/* ---------------- SEARCH CONDITIONS ---------------- */

		if clientID != "" {
			conditions = append(conditions, fmt.Sprintf("c.client_id = $%d", argIndex))
			args = append(args, clientID)
			argIndex++
		}

		if userID != "" {
			conditions = append(conditions, fmt.Sprintf("c.user_id = $%d", argIndex))
			args = append(args, userID)
			argIndex++
		}

		if organization != "" {
			conditions = append(conditions, fmt.Sprintf("c.organization ILIKE $%d", argIndex))
			args = append(args, "%"+organization+"%")
			argIndex++
		}

		if employeeID != "" {
			conditions = append(conditions, fmt.Sprintf("u.employee_id ILIKE $%d", argIndex))
			args = append(args, "%"+employeeID+"%")
			argIndex++
		}

		if email != "" {
			conditions = append(conditions, fmt.Sprintf("u.email ILIKE $%d", argIndex))
			args = append(args, "%"+email+"%")
			argIndex++
		}

		if firstName != "" {
			conditions = append(conditions, fmt.Sprintf("u.first_name ILIKE $%d", argIndex))
			args = append(args, "%"+firstName+"%")
			argIndex++
		}

		if lastName != "" {
			conditions = append(conditions, fmt.Sprintf("u.last_name ILIKE $%d", argIndex))
			args = append(args, "%"+lastName+"%")
			argIndex++
		}

		if isAdmin != "" {
			conditions = append(conditions, fmt.Sprintf("u.is_admin = $%d", argIndex))
			args = append(args, isAdmin)
			argIndex++
		}

		if address != "" {
			conditions = append(conditions, fmt.Sprintf("u.address ILIKE $%d", argIndex))
			args = append(args, "%"+address+"%")
			argIndex++
		}

		if city != "" {
			conditions = append(conditions, fmt.Sprintf("u.city ILIKE $%d", argIndex))
			args = append(args, "%"+city+"%")
			argIndex++
		}

		if state != "" {
			conditions = append(conditions, fmt.Sprintf("u.state ILIKE $%d", argIndex))
			args = append(args, "%"+state+"%")
			argIndex++
		}

		if country != "" {
			conditions = append(conditions, fmt.Sprintf("u.country ILIKE $%d", argIndex))
			args = append(args, "%"+country+"%")
			argIndex++
		}

		if zipCode != "" {
			conditions = append(conditions, fmt.Sprintf("u.zip_code ILIKE $%d", argIndex))
			args = append(args, "%"+zipCode+"%")
			argIndex++
		}

		if phoneNo != "" {
			conditions = append(conditions, fmt.Sprintf("u.phone_no ILIKE $%d", argIndex))
			args = append(args, "%"+phoneNo+"%")
			argIndex++
		}

		if roleID != "" {
			conditions = append(conditions, fmt.Sprintf("u.role_id = $%d", argIndex))
			args = append(args, roleID)
			argIndex++
		}

		if projectID != "" {
			// Primary query: project -> end_client -> client relationship
			conditions = append(conditions, fmt.Sprintf(`
				c.client_id IN (
					SELECT ec.client_id
					FROM end_client ec
					WHERE ec.id IN (
						SELECT p.client_id
						FROM project p
						WHERE p.project_id = $%d
					)
				)
			`, argIndex))
			args = append(args, projectID)
			argIndex++
		}

		if len(conditions) == 0 {
			conditions = append(conditions, "1=1")
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT QUERY ---------------- */
		var total int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM client c
			JOIN users u ON c.user_id = u.id
			WHERE %s
		`, whereClause)

		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY ---------------- */
		query := fmt.Sprintf(`
			SELECT 
				c.client_id, c.user_id, c.organization, c.store_id,
				u.email, u.first_name, u.last_name, u.created_at, u.updated_at,
				u.first_access, u.last_access, u.profile_picture, u.is_admin,
				u.address, u.city, u.state, u.country, u.zip_code, u.phone_no,
				u.role_id, r.role_name, u.project_suspend,
				u.phone_code, p.phone_code
			FROM client c
			JOIN users u ON c.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			JOIN phone_code p ON u.phone_code = p.id
			WHERE %s
			ORDER BY c.client_id DESC
		`, whereClause)

		if usePagination {
			query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		/* ---------------- RESPONSE ---------------- */
		var clients []struct {
			models.Client
			User struct {
				models.User
				RoleName string `json:"role_name"`
			} `json:"user"`
		}

		for rows.Next() {
			var client models.Client
			var user struct {
				models.User
				RoleName string `json:"role_name"`
			}

			if err := rows.Scan(
				&client.ClientID, &client.UserID, &client.Organization, &client.StoreID,
				&user.Email, &user.FirstName, &user.LastName, &user.CreatedAt, &user.UpdatedAt,
				&user.FirstAccess, &user.LastAccess, &user.ProfilePic, &user.IsAdmin,
				&user.Address, &user.City, &user.State, &user.Country, &user.ZipCode,
				&user.PhoneNo, &user.RoleID, &user.RoleName, &user.Suspended,
				&user.PhoneCode, &user.PhoneCodeName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			clients = append(clients, struct {
				models.Client
				User struct {
					models.User
					RoleName string `json:"role_name"`
				} `json:"user"`
			}{Client: client, User: user})
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": clients,
		}

		if usePagination {
			response["pagination"] = gin.H{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": int(math.Ceil(float64(total) / float64(limit))),
			}
		}

		c.JSON(http.StatusOK, response)

		/* ---------------- ACTIVITY LOG ---------------- */
		go SaveActivityLog(db, models.ActivityLog{
			EventContext: "Client",
			EventName:    "Search",
			Description:  "Fetched clients with pagination & filters",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

// GetClientByID returns a single client by client_id.
// @Summary Get client by ID
// @Description Returns client details by client_id. Requires Authorization header.
// @Tags Clients
// @Accept json
// @Produce json
// @Param client_id path int true "Client ID"
// @Success 200 {object} models.Client
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/client_fetch/{client_id} [get]
func GetClientByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Extract client_id from the route parameters
		clientID := c.Param("client_id")
		if clientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
			return
		}

		// SQL query to fetch the client and its associated user and role details
		query := `SELECT c.client_id, c.user_id, c.organization, c.store_id,
                         u.email, u.password, u.first_name, u.last_name, u.created_at, u.updated_at,
                         u.first_access, u.last_access, u.profile_picture, u.is_admin, u.address, u.city, 
                         u.state, u.country, u.zip_code, u.phone_no, u.role_id, r.role_name, u.phone_code, p.phone_code
                  FROM client c
                  JOIN users u ON c.user_id = u.id
                  JOIN roles r ON u.role_id = r.role_id
				  JOIN phone_code p ON u.phone_code = p.id
                  WHERE c.client_id = $1`

		// Define a structure to hold the result
		var client struct {
			ClientID      int       `json:"client_id"`
			Organization  string    `json:"organization"`
			UserID        int       `json:"user_id"`
			Email         string    `json:"email" `
			Password      string    `json:"password" `
			FirstName     string    `json:"first_name" `
			LastName      string    `json:"last_name" `
			CreatedAt     time.Time `json:"created_at"`
			UpdatedAt     time.Time `json:"updated_at"`
			FirstAccess   time.Time `json:"first_access,omitempty"`
			LastAccess    time.Time `json:"last_access,omitempty"`
			ProfilePic    string    `json:"profile_picture"`
			IsAdmin       bool      `json:"is_admin"`
			Address       string    `json:"address" `
			City          string    `json:"city" `
			State         string    `json:"state"`
			Country       string    `json:"country"`
			ZipCode       string    `json:"zip_code" `
			PhoneNo       string    `json:"phone_no" `
			RoleID        int       `json:"role_id"`
			RoleName      string    `json:"role_name"`
			StoreID       int       `json:"store_id"`
			PhoneCode     int       `json:"phone_code"`
			PhoneCodeName string    `json:"phone_code_name"`
		}

		// Execute the query and scan the result
		err = db.QueryRow(query, clientID).Scan(
			&client.ClientID, &client.UserID, &client.Organization, &client.StoreID,
			&client.Email, &client.Password, &client.FirstName, &client.LastName,
			&client.CreatedAt, &client.UpdatedAt, &client.FirstAccess, &client.LastAccess,
			&client.ProfilePic, &client.IsAdmin, &client.Address, &client.City, &client.State,
			&client.Country, &client.ZipCode, &client.PhoneNo, &client.RoleID, &client.RoleName, &client.PhoneCode, &client.PhoneCodeName,
		)

		// Handle errors
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching client", "details": err.Error()})
			}
			return
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext: "Client",
			EventName:    "Get",
			Description:  fmt.Sprintf("client with id %d fetched successfully", client.ClientID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Return the response
		c.JSON(http.StatusOK, client)
	}
}

// CreateClient creates a new client and user.
// @Summary Create client
// @Description Creates a new client with user account. Request body: organization, email, password, first_name, last_name, etc. Requires Authorization header.
// @Tags Clients
// @Accept json
// @Produce json
// @Param body body models.CreateClientRequest true "Client and user data"
// @Success 201 {object} models.CreateClientResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/client_create [post]
func CreateClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Define the input JSON structure
		type RequestBody struct {
			Organization   string `json:"organization"`
			Email          string `json:"email"`
			Password       string `json:"password"`
			FirstName      string `json:"first_name"`
			LastName       string `json:"last_name"`
			FirstAccess    string `json:"first_access"`
			LastAccess     string `json:"last_access"`
			ProfilePicture string `json:"profile_picture"`
			Address        string `json:"address"`
			City           string `json:"city"`
			State          string `json:"state"`
			Country        string `json:"country"`
			ZipCode        string `json:"zip_code"`
			PhoneNo        string `json:"phone_no"`
			PhoneCode      int    `json:"phone_code"`
			EmailSend      bool   `json:"emailsend"`
			// Optional: Only needed if superadmin wants to use a specific custom template
			// If not provided, system automatically uses the default welcome_client template
			CustomTemplateID *int `json:"custom_template_id,omitempty"`
			StoreID          *int `json:"store_id,omitempty"`
		}

		var requestBody RequestBody

		// Bind JSON input to the structure
		if err := c.ShouldBindJSON(&requestBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Start a new transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}

		// Ensure the transaction is rolled back if any step fails
		defer func() {
			if p := recover(); p != nil {
				tx.Rollback()
				panic(p)
			}
		}()
		defer tx.Rollback()

		// Insert User into users table
		var userID int
		userQuery := `INSERT INTO users (employee_id, email, password, first_name, last_name, created_at, updated_at, first_access, last_access, 
		profile_picture, is_admin, address, city, state, country, zip_code, phone_no, role_id, phone_code) 
		VALUES (0, $1, $2, $3, $4, NOW(), NOW(), NOW(), NOW(), $5, TRUE, $6, $7, $8, $9, $10, $11, 2, $12) RETURNING id`

		err = tx.QueryRow(userQuery,
			requestBody.Email, requestBody.Password, requestBody.FirstName, requestBody.LastName,
			requestBody.ProfilePicture, requestBody.Address, requestBody.City, requestBody.State,
			requestBody.Country, requestBody.ZipCode, requestBody.PhoneNo, requestBody.PhoneCode,
		).Scan(&userID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "details": err.Error()})
			return
		}

		// Insert Client into client table
		var clientID int
		clientQuery := `INSERT INTO client (user_id, organization, store_id) VALUES ($1, $2, $3) RETURNING client_id`

		err = tx.QueryRow(clientQuery, userID, requestBody.Organization, requestBody.StoreID).Scan(&clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create client", "details": err.Error()})
			return
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		// If emailsend is true, send the welcome email
		if requestBody.EmailSend {
			user := models.User{
				Email:     requestBody.Email,
				FirstName: requestBody.FirstName,
				Password:  requestBody.Password,
				IsAdmin:   true, // Assuming is_admin is always true here as per your query
			}

			// Create email service and send templated email
			emailService := services.NewEmailService(db)
			var templateID int = 2
			if err := emailService.SendWelcomeClientEmail(user, requestBody.Organization, &templateID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Failed to send email",
					"details": err.Error(),
				})
				return
			}
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext:      "Client",
			EventName:         "Create",
			Description:       fmt.Sprintf("Client created successfully with id %d", clientID),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			AffectedUserName:  requestBody.FirstName + " " + requestBody.LastName,
			AffectedUserEmail: requestBody.Email,
			CreatedAt:         time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Construct JSON response
		response := gin.H{
			"message":   "Client created successfully",
			"client_id": clientID,
		}

		c.JSON(http.StatusCreated, response)
	}
}

// UpdateClient updates an existing client and user.
// @Summary Update client
// @Description Updates client and linked user. Request body: organization, email, first_name, last_name, etc. Requires Authorization header.
// @Tags Clients
// @Accept json
// @Produce json
// @Param body body object true "Update payload (organization, email, first_name, last_name, ...)"
// @Success 200 {object} models.MessageResponse "message: Client updated successfully"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/client_update [put]
func UpdateClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		// Extract session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}

		// Get session and user name
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Define input structure
		type UpdateRequest struct {
			ClientID       int    `json:"client_id"` // This is used to find the record, but not updatable
			Organization   string `json:"organization"`
			Email          string `json:"email"`
			Password       string `json:"password"`
			FirstName      string `json:"first_name"`
			LastName       string `json:"last_name"`
			ProfilePicture string `json:"profile_picture"`
			Address        string `json:"address"`
			City           string `json:"city"`
			State          string `json:"state"`
			Country        string `json:"country"`
			ZipCode        string `json:"zip_code"`
			PhoneNo        string `json:"phone_no"`
			StoreID        *int   `json:"store_id,omitempty"`
			PhoneCode      int    `json:"phone_code"`
		}

		var req UpdateRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}
		defer func() {
			if p := recover(); p != nil {
				tx.Rollback()
				panic(p)
			}
		}()
		defer tx.Rollback()

		// Get user_id from client_id
		var userID int
		err = tx.QueryRow(`SELECT user_id FROM client WHERE client_id = $1`, req.ClientID).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found", "details": err.Error()})
			return
		}

		// Update user fields
		_, err = tx.Exec(`
			UPDATE users 
			SET email=$1, password=$2, first_name=$3, last_name=$4, profile_picture=$5,
				address=$6, city=$7, state=$8, country=$9, zip_code=$10, phone_no=$11, updated_at=NOW(), phone_code=$12
			WHERE id=$13`,
			req.Email, req.Password, req.FirstName, req.LastName, req.ProfilePicture,
			req.Address, req.City, req.State, req.Country, req.ZipCode, req.PhoneNo, req.PhoneCode, userID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user", "details": err.Error()})
			return
		}

		// Update organization name
		_, err = tx.Exec(`UPDATE client SET organization = $1, store_id = $2 WHERE client_id = $3`, req.Organization, req.StoreID, req.ClientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update client", "details": err.Error()})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext:      "Client",
			EventName:         "Update",
			Description:       fmt.Sprintf("Client with id %d updated successfully", req.ClientID),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			AffectedUserName:  req.FirstName + " " + req.LastName,
			AffectedUserEmail: req.Email,
			CreatedAt:         time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Client updated but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Return success
		c.JSON(http.StatusOK, gin.H{"message": "Client updated successfully"})
	}
}

// GetUserClientProjectsOverviewHandler godoc
// @Summary      Get client projects overview
// @Tags         clients
// @Param        client_id  path  int  true  "Client ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/client_projects/{client_id} [get]
func GetUserClientProjectsOverviewHandler(db *sql.DB) gin.HandlerFunc {
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

		// Validate client_id param
		clientID, err := strconv.Atoi(c.Param("client_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// ✅ fetch user & role
		var userID, roleID int
		var roleName string
		err = db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ✅ optimized query — same as GetProjectsOverviewHandler but with client filter
		query := `
			WITH project_metrics AS (
				SELECT 
					p.project_id, p.name, p.priority, p.project_status, p.start_date, p.end_date,
					p.logo, p.description, p.created_at, p.updated_at, p.client_id, p.budget, 
					p.template_id, p.suspend, p.subscription_start_date, p.subscription_end_date,
					COALESCE(e.total_elements, 0) as total_elements,
					COALESCE(ps.casted_elements, 0) as casted_elements,
					COALESCE(ps.in_stock, 0) as in_stock,
					COALESCE(a.in_production, 0) as in_production,
					COALESCE(et.element_type_count, 0) as element_type_count,
					COALESCE(pm.project_members_count, 0) as project_members_count,
					COALESCE(ps.erected_elements, 0) as erected_elements
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
    			JOIN client c ON ec.client_id = c.client_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as total_elements
					FROM element
					GROUP BY project_id
				) e ON p.project_id = e.project_id
				LEFT JOIN (
					SELECT 
						e.project_id,
						COUNT(*) FILTER (WHERE ps.stockyard = true AND ps.order_by_erection = false AND ps.erected = false AND ps.dispatch_status = false) as casted_elements,
						COUNT(*) FILTER (WHERE ps.stockyard = true) as in_stock,
						COUNT(*) FILTER (WHERE ps.erected = true) as erected_elements
					FROM precast_stock ps
					JOIN element e ON ps.element_id = e.id
					GROUP BY e.project_id
				) ps ON p.project_id = ps.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as in_production
					FROM activity
					WHERE completed = false
					GROUP BY project_id
				) a ON p.project_id = a.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as element_type_count
					FROM element_type
					GROUP BY project_id
				) et ON p.project_id = et.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as project_members_count
					FROM project_members
					GROUP BY project_id
				) pm ON p.project_id = pm.project_id
				WHERE c.client_id = $1
			),
			stockyard_data AS (
				SELECT 
					ps.project_id,
					COALESCE(
						json_agg(
							json_build_object('id', s.id, 'name', s.yard_name)
						) FILTER (WHERE s.id IS NOT NULL),
						'[]'::json
					) as stockyards
				FROM project_stockyard ps
				LEFT JOIN stockyard s ON ps.stockyard_id = s.id
				GROUP BY ps.project_id
			)
			SELECT 
				pm.*,
				COALESCE(sd.stockyards, '[]'::json) as stockyards_json
			FROM project_metrics pm
			LEFT JOIN stockyard_data sd ON pm.project_id = sd.project_id
			ORDER BY pm.project_id
		`

		rows, err := db.Query(query, clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects", "details": err.Error()})
			return
		}
		defer rows.Close()

		// ✅ process results
		var totalElements, castedElements, inStock, inProduction, notInProduction, elementTypeCount, projectMembersCount int
		projectMetricsList := []models.ProjectMetricsWithDetails{}

		for rows.Next() {
			var pm models.ProjectMetricsWithDetails
			var stockyardsJSON string

			err := rows.Scan(
				&pm.ProjectID, &pm.Name, &pm.Priority, &pm.ProjectStatus,
				&pm.StartDate, &pm.EndDate, &pm.Logo, &pm.Description,
				&pm.CreatedAt, &pm.UpdatedAt, &pm.ClientId, &pm.Budget,
				&pm.TemplateID, &pm.Suspend, &pm.SubscriptionStartDate, &pm.SubscriptionEndDate,
				&pm.TotalElements, &pm.CastedElements, &pm.InStock, &pm.InProduction,
				&pm.ElementTypeCount, &pm.ProjectMembersCount, &pm.ErectedElements, &stockyardsJSON,
			)
			if err != nil {
				continue
			}

			if err := json.Unmarshal([]byte(stockyardsJSON), &pm.Stockyards); err != nil {
				pm.Stockyards = []models.StockyardMinimal{}
			}

			// aggregates
			totalElements += pm.TotalElements
			castedElements += pm.CastedElements
			inStock += pm.InStock
			inProduction += pm.InProduction
			notInProduction += pm.TotalElements - pm.InProduction
			elementTypeCount += pm.ElementTypeCount
			projectMembersCount += pm.ProjectMembersCount

			projectMetricsList = append(projectMetricsList, pm)
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id": clientID,
			"projects":  projectMetricsList,
			"aggregates": gin.H{
				"total_elements":        totalElements,
				"casted_elements":       castedElements,
				"in_stock":              inStock,
				"in_production":         inProduction,
				"not_in_production":     notInProduction,
				"element_type_count":    elementTypeCount,
				"project_members_count": projectMembersCount,
			},
		})

		// ✅ async logging
		go func() {
			_ = SaveActivityLog(db, models.ActivityLog{
				EventContext: "Client",
				EventName:    "Get",
				Description:  fmt.Sprintf("Fetched project overview for client %d", clientID),
				UserName:     userName,
				HostName:     session.HostName,
				IPAddress:    session.IPAddress,
				CreatedAt:    time.Now(),
				ProjectID:    0,
			})
		}()
	}
}

// GetStockyardProjectsHandler godoc
// @Summary      Get projects for a stockyard (with counts)
// @Tags         stockyards
// @Param        stockyard_id  path  int  true  "Stockyard ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/stockyard_project/{stockyard_id} [get]
func GetStockyardProjectsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth ---
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

		// --- parse stockyard_id ---
		stockyardID, err := strconv.Atoi(c.Param("stockyard_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stockyard ID"})
			return
		}

		var stockyardName string
		err = db.QueryRow(`SELECT yard_name FROM stockyard WHERE id = $1`, stockyardID).Scan(&stockyardName)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Stockyard not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyard", "details": err.Error()})
			}
			return
		}

		// --- query: base projects for that stockyard + metrics (metrics limited to this stockyard) ---
		query := `
			WITH base_projects AS (
    SELECT 
        p.project_id, p.name, p.priority, p.project_status, p.start_date, p.end_date,
        p.logo, p.description, p.created_at, p.updated_at, p.client_id, p.budget, 
        p.template_id, p.suspend, p.subscription_start_date, p.subscription_end_date
    FROM project p
    JOIN project_stockyard psp ON p.project_id = psp.project_id
    WHERE psp.stockyard_id = $1
)
SELECT 
    bp.project_id, bp.name, bp.priority, bp.project_status, bp.start_date, bp.end_date,
    bp.logo, bp.description, bp.created_at, bp.updated_at, bp.client_id, bp.budget, 
    bp.template_id, bp.suspend, bp.subscription_start_date, bp.subscription_end_date,
    COALESCE(e.total_elements, 0)       AS total_elements,
    COALESCE(sm.total_check_in, 0)     AS total_check_in,
    COALESCE(sm.total_check_out, 0)    AS total_check_out,
    COALESCE(sm.in_stock, 0)           AS in_stock,
    COALESCE(sm.element_type_count, 0) AS element_type_count
FROM base_projects bp
LEFT JOIN (
    SELECT project_id, COUNT(*) AS total_elements
    FROM element
    GROUP BY project_id
) e ON bp.project_id = e.project_id
LEFT JOIN (
    SELECT
        ps.project_id,
        COUNT(*) AS total_elements,
        COUNT(*) FILTER (WHERE ps.stockyard = TRUE ) AS total_check_in,
        COUNT(*) FILTER (WHERE ps.erected = TRUE ) AS total_check_out,
        COUNT(*) FILTER (WHERE ps.stockyard = TRUE AND ps.erected = FALSE) AS in_stock,
        COUNT(DISTINCT ps.element_type_id) AS element_type_count
    FROM precast_stock ps
    WHERE ps.stockyard_id = $1 
    GROUP BY ps.project_id
) sm ON bp.project_id = sm.project_id;

		`

		rows, err := db.Query(query, stockyardID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyard projects", "details": err.Error()})
			return
		}
		defer rows.Close()

		// Response struct (date fields nullable)
		type ProjectOverview struct {
			ProjectID             int        `json:"project_id"`
			Name                  string     `json:"name"`
			Priority              string     `json:"priority"`
			ProjectStatus         string     `json:"project_status"`
			Logo                  string     `json:"logo"`
			Description           string     `json:"description"`
			CreatedAt             time.Time  `json:"created_at"`
			UpdatedAt             time.Time  `json:"updated_at"`
			ClientId              int        `json:"client_id"`
			Budget                string     `json:"budget"`
			TemplateID            int        `json:"template_id"`
			Suspend               bool       `json:"suspend"`
			SubscriptionStartDate *time.Time `json:"subscription_start_date"`
			SubscriptionEndDate   *time.Time `json:"subscription_end_date"`
			TotalElements         int        `json:"total_elements"`
			TotalCheckIn          int        `json:"total_check_in"`
			TotalCheckOut         int        `json:"total_check_out"`
			InStock               int        `json:"in_stock"`
			ElementTypeCount      int        `json:"element_type_count"`
			StartDate             *time.Time `json:"start_date,omitempty"`
			EndDate               *time.Time `json:"end_date,omitempty"`
		}

		var projects []ProjectOverview
		var aggTotalElements, aggCheckIn, aggCheckOut, aggInStock, aggElementTypes int

		for rows.Next() {
			var p ProjectOverview
			// use sql.NullTime for start/end to handle NULLs safely
			var start sql.NullTime
			var end sql.NullTime

			// scanning -- order MUST match SELECT order above
			if err := rows.Scan(
				&p.ProjectID,             // bp.project_id
				&p.Name,                  // bp.name
				&p.Priority,              // bp.priority  (ignored)
				&p.ProjectStatus,         // bp.project_status (ignored)
				&p.StartDate,             // bp.start_date
				&p.EndDate,               // bp.end_date
				&p.Logo,                  // bp.logo
				&p.Description,           // bp.description
				&p.CreatedAt,             // bp.created_at
				&p.UpdatedAt,             // bp.updated_at
				&p.ClientId,              // bp.client_id
				&p.Budget,                // bp.budget
				&p.TemplateID,            // bp.template_id
				&p.Suspend,               // bp.suspend
				&p.SubscriptionStartDate, // bp.subscription_start_date
				&p.SubscriptionEndDate,   // bp.subscription_end_date
				&p.TotalElements,         // total_elements
				&p.TotalCheckIn,          // total_check_in
				&p.TotalCheckOut,         // total_check_out
				&p.InStock,               // in_stock
				&p.ElementTypeCount,      // element_type_count
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan row", "details": err.Error()})
				return
			}

			if start.Valid {
				t := start.Time
				p.StartDate = &t
			}
			if end.Valid {
				t := end.Time
				p.EndDate = &t
			}

			projects = append(projects, p)
			aggTotalElements += p.TotalElements
			aggCheckIn += p.TotalCheckIn
			aggCheckOut += p.TotalCheckOut
			aggInStock += p.InStock
			aggElementTypes += p.ElementTypeCount
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error reading rows", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"stockyard_id":   stockyardID,
			"stockyard_name": stockyardName,
			"projects":       projects,
			"aggregates": gin.H{
				"total_elements":     aggTotalElements,
				"total_check_in":     aggCheckIn,
				"total_check_out":    aggCheckOut,
				"in_stock":           aggInStock,
				"total_element_type": aggElementTypes,
			},
		})

		// async log
		go func() {
			_ = SaveActivityLog(db, models.ActivityLog{
				EventContext: "Stockyard",
				EventName:    "GetProjects",
				Description:  fmt.Sprintf("Fetched projects for stockyard %d", stockyardID),
				UserName:     userName,
				HostName:     session.HostName,
				IPAddress:    session.IPAddress,
				CreatedAt:    time.Now(),
				ProjectID:    0,
			})
		}()
	}
}

// GetEndClientProjectsOverviewHandler godoc
// @Summary      Get end-client projects overview
// @Tags         end-clients
// @Param        client_id  path  int  true  "End client ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/endclient_projects/{client_id} [get]
func GetEndClientProjectsOverviewHandler(db *sql.DB) gin.HandlerFunc {
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

		// Validate client_id param
		clientID := c.Param("client_id")
		if clientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID is required"})
			return
		}

		// ✅ fetch user & role
		var userID, roleID int
		var roleName string
		err = db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// ✅ optimized query — same as GetProjectsOverviewHandler but with client filter
		query := `
			WITH project_metrics AS (
				SELECT 
					p.project_id, p.name, p.priority, p.project_status, p.start_date, p.end_date,
					p.logo, p.description, p.created_at, p.updated_at, p.client_id, p.budget, 
					p.template_id, p.suspend, p.subscription_start_date, p.subscription_end_date,
					COALESCE(e.total_elements, 0) as total_elements,
					COALESCE(ps.casted_elements, 0) as casted_elements,
					COALESCE(ps.in_stock, 0) as in_stock,
					COALESCE(a.in_production, 0) as in_production,
					COALESCE(et.element_type_count, 0) as element_type_count,
					COALESCE(pm.project_members_count, 0) as project_members_count,
					COALESCE(ps.erected_elements, 0) as erected_elements
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as total_elements
					FROM element
					GROUP BY project_id
				) e ON p.project_id = e.project_id
				LEFT JOIN (
					SELECT 
						e.project_id,
						COUNT(*) FILTER (WHERE ps.stockyard = true AND ps.order_by_erection = false AND ps.erected = false AND ps.dispatch_status = false) as casted_elements,
						COUNT(*) FILTER (WHERE ps.stockyard = true) as in_stock,
						COUNT(*) FILTER (WHERE ps.erected = true) as erected_elements
					FROM precast_stock ps
					JOIN element e ON ps.element_id = e.id
					GROUP BY e.project_id
				) ps ON p.project_id = ps.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as in_production
					FROM activity
					WHERE completed = false
					GROUP BY project_id
				) a ON p.project_id = a.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as element_type_count
					FROM element_type
					GROUP BY project_id
				) et ON p.project_id = et.project_id
				LEFT JOIN (
					SELECT project_id, COUNT(*) as project_members_count
					FROM project_members
					GROUP BY project_id
				) pm ON p.project_id = pm.project_id
				WHERE p.client_id = $1
			),
			stockyard_data AS (
				SELECT 
					ps.project_id,
					COALESCE(
						json_agg(
							json_build_object('id', s.id, 'name', s.yard_name)
						) FILTER (WHERE s.id IS NOT NULL),
						'[]'::json
					) as stockyards
				FROM project_stockyard ps
				LEFT JOIN stockyard s ON ps.stockyard_id = s.id
				GROUP BY ps.project_id
			)
			SELECT 
				pm.*,
				COALESCE(sd.stockyards, '[]'::json) as stockyards_json
			FROM project_metrics pm
			LEFT JOIN stockyard_data sd ON pm.project_id = sd.project_id
			ORDER BY pm.project_id
		`

		rows, err := db.Query(query, clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects", "details": err.Error()})
			return
		}
		defer rows.Close()

		// ✅ process results
		var totalElements, castedElements, inStock, inProduction, notInProduction, elementTypeCount, projectMembersCount int
		projectMetricsList := []models.ProjectMetricsWithDetails{}

		for rows.Next() {
			var pm models.ProjectMetricsWithDetails
			var stockyardsJSON string

			err := rows.Scan(
				&pm.ProjectID, &pm.Name, &pm.Priority, &pm.ProjectStatus,
				&pm.StartDate, &pm.EndDate, &pm.Logo, &pm.Description,
				&pm.CreatedAt, &pm.UpdatedAt, &pm.ClientId, &pm.Budget,
				&pm.TemplateID, &pm.Suspend, &pm.SubscriptionStartDate, &pm.SubscriptionEndDate,
				&pm.TotalElements, &pm.CastedElements, &pm.InStock, &pm.InProduction,
				&pm.ElementTypeCount, &pm.ProjectMembersCount, &pm.ErectedElements, &stockyardsJSON,
			)
			if err != nil {
				continue
			}

			if err := json.Unmarshal([]byte(stockyardsJSON), &pm.Stockyards); err != nil {
				pm.Stockyards = []models.StockyardMinimal{}
			}

			// aggregates
			totalElements += pm.TotalElements
			castedElements += pm.CastedElements
			inStock += pm.InStock
			inProduction += pm.InProduction
			notInProduction += pm.TotalElements - pm.InProduction
			elementTypeCount += pm.ElementTypeCount
			projectMembersCount += pm.ProjectMembersCount

			projectMetricsList = append(projectMetricsList, pm)
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id": clientID,
			"projects":  projectMetricsList,
			"aggregates": gin.H{
				"total_elements":        totalElements,
				"casted_elements":       castedElements,
				"in_stock":              inStock,
				"in_production":         inProduction,
				"not_in_production":     notInProduction,
				"element_type_count":    elementTypeCount,
				"project_members_count": projectMembersCount,
			},
		})

		// ✅ async logging
		go func() {
			_ = SaveActivityLog(db, models.ActivityLog{
				EventContext: "Client",
				EventName:    "Get",
				Description:  fmt.Sprintf("Fetched project overview for client %s", clientID),
				UserName:     userName,
				HostName:     session.HostName,
				IPAddress:    session.IPAddress,
				CreatedAt:    time.Now(),
				ProjectID:    0,
			})
		}()
	}
}

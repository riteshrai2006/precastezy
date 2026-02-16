package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// CreateEndClient godoc
// @Summary      Create end client
// @Tags         end-clients
// @Accept       json
// @Produce      json
// @Param        body  body      models.EndClient  true  "End client data"
// @Success      200   {object}  models.EndClient
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/end_clients [post]
func CreateEndClient(db *sql.DB) gin.HandlerFunc {
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

		var client models.EndClient
		if err := c.ShouldBindJSON(&client); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `
			INSERT INTO end_client 
			(email, contact_person, address, attachment, cin, gst_number, phone_no, profile_picture, created_by, client_id, phone_code, abbreviation, organization_name) 
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id, created_at, updated_at
		`
		err := db.QueryRow(query,
			client.Email, client.ContactPerson, client.Address,
			pq.Array(client.Attachment), client.CIN, client.GSTNumber,
			client.PhoneNo, client.ProfilePicture, createdBy, client.ClientID, client.PhoneCode, client.Abbreviation, client.OrganizationName,
		).Scan(&client.ID, &client.CreatedAt, &client.UpdatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Updated by and ClientID
		client.CreatedBy = createdBy
		c.JSON(http.StatusOK, client)

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", client.ClientID).Scan(&clientName)
		if err != nil {
			clientName = fmt.Sprintf("Client %d", client.ClientID)
		}

		// Send notification to the user who created the end client
		notif := models.Notification{
			UserID:    createdBy,
			Message:   fmt.Sprintf("New end client created: %s for client: %s", client.OrganizationName, clientName),
			Status:    "unread",
			Action:    "https://precastezy.blueinvent.com/projectclient",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = db.Exec(`
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		// Send notifications to all client users and end_client users
		sendClientNotifications(db, client.ClientID,
			fmt.Sprintf("New end client created: %s for client: %s", client.OrganizationName, clientName),
			"https://precastezy.blueinvent.com/projectclient")
	}
}

// GetEndClients godoc
// @Summary      List end clients
// @Tags         end-clients
// @Param        page       query  int  false  "Page"
// @Param        page_size  query  int  false  "Page size"
// @Success      200  {object}  object
// @Router       /api/end_clients [get]
func GetEndClients(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		/* ---------------- PAGINATION ---------------- */
		pageStr := c.Query("page")
		limitStr := c.Query("page_size")

		usePagination := pageStr != "" || limitStr != ""

		page := 1
		limit := 10

		if usePagination {
			var parseErr error
			page, parseErr = strconv.Atoi(pageStr)
			if parseErr != nil || page < 1 {
				page = 1
			}
			limit, parseErr = strconv.Atoi(limitStr)
			if parseErr != nil || limit < 1 || limit > 100 {
				limit = 10
			}
		}
		offset := (page - 1) * limit

		/* ---------------- SESSION ---------------- */
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
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

		/* ---------------- FILTER PARAMS ---------------- */
		email := c.Query("email")
		contactPerson := c.Query("contact_person")
		address := c.Query("address")
		cin := c.Query("cin")
		gstNumber := c.Query("gst_number")
		phoneNo := c.Query("phone_no")
		filterClientID := c.Query("client_id")
		organization := c.Query("organization")

		var conditions []string
		var args []interface{}
		argIndex := 1

		/* ---------------- ROLE ACCESS ---------------- */
		if strings.EqualFold(roleName, "admin") {
			var clientID int
			if err := db.QueryRow(`SELECT client_id FROM client WHERE user_id=$1`, userID).
				Scan(&clientID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get client"})
				return
			}

			conditions = append(conditions, fmt.Sprintf("ec.client_id = $%d", argIndex))
			args = append(args, clientID)
			argIndex++
		}

		/* ---------------- ADVANCED SEARCH ---------------- */

		if email != "" {
			conditions = append(conditions, fmt.Sprintf("ec.email ILIKE $%d", argIndex))
			args = append(args, "%"+email+"%")
			argIndex++
		}

		if contactPerson != "" {
			conditions = append(conditions, fmt.Sprintf("ec.contact_person ILIKE $%d", argIndex))
			args = append(args, "%"+contactPerson+"%")
			argIndex++
		}

		if address != "" {
			conditions = append(conditions, fmt.Sprintf("ec.address ILIKE $%d", argIndex))
			args = append(args, "%"+address+"%")
			argIndex++
		}

		if cin != "" {
			conditions = append(conditions, fmt.Sprintf("ec.cin ILIKE $%d", argIndex))
			args = append(args, "%"+cin+"%")
			argIndex++
		}

		if gstNumber != "" {
			conditions = append(conditions, fmt.Sprintf("ec.gst_number ILIKE $%d", argIndex))
			args = append(args, "%"+gstNumber+"%")
			argIndex++
		}

		if phoneNo != "" {
			conditions = append(conditions, fmt.Sprintf("ec.phone_no ILIKE $%d", argIndex))
			args = append(args, "%"+phoneNo+"%")
			argIndex++
		}

		if filterClientID != "" {
			conditions = append(conditions, fmt.Sprintf("ec.client_id = $%d", argIndex))
			args = append(args, filterClientID)
			argIndex++
		}

		if organization != "" {
			conditions = append(conditions, fmt.Sprintf("c.organization ILIKE $%d", argIndex))
			args = append(args, "%"+organization+"%")
			argIndex++
		}

		if len(conditions) == 0 {
			conditions = append(conditions, "1=1")
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT ---------------- */
		var total int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM end_client ec
			LEFT JOIN client c ON ec.client_id=c.client_id
			WHERE %s
		`, whereClause)

		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY ---------------- */
		query := fmt.Sprintf(`
			SELECT 
				ec.id, ec.email, ec.contact_person, ec.address, ec.attachment,
				ec.cin, ec.gst_number, ec.phone_no, ec.profile_picture,
				ec.created_at, ec.updated_at, ec.created_by, ec.client_id,
				c.organization, ec.phone_code, pc.phone_code,
				ec.abbreviation, ec.organization_name
			FROM end_client ec
			LEFT JOIN client c ON ec.client_id=c.client_id
			JOIN phone_code pc ON ec.phone_code=pc.id
			WHERE %s
			ORDER BY ec.created_at DESC
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
		var clients []models.EndClient
		for rows.Next() {
			var cl models.EndClient
			if err := rows.Scan(
				&cl.ID, &cl.Email, &cl.ContactPerson, &cl.Address,
				pq.Array(&cl.Attachment),
				&cl.CIN, &cl.GSTNumber, &cl.PhoneNo, &cl.ProfilePicture,
				&cl.CreatedAt, &cl.UpdatedAt, &cl.CreatedBy, &cl.ClientID,
				&cl.Organization, &cl.PhoneCode, &cl.PhoneCodeName,
				&cl.Abbreviation, &cl.OrganizationName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			clients = append(clients, cl)
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": clients,
		}

		// Only include pagination if pagination parameters were provided
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
			EventContext: "End Clients",
			EventName:    "Search",
			Description:  "Fetched end clients with pagination & filters",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

// GetEndClientsByClient godoc
// @Summary      Get end clients by client ID
// @Tags         end-clients
// @Param        client_id  path      int  true  "Client ID"
// @Success      200        {array}   models.EndClient
// @Router       /api/end_client/{client_id} [get]
func GetEndClientsByClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Get client_id from URL parameter ---
		clientID := c.Param("client_id")
		if clientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing client_id"})
			return
		}

		// --- Query end clients for this client ---
		rows, err := db.Query(`
			SELECT 
				ec.id, ec.email, ec.contact_person, ec.address, ec.attachment, 
				ec.cin, ec.gst_number, ec.phone_no, ec.profile_picture, 
				ec.created_at, ec.updated_at, ec.created_by, ec.client_id,
				c.organization, ec.phone_code, pc.phone_code, ec.abbreviation, ec.organization_name
			FROM end_client ec
			LEFT JOIN client c ON ec.client_id = c.client_id
			JOIN phone_code pc ON ec.phone_code = pc.id
			WHERE ec.client_id = $1
			ORDER BY ec.created_at DESC
		`, clientID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var clients []models.EndClient
		for rows.Next() {
			var cl models.EndClient
			if err := rows.Scan(
				&cl.ID, &cl.Email, &cl.ContactPerson, &cl.Address, pq.Array(&cl.Attachment),
				&cl.CIN, &cl.GSTNumber, &cl.PhoneNo, &cl.ProfilePicture,
				&cl.CreatedAt, &cl.UpdatedAt, &cl.CreatedBy, &cl.ClientID,
				&cl.Organization, &cl.PhoneCode, &cl.PhoneCodeName, &cl.Abbreviation, &cl.OrganizationName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			clients = append(clients, cl)
		}

		// --- Check for iteration errors ---
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// --- Return empty array if no clients found (not 404) ---
		if clients == nil {
			clients = []models.EndClient{}
		}

		c.JSON(http.StatusOK, clients)
	}
}

// UpdateEndClient godoc
// @Summary      Update end client
// @Tags         end-clients
// @Param        id     path      int  true  "End client ID"
// @Param        body   body      models.EndClient  true  "End client data"
// @Success      200    {object}  object
// @Failure      400    {object}  models.ErrorResponse
// @Router       /api/end_clients/{id} [put]
func UpdateEndClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Fetch old end client info before update for notifications
		var oldOrganizationName string
		var oldClientID int
		err := db.QueryRow("SELECT organization_name, client_id FROM end_client WHERE id = $1", id).Scan(&oldOrganizationName, &oldClientID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "End client not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch end client info"})
			return
		}

		var client models.EndClient
		if err := c.ShouldBindJSON(&client); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `
			UPDATE end_client 
			SET email=$1, contact_person=$2, address=$3, attachment=$4, cin=$5, gst_number=$6, phone_no=$7, profile_picture=$8, updated_at=NOW() , phone_code=$9, abbreviation=$11, organization_name=$12
			WHERE id=$10 RETURNING id, created_at, updated_at, created_by, client_id
		`
		err = db.QueryRow(query,
			client.Email, client.ContactPerson, client.Address,
			pq.Array(client.Attachment), client.CIN, client.GSTNumber,
			client.PhoneNo, client.ProfilePicture, client.PhoneCode, id, client.Abbreviation, client.OrganizationName,
		).Scan(&client.ID, &client.CreatedAt, &client.UpdatedAt, &client.CreatedBy, &client.ClientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, client)

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", client.ClientID).Scan(&clientName)
		if err != nil {
			clientName = fmt.Sprintf("Client %d", client.ClientID)
		}

		// Fetch user_id from the session table
		sessionID := c.GetHeader("Authorization")
		var userID int
		if sessionID != "" {
			err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
			if err == nil {
				// Send notification to the user who updated the end client
				notif := models.Notification{
					UserID:    userID,
					Message:   fmt.Sprintf("End client updated: %s for client: %s", client.OrganizationName, clientName),
					Status:    "unread",
					Action:    "https://precastezy.blueinvent.com/projectclient",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}

				_, err = db.Exec(`
					INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6)
				`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)
			}
		}

		// Send notifications to all client users and end_client users
		sendClientNotifications(db, client.ClientID,
			fmt.Sprintf("End client updated: %s for client: %s", client.OrganizationName, clientName),
			"https://precastezy.blueinvent.com/projectclient")
	}
}

// DeleteEndClient godoc
// @Summary      Delete end client
// @Tags         end-clients
// @Param        id   path      int  true  "End client ID"
// @Success      200  {object}  object
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/end_clients/{id} [delete]
func DeleteEndClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Fetch end client info before deletion for notifications
		var organizationName string
		var clientID int
		err := db.QueryRow("SELECT organization_name, client_id FROM end_client WHERE id = $1", id).Scan(&organizationName, &clientID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "End client not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch end client info"})
			return
		}

		_, err = db.Exec(`DELETE FROM end_client WHERE id = $1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Client deleted successfully"})

		// Get client name for notification
		var clientName string
		err = db.QueryRow("SELECT organization FROM client WHERE client_id = $1", clientID).Scan(&clientName)
		if err != nil {
			clientName = fmt.Sprintf("Client %d", clientID)
		}

		// Fetch user_id from the session table
		sessionID := c.GetHeader("Authorization")
		var userID int
		if sessionID != "" {
			err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
			if err == nil {
				// Send notification to the user who deleted the end client
				notif := models.Notification{
					UserID:    userID,
					Message:   fmt.Sprintf("End client deleted: %s from client: %s", organizationName, clientName),
					Status:    "unread",
					Action:    "https://precastezy.blueinvent.com/projectclient",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}

				_, err = db.Exec(`
					INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6)
				`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)
			}
		}

		// Send notifications to all client users and end_client users
		sendClientNotifications(db, clientID,
			fmt.Sprintf("End client deleted: %s from client: %s", organizationName, clientName),
			"https://precastezy.blueinvent.com/projectclient")
	}
}

// GetEndClient godoc
// @Summary      Get end client by ID
// @Tags         end-clients
// @Param        id   path      int  true  "End client ID"
// @Success      200  {object}  models.EndClient
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/end_clients/{id} [get]
func GetEndClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var cl models.EndClient
		query := `
			SELECT 
				ec.id, ec.email, ec.contact_person, ec.address, ec.attachment, 
				ec.cin, ec.gst_number, ec.phone_no, ec.profile_picture, 
				ec.created_at, ec.updated_at, ec.created_by, ec.client_id,
				c.organization, ec.phone_code, pc.phone_code, ec.abbreviation, ec.organization_name 
			FROM end_client ec
			LEFT JOIN client c ON ec.client_id = c.client_id
			JOIN phone_code pc ON ec.phone_code = pc.id
			WHERE ec.id = $1
		`
		err := db.QueryRow(query, id).Scan(
			&cl.ID, &cl.Email, &cl.ContactPerson, &cl.Address, pq.Array(&cl.Attachment),
			&cl.CIN, &cl.GSTNumber, &cl.PhoneNo, &cl.ProfilePicture,
			&cl.CreatedAt, &cl.UpdatedAt, &cl.CreatedBy, &cl.ClientID,
			&cl.Organization, &cl.PhoneCode, &cl.PhoneCodeName, &cl.Abbreviation, &cl.OrganizationName,
		)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, cl)
	}
}

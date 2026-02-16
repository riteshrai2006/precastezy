package handlers

import (
	"backend/models"
	"backend/services"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
)

// sendProjectClientNotifications sends notifications to all client and end_client users associated with a project
func sendProjectClientNotifications(db *sql.DB, projectID int, message string, action string) {
	// Get all user IDs associated with the project's client and end_client:
	// 1. Client users (through project -> end_client -> client)
	query := `
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.id IN (
			-- Client users (through project -> end_client -> client)
			SELECT cl.user_id
			FROM project p
			JOIN end_client ec ON p.client_id = ec.id
			JOIN client cl ON ec.client_id = cl.client_id
			WHERE p.project_id = $1 AND cl.user_id IS NOT NULL
		)
	`

	rows, err := db.Query(query, projectID)
	if err != nil {
		log.Printf("Failed to fetch project client stakeholders: %v", err)
		return
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			log.Printf("Failed to scan user ID: %v", err)
			continue
		}
		userIDs = append(userIDs, userID)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over user IDs: %v", err)
		return
	}

	// Send notification to each user
	now := time.Now()
	for _, userID := range userIDs {
		notif := models.Notification{
			UserID:    userID,
			Message:   message,
			Status:    "unread",
			Action:    action,
			CreatedAt: now,
			UpdatedAt: now,
		}

		_, err = db.Exec(`
			INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

		if err != nil {
			log.Printf("Failed to insert notification for user %d: %v", userID, err)
		}
	}
}

// CreateMember adds a member to a project (existing user or new user).
// @Summary Create project member
// @Description Adds a member to a project. Query: existed=true for existing user. Body: project_id, role_id, email or user_id, etc. Requires Authorization header.
// @Tags Project Members
// @Accept json
// @Produce json
// @Param existed query string false "Set true to add existing user by email/user_id"
// @Param body body object true "project_id, role_id, email or user_id, emailsend, etc."
// @Success 201 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/create_project_members [post]
func CreateMember(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Session check ---
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

		existed := c.Query("existed") // if "true", we add existing user

		// ====================================================
		// --- EXISTING USER FLOW ---
		// ====================================================
		if existed == "true" {
			var input struct {
				ProjectID int    `json:"project_id"`
				RoleID    int    `json:"role_id"`
				Email     string `json:"email"`
				UserID    *int   `json:"user_id"` // optional, if provided directly
				EmailSend bool   `json:"emailsend"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
				return
			}
			if input.ProjectID == 0 || (input.UserID == nil && input.Email == "") {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID and either user_id or email are required"})
				return
			}

			// Resolve user_id
			userID := 0
			if input.UserID != nil {
				userID = *input.UserID
			} else {
				err = db.QueryRow(`SELECT id FROM users WHERE email = $1`, input.Email).Scan(&userID)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "details": err.Error()})
					return
				}
			}

			// Insert into project_members
			var projectMemberID int
			err = db.QueryRow(`
				INSERT INTO project_members (project_id, user_id, role_id, created_at, updated_at)
				VALUES ($1, $2, $3, NOW(), NOW())
				RETURNING id
			`, input.ProjectID, userID, input.RoleID).Scan(&projectMemberID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add existing user as project member", "details": err.Error()})
				return
			}

			// Fetch project details
			var projectName, organization string
			err = db.QueryRow(`SELECT p.name, c.organization
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
				JOIN client c ON ec.client_id = c.client_id
				WHERE p.project_id = $1`, input.ProjectID).Scan(&projectName, &organization)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project details", "details": err.Error()})
				return
			}

			// Optional: send email
			if input.EmailSend && input.Email != "" {
				emailService := services.NewEmailService(db)
				emailData := models.EmailData{
					ClientName:   "", // unknown for existing
					Email:        input.Email,
					Role:         "Project Member",
					Organization: organization,
					ProjectName:  projectName,
					ProjectID:    strconv.Itoa(input.ProjectID),
					CompanyName:  organization,
					SupportEmail: "support@blueinvent.com",
					LoginURL:     "https://precastezy.blueinvent.com/login",
					AdminName:    userName,
					UserName:     userName,
				}
				templateType := "welcome user"
				var templateID int = 3
				_ = emailService.SendTemplatedEmail(templateType, emailData, &templateID)
			}

			// Get userID from session for notification
			var adminUserID int
			err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
			if err != nil {
				log.Printf("Failed to fetch user_id for notification: %v", err)
			} else {
				// Send notification to the admin user who added the member
				notif := models.Notification{
					UserID:    adminUserID,
					Message:   fmt.Sprintf("New member added to project: %s", projectName),
					Status:    "unread",
					Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", input.ProjectID),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}

				_, err = db.Exec(`
					INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6)
				`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

				if err != nil {
					log.Printf("Failed to insert notification: %v", err)
				}
			}

			// Send notifications to all client and end_client users
			sendProjectClientNotifications(db, input.ProjectID,
				fmt.Sprintf("New member added to project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", input.ProjectID))

			// Log activity
			log := models.ActivityLog{
				EventContext: "Project Member",
				EventName:    "Existing User Added to Project",
				Description:  fmt.Sprintf("User with ID %d added to project with ID %d", userID, input.ProjectID),
				UserName:     userName,
				HostName:     session.HostName,
				IPAddress:    session.IPAddress,
				CreatedAt:    time.Now(),
				ProjectID:    input.ProjectID,
			}
			_ = SaveActivityLog(db, log)

			c.JSON(http.StatusCreated, gin.H{
				"message":      "Existing user added to project successfully",
				"user_id":      userID,
				"project_id":   input.ProjectID,
				"project_role": input.RoleID,
			})
			return
		}

		// ====================================================
		// --- NEW USER FLOW ---
		// ====================================================
		var input struct {
			ProjectID        int    `json:"project_id"`
			RoleID           int    `json:"role_id"`
			EmployeeID       string `json:"employee_id"`
			Email            string `json:"email"`
			Password         string `json:"password"`
			FirstName        string `json:"first_name"`
			LastName         string `json:"last_name"`
			ProfilePic       string `json:"profile_picture"`
			IsAdmin          bool   `json:"is_admin"`
			Address          string `json:"address"`
			City             string `json:"city"`
			State            string `json:"state"`
			Country          string `json:"country"`
			ZipCode          string `json:"zip_code"`
			PhoneNo          string `json:"phone_no"`
			EmailSend        bool   `json:"emailsend"`
			CustomTemplateID *int   `json:"custom_template_id"`
			PhoneCode        int    `json:"phone_code"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}
		if input.ProjectID == 0 || input.EmployeeID == "" || input.Email == "" || input.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID, Employee ID, Email, and Password are required"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}

		// Insert into users
		queryInsertUser := `
			INSERT INTO users (employee_id, email, password, first_name, last_name, profile_picture, 
			is_admin, address, city, state, country, zip_code, phone_no, role_id, created_at, updated_at, phone_code) 
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW(), $15) 
			RETURNING id, created_at, updated_at
		`
		var user models.User
		err = tx.QueryRow(queryInsertUser,
			input.EmployeeID, input.Email, input.Password,
			input.FirstName, input.LastName, input.ProfilePic,
			input.IsAdmin, input.Address, input.City, input.State, input.Country,
			input.ZipCode, input.PhoneNo, input.RoleID, input.PhoneCode,
		).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "details": err.Error()})
			return
		}

		// Insert into project_members
		var projectMember models.ProjectMember
		err = tx.QueryRow(`
			INSERT INTO project_members (project_id, user_id, role_id, created_at, updated_at) 
			VALUES ($1, $2, $3, NOW(), NOW()) 
			RETURNING id, created_at, updated_at
		`, input.ProjectID, user.ID, input.RoleID).Scan(&projectMember.ID, &projectMember.CreatedAt, &projectMember.UpdatedAt)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project member", "details": err.Error()})
			return
		}

		// Fetch project details
		var projectName, organization string
		err = db.QueryRow(`SELECT p.name, c.organization
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
				JOIN client c ON ec.client_id = c.client_id
				WHERE p.project_id = $1`, input.ProjectID).Scan(&projectName, &organization)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project details", "details": err.Error()})
			return
		}

		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		// Optional email
		if input.EmailSend {
			emailService := services.NewEmailService(db)
			emailData := models.EmailData{
				ClientName:   input.FirstName + " " + input.LastName,
				Email:        input.Email,
				Password:     input.Password,
				Role:         "Project Member",
				Organization: organization,
				ProjectName:  projectName,
				ProjectID:    strconv.Itoa(input.ProjectID),
				CompanyName:  organization,
				SupportEmail: "support@blueinvent.com",
				LoginURL:     "https://precastezy.blueinvent.com/login",
				AdminName:    input.FirstName + " " + input.LastName,
				UserName:     input.FirstName + " " + input.LastName,
			}
			templateType := "welcome user"
			var templateID int = 3
			_ = emailService.SendTemplatedEmail(templateType, emailData, &templateID)
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who created the member
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("New member created in project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", input.ProjectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all client and end_client users
		sendProjectClientNotifications(db, input.ProjectID,
			fmt.Sprintf("New member created in project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", input.ProjectID))

		// Log activity
		log := models.ActivityLog{
			EventContext:      "Project Member",
			EventName:         "Project Member Created",
			Description:       fmt.Sprintf("Member with ID %d created successfully in project with ID %d", user.ID, input.ProjectID),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			AffectedUserName:  input.FirstName + " " + input.LastName,
			AffectedUserEmail: input.Email,
			ProjectID:         input.ProjectID,
		}
		_ = SaveActivityLog(db, log)

		c.JSON(http.StatusCreated, gin.H{
			"message": "User and project member created successfully",
			"user_id": user.ID,
		})
	}
}

// UpdateMember updates a project member's role.
// @Summary Update project member
// @Description Updates member role for project. Path: project_id. Body: user_id, role_id, etc. Requires Authorization header.
// @Tags Project Members
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param body body object true "user_id, role_id, etc."
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/update_project_members/{project_id} [put]
func UpdateMember(db *sql.DB) gin.HandlerFunc {
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

		// Extract `project_id` from the route
		projectIDParam := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDParam)
		if err != nil || projectID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID in route"})
			return
		}

		var input struct {
			RoleID     int    `json:"role_id"`
			UserID     int    `json:"user_id"` // Required to identify the user to update
			EmployeeID string `json:"employee_id"`
			Email      string `json:"email"`
			Password   string `json:"password"`
			FirstName  string `json:"first_name"`
			LastName   string `json:"last_name"`
			ProfilePic string `json:"profile_picture"`
			IsAdmin    bool   `json:"is_admin"`
			Address    string `json:"address"`
			City       string `json:"city"`
			State      string `json:"state"`
			Country    string `json:"country"`
			ZipCode    string `json:"zip_code"`
			PhoneNo    string `json:"phone_no"`
			PhoneCode  int    `json:"phone_code"`
		}

		// Bind incoming JSON payload to the input struct
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Validate required fields
		if input.UserID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction", "details": err.Error()})
			return
		}

		// Update user information in the `users` table
		queryUpdateUser := `
			UPDATE users 
			SET employee_id = $1, email = $2, password = $3, first_name = $4, last_name = $5, 
				profile_picture = $6, is_admin = $7, address = $8, city = $9, state = $10, 
				country = $11, zip_code = $12, phone_no = $13, role_id = $14, updated_at = NOW(), phone_code = $15
			WHERE id = $16
		`
		_, err = tx.Exec(queryUpdateUser, input.EmployeeID, input.Email, input.Password, input.FirstName,
			input.LastName, input.ProfilePic, input.IsAdmin, input.Address, input.City, input.State,
			input.Country, input.ZipCode, input.PhoneNo, input.RoleID, input.UserID, input.PhoneCode)

		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user", "details": err.Error()})
			return
		}

		// Update project role in the `project_members` table
		queryUpdateProjectMember := `
			UPDATE project_members 
			SET role_id = $1, updated_at = NOW()
			WHERE project_id = $2 AND user_id = $3
		`
		_, err = tx.Exec(queryUpdateProjectMember, input.RoleID, projectID, input.UserID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project member role", "details": err.Error()})
			return
		}

		// Commit the transaction
		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction", "details": err.Error()})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who updated the member
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Member updated in project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", projectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all client and end_client users
		sendProjectClientNotifications(db, projectID,
			fmt.Sprintf("Member updated in project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", projectID))

		// Log the activity
		log := models.ActivityLog{
			EventContext:      "Project Members",
			EventName:         "Project Member Updated",
			Description:       fmt.Sprintf("Member with ID %d updated successfully in project with ID %d", input.UserID, projectID),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			AffectedUserName:  input.FirstName + " " + input.LastName,
			AffectedUserEmail: input.Email,
			ProjectID:         projectID,
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Return a success response
		c.JSON(http.StatusOK, gin.H{
			"message": "User and project member updated successfully",
		})
	}
}

// GetMembers returns all members of a project.
// @Summary Get project members
// @Description Returns members for the given project_id. Requires Authorization header.
// @Tags Project Members
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} object "List of project members"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project/{project_id}/members [get]
func GetMembers(db *sql.DB) gin.HandlerFunc {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		log.Printf("Fetching members for project_id: %d", projectID)

		// Define the response struct
		type Member struct {
			ID            int       `json:"id"`
			ProjectID     int       `json:"project_id"`
			UserID        int       `json:"user_id"`
			RoleID        int       `json:"role_id"`
			CreatedAt     time.Time `json:"created_at"`
			UpdatedAt     time.Time `json:"updated_at"`
			EmployeeID    string    `json:"employee_id"`
			Email         string    `json:"email"`
			FirstName     string    `json:"first_name"`
			LastName      string    `json:"last_name"`
			ProfilePic    string    `json:"profile_picture"`
			IsAdmin       bool      `json:"is_admin"`
			Address       string    `json:"address"`
			City          string    `json:"city"`
			State         string    `json:"state"`
			Country       string    `json:"country"`
			ZipCode       string    `json:"zip_code"`
			PhoneNo       string    `json:"phone_no"`
			RoleName      string    `json:"role_name"`
			Suspend       bool      `json:"suspended"`
			PhoneCode     int       `json:"phone_code"`
			PhoneCodeName string    `json:"phone_code_name"`
		}

		var members []Member
		rows, err := db.Query(`
            SELECT 
                pm.id, pm.project_id, pm.user_id, pm.role_id, pm.created_at, pm.updated_at,
                u.employee_id, u.email, u.first_name, u.last_name, u.profile_picture, u.is_admin,
                u.address, u.city, u.state, u.country, u.zip_code, u.phone_no, u.suspended,
                r.role_name, u.phone_code, pc.phone_code
            FROM project_members pm
            JOIN users u ON u.id = pm.user_id
			JOIN phone_code pc ON u.phone_code = pc.id
            JOIN roles r ON r.role_id = pm.role_id
            WHERE pm.project_id = $1`, projectID)
		if err != nil {
			log.Printf("Query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch members"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var member Member
			if err := rows.Scan(
				&member.ID, &member.ProjectID, &member.UserID, &member.RoleID, &member.CreatedAt, &member.UpdatedAt,
				&member.EmployeeID, &member.Email, &member.FirstName, &member.LastName, &member.ProfilePic, &member.IsAdmin,
				&member.Address, &member.City, &member.State, &member.Country, &member.ZipCode, &member.PhoneNo, &member.Suspend,
				&member.RoleName, &member.PhoneCode, &member.PhoneCodeName); err != nil {
				log.Printf("Scan error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan user", "details": err.Error()})
				return
			}
			members = append(members, member)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Rows iteration error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error occurred during rows iteration"})
			return
		}

		if len(members) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No members found for the project"})
			return
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext: "Project Members",
			EventName:    "Get Project Members",
			Description:  fmt.Sprintf("Members fetched successfully in project with ID %d", projectID),
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

		c.JSON(http.StatusOK, members)
	}
}

// DeleteMember removes a member from a project.
// @Summary Delete project member
// @Description Removes user_id from project_id. Requires Authorization header.
// @Tags Project Members
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param user_id path int true "User ID to remove"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project/{project_id}/members/{user_id} [delete]
func DeleteMember(db *sql.DB) gin.HandlerFunc {
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

		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		userID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		// Delete user from project_members table
		_, err = db.Exec(`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove user from project"})
			return
		}

		// Fetch user details
		var firstName, lastName, email string
		err = db.QueryRow(`SELECT first_name, last_name, email FROM users WHERE id = $1`, userID).
			Scan(&firstName, &lastName, &email)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user details"})
			}
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who deleted the member
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Member removed from project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", projectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all client and end_client users
		sendProjectClientNotifications(db, projectID,
			fmt.Sprintf("Member removed from project: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/members", projectID))

		// Log the activity
		log := models.ActivityLog{
			EventContext:      "Project Members",
			EventName:         "Project Member Deleted",
			Description:       fmt.Sprintf("Member with ID %d deleted successfully in project with ID %d", userID, projectID),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			AffectedUserName:  firstName + " " + lastName,
			AffectedUserEmail: email,
			ProjectID:         projectID,
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "User removed from project successfully"})
	}
}

// ExportMembersPDF exports project members as PDF.
// @Summary Export project members PDF
// @Description Returns PDF file of project members for project_id. Requires Authorization header.
// @Tags Project Members
// @Accept json
// @Produce application/pdf
// @Param project_id path int true "Project ID"
// @Success 200 {file} file "PDF file"
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project_member_exports/{project_id} [get]
func ExportMembersPDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := strconv.Atoi(c.Param("project_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var projectName string
		err = db.QueryRow(`SELECT name FROM project WHERE project_id = $1`, projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		// Fetch members data (same query as in GetMembers)
		rows, err := db.Query(`
            SELECT 
                pm.id, pm.project_id, pm.user_id, pm.role_id, pm.created_at, pm.updated_at,
                u.employee_id, u.email, u.first_name, u.last_name, u.profile_picture, u.is_admin,
                u.address, u.city, u.state, u.country, u.zip_code, u.phone_no, u.suspended,
                r.role_name, u.phone_code, pc.phone_code
            FROM project_members pm
            JOIN users u ON u.id = pm.user_id
			JOIN phone_code pc ON u.phone_code = pc.id
            JOIN roles r ON r.role_id = pm.role_id
            WHERE pm.project_id = $1`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch members"})
			return
		}
		defer rows.Close()

		type Member struct {
			ID            int
			EmployeeID    string
			FirstName     string
			LastName      string
			Email         string
			PhoneCodeName string
			PhoneNo       string
			RoleName      string
			City          string
			Country       string
			Suspend       bool
		}

		var members []Member
		for rows.Next() {
			var m Member
			var projectID, userID, roleID int
			var createdAt, updatedAt time.Time
			var isAdmin bool
			var state, zip, address, profilePic string
			if err := rows.Scan(
				&projectID, &projectID, &userID, &roleID, &createdAt, &updatedAt,
				&m.EmployeeID, &m.Email, &m.FirstName, &m.LastName, &profilePic, &isAdmin,
				&address, &m.City, &state, &m.Country, &zip, &m.PhoneNo, &m.Suspend,
				&m.RoleName, &roleID, &m.PhoneCodeName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan data"})
				return
			}
			members = append(members, m)
		}

		if len(members) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No members found for this project"})
			return
		}

		// --- Generate PDF ---
		pdf := gofpdf.New("L", "mm", "A4", "") // Landscape mode
		pdf.AddPage()
		pdf.SetMargins(10, 10, 10)
		pdf.SetFont("Arial", "", 10)

		// --- Header ---
		pdf.SetFont("Arial", "B", 18)
		pdf.Cell(0, 10, fmt.Sprintf("Project Members Report (Project Name: %s)", projectName))
		pdf.Ln(12)

		pdf.SetFont("Arial", "", 11)
		pdf.Cell(0, 8, fmt.Sprintf("Generated On: %s", time.Now().Format("02-Jan-2006 15:04:05")))
		pdf.Ln(10)

		// --- Table Header ---
		pdf.SetFont("Arial", "B", 11)
		pdf.SetFillColor(230, 230, 230)

		headers := []string{"S.No", "Employee ID", "Name", "Email", "Phone", "Role", "City", "Country", "Status"}
		widths := []float64{15, 25, 40, 55, 35, 35, 25, 25, 25}

		for i, h := range headers {
			pdf.CellFormat(widths[i], 8, h, "1", 0, "C", true, 0, "")
		}
		pdf.Ln(-1)

		// --- Table Data ---
		pdf.SetFont("Arial", "", 10)
		for i, m := range members {
			status := "Active"
			if m.Suspend {
				status = "Suspended"
			}

			data := []string{
				fmt.Sprintf("%d", i+1),
				m.EmployeeID,
				m.FirstName + " " + m.LastName,
				m.Email,
				fmt.Sprintf("%s %s", m.PhoneCodeName, m.PhoneNo),
				m.RoleName,
				m.City,
				m.Country,
				status,
			}

			for j, val := range data {
				align := "L"
				if j == 0 || j == 8 {
					align = "C"
				}
				pdf.CellFormat(widths[j], 8, val, "1", 0, align, false, 0, "")
			}
			pdf.Ln(-1)
		}

		// --- Footer ---
		pdf.Ln(10)
		pdf.SetFont("Arial", "I", 9)
		pdf.Cell(0, 8, "This is a system-generated report. No signature required.")
		pdf.Ln(5)
		pdf.Cell(0, 8, fmt.Sprintf("Total Members: %d", len(members)))

		// --- Output ---
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=project_%d_members.pdf", projectID))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}
	}
}

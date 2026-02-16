package handlers

import (
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// GetUser retrieves user information by ID
// @Summary Get user by ID
// @Description Retrieve user information by ID
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "User ID"
// @Success 200 {object} models.UserResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/user_fetch/{id} [get]
func GetUser(db *sql.DB) gin.HandlerFunc {
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

		idStr := c.Param("id")
		if idStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required in the URL"})
			return
		}

		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		user, err := getUserByID(db, id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, user)

		log := models.ActivityLog{
			EventContext:      "User",
			EventName:         "GET",
			Description:       "GET user" + user.FirstName,
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  user.FirstName,
			AffectedUserEmail: user.Email,
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

func getUserByID(db *sql.DB, id int) (models.User, error) {
	var user models.User
	var firstAccess, lastAccess sql.NullTime
	var employeeID, profilePicture sql.NullString

	query := `
		SELECT 
			u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name, 
			u.created_at, u.updated_at, u.first_access, u.last_access, 
			u.profile_picture, u.is_admin, u.address, u.city, u.state, u.country, 
			u.zip_code, u.phone_no, u.role_id, r.role_name, u.phone_code, pc.phone_code
		FROM 
			users u
		JOIN roles r ON u.role_id = r.role_id
		JOIN phone_code pc ON u.phone_code = pc.id
		WHERE u.id = $1`
	err := db.QueryRow(query, id).Scan(
		&user.ID, &employeeID, &user.Email, &user.Password, &user.FirstName, &user.LastName, &user.CreatedAt, &user.UpdatedAt, &firstAccess, &lastAccess, &profilePicture, &user.IsAdmin, &user.Address, &user.City, &user.State, &user.Country, &user.ZipCode, &user.PhoneNo, &user.RoleID, &user.RoleName, &user.PhoneCode, &user.PhoneCodeName)

	if err != nil {
		return user, err
	}

	// Handle sql.NullTime for FirstAccess and LastAccess
	user.FirstAccess = firstAccess.Time
	if !firstAccess.Valid {
		user.FirstAccess = time.Time{} // Zero value of time.Time
	}

	user.LastAccess = lastAccess.Time
	if !lastAccess.Valid {
		user.LastAccess = time.Time{} // Zero value of time.Time
	}

	// Handle sql.NullString for EmployeeID
	if employeeID.Valid {
		user.EmployeeId = employeeID.String
	} else {
		user.EmployeeId = "" // Do not include EmployeeId if it is NULL
	}

	// Handle sql.NullString for ProfilePicture
	if profilePicture.Valid {
		user.ProfilePic = profilePicture.String
	} else {
		user.ProfilePic = "" // Set to empty string if NULL
	}

	return user, nil
}

// SuspendUser godoc
// @Summary      Suspend or unsuspend user
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "User ID"
// @Param        body  body  object  true  "suspended (bool)"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      403  {object}  object
// @Router       /api/users/{id}/suspend [put]
func SuspendUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Auth check (you already have this logic)
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

		// 2. Only allow superadmins to suspend users
		isAdmin, err := getUserIsAdmin(db, session.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check admin status"})
			return
		}
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only admins can suspend users"})
			return
		}

		// 3. Get target user ID
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		// 4. Parse suspension status from request body
		var req struct {
			Suspended bool `json:"suspended"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// 5. Update user's suspension status
		u, err := getUserByID(db, id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user: " + err.Error()})
			return
		}

		// Already suspended/unsuspended? Skip further steps.
		// (Optional: If you want to log even if status didn't change, you can remove this)
		if u.Suspended == req.Suspended {
			c.JSON(http.StatusOK, gin.H{"message": "User is already in requested suspension state"})
			return
		}

		// Update user and fetch log data
		affectedUser, err := suspendUser(db, id, req.Suspended)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user: " + err.Error()})
			return
		}

		// 6. If suspending, delete all sessions for that user
		if req.Suspended {
			if err := deleteAllSessionsForUser(db, id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete sessions", "details": err.Error()})
				return
			}
		}

		// 7. Activity logging (add to your function!)
		log := models.ActivityLog{
			EventContext:      "User",
			EventName:         "SUSPEND",
			Description:       "Set user " + affectedUser.FirstName + " suspension to " + strconv.FormatBool(req.Suspended),
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  affectedUser.FirstName,
			AffectedUserEmail: affectedUser.Email,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log activity"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "User suspension status updated",
			"suspended":     req.Suspended,
			"user_id":       id,
			"affected_user": u.FirstName + " " + u.LastName,
		})
	}
}

// Suspend a user and return their current data
func suspendUser(db *sql.DB, userID int, suspended bool) (models.User, error) {
	const updateQuery = `
		UPDATE users u
		SET suspended = $1
		WHERE u.id = $2
		RETURNING
			u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name,
			u.created_at, u.updated_at, u.first_access, u.last_access,
			u.profile_picture, u.is_admin, u.address, u.city, u.state, u.country,
			u.zip_code, u.phone_no, u.role_id, u.suspended
	` // Remove role_name, or JOIN roles if you really want it!

	var user models.User
	var (
		employeeID, profilePicture sql.NullString
		firstAccess, lastAccess    sql.NullTime
	)
	err := db.QueryRow(updateQuery, suspended, userID).Scan(
		&user.ID, &employeeID, &user.Email, &user.Password, &user.FirstName, &user.LastName,
		&user.CreatedAt, &user.UpdatedAt, &firstAccess, &lastAccess, &profilePicture,
		&user.IsAdmin, &user.Address, &user.City, &user.State, &user.Country,
		&user.ZipCode, &user.PhoneNo, &user.RoleID, &user.Suspended,
	)
	if err != nil {
		return models.User{}, err
	}

	// Handle nullable fields
	user.EmployeeId = employeeID.String
	user.ProfilePic = profilePicture.String
	user.FirstAccess = firstAccess.Time
	user.LastAccess = lastAccess.Time

	return user, nil
}

// Delete all sessions for a user
func deleteAllSessionsForUser(db *sql.DB, userID int) error {
	_, err := db.Exec(`DELETE FROM session WHERE user_id = $1`, userID)
	return err
}

// Check if requesting user is admin (implement as needed)
func getUserIsAdmin(db *sql.DB, userID int) (bool, error) {
	var roleID int

	// Step 1: Get the role_id of the user
	err := db.QueryRow(`SELECT role_id FROM users WHERE id = $1`, userID).Scan(&roleID)
	if err != nil {
		return false, err
	}

	// Step 2: Get the permission_id of "SuspendUser"
	var permissionID int
	err = db.QueryRow(`SELECT id FROM permissions WHERE permission_name = $1`, "SuspendUser").Scan(&permissionID)
	if err != nil {
		return false, err
	}

	// Step 3: Check if (roleID, permissionID) exists in role_permissions table
	var exists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM role_permissions 
			WHERE role_id = $1 AND permission_id = $2
		)`, roleID, permissionID).Scan(&exists)
	if err != nil {
		return false, err
	}

	// Step 4: Return result
	return exists, nil
}

// GetAllUsers retrieves all users in the system
// @Summary Get all users
// @Description Retrieve all users in the system
// @Tags Users
// @Accept json
// @Produce json
// @Success 200 {array} models.UserResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/users [get]

func GetAllUsers(db *sql.DB) gin.HandlerFunc {
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
		if err := db.QueryRow(`SELECT user_id FROM session WHERE session_id=$1`, sessionID).Scan(&userID); err != nil {
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

		/* ---------------- FILTERS ---------------- */
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
		filterRoleID := c.Query("role_id")
		projectID := c.Query("project_id")

		var conditions []string
		var args []interface{}
		argIndex := 1

		/* ---------------- ROLE ACCESS ---------------- */
		switch roleName {
		case "superadmin":
			conditions = append(conditions, "1=1")

		case "admin":
			conditions = append(conditions, fmt.Sprintf(`
				u.id IN (
					SELECT pm.user_id
					FROM project_members pm
					JOIN project p ON pm.project_id=p.project_id
					JOIN end_client ec ON p.client_id=ec.id
					JOIN client cl ON ec.client_id=cl.client_id
					WHERE cl.user_id=$%d AND p.suspend=false
				)
			`, argIndex))
			args = append(args, userID)
			argIndex++

		default:
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		/* ---------------- ADVANCED SEARCH ---------------- */

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

		if filterRoleID != "" {
			conditions = append(conditions, fmt.Sprintf("u.role_id = $%d", argIndex))
			args = append(args, filterRoleID)
			argIndex++
		}

		if projectID != "" {
			conditions = append(conditions, fmt.Sprintf(`
				u.id IN (
					SELECT pm.user_id FROM project_members pm
					WHERE pm.project_id = $%d
				)
			`, argIndex))
			args = append(args, projectID)
			argIndex++
		}

		whereClause := strings.Join(conditions, " AND ")

		/* ---------------- COUNT ---------------- */
		var total int
		countQuery := fmt.Sprintf(`
			SELECT COUNT(DISTINCT u.id)
			FROM users u
			WHERE %s
		`, whereClause)

		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		/* ---------------- MAIN QUERY ---------------- */
		query := fmt.Sprintf(`
			SELECT 
				u.id, u.employee_id, u.email, u.first_name, u.last_name,
				u.created_at, u.updated_at, u.first_access, u.last_access,
				u.profile_picture, u.is_admin, u.address, u.city, u.state,
				u.country, u.zip_code, u.phone_no, u.role_id, r.role_name,
				u.phone_code, pc.phone_code,
				COALESCE(array_agg(DISTINCT p.name) FILTER (WHERE p.name IS NOT NULL), '{}') AS project_names
			FROM users u
			JOIN roles r ON u.role_id=r.role_id
			JOIN phone_code pc ON u.phone_code=pc.id
			LEFT JOIN project_members pm ON u.id=pm.user_id
			LEFT JOIN project p ON pm.project_id=p.project_id
			WHERE %s
			GROUP BY u.id, r.role_name, pc.phone_code
			ORDER BY u.created_at DESC
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

		type UserResponse struct {
			ID           int       `json:"id"`
			EmployeeId   string    `json:"employee_id"`
			Email        string    `json:"email"`
			FirstName    string    `json:"first_name"`
			LastName     string    `json:"last_name"`
			CreatedAt    time.Time `json:"created_at"`
			UpdatedAt    time.Time `json:"updated_at"`
			IsAdmin      bool      `json:"is_admin"`
			Address      string    `json:"address"`
			City         string    `json:"city"`
			State        string    `json:"state"`
			Country      string    `json:"country"`
			ZipCode      string    `json:"zip_code"`
			PhoneNo      string    `json:"phone_no"`
			RoleID       int       `json:"role_id"`
			RoleName     string    `json:"role_name"`
			ProjectNames []string  `json:"project_names"`
		}

		var users []UserResponse
		for rows.Next() {
			var u UserResponse
			var emp sql.NullString
			var projects pq.StringArray

			rows.Scan(
				&u.ID, &emp, &u.Email, &u.FirstName, &u.LastName,
				&u.CreatedAt, &u.UpdatedAt, new(sql.NullTime), new(sql.NullTime),
				new(sql.NullString), &u.IsAdmin, &u.Address, &u.City, &u.State,
				&u.Country, &u.ZipCode, &u.PhoneNo, &u.RoleID, &u.RoleName,
				new(int), new(string), &projects,
			)

			if emp.Valid {
				u.EmployeeId = emp.String
			}
			u.ProjectNames = projects
			users = append(users, u)
		}

		/* ---------------- RESPONSE ---------------- */
		response := gin.H{
			"data": users,
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

		/* ---------------- LOG ---------------- */
		go SaveActivityLog(db, models.ActivityLog{
			EventContext: "User",
			EventName:    "Search",
			Description:  "Fetched users with pagination & filters",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		})
	}
}

// func GetAllUsers(db *sql.DB) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		sessionID := c.GetHeader("Authorization")
// 		if sessionID == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
// 			return
// 		}

// 		session, userName, err := GetSessionDetails(db, sessionID)
// 		if err != nil {
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
// 			return
// 		}

// 		// Step 2: Get user_id from session
// 		var userID int
// 		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
// 		if err != nil {
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
// 			return
// 		}

// 		// --- fetch logged-in user role ---
// 		// Step 3: Get role_id from users table
// 		var roleID int
// 		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role_id"})
// 			return
// 		}

// 		// Step 4: Get role name from roles table
// 		var roleName string
// 		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get role name"})
// 			return
// 		}

// 		// --- role based filter ---
// 		var rows *sql.Rows
// 		switch roleName {
// 		case "superadmin":
// 			// superadmin: fetch all users with their projects
// 			query := `
// 				SELECT
// 					u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name,
// 					u.created_at, u.updated_at, u.first_access, u.last_access,
// 					u.profile_picture, u.is_admin, u.address, u.city, u.state, u.country,
// 					u.zip_code, u.phone_no, u.role_id, r.role_name, u.phone_code, pc.phone_code,
// 					COALESCE(array_agg(DISTINCT p.name) FILTER (WHERE p.name IS NOT NULL), '{}') AS project_names
// 				FROM users u
// 				JOIN roles r ON u.role_id = r.role_id
// 				JOIN phone_code pc ON u.phone_code = pc.id
// 				LEFT JOIN project_members pm ON u.id = pm.user_id
// 				LEFT JOIN project p ON pm.project_id = p.project_id AND p.suspend = false
// 				GROUP BY u.id, r.role_name, pc.phone_code
// 			`
// 			rows, err = db.Query(query)
// 		case "admin":
// 			// admin: fetch only its project members
// 			query := `
// 				SELECT
// 					u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name,
// 					u.created_at, u.updated_at, u.first_access, u.last_access,
// 					u.profile_picture, u.is_admin, u.address, u.city, u.state, u.country,
// 					u.zip_code, u.phone_no, u.role_id, r.role_name, u.phone_code, pc.phone_code,
// 					COALESCE(array_agg(DISTINCT p.name) FILTER (WHERE p.name IS NOT NULL), '{}') AS project_names
// 				FROM users u
// 				JOIN roles r ON u.role_id = r.role_id
// 				JOIN phone_code pc ON u.phone_code = pc.id
// 				LEFT JOIN project_members pm ON u.id = pm.user_id
// 				LEFT JOIN project p ON pm.project_id = p.project_id
// 				WHERE pm.project_id IN (
// 					SELECT p.project_id
// 					FROM project p
// 					JOIN end_client ec ON p.client_id = ec.id
// 					JOIN client cl ON ec.client_id = cl.client_id
// 					WHERE cl.user_id = $1 AND p.suspend = false
// 					UNION
// 					SELECT cl.user_id AS user_id, p.name AS project_name
//     			FROM project p
//     			JOIN end_client ec ON p.client_id = ec.id
//     			JOIN client cl ON ec.client_id = cl.client_id
//     			WHERE p.suspend = false
// 				)  proj ON proj.user_id = u.id
// 				GROUP BY u.id, r.role_name, pc.phone_code
// 			`
// 			rows, err = db.Query(query, userID)
// 		default:
// 			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
// 			return
// 		}

// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve users", "details": err.Error()})
// 			return
// 		}
// 		defer rows.Close()

// 		// --- response struct ---
// 		type UserResponse struct {
// 			ID            int       `json:"id"`
// 			EmployeeId    string    `json:"employee_id"`
// 			Email         string    `json:"email"`
// 			FirstName     string    `json:"first_name"`
// 			LastName      string    `json:"last_name"`
// 			CreatedAt     time.Time `json:"created_at"`
// 			UpdatedAt     time.Time `json:"updated_at"`
// 			FirstAccess   time.Time `json:"first_access,omitempty"`
// 			LastAccess    time.Time `json:"last_access,omitempty"`
// 			ProfilePic    string    `json:"profile_pic"`
// 			IsAdmin       bool      `json:"is_admin"`
// 			Address       string    `json:"address"`
// 			City          string    `json:"city"`
// 			State         string    `json:"state"`
// 			Country       string    `json:"country"`
// 			ZipCode       string    `json:"zip_code"`
// 			PhoneNo       string    `json:"phone_no"`
// 			RoleID        int       `json:"role_id"`
// 			RoleName      string    `json:"role_name"`
// 			PhoneCode     int       `json:"phone_code"`
// 			PhoneCodeName string    `json:"phone_code_name"`
// 			ProjectNames  []string  `json:"project_names"`
// 		}

// 		var users []UserResponse
// 		for rows.Next() {
// 			var u UserResponse
// 			var firstAccess, lastAccess sql.NullTime
// 			var employeeID, profilePicture sql.NullString
// 			var projectNames pq.StringArray // PostgreSQL string array

// 			err := rows.Scan(
// 				&u.ID, &employeeID, &u.Email, new(string), &u.FirstName, &u.LastName, &u.CreatedAt, &u.UpdatedAt,
// 				&firstAccess, &lastAccess, &profilePicture, &u.IsAdmin, &u.Address, &u.City, &u.State, &u.Country,
// 				&u.ZipCode, &u.PhoneNo, &u.RoleID, &u.RoleName, &u.PhoneCode, &u.PhoneCodeName, &projectNames,
// 			)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan user", "details": err.Error()})
// 				return
// 			}

// 			// null handling
// 			if employeeID.Valid {
// 				u.EmployeeId = employeeID.String
// 			}
// 			if profilePicture.Valid {
// 				u.ProfilePic = profilePicture.String
// 			}
// 			if firstAccess.Valid {
// 				u.FirstAccess = firstAccess.Time
// 			}
// 			if lastAccess.Valid {
// 				u.LastAccess = lastAccess.Time
// 			}
// 			u.ProjectNames = projectNames

// 			users = append(users, u)
// 		}

// 		if err = rows.Err(); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "Row iteration error", "details": err.Error()})
// 			return
// 		}

// 		c.JSON(http.StatusOK, users)

// 		// log
// 		go func() {
// 			_ = SaveActivityLog(db, models.ActivityLog{
// 				EventContext: "User",
// 				EventName:    "GET",
// 				Description:  fmt.Sprintf("Fetched users by role %s", roleName),
// 				UserName:     userName,
// 				HostName:     session.HostName,
// 				IPAddress:    session.IPAddress,
// 				CreatedAt:    time.Now(),
// 				ProjectID:    0,
// 			})
// 		}()
// 	}
// }

// GetUserFromSession retrieves current user information from session
// @Summary Get user from session
// @Description Get current user information from session
// @Tags Users
// @Accept json
// @Produce json
// @Success 200 {object} models.UserResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/get_user [get]

func GetUserFromSession(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		// Read the session ID from the Authorization header
		sessionID := c.GetHeader("Authorization")

		// Ensure the session ID is not empty
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID in Authorization header"})
			return
		}

		// Retrieve the UserID from the Session table
		var userID int
		sessionQuery := `SELECT user_id FROM session WHERE session_id = $1`
		err := db.QueryRow(sessionQuery, sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Fetch the user details
		var user models.User
		var firstAccess, lastAccess sql.NullTime
		var employeeID, profilePicture sql.NullString // To handle employee_id being NULL

		userQuery := `SELECT 
			u.id, u.employee_id, u.email, u.password, u.first_name, u.last_name, 
			u.created_at, u.updated_at, u.first_access, u.last_access, 
			u.profile_picture, u.is_admin, u.address, u.city, u.state, 
			u.country, u.zip_code, u.phone_no, u.role_id, r.role_name, u.phone_code, pc.phone_code
		FROM 
			users u
		JOIN roles r ON u.role_id = r.role_id
		JOIN phone_code pc ON u.phone_code = pc.id
		WHERE u.id = $1`

		err = db.QueryRow(userQuery, userID).Scan(
			&user.ID,
			&employeeID,
			&user.Email,
			&user.Password,
			&user.FirstName,
			&user.LastName,
			&user.CreatedAt,
			&user.UpdatedAt,
			&firstAccess,
			&lastAccess,
			&profilePicture,
			&user.IsAdmin,
			&user.Address,
			&user.City,
			&user.State,
			&user.Country,
			&user.ZipCode,
			&user.PhoneNo,
			&user.RoleID,
			&user.RoleName,
			&user.PhoneCode,
			&user.PhoneCodeName,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Handle sql.NullTime for FirstAccess and LastAccess
		user.FirstAccess = firstAccess.Time
		if !firstAccess.Valid {
			user.FirstAccess = time.Time{} // Zero value of time.Time
		}

		user.LastAccess = lastAccess.Time
		if !lastAccess.Valid {
			user.LastAccess = time.Time{} // Zero value of time.Time
		}

		// Handle sql.NullString for EmployeeID
		if employeeID.Valid {
			user.EmployeeId = employeeID.String
		} else {
			user.EmployeeId = "" // Do not include EmployeeId if it is NULL
		}

		// Handle sql.NullString for ProfilePicture
		if profilePicture.Valid {
			user.ProfilePic = profilePicture.String
		} else {
			user.ProfilePic = "" // Set to empty string if NULL
		}

		caps := models.ProjectCapabilities{}

		if strings.EqualFold(user.RoleName, "superadmin") {
			caps.HRA = true
			caps.WorkOrder = true
			caps.Invoice = true
			caps.Calculator = true

			c.JSON(http.StatusOK, gin.H{
				"user":         user,
				"capabilities": caps,
			})
			return
		}

		if strings.EqualFold(user.RoleName, "admin") {

			err := db.QueryRow(`
		SELECT
			COALESCE(BOOL_OR(p.hra), false),
			COALESCE(BOOL_OR(p.work_order), false),
			COALESCE(BOOL_OR(p.invoice), false),
			COALESCE(BOOL_OR(p.calculator), false)
		FROM project p
		JOIN end_client ec ON p.client_id = ec.id
		JOIN client c ON ec.client_id = c.client_id
		WHERE c.user_id = $1
	`, user.ID).Scan(&caps.HRA, &caps.WorkOrder, &caps.Invoice, &caps.Calculator)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"user":         user,
				"capabilities": caps,
			})
			return
		}

		permIDs := map[string]int{}

		rows, err := db.Query(`SELECT permission_id, permission_name FROM permissions`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var name string
			rows.Scan(&id, &name)
			key := strings.ToLower(strings.ReplaceAll(name, "_", ""))
			permIDs[key] = id

		}

		projectRows, err := db.Query(`
	SELECT DISTINCT project_id
	FROM project_roles
	WHERE role_id = $1
`, user.RoleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer projectRows.Close()

		for projectRows.Next() {
			var projectID int
			projectRows.Scan(&projectID)

			isMember, err := IsProjectMember(db, user.ID, projectID)
			log.Println("member check:", isMember, "err:", err)
			if err != nil || !isMember {
				continue
			}

			// ---- HRA ----
			if ok, _ := HasProjectPermission(db, user.ID, projectID, permIDs["hra"]); ok {
				var flag bool
				db.QueryRow(`SELECT hra FROM project WHERE project_id=$1`, projectID).Scan(&flag)
				log.Println("   project hra flag:", flag)

				if flag {
					caps.HRA = true
				}
			}

			// ---- WORK ORDER ----
			if ok, _ := HasProjectPermission(db, user.ID, projectID, permIDs["workorder"]); ok {
				var flag bool
				db.QueryRow(`SELECT work_order FROM project WHERE project_id=$1`, projectID).Scan(&flag)
				log.Println("   project work_order flag:", flag)

				if flag {
					caps.WorkOrder = true
				}
			}

			// ---- INVOICE ----
			if ok, _ := HasProjectPermission(db, user.ID, projectID, permIDs["invoice"]); ok {
				var flag bool
				db.QueryRow(`SELECT invoice FROM project WHERE project_id=$1`, projectID).Scan(&flag)
				log.Println("   project invoice flag:", flag)
				if flag {
					caps.Invoice = true
				}
			}

			// ---- CALCULATOR ----
			if ok, _ := HasProjectPermission(db, user.ID, projectID, permIDs["calculator"]); ok {
				var flag bool
				db.QueryRow(`SELECT calculator FROM project WHERE project_id=$1`, projectID).Scan(&flag)
				log.Println("   project calculator flag:", flag)
				if flag {
					caps.Calculator = true
				}
			}

			// short-circuit if all true
			if caps.HRA && caps.WorkOrder && caps.Invoice && caps.Calculator {
				log.Println("✅ all capabilities true — breaking early")
				break
			}
		}

		// Return the user details
		c.JSON(http.StatusOK, gin.H{
			"user":         user,
			"capabilities": caps,
		})

	}
}

func HasProjectPermission(db *sql.DB, userID int, projectID int, permissionID int) (bool, error) {
	var roleID int
	query := `
	SELECT role_id
	FROM users
	WHERE id = $1`
	err := db.QueryRow(query, userID).Scan(&roleID)
	if err != nil {
		log.Println("Error fetching role_id for user:", err)
		return false, err
	}
	var count int

	query = `
		SELECT COUNT(*)
		FROM project_roles pr
		JOIN role_permissions rp ON pr.role_id = rp.role_id
		WHERE pr.project_id = $1
		  AND rp.permission_id = $2
		  AND pr.role_id = $3
	`

	err = db.QueryRow(query, projectID, permissionID, roleID).Scan(&count)

	log.Println(
		"Permission check:",
		"userID =", userID,
		"roleID =", roleID,
		"projectID =", projectID,
		"permissionID =", permissionID,
		"count =", count,
		"err =", err,
	)

	return count > 0, err
}

func IsProjectMember(db *sql.DB, userID int, projectID int) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM project_members WHERE user_id = $1 AND project_id = $2`
	err := db.QueryRow(query, userID, projectID).Scan(&count)
	return count > 0, err
}

// CreateUser creates a new user account
// @Summary Create user
// @Description Create a new user account
// @Tags Users
// @Accept json
// @Produce json
// @Param request body models.CreateUserRequest true "User creation request"
// @Success 201 {object} models.UserResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/create_user [post]
func CreateUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session ID
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

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session. Session ID not found."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching session: " + err.Error()})
			}
			return
		}

		// Fetch the user's role_id and is_admin status
		var roleID int
		var isAdmin bool
		err = db.QueryRow("SELECT role_id, is_admin FROM users WHERE id = $1", userID).Scan(&roleID, &isAdmin)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Check if the current user is an admin
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only admins can create users"})
			return
		}

		var currentUserRole string
		err = db.QueryRow("SELECT role_name FROM roles where role_id = $1", roleID).Scan(&currentUserRole)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name: " + err.Error()})
			return
		}

		// Parse request body for the new user
		var user models.User
		if err := c.ShouldBindJSON(&user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if user already exists (by email or employee ID)
		var existingUserID int
		err = db.QueryRow(
			"SELECT id FROM users WHERE email = $1 OR employee_id = $2",
			user.Email, user.EmployeeId,
		).Scan(&existingUserID)

		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
			return
		}

		if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
			return
		}

		// Set timestamps
		user.CreatedAt = time.Now()
		user.UpdatedAt = user.CreatedAt
		user.FirstAccess = time.Now()
		user.LastAccess = user.CreatedAt

		// Insert new user into the database
		sqlStatement := `
			INSERT INTO users (employee_id, email, password, first_name, last_name, created_at, updated_at, first_access, last_access, profile_picture, is_admin, address, city, state, country, zip_code, phone_no, role_id, phone_code)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
			RETURNING id`

		err = db.QueryRow(
			sqlStatement,
			user.EmployeeId, user.Email, user.Password, user.FirstName, user.LastName,
			user.CreatedAt, user.UpdatedAt, user.FirstAccess, user.LastAccess, user.ProfilePic,
			user.IsAdmin, user.Address, user.City, user.State, user.Country,
			user.ZipCode, user.PhoneNo, user.RoleID, user.PhoneCode,
		).Scan(&user.ID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user: " + err.Error()})
			return
		}

		var roleName string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", user.RoleID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch role name: " + err.Error()})
			return
		}

		// Send confirmation email (optional, handle errors gracefully)
		if err = SendEmail(user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "User created, but failed to send email: " + err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("New user %s %s (%s) created", user.FirstName, user.LastName, roleName),
			Status:    "unread",
			Action:    fmt.Sprintf("/api/user_fetch/%d", user.ID), // example route for frontend
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

		// Success response
		c.JSON(http.StatusCreated, gin.H{
			"message": "User created successfully",
			"user_id": user.ID,
		})

		log := models.ActivityLog{
			EventContext:      "User",
			EventName:         "Create",
			Description:       "Create User" + user.FirstName + user.LastName,
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  user.FirstName + user.LastName,
			AffectedUserEmail: user.Email,
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

// UpdateUser updates user information
// @Summary Update user
// @Description Update user information
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "User ID"
// @Param request body models.UpdateUserRequest true "User update request"
// @Success 200 {object} models.UserResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/update_user/{id} [put]
func UpdateUser(db *sql.DB) gin.HandlerFunc {
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

		var user models.User
		if err := c.ShouldBindJSON(&user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Parse user ID from the URL
		userIDStr := c.Param("id")
		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		// Check if the user exists
		var existingUserID int
		err = db.QueryRow("SELECT id FROM users WHERE id = $1", userID).Scan(&existingUserID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var updates []string
		var fields []interface{}
		placeholderIndex := 1

		// Other fields can be updated by any authorized user
		if user.EmployeeId != "" {
			updates = append(updates, fmt.Sprintf("employee_id = $%d", placeholderIndex))
			fields = append(fields, user.EmployeeId)
			placeholderIndex++
		}
		if user.Email != "" {
			updates = append(updates, fmt.Sprintf("email = $%d", placeholderIndex))
			fields = append(fields, user.Email)
			placeholderIndex++
		}
		if user.Password != "" {
			updates = append(updates, fmt.Sprintf("password = $%d", placeholderIndex))
			fields = append(fields, user.Password)
			placeholderIndex++
		}
		if user.FirstName != "" {
			updates = append(updates, fmt.Sprintf("first_name = $%d", placeholderIndex))
			fields = append(fields, user.FirstName)
			placeholderIndex++
		}
		if user.LastName != "" {
			updates = append(updates, fmt.Sprintf("last_name = $%d", placeholderIndex))
			fields = append(fields, user.LastName)
			placeholderIndex++
		}
		if user.ProfilePic != "" {
			updates = append(updates, fmt.Sprintf("profile_picture = $%d", placeholderIndex))
			fields = append(fields, user.ProfilePic)
			placeholderIndex++
		}
		if user.ZipCode != "" {
			updates = append(updates, fmt.Sprintf("zip_code = $%d", placeholderIndex))
			fields = append(fields, user.ZipCode)
			placeholderIndex++
		}
		if user.PhoneNo != "" {
			updates = append(updates, fmt.Sprintf("phone_no = $%d", placeholderIndex))
			fields = append(fields, user.PhoneNo)
			placeholderIndex++
		}
		if user.State != "" {
			updates = append(updates, fmt.Sprintf("state = $%d", placeholderIndex))
			fields = append(fields, user.State)
			placeholderIndex++
		}
		if user.Address != "" {
			updates = append(updates, fmt.Sprintf("address = $%d", placeholderIndex))
			fields = append(fields, user.Address)
			placeholderIndex++
		}
		if user.IsAdmin {
			updates = append(updates, fmt.Sprintf("is_admin = $%d", placeholderIndex))
			fields = append(fields, user.IsAdmin)
			placeholderIndex++
		}
		if user.City != "" {
			updates = append(updates, fmt.Sprintf("city = $%d", placeholderIndex))
			fields = append(fields, user.City)
			placeholderIndex++
		}
		if user.Country != "" {
			updates = append(updates, fmt.Sprintf("country = $%d", placeholderIndex))
			fields = append(fields, user.Country)
			placeholderIndex++
		}

		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
			return
		}

		sqlStatement := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(updates, ", "), placeholderIndex)
		fields = append(fields, userID)

		_, err = db.Exec(sqlStatement, fields...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Create database notification for the admin
		notif := models.Notification{
			UserID:    userID,
			Message:   fmt.Sprintf("User %s %s updated", user.FirstName, user.LastName),
			Status:    "unread",
			Action:    fmt.Sprintf("/api/user_fetch/%d", user.ID), // example route for frontend
			UpdatedAt: time.Now(),
		}

		_, err = db.Exec(`
		INSERT INTO notifications (user_id, message, status, action, updated_at)
		VALUES ($1, $2, $3, $4, $6)
		`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.UpdatedAt)

		if err != nil {
			log.Printf("Failed to insert notification: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})

		log := models.ActivityLog{
			EventContext:      "User",
			EventName:         "Update",
			Description:       "Update User" + user.FirstName + user.LastName,
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  user.FirstName + user.LastName,
			AffectedUserEmail: user.Email,
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

func SendEmail(user models.User) error {
	// For now, we'll use the old method if template system is not available
	// In a full implementation, you would inject the email template handler here
	// and use the template system to send email
	return SendEmailLegacy(user)
}

// SendEmailLegacy is the original email sending function
func SendEmailLegacy(user models.User) error {
	auth := smtp.PlainAuth(
		"",
		"om.s@blueinvent.com",
		"gloycbfukxdyeczj",
		"smtp.gmail.com",
	)

	from := "vasug7409@gmail.com"
	to := []string{user.Email}
	subject := "Welcome to Our Platform!"

	role := "User"
	if user.IsAdmin {
		role = "Admin"
	}
	body := fmt.Sprintf("Hello %s,\n\nYour account has been created successfully.\n\nHere are your credentials:\n\nPassword: %s\nRole: %s\n\nPlease change your password after logging in for the first time.\n\nBest Regards,\nYour Company",
		user.FirstName, user.Password, role)

	msg := []byte("From: " + from + "\r\n" +
		"To: " + user.Email + "\r\n" +
		"Subject: " + subject + "\r\n\r\n" +
		body + "\r\n")

	err := smtp.SendMail(
		"smtp.gmail.com:587",
		auth,
		from,
		to,
		msg,
	)

	return err
}

// DeleteUser deletes a user account
// @Summary Delete user
// @Description Delete a user account
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "User ID"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/user_delete/{id} [delete]
func DeleteUser(db *sql.DB) gin.HandlerFunc {
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

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		// Get user details before deletion for notification
		var user models.User
		err = db.QueryRow(`
			SELECT u.id, u.first_name, u.last_name, u.email, u.role_id, r.role_name
			FROM users u
			JOIN roles r ON u.role_id = r.role_id
			WHERE u.id = $1`, id).Scan(
			&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.RoleID, &user.RoleName)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user details"})
			}
			return
		}

		err = deleteUserFromDB(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("User with ID %d successfully deleted", id)})

		log := models.ActivityLog{
			EventContext:      "User",
			EventName:         "Delete",
			Description:       "Delete user" + user.FirstName + user.LastName,
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  user.FirstName + user.LastName,
			AffectedUserEmail: user.Email,
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

func deleteUserFromDB(id int) error {
	db := storage.GetDB()
	query := "DELETE FROM users WHERE id = $1"
	_, err := db.Exec(query, id)
	return err
}

// SuspendClient godoc
// @Summary      Suspend or unsuspend client
// @Tags         clients
// @Accept       json
// @Produce      json
// @Param        client_id  path  int  true  "Client ID"
// @Param        body       body  object  true  "suspended (bool)"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      403  {object}  object
// @Router       /api/client/{client_id}/suspend [put]
func SuspendClient(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Auth check
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

		// Check if user is superadmin
		var roleID int
		err = db.QueryRow(`SELECT role_id FROM users WHERE id = $1`, session.UserID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user role"})
			return
		}
		if roleID != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized user"})
			return
		}

		// 2. Parse client ID
		clientIDStr := c.Param("client_id")
		clientID, err := strconv.Atoi(clientIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		log.Print(clientID)
		var user_id int
		err = db.QueryRow(`SELECT user_id FROM client WHERE client_id = $1`, clientID).Scan(&user_id)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// 3. Parse JSON body to get suspend flag
		var request struct {
			Suspend bool `json:"suspend"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body"})
			return
		}

		_, err = db.Exec(`UPDATE users SET project_suspend = $1 WHERE id = $2`, request.Suspend, user_id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update client suspension status"})
			return
		}

		// 5. Fetch project IDs for this client
		rows, err := db.Query(`SELECT project_id FROM project WHERE client_id = $1`, clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects of the client"})
			return
		}
		defer rows.Close()

		var projectIDs []int
		for rows.Next() {
			var pid int
			if err := rows.Scan(&pid); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading project ID"})
				return
			}
			projectIDs = append(projectIDs, pid)
		}

		if len(projectIDs) == 0 {
			action := "suspended"
			if !request.Suspend {
				action = "unsuspended"
			}
			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Client %s. No associated projects found.", action)})
			return
		}

		projectSuspend := "Onhold"
		if !request.Suspend {
			projectSuspend = "Ongoing"
		}

		_, err = db.Exec(`UPDATE project SET suspend = $1, project_status = $2  WHERE project_id = ANY($3)`, request.Suspend, projectSuspend, pq.Array(projectIDs))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project suspension status", "details": err.Error()})
			return
		}

		// 6. Get all user IDs from project_members for the projects
		userRows, err := db.Query(`
			SELECT DISTINCT user_id 
			FROM project_members 
			WHERE project_id = ANY($1)`, pq.Array(projectIDs))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users for projects"})
			return
		}
		defer userRows.Close()

		var userIDs []int
		for userRows.Next() {
			var uid int
			if err := userRows.Scan(&uid); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading user ID"})
				return
			}
			userIDs = append(userIDs, uid)
		}

		// 7. Update project_suspend for users
		if len(userIDs) > 0 {
			_, err = db.Exec(`UPDATE users SET project_suspend = $1 WHERE id = ANY($2)`, request.Suspend, pq.Array(userIDs))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user suspension status"})
				return
			}
		}

		// 9. Delete all sessions of users if suspending
		if request.Suspend {
			for _, uid := range userIDs {
				if err := deleteAllSessionsForUser(db, uid); err != nil {
					// Log but don't block the flow
					fmt.Printf("Failed to delete sessions for user %d: %v\n", uid, err)
				}
			}
		}

		action := "suspended"
		if !request.Suspend {
			action = "unsuspended"
		}

		c.JSON(http.StatusOK, gin.H{
			"message": fmt.Sprintf("Client and associated users %s successfully", action),
			"users":   userIDs,
			"user":    userName,
		})

		log := models.ActivityLog{
			EventContext: "Client",
			EventName:    "PUT",
			Description:  fmt.Sprintf("Suspend Client %d", clientID),
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

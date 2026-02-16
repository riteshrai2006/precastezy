package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func ValidateProjectInput(project *models.Project) error {
	if project.Name == "" ||
		project.Priority == "" ||
		project.ProjectStatus == "" ||
		project.StartDate.IsZero() ||
		project.EndDate.IsZero() ||
		project.ClientId == 0 ||
		project.Budget == "" {
		return errors.New("required fields cannot be null or empty")
	}

	validStatuses := map[string]bool{
		"Completed": true,
		"Inactive":  true,
		"Ongoing":   true,
		"Cancelled": true,
		"Critical":  true,
	}

	if !validStatuses[project.ProjectStatus] {
		return errors.New("invalid project status; allowed values are Completed, Inactive, Ongoing, Cancelled, Critical")
	}

	return nil
}

// GetProjectsByRole godoc
// @Summary      Get projects by current user role
// @Tags         projects
// @Param        filter  query     string  false  "Filter (hra, workorder, invoice)"
// @Success      200     {array}   object
// @Failure      400     {object}  models.ErrorResponse
// @Failure      401     {object}  models.ErrorResponse
// @Router       /api/project_by_role [get]
func GetProjectsByRole(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session"})
			return
		}

		var userID int
		err := db.QueryRow(
			`SELECT user_id FROM session WHERE session_id=$1`,
			sessionID,
		).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		var roleID int
		var roleName string
		err = db.QueryRow(`
			SELECT u.role_id, r.role_name
			FROM users u
			JOIN roles r ON u.role_id = r.role_id
			WHERE u.id = $1
		`, userID).Scan(&roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		filter := strings.ToLower(c.Query("filter")) // hra, workorder, invoice

		var projects []gin.H

		// ====================================================
		// ✅ SUPERADMIN → all projects
		// ====================================================

		if strings.EqualFold(roleName, "superadmin") {
			projects := []gin.H{}

			rows, err := db.Query(`SELECT project_id, name FROM project`)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			defer rows.Close()

			for rows.Next() {
				var id int
				var name string
				rows.Scan(&id, &name)
				projects = append(projects, gin.H{
					"project_id": id,
					"name":       name,
				})
			}

			c.JSON(http.StatusOK, projects)
			return
		}

		// ====================================================
		// ✅ ADMIN → only client projects + boolean filter
		// ====================================================

		if strings.EqualFold(roleName, "admin") {
			projects := []gin.H{}

			column := filter // hra, workorder, invoice
			if column == "workorder" {
				column = "work_order"
			}

			query := fmt.Sprintf(`
				SELECT p.project_id, p.name
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
				JOIN client c ON ec.client_id = c.client_id
				WHERE c.user_id = $1
				  AND p.%s = true
			`, column)

			rows, err := db.Query(query, userID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			defer rows.Close()

			for rows.Next() {
				var id int
				var name string
				rows.Scan(&id, &name)
				projects = append(projects, gin.H{
					"project_id": id,
					"name":       name,
				})
			}

			c.JSON(http.StatusOK, projects)
			return
		}

		// ====================================================
		// ✅ OTHER ROLES → member + permission + boolean
		// ====================================================

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
		`, roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer projectRows.Close()

		for projectRows.Next() {
			projects := []gin.H{}
			var projectID int
			projectRows.Scan(&projectID)

			isMember, _ := IsProjectMember(db, userID, projectID)
			if !isMember {
				continue
			}

			permID := permIDs[filter]

			ok, _ := HasProjectPermission(db, userID, projectID, permID)
			if !ok {
				continue
			}

			column := filter
			if column == "workorder" {
				column = "work_order"
			}

			var enabled bool
			query := fmt.Sprintf(`SELECT %s FROM project WHERE project_id=$1`, column)
			db.QueryRow(query, projectID).Scan(&enabled)

			if !enabled {
				continue
			}

			var name string
			db.QueryRow(`SELECT name FROM project WHERE project_id=$1`, projectID).Scan(&name)

			projects = append(projects, gin.H{
				"project_id": projectID,
				"name":       name,
			})
		}

		c.JSON(http.StatusOK, projects)
	}
}

// GetProject godoc
// @Summary      Get project by ID (detailed)
// @Tags         projects
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Router       /api/project_get/{project_id} [get]
func GetProject(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var project models.ResponseProject

		// Get only core project details
		query := `
			SELECT 
				project_id, name, priority, project_status, start_date, end_date, logo, 
				description, created_at, updated_at, last_updated_at, last_updated_by, 
				client_id, budget, template_id, subscription_start_date, subscription_end_date, abbreviation
			FROM project
			WHERE project_id = $1
		`
		err = db.QueryRow(query, projectID).Scan(
			&project.ProjectId,
			&project.Name,
			&project.Priority,
			&project.ProjectStatus,
			&project.StartDate,
			&project.EndDate,
			&project.Logo,
			&project.Description,
			&project.CreatedAt,
			&project.UpdatedAt,
			&project.LastUpdated,
			&project.LastUpdatedBy,
			&project.ClientId,
			&project.Budget,
			&project.TemplateID,
			&project.SubscriptionStartDate,
			&project.SubscriptionEndDate,
			&project.Abbreviation,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project", "details": err.Error()})
			}
			return
		}

		// Fetch stockyards (id + name)
		rows, err := db.Query(`
			SELECT s.id, s.yard_name
		FROM stockyard s
		JOIN project_stockyard ps ON ps.stockyard_id = s.id
		WHERE ps.project_id = $1
		`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stockyards", "details": err.Error()})
			return
		}
		defer rows.Close()

		var stockyards []models.Stockyard
		for rows.Next() {
			var stockyard models.Stockyard
			if err := rows.Scan(&stockyard.ID, &stockyard.YardName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning stockyards", "details": err.Error()})
				return
			}
			stockyards = append(stockyards, stockyard)
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading stockyards", "details": err.Error()})
			return
		}

		project.Stockyards = stockyards

		// // Fetch project roles with quantity
		// roleRows, err := db.Query(`
		// 	SELECT r.role_id, r.role_name, COUNT(*) as quantity
		// 	FROM project_roles pr
		// 	JOIN roles r ON pr.role_id = r.role_id
		// 	WHERE pr.project_id = $1
		// 	GROUP BY r.role_id, r.role_name
		// `, projectID)
		// if err != nil {
		// 	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project roles", "details": err.Error()})
		// 	return
		// }
		// defer roleRows.Close()

		// var roles []models.RoleQuantity
		// for roleRows.Next() {
		// 	var role models.RoleQuantity
		// 	var roleName string
		// 	if err := roleRows.Scan(&role.RoleID, &roleName, &role.Quantity); err != nil {
		// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning project roles", "details": err.Error()})
		// 		return
		// 	}
		// 	roles = append(roles, role)
		// }
		// if err := roleRows.Err(); err != nil {
		// 	c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading project roles", "details": err.Error()})
		// 	return
		// }

		// project.Roles = roles

		c.JSON(http.StatusOK, project)

		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Get",
			Description:  "Get Project",
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
	}
}

// GetProjectRoles godoc
// @Summary      Get roles for a project
// @Tags         projects
// @Param        project_id  path      int  true  "Project ID"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Router       /api/project_roles/{project_id} [get]
func GetProjectRoles(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session ID
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

		// Get project_id from params
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Fetch project roles with quantity
		roleRows, err := db.Query(`
			SELECT r.role_id, r.role_name, COUNT(*) as quantity
			FROM project_roles pr
			JOIN roles r ON pr.role_id = r.role_id
			WHERE pr.project_id = $1
			GROUP BY r.role_id, r.role_name
		`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project roles", "details": err.Error()})
			return
		}
		defer roleRows.Close()

		var roles []models.RoleQuantity
		for roleRows.Next() {
			var role models.RoleQuantity
			if err := roleRows.Scan(&role.RoleID, &role.RoleName, &role.Quantity); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning project roles", "details": err.Error()})
				return
			}
			roles = append(roles, role)
		}
		if err := roleRows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading project roles", "details": err.Error()})
			return
		}

		// Save activity log
		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "GetRoles",
			Description:  "Fetched project roles",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}
		if logErr := SaveActivityLog(db, log); logErr != nil {
			// Don’t block response if logging fails, just return warning
			c.JSON(http.StatusOK, gin.H{
				"roles":   roles,
				"warning": "Roles fetched but failed to log activity: " + logErr.Error(),
			})
			return
		}

		// ✅ Final JSON response
		c.JSON(http.StatusOK, gin.H{
			"project_id": projectID,
			"roles":      roles,
		})
	}
}

// CreateProject creates a new project.
// @Summary Create project
// @Description Creates a new project. Request body: name, priority, project_status, start_date, end_date, client_id, budget, template_id, stockyards, roles, etc. Requires Authorization header.
// @Tags Projects
// @Accept json
// @Produce json
// @Param body body models.Project true "Project data"
// @Success 201 {object} models.CreateProjectResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project_create [post]
func CreateProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing", "details": ""})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var project models.Project
		if err := c.ShouldBindJSON(&project); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := ValidateProjectInput(&project); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		project.CreatedAt = time.Now()
		project.UpdatedAt = time.Now()
		project.LastUpdated = time.Now()
		project.ProjectId = repository.GenerateRandomNumber()

		// Insert project details into the project table
		sqlStatement := `
        INSERT INTO project (
            project_id, name, priority, project_status, start_date, end_date, logo,
            description, created_at, updated_at, last_updated_at, last_updated_by, client_id, budget, template_id, subscription_start_date, subscription_end_date, abbreviation,
    work_order, hra, invoice, calculator
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
        ) RETURNING project_id`

		err = db.QueryRow(sqlStatement,
			project.ProjectId, project.Name, project.Priority, project.ProjectStatus,
			project.StartDate.ToTime(), project.EndDate.ToTime(), project.Logo,
			project.Description, project.CreatedAt, project.UpdatedAt,
			project.LastUpdated, project.LastUpdatedBy, project.ClientId, project.Budget, project.TemplateID, project.SubscriptionStartDate.ToTime(), project.SubscriptionEndDate.ToTime(), project.Abbreviation, project.WorkOrder, project.HRA, project.Invoice, project.Calculator).Scan(&project.ProjectId)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Insert stockyards associated with the project
		sqlStockyard := `INSERT INTO project_stockyard (project_id, stockyard_id, created_at, updated_at) VALUES ($1, $2, $3, $4)`

		for _, stockyardID := range project.Stockyards {
			_, err := db.Exec(sqlStockyard, project.ProjectId, stockyardID, time.Now(), time.Now())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stockyard", "details": err.Error()})
				return
			}
		}

		// Copy template stages into project_stages
		rows, err := db.Query(`SELECT id, name, qc_assign, template_id, "order", completion_stage, inventory_deduction FROM stages WHERE template_id = $1`, project.TemplateID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch template stages", "details": err.Error()})
			return
		}
		defer rows.Close()

		stageInsertStmt := `
        INSERT INTO project_stages (
            name, project_id, assigned_to, qc_assign, qc_id, paper_id, template_id, "order", completion_stage, inventory_deduction
        ) VALUES ($1, $2, NULL, $3, NULL, NULL, $4, $5, $6, $7)`

		for rows.Next() {
			var stage models.Stage
			if err := rows.Scan(&stage.ID, &stage.Name, &stage.QCAssign, &stage.TemplateID, &stage.Order, &stage.CompletionStage, &stage.InventoryDeduction); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning stages", "details": err.Error()})
				return
			}

			_, err = db.Exec(stageInsertStmt, stage.Name, project.ProjectId, stage.QCAssign, stage.TemplateID, stage.Order, stage.CompletionStage, stage.InventoryDeduction)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert project stage", "details": err.Error()})
				return
			}
		}

		roleInsertStmt := `
    INSERT INTO project_roles (project_id, role_id)
    VALUES ($1, $2)`

		for _, role := range project.Roles {
			for i := 0; i < role.Quantity; i++ {
				_, err := db.Exec(roleInsertStmt, project.ProjectId, role.RoleID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert role", "details": err.Error()})
					return
				}
			}
		}

		// Log the activity
		activityLog := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Project Created",
			Description:  "Project created successfully" + " " + project.Name + " " + strconv.Itoa(project.ProjectId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    project.ProjectId,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Project created but failed to log activity", "details": logErr.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {

			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("New project created: %s", project.Name),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", project.ProjectId).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Project Created",
				fmt.Sprintf("New project created: %s", project.Name),
				map[string]string{
					"project_id":   strconv.Itoa(project.ProjectId),
					"project_name": project.Name,
					"action":       "project_created",
				},
				"project_created")
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, project.ProjectId,
			fmt.Sprintf("New project created: %s", project.Name),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", project.ProjectId))

		c.JSON(http.StatusCreated, gin.H{
			"message":    "Project created successfully",
			"project_id": project.ProjectId,
		})
	}
}

// UpdateeProject updates a project (project_id in body). Route has no path param in main.
// @Summary Update project (legacy)
// @Description Updates project. Body must include project fields. Requires Authorization header.
// @Tags Projects
// @Accept json
// @Produce json
// @Param body body models.Project true "Project data (include project_id)"
// @Success 200 {object} models.CreateProjectResponse "message, project_id"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project_update [put]
func UpdateeProject(db *sql.DB) gin.HandlerFunc {
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

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var project models.Project
		if err := c.ShouldBindJSON(&project); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
			return
		}

		project.ProjectId = projectID
		project.UpdatedAt = time.Now()
		project.LastUpdated = time.Now()

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// Update project table
		updateQuery := `
			UPDATE project SET 
				name = $1, priority = $2, project_status = $3,
				start_date = $4, end_date = $5, logo = $6,
				description = $7, updated_at = $8, last_updated_at = $9,
				last_updated_by = $10, client_id = $11, budget = $12, template_id = $13, abbreviation = $15, work_order = $16, hra = $17, invoice = $18, calculator = $19
			WHERE project_id = $14
		`
		_, err = tx.Exec(updateQuery,
			project.Name, project.Priority, project.ProjectStatus,
			project.StartDate.ToTime(), project.EndDate.ToTime(), project.Logo,
			project.Description, project.UpdatedAt, project.LastUpdated,
			project.LastUpdatedBy, project.ClientId, project.Budget, project.TemplateID,
			project.ProjectId, project.Abbreviation, project.WorkOrder, project.HRA, project.Invoice, project.Calculator,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project", "details": err.Error()})
			return
		}

		// Re-insert updated roles
		roleInsertStmt := `INSERT INTO project_roles (project_id, role_id) VALUES ($1, $2)`
		for _, role := range project.Roles {
			for i := 0; i < role.Quantity; i++ {
				_, err := tx.Exec(roleInsertStmt, project.ProjectId, role.RoleID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert project role", "details": err.Error()})
					return
				}
			}
		}

		// Re-insert updated stockyards
		stockyardInsertStmt := `INSERT INTO project_stockyard (project_id, stockyard_id, created_at, updated_at) VALUES ($1, $2, $3, $4)`
		for _, stockyardID := range project.Stockyards {
			_, err := tx.Exec(stockyardInsertStmt, project.ProjectId, stockyardID, time.Now(), time.Now())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stockyard", "details": err.Error()})
				return
			}
		}

		// Log the update
		activityLog := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Project Updated",
			Description:  "Project updated: " + project.Name + " " + strconv.Itoa(project.ProjectId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    project.ProjectId,
		}
		if err := SaveActivityLog(db, activityLog); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Project updated but failed to log activity", "details": err.Error()})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the project
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Project updated: %s", project.Name),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", project.ProjectId).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Project Updated",
				fmt.Sprintf("New project created: %s", project.Name),
				map[string]string{
					"project_id":   strconv.Itoa(project.ProjectId),
					"project_name": project.Name,
					"action":       "project_created",
				},
				"project_created")
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, project.ProjectId,
			fmt.Sprintf("Project updated: %s", project.Name),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", project.ProjectId))

		c.JSON(http.StatusOK, gin.H{"message": "Project updated successfully"})
	}
}

// UpdateProject godoc
// @Summary      Update project by project_id
// @Tags         projects
// @Param        project_id  path      int  true  "Project ID"
// @Param        body        body      object  true  "Project fields"
// @Success      200         {object}  object
// @Failure      400         {object}  models.ErrorResponse
// @Failure      401         {object}  models.ErrorResponse
// @Router       /api/project_update/{project_id} [put]
func UpdateProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing", "details": ""})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		var project models.Project
		if err := c.ShouldBindJSON(&project); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		project.UpdatedAt = time.Now()
		project.LastUpdated = time.Now()

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction", "details": err.Error()})
			return
		}
		defer tx.Rollback() // rollback if any error or early return

		// Update project
		updateStmt := `UPDATE project SET 
				name = $1, priority = $2, project_status = $3,
				start_date = $4, end_date = $5, logo = $6,
				description = $7, updated_at = $8, last_updated_at = $9,
				last_updated_by = $10, client_id = $11, budget = $12, template_id = $13, 
				subscription_start_date = $14, subscription_end_date = $15, abbreviation = $16, work_order = $17, hra = $18, invoice = $19, calculator = $20
			WHERE project_id = $21`

		_, err = tx.Exec(updateStmt,
			project.Name, project.Priority, project.ProjectStatus,
			project.StartDate.ToTime(), project.EndDate.ToTime(), project.Logo,
			project.Description, project.UpdatedAt, project.LastUpdated,
			project.LastUpdatedBy, project.ClientId, project.Budget, project.TemplateID,
			project.SubscriptionStartDate.ToTime(), project.SubscriptionEndDate.ToTime(), project.Abbreviation, project.WorkOrder, project.HRA, project.Invoice, project.Calculator,
			projectID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project", "details": err.Error()})
			return
		}

		// Delete old stockyards
		_, err = tx.Exec(`DELETE FROM project_stockyard WHERE project_id = $1`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stockyards", "details": err.Error()})
			return
		}

		// Insert new stockyards
		insertStockyardStmt := `INSERT INTO project_stockyard (project_id, stockyard_id, created_at, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, stockyard_id) DO NOTHING;`

		seen := map[int]bool{}
		for _, stockyardID := range project.Stockyards {
			if seen[stockyardID] {
				continue
			}
			seen[stockyardID] = true

			_, err := tx.Exec(insertStockyardStmt, projectID, stockyardID, time.Now(), time.Now())
			if err != nil {
				log.Println("Insert stockyard failed:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert stockyard", "details": err.Error()})
				return
			}
		}

		// Insert new project roles (without deleting existing ones)
		if len(project.Roles) > 0 {
			insertRoleStmt := `INSERT INTO project_roles (project_id, role_id)
VALUES ($1, $2)`

			for _, role := range project.Roles {
				// Insert the role multiple times based on quantity
				for i := 0; i < role.Quantity; i++ {
					_, err := tx.Exec(insertRoleStmt, projectID, role.RoleID)
					if err != nil {
						log.Println("Insert project role failed:", err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert project role", "details": err.Error()})
						return
					}
				}
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed", "details": err.Error()})
			return
		}

		// Save activity log (outside transaction is okay)
		activityLog := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Project Updated",
			Description:  "Project updated successfully " + project.Name + " " + strconv.Itoa(project.ProjectId),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    project.ProjectId,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Project updated but failed to log activity", "details": logErr.Error()})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who updated the project
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Project updated: %s", project.Name),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", project.ProjectId).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Project Updated",
				fmt.Sprintf("New project created: %s", project.Name),
				map[string]string{
					"project_id":   strconv.Itoa(project.ProjectId),
					"project_name": project.Name,
					"action":       "project_created",
				},
				"project_created")
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Project updated: %s", projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", projectID))

		c.JSON(http.StatusOK, gin.H{
			"message":    "Project updated successfully",
			"project_id": project.ProjectId,
		})
	}
}

// DeleteProject deletes a project by ID.
// @Summary Delete project
// @Description Deletes project and associated roles/members. Requires session_id header.
// @Tags Projects
// @Accept json
// @Produce json
// @Param id path int true "Project ID"
// @Success 200 {object} models.MessageResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project_delete/{id} [delete]
func DeleteProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("session_id")
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

		// Parse the project ID from URL parameters
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// Start a transaction to ensure all deletions happen together
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Delete associated roles from project_roles table
		_, err = tx.Exec("DELETE FROM project_roles WHERE project_id = $1", id)
		if err != nil {
			tx.Rollback() // Rollback the transaction if there's an error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete project roles"})
			return
		}

		// Delete associated members from project_members table
		_, err = tx.Exec("DELETE FROM project_members WHERE project_id = $1", id)
		if err != nil {
			tx.Rollback() // Rollback the transaction if there's an error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete project members"})
			return
		}

		// Delete the project from the project table
		result, err := tx.Exec("DELETE FROM project WHERE project_id = $1", id)
		if err != nil {
			tx.Rollback() // Rollback the transaction if there's an error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete project"})
			return
		}

		// Check if any rows were affected
		if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
			tx.Rollback() // Rollback if project not found
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}

		// Commit the transaction if all deletions were successful
		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		// Get project name before deletion (for notification)
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", id).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", id)
		}

		// Send notifications to all project members, clients, and end_clients BEFORE deletion
		sendProjectNotifications(db, id,
			fmt.Sprintf("Project deleted: %s", projectName),
			"https://precastezy.blueinvent.com/projects")

		// Log the activity
		activityLog := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Project Deleted",
			Description:  fmt.Sprintf("Project with ID %d deleted successfully", id),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    id,
		}

		if logErr := SaveActivityLog(db, activityLog); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Project deleted but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who deleted the project
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Project deleted: %s", projectName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

		c.JSON(http.StatusOK, gin.H{"message": "Project, associated roles, and members successfully deleted"})
	}
}

// FetchProject returns a single project by ID.
// @Summary Get project by ID
// @Description Returns one project by id. Requires Authorization header.
// @Tags Projects
// @Accept json
// @Produce json
// @Param id path int true "Project ID"
// @Success 200 {object} models.Project
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project_fetch/{id} [get]
func FetchProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		// Validate project ID early
		projectID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		// ✅ OPTIMIZED: Single query for session + user + access check
		var userID, roleID int
		var authorized bool
		var session models.Session
		var userName string

		err = db.QueryRow(`
			SELECT 
				u.id, u.role_id, CONCAT(u.first_name, ' ', u.last_name),
				s.session_id, s.host_name, s.ip_address, s.timestp,
				CASE 
					WHEN u.role_id = 1 THEN true
					ELSE EXISTS (
						SELECT 1 FROM project_members pm 
						WHERE pm.project_id = $2 AND pm.user_id = u.id
						UNION
						SELECT 1 FROM client cl 
            				JOIN end_client ec ON ec.client_id = cl.client_id
            				JOIN project p ON p.client_id = ec.id
            				WHERE p.project_id = $2 
              				AND cl.user_id = u.id
					)
				END as authorized
			FROM users u
			JOIN session s ON s.user_id = u.id
			WHERE s.session_id = $1::text AND s.expires_at > NOW()
		`, sessionID, projectID).Scan(&userID, &roleID, &userName, &session.SessionID, &session.HostName, &session.IPAddress, &session.Timestamp, &authorized)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Set UserID in session object
		session.UserID = userID

		if !authorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "You are not authorized to access this project"})
			return
		}

		// Fetch project details with optimized query
		fetchProjectDetailsOptimized(db, c, roleID, session, userName, projectID)
	}
}

func fetchProjectDetailsOptimized(db *sql.DB, c *gin.Context, roleID int, session models.Session, userName string, projectID int) {
	query := `
		WITH element_counts AS (
			SELECT 
				project_id,
				COUNT(*) FILTER (WHERE status = 'Erected') AS completed_elements,
				COUNT(*) AS total_elements
			FROM element
			WHERE project_id = $1
			GROUP BY project_id
		),
		role_permissions AS (
			SELECT 
				COALESCE(
					json_agg(
						json_build_object(
							'permission_id', p.permission_id,
							'permission_name', p.permission_name
						)
					) FILTER (WHERE p.permission_id IS NOT NULL),
					'[]'::json
				) as permissions
			FROM role_permissions rp
			LEFT JOIN permissions p ON rp.permission_id = p.permission_id
			WHERE rp.role_id = $2::int
		),
		stages_assigned_check AS (
			SELECT 
				$1 AS project_id,
				EXISTS (
					SELECT 1 
					FROM project_stages ps
					WHERE ps.project_id = $1
						AND ps.assigned_to IS NOT NULL 
						AND ps.assigned_to != 0
						AND ps.qc_id IS NOT NULL 
						AND ps.qc_id != 0
				) AS stages_assigned
		)
		SELECT 
			p.project_id, p.name, p.priority, p.project_status, p.start_date,  
			p.end_date, p.logo, p.description, p.budget, p.abbreviation,
			c.id, c.contact_person AS client_name, p.suspend,
			COALESCE(ec.total_elements, 0) AS total_elements, 
			COALESCE(ec.completed_elements, 0) AS completed_elements,
			rp.permissions,
			COALESCE(sac.stages_assigned, false) AS stages_assigned
		FROM project p
		LEFT JOIN end_client c ON p.client_id = c.id
		LEFT JOIN client cl ON c.client_id = cl.client_id
		LEFT JOIN element_counts ec ON p.project_id = ec.project_id
		LEFT JOIN stages_assigned_check sac ON p.project_id = sac.project_id
		CROSS JOIN role_permissions rp
		WHERE p.project_id = $1
	`

	var project struct {
		ProjectID         int                 `json:"project_id"`
		Name              string              `json:"name"`
		Priority          string              `json:"priority"`
		ProjectStatus     string              `json:"project_status"`
		StartDate         time.Time           `json:"start_date"`
		EndDate           time.Time           `json:"end_date"`
		Logo              string              `json:"logo"`
		Description       string              `json:"description"`
		Budget            string              `json:"budget"`
		Abbreviation      string              `json:"abbreviation"`
		ClientId          int                 `json:"client_id"`
		ClientName        string              `json:"client_name"`
		Suspend           bool                `json:"suspend"`
		TotalElements     int                 `json:"total_elements"`
		CompletedElements int                 `json:"completed_elements"`
		Progress          int                 `json:"progress"`
		Permissions       []models.Permission `json:"permissions"`
		StagesAssigned    bool                `json:"stages_assigned"`
	}

	var permissionsJSON string

	fmt.Printf("roleID type: %T, value: %v\n", roleID, roleID)

	if err := db.QueryRow(query, projectID, roleID).Scan(
		&project.ProjectID, &project.Name, &project.Priority, &project.ProjectStatus,
		&project.StartDate, &project.EndDate, &project.Logo, &project.Description,
		&project.Budget, &project.Abbreviation, &project.ClientId, &project.ClientName, &project.Suspend,
		&project.TotalElements, &project.CompletedElements, &permissionsJSON, &project.StagesAssigned,
	); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found", "errors": err.Error()})
		return
	}

	// Override suspend for superadmin
	if roleID == 1 {
		project.Suspend = false
	}

	// Parse permissions JSON
	if err := json.Unmarshal([]byte(permissionsJSON), &project.Permissions); err != nil {
		// If JSON parsing fails, set empty permissions
		project.Permissions = []models.Permission{}
	}

	// Calculate progress
	if project.TotalElements > 0 {
		project.Progress = (project.CompletedElements * 100) / project.TotalElements
	}

	// Log activity (async to avoid blocking response)
	go func() {
		_ = SaveActivityLog(db, models.ActivityLog{
			EventContext: "Project",
			EventName:    "Project Fetched",
			Description:  fmt.Sprintf("Project with ID %d fetched successfully", projectID),
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		})
	}()

	c.JSON(http.StatusOK, project)
}

// FetchAllProjects returns all projects.
// @Summary Get all projects
// @Description Returns all projects. Requires Authorization header.
// @Tags Projects
// @Accept json
// @Produce json
// @Success 200 {array} models.Project
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/projects [get]
func FetchAllProjects(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		userID := session.UserID

		// Fetch the user's role_id
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		var query string
		var args []interface{}

		// Superadmin (role_id = 1) can fetch all projects
		if roleID == 1 {
			query = `
				SELECT 
					p.project_id, p.name, p.priority, p.project_status, p.start_date,
					p.end_date, p.logo, p.description, p.budget, p.client_id, p.suspend,
					c.contact_person AS client_name, p.abbreviation
				FROM project p
				JOIN end_client c ON p.client_id = c.id
			`
		} else {
			// Non-superadmin: Fetch projects where the user is a member or a client user
			query = `
				SELECT 
					p.project_id, p.name, p.priority, p.project_status, p.start_date,
					p.end_date, p.logo, p.description, p.budget, p.client_id,p.suspend,
					ec.contact_person AS client_name, p.abbreviation
				FROM project p
				JOIN end_client ec ON p.client_id = ec.id
				WHERE p.project_id IN (
					SELECT project_id FROM project_members WHERE user_id = $1
					UNION
					SELECT p.project_id FROM project p
					JOIN end_client ec ON p.client_id = ec.id
    					JOIN client cl ON ec.client_id = cl.client_id
    					WHERE cl.user_id = $1
				)
			`
			args = append(args, userID)
		}

		// Fetch all accessible projects
		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching projects: " + err.Error()})
			return
		}
		defer rows.Close()

		var projects []struct {
			ProjectID         int       `json:"project_id"`
			Name              string    `json:"name"`
			Priority          string    `json:"priority"`
			ProjectStatus     string    `json:"project_status"`
			StartDate         time.Time `json:"start_date"`
			EndDate           time.Time `json:"end_date"`
			Logo              string    `json:"logo"`
			Description       string    `json:"description"`
			Budget            string    `json:"budget"`
			ClientName        string    `json:"client_name"`
			Abbreviation      string    `json:"abbreviation"`
			ClientID          int       `json:"client_id"`
			TotalElements     int       `json:"total_elements"`
			CompletedElements int       `json:"completed_elements"`
			ErectedElements   int       `json:"erected_elements"`
			Progress          int       `json:"progress"`
			Suspend           bool      `json:"suspend"`
		}

		for rows.Next() {
			var project struct {
				ProjectID         int       `json:"project_id"`
				Name              string    `json:"name"`
				Priority          string    `json:"priority"`
				ProjectStatus     string    `json:"project_status"`
				StartDate         time.Time `json:"start_date"`
				EndDate           time.Time `json:"end_date"`
				Logo              string    `json:"logo"`
				Description       string    `json:"description"`
				Budget            string    `json:"budget"`
				ClientName        string    `json:"client_name"`
				Abbreviation      string    `json:"abbreviation"`
				ClientID          int       `json:"client_id"`
				TotalElements     int       `json:"total_elements"`
				CompletedElements int       `json:"completed_elements"`
				ErectedElements   int       `json:"erected_elements"`
				Progress          int       `json:"progress"`
				Suspend           bool      `json:"suspend"`
			}

			if err := rows.Scan(
				&project.ProjectID, &project.Name, &project.Priority, &project.ProjectStatus,
				&project.StartDate, &project.EndDate, &project.Logo, &project.Description,
				&project.Budget, &project.ClientID, &project.Suspend, &project.ClientName, &project.Abbreviation,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning project: " + err.Error()})
				return
			}

			// if roleID == 1 {
			// 	project.Suspend = false
			// }

			// Fetch total elements and completed elements for the project
			if err := db.QueryRow(`SELECT COUNT(*) FROM element WHERE project_id = $1`, project.ProjectID).Scan(&project.TotalElements); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if err := db.QueryRow(`SELECT COUNT(*) FROM precast_stock WHERE project_id = $1 AND order_by_erection = false AND erected =  false`, project.ProjectID).Scan(&project.CompletedElements); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if err := db.QueryRow(`SELECT COUNT(*) FROM precast_stock WHERE project_id = $1 AND order_by_erection = true AND erected =  true`, project.ProjectID).Scan(&project.ErectedElements); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Calculate progress
			if project.TotalElements > 0 {
				project.Progress = (project.CompletedElements * 100) / project.TotalElements
			} else {
				project.Progress = 0
			}

			projects = append(projects, project)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating over projects: " + err.Error()})
			return
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "View Projects",
			Description:  "User fetched all project list",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Projects fetched but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Return the list of projects
		c.JSON(http.StatusOK, projects)
	}
}

// FetchAllProjectsBasic fetches all projects but returns only id, name, and suspend fields
func FetchAllProjectsBasic(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate session ID
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user_id from the session table
		userID := session.UserID

		// Fetch the user's role_id
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", userID).Scan(&roleID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		var query string
		var args []interface{}

		// Superadmin (role_id = 1) can fetch all projects
		if roleID == 1 {
			query = `
				SELECT 
					p.project_id, p.name, p.suspend
				FROM project p
			`
		} else {
			// Non-superadmin: Fetch projects where the user is a member or a client user
			query = `
				SELECT 
    p.project_id, p.name, p.suspend
FROM project p
WHERE p.project_id IN (
    SELECT project_id FROM project_members WHERE user_id = $1
    UNION
    SELECT p.project_id 
    FROM project p
    JOIN end_client ec ON p.client_id = ec.id
    JOIN client cl ON ec.client_id = cl.client_id
    WHERE cl.user_id = $1
)
			`
			args = append(args, userID)
		}

		// Fetch all accessible projects
		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching projects: " + err.Error()})
			return
		}
		defer rows.Close()

		var projects []struct {
			ProjectID int    `json:"project_id"`
			Name      string `json:"name"`
			Suspend   bool   `json:"suspend"`
		}

		for rows.Next() {
			var project struct {
				ProjectID int    `json:"project_id"`
				Name      string `json:"name"`
				Suspend   bool   `json:"suspend"`
			}

			if err := rows.Scan(
				&project.ProjectID, &project.Name, &project.Suspend,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning project: " + err.Error()})
				return
			}

			projects = append(projects, project)
		}

		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating over projects: " + err.Error()})
			return
		}

		// Log the activity
		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "View Projects Basic",
			Description:  "User fetched basic project list (id, name, suspend)",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Projects fetched but failed to log activity",
				"details": logErr.Error(),
			})
			return
		}

		// Return the list of projects with basic info only
		c.JSON(http.StatusOK, projects)
	}
}

// GetRolesByProjectID godoc
// @Summary      Get roles by project ID
// @Description  Returns roles for a project with quantity (total - assigned)
// @Tags         roles
// @Param        id   path  int  true  "Project ID"
// @Success      200  {array}  object
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/get_role/{id} [get]
func GetRolesByProjectID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

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

		/* ---------------- PROJECT ID ---------------- */
		projectIDStr := c.Param("id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
			return
		}

		/* ---------------- QUERY ---------------- */
		/*
		   quantity = total roles - assigned members
		*/
		rows, err := db.Query(`
			SELECT
				r.role_id,
				r.role_name,
				COUNT(pr.id) - COUNT(pm.user_id) AS quantity
			FROM project_roles pr
			JOIN roles r ON pr.role_id = r.role_id
			LEFT JOIN project_members pm
				ON pm.project_id = pr.project_id
				AND pm.role_id = pr.role_id
			WHERE pr.project_id = $1
			GROUP BY r.role_id, r.role_name
			ORDER BY r.role_name
		`, projectID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		/* ---------------- RESULT ---------------- */
		type RoleWithQuantity struct {
			RoleID   int    `json:"role_id"`
			RoleName string `json:"role_name"`
			Quantity int    `json:"quantity"`
		}

		var roles []RoleWithQuantity

		for rows.Next() {
			var r RoleWithQuantity
			if err := rows.Scan(&r.RoleID, &r.RoleName, &r.Quantity); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Safety: never send negative quantity
			if r.Quantity < 0 {
				r.Quantity = 0
			}

			roles = append(roles, r)
		}

		if len(roles) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "No roles found for this project"})
			return
		}

		/* ---------------- ACTIVITY LOG ---------------- */
		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "Get Roles With Quantity",
			Description:  "Fetched roles with available quantity for project",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		if err := SaveActivityLog(db, log); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Roles fetched but failed to log activity",
				"details": err.Error(),
			})
			return
		}

		/* ---------------- RESPONSE ---------------- */
		c.JSON(http.StatusOK, roles)
	}
}

type SuspendProjectRequest struct {
	Suspended bool `json:"suspend"`
}

// SuspendProjectHandler godoc
// @Summary      Suspend or unsuspend project
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        project_id  path  int  true  "Project ID"
// @Param        body        body  object  true  "suspended (bool)"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Failure      403  {object}  object
// @Router       /api/project/{project_id}/suspend [put]
func SuspendProjectHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get session ID from header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID required"})
			c.Abort()
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Fetch user by session ID
		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "err": err.Error()})
			c.Abort()
			return
		}

		// Check user's role
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", user.ID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to retrieve role ID"})
			c.Abort()
			return
		}

		// Only superadmin (role_id = 1) allowed
		if roleID != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized: Only superadmin can update suspension status"})
			return
		}

		// Get project_id from URL
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			return
		}

		// Bind JSON body
		var req SuspendProjectRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
			return
		}

		projectStatus := "Ongoing"
		if req.Suspended {
			projectStatus = "OnHold"
		}

		// Update suspended field in the project table
		_, err = db.Exec("UPDATE project SET suspend = $1, project_status = $2 WHERE project_id = $3", req.Suspended, projectStatus, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update project suspension", "details": err.Error()})
			return
		}

		status := "unsuspended"
		if req.Suspended {
			status = "suspended"
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectID)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who suspended/unsuspended the project
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Project %s: %s", status, projectName),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", projectID).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Project Suspended",
				fmt.Sprintf("New project created: %s", projectName),
				map[string]string{
					"project_id":   strconv.Itoa(projectID),
					"project_name": projectName,
					"action":       "project_created",
				},
				"project_created")
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectID,
			fmt.Sprintf("Project %s: %s", status, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", projectID))

		c.JSON(http.StatusOK, gin.H{"message": "Project successfully " + status})

		log := models.ActivityLog{
			EventContext: "Project",
			EventName:    "POST",
			Description:  fmt.Sprintf("Project %s", status),
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
	}
}

// setRedemptionHandler handles setting a redemption period for a project.
// It now returns a gin.HandlerFunc.
func SetRedemptionHandler(db *sql.DB) gin.HandlerFunc {
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

		projectID := c.Param("project_id")
		days := c.Param("days")

		projectId, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		}

		var daysInt int
		_, err = fmt.Sscanf(days, "%d", &daysInt)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid days parameter"})
			return
		}

		query := fmt.Sprintf(`
			UPDATE project
			SET redemption_end_date = CURRENT_DATE + INTERVAL '%d days',
				project_suspend = FALSE AND project_status = 'Ongoing'
			WHERE id = $1;
		`, daysInt)
		_, err = db.Exec(query, projectID)
		if err != nil {
			log.Printf("Error setting redemption for project %s: %v", projectID, err)
			c.JSON(500, gin.H{"error": "Failed to set redemption period"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who set redemption
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Redemption set for project: %s (%d days)", projectName, daysInt),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", projectId).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Redemption Set",
				fmt.Sprintf("New project created: %s", projectName),
				map[string]string{
					"project_id":   strconv.Itoa(projectId),
					"project_name": projectName,
					"action":       "project_created",
				},
				"project_created")
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectId,
			fmt.Sprintf("Redemption set for project: %s (%d days)", projectName, daysInt),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", projectId))

		c.JSON(200, gin.H{"message": fmt.Sprintf("Redemption set for project %s for %d days. Project unsuspended.", projectID, daysInt)})

		log := models.ActivityLog{
			EventContext: "User Redeemption",
			EventName:    "PUT",
			Description:  "Superadmin give redemption the the project",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// extendSubscriptionHandler handles extending the subscription end date.
// It now returns a gin.HandlerFunc.
func ExtendSubscriptionHandler(db *sql.DB) gin.HandlerFunc {
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

		projectID := c.Param("project_id")
		days := c.Param("days")

		projectId, err := strconv.Atoi(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}

		var daysInt int
		_, err = fmt.Sscanf(days, "%d", &daysInt)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid days parameter"})
			return
		}

		// When extending subscription, ensure project is unsuspended and clear redemption date
		query := fmt.Sprintf(`
			UPDATE project
			SET subscription_end_date = subscription_end_date + INTERVAL '%d days',
				project_suspend = FALSE,
				redemption_end_date = NULL AND project_status = 'Ongoing'
			WHERE id = $1;
		`, daysInt)
		_, err = db.Exec(query, projectID)
		if err != nil {
			log.Printf("Error extending subscription for project %s: %v", projectID, err)
			c.JSON(500, gin.H{"error": "Failed to extend subscription"})
			return
		}

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", projectId).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", projectId)
		}

		// Fetch user_id from the session table
		var userID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the user who extended subscription
			notif := models.Notification{
				UserID:    userID,
				Message:   fmt.Sprintf("Subscription extended for project: %s (%d days)", projectName, daysInt),
				Status:    "unread",
				Action:    "https://precastezy.blueinvent.com/overview",
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

			var endclientId int
			err = db.QueryRow("SELECT client_id FROM project WHERE project_id = $1", projectId).Scan(&endclientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientId int
			err = db.QueryRow("SELECT client_id FROM end_client WHERE id = $1", endclientId).Scan(&clientId)
			if err != nil {
				log.Printf("Failed to fetch client_id for notification: %v", err)
			}

			var clientUserId int
			err = db.QueryRow("SELECT user_id FROM client WHERE client_id = $1", clientId).Scan(&clientUserId)
			if err != nil {
				log.Printf("Failed to fetch client_user_id for notification: %v", err)
			}

			// Send push notification to the user who created the project
			log.Printf("Attempting to send push notification to user %d for project creation", userID)
			SendNotificationHelper(db, clientUserId,
				"Subscription Extended",
				fmt.Sprintf("New project created: %s", projectName),
				map[string]string{
					"project_id":   strconv.Itoa(projectId),
					"project_name": projectName,
					"action":       "project_created",
				},
				"project_created")
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, projectId,
			fmt.Sprintf("Subscription extended for project: %s (%d days)", projectName, daysInt),
			fmt.Sprintf("https://precastezy.blueinvent.com/projects/%d", projectId))

		c.JSON(200, gin.H{"message": fmt.Sprintf("Subscription extended for project %s by %d days. Project unsuspended.", projectID, daysInt)})

		log := models.ActivityLog{
			EventContext: "Subscription",
			EventName:    "PUT",
			Description:  "Extend Subscription Date of USer",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectId,
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

// sendProjectNotifications sends notifications to all project members, clients, and end_clients
func sendProjectNotifications(db *sql.DB, projectID int, message string, action string) {
	query := `
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.id IN (
			-- Project members
			SELECT pm.user_id
			FROM project_members pm
			WHERE pm.project_id = $1
			
			UNION
			
			-- Client users
			SELECT cl.user_id
			FROM project p
			JOIN end_client ec ON p.client_id = ec.id
			JOIN client cl ON ec.client_id = cl.client_id
			WHERE p.project_id = $1 AND cl.user_id IS NOT NULL
		)
	`

	rows, err := db.Query(query, projectID)
	if err != nil {
		log.Printf("Failed to fetch project stakeholders: %v", err)
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

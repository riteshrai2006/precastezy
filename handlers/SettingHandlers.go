package handlers

import (
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateRole godoc
// @Summary      Create role
// @Tags         roles
// @Accept       json
// @Produce      json
// @Param        body  body      models.Role  true  "Role (role_name)"
// @Success      201   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /api/roles [post]
func CreateRole(db *sql.DB) gin.HandlerFunc {
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

		var role models.Role
		if err := c.ShouldBindJSON(&role); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err = db.Exec("INSERT INTO roles (role_name) VALUES ($1)", role.RoleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"message": "Role created"})

		log := models.ActivityLog{
			EventContext: "Role",
			EventName:    "Create",
			Description:  "Create Role" + role.RoleName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
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

// GetRoles godoc
// @Summary      List roles
// @Tags         roles
// @Success      200  {array}  models.Role
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/roles [get]
func GetRoles(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query("SELECT role_id, role_name FROM roles WHERE LOWER(role_name) != 'superadmin'")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var roles []models.Role
		for rows.Next() {
			var role models.Role
			if err := rows.Scan(&role.RoleID, &role.RoleName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			roles = append(roles, role)
		}
		c.JSON(http.StatusOK, roles)

		log := models.ActivityLog{
			EventContext: "Role",
			EventName:    "Get",
			Description:  "Get All Roles",
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

// UpdateRole godoc
// @Summary      Update role
// @Tags         roles
// @Param        id     path      int  true  "Role ID"
// @Param        body   body      models.Role  true  "Role (role_name)"
// @Success      200    {object}  object
// @Failure      400    {object}  models.ErrorResponse
// @Router       /api/roles/{id} [put]
func UpdateRole(db *sql.DB) gin.HandlerFunc {
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
		var role models.Role
		if err := c.ShouldBindJSON(&role); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err = db.Exec("UPDATE roles SET role_name=$1 WHERE role_id=$2", role.RoleName, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Role updated"})

		log := models.ActivityLog{
			EventContext: "Role",
			EventName:    "Update",
			Description:  "Update role" + role.RoleName,
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

// DeleteRole godoc
// @Summary      Delete role
// @Tags         roles
// @Param        id   path      int  true  "Role ID"
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/roles/{id} [delete]
func DeleteRole(db *sql.DB) gin.HandlerFunc {
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

		var name string
		_ = db.QueryRow(`SELECT role_name from roles where role_id= $1`, id).Scan(&name)

		_, err = db.Exec("DELETE FROM roles WHERE role_id=$1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Role deleted"})

		log := models.ActivityLog{
			EventContext: "Role",
			EventName:    "Delete",
			Description:  "Delete role" + name,
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

// CreatePermission godoc
// @Summary      Create permission
// @Tags         permissions
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Permission"
// @Success      201   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/permissions [post]
func CreatePermission(db *sql.DB) gin.HandlerFunc {
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

		var perm models.Permission
		if err := c.ShouldBindJSON(&perm); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err = db.Exec("INSERT INTO permissions (permission_name) VALUES ($1)", perm.PermissionName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"message": "Permission created"})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Create",
			Description:  "Create Permission" + perm.PermissionName,
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

// GetPermissions
// GetPermissions godoc
// @Summary      List permissions
// @Tags         permissions
// @Success      200  {array}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/permissions [get]
func GetPermissions(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query("SELECT permission_id, permission_name FROM permissions")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var permissions []models.Permission
		for rows.Next() {
			var perm models.Permission
			if err := rows.Scan(&perm.PermissionID, &perm.PermissionName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			permissions = append(permissions, perm)
		}
		c.JSON(http.StatusOK, permissions)

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Get",
			Description:  "Get All Permission",
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

// UpdatePermission godoc
// @Summary      Update permission
// @Tags         permissions
// @Param        id    path      int  true  "Permission ID"
// @Param        body  body      object  true  "Permission (permission_name)"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/permissions/{id} [put]
func UpdatePermission(db *sql.DB) gin.HandlerFunc {
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
		var perm models.Permission
		if err := c.ShouldBindJSON(&perm); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err = db.Exec("UPDATE permissions SET permission_name=$1 WHERE permission_id=$2", perm.PermissionName, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Permission updated"})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Update",
			Description:  "Update Permission" + perm.PermissionName,
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

// DeletePermission godoc
// @Summary      Delete permission
// @Tags         permissions
// @Param        id   path  int  true  "Permission ID"
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/permissions/{id} [delete]
func DeletePermission(db *sql.DB) gin.HandlerFunc {
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

		var name string
		_ = db.QueryRow(`SELECT permission_name from permissions where permission_id = $1`, id).Scan(&name)

		_, err = db.Exec("DELETE FROM permissions WHERE permission_id=$1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Permission deleted"})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Delete",
			Description:  "Delete Permission",
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

// Role Permissions ---------------------------------------------------------------------------------------------
//------------------------------------------------------------------------------------------------------------------------
//--------------------------------------------------------------------------------------------------------------------------

// CreateRolePermission godoc
// @Summary      Create role permissions (bulk)
// @Tags         role-permissions
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Array of {role_id, permissions[]}"
// @Success      201   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/role-permissions [post]
func CreateRolePermission(db *sql.DB) gin.HandlerFunc {
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

		var bulkInput []struct {
			RoleID      int   `json:"role_id"`
			Permissions []int `json:"permissions"`
		}

		if err := c.ShouldBindJSON(&bulkInput); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		stmt, err := tx.Prepare("INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)")
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare statement"})
			return
		}
		defer stmt.Close()

		for _, item := range bulkInput {
			for _, permID := range item.Permissions {
				var exists bool
				err := db.QueryRow(`SELECT EXISTS (
					SELECT 1 FROM role_permissions 
					WHERE role_id = $1 AND permission_id = $2
				)`, item.RoleID, permID).Scan(&exists)
				if err != nil {
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error checking existing permissions"})
					return
				}

				if !exists {
					if _, err := stmt.Exec(item.RoleID, permID); err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert permission"})
						return
					}
				}
			}
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Role permissions created successfully"})

		log := models.ActivityLog{
			EventContext: "Role Permission",
			EventName:    "Create",
			Description:  "Create Role's Permission",
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

// GetRolePermissions godoc
// @Summary      List all role-permission mappings
// @Tags         role-permissions
// @Success      200  {array}  models.RolePermission
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/role-permissions [get]
func GetRolePermissions(db *sql.DB) gin.HandlerFunc {
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

		rows, err := db.Query("SELECT role_id, permission_id FROM role_permissions")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var rolePerms []models.RolePermission
		for rows.Next() {
			var rolePerm models.RolePermission
			if err := rows.Scan(&rolePerm.RoleID, &rolePerm.PermissionID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			rolePerms = append(rolePerms, rolePerm)
		}
		c.JSON(http.StatusOK, rolePerms)

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Get",
			Description:  "Get Role's Permission",
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

// GetRolePermissionByRoleID godoc
// @Summary      Get permissions for a role
// @Tags         role-permissions
// @Param        role_id  path  int  true  "Role ID"
// @Success      200      {array}  object
// @Failure      401      {object}  models.ErrorResponse
// @Router       /api/role-permissions/{role_id} [get]
func GetRolePermissionByRoleID(db *sql.DB) gin.HandlerFunc {
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

		roleID := c.Param("role_id")
		if roleID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Role ID is required"})
			return
		}

		query := `
			SELECT p.permission_id, p.permission_name
			FROM role_permissions rp
			JOIN permissions p ON rp.permission_id = p.permission_id
			WHERE rp.role_id = $1
		`
		rows, err := db.Query(query, roleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch permissions"})
			return
		}
		defer rows.Close()

		var permissions []models.Permission

		for rows.Next() {
			var perm models.Permission
			if err := rows.Scan(&perm.PermissionID, &perm.PermissionName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read permissions"})
				return
			}
			permissions = append(permissions, perm)
		}

		if len(permissions) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No permissions found for this role"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"role_id":     roleID,
			"permissions": permissions,
		})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "GET",
			Description:  "Get Role Permission",
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

// UpdateRolePermission godoc
// @Summary      Update role permissions (bulk replace)
// @Tags         role-permissions
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Array of {role_id, permissions[]}"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/role-permissions [put]
func UpdateRolePermission(db *sql.DB) gin.HandlerFunc {
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

		var bulkInput []struct {
			RoleID      int   `json:"role_id"`
			Permissions []int `json:"permissions"`
		}

		// Bind JSON input
		if err := c.ShouldBindJSON(&bulkInput); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
			return
		}

		// Start a new database transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Prepare DELETE statement to remove existing role permissions
		stmtDelete, err := tx.Prepare("DELETE FROM role_permissions WHERE role_id = $1")
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare delete statement"})
			return
		}
		defer stmtDelete.Close()

		// Prepare INSERT statement to add new permissions
		stmtInsert, err := tx.Prepare("INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)")
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare insert statement"})
			return
		}
		defer stmtInsert.Close()

		// Loop through the input to delete and insert permissions for each role
		for _, item := range bulkInput {
			// Skip superadmin role (role_id = 1)
			if item.RoleID == 1 {
				continue
			}

			// Delete existing permissions for non-superadmin roles
			if _, err := stmtDelete.Exec(item.RoleID); err != nil {
				tx.Rollback()
				fmt.Printf("Error executing DELETE for role_id=%d: %v\n", item.RoleID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing permissions"})
				return
			}

			// Insert new permissions
			for _, permID := range item.Permissions {
				if _, err := stmtInsert.Exec(item.RoleID, permID); err != nil {
					tx.Rollback()
					fmt.Printf("Error executing INSERT for role_id=%d, permID=%d: %v\n", item.RoleID, permID, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert new permissions"})
					return
				}
			}
		}

		// Commit the transaction to finalize the changes
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed"})
			return
		}

		// Return success message
		c.JSON(http.StatusOK, gin.H{"message": "Role permissions updated successfully"})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Update",
			Description:  "Update Role Permission",
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

// DeleteRolePermission godoc
// @Summary      Delete role-permission mapping
// @Tags         role-permissions
// @Param        id   path  int  true  "Role-permission mapping ID"
// @Success      200  {object}  object
// @Failure      401  {object}  models.ErrorResponse
// @Router       /api/role-permissions/{id} [delete]
func DeleteRolePermission(db *sql.DB) gin.HandlerFunc {
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

		var name string
		_ = db.QueryRow(`SELECT role_name FROM role_permissions where role_id = $1`, id).Scan(&name)

		_, err = db.Exec("DELETE FROM role_permissions WHERE role_id=$1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Role permission deleted"})

		log := models.ActivityLog{
			EventContext: "Permission",
			EventName:    "Delete",
			Description:  "Delete Role Permission" + name,
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

// -------------------------------------------------------------------------------------------------
// USER SETTING ------------------------------------------------------------------------------------

// CreateSettingHandler godoc
// @Summary      Create or update user setting
// @Description  Create or update settings (e.g. allow_multiple_sessions) for a user. Also used at POST /api/multiple.
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        body  body      models.Setting  true  "Setting (user_id, allow_multiple_sessions)"
// @Success      201   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /api/settings [post]
func CreateSettingHandler(db *sql.DB) gin.HandlerFunc {
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

		// Define the input struct for JSON binding
		var setting models.Setting

		// Bind the JSON request to the `Setting` struct
		if err := c.ShouldBindJSON(&setting); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid input data",
			})
			return
		}

		// Validate `user_id`
		if setting.UserID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid user_id",
			})
			return
		}

		// Insert or update the setting in the database
		query := `
            INSERT INTO settings (user_id, allow_multiple_sessions)
            VALUES ($1, $2)
            ON CONFLICT (user_id) DO UPDATE 
            SET allow_multiple_sessions = EXCLUDED.allow_multiple_sessions
        `

		_, err = db.Exec(query, setting.UserID, setting.AllowMultipleSessions)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to create or update setting: %v", err),
			})
			return
		}

		var first_name, last_name, email string
		err = db.QueryRow(`SELECT first_name, last_name, email FROM users WHERE id = $1`, setting.UserID).Scan(&first_name, &last_name, &email)
		if err != nil {
			// Log the error but don't fail the request
			fmt.Printf("Failed to get user details: %v\n", err)
			first_name = "Unknown"
			last_name = "User"
			email = "unknown@example.com"
		}

		// Return success response
		c.JSON(http.StatusCreated, gin.H{
			"message": "Setting updated successfully",
			"setting": setting,
		})

		log := models.ActivityLog{
			EventContext:      "Setting",
			EventName:         "Create",
			Description:       "Create Multiple Session of" + first_name + last_name,
			UserName:          userName,
			HostName:          session.HostName,
			IPAddress:         session.IPAddress,
			CreatedAt:         time.Now(),
			ProjectID:         0,
			AffectedUserName:  first_name,
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

// GetSettingHandler godoc
// @Summary      Get user setting
// @Description  Get settings for a user by user_id. Also used at GET /api/multiple/:user_id.
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        user_id  path      int  true  "User ID"
// @Success      200      {object}  models.Setting
// @Failure      400      {object}  models.ErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      500      {object}  models.ErrorResponse
// @Router       /api/settings/{user_id} [get]
func GetSettingHandler(db *sql.DB) gin.HandlerFunc {
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

		// Extract user_id from the query parameters
		userIDStr := c.Param("user_id")

		// Validate the presence of user_id
		if userIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
			return
		}

		// Convert user_id to an integer
		userID, err := strconv.Atoi(userIDStr)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user_id"})
			return
		}

		// Fetch the setting for the given user_id
		query := `SELECT user_id, allow_multiple_sessions FROM settings WHERE user_id = $1`
		var setting models.Setting

		err = db.QueryRow(query, userID).Scan(&setting.UserID, &setting.AllowMultipleSessions)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "No settings found for the given user_id"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to fetch settings: %v", err)})
			}
			return
		}

		// Return the fetched setting
		c.JSON(http.StatusOK, gin.H{"setting": setting})

		log := models.ActivityLog{
			EventContext: "Setting",
			EventName:    "Get",
			Description:  "Get Setting",
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

// GetUserSessionInfo retrieves session information for a specific user
// @Summary Get user session info
// @Description Retrieve session count and details for a specific user
// @Tags Settings
// @Accept json
// @Produce json
// @Param user_id path int true "User ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/settings/user/{user_id}/sessions [get]
func GetUserSessionInfo(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		userIDStr := c.Param("user_id")
		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		// Get session count
		sessionCount, err := storage.GetUserSessionCount(db, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get session count", "details": err.Error()})
			return
		}

		// Get session details
		sessions, err := storage.GetUserSessions(db, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get session details", "details": err.Error()})
			return
		}

		// Get user details
		var firstName, lastName, email string
		err = db.QueryRow(`SELECT first_name, last_name, email FROM users WHERE id = $1`, userID).Scan(&firstName, &lastName, &email)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}

		// Get settings to check if multiple sessions are allowed
		var allowMultipleSessions bool
		err = db.QueryRow("SELECT allow_multiple_sessions FROM settings WHERE user_id = $1", userID).Scan(&allowMultipleSessions)
		if err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get settings", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "User session information retrieved successfully",
			"data": gin.H{
				"user_id":                 userID,
				"user_name":               firstName + " " + lastName,
				"user_email":              email,
				"allow_multiple_sessions": allowMultipleSessions,
				"current_session_count":   sessionCount,
				"max_sessions_allowed":    2,
				"sessions":                sessions,
			},
		})
	}
}

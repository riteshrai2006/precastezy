package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// ==================== PEOPLE CRUD OPERATIONS ====================

// CreatePeople creates a new person
// @Summary Create person
// @Description Create a new person
// @Tags People
// @Accept json
// @Produce json
// @Param request body models.People true "People creation request"
// @Success 201 {object} models.PeopleResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/people [post]
func CreatePeople(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var people models.People
		if err := c.ShouldBindJSON(&people); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", people.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if email already exists
		var existingID int
		err = db.QueryRow("SELECT id FROM people WHERE email = $1", people.Email).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Person with this email already exists"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Insert new person
		var id int
		err = db.QueryRow(
			"INSERT INTO people (name, email, phone_no, department_id, category_id, project_id, created_at, updated_at, phone_code) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id",
			people.Name, people.Email, people.PhoneNo, people.DepartmentID, people.CategoryID, people.ProjectID, time.Now(), time.Now(), people.PhoneCode,
		).Scan(&id)

		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Person with this email already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create person"})
			return
		}

		people.ID = uint(id)
		c.JSON(http.StatusCreated, models.PeopleResponse{
			Success: true,
			Message: "Person created successfully",
			Data:    &people,
		})
	}
}

// GetPeople retrieves all people
// @Summary Get all people
// @Description Retrieve all people
// @Tags People
// @Produce json
// @Success 200 {object} models.PeopleListResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/people [get]
func GetPeople(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing session ID"})
			return
		}

		// ✅ Validate session and fetch role details
		var userID, roleID int
		var roleName string

		err := db.QueryRow(`
			SELECT s.user_id, u.role_id, r.role_name
			FROM session s
			JOIN users u ON s.user_id = u.id
			JOIN roles r ON u.role_id = r.role_id
			WHERE s.session_id = $1 AND s.expires_at > NOW()
		`, sessionID).Scan(&userID, &roleID, &roleName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session"})
			return
		}

		// ✅ Build access control condition
		var accessCondition string
		var args []interface{}
		argIndex := 1

		switch roleName {
		case "superadmin":
			accessCondition = "1=1" // No restriction

		case "admin":
			accessCondition = `
				pr.client_id IN (
					SELECT ec.id 
					FROM end_client ec
					JOIN client c ON ec.client_id = c.client_id
					WHERE c.user_id = $` + fmt.Sprint(argIndex) + `
				)
			`
			args = append(args, userID)
			argIndex++

		default:
			// ❌ No permission for other roles
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "No permission to view people",
			})
			return
		}

		// ✅ Query people data with access control
		query := fmt.Sprintf(`
			SELECT 
				p.id, 
				p.name, 
				p.email, 
				p.phone_no, 
				p.department_id, 
				p.category_id, 
				p.project_id, 
				pr.name AS project_name, 
				p.created_at, 
				p.updated_at, 
				p.phone_code, 
				pc.phone_code
			FROM people p 
			LEFT JOIN project pr ON p.project_id = pr.project_id 
			JOIN phone_code pc ON p.phone_code = pc.id
			WHERE %s
			ORDER BY p.name
		`, accessCondition)

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch people", "details": err.Error()})
			return
		}
		defer rows.Close()

		var peopleList []models.People
		for rows.Next() {
			var person models.People
			var phoneNo sql.NullString

			if err := rows.Scan(
				&person.ID,
				&person.Name,
				&person.Email,
				&phoneNo,
				&person.DepartmentID,
				&person.CategoryID,
				&person.ProjectID,
				&person.ProjectName,
				&person.CreatedAt,
				&person.UpdatedAt,
				&person.PhoneCode,
				&person.PhoneCodeName,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan person data", "details": err.Error()})
				return
			}

			if phoneNo.Valid {
				person.PhoneNo = phoneNo.String
			}
			peopleList = append(peopleList, person)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating people", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, models.PeopleListResponse{
			Success: true,
			Message: "People retrieved successfully",
			Data:    peopleList,
		})
	}
}

// GetPeopleByProject retrieves all people for a specific project
// @Summary Get people by project
// @Description Retrieve all people for a specific project
// @Tags People
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {object} models.PeopleListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/projects/{project_id}/people [get]
func GetPeopleByProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
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

		rows, err := db.Query(`SELECT p.id, p.name, p.email, p.phone_no, p.department_id, p.category_id, p.project_id, p.phone_code, pc.phone_code 
		 						FROM people p  
								JOIN phone_code pc ON p.phone_code = pc.id
								WHERE p.project_id = $1
								ORDER BY name`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch people"})
			return
		}
		defer rows.Close()

		var peopleList []models.People
		for rows.Next() {
			var person models.People
			var phoneNo sql.NullString
			if err := rows.Scan(&person.ID, &person.Name, &person.Email, &phoneNo, &person.DepartmentID, &person.CategoryID, &person.ProjectID, &person.PhoneCode, &person.PhoneCodeName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan person data"})
				return
			}
			if phoneNo.Valid {
				person.PhoneNo = phoneNo.String
			}
			peopleList = append(peopleList, person)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating people"})
			return
		}

		c.JSON(http.StatusOK, models.PeopleListResponse{
			Success: true,
			Message: "People retrieved successfully",
			Data:    peopleList,
		})
	}
}

// GetPeopleByDepartment retrieves all people for a specific department
// @Summary Get people by department
// @Description Retrieve all people for a specific department
// @Tags People
// @Produce json
// @Param department_id path int true "Department ID"
// @Success 200 {object} models.PeopleListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/departments/{department_id}/people [get]
func GetPeopleByDepartment(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		departmentIDStr := c.Param("id")
		departmentID, err := strconv.Atoi(departmentIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid department ID"})
			return
		}

		rows, err := db.Query(`SELECT p.id, p.name, p.email, p.phone_no, p.department_id, p.category_id, p.project_id, p.phone_code, pc.phone_code
								FROM people p
								JOIN phone_code pc ON p.phone_code = pc.id 
								WHERE department_id = $1 ORDER BY name`, departmentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch people"})
			return
		}
		defer rows.Close()

		var peopleList []models.People
		for rows.Next() {
			var person models.People
			var phoneNo sql.NullString
			if err := rows.Scan(&person.ID, &person.Name, &person.Email, &phoneNo, &person.DepartmentID, &person.CategoryID, &person.ProjectID, &person.PhoneCode, &person.PhoneCodeName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan person data"})
				return
			}
			if phoneNo.Valid {
				person.PhoneNo = phoneNo.String
			}
			peopleList = append(peopleList, person)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating people"})
			return
		}

		c.JSON(http.StatusOK, models.PeopleListResponse{
			Success: true,
			Message: "People retrieved successfully",
			Data:    peopleList,
		})
	}
}

// GetPeopleByCategory retrieves all people for a specific category
// @Summary Get people by category
// @Description Retrieve all people for a specific category
// @Tags People
// @Produce json
// @Param category_id path int true "Category ID"
// @Success 200 {object} models.PeopleListResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/categories/{category_id}/people [get]
func GetPeopleByCategory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		categoryIDStr := c.Param("id")
		categoryID, err := strconv.Atoi(categoryIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
			return
		}

		rows, err := db.Query(`SELECT p.id, p.name, p.email, p.phone_no, p.department_id, p.category_id, p.project_id, p.phone_code, pc.phone_code 
								FROM people p
								JOIN phone_code pc ON p.phone_code = pc.id
								WHERE category_id = $1 ORDER BY name`, categoryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch people"})
			return
		}
		defer rows.Close()

		var peopleList []models.People
		for rows.Next() {
			var person models.People
			var phoneNo sql.NullString
			if err := rows.Scan(&person.ID, &person.Name, &person.Email, &phoneNo, &person.DepartmentID, &person.CategoryID, &person.ProjectID, &person.PhoneCode, &person.PhoneCodeName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan person data"})
				return
			}
			if phoneNo.Valid {
				person.PhoneNo = phoneNo.String
			}
			peopleList = append(peopleList, person)
		}

		if err = rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating people"})
			return
		}

		c.JSON(http.StatusOK, models.PeopleListResponse{
			Success: true,
			Message: "People retrieved successfully",
			Data:    peopleList,
		})
	}
}

// GetPerson retrieves a specific person by ID
// @Summary Get person by ID
// @Description Retrieve a specific person by its ID
// @Tags People
// @Produce json
// @Param id path int true "Person ID"
// @Success 200 {object} models.PeopleResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/people/{id} [get]
func GetPerson(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
			return
		}

		var person models.People
		var phoneNo sql.NullString
		err = db.QueryRow(`SELECT p.id, p.name, p.email, p.phone_no, p.department_id, p.category_id, p.project_id, p.phone_code, pc.phone_code 
						FROM people p
						JOIN phone_code pc ON p.phone_code = pc.id
						WHERE id = $1`, id).Scan(&person.ID, &person.Name, &person.Email, &phoneNo, &person.DepartmentID, &person.CategoryID, &person.ProjectID, &person.PhoneCode, &person.PhoneCodeName)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch person"})
			return
		}

		if phoneNo.Valid {
			person.PhoneNo = phoneNo.String
		}

		c.JSON(http.StatusOK, models.PeopleResponse{
			Success: true,
			Message: "Person retrieved successfully",
			Data:    &person,
		})
	}
}

// UpdatePeople updates an existing person
// @Summary Update person
// @Description Update an existing person
// @Tags People
// @Accept json
// @Produce json
// @Param id path int true "Person ID"
// @Param request body models.People true "People update request"
// @Success 200 {object} models.PeopleResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Router /api/people/{id} [put]
func UpdatePeople(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
			return
		}

		var people models.People
		if err := c.ShouldBindJSON(&people); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if person exists
		var existingID int
		err = db.QueryRow("SELECT id FROM people WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Check if project exists
		var projectExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM project WHERE project_id = $1)", people.ProjectID).Scan(&projectExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !projectExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project does not exist"})
			return
		}

		// Check if department exists
		var departmentExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM departments WHERE id = $1)", people.DepartmentID).Scan(&departmentExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !departmentExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Department does not exist"})
			return
		}

		// Check if category exists
		var categoryExists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM categories WHERE id = $1)", people.CategoryID).Scan(&categoryExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		if !categoryExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Category does not exist"})
			return
		}

		// Check if another person with same email exists
		var conflictingID int
		err = db.QueryRow("SELECT id FROM people WHERE email = $1 AND id != $2", people.Email, id).Scan(&conflictingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Person with this email already exists"})
			return
		} else if err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Update person
		result, err := db.Exec(
			"UPDATE people SET name = $1, email = $2, phone_no = $3, department_id = $4, category_id = $5, project_id = $6, updated_at = $7, phone_code = $8 WHERE id = $9",
			people.Name, people.Email, people.PhoneNo, people.DepartmentID, people.CategoryID, people.ProjectID, time.Now(), people.PhoneCode, id,
		)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "Person with this email already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update person"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check update result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
			return
		}

		people.ID = uint(id)
		c.JSON(http.StatusOK, models.PeopleResponse{
			Success: true,
			Message: "Person updated successfully",
			Data:    &people,
		})
	}
}

// DeletePeople deletes a person
// @Summary Delete person
// @Description Delete a person by its ID
// @Tags People
// @Produce json
// @Param id path int true "Person ID"
// @Success 200 {object} models.PeopleResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/people/{id} [delete]
func DeletePeople(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
			return
		}
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
			return
		}

		// Check if person exists
		var existingID int
		err = db.QueryRow("SELECT id FROM people WHERE id = $1", id).Scan(&existingID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Delete person
		result, err := db.Exec("DELETE FROM people WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete person"})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check delete result"})
			return
		}

		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
			return
		}

		c.JSON(http.StatusOK, models.PeopleResponse{
			Success: true,
			Message: "Person deleted successfully",
		})
	}
}

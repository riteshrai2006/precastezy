// @title           Precast API
// @version         1.0
// @description     Precast Backend API - All endpoints used in the application.
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    https://precastezy.blueinvent.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      https://precastezy.blueinvent.com

// @BasePath  /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

// @schemes http https
package main

import (
	_ "backend/docs"
	"backend/handlers"
	appapi "backend/handlers/AppAPI"
	"backend/models"
	"backend/repository"
	"backend/services"
	"backend/storage"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/robfig/cron/v3"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/swag"
)

// Custom response writer to intercept HTML and inject CSS
type cssInjectorWriter struct {
	gin.ResponseWriter
	body *strings.Builder
}

func (w *cssInjectorWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *cssInjectorWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func (w *cssInjectorWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *cssInjectorWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func CORSConfig() cors.Config {
	corsConfig := cors.DefaultConfig()
	// Allow all origins for precastezy.blueinvent.com (main production domain)
	corsConfig.AllowOrigins = []string{
		"https://precastezy.blueinvent.com",
		"https://precast.blueinvent.com",
		"http://localhost:9000",
		"http://localhost:8080",
		"http://localhost:3000",
	}
	corsConfig.AllowCredentials = true
	// Allow all common headers - comprehensive list for precastezy.blueinvent.com
	corsConfig.AllowHeaders = []string{
		"Content-Type", "Content-Length", "Accept-Encoding", "X-XSRF-TOKEN",
		"Accept", "Origin", "X-Requested-With", "Authorization", "User-Agent",
		"Cache-Control", "Referer", "X-Requested-With",
		"Access-Control-Request-Method", "Access-Control-Request-Headers",
		"X-Custom-Header", "X-API-Key", "X-Client-Version",
		"Accept-Language", "Accept-Charset", "DNT", "Connection",
		"Upgrade-Insecure-Requests", "Sec-Fetch-Dest", "Sec-Fetch-Mode",
		"Sec-Fetch-Site", "Sec-Fetch-User",
	}
	// Allow all HTTP methods
	corsConfig.AllowMethods = []string{
		"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD", "PATCH", "CONNECT", "TRACE",
	}
	// Expose all common response headers
	corsConfig.ExposeHeaders = []string{
		"Content-Length", "Authorization", "Content-Type", "X-Total-Count",
		"X-Page-Count", "Access-Control-Allow-Origin", "Access-Control-Allow-Credentials",
	}
	corsConfig.MaxAge = 12 * time.Hour // Cache preflight requests for 12 hours
	return corsConfig
}

// IsAdminByRoleID checks if a user is an admin based on their role ID.
func IsAdminByRoleID(db *sql.DB, roleID int) (bool, error) {
	var roleName string
	err := db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", roleID).Scan(&roleName)
	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return strings.EqualFold(roleName, "superadmin") || strings.EqualFold(roleName, "admin"), nil
}

// RBACMiddleware ensures users have appropriate permissions based on roles and project access.
func RBACMiddleware(db *sql.DB, requiredPermission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID required"})
			c.Abort()
			return
		}

		// Fetch user by session ID
		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "err": err.Error()})
			c.Abort()
			return
		}

		// Fetch the role_id from the users table by user_id
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", user.ID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to retrieve role ID"})
			c.Abort()
			return
		}

		// Check if the user is an admin by RoleID
		isAdmin, err := IsAdminByRoleID(db, roleID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to check admin role"})
			c.Abort()
			return
		}

		// Allow admins unrestricted access
		if isAdmin {
			c.Set("user", user)
			c.Next()
			return
		}

		// Extract project ID and task ID from route parameters
		projectIDParam := c.Param("project_id")
		taskIDParam := c.Param("task_id")

		// Convert projectIDParam to int
		var projectID int
		if projectIDParam != "" {
			projectID, err = strconv.Atoi(projectIDParam)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project ID"})
				c.Abort()
				return
			}
		}

		// Fetch permission ID for the required permission
		permissionID, err := storage.GetPermissionID(db, requiredPermission)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "Failed to retrieve permission ID"})
			c.Abort()
			return
		}

		// Check if user has required permission for the project
		if projectID > 0 {
			hasPermission, err := HasProjectPermission(db, user.ID, projectID, permissionID)
			if err != nil || !hasPermission {
				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this project"})
				c.Abort()
				return
			}

			// Check if user is a member of the project
			isMember, err := IsProjectMember(db, user.ID, projectID)
			if err != nil || !isMember {
				c.JSON(http.StatusForbidden, gin.H{"error": "You are not a member of this project"})
				c.Abort()
				return
			}
		}

		// Check if user is assigned to the task, if applicable
		if taskIDParam != "" {
			taskID, err := strconv.Atoi(taskIDParam)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
				c.Abort()
				return
			}

			isAssigned, err := IsTaskAssignee(db, user.ID, taskID)
			if err != nil || !isAssigned {
				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this task"})
				c.Abort()
				return
			}
		}

		// Store user info for use in handlers
		c.Set("user", user)
		c.Next()
	}
}

// Middleware to block suspended projects unless user is superadmin
func CheckProjectSuspension(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session ID required"})
			c.Abort()
			return
		}

		// Fetch user by session ID
		user, err := storage.GetUserBySessionID(db, sessionID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "err": err.Error()})
			c.Abort()
			return
		}

		// Fetch the role_id from the users table by user_id
		var roleID int
		err = db.QueryRow("SELECT role_id FROM users WHERE id = $1", user.ID).Scan(&roleID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to retrieve role ID"})
			c.Abort()
			return
		}

		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
			c.Abort()
			return
		}

		var suspended bool
		err = db.QueryRow("SELECT suspend FROM project WHERE project_id = $1", projectID).Scan(&suspended)
		if err != nil {
			log.Println("Error checking suspension status:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "details": err.Error()})
			c.Abort()
			return
		}

		if suspended && roleID != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: project is suspended"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// HasProjectPermission checks if a user has a specific permission in a project.
func HasProjectPermission(db *sql.DB, userID int, projectID int, permissionID int) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM project_roles pr
		JOIN role_permissions rp ON pr.role_id = rp.role_id
		WHERE pr.project_id = $1
		  AND rp.permission_id = $2`
	err := db.QueryRow(query, projectID, permissionID).Scan(&count)
	return count > 0, err
}

// IsTaskAssignee checks if a user is assigned to a task.
func IsTaskAssignee(db *sql.DB, userID int, taskID int) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM task_assigned_to WHERE member_id = $1 AND task_id = $2`
	err := db.QueryRow(query, userID, taskID).Scan(&count)
	return count > 0, err
}

// IsProjectMember checks if a user is a member of a project.
func IsProjectMember(db *sql.DB, userID int, projectID int) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM project_members WHERE user_id = $1 AND project_id = $2`
	err := db.QueryRow(query, userID, projectID).Scan(&count)
	return count > 0, err
}

func HelloWorld(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Hello, World!"})
}

func runSuspensionJob(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for suspension job: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Recovered from panic in runSuspensionJob: %v", r)
		}
	}()

	// 1. Suspend projects when subscription ends and not in redemption
	suspendQuery := `
		UPDATE project
		SET suspend = TRUE,
			project_status = 'Onhold'
		WHERE subscription_end_date < CURRENT_DATE
		  AND (redemption_end_date IS NULL OR redemption_end_date < CURRENT_DATE)
		  AND suspend = FALSE
	`
	res, err := tx.Exec(suspendQuery)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to suspend projects: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	log.Printf("Suspended %d projects due to subscription expiry.", rowsAffected)

	return tx.Commit()
}

func AutoCreateTasks(db *sql.DB) {
	log.Println("AutoCreateTasks started")

	tx, err := db.Begin()
	if err != nil {
		log.Println("Failed to begin transaction:", err)
		return
	}
	defer tx.Rollback()

	taskTypeID := 59
	priority := "Medium"
	status := "Inprogress"
	colorCode := "#FF5733"
	description := "Auto-generated task"
	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, 7)
	stockyardID := 19

	projectIDs := []int{
		978617912,
		937579592,
		942452639,
		591802747,
	}

	// 2. For each project, create elements up to daily target
	for _, projectID := range projectIDs {
		// Generate random daily target between 15-20 elements
		dailyTarget := 15 + rand.Intn(6) // random number between 15 and 20
		log.Printf("Project %d: Daily target set to %d elements", projectID, dailyTarget)

		// Fetch all element_type_ids for this project
		etRows, err := tx.Query(`SELECT element_type_id FROM element_type WHERE project_id = $1`, projectID)
		if err != nil {
			log.Printf("Failed to fetch element_type_ids for project_id %d: %v", projectID, err)
			continue
		}

		var elementTypeIDs []int
		for etRows.Next() {
			var id int
			if err := etRows.Scan(&id); err == nil {
				elementTypeIDs = append(elementTypeIDs, id)
			}
		}
		etRows.Close()

		elementCount := 0
		taskCounter := 0

		for _, elementTypeID := range elementTypeIDs {
			if elementCount >= dailyTarget {
				log.Printf("Project %d: Daily target of %d elements reached, stopping", projectID, dailyTarget)
				break
			}

			// Fetch all unique floor_ids for this element_type_id from element table
			floorRows, err := tx.Query(`SELECT DISTINCT target_location FROM element WHERE element_type_id = $1 AND instage = FALSE AND project_id = $2`, elementTypeID, projectID)
			if err != nil {
				log.Printf("Failed to fetch floor_ids for element_type_id %d: %v", elementTypeID, err)
				continue
			}

			var floorIDs []int
			for floorRows.Next() {
				var floorID int
				if err := floorRows.Scan(&floorID); err == nil {
					floorIDs = append(floorIDs, floorID)
				}
			}
			floorRows.Close()

			if len(floorIDs) == 0 {
				log.Printf("No available floors for element_type_id %d", elementTypeID)
				continue
			}

			for _, floorID := range floorIDs {
				if elementCount >= dailyTarget {
					break
				}

				// Get floor name from precast table
				var floorName string
				err := tx.QueryRow(`SELECT name FROM precast WHERE id = $1`, floorID).Scan(&floorName)
				if err != nil {
					log.Printf("Failed to fetch floor name for floor_id %d: %v", floorID, err)
					continue
				}

				// Get stage_path
				var stagePath string
				err = tx.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
				if err != nil {
					log.Printf("No stage_path for element_type_id %d: %v", elementTypeID, err)
					continue
				}

				stageArray := strings.Split(strings.Trim(stagePath, "{}"), ",")
				if len(stageArray) == 0 {
					log.Printf("Empty stage path for element_type_id %d", elementTypeID)
					continue
				}
				stageID, _ := strconv.Atoi(strings.TrimSpace(stageArray[0]))

				// Get assignment info
				var assignedTo int
				var qcID, paperID *int
				err = tx.QueryRow(`SELECT assigned_to, qc_id, paper_id FROM project_stages WHERE id = $1`, stageID).Scan(&assignedTo, &qcID, &paperID)
				if err != nil {
					log.Printf("No stage assignment for stage_id %d: %v", stageID, err)
					continue
				}

				// Check available elements for this floor
				rows, err := tx.Query(`
					SELECT id, element_name FROM element
					WHERE element_type_id = $1 AND target_location = $2 AND instage = FALSE AND project_id = $3
					ORDER BY id ASC
				`, elementTypeID, floorID, projectID)
				if err != nil {
					log.Printf("Failed to fetch elements: %v", err)
					continue
				}

				var elementIDs []int
				var elementNames []string
				for rows.Next() {
					var id int
					var name string
					if err := rows.Scan(&id, &name); err == nil {
						elementIDs = append(elementIDs, id)
						elementNames = append(elementNames, name)
					}
				}
				rows.Close()

				if len(elementIDs) == 0 {
					continue
				}

				// Calculate how many elements we can take from this floor
				elementsToTake := len(elementIDs)
				remainingNeeded := dailyTarget - elementCount

				if elementsToTake > remainingNeeded {
					elementsToTake = remainingNeeded
					elementIDs = elementIDs[:elementsToTake]
					elementNames = elementNames[:elementsToTake]
				}

				// Create task
				taskCounter++
				name := "Auto Task " + strconv.Itoa(taskCounter) + " - " + floorName
				var taskID int
				err = tx.QueryRow(`
					INSERT INTO task (project_id, task_type_id, name, stage_id, description, priority,
					assigned_to, estimated_effort_in_hrs, start_date, end_date, status, color_code,
					element_type_id, floor_id)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
					RETURNING task_id
				`, projectID, taskTypeID, name, stageID, description, priority,
					assignedTo, 8, startDate, endDate, status, colorCode, elementTypeID, floorID).Scan(&taskID)
				if err != nil {
					log.Printf("Failed to insert task for element_type_id %d: %v", elementTypeID, err)
					continue
				}

				// Mark elements as in stage
				_, err = tx.Exec(`UPDATE element SET instage = TRUE WHERE id = ANY($1)`, pq.Array(elementIDs))
				if err != nil {
					log.Printf("Failed to update instage: %v", err)
					continue
				}

				// Insert activities + complete_production
				for idx, eid := range elementIDs {
					ename := elementNames[idx]

					_, err = tx.Exec(`
						INSERT INTO activity (task_id, project_id, name, stage_id, status, element_id,
						assigned_to, start_date, end_date, priority, qc_id, paper_id, stockyard_id)
						VALUES ($1,$2,$3,$4,'Inprogress',$5,$6,$7,$8,$9,$10,$11,$12)
					`, taskID, projectID, ename, stageID, eid, assignedTo, startDate, endDate, priority,
						qcID, paperID, stockyardID)
					if err != nil {
						log.Printf("Failed to insert activity for element %d: %v", eid, err)
						continue
					}

					// Get activity ID
					var activityID int
					err = tx.QueryRow(`SELECT id FROM activity WHERE task_id=$1 AND element_id=$2 AND completed = false ORDER BY id DESC LIMIT 1`, taskID, eid).Scan(&activityID)
					if err != nil {
						log.Printf("Failed to fetch activity id: %v", err)
						continue
					}

					// Insert into complete_production
					_, err = tx.Exec(`
						INSERT INTO complete_production (task_id, activity_id, project_id, element_id,
						element_type_id, floor_id, stage_id, user_id, started_at)
						VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
					`, taskID, activityID, projectID, eid, elementTypeID, floorID, stageID, assignedTo, time.Now())
					if err != nil {
						log.Printf("Failed to insert complete_production: %v", err)
					}
				}

				elementCount += len(elementIDs)
				log.Printf("Created task %d for element_type_id %d on floor_id %d (%s) in project %d", taskID, elementTypeID, floorID, floorName, projectID)

				if elementCount >= dailyTarget {
					break
				}
			}
		}
	}

	// Commit
	if err := tx.Commit(); err != nil {
		log.Println("Commit failed:", err)
		return
	}

	log.Println("AutoCreateTasks finished successfully")
}

func ErectedHandler(db *sql.DB) error {
	log.Println("[ErectedHandler] START")

	tx, err := db.Begin()
	if err != nil {
		log.Printf("[ErectedHandler] tx begin error: %v", err)
		return err
	}
	defer tx.Rollback()

	projectIDs := []int{
		978617912,
		937579592,
		942452639,
		591802747,
	}

	for _, projectID := range projectIDs {
		quantity := 15 + rand.Intn(6)
		log.Printf("[ErectedHandler] project=%d target=%d", projectID, quantity)

		rows, err := tx.Query(`
			SELECT element_id FROM precast_stock
			WHERE erected = false AND project_id = $1
			ORDER BY RANDOM()
			LIMIT $2
		`, projectID, quantity)
		if err != nil {
			log.Printf("[ErectedHandler] select elements error project=%d: %v", projectID, err)
			return err
		}

		var elementIDs []int
		for rows.Next() {
			var eid int
			if err := rows.Scan(&eid); err != nil {
				log.Printf("[ErectedHandler] scan element_id error: %v", err)
				rows.Close()
				return err
			}
			elementIDs = append(elementIDs, eid)
		}
		rows.Close()

		log.Printf("[ErectedHandler] project=%d eligible_elements=%d", projectID, len(elementIDs))

		if len(elementIDs) == 0 {
			log.Printf("[ErectedHandler] project=%d nothing to erect", projectID)
			continue
		}

		res, err := tx.Exec(`
			UPDATE precast_stock
			SET stockyard = true,
			    erected = true,
			    order_by_erection = true,
			    dispatch_status = true
			WHERE element_id = ANY($1) AND project_id = $2
		`, pq.Array(elementIDs), projectID)
		if err != nil {
			log.Printf("[ErectedHandler] update precast_stock failed project=%d: %v", projectID, err)
			return err
		}

		affected, _ := res.RowsAffected()
		log.Printf("[ErectedHandler] precast_stock updated rows=%d project=%d", affected, projectID)

		res2, err := tx.Exec(`UPDATE element SET status = 'Erected' WHERE id = ANY($1)`, pq.Array(elementIDs))
		if err != nil {
			log.Printf("[ErectedHandler] update element status failed: %v", err)
			return err
		}

		affected2, _ := res2.RowsAffected()
		log.Printf("[ErectedHandler] element updated rows=%d project=%d", affected2, projectID)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[ErectedHandler] commit failed: %v", err)
		return err
	}

	log.Println("[ErectedHandler] END SUCCESS")
	return nil
}

func CompleteActivityToStockyard(db *sql.DB) error {
	log.Println("[CompleteActivityToStockyard] START")

	tx, err := db.Begin()
	if err != nil {
		log.Printf("[CompleteActivityToStockyard] tx begin error: %v", err)
		return err
	}
	defer tx.Rollback()

	activities, err := GetLatestActivities(tx)
	if err != nil {
		log.Printf("[CompleteActivityToStockyard] fetch activities error: %v", err)
		return err
	}

	log.Printf("[CompleteActivityToStockyard] activities fetched=%d", len(activities))

	for _, a := range activities {
		log.Printf("[CompleteActivityToStockyard] force completing activity_id=%d", a.ID)

		// 1️⃣ Force mark ALL statuses as completed
		_, err := tx.Exec(`
			UPDATE activity
			SET
				status = 'completed',
				qc_status = 'completed',
				meshmold_qc_status = 'completed',
				mesh_mold_status = 'completed',
				reinforcement_qc_status = 'completed',
				reinforcement_status = 'completed'
			WHERE id = $1
		`, a.ID)
		if err != nil {
			log.Printf("[CompleteActivityToStockyard] status update failed activity=%d: %v", a.ID, err)
			return err
		}

		// 2️⃣ Move element to stockyard (outside tx uses db)
		log.Printf("[CompleteActivityToStockyard] activity=%d moving to stockyard", a.ID)
		if _, err := CreateePrecastStock(db, a.ElementID, a.ProjectID, a.StockyardID); err != nil {
			log.Printf("[CompleteActivityToStockyard] stockyard insert failed activity=%d: %v", a.ID, err)
			return err
		}

		// 3️⃣ Mark activity completed
		_, err = tx.Exec(`
			UPDATE activity
			SET completed = true
			WHERE id = $1
		`, a.ID)
		if err != nil {
			log.Printf("[CompleteActivityToStockyard] mark completed failed activity=%d: %v", a.ID, err)
			return err
		}

		// 4️⃣ Insert complete_production record
		_, err = tx.Exec(`
			INSERT INTO complete_production (
				element_id, activity_id, element_type_id,
				task_id, project_id, started_at,
				updated_at, user_id, status
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`,
			a.ElementID,
			a.ID,
			a.ElementTypeID,
			a.TaskID,
			a.ProjectID,
			time.Now(),
			time.Now(),
			60,
			"completed",
		)
		if err != nil {
			log.Printf("[CompleteActivityToStockyard] complete_production insert failed activity=%d: %v", a.ID, err)
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[CompleteActivityToStockyard] commit failed: %v", err)
		return err
	}

	log.Println("[CompleteActivityToStockyard] END SUCCESS")
	return nil
}

func CreateePrecastStock(db *sql.DB, elementID int, projectID int, stockyardID int) (int, error) {
	log.Printf("[CreateePrecastStock] Creating precast stock for elementID=%d, projectID=%d, stockyardID=%d", elementID, projectID, stockyardID)
	var elementTypeID, targetLocation int
	var elementName string
	var disable bool

	// Fetch element details from the element table
	query := "SELECT element_type_id, element_name, target_location, disable FROM element WHERE id = $1 AND project_id = $2 LIMIT 1"
	err := db.QueryRow(query, elementID, projectID).Scan(&elementTypeID, &elementName, &targetLocation, &disable)
	if err != nil {
		log.Printf("[CreateePrecastStock]Failed to fetch element details: %v", err)
		return 0, fmt.Errorf("element not found or database error: %w", err)
	}

	// Fetch element type details
	var thickness, length, height, weight, density float32
	var elementTypeName, elementType string
	query = "SELECT element_type, element_type_name, thickness, length, height, mass, density FROM element_type WHERE element_type_id = $1 AND project_id = $2 LIMIT 1"
	err = db.QueryRow(query, elementTypeID, projectID).Scan(&elementType, &elementTypeName, &thickness, &length, &height, &weight, &density)
	if err != nil {
		log.Printf("[CreateePrecastStock] Failed to fetch element type details: %v", err)
		return 0, fmt.Errorf("element type not found or database error: %w", err)
	}

	// Correct format for dimensions
	dimensions := fmt.Sprintf("Thickness: %.2fmm, Length: %.2fmm, Height: %.2fmm", thickness, length, height)

	// Correct volume calculation
	volume := (thickness * length * height) / 1000000000.0
	elementWeight := volume * density

	// Define necessary values
	productionDate := time.Now() // Assign a valid timestamp
	storageLocation := "default_location"
	dispatchStatus := false

	// Insert into precast_stock table
	query = `
			INSERT INTO precast_stock (
				element_id, element_type, element_type_id, stockyard_id, dimensions, weight,
				production_date, storage_location, dispatch_status, created_at, updated_at,
				project_id, target_location, stockyard
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10, $11, true)
			RETURNING id;
		`

	var insertedID int
	err = db.QueryRow(query, elementID, elementType, elementTypeID, stockyardID,
		dimensions, elementWeight, productionDate, storageLocation,
		dispatchStatus, projectID, targetLocation).Scan(&insertedID)

	if err != nil {
		log.Printf("[CreateePrecastStock] Failed to insert precast stock: %v", err)
		return 0, fmt.Errorf("failed to create precast stock: %w", err)
	}

	return insertedID, nil
}

func GetLatestActivities(tx *sql.Tx) ([]models.Activity, error) {
	// First, fetch all project IDs
	projectRows, err := tx.Query(`SELECT project_id FROM project`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch project IDs: %w", err)
	}
	defer projectRows.Close()

	projectIDs := []int{
		978617912,
		937579592,
		942452639,
		591802747,
	}

	var allActivities []models.Activity

	// Process each project
	for _, projectID := range projectIDs {
		// Calculate quantity for this project (30-50 activities per project)
		quantity := 15 + rand.Intn(6)

		rows, err := tx.Query(`
			SELECT id, task_id, project_id, element_id, stage_id, stockyard_id
			FROM activity
			WHERE project_id = $1 AND completed = false
			ORDER BY id DESC
			LIMIT $2
		`, projectID, quantity)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch activities for project %d: %w", projectID, err)
		}

		for rows.Next() {
			var a models.Activity
			if err := rows.Scan(&a.ID, &a.TaskID, &a.ProjectID, &a.ElementID, &a.StageID, &a.StockyardID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan activity: %w", err)
			}
			allActivities = append(allActivities, a)
		}
		rows.Close()

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error reading activities for project %d: %w", projectID, err)
		}
	}

	return allActivities, nil
}

func GetStageNameByID(tx *sql.Tx, stageID int) (string, error) {
	var name string
	err := tx.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, stageID).Scan(&name)
	return strings.ToLower(name), err
}

func RunWorkOrderRecurrenceNotifications(db *sql.DB, cronLogger *log.Logger) error {
	rows, err := db.Query(`SELECT id, recurrence_patterns, wo_validate FROM work_order WHERE recurrence_patterns IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("failed to fetch work orders: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var workOrderID int
		var recurrenceData []byte
		var woValidate time.Time

		if err := rows.Scan(&workOrderID, &recurrenceData, &woValidate); err != nil {
			if cronLogger != nil {
				cronLogger.Printf("Scan error for WorkOrder %d: %v", workOrderID, err)
			}
			continue
		}

		// Skip expired work orders
		if time.Now().After(woValidate) {
			continue
		}

		var patterns []models.RecurrencePattern
		if err := json.Unmarshal(recurrenceData, &patterns); err != nil {
			if cronLogger != nil {
				cronLogger.Printf("Invalid recurrence pattern for WorkOrder %d: %v", workOrderID, err)
			}
			continue
		}

		today := time.Now()
		for _, p := range patterns {
			switch p.PatternType {
			case "date":
				if shouldRunDatePattern(p.DateValue, today) {
					sendInvoiceReminder(workOrderID, p, cronLogger)
				}
			case "week":
				if shouldRunWeekPattern(p.WeekNumber, p.DayOfWeek, today) {
					sendInvoiceReminder(workOrderID, p, cronLogger)
				}
			}
		}
	}

	return nil
}

func shouldRunDatePattern(dateValue string, today time.Time) bool {
	if dateValue == "penultimate" {
		// 2nd last day of month
		lastDay := time.Date(today.Year(), today.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
		return today.Day() == lastDay-1
	}
	// Normal numeric day check
	if today.Day() == parseInt(dateValue) {
		return true
	}
	return false
}

func shouldRunWeekPattern(weekNumber, dayOfWeek string, today time.Time) bool {
	dayMap := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	weekNumMap := map[string]int{"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5}

	targetDay := dayMap[dayOfWeek]
	targetWeek := weekNumMap[weekNumber]

	if today.Weekday() != targetDay {
		return false
	}

	firstDay := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.Local)
	weekNum := ((today.Day() + int(firstDay.Weekday())) / 7) + 1
	return weekNum == targetWeek
}

func sendInvoiceReminder(workOrderID int, pattern models.RecurrencePattern, cronLogger *log.Logger) {
	logMsg := fmt.Sprintf("🔔 Auto-generate Invoice for WorkOrder #%d (Pattern: %+v)", workOrderID, pattern)
	log.Println(logMsg)
	if cronLogger != nil {
		cronLogger.Println(logMsg)
	}

	db := storage.InitDB()

	// Ensure history table exists
	if err := ensureElementInvoiceHistoryTable(db); err != nil {
		log.Printf("❌ Failed to ensure history table: %v", err)
		if cronLogger != nil {
			cronLogger.Printf("❌ Failed to ensure history table: %v", err)
		}
		return
	}

	// Determine time window from pattern
	start, end := determineTimeWindowFromPattern(pattern, time.Now())

	// Adjust start based on last_invoice_generated_at so we only bill new progress
	// Note: this is an extra guard in addition to history-based deduplication
	var lastInvAt sql.NullTime
	if err := db.QueryRow(`SELECT last_invoice_generated_at FROM work_order WHERE id=$1`, workOrderID).Scan(&lastInvAt); err != nil {
		log.Printf("[invoice-cron] warn: fetch last_invoice_generated_at failed for WO #%d: %v", workOrderID, err)
		if cronLogger != nil {
			cronLogger.Printf("[invoice-cron] warn: fetch last_invoice_generated_at failed for WO #%d: %v", workOrderID, err)
		}
	} else if lastInvAt.Valid {
		if lastInvAt.Time.After(start) {
			// Use strictly after the previous generation moment to avoid re-counting boundary rows
			start = lastInvAt.Time.Add(1 * time.Second)
		}
	}

	if err := autoGenerateInvoiceFromCron(db, workOrderID, start, end, cronLogger); err != nil {
		log.Printf("❌ Failed to auto-generate invoice for WO #%d: %v", workOrderID, err)
		if cronLogger != nil {
			cronLogger.Printf("❌ Failed to auto-generate invoice for WO #%d: %v", workOrderID, err)
		}
	} else {
		log.Printf("✅ Invoice auto-generated for WO #%d for period %s - %s", workOrderID, start.Format(time.RFC3339), end.Format(time.RFC3339))
		if cronLogger != nil {
			cronLogger.Printf("✅ Invoice auto-generated for WO #%d for period %s - %s", workOrderID, start.Format(time.RFC3339), end.Format(time.RFC3339))
		}
	}
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ensureElementInvoiceHistoryTable creates the element invoice history table if it does not exist.
func ensureElementInvoiceHistoryTable(db *sql.DB) error {
	log.Printf("[invoice-cron] ensuring table element_invoice_history exists")
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS element_invoice_history (
            id SERIAL PRIMARY KEY,
            work_order_id INT NOT NULL,
            element_id INT NOT NULL,
            stage TEXT NOT NULL,
            volume DOUBLE PRECISION NOT NULL,
            period_start TIMESTAMP NOT NULL,
            period_end TIMESTAMP NOT NULL,
            invoice_id INT,
            created_at TIMESTAMP NOT NULL DEFAULT NOW()
        )
    `)
	if err != nil {
		log.Printf("[invoice-cron] ensure table error: %v", err)
	} else {
		log.Printf("[invoice-cron] table element_invoice_history ready")
	}
	return err
}

// determineTimeWindowFromPattern returns [start,end] for which elements are considered in this billing cycle.
func determineTimeWindowFromPattern(p models.RecurrencePattern, now time.Time) (time.Time, time.Time) {
	// Default: previous day window
	start := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())

	switch p.PatternType {
	case "date":
		// Monthly: previous month window until today
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		start = firstOfMonth
		end = now
	case "week":
		// Weekly: last 7 days
		start = now.AddDate(0, 0, -6)
		end = now
	}
	return start, end
}

// autoGenerateInvoiceFromCron computes staged volumes using precast_stock and creates invoice + items.
func autoGenerateInvoiceFromCron(db *sql.DB, workOrderID int, periodStart, periodEnd time.Time, cronLogger *log.Logger) error {
	log.Printf("[invoice-cron] begin generation for WO #%d window %s -> %s", workOrderID, periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339))
	tx, err := db.Begin()
	if err != nil {
		log.Printf("[invoice-cron] tx begin error: %v", err)
		return err
	}
	defer func() {
		if err != nil {
			log.Printf("[invoice-cron] rolling back due to error: %v", err)
			tx.Rollback()
		}
	}()

	if cronLogger != nil {
		cronLogger.Printf("Starting autoGenerateInvoiceFromCron for WO #%d, period %s - %s", workOrderID, periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339))
	}

	// 1) Fetch WO context: endclient_id, project_id and created_by default (fallback to WO.created_by)
	var createdBy int
	var endClientID, projectID int
	var woNumber string
	if err = tx.QueryRow(`SELECT COALESCE(created_by,'0')::int, endclient_id, project_id, wo_number FROM work_order WHERE id=$1`, workOrderID).Scan(&createdBy, &endClientID, &projectID, &woNumber); err != nil {
		return fmt.Errorf("work order context: %w", err)
	}
	log.Printf("[invoice-cron] WO context: created_by=%d end_client=%d project=%d wo_number=%s", createdBy, endClientID, projectID, woNumber)

	// 2) Compute next revision_no for this work_order
	var revisionNo int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(revision_no), -1) + 1 FROM invoice WHERE work_order_id=$1`, workOrderID).Scan(&revisionNo); err != nil {
		return fmt.Errorf("revision_no: %w", err)
	}
	log.Printf("[invoice-cron] next revision_no=%d", revisionNo)

	// 3) Build invoice name from end client and project (abbreviations)
	var endClientName, projectName string
	if err = tx.QueryRow(`SELECT abbreviation FROM end_client WHERE id=$1`, endClientID).Scan(&endClientName); err != nil {
		return fmt.Errorf("end client name: %w", err)
	}
	if err = tx.QueryRow(`SELECT abbreviation FROM project WHERE project_id=$1`, projectID).Scan(&projectName); err != nil {
		return fmt.Errorf("project name: %w", err)
	}
	invoiceName := fmt.Sprintf("%s-%s-%d", endClientName, projectName, revisionNo)
	log.Printf("[invoice-cron] invoice name=%s", invoiceName)

	// 3b) Load payment terms (JSON) from work_order: e.g. {"casted":50,"dispatch":15,"erection":25,"handover":10}
	var paymentTermsJSON []byte
	paymentTerms := map[string]float64{}
	if errPT := tx.QueryRow(`SELECT payment_term FROM work_order WHERE id=$1`, workOrderID).Scan(&paymentTermsJSON); errPT != nil {
		log.Printf("[invoice-cron] payment_term fetch error (using defaults): %v", errPT)
	} else if len(paymentTermsJSON) > 0 {
		if err := json.Unmarshal(paymentTermsJSON, &paymentTerms); err != nil {
			log.Printf("[invoice-cron] payment_term JSON invalid (using defaults): %v", err)
		} else {
			log.Printf("[invoice-cron] payment_term loaded: %s", string(paymentTermsJSON))
		}
	}

	// 4) Billing/Shipping address from WO
	var billingAddress, shippingAddress string
	if err = tx.QueryRow(`SELECT billed_address, shipped_address FROM work_order WHERE id=$1`, workOrderID).Scan(&billingAddress, &shippingAddress); err != nil {
		return fmt.Errorf("addresses: %w", err)
	}
	log.Printf("[invoice-cron] addresses loaded (billing=%t, shipping=%t)", billingAddress != "", shippingAddress != "")

	// 5) Collect staged volumes by matching element -> (element_type -> name) and floor -> tower via precast
	aggregates, err := computeStagedAggregates(tx, workOrderID, projectID, periodStart, periodEnd)
	if err != nil {
		log.Printf("[invoice-cron] computeStagedAggregates error: %v", err)
		return err
	}
	log.Printf("[invoice-cron] aggregates count=%d", len(aggregates))
	if len(aggregates) == 0 {
		// Nothing to invoice this cycle
		log.Printf("[invoice-cron] no staged elements found in window; committing no-op")
		return tx.Commit()
	}

	// 6) Compute total_amount with stage rules from payment_term JSON
	var totalAmount float64
	for _, a := range aggregates {
		multiplier := 1.0
		// Normalize stage keys to match payment_term keys
		stageKey := strings.ToLower(a.stage)
		switch stageKey {
		case "dispatched":
			stageKey = "dispatch"
		case "casted", "dispatch", "erection", "handover":
			// keep as-is
		default:
			// unknown stage → default 100%
		}
		if pct, ok := paymentTerms[stageKey]; ok && pct >= 0 {
			multiplier = pct / 100.0
		}
		line := a.unitRate * a.volume * multiplier
		// tax included on line value (if required)
		if a.tax > 0 {
			line = line + (line * a.tax / 100.0)
		}
		totalAmount += line
		log.Printf("[invoice-cron] line: material_id=%d item=%s stage=%s pct=%.2f vol=%.3f rate=%.3f tax=%.3f multiplier=%.2f line_total=%.3f", a.materialID, a.itemName, a.stage, multiplier*100.0, a.volume, a.unitRate, a.tax, multiplier, line)
	}
	log.Printf("[invoice-cron] total_amount=%.3f", totalAmount)

	// 7) Insert invoice
	var invoiceID int
	if err = tx.QueryRow(`
        INSERT INTO invoice (work_order_id, created_by, revision_no, billing_address, shipping_address, name, total_amount)
        VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id
    `, workOrderID, createdBy, revisionNo, billingAddress, shippingAddress, invoiceName, totalAmount).Scan(&invoiceID); err != nil {
		return fmt.Errorf("insert invoice: %w", err)
	}
	log.Printf("[invoice-cron] created invoice id=%d", invoiceID)

	// 8) Insert invoice items and update volume_used, and record element history per element used
	for _, a := range aggregates {
		// Insert invoice_item (stores item_id=materialID, volume)
		if _, err = tx.Exec(`INSERT INTO invoice_item (invoice_id, item_id, volume) VALUES ($1,$2,$3)`, invoiceID, a.materialID, a.volume); err != nil {
			return fmt.Errorf("insert invoice_item: %w", err)
		}
		log.Printf("[invoice-cron] added invoice_item: invoice_id=%d item_id=%d volume=%.3f", invoiceID, a.materialID, a.volume)

		// Update work_order_material.volume_used
		if _, err = tx.Exec(`UPDATE work_order_material SET volume_used = COALESCE(volume_used,0) + $1 WHERE work_order_id=$2 AND id=$3`, a.volume, workOrderID, a.materialID); err != nil {
			return fmt.Errorf("update wom volume_used: %w", err)
		}
		log.Printf("[invoice-cron] updated wom volume_used: wom_id=%d +%.3f", a.materialID, a.volume)
	}

	// 9) Insert element history rows for deduplication in future cycles
	if err = recordElementHistory(tx, workOrderID, invoiceID, periodStart, periodEnd, projectID); err != nil {
		return err
	}

	// 10) Update work_order with last invoice references atomically within the same transaction
	if _, err = tx.Exec(`UPDATE work_order SET last_invoice_id=$1, last_invoice_generated_at=$2 WHERE id=$3`, invoiceID, periodEnd, workOrderID); err != nil {
		return fmt.Errorf("update work_order last invoice fields: %w", err)
	}
	log.Printf("[invoice-cron] updated work_order #%d last_invoice_id=%d last_invoice_generated_at=%s", workOrderID, invoiceID, periodEnd.Format(time.RFC3339))

	if err := tx.Commit(); err != nil {
		log.Printf("[invoice-cron] commit error: %v", err)
		return err
	}
	if cronLogger != nil {
		cronLogger.Printf("Completed autoGenerateInvoiceFromCron for WO #%d (invoice_id=%d)", workOrderID, invoiceID)
	}
	log.Printf("[invoice-cron] done for WO #%d (invoice_id=%d)", workOrderID, invoiceID)
	return nil
}

// computeStagedAggregates returns aggregated volumes per matched work_order_material for elements in given period per stage.
func computeStagedAggregates(tx *sql.Tx, workOrderID int, projectID int, start, end time.Time) ([]struct {
	materialID int
	itemName   string
	unitRate   float64
	tax        float64
	hsnCode    sql.NullInt64
	stage      string
	volume     float64
}, error) {
	// We gather eligible elements per stage and sum volumes. Exclude elements already in history for that stage.
	// Assumptions:
	// - precast_stock(element_id, element_type_id, target_location, stockyard, dispatch, erected, recieve_in_erection, starting_date, dispatch_end, updated_at, project_id)
	// - element_type(element_type_id, element_type, volume)
	// - precast(id, parent_id) for hierarchy; target_location corresponds to floor id; tower id is parent_id
	// - work_order_material(id, work_order_id, item_name, unit_rate, tax, floor_id int[])

	// We'll union stages into a temp table with columns: element_id, stage, ele_volume, floor_id
	// Then join to element_type for per-element volume and to work_order_material by (item_name=element_type.element_type) and floor_id ANY(floor_id)

	// Create temp table
	log.Printf("[invoice-cron] creating temp table tmp_stage_elements")
	_, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS tmp_stage_elements (
	           element_id INT,
	           stage TEXT,
	           timestamp TIMESTAMP,
	           floor_id INT
	       ) ON COMMIT DROP`)
	if err != nil {
		return nil, fmt.Errorf("create temp table: %w", err)
	}

	// Clear previous rows
	if _, err = tx.Exec(`DELETE FROM tmp_stage_elements`); err != nil {
		return nil, fmt.Errorf("clear temp table: %w", err)
	}
	log.Printf("[invoice-cron] temp table cleared")

	// Insert Casted
	// Billable Only
	if res, err2 := tx.Exec(`
	       INSERT INTO tmp_stage_elements(element_id, stage, timestamp, floor_id)
	       SELECT ps.element_id, 'casted', ps.production_date, ps.target_location
	       FROM precast_stock ps
		   JOIN element e ON e.id = ps.element_id
	       WHERE ps.project_id = $1
		   	 AND e.billable = true                                                
	         AND COALESCE(ps.stockyard,false) = false
	         AND ps.production_date >= $2 AND ps.production_date <= $3
	         AND NOT EXISTS (
	             SELECT 1 FROM element_invoice_history h
	             WHERE h.element_id = ps.element_id AND h.stage = 'casted'
	               AND h.work_order_id = $4
	         )
	`, projectID, start, end, workOrderID); err2 != nil {
		return nil, fmt.Errorf("temp stage fill (casted): %w", err2)
	} else {
		rc, _ := res.RowsAffected()
		log.Printf("[invoice-cron] staged casted rows=%d", rc)
	}

	// Insert Dispatched
	if res, err2 := tx.Exec(`
	       INSERT INTO tmp_stage_elements(element_id, stage, timestamp, floor_id)
	       SELECT ps.element_id, 'dispatched', ps.dispatch_end, ps.target_location
	       FROM precast_stock ps
		   JOIN element e ON e.id = ps.element_id
	       WHERE ps.project_id = $1
		   	 AND e.billable = true
	         AND COALESCE(ps.stockyard,false) = true
	         AND COALESCE(ps.dispatch_status,true) = true
	         AND ps.dispatch_end >= $2 AND ps.dispatch_end <= $3
	         AND NOT EXISTS (
	             SELECT 1 FROM element_invoice_history h
	             WHERE h.element_id = ps.element_id AND h.stage = 'dispatched'
	               AND h.work_order_id = $4
	         )
	`, projectID, start, end, workOrderID); err2 != nil {
		return nil, fmt.Errorf("temp stage fill (dispatched): %w", err2)
	} else {
		rc, _ := res.RowsAffected()
		log.Printf("[invoice-cron] staged dispatched rows=%d", rc)
	}

	// Insert Erection
	if res, err2 := tx.Exec(`
	       INSERT INTO tmp_stage_elements(element_id, stage, timestamp, floor_id)
	       SELECT ps.element_id, 'erection', ps.updated_at, ps.target_location
	       FROM precast_stock ps
		   JOIN element e ON e.id = ps.element_id
	       WHERE ps.project_id = $1
	         AND e.billable = true
	         AND COALESCE(ps.stockyard,false) = true
	         AND COALESCE(ps.dispatch_status,true) = true
	         AND COALESCE(ps.erected,false) = true
	         AND ps.updated_at >= $2 AND ps.updated_at <= $3
	         AND NOT EXISTS (
	             SELECT 1 FROM element_invoice_history h
	             WHERE h.element_id = ps.element_id AND h.stage = 'erection'
	               AND h.work_order_id = $4
	         )
	`, projectID, start, end, workOrderID); err2 != nil {
		return nil, fmt.Errorf("temp stage fill (erection): %w", err2)
	} else {
		rc, _ := res.RowsAffected()
		log.Printf("[invoice-cron] staged erection rows=%d", rc)
	}

	// Insert Handover
	if res, err2 := tx.Exec(`
	       INSERT INTO tmp_stage_elements(element_id, stage, timestamp, floor_id)
	       SELECT ps.element_id, 'handover', ps.updated_at, ps.target_location
	       FROM precast_stock ps
		   JOIN element e ON e.id = ps.element_id
	       WHERE ps.project_id = $1
	         AND e.billable = true\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\\
	         AND COALESCE(ps.stockyard,false) = true
	         AND COALESCE(ps.dispatch_status,true) = true
	         AND COALESCE(ps.erected,false) = true
	         AND COALESCE(ps.recieve_in_erection,false) = true
	         AND ps.updated_at >= $2 AND ps.updated_at <= $3
	         AND NOT EXISTS (
	             SELECT 1 FROM element_invoice_history h
	             WHERE h.element_id = ps.element_id AND h.stage = 'handover'
	               AND h.work_order_id = $4
	         )
	`, projectID, start, end, workOrderID); err2 != nil {
		return nil, fmt.Errorf("temp stage fill (handover): %w", err2)
	} else {
		rc, _ := res.RowsAffected()
		log.Printf("[invoice-cron] staged handover rows=%d", rc)
	}

	// Aggregate by matching materials
	log.Printf("[invoice-cron] aggregating staged elements → materials")
	rows, err := tx.Query(`
        SELECT wom.id AS material_id,
               wom.item_name,
               wom.unit_rate,
               COALESCE(wom.tax,0) AS tax,
               wom.hsn_code,
               tse.stage,
               SUM(et.volume) AS total_volume
        FROM tmp_stage_elements tse
        JOIN element e ON e.id = tse.element_id
        JOIN element_type et ON et.element_type_id = e.element_type_id AND et.project_id = $1
        JOIN work_order_material wom ON wom.work_order_id = $2
            AND LOWER(wom.item_name) = LOWER(et.element_type)
            AND (
                wom.floor_id IS NULL
                OR array_length(wom.floor_id, 1) = 0
                OR tse.floor_id = ANY(wom.floor_id)
            )
        GROUP BY wom.id, wom.item_name, wom.unit_rate, wom.hsn_code, tse.stage, wom.tax
    `, projectID, workOrderID)
	if err != nil {
		return nil, fmt.Errorf("aggregate query: %w", err)
	}
	defer rows.Close()

	var out []struct {
		materialID int
		itemName   string
		unitRate   float64
		tax        float64
		hsnCode    sql.NullInt64
		stage      string
		volume     float64
	}
	for rows.Next() {
		var a struct {
			materialID int
			itemName   string
			unitRate   float64
			tax        float64
			hsnCode    sql.NullInt64
			stage      string
			volume     float64
		}
		if err := rows.Scan(&a.materialID, &a.itemName, &a.unitRate, &a.tax, &a.hsnCode, &a.stage, &a.volume); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	log.Printf("[invoice-cron] aggregation rows=%d", len(out))
	return out, nil
}

// recordElementHistory writes one row per element per stage for elements in tmp table for deduplication.
func recordElementHistory(tx *sql.Tx, workOrderID int, invoiceID int, start, end time.Time, projectID int) error {
	// Insert history rows for all elements and their stages used in this cycle
	res, err := tx.Exec(`
        INSERT INTO element_invoice_history (work_order_id, element_id, stage, volume, period_start, period_end, invoice_id)
        SELECT $1, tse.element_id, tse.stage, et.volume, $2, $3, $4
        FROM tmp_stage_elements tse
        JOIN element e ON e.id = tse.element_id
        JOIN element_type et ON et.element_type_id = e.element_type_id AND et.project_id = $5
    `, workOrderID, start, end, invoiceID, projectID)
	if err == nil {
		if rc, e2 := res.RowsAffected(); e2 == nil {
			log.Printf("[invoice-cron] history rows inserted=%d (invoice_id=%d)", rc, invoiceID)
		}
	}
	return err
}

var cronRunning int32

func safeGo(
	ctx context.Context,
	wg *sync.WaitGroup,
	name string,
	fn func(context.Context) error,
	cronLogger *log.Logger,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in %s: %v\n%s", name, r, debug.Stack())
				if cronLogger != nil {
					cronLogger.Printf("PANIC in %s: %v\n%s", name, r, debug.Stack())
				}
			}
		}()

		if err := fn(ctx); err != nil {
			log.Printf("%s failed: %v", name, err)
			if cronLogger != nil {
				cronLogger.Printf("%s failed: %v", name, err)
			}
		} else {
			log.Printf("%s completed successfully", name)
			if cronLogger != nil {
				cronLogger.Printf("%s completed successfully", name)
			}
		}
	}()
}

// ginPathToSwaggerPath converts Gin path params :param to Swagger {param}
var ginPathParamRe = regexp.MustCompile(`:([^/]+)`)

func ginPathToSwaggerPath(path string) string {
	return ginPathParamRe.ReplaceAllString(path, "{$1}")
}

// Common API response/request models for Swagger so Example Value and Model show real JSON structure.
var swaggerDefinitions = map[string]interface{}{
	"ApiResponseDataItem": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":           map[string]interface{}{"type": "integer", "example": 425},
			"project_id":   map[string]interface{}{"type": "integer", "example": 731623920},
			"stockyard_id": map[string]interface{}{"type": "integer", "example": 23},
			"user_id":      map[string]interface{}{"type": "integer", "example": 320},
			"created_at":   map[string]interface{}{"type": "string", "format": "date-time", "example": "2026-01-28T05:49:18.445326Z"},
			"updated_at":   map[string]interface{}{"type": "string", "format": "date-time", "example": "2026-02-04T12:26:17.582917Z"},
			"project_name": map[string]interface{}{"type": "string", "example": "Suraksha"},
			"yard_name":    map[string]interface{}{"type": "string", "example": "Blueinvent Technologies"},
			"user_name":    map[string]interface{}{"type": "string", "example": "Suraksha Stockyard1"},
		},
	},
	"ApiResponse": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"data": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"$ref": "#/definitions/ApiResponseDataItem"},
				"description": "List of items (structure may vary by endpoint)",
			},
			"message": map[string]interface{}{"type": "string", "example": "stockyards fetched successfully"},
		},
	},
	"ApiRequest": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project_id":   map[string]interface{}{"type": "integer", "example": 731623920},
			"stockyard_id": map[string]interface{}{"type": "integer", "example": 23},
			"user_id":      map[string]interface{}{"type": "integer", "example": 320},
		},
		"description": "Request body (fields may vary by endpoint)",
	},
}

// buildSwaggerFromRoutes returns a handler that serves Swagger 2.0 JSON with all registered routes.
// Uses JSON models so each API shows input/output structure and example value.
func buildSwaggerFromRoutes(engine *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		paths := make(map[string]interface{})
		routes := engine.Routes()
		for _, route := range routes {
			if strings.HasPrefix(route.Path, "/swagger") {
				continue
			}
			path := ginPathToSwaggerPath(route.Path)
			if paths[path] == nil {
				paths[path] = make(map[string]interface{})
			}
			method := strings.ToLower(route.Method)

			op := map[string]interface{}{
				"summary":     route.Method + " " + route.Path,
				"description": "API endpoint: " + route.Path,
				"tags":        []string{"API"},
				"produces":    []string{"application/json"},
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Success - returns JSON",
						"schema":      map[string]interface{}{"$ref": "#/definitions/ApiResponse"},
					},
					"400": map[string]interface{}{
						"description": "Bad Request",
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"error": map[string]interface{}{"type": "string"},
							},
						},
					},
					"500": map[string]interface{}{
						"description": "Internal Server Error",
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"error": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			}

			// POST/PUT/PATCH: request body with JSON model
			if method == "post" || method == "put" || method == "patch" {
				op["consumes"] = []string{"application/json"}
				op["parameters"] = []map[string]interface{}{
					{
						"in":          "body",
						"name":        "body",
						"required":    true,
						"description": "JSON body. See model below; fields may vary by endpoint.",
						"schema":      map[string]interface{}{"$ref": "#/definitions/ApiRequest"},
					},
				}
			}

			(paths[path].(map[string]interface{}))[method] = op
		}
		doc := map[string]interface{}{
			"swagger":     "2.0",
			"definitions": swaggerDefinitions,
			"info": map[string]interface{}{
				"title":       "Precast API",
				"description": "Precast Backend API. Response model: { data: [], message }. Request body model shown for POST/PUT/PATCH.",
				"version":     "1.0",
			},
			"host":     c.Request.Host,
			"basePath": "/",
			"schemes":  []string{"http", "https"},
			"paths":    paths,
		}
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, doc)
	}
}

func main() {
	db := storage.InitDB()
	// Initialize GORM database
	_ = storage.InitGormDB()

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Initialize Firebase Cloud Messaging service using HTTP v1 API
	credentialsPath := os.Getenv("FCM_CREDENTIALS_PATH")
	if credentialsPath == "" {
		credentialsPath = "firebase-credentials.json" // Default path
	}
	fcmService, err := services.NewFCMService(credentialsPath, db)
	if err != nil {
		log.Printf("Warning: Failed to initialize FCM service: %v. Push notifications will be disabled.", err)
		fcmService = nil
	} else {
		log.Println("FCM service initialized successfully")
	}

	// Set global FCM service for handlers
	handlers.SetFCMService(fcmService)

	// Setup cron job to run maintenance daily at 12:30 PM
	c := cron.New(
		cron.WithLogger(cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))),
	)

	// Open a file for cron error logging
	cronLogFile, err := os.OpenFile("cron_errors.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Failed to open cron error log file: %v", err)
	}
	cronLogger := log.New(cronLogFile, "CRON_ERROR: ", log.LstdFlags)

	_, err = c.AddFunc("50 11 * * *", func() {
		// ------------------ CRON LOCK ------------------
		if !atomic.CompareAndSwapInt32(&cronRunning, 0, 1) {
			log.Println("Previous cron still running. Skipping this run.")
			if cronLogger != nil {
				cronLogger.Println("Previous cron still running. Skipping this run.")
			}
			return
		}
		defer atomic.StoreInt32(&cronRunning, 0)

		log.Println("Starting daily maintenance cron job (11:50 AM IST)")
		if cronLogger != nil {
			cronLogger.Println("Starting daily maintenance cron job (11:50 AM IST)")
		}

		// ------------------ TIMEOUT CONTEXT ------------------
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()

		var wg sync.WaitGroup

		// ------------------ LIGHTWEIGHT JOBS (PARALLEL) ------------------

		safeGo(ctx, &wg, "CleanupExpiredSessions", func(ctx context.Context) error {
			return storage.CleanupExpiredSessions(db)
		}, cronLogger)

		safeGo(ctx, &wg, "ProjectSuspensionJob", func(ctx context.Context) error {
			return runSuspensionJob(db)
		}, cronLogger)

		safeGo(ctx, &wg, "WorkOrderRecurrenceNotifications", func(ctx context.Context) error {
			return RunWorkOrderRecurrenceNotifications(db, cronLogger)
		}, cronLogger)

		// ------------------ HEAVY JOBS (SEQUENTIAL) ------------------

		safeGo(ctx, &wg, "AutoCreateTasks", func(ctx context.Context) error {
			AutoCreateTasks(db)
			return nil
		}, cronLogger)

		safeGo(ctx, &wg, "CompleteActivityToStockyard", func(ctx context.Context) error {
			return CompleteActivityToStockyard(db)
		}, cronLogger)

		safeGo(ctx, &wg, "ErectedHandler", func(ctx context.Context) error {
			return ErectedHandler(db)
		}, cronLogger)

		// ------------------ WAIT WITH CANCELLATION ------------------

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("All cron jobs finished")
			if cronLogger != nil {
				cronLogger.Println("All cron jobs finished")
			}
		case <-ctx.Done():
			log.Println("Cron timeout reached, jobs cancelled")
			if cronLogger != nil {
				cronLogger.Println("Cron timeout reached, jobs cancelled")
			}
		}

		log.Println("Daily cron cycle completed")
		if cronLogger != nil {
			cronLogger.Println("Daily cron cycle completed")
		}
	})
	if err != nil {
		log.Fatalf("Failed to schedule daily maintenance cron job: %v", err)
	}

	c.Start()

	r := gin.Default()
	r.MaxMultipartMemory = 8 << 20

	r.Use(cors.New(CORSConfig()))

	r.GET("/api/project/:project_id/status", CheckProjectSuspension(db), handlers.GetProjectStatus(db))

	r.POST("/api/solve", handlers.SolveHandler)

	// ==================== 1. AUTH & LOGIN ====================
	r.POST("/api/login", handlers.LoginHandler(db))
	r.POST("/api/refresh-token", handlers.RefreshTokenHandler(db))
	r.POST("/api/validate-session", handlers.ValidateSession(db))
	r.GET("/api/session/:user_id", handlers.GetSessionHandler(db))
	r.DELETE("/api/session/:user_id", handlers.DeleteSessionHandler(db))
	r.GET("/api/active-devices", handlers.GetActiveDevicesHandler(db))
	r.POST("/api/logout-device", handlers.LogoutDeviceHandler(db))

	// ==================== 2. USERS ====================
	r.POST("/api/create_user", handlers.CreateUser(db))
	r.PUT("/api/update_user/:id", handlers.UpdateUser(db))
	r.GET("/api/user_fetch/:id", handlers.GetUser(db))
	r.GET("/api/users", handlers.GetAllUsers(db))
	r.DELETE("/api/user_delete/:id", handlers.DeleteUser(db))
	r.GET("/api/get_user", handlers.GetUserFromSession(db))

	// ==================== 3. USER SETTINGS ====================
	r.POST("/api/settings", handlers.CreateSettingHandler(db))
	r.GET("/api/settings/:user_id", handlers.GetSettingHandler(db))

	// ==================== 4. ROLES & PERMISSIONS ====================
	r.POST("/api/roles", handlers.CreateRole(db))
	r.GET("/api/roles", handlers.GetRoles(db))
	r.PUT("/api/roles/:id", handlers.UpdateRole(db))
	r.DELETE("/api/roles/:id", handlers.DeleteRole(db))
	r.POST("/api/permissions", handlers.CreatePermission(db))
	r.GET("/api/permissions", handlers.GetPermissions(db))
	r.PUT("/api/permissions/:id", handlers.UpdatePermission(db))
	r.DELETE("/api/permissions/:id", handlers.DeletePermission(db))
	r.POST("/api/role-permissions", handlers.CreateRolePermission(db))
	r.GET("/api/role-permissions", handlers.GetRolePermissions(db))
	r.GET("/api/role-permissions/:role_id", handlers.GetRolePermissionByRoleID(db))
	r.PUT("/api/role-permissions", handlers.UpdateRolePermission(db))
	r.DELETE("/api/role-permissions/:id", handlers.DeleteRolePermission(db))

	// ==================== 5. PROJECTS ====================
	r.POST("/api/project_create", handlers.CreateProject(db))
	r.PUT("/api/project_update", handlers.UpdateeProject(db))
	r.DELETE("/api/project_delete/:id", handlers.DeleteProject(db))
	r.GET("/api/project_fetch/:id", handlers.FetchProject(db))
	r.GET("/api/projects", handlers.FetchAllProjects(db))
	r.GET("/api/project_get/:project_id", CheckProjectSuspension(db), handlers.GetProject(db))
	r.GET("/api/project_by_role", handlers.GetProjectsByRole(db))
	r.GET("/api/project_roles/:project_id", handlers.GetProjectRoles(db))
	r.PUT("/api/project_update/:project_id", CheckProjectSuspension(db), handlers.UpdateProject(db))

	// ==================== 6. DRAWINGS & REVISIONS ====================
	r.GET("/api/drawings_by_element_type/:element_type_id", handlers.GetDrawingsByElementType)
	r.GET("/api/drawing_revision_fetch/:project_id", CheckProjectSuspension(db), handlers.GetDrawingRevisionByProjectID)
	r.GET("/api/drawing_revision_get/:drawing_revision_id", handlers.GetDrawingRevisionByRevisionId)
	r.PUT("/api/drawing_update", handlers.UpdateDrawingHandler(db))
	r.DELETE("/api/drawing_delete/:id", handlers.DeleteDrawing(db))
	r.GET("/api/drawing", handlers.GetAllDrawings(db))
	r.GET("/api/drawing_fetch/:project_id", CheckProjectSuspension(db), handlers.GetDrawingsByProjectID(db))
	r.GET("/api/drawing_get/:drawing_id", handlers.GetDrawingByDrawingID(db))

	// ==================== 7. DRAWING TYPES ====================
	r.POST("/api/drawingtype_create", handlers.CreateDrawingType(db))
	r.GET("/api/drawingtype", handlers.GetAllDrawingType(db))
	r.GET("/api/drawingtype/:project_id", CheckProjectSuspension(db), handlers.GetAllDrawingTypeByprojectid(db))
	r.PUT("/api/drawingtype_update/:id", handlers.UpdateDrawingType(db))
	r.GET("/drawing-type/:id", handlers.GetDrawingTypeByID(db))

	// ==================== 8. ELEMENT TYPES ====================
	//r.GET("/api/elementtype_fetch_get/:project_id", CheckProjectSuspension(db), RBACMiddleware(db, "viewelementtype"), handlers.FetchElementTypeAndDrawingByProjectID(db))
	r.GET("/api/elementtype_get/:element_type_id", handlers.FetchElementTypeByID)

	r.GET("/api/elementtype_name", handlers.FetchElementTypesName)
	r.GET("/api/element-types/search", handlers.SearchElementTypes(db))
	r.POST("/api/elementtype_create", handlers.CreateElementType(db))
	r.PUT("/api/elementtype_update/:element_type_id", handlers.UpdateElementType(db))
	r.DELETE("/api/elementtype_delete/:id", handlers.DeleteElementType)
	r.GET("/api/get_element_type_quantity/:project_id", CheckProjectSuspension(db), handlers.GetElementTypeQuantity(db))
	//r.GET("/api/elementtype/fetch/:project_id", CheckProjectSuspension(db), handlers.GetElementTypesByProjectWith(db))
	//r.POST("/api/elementtype_rollback/:project_id", CheckProjectSuspension(db), handlers.RollbackAllElementTypeData(db))
	r.GET("/api/get_element_type/quantity/:project_id", handlers.GetElementTypeQuantity(db))
	r.GET("/api/elementtype_fetch/:project_id", handlers.GetElementTypesByProjectWith(db))

	// ==================== 9. ELEMENTS ====================
	r.GET("/api/fetch_element_type_name", handlers.GetAllelementType)
	r.POST("/api/element_type_name_create", handlers.CreateElementTypeName)
	r.POST("/api/element_create", handlers.Element)
	r.GET("/api/element_fetch/:element_id", handlers.GetElementsWithDrawingsByElementId(db))
	r.GET("/api/element_get/:project_id", CheckProjectSuspension(db), handlers.GetElementsWithDrawingsByProjectId(db))
	r.GET("/api/element/:element_type_id", handlers.GetElementsByElementTypeID(db))
	r.GET("/api/element", handlers.GetAllElementsWithDrawings(db))
	r.GET("/api/elements", handlers.GetAllElements(db))
	//r.PUT("/api/element_update/:id", handlers.UpdateElement(db))
	r.DELETE("/api/element_delete/:id", handlers.DeleteElement(db))

	// ==================== 10. CLIENTS ====================
	r.GET("/api/client_fetch/:client_id", handlers.GetClientByID(db))
	r.GET("/api/client", handlers.GetAllClient(db))
	r.GET("/api/client_search", handlers.SearchClients(db))
	r.POST("/api/client_create", handlers.CreateClient(db))
	r.PUT("/api/client_update", handlers.UpdateClient(db))
	//r.DELETE("/client_delete/:id", RBACMiddleware(db, "delete_client"), handlers.Deletec(db))

	// ==================== 11. QC STATUSES ====================
	r.GET("/api/qcstatuses_fetch", handlers.GetAllQCStatuses(db))
	r.GET("/api/qcstatuses/:id", handlers.GetQCStatus(db))
	r.POST("/api/qcstatuses_create", handlers.CreateQCStatus(db))
	r.PUT("/api/qcstatuses_update/:id", handlers.UpdateQCStatus(db))
	r.DELETE("/api/qcstatuses_delete/:id", handlers.DeleteQCStatus(db))

	// ==================== 12. INVENTORY ====================
	r.POST("/api/inventory_create", handlers.CreatePurchase(db))
	r.POST("/api/inventory_check_shortage", handlers.CheckInventoryShortage(db))
	r.POST("/api/inventory_generate_purchase_request", handlers.GeneratePurchaseRequest(db))
	r.GET("/api/inventory_shortage_summary", handlers.GetInventoryShortageSummary(db))
	r.GET("/api/inv_purchases", handlers.FetchAllInvPurchases(db))
	r.GET("/api/inv_purchases/:id", handlers.FetchInvPurchaseByID(db))
	r.GET("/api/invlineitems", handlers.FetchAllInvLineItems(db))
	r.GET("/api/invlineitems/:id", handlers.FetchInvLineItemByID(db))
	r.GET("/api/invtransactions", handlers.FetchAllInvTransactions(db))
	r.GET("/api/invtransactions/:id", handlers.FetchInvTransactionByID(db))
	r.GET("/api/invtracks", handlers.FetchAllInvTracks(db))
	r.GET("/api/invtracks/:id", handlers.FetchInvTrackByID(db))
	r.GET("/api/invatory_view", handlers.InventoryView(db))
	r.GET("/api/invatory_view/:project_id", CheckProjectSuspension(db), handlers.InventoryViewProjectId(db))
	r.GET("/api/invatory_view_each_bom/:bom_id", handlers.InventoryViewEachBOM(db))

	// ==================== 13. WAREHOUSES ====================
	r.GET("/api/get_warehouses", handlers.GetWarehouses(db))
	r.GET("/api/get_warehouses/:id", handlers.GetWarehouseById(db))
	r.GET("/api/fetch_warehouses/:project_id", CheckProjectSuspension(db), handlers.GetWarehousesProjectId(db))
	r.POST("/api/create_warehouses", handlers.CreateWarehouse(db))
	r.PUT("/api/update_warehouses/:id", handlers.UpdateWarehouse(db))
	r.DELETE("/api/delete_warehouses/:id", handlers.DeleteWarehouse(db))

	// ==================== 14. VENDORS ====================
	r.GET("/api/get_vendor", handlers.GetVendors(db))
	r.GET("/api/get_vendor/:id", handlers.GetVendorByID(db))
	r.GET("/api/fetch_vendor/:project_id", CheckProjectSuspension(db), handlers.GetVendorsProjectId(db))
	r.POST("/api/create_Vendor", handlers.CreateVendor(db))
	r.PUT("/api/update_Vendor/:id", handlers.UpdateVendor(db))
	r.DELETE("/api/delete_Vendor/:id", handlers.DeleteVendor(db))

	// ==================== 15. PROJECT MEMBERS ====================
	r.POST("/api/create_project_members", handlers.CreateMember(db))
	r.GET("/api/project/:project_id/members", handlers.GetMembers(db))
	r.PUT("/api/update_project_members/:project_id", CheckProjectSuspension(db), handlers.UpdateMember(db))
	r.DELETE("/api/project/:project_id/members/:user_id", CheckProjectSuspension(db), handlers.DeleteMember(db))

	r.GET("/api/project_member_exports/:project_id", handlers.ExportMembersPDF(db))

	// ==================== 16. FILE UPLOAD ====================
	r.POST("/api/upload", handlers.UploadFile)

	r.GET("/api/get-file", handlers.ServeFile)

	// ==================== 17. BOM PRODUCTS ====================
	r.POST("/api/create_bom_products", handlers.CreateBOMProduct(db))
	r.GET("/api/get_bom_products", handlers.GetAllBOMProducts(db))
	r.GET("/api/get_bom_products/:id", handlers.GetBOMProductByID(db))
	r.GET("/api/fetch_bom_products/:project_id", CheckProjectSuspension(db), handlers.GetAllBOMProductsProjectId(db))
	r.PUT("/api/update_bom_products/:id", handlers.UpdateBOMProduct(db))
	r.DELETE("/api/delete_bom_products/:id", handlers.DeleteBOMProduct(db))
	r.GET("/api/get_bom_master_products", handlers.GetAllBOMMasterProducts(db))
	r.GET("/api/bom_uses_list_pdf/:project_id", CheckProjectSuspension(db), handlers.BOMUsesListPDF(db))
	// ==================== 18. ELEMENT TYPE BOM ====================
	r.GET("/api/get_bom", handlers.GetAllBOMPros(db))
	r.GET("/api/get_bom/:id", handlers.GetBOMPro(db))
	r.GET("/api/bom_fetch/:element_type_id", handlers.GetBOMProByElementTypeID(db))
	r.GET("/api/bom_get_fetch/:project_id", CheckProjectSuspension(db), handlers.GetBOMProByProjectId(db))
	//r.GET("/api/elements_with_updated_bom/:element_type_id", handlers.GetElementsWithUpdatedBOM(db))

	// ==================== 19. INVENTORY ADJUSTMENT ====================
	r.GET("/api/element_types_with_updated_bom/:project_id", CheckProjectSuspension(db), handlers.GetElementTypesWithUpdatedBOMAndCompletedElements(db))
	r.GET("/api/element_types_with_updated_bom/:project_id/:element_type_id", CheckProjectSuspension(db), handlers.GetElementTypesWithUpdatedBOMByProjectAndElementType(db))
	r.GET("/api/inventory-adjustment-logs/:project_id", CheckProjectSuspension(db), handlers.GetInventoryAdjustmentLogs(db))
	r.POST("/api/inventory-adjustment", CheckProjectSuspension(db), handlers.CreateInventoryAdjustmentWithBOM(db))

	// ==================== 20. PRECAST HIERARCHY ====================
	r.GET("/api/precast", handlers.GetHierarchy)
	r.GET("/api/get_precast/:id", handlers.GetHierarchyByID)
	r.POST("/api/create_precast", handlers.InsertPrecast)
	r.PUT("/api/update_precast/:id", handlers.UpdatePrecast)
	r.DELETE("/api/delete_precast/:id", handlers.DeletePrecast)
	r.GET("/api/get_precast_project/:project_id", CheckProjectSuspension(db), handlers.GetHierarchyByProjectID)
	r.GET("/api/precast/parents_names/:project_id", CheckProjectSuspension(db), handlers.GetPrecastNamesWithNullParent)
	r.GET("/api/precast/floors/:project_id/:parent_id", CheckProjectSuspension(db), handlers.GetFloorNamesWithIDs)

	// ==================== 21. KANBAN – MILESTONES ====================
	r.GET("/api/get_milestones/:project_id", CheckProjectSuspension(db), handlers.GetAllMilestonesHandler(db))
	r.GET("/api/get_milestone/:project_id/:id", CheckProjectSuspension(db), handlers.GetMilestoneHandler(db))
	r.POST("/api/create_milestone", handlers.CreateMilestoneHandler(db))
	r.PUT("/api/update_milestone/:id", handlers.UpdateMilestoneHandler(db))
	r.DELETE("/api/delete_milestone/:id", handlers.DeleteMilestoneHandler(db))

	// ==================== 22. KANBAN – TASK TYPES ====================
	r.GET("/api/get_tasktypes/:project_id", CheckProjectSuspension(db), handlers.GetAllTaskTypesHandler(db))
	r.GET("/api/get_tasktype/:project_id/:id", CheckProjectSuspension(db), handlers.GetTaskTypeHandler(db))
	r.POST("/api/create_tasktype/", handlers.CreateTaskTypeHandler(db))
	r.PUT("/api/update_tasktype/:id", handlers.UpdateTaskTypeHandler(db))
	r.DELETE("/api/delete_tasktype/:id", handlers.DeleteTaskTypeHandler(db))

	// ==================== 23. KANBAN – TASKS ====================
	r.GET("/api/get_alltasks/:project_id", CheckProjectSuspension(db), handlers.GetAllTasks(db))
	r.GET("/api/get_task/:project_id/:task_id", CheckProjectSuspension(db), handlers.GetTaskHandler(db))
	r.POST("/api/create_task/", handlers.CreateTaskHandler(db))
	r.PUT("/api/update_task/:id", handlers.UpdateTaskHandler(db))
	r.DELETE("/api/delete_task/:id", handlers.DeleteTaskHandler(db))

	// ==================== 24. KANBAN – ACTIVITY ====================
	r.GET("/api/get_activity/:project_id", CheckProjectSuspension(db), handlers.GetActivityHandlerByAssignee(db))
	r.GET("/api/get_allactivity/:project_id", CheckProjectSuspension(db), handlers.GetActivityHandlerWithElement(db))
	r.GET("/api/get_alllist_tasks/:project_id", CheckProjectSuspension(db), handlers.GetAllListTask(db))
	r.PUT("/api/update_activity_status", handlers.UpdateActivityStatusHandler(db))

	// ==================== 25. CSV/EXCEL IMPORT ====================
	r.POST("/api/import_csv_bom/:project_id", CheckProjectSuspension(db), handlers.ImportCSVBOM)
	r.POST("/api/import_csv/:project_id", CheckProjectSuspension(db), handlers.ImportCSVPrecast)
	r.POST("/api/import_csv_element_type/:project_id", CheckProjectSuspension(db), handlers.ImportElementTypeCSVHandler)
	r.POST("/api/project/:project_id/import/excel", CheckProjectSuspension(db), handlers.ImportElementTypeExcelHandler)
	//r.POST("/api/project/:project_id/update/excel", CheckProjectSuspension(db), handlers.UpdateElementTypeExcelHandler)
	//r.GET("/api/project/:project_id/export/excel", handlers.ExportElementTypeExcelHandler)

	// ==================== 26. JOB MANAGEMENT ====================
	jobManager := handlers.NewGormJobManager()
	r.POST("/api/project/:project_id/jobs", func(c *gin.Context) {
		jobID, err := jobManager.CreateImportJobAndGetID(c, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "Import job created successfully",
			"job_id":  jobID,
			"status":  "pending",
		})
	})
	r.GET("/api/jobs/:job_id", jobManager.GetJobStatus)

	// Test endpoint to check if import_jobs table exists
	r.GET("/api/test/import-jobs-table", func(c *gin.Context) {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM import_jobs").Scan(&count)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "import_jobs table not found or error accessing it",
				"details": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "import_jobs table exists",
			"count":   count,
		})
	})
	r.GET("/api/project/:project_id/jobs", CheckProjectSuspension(db), jobManager.GetJobsByProject)
	r.GET("/api/jobs/pending-processing/:project_id", CheckProjectSuspension(db), handlers.GetPendingAndProcessingJobsGorm(storage.GetGormDB()))

	r.DELETE("/api/jobs/:job_id/terminate", jobManager.TerminateJobAndRollback)
	r.GET("/api/jobs/running", jobManager.ListRunningJobs)
	r.POST("/api/jobs/:job_id/enable-rollback", handlers.EnableRollbackForJob(db))
	r.POST("/api/rollback/element_type/:project_id/:job_id", handlers.RollbackAllElementTypeDataWithGorm(db))

	// ==================== 27. EXPORT (CSV/EXCEL) ====================
	r.GET("/api/export_csv_bom", handlers.ExportCSVBOM)
	r.GET("/api/export_csv_precast", handlers.ExportCSVPrecast)
	//r.GET("/api/export_excel_element_type/:project_id", handlers.ExportExcellementType)
	r.POST("/api/export_excel_element_type/:project_id", handlers.ExportExcellementType)
	//r.GET("/api/export_excel_element_type_prefield/:project_id", handlers.ExportCSVElementTypePreField)
	r.GET("/api/export/element_type/csv/:project_id", CheckProjectSuspension(db), handlers.ExportCSVElementType)

	// ==================== 28. ROLES (ALT ROUTES) ====================
	r.GET("/api/get_roles", handlers.GetRoles(db))
	r.POST("/api/create_role", handlers.CreateRole(db))
	r.PUT("/api/update_role/:id", handlers.UpdateRole(db))
	r.DELETE("/api/delete_role/:id", handlers.DeleteRole(db))
	r.GET("/api/get_role/:id", handlers.GetRolesByProjectID(db))

	// ==================== 29. PERMISSIONS (ALT ROUTES) ====================
	r.POST("/api/create_permission", handlers.CreatePermission(db))
	r.GET("/api/get_permissions", handlers.GetPermissions(db))
	r.PUT("/api/update_permission/:id", handlers.UpdatePermission(db))
	r.DELETE("/api/delete_permission/:id", handlers.DeletePermission(db))
	r.GET("/api/get_permission/:role_id", handlers.GetRolePermissionByRoleID(db))

	// ==================== 30. ROLE PERMISSION (ALT ROUTES) ====================
	r.POST("/api/create_role_permission", handlers.CreateRolePermission(db))
	r.GET("/api/get_role_permissions", handlers.GetRolePermissions(db))
	r.PUT("/api/update_role_permission/:id", handlers.UpdateRolePermission(db))
	r.DELETE("/api/delete_role_permission/:id", handlers.DeleteRolePermission(db))

	// ==================== 31. PRODUCTS ====================
	r.GET("/api/products/:id", repository.FetchElementTypeWithProducts(db))

	// ==================== 32. SETTINGS (MULTIPLE) ====================
	r.GET("/api/multiple/:user_id", handlers.GetSettingHandler(db))
	r.POST("/api/multiple", handlers.CreateSettingHandler(db))

	// ==================== 33. TEMPLATES & STAGES ====================
	r.POST("/api/create_template", handlers.CreateTemplateHandler(db))
	r.PUT("/api/update_template/:id", handlers.UpdateTemplateHandler(db))
	r.DELETE("/api/delete_template/:id", handlers.DeleteTemplateHandler(db))
	r.GET("/api/get_template/:id", handlers.GetTemplateByIDHandler(db))

	r.GET("/api/get_templatestages/:template_id", handlers.GetTemplateHandler(db))
	r.GET("/api/get_alltemplatestages", handlers.GetAllTemplatesHandler(db))
	r.GET("/api/get_allstages/:project_id", CheckProjectSuspension(db), handlers.GetAllStagesByProjectID(db))
	r.GET("/api/get_stage/:stage_id", handlers.GetStageByID(db))
	r.GET("/api/get_template", handlers.GetAllTemplates(db))
	r.POST("/api/create_projectstage/", handlers.CreateProjectStage(db))
	r.PUT("/api/update_project_stage/:id", handlers.UpdateProjectStage(db))

	// ==================== 34. VIEWS ====================
	r.GET("/api/get_view/:project_id", CheckProjectSuspension(db), handlers.GetAllView(db))
	r.GET("/api/get_allview/:project_id", CheckProjectSuspension(db), handlers.GetAllViews(db))

	r.GET("/api/export_project_views/:project_id", handlers.ExportAllViewsPDF(db))

	// ==================== 35. REPORTS (PRECAST/STAGE) ====================
	r.GET("/api/get_element_report/", handlers.GetElementReportAccordingToStage(db))
	r.GET("/api/get_precast_report/:project_id", CheckProjectSuspension(db), handlers.GetPrecastReport(db))
	r.GET("/api/get_weekly_report/:project_id", CheckProjectSuspension(db), handlers.GetWeeklyStageReport(db))
	r.GET("/api/get_monthly_report/:project_id", CheckProjectSuspension(db), handlers.GetMonthlyStageReport(db))
	r.GET("/api/get_daily_report/:project_id", CheckProjectSuspension(db), handlers.GetDailyStageReport(db))

	// ==================== 36. STOCKYARDS (MASTER) ====================
	r.GET("/api/stockyards", handlers.GetStockyard(db))
	r.POST("/api/stockyards", handlers.CreateStockyard(db))
	r.PUT("/api/stockyards/:id", handlers.UpdateStockyard(db))
	r.DELETE("/api/stockyards/:id", handlers.DeleteStockyard(db))
	r.GET("/api/stockyard/:id", handlers.GetStockyardByID(db))

	// ==================== 37. ERECTION ====================
	r.GET("/api/erection_orders/:project_id", CheckProjectSuspension(db), handlers.GetErectionOrderData(db))
	r.GET("/api/erection_orders/approved/:project_id", CheckProjectSuspension(db), handlers.GetApprovedErectionOrderData(db))
	r.POST("/api/stock_erection", handlers.RageStockRequestByErection(db))
	r.GET("/api/erection_stock/received/:project_id", CheckProjectSuspension(db), handlers.GetReceivedErectedStock(db))
	r.POST("/api/erection_stock/update", handlers.UpdateErectedStatus(db))
	r.PUT("/api/erection_stock/update_when_erected", handlers.UpdateStockErectedWhenErected(db))

	// ==================== 38. VEHICLES ====================
	r.POST("/api/vehicles", handlers.CreateVehicleDetails(db))
	r.GET("/api/vehicles", handlers.GetAllVehicles(db))
	r.GET("/api/vehicles/:id", handlers.GetVehicleByID(db))
	r.PUT("/api/vehicles/:id", handlers.UpdateVehicleDetails(db))
	r.DELETE("/api/vehicles/:id", handlers.DeleteVehicleDetails(db))

	// ==================== 39. DISPATCH ====================
	r.POST("/api/dispatch_order", handlers.CreateAndSaveDispatchOrder(db))
	r.POST("/api/dispatch_order/:order_id/receive", handlers.ReceiveDispatchOrderByErection(db))
	r.GET("/api/dispatch_order/:project_id", CheckProjectSuspension(db), handlers.GetDispatchOrdersByProjectID(db))
	r.GET("/api/dispatch_order/pdf/:order_id", handlers.GenerateDispatchPDF(db))
	r.GET("/api/dispatch_order/logs/:project_id", CheckProjectSuspension(db), handlers.GetDispatchTrackingLogs(db))
	r.POST("/api/dispatch_order/:order_id/in-transit", handlers.UpdateDispatchToInTransit(db))

	// ==================== 40. QR CODE ====================
	r.GET("/api/generate-qr/:id", handlers.GenerateQRCodeJPEG(db))

	// ==================== 41. PRECAST STOCK ====================
	r.GET("/api/stock-summary/approved-erected/:project_id", CheckProjectSuspension(db), handlers.GetApprovedErectedStockSummary(db))
	r.PUT("/api/update_stock", handlers.UpdateStockByPlaning(db))
	r.GET("/api/in_stockyards", handlers.InPrecastStock(db))
	r.GET("/api/:project_id/received_stockyards", handlers.ReceivedPrecastStock(db))
	r.PUT("/api/update_stockyard/recieve_element", handlers.UpdateStockyardReceived(db))
	r.GET("/api/stockyard_item/:project_id", CheckProjectSuspension(db), handlers.GetElementlistFromStockYard(db))
	r.GET("/api/stock-erected/logs/:project_id", CheckProjectSuspension(db), handlers.GetStockErectedLogs(db))
	r.GET("/api/precast_stock/all/:project_id", CheckProjectSuspension(db), handlers.GetAllPrecastStock(db))
	r.GET("/api/stock/approval-logs/:project_id", CheckProjectSuspension(db), handlers.GetPrecastStockApprovalLogs(db))
	r.GET("/api/precast_stock/pending_approvals/:project_id", CheckProjectSuspension(db), handlers.GetPendingApprovalRequests(db))

	// ==================== 42. ELEMENT DETAILS ====================
	r.POST("/api/element_details", handlers.GetElementDetailsByTypeAndLocation(db))

	// ==================== 43. TEST ====================
	r.GET("/test", func(c *gin.Context) {
		start := time.Now() // Start the timer

		// Simulate an error scenario
		status := http.StatusInternalServerError
		c.JSON(status, gin.H{"error": "something went wrong"})

		duration := time.Since(start)

		// Log execution time only if the status is not http.StatusOK
		if status != http.StatusOK {
			log.Printf("API execution time for status %d: %s", status, duration)
		}
	})

	// ==================== 44. QUESTIONS ====================
	questionGroup := r.Group("/api/questions")
	{
		questionGroup.POST("", handlers.CreateQuestions(db))
		questionGroup.GET("/:paper_id", handlers.GetQuestions(db))
		questionGroup.POST("/answers", handlers.SubmitAnswers(db))
		questionGroup.GET("/answers/:stage_id/:task_id/:project_id", CheckProjectSuspension(db), handlers.GetAnswers(db))
		questionGroup.GET("/papers/:project_id", CheckProjectSuspension(db), handlers.GetAllPapers(db))
		questionGroup.PUT("/update_questions/:question_id", handlers.UpdateSingleQuestionHandler(db))
		questionGroup.PUT("/update_paper/:paper_id", handlers.UpdateQuestions(db))
		questionGroup.DELETE("/delete/:question_id", handlers.DeleteSingleQuestionHandler(db))
		questionGroup.DELETE("/paper_dalete/:paper_id", handlers.DeletePaperAndQuestionsHandler(db))
	}

	// ==================== 45. RECTIFICATION ====================
	r.GET("/api/rectification/:project_id", CheckProjectSuspension(db), handlers.GetRectificationHandler(db))
	r.PUT("/api/update_rectification", handlers.UpdateRectificationHandler(db))

	// ==================== 46. NOTIFICATIONS ====================
	r.GET("/api/notifications", handlers.GetMyNotificationsHandler(db))
	r.PUT("/api/notifications/:id/read", handlers.MarkNotificationAsReadHandler(db))
	r.PUT("/api/notifications/read-all", handlers.MarkAllNotificationsAsReadHandler(db))
	r.POST("/api/fcm/register-token", handlers.RegisterFCMTokenHandler(db, fcmService))
	r.DELETE("/api/fcm/remove-token", handlers.RemoveFCMTokenHandler(db, fcmService))

	// ==================== 47. PRODUCTION HISTORY ====================
	r.GET("/api/production_history/:project_id", CheckProjectSuspension(db), handlers.GetAllCompleteProduction(db))

	// ==================== 48. APP API (MOBILE TASKS) ====================
	r.GET("/api/app/tasks/:project_id", appapi.GetTaskListByAssignee(db))
	r.GET("/api/app/tasks", appapi.GetTaskListByAssignee(db))
	r.GET("/api/app/tasks/count/:project_id", appapi.GetTaskCountByStatus(db))
	r.GET("/api/app/tasks/count", appapi.GetTaskCountByStatus(db))
	r.GET("/api/app/complete-production", appapi.GetCompleteProductionList(db))

	// ==================== 49. DASHBOARD ====================
	r.GET("/api/projects/:project_id/production-summary", handlers.GetProductionSummary(db))
	r.GET("/api/qc-history/:project_id", CheckProjectSuspension(db), handlers.GetQCSummary(db))
	r.GET("/api/material_usage_reports_concrete/:project_id", CheckProjectSuspension(db), handlers.GetConcreteUsageReportsRi(db))
	r.GET("/api/material_usage_reports_steel/:project_id", CheckProjectSuspension(db), handlers.GetSteelUsageReportsRi(db))
	r.GET("/api/project_status", handlers.GetProjectStatusCounts(db))
	r.GET("/api/element_status", handlers.GetElementStatusCounts(db))
	r.GET("/api/element_status_project", handlers.GetElementStatusCountsPerProject(db))
	r.GET("/api/element_status_breakdown/:project_id", CheckProjectSuspension(db), handlers.GetElementStatusBreakdown(db))
	//r.GET("/api/element_type_status_breakdown/:project_id/:hierarchy_id", handlers.GetElementTypeStatusBreakdownByProjectAndHierarchy(db))
	r.GET("/api/element_type_status_breakdown_multiple/:project_id", CheckProjectSuspension(db), handlers.GetElementTypeStatusBreakdownByMultipleHierarchies(db))
	r.GET("/api/element_stages_graph", handlers.GetStageWiseStatsHandler(db))
	r.GET("/api/element_graph", handlers.GetElementProductionGraphDayWise(db))
	r.GET("/api/dashboard/towers/:project_id", CheckProjectSuspension(db), handlers.GetTowersList(db))
	r.GET("/api/totalworkers", handlers.GetTotalWorkersHandler(db))
	r.GET("/api/average_casted", handlers.GetAverageDailyCastingHandler(db))
	r.GET("/api/average_erected", handlers.GetAverageDailyErectedHandler(db))
	r.GET("/api/total_rejections", handlers.GetTotalRejectionsHandler(db))
	r.GET("/api/monthly_rejections", handlers.GetMonthlyRejectionsHandler(db))
	r.GET("/api/projects_overview", handlers.GetProjectsOverviewHandler(db))

	// ==================== 50. AUTH – FORGOT/RESET PASSWORD ====================
	r.POST("/api/auth/forgot-password", handlers.ForgetPasswordHandler(db, "https://precastezy.blueinvent.com/reset-password/"))
	r.POST("/api/auth/reset-password/:token", handlers.ResetPasswordHandler(db))

	// ==================== 51. CHANGE PASSWORD & OVERVIEWS ====================
	r.POST("/api/change_password", handlers.ChangePasswordHandler(db))
	r.GET("/api/client_projects/:client_id", handlers.GetUserClientProjectsOverviewHandler(db))
	r.GET("/api/endclient_projects/:client_id", handlers.GetEndClientProjectsOverviewHandler(db))
	r.GET("/api/stockyard_project/:stockyard_id", handlers.GetStockyardProjectsHandler(db))

	// ==================== 52. DASHBOARD REPORTS ====================
	r.GET("/api/production_reports/:project_id", CheckProjectSuspension(db), handlers.GetProductionReports(db))
	r.GET("/api/qc_reports/:project_id", CheckProjectSuspension(db), handlers.GetQCReports(db))
	r.GET("/api/qc_reports_stagewise/:project_id", handlers.GetQCReportsStageWise(db))

	r.GET("/api/dashboard_trends", handlers.GetDashboardTrends(db))

	// ==================== 53. DELETED ELEMENTS & LIFECYCLE ====================
	r.GET("/api/deleted_elements/:project_id", CheckProjectSuspension(db), handlers.GetDeletedElementsWithDrawings(db))

	r.GET("/api/element_lifecycle/:element_id", handlers.GetElementLifecycleHandler(db))

	// ==================== 54. ACTIVITY LOGS ====================
	r.GET("/api/logs", handlers.GetActivityLogsHandler(db))
	r.GET("/api/log/search", handlers.SearchActivityLogsHandler(db))

	// ==================== 55. SCAN ELEMENT ====================
	r.GET("/api/scan_element/:id", handlers.GetElementByID(db))

	// ==================== 56. SUSPEND (USER/CLIENT/PROJECT) ====================
	r.PUT("/api/users/:id/suspend", handlers.SuspendUser(db))
	r.PUT("/api/client/:client_id/suspend", handlers.SuspendClient(db))
	r.PUT("/api/project/:project_id/suspend", handlers.SuspendProjectHandler(db))

	// ==================== 57. SUBSCRIPTION & REDEMPTION ====================
	r.GET("/api/planned_casted", handlers.GetPlannedVsCastedElements(db))
	r.PUT("/api/extend_subscription/:project_id/:days", handlers.ExtendSubscriptionHandler(db))
	r.PUT("/api/set_redeemption/:project_id/:days", handlers.SetRedemptionHandler(db))

	// ==================== 58. ELEMENT TYPE & STOCKYARD REPORTS ====================
	r.GET("/api/element_type_reports/:project_id", handlers.GetElementTypeReports(db))
	r.GET("/api/element_type_reports_assigned/:project_id", handlers.GetElementTypeReportsviaManager(db))
	r.GET("/api/stockyard_reports/:project_id", handlers.GetStockyardReports(db))
	r.GET("/api/stockyard_reports_by_stockyards/:project_id", handlers.GetStockyardReportsByStockyards(db))

	// // Quotation Routes
	// quotationHandler := handlers.NewQuotationHandler(db)
	// r.POST("/api/project/:project_id/quotations/upload", CheckProjectSuspension(db), quotationHandler.UploadQuotation)
	// r.GET("/api/project/:project_id/quotations", CheckProjectSuspension(db), quotationHandler.GetQuotations)
	// r.GET("/api/quotations/:quotation_id", quotationHandler.GetQuotationDetails)

	// ==================== 59. MANPOWER – SKILL TYPES ====================
	r.POST("/api/skill-types", handlers.CreateSkillType(db))
	r.GET("/api/skill-types", handlers.GetSkillTypes(db))
	r.GET("/api/skill-types/:id", handlers.GetSkillType(db))
	r.PUT("/api/skill-types/:id", handlers.UpdateSkillType(db))
	r.DELETE("/api/skill-types/:id", handlers.DeleteSkillType(db))

	// ==================== 60. MANPOWER – SKILLS ====================
	r.POST("/api/skills", handlers.CreateSkill(db))
	r.GET("/api/skills", handlers.GetSkills(db))
	r.GET("/api/skills/:id", handlers.GetSkill(db))
	r.GET("/api/skills/skill-type/:id", handlers.GetSkillBySkillTypeID(db))
	r.PUT("/api/skills/:id", handlers.UpdateSkill(db))
	r.DELETE("/api/skills/:id", handlers.DeleteSkill(db))

	// ==================== 61. MANPOWER – DEPARTMENTS ====================
	r.POST("/api/departments", handlers.CreateDepartment(db))
	r.GET("/api/departments", handlers.GetDepartments(db))
	r.GET("/api/departments/:id", handlers.GetDepartment(db))
	r.PUT("/api/departments/:id", handlers.UpdateDepartment(db))
	r.DELETE("/api/departments/:id", handlers.DeleteDepartment(db))

	// ==================== 62. MANPOWER – CATEGORIES ====================
	r.POST("/api/categories", handlers.CreateCategory(db))
	r.GET("/api/categories", handlers.GetCategories(db))
	r.GET("/api/categories/:id", handlers.GetCategory(db))
	r.PUT("/api/categories/:id", handlers.UpdateCategory(db))
	r.DELETE("/api/categories/:id", handlers.DeleteCategory(db))
	r.GET("/api/projects/:project_id/categories", handlers.GetCategoriesByProject(db))

	// ==================== 63. MANPOWER – PEOPLE ====================
	r.POST("/api/people", handlers.CreatePeople(db))
	r.GET("/api/people", handlers.GetPeople(db))
	r.GET("/api/people/:id", handlers.GetPerson(db))
	r.PUT("/api/people/:id", handlers.UpdatePeople(db))
	r.DELETE("/api/people/:id", handlers.DeletePeople(db))
	r.GET("/api/projects/:project_id/people", handlers.GetPeopleByProject(db))
	r.GET("/api/departments/:id/people", handlers.GetPeopleByDepartment(db))
	r.GET("/api/categories/:id/people", handlers.GetPeopleByCategory(db))

	// ==================== 64. MANPOWER – COUNT ====================
	r.POST("/api/manpower-count", handlers.CreateManpowerCount(db))
	r.POST("/api/manpower-count/bulk", handlers.CreateManpowerCountBulk(db))
	r.GET("/api/manpower-count", handlers.GetManpowerCounts(db))
	r.GET("/api/manpower-count/:id", handlers.GetManpowerCount(db))
	r.PUT("/api/manpower-count/:id", handlers.UpdateManpowerCount(db))
	r.DELETE("/api/manpower-count/:id", handlers.DeleteManpowerCount(db))
	r.GET("/api/projects/:project_id/manpower-count", handlers.GetManpowerCountsByProject(db))
	r.GET("/api/manpower-count/date/:date", handlers.GetManpowerCountsByDate(db))
	r.GET("/api/manpower-count/dashboard", handlers.GetManpowerCountDashboard(db))

	// ==================== 65. MANPOWER – DASHBOARDS ====================
	r.GET("/api/manpower/dashboard", handlers.GetManpowerDashboard(db))
	r.GET("/api/manpower/skills/dashboard", handlers.GetTotalSkillTypeCount(db))
	r.GET("/api/manpower/skill_type/dashboard", handlers.GetTotalSkillTypeTypeCount(db))
	r.GET("/api/manpower/vendor/dashboard", handlers.GetVendorCount(db))
	r.GET("/api/manpower/share/dashboard", handlers.GetVendorShare(db))

	// ==================== 66. PROJECTS (BASIC) ====================
	r.GET("/api/projects/basic", handlers.FetchAllProjectsBasic(db))

	// ==================== 67. EMAIL TEMPLATES ====================
	r.POST("/api/email-templates", handlers.CreateEmailTemplate(db))
	r.GET("/api/email-templates", handlers.GetEmailTemplates(db))
	r.GET("/api/email-templates/:id", handlers.GetEmailTemplateByID(db))
	r.PUT("/api/email-templates/:id", handlers.UpdateEmailTemplate(db))
	r.DELETE("/api/email-templates/:id", handlers.DeleteEmailTemplate(db))
	r.GET("/api/email-templates/type/:type", handlers.GetTemplatesByType(db))

	// ==================== 68. MANPOWER – PROJECT SUMMARY ====================
	r.GET("/api/manpower_project/summary", handlers.GetProjectManpowerHandler(db))
	r.GET("/api/manpower_project/summary/h1", handlers.GetManpowerBreakdown(db))
	r.GET("/api/manpower_project/summary/h2", handlers.GetCategoryDepartmentBreakdown(db))
	r.GET("/api/manpower_project/summary/h3", handlers.GetDepartmentPeopleBreakdown(db))
	r.GET("/api/manpower_project/summary/h4", handlers.GetSkillTypeSummaryHandler(db))
	r.GET("/api/manpower_project/summary/h5", handlers.GetSkillSummaryHandler(db))

	r.GET("/api/project_manpower/dashboard", handlers.GetProjectManHandler(db))

	// ==================== 69. END CLIENTS ====================
	r.POST("/api/end_clients", handlers.CreateEndClient(db))
	r.GET("/api/end_clients", handlers.GetEndClients(db))
	r.GET("/api/end_clients/:id", handlers.GetEndClient(db))
	r.PUT("/api/end_clients/:id", handlers.UpdateEndClient(db))
	r.DELETE("/api/end_clients/:id", handlers.DeleteEndClient(db))
	r.GET("/api/end_client/:client_id", handlers.GetEndClientsByClient(db))

	// ==================== 70. WORK ORDERS ====================
	r.POST("/api/workorders", handlers.CreateWorkOrder(db))
	r.GET("/api/workorders", handlers.GetAllWorkOrders(db))
	r.GET("/api/workorders/:id", handlers.GetWorkOrder(db))
	r.PUT("/api/workorders/:id", handlers.UpdateWorkOrder(db))
	r.GET("/api/wo_revisions/:id", handlers.GetWorkOrderRevisions(db))
	r.DELETE("/api/workorders/:id", handlers.DeleteWorkOrder(db))
	r.POST("/api/workorders_amendment", handlers.CreateWorkOrderAmendment(db))
	r.GET("/api/work-orders/search", handlers.SearchWorkOrders(db))

	// ==================== 71. PHONE CODES ====================
	r.POST("/api/phonecodes", handlers.CreatePhoneCode(db))
	r.GET("/api/phonecodes", handlers.GetAllPhoneCodes(db))
	r.GET("/api/phonecodes/:id", handlers.GetPhoneCode(db))
	r.PUT("/api/phonecodes/:id", handlers.UpdatePhoneCode(db))
	r.DELETE("/api/phonecodes/:id", handlers.DeletePhoneCode(db))

	// ==================== 72. UNITS ====================
	r.POST("/api/units", handlers.CreateUnit(db))
	r.GET("/api/units", handlers.GetUnits(db))
	r.GET("/api/units/:id", handlers.GetUnitByID(db))
	r.PUT("/api/units/:id", handlers.UpdateUnit(db))
	r.DELETE("/api/units/:id", handlers.DeleteUnit(db))

	// ==================== 73. CURRENCY ====================
	r.POST("/api/currency", handlers.CreateCurrency(db))
	r.GET("/api/currency", handlers.GetCurrencies(db))
	r.GET("/api/currency/:id", handlers.GetCurrencyByID(db))
	r.PUT("/api/currency/:id", handlers.UpdateCurrency(db))
	r.DELETE("/api/currency/:id", handlers.DeleteCurrency(db))

	// ==================== 74. PDF SUMMARY ====================
	r.GET("/api/elementtype_pdf_summary/:project_id", handlers.GenerateElementTypesPDFSummary(db))
	r.GET("/api/element_details_pdf", handlers.GenerateElementDetailsPDF(db))
	r.GET("/api/elements_with_drawings_pdf/:project_id", handlers.GenerateElementsWithDrawingsPDF(db))
	r.GET("/api/element_by_id_pdf/:id", handlers.GenerateElementByIDPDF(db))

	// ==================== 75. INVOICES ====================
	r.POST("/api/invoices", handlers.CreateInvoice(db))
	r.GET("/api/allinvoices/:id", handlers.GetAllInvoicesByWorkOrderId(db))
	r.GET("/api/invoice/:id", handlers.GetInvoice(db))
	r.PUT("/api/invoices/:id", handlers.UpdateInvoice(db))
	r.GET("/api/invoices", handlers.GetAllInvoices(db))
	r.PUT("/api/invoice/:id/submit", handlers.SubmitInvoice(db))
	r.GET("/api/invoices/search", handlers.SearchInvoices(db))

	r.PUT("/api/update_invoice_payment/:id", handlers.UpdateInvoicePayment(db))
	r.GET("/api/get_invoice_payment/:id", handlers.GetInvoicePayments(db))
	r.GET("/api/pending-invoices", handlers.GetPendingInvoices(db))
	r.GET("/api/search-pending-invoice", handlers.SearchPendingInvoices(db))

	r.GET("/api/invoice_pdf/:id", handlers.GenerateInvoicePDF(db))

	// ==================== 76. DASHBOARD PDF ====================
	r.GET("/api/dashboard_pdf", handlers.ExportDashboardPDF(db))

	// ==================== 77. TRANSPORTERS ====================
	r.POST("/api/transporters", handlers.CreateTransporter(db))
	r.GET("/api/transporters", handlers.GetAllTransporters(db))
	r.GET("/api/transporters/:id", handlers.GetTransporterByID(db))
	r.PUT("/api/transporters/:id", handlers.UpdateTransporter(db))
	r.DELETE("/api/transporters/:id", handlers.DeleteTransporter(db))

	// ==================== 78. PROJECT STOCKYARDS ====================
	r.PUT("/api/project-stockyards/:id/manager", handlers.AssignStockyardManager(db))
	r.GET("/api/projects/:project_id/stockyards", handlers.GetProjectStockyards(db))
	r.GET("/api/projects/:project_id/stockyards/:stockyard_id", handlers.GetProjectStockyard(db))
	r.GET("/api/projects/:project_id/my-stockyards", handlers.GetMyProjectStockyards(db))

	r.PUT("/api/projects/:project_id/assign-stockyard/:element_id", handlers.AssignElementToStockyard(db))

	// ==================== 79. SWAGGER ====================
	r.GET("/swagger/*any", func(c *gin.Context) {
		// Handle specific routes first to avoid conflicts
		if c.Param("any") == "/custom.css" {
			c.Header("Content-Type", "text/css")
			c.String(http.StatusOK, `
/* Beautified Swagger Header Styles */
.swagger-ui .topbar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  padding: 20px 0;
  box-shadow: 0 2px 10px rgba(0,0,0,0.1);
}

.swagger-ui .info {
  margin: 40px 0;
  padding: 30px;
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 4px 20px rgba(0,0,0,0.08);
}

.swagger-ui .info .title {
  font-size: 36px !important;
  font-weight: 700 !important;
  color: #2c3e50 !important;
  margin-bottom: 10px !important;
  display: block !important;
  visibility: visible !important;
  opacity: 1 !important;
}

.swagger-ui .info h2 {
  font-size: 36px !important;
  font-weight: 700 !important;
  color: #2c3e50 !important;
  margin-bottom: 10px !important;
  display: block !important;
  visibility: visible !important;
  opacity: 1 !important;
}

.swagger-ui .info .version {
  display: inline-block;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: #fff;
  padding: 6px 16px;
  border-radius: 20px;
  font-size: 14px;
  font-weight: 600;
  margin-left: 15px;
  margin-bottom: 15px;
}

.swagger-ui .info .base-url {
  background: #f8f9fa;
  padding: 15px 20px;
  border-radius: 8px;
  margin: 20px 0;
  border-left: 4px solid #667eea;
  font-family: 'Monaco', 'Menlo', monospace;
  font-size: 14px;
  color: #495057;
}

.swagger-ui .info .base-url code {
  background: transparent;
  color: #667eea;
  font-weight: 600;
  padding: 0;
}

.swagger-ui .info .description {
  font-size: 16px;
  line-height: 1.8;
  color: #555;
  margin: 20px 0;
}

.swagger-ui .info .description p {
  margin: 10px 0;
}

.swagger-ui .info .main {
  margin-top: 30px;
}

.swagger-ui .info .main a {
  color: #667eea;
  text-decoration: none;
  font-weight: 600;
  transition: color 0.3s ease;
  margin-right: 20px;
}

.swagger-ui .info .main a:hover {
  color: #764ba2;
  text-decoration: underline;
}

.swagger-ui .scheme-container {
  background: #f8f9fa;
  padding: 20px;
  border-radius: 8px;
  margin: 20px 0;
}

@media (max-width: 768px) {
  .swagger-ui .info .title {
    font-size: 28px;
  }
  
  .swagger-ui .info {
    padding: 20px;
  }
}
`)
			return
		}
		
		if c.Param("any") == "/doc.json" {
			// Always try to read the processed swagger.json file first
			// Use absolute path to ensure we get the correct file
			swaggerPath := "/home/ubuntu/precast-backend/docs/swagger.json"
			swaggerJSON, err := os.ReadFile(swaggerPath)
			if err != nil {
				// Try relative path as fallback
				swaggerJSON, err = os.ReadFile("docs/swagger.json")
			}
			
			if err == nil && len(swaggerJSON) > 100 {
				// Successfully read from file (check length to ensure it's valid)
				c.Header("Content-Type", "application/json")
				c.String(http.StatusOK, string(swaggerJSON))
				return
			}
			
			// Fallback to ReadDoc if file not found or invalid
			doc, err := swag.ReadDoc("swagger")
			if err != nil {
				c.String(http.StatusInternalServerError, `{"error":"swagger doc not found"}`)
				return
			}
			c.Header("Content-Type", "application/json")
			c.String(http.StatusOK, doc)
			return
		}
		
		// Intercept Swagger UI HTML to inject custom CSS
		// Check if this is an HTML page (index.html or root)
		if c.Param("any") == "/index.html" || c.Param("any") == "/" {
			// Create a custom response writer to capture HTML
			originalWriter := c.Writer
			captureWriter := &cssInjectorWriter{
				ResponseWriter: originalWriter,
				body:          &strings.Builder{},
			}
			c.Writer = captureWriter
			
			// Call the original Swagger handler
			ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json"))(c)
			
			// Restore original writer
			c.Writer = originalWriter
			
			// Get captured HTML
			html := captureWriter.body.String()
			
			// Inject CSS link before </head> tag
			if strings.Contains(html, "</head>") {
				cssLink := `    <link rel="stylesheet" type="text/css" href="/swagger/custom.css">
`
				html = strings.Replace(html, "</head>", cssLink+"</head>", 1)
				// Copy status code and headers
				for k, v := range captureWriter.Header() {
					if k != "Content-Length" {
						c.Header(k, strings.Join(v, ", "))
					}
				}
				c.Header("Content-Type", "text/html; charset=utf-8")
				c.Header("Content-Length", strconv.Itoa(len(html)))
				c.String(http.StatusOK, html)
				return
			}
			
			// If no head tag, serve original response
			c.String(http.StatusOK, html)
			return
		}
		
		// Serve Swagger UI for all other routes (static files, etc.)
		ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json"))(c)
	})

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	// Validate port is numeric
	portInt, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Invalid PORT environment variable: %s. Must be a number.", port)
	}
	if portInt < 0 || portInt > 65535 {
		log.Fatalf("Invalid PORT: %d. Must be between 0 and 65535.", portInt)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown job manager first
	if err := jobManager.GracefulShutdown(20 * time.Second); err != nil {
		log.Printf("Warning: Job manager shutdown error: %v", err)
	}

	// Shutdown HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exiting")
}

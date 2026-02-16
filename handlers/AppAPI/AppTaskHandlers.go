// Package appapi provides HTTP handlers for the mobile application API endpoints.
// This package handles task-related operations including:
//   - Retrieving task lists by assignee
//   - Counting tasks by status
//   - Fetching complete production records
//
// All handlers follow Clean Code principles with:
//   - Single Responsibility Principle
//   - DRY (Don't Repeat Yourself)
//   - Small, focused functions
//   - Comprehensive error handling
//   - Proper documentation
package appapi

import (
	"backend/handlers"
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Task status constants
const (
	StatusPending       = "pending"
	StatusCompleted     = "completed"
	StatusInProgress    = "inprogress"
	StatusInProgressAlt = "in progress"
	StatusRejected      = "rejected"
)

// Stage name constants
const (
	StageMeshMould     = "Mesh & Mould"
	StageReinforcement = "Reinforcement"
)

// buildUserTaskFilterCondition builds the WHERE clause condition for filtering tasks by user.
// It handles multiple scenarios:
//   - Tasks assigned directly to the user
//   - QC tasks assigned to the user (with special handling for Mesh & Mould and Reinforcement stages)
//   - Tasks in Mesh & Mould or Reinforcement stages where user is assigned to Reinforcement stage
//
// Parameters:
//   - userIDPlaceholder: The SQL parameter placeholder index (e.g., $1, $2) for the user ID
//
// Returns:
//   - A SQL WHERE condition string that can be used in queries
func buildUserTaskFilterCondition(userIDPlaceholder int) string {
	return fmt.Sprintf(`(
		a.assigned_to = $%d

		OR (
			a.qc_id = $%d
			AND s.name NOT IN ('%s', '%s')
			AND a.status = 'completed'
			AND a.paper_id IS NOT NULL
		)

		OR (
			a.qc_id = $%d
			AND s.name IN ('%s')
			AND (a.mesh_mold_status = 'completed' )
			AND a.paper_id IS NOT NULL
		)

		OR (
			a.qc_id = $%d
			AND s.name IN ('%s')
			AND a.reinforcement_status = 'completed'
			AND a.paper_id IS NOT NULL
		)

		OR (
			s.name IN ('%s', '%s')
			AND EXISTS (
				SELECT 1 
				FROM project_stages ps
				WHERE ps.name = '%s' AND ps.assigned_to = $%d
			)
		)
	)`,
		userIDPlaceholder,
		userIDPlaceholder, StageMeshMould, StageReinforcement,
		userIDPlaceholder, StageMeshMould,
		userIDPlaceholder, StageReinforcement,
		StageMeshMould, StageReinforcement, StageReinforcement, userIDPlaceholder,
	)
}

// buildTaskCountQuery builds the SQL query for counting tasks grouped by status.
// Pending tasks are counted from activity table (same logic as GetTaskListByAssignee).
// Completed tasks are counted from complete_production table (same logic as GetCompleteProductionList).
// InProgress and Rejected tasks are counted from activity table based on status field.
//
// Parameters:
//   - hasProjectFilter: If true, includes project_id filter in WHERE clause
//
// Returns:
//   - A complete SQL SELECT query string with aggregated counts
func buildTaskCountQuery(hasProjectFilter bool) string {
	// Count pending tasks from activity table (same as GetTaskListByAssignee)
	// All tasks with completed = false that match user filter conditions
	pendingQuery := `
		SELECT COUNT(*) 
		FROM activity a
		LEFT JOIN task t ON a.task_id = t.task_id
		LEFT JOIN project_stages s ON a.stage_id = s.id
		LEFT JOIN element e ON a.element_id = e.id
		LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
		WHERE 
			%s
			AND a.completed = false
			AND %s`

	// Count completed tasks from complete_production table (same as GetCompleteProductionList)
	completedQuery := `
		SELECT COUNT(*) 
		FROM complete_production cp
		WHERE cp.user_id = $%d %s AND cp.updated_at IS NOT NULL`

	// Count inprogress and rejected from activity table based on status
	statusQuery := `
		SELECT 
			COALESCE(SUM(CASE WHEN LOWER(a.status) = '%s' OR LOWER(a.status) = '%s' THEN 1 ELSE 0 END), 0) AS inprogress,
			COALESCE(SUM(CASE WHEN LOWER(a.status) = '%s' THEN 1 ELSE 0 END), 0) AS rejected
		FROM activity a
		LEFT JOIN task t ON a.task_id = t.task_id
		LEFT JOIN project_stages s ON a.stage_id = s.id
		LEFT JOIN element e ON a.element_id = e.id
		LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
		WHERE 
			%s
			AND a.completed = false
			AND %s`

	// Build the final query using subqueries
	// Parameter order: [projectID, userID] if hasProjectFilter, else [userID]
	var pendingWhereConditions string
	var completedProjectFilter string
	var statusWhereConditions string
	var pendingUserIDPlaceholder int
	var completedUserIDPlaceholder int
	var statusUserIDPlaceholder int

	if hasProjectFilter {
		// With project filter: $1 = projectID, $2 = userID
		pendingWhereConditions = "a.project_id = $1"
		pendingUserIDPlaceholder = 2
		completedProjectFilter = "AND cp.project_id = $1"
		completedUserIDPlaceholder = 2 // userID is $2, projectID is $1
		statusWhereConditions = "a.project_id = $1"
		statusUserIDPlaceholder = 2
	} else {
		// Without project filter: $1 = userID
		pendingWhereConditions = "a.completed = false"
		pendingUserIDPlaceholder = 1
		completedProjectFilter = ""
		completedUserIDPlaceholder = 1
		statusWhereConditions = "a.completed = false"
		statusUserIDPlaceholder = 1
	}

	userFilterPending := buildUserTaskFilterCondition(pendingUserIDPlaceholder)
	userFilterStatus := buildUserTaskFilterCondition(statusUserIDPlaceholder)

	// Build pending subquery
	pendingSubquery := fmt.Sprintf(pendingQuery, pendingWhereConditions, userFilterPending)

	// Build completed subquery
	completedSubquery := fmt.Sprintf(completedQuery, completedUserIDPlaceholder, completedProjectFilter)

	// Build status subquery
	statusSubquery := fmt.Sprintf(statusQuery,
		StatusInProgress, StatusInProgressAlt, StatusRejected,
		statusWhereConditions, userFilterStatus)

	// Final query combining all counts using subqueries
	return fmt.Sprintf(`
		SELECT 
			(%s) AS pending,
			(%s) AS completed,
			(SELECT inprogress FROM (%s) AS status_counts) AS inprogress,
			(SELECT rejected FROM (%s) AS status_counts) AS rejected,
			(%s) + (%s) + (SELECT inprogress FROM (%s) AS status_counts) + (SELECT rejected FROM (%s) AS status_counts) AS total`,
		pendingSubquery,
		completedSubquery,
		statusSubquery,
		statusSubquery,
		pendingSubquery,
		completedSubquery,
		statusSubquery,
		statusSubquery,
	)
}

// validateAndGetUserSession validates the session from the Authorization header and retrieves user information.
// It performs the following validations:
//  1. Checks if Authorization header exists
//  2. Validates session using handlers.GetSessionDetails
//  3. Retrieves user details using storage.GetUserBySessionID
//
// Parameters:
//   - c: Gin context containing the HTTP request
//   - db: Database connection
//
// Returns:
//   - session: Validated session object
//   - userName: Username associated with the session
//   - userID: User ID from the validated user
//   - err: Error if validation fails at any step
func validateAndGetUserSession(c *gin.Context, db *sql.DB) (session models.Session, userName string, userID int, err error) {
	sessionID := c.GetHeader("Authorization")
	if sessionID == "" {
		return session, "", 0, fmt.Errorf("session_id header is missing")
	}

	session, userName, err = handlers.GetSessionDetails(db, sessionID)
	if err != nil {
		return session, "", 0, fmt.Errorf("invalid session: %w", err)
	}

	user, err := storage.GetUserBySessionID(db, sessionID)
	if err != nil || user == nil {
		return session, "", 0, fmt.Errorf("invalid session: %w", err)
	}

	return session, userName, user.ID, nil
}

// parseProjectID extracts and validates project_id from request parameters.
// It checks both path parameters and query parameters for the project_id.
//
// Parameters:
//   - c: Gin context containing the HTTP request
//
// Returns:
//   - projectID: Parsed project ID (0 if not provided)
//   - hasFilter: Boolean indicating if project_id was provided
//   - err: Error if project_id is provided but invalid
func parseProjectID(c *gin.Context) (projectID int, hasFilter bool, err error) {
	projectIDParam := c.Param("project_id")
	if projectIDParam == "" {
		projectIDParam = strings.TrimSpace(c.Query("project_id"))
	}

	if projectIDParam == "" {
		return 0, false, nil
	}

	pid, err := strconv.Atoi(projectIDParam)
	if err != nil {
		return 0, false, fmt.Errorf("invalid project ID: %w", err)
	}

	return pid, true, nil
}

// getTowerAndFloor fetches tower and floor names from the precast hierarchy table.
// The function handles two scenarios:
//  1. If the target_location has a parent_id, it's a floor and the parent is the tower
//  2. If the target_location has no parent_id, it's a tower itself and floor defaults to "common"
//
// Parameters:
//   - db: Database connection
//   - targetLocationID: The ID of the target location in the precast table
//
// Returns:
//   - tower: Name of the tower (empty string on error)
//   - floor: Name of the floor (defaults to "common" if not found)
//   - err: Error if database query fails
func getTowerAndFloor(db *sql.DB, targetLocationID int64) (tower string, floor string, err error) {
	var name sql.NullString
	var parentID sql.NullInt64

	// Fetch hierarchy details (could be a tower or floor)
	err = db.QueryRow(`SELECT name, parent_id FROM precast WHERE id = $1`, targetLocationID).Scan(&name, &parentID)
	if err != nil {
		return "", "", err
	}

	// If parent_id exists, this hierarchy is a floor, parent is the tower
	if parentID.Valid && parentID.Int64 != 0 {
		// This is a floor - parent is the tower
		floor = name.String
		if floor == "" {
			floor = "common"
		}

		// Fetch tower name from parent
		err = db.QueryRow(`SELECT name FROM precast WHERE id = $1`, parentID.Int64).Scan(&name)
		if err != nil {
			return "", "", err
		}
		tower = name.String
	} else {
		// No parent - this hierarchy is a tower itself
		tower = name.String
		floor = "common" // Default floor name when no floor exists
	}

	return tower, floor, nil
}

// buildTaskListQuery builds the SQL query for retrieving tasks assigned to a user.
// The query joins activity, task, project_stages, element, and element_type tables
// to retrieve comprehensive task information.
//
// Parameters:
//   - hasProjectFilter: If true, includes project_id filter in WHERE clause
//
// Returns:
//   - A complete SQL SELECT query string
func buildTaskListQuery(hasProjectFilter bool) string {
	baseQuery := `
				SELECT 
					a.id, 
					a.task_id, 
					a.project_id, 
					a.name, 
					a.priority, 
					a.stage_id, 
					s.name AS stage_name, 
					a.assigned_to, 
					a.start_date, 
					a.end_date, 
					a.status, 
					a.element_id, 
					a.qc_id, 
					a.paper_id, 
					a.qc_status, 
					a.mesh_mold_status, 
					a.reinforcement_status,
					t.element_type_id, 
					COALESCE(e.element_id, '') AS element_name,
					COALESCE(e.target_location, 0) AS target_location,
					COALESCE(et.element_type, '') AS element_type
				FROM activity a
				LEFT JOIN task t ON a.task_id = t.task_id
				LEFT JOIN project_stages s ON a.stage_id = s.id
				LEFT JOIN element e ON a.element_id = e.id
				LEFT JOIN element_type et ON t.element_type_id = et.element_type_id
				WHERE 
					%s
					AND %s
				ORDER BY a.start_date DESC`

	userIDPlaceholder := 1
	var whereConditions string
	if hasProjectFilter {
		whereConditions = "a.project_id = $1\n\t\t\t\tAND a.completed = false"
		userIDPlaceholder = 2
	} else {
		whereConditions = "a.completed = false"
		userIDPlaceholder = 1
	}

	userFilter := buildUserTaskFilterCondition(userIDPlaceholder)
	return fmt.Sprintf(baseQuery, whereConditions, userFilter)
}

// GetTaskListByAssignee retrieves tasks assigned to the authenticated user.
// It uses the same filtering logic as GetActivityHandlerByAssignee to determine
// which tasks belong to the user, including:
//   - Tasks directly assigned to the user
//   - QC tasks assigned to the user
//   - Tasks in special stages (Mesh & Mould, Reinforcement) where user has permissions
//
// The endpoint supports optional project_id filtering via path or query parameter.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// GetTaskListByAssignee godoc
// @Summary      Get task list by assignee (app API)
// @Tags         app-tasks
// @Param        project_id  path     int  false  "Project ID (optional)"
// @Success      200         {object}  object
// @Failure      400         {object}  object
// @Failure      401         {object}  object
// @Router       /api/app/tasks [get]
// @Router       /api/app/tasks/{project_id} [get]
func GetTaskListByAssignee(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, userName, userID, err := validateAndGetUserSession(c, db)
		if err != nil {
			handleAuthError(c, err)
			return
		}

		projectID, hasProjectFilter, err := parseProjectID(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := buildTaskListQuery(hasProjectFilter)
		var args []interface{}
		if hasProjectFilter {
			args = []interface{}{projectID, userID}
		} else {
			args = []interface{}{userID}
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activities", "details": err.Error()})
			return
		}
		defer rows.Close()

		processActivities(rows, db, projectID, c, session, userName)
	}
}

// activityWithElement represents an activity with its associated element information
type activityWithElement struct {
	Activity       models.Activity
	ElementName    string
	ElementType    string
	TargetLocation sql.NullInt64
}

// elementTypeCacheEntry represents cached element type information
type elementTypeCacheEntry struct {
	ElementTypeID   int
	ElementTypeName string
	Drawings        []models.AppDrawing
}

// scanActivityRow scans a single row from the database result set into an activityWithElement struct.
//
// Parameters:
//   - rows: Database result set
//
// Returns:
//   - activityWithElem: Scanned activity with element information
//   - err: Error if scanning fails
func scanActivityRow(rows *sql.Rows) (activityWithElement, error) {
	var activity models.Activity
	var paperID, qcID sql.NullInt64
	var elemTypeID int
	var elementName string
	var qcStatus sql.NullString
	var startDate, endDate sql.NullTime
	var stageName sql.NullString
	var targetLocation sql.NullInt64
	var elementType sql.NullString

	err := rows.Scan(
		&activity.ID, &activity.TaskID, &activity.ProjectID, &activity.Name, &activity.Priority,
		&activity.StageID, &stageName, &activity.AssignedTo, &startDate, &endDate, &activity.Status,
		&activity.ElementID, &qcID, &paperID, &qcStatus, &activity.MeshMoldStatus,
		&activity.ReinforcementStatus, &elemTypeID, &elementName, &targetLocation, &elementType,
	)
	if err != nil {
		return activityWithElement{}, fmt.Errorf("error scanning activity row: %w", err)
	}

	// Assign nullable values
	activity.PaperID = int(paperID.Int64)
	activity.QCID = int(qcID.Int64)
	activity.QCStatus = qcStatus.String
	activity.StageName = stageName.String

	elementTypeValue := ""
	if elementType.Valid {
		elementTypeValue = elementType.String
	}

	return activityWithElement{
		Activity:       activity,
		ElementName:    elementName,
		ElementType:    elementTypeValue,
		TargetLocation: targetLocation,
	}, nil
}

// getOrFetchElementTypeInfo retrieves element type information from cache or database.
// This function implements caching to avoid duplicate database queries for the same task.
//
// Parameters:
//   - db: Database connection
//   - taskID: Task ID to fetch element type for
//   - cache: Map of cached element type information
//
// Returns:
//   - elementTypeID: ID of the element type
//   - elementTypeName: Name of the element type
//   - drawings: List of drawings associated with the element type
//   - err: Error if database query fails
func getOrFetchElementTypeInfo(db *sql.DB, taskID int, cache map[int]elementTypeCacheEntry) (int, string, []models.AppDrawing, error) {
	if cached, exists := cache[taskID]; exists {
		return cached.ElementTypeID, cached.ElementTypeName, cached.Drawings, nil
	}

	var elementTypeID int
	var elementTypeName string
	err := db.QueryRow(`
		SELECT t.element_type_id, COALESCE(et.element_type_name, '') 
				FROM task t 
				LEFT JOIN element_type et ON t.element_type_id = et.element_type_id 
				WHERE t.task_id = $1`, taskID).Scan(&elementTypeID, &elementTypeName)
	if err != nil {
		return 0, "", nil, fmt.Errorf("error fetching element type for task %d: %w", taskID, err)
	}

	drawings := fetchDrawingsForElementType(db, elementTypeID)
	cache[taskID] = elementTypeCacheEntry{
		ElementTypeID:   elementTypeID,
		ElementTypeName: elementTypeName,
		Drawings:        drawings,
	}

	return elementTypeID, elementTypeName, drawings, nil
}

// buildActivityResponse creates an AppTaskResponse from activity and element information.
//
// Parameters:
//   - db: Database connection
//   - activityWithElem: Activity with element information
//   - elementTypeName: Name of the element type
//   - drawings: List of drawings for the element type
//
// Returns:
//   - response: Formatted AppTaskResponse
//   - err: Error if tower/floor lookup fails
func buildActivityResponse(db *sql.DB, activityWithElem activityWithElement, elementTypeName string, drawings []models.AppDrawing) (models.AppTaskResponse, error) {
	elementName := activityWithElem.ElementName
	if elementName == "" {
		elementName = strconv.Itoa(activityWithElem.Activity.ElementID)
	}

	tower := ""
	floor := ""
	if activityWithElem.TargetLocation.Valid && activityWithElem.TargetLocation.Int64 != 0 {
		t, f, err := getTowerAndFloor(db, activityWithElem.TargetLocation.Int64)
		if err != nil {
			log.Printf("Error fetching tower/floor: %v", err)
		} else {
			tower = t
			floor = f
		}
	}

	return models.AppTaskResponse{
		ID:              activityWithElem.Activity.ID,
		TaskID:          activityWithElem.Activity.TaskID,
		ProjectID:       activityWithElem.Activity.ProjectID,
		StageID:         activityWithElem.Activity.StageID,
		StageName:       activityWithElem.Activity.StageName,
		QCStatus:        activityWithElem.Activity.QCStatus,
		ElementID:       activityWithElem.Activity.ElementID,
		ElementName:     elementName,
		ElementType:     activityWithElem.ElementType,
		ElementTypeName: elementTypeName,
		Tower:           tower,
		Floor:           floor,
		PaperID:         activityWithElem.Activity.PaperID,
		FilterType:      "all",
		Drawings:        drawings,
	}, nil
}

// processActivities processes database rows and converts them into AppTaskResponse objects.
// It groups activities by task ID, fetches element type information with caching,
// and builds the final response structure.
//
// Parameters:
//   - rows: Database result set containing activity rows
//   - db: Database connection
//   - projectID: Project ID for logging purposes
//   - c: Gin context for HTTP response
//   - session: User session information
//   - userName: Username for activity logging
func processActivities(rows *sql.Rows, db *sql.DB, projectID int, c *gin.Context, session models.Session, userName string) {
	taskMap := make(map[int][]activityWithElement)
	taskIDs := make(map[int]struct{})

	// Scan all rows and group by task ID
	for rows.Next() {
		activityWithElem, err := scanActivityRow(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning activities", "details": err.Error()})
			return
		}

		taskMap[activityWithElem.Activity.TaskID] = append(taskMap[activityWithElem.Activity.TaskID], activityWithElem)
		taskIDs[activityWithElem.Activity.TaskID] = struct{}{}
	}

	if len(taskMap) == 0 {
		c.JSON(http.StatusOK, models.AppTaskListResponse{
			Tasks:      []models.AppTaskResponse{},
			TotalCount: 0,
			Filter:     "all",
		})
		return
	}

	// Build responses with element type caching
	elementTypeCache := make(map[int]elementTypeCacheEntry)
	var results []models.AppTaskResponse

	for taskID := range taskIDs {
		taskActivities := taskMap[taskID]
		if len(taskActivities) == 0 {
			continue
		}

		// Get element type info (cached)
		_, elementTypeName, drawings, err := getOrFetchElementTypeInfo(db, taskID, elementTypeCache)
		if err != nil {
			log.Printf("Error fetching element type for task %d: %v", taskID, err)
			continue
		}

		// Create one response for each activity
		for _, activityWithElem := range taskActivities {
			response, err := buildActivityResponse(db, activityWithElem, elementTypeName, drawings)
			if err != nil {
				log.Printf("Error building activity response: %v", err)
				continue
			}
			results = append(results, response)
		}
	}

	response := models.AppTaskListResponse{
		Tasks:      results,
		TotalCount: len(results),
		Filter:     "all",
	}

	c.JSON(http.StatusOK, response)
	logActivity(db, session, userName, projectID, "Get Task List By Assignee")
}

// fetchDrawingsForElementType retrieves all drawings associated with an element type.
// Drawings are ordered by update timestamp in descending order (most recent first).
//
// Parameters:
//   - db: Database connection
//   - elementTypeID: ID of the element type to fetch drawings for
//
// Returns:
//   - List of AppDrawing objects containing drawing type name, file path, and version
func fetchDrawingsForElementType(db *sql.DB, elementTypeID int) []models.AppDrawing {
	drawings := []models.AppDrawing{}
	query := `
		SELECT 
			COALESCE(dt.drawing_type_name, '') AS drawing_type_name, 
			COALESCE(d.file, '') AS file, 
			COALESCE(d.current_version, '') AS version
        FROM drawings d
        LEFT JOIN drawing_type dt ON d.drawing_type_id = dt.drawing_type_id
        WHERE d.element_type_id = $1
        ORDER BY d.update_at DESC`

	rows, err := db.Query(query, elementTypeID)
	if err != nil {
		log.Printf("Error fetching drawings for element type %d: %v", elementTypeID, err)
		return drawings
	}
	defer rows.Close()

	for rows.Next() {
		var d models.AppDrawing
		if err := rows.Scan(&d.DrawingTypeName, &d.DrawingFile, &d.DrawingVersion); err != nil {
			log.Printf("Error scanning drawing row: %v", err)
			continue
		}
		drawings = append(drawings, d)
	}
	return drawings
}

// buildCompleteProductionQuery builds the SQL query for retrieving complete production records.
//
// Parameters:
//   - hasProjectFilter: If true, includes project_id filter in WHERE clause
//
// Returns:
//   - A complete SQL SELECT query string
func buildCompleteProductionQuery(hasProjectFilter bool) string {
	baseQuery := `
		SELECT 
			cp.id, cp.task_id, cp.activity_id, cp.project_id, cp.element_id, cp.element_type_id,
			cp.user_id, COALESCE(cp.stage_id, 0) AS stage_id, cp.started_at, cp.updated_at,
			COALESCE(cp.status, '') AS status, COALESCE(cp.floor_id, 0) AS floor_id,
			COALESCE(et.element_type_name, '') AS element_type_name,
			COALESCE(et.element_type, '') AS element_type,
			COALESCE(e.element_id, '') AS element_name,
			COALESCE(ps.name, '') AS stage_name,
			COALESCE(a.name, '') AS activity_name,
			COALESCE(e.target_location, 0) AS target_location
		FROM complete_production cp
		LEFT JOIN element_type et ON cp.element_type_id = et.element_type_id
		LEFT JOIN element e ON cp.element_id = e.id
		LEFT JOIN project_stages ps ON cp.stage_id = ps.id
		LEFT JOIN activity a ON cp.activity_id = a.id
		WHERE cp.user_id = $1 %s AND cp.updated_at IS NOT NULL
		ORDER BY cp.updated_at DESC`

	projectFilter := ""
	if hasProjectFilter {
		projectFilter = "AND cp.project_id = $2"
	}

	return fmt.Sprintf(baseQuery, projectFilter)
}

// GetCompleteProductionList retrieves complete production records for the authenticated user.
// This endpoint returns production records that have been completed, formatted as AppTaskResponse objects.
// It supports optional project_id filtering via query parameter.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function
//
// GetCompleteProductionList godoc
// @Summary      Get complete production list (app API)
// @Tags         app-tasks
// @Param        project_id  query  int  false  "Project ID (optional)"
// @Success      200         {object}  object
// @Failure      400         {object}  object
// @Failure      401         {object}  object
// @Router       /api/app/complete-production [get]
func GetCompleteProductionList(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, userName, userID, err := validateAndGetUserSession(c, db)
		if err != nil {
			handleAuthError(c, err)
			return
		}

		projectIDStr := strings.TrimSpace(c.Query("project_id"))
		hasProjectFilter := false
		var projectID int
		if projectIDStr != "" {
			pid, err := strconv.Atoi(projectIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
				return
			}
			hasProjectFilter = true
			projectID = pid
		}

		query := buildCompleteProductionQuery(hasProjectFilter)
		var args []interface{}
		if hasProjectFilter {
			args = []interface{}{userID, projectID}
		} else {
			args = []interface{}{userID}
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch records", "details": err.Error()})
			return
		}
		defer rows.Close()

		processCompleteProduction(rows, db, projectID, c, session, userName)
	}
}

// completeProductionWithDetails represents a complete production record with all related details
type completeProductionWithDetails struct {
	ID              int
	TaskID          int
	ActivityID      int
	ProjectID       int
	ElementID       int
	ElementTypeID   int
	UserID          int
	StageID         int
	StartedAt       time.Time
	UpdatedAt       *time.Time
	Status          string
	FloorID         int
	ElementType     string
	ElementTypeName string
	ElementName     string
	StageName       string
	ActivityName    string
	TargetLocation  sql.NullInt64
}

// scanCompleteProductionRow scans a single row from the complete production result set.
//
// Parameters:
//   - rows: Database result set
//
// Returns:
//   - cp: Scanned complete production record
//   - err: Error if scanning fails
func scanCompleteProductionRow(rows *sql.Rows) (completeProductionWithDetails, error) {
	var cp completeProductionWithDetails
	var updatedAt sql.NullTime

	err := rows.Scan(
		&cp.ID, &cp.TaskID, &cp.ActivityID, &cp.ProjectID, &cp.ElementID, &cp.ElementTypeID,
		&cp.UserID, &cp.StageID, &cp.StartedAt, &updatedAt, &cp.Status, &cp.FloorID,
		&cp.ElementTypeName, &cp.ElementType, &cp.ElementName, &cp.StageName, &cp.ActivityName, &cp.TargetLocation,
	)
	if err != nil {
		return completeProductionWithDetails{}, fmt.Errorf("failed to scan complete production row: %w", err)
	}

	if updatedAt.Valid {
		cp.UpdatedAt = &updatedAt.Time
	}

	return cp, nil
}

// buildCompleteProductionResponse creates an AppTaskResponse from complete production details.
//
// Parameters:
//   - db: Database connection
//   - cp: Complete production record details
//   - drawings: List of drawings for the element type
//
// Returns:
//   - response: Formatted AppTaskResponse
//   - err: Error if tower/floor lookup fails
func buildCompleteProductionResponse(db *sql.DB, cp completeProductionWithDetails, drawings []models.AppDrawing) (models.AppTaskResponse, error) {
	elementName := cp.ElementName
	if elementName == "" {
		elementName = strconv.Itoa(cp.ElementID)
	}

	tower := ""
	floor := ""
	if cp.TargetLocation.Valid && cp.TargetLocation.Int64 != 0 {
		t, f, err := getTowerAndFloor(db, cp.TargetLocation.Int64)
		if err != nil {
			log.Printf("Error fetching tower/floor: %v", err)
		} else {
			tower = t
			floor = f
		}
	}

	return models.AppTaskResponse{
		ID:              cp.ID,
		TaskID:          cp.TaskID,
		ProjectID:       cp.ProjectID,
		StageID:         cp.StageID,
		StageName:       cp.StageName,
		ElementID:       cp.ElementID,
		ElementName:     elementName,
		ElementType:     cp.ElementType,
		ElementTypeName: cp.ElementTypeName,
		Tower:           tower,
		Floor:           floor,
		FilterType:      "complete",
		Drawings:        drawings,
	}, nil
}

// processCompleteProduction processes database rows containing complete production records
// and converts them into AppTaskResponse objects with element type information and drawings.
//
// Parameters:
//   - rows: Database result set containing complete production rows
//   - db: Database connection
//   - projectID: Project ID for logging purposes
//   - c: Gin context for HTTP response
//   - session: User session information
//   - userName: Username for activity logging
func processCompleteProduction(rows *sql.Rows, db *sql.DB, projectID int, c *gin.Context, session models.Session, userName string) {
	var results []models.AppTaskResponse
	elementTypeDrawingsCache := make(map[int][]models.AppDrawing)

	// Scan all rows and build responses
	for rows.Next() {
		cp, err := scanCompleteProductionRow(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read record", "details": err.Error()})
			return
		}

		// Get drawings from cache or fetch from database
		drawings, exists := elementTypeDrawingsCache[cp.ElementTypeID]
		if !exists {
			drawings = fetchDrawingsForElementType(db, cp.ElementTypeID)
			elementTypeDrawingsCache[cp.ElementTypeID] = drawings
		}

		response, err := buildCompleteProductionResponse(db, cp, drawings)
		if err != nil {
			log.Printf("Error building complete production response: %v", err)
			continue
		}

		results = append(results, response)
	}

	if len(results) == 0 {
		c.JSON(http.StatusOK, models.AppTaskListResponse{
			Tasks:      []models.AppTaskResponse{},
			TotalCount: 0,
			Filter:     "complete",
		})
		return
	}

	response := models.AppTaskListResponse{
		Tasks:      results,
		TotalCount: len(results),
		Filter:     "complete",
	}

	c.JSON(http.StatusOK, response)
	logActivity(db, session, userName, projectID, "Get Complete Production List")
}

// GetTaskCountByStatus retrieves count of tasks grouped by status for the authenticated user.
// The endpoint returns counts for four status categories: pending, completed, inprogress, and rejected.
// It uses the same filtering logic as GetTaskListByAssignee to determine which tasks belong to the user.
// Supports optional project_id filtering via path or query parameter.
//
// Parameters:
//   - db: Database connection
//
// Returns:
//   - gin.HandlerFunc: HTTP handler function that returns AppTaskCountResponse
//
// GetTaskCountByStatus godoc
// @Summary      Get task count by status (app API)
// @Tags         app-tasks
// @Param        project_id  path     int  false  "Project ID (optional)"
// @Success      200         {object}  object
// @Failure      400         {object}  object
// @Failure      401         {object}  object
// @Router       /api/app/tasks/count [get]
// @Router       /api/app/tasks/count/{project_id} [get]
func GetTaskCountByStatus(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, userName, userID, err := validateAndGetUserSession(c, db)
		if err != nil {
			handleAuthError(c, err)
			return
		}

		projectID, hasProjectFilter, err := parseProjectID(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		counts, err := fetchTaskCountsByStatus(db, userID, projectID, hasProjectFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task counts", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, counts)
		logActivity(db, session, userName, projectID, "Get Task Count By Status")
	}
}

// fetchTaskCountsByStatus executes the database query and returns task counts grouped by status.
// The function aggregates tasks into four categories: pending, completed, inprogress, and rejected.
//
// Parameters:
//   - db: Database connection
//   - userID: ID of the user to filter tasks for
//   - projectID: Project ID (used only if hasProjectFilter is true)
//   - hasProjectFilter: Whether to include project_id in the query
//
// Returns:
//   - AppTaskCountResponse: Object containing counts for each status category and total
//   - error: Error if query execution or scanning fails
func fetchTaskCountsByStatus(db *sql.DB, userID, projectID int, hasProjectFilter bool) (models.AppTaskCountResponse, error) {
	query := buildTaskCountQuery(hasProjectFilter)

	var args []interface{}
	if hasProjectFilter {
		args = []interface{}{projectID, userID}
	} else {
		args = []interface{}{userID}
	}

	var pending, completed, inprogress, rejected, total int
	err := db.QueryRow(query, args...).Scan(&pending, &completed, &inprogress, &rejected, &total)
	if err != nil {
		return models.AppTaskCountResponse{}, fmt.Errorf("failed to scan task counts: %w", err)
	}

	return models.AppTaskCountResponse{
		Pending:   pending,
		Completed: completed,
		Total:     total,
	}, nil
}

// handleAuthError handles authentication and authorization errors with appropriate HTTP status codes.
// It distinguishes between missing session headers (400 Bad Request) and invalid sessions (401 Unauthorized).
//
// Parameters:
//   - c: Gin context for HTTP response
//   - err: Error object containing the error message
func handleAuthError(c *gin.Context, err error) {
	if strings.Contains(err.Error(), "session_id header is missing") {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
	}
}

// logActivity logs user activity to the activity log table for audit purposes.
// This function is used to track user actions across the application.
//
// Parameters:
//   - db: Database connection
//   - session: User session information
//   - userName: Username performing the action
//   - projectID: Project ID associated with the action (0 if not applicable)
//   - description: Description of the activity being logged
func logActivity(db *sql.DB, session models.Session, userName string, projectID int, description string) {
	logEntry := models.ActivityLog{
		EventContext: "Task",
		EventName:    "Get",
		Description:  description,
		UserName:     userName,
		HostName:     session.HostName,
		IPAddress:    session.IPAddress,
		CreatedAt:    time.Now(),
		ProjectID:    projectID,
	}
	if err := handlers.SaveActivityLog(db, logEntry); err != nil {
		log.Printf("Failed to save activity log: %v", err)
	}
}

// ritesh

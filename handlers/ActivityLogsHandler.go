package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Helper to fetch session details
func GetSessionDetails(db *sql.DB, sessionID string) (models.Session, string, error) {
	var session models.Session
	var userName string

	query := `
        SELECT s.user_id, CONCAT(u.first_name, ' ', u.last_name) AS user_name, s.host_name, s.ip_address 
        FROM session s
        JOIN users u ON s.user_id = u.id
        WHERE s.session_id = $1`

	err := db.QueryRow(query, sessionID).Scan(
		&session.UserID,
		&userName,
		&session.HostName,
		&session.IPAddress,
	)
	if err != nil {
		return models.Session{}, "", err
	}
	return session, userName, nil
}

// Helper to save activity logs
func SaveActivityLog(db *sql.DB, log models.ActivityLog) error {
	query := `
    INSERT INTO activity_logs (
        created_at, user_name, host_name, event_context, ip_address,
        description, event_name, affected_user_name, affected_user_email, project_id
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := db.Exec(query,
		log.CreatedAt, log.UserName, log.HostName, log.EventContext, log.IPAddress,
		log.Description, log.EventName, log.AffectedUserName, log.AffectedUserEmail, log.ProjectID,
	)
	return err
}

// GetActivityLogsHandler godoc
// @Summary      Get activity logs
// @Tags         activity-logs
// @Param        page   query  int  false  "Page"
// @Param        limit  query  int  false  "Limit"
// @Success      200    {object}  object
// @Router       /api/logs [get]
func GetActivityLogsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "10")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 10
		}

		offset := (page - 1) * limit

		// ----------- Step 1: Count total records -----------
		var totalRecords int
		countQuery := `SELECT COUNT(*) FROM activity_logs`
		if err := db.QueryRow(countQuery).Scan(&totalRecords); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error counting logs"})
			return
		}

		totalPages := int(math.Ceil(float64(totalRecords) / float64(limit)))
		hasNext := page < totalPages
		hasPrev := page > 1

		// ----------- Step 2: Fetch paginated data -----------
		query := `
			SELECT id, created_at, user_name, host_name, event_context, ip_address,
				   description, event_name, affected_user_name, affected_user_email, project_id
			FROM activity_logs
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`

		rows, err := db.Query(query, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error querying logs"})
			return
		}
		defer rows.Close()

		var logs []models.ActivityLog
		for rows.Next() {
			var (
				log               models.ActivityLog
				userName          sql.NullString
				hostName          sql.NullString
				eventContext      sql.NullString
				ipAddress         sql.NullString
				description       sql.NullString
				eventName         sql.NullString
				affectedUserName  sql.NullString
				affectedUserEmail sql.NullString
				projectID         sql.NullInt64
			)

			err := rows.Scan(
				&log.ID, &log.CreatedAt, &userName, &hostName, &eventContext, &ipAddress,
				&description, &eventName, &affectedUserName, &affectedUserEmail, &projectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning logs"})
				return
			}

			log.UserName = getStringOrEmpty(userName)
			log.HostName = getStringOrEmpty(hostName)
			log.EventContext = getStringOrEmpty(eventContext)
			log.IPAddress = getStringOrEmpty(ipAddress)
			log.Description = getStringOrEmpty(description)
			log.EventName = getStringOrEmpty(eventName)
			log.AffectedUserName = getStringOrEmpty(affectedUserName)
			log.AffectedUserEmail = getStringOrEmpty(affectedUserEmail)
			log.ProjectID = getIntOrZero(projectID)

			logs = append(logs, log)
		}

		// ----------- Step 3: Send response -----------
		c.JSON(http.StatusOK, gin.H{
			"logs": logs,
			"pagination": gin.H{
				"current_page":  page,
				"page_size":     limit,
				"total_records": totalRecords,
				"total_pages":   totalPages,
				"has_next":      hasNext,
				"has_prev":      hasPrev,
			},
		})
	}
}

func getStringOrEmpty(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func getIntOrZero(ni sql.NullInt64) int {
	if ni.Valid {
		return int(ni.Int64)
	}
	return 0
}

// SearchActivityLogsHandler godoc
// @Summary      Search activity logs
// @Tags         activity-logs
// @Param        q  query  string  false  "Search query"
// @Success      200  {object}  object
// @Router       /api/log/search [get]
func SearchActivityLogsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Collect all expected filters
		filters := map[string]interface{}{
			"user_name":           c.Query("user_name"),
			"host_name":           c.Query("host_name"),
			"event_context":       c.Query("event_context"),
			"ip_address":          c.Query("ip_address"),
			"description":         c.Query("description"),
			"event_name":          c.Query("event_name"),
			"affected_user_name":  c.Query("affected_user_name"),
			"affected_user_email": c.Query("affected_user_email"),
			"project_id":          c.Query("project_id"),
		}

		// Optional match type for event_context (default: contains)
		eventContextMatch := c.DefaultQuery("event_context_match", "contains")

		// Pagination
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 10
		}
		offset := (page - 1) * limit

		// Dynamic WHERE clause
		whereClauses := []string{}
		args := []interface{}{}
		argIndex := 1

		for key, value := range filters {
			// Skip empty values
			strVal := strings.TrimSpace(fmt.Sprintf("%v", value))
			if strVal == "" {
				continue
			}

			switch key {
			case "project_id":
				whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", key, argIndex))
				val, err := strconv.Atoi(strVal)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project_id"})
					return
				}
				args = append(args, val)

			case "event_context":
				if eventContextMatch == "exact" {
					whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", key, argIndex))
					args = append(args, strVal)
				} else {
					whereClauses = append(whereClauses, fmt.Sprintf("%s ILIKE $%d", key, argIndex))
					args = append(args, "%"+strVal+"%")
				}

			default:
				whereClauses = append(whereClauses, fmt.Sprintf("%s ILIKE $%d", key, argIndex))
				args = append(args, "%"+strVal+"%")
			}
			argIndex++
		}

		// ---------- Count total matching records ----------
		countQuery := `SELECT COUNT(*) FROM activity_logs`
		if len(whereClauses) > 0 {
			countQuery += " WHERE " + strings.Join(whereClauses, " AND ")
		}

		var totalRecords int
		err := db.QueryRow(countQuery, args...).Scan(&totalRecords)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error counting logs"})
			return
		}

		totalPages := int(math.Ceil(float64(totalRecords) / float64(limit)))
		hasNext := page < totalPages
		hasPrev := page > 1

		// ---------- Build main SELECT query ----------
		selectQuery := `
			SELECT id, created_at, user_name, host_name, event_context, ip_address,
				   description, event_name, affected_user_name, affected_user_email, project_id
			FROM activity_logs
		`
		if len(whereClauses) > 0 {
			selectQuery += " WHERE " + strings.Join(whereClauses, " AND ")
		}
		selectQuery += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIndex, argIndex+1)

		args = append(args, limit, offset)

		rows, err := db.Query(selectQuery, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error searching logs"})
			return
		}
		defer rows.Close()

		var logs []models.ActivityLog
		for rows.Next() {
			var (
				log               models.ActivityLog
				userName          sql.NullString
				hostName          sql.NullString
				eventContext      sql.NullString
				ipAddress         sql.NullString
				description       sql.NullString
				eventName         sql.NullString
				affectedUserName  sql.NullString
				affectedUserEmail sql.NullString
				projectID         sql.NullInt64
			)

			err := rows.Scan(
				&log.ID, &log.CreatedAt, &userName, &hostName, &eventContext,
				&ipAddress, &description, &eventName, &affectedUserName,
				&affectedUserEmail, &projectID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning logs"})
				return
			}

			log.UserName = getStringOrEmpty(userName)
			log.HostName = getStringOrEmpty(hostName)
			log.EventContext = getStringOrEmpty(eventContext)
			log.IPAddress = getStringOrEmpty(ipAddress)
			log.Description = getStringOrEmpty(description)
			log.EventName = getStringOrEmpty(eventName)
			log.AffectedUserName = getStringOrEmpty(affectedUserName)
			log.AffectedUserEmail = getStringOrEmpty(affectedUserEmail)
			log.ProjectID = getIntOrZero(projectID)

			logs = append(logs, log)
		}

		// ---------- Final JSON response ----------
		c.JSON(http.StatusOK, gin.H{
			"logs": logs,
			"pagination": gin.H{
				"current_page":  page,
				"page_size":     limit,
				"total_records": totalRecords,
				"total_pages":   totalPages,
				"has_next":      hasNext,
				"has_prev":      hasPrev,
			},
		})
	}
}

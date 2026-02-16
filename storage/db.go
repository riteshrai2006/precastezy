package storage

import (
	"backend/models"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var db *sql.DB

func InitDB() *sql.DB {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")

	connStr := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable",
		user, password, dbname, host, port)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Set connection pool settings optimized for light server load
	db.SetMaxOpenConns(20)                 // Maximum number of open connections (reduced for lighter load)
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections (reduced)
	db.SetConnMaxLifetime(5 * time.Minute) // Close connections after 5 minutes (reduced)
	db.SetConnMaxIdleTime(2 * time.Minute) // Close idle connections after 2 minutes (reduced)

	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	return db
}

func GetDB() *sql.DB {
	return db
}

// SaveSession saves a new session for a user, handling multiple device support.
// If allowMultipleSessions is true, it allows multiple devices to be logged in simultaneously.
// If false, it deletes all existing sessions before creating a new one.
func SaveSession(db *sql.DB, session *models.Session, allowMultipleSessions bool) error {
	if !allowMultipleSessions {
		// If multiple sessions are NOT allowed, delete all existing sessions for this user
		deleteAllQuery := `DELETE FROM session WHERE user_id = $1`
		_, err := db.Exec(deleteAllQuery, session.UserID)
		if err != nil {
			return fmt.Errorf("failed to delete all user sessions: %v", err)
		}
	}

	// Insert the new session with refresh token stored in the same table
	insertQuery := `INSERT INTO session (user_id, session_id, host_name, ip_address, timestp, expires_at, refresh_token, refresh_token_expires_at)
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := db.Exec(insertQuery, session.UserID, session.SessionID, session.HostName, session.IPAddress, session.Timestamp, session.ExpiresAt, session.RefreshToken, session.RefreshTokenExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert new session: %v", err)
	}
	return nil
}

// SaveRefreshToken stores a refresh token in the session table bound to a session.
// This allows each device/session to have its own refresh token.
func SaveRefreshToken(db *sql.DB, userID int, sessionID string, refreshToken string, expiresAt time.Time) error {
	// Update the session table with the refresh token
	updateQuery := `UPDATE session SET refresh_token = $1, refresh_token_expires_at = $2 WHERE session_id = $3 AND user_id = $4`

	result, err := db.Exec(updateQuery, refreshToken, expiresAt, sessionID, userID)
	if err != nil {
		return fmt.Errorf("failed to save refresh token: %v", err)
	}

	// Check if any row was updated
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found for session_id: %s and user_id: %d", sessionID, userID)
	}

	return nil
}

// GetRefreshTokenBySession retrieves a refresh token for a specific session
func GetRefreshTokenBySession(db *sql.DB, sessionID string) (string, error) {
	var refreshToken string
	err := db.QueryRow(`
		SELECT refresh_token FROM session 
		WHERE session_id = $1 AND refresh_token_expires_at > NOW()`, sessionID).Scan(&refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token not found: %v", err)
	}
	return refreshToken, nil
}

// DeleteRefreshToken removes a refresh token for a session (for logout)
func DeleteRefreshToken(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`UPDATE session SET refresh_token = NULL, refresh_token_expires_at = NULL WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete refresh token: %v", err)
	}
	return nil
}

func GetSession(db *sql.DB, userID int) (*models.Session, error) {
	var session models.Session
	query := `SELECT user_id, session_id, host_name, timestp FROM session WHERE user_id = $1`
	err := db.QueryRow(query, userID).Scan(&session.UserID, &session.SessionID, &session.HostName, &session.Timestamp)
	return &session, err
}

func DeleteSession(db *sql.DB, userID int) error {
	query := `DELETE FROM session WHERE user_id = $1`
	_, err := db.Exec(query, userID)
	return err
}

// GetUserSessionCount returns the number of active sessions for a user
func GetUserSessionCount(db *sql.DB, userID int) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM session WHERE user_id = $1 AND expires_at > NOW()`
	err := db.QueryRow(query, userID).Scan(&count)
	return count, err
}

// GetUserSessions returns all active sessions for a user
func GetUserSessions(db *sql.DB, userID int) ([]models.Session, error) {
	query := `SELECT user_id, session_id, host_name, ip_address, timestp, expires_at 
              FROM session WHERE user_id = $1 AND expires_at > NOW() 
              ORDER BY timestp DESC`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var session models.Session
		err := rows.Scan(&session.UserID, &session.SessionID, &session.HostName, &session.IPAddress, &session.Timestamp, &session.ExpiresAt)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetActiveDevices returns active device information for a user
// Returns session_id, ip_address, and timestamp for each active device
func GetActiveDevices(db *sql.DB, userID int) ([]map[string]interface{}, error) {
	query := `SELECT session_id, ip_address, timestp, expires_at 
              FROM session 
              WHERE user_id = $1 AND expires_at > NOW() 
              ORDER BY timestp DESC`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []map[string]interface{}
	for rows.Next() {
		var sessionID, ipAddress string
		var timestamp, expiresAt time.Time
		err := rows.Scan(&sessionID, &ipAddress, &timestamp, &expiresAt)
		if err != nil {
			return nil, err
		}
		devices = append(devices, map[string]interface{}{
			"session_id": sessionID,
			"ip_address": ipAddress,
			"login_time": timestamp,
			"expires_at": expiresAt,
		})
	}

	return devices, nil
}

// DeleteSessionByID deletes a specific session by session_id
func DeleteSessionByID(db *sql.DB, sessionID string, userID int) error {
	query := `DELETE FROM session WHERE session_id = $1 AND user_id = $2`
	result, err := db.Exec(query, sessionID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found or already deleted")
	}

	return nil
}

func GetUserByEmail(db *sql.DB, email string) (*models.User, error) {
	var user models.User
	query := `SELECT id, email, password, suspended, project_suspend FROM users WHERE LOWER(email) = LOWER($1)`

	err := db.QueryRow(query, email).Scan(&user.ID, &user.Email, &user.Password, &user.Suspended, &user.ProjectSuspend)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user with email %s not found", email)
		}
		return nil, fmt.Errorf("failed to query user: %v", err)
	}

	return &user, nil
}

func UpdateSessionToken(db *sql.DB, userID int, token string, email string) error {
	query := `UPDATE session SET session_id = $1, host_name = $2, timestamp = $3 WHERE user_id = $4`
	_, err := db.Exec(query, token, email, time.Now(), userID)
	return err
}

func CleanupExpiredSessions(db *sql.DB) error {
	threshold := time.Now().Add(-24 * time.Hour)
	_, err := db.Exec("DELETE FROM session WHERE expires_at < $1", threshold)
	return err
}

func LogChange(db *sql.DB, userID, entityType, entityID, changeType, oldValue, newValue string) error {
	query := `INSERT INTO user_changes (user_id, entity_type, entity_id, change_type, old_value, new_value) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := db.Exec(query, userID, entityType, entityID, changeType, oldValue, newValue)
	return err
}

// GetUserBySessionID retrieves a User by the given session ID from the database.
func GetUserBySessionID(db *sql.DB, sessionID string) (*models.User, error) {
	// Assuming there is a table named 'session' where the session ID is stored along with user ID
	// and a table named 'users' where user details are stored.
	query := `
		SELECT u.id, u.employee_id, u.email, u.first_name, u.last_name, 
			   u.created_at, u.updated_at, u.first_access, u.last_access,
			   u.profile_picture, u.is_admin, u.address, u.city, 
			   u.state, u.country, u.zip_code, u.phone_no, r.role_name, u.suspended
		FROM session s
		JOIN users u ON s.user_id = u.id
		JOIN roles r ON u.role_id = r.role_id
		WHERE s.session_id = $1
	`

	var user models.User
	var firstAccess, lastAccess sql.NullTime

	err := db.QueryRow(query, sessionID).Scan(
		&user.ID, &user.EmployeeId, &user.Email, &user.FirstName,
		&user.LastName, &user.CreatedAt, &user.UpdatedAt,
		&firstAccess, &lastAccess, &user.ProfilePic,
		&user.IsAdmin, &user.Address, &user.City,
		&user.State, &user.Country, &user.ZipCode,
		&user.PhoneNo, &user.RoleName, &user.Suspended,
	)
	if err != nil || user.Suspended {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found for the given session ID or account suspended")
		}
		return nil, err
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

	return &user, nil
}

// GetPermissionID fetches the permission ID by its name from the database
func GetPermissionID(db *sql.DB, permissionName string) (int, error) {
	var permissionID int
	query := `SELECT permission_id FROM permissions WHERE permission_name = $1`
	err := db.QueryRow(query, permissionName).Scan(&permissionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, errors.New("permission not found")
		}
		return 0, err
	}
	return permissionID, nil
}

func GetRoleIDByUserID(db *sql.DB, userID int) (int, error) {
	var roleID int
	query := `SELECT role_id FROM user_roles WHERE user_id = $1` // Adjust table name if needed.
	err := db.QueryRow(query, userID).Scan(&roleID)
	return roleID, err
}

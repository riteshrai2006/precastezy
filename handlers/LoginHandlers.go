package handlers

import (
	"backend/models"
	"backend/storage"
	"backend/utils"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// LoginHandler handles user authentication
// @Summary Login user
// @Description Authenticate user and return session token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} models.LoginResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Router /api/login [post]

func LoginHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for the token in the Authorization header
		token := c.GetHeader("Authorization")

		// Remove "Bearer " prefix if present (TrimPrefix is safe even if prefix doesn't exist)
		token = strings.TrimPrefix(token, "Bearer ")

		// Trim any whitespace
		token = strings.TrimSpace(token)

		// If token exists and is valid, use token-based login
		if token != "" {
			parsedToken, err := utils.ValidateJWT(token)
			// If token validation fails, fall through to email/password login
			// This allows users with expired/invalid tokens to still log in with credentials
			if err == nil && parsedToken.Valid {
				// Ensure parsedToken.Claims can be type-asserted to jwt.MapClaims
				claims, ok := parsedToken.Claims.(jwt.MapClaims)
				if !ok {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims structure"})
					return
				}

				// Check if the "Email" field exists and is of type string
				email, ok := claims["email"].(string)
				if !ok || email == "" {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Email claim missing or invalid"})
					return
				}

				// Retrieve the user based on the email claim
				user, err := storage.GetUserByEmail(db, email)
				if err != nil {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
					return
				}

				if user.Suspended {
					c.JSON(http.StatusForbidden, gin.H{"error": "Account is suspended"})
					return
				}

				// Fetch role name to check if user is QC
				var roleName string
				err = db.QueryRow("SELECT r.role_name FROM users u JOIN roles r ON u.role_id = r.role_id WHERE u.id = $1", user.ID).Scan(&roleName)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user role", "details": err.Error()})
					return
				}

				// Check if role is QA/QC (case-insensitive)
				isQC := strings.EqualFold(roleName, "QA/QC")

				// Token is valid and user is found
				c.JSON(http.StatusOK, gin.H{
					"message":      "User successfully logged in via token",
					"access_token": token,
					"qc":           isQC,
					"role":         roleName,
					"user": gin.H{
						"id":    user.ID,
						"email": user.Email,
					},
				})
				return
			}
			// If token validation failed, fall through to email/password login
		}

		// No valid token; proceed with email and password login
		var loginData struct {
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required"`
			IP       string `json:"ip" binding:"required"` // Include IP in the JSON payload
		}

		if err := c.ShouldBindJSON(&loginData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		// Retrieve user by email
		user, err := storage.GetUserByEmail(db, loginData.Email)
		if err != nil || user.Password != loginData.Password {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}

		if user.Suspended || user.ProjectSuspend {
			c.JSON(http.StatusForbidden, gin.H{"error": "Account is suspended"})
			return
		}

		// Fetch the "multiple sessions" setting for this specific user
		// Default to true to allow multiple devices by default
		allowMultipleSessions := true
		// err = db.QueryRow("SELECT allow_multiple_sessions FROM settings WHERE user_id = $1", user.ID).Scan(&allowMultipleSessions)
		// if err != nil {
		// 	// If no setting exists for the user, default to true (allow multiple sessions)
		// 	// Only return error if it's not a "no rows" error
		// 	if err != sql.ErrNoRows {
		// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings", "details": err.Error()})
		// 		return
		// 	}
		// 	// Default to true if no setting exists (allow multiple devices)
		// 	allowMultipleSessions = true
		// }

		// Check device count FIRST before generating any tokens or proceeding with login
		// This prevents unnecessary token generation if user already has 3 devices
		if allowMultipleSessions {
			sessionCount, err := storage.GetUserSessionCount(db, user.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check active sessions", "details": err.Error()})
				return
			}

			const maxSessions = 3
			// If user already has 3 active sessions, return error immediately
			// IMPORTANT: No devices are logged out automatically - user must manually logout
			if sessionCount >= maxSessions {
				devices, err := storage.GetActiveDevices(db, user.ID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get active devices", "details": err.Error()})
					return
				}

				// Return 409 Conflict - login does NOT proceed, no tokens generated, no devices logged out
				c.JSON(http.StatusConflict, gin.H{
					"error":           "Maximum device limit reached",
					"message":         "You have reached the maximum limit of 3 active devices. Please logout from one device to continue.",
					"max_devices":     maxSessions,
					"current_devices": sessionCount,
					"active_devices":  devices,
					"requires_logout": true,
				})
				return // Early return - prevents token generation and session creation
			}
		}

		// Only generate tokens if device limit check passes
		// Generate a new JWT token
		newToken, err := utils.GenerateJWT(user.Email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}

		// Generate refresh token bound to this session (device)
		refreshToken, err := utils.GenerateRefreshToken(user.Email, newToken)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
			return
		}

		// Create and save a new session with refresh token
		// Access token expires in 15 minutes, refresh token expires in 15 days
		session := &models.Session{
			UserID:                user.ID,
			SessionID:             newToken,
			HostName:              user.Email,
			IPAddress:             loginData.IP,
			Timestamp:             time.Now(),
			ExpiresAt:             time.Now().Add(15 * time.Minute), // Access token expiry
			RefreshToken:          refreshToken,
			RefreshTokenExpiresAt: time.Now().Add(15 * 24 * time.Hour), // Refresh token expiry (15 days)
		}

		// Save session with refresh token in the same table
		if err := storage.SaveSession(db, session, allowMultipleSessions); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session", "details": err.Error()})
			return
		}

		// Fetch role name to check if user is QC
		var roleName string
		err = db.QueryRow("SELECT r.role_name FROM users u JOIN roles r ON u.role_id = r.role_id WHERE u.id = $1", user.ID).Scan(&roleName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user role", "details": err.Error()})
			return
		}

		// Check if role is QA/QC (case-insensitive)
		isQC := strings.EqualFold(roleName, "QA/QC")

		c.JSON(http.StatusOK, gin.H{
			"message":       "Login successful",
			"access_token":  newToken,
			"refresh_token": refreshToken,
			"qc":            isQC,
			"role":          roleName,
			"expires_in":    900, // 15 minutes in seconds
		})

		var name string
		err = db.QueryRow(`SELECT first_name from users where id = $1`, session.UserID).Scan(&name)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"err": err.Error()})
			return
		}

		log := models.ActivityLog{
			EventContext: "Login",
			EventName:    "Post",
			Description:  "User Logged In",
			UserName:     name,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0, // No specific project ID for this operation
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

// GetSessionHandler retrieves session information
// @Summary Get session by user ID
// @Description Retrieve session information for a specific user
// @Tags Authentication
// @Accept json
// @Produce json
// @Param user_id path int true "User ID"
// @Success 200 {object} models.SessionResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/session/{user_id} [get]
func GetSessionHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			utils.ErrorResponse(c, "No token provided", http.StatusUnauthorized)
			return
		}

		parsedToken, err := utils.ValidateJWT(token)
		if err != nil {
			utils.ErrorResponse(c, "Invalid token", http.StatusUnauthorized)
			return
		}

		claims := parsedToken.Claims.(jwt.MapClaims)
		exp, ok := claims["exp"].(float64)
		if !ok || time.Now().Unix() > int64(exp) {
			utils.ErrorResponse(c, "Token expired", http.StatusUnauthorized)
			return
		}

		email, ok := claims["Email"].(string)
		if !ok {
			utils.ErrorResponse(c, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		user, err := storage.GetUserByEmail(db, email)
		if err != nil {
			utils.ErrorResponse(c, "User not found", http.StatusUnauthorized)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "User is logged in", "user": user})
	}
}

// DeleteSessionHandler deletes user session
// @Summary Delete session
// @Description Delete user session
// @Tags Authentication
// @Accept json
// @Produce json
// @Param user_id path int true "User ID"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Router /api/session/{user_id} [delete]
func DeleteSessionHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("user_id")

		userIDInt, err := strconv.Atoi(userID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		if err := storage.DeleteSession(db, userIDInt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete session"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Session deleted, user logged out"})
	}
}

// GetActiveDevicesHandler returns all active devices for the authenticated user
// @Summary Get active devices
// @Description Get list of all active devices/sessions for the current user
// @Tags Authentication
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer token"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} models.ErrorResponse
// @Router /api/active-devices [get]
func GetActiveDevicesHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from header
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing Authorization header"})
			return
		}

		sessionToken := authHeader
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(sessionToken, bearerPrefix) {
			sessionToken = strings.TrimSpace(strings.TrimPrefix(sessionToken, bearerPrefix))
		}

		// Validate token and get user
		parsedToken, err := utils.ValidateJWT(sessionToken)
		if err != nil || !parsedToken.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		// Access tokens store email in lowercase claim key per utils.GenerateJWT,
		// but fall back to "Email" for older tokens.
		email, _ := claims["email"].(string)
		if email == "" {
			email, _ = claims["Email"].(string)
		}
		if email == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Email claim missing or invalid"})
			return
		}

		user, err := storage.GetUserByEmail(db, email)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			return
		}

		// Get active devices
		devices, err := storage.GetActiveDevices(db, user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get active devices", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":        "Active devices retrieved successfully",
			"active_devices": devices,
			"device_count":   len(devices),
		})
	}
}

// LogoutDeviceHandler logs out a specific device by session_id
// @Summary Logout specific device
// @Description Logout a specific device by providing its session_id
// @Tags Authentication
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer token"
// @Param request body map[string]string true "Session ID to logout"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/logout-device [post]
func LogoutDeviceHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from header to identify the user
		// authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		// if authHeader == "" {
		// 	c.JSON(http.StatusBadRequest, gin.H{"error": "Missing Authorization header"})
		// 	return
		// }

		// sessionToken := authHeader
		// const bearerPrefix = "Bearer "
		// if strings.HasPrefix(sessionToken, bearerPrefix) {
		// 	sessionToken = strings.TrimSpace(strings.TrimPrefix(sessionToken, bearerPrefix))
		// }

		// // Validate token and get user
		// parsedToken, err := utils.ValidateJWT(sessionToken)
		// if err != nil || !parsedToken.Valid {
		// 	c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		// 	return
		// }

		// claims, ok := parsedToken.Claims.(jwt.MapClaims)
		// if !ok {
		// 	c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
		// 	return
		// }

		// // Prefer lowercase "email" claim, fallback to "Email" for older tokens.
		// email, _ := claims["email"].(string)
		// if email == "" {
		// 	email, _ = claims["Email"].(string)
		// }
		// if email == "" {
		// 	c.JSON(http.StatusUnauthorized, gin.H{"error": "Email claim missing or invalid"})
		// 	return
		// }

		// user, err := storage.GetUserByEmail(db, email)
		// if err != nil {
		// 	c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		// 	return
		// }

		// Get session_id from request body
		var requestData struct {
			SessionID string `json:"session_id" binding:"required"`
		}

		if err := c.ShouldBindJSON(&requestData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Verify the session belongs to this user before deleting
		var sessionUserID int
		err := db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", requestData.SessionID).Scan(&sessionUserID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify session", "details": err.Error()})
			return
		}

		// // Ensure the session belongs to the authenticated user
		// if sessionUserID != user.ID {
		// 	c.JSON(http.StatusForbidden, gin.H{"error": "You can only logout your own devices"})
		// 	return
		// }

		// Delete the specific session
		if err := storage.DeleteSessionByID(db, requestData.SessionID, sessionUserID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout device", "details": err.Error()})
			return
		}

		// Also clear the refresh token
		_ = storage.DeleteRefreshToken(db, requestData.SessionID)

		c.JSON(http.StatusOK, gin.H{
			"message":    "Device logged out successfully",
			"session_id": requestData.SessionID,
		})
	}
}

// RefreshTokenHandler handles refresh token requests to get new access tokens
// @Summary Refresh access token
// @Description Exchange refresh token for a new access token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body object true "Refresh token request" SchemaExample({"refresh_token": "string"})
// @Success 200 {object} object "New access token"
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/refresh-token [post]
func RefreshTokenHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var refreshRequest struct {
			RefreshToken string `json:"refresh_token" binding:"required"`
		}

		if err := c.ShouldBindJSON(&refreshRequest); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Refresh token is required"})
			return
		}

		// Validate the refresh token
		parsedToken, err := utils.ValidateJWT(refreshRequest.RefreshToken)
		if err != nil || !parsedToken.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
			return
		}

		// Check token type
		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims structure"})
			return
		}

		// Verify it's a refresh token
		tokenType, ok := claims["type"].(string)
		if !ok || tokenType != "refresh" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token type"})
			return
		}

		// Get sessionId from token to scope refresh to a single device/session
		sessionID, ok := claims["sessionId"].(string)
		if !ok || sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session information missing in refresh token"})
			return
		}

		// Get email from token
		email, ok := claims["email"].(string)
		if !ok || email == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Email claim missing or invalid"})
			return
		}

		// Retrieve user
		user, err := storage.GetUserByEmail(db, email)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			return
		}

		if user.Suspended || user.ProjectSuspend {
			c.JSON(http.StatusForbidden, gin.H{"error": "Account is suspended"})
			return
		}

		// Verify refresh token exists and is still valid
		// Query by refresh_token and user_id instead of session_id, since session_id changes on each refresh
		// Only check refresh_token_expires_at, not expires_at, because access tokens expire (15 min) but refresh tokens are valid for 15 days
		var existingUserID int
		var existingSessionID string
		var refreshTokenExpiresAt time.Time
		err = db.QueryRow(`
			SELECT user_id, session_id, refresh_token_expires_at FROM session 
			WHERE refresh_token = $1 AND user_id = $2 AND refresh_token_expires_at > NOW()`,
			refreshRequest.RefreshToken, user.ID).Scan(&existingUserID, &existingSessionID, &refreshTokenExpiresAt)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found, expired, or refresh token mismatch"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify session", "details": err.Error()})
			}
			return
		}

		// Generate new access token bound to the same session owner
		newAccessToken, err := utils.GenerateJWT(user.Email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate access token"})
			return
		}

		// Only rotate refresh token if it's expiring within 1 day (security best practice)
		// Otherwise, keep the same refresh token until it actually expires
		now := time.Now()
		refreshTokenExpiresSoon := refreshTokenExpiresAt.Sub(now) < 24*time.Hour
		var newRefreshToken string
		var newRefreshTokenExpiresAt time.Time

		if refreshTokenExpiresSoon {
			// Generate new refresh token if current one expires within 24 hours
			newRefreshToken, err = utils.GenerateRefreshToken(user.Email, newAccessToken)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
				return
			}
			newRefreshTokenExpiresAt = time.Now().Add(15 * 24 * time.Hour) // Refresh token expiry (15 days)
		} else {
			// Keep the same refresh token
			newRefreshToken = refreshRequest.RefreshToken
			newRefreshTokenExpiresAt = refreshTokenExpiresAt
		}

		// Update access token (session_id) and expires_at
		// Only update refresh_token if it's being rotated (expiring soon)
		var result sql.Result
		var updateErr error
		if refreshTokenExpiresSoon {
			// Update both access token and refresh token
			result, updateErr = db.Exec(`
				UPDATE session 
				SET session_id = $1, expires_at = $2, timestp = $3, refresh_token = $4, refresh_token_expires_at = $5
				WHERE refresh_token = $6 AND user_id = $7`,
				newAccessToken, time.Now().Add(15*time.Minute), time.Now(), newRefreshToken, newRefreshTokenExpiresAt, refreshRequest.RefreshToken, user.ID)
		} else {
			// Only update access token, keep refresh_token unchanged
			result, updateErr = db.Exec(`
				UPDATE session 
				SET session_id = $1, expires_at = $2, timestp = $3
				WHERE refresh_token = $4 AND user_id = $5`,
				newAccessToken, time.Now().Add(15*time.Minute), time.Now(), refreshRequest.RefreshToken, user.ID)
		}

		if updateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update session", "details": updateErr.Error()})
			return
		}

		// Verify that exactly one row was updated (safety check)
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify session update", "details": err.Error()})
			return
		}
		if rowsAffected == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session update failed - no matching session found"})
			return
		}
		if rowsAffected > 1 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Multiple sessions updated - this should not happen"})
			return
		}

		// Fetch role name to check if user is QC
		var roleName string
		err = db.QueryRow("SELECT r.role_name FROM users u JOIN roles r ON u.role_id = r.role_id WHERE u.id = $1", user.ID).Scan(&roleName)
		if err != nil {
			// Log error but don't fail the request
			roleName = ""
		}

		isQC := strings.EqualFold(roleName, "QA/QC")

		c.JSON(http.StatusOK, gin.H{
			"message":       "Token refreshed successfully",
			"access_token":  newAccessToken,
			"refresh_token": newRefreshToken,
			"qc":            isQC,
			"expires_in":    900, // 5 minutes in seconds
		})
	}
}

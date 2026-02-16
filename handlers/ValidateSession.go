package handlers

import (
	"backend/utils"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// ValidateSession validates user session
// @Summary Validate session
// @Description Validate user session token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer token"
// @Success 200 {object} models.ValidateSessionResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/validate-session [post]

func ValidateSession(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		if sessionToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header missing token"})
			return
		}

		// Validate JWT (checks signature and expiration)
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

		exp, ok := claims["exp"].(float64)
		if !ok || time.Now().Unix() > int64(exp) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
			return
		}

		// Ensure session exists and is not expired in DB
		var sessionHost string
		var expiresAt time.Time
		err = db.QueryRow("SELECT host_name, expires_at FROM session WHERE session_id = $1 AND expires_at > NOW()", sessionToken).
			Scan(&sessionHost, &expiresAt)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session"})
			return
		}

		var host_name string
		err = db.QueryRow("SELECT role_id FROM users WHERE email = $1", sessionHost).Scan(&host_name)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details:": err.Error()})
			return
		}

		var role_name string
		err = db.QueryRow("SELECT role_name FROM roles WHERE role_id = $1", host_name).Scan(&role_name)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid role", "details:": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "Session validated",
			"session_id": sessionToken,
			"host_name":  sessionHost,
			"role_name":  role_name,
		})
	}
}

package handlers

import (
	"backend/models"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ForgetPasswordHandler godoc
// @Summary      Forgot password
// @Description  Request password reset link by email
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "{\"email\":\"user@example.com\"}"
// @Success      200   {object}  object
// @Failure      400   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Router       /api/auth/forgot-password [post]
func ForgetPasswordHandler(db *sql.DB, frontendBaseURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		type Request struct {
			Email string `json:"email" binding:"required,email"`
		}
		var req Request

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email"})
			return
		}

		var userID int
		err := db.QueryRow("SELECT id FROM users WHERE email=$1", req.Email).Scan(&userID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Email not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		token := uuid.New().String()
		expiry := time.Now().Add(15 * time.Minute)

		_, err = db.Exec(`UPDATE users SET reset_token=$1, reset_token_expiry=$2 WHERE id=$3`, token, expiry, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save token"})
			return
		}

		resetLink := fmt.Sprintf("%s%s", frontendBaseURL, token)
		log.Printf("Reset link: %s", resetLink)

		// Inside your handler after generating `resetLink`
		subject := "Reset Your Password"
		body := fmt.Sprintf("Click the link below to reset your password:\n\n%s\n\nThis link will expire in 15 minutes.", resetLink)

		if err := sendEmail(req.Email, subject, body); err != nil {
			log.Printf("Failed to send email: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send reset email"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Reset link sent to email"})
	}
}

func sendEmail(toEmail, subject, body string) error {
	auth := smtp.PlainAuth(
		"",
		"om.s@blueinvent.com", // username (email account)
		"gloycbfukxdyeczj",    // app password (Gmail app password)
		"smtp.gmail.com",      // SMTP host
	)

	from := "om.s@blueinvent.com"
	to := []string{toEmail}

	msg := []byte("From: " + from + "\r\n" +
		"To: " + toEmail + "\r\n" +
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

// ResetPasswordHandler godoc
// @Summary      Reset password with token
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        token   path      string  true  "Reset token"
// @Param        body    body      object  true  "{\"password\":\"newpassword\"}"
// @Success      200     {object}  object
// @Failure      400     {object}  models.ErrorResponse
// @Router       /api/auth/reset-password/{token} [post]
func ResetPasswordHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")
		if token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Token is required"})
			return
		}

		type Request struct {
			NewPassword string `json:"new_password" binding:"required,min=6"`
		}
		var req Request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid password format"})
			return
		}

		var userID int
		var expiry time.Time
		err := db.QueryRow(`SELECT id, reset_token_expiry FROM users WHERE reset_token=$1`, token).
			Scan(&userID, &expiry)

		if err == sql.ErrNoRows {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired token"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		if time.Now().After(expiry) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Token has expired"})
			return
		}

		// Save password directly (⚠️ not secure)
		_, err = db.Exec(`UPDATE users SET password=$1, reset_token=NULL, reset_token_expiry=NULL WHERE id=$2`, req.NewPassword, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Password reset successful"})
	}
}

// ChangePasswordHandler godoc
// @Summary      Change password (authenticated user)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "old_password, new_password"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/change_password [post]
func ChangePasswordHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		type Request struct {
			OldPassword string `json:"old_password" binding:"required"`
			NewPassword string `json:"new_password" binding:"required,min=6"`
		}
		var req Request

		// Step 1: Bind JSON
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}

		// Step 2: Extract session_id from Authorization header
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization token (session_id) required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Step 3: Get user_id from session table
		var userID int
		err = db.QueryRow(`SELECT user_id FROM session WHERE session_id = $1`, sessionID).Scan(&userID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Step 4: Get current password of user
		var currentPassword string
		err = db.QueryRow(`SELECT password FROM users WHERE id = $1`, userID).Scan(&currentPassword)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}

		// Step 5: Compare old_password
		if currentPassword != req.OldPassword {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Old password is incorrect"})
			return
		}

		// Step 6: Update password
		_, err = db.Exec(`UPDATE users SET password = $1 WHERE id = $2`, req.NewPassword, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})

		log := models.ActivityLog{
			EventContext: "Change Password",
			EventName:    "Update",
			Description:  "User changed password",
			UserName:     userName,
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

package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/models" // Replace with your actual project path

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"golang.org/x/net/html"
)

// CreateEmailTemplate creates a new email template
// @Summary Create email template
// @Description Create a new email template
// @Tags Email Templates
// @Accept json
// @Produce json
// @Param template body models.EmailTemplateRequest true "Email template data"
// @Success 201 {object} models.EmailTemplateResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/email-templates [post]
func CreateEmailTemplate(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		var request models.EmailTemplateRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}

		// Validate template type
		validTypes := []string{"welcome_client", "welcome_admin", "welcome_user", "password_reset", "project_invitation", "notification"}
		isValidType := false
		for _, t := range validTypes {
			if request.TemplateType == t {
				isValidType = true
				break
			}
		}
		if !isValidType {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template type", "valid_types": validTypes})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// If this template is set as default, unset other defaults of the same type
		if request.IsDefault {
			_, err = tx.Exec("UPDATE email_templates SET is_default = false WHERE template_type = $1", request.TemplateType)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update existing defaults"})
				return
			}
		}

		// Sanitize HTML body content from frontend text editor
		sanitizedBody := sanitizeHTML(request.Body)

		// Convert variables to JSON
		variablesJSON, err := json.Marshal(request.Variables)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process variables"})
			return
		}

		// Insert new template
		var templateID int
		query := `
			INSERT INTO email_templates (name, subject, body, template_type, is_default, is_active, variables, cc, bcc, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id`

		err = tx.QueryRow(query,
			request.Name, request.Subject, sanitizedBody, request.TemplateType,
			request.IsDefault, request.IsActive, variablesJSON, request.CC, request.BCC, session.UserID,
		).Scan(&templateID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template", "details": err.Error()})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		// Log activity
		log := models.ActivityLog{
			EventContext:     "Email Template",
			EventName:        "Create",
			Description:      fmt.Sprintf("Email template '%s' created successfully", request.Name),
			UserName:         userName,
			HostName:         session.HostName,
			IPAddress:        session.IPAddress,
			AffectedUserName: userName,
			CreatedAt:        time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			// Log error but don't fail the request
			fmt.Printf("Failed to log activity: %v\n", logErr)
		}

		// Get created template
		template, err := models.GetTemplateByID(db, templateID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Template created but failed to retrieve"})
			return
		}

		// Convert to response format
		var variables []models.EmailTemplateVariable
		if template.Variables != nil {
			json.Unmarshal(template.Variables, &variables)
		}

		response := models.EmailTemplateResponse{
			ID:           template.ID,
			Name:         template.Name,
			Subject:      template.Subject,
			Body:         template.Body,
			TemplateType: template.TemplateType,
			IsDefault:    template.IsDefault,
			IsActive:     template.IsActive,
			Variables:    variables,
			CC:           template.CC,
			BCC:          template.BCC,
			CreatedBy:    template.CreatedBy,
			CreatedAt:    template.CreatedAt,
			UpdatedAt:    template.UpdatedAt,
			UpdatedBy:    template.UpdatedBy,
		}

		c.JSON(http.StatusCreated, response)
	}
}

// GetEmailTemplates retrieves all email templates
// @Summary Get all email templates
// @Description Retrieve all active email templates
// @Tags Email Templates
// @Produce json
// @Success 200 {array} models.EmailTemplateResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/email-templates [get]
func GetEmailTemplates(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		templates, err := models.GetAllTemplates(db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve templates", "details": err.Error()})
			return
		}

		var responses []models.EmailTemplateResponse
		for _, template := range templates {
			var variables []models.EmailTemplateVariable
			if template.Variables != nil {
				json.Unmarshal(template.Variables, &variables)
			}

			response := models.EmailTemplateResponse{
				ID:           template.ID,
				Name:         template.Name,
				TemplateType: template.TemplateType,
			}
			responses = append(responses, response)
		}

		c.JSON(http.StatusOK, responses)
	}
}

// GetEmailTemplateByID retrieves a specific email template
// @Summary Get email template by ID
// @Description Retrieve a specific email template by its ID
// @Tags Email Templates
// @Produce json
// @Param id path int true "Template ID"
// @Success 200 {object} models.EmailTemplateResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/email-templates/{id} [get]
func GetEmailTemplateByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		template, err := models.GetTemplateByID(db, id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template", "details": err.Error()})
			}
			return
		}

		var variables []models.EmailTemplateVariable
		if template.Variables != nil {
			json.Unmarshal(template.Variables, &variables)
		}

		response := models.EmailTemplateResponse{
			ID:           template.ID,
			Name:         template.Name,
			Subject:      template.Subject,
			Body:         template.Body,
			TemplateType: template.TemplateType,
			IsDefault:    template.IsDefault,
			IsActive:     template.IsActive,
			Variables:    variables,
			CC:           template.CC,
			BCC:          template.BCC,
			CreatedBy:    template.CreatedBy,
			CreatedAt:    template.CreatedAt,
			UpdatedAt:    template.UpdatedAt,
			UpdatedBy:    template.UpdatedBy,
		}

		c.JSON(http.StatusOK, response)
	}
}

// sanitizeHTML cleans and validates HTML content from the frontend text editor
func sanitizeHTML(input string) string {
	// Parse the HTML
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		// If parsing fails, return the original input
		return input
	}

	// Define allowed HTML tags and attributes
	allowedTags := map[string]bool{
		"p": true, "br": true, "strong": true, "b": true, "em": true, "i": true,
		"u": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"ul": true, "ol": true, "li": true, "div": true, "span": true, "a": true,
		"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
		"blockquote": true, "code": true, "pre": true, "hr": true,
	}

	allowedAttributes := map[string]map[string]bool{
		"a":     {"href": true, "target": true, "title": true},
		"img":   {"src": true, "alt": true, "title": true, "width": true, "height": true},
		"table": {"border": true, "cellpadding": true, "cellspacing": true, "width": true},
		"td":    {"colspan": true, "rowspan": true, "width": true, "height": true},
		"th":    {"colspan": true, "rowspan": true, "width": true, "height": true},
	}

	// Create a new document to avoid modifying the original
	var newDoc html.Node
	newDoc.Type = html.DocumentNode

	var processNode func(*html.Node, *html.Node)
	processNode = func(src *html.Node, dst *html.Node) {
		for child := src.FirstChild; child != nil; child = child.NextSibling {
			switch child.Type {
			case html.TextNode:
				// Copy text nodes directly
				newText := &html.Node{
					Type: html.TextNode,
					Data: child.Data,
				}
				dst.AppendChild(newText)
			case html.ElementNode:
				// Check if tag is allowed
				if allowedTags[child.Data] {
					// Create new element node
					newElement := &html.Node{
						Type: html.ElementNode,
						Data: child.Data,
					}

					// Copy allowed attributes
					for _, attr := range child.Attr {
						if allowedAttributes[child.Data] != nil && allowedAttributes[child.Data][attr.Key] {
							newElement.Attr = append(newElement.Attr, attr)
						}
					}

					// Add to destination
					dst.AppendChild(newElement)

					// Process children recursively
					processNode(child, newElement)
				} else {
					// For disallowed tags, just process their children (content only)
					processNode(child, dst)
				}
			}
		}
	}

	// Process the document
	processNode(doc, &newDoc)

	// Convert back to string
	var buf strings.Builder
	err = html.Render(&buf, &newDoc)
	if err != nil {
		return input
	}

	result := buf.String()

	// Remove the <html><head></head><body> wrapper that html.Render adds
	if strings.HasPrefix(result, "<html>") {
		start := strings.Index(result, "<body>")
		end := strings.Index(result, "</body>")
		if start != -1 && end != -1 {
			result = result[start+6 : end]
		}
	}

	return strings.TrimSpace(result)
}

// UpdateEmailTemplate updates an existing email template
// @Summary Update email template
// @Description Update an existing email template
// @Tags Email Templates
// @Accept json
// @Produce json
// @Param id path int true "Template ID"
// @Param template body models.EmailTemplateRequest true "Updated email template data"
// @Success 200 {object} models.EmailTemplateResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/email-templates/{id} [put]
func UpdateEmailTemplate(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		var request models.EmailTemplateRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
			return
		}
		// Check if template exists
		_, err = models.GetTemplateByID(db, id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template"})
			}
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		defer tx.Rollback()

		// If this template is set as default, unset other defaults of the same type
		if request.IsDefault {
			_, err = tx.Exec("UPDATE email_templates SET is_default = false WHERE template_type = $1 AND id != $2",
				request.TemplateType, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update existing defaults"})
				return
			}
		}

		// Sanitize HTML body content from frontend text editor
		sanitizedBody := sanitizeHTML(request.Body)

		// Convert variables to JSON
		variablesJSON, err := json.Marshal(request.Variables)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process variables"})
			return
		}

		// Update template
		query := `
			UPDATE email_templates 
			SET name = $1, subject = $2, body = $3, template_type = $4, 
			    is_default = $5, is_active = $6, variables = $7, cc = $8, bcc = $9, updated_by = $10
			WHERE id = $11`

		result, err := tx.Exec(query,
			request.Name, request.Subject, sanitizedBody, request.TemplateType,
			request.IsDefault, request.IsActive, variablesJSON, pq.Array(request.CC), pq.Array(request.BCC), session.UserID, id)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update template", "details": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		// Log activity
		log := models.ActivityLog{
			EventContext:     "Email Template",
			EventName:        "Update",
			Description:      fmt.Sprintf("Email template '%s' updated successfully", request.Name),
			UserName:         userName,
			HostName:         session.HostName,
			IPAddress:        session.IPAddress,
			AffectedUserName: userName,
			CreatedAt:        time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			fmt.Printf("Failed to log activity: %v\n", logErr)
		}

		// Get updated template
		template, err := models.GetTemplateByID(db, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Template updated but failed to retrieve"})
			return
		}

		var variables []models.EmailTemplateVariable
		if template.Variables != nil {
			json.Unmarshal(template.Variables, &variables)
		}

		response := models.EmailTemplateResponse{
			ID:           template.ID,
			Name:         template.Name,
			Subject:      template.Subject,
			Body:         template.Body,
			TemplateType: template.TemplateType,
			IsDefault:    template.IsDefault,
			IsActive:     template.IsActive,
			Variables:    variables,
			CC:           template.CC,
			BCC:          template.BCC,
			CreatedBy:    template.CreatedBy,
			CreatedAt:    template.CreatedAt,
			UpdatedAt:    template.UpdatedAt,
			UpdatedBy:    template.UpdatedBy,
		}

		c.JSON(http.StatusOK, response)
	}
}

// DeleteEmailTemplate deletes an email template
// @Summary Delete email template
// @Description Delete an email template (soft delete)
// @Tags Email Templates
// @Produce json
// @Param id path int true "Template ID"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/email-templates/{id} [delete]
func DeleteEmailTemplate(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
			return
		}

		// Check if template exists and get its name
		existingTemplate, err := models.GetTemplateByID(db, id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template"})
			}
			return
		}

		// Check if it's a default template
		if existingTemplate.IsDefault {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete default template. Please set another template as default first."})
			return
		}

		// Soft delete the template
		result, err := db.Exec("UPDATE email_templates SET is_active = false WHERE id = $1", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete template", "details": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			return
		}

		// Log activity
		log := models.ActivityLog{
			EventContext:     "Email Template",
			EventName:        "Delete",
			Description:      fmt.Sprintf("Email template '%s' deleted successfully", existingTemplate.Name),
			UserName:         userName,
			HostName:         session.HostName,
			IPAddress:        session.IPAddress,
			AffectedUserName: userName,
			CreatedAt:        time.Now(),
		}

		if logErr := SaveActivityLog(db, log); logErr != nil {
			fmt.Printf("Failed to log activity: %v\n", logErr)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Template deleted successfully"})
	}
}

// GetTemplatesByType retrieves templates by type
// @Summary Get templates by type
// @Description Retrieve all templates of a specific type
// @Tags Email Templates
// @Produce json
// @Param type path string true "Template type"
// @Success 200 {array} models.EmailTemplateResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/email-templates/type/{type} [get]
func GetTemplatesByType(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract session ID from headers
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is missing"})
			return
		}

		// Fetch session details
		_, _, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		templateType := c.Param("type")

		// Validate template type
		validTypes := []string{"welcome_client", "welcome_admin", "password_reset", "project_invitation", "notification"}
		isValidType := false
		for _, t := range validTypes {
			if templateType == t {
				isValidType = true
				break
			}
		}
		if !isValidType {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template type", "valid_types": validTypes})
			return
		}

		templates, err := models.GetTemplatesByType(db, templateType)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve templates", "details": err.Error()})
			return
		}

		var responses []models.EmailTemplateResponse
		for _, template := range templates {
			var variables []models.EmailTemplateVariable
			if template.Variables != nil {
				json.Unmarshal(template.Variables, &variables)
			}

			response := models.EmailTemplateResponse{
				ID:           template.ID,
				Name:         template.Name,
				Subject:      template.Subject,
				Body:         template.Body,
				TemplateType: template.TemplateType,
				IsDefault:    template.IsDefault,
				IsActive:     template.IsActive,
				Variables:    variables,
				CreatedBy:    template.CreatedBy,
				CreatedAt:    template.CreatedAt,
				UpdatedAt:    template.UpdatedAt,
				UpdatedBy:    template.UpdatedBy,
			}
			responses = append(responses, response)
		}

		c.JSON(http.StatusOK, responses)
	}
}

package models

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

// EmailTemplate represents the email_templates table
type EmailTemplate struct {
	ID           int             `json:"id" example:"1"`
	Name         string          `json:"name" example:"Welcome Email"`
	Subject      string          `json:"subject" example:"Welcome to the project"`
	Body         string          `json:"body" example:"Hello {{user_name}}"`
	TemplateType string          `json:"template_type" example:"welcome"`
	IsDefault    bool            `json:"is_default" example:"false"`
	IsActive     bool            `json:"is_active" example:"true"`
	Variables    json.RawMessage `json:"variables"`
	CC           []string        `json:"cc,omitempty"`
	BCC          []string        `json:"bcc,omitempty"`
	CreatedBy    *int            `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt    time.Time       `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	UpdatedBy    *int            `json:"updated_by"`
}

// EmailTemplateVariable represents a single variable in the template
type EmailTemplateVariable struct {
	Key         string `json:"key" example:"user_name"`
	Description string `json:"description" example:"Name of the user"`
}

// EmailTemplateRequest represents the request structure for creating/updating templates
type EmailTemplateRequest struct {
	Name         string                  `json:"name" binding:"required" example:"Welcome Email"`
	Subject      string                  `json:"subject" binding:"required" example:"Welcome"`
	Body         string                  `json:"body" binding:"required" example:"Hello {{user_name}}"`
	TemplateType string                  `json:"template_type" binding:"required" example:"welcome"`
	IsDefault    bool                    `json:"is_default" example:"false"`
	IsActive     bool                    `json:"is_active" example:"true"`
	Variables    []EmailTemplateVariable `json:"variables"`
	CC           []string                `json:"cc"`
	BCC          []string                `json:"bcc"`
}

// EmailTemplateResponse represents the response structure for templates
type EmailTemplateResponse struct {
	ID           int                     `json:"id"`
	Name         string                  `json:"name"`
	Subject      string                  `json:"subject"`
	Body         string                  `json:"body"`
	CC           []string                `json:"cc"`
	BCC          []string                `json:"bcc"`
	TemplateType string                  `json:"template_type"`
	IsDefault    bool                    `json:"is_default"`
	IsActive     bool                    `json:"is_active"`
	Variables    []EmailTemplateVariable `json:"variables"`
	CreatedBy    *int                    `json:"created_by"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	UpdatedBy    *int                    `json:"updated_by"`
}

// EmailData represents the data structure for email sending with template variables
type EmailData struct {
	ProjectName  string `json:"project_name"`
	ClientName   string `json:"client_name"`
	AdminName    string `json:"admin_name"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	Organization string `json:"organization"`
	ProjectID    string `json:"project_id"`
	UserName     string `json:"user_name"`
	CompanyName  string `json:"company_name"`
	LoginURL     string `json:"login_url"`
	SupportEmail string `json:"support_email"`
}

// TemplateVariableMap represents a map of template variables for easy lookup
type TemplateVariableMap map[string]string

// GetDefaultTemplate retrieves the default template for a given type
func GetDefaultTemplate(db *sql.DB, templateType string) (*EmailTemplate, error) {
	var template EmailTemplate
	query := `
		SELECT id, name, subject, body, template_type, is_default, is_active, 
		       variables, cc, bcc, created_by, created_at, updated_at, updated_by
		FROM email_templates 
		WHERE template_type = $1 AND is_default = true AND is_active = true
		ORDER BY created_at DESC 
		LIMIT 1`

	err := db.QueryRow(query, templateType).Scan(
		&template.ID, &template.Name, &template.Subject, &template.Body,
		&template.TemplateType, &template.IsDefault, &template.IsActive,
		&template.Variables, &template.CC, &template.BCC, &template.CreatedBy, &template.CreatedAt,
		&template.UpdatedAt, &template.UpdatedBy,
	)

	if err != nil {
		return nil, err
	}

	return &template, nil
}

// GetTemplateByID retrieves a template by its ID
func GetTemplateByID(db *sql.DB, id int) (*EmailTemplate, error) {
	var template EmailTemplate
	query := `
		SELECT id, name, subject, body, template_type, is_default, is_active, 
		       variables, cc, bcc, created_by, created_at, updated_at, updated_by
		FROM email_templates 
		WHERE id = $1 AND is_active = true`

	var cc, bcc pq.StringArray
	var variables sql.NullString

	err := db.QueryRow(query, id).Scan(
		&template.ID, &template.Name, &template.Subject, &template.Body,
		&template.TemplateType, &template.IsDefault, &template.IsActive,
		&variables, &cc, &bcc, &template.CreatedBy, &template.CreatedAt,
		&template.UpdatedAt, &template.UpdatedBy,
	)

	// Assign cc/bcc properly
	template.CC = []string(cc)
	template.BCC = []string(bcc)

	// Handle Variables conversion
	if variables.Valid {
		template.Variables = json.RawMessage(variables.String)
	}

	if err != nil {
		return nil, err
	}

	return &template, nil
}

// GetAllTemplates retrieves all active templates
func GetAllTemplates(db *sql.DB) ([]EmailTemplate, error) {
	query := `
		SELECT id, name, subject, body, template_type, is_default, is_active, 
		       variables, cc, bcc, created_by, created_at, updated_at, updated_by
		FROM email_templates 
		WHERE is_active = true
		ORDER BY template_type, name`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []EmailTemplate
	for rows.Next() {
		var template EmailTemplate

		var cc, bcc sql.NullString
		var variables sql.NullString

		err := rows.Scan(
			&template.ID, &template.Name, &template.Subject, &template.Body,
			&template.TemplateType, &template.IsDefault, &template.IsActive,
			&variables, &cc, &bcc, &template.CreatedBy, &template.CreatedAt,
			&template.UpdatedAt, &template.UpdatedBy,
		)
		if err != nil {
			return nil, err
		}

		if cc.Valid {
			// If stored as JSON array like {"a","b"}
			var arr []string
			if err := json.Unmarshal([]byte(cc.String), &arr); err == nil {
				template.CC = arr
			}
		}

		if bcc.Valid {
			var arr []string
			if err := json.Unmarshal([]byte(bcc.String), &arr); err == nil {
				template.BCC = arr
			}
		}

		// Handle Variables conversion
		if variables.Valid {
			template.Variables = json.RawMessage(variables.String)
		}
		templates = append(templates, template)
	}

	return templates, nil
}

// GetTemplatesByType retrieves all templates of a specific type
func GetTemplatesByType(db *sql.DB, templateType string) ([]EmailTemplate, error) {
	query := `
		SELECT id, name, subject, body, template_type, is_default, is_active, 
		       variables, cc, bcc, created_by, created_at, updated_at, updated_by
		FROM email_templates 
		WHERE template_type = $1 AND is_active = true
		ORDER BY is_default DESC, name`

	rows, err := db.Query(query, templateType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []EmailTemplate
	for rows.Next() {
		var template EmailTemplate
		err := rows.Scan(
			&template.ID, &template.Name, &template.Subject, &template.Body,
			&template.TemplateType, &template.IsDefault, &template.IsActive,
			&template.Variables, &template.CC, &template.BCC, &template.CreatedBy, &template.CreatedAt,
			&template.UpdatedAt, &template.UpdatedBy,
		)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}

	return templates, nil
}

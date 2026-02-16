package services

import (
	"database/sql"
	"fmt"
	"net/smtp"
	"regexp"
	"strings"

	"backend/models" // Replace with your actual project path

	"golang.org/x/net/html"
)

// convertHTMLToText converts HTML content to plain text for email sending
func convertHTMLToText(htmlContent string) string {
	// Parse the HTML
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		// If parsing fails, return the original content
		return htmlContent
	}

	var text strings.Builder
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			text.WriteString(n.Data)
		case html.ElementNode:
			// Add line breaks for block elements
			switch n.Data {
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6":
				text.WriteString("\n")
			case "li":
				text.WriteString("• ")
			case "table":
				text.WriteString("\n")
			case "tr":
				text.WriteString("\n")
			case "td", "th":
				text.WriteString(" | ")
			}
		}

		// Recursively process child nodes
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			extractText(child)
		}
	}

	extractText(doc)

	result := text.String()

	// Clean up the text
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n") // Remove excessive line breaks
	result = strings.TrimSpace(result)                    // Remove leading/trailing whitespace

	return result
}

// PreviewEmailAsText converts HTML template to plain text for preview purposes
// This can be used by the frontend to show users how their HTML will appear in emails
func (es *EmailService) PreviewEmailAsText(htmlContent string, emailData models.EmailData) (string, error) {
	// First process the template with variables
	processedContent, err := es.processTemplate(htmlContent, emailData)
	if err != nil {
		return "", fmt.Errorf("failed to process template: %v", err)
	}

	// Then convert to plain text
	plainText := convertHTMLToText(processedContent)
	return plainText, nil
}

// EmailService handles email operations with template support
type EmailService struct {
	db *sql.DB
}

// NewEmailService creates a new email service instance
func NewEmailService(db *sql.DB) *EmailService {
	return &EmailService{db: db}
}

// SendTemplatedEmail sends an email using a template with variable substitution
func (es *EmailService) SendTemplatedEmail(templateType string, emailData models.EmailData, customTemplateID *int) error {
	var emailTemplate *models.EmailTemplate
	var err error

	// Template selection logic:
	// 1. If custom template ID is provided, use that specific template
	// 2. Otherwise, automatically use the default template for the type
	if customTemplateID != nil {
		emailTemplate, err = models.GetTemplateByID(es.db, *customTemplateID)
		if err != nil {
			return fmt.Errorf("failed to get custom template (ID: %d): %v", *customTemplateID, err)
		}
		// Verify the template is of the correct type
		if emailTemplate.TemplateType != templateType {
			return fmt.Errorf("custom template type mismatch: expected %s, got %s", templateType, emailTemplate.TemplateType)
		}
	} else {
		// Automatically use the default template for the type
		emailTemplate, err = models.GetDefaultTemplate(es.db, templateType)
		if err != nil {
			return fmt.Errorf("failed to get default template for type '%s': %v", templateType, err)
		}
	}

	// Process the template with variables
	subject, err := es.processTemplate(emailTemplate.Subject, emailData)
	if err != nil {
		return fmt.Errorf("failed to process subject template: %v", err)
	}

	body, err := es.processTemplate(emailTemplate.Body, emailData)
	if err != nil {
		return fmt.Errorf("failed to process body template: %v", err)
	}

	// Convert HTML body to plain text for email sending
	plainTextBody := convertHTMLToText(body)

	// Send the email
	return es.sendEmail(emailData.Email, subject, plainTextBody, emailTemplate.CC, emailTemplate.BCC)
}

// processTemplate processes a template string with variable substitution
func (es *EmailService) processTemplate(templateStr string, data models.EmailData) (string, error) {
	// Create a map of variables for template processing
	variables := map[string]string{
		"project_name":  data.ProjectName,
		"client_name":   data.ClientName,
		"admin_name":    data.AdminName,
		"email":         data.Email,
		"password":      data.Password,
		"role":          data.Role,
		"organization":  data.Organization,
		"project_id":    data.ProjectID,
		"user_name":     data.UserName,
		"company_name":  data.CompanyName,
		"login_url":     data.LoginURL,
		"support_email": data.SupportEmail,
	}

	// Replace variables in the template
	result := templateStr
	for key, value := range variables {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}

	return result, nil
}

// sendEmail sends an email using SMTP with optional CC and BCC
func (es *EmailService) sendEmail(to, subject, body string, cc, bcc []string) error {
	auth := smtp.PlainAuth(
		"",
		"om.s@blueinvent.com",
		"gloycbfukxdyeczj",
		"smtp.gmail.com",
	)

	from := "vasug7409@gmail.com"
	toList := []string{to}

	// Add CC recipients if provided
	if len(cc) > 0 {
		toList = append(toList, cc...)
	}

	// Add BCC recipients if provided
	if len(bcc) > 0 {
		toList = append(toList, bcc...)
	}

	// Build email headers
	headers := []string{
		"From: " + from,
		"To: " + to,
	}

	// Add CC header if CC recipients exist
	if len(cc) > 0 {
		headers = append(headers, "Cc: "+strings.Join(cc, ", "))
	}

	headers = append(headers,
		"Subject: "+subject,
		"",
		body,
	)

	msg := []byte(strings.Join(headers, "\r\n") + "\r\n")

	err := smtp.SendMail(
		"smtp.gmail.com:587",
		auth,
		from,
		toList,
		msg,
	)

	return err
}

// SendWelcomeClientEmail sends a welcome email to a new client
func (es *EmailService) SendWelcomeClientEmail(user models.User, organization string, customTemplateID *int) error {

	// You can add logic here to get project details from the database
	// For now, using default values

	emailData := models.EmailData{
		ClientName:   user.FirstName + " " + user.LastName,
		Email:        user.Email,
		Password:     user.Password,
		Role:         "Client",
		CompanyName:  organization,
		LoginURL:     "https://precastezy.blueinvent.com/login",
		SupportEmail: "support@blueinvent.com",
	}

	return es.SendTemplatedEmail("welcome client", emailData, customTemplateID)
}

// SendWelcomeAdminEmail sends a welcome email to a new admin
func (es *EmailService) SendWelcomeAdminEmail(user models.User, customTemplateID *int) error {
	emailData := models.EmailData{
		ProjectName:  "Our Platform",
		AdminName:    user.FirstName + " " + user.LastName,
		Email:        user.Email,
		Password:     user.Password,
		Role:         "Admin",
		CompanyName:  "Your Company",
		LoginURL:     "https://precastezy.blueinvent.com/login",
		SupportEmail: "support@blueinvent.com",
	}

	return es.SendTemplatedEmail("welcome_user", emailData, customTemplateID)
}

// ValidateTemplate validates a template string for syntax errors
func (es *EmailService) ValidateTemplate(templateStr string) error {
	// Check for unmatched braces
	openBraces := strings.Count(templateStr, "{{")
	closeBraces := strings.Count(templateStr, "}}")

	if openBraces != closeBraces {
		return fmt.Errorf("unmatched braces in template")
	}

	// Check for valid variable syntax
	re := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	matches := re.FindAllStringSubmatch(templateStr, -1)

	validVariables := map[string]bool{
		"project_name":  true,
		"client_name":   true,
		"admin_name":    true,
		"email":         true,
		"password":      true,
		"role":          true,
		"organization":  true,
		"project_id":    true,
		"user_name":     true,
		"company_name":  true,
		"login_url":     true,
		"support_email": true,
	}

	for _, match := range matches {
		if len(match) > 1 {
			variable := strings.TrimSpace(match[1])
			if !validVariables[variable] {
				return fmt.Errorf("invalid variable: %s", variable)
			}
		}
	}

	return nil
}

// GetAvailableVariables returns a list of available template variables
func (es *EmailService) GetAvailableVariables() []models.EmailTemplateVariable {
	return []models.EmailTemplateVariable{
		{Key: "project_name", Description: "Project name"},
		{Key: "client_name", Description: "Client full name"},
		{Key: "admin_name", Description: "Admin full name"},
		{Key: "email", Description: "User email"},
		{Key: "password", Description: "User password"},
		{Key: "role", Description: "User role"},
		{Key: "organization", Description: "Client organization"},
		{Key: "project_id", Description: "Project ID"},
		{Key: "user_name", Description: "User name"},
		{Key: "company_name", Description: "Company name"},
		{Key: "login_url", Description: "Login URL"},
		{Key: "support_email", Description: "Support email"},
	}
}

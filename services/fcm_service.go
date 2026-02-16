package services

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

// FCMService handles Firebase Cloud Messaging operations using HTTP v1 API
type FCMService struct {
	projectID   string
	credentials *jwt.Config
	db          *sql.DB
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
}

// ServiceAccountCredentials represents the structure of Firebase service account JSON
type ServiceAccountCredentials struct {
	Type                string `json:"type"`
	ProjectID           string `json:"project_id"`
	PrivateKeyID        string `json:"private_key_id"`
	PrivateKey          string `json:"private_key"`
	ClientEmail         string `json:"client_email"`
	ClientID            string `json:"client_id"`
	AuthURI             string `json:"auth_uri"`
	TokenURI            string `json:"token_uri"`
	AuthProviderCertURL string `json:"auth_provider_x509_cert_url"`
	ClientCertURL       string `json:"client_x509_cert_url"`
}

// NewFCMService initializes and returns a new FCM service using HTTP v1 API
// credentialsPath: Path to Firebase service account JSON file (e.g., "firebase-credentials.json")
func NewFCMService(credentialsPath string, db *sql.DB) (*FCMService, error) {
	if credentialsPath == "" {
		return nil, fmt.Errorf("credentials path is required")
	}

	// Read and parse service account credentials
	data, err := readFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("error reading credentials file: %v", err)
	}

	var creds ServiceAccountCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("error parsing credentials: %v", err)
	}

	// Parse private key to validate it
	_, err = parsePrivateKey(creds.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %v", err)
	}

	// Create JWT config for OAuth2
	// jwt.Config expects the private key as []byte (PEM format)
	// Convert escaped newlines to actual newlines
	privateKeyStr := strings.ReplaceAll(creds.PrivateKey, "\\n", "\n")
	privateKeyBytes := []byte(privateKeyStr)

	config := &jwt.Config{
		Email:      creds.ClientEmail,
		PrivateKey: privateKeyBytes,
		Scopes:     []string{"https://www.googleapis.com/auth/firebase.messaging"},
		TokenURL:   creds.TokenURI,
	}

	// Create token source
	tokenSource := config.TokenSource(context.Background())

	return &FCMService{
		projectID:   creds.ProjectID,
		credentials: config,
		db:          db,
		httpClient:  &http.Client{},
		tokenSource: tokenSource,
	}, nil
}

// readFile reads a file and returns its contents
func readFile(path string) ([]byte, error) {
	// Try to read from file system first
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	// If file not found, try reading from environment variable or embedded data
	return nil, fmt.Errorf("file not found: %s", path)
}

// parsePrivateKey parses a PEM-encoded private key
func parsePrivateKey(keyData string) (*rsa.PrivateKey, error) {
	// Remove newlines and BEGIN/END markers if present
	keyData = strings.ReplaceAll(keyData, "\\n", "\n")
	keyData = strings.TrimSpace(keyData)

	block, _ := pem.Decode([]byte(keyData))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS1 format
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %v", err)
		}
	}

	rsaKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA private key")
	}

	return rsaKey, nil
}

// SendNotification sends a push notification to a single FCM token using HTTP v1 API
func (f *FCMService) SendNotification(ctx context.Context, token, title, body string, data map[string]string) error {
	if token == "" {
		return fmt.Errorf("FCM token cannot be empty")
	}

	// Get OAuth token
	oauthToken, err := f.tokenSource.Token()
	if err != nil {
		log.Printf("Error getting OAuth token: %v", err)
		return fmt.Errorf("error getting OAuth token: %v", err)
	}

	log.Printf("OAuth token obtained successfully, sending notification to token: %s...", token[:min(20, len(token))])

	// Prepare FCM HTTP v1 message payload
	// For web notifications, we need both notification and data fields
	message := map[string]interface{}{
		"message": map[string]interface{}{
			"token": token,
			"notification": map[string]string{
				"title": title,
				"body":  body,
			},
			"data": convertDataMap(data),
			"android": map[string]interface{}{
				"priority": "high",
				"notification": map[string]interface{}{
					"sound":      "default",
					"channel_id": "default",
				},
			},
			"apns": map[string]interface{}{
				"headers": map[string]string{
					"apns-priority": "10",
				},
				"payload": map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]string{
							"title": title,
							"body":  body,
						},
						"sound": "default",
					},
				},
			},
			"webpush": map[string]interface{}{
				"notification": map[string]interface{}{
					"title": title,
					"body":  body,
					"icon":  "/firebase-logo.png", // Optional: path to your icon
					"badge": "/firebase-logo.png", // Optional: path to your badge
				},
				"fcm_options": map[string]interface{}{
					"link": data["action"], // Optional: link to open when notification is clicked
				},
			},
		},
	}

	// Use HTTP v1 endpoint
	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", f.projectID)
	log.Printf("FCM HTTP v1 endpoint: %s", endpoint)
	log.Printf("FCM message payload: %+v", message)
	return f.sendHTTPv1Request(ctx, endpoint, oauthToken.AccessToken, message)
}

// SendMulticastNotification sends push notifications to multiple FCM tokens
func (f *FCMService) SendMulticastNotification(ctx context.Context, tokens []string, title, body string, data map[string]string) error {
	if len(tokens) == 0 {
		return nil
	}

	// Filter out empty tokens
	validTokens := []string{}
	for _, token := range tokens {
		if strings.TrimSpace(token) != "" {
			validTokens = append(validTokens, token)
		}
	}

	if len(validTokens) == 0 {
		return nil
	}

	// FCM legacy API supports up to 1000 tokens per request
	// For simplicity, we'll send individual requests for each token
	// For better performance with many tokens, consider batching
	failureCount := 0
	for _, token := range validTokens {
		err := f.SendNotification(ctx, token, title, body, data)
		if err != nil {
			failureCount++
			log.Printf("Failed to send to token %s: %v", token, err)
		}
	}

	if failureCount > 0 {
		log.Printf("Failed to send %d notifications out of %d", failureCount, len(validTokens))
	}

	return nil
}

// SendNotificationToUser sends a push notification to a user by their user ID
func (f *FCMService) SendNotificationToUser(ctx context.Context, userID int, title, body string, data map[string]string) error {
	var fcmToken string
	err := f.db.QueryRow(`SELECT fcm_token FROM users WHERE id = $1 AND fcm_token IS NOT NULL AND fcm_token != ''`, userID).Scan(&fcmToken)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("No FCM token found for user %d", userID)
			return nil // Not an error, user just doesn't have a token
		}
		return fmt.Errorf("error fetching FCM token for user %d: %v", userID, err)
	}

	if fcmToken == "" {
		log.Printf("FCM token is empty for user %d", userID)
		return nil
	}

	log.Printf("Sending FCM notification to user %d with token: %s...", userID, fcmToken[:min(20, len(fcmToken))])
	return f.SendNotification(ctx, fcmToken, title, body, data)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SendNotificationToUsers sends push notifications to multiple users by their user IDs
func (f *FCMService) SendNotificationToUsers(ctx context.Context, userIDs []int, title, body string, data map[string]string) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Build query to get all FCM tokens
	placeholders := make([]string, len(userIDs))
	args := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`SELECT fcm_token FROM users WHERE id IN (%s) AND fcm_token IS NOT NULL AND fcm_token != ''`, strings.Join(placeholders, ","))
	rows, err := f.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("error fetching FCM tokens: %v", err)
	}
	defer rows.Close()

	tokens := []string{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			log.Printf("Error scanning FCM token: %v", err)
			continue
		}
		if token != "" {
			tokens = append(tokens, token)
		}
	}

	if len(tokens) == 0 {
		return nil
	}

	return f.SendMulticastNotification(ctx, tokens, title, body, data)
}

// SaveFCMToken saves or updates the FCM token for a user
func (f *FCMService) SaveFCMToken(userID int, token string) error {
	_, err := f.db.Exec(`UPDATE users SET fcm_token = $1 WHERE id = $2`, token, userID)
	if err != nil {
		return fmt.Errorf("error saving FCM token: %v", err)
	}
	return nil
}

// RemoveFCMToken removes the FCM token for a user
func (f *FCMService) RemoveFCMToken(userID int) error {
	_, err := f.db.Exec(`UPDATE users SET fcm_token = NULL WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("error removing FCM token: %v", err)
	}
	return nil
}

// sendHTTPv1Request sends an HTTP POST request to FCM HTTP v1 API
func (f *FCMService) sendHTTPv1Request(ctx context.Context, endpoint, accessToken string, payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling payload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			log.Printf("FCM API error (status %d): %v", resp.StatusCode, errorResp)
			return fmt.Errorf("FCM API error (status %d): %v", resp.StatusCode, errorResp)
		}
		bodyBytes := make([]byte, 1024)
		n, _ := resp.Body.Read(bodyBytes)
		log.Printf("FCM API error: status code %d, body: %s", resp.StatusCode, string(bodyBytes[:n]))
		return fmt.Errorf("FCM API error: status code %d, body: %s", resp.StatusCode, string(bodyBytes[:n]))
	}

	// HTTP v1 API returns success response
	var fcmResponse struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&fcmResponse); err != nil {
		// Response might be empty on success, which is fine
		log.Printf("Note: Could not decode FCM response (may be empty): %v", err)
	} else {
		log.Printf("FCM notification sent successfully. Response name: %s", fcmResponse.Name)
	}

	return nil
}

// convertDataMap converts map[string]string to map[string]string for FCM data payload
// All values must be strings in FCM data
func convertDataMap(data map[string]string) map[string]string {
	if data == nil {
		return make(map[string]string)
	}
	result := make(map[string]string)
	for k, v := range data {
		result[k] = v
	}
	return result
}

// SendNotificationWithDB sends a notification and also saves it to the database
func (f *FCMService) SendNotificationWithDB(ctx context.Context, userID int, title, body string, data map[string]string, action string) error {
	// Send push notification
	err := f.SendNotificationToUser(ctx, userID, title, body, data)
	if err != nil {
		log.Printf("Error sending push notification to user %d: %v", userID, err)
		// Continue to save notification in DB even if push fails
	}

	// Save notification to database
	_, err = f.db.Exec(`
		INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
		VALUES ($1, $2, 'unread', $3, NOW(), NOW())
	`, userID, body, action)
	if err != nil {
		return fmt.Errorf("error saving notification to database: %v", err)
	}

	return nil
}

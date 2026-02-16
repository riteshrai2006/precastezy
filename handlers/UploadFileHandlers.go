package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mime/multipart"

	"github.com/gin-gonic/gin"
)

const imageDir = "/var/www/dataprecast/"

// ServeFile godoc
// @Summary      Serve file
// @Description  Serve a file by name from query param ?file=filename
// @Tags         upload
// @Produce      application/octet-stream
// @Param        file  query     string  true  "File name (path relative to storage)"
// @Success      200   {file}   file    "File content"
// @Failure      400   {object}  object
// @Failure      403   {object}  object
// @Failure      404   {object}  object
// @Failure      500   {object}  object
// @Router       /api/get-file [get]
func ServeFile(c *gin.Context) {
	// Get the file name from the query parameter
	fileName := c.Query("file") // Use ?file=filename in the query
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file parameter is required"})
		return
	}

	// Secure the file path to prevent directory traversal attacks
	cleanFileName := filepath.Clean(fileName)
	if cleanFileName != fileName || strings.Contains(cleanFileName, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return
	}

	// Construct full file path and ensure it's within imageDir
	absoluteImageDir, err := filepath.Abs(imageDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}

	filePath := filepath.Join(absoluteImageDir, cleanFileName)
	if !strings.HasPrefix(filePath, absoluteImageDir+string(os.PathSeparator)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Check if the file exists and is not a directory
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	// Open the file to determine its MIME type
	file, err := os.Open(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}
	defer file.Close()

	// Read a small portion of the file to detect its MIME type
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}

	// Detect the content type
	contentType := http.DetectContentType(buffer)
	c.Writer.Header().Set("Content-Type", contentType)

	// Serve the file
	c.File(filePath)
}

// UploadFile godoc
// @Summary      Upload file
// @Description  Upload a file (multipart form, field name: file)
// @Tags         upload
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file    true  "File to upload"
// @Success      200   {object}  object  "message, file path, etc."
// @Failure      400   {object}  object
// @Failure      500   {object}  object
// @Router       /api/upload [post]
func UploadFile(c *gin.Context) {
	// Get the uploaded file (accept any file type)
	file, handler, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Error retrieving the file",
		})
		return
	}
	defer file.Close()

	// Validate and sanitize the file name
	filename := filepath.Base(handler.Filename)
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid file name",
		})
		return
	}

	// No file size restrictions - nginx configured to allow large uploads

	// Ensure the directory exists
	if _, err := os.Stat(imageDir); os.IsNotExist(err) {
		if err := os.MkdirAll(imageDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Unable to create directory",
			})
			return
		}
	}

	// Create a unique file name
	uniqueName := fmt.Sprintf("%d-%s", time.Now().Unix(), filename)
	dstPath := filepath.Join(imageDir, uniqueName)

	// Attempt to create the file
	dst, err := os.Create(dstPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Unable to create the file",
			"details": err.Error(),
			"path":    dstPath,
		})
		return
	}
	defer dst.Close()

	// Copy the file
	if _, err := io.Copy(dst, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to save the file",
		})
		return
	}

	// Success response
	c.JSON(http.StatusOK, gin.H{
		"message":   "File uploaded successfully",
		"file_name": uniqueName,
		"file_path": dstPath,
		"file_size": handler.Size,
		"file_type": handler.Header.Get("Content-Type"),
	})
}

// UploadFileToDirectory is a utility function that uploads a file to a specified directory
// and returns the file path. This can be used by other handlers like import handlers.
func UploadFileToDirectory(file *multipart.FileHeader, uploadDir string, maxSize int64) (string, error) {
	// Validate and sanitize the file name
	filename := filepath.Base(file.Filename)
	if filename == "" {
		return "", fmt.Errorf("invalid file name")
	}

	// Check file size
	if maxSize > 0 && file.Size > maxSize {
		return "", fmt.Errorf("file size exceeds the allowed limit")
	}

	// Ensure the directory exists with proper permissions
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", fmt.Errorf("unable to create directory %s: %w", uploadDir, err)
	}

	// Create a unique file name
	uniqueName := fmt.Sprintf("%d-%s", time.Now().Unix(), filename)
	dstPath := filepath.Join(uploadDir, uniqueName)

	// Open the source file
	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// Attempt to create the file
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("unable to create the file: %w", err)
	}
	defer dst.Close()

	// Copy the file
	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("unable to save the file: %w", err)
	}

	return dstPath, nil
}

// CleanupFile removes a file from the server
// func CleanupFile(filePath string) error {
// 	if filePath == "" {
// 		return nil
// 	}

// 	if err := os.Remove(filePath); err != nil {
// 		return fmt.Errorf("failed to remove file: %w", err)
// 	}

// 	return nil
// }

// FileExists checks if a file exists at the given path
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// EnsureDirectoryExists creates a directory with proper permissions if it doesn't exist
func EnsureDirectoryExists(dirPath string) error {
	// Check if directory exists
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		// Create directory with proper permissions
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
		}
	}

	// Ensure directory is writable
	if err := os.Chmod(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to set directory permissions for %s: %w", dirPath, err)
	}

	// Test if directory is actually writable by creating a test file
	testFile := filepath.Join(dirPath, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("directory %s is not writable: %w", dirPath, err)
	}
	// Clean up test file
	os.Remove(testFile)

	return nil
}

/*func UploadFile(c *gin.Context) {
	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		fmt.Println("Error retrieving file:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to upload file: " + err.Error()})
		return
	}

	// Get the user-specified upload location from the form
	userLocation := c.PostForm("upload_location")

	// Predefined valid upload locations
	validLocations := map[string]string{
		"drawing":        "/var/www/html/precast/files/drawing",
		"user_profile":   "/var/www/html/precast/files/user_profile/",
		"project_logo":   "/var/www/html/precast/files/project_logo/",
		"client_profile": "/var/www/html/precast/files/client_profile/",
		"element":        "/var/www/html/precast/files/element/",
		"task":           "/var/www/html/precast/files/task/",
		// Add more valid locations as needed
	}

	// Check if the provided location is valid
	uploadPath, ok := validLocations[userLocation]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid upload location"})
		return
	}

	// Generate a unique filename using the current timestamp
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(file.Filename))

	// Ensure the directory exists
	if err := ensureDirExists(uploadPath); err != nil {
		fmt.Println("Error ensuring directory exists:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory: " + err.Error()})
		return
	}

	// Construct the full file path for the current upload path
	filePath := filepath.Join(uploadPath, filename)

	// Save the uploaded file to the specified location
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		fmt.Println("Error saving file:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	// After uploading, return the URL from the specified location
	baseURL := fmt.Sprintf("http://13.60.216.91:9000/precast/files/%s/", userLocation)
	fileURL := fmt.Sprintf("%s%s", baseURL, filename)
	c.JSON(http.StatusOK, gin.H{"url": fileURL})
}

// Function to ensure the directory exists, creating it if necessary
func ensureDirExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			fmt.Println("Error creating directory:", err)
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	return nil
}
func DeleteFile(c *gin.Context) {
	// Get the file name or path from the request (e.g., passed as a parameter or form field)
	fileName := c.Query("file_name") // assuming file name is provided as a query parameter

	// Predefined valid upload locations
	validLocations := map[string]string{
		"drawing":        "/var/www/html/precast/files/drawing/",
		"user_profile":   "/var/www/html/precast/files/user_profile/",
		"project_logo":   "/var/www/html/precast/files/project_logo/",
		"client_profile": "/var/www/html/precast/files/client_profile/",
		"element":        "/var/www/html/precast/files/element/",
		// Add more valid locations as needed
	}

	// Get the user-specified upload location from the request (e.g., passed as a query parameter)
	userLocation := c.Query("upload_location")
	uploadPath, ok := validLocations[userLocation]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid upload location"})
		return
	}

	// Construct the full file path
	filePath := filepath.Join(uploadPath, fileName)

	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// Attempt to delete the file
	if err := os.Remove(filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete file: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "File deleted successfully"})
}

func UpdateFile(c *gin.Context) {
	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		fmt.Println("Error retrieving file:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to upload file: " + err.Error()})
		return
	}

	// Get the user-specified upload location from the form
	userLocation := c.PostForm("upload_location")

	// Get the old file path from the request
	oldFilePath := c.PostForm("old_file_path")

	// Predefined valid upload locations
	validLocations := map[string]string{
		"drawing":        "/var/www/dataprecast/drawing",
		"user_profile":   "/var/www/dataprecast/user_profile/",
		"project_logo":   "/var/www/dataprecast/project_logo/",
		"client_profile": "/var/www/dataprecast/client_profile/",
		"element":        "/var/www/dataprecast/element/",
		"task":           "/var/www/dataprecast/task/",
		// Add more valid locations as needed
	}

	// Check if the provided location is valid
	uploadPath, ok := validLocations[userLocation]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid upload location"})
		return
	}

	// Delete the old file if the path is provided
	if oldFilePath != "" {
		if err := os.Remove(oldFilePath); err != nil {
			fmt.Println("Error deleting old file:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete old file: " + err.Error()})
			return
		}
	}

	// Generate a unique filename using the current timestamp
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(file.Filename))

	// Ensure the directory exists
	if err := ensureDirExists(uploadPath); err != nil {
		fmt.Println("Error ensuring directory exists:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory: " + err.Error()})
		return
	}

	// Construct the full file path for the current upload path
	filePath := filepath.Join(uploadPath, filename)

	// Save the uploaded file to the specified location
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		fmt.Println("Error saving file:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	// After uploading, return the URL from the specified location
	baseURL := fmt.Sprintf("https://precastezy.blueinvent.com:9000/%s/", userLocation)
	fileURL := fmt.Sprintf("%s%s", baseURL, filename)
	c.JSON(http.StatusOK, gin.H{"url": fileURL})
}
*/

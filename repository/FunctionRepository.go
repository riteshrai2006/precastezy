package repository

import (
	"backend/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func GenerateRandomNumber() int {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	return rng.Intn(900000000) + 100000000
}

/*
	func GenerateRandomCode() string {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		prefix := string(rng.Intn(26)+'A') + string(rng.Intn(26)+'A')
		number := rng.Intn(90000) + 10000

		// Convert number to string before concatenation
		return fmt.Sprintf("%s%d", prefix, number)
	}
*/
func GenerateRandomCode() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	prefix := string(letters[rng.Intn(len(letters))]) + string(letters[rng.Intn(len(letters))])
	number := rng.Intn(90000) + 10000

	return fmt.Sprintf("%s%d", prefix, number)
}

func GenerateElementID(elementType string, NamingConvention string, sequenceNumber int) string {
	// Ensure the element type is converted to uppercase
	formattedElementType := strings.ToUpper(elementType)

	// Format the sequence number as a 4-digit string with leading zeros (e.g., 0001, 0002, ...)
	formattedSequence := fmt.Sprintf("%04d", sequenceNumber)

	// Combine to form the final Element ID in the format "elementType/0001"
	elementID := formattedElementType + "/" + NamingConvention + "/" + formattedSequence

	return elementID
}

func GenerateVersionCode(previousVersion string) string {
	if previousVersion == "" {
		return "RV-01"
	}

	if !strings.HasPrefix(previousVersion, "RV-") {
		fmt.Println("invalid version format")
	}

	versionNumberStr := strings.TrimPrefix(previousVersion, "RV-")

	versionNumber, err := strconv.Atoi(versionNumberStr)
	if err != nil {
		fmt.Printf("invalid version number: %v", err)
	}

	nextVersion := versionNumber + 1

	newVersionCode := "RV-" + fmt.Sprintf("%02d", nextVersion)

	return newVersionCode
}

// FetchHierarchyResponse retrieves data from the precast table
func FetchHierarchyResponse(db *sql.DB, hierarchyID int) (*models.HierarchyResponce, error) {
	query := `
		SELECT hierarchy_id, quantity, project_id, name, description, parent_id, prefix, naming_convention
		FROM precast
		WHERE hierarchy_id = $1
	`
	row := db.QueryRow(query, hierarchyID)

	var hierarchy models.HierarchyResponce
	err := row.Scan(
		&hierarchy.HierarchyID,
		&hierarchy.Quantity,
		&hierarchy.ProjectID,
		&hierarchy.Name,
		&hierarchy.Description,
		&hierarchy.ParentID,
		&hierarchy.Prefix,
		&hierarchy.NamingConvention,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hierarchy response for hierarchy_id %d: %w", hierarchyID, err)
	}
	return &hierarchy, nil
}

func FetchHierarchyPath(db *sql.DB, hierarchyID int) (string, error) {
	query := `
        WITH RECURSIVE hierarchy_path AS (
            SELECT 
                id,
                name,
                parent_id,
                CAST(name AS TEXT) AS path
            FROM precast
            WHERE id = $1

            UNION ALL

            SELECT 
                p.id,
                p.name,
                p.parent_id,
                h.path || ' -> ' || p.name
            FROM precast p
            INNER JOIN hierarchy_path h ON p.id = h.parent_id
        )
        SELECT path
        FROM hierarchy_path
        WHERE parent_id IS NULL;
    `

	var path string
	err := db.QueryRow(query, hierarchyID).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("failed to fetch hierarchy path: %w", err)
	}

	return path, nil
}
func FetchElementTypeWithProducts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract the element ID from the request parameters
		elementID := c.Param("id")
		if elementID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "element ID is required"})
			return
		}

		// Query to fetch the `elementtype_id` from the `element` table
		elementQuery := `
			SELECT 
				element_type_id
			FROM 
				element
			WHERE 
				id = $1;
		`
		var elementTypeID int

		// Get elementTypeID
		err := db.QueryRow(elementQuery, elementID).Scan(&elementTypeID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "element or associated element type not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error querying element: %s", err.Error())})
			return
		}

		// Query to fetch JSONB product data from the database
		productsQuery := `
			SELECT 
				product 
			FROM 
				bompro 
			WHERE 
				element_type_id = $1;
		`

		// Query the database
		rows, err := db.Query(productsQuery, elementTypeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error querying products: %s", err.Error())})
			return
		}
		defer rows.Close()

		// Parse JSONB data into models.Product
		var products []models.Product
		for rows.Next() {
			var productJSON []byte
			err := rows.Scan(&productJSON) // Scan the JSONB column into a byte slice
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error scanning product JSON: %s", err.Error())})
				return
			}

			// Unmarshal the JSONB data
			var productData []models.Product
			err = json.Unmarshal(productJSON, &productData)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error unmarshalling product JSON: %s", err.Error())})
				return
			}

			// Append the parsed product slice to the response
			products = append(products, productData...)
		}

		// Respond with the populated products
		c.JSON(http.StatusOK, products)
	}
}

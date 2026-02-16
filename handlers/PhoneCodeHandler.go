package handlers

import (
	"database/sql"
	"net/http"

	"backend/models"

	"github.com/gin-gonic/gin"
)

// CreatePhoneCode godoc
// @Summary      Create phone code
// @Tags         phone-codes
// @Accept       json
// @Produce      json
// @Param        body  body      models.PhoneCode  true  "Phone code"
// @Success      201   {object}  models.PhoneCode
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/phonecodes [post]
func CreatePhoneCode(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var pc models.PhoneCode
		if err := c.ShouldBindJSON(&pc); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := db.QueryRow(`
			INSERT INTO phone_code (country_name, phone_code)
			VALUES ($1, $2) RETURNING id
		`, pc.CountryName, pc.PhoneCode).Scan(&pc.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, pc)
	}
}

// GetAllPhoneCodes godoc
// @Summary      List phone codes
// @Tags         phone-codes
// @Success      200  {array}  models.PhoneCode
// @Router       /api/phonecodes [get]
func GetAllPhoneCodes(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(`SELECT id, country_name, phone_code FROM phone_code ORDER BY country_name`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var codes []models.PhoneCode
		for rows.Next() {
			var pc models.PhoneCode
			if err := rows.Scan(&pc.ID, &pc.CountryName, &pc.PhoneCode); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			codes = append(codes, pc)
		}

		c.JSON(http.StatusOK, codes)
	}
}

// GetPhoneCode godoc
// @Summary      Get phone code by ID
// @Tags         phone-codes
// @Param        id   path      int  true  "Phone code ID"
// @Success      200  {object}  models.PhoneCode
// @Router       /api/phonecodes/{id} [get]
func GetPhoneCode(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var pc models.PhoneCode
		err := db.QueryRow(`SELECT id, country_name, phone_code FROM phone_code WHERE id=$1`, id).
			Scan(&pc.ID, &pc.CountryName, &pc.PhoneCode)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Phone code not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, pc)
	}
}

// UpdatePhoneCode godoc
// @Summary      Update phone code
// @Tags         phone-codes
// @Param        id     path      int  true  "Phone code ID"
// @Param        body   body      models.PhoneCode  true  "Phone code"
// @Success      200    {object}  models.PhoneCode
// @Router       /api/phonecodes/{id} [put]
func UpdatePhoneCode(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var pc models.PhoneCode
		if err := c.ShouldBindJSON(&pc); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, err := db.Exec(`
			UPDATE phone_code SET country_name=$1, phone_code=$2 WHERE id=$3
		`, pc.CountryName, pc.PhoneCode, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Phone code not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Phone code updated successfully"})
	}
}

// DeletePhoneCode godoc
// @Summary      Delete phone code
// @Tags         phone-codes
// @Param        id   path      int  true  "Phone code ID"
// @Success      200  {object}  object
// @Router       /api/phonecodes/{id} [delete]
func DeletePhoneCode(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		result, err := db.Exec(`DELETE FROM phone_code WHERE id=$1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Phone code not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Phone code deleted successfully"})
	}
}

package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"backend/models"

	"github.com/gin-gonic/gin"
)

// CreateCurrency godoc
// @Summary      Create currency
// @Tags         currency
// @Accept       json
// @Produce      json
// @Param        body  body      models.Currency  true  "Currency"
// @Success      201   {object}  models.Currency
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/currency [post]
func CreateCurrency(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cur models.Currency
		if err := c.ShouldBindJSON(&cur); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `INSERT INTO currency (currency_name, currency_code, symbol) VALUES ($1, $2, $3) RETURNING id`
		err := db.QueryRow(query, cur.CurrencyName, cur.CurrencyCode, cur.Symbol).Scan(&cur.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, cur)
	}
}

// GetCurrencies godoc
// @Summary      List currencies
// @Tags         currency
// @Success      200  {array}  models.Currency
// @Router       /api/currency [get]
func GetCurrencies(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(`SELECT id, currency_name, currency_code, symbol FROM currency ORDER BY id`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var currencies []models.Currency
		for rows.Next() {
			var cur models.Currency
			if err := rows.Scan(&cur.ID, &cur.CurrencyName, &cur.CurrencyCode, &cur.Symbol); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			currencies = append(currencies, cur)
		}

		c.JSON(http.StatusOK, currencies)
	}
}

// GetCurrencyByID godoc
// @Summary      Get currency by ID
// @Tags         currency
// @Param        id   path      int  true  "Currency ID"
// @Success      200  {object}  models.Currency
// @Router       /api/currency/{id} [get]
func GetCurrencyByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var cur models.Currency
		err := db.QueryRow(`SELECT id, currency_name, currency_code, symbol FROM currency WHERE id=$1`, id).
			Scan(&cur.ID, &cur.CurrencyName, &cur.CurrencyCode, &cur.Symbol)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Currency not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, cur)
	}
}

// UpdateCurrency godoc
// @Summary      Update currency
// @Tags         currency
// @Param        id     path      int  true  "Currency ID"
// @Param        body   body      models.Currency  true  "Currency"
// @Success      200    {object}  models.Currency
// @Router       /api/currency/{id} [put]
func UpdateCurrency(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var cur models.Currency
		if err := c.ShouldBindJSON(&cur); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `UPDATE currency SET currency_name=$1, currency_code=$2, symbol=$3 WHERE id=$4`
		res, err := db.Exec(query, cur.CurrencyName, cur.CurrencyCode, cur.Symbol, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Currency not found"})
			return
		}

		cur.ID, _ = strconv.Atoi(id)
		c.JSON(http.StatusOK, cur)
	}
}

// DeleteCurrency godoc
// @Summary      Delete currency
// @Tags         currency
// @Param        id   path      int  true  "Currency ID"
// @Success      200  {object}  object
// @Router       /api/currency/{id} [delete]
func DeleteCurrency(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		res, err := db.Exec(`DELETE FROM currency WHERE id=$1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Currency not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Currency deleted successfully"})
	}
}

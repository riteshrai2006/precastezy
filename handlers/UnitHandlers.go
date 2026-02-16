package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"backend/models"

	"github.com/gin-gonic/gin"
)

// CreateUnit godoc
// @Summary      Create unit
// @Tags         units
// @Accept       json
// @Produce      json
// @Param        body  body      models.Unit  true  "Unit"
// @Success      201   {object}  models.Unit
// @Failure      400   {object}  models.ErrorResponse
// @Router       /api/units [post]
func CreateUnit(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var u models.Unit
		if err := c.ShouldBindJSON(&u); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `INSERT INTO units (unit_name, description) VALUES ($1, $2) RETURNING id`
		err := db.QueryRow(query, u.UnitName, u.Description).Scan(&u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, u)
	}
}

// GetUnits godoc
// @Summary      List units
// @Tags         units
// @Success      200  {array}  models.Unit
// @Router       /api/units [get]
func GetUnits(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(`SELECT id, unit_name, description FROM units ORDER BY id`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var units []models.Unit
		for rows.Next() {
			var u models.Unit
			if err := rows.Scan(&u.ID, &u.UnitName, &u.Description); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			units = append(units, u)
		}

		c.JSON(http.StatusOK, units)
	}
}

// GetUnitByID godoc
// @Summary      Get unit by ID
// @Tags         units
// @Param        id   path      int  true  "Unit ID"
// @Success      200  {object}  models.Unit
// @Failure      404  {object}  models.ErrorResponse
// @Router       /api/units/{id} [get]
func GetUnitByID(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var u models.Unit
		err := db.QueryRow(`SELECT id, unit_name, description FROM units WHERE id=$1`, id).
			Scan(&u.ID, &u.UnitName, &u.Description)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Unit not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, u)
	}
}

// UpdateUnit godoc
// @Summary      Update unit
// @Tags         units
// @Param        id    path      int         true  "Unit ID"
// @Param        body  body      models.Unit  true  "Unit"
// @Success      200   {object}  models.Unit
// @Router       /api/units/{id} [put]
func UpdateUnit(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var u models.Unit
		if err := c.ShouldBindJSON(&u); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		query := `UPDATE units SET unit_name=$1, description=$2 WHERE id=$3`
		res, err := db.Exec(query, u.UnitName, u.Description, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Unit not found"})
			return
		}
		intID, err := strconv.Atoi(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid unit ID"})
			return
		}
		u.ID = intID
		c.JSON(http.StatusOK, u)
	}
}

// DeleteUnit godoc
// @Summary      Delete unit
// @Tags         units
// @Param        id   path      int  true  "Unit ID"
// @Success      200  {object}  object
// @Router       /api/units/{id} [delete]
func DeleteUnit(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		res, err := db.Exec(`DELETE FROM units WHERE id=$1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Unit not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Unit deleted successfully"})
	}
}

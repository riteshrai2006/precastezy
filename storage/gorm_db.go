package storage

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var gormDB *gorm.DB

// InitGormDB initializes GORM database connection
func InitGormDB() *gorm.DB {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Kolkata",
		host, user, password, dbname, port)

	var err error
	gormDB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Info),
		DisableForeignKeyConstraintWhenMigrating: true,
		DryRun:                                   false,
		DisableAutomaticPing:                     false,
	})
	if err != nil {
		log.Fatal("Failed to connect to database with GORM:", err)
	}

	// Get the underlying sql.DB object
	sqlDB, err := gormDB.DB()
	if err != nil {
		log.Fatal("Failed to get underlying sql.DB:", err)
	}

	// Set connection pool settings optimized for performance
	sqlDB.SetMaxIdleConns(10)                  // Increased for better connection reuse
	sqlDB.SetMaxOpenConns(50)                  // Increased for better concurrency
	sqlDB.SetConnMaxLifetime(10 * time.Minute) // Increased connection lifetime
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)  // Close idle connections after 5 minutes

	// Auto migrate models (optional - uncomment if you want automatic table creation)
	// autoMigrateModels() // Temporarily disabled to debug connection issues

	return gormDB
}

// GetGormDB returns the GORM database instance
func GetGormDB() *gorm.DB {
	return gormDB
}

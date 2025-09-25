package database

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"video-management/internal/config"
	"video-management/internal/entity" // Import the new entity package
)

func InitDB(cfg config.Config) (*gorm.DB, error) {
	if cfg.DBHost == "" {
		cfg.DBHost = "db"
	}
	if cfg.DBPort == "" {
		cfg.DBPort = "5432"
	}
	if cfg.DBUser == "" {
		cfg.DBUser = "postgres"
	}
	if cfg.DBPassword == "" {
		cfg.DBPassword = "postgres"
	}
	if cfg.DBName == "" {
		cfg.DBName = "movie"
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Migrate the schema
	db.AutoMigrate(&entity.Video{}) // Use entity.Video

	return db, nil
}

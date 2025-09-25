package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"video-management/api/handler"
	"video-management/internal/config"
	"video-management/internal/database"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	if cfg.ServerPort == "" {
		cfg.ServerPort = "8080"
	}

	db, err := database.InitDB(cfg)
	if err != nil {
		panic("failed to connect database: " + err.Error())
	}

	progressMu := &sync.RWMutex{}
	jobProgress := make(map[string]float64)

	h := &handler.Handler{
		DB:          db,
		ProgressMu:  progressMu,
		JobProgress: jobProgress,
	}

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.POST("/api/upload", h.Upload)
	e.GET("/api/videos", h.GetVideos)
	e.DELETE("/api/videos/:id", h.DeleteVideo)
	e.GET("/progress/:id", h.GetProgress)

	// Serve HLS static
	e.Static("/hls", "./hls")

	e.Logger.Infof("server starting on :%s", cfg.ServerPort)
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%s", cfg.ServerPort)))
}

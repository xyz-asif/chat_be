// Package main Chat API
package main

import (
	"context"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/xyz-asif/gotodo/internal/config"
	"github.com/xyz-asif/gotodo/internal/database"
	"github.com/xyz-asif/gotodo/internal/features/chat"
	"github.com/xyz-asif/gotodo/internal/features/connections"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/middleware"
	"github.com/xyz-asif/gotodo/internal/routes"
	"github.com/xyz-asif/gotodo/pkg/response"
)

func main() {
	// 1. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Connect Database
	db, err := database.Connect(cfg.MongoDBURI, cfg.DatabaseName)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 3. Create MongoDB Indexes
	if err := database.CreateIndexes(context.Background(), db.Database); err != nil {
		log.Printf("Warning: Failed to create indexes: %v", err)
	}

	// 4. Setup Repositories
	userRepo := users.NewRepository(db.Database)
	connectionRepo := connections.NewRepository(db.Database)
	chatRepo := chat.NewRepository(db.Database)

	// Initialize WebSockets Hub
	chatHub := chat.NewHub()
	go chatHub.Run() // Run the hub in a background goroutine

	// Initialize services
	userService := users.NewService(userRepo)
	connectionService := connections.NewService(connectionRepo)
	chatService := chat.NewService(chatRepo, userRepo, chatHub)

	// Initialize handlers
	userHandler := users.NewHandler(userService)
	connectionHandler := connections.NewHandler(connectionService)
	chatHandler := chat.NewHandler(chatService)

	// 5. Setup Middleware
	authMiddleware, err := middleware.NewAuthMiddleware(cfg.FirebaseCredsPath, cfg.FirebaseProjectID, userService)
	if err != nil {
		log.Printf("Warning: Firebase Auth not setup: %v", err)
	}

	// 6. Setup Fiber
	app := fiber.New(fiber.Config{
		AppName: "Chat API v1.0",
	})
	app.Use(logger.New())
	app.Use(cors.New())

	// Root Route
	app.Get("/", func(c *fiber.Ctx) error {
		return response.OK(c, "Welcome to Chat API", fiber.Map{
			"version":     "v1",
			"healthCheck": "/health",
		})
	})

	// 7. Setup Routes
	routes.SetupRoutes(
		app,
		authMiddleware,
		userHandler,
		connectionHandler,
		chatHandler,
	)
	// 8. Start Server
	log.Printf("🚀 Starting Chat API on port %s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

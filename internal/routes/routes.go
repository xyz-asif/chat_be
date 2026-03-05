package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/middleware"
)

// MVP Feature: Users Core
func SetupRoutes(
	app *fiber.App,
	authMiddleware *middleware.AuthMiddleware,
	userHandler *users.Handler,
) {
	api := app.Group("/api/v1")

	// Health check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// User Routes - MVP Feature: User Profile Management
	usersGroup := api.Group("/users")
	usersGroup.Get("/me", authMiddleware.VerifyToken, userHandler.GetMe)
	usersGroup.Patch("/me", authMiddleware.VerifyToken, userHandler.UpdateProfile)
	usersGroup.Post("/:id/follow", authMiddleware.VerifyToken, userHandler.FollowUser)
	usersGroup.Delete("/:id/follow", authMiddleware.VerifyToken, userHandler.UnfollowUser)

	// Note: Rate limiting should be applied via middleware to write endpoints
}

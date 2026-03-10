package routes

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/features/chat"
	"github.com/xyz-asif/gotodo/internal/features/connections"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/middleware"
)

func SetupRoutes(
	app *fiber.App,
	authMiddleware *middleware.AuthMiddleware,
	userHandler *users.Handler,
	connectionHandler *connections.Handler,
	chatHandler *chat.Handler,
) {
	api := app.Group("/api/v1")

	// Health check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// ── User Routes ──
	usersGroup := api.Group("/users")
	usersGroup.Get("/search", authMiddleware.VerifyToken, userHandler.Search)
	usersGroup.Get("/search-with-status", authMiddleware.VerifyToken, userHandler.SearchWithConnectionStatus)
	usersGroup.Get("/me", authMiddleware.VerifyToken, userHandler.GetMe)
	usersGroup.Patch("/me", authMiddleware.VerifyToken, userHandler.UpdateProfile)
	usersGroup.Post("/:id/follow", authMiddleware.VerifyToken, userHandler.FollowUser)
	usersGroup.Delete("/:id/follow", authMiddleware.VerifyToken, userHandler.UnfollowUser)

	// ── Connection / Friend Request Routes ──
	connGroup := api.Group("/connections")
	connGroup.Post("/request", authMiddleware.VerifyToken, connectionHandler.SendRequest)
	connGroup.Post("/:id/accept", authMiddleware.VerifyToken, connectionHandler.AcceptRequest)
	connGroup.Post("/:id/reject", authMiddleware.VerifyToken, connectionHandler.RejectRequest)
	connGroup.Post("/:id/cancel", authMiddleware.VerifyToken, connectionHandler.CancelRequest)
	connGroup.Delete("/:id", authMiddleware.VerifyToken, connectionHandler.RemoveConnection)
	connGroup.Get("/pending", authMiddleware.VerifyToken, connectionHandler.GetPendingRequests)
	connGroup.Get("/friends", authMiddleware.VerifyToken, connectionHandler.GetFriendsList)

	// ── Chat & Messaging Routes ──
	chatGroup := api.Group("/chat")

	// Rooms
	chatGroup.Get("/rooms", authMiddleware.VerifyToken, chatHandler.GetUserRooms)
	chatGroup.Post("/rooms/direct/:id", authMiddleware.VerifyToken, chatHandler.GetOrCreateDirectRoom)
	chatGroup.Get("/rooms/:roomId/messages", authMiddleware.VerifyToken, chatHandler.GetRoomMessages)
	chatGroup.Post("/rooms/:roomId/messages", authMiddleware.VerifyToken, chatHandler.SendMessage)
	chatGroup.Post("/rooms/:roomId/read", authMiddleware.VerifyToken, chatHandler.MarkRoomAsRead)
	chatGroup.Delete("/rooms/:roomId", authMiddleware.VerifyToken, chatHandler.DeleteChat)

	// Messages
	chatGroup.Patch("/messages/:messageId/status", authMiddleware.VerifyToken, chatHandler.UpdateMessageStatus)
	chatGroup.Put("/messages/:messageId/reactions", authMiddleware.VerifyToken, chatHandler.UpdateMessageReaction)
	chatGroup.Patch("/messages/:messageId", authMiddleware.VerifyToken, chatHandler.EditMessage)
	chatGroup.Delete("/messages/:messageId", authMiddleware.VerifyToken, chatHandler.DeleteMessage)

	// Presence
	chatGroup.Get("/users/:id/presence", authMiddleware.VerifyToken, chatHandler.GetUserPresence)

	// WebSocket
	chatGroup.Get("/ws", authMiddleware.VerifyToken, chatHandler.WsUpgrade, websocket.New(chatHandler.WebSocketHandle))
}

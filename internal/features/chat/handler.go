package chat

import (
	"log"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/models"
	"github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// WsUpgrade handles the initial HTTP request and upgrades it to a WebSocket connection
func (h *Handler) WsUpgrade(c *fiber.Ctx) error {
	// The auth token is passed as a query param or header.
	// We rely on the authMiddleware to inject the user into c.Locals BEFORE this runs
	// but WebSockets limits header changes in browser JS. So often we verify via query params like ?token=
	// Let's assume standard auth middleware handles it and sets c.Locals("user")

	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized access to websocket")
	}

	// Ensure the connection is a valid websocket upgrade request
	if websocket.IsWebSocketUpgrade(c) {
		// Set the user ID into the local context so it survives the upgrade
		c.Locals("userID", user.ID.Hex())
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

// WebSocketHandle is the Fiber WebSocket handler itself
func (h *Handler) WebSocketHandle(c *websocket.Conn) {
	// Extract the userID injected during WsUpgrade phase
	userID, ok := c.Locals("userID").(string)
	if !ok {
		log.Println("WS Error: User ID missing in websocket context")
		return
	}

	// Let the service process the connection
	h.service.HandleWebSocket(c, userID)
}

// GetOrCreateDirectRoom HTTP Endpoint to start a chat with someone
func (h *Handler) GetOrCreateDirectRoom(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	targetUserId := c.Params("id")
	if targetUserId == "" {
		return response.BadRequest(c, "target user ID is required")
	}

	room, err := h.service.GetOrCreateDirectRoom(c.Context(), user.ID.Hex(), targetUserId)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Room retrieved successfully", room)
}

// GetUserRooms HTTP Endpoint to list all chats
func (h *Handler) GetUserRooms(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	rooms, err := h.service.GetUserRooms(c.Context(), user.ID.Hex())
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Rooms retrieved", rooms)
}

// GetRoomMessages HTTP Endpoint to fetch history
func (h *Handler) GetRoomMessages(c *fiber.Ctx) error {
	roomID := c.Params("roomId")
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	msgs, err := h.service.GetRoomMessages(c.Context(), roomID, limit, offset)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Messages retrieved", msgs)
}

// SendMessage HTTP Endpoint (Optional fallback instead of WebSocket)
func (h *Handler) SendMessage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	roomID := c.Params("roomId")
	var req struct {
		Content string `json:"content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	msg, err := h.service.SendMessage(c.Context(), user.ID.Hex(), roomID, req.Content)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.Created(c, "Message sent", msg)
}

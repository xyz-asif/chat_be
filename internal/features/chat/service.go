package chat

import (
	"context"
	"errors"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	SendMessage(ctx context.Context, senderID, roomID, content string) (*models.MessageResponse, error)
	GetOrCreateDirectRoom(ctx context.Context, user1ID, user2ID string) (*models.RoomResponse, error)
	GetRoomMessages(ctx context.Context, roomID string, limit, offset int) ([]models.MessageResponse, error)
	GetUserRooms(ctx context.Context, userID string) ([]models.RoomResponse, error)
	HandleWebSocket(c *websocket.Conn, userID string)
}

type service struct {
	repo     Repository
	userRepo users.Repository
	hub      *Hub
}

func NewService(repo Repository, userRepo users.Repository, hub *Hub) Service {
	return &service{
		repo:     repo,
		userRepo: userRepo,
		hub:      hub,
	}
}

func (s *service) GetOrCreateDirectRoom(ctx context.Context, user1IDStr, user2IDStr string) (*models.RoomResponse, error) {
	user1ID, err := bson.ObjectIDFromHex(user1IDStr)
	if err != nil {
		return nil, errors.New("invalid user1 id")
	}
	user2ID, err := bson.ObjectIDFromHex(user2IDStr)
	if err != nil {
		return nil, errors.New("invalid user2 id")
	}

	if user1ID == user2ID {
		return nil, errors.New("cannot create room with yourself")
	}

	room, err := s.repo.GetDirectRoom(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}

	if room == nil {
		// Create new direct room
		room = &models.Room{
			Type:         models.RoomTypeDirect,
			Participants: []bson.ObjectID{user1ID, user2ID},
		}
		if err := s.repo.CreateRoom(ctx, room); err != nil {
			return nil, err
		}
	}

	return s.buildRoomResponse(ctx, room)
}

func (s *service) SendMessage(ctx context.Context, senderIDStr, roomIDStr, content string) (*models.MessageResponse, error) {
	senderID, err := bson.ObjectIDFromHex(senderIDStr)
	if err != nil {
		return nil, errors.New("invalid sender id")
	}
	roomID, err := bson.ObjectIDFromHex(roomIDStr)
	if err != nil {
		return nil, errors.New("invalid room id")
	}

	if content == "" {
		return nil, errors.New("message content cannot be empty")
	}

	// Verify user is in the room
	room, err := s.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found or error accessing room")
	}

	isParticipant := false
	for _, p := range room.Participants {
		if p == senderID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return nil, errors.New("sender is not a participant in this room")
	}

	// Save the message
	msg := &models.Message{
		RoomID:   roomID,
		SenderID: senderID,
		Content:  content,
		Status:   models.MessageStatusSent,
	}

	if err := s.repo.SaveMessage(ctx, msg); err != nil {
		return nil, err
	}

	// Update room last message
	if err := s.repo.UpdateRoomLastMessage(ctx, roomID, content); err != nil {
		// Log error but don't fail message send
	}

	resp := buildMessageResponse(msg, nil)

	// Broadcast via WebSocket
	wsMsg := models.WSMessage{
		Type:    "message",
		RoomID:  roomIDStr,
		Payload: resp,
	}

	for _, p := range room.Participants {
		// Optimize by not sending to sender if desired, or let client handle deduping
		// Let's send to everyone including sender so they know it hit the server
		_ = s.hub.SendMessage(p.Hex(), wsMsg)
	}

	return resp, nil
}

func (s *service) GetRoomMessages(ctx context.Context, roomIDStr string, limit, offset int) ([]models.MessageResponse, error) {
	roomID, err := bson.ObjectIDFromHex(roomIDStr)
	if err != nil {
		return nil, errors.New("invalid room id")
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	msgs, err := s.repo.GetMessagesByRoom(ctx, roomID, limit, offset)
	if err != nil {
		return nil, err
	}

	// Build responses
	var responses []models.MessageResponse
	for _, m := range msgs {
		responses = append(responses, *buildMessageResponse(&m, nil))
	}

	// Reverse to chronological order (repo returns newest first for pagination, but UI wants oldest first)
	for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
		responses[i], responses[j] = responses[j], responses[i]
	}

	return responses, nil
}

func (s *service) GetUserRooms(ctx context.Context, userIDStr string) ([]models.RoomResponse, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	rooms, err := s.repo.GetUserRooms(ctx, userID)
	if err != nil {
		return nil, err
	}

	var responses []models.RoomResponse
	for _, r := range rooms {
		if resp, err := s.buildRoomResponse(ctx, &r); err == nil {
			responses = append(responses, *resp)
		}
	}

	return responses, nil
}

func (s *service) HandleWebSocket(c *websocket.Conn, userID string) {
	client := &clientContext{
		userID: userID,
		conn:   c,
	}

	s.hub.register <- client

	// Ensure cleanup when routine ends
	defer func() {
		s.hub.unregister <- client
	}()

	// Read loop
	for {
		var msg models.WSMessage
		if err := c.ReadJSON(&msg); err != nil {
			// Ignore standard close errors
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// log.Printf("WS Error reading message from %s: %v", userID, err)
			}
			break
		}

		// Handle specific incoming events like typing indicators directly through the hub
		switch msg.Type {
		case "typing_start", "typing_stop", "message_read":
			// Basic relay to other participants in the room
			if msg.RoomID == "" {
				continue
			}

			roomID, err := bson.ObjectIDFromHex(msg.RoomID)
			if err != nil {
				continue
			}

			// Ideally, we'd cache room participants. For MVP, we fetch.
			room, err := s.repo.GetRoomByID(context.Background(), roomID)
			if err != nil {
				continue
			}

			// Broadcast event to everyone in the room except sender
			for _, p := range room.Participants {
				participantHex := p.Hex()
				if participantHex != userID {
					_ = s.hub.SendMessage(participantHex, msg)
				}
			}
		}
	}
}

// Helpers
func (s *service) buildRoomResponse(ctx context.Context, room *models.Room) (*models.RoomResponse, error) {
	resp := &models.RoomResponse{
		ID:           room.ID.Hex(),
		Type:         room.Type,
		Name:         room.Name,
		LastMessage:  room.LastMessage,
		LastUpdated:  room.LastUpdated,
		Participants: make([]models.User, 0),
	}

	for _, p := range room.Participants {
		user, err := s.userRepo.GetUserByID(ctx, p)
		if err == nil && user != nil {
			resp.Participants = append(resp.Participants, *user)
		}
	}

	return resp, nil
}

func buildMessageResponse(msg *models.Message, replyTo *models.MessageResponse) *models.MessageResponse {
	return &models.MessageResponse{
		ID:        msg.ID.Hex(),
		RoomID:    msg.RoomID.Hex(),
		SenderID:  msg.SenderID.Hex(),
		Content:   msg.Content,
		Status:    msg.Status,
		Reactions: msg.Reactions,
		ReplyTo:   replyTo,
		CreatedAt: msg.CreatedAt,
		UpdatedAt: msg.UpdatedAt,
	}
}

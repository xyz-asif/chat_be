package chat

import (
	"context"
	"errors"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/features/connections"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	SendMessage(ctx context.Context, senderID, roomID, content, replyToID string) (*models.MessageResponse, error)
	GetOrCreateDirectRoom(ctx context.Context, user1ID, user2ID string) (*models.RoomResponse, error)
	GetRoomMessages(ctx context.Context, userID, roomID string, limit, offset int) ([]models.MessageResponse, error)
	GetUserRooms(ctx context.Context, userID string) ([]models.RoomResponse, error)
	GetUserPresence(ctx context.Context, userID string) (map[string]interface{}, error)
	UpdateMessageStatus(ctx context.Context, userID, messageID, status string) error
	UpdateMessageReaction(ctx context.Context, userID, messageID, emoji string) error
	MarkRoomAsRead(ctx context.Context, userID, roomID string) error
	EditMessage(ctx context.Context, userID, messageID, content string) error
	DeleteMessage(ctx context.Context, userID, messageID string) error
	HandleWebSocket(c *websocket.Conn, userID string)
}

type service struct {
	repo     Repository
	userRepo users.Repository
	connRepo connections.Repository
	hub      *Hub
}

func NewService(repo Repository, userRepo users.Repository, connRepo connections.Repository, hub *Hub) Service {
	return &service{
		repo:     repo,
		userRepo: userRepo,
		connRepo: connRepo,
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

	// Check if users have an accepted connection before allowing a chat room
	conn, err := s.connRepo.GetConnectionBetweenUsers(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != models.ConnectionStatusAccepted {
		return nil, errors.New("you must be connected (friends) with this user before chatting")
	}

	room, err := s.repo.GetDirectRoom(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}

	if room == nil {
		room = &models.Room{
			Type:         models.RoomTypeDirect,
			Participants: []bson.ObjectID{user1ID, user2ID},
			UnreadCounts: map[string]int{user1IDStr: 0, user2IDStr: 0},
		}
		if err := s.repo.CreateRoom(ctx, room); err != nil {
			return nil, err
		}
	}

	return s.buildRoomResponse(ctx, room, user1IDStr)
}

func (s *service) SendMessage(ctx context.Context, senderIDStr, roomIDStr, content, replyToIDStr string) (*models.MessageResponse, error) {
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

	room, err := s.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found or error accessing room")
	}

	if !isUserInRoom(room, senderID) {
		return nil, errors.New("sender is not a participant in this room")
	}

	var replyToObjId *bson.ObjectID
	if replyToIDStr != "" {
		objID, err := bson.ObjectIDFromHex(replyToIDStr)
		if err == nil {
			if _, err := s.repo.GetMessageByID(ctx, objID); err == nil {
				replyToObjId = &objID
			}
		}
	}

	msg := &models.Message{
		RoomID:    roomID,
		SenderID:  senderID,
		Content:   content,
		Status:    models.MessageStatusSent,
		ReplyToID: replyToObjId,
	}

	if err := s.repo.SaveMessage(ctx, msg); err != nil {
		return nil, err
	}

	// Update room last message + sender
	_ = s.repo.UpdateRoomLastMessage(ctx, roomID, content, senderID)

	// Increment unread count for everyone except the sender
	_ = s.repo.IncrementUnreadCounts(ctx, roomID, senderIDStr)

	resp := s.buildMessageResponse(ctx, msg)

	// Broadcast via WebSocket
	wsMsg := models.WSMessage{
		Type:    "message",
		RoomID:  roomIDStr,
		Payload: resp,
	}

	userIDs := make([]string, len(room.Participants))
	for i, p := range room.Participants {
		userIDs[i] = p.Hex()
	}
	_ = s.hub.SendToUsers(userIDs, wsMsg)

	return resp, nil
}

func (s *service) GetRoomMessages(ctx context.Context, userIDStr, roomIDStr string, limit, offset int) ([]models.MessageResponse, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	roomID, err := bson.ObjectIDFromHex(roomIDStr)
	if err != nil {
		return nil, errors.New("invalid room id")
	}

	room, err := s.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}
	if !isUserInRoom(room, userID) {
		return nil, errors.New("unauthorized: not a participant")
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

	responses := make([]models.MessageResponse, 0, len(msgs))
	for _, m := range msgs {
		responses = append(responses, *s.buildMessageResponse(ctx, &m))
	}

	// Reverse to chronological order
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

	responses := make([]models.RoomResponse, 0, len(rooms))
	for _, r := range rooms {
		if resp, err := s.buildRoomResponse(ctx, &r, userIDStr); err == nil {
			responses = append(responses, *resp)
		}
	}

	return responses, nil
}

func (s *service) MarkRoomAsRead(ctx context.Context, userIDStr, roomIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	roomID, err := bson.ObjectIDFromHex(roomIDStr)
	if err != nil {
		return errors.New("invalid room id")
	}

	room, err := s.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return errors.New("room not found")
	}
	if !isUserInRoom(room, userID) {
		return errors.New("unauthorized: not a participant")
	}

	// Reset the unread count for this user
	if err := s.repo.ResetUnreadCount(ctx, roomID, userIDStr); err != nil {
		return err
	}

	// Batch mark all unread messages from other senders as "read"
	if err := s.repo.MarkRoomMessagesAsRead(ctx, roomID, userID); err != nil {
		return err
	}

	// Broadcast read receipt to other participants so their UI updates the blue ticks
	for _, p := range room.Participants {
		pHex := p.Hex()
		if pHex != userIDStr {
			wsMsg := models.WSMessage{
				Type:   "room_read",
				RoomID: roomIDStr,
				Payload: map[string]string{
					"readBy": userIDStr,
				},
			}
			_ = s.hub.SendMessage(pHex, wsMsg)
		}
	}

	return nil
}

func (s *service) EditMessage(ctx context.Context, userIDStr, messageIDStr, content string) error {
	senderID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	messageID, err := bson.ObjectIDFromHex(messageIDStr)
	if err != nil {
		return errors.New("invalid message id")
	}

	if content == "" {
		return errors.New("content cannot be empty")
	}

	msg, err := s.repo.GetMessageByID(ctx, messageID)
	if err != nil {
		return errors.New("message not found")
	}

	// Only the sender can edit their own message
	if msg.SenderID != senderID {
		return errors.New("unauthorized: only the sender can edit this message")
	}

	if msg.IsDeleted {
		return errors.New("cannot edit a deleted message")
	}

	if err := s.repo.UpdateMessageContent(ctx, messageID, content); err != nil {
		return err
	}

	// Broadcast edit to the room
	room, err := s.repo.GetRoomByID(ctx, msg.RoomID)
	if err == nil {
		wsMsg := models.WSMessage{
			Type:   "message_edited",
			RoomID: msg.RoomID.Hex(),
			Payload: map[string]string{
				"messageId": msg.ID.Hex(),
				"content":   content,
			},
		}
		userIDs := make([]string, len(room.Participants))
		for i, p := range room.Participants {
			userIDs[i] = p.Hex()
		}
		_ = s.hub.SendToUsers(userIDs, wsMsg)
	}

	return nil
}

func (s *service) DeleteMessage(ctx context.Context, userIDStr, messageIDStr string) error {
	senderID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	messageID, err := bson.ObjectIDFromHex(messageIDStr)
	if err != nil {
		return errors.New("invalid message id")
	}

	msg, err := s.repo.GetMessageByID(ctx, messageID)
	if err != nil {
		return errors.New("message not found")
	}

	// Only the sender can delete their own message
	if msg.SenderID != senderID {
		return errors.New("unauthorized: only the sender can delete this message")
	}

	if msg.IsDeleted {
		return errors.New("message is already deleted")
	}

	if err := s.repo.SoftDeleteMessage(ctx, messageID); err != nil {
		return err
	}

	// Broadcast deletion to the room
	room, err := s.repo.GetRoomByID(ctx, msg.RoomID)
	if err == nil {
		wsMsg := models.WSMessage{
			Type:   "message_deleted",
			RoomID: msg.RoomID.Hex(),
			Payload: map[string]string{
				"messageId": msg.ID.Hex(),
			},
		}
		userIDs := make([]string, len(room.Participants))
		for i, p := range room.Participants {
			userIDs[i] = p.Hex()
		}
		_ = s.hub.SendToUsers(userIDs, wsMsg)
	}

	return nil
}

func (s *service) GetUserPresence(ctx context.Context, userIDStr string) (map[string]interface{}, error) {
	_, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	isOnline := s.hub.IsUserOnline(userIDStr)

	return map[string]interface{}{
		"userId": userIDStr,
		"online": isOnline,
	}, nil
}

func (s *service) UpdateMessageStatus(ctx context.Context, userIDStr, messageIDStr, status string) error {
	_, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}

	messageID, err := bson.ObjectIDFromHex(messageIDStr)
	if err != nil {
		return errors.New("invalid message id")
	}

	if status != models.MessageStatusDelivered && status != models.MessageStatusRead {
		return errors.New("invalid status: must be delivered or read")
	}

	msg, err := s.repo.GetMessageByID(ctx, messageID)
	if err != nil {
		return errors.New("message not found")
	}

	if err := s.repo.UpdateMessageStatus(ctx, messageID, status); err != nil {
		return err
	}

	wsMsg := models.WSMessage{
		Type:   "message_status_changed",
		RoomID: msg.RoomID.Hex(),
		Payload: map[string]string{
			"messageId": msg.ID.Hex(),
			"status":    status,
			"markedBy":  userIDStr,
		},
	}
	_ = s.hub.SendMessage(msg.SenderID.Hex(), wsMsg)

	return nil
}

func (s *service) UpdateMessageReaction(ctx context.Context, userIDStr, messageIDStr, emoji string) error {
	_, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}

	messageID, err := bson.ObjectIDFromHex(messageIDStr)
	if err != nil {
		return errors.New("invalid message id")
	}

	msg, err := s.repo.GetMessageByID(ctx, messageID)
	if err != nil {
		return errors.New("message not found")
	}

	if currentEmoji, exists := msg.Reactions[userIDStr]; exists && currentEmoji == emoji {
		emoji = ""
	}

	if err := s.repo.UpdateMessageReaction(ctx, messageID, userIDStr, emoji); err != nil {
		return err
	}

	room, err := s.repo.GetRoomByID(ctx, msg.RoomID)
	if err == nil {
		wsMsg := models.WSMessage{
			Type:   "reaction_updated",
			RoomID: msg.RoomID.Hex(),
			Payload: map[string]string{
				"messageId": msg.ID.Hex(),
				"userId":    userIDStr,
				"emoji":     emoji,
			},
		}

		userIDs := make([]string, len(room.Participants))
		for i, p := range room.Participants {
			userIDs[i] = p.Hex()
		}
		_ = s.hub.SendToUsers(userIDs, wsMsg)
	}

	return nil
}

func (s *service) HandleWebSocket(c *websocket.Conn, userID string) {
	client := &clientContext{
		userID: userID,
		conn:   c,
	}

	s.hub.register <- client

	defer func() {
		s.hub.unregister <- client
	}()

	for {
		var msg models.WSMessage
		if err := c.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// log error
			}
			break
		}

		switch msg.Type {
		case "typing_start", "typing_stop":
			if msg.RoomID == "" {
				continue
			}
			roomID, err := bson.ObjectIDFromHex(msg.RoomID)
			if err != nil {
				continue
			}
			room, err := s.repo.GetRoomByID(context.Background(), roomID)
			if err != nil {
				continue
			}
			// Add sender info to the payload for the frontend
			msg.Payload = map[string]string{"userId": userID}
			for _, p := range room.Participants {
				pHex := p.Hex()
				if pHex != userID {
					_ = s.hub.SendMessage(pHex, msg)
				}
			}
		}
	}
}

// ── Helpers ──

func isUserInRoom(room *models.Room, userID bson.ObjectID) bool {
	for _, p := range room.Participants {
		if p == userID {
			return true
		}
	}
	return false
}

func (s *service) buildRoomResponse(ctx context.Context, room *models.Room, forUserID string) (*models.RoomResponse, error) {
	resp := &models.RoomResponse{
		ID:           room.ID.Hex(),
		Type:         room.Type,
		Name:         room.Name,
		LastMessage:  room.LastMessage,
		LastUpdated:  room.LastUpdated,
		Participants: make([]models.ParticipantInfo, 0, len(room.Participants)),
	}

	// Unread count for the requesting user
	if room.UnreadCounts != nil {
		resp.UnreadCount = room.UnreadCounts[forUserID]
	}

	// Resolve last message sender name
	if room.LastMessageSenderID != nil {
		if sender, err := s.userRepo.GetUserByID(ctx, *room.LastMessageSenderID); err == nil {
			resp.LastMessageSenderName = sender.DisplayName
		}
	}

	// Build participant info with online status
	for _, p := range room.Participants {
		user, err := s.userRepo.GetUserByID(ctx, p)
		if err == nil && user != nil {
			resp.Participants = append(resp.Participants, models.ParticipantInfo{
				ID:          user.ID.Hex(),
				DisplayName: user.DisplayName,
				PhotoURL:    user.PhotoURL,
				Email:       user.Email,
				IsOnline:    s.hub.IsUserOnline(user.ID.Hex()),
			})
		}
	}

	return resp, nil
}

func (s *service) buildMessageResponse(ctx context.Context, msg *models.Message) *models.MessageResponse {
	var replyToResp *models.MessageResponse

	if msg.ReplyToID != nil {
		if replyMsg, err := s.repo.GetMessageByID(ctx, *msg.ReplyToID); err == nil {
			replyToResp = &models.MessageResponse{
				ID:        replyMsg.ID.Hex(),
				RoomID:    replyMsg.RoomID.Hex(),
				SenderID:  replyMsg.SenderID.Hex(),
				Content:   replyMsg.Content,
				Status:    replyMsg.Status,
				CreatedAt: replyMsg.CreatedAt,
			}
		}
	}

	return &models.MessageResponse{
		ID:        msg.ID.Hex(),
		RoomID:    msg.RoomID.Hex(),
		SenderID:  msg.SenderID.Hex(),
		Content:   msg.Content,
		Status:    msg.Status,
		Reactions: msg.Reactions,
		ReplyTo:   replyToResp,
		IsEdited:  msg.IsEdited,
		IsDeleted: msg.IsDeleted,
		CreatedAt: msg.CreatedAt,
		UpdatedAt: msg.UpdatedAt,
	}
}

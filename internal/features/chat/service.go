package chat

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/features/connections"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	SendMessage(ctx context.Context, senderID, roomID, content, msgType string, metadata *models.MediaMetadata, replyToID string) (*models.MessageResponse, error)
	GetOrCreateDirectRoom(ctx context.Context, user1ID, user2ID string) (*models.RoomResponse, error)
	GetRoomMessages(ctx context.Context, userID, roomID string, limit int, beforeID string) (*models.MessagesPage, error)
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

	// Atomic find-or-create eliminates the TOCTOU race between two concurrent requests
	room, err := s.repo.GetOrCreateDirectRoomAtomic(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}

	return s.buildRoomResponse(ctx, room, user1IDStr)
}

func (s *service) SendMessage(ctx context.Context, senderIDStr, roomIDStr, content, msgType string, metadata *models.MediaMetadata, replyToIDStr string) (*models.MessageResponse, error) {
	senderID, err := bson.ObjectIDFromHex(senderIDStr)
	if err != nil {
		return nil, errors.New("invalid sender id")
	}
	roomID, err := bson.ObjectIDFromHex(roomIDStr)
	if err != nil {
		return nil, errors.New("invalid room id")
	}

	if err := validateMessageContent(msgType, content); err != nil {
		return nil, err
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
		Type:      msgType,
		Content:   content,
		Metadata:  metadata,
		Status:    models.MessageStatusSent,
		ReplyToID: replyToObjId,
	}

	if err := s.repo.SaveMessage(ctx, msg); err != nil {
		return nil, err
	}

	// Auto-advance to "delivered" if at least one recipient is currently online.
	// This avoids requiring the frontend to call PATCH /messages/:id/status manually.
	for _, p := range room.Participants {
		if p != senderID && s.hub.IsUserOnline(p.Hex()) {
			if err := s.repo.UpdateMessageStatus(ctx, msg.ID, models.MessageStatusDelivered); err == nil {
				msg.Status = models.MessageStatusDelivered
			}
			break
		}
	}

	// Update room last message + sender
	preview := getMessagePreview(msgType, content, metadata)
	if err := s.repo.UpdateRoomLastMessage(ctx, roomID, preview, msgType, senderID); err != nil {
		log.Printf("SendMessage: failed to update room last message for room %s: %v", roomIDStr, err)
	}

	// Increment unread count for everyone except the sender (room.Participants already in memory)
	if err := s.repo.IncrementUnreadCounts(ctx, roomID, room.Participants, senderIDStr); err != nil {
		log.Printf("SendMessage: failed to increment unread counts for room %s: %v", roomIDStr, err)
	}

	resp := s.buildMessageResponse(ctx, msg)

	userIDs := make([]string, len(room.Participants))
	for i, p := range room.Participants {
		userIDs[i] = p.Hex()
	}

	// Broadcast the new message to all participants
	_ = s.hub.SendToUsers(userIDs, models.WSMessage{
		Type:    "message",
		RoomID:  roomIDStr,
		Payload: resp,
	})

	// Broadcast room_updated so every participant's chat list re-orders in real time
	_ = s.hub.SendToUsers(userIDs, models.WSMessage{
		Type:   "room_updated",
		RoomID: roomIDStr,
		Payload: map[string]interface{}{
			"lastMessage":     preview,
			"lastMessageType": msgType,
			"lastUpdated":     msg.CreatedAt,
			"lastSenderId":    senderIDStr,
		},
	})

	return resp, nil
}

func (s *service) GetRoomMessages(ctx context.Context, userIDStr, roomIDStr string, limit int, beforeIDStr string) (*models.MessagesPage, error) {
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
	if limit > 100 {
		limit = 100
	}

	// Parse optional cursor: only messages with _id < beforeID are returned
	var beforeID *bson.ObjectID
	if beforeIDStr != "" {
		id, err := bson.ObjectIDFromHex(beforeIDStr)
		if err != nil {
			return nil, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	// Fetch one extra to determine hasMore without a separate COUNT query
	msgs, err := s.repo.GetMessagesByRoom(ctx, roomID, limit+1, beforeID)
	if err != nil {
		return nil, err
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	responses := make([]models.MessageResponse, 0, len(msgs))
	for _, m := range msgs {
		responses = append(responses, *s.buildMessageResponse(ctx, &m))
	}

	// Reverse from newest-first (DB order) to chronological for the client
	for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
		responses[i], responses[j] = responses[j], responses[i]
	}

	return &models.MessagesPage{
		Messages: responses,
		HasMore:  hasMore,
	}, nil
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
		resp, err := s.buildRoomResponse(ctx, &r, userIDStr)
		if err != nil {
			log.Printf("GetUserRooms: failed to build room response for room %s: %v", r.ID.Hex(), err)
			continue
		}
		responses = append(responses, *resp)
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
	wsMsg := models.WSMessage{
		Type:   "room_read",
		RoomID: roomIDStr,
		Payload: map[string]string{
			"readBy": userIDStr,
		},
	}
	var otherParticipants []string
	for _, p := range room.Participants {
		if pHex := p.Hex(); pHex != userIDStr {
			otherParticipants = append(otherParticipants, pHex)
		}
	}
	if len(otherParticipants) > 0 {
		_ = s.hub.SendToUsers(otherParticipants, wsMsg)
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

	if msg.Type != models.MessageTypeText && msg.Type != "" {
		return errors.New("only text messages can be edited")
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
	userID, err := bson.ObjectIDFromHex(userIDStr)
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

	// Ensure the caller is a participant in the room, not just any authenticated user
	room, err := s.repo.GetRoomByID(ctx, msg.RoomID)
	if err != nil {
		return errors.New("room not found")
	}
	if !isUserInRoom(room, userID) {
		return errors.New("unauthorized: not a participant in this room")
	}

	// Prevent status downgrade: sent < delivered < read
	statusRank := map[string]int{
		models.MessageStatusSent:      0,
		models.MessageStatusDelivered: 1,
		models.MessageStatusRead:      2,
	}
	if statusRank[status] <= statusRank[msg.Status] {
		// Already at this status or higher — idempotent, not an error
		return nil
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
	userID, err := bson.ObjectIDFromHex(userIDStr)
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

	// Ensure the caller is a participant in the room
	room, err := s.repo.GetRoomByID(ctx, msg.RoomID)
	if err != nil {
		return errors.New("room not found")
	}
	if !isUserInRoom(room, userID) {
		return errors.New("unauthorized: not a participant in this room")
	}

	if currentEmoji, exists := msg.Reactions[userIDStr]; exists && currentEmoji == emoji {
		emoji = ""
	}

	if err := s.repo.UpdateMessageReaction(ctx, messageID, userIDStr, emoji); err != nil {
		return err
	}

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

	return nil
}

// broadcastUserPresence notifies all unique participants across the user's rooms
// that the user has come online or gone offline. Called on WS connect/disconnect.
func (s *service) broadcastUserPresence(userID string, online bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return
	}

	rooms, err := s.repo.GetUserRooms(ctx, uid)
	if err != nil {
		log.Printf("broadcastUserPresence: failed to fetch rooms for user %s: %v", userID, err)
		return
	}

	// Collect unique participant IDs across all rooms, excluding the user themselves
	seen := make(map[string]bool)
	var recipients []string
	for _, room := range rooms {
		for _, p := range room.Participants {
			pHex := p.Hex()
			if pHex != userID && !seen[pHex] {
				seen[pHex] = true
				recipients = append(recipients, pHex)
			}
		}
	}

	if len(recipients) == 0 {
		return
	}

	eventType := "user_offline"
	if online {
		eventType = "user_online"
	}

	_ = s.hub.SendToUsers(recipients, models.WSMessage{
		Type: eventType,
		Payload: map[string]string{
			"userId": userID,
		},
	})
}

func (s *service) HandleWebSocket(c *websocket.Conn, userID string) {
	client := &clientContext{
		userID: userID,
		conn:   c,
		send:   make(chan []byte, sendBufSize),
	}

	s.hub.register <- client

	// Notify room participants that this user is now online
	go s.broadcastUserPresence(userID, true)

	defer func() {
		s.hub.MarkDisconnecting(userID) // Mark immediately so IsUserOnline returns false
		s.hub.unregister <- client
		// Notify room participants that this user has gone offline
		go s.broadcastUserPresence(userID, false)
	}()

	// Start ping/pong to detect dead connections quickly
	go s.pingPong(c, client)

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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			room, err := s.repo.GetRoomByID(ctx, roomID)
			cancel()
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
		ID:              room.ID.Hex(),
		Type:            room.Type,
		Name:            room.Name,
		LastMessage:     room.LastMessage,
		LastMessageType: room.LastMessageType,
		LastUpdated:     room.LastUpdated,
		Participants:    make([]models.ParticipantInfo, 0, len(room.Participants)),
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
	resp := &models.MessageResponse{
		ID:        msg.ID.Hex(),
		RoomID:    msg.RoomID.Hex(),
		SenderID:  msg.SenderID.Hex(),
		Type:      msg.Type,
		Content:   msg.Content,
		Metadata:  msg.Metadata,
		Status:    msg.Status,
		Reactions: msg.Reactions,
		IsEdited:  msg.IsEdited,
		IsDeleted: msg.IsDeleted,
		CreatedAt: msg.CreatedAt,
		UpdatedAt: msg.UpdatedAt,
	}

	if resp.Type == "" {
		resp.Type = models.MessageTypeText
	}

	// Populate sender display info so frontend does not need a separate user lookup
	if sender, err := s.userRepo.GetUserByID(ctx, msg.SenderID); err == nil && sender != nil {
		resp.SenderName = sender.DisplayName
		resp.SenderPhotoURL = sender.PhotoURL
	}

	// Populate reply-to message (one level deep only)
	if msg.ReplyToID != nil {
		if replyMsg, err := s.repo.GetMessageByID(ctx, *msg.ReplyToID); err == nil {
			replyResp := &models.MessageResponse{
				ID:        replyMsg.ID.Hex(),
				RoomID:    replyMsg.RoomID.Hex(),
				SenderID:  replyMsg.SenderID.Hex(),
				Type:      replyMsg.Type,
				Content:   replyMsg.Content,
				Metadata:  replyMsg.Metadata,
				Status:    replyMsg.Status,
				IsDeleted: replyMsg.IsDeleted,
				CreatedAt: replyMsg.CreatedAt,
			}
			if replyResp.Type == "" {
				replyResp.Type = models.MessageTypeText
			}
			if replySender, err := s.userRepo.GetUserByID(ctx, replyMsg.SenderID); err == nil && replySender != nil {
				replyResp.SenderName = replySender.DisplayName
				replyResp.SenderPhotoURL = replySender.PhotoURL
			}
			resp.ReplyTo = replyResp
		}
	}

	return resp
}

// pingPong sends periodic ping frames to detect dead connections.
// If the client doesn't respond with a pong within the timeout, the connection is closed.
func (s *service) pingPong(c *websocket.Conn, client *clientContext) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				// Failed to send ping, close the connection
				c.Close()
				return
			}
		case <-client.send:
			// Client disconnected, exit
			return
		}
	}
}

func validateMessageContent(msgType, content string) error {
	switch msgType {
	case models.MessageTypeText:
		if content == "" {
			return errors.New("message content cannot be empty")
		}
		if len([]rune(content)) > 2000 {
			return errors.New("message content exceeds maximum length of 2000 characters")
		}
	case models.MessageTypeImage, models.MessageTypeVideo, models.MessageTypeAudio, models.MessageTypeFile, models.MessageTypeGIF, models.MessageTypeLink:
		if !strings.HasPrefix(content, "https://") {
			return errors.New("media content must be a valid HTTPS URL")
		}
		if len([]rune(content)) > 2048 {
			return errors.New("media URL exceeds maximum length of 2048 characters")
		}
		u, err := url.Parse(content)
		if err != nil {
			return errors.New("invalid media URL")
		}
		host := u.Host

		// Ensure URL has a valid host
		if host == "" {
			return errors.New("URL must contain a valid host")
		}

		if msgType == models.MessageTypeGIF {
			isGiphy := host == "giphy.com" || strings.HasSuffix(host, ".giphy.com")
			isCloudinary := host == "res.cloudinary.com"
			if !isGiphy && !isCloudinary {
				return errors.New("domain not whitelisted for GIF")
			}
		} else if msgType != models.MessageTypeLink {
			if host != "res.cloudinary.com" {
				return errors.New("domain not whitelisted for media")
			}
		}
	default:
		return errors.New("invalid or unknown message type")
	}
	return nil
}

func getMessagePreview(msgType, content string, metadata *models.MediaMetadata) string {
	switch msgType {
	case models.MessageTypeText:
		return content
	case models.MessageTypeImage:
		return "📷 Photo"
	case models.MessageTypeVideo:
		return "🎥 Video"
	case models.MessageTypeAudio:
		return "🎵 Audio"
	case models.MessageTypeFile:
		if metadata != nil && metadata.FileName != "" {
			return "📎 " + metadata.FileName
		}
		return "📎 File"
	case models.MessageTypeGIF:
		return "GIF"
	case models.MessageTypeLink:
		return "🔗 Link"
	default:
		return "Message"
	}
}

package chat

import (
	"context"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	CreateRoom(ctx context.Context, room *models.Room) error
	GetRoomByID(ctx context.Context, roomID bson.ObjectID) (*models.Room, error)
	GetDirectRoom(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error)
	GetOrCreateDirectRoomAtomic(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error)
	GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error)

	SaveMessage(ctx context.Context, msg *models.Message) error
	GetMessageByID(ctx context.Context, messageID bson.ObjectID) (*models.Message, error)
	// GetMessagesByRoom returns up to limit messages in the room.
	// If beforeID is non-nil, only messages with _id < beforeID are returned
	// (cursor-based / keyset pagination — O(1) regardless of page depth).
	GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Message, error)
	UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage, lastMessageType string, senderID bson.ObjectID) error
	UpdateMessageStatus(ctx context.Context, messageID bson.ObjectID, status string) error
	UpdateMessageReaction(ctx context.Context, messageID bson.ObjectID, userID, emoji string) error
	UpdateMessageContent(ctx context.Context, messageID bson.ObjectID, content string) error
	SoftDeleteMessage(ctx context.Context, messageID bson.ObjectID) error

	// Unread count management
	// IncrementUnreadCounts bumps the unread counter for every participant except exceptUserID.
	// Callers must pass the already-known participants slice to avoid a redundant DB fetch.
	IncrementUnreadCounts(ctx context.Context, roomID bson.ObjectID, participants []bson.ObjectID, exceptUserID string) error
	ResetUnreadCount(ctx context.Context, roomID bson.ObjectID, userID string) error
	MarkRoomMessagesAsRead(ctx context.Context, roomID, senderID bson.ObjectID) error
}

type repository struct {
	db       *mongo.Database
	rooms    *mongo.Collection
	messages *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		db:       db,
		rooms:    db.Collection("chat_rooms"),
		messages: db.Collection("chat_messages"),
	}
}

func (r *repository) CreateRoom(ctx context.Context, room *models.Room) error {
	room.CreatedAt = time.Now()
	room.LastUpdated = time.Now()
	if room.UnreadCounts == nil {
		room.UnreadCounts = make(map[string]int)
	}

	res, err := r.rooms.InsertOne(ctx, room)
	if err != nil {
		return err
	}
	room.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetRoomByID(ctx context.Context, roomID bson.ObjectID) (*models.Room, error) {
	var room models.Room
	if err := r.rooms.FindOne(ctx, bson.M{"_id": roomID}).Decode(&room); err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *repository) GetDirectRoom(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
	var room models.Room
	filter := bson.M{
		"type": models.RoomTypeDirect,
		"participants": bson.M{
			"$all":  []bson.ObjectID{user1ID, user2ID},
			"$size": 2,
		},
	}

	err := r.rooms.FindOne(ctx, filter).Decode(&room)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &room, nil
}

// GetOrCreateDirectRoomAtomic finds or atomically creates a direct room between two users.
// Using FindOneAndUpdate with upsert eliminates the check-then-insert race condition.
func (r *repository) GetOrCreateDirectRoomAtomic(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
	// First, try to find the existing direct room
	room, err := r.GetDirectRoom(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}
	if room != nil {
		return room, nil
	}

	// Create a new direct room if it doesn't exist
	newRoom := &models.Room{
		Type:         models.RoomTypeDirect,
		Participants: []bson.ObjectID{user1ID, user2ID},
		UnreadCounts: map[string]int{
			user1ID.Hex(): 0,
			user2ID.Hex(): 0,
		},
	}

	if err := r.CreateRoom(ctx, newRoom); err != nil {
		return nil, err
	}

	return newRoom, nil
}

func (r *repository) GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error) {
	filter := bson.M{"participants": userID}
	opts := options.Find().SetSort(bson.D{{Key: "lastUpdated", Value: -1}})

	cursor, err := r.rooms.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rooms []models.Room
	if err = cursor.All(ctx, &rooms); err != nil {
		return nil, err
	}

	return rooms, nil
}

func (r *repository) SaveMessage(ctx context.Context, msg *models.Message) error {
	msg.CreatedAt = time.Now()
	res, err := r.messages.InsertOne(ctx, msg)
	if err != nil {
		return err
	}
	msg.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Message, error) {
	filter := bson.M{"roomId": roomID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.messages.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var msgs []models.Message
	if err = cursor.All(ctx, &msgs); err != nil {
		return nil, err
	}

	return msgs, nil
}

func (r *repository) UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage, lastMessageType string, senderID bson.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"lastMessage":         lastMessage,
			"lastMessageType":     lastMessageType,
			"lastMessageSenderId": senderID,
			"lastUpdated":         time.Now(),
		},
	}
	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, update)
	return err
}

func (r *repository) GetMessageByID(ctx context.Context, messageID bson.ObjectID) (*models.Message, error) {
	var msg models.Message
	if err := r.messages.FindOne(ctx, bson.M{"_id": messageID}).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (r *repository) UpdateMessageStatus(ctx context.Context, messageID bson.ObjectID, status string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) UpdateMessageReaction(ctx context.Context, messageID bson.ObjectID, userID, emoji string) error {
	var update bson.M
	if emoji == "" {
		update = bson.M{
			"$unset": bson.M{"reactions." + userID: ""},
			"$set":   bson.M{"updatedAt": time.Now()},
		}
	} else {
		update = bson.M{
			"$set": bson.M{
				"reactions." + userID: emoji,
				"updatedAt":           time.Now(),
			},
		}
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) UpdateMessageContent(ctx context.Context, messageID bson.ObjectID, content string) error {
	update := bson.M{
		"$set": bson.M{
			"content":   content,
			"isEdited":  true,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) SoftDeleteMessage(ctx context.Context, messageID bson.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"content":   "This message was deleted",
			"isDeleted": true,
			"updatedAt": time.Now(),
		},
		// Clear reactions and metadata — deleted messages should not show media or emojis
		"$unset": bson.M{
			"reactions": "",
			"metadata":  "",
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

// IncrementUnreadCounts bumps unread count for all participants EXCEPT the sender.
// participants is passed in by the caller — no extra DB fetch needed.
func (r *repository) IncrementUnreadCounts(ctx context.Context, roomID bson.ObjectID, participants []bson.ObjectID, exceptUserID string) error {
	incMap := bson.M{}
	for _, p := range participants {
		if hex := p.Hex(); hex != exceptUserID {
			incMap["unreadCounts."+hex] = 1
		}
	}

	if len(incMap) == 0 {
		return nil
	}

	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, bson.M{"$inc": incMap})
	return err
}

// ResetUnreadCount sets a user's unread count back to 0
func (r *repository) ResetUnreadCount(ctx context.Context, roomID bson.ObjectID, userID string) error {
	update := bson.M{
		"$set": bson.M{
			"unreadCounts." + userID: 0,
		},
	}
	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, update)
	return err
}

// MarkRoomMessagesAsRead marks all messages from other senders as "read" in bulk
func (r *repository) MarkRoomMessagesAsRead(ctx context.Context, roomID, readerID bson.ObjectID) error {
	filter := bson.M{
		"roomId":   roomID,
		"senderId": bson.M{"$ne": readerID}, // Only mark messages from OTHER users
		"status":   bson.M{"$ne": models.MessageStatusRead},
	}
	update := bson.M{
		"$set": bson.M{
			"status":    models.MessageStatusRead,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateMany(ctx, filter, update)
	return err
}

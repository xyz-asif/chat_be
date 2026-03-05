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
	GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error)

	SaveMessage(ctx context.Context, msg *models.Message) error
	GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit, offset int) ([]models.Message, error)
	UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage string) error
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

	// A direct room must have exactly these two participants
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
			return nil, nil // Not found but not an error
		}
		return nil, err
	}
	return &room, nil
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

// SaveMessage stores a new message in the DB
func (r *repository) SaveMessage(ctx context.Context, msg *models.Message) error {
	msg.CreatedAt = time.Now()
	res, err := r.messages.InsertOne(ctx, msg)
	if err != nil {
		return err
	}
	msg.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit, offset int) ([]models.Message, error) {
	filter := bson.M{"roomId": roomID}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}). // Newest first
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

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

func (r *repository) UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage string) error {
	update := bson.M{
		"$set": bson.M{
			"lastMessage": lastMessage,
			"lastUpdated": time.Now(),
		},
	}
	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, update)
	return err
}

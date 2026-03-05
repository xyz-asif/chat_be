package database

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CreateIndexes creates all necessary indexes for the Chat platform
func CreateIndexes(ctx context.Context, db *mongo.Database) error {
	log.Println("Creating MongoDB indexes...")

	// ── Users ──
	usersIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "firebaseUid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
		// Text index for user search
		{Keys: bson.D{{Key: "displayName", Value: "text"}, {Key: "email", Value: "text"}}},
	}
	if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersIndexes); err != nil {
		log.Printf("Warning: Users index creation issue: %v", err)
	}

	// ── Follows ──
	followsIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "followedUserId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "followedUserId", Value: 1}}},
	}
	if _, err := db.Collection("follows").Indexes().CreateMany(ctx, followsIndexes); err != nil {
		log.Printf("Warning: Follows index creation issue: %v", err)
	}

	// ── Connections ──
	connectionsIndexes := []mongo.IndexModel{
		// Fast lookup: does a connection exist between these two users?
		{Keys: bson.D{{Key: "senderId", Value: 1}, {Key: "receiverId", Value: 1}}, Options: options.Index().SetUnique(true)},
		// Fast lookup: all connections for a given user (pending, friends list)
		{Keys: bson.D{{Key: "receiverId", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "senderId", Value: 1}, {Key: "status", Value: 1}}},
	}
	if _, err := db.Collection("connections").Indexes().CreateMany(ctx, connectionsIndexes); err != nil {
		log.Printf("Warning: Connections index creation issue: %v", err)
	}

	// ── Chat Rooms ──
	roomsIndexes := []mongo.IndexModel{
		// Fast lookup: all rooms a user participates in, sorted by last activity
		{Keys: bson.D{{Key: "participants", Value: 1}, {Key: "lastUpdated", Value: -1}}},
		// Fast lookup: find existing direct room between two people
		{Keys: bson.D{{Key: "type", Value: 1}, {Key: "participants", Value: 1}}},
	}
	if _, err := db.Collection("chat_rooms").Indexes().CreateMany(ctx, roomsIndexes); err != nil {
		log.Printf("Warning: Chat rooms index creation issue: %v", err)
	}

	// ── Chat Messages ──
	messagesIndexes := []mongo.IndexModel{
		// Fast lookup: messages in a room sorted by time (the primary query)
		{Keys: bson.D{{Key: "roomId", Value: 1}, {Key: "createdAt", Value: -1}}},
	}
	if _, err := db.Collection("chat_messages").Indexes().CreateMany(ctx, messagesIndexes); err != nil {
		log.Printf("Warning: Chat messages index creation issue: %v", err)
	}

	log.Println("✅ All MongoDB indexes created successfully")
	return nil
}

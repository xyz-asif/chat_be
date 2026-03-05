package database

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MVP Launch: MongoDB Indexes - Completed
// CreateIndexes creates all necessary indexes for the Chat platform
func CreateIndexes(ctx context.Context, db *mongo.Database) error {
	log.Println("Creating MongoDB indexes...")

	// Users collection indexes
	usersIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "firebaseUid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
	}
	if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersIndexes); err != nil {
		return err
	}

	// User Follows collection indexes
	followsIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "followedUserId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "followedUserId", Value: 1}}},
	}
	if _, err := db.Collection("follows").Indexes().CreateMany(ctx, followsIndexes); err != nil {
		return err
	}

	log.Println("✅ All MongoDB indexes created successfully")
	return nil
}

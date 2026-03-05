package users

import (
	"context"
	"errors"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByFirebaseUID(ctx context.Context, uid string) (*models.User, error)
	GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
	UpdateUser(ctx context.Context, id bson.ObjectID, updates map[string]interface{}) error // MVP Feature: User Profile Management
	IncrementProfileViews(ctx context.Context, userID bson.ObjectID) error                  // MVP Feature: User Profile Management
	FollowUser(ctx context.Context, followerID, followedID bson.ObjectID) error
	UnfollowUser(ctx context.Context, followerID, followedID bson.ObjectID) error
	SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error)
}

type repository struct {
	db          *mongo.Database
	client      *mongo.Client // MVP Launch: Transaction support
	collection  *mongo.Collection
	followsColl *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		db:          db,
		client:      db.Client(), // MVP Launch: Transaction support
		collection:  db.Collection("users"),
		followsColl: db.Collection("follows"),
	}
}

func (r *repository) CreateUser(ctx context.Context, user *models.User) error {
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	user.LastLoginAt = time.Now()
	user.IsActive = true
	user.Stats = models.UserStats{}

	res, err := r.collection.InsertOne(ctx, user)
	if err != nil {
		return err
	}
	user.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetUserByFirebaseUID(ctx context.Context, uid string) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"firebaseUid": uid}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil // Return nil if not found
		}
		return nil, err
	}
	return &user, nil
}

func (r *repository) GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// MVP Feature: User Profile Management – Completed
func (r *repository) UpdateUser(ctx context.Context, id bson.ObjectID, updates map[string]interface{}) error {
	updates["updatedAt"] = time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updates})
	return err
}

// MVP Feature: User Profile Management – Completed
func (r *repository) IncrementProfileViews(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{"$inc": bson.M{"stats.totalProfileViews": 1}},
	)
	return err
}

func (r *repository) GetFollowedUsers(ctx context.Context, userID bson.ObjectID) ([]bson.ObjectID, error) {
	cursor, err := r.followsColl.Find(ctx, bson.M{"followerId": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var follows []models.Follow
	if err := cursor.All(ctx, &follows); err != nil {
		return nil, err
	}

	var followedIDs []bson.ObjectID
	for _, f := range follows {
		followedIDs = append(followedIDs, f.FollowedUserID)
	}
	return followedIDs, nil
}

// MVP Launch: User-to-User Follow System - Completed
func (r *repository) FollowUser(ctx context.Context, followerID, followedUserID bson.ObjectID) error {
	// Check if already following
	count, err := r.followsColl.CountDocuments(ctx, bson.M{"followerId": followerID, "followedUserId": followedUserID})
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("already following")
	}

	// Execute in transaction
	session, err := r.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx context.Context) (interface{}, error) {
		// 1. Insert follow
		follow := models.Follow{FollowerID: followerID, FollowedUserID: followedUserID, CreatedAt: time.Now()}
		_, err := r.followsColl.InsertOne(sessCtx, follow)
		if err != nil {
			return nil, err
		}

		// 2. Increment follower's following count
		_, err = r.collection.UpdateOne(sessCtx, bson.M{"_id": followerID}, bson.M{"$inc": bson.M{"stats.followingCount": 1}})
		if err != nil {
			return nil, err
		}

		// 3. Increment followed user's followers count
		_, err = r.collection.UpdateOne(sessCtx, bson.M{"_id": followedUserID}, bson.M{"$inc": bson.M{"stats.followersCount": 1}})
		if err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

// MVP Launch: User-to-User Follow System - Completed
func (r *repository) UnfollowUser(ctx context.Context, followerID, followedUserID bson.ObjectID) error {
	session, err := r.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx context.Context) (interface{}, error) {
		// 1. Delete follow
		res, err := r.followsColl.DeleteOne(sessCtx, bson.M{"followerId": followerID, "followedUserId": followedUserID})
		if err != nil {
			return nil, err
		}
		if res.DeletedCount == 0 {
			return nil, errors.New("not following")
		}

		// 2. Decrement follower's following count
		_, err = r.collection.UpdateOne(sessCtx, bson.M{"_id": followerID}, bson.M{"$inc": bson.M{"stats.followingCount": -1}})
		if err != nil {
			return nil, err
		}

		// 3. Decrement followed user's followers count
		_, err = r.collection.UpdateOne(sessCtx, bson.M{"_id": followedUserID}, bson.M{"$inc": bson.M{"stats.followersCount": -1}})
		if err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

func (r *repository) SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error) {
	// Create a case-insensitive regex search on email or displayName
	filter := bson.M{
		"$or": []bson.M{
			{"displayName": bson.M{"$regex": query, "$options": "i"}},
			{"email": bson.M{"$regex": query, "$options": "i"}},
		},
		"isActive": true, // Only return active users
	}

	importOptions := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(offset)).
		SetSort(bson.D{{Key: "displayName", Value: 1}}) // Sort alphabetically

	cursor, err := r.collection.Find(ctx, filter, importOptions)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}

package users

import (
	"context"
	"errors"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	GetOrCreateUser(ctx context.Context, firebaseUID, email, displayName, photoURL string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error)
	FollowUser(ctx context.Context, followerID, followedUserID string) error
	UnfollowUser(ctx context.Context, followerID, followedUserID string) error
	SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error)
	GetFeed(ctx context.Context, userID string) ([]interface{}, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{
		repo: repo,
	}
}

// MVP Feature: Authentication - Completed
func (s *service) GetOrCreateUser(ctx context.Context, uid, email, name, photoURL string) (*models.User, error) {
	user, err := s.repo.GetUserByFirebaseUID(ctx, uid)
	if err != nil {
		return nil, err
	}

	// User doesn't exist, create new
	if user == nil {
		newUser := &models.User{
			FirebaseUID: uid,
			Email:       email,
			DisplayName: name,
			PhotoURL:    photoURL,
		}
		if err := s.repo.CreateUser(ctx, newUser); err != nil {
			return nil, err
		}
		return newUser, nil
	}
	return user, nil
}

// MVP Launch: Get user by ID
func (s *service) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	uID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetUserByID(ctx, uID)
}

// MVP Feature: User Profile Management - Completed
func (s *service) UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error) {
	uID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	// Validate allowed fields
	allowedFields := map[string]bool{
		"displayName": true,
		"photoURL":    true,
		"bio":         true,
		"preferences": true,
	}

	filteredUpdates := make(map[string]interface{})
	for key, value := range updates {
		if allowedFields[key] {
			filteredUpdates[key] = value
		}
	}

	if len(filteredUpdates) == 0 {
		return nil, errors.New("no valid fields to update")
	}

	if err := s.repo.UpdateUser(ctx, uID, filteredUpdates); err != nil {
		return nil, err
	}

	return s.repo.GetUserByID(ctx, uID)
}

// MVP Launch: User-to-User Follow System - Completed
func (s *service) FollowUser(ctx context.Context, followerID, followedUserID string) error {
	fID, err := bson.ObjectIDFromHex(followerID)
	if err != nil {
		return err
	}
	targetID, err := bson.ObjectIDFromHex(followedUserID)
	if err != nil {
		return err
	}

	if fID == targetID {
		return errors.New("cannot follow yourself")
	}

	// Follow user
	if err := s.repo.FollowUser(ctx, fID, targetID); err != nil {
		return err
	}

	return nil
}

// MVP Launch: User-to-User Follow System - Completed
func (s *service) UnfollowUser(ctx context.Context, followerID, followedUserID string) error {
	fID, err := bson.ObjectIDFromHex(followerID)
	if err != nil {
		return err
	}
	targetID, err := bson.ObjectIDFromHex(followedUserID)
	if err != nil {
		return err
	}

	return s.repo.UnfollowUser(ctx, fID, targetID)
}

func (s *service) SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	if query == "" {
		return []models.User{}, nil
	}

	return s.repo.SearchUsers(ctx, query, limit, offset)
}

// Placeholder for feed
func (s *service) GetFeed(ctx context.Context, userID string) ([]interface{}, error) {
	return []interface{}{}, nil
}

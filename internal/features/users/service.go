package users

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// HubSender defines the interface for sending WebSocket messages
type HubSender interface {
	SendToUsers(userIDs []string, msg models.WSMessage) error
}

type Service interface {
	GetOrCreateUser(ctx context.Context, firebaseUID, email, displayName, photoURL string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	GetUsersByIDs(ctx context.Context, userIDs []string) (map[string]*models.User, error)
	UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error)
	FollowUser(ctx context.Context, followerID, followedUserID string) error
	UnfollowUser(ctx context.Context, followerID, followedUserID string) error
	SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error)
	GetFeed(ctx context.Context, userID string) ([]interface{}, error)
}

type service struct {
	repo        Repository
	hub         HubSender
	connRepo    ConnectionRepository
	chatRepo    ChatRepository
}

type ConnectionRepository interface {
	GetUserConnections(ctx context.Context, userID bson.ObjectID, status string) ([]models.Connection, error)
}

type ChatRepository interface {
	GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error)
}

func NewService(repo Repository, hub HubSender, connRepo ConnectionRepository, chatRepo ChatRepository) Service {
	return &service{
		repo:        repo,
		hub:         hub,
		connRepo:    connRepo,
		chatRepo:    chatRepo,
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

// GetUsersByIDs fetches multiple users by their string IDs in a single batch
func (s *service) GetUsersByIDs(ctx context.Context, userIDs []string) (map[string]*models.User, error) {
	// Convert string IDs to ObjectIDs
	objectIDs := make([]bson.ObjectID, len(userIDs))
	for i, idStr := range userIDs {
		id, err := bson.ObjectIDFromHex(idStr)
		if err != nil {
			return nil, errors.New("invalid user ID: "+idStr)
		}
		objectIDs[i] = id
	}

	// Fetch users in batch
	userMap, err := s.repo.GetUsersByIDs(ctx, objectIDs)
	if err != nil {
		return nil, err
	}

	// Convert back to string map for frontend compatibility
	result := make(map[string]*models.User)
	for objID, user := range userMap {
		result[objID.Hex()] = user
	}

	return result, nil
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

	updatedUser, err := s.repo.GetUserByID(ctx, uID)
	if err != nil {
		return nil, err
	}

	// Broadcast profile update to friends and chat participants asynchronously
	// This doesn't affect the API response - fire and forget
	go s.broadcastProfileUpdate(userID, filteredUpdates, updatedUser)

	return updatedUser, nil
}

// broadcastProfileUpdate sends profile changes to all users who have a connection or chat with this user
func (s *service) broadcastProfileUpdate(userID string, updates map[string]interface{}, user *models.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return
	}

	// Collect unique recipient IDs
	recipients := make(map[string]bool)

	// 1. Get all friends (connections) with accepted status
	if s.connRepo != nil {
		connections, err := s.connRepo.GetUserConnections(ctx, uid, models.ConnectionStatusAccepted)
		if err != nil {
			log.Printf("broadcastProfileUpdate: failed to get friends for user %s: %v", userID, err)
		} else {
			for _, conn := range connections {
				// Determine the other user in the connection
				var friendID string
				if conn.SenderID == uid {
					friendID = conn.ReceiverID.Hex()
				} else {
					friendID = conn.SenderID.Hex()
				}
				if friendID != userID {
					recipients[friendID] = true
				}
			}
		}
	}

	// 2. Get all chat room participants
	if s.chatRepo != nil {
		rooms, err := s.chatRepo.GetUserRooms(ctx, uid)
		if err != nil {
			log.Printf("broadcastProfileUpdate: failed to get rooms for user %s: %v", userID, err)
		} else {
			for _, room := range rooms {
				for _, p := range room.Participants {
					pHex := p.Hex()
					if pHex != userID {
						recipients[pHex] = true
					}
				}
			}
		}
	}

	// If no recipients, nothing to broadcast
	if len(recipients) == 0 {
		return
	}

	// Convert map to slice
	recipientList := make([]string, 0, len(recipients))
	for id := range recipients {
		recipientList = append(recipientList, id)
	}

	// Build payload with only changed fields + user ID
	payload := map[string]interface{}{
		"userId": userID,
	}
	for key, value := range updates {
		payload[key] = value
	}
	// Always include current displayName and photoURL for consistency
	payload["displayName"] = user.DisplayName
	payload["photoURL"] = user.PhotoURL

	// Send WebSocket message
	if s.hub != nil {
		_ = s.hub.SendToUsers(recipientList, models.WSMessage{
			Type:    "profile_updated",
			Payload: payload,
		})
	}
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

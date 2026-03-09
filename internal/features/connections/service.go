package connections

import (
	"context"
	"errors"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	SendRequest(ctx context.Context, senderID, receiverID string) (*models.Connection, error)
	AcceptRequest(ctx context.Context, userID, connectionID string) error
	RejectRequest(ctx context.Context, userID, connectionID string) error
	GetPendingRequests(ctx context.Context, userID string) ([]models.Connection, error)
	GetFriendsList(ctx context.Context, userID string) ([]models.Connection, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{
		repo: repo,
	}
}

func (s *service) SendRequest(ctx context.Context, senderIDStr, receiverIDStr string) (*models.Connection, error) {
	senderID, err := bson.ObjectIDFromHex(senderIDStr)
	if err != nil {
		return nil, errors.New("invalid sender id")
	}
	receiverID, err := bson.ObjectIDFromHex(receiverIDStr)
	if err != nil {
		return nil, errors.New("invalid receiver id")
	}

	if senderID == receiverID {
		return nil, errors.New("cannot send request to yourself")
	}

	// Check if connection already exists
	existingConn, err := s.repo.GetConnectionBetweenUsers(ctx, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	if existingConn != nil {
		if existingConn.Status == models.ConnectionStatusPending {
			return nil, errors.New("request already pending")
		}
		if existingConn.Status == models.ConnectionStatusAccepted {
			return nil, errors.New("already connected")
		}
		if existingConn.Status == models.ConnectionStatusBlocked {
			return nil, errors.New("cannot send request")
		}

		// If rejected, update both status and direction in a single DB write
		if existingConn.Status == models.ConnectionStatusRejected {
			// Ensure the new sender is the one initiating again (might have been rejected by the other party)
			if err := s.repo.UpdateConnectionDirection(ctx, existingConn.ID, senderID, receiverID); err != nil {
				return nil, err
			}
			existingConn.Status = models.ConnectionStatusPending
			existingConn.SenderID = senderID
			existingConn.ReceiverID = receiverID
			return existingConn, nil
		}
	}

	// Create new connection
	conn := &models.Connection{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Status:     models.ConnectionStatusPending,
	}

	if err := s.repo.CreateConnection(ctx, conn); err != nil {
		return nil, err
	}

	return conn, nil
}

func (s *service) AcceptRequest(ctx context.Context, userIDStr, connectionIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}

	// Ensure the user accepting is the receiver
	if conn.ReceiverID != userID {
		return errors.New("unauthorized to accept this request")
	}

	if conn.Status != models.ConnectionStatusPending {
		return errors.New("request is not pending")
	}

	return s.repo.UpdateConnectionStatus(ctx, connID, models.ConnectionStatusAccepted)
}

func (s *service) RejectRequest(ctx context.Context, userIDStr, connectionIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}

	// Ensure the user rejecting is the receiver
	if conn.ReceiverID != userID {
		return errors.New("unauthorized to reject this request")
	}

	if conn.Status != models.ConnectionStatusPending {
		return errors.New("request is not pending")
	}

	return s.repo.UpdateConnectionStatus(ctx, connID, models.ConnectionStatusRejected)
}

func (s *service) GetPendingRequests(ctx context.Context, userIDStr string) ([]models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	// Get all connections where this user is involved
	allConns, err := s.repo.GetUserConnections(ctx, userID, models.ConnectionStatusPending)
	if err != nil {
		return nil, err
	}

	// Filter to only requests WHERE the user is the RECEIVER
	var pendingRequests []models.Connection
	for _, conn := range allConns {
		if conn.ReceiverID == userID {
			pendingRequests = append(pendingRequests, conn)
		}
	}

	return pendingRequests, nil
}

func (s *service) GetFriendsList(ctx context.Context, userIDStr string) ([]models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	return s.repo.GetUserConnections(ctx, userID, models.ConnectionStatusAccepted)
}

package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Connection types/statuses
const (
	ConnectionStatusPending  = "pending"
	ConnectionStatusAccepted = "accepted"
	ConnectionStatusRejected = "rejected"
	ConnectionStatusBlocked  = "blocked"
)

// Connection represents a relationship or chat request between two users
type Connection struct {
	ID                  bson.ObjectID `bson:"_id,omitempty" json:"id"`
	SenderID            bson.ObjectID `bson:"senderId" json:"senderId"`
	ReceiverID          bson.ObjectID `bson:"receiverId" json:"receiverId"`
	Status              string        `bson:"status" json:"status"` // pending, accepted, rejected, blocked
	SenderDisplayName   string        `bson:"-" json:"senderDisplayName,omitempty"`
	SenderEmail         string        `bson:"-" json:"senderEmail,omitempty"`
	SenderPhotoURL      string        `bson:"-" json:"senderPhotoUrl,omitempty"`
	ReceiverDisplayName string        `bson:"-" json:"receiverDisplayName,omitempty"`
	ReceiverEmail       string        `bson:"-" json:"receiverEmail,omitempty"`
	ReceiverPhotoURL    string        `bson:"-" json:"receiverPhotoUrl,omitempty"`
	CreatedAt           time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt           time.Time     `bson:"updatedAt" json:"updatedAt"`
}

// ConnectionResponse is used for sending connection data with user details to the frontend
type ConnectionResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	User      User      `json:"user"` // The *other* user in the connection (sender or receiver)
	CreatedAt time.Time `json:"createdAt"`
}

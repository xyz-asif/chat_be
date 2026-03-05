package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Room Type constants
const (
	RoomTypeDirect = "direct"
	RoomTypeGroup  = "group"
)

// Message Status constants
const (
	MessageStatusSent      = "sent"
	MessageStatusDelivered = "delivered"
	MessageStatusRead      = "read"
)

// Room represents a chat conversation container
type Room struct {
	ID           bson.ObjectID   `bson:"_id,omitempty" json:"id"`
	Type         string          `bson:"type" json:"type"`                     // "direct" or "group"
	Name         string          `bson:"name,omitempty" json:"name,omitempty"` // For groups
	Participants []bson.ObjectID `bson:"participants" json:"participants"`
	LastMessage  string          `bson:"lastMessage,omitempty" json:"lastMessage,omitempty"`
	LastUpdated  time.Time       `bson:"lastUpdated" json:"lastUpdated"`
	CreatedAt    time.Time       `bson:"createdAt" json:"createdAt"`
}

// RoomResponse is used to send room details with populated participant info to the client
type RoomResponse struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Name         string    `json:"name,omitempty"`
	Participants []User    `json:"participants"`
	LastMessage  string    `json:"lastMessage,omitempty"`
	LastUpdated  time.Time `json:"lastUpdated"`
}

// Message represents an individual chat message
type Message struct {
	ID        bson.ObjectID     `bson:"_id,omitempty" json:"id"`
	RoomID    bson.ObjectID     `bson:"roomId" json:"roomId"`
	SenderID  bson.ObjectID     `bson:"senderId" json:"senderId"`
	Content   string            `bson:"content" json:"content"`
	Status    string            `bson:"status" json:"status"`                           // sent, delivered, read
	Reactions map[string]string `bson:"reactions,omitempty" json:"reactions,omitempty"` // map[userID]emoji
	ReplyToID *bson.ObjectID    `bson:"replyToId,omitempty" json:"replyToId,omitempty"`
	CreatedAt time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time         `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

// MessageResponse is used to send formatted message objects to clients
type MessageResponse struct {
	ID        string            `json:"id"`
	RoomID    string            `json:"roomId"`
	SenderID  string            `json:"senderId"`
	Content   string            `json:"content"`
	Status    string            `json:"status"`
	Reactions map[string]string `json:"reactions,omitempty"`
	ReplyTo   *MessageResponse  `json:"replyTo,omitempty"` // Optional nested reply
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt,omitempty"`
}

// WSMessage represents the payload sent/received over the WebSocket connection
type WSMessage struct {
	Type    string      `json:"type"`              // e.g., "message", "typing_start", "message_read"
	RoomID  string      `json:"roomId,omitempty"`  // The target room
	Payload interface{} `json:"payload,omitempty"` // Type-specific payload
}

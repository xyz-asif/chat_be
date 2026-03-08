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

// Message Type constants
const (
	MessageTypeText  = "text"
	MessageTypeImage = "image"
	MessageTypeVideo = "video"
	MessageTypeAudio = "audio"
	MessageTypeFile  = "file"
	MessageTypeGIF   = "gif"
	MessageTypeLink  = "link"
)

// MediaMetadata contains extra information for media messages
type MediaMetadata struct {
	MimeType     string `bson:"mimeType,omitempty" json:"mimeType,omitempty"`
	FileName     string `bson:"fileName,omitempty" json:"fileName,omitempty"`
	FileSize     int64  `bson:"fileSize,omitempty" json:"fileSize,omitempty"`
	ThumbnailURL string `bson:"thumbnailURL,omitempty" json:"thumbnailURL,omitempty"`
	Duration     int    `bson:"duration,omitempty" json:"duration,omitempty"`
	Width        int    `bson:"width,omitempty" json:"width,omitempty"`
	Height       int    `bson:"height,omitempty" json:"height,omitempty"`
}

// Room represents a chat conversation container
type Room struct {
	ID                  bson.ObjectID   `bson:"_id,omitempty" json:"id"`
	Type                string          `bson:"type" json:"type"`                     // "direct" or "group"
	Name                string          `bson:"name,omitempty" json:"name,omitempty"` // For groups
	Participants        []bson.ObjectID `bson:"participants" json:"participants"`
	LastMessage         string          `bson:"lastMessage,omitempty" json:"lastMessage,omitempty"`
	LastMessageType     string          `bson:"lastMessageType,omitempty" json:"lastMessageType,omitempty"`
	LastMessageSenderID *bson.ObjectID  `bson:"lastMessageSenderId,omitempty" json:"lastMessageSenderId,omitempty"`
	UnreadCounts        map[string]int  `bson:"unreadCounts,omitempty" json:"unreadCounts,omitempty"` // map[userIDHex]count
	LastUpdated         time.Time       `bson:"lastUpdated" json:"lastUpdated"`
	CreatedAt           time.Time       `bson:"createdAt" json:"createdAt"`
}

// ParticipantInfo is used in RoomResponse to include user details + online status
type ParticipantInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	PhotoURL    string `json:"photoURL,omitempty"`
	Email       string `json:"email"`
	IsOnline    bool   `json:"isOnline"`
}

// RoomResponse is used to send room details with populated participant info to the client
type RoomResponse struct {
	ID                    string            `json:"id"`
	Type                  string            `json:"type"`
	Name                  string            `json:"name,omitempty"`
	Participants          []ParticipantInfo `json:"participants"`
	LastMessage           string            `json:"lastMessage,omitempty"`
	LastMessageType       string            `json:"lastMessageType,omitempty"`
	LastMessageSenderName string            `json:"lastMessageSenderName,omitempty"` // e.g. "Alice"
	UnreadCount           int               `json:"unreadCount"`
	LastUpdated           time.Time         `json:"lastUpdated"`
}

// Message represents an individual chat message
type Message struct {
	ID        bson.ObjectID     `bson:"_id,omitempty" json:"id"`
	RoomID    bson.ObjectID     `bson:"roomId" json:"roomId"`
	SenderID  bson.ObjectID     `bson:"senderId" json:"senderId"`
	Type      string            `bson:"type" json:"type"`
	Content   string            `bson:"content" json:"content"`
	Metadata  *MediaMetadata    `bson:"metadata,omitempty" json:"metadata,omitempty"`
	Status    string            `bson:"status" json:"status"`                           // sent, delivered, read
	Reactions map[string]string `bson:"reactions,omitempty" json:"reactions,omitempty"` // map[userID]emoji
	ReplyToID *bson.ObjectID    `bson:"replyToId,omitempty" json:"replyToId,omitempty"`
	IsEdited  bool              `bson:"isEdited,omitempty" json:"isEdited,omitempty"`
	IsDeleted bool              `bson:"isDeleted,omitempty" json:"isDeleted,omitempty"`
	CreatedAt time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time         `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

// MessageResponse is used to send formatted message objects to clients
type MessageResponse struct {
	ID             string            `json:"id"`
	RoomID         string            `json:"roomId"`
	SenderID       string            `json:"senderId"`
	SenderName     string            `json:"senderName,omitempty"`
	SenderPhotoURL string            `json:"senderPhotoURL,omitempty"`
	Type           string            `json:"type"`
	Content        string            `json:"content"`
	Metadata       *MediaMetadata    `json:"metadata,omitempty"`
	Status         string            `json:"status"`
	Reactions      map[string]string `json:"reactions,omitempty"`
	ReplyTo        *MessageResponse  `json:"replyTo,omitempty"` // Optional nested reply
	IsEdited       bool              `json:"isEdited,omitempty"`
	IsDeleted      bool              `json:"isDeleted,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt,omitempty"`
}

// MessagesPage is the paginated response for GetRoomMessages.
// Cursor-based: pass the ID of the oldest message in the current page
// as the next request's `before` parameter to load the previous page.
type MessagesPage struct {
	Messages []MessageResponse `json:"messages"`
	HasMore  bool              `json:"hasMore"`
}

// WSMessage represents the payload sent/received over the WebSocket connection
type WSMessage struct {
	Type    string      `json:"type"`              // e.g., "message", "typing_start", "message_read"
	RoomID  string      `json:"roomId,omitempty"`  // The target room
	Payload interface{} `json:"payload,omitempty"` // Type-specific payload
}

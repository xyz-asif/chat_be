# Backend: Notification System Implementation Guide

This adds a reusable notification feature to your Go/Fiber backend. It follows
the exact same pattern as your existing `chat`, `connections`, and `users` packages:
model → repository → service → handler → routes → wire in main.go.

The system is designed to be **feature-agnostic** — chat, connections, posts, comments,
or any future feature just calls `notificationService.Send(...)` and everything else
(storage, FCM push, WebSocket real-time delivery, grouping) is handled automatically.

---

## 1. Model

**File: `internal/models/notification.go`**

```go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Notification types — add new ones as you add features.
// The frontend switches on these to decide the icon and navigation.
const (
	NotifTypeConnectionRequest  = "connection_request"
	NotifTypeConnectionAccepted = "connection_accepted"
	NotifTypeNewMessage         = "new_message"
	// Future: "post_like", "post_comment", "mention", "follow", etc.
)

// Resource types — what entity the notification points to.
// The frontend switches on these to decide WHERE to navigate.
const (
	ResourceTypeConnection = "connection"
	ResourceTypeChatRoom   = "chat_room"
	// Future: "post", "comment", "user_profile", etc.
)

// Notification is the DB document stored in the "notifications" collection.
type Notification struct {
	ID           bson.ObjectID  `bson:"_id,omitempty" json:"id"`
	RecipientID  bson.ObjectID  `bson:"recipientId" json:"recipientId"`
	ActorID      bson.ObjectID  `bson:"actorId" json:"actorId"`
	Type         string         `bson:"type" json:"type"`                 // e.g. "connection_request"
	ResourceType string         `bson:"resourceType" json:"resourceType"` // e.g. "connection"
	ResourceID   string         `bson:"resourceId" json:"resourceId"`     // hex ID of the resource
	Title        string         `bson:"title" json:"title"`
	Body         string         `bson:"body" json:"body"`
	ImageURL     string         `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"` // actor's photo
	IsRead       bool           `bson:"isRead" json:"isRead"`
	GroupKey     string         `bson:"groupKey,omitempty" json:"groupKey,omitempty"` // for dedup/grouping
	CreatedAt    time.Time      `bson:"createdAt" json:"createdAt"`
}

// NotificationResponse is what the API returns to the frontend.
// Includes actor display info so the frontend doesn't need a separate user lookup.
type NotificationResponse struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	ResourceType    string    `json:"resourceType"`
	ResourceID      string    `json:"resourceId"`
	Title           string    `json:"title"`
	Body            string    `json:"body"`
	ActorID         string    `json:"actorId"`
	ActorName       string    `json:"actorName"`
	ActorPhotoURL   string    `json:"actorPhotoUrl,omitempty"`
	IsRead          bool      `json:"isRead"`
	CreatedAt       time.Time `json:"createdAt"`
}

// SendNotificationRequest is the input other services use to create a notification.
// This is an internal struct, not an API request body.
type SendNotificationRequest struct {
	RecipientID  bson.ObjectID
	ActorID      bson.ObjectID
	Type         string // NotifType constant
	ResourceType string // ResourceType constant
	ResourceID   string // hex string of the resource
	Title        string
	Body         string
	GroupKey     string // optional: for dedup (e.g. "msg:<roomId>" to group messages per room)
}
```

**Key design decisions:**
- `ResourceType` + `ResourceID` drive frontend navigation. The notification system never needs to know about routes.
- `GroupKey` enables message grouping. When a second message arrives for the same room before the user reads the first notification, we update the existing one instead of creating a duplicate.
- `ImageURL` is populated from the actor's profile photo at creation time. This avoids a join on every read.

---

## 2. Repository

**File: `internal/features/notifications/repository.go`**

```go
package notifications

import (
	"context"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Create(ctx context.Context, notif *models.Notification) error
	GetByRecipient(ctx context.Context, recipientID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Notification, error)
	GetUnreadCount(ctx context.Context, recipientID bson.ObjectID) (int64, error)
	MarkAsRead(ctx context.Context, notifID, recipientID bson.ObjectID) error
	MarkAllAsRead(ctx context.Context, recipientID bson.ObjectID) error
	FindByGroupKey(ctx context.Context, recipientID bson.ObjectID, groupKey string) (*models.Notification, error)
	UpdateGroupedNotification(ctx context.Context, notifID bson.ObjectID, title, body string) error
}

type repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		collection: db.Collection("notifications"),
	}
}

func (r *repository) Create(ctx context.Context, notif *models.Notification) error {
	notif.CreatedAt = time.Now()
	notif.IsRead = false

	res, err := r.collection.InsertOne(ctx, notif)
	if err != nil {
		return err
	}
	notif.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetByRecipient(ctx context.Context, recipientID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Notification, error) {
	filter := bson.M{"recipientId": recipientID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var notifs []models.Notification
	if err := cursor.All(ctx, &notifs); err != nil {
		return nil, err
	}
	return notifs, nil
}

func (r *repository) GetUnreadCount(ctx context.Context, recipientID bson.ObjectID) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"recipientId": recipientID,
		"isRead":      false,
	})
}

func (r *repository) MarkAsRead(ctx context.Context, notifID, recipientID bson.ObjectID) error {
	// Ensure the notification belongs to the recipient (security check)
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": notifID, "recipientId": recipientID},
		bson.M{"$set": bson.M{"isRead": true}},
	)
	return err
}

func (r *repository) MarkAllAsRead(ctx context.Context, recipientID bson.ObjectID) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"recipientId": recipientID, "isRead": false},
		bson.M{"$set": bson.M{"isRead": true}},
	)
	return err
}

func (r *repository) FindByGroupKey(ctx context.Context, recipientID bson.ObjectID, groupKey string) (*models.Notification, error) {
	var notif models.Notification
	err := r.collection.FindOne(ctx, bson.M{
		"recipientId": recipientID,
		"groupKey":    groupKey,
		"isRead":      false, // only group with unread notifications
	}).Decode(&notif)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &notif, nil
}

func (r *repository) UpdateGroupedNotification(ctx context.Context, notifID bson.ObjectID, title, body string) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": notifID},
		bson.M{"$set": bson.M{
			"title":     title,
			"body":      body,
			"createdAt": time.Now(), // bump to top of list
		}},
	)
	return err
}
```

---

## 3. Service

**File: `internal/features/notifications/service.go`**

This is the core. Other features call `Send()`. The service handles grouping,
storage, WebSocket real-time delivery, and FCM push.

```go
package notifications

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// HubSender sends WebSocket messages (satisfied by chat.Hub)
type HubSender interface {
	SendMessage(userID string, msg models.WSMessage) error
	IsUserOnline(userID string) bool
}

// UserLookup fetches user info for populating notification display data
type UserLookup interface {
	GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
}

// FCMSender sends push notifications (implement this interface)
type FCMSender interface {
	SendPush(ctx context.Context, tokens []string, title, body string, data map[string]string) error
}

type Service interface {
	Send(ctx context.Context, req models.SendNotificationRequest) error
	GetNotifications(ctx context.Context, userID string, limit int, before string) ([]models.NotificationResponse, bool, error)
	GetUnreadCount(ctx context.Context, userID string) (int64, error)
	MarkAsRead(ctx context.Context, userID, notifID string) error
	MarkAllAsRead(ctx context.Context, userID string) error
}

type service struct {
	repo       Repository
	userLookup UserLookup
	hub        HubSender
	fcm        FCMSender // nil if FCM not configured — push is skipped gracefully
}

func NewService(repo Repository, userLookup UserLookup, hub HubSender, fcm FCMSender) Service {
	return &service{
		repo:       repo,
		userLookup: userLookup,
		hub:        hub,
		fcm:        fcm,
	}
}

// Send creates a notification, delivers it via WebSocket if online, or FCM push if offline.
// This is the method all other features call.
func (s *service) Send(ctx context.Context, req models.SendNotificationRequest) error {
	// Don't notify yourself
	if req.RecipientID == req.ActorID {
		return nil
	}

	// Look up actor info for display
	actor, err := s.userLookup.GetUserByID(ctx, req.ActorID)
	if err != nil {
		return fmt.Errorf("failed to look up actor: %w", err)
	}

	// Handle grouping: if a GroupKey is set and an unread notification with the
	// same key exists, update it instead of creating a duplicate.
	// Example: multiple messages from Alice → "Alice sent 3 messages" instead of 3 separate notifs.
	if req.GroupKey != "" {
		existing, err := s.repo.FindByGroupKey(ctx, req.RecipientID, req.GroupKey)
		if err != nil {
			log.Printf("notification grouping lookup failed: %v", err)
			// Fall through to create a new one
		}
		if existing != nil {
			if err := s.repo.UpdateGroupedNotification(ctx, existing.ID, req.Title, req.Body); err != nil {
				log.Printf("notification grouping update failed: %v", err)
			} else {
				// Deliver the updated notification via WebSocket
				s.deliverRealtime(existing.ID.Hex(), req, actor)
				return nil
			}
		}
	}

	// Create new notification
	notif := &models.Notification{
		RecipientID:  req.RecipientID,
		ActorID:      req.ActorID,
		Type:         req.Type,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		Title:        req.Title,
		Body:         req.Body,
		ImageURL:     actor.PhotoURL,
		GroupKey:     req.GroupKey,
	}

	if err := s.repo.Create(ctx, notif); err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	// Real-time delivery via WebSocket
	s.deliverRealtime(notif.ID.Hex(), req, actor)

	// FCM push if recipient is offline
	recipientHex := req.RecipientID.Hex()
	if s.fcm != nil && !s.hub.IsUserOnline(recipientHex) {
		go s.sendPush(req.RecipientID, req, actor)
	}

	return nil
}

// deliverRealtime sends the notification over WebSocket for instant UI update
func (s *service) deliverRealtime(notifID string, req models.SendNotificationRequest, actor *models.User) {
	recipientHex := req.RecipientID.Hex()

	_ = s.hub.SendMessage(recipientHex, models.WSMessage{
		Type: "notification",
		Payload: map[string]interface{}{
			"id":            notifID,
			"type":          req.Type,
			"resourceType":  req.ResourceType,
			"resourceId":    req.ResourceID,
			"title":         req.Title,
			"body":          req.Body,
			"actorId":       req.ActorID.Hex(),
			"actorName":     actor.DisplayName,
			"actorPhotoUrl": actor.PhotoURL,
		},
	})
}

// sendPush sends an FCM notification to offline users
func (s *service) sendPush(recipientID bson.ObjectID, req models.SendNotificationRequest, actor *models.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	recipient, err := s.userLookup.GetUserByID(ctx, recipientID)
	if err != nil || recipient == nil || len(recipient.FCMTokens) == 0 {
		return
	}

	data := map[string]string{
		"type":         req.Type,
		"resourceType": req.ResourceType,
		"resourceId":   req.ResourceID,
	}

	if err := s.fcm.SendPush(ctx, recipient.FCMTokens, req.Title, req.Body, data); err != nil {
		log.Printf("FCM push failed for user %s: %v", recipientID.Hex(), err)
	}
}

func (s *service) GetNotifications(ctx context.Context, userIDStr string, limit int, beforeStr string) ([]models.NotificationResponse, bool, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, false, errors.New("invalid user id")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	var beforeID *bson.ObjectID
	if beforeStr != "" {
		id, err := bson.ObjectIDFromHex(beforeStr)
		if err != nil {
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	// Fetch one extra to determine hasMore
	notifs, err := s.repo.GetByRecipient(ctx, userID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(notifs) > limit
	if hasMore {
		notifs = notifs[:limit]
	}

	// Build responses with actor info
	responses := make([]models.NotificationResponse, 0, len(notifs))
	for _, n := range notifs {
		resp := models.NotificationResponse{
			ID:           n.ID.Hex(),
			Type:         n.Type,
			ResourceType: n.ResourceType,
			ResourceID:   n.ResourceID,
			Title:        n.Title,
			Body:         n.Body,
			ActorID:      n.ActorID.Hex(),
			IsRead:       n.IsRead,
			CreatedAt:    n.CreatedAt,
			ActorPhotoURL: n.ImageURL, // stored at creation time
		}

		// Actor name: look up from DB (could cache this, but fine for now)
		if actor, err := s.userLookup.GetUserByID(ctx, n.ActorID); err == nil && actor != nil {
			resp.ActorName = actor.DisplayName
		}

		responses = append(responses, resp)
	}

	return responses, hasMore, nil
}

func (s *service) GetUnreadCount(ctx context.Context, userIDStr string) (int64, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return 0, errors.New("invalid user id")
	}
	return s.repo.GetUnreadCount(ctx, userID)
}

func (s *service) MarkAsRead(ctx context.Context, userIDStr, notifIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	notifID, err := bson.ObjectIDFromHex(notifIDStr)
	if err != nil {
		return errors.New("invalid notification id")
	}
	return s.repo.MarkAsRead(ctx, notifID, userID)
}

func (s *service) MarkAllAsRead(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	return s.repo.MarkAllAsRead(ctx, userID)
}
```

---

## 4. FCM Sender Implementation

**File: `internal/features/notifications/fcm.go`**

```go
package notifications

import (
	"context"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FirebaseFCM implements FCMSender using Firebase Admin SDK
type FirebaseFCM struct {
	client *messaging.Client
}

// NewFirebaseFCM creates an FCM sender. Returns nil (not an error) if
// credentials are missing — the notification service handles nil gracefully
// by skipping push notifications.
func NewFirebaseFCM(credPath, projectID string) *FirebaseFCM {
	if credPath == "" && projectID == "" {
		log.Println("FCM: No credentials configured, push notifications disabled")
		return nil
	}

	ctx := context.Background()
	var opts []option.ClientOption
	if credPath != "" {
		opts = append(opts, option.WithCredentialsFile(credPath))
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, opts...)
	if err != nil {
		log.Printf("FCM: Failed to init Firebase app: %v", err)
		return nil
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("FCM: Failed to init messaging client: %v", err)
		return nil
	}

	log.Println("FCM: Push notifications enabled")
	return &FirebaseFCM{client: client}
}

func (f *FirebaseFCM) SendPush(ctx context.Context, tokens []string, title, body string, data map[string]string) error {
	if f == nil || len(tokens) == 0 {
		return nil
	}

	message := &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				ChannelID: "high_importance_channel", // matches your Flutter channel
				Sound:     "default",
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:            "default",
					MutableContent:   true,
					ContentAvailable: true,
				},
			},
		},
	}

	resp, err := f.client.SendEachForMulticast(ctx, message)
	if err != nil {
		return err
	}

	// Log failures (stale tokens, etc.) but don't fail the notification
	if resp.FailureCount > 0 {
		for i, r := range resp.Responses {
			if r.Error != nil {
				log.Printf("FCM: Token %d failed: %v", i, r.Error)
				// TODO: Remove stale tokens from user's FCMTokens array
			}
		}
	}

	return nil
}
```

---

## 5. Handler

**File: `internal/features/notifications/handler.go`**

```go
package notifications

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/models"
	"github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// GetNotifications returns paginated notification list.
// Query params: limit (default 20, max 50), before (cursor: notification ID)
func (h *Handler) GetNotifications(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	limit := c.QueryInt("limit", 20)
	before := c.Query("before")

	notifs, hasMore, err := h.service.GetNotifications(c.Context(), user.ID.Hex(), limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Notifications retrieved", fiber.Map{
		"notifications": notifs,
		"hasMore":       hasMore,
	})
}

// GetUnreadCount returns the count of unread notifications (for badge).
func (h *Handler) GetUnreadCount(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	count, err := h.service.GetUnreadCount(c.Context(), user.ID.Hex())
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Unread count", fiber.Map{"count": count})
}

// MarkAsRead marks a single notification as read.
func (h *Handler) MarkAsRead(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	notifID := c.Params("id")
	if notifID == "" {
		return response.BadRequest(c, "notification ID required")
	}

	if err := h.service.MarkAsRead(c.Context(), user.ID.Hex(), notifID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Marked as read", nil)
}

// MarkAllAsRead marks all notifications as read.
func (h *Handler) MarkAllAsRead(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	if err := h.service.MarkAllAsRead(c.Context(), user.ID.Hex()); err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "All marked as read", nil)
}
```

---

## 6. Database Indexes

**Add to `internal/database/indexes.go`** inside `CreateIndexes`:

```go
// ── Notifications ──
notifsIndexes := []mongo.IndexModel{
	// Primary query: paginated list for a user, newest first
	{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "_id", Value: -1}}},
	// Unread count query
	{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "isRead", Value: 1}}},
	// Grouping lookup (find existing unread notification with same groupKey)
	{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "groupKey", Value: 1}, {Key: "isRead", Value: 1}}},
}
if _, err := db.Collection("notifications").Indexes().CreateMany(ctx, notifsIndexes); err != nil {
	log.Printf("Warning: Notifications index creation issue: %v", err)
}
```

---

## 7. Routes

**Add to `internal/routes/routes.go`:**

Update the function signature to accept the new handler:

```go
func SetupRoutes(
	app *fiber.App,
	authMiddleware *middleware.AuthMiddleware,
	userHandler *users.Handler,
	connectionHandler *connections.Handler,
	chatHandler *chat.Handler,
	notifHandler *notifications.Handler, // ← add this
) {
```

Add the notification routes after the chat routes:

```go
// ── Notification Routes ──
notifGroup := api.Group("/notifications")
notifGroup.Get("/", authMiddleware.VerifyToken, notifHandler.GetNotifications)
notifGroup.Get("/unread-count", authMiddleware.VerifyToken, notifHandler.GetUnreadCount)
notifGroup.Post("/:id/read", authMiddleware.VerifyToken, notifHandler.MarkAsRead)
notifGroup.Post("/read-all", authMiddleware.VerifyToken, notifHandler.MarkAllAsRead)
```

---

## 8. Wire in main.go

Add after the existing service initializations:

```go
// Initialize notification system
notifRepo := notifications.NewRepository(db.Database)
fcmSender := notifications.NewFirebaseFCM(cfg.FirebaseCredsPath, cfg.FirebaseProjectID)
notifService := notifications.NewService(notifRepo, userRepo, chatHub, fcmSender)
notifHandler := notifications.NewHandler(notifService)
```

Update `routes.SetupRoutes` to include `notifHandler`:

```go
routes.SetupRoutes(
	app,
	authMiddleware,
	userHandler,
	connectionHandler,
	chatHandler,
	notifHandler, // ← add this
)
```

---

## 9. FCM Token Registration

You already have `FCMTokens []string` on the User model. Add an endpoint for
the frontend to register/update tokens.

**Add to `internal/features/users/handler.go`:**

```go
// RegisterFCMToken saves the device's FCM token for push notifications.
func (h *Handler) RegisterFCMToken(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := c.BodyParser(&req); err != nil || req.Token == "" {
		return response.BadRequest(c, "token is required")
	}

	if err := h.service.RegisterFCMToken(c.Context(), user.ID.Hex(), req.Token); err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Token registered", nil)
}
```

**Add to `internal/features/users/service.go`:**

```go
func (s *service) RegisterFCMToken(ctx context.Context, userID, token string) error {
	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	return s.repo.AddFCMToken(ctx, uid, token)
}
```

**Add to `internal/features/users/repository.go`:**

```go
// AddFCMToken adds a token if not already present (idempotent).
func (r *repository) AddFCMToken(ctx context.Context, userID bson.ObjectID, token string) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$addToSet": bson.M{"fcmTokens": token}},
	)
	return err
}
```

Add the route in `routes.go`:
```go
usersGroup.Post("/me/fcm-token", authMiddleware.VerifyToken, userHandler.RegisterFCMToken)
```

---

## 10. Trigger Notifications from Existing Features

### Connection Request Sent

In `connections/service.go` `SendRequest()`, after creating the connection:

```go
// At the top of the file, add notifService to the service struct:
type service struct {
	repo         Repository
	notifService notifications.Service // add this
}

// In SendRequest, after s.repo.CreateConnection:
_ = s.notifService.Send(ctx, models.SendNotificationRequest{
	RecipientID:  receiverID,
	ActorID:      senderID,
	Type:         models.NotifTypeConnectionRequest,
	ResourceType: models.ResourceTypeConnection,
	ResourceID:   conn.ID.Hex(),
	Title:        "New friend request",
	Body:         senderName + " sent you a friend request",
})
```

You'll need to look up the sender's name. Either pass it in or fetch it.

### Connection Accepted

In `connections/service.go` `AcceptRequest()`, after updating status:

```go
_ = s.notifService.Send(ctx, models.SendNotificationRequest{
	RecipientID:  conn.SenderID,
	ActorID:      userID,       // the one who accepted
	Type:         models.NotifTypeConnectionAccepted,
	ResourceType: models.ResourceTypeConnection,
	ResourceID:   conn.ID.Hex(),
	Title:        "Request accepted",
	Body:         acceptorName + " accepted your friend request",
})
```

### New Chat Message (Offline Users Only)

In `chat/service.go` `SendMessage()`, after broadcasting via WebSocket:

```go
// Send notification to offline participants only
for _, p := range room.Participants {
	pHex := p.Hex()
	if pHex != senderIDStr && !s.hub.IsUserOnline(pHex) {
		_ = s.notifService.Send(ctx, models.SendNotificationRequest{
			RecipientID:  p,
			ActorID:      senderID,
			Type:         models.NotifTypeNewMessage,
			ResourceType: models.ResourceTypeChatRoom,
			ResourceID:   roomIDStr,
			Title:        senderName,
			Body:         preview,
			GroupKey:     "msg:" + roomIDStr, // groups messages per room
		})
	}
}
```

The `GroupKey` ensures that if Alice sends 5 messages while Bob is offline,
Bob gets one notification that updates to show the latest message — not 5
separate notifications.

---

## 11. WebSocket Event for Real-Time Notifications

Add `"notification"` to the WebSocket event types. The frontend will handle
this in `ws_event_handler.dart` to update the notification badge and optionally
show a toast.

The payload is already sent by `deliverRealtime()` in the service. No additional
backend work needed — the hub delivers it like any other WS message.

---

## API Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/notifications?limit=20&before=<id>` | Paginated notification list |
| GET | `/api/v1/notifications/unread-count` | Badge count |
| POST | `/api/v1/notifications/:id/read` | Mark one as read |
| POST | `/api/v1/notifications/read-all` | Mark all as read |
| POST | `/api/v1/users/me/fcm-token` | Register FCM token |

---

## Edge Cases Handled

1. **Self-notification**: `Send()` returns early if recipientID == actorID
2. **Offline push**: Only sent when recipient has no active WebSocket connection
3. **Message grouping**: Multiple messages from same room → single notification that updates
4. **Stale FCM tokens**: Logged on failure, ready for cleanup (TODO in fcm.go)
5. **No FCM credentials**: `fcmSender` is nil → push silently skipped, in-app still works
6. **Cursor pagination**: Same pattern as your chat messages, no offset-based slowdown
7. **Security**: `MarkAsRead` checks recipientID matches the authenticated user
8. **Actor info embedded**: `ImageURL` stored at creation time, `ActorName` resolved on read

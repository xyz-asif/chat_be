# Prompt: Add Media Message Support to Go/Fiber Chat Backend

## Context

This is a Go backend built with Fiber (HTTP framework) + WebSocket + MongoDB. It uses a clean layered architecture: `handler.go` → `service.go` → `repository.go`, with models in `internal/models/chat.go`. State management uses a WebSocket Hub (`hub.go`) for real-time broadcasting.

**Currently the backend supports 1-to-1 text messaging only. We are adding support for media messages (images, videos, audio, files, GIFs, and links).**

### Critical Architectural Decision

Media files will **NOT** be uploaded through the backend. The Flutter client uploads media directly to Cloudinary using an unsigned upload preset. After upload, Cloudinary returns a `secure_url`. The Flutter client then sends this URL to the backend as part of the message payload. **The backend only stores the URL and broadcasts the message. The backend must NOT contain any Cloudinary SDK integration or file upload handling.**

---

## Existing Codebase Reference

Below are the actual files that need modification. Read them carefully to understand the existing patterns before making changes.

### File: `internal/models/chat.go`

This file contains all chat-related data structures. Key types:

- `Message` — the MongoDB document struct (uses `bson.ObjectID`, not UUID)
- `MessageResponse` — the JSON response sent to clients
- `WSMessage` — the WebSocket payload envelope
- `Room` — chat room with `LastMessage string` field
- `RoomResponse` — room response with `LastMessage string` field

Current `Message` struct:

```go
type Message struct {
    ID        bson.ObjectID     `bson:"_id,omitempty" json:"id"`
    RoomID    bson.ObjectID     `bson:"roomId" json:"roomId"`
    SenderID  bson.ObjectID     `bson:"senderId" json:"senderId"`
    Content   string            `bson:"content" json:"content"`
    Status    string            `bson:"status" json:"status"`
    Reactions map[string]string `bson:"reactions,omitempty" json:"reactions,omitempty"`
    ReplyToID *bson.ObjectID    `bson:"replyToId,omitempty" json:"replyToId,omitempty"`
    IsEdited  bool              `bson:"isEdited,omitempty" json:"isEdited,omitempty"`
    IsDeleted bool              `bson:"isDeleted,omitempty" json:"isDeleted,omitempty"`
    CreatedAt time.Time         `bson:"createdAt" json:"createdAt"`
    UpdatedAt time.Time         `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}
```

Current `MessageResponse` struct:

```go
type MessageResponse struct {
    ID             string            `json:"id"`
    RoomID         string            `json:"roomId"`
    SenderID       string            `json:"senderId"`
    SenderName     string            `json:"senderName,omitempty"`
    SenderPhotoURL string            `json:"senderPhotoURL,omitempty"`
    Content        string            `json:"content"`
    Status         string            `json:"status"`
    Reactions      map[string]string `json:"reactions,omitempty"`
    ReplyTo        *MessageResponse  `json:"replyTo,omitempty"`
    IsEdited       bool              `json:"isEdited,omitempty"`
    IsDeleted      bool              `json:"isDeleted,omitempty"`
    CreatedAt      time.Time         `json:"createdAt"`
    UpdatedAt      time.Time         `json:"updatedAt,omitempty"`
}
```

### File: `internal/features/chat/service.go`

This contains all business logic. Key methods to update:

- `SendMessage(ctx, senderID, roomID, content, replyToID)` — currently only accepts `content string`. This is where validation happens and where the message is built, saved, and broadcast.
- `buildMessageResponse(ctx, msg)` — builds `MessageResponse` from `Message`. Must map new fields.
- `EditMessage(ctx, userID, messageID, content)` — should probably block editing media messages (or only allow editing the caption).
- `HandleWebSocket(c, userID)` — WebSocket read loop, currently handles `typing_start`/`typing_stop`.

Key patterns in SendMessage:

```go
// Current validation
if content == "" {
    return nil, errors.New("message content cannot be empty")
}
if len([]rune(content)) > 2000 {
    return nil, errors.New("message content exceeds maximum length of 2000 characters")
}

// Current message construction
msg := &models.Message{
    RoomID:    roomID,
    SenderID:  senderID,
    Content:   content,
    Status:    models.MessageStatusSent,
    ReplyToID: replyToObjId,
}

// Room last message update (currently stores raw content)
s.repo.UpdateRoomLastMessage(ctx, roomID, content, senderID)

// Room updated broadcast (currently sends raw content)
_ = s.hub.SendToUsers(userIDs, models.WSMessage{
    Type:   "room_updated",
    RoomID: roomIDStr,
    Payload: map[string]interface{}{
        "lastMessage":  content,
        "lastUpdated":  msg.CreatedAt,
        "lastSenderId": senderIDStr,
    },
})
```

### File: `internal/features/chat/handler.go`

HTTP handlers. Key method to update:

- `SendMessage(c)` — currently parses `{ "content": "...", "replyToId": "..." }`. Must accept new fields.

```go
var req struct {
    Content   string `json:"content"`
    ReplyToID string `json:"replyToId,omitempty"`
}
```

Then calls: `h.service.SendMessage(c.Context(), user.ID.Hex(), roomID, req.Content, req.ReplyToID)`

### File: `internal/features/chat/repository.go`

Data access layer. `SaveMessage` and `GetMessagesByRoom` need no changes (MongoDB is schemaless, new fields persist automatically). But `UpdateRoomLastMessage` stores `lastMessage string` — this needs consideration for media preview text.

### File: `internal/features/chat/hub.go`

WebSocket hub. **No changes needed** — it already handles generic `models.WSMessage` broadcasting. Media messages flow through the same pipeline.

### File: `routes/routes.go`

Current relevant endpoints:

```
POST /api/v1/chat/rooms/:roomId/messages  → chatHandler.SendMessage
GET  /api/v1/chat/rooms/:roomId/messages  → chatHandler.GetRoomMessages
PATCH /api/v1/chat/messages/:messageId    → chatHandler.EditMessage
```

**No new routes are needed.** The existing `SendMessage` endpoint handles media messages with the updated request body.

---

## Requirements

### 1. Message Type Constants

Add these message type constants to `internal/models/chat.go`:

```
text, image, video, audio, file, gif, link
```

Default type is `"text"` for backward compatibility with existing messages that don't have a `type` field.

### 2. MediaMetadata Struct

Create a `MediaMetadata` struct in `internal/models/chat.go` with these optional fields:

| Field        | Type   | Purpose                              |
|--------------|--------|--------------------------------------|
| mimeType     | string | e.g. "image/jpeg", "video/mp4"       |
| fileName     | string | Original filename for file downloads  |
| fileSize     | int64  | Size in bytes                        |
| thumbnailURL | string | Preview thumbnail (for video/file)   |
| duration     | int    | Duration in seconds (audio/video)    |
| width        | int    | Media dimensions (image/video)       |
| height       | int    | Media dimensions (image/video)       |

All fields should be `omitempty` in both bson and json tags. Use a pointer (`*MediaMetadata`) on the Message struct so it's `nil` for text messages and omitted from JSON/BSON entirely.

### 3. Update Message Struct

Add two new fields to the `Message` struct:

- `Type string` — bson:"type" json:"type" (the message type)
- `Metadata *MediaMetadata` — bson:"metadata,omitempty" json:"metadata,omitempty"

### 4. Update MessageResponse Struct

Mirror the same two fields:

- `Type string` — json:"type"
- `Metadata *MediaMetadata` — json:"metadata,omitempty"

### 5. Update RoomResponse Struct

Add a field to convey the last message type so the Flutter client can show appropriate previews in the room list:

- `LastMessageType string` — json:"lastMessageType,omitempty"

### 6. Update Room Struct

Add a field to store the type of the last message:

- `LastMessageType string` — bson:"lastMessageType,omitempty" json:"lastMessageType,omitempty"

### 7. Update Handler — SendMessage Request Body

Change the request struct in `handler.go` `SendMessage` to accept:

```go
var req struct {
    Content   string                `json:"content"`
    Type      string                `json:"type"`
    Metadata  *models.MediaMetadata `json:"metadata,omitempty"`
    ReplyToID string                `json:"replyToId,omitempty"`
}
```

If `req.Type` is empty, default it to `"text"` for backward compatibility.

Pass `req.Type` and `req.Metadata` through to the service layer.

### 8. Update Service Interface & SendMessage Signature

Update the `Service` interface and `SendMessage` method signature to accept message type and metadata:

```
SendMessage(ctx, senderID, roomID, content, msgType string, metadata *models.MediaMetadata, replyToID string)
```

### 9. Update Service — Validation Logic

Replace the current simple validation in `SendMessage` with type-aware validation:

**For `text` type:**
- Content must not be empty
- Content must not exceed 2000 characters (existing rule)

**For media types (`image`, `video`, `audio`, `file`, `gif`, `link`):**
- Content must be a valid URL starting with `https://`
- Validate URL can be parsed with `net/url`
- Whitelist allowed hosts: `res.cloudinary.com`, `media.giphy.com` (for GIFs)
- Content (URL) length should not exceed 2048 characters
- Metadata is optional but accepted

**For all types:**
- Reject invalid/unknown message types with an error
- Validate `msgType` against the defined constants

Create a dedicated validation helper function (e.g., `validateMessageContent(msgType, content string) error`) to keep `SendMessage` clean.

### 10. Update Service — Message Construction

When building the `Message` struct in `SendMessage`, include the new fields:

```go
msg := &models.Message{
    RoomID:    roomID,
    SenderID:  senderID,
    Content:   content,
    Type:      msgType,
    Metadata:  metadata,
    Status:    models.MessageStatusSent,
    ReplyToID: replyToObjId,
}
```

### 11. Update Service — Room Last Message Preview

When updating `UpdateRoomLastMessage`, store a human-readable preview string for non-text messages instead of the raw URL. Use a helper function:

```
text     → store the actual content (existing behavior)
image    → "📷 Photo"
video    → "🎥 Video"
audio    → "🎵 Audio"
file     → "📎 File"  (or "📎 {fileName}" if metadata.FileName is set)
gif      → "GIF"
link     → "🔗 Link"
```

Also update the `room_updated` WebSocket broadcast to include the message type so the Flutter client can render the preview correctly.

### 12. Update Service — buildMessageResponse

Map the `Type` and `Metadata` fields from `Message` to `MessageResponse`:

```go
resp.Type = msg.Type
if resp.Type == "" {
    resp.Type = MessageTypeText  // backward compat for old messages
}
resp.Metadata = msg.Metadata
```

### 13. Update Service — buildRoomResponse

Map the new `LastMessageType` field from `Room` to `RoomResponse`.

### 14. Update Service — EditMessage

Add a guard: only `text` type messages can be edited. If someone tries to edit a media message, return an error: `"only text messages can be edited"`.

### 15. Update Repository — UpdateRoomLastMessage

Update the method signature and implementation to also store `lastMessageType`:

```go
UpdateRoomLastMessage(ctx, roomID, lastMessage, lastMessageType string, senderID bson.ObjectID) error
```

Update the `$set` to include `"lastMessageType": lastMessageType`.

### 16. Update Repository Interface

Update the `Repository` interface to match the new `UpdateRoomLastMessage` signature.

---

## What NOT To Change

- **hub.go** — No changes. It already handles generic WSMessage broadcasting.
- **routes.go** — No new endpoints needed.
- **indexes.go** — No new indexes needed (type queries are not paginated).
- **mongo.go / transaction.go** — No changes.
- **connection.go / user.go** — No changes.
- **No Cloudinary SDK or upload logic** — The backend never touches media files.

---

## Example Client Payloads

### Text Message (backward compatible)

```json
POST /api/v1/chat/rooms/:roomId/messages

{
    "content": "Hello, how are you?",
    "type": "text"
}
```

Or without type (defaults to "text"):

```json
{
    "content": "Hello, how are you?"
}
```

### Image Message

```json
{
    "type": "image",
    "content": "https://res.cloudinary.com/demo/image/upload/chat_media/photo123.jpg",
    "metadata": {
        "mimeType": "image/jpeg",
        "fileSize": 245000,
        "width": 1080,
        "height": 720,
        "thumbnailURL": "https://res.cloudinary.com/demo/image/upload/w_200,h_200,c_thumb/chat_media/photo123.jpg"
    }
}
```

### Video Message

```json
{
    "type": "video",
    "content": "https://res.cloudinary.com/demo/video/upload/chat_media/video456.mp4",
    "metadata": {
        "mimeType": "video/mp4",
        "fileSize": 15000000,
        "duration": 45,
        "width": 1920,
        "height": 1080,
        "thumbnailURL": "https://res.cloudinary.com/demo/video/upload/so_2,w_400/chat_media/video456.jpg"
    }
}
```

### Audio Message

```json
{
    "type": "audio",
    "content": "https://res.cloudinary.com/demo/video/upload/chat_media/voice789.mp3",
    "metadata": {
        "mimeType": "audio/mpeg",
        "fileSize": 500000,
        "duration": 30
    }
}
```

### File Message

```json
{
    "type": "file",
    "content": "https://res.cloudinary.com/demo/raw/upload/chat_media/report.pdf",
    "metadata": {
        "mimeType": "application/pdf",
        "fileName": "Q4_Report.pdf",
        "fileSize": 2500000
    }
}
```

### GIF Message

```json
{
    "type": "gif",
    "content": "https://media.giphy.com/media/abc123/giphy.gif",
    "metadata": {
        "width": 480,
        "height": 270
    }
}
```

### Link Message

```json
{
    "type": "link",
    "content": "https://example.com/article/interesting-read"
}
```

### Reply to a Media Message

```json
{
    "type": "text",
    "content": "That's a great photo!",
    "replyToId": "6651abc123def456ghi789"
}
```

---

## Expected WebSocket Broadcasts After Media Message

### `message` event (to all room participants)

```json
{
    "type": "message",
    "roomId": "room123",
    "payload": {
        "id": "msg456",
        "roomId": "room123",
        "senderId": "user789",
        "senderName": "Alice",
        "senderPhotoURL": "https://...",
        "type": "image",
        "content": "https://res.cloudinary.com/.../photo.jpg",
        "metadata": {
            "mimeType": "image/jpeg",
            "width": 1080,
            "height": 720
        },
        "status": "sent",
        "createdAt": "2025-03-07T10:30:00Z"
    }
}
```

### `room_updated` event (to all room participants)

```json
{
    "type": "room_updated",
    "roomId": "room123",
    "payload": {
        "lastMessage": "📷 Photo",
        "lastMessageType": "image",
        "lastUpdated": "2025-03-07T10:30:00Z",
        "lastSenderId": "user789"
    }
}
```

---

## Summary of Files to Modify

| File | Changes |
|------|---------|
| `internal/models/chat.go` | Add type constants, `MediaMetadata` struct, update `Message`, `MessageResponse`, `Room`, `RoomResponse` |
| `internal/features/chat/handler.go` | Update `SendMessage` request body parsing, pass new fields to service |
| `internal/features/chat/service.go` | Update `Service` interface, `SendMessage` signature + validation + construction + preview, update `buildMessageResponse`, `buildRoomResponse`, guard `EditMessage` |
| `internal/features/chat/repository.go` | Update `Repository` interface and `UpdateRoomLastMessage` signature + implementation |

---

## Security Checklist

- Reject payloads where `type` is not one of the defined constants
- For media types, validate `content` is a valid HTTPS URL
- Whitelist URL hosts (`res.cloudinary.com`, `media.giphy.com`)
- Cap URL length at 2048 characters
- Existing auth middleware ensures only authenticated users send messages
- Existing `isUserInRoom` check ensures sender belongs to the room
- Do NOT add any file upload or Cloudinary integration to the backend

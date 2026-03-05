# Chat App — Backend API Documentation

> **Base URL:** `http://<your-server>:8080/api/v1`
> **All authenticated requests require a Firebase ID token in the header.**

---

## Table of Contents
1. [Authentication](#1-authentication)
2. [Standard Response Format](#2-standard-response-format)
3. [Data Models](#3-data-models)
4. [User Endpoints](#4-user-endpoints)
5. [Connection (Friends) Endpoints](#5-connection-friends-endpoints)
6. [Chat Room Endpoints](#6-chat-room-endpoints)
7. [Message Endpoints](#7-message-endpoints)
8. [Presence Endpoint](#8-presence-endpoint)
9. [WebSocket — Real-time Events](#9-websocket--real-time-events)
10. [Error Handling](#10-error-handling)

---

## 1. Authentication

This backend uses **Firebase Authentication**. The Flutter app must use the Firebase SDK to sign the user in via Google, then send the resulting **Firebase ID Token** on every API request.

### How to Authenticate Every Request

Add the following HTTP header to **every** API call:

```
Authorization: Bearer <FIREBASE_ID_TOKEN>
```

The backend verifies this token against Firebase and extracts the user. **No separate login endpoint is needed.** On the first request with a valid token, the user is automatically created in the database.

### First-Time Setup Flow
1. Flutter: User taps "Sign in with Google"
2. Flutter: Firebase returns an `idToken`
3. Flutter: Call `GET /api/v1/users/me` with the token
4. Backend: Automatically creates the user profile if new
5. Backend: Returns the user's profile

---

## 2. Standard Response Format

**ALL** endpoints return the same JSON envelope:

### Success Response
```json
{
  "success": true,
  "statusCode": 200,
  "message": "Operation successful",
  "data": { ... }
}
```

### Error Response
```json
{
  "success": false,
  "statusCode": 400,
  "message": "Descriptive error message"
}
```

> **Flutter Tip:** Always check `success` first. If `false`, show `message` to the user.

---

## 3. Data Models

### User
```json
{
  "id": "507f1f77bcf86cd799439011",
  "firebaseUid": "abc123xyz",
  "email": "alice@example.com",
  "displayName": "Alice Smith",
  "photoURL": "https://example.com/avatar.jpg",
  "bio": "Hey there!",
  "isActive": true,
  "createdAt": "2026-03-01T10:00:00Z",
  "updatedAt": "2026-03-05T12:00:00Z"
}
```

### ParticipantInfo (inside RoomResponse)
```json
{
  "id": "507f1f77bcf86cd799439011",
  "displayName": "Alice Smith",
  "photoURL": "https://example.com/avatar.jpg",
  "email": "alice@example.com",
  "isOnline": true
}
```

### RoomResponse
```json
{
  "id": "63f1a2b3c4d5e6f7a8b9c0d1",
  "type": "direct",
  "name": "",
  "participants": [ ParticipantInfo, ParticipantInfo ],
  "lastMessage": "Hey, how are you?",
  "lastMessageSenderName": "Alice",
  "unreadCount": 3,
  "lastUpdated": "2026-03-05T14:00:00Z"
}
```

> **For unread badge:** Use `unreadCount` from this object.
> **For online green dot:** Check `participants[i].isOnline`.

### MessageResponse
```json
{
  "id": "63f1a2b3c4d5e6f7a8b9c0d2",
  "roomId": "63f1a2b3c4d5e6f7a8b9c0d1",
  "senderId": "507f1f77bcf86cd799439011",
  "content": "Hello!",
  "status": "read",
  "reactions": {
    "507f1f77bcf86cd799439012": "👍"
  },
  "replyTo": {
    "id": "63f1a2b3c4d5e6f7a8b9c0d0",
    "senderId": "507f1f77bcf86cd799439012",
    "content": "Are you there?",
    "status": "read",
    "createdAt": "2026-03-05T13:55:00Z"
  },
  "isEdited": false,
  "isDeleted": false,
  "createdAt": "2026-03-05T14:00:00Z",
  "updatedAt": "2026-03-05T14:00:00Z"
}
```

> **Message status values:** `"sent"` → `"delivered"` → `"read"` (Blue ticks)
> **`replyTo`** is `null` unless the message is a reply.
> **`reactions`** is a map of `userId → emoji`. Empty object `{}` means no reactions.
> **`isDeleted: true`** means content is `"This message was deleted"` — render greyed out italicised text.

### Connection
```json
{
  "id": "63f1a2b3c4d5e6f7a8b9c0e1",
  "senderId": "507f1f77bcf86cd799439011",
  "receiverId": "507f1f77bcf86cd799439012",
  "status": "pending",
  "createdAt": "2026-03-05T10:00:00Z",
  "updatedAt": "2026-03-05T10:00:00Z"
}
```
> **Status values:** `"pending"`, `"accepted"`, `"rejected"`, `"blocked"`

---

## 4. User Endpoints

### `GET /users/me` — Get My Profile
Returns the authenticated user's profile.

**Response `data`:** `User` object

---

### `PATCH /users/me` — Update My Profile
Update profile fields. Send only the fields you want to change.

**Request Body:**
```json
{
  "displayName": "Alice Smith",
  "photoURL": "https://storage.googleapis.com/my-bucket/avatar.jpg",
  "bio": "Living life 🚀"
}
```

| Field | Type | Description |
|---|---|---|
| `displayName` | `string` | Optional. User's display name |
| `photoURL` | `string` | Optional. Full URL of profile image |
| `bio` | `string` | Optional. Short bio |

**Response `data`:** Updated `User` object

> **Profile Image Flow:** Upload image to Firebase Storage from Flutter, get the download URL, then send the URL here in `photoURL`.

---

### `GET /users/search?q=alice&limit=20&offset=0` — Search Users

**Query Params:**

| Param | Type | Default | Description |
|---|---|---|---|
| `q` | `string` | required | Search term (name or email) |
| `limit` | `int` | 20 | Max 50 |
| `offset` | `int` | 0 | For pagination |

**Response `data`:** Array of `User` objects

---

## 5. Connection (Friends) Endpoints

### `POST /connections/request` — Send Friend Request

**Request Body:**
```json
{
  "receiverId": "507f1f77bcf86cd799439012"
}
```

**Response `data`:** `Connection` object (status: `"pending"`)

**Errors:**
- `400` — Already sent a pending request
- `400` — User not found

---

### `POST /connections/:id/accept` — Accept a Friend Request

`:id` is the connection ID (from the pending requests list).

**Request Body:** None

**Response `data`:** `Connection` object (status: `"accepted"`)

---

### `POST /connections/:id/reject` — Reject a Friend Request

`:id` is the connection ID.

**Request Body:** None

**Response `data`:** `Connection` object (status: `"rejected"`)

---

### `GET /connections/pending` — List Pending Requests (Received)

**Response `data`:** Array of `Connection` objects where you are the receiver and status is `"pending"`.

---

### `GET /connections/friends` — List All Friends

**Response `data`:** Array of `Connection` objects where status is `"accepted"`.

---

## 6. Chat Room Endpoints

### `POST /chat/rooms/direct/:id` — Get or Create a 1-on-1 Chat Room

`:id` is the **target user's ID**.

> ⚠️ **You must be friends (accepted connection) with the user to create a room.** Returns `400` otherwise.

**Request Body:** None

**Response `data`:** `RoomResponse` object

> Call this when a user taps on a friend's name to open a chat. If a room already exists between your two users, it returns the existing one.

---

### `GET /chat/rooms` — List All Chat Rooms (Chat List Screen)

Returns all rooms the authenticated user is part of, sorted by `lastUpdated` (newest first).

**Response `data`:** Array of `RoomResponse` objects

> This is the data for your main chat list screen. Each room contains `unreadCount` and `participants[i].isOnline`.

---

### `GET /chat/rooms/:roomId/messages?limit=50&offset=0` — Get Message History

**Query Params:**

| Param | Type | Default | Max |
|---|---|---|---|
| `limit` | `int` | 50 | 200 |
| `offset` | `int` | 0 | — |

**Response `data`:** Array of `MessageResponse` objects, in **oldest-first** (chronological) order.

> **Pagination:** To load older messages, increment `offset`. E.g., first load: `offset=0`, next page: `offset=50`.

---

### `POST /chat/rooms/:roomId/read` — Mark Room as Read

Call this immediately when the user **opens** a chat room. Resets the unread count to 0 and marks all unread messages as "read". Also broadcasts `room_read` via WebSocket to the other participant to update their blue ticks.

**Request Body:** None

**Response `data`:** `null`

---

## 7. Message Endpoints

### `POST /chat/rooms/:roomId/messages` — Send a Message

**Request Body:**
```json
{
  "content": "Hello there!",
  "replyToId": "63f1a2b3c4d5e6f7a8b9c0d0"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `content` | `string` | ✅ Yes | Message text |
| `replyToId` | `string` | ❌ No | ID of the message being replied to |

**Response `data`:** `MessageResponse` object (HTTP 201)

> After sending, a **WebSocket `message` event** is broadcast to all participants in the room. Build your UI to append messages received via WS rather than polling.

---

### `PATCH /chat/messages/:messageId/status` — Update Message Status (Blue Ticks)

Call this to mark a message as `"delivered"` or `"read"`.

**Request Body:**
```json
{
  "status": "read"
}
```

| Status | Meaning |
|---|---|
| `"delivered"` | Message received on device (grey double tick) |
| `"read"` | Message opened by user (blue double tick) |

> The sender automatically receives a **WebSocket `message_status_changed` event**.

---

### `PUT /chat/messages/:messageId/reactions` — Add / Remove Reaction

Send an emoji to add it. Send the **same emoji again** to toggle it off. Send an **empty string** to remove any existing reaction.

**Request Body:**
```json
{
  "emoji": "👍"
}
```

> All room participants receive a **WebSocket `reaction_updated` event**.

---

### `PATCH /chat/messages/:messageId` — Edit a Message

Only the sender can edit their own message.

**Request Body:**
```json
{
  "content": "Corrected message text"
}
```

> All room participants receive a **WebSocket `message_edited` event**.
> The edited message will have `isEdited: true`.

---

### `DELETE /chat/messages/:messageId` — Delete a Message

Only the sender can delete their own message. This is a **soft delete** — the message content becomes `"This message was deleted"` and `isDeleted` becomes `true`. The message is never fully removed.

**Request Body:** None

> All room participants receive a **WebSocket `message_deleted` event**.

---

## 8. Presence Endpoint

### `GET /chat/users/:id/presence` — Get a User's Online Status

`:id` is the target user's ID.

**Response `data`:**
```json
{
  "userId": "507f1f77bcf86cd799439012",
  "online": true
}
```

> **Flutter Tip:** You don't need to call this for the chat list — online status is already embedded in `RoomResponse.participants[i].isOnline`. Use this endpoint only when you want to check a specific user's status on-demand (e.g., on a profile page).

---

## 9. WebSocket — Real-time Events

### Connecting

**URL:** `ws://<your-server>:8080/api/v1/chat/ws`

**Authentication:** Pass the Firebase ID token as a query parameter:

```
ws://localhost:8080/api/v1/chat/ws?token=<FIREBASE_ID_TOKEN>
```

Or via header (if your WebSocket client supports it):
```
Authorization: Bearer <FIREBASE_ID_TOKEN>
```

### Message Format

All messages (sent and received) use this JSON envelope:

```json
{
  "type": "<event_type>",
  "roomId": "<room_id>",
  "payload": { ... }
}
```

---

### Events You RECEIVE from the Server

#### `message` — A new message was sent in a room
```json
{
  "type": "message",
  "roomId": "63f1...",
  "payload": { /* Full MessageResponse object */ }
}
```
> Append this to the message list for the matching `roomId`. Also update the `lastMessage` preview in the chat list and move that room to the top.

---

#### `message_status_changed` — A message tick status changed
```json
{
  "type": "message_status_changed",
  "roomId": "63f1...",
  "payload": {
    "messageId": "63f1...",
    "status": "read",
    "markedBy": "507f..."
  }
}
```
> Find the message by `messageId` and update its `status`. Used to show grey → blue ticks.

---

#### `reaction_updated` — A reaction was added or removed
```json
{
  "type": "reaction_updated",
  "roomId": "63f1...",
  "payload": {
    "messageId": "63f1...",
    "userId": "507f...",
    "emoji": "👍"
  }
}
```
> If `emoji` is an **empty string `""`**, remove that user's reaction from the message. Otherwise, update/add it.

---

#### `message_edited` — A message was edited
```json
{
  "type": "message_edited",
  "roomId": "63f1...",
  "payload": {
    "messageId": "63f1...",
    "content": "Updated message text"
  }
}
```
> Find the message by `messageId`, update `content`, and set `isEdited: true`.

---

#### `message_deleted` — A message was deleted
```json
{
  "type": "message_deleted",
  "roomId": "63f1...",
  "payload": {
    "messageId": "63f1..."
  }
}
```
> Find the message by `messageId`, set `content` to `"This message was deleted"` and `isDeleted: true`.

---

#### `room_read` — Another participant read the room (batch read receipt)
```json
{
  "type": "room_read",
  "roomId": "63f1...",
  "payload": {
    "readBy": "507f..."
  }
}
```
> Update all your sent messages in `roomId` to `status: "read"` (blue ticks).

---

#### `typing_start` — A user started typing
```json
{
  "type": "typing_start",
  "roomId": "63f1...",
  "payload": {
    "userId": "507f..."
  }
}
```
> Show "Alice is typing..." indicator in the chat.

---

#### `typing_stop` — A user stopped typing
```json
{
  "type": "typing_stop",
  "roomId": "63f1...",
  "payload": {
    "userId": "507f..."
  }
}
```
> Hide the typing indicator.

---

### Events You SEND to the Server

#### Typing Started
```json
{
  "type": "typing_start",
  "roomId": "63f1..."
}
```

#### Typing Stopped
```json
{
  "type": "typing_stop",
  "roomId": "63f1..."
}
```

> Send `typing_start` when the user begins typing in a text field, and `typing_stop` when they stop (use a debounce timer of ~1-2 seconds).

---

## 10. Error Handling

| HTTP Status | Meaning | When it Happens |
|---|---|---|
| `200` | OK | Successful fetch/update |
| `201` | Created | New resource created (e.g., message sent) |
| `400` | Bad Request | Invalid request body, missing fields, business logic failure |
| `401` | Unauthorized | Missing or expired Firebase token |
| `403` | Forbidden | Authenticated but not allowed to do this action |
| `404` | Not Found | Resource not found |
| `409` | Conflict | Duplicate request (e.g., already sent a friend request) |
| `422` | Unprocessable | Validation error |
| `500` | Internal Server Error | Backend bug — report it |

For all errors, the response body is:
```json
{
  "success": false,
  "statusCode": 400,
  "message": "Human-readable error message"
}
```

---

## Quick Reference — All Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/users/me` | ✅ | Get my profile |
| `PATCH` | `/users/me` | ✅ | Update my profile |
| `GET` | `/users/search?q=` | ✅ | Search users |
| `POST` | `/connections/request` | ✅ | Send friend request |
| `POST` | `/connections/:id/accept` | ✅ | Accept friend request |
| `POST` | `/connections/:id/reject` | ✅ | Reject friend request |
| `GET` | `/connections/pending` | ✅ | Get pending requests |
| `GET` | `/connections/friends` | ✅ | Get friends list |
| `GET` | `/chat/rooms` | ✅ | Get chat list |
| `POST` | `/chat/rooms/direct/:id` | ✅ | Open/create chat with user |
| `GET` | `/chat/rooms/:roomId/messages` | ✅ | Get message history |
| `POST` | `/chat/rooms/:roomId/messages` | ✅ | Send a message |
| `POST` | `/chat/rooms/:roomId/read` | ✅ | Mark room as read |
| `PATCH` | `/chat/messages/:messageId/status` | ✅ | Update message tick status |
| `PUT` | `/chat/messages/:messageId/reactions` | ✅ | Add/remove emoji reaction |
| `PATCH` | `/chat/messages/:messageId` | ✅ | Edit a message |
| `DELETE` | `/chat/messages/:messageId` | ✅ | Delete a message |
| `GET` | `/chat/users/:id/presence` | ✅ | Get user online status |
| `WS` | `/chat/ws` | ✅ | WebSocket connection |
| `GET` | `/health` | ❌ | Health check |

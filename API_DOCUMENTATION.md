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
11. [Quick Reference](#11-quick-reference--all-endpoints)

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
> **For online green dot:** Check `participants[i].isOnline`. This is the at-load-time value. For live updates, listen for `user_online` / `user_offline` WebSocket events.

### MessageResponse
```json
{
  "id": "63f1a2b3c4d5e6f7a8b9c0d2",
  "roomId": "63f1a2b3c4d5e6f7a8b9c0d1",
  "senderId": "507f1f77bcf86cd799439011",
  "senderName": "Alice Smith",
  "senderPhotoURL": "https://example.com/avatar.jpg",
  "content": "Hello!",
  "status": "delivered",
  "reactions": {
    "507f1f77bcf86cd799439012": "👍"
  },
  "replyTo": {
    "id": "63f1a2b3c4d5e6f7a8b9c0d0",
    "senderId": "507f1f77bcf86cd799439012",
    "senderName": "Bob Jones",
    "senderPhotoURL": "https://example.com/bob.jpg",
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

> **Message status values:** `"sent"` → `"delivered"` → `"read"` (one-way only — status never goes backwards)
> - `"sent"` — saved on server, recipient offline at time of sending (single grey tick)
> - `"delivered"` — recipient was online when the message was sent (double grey tick). **Set automatically by the backend — no frontend action required.**
> - `"read"` — recipient called `POST /rooms/:roomId/read` (double blue tick)
>
> **`senderName` / `senderPhotoURL`** — included on every message so the frontend never needs a separate user lookup to render the message bubble.
> **`replyTo`** is `null` unless the message is a reply. One level deep only.
> **`reactions`** is a map of `userId → emoji`. Omitted when empty. On a deleted message, reactions are always cleared.
> **`isDeleted: true`** means content is `"This message was deleted"` — render greyed out italicised text.

### MessagesPage (response for message history)
```json
{
  "messages": [ MessageResponse, MessageResponse, "..." ],
  "hasMore": true
}
```

> **`messages`** is in chronological (oldest-first) order.
> **`hasMore`** — if `true`, there are older messages to load. Pass the `id` of the first (oldest) message in the current list as the `before` param to fetch the previous page.

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
  "bio": "Living life"
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

> You must be friends (accepted connection) with the user to create a room. Returns `400` otherwise.

**Request Body:** None

**Response `data`:** `RoomResponse` object

> Call this when a user taps on a friend's name to open a chat. If a room already exists between your two users, it returns the existing one.

---

### `GET /chat/rooms` — List All Chat Rooms (Chat List Screen)

Returns all rooms the authenticated user is part of, sorted by `lastUpdated` (newest first).

**Response `data`:** Array of `RoomResponse` objects

> This is the data for your main chat list screen. Each room contains `unreadCount` and `participants[i].isOnline`.
>
> **Live re-ordering:** Listen for the `room_updated` WebSocket event. When received, move that room to the top of the list and update its `lastMessage` preview — no re-fetch needed.

---

### `GET /chat/rooms/:roomId/messages` — Get Message History

Cursor-based pagination. Returns messages in **chronological (oldest-first)** order.

**Query Params:**

| Param | Type | Default | Max | Description |
|---|---|---|---|---|
| `limit` | `int` | 50 | 100 | Number of messages per page |
| `before` | `string` | — | — | Message ID cursor. Returns messages older than this ID. |

**Response `data`:** `MessagesPage` object

**Pagination flow:**
1. **First load** — call with no `before` param. Gets the latest `limit` messages.
2. **Load older messages** — pass the `id` of the **oldest (first) message** currently displayed as `before`.
3. **Stop** when `hasMore` is `false`.

```
First page:  GET /chat/rooms/:roomId/messages?limit=50
Older page:  GET /chat/rooms/:roomId/messages?limit=50&before=63f1a2b3c4d5e6f7a8b9c0d2
```

> The `before` cursor is stable — inserting new messages never shifts old pages.

---

### `POST /chat/rooms/:roomId/read` — Mark Room as Read

Call this immediately when the user **opens** a chat room. Resets the unread count to 0 and marks all unread messages as `"read"`. Also broadcasts `room_read` via WebSocket to the other participant to trigger blue ticks on their end.

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
| `content` | `string` | Yes | Message text. Max 2000 characters. |
| `replyToId` | `string` | No | ID of the message being replied to |

**Response `data`:** `MessageResponse` object (HTTP 201)

> **Status on send:** If the recipient is online at the moment of sending, the message is automatically saved as `"delivered"` — you will see `status: "delivered"` in both the HTTP response and the WS broadcast. If the recipient is offline, it will be `"sent"`.
>
> After sending, two **WebSocket events** are broadcast to all participants:
> - `message` — the new message itself
> - `room_updated` — signals the chat list to re-order

---

### `PATCH /chat/messages/:messageId/status` — Update Message Status

Call this to manually mark a message as `"read"`. The `"delivered"` status is now handled automatically by the backend when a message is sent to an online user — **you no longer need to call this for delivered**.

**Request Body:**
```json
{
  "status": "read"
}
```

| Status | Meaning |
|---|---|
| `"delivered"` | Message received on device (double grey tick) — handled automatically, only call manually if needed |
| `"read"` | Message opened by user (double blue tick) |

> Prefer using `POST /rooms/:roomId/read` to bulk-mark all messages as read when a room is opened. Use this endpoint only to mark individual messages.
>
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

> **Flutter Tip:** Use this only for on-demand checks (e.g. a profile page). For the chat list, online status is embedded in `RoomResponse.participants[i].isOnline` at load time. For live updates, listen for `user_online` / `user_offline` WebSocket events — **do not poll this endpoint on a timer**.

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

`roomId` is omitted for events that are not room-specific (e.g. `user_online`).

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
> Append to the message list for the matching `roomId`. The `status` field in the payload is already correct (`"sent"` or `"delivered"`).

---

#### `room_updated` — Chat list needs re-ordering
```json
{
  "type": "room_updated",
  "roomId": "63f1...",
  "payload": {
    "lastMessage": "Hey!",
    "lastUpdated": "2026-03-06T10:00:00Z",
    "lastSenderId": "507f..."
  }
}
```
> Move the room matching `roomId` to the top of the chat list. Update its last message preview with `lastMessage`. **This fires on every new message — use it instead of re-fetching the room list.**

---

#### `user_online` — A contact came online
```json
{
  "type": "user_online",
  "payload": {
    "userId": "507f..."
  }
}
```
> Show the green online dot next to this user in the chat list and chat screen. Only sent to users who share a chat room with the user who connected.

---

#### `user_offline` — A contact went offline
```json
{
  "type": "user_offline",
  "payload": {
    "userId": "507f..."
  }
}
```
> Remove the green online dot for this user. Only sent to users who share a chat room with the user who disconnected.

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
> Find the message by `messageId` and update its `status`. Used to show grey → blue ticks on individual messages.

---

#### `room_read` — Another participant read the entire room
```json
{
  "type": "room_read",
  "roomId": "63f1...",
  "payload": {
    "readBy": "507f..."
  }
}
```
> Update **all** your sent messages in `roomId` to `status: "read"` (blue ticks). This fires when the other user opens the chat room.

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
> If `emoji` is an **empty string `""`**, remove that user's reaction from the message. Otherwise, set `reactions[userId] = emoji`.

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
> Show "Alice is typing..." indicator in the chat screen.

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

Only typing events need to be sent from the client. All other state changes go through REST endpoints.

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

> Send `typing_start` when the user begins typing in a text field, and `typing_stop` when they stop (use a debounce timer of ~1-2 seconds of inactivity).

---

### Full WebSocket Event Summary

| Event `type` | Direction | Trigger |
|---|---|---|
| `message` | Server → Client | New message sent in a room |
| `room_updated` | Server → Client | New message sent — update chat list order |
| `user_online` | Server → Client | A contact's WebSocket connected |
| `user_offline` | Server → Client | A contact's WebSocket disconnected |
| `message_status_changed` | Server → Client | Individual message status updated |
| `room_read` | Server → Client | Another participant read the whole room |
| `reaction_updated` | Server → Client | Emoji added or removed on a message |
| `message_edited` | Server → Client | Message content changed |
| `message_deleted` | Server → Client | Message soft-deleted |
| `typing_start` | Client → Server → Client | User started typing |
| `typing_stop` | Client → Server → Client | User stopped typing |

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

## 11. Quick Reference — All Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/users/me` | Yes | Get my profile |
| `PATCH` | `/users/me` | Yes | Update my profile |
| `GET` | `/users/search?q=` | Yes | Search users |
| `POST` | `/connections/request` | Yes | Send friend request |
| `POST` | `/connections/:id/accept` | Yes | Accept friend request |
| `POST` | `/connections/:id/reject` | Yes | Reject friend request |
| `GET` | `/connections/pending` | Yes | Get pending requests |
| `GET` | `/connections/friends` | Yes | Get friends list |
| `GET` | `/chat/rooms` | Yes | Get chat list |
| `POST` | `/chat/rooms/direct/:id` | Yes | Open/create chat with user |
| `GET` | `/chat/rooms/:roomId/messages?limit=50&before=<id>` | Yes | Get message history (cursor paginated) |
| `POST` | `/chat/rooms/:roomId/messages` | Yes | Send a message |
| `POST` | `/chat/rooms/:roomId/read` | Yes | Mark room as read (blue ticks) |
| `PATCH` | `/chat/messages/:messageId/status` | Yes | Update individual message tick status |
| `PUT` | `/chat/messages/:messageId/reactions` | Yes | Add/remove emoji reaction |
| `PATCH` | `/chat/messages/:messageId` | Yes | Edit a message |
| `DELETE` | `/chat/messages/:messageId` | Yes | Delete a message |
| `GET` | `/chat/users/:id/presence` | Yes | Get user online status (on-demand) |
| `WS` | `/chat/ws?token=<token>` | Yes | WebSocket connection |
| `GET` | `/health` | No | Health check |

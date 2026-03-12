# Chat List Search & Pagination API Documentation
## ChatBee Backend — Frontend Integration Guide

---

## Overview

The chat list API now supports **search** and **pagination** for better performance and user experience. This allows users to:
- Search for chats by participant name or room name
- Paginate through large chat lists efficiently
- Get real-time metadata (total count, hasMore flag)

---

## API Endpoint

### `GET /api/v1/chat/rooms`

Retrieves the list of chat rooms for the authenticated user with optional search and pagination.

#### Authentication
**Required** — Bearer token in `Authorization` header

```
Authorization: Bearer <FIREBASE_ID_TOKEN>
```

#### Query Parameters

| Parameter | Type   | Required | Default | Description                                                                 |
|-----------|--------|----------|---------|------------------------------------------------------------------------------|
| `q`       | string | No       | `""`    | Search query — searches participant display names and room names            |
| `limit`   | int    | No       | `20`    | Number of rooms per page (max 50)                                           |
| `offset`  | int    | No       | `0`     | Number of rooms to skip (for pagination)                                     |

#### Parameter Rules
- `limit` is capped at **50** (values > 50 will be clamped)
- `limit` of 0 or negative defaults to **20**
- `offset` negative values default to **0**
- Empty `q` returns all rooms (sorted by most recent activity)

---

## Response Format

### Success Response (200 OK)

```json
{
  "success": true,
  "message": "Rooms retrieved",
  "data": {
    "rooms": [
      {
        "id": "67d1234567890abcdef12345",
        "type": "direct",
        "name": "",
        "otherUser": {
          "id": "67d1234567890abcdef67890",
          "displayName": "John Doe",
          "photoURL": "https://...",
          "email": "john@example.com",
          "isOnline": true
        },
        "participants": [
          {
            "id": "67d1234567890abcdef12345",
            "displayName": "You",
            "photoURL": "https://...",
            "email": "you@example.com",
            "isOnline": true
          },
          {
            "id": "67d1234567890abcdef67890",
            "displayName": "John Doe",
            "photoURL": "https://...",
            "email": "john@example.com",
            "isOnline": true
          }
        ],
        "lastMessage": "Hey, how are you?",
        "lastMessageType": "text",
        "lastMessageSenderName": "John Doe",
        "unreadCount": 3,
        "isDirect": true,
        "lastUpdated": "2026-03-12T10:30:00Z"
      }
    ],
    "totalCount": 150,
    "hasMore": true,
    "limit": 20,
    "offset": 0
  }
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `rooms` | array | List of room objects |
| `rooms[].id` | string | Room ID (MongoDB ObjectID) |
| `rooms[].type` | string | `"direct"` or `"group"` |
| `rooms[].name` | string | Room name (empty for direct chats) |
| `rooms[].otherUser` | object | **Convenience field** — the other participant (only for direct chats) |
| `rooms[].participants` | array | All participants with online status |
| `rooms[].lastMessage` | string | Last message preview |
| `rooms[].lastMessageType` | string | Type: `text`, `image`, `video`, etc. |
| `rooms[].lastMessageSenderName` | string | Display name of last message sender |
| `rooms[].unreadCount` | int | Number of unread messages for current user |
| `rooms[].isDirect` | boolean | True if direct chat, false if group |
| `rooms[].lastUpdated` | string | ISO 8601 timestamp of last activity |
| `totalCount` | int | Total number of rooms matching the query |
| `hasMore` | boolean | True if more pages available |
| `limit` | int | Actual limit used for this query |
| `offset` | int | Actual offset used for this query |

---

## Usage Examples

### 1. Get First Page (Default)

**Request:**
```http
GET /api/v1/chat/rooms
Authorization: Bearer eyJhbGciOiJSUzI1NiIs...
```

**Response:**
```json
{
  "success": true,
  "message": "Rooms retrieved",
  "data": {
    "rooms": [...],
    "totalCount": 45,
    "hasMore": true,
    "limit": 20,
    "offset": 0
  }
}
```

---

### 2. Search for Chats

**Request:**
```http
GET /api/v1/chat/rooms?q=john&limit=10
Authorization: Bearer eyJhbGciOiJSUzI1NiIs...
```

**How Search Works:**
- Searches **participant display names** (case-insensitive partial match)
- Searches **room names** for group chats
- Returns rooms where current user is a participant AND matches search

**Response:**
```json
{
  "success": true,
  "message": "Rooms retrieved",
  "data": {
    "rooms": [
      {
        "id": "...",
        "otherUser": {
          "displayName": "John Smith",  // Matched "john"
          ...
        }
      },
      {
        "id": "...",
        "otherUser": {
          "displayName": "Johnny Doe",  // Matched "john"
          ...
        }
      }
    ],
    "totalCount": 5,
    "hasMore": false,
    "limit": 10,
    "offset": 0
  }
}
```

---

### 3. Paginate Through Results

**First Page:**
```http
GET /api/v1/chat/rooms?limit=20&offset=0
```

**Second Page:**
```http
GET /api/v1/chat/rooms?limit=20&offset=20
```

**Third Page:**
```http
GET /api/v1/chat/rooms?limit=20&offset=40
```

**When to Stop:** When `hasMore` is `false` or `rooms` array is empty

---

### 4. Search with Pagination

**Request:**
```http
GET /api/v1/chat/rooms?q=mike&limit=10&offset=10
Authorization: Bearer eyJhbGciOiJSUzI1NiIs...
```

This searches for "mike" and returns results 11-20 (skipping first 10 matches).

---

## Error Responses

### 401 Unauthorized
```json
{
  "success": false,
  "statusCode": 401,
  "message": "Unauthorized"
}
```

### 500 Internal Server Error
```json
{
  "success": false,
  "statusCode": 500,
  "message": "Error description"
}
```

---

## Frontend Implementation Guide

### Flutter / Dart Example

```dart
class ChatListRepository {
  final Dio _dio;
  
  ChatListRepository(this._dio);
  
  /// Fetches paginated chat list
  Future<ChatListResponse> getChatList({
    String? searchQuery,
    int limit = 20,
    int offset = 0,
  }) async {
    final response = await _dio.get(
      '/api/v1/chat/rooms',
      queryParameters: {
        if (searchQuery != null && searchQuery.isNotEmpty) 'q': searchQuery,
        'limit': limit,
        'offset': offset,
      },
    );
    
    return ChatListResponse.fromJson(response.data['data']);
  }
}

// Response model
class ChatListResponse {
  final List<Room> rooms;
  final int totalCount;
  final bool hasMore;
  final int limit;
  final int offset;
  
  ChatListResponse({
    required this.rooms,
    required this.totalCount,
    required this.hasMore,
    required this.limit,
    required this.offset,
  });
  
  factory ChatListResponse.fromJson(Map<String, dynamic> json) {
    return ChatListResponse(
      rooms: (json['rooms'] as List)
          .map((r) => Room.fromJson(r))
          .toList(),
      totalCount: json['totalCount'],
      hasMore: json['hasMore'],
      limit: json['limit'],
      offset: json['offset'],
    );
  }
}
```

### Infinite Scroll Implementation

```dart
class ChatListController extends StateNotifier<ChatListState> {
  final ChatListRepository _repository;
  int _currentOffset = 0;
  bool _hasMore = true;
  String? _currentSearch;
  
  Future<void> loadMore() async {
    if (!_hasMore || state.isLoadingMore) return;
    
    state = state.copyWith(isLoadingMore: true);
    
    try {
      final response = await _repository.getChatList(
        searchQuery: _currentSearch,
        limit: 20,
        offset: _currentOffset,
      );
      
      _hasMore = response.hasMore;
      _currentOffset += response.rooms.length;
      
      state = state.copyWith(
        rooms: [...state.rooms, ...response.rooms],
        isLoadingMore: false,
        hasMore: _hasMore,
      );
    } catch (e) {
      state = state.copyWith(
        isLoadingMore: false,
        error: e.toString(),
      );
    }
  }
  
  Future<void> search(String query) async {
    _currentOffset = 0;
    _hasMore = true;
    _currentSearch = query.isEmpty ? null : query;
    
    state = state.copyWith(rooms: [], isLoading: true);
    
    try {
      final response = await _repository.getChatList(
        searchQuery: _currentSearch,
        limit: 20,
        offset: 0,
      );
      
      _hasMore = response.hasMore;
      _currentOffset = response.rooms.length;
      
      state = state.copyWith(
        rooms: response.rooms,
        isLoading: false,
        hasMore: _hasMore,
      );
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }
}
```

---

## Edge Cases & Testing Checklist

### Search Edge Cases

| Test Case | Expected Behavior |
|-----------|-------------------|
| Empty string `q=` | Returns all rooms (no filter) |
| Single character `q=a` | Returns rooms matching names with "a" |
| Special characters `q=@#$%` | Returns empty array (no matches) |
| SQL injection attempt `q=' OR '1'='1` | Treated as literal string, safe |
| Very long query (100+ chars) | Works but likely returns empty |
| Unicode characters `q=日本語` | Supports UTF-8 search |
| Case sensitivity `q=john` vs `q=JOHN` | Case-insensitive (both match "John") |
| Partial match `q=jo` | Matches "John", "Joseph", "Johan" |

### Pagination Edge Cases

| Test Case | Expected Behavior |
|-----------|-------------------|
| `offset=0` | Returns first page |
| `offset` > total count | Returns empty `rooms` array, `hasMore=false` |
| `limit=0` | Uses default (20) |
| `limit=100` | Clamped to 50 |
| `limit=-5` | Uses default (20) |
| `offset=-10` | Clamped to 0 |
| Large `offset=10000` | Works if user has that many rooms |

### Combined Scenarios

| Test Case | Expected Behavior |
|-----------|-------------------|
| Search + pagination | Returns paginated search results |
| Search with 0 results | `rooms=[]`, `totalCount=0`, `hasMore=false` |
| Exact page boundary | `hasMore=false` when last page |
| Single room result | `rooms=[oneItem]`, `hasMore=false` |

---

## Backward Compatibility

The existing endpoint `GET /api/v1/chat/rooms` **remains fully backward compatible**:

- **Old clients** (no query params) → Returns all rooms with new metadata wrapper
- **New clients** (with query params) → Returns paginated/search results

**Migration Path:**
1. Frontend can incrementally adopt new parameters
2. No breaking changes to existing functionality
3. Old response format is wrapped in new metadata structure

---

## Performance Notes

- **Index Support:** MongoDB indexes on `participants`, `lastUpdated`, and `displayName` ensure fast queries
- **Search Complexity:** O(n) where n = number of matching users (uses regex index)
- **Pagination:** O(limit) — constant time regardless of total room count
- **Recommended:** Use `limit=20` for smooth UX, fetch more on scroll

---

## Summary

| Feature | Status | Notes |
|---------|--------|-------|
| Basic pagination | ✅ Ready | Use `limit` and `offset` |
| Search by name | ✅ Ready | Use `q` parameter |
| Metadata (total, hasMore) | ✅ Ready | In response `data` object |
| Backward compatibility | ✅ Ready | Old clients work unchanged |
| MongoDB indexes | ✅ Ready | Optimized for performance |

**Ready for frontend implementation!** 🚀

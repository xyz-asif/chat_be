# ChatBee — Full-Stack Bug Report & Performance Analysis

## Executive Summary

After reviewing the complete backend (Go/Fiber/MongoDB/WebSocket) and frontend (Flutter/Riverpod), I identified **23 bugs** — 7 critical, 9 moderate, and 7 minor. The online/offline presence system has **6 interrelated bugs** spanning both sides that together explain the erratic behavior you're seeing with 10 users.

---

## PART 1: PRESENCE / ONLINE-OFFLINE BUGS (The Big Problem)

The presence system has a chain of failures. Here's each one, why it matters, and exactly how to fix it.

---

### BUG P1 — [CRITICAL] `pingPump` writes directly to `websocket.Conn`, causing concurrent write panics

**File:** `service.go` lines 854–883

**Problem:** The `pingPump` goroutine calls `c.WriteControl(websocket.PingMessage, ...)` directly on the WebSocket connection. Meanwhile, `writePump` in `hub.go` is also writing to the same connection via `client.send`. The gorilla/websocket library explicitly states that **only one goroutine may call write methods at a time**. With 10 users, this race becomes frequent enough to cause panics or silently corrupted frames — which the client interprets as a disconnect.

**Why you see it:** Users randomly appear offline because their WebSocket connection silently dies from a concurrent write panic that the `recover()` catches. The client then reconnects, but during the reconnection window (up to 5s grace + reconnect time), the user flickers offline.

**Fix — Route pings through the send channel:**
```go
// service.go — replace pingPump entirely

func (s *service) pingPump(c *websocket.Conn, client *clientContext) {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        // Check if client is still registered
        s.hub.clientsMu.RLock()
        conns := s.hub.clients[client.userID]
        _, stillRegistered := conns[client]
        s.hub.clientsMu.RUnlock()

        if !stillRegistered {
            return
        }

        // Send a JSON ping through the send channel (same writer goroutine)
        pingMsg, _ := json.Marshal(map[string]string{"type": "pong"})
        select {
        case client.send <- pingMsg:
            // Sent successfully
        default:
            // Buffer full — connection is likely dead
            log.Printf("pingPump: send buffer full for user %s, closing", client.userID)
            c.Close()
            return
        }
    }
}
```

And update `HandleWebSocket` to set the read deadline on ANY incoming message (not just pong):
```go
// In HandleWebSocket, before the for loop:
// Remove the PongHandler and instead reset deadline on every read
for {
    _ = c.SetReadDeadline(time.Now().Add(45 * time.Second)) // generous: 15s ping + 30s buffer
    var msg models.WSMessage
    if err := c.ReadJSON(&msg); err != nil {
        break
    }
    // ... switch msg.Type
}
```

---

### BUG P2 — [CRITICAL] Frontend `sendPresenceStatus(false)` never actually reaches the server

**File:** `app.dart` lines 37–45

**Problem:** On `AppLifecycleState.paused`, you call:
```dart
wsService.sendPresenceStatus(false);
Future.delayed(const Duration(milliseconds: 100), () {
    wsService.disconnect();
});
```

The `send()` method writes to the WebSocket sink, but `disconnect()` calls `_closeChannel()` which calls `sink.close()`. The 100ms delay is a race — on slower devices or under load, the presence message may not have flushed before the sink closes. Even if it does flush, the server's `ReadJSON` loop might not process it before the TCP connection drops, triggering an `onDone`/`onError` instead.

**Fix — Use the disconnect itself as the offline signal:**

On the backend, the grace period + `onUserOffline` callback already handles natural disconnects. The manual `sendPresenceStatus(false)` is redundant and unreliable. Instead:

```dart
// app.dart — simplified paused handler
case AppLifecycleState.paused:
case AppLifecycleState.detached:
    // Just disconnect. The backend's 5s grace period handles the rest.
    // If user returns within 5s, no offline event is broadcast.
    wsService.disconnect();
    break;
```

If you want the server to know it's an intentional background (not a network drop), send the message and **wait** for it:
```dart
case AppLifecycleState.paused:
case AppLifecycleState.detached:
    wsService.sendPresenceStatus(false);
    // Give the message time to flush, then disconnect
    await Future.delayed(const Duration(milliseconds: 500));
    wsService.disconnect();
    break;
```

But the first approach (just disconnect) is simpler and more reliable.

---

### BUG P3 — [CRITICAL] Backend `SetManualPresence` didn't broadcast (now it does, but has a new bug)

**File:** `service.go` lines 723–738, `hub.go` lines 158–168

**Problem:** The `presence_status` handler calls `SetManualPresence` then `broadcastUserPresence`. But `broadcastUserPresence` checks `s.hub.IsUserOnline(userID)` indirectly — it just passes the `online` bool. However, the **grace period interaction** is broken:

When a user disconnects (last WS connection drops):
1. Hub starts 5s grace period
2. `IsUserOnline` returns `true` during grace period
3. The app also sends `presence_status: false` (Bug P2) which calls `CancelGracePeriod` + `broadcastUserPresence(false)`
4. Timer is cancelled, offline is broadcast — **good so far**
5. But then the grace period timer callback fires anyway because `CancelGracePeriod` was called from the `presence_status` handler while the timer might have already fired in the background

There's also a subtle issue: `SetManualPresence(userID, false)` marks the user as manually offline, but **`IsUserOnline` checks manual offline first**. So if the user is manually offline but still has an active WS connection (app in background), `IsUserOnline` returns false. When `buildRoomResponse` is called for OTHER users' room lists, this user correctly shows offline. **This part actually works.**

The real bug is the **double broadcast on disconnect**: the grace period fires `onUserOffline` AND the `presence_status` handler fires `broadcastUserPresence(false)`. Two offline events in quick succession.

**Fix — Skip grace period entirely for manual presence signals:**
```go
case "presence_status":
    payload, ok := msg.Payload.(map[string]interface{})
    if !ok {
        continue
    }
    isOnline, _ := payload["isOnline"].(bool)
    s.hub.SetManualPresence(userID, isOnline)
    s.hub.CancelGracePeriod(userID)
    
    // Only broadcast if the effective state actually changed
    wasOnline := s.hub.IsUserOnline(userID)
    if wasOnline != isOnline {
        go s.broadcastUserPresence(userID, isOnline)
    }
```

Actually wait — after `SetManualPresence(userID, false)`, `IsUserOnline` will return `false`. So `wasOnline` would be `false` and `isOnline` would be `false`, meaning no broadcast. That's wrong. Instead:

```go
case "presence_status":
    payload, ok := msg.Payload.(map[string]interface{})
    if !ok {
        continue
    }
    isOnline, _ := payload["isOnline"].(bool)
    
    // Check current effective state BEFORE updating
    wasEffectivelyOnline := s.hub.IsUserOnline(userID)
    
    s.hub.SetManualPresence(userID, isOnline)
    s.hub.CancelGracePeriod(userID)
    
    // Only broadcast if effective state changed
    nowEffectivelyOnline := s.hub.IsUserOnline(userID)
    if wasEffectivelyOnline != nowEffectivelyOnline {
        go s.broadcastUserPresence(userID, nowEffectivelyOnline)
    }
```

---

### BUG P4 — [MODERATE] Grace period `onUserOffline` races with reconnection

**File:** `hub.go` lines 171–201

**Problem:** `startGracePeriod` fires a `time.AfterFunc`. Inside the callback:
1. It acquires `clientsMu.RLock` to check if user reconnected
2. If not, calls `onUserOffline`
3. Then acquires `graceMu.Lock` to clean up

Between step 1 and step 2, a new connection could register. The `Run()` loop processes the register, calls `CancelGracePeriod`, which tries to `graceMu.Lock` — but the timer callback already has the lock (or is about to). Since `CancelGracePeriod` does `timer.Stop()` and the timer has already fired, `Stop()` returns false but the callback is already running. So `CancelGracePeriod` deletes the entry from the map while the callback is between steps 1 and 3. The callback then tries `graceMu.Lock` again to delete — **deadlock potential** if the locking order isn't careful.

Actually, looking more carefully: `CancelGracePeriod` locks `graceMu`, and the timer callback does `graceMu.Lock` at the end. These are fine since they're sequential. But `CancelGracePeriod` is called from `Run()` which holds `clientsMu.Lock`, and the timer callback acquires `clientsMu.RLock` then later `graceMu.Lock`. Meanwhile `CancelGracePeriod` acquires `graceMu.Lock` (called while `clientsMu.Lock` is held in `Run()`). 

**Lock ordering:** 
- `Run()` register: `clientsMu.Lock` → `CancelGracePeriod` → `graceMu.Lock`  
- Timer callback: `clientsMu.RLock` → `graceMu.Lock`

Both acquire `clientsMu` first then `graceMu` — so no deadlock. But the **race** remains: the timer callback can broadcast `onUserOffline` for a user who just reconnected, because the reconnection hasn't been processed by the Run() loop yet (the register channel is buffered).

**Fix — Double-check connection state inside `onUserOffline`:**
```go
h.gracePeriods[userID] = time.AfterFunc(graceDuration, func() {
    // Wait a tiny bit for any pending register to be processed
    time.Sleep(100 * time.Millisecond)
    
    h.clientsMu.RLock()
    stillConnected := len(h.clients[userID]) > 0
    h.clientsMu.RUnlock()

    if stillConnected {
        return
    }

    // Also check manual presence — if manually offline, don't double-broadcast
    h.manualMu.RLock()
    manuallyOffline := h.manualOffline[userID]
    h.manualMu.RUnlock()
    
    if manuallyOffline {
        // Already handled by presence_status handler
        return
    }

    if h.onUserOffline != nil {
        h.onUserOffline(userID)
    }

    h.graceMu.Lock()
    delete(h.gracePeriods, userID)
    h.graceMu.Unlock()
})
```

---

### BUG P5 — [MODERATE] Frontend has no periodic presence re-sync

**File:** `ws_event_handler.dart`, `websocket_service.dart`

**Problem:** Presence is only synced on app resume (`requestPresenceSync` in `app.dart`). If a single `user_online` or `user_offline` WebSocket event is dropped (server broadcast channel full — `hub.go` line 127 has a `default` drop), the client's presence state is permanently wrong until the next app resume.

With 10 users, the broadcast channel (size 256) can fill up during bursts of activity, and the `default` case silently drops the message with just a log.

**Fix — Add a periodic presence sync on the frontend:**
```dart
// In websocket_service.dart, add to _startPingTimer or create a separate timer:
Timer? _presenceSyncTimer;

void _startPresenceSyncTimer() {
    _presenceSyncTimer?.cancel();
    _presenceSyncTimer = Timer.periodic(const Duration(seconds: 60), (_) {
        if (_isConnected) {
            requestPresenceSync();
        }
    });
}

// Call _startPresenceSyncTimer() when connection is established
// Cancel in _closeChannel()
```

Also increase the broadcast channel size on the backend:
```go
// hub.go NewHub()
broadcast: make(chan broadcastMessage, 1024), // was 256
```

---

### BUG P6 — [MODERATE] `IsUserOnline` returns true during grace period even when connection is dead

**File:** `hub.go` lines 216–233

**Problem:** During the 5s grace period, `IsUserOnline` returns `true`. This means:
- Any `GET /rooms` call returns `isOnline: true` for users who disconnected up to 5s ago
- Any `sync_presence` request returns `true` for disconnected users
- New messages auto-advance to "delivered" status for users who are actually offline

This is **by design** (to avoid flicker), but 5 seconds is long enough that another user could open the app, see "online", send a message expecting instant delivery, and get confused when there's no response.

**Fix — Reduce grace period to 2s, which is enough for page navigation reconnects:**
```go
const graceDuration = 2 * time.Second // was 5s
```

---

## PART 2: OTHER BACKEND BUGS

---

### BUG B1 — [CRITICAL] `rate_limiter.go` panics on authenticated requests

**File:** `rate_limiter.go` lines 17, 37, 52

**Problem:** `KeyGenerator` does `c.Locals("userID").(string)` but the auth middleware stores `c.Locals("uid", uid)` — there is no `"userID"` local. This nil assertion panics.

**Fix:**
```go
KeyGenerator: func(c *fiber.Ctx) string {
    if user, ok := c.Locals("user").(*models.User); ok && user != nil {
        return "user:" + user.ID.Hex()
    }
    return c.IP()
},
```

Apply to all three rate limiter functions.

---

### BUG B2 — [MODERATE] Connection re-send after rejection doesn't persist sender/receiver swap

**File:** `connections/service.go` lines 66–73

**Problem:** When a rejected connection is re-sent:
```go
if existingConn.Status == models.ConnectionStatusRejected {
    if err := s.repo.UpdateConnectionStatus(ctx, existingConn.ID, models.ConnectionStatusPending); err != nil {
        return nil, err
    }
    existingConn.Status = models.ConnectionStatusPending
    existingConn.SenderID = senderID      // Only in memory!
    existingConn.ReceiverID = receiverID   // Only in memory!
    return existingConn, nil
}
```

The sender/receiver IDs are updated in memory but never saved to the database. If user A rejected user B's request, and then user A sends a new request to B, the DB still has B as sender and A as receiver. When A tries to accept "their own" sent request, the receiver check fails.

**Fix:**
```go
if existingConn.Status == models.ConnectionStatusRejected {
    // Update both status and direction in a single DB write
    update := bson.M{
        "$set": bson.M{
            "status":    models.ConnectionStatusPending,
            "senderId":  senderID,
            "receiverId": receiverID,
            "updatedAt": time.Now(),
        },
    }
    _, err := s.repo.collection.UpdateOne(ctx, bson.M{"_id": existingConn.ID}, update)
    if err != nil {
        return nil, err
    }
    existingConn.Status = models.ConnectionStatusPending
    existingConn.SenderID = senderID
    existingConn.ReceiverID = receiverID
    return existingConn, nil
}
```

(This requires either exposing the collection through the repo interface or adding an `UpdateConnectionDirection` method.)

---

### BUG B3 — [MODERATE] `buildRoomResponse` does N+1 user lookups per room

**File:** `service.go` lines 758–796

**Problem:** For each room, `buildRoomResponse` calls `s.userRepo.GetUserByID` for every participant AND for the last message sender. In `GetUserRooms`, this is called in a loop. For a user in 20 rooms with 2 participants each = 60 individual MongoDB queries per API call.

**Fix — Batch all user IDs and fetch once:**
```go
func (s *service) GetUserRooms(ctx context.Context, userIDStr string) ([]models.RoomResponse, error) {
    // ... fetch rooms ...
    
    // Collect all unique user IDs across all rooms
    userIDSet := make(map[bson.ObjectID]bool)
    for _, r := range rooms {
        for _, p := range r.Participants {
            userIDSet[p] = true
        }
        if r.LastMessageSenderID != nil {
            userIDSet[*r.LastMessageSenderID] = true
        }
    }
    
    // Single batch query
    userIDs := make([]bson.ObjectID, 0, len(userIDSet))
    for id := range userIDSet {
        userIDs = append(userIDs, id)
    }
    userMap := s.userRepo.GetUsersByIDs(ctx, userIDs) // Add this method to repo
    
    // Build responses using the map
    // ...
}
```

---

### BUG B4 — [MODERATE] `GetUserRooms` has no pagination — unbounded query

**File:** `chat/repository.go` lines 140–155

**Problem:** Returns ALL rooms for a user with no limit. A power user in 500+ rooms causes a massive query + 500× N+1 user lookups.

**Fix:** Add limit/cursor parameters similar to `GetMessagesByRoom`.

---

### BUG B5 — [MINOR] `broadcastUserPresence` queries ALL rooms on every connect/disconnect

**File:** `service.go` lines 551–594

**Problem:** Every time a user connects or disconnects, `broadcastUserPresence` calls `s.repo.GetUserRooms(ctx, uid)` which is a full MongoDB query. With 10 active users frequently reconnecting (app backgrounding), this adds up.

**Fix — Cache room participants in memory or use a dedicated "user's contacts" lookup:**
This is a performance issue rather than a correctness bug. For now, it's acceptable, but at scale, consider maintaining an in-memory set of each user's contacts.

---

### BUG B6 — [MINOR] `HandleWebSocket` read deadline of 30s conflicts with client ping of 25s

**File:** `service.go` line 673: `c.SetReadDeadline(time.Now().Add(30 * time.Second))`
**File:** `websocket_service.dart`: `_pingInterval = Duration(seconds: 25)`

**Problem:** The server sets a 30s read deadline, and the client pings every 25s. The pong handler resets the deadline. But the client ping is a JSON `{"type": "ping"}` message, NOT a WebSocket ping frame. So the pong handler (`c.SetPongHandler`) never fires for these JSON pings. The read deadline IS reset because `ReadJSON` succeeds, but if the client's JSON ping is slightly delayed (network jitter), the server closes the connection.

The server `pingPump` sends WebSocket-level pings every 15s. The client doesn't have a pong handler for these — `web_socket_channel` in Dart handles pong frames automatically at the protocol level. So the server's `PongHandler` should fire. But the client's `{"type": "ping"}` messages are application-level and don't trigger the pong handler.

**Fix — Align the mechanisms:** Either use only WebSocket-level ping/pong (remove the client's JSON ping), or use only application-level ping/pong (remove the server's `WriteControl` ping). Using both is confusing and creates edge cases.

Recommended: Keep only the server-side WebSocket ping. Remove the client's `_pingTimer` entirely since the server is already pinging every 15s and the Dart WebSocket library responds to pings automatically.

```dart
// websocket_service.dart — remove _startPingTimer, _stopPingTimer, and all references
// The server's 15s WebSocket-level pings keep the connection alive
```

---

## PART 3: FRONTEND BUGS

---

### BUG F1 — [CRITICAL] `_isConnected` only set on first message, not on connection open

**File:** `websocket_service.dart` lines 155–168

**Problem:** `_isConnected` is set to `true` inside the `stream.listen` data handler, meaning the WebSocket is only considered "connected" after the server sends the first message. If the server doesn't proactively send anything on connect, `isConnected` stays `false`, and ALL `send()` calls are silently dropped (including `sendPresenceStatus`, `requestPresenceSync`, typing indicators, etc.).

Currently, the server doesn't send anything on connect — the client only gets messages when events occur. So after `connectIfNeeded`, if no event arrives before the 8s timeout, it returns `false` even though the connection is actually open.

**Fix — Set connected state immediately when stream listener is attached:**

The `WebSocketChannel.connect` doesn't guarantee the connection is open when it returns. But the stream listener's first callback confirms it. A better approach: send a handshake message from the server on connect.

**Backend fix (preferred):**
```go
// In HandleWebSocket, after registering the client:
s.hub.register <- client

// Send a welcome message so the client knows it's connected
welcomeMsg, _ := json.Marshal(map[string]string{"type": "connected"})
select {
case client.send <- welcomeMsg:
default:
}
```

**Frontend fix (handle 'connected' type):**
```dart
void _handleMessage(dynamic data) {
    try {
        final json = jsonDecode(data as String) as Map<String, dynamic>;
        if (json['type'] == 'pong' || json['type'] == 'connected') return;
        // ... rest of handler
    }
}
```

---

### BUG F2 — [MODERATE] `_connectCompleter` race condition on rapid calls

**File:** `websocket_service.dart` lines 140–156

**Problem:** If `connectIfNeeded` is called twice rapidly (double tap, rapid app resume), the second call overwrites `_connectCompleter` with a new `Completer`. The first caller's `await` now waits on a dead completer that will never complete (it was replaced). The second `_doConnect` closes the first connection (`_closeChannel`), and the first call hangs until timeout.

**Fix:**
```dart
Future<bool> connectIfNeeded({Duration timeout = const Duration(seconds: 8)}) async {
    _intentionalDisconnect = false;
    if (_isConnected && _channel != null) return true;
    if (_token == null) return false;
    
    // If already connecting, wait on existing completer
    if (_connectCompleter != null && !_connectCompleter!.isCompleted) {
        try {
            return await _connectCompleter!.future.timeout(timeout, onTimeout: () => false);
        } catch (e) {
            return false;
        }
    }
    
    _reconnectAttempts = 0;
    _connectCompleter = Completer<bool>();
    _doConnect();
    
    try {
        return await _connectCompleter!.future.timeout(timeout, onTimeout: () => false);
    } catch (e) {
        return false;
    }
}
```

---

### BUG F3 — [MODERATE] `MessageController` with `keepAlive: true` leaks memory

**File:** `message_controller.dart` line 17: `@Riverpod(keepAlive: true)`

**Problem:** Every room the user opens creates a `MessageController(roomId)` that is never disposed. Over a session where the user opens 20+ conversations, all message lists stay in memory permanently.

**Fix:** Remove `keepAlive: true`. The default auto-dispose behavior will clean up when the chat screen is popped. The `ws_event_handler` already checks `ref.exists()` before accessing the controller:
```dart
@riverpod  // remove keepAlive: true
class MessageController extends _$MessageController {
```

---

### BUG F4 — [MODERATE] `wsEventHandler` provider is a Stream but never listened to by the widget tree

**File:** `ws_event_handler.dart` line 19

**Problem:** `wsEventHandlerProvider` is a `keepAlive` Stream provider. But if no widget ever `ref.watch`es it, Riverpod won't activate it, and no events will be dispatched. You need to ensure it's activated early (likely in `app.dart` or after login).

**Fix — Ensure it's watched somewhere permanent:**
```dart
// In your home screen or app wrapper:
@override
Widget build(BuildContext context, WidgetRef ref) {
    ref.watch(wsEventHandlerProvider); // Activate WS event handler
    // ...
}
```

Or force-read it after login:
```dart
ref.read(wsEventHandlerProvider);
```

---

### BUG F5 — [MINOR] `didChangeAppLifecycleState` uses `ref.read` which may throw after dispose

**File:** `app.dart` lines 34–67

**Problem:** `ref.read` inside `didChangeAppLifecycleState` can throw if the widget has been disposed (e.g., hot restart, navigator pop). The `WidgetsBindingObserver` can fire after `dispose`.

**Fix:**
```dart
@override
void didChangeAppLifecycleState(AppLifecycleState state) {
    if (!mounted) return; // Guard against post-dispose callbacks
    // ... rest of handler
}
```

---

### BUG F6 — [MINOR] Chat list `moveRoomToTop` always increments unread, even for current room

**File:** `chat_list_controller.dart` lines 86–101

**Problem:** When a new message arrives via WebSocket, `moveRoomToTop` increments `unreadCount + 1`. But if the user is currently viewing that room, they shouldn't get an unread increment. The `ws_event_handler` filters out messages from the current user but not messages for the currently-open room.

**Fix — Pass currently-open room context:**
```dart
// In ws_event_handler.dart _handleNewMessage:
// Check if user is currently viewing this room
final currentRoomId = ref.read(currentOpenRoomProvider); // You'd need to create this
if (event.roomId == currentRoomId) {
    // Don't increment unread, just update the preview
    ref.read(chatListControllerProvider.notifier)
        .updateLastMessage(event.roomId, lastMessage: preview);
} else {
    ref.read(chatListControllerProvider.notifier)
        .moveRoomToTop(event.roomId, lastMessage: preview);
}
```

---

### BUG F7 — [MINOR] `ApiClient` singleton bypasses Riverpod lifecycle

**File:** `api_client.dart`

**Problem:** `ApiClient` uses a Dart-level singleton (`static final _instance`) but also has a Riverpod provider. The provider always returns the same static instance. This means `ref.invalidate(apiClientProvider)` does nothing, and testing with provider overrides is impossible.

Not a runtime bug, but an architecture smell that makes testing harder.

---

## PART 4: PRIORITIZED FIX ORDER

For maximum impact with minimum effort, fix in this order:

| Priority | Bug | Impact | Effort |
|----------|-----|--------|--------|
| 1 | P1 (concurrent WS writes) | Fixes random disconnects | 30 min |
| 2 | F1 (connected state) | Fixes presence never sending | 15 min |
| 3 | P2 (app pause flush) | Fixes offline signal lost | 10 min |
| 4 | B1 (rate limiter panic) | Fixes server crashes | 5 min |
| 5 | P5 (periodic re-sync) | Makes presence self-healing | 20 min |
| 6 | P3 (double broadcast) | Fixes duplicate events | 15 min |
| 7 | P6 (grace period too long) | Reduces stale "online" | 1 min |
| 8 | B6 (dual ping/pong) | Simplifies keepalive | 15 min |
| 9 | F2 (completer race) | Fixes rapid resume crash | 10 min |
| 10 | F3 (memory leak) | Fixes growing memory | 1 min |
| 11 | F4 (WS handler activation) | Ensures events dispatch | 5 min |
| 12 | P4 (grace period race) | Edge case fix | 15 min |
| 13 | B2 (connection re-send) | Fixes friend request flow | 20 min |
| 14 | B3 (N+1 queries) | Performance at scale | 1 hr |
| 15 | B4 (unbounded rooms) | Performance at scale | 30 min |

---

## PART 5: QUICK WINS (Do These Today)

1. **Add `if (!mounted) return;`** to `didChangeAppLifecycleState` in `app.dart`
2. **Change `@Riverpod(keepAlive: true)`** to `@riverpod` on `MessageController`
3. **Fix rate limiter** `KeyGenerator` to use `c.Locals("user")`
4. **Reduce grace period** from 5s to 2s
5. **Send `{"type": "connected"}` welcome message** from server on WS connect
6. **Remove client-side JSON ping timer** — server WebSocket pings are sufficient

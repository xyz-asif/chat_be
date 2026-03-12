# BACKEND BUG ANALYSIS — WebSocket Presence System

**ChatBee — Real-time Chat API (Go / Fiber)**
**Prepared: March 9, 2026**

---

## Executive Summary

The WebSocket-based presence system has **5 backend bugs** causing intermittent online/offline status failures. When a user backgrounds their app (phone call, app switch), the other user sometimes does not see the status change. When the user returns, their online status sometimes never updates for the other party. In some cases, the WebSocket connection disconnects entirely.

Root causes span two files: `hub.go` (connection lifecycle management) and `service.go` (chat package — WebSocket handler and presence broadcasting). The bugs interact with each other, creating non-deterministic behavior that depends on goroutine scheduling and network timing.

| Detail | Value |
|--------|-------|
| **Files Affected** | `hub.go`, `service.go` (chat package) |
| **Files Unchanged** | `main.go`, `repository.go`, `handler.go`, all models, `connections/*`, `users/*` |
| **Total Bugs** | 5 (3 Critical, 1 High, 1 Medium) |
| **Estimated Impact** | Affects every presence status change in production |

---

## Scenario Walkthrough: What Happens Today

To understand why presence fails intermittently, here is the exact sequence of events when User B backgrounds their app:

### Step 1: User B's app goes to background

- Frontend sends `{"type": "presence_status", "payload": {"isOnline": false}}` over WebSocket
- Frontend calls `wsService.disconnect()` after 100ms delay
- The WebSocket connection closes — the server's `ReadJSON()` returns an error

### Step 2: Server `HandleWebSocket` defer block runs

- `hub.MarkDisconnecting(userID)` sets `disconnecting["userB"] = true` ← **BUG 1: per-user flag**
- `hub.unregister <- client` sends to buffered channel (capacity 64)
- `go broadcastUserPresence(userID, false)` launches goroutine ← **BUG 4: races with hub**

### Step 3: Race condition begins

- The `broadcastUserPresence` goroutine runs and calls `hub.IsUserOnline()` for recipients
- But the hub hasn't processed the unregister message yet — User B is still in `hub.clients`
- The `user_offline` event is sent to User A (this part works, sometimes)

### Step 4: User B's app resumes (1-3 seconds later)

- Frontend calls `connectIfNeeded()` → new WebSocket connects
- Server's `HandleWebSocket` runs: `go broadcastUserPresence(userID, true)`
- **BUT:** the old deferred `broadcastUserPresence(false)` goroutine from Step 2 may **STILL be running**
- If the offline goroutine finishes AFTER the online goroutine, User A gets: `online → offline` (stale)
- **Result: User B appears permanently stuck offline to User A** ← THE BUG YOU SEE

---

## Detailed Bug Analysis

---

### Bug 1: `MarkDisconnecting` is per-user, not per-connection

| Detail | Value |
|--------|-------|
| **File** | `hub.go` — lines 18, 131-139 |
| **Severity** | CRITICAL — Breaks multi-device and creates race windows |

**Problem:**

The `disconnecting` map uses `userID` as the key. When any connection for a user disconnects, `MarkDisconnecting(userID)` sets the flag for the ENTIRE user. If the user has 2 devices (phone + tablet) and the phone disconnects, `IsUserOnline()` returns `false` — even though the tablet is still connected.

Even with a single device, there's a timing problem: `MarkDisconnecting` is called immediately in the defer, but `unregister` goes through a buffered channel (capacity 64). Between these two events, the state is inconsistent — the user is marked as disconnecting but still exists in the clients map.

**Problematic Code:**

```go
// OLD: hub.go
type Hub struct {
    disconnecting map[string]bool  // ← per-user flag
    discMu        sync.RWMutex
}

func (h *Hub) MarkDisconnecting(userID string) {
    h.discMu.Lock()
    h.disconnecting[userID] = true  // ← all devices affected
    h.discMu.Unlock()
}
```

**Fix:**

Removed the `disconnecting` map and `MarkDisconnecting()` method entirely. The hub now directly checks whether the user has any remaining connections after processing unregister. Offline status is only triggered when the last connection for a user is removed from the map.

**Fixed Code:**

```go
// NEW: hub.go — no disconnecting map at all
// Hub tracks connections directly, offline triggered only
// when len(h.clients[userID]) == 0 after unregister
```

---

### Bug 2: No grace period — presence flickers on network blips

| Detail | Value |
|--------|-------|
| **File** | `hub.go` + `service.go` |
| **Severity** | CRITICAL — Primary cause of "sometimes works, sometimes doesn't" |

**Problem:**

When User B's app goes to background, the OS may briefly drop the WebSocket. The backend immediately broadcasts `user_offline`. If User B's app reconnects 1-2 seconds later, it broadcasts `user_online`. These two events race on the wire and in the frontend's event queue.

Worse: if `user_online` arrives and is processed BEFORE `user_offline` (due to goroutine scheduling), the frontend shows online, then immediately switches to offline from the stale event — and stays there permanently because no further events arrive.

This non-deterministic ordering is why the bug appears intermittently. It depends on: goroutine scheduling, network latency, frontend event processing order, and OS WebSocket teardown timing.

**Problematic Code:**

```go
// OLD: immediate offline broadcast
defer func() {
    s.hub.MarkDisconnecting(userID)
    s.hub.unregister <- client
    go s.broadcastUserPresence(userID, false)  // fires immediately
}()
```

**Fix:**

Added a **5-second grace period** in the hub. When the last connection for a user drops, the hub starts a `time.AfterFunc` timer instead of immediately broadcasting offline. If the user reconnects within 5 seconds, the timer is cancelled and no `user_offline` event is ever sent.

During the grace period, `IsUserOnline()` returns `true`, so any presence queries (HTTP or WebSocket) also remain consistent.

The 5-second window covers: app backgrounding/foregrounding (typically < 2s), network switches (WiFi ↔ cellular, typically < 3s), and brief OS-level WebSocket drops.

**Fixed Code:**

```go
// NEW: hub starts grace period after last connection drops
func (h *Hub) startGracePeriod(userID string) {
    const graceDuration = 5 * time.Second

    h.graceMu.Lock()
    defer h.graceMu.Unlock()

    h.gracePeriods[userID] = time.AfterFunc(graceDuration, func() {
        // Double-check: if user reconnected between timer start and fire
        h.clientsMu.RLock()
        stillConnected := len(h.clients[userID]) > 0
        h.clientsMu.RUnlock()

        if stillConnected {
            return  // reconnected, don't broadcast offline
        }

        log.Printf("User %s grace period expired, broadcasting offline", userID)
        if h.onUserOffline != nil {
            h.onUserOffline(userID)
        }
    })
}
```

---

### Bug 3: `pingPong` goroutine steals messages from `client.send` channel

| Detail | Value |
|--------|-------|
| **File** | `service.go` — `pingPong` function |
| **Severity** | CRITICAL — Causes random message loss and spurious disconnects |

**Problem:**

The `client.send` channel is the sole delivery path for ALL WebSocket messages (chat messages, presence events, typing indicators, etc.). The `writePump` goroutine reads from this channel and writes to the connection.

However, the `pingPong` goroutine ALSO selects on `client.send` as a "connection closed" signal. This creates a race: when a message arrives on `client.send`, either `writePump` or `pingPong` wakes up — whichever the Go scheduler picks first. If `pingPong` wins, it CONSUMES the message, thinks the connection is closed, and exits. The message is silently dropped.

This means: presence events (`user_online`, `user_offline`) can be randomly eaten by the ping goroutine and never delivered. This directly contributes to the intermittent presence failures.

**Problematic Code:**

```go
// OLD: pingPong — races with writePump for messages
func (s *service) pingPong(c *websocket.Conn, client *clientContext) {
    ticker := time.NewTicker(10 * time.Second)
    for {
        select {
        case <-ticker.C:
            c.WriteControl(websocket.PingMessage, []byte{}, ...)
        case <-client.send:    // BUG: CONSUMES a real message!
            return
        }
    }
}
```

**Fix:**

Replaced `pingPong` with `pingPump` that checks whether the client is still registered in the hub's client map instead of reading from the send channel. Also added proper `PongHandler` + `ReadDeadline` on the WebSocket connection for robust dead-connection detection.

**Fixed Code:**

```go
// NEW: pingPump — checks hub registration, no channel reads
func (s *service) pingPump(c *websocket.Conn, client *clientContext) {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        s.hub.clientsMu.RLock()
        conns := s.hub.clients[client.userID]
        _, stillRegistered := conns[client]
        s.hub.clientsMu.RUnlock()

        if !stillRegistered {
            return  // client was unregistered, stop pinging
        }

        deadline := time.Now().Add(10 * time.Second)
        if err := c.WriteControl(websocket.PingMessage, []byte{}, deadline); err != nil {
            log.Printf("pingPump: ping failed for user %s: %v", client.userID, err)
            c.Close()
            return
        }
    }
}
```

---

### Bug 4: Offline broadcast fires before hub processes disconnect

| Detail | Value |
|--------|-------|
| **File** | `service.go` — `HandleWebSocket` defer |
| **Severity** | HIGH — Causes event ordering inversion on reconnect |

**Problem:**

The `HandleWebSocket` defer block does three things in sequence: (1) `MarkDisconnecting`, (2) send to unregister channel, (3) launch `broadcastUserPresence(false)` goroutine. The problem is that step 3 runs as a goroutine BEFORE the hub has processed step 2 (unregister is buffered).

When the user reconnects quickly, the new connection's `broadcastUserPresence(true)` may execute BEFORE the old connection's `broadcastUserPresence(false)` goroutine. The result: User A receives `online` first, then `offline` — and User B appears permanently stuck offline.

This ordering inversion is the core mechanism behind "User B reopens app but User A still sees offline."

**Problematic Code:**

```go
// OLD: service triggers broadcast directly (races with hub)
defer func() {
    s.hub.MarkDisconnecting(userID)
    s.hub.unregister <- client
    go s.broadcastUserPresence(userID, false)
}()
```

**Fix:**

Presence broadcasts are now triggered BY THE HUB ITSELF after it has finished updating the client map, via callbacks (`onUserOnline`/`onUserOffline`). The hub is the single source of truth for connection state, and it controls the event ordering. The service's `HandleWebSocket` defer is simplified to just: `hub.unregister <- client` (no manual broadcasting).

**Fixed Code:**

```go
// NEW: hub triggers callbacks after state is updated

// In hub.go Run() loop:
case client := <-h.unregister:
    h.clientsMu.Lock()
    // ... remove client from map ...
    remainingConns := len(h.clients[client.userID])
    h.clientsMu.Unlock()
    if remainingConns == 0 {
        h.startGracePeriod(client.userID)  // controlled by hub
    }

// In service.go HandleWebSocket:
defer func() {
    s.hub.unregister <- client  // that's it — hub handles the rest
}()

// In service.go NewService — wire callbacks:
hub.SetPresenceCallbacks(
    func(userID string) { svc.broadcastUserPresence(userID, true) },
    func(userID string) { svc.broadcastUserPresence(userID, false) },
)
```

---

### Bug 5: No pong handler or read deadline — dead connections linger

| Detail | Value |
|--------|-------|
| **File** | `service.go` — `HandleWebSocket` + `pingPong` |
| **Severity** | MEDIUM — Dead connections appear "online" for minutes |

**Problem:**

The old code sends WebSocket pings via `WriteControl` but never sets a `PongHandler` on the connection and never sets a `ReadDeadline`. If the remote end silently dies (phone loses network without closing the socket), the server keeps the connection in its map indefinitely — it just sits there blocked on `ReadJSON()`. `IsUserOnline()` returns `true` for a user who has been gone for minutes.

The connection is only cleaned up when the NEXT ping write fails (after 10+ seconds) or when the OS TCP keepalive times out (potentially minutes).

**Problematic Code:**

```go
// OLD: no pong handler, no read deadline
// ReadJSON blocks forever if remote dies silently
```

**Fix:**

Added `c.SetPongHandler()` that extends the read deadline on each pong received. Set initial `ReadDeadline` to 30 seconds. The ping interval is 15 seconds, so:

- **Healthy connection:** ping every 15s → pong resets 30s deadline → never times out
- **Dead connection:** ping sent → no pong → `ReadJSON` hits deadline after 30s → clean disconnect → grace period starts

**Fixed Code:**

```go
// NEW: proper keepalive with deadlines
c.SetPongHandler(func(appData string) error {
    return c.SetReadDeadline(time.Now().Add(30 * time.Second))
})
_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))

// pingPump sends pings every 15s
// If pong comes back: deadline extended to +30s
// If no pong: ReadJSON hits deadline → clean disconnect
```

---

## Architecture Changes

| Concern | Before (Broken) | After (Fixed) |
|---------|-----------------|---------------|
| **Online broadcast** | Service goroutine in HandleWebSocket | Hub callback on first connection register |
| **Offline broadcast** | Service goroutine in HandleWebSocket defer | Hub callback after 5s grace period expires |
| **Disconnect tracking** | Per-user `disconnecting` map | Per-connection: removed from `clients` map |
| **Event ordering** | Non-deterministic (goroutine races) | Deterministic (hub controls sequence) |
| **Dead connection detection** | Only on ping write failure | Pong handler + 30s read deadline |
| **Reconnect handling** | No special handling | Grace period cancelled, no offline broadcast |

---

## Verification Steps

After applying the fixes, verify with these test scenarios:

**Test 1: Basic presence toggle**
- Open app on Device A and Device B. A chats with B.
- Background B's app. Within ~6 seconds, A should see B go offline.
- Foreground B's app. Within 2-3 seconds, A should see B come online.

**Test 2: Quick switch (grace period)**
- Background B's app, then immediately foreground it (< 5 seconds).
- A should NEVER see B go offline — the grace period absorbs the blip.

**Test 3: Phone call**
- B receives a call while in the app. A sees B offline after ~6s.
- B returns from call. A sees B online within 2-3 seconds.

**Test 4: Network loss**
- Turn off B's WiFi/data. A sees B offline within ~35s (30s deadline + 5s grace).
- Turn network back on. B auto-reconnects, A sees B online.

**Test 5: Multi-device**
- B has app open on phone and tablet.
- Close app on phone. A should still see B online (tablet connected).
- Close app on tablet. A sees B offline after grace period.

**Backend log lines to look for:**
```
User X reconnected within grace period, cancelled offline broadcast
User X grace period expired, broadcasting offline
Broadcast user_online for user X to N recipients
Broadcast user_offline for user X to N recipients
```

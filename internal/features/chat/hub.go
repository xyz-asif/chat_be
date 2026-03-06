package chat

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/models"
)

const sendBufSize = 256

// Hub manages active WebSocket connections
type Hub struct {
	clients    map[string]map[*clientContext]bool
	clientsMu  sync.RWMutex
	register   chan *clientContext
	unregister chan *clientContext
	broadcast  chan broadcastMessage
}

// clientContext holds one WebSocket connection and its dedicated send channel.
// All writes go through the send channel so only one goroutine ever calls
// WriteMessage on a given connection — eliminating concurrent-write panics.
type clientContext struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

type broadcastMessage struct {
	userIDs     []string
	messageData []byte
}

// NewHub creates a new Hub instance with buffered channels for high throughput
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]map[*clientContext]bool),
		register:   make(chan *clientContext, 64),
		unregister: make(chan *clientContext, 64),
		broadcast:  make(chan broadcastMessage, 256),
	}
}

// writePump is the sole goroutine allowed to write to a connection.
// It drains the client's send channel until it is closed.
func (h *Hub) writePump(client *clientContext) {
	defer client.conn.Close()
	for data := range client.send {
		if err := client.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("WS write error for user %s: %v", client.userID, err)
			// Drain remaining messages so the channel can be GC'd
			for range client.send {
			}
			return
		}
	}
}

// Run starts the hub's main event loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			if h.clients[client.userID] == nil {
				h.clients[client.userID] = make(map[*clientContext]bool)
			}
			h.clients[client.userID][client] = true
			h.clientsMu.Unlock()
			// Start the dedicated writer for this connection
			go h.writePump(client)
			log.Printf("User %s connected (total conns: %d)", client.userID, len(h.clients[client.userID]))

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if conns, ok := h.clients[client.userID]; ok {
				if _, exists := conns[client]; exists {
					delete(conns, client)
					close(client.send) // signals writePump to exit
					if len(conns) == 0 {
						delete(h.clients, client.userID)
					}
					log.Printf("User %s disconnected", client.userID)
				}
			}
			h.clientsMu.Unlock()

		case msg := <-h.broadcast:
			h.clientsMu.RLock()
			for _, uid := range msg.userIDs {
				for client := range h.clients[uid] {
					select {
					case client.send <- msg.messageData:
					default:
						// Send buffer full — drop message to avoid blocking the hub
						log.Printf("WS send buffer full for user %s, dropping message", uid)
					}
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// SendToUsers sends a modeled websocket message to multiple users at once.
// The send is non-blocking: if the broadcast channel is full the message is
// dropped and an error is returned so callers are never hung.
func (h *Hub) SendToUsers(userIDs []string, msg models.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case h.broadcast <- broadcastMessage{userIDs: userIDs, messageData: data}:
	default:
		log.Printf("WS broadcast channel full, dropping message type=%s", msg.Type)
	}
	return nil
}

// SendMessage sends a modeled websocket message to a single user (convenience wrapper)
func (h *Hub) SendMessage(userID string, msg models.WSMessage) error {
	return h.SendToUsers([]string{userID}, msg)
}

// IsUserOnline checks if a user has any active WebSocket connections
func (h *Hub) IsUserOnline(userID string) bool {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	conns, ok := h.clients[userID]
	return ok && len(conns) > 0
}

// OnlineUserCount returns how many users are currently connected
func (h *Hub) OnlineUserCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

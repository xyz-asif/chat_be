package chat

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/models"
)

// Hub manages active WebSocket connections
type Hub struct {
	clients    map[string]map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
	register   chan *clientContext
	unregister chan *clientContext
	broadcast  chan broadcastMessage
}

type clientContext struct {
	userID string
	conn   *websocket.Conn
	mu     sync.Mutex // Per-connection write mutex to prevent concurrent writes
}

type broadcastMessage struct {
	userIDs     []string // Send to multiple users at once
	messageData []byte
}

// NewHub creates a new Hub instance with buffered channels for high throughput
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]map[*websocket.Conn]bool),
		register:   make(chan *clientContext, 64),
		unregister: make(chan *clientContext, 64),
		broadcast:  make(chan broadcastMessage, 256),
	}
}

// Run starts the hub's main event loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			if h.clients[client.userID] == nil {
				h.clients[client.userID] = make(map[*websocket.Conn]bool)
			}
			h.clients[client.userID][client.conn] = true
			h.clientsMu.Unlock()
			log.Printf("User %s connected (total conns: %d)", client.userID, len(h.clients[client.userID]))

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if conns, ok := h.clients[client.userID]; ok {
				if _, exists := conns[client.conn]; exists {
					delete(conns, client.conn)
					client.conn.Close()
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
				if conns, ok := h.clients[uid]; ok {
					for conn := range conns {
						// Fire-and-forget write in a goroutine to avoid blocking the hub loop
						go func(c *websocket.Conn, data []byte) {
							if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
								log.Printf("WS write error for user: %v", err)
							}
						}(conn, msg.messageData)
					}
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// SendToUsers sends a modeled websocket message to multiple users at once
func (h *Hub) SendToUsers(userIDs []string, msg models.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.broadcast <- broadcastMessage{
		userIDs:     userIDs,
		messageData: data,
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

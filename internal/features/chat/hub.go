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
	// Map of userID to their active connection
	// Using a map of maps allows a single user to be connected from multiple devices
	clients    map[string]map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
	register   chan *clientContext
	unregister chan *clientContext
	broadcast  chan broadcastMessage
}

type clientContext struct {
	userID string
	conn   *websocket.Conn
}

type broadcastMessage struct {
	userID      string
	messageData []byte
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]map[*websocket.Conn]bool),
		register:   make(chan *clientContext),
		unregister: make(chan *clientContext),
		broadcast:  make(chan broadcastMessage),
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
			log.Printf("User %s connected via WS", client.userID)

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if conns, ok := h.clients[client.userID]; ok {
				if _, exists := conns[client.conn]; exists {
					delete(conns, client.conn)
					client.conn.Close()
					if len(conns) == 0 {
						delete(h.clients, client.userID)
					}
					log.Printf("User %s disconnected via WS", client.userID)
				}
			}
			h.clientsMu.Unlock()

		case msg := <-h.broadcast:
			h.clientsMu.RLock()
			conns, ok := h.clients[msg.userID]
			if ok {
				for conn := range conns {
					if err := conn.WriteMessage(websocket.TextMessage, msg.messageData); err != nil {
						log.Printf("WS Error sending message to %s: %v", msg.userID, err)
						// We don't remove the connection here, the read pump will handle the disconnect
					}
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// SendMessage sends a modeled websocket message to a specific user
func (h *Hub) SendMessage(userID string, msg models.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.broadcast <- broadcastMessage{
		userID:      userID,
		messageData: data,
	}
	return nil
}

// IsUserOnline checks if a user has any active WebSocket connections
func (h *Hub) IsUserOnline(userID string) bool {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	conns, ok := h.clients[userID]
	return ok && len(conns) > 0
}

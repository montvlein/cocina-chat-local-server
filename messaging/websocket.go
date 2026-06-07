package messaging

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/cocina/server-mvp/auth"
	"github.com/gorilla/websocket"
)

// Client represents a WebSocket connection
type Client struct {
	Conn   *websocket.Conn
	Send   chan []byte
	UserID string
	Mu     sync.Mutex
}

// Hub manages all active WebSocket connections
type Hub struct {
	clients    map[string]*Client // userID -> client (one per user for MVP)
	register   chan *Client
	unregister chan *Client
	broadcast  chan broadcastMessage
	mu         sync.RWMutex
	tokenSvc   *auth.TokenService
}

type broadcastMessage struct {
	userID string
	data   []byte
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan broadcastMessage, 256),
		tokenSvc:   auth.NewTokenService("cocina-mvp-secret-key-change-in-production"),
	}
}

// Run starts the hub event loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.UserID] = client
			h.mu.Unlock()
			log.Printf("Client registered: %s", client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("Client unregistered: %s", client.UserID)

		case msg := <-h.broadcast:
			h.mu.RLock()
			for _, c := range h.clients {
				if c.UserID != msg.userID {
					select {
					case c.Send <- msg.data:
					default:
						close(c.Send)
						delete(h.clients, c.UserID)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// NewClient creates a WebSocket client without registering it yet.
// Registration happens after successful identify.
func (h *Hub) NewClient(conn *websocket.Conn) *Client {
	return &Client{
		Conn:   conn,
		Send:   make(chan []byte, 256),
		UserID: "",
	}
}

// StartClient starts the read/write pumps for a client connection.
func (h *Hub) StartClient(client *Client) {
	go h.writePump(client)
	go h.ReadPump(client)
}

// UnregisterClient removes a client from the hub
func (h *Hub) UnregisterClient(userID string) {
	h.unregister <- &Client{UserID: userID}
}

// BroadcastMessage sends a message to all connected clients except the sender
func (h *Hub) BroadcastMessage(senderID string, data []byte) {
	h.broadcast <- broadcastMessage{
		userID: senderID,
		data:   data,
	}
}

// SendToUser sends a message to a specific user if they're connected
func (h *Hub) SendToUser(userID string, data []byte) bool {
	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case client.Send <- data:
		return true
	default:
		return false
	}
}

// writePump pumps messages from the send channel to the WebSocket connection
func (h *Hub) writePump(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			if !ok {
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			client.Mu.Lock()
			client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				client.Mu.Unlock()
				return
			}
			client.Mu.Unlock()

		case <-ticker.C:
			client.Mu.Lock()
			client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				client.Mu.Unlock()
				return
			}
			client.Mu.Unlock()
		}
	}
}

// ReadPump reads messages from the WebSocket connection and dispatches them
func (h *Hub) ReadPump(client *Client) {
	defer func() {
		h.unregister <- client
	}()

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			return
		}

		var wsMsg WSIncomingMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		h.handleClientMessage(client, wsMsg)
	}
}

// WSIncomingMessage represents a message received from a client
type WSIncomingMessage struct {
	Op      string                 `json:"op"`       // Operation type (client sends "op")
	Type    string                 `json:"type"`     // Fallback for compatibility
	Payload map[string]interface{} `json:"payload"`  // Client always uses "payload"
}

func (h *Hub) handleClientMessage(client *Client, msg WSIncomingMessage) {
	// Determine the actual type - check both "op" and "type" fields
	messageType := msg.Op
	if messageType == "" {
		messageType = msg.Type
	}

	switch messageType {
	case "identify":
		h.handleIdentify(client, msg.Payload)
	case "message":
		h.handleTextMessage(client.UserID, msg.Payload)
	case "typing":
		h.handleTypingIndicator(client.UserID, msg.Payload)
	case "ping":
		h.sendPong(client.UserID)
	default:
		log.Printf("Unknown message type: %s", messageType)
	}
}

func (h *Hub) handleIdentify(client *Client, payload map[string]interface{}) {
	token, _ := payload["token"].(string)
	if token == "" {
		log.Println("Identify failed: no token provided")
		return
	}

	// Validate the access token
	user, err := h.tokenSvc.ValidateAccessToken(token)
	if err != nil {
		log.Printf("Invalid token during identify: %v", err)
		return
	}

	log.Printf("Client identified successfully: %s", user.ID)

	client.UserID = user.ID
	h.register <- client
	// Send welcome message with user ID
	welcomeMsg := WSOutgoingMessage{
		Type:      "connected",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"user_id": user.ID,
			"message": "Successfully identified",
		},
	}

	data, _ := json.Marshal(welcomeMsg)
	client.Mu.Lock()
	client.Conn.WriteMessage(websocket.TextMessage, data)
	client.Mu.Unlock()
}

func (h *Hub) handleTextMessage(senderID string, payload map[string]interface{}) {
	receiverID, _ := payload["receiver_id"].(string)
	channelID, _ := payload["channel_id"].(string)
	content, _ := payload["content"].(string)

	wsMsg := WSOutgoingMessage{
		Type:      "message",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"sender_id":   senderID,
			"receiver_id": receiverID,
			"channel_id":  channelID,
			"content":     content,
		},
	}

	data, _ := json.Marshal(wsMsg)

	// Broadcast to all clients except sender
	h.BroadcastMessage(senderID, data)
}

func (h *Hub) handleTypingIndicator(userID string, payload map[string]interface{}) {
	channelID, _ := payload["channel_id"].(string)

	wsMsg := WSOutgoingMessage{
		Type:      "typing",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"user_id":    userID,
			"channel_id": channelID,
			"is_typing":  true,
		},
	}

	data, _ := json.Marshal(wsMsg)
	h.BroadcastMessage(userID, data)
}

func (h *Hub) sendPong(senderID string) {
	wsMsg := WSOutgoingMessage{
		Type:      "pong",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"timestamp": time.Now().UnixMilli(),
		},
	}

	data, _ := json.Marshal(wsMsg)
	h.SendToUser(senderID, data)
}

// WSOutgoingMessage represents a message sent to clients
type WSOutgoingMessage struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

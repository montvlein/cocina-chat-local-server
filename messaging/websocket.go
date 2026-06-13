package messaging

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/cocina/server-mvp/auth"
	"github.com/cocina/server-mvp/ids"
	"github.com/cocina/server-mvp/org"
	"github.com/cocina/server-mvp/types"
	"github.com/gorilla/websocket"
)

const (
	maxReplayBuffer = 500
	maxReplayGap    = 200
	typingTTL       = 5 * time.Second
)

// Client represents a WebSocket connection.
type Client struct {
	Conn       *websocket.Conn
	Send       chan []byte
	UserID     string
	SessionID  string
	seq        int64
	replayBuf  []wsEnvelope
	subscribed map[string]bool
	typingTTL  map[string]*time.Timer
	Mu         sync.Mutex
}

type wsEnvelope struct {
	Op      string                 `json:"op"`
	Seq     int64                  `json:"seq"`
	Payload map[string]interface{} `json:"payload"`
}

// Hub manages all active WebSocket connections.
type Hub struct {
	clients    map[string]*Client
	presence   map[string]string
	sessionBuf map[string][]wsEnvelope
	register   chan *Client
	unregister chan *Client
	broadcast  chan broadcastMessage
	mu         sync.RWMutex
	authSvc    *auth.AuthService
	orgSvc     *org.Service
	msgSvc     *MessageService
}

type broadcastMessage struct {
	userID string
	data   []byte
}

// NewWebSocketHub creates a new WebSocket hub.
func NewWebSocketHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		presence:   make(map[string]string),
		sessionBuf: make(map[string][]wsEnvelope),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan broadcastMessage, 256),
	}
}

func (h *Hub) SetAuthService(authSvc *auth.AuthService)   { h.authSvc = authSvc }
func (h *Hub) SetOrgService(orgSvc *org.Service)         { h.orgSvc = orgSvc }
func (h *Hub) SetMessageService(msgSvc *MessageService)  { h.msgSvc = msgSvc }

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.UserID] = client
			h.mu.Unlock()
			log.Printf("Client registered: %s (session %s)", client.UserID, client.SessionID)
			h.emitPresenceOnline(client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			if existing, ok := h.clients[client.UserID]; ok && existing == client {
				delete(h.clients, client.UserID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("Client unregistered: %s", client.UserID)
			if client.UserID != "" {
				h.emitPresenceOffline(client.UserID)
			}

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

func (h *Hub) NewClient(conn *websocket.Conn) *Client {
	return &Client{
		Conn:       conn,
		Send:       make(chan []byte, 256),
		subscribed: make(map[string]bool),
		typingTTL:  make(map[string]*time.Timer),
	}
}

func (h *Hub) StartClient(client *Client) {
	go h.writePump(client)
	go h.ReadPump(client)
}

func (h *Hub) UnregisterClient(userID string) {
	h.unregister <- &Client{UserID: userID}
}

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

func (h *Hub) enqueue(client *Client, op string, payload map[string]interface{}) []byte {
	client.Mu.Lock()
	defer client.Mu.Unlock()

	client.seq++
	evt := wsEnvelope{Op: op, Seq: client.seq, Payload: payload}
	client.replayBuf = append(client.replayBuf, evt)
	if len(client.replayBuf) > maxReplayBuffer {
		client.replayBuf = client.replayBuf[len(client.replayBuf)-maxReplayBuffer:]
	}

	if client.UserID != "" {
		h.mu.Lock()
		buf := append(h.sessionBuf[client.UserID], evt)
		if len(buf) > maxReplayBuffer {
			buf = buf[len(buf)-maxReplayBuffer:]
		}
		h.sessionBuf[client.UserID] = buf
		h.mu.Unlock()
	}

	data, _ := json.Marshal(evt)
	return data
}

func (h *Hub) sendToClient(client *Client, op string, payload map[string]interface{}) {
	data := h.enqueue(client, op, payload)
	select {
	case client.Send <- data:
	default:
	}
}

func (h *Hub) BroadcastRaw(senderID string, data []byte) {
	h.broadcast <- broadcastMessage{userID: senderID, data: data}
}

func (h *Hub) EmitMessageNew(senderID string, msg *types.Message, senderName string) {
	messagePayload := map[string]interface{}{
		"id":           msg.ID,
		"sender_id":    msg.SenderID,
		"sender_name":  senderName,
		"receiver_id":  msg.ReceiverID,
		"channel_id":   msg.ChannelID,
		"content":      msg.Content,
		"content_type": msg.ContentType,
		"is_read":      msg.IsRead,
		"created_at":   msg.CreatedAt,
	}

	payload := map[string]interface{}{
		"channel_id": msg.ChannelID,
		"message":    messagePayload,
	}

	if msg.ReceiverID != "" {
		h.mu.RLock()
		_, online := h.clients[msg.ReceiverID]
		h.mu.RUnlock()
		if online {
			h.mu.RLock()
			recipient := h.clients[msg.ReceiverID]
			h.mu.RUnlock()
			if recipient != nil {
				h.sendToClient(recipient, "message.new", payload)
				if h.msgSvc != nil {
					_ = h.msgSvc.MarkDelivered(msg.ID, msg.ReceiverID)
				}
				h.sendReceiptDelivered(senderID, msg, msg.ReceiverID)
			}
		}
		return
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.UserID != senderID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	for _, client := range clients {
		h.sendToClient(client, "message.new", payload)
	}
}

func (h *Hub) sendReceiptDelivered(senderID string, msg *types.Message, recipientID string) {
	h.mu.RLock()
	senderClient, ok := h.clients[senderID]
	h.mu.RUnlock()
	if !ok {
		return
	}

	payload := map[string]interface{}{
		"message_id":    msg.ID,
		"channel_id":    msg.ChannelID,
		"recipient_id":  recipientID,
		"delivered_at":  time.Now().UTC().Format(time.RFC3339),
	}
	h.sendToClient(senderClient, "receipt.delivered", payload)
}

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

type WSIncomingMessage struct {
	Op      string                 `json:"op"`
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

func (h *Hub) handleClientMessage(client *Client, msg WSIncomingMessage) {
	messageType := msg.Op
	if messageType == "" {
		messageType = msg.Type
	}

	switch messageType {
	case "identify":
		h.handleIdentify(client, msg.Payload)
	case "subscribe":
		h.handleSubscribe(client, msg.Payload)
	case "typing.start":
		h.handleTypingStart(client, msg.Payload)
	case "typing.stop":
		h.handleTypingStop(client, msg.Payload)
	case "receipt.read":
		h.handleReceiptRead(client, msg.Payload)
	case "presence":
		h.handlePresenceUpdate(client.UserID, msg.Payload)
	case "ping":
		h.sendPong(client)
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

	if h.authSvc == nil {
		log.Println("Identify failed: auth service not configured")
		return
	}

	user, err := h.authSvc.ValidateAccessToken(token)
	if err != nil {
		log.Printf("Invalid token during identify: %v", err)
		return
	}

	lastSeq := int64(0)
	if v, ok := payload["last_seq"].(float64); ok {
		lastSeq = int64(v)
	}

	client.UserID = user.ID
	client.SessionID = ids.NewUUID()

	h.mu.RLock()
	if buf := h.sessionBuf[user.ID]; len(buf) > 0 {
		client.seq = buf[len(buf)-1].Seq
		client.replayBuf = append([]wsEnvelope{}, buf...)
	}
	h.mu.RUnlock()

	h.register <- client

	if lastSeq > 0 && h.tryReplay(client, lastSeq) {
		return
	}

	h.sendHello(client, user)
}

func (h *Hub) tryReplay(client *Client, lastSeq int64) bool {
	h.mu.RLock()
	events := h.sessionBuf[client.UserID]
	h.mu.RUnlock()

	var toReplay []wsEnvelope
	for _, evt := range events {
		if evt.Seq > lastSeq {
			toReplay = append(toReplay, evt)
		}
	}
	if len(toReplay) == 0 {
		return false
	}
	if toReplay[0].Seq-lastSeq > maxReplayGap {
		return false
	}

	client.Mu.Lock()
	defer client.Mu.Unlock()

	for _, evt := range toReplay {
		data, _ := json.Marshal(evt)
		client.Conn.WriteMessage(websocket.TextMessage, data)
		client.replayBuf = append(client.replayBuf, evt)
	}
	return true
}

func (h *Hub) sendHello(client *Client, user *types.User) {
	onlineIDs := h.onlineUserIDs()

	payload := map[string]interface{}{
		"server_version": "0.1.0-mvp",
		"server_mode":    "saas",
		"session_id":     client.SessionID,
		"user": map[string]interface{}{
			"id":              user.ID,
			"email":           user.Email,
			"username":        user.Username,
			"presence_status": h.getPresenceForUser(user.ID),
		},
		"workspaces": []types.Workspace{},
		"channels":   []types.Channel{},
		"presence": map[string]interface{}{
			"online_user_ids": onlineIDs,
		},
	}

	if h.orgSvc != nil {
		if hello, err := h.orgSvc.BuildHelloForUser(user.ID); err == nil && hello != nil {
			payload["workspaces"] = hello.Workspaces
			payload["channels"] = hello.Channels
			for _, ch := range hello.Channels {
				client.subscribed[ch.ID] = true
			}
		}
	}

	h.sendToClient(client, "hello", payload)
}

func (h *Hub) handleSubscribe(client *Client, payload map[string]interface{}) {
	if client.UserID == "" || h.orgSvc == nil {
		return
	}

	rawIDs, _ := payload["channel_ids"].([]interface{})
	for _, raw := range rawIDs {
		channelID, _ := raw.(string)
		if channelID == "" {
			continue
		}
		canAccess, err := h.orgSvc.UserCanAccessChannel(client.UserID, channelID)
		if err != nil || !canAccess {
			continue
		}
		client.subscribed[channelID] = true
	}
}

func (h *Hub) handleTypingStart(client *Client, payload map[string]interface{}) {
	channelID, _ := payload["channel_id"].(string)
	if channelID == "" || client.UserID == "" {
		return
	}

	if timer, ok := client.typingTTL[channelID]; ok {
		timer.Stop()
	}
	client.typingTTL[channelID] = time.AfterFunc(typingTTL, func() {
		h.fanoutTyping(client.UserID, channelID, "typing.stop")
		delete(client.typingTTL, channelID)
	})

	h.fanoutTyping(client.UserID, channelID, "typing.start")
}

func (h *Hub) handleTypingStop(client *Client, payload map[string]interface{}) {
	channelID, _ := payload["channel_id"].(string)
	if channelID == "" || client.UserID == "" {
		return
	}
	if timer, ok := client.typingTTL[channelID]; ok {
		timer.Stop()
		delete(client.typingTTL, channelID)
	}
	h.fanoutTyping(client.UserID, channelID, "typing.stop")
}

func (h *Hub) fanoutTyping(userID, channelID, op string) {
	payload := map[string]interface{}{
		"channel_id": channelID,
		"user_id":    userID,
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.UserID != userID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range clients {
		h.sendToClient(c, op, payload)
	}
}

func (h *Hub) handleReceiptRead(client *Client, payload map[string]interface{}) {
	channelID, _ := payload["channel_id"].(string)
	lastReadID, _ := payload["last_read_message_id"].(string)
	if channelID == "" || lastReadID == "" || client.UserID == "" || h.msgSvc == nil {
		return
	}

	if _, err := h.msgSvc.MarkChannelRead(client.UserID, channelID, lastReadID); err != nil {
		log.Printf("Failed to mark channel read: %v", err)
		return
	}

	readPayload := map[string]interface{}{
		"channel_id":             channelID,
		"reader_id":              client.UserID,
		"last_read_message_id":   lastReadID,
		"read_at":                time.Now().UTC().Format(time.RFC3339),
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.UserID != client.UserID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range clients {
		h.sendToClient(c, "receipt.read", readPayload)
	}
}

func (h *Hub) sendPong(client *Client) {
	h.sendToClient(client, "pong", map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
	})
}

func (h *Hub) getPresenceForUser(userID string) string {
	if h.authSvc != nil {
		if user, err := h.authSvc.GetUserByID(userID); err == nil && user.PresenceStatus != "" {
			h.mu.Lock()
			h.presence[userID] = user.PresenceStatus
			h.mu.Unlock()
			return user.PresenceStatus
		}
	}
	h.mu.RLock()
	status, ok := h.presence[userID]
	h.mu.RUnlock()
	if ok && status != "" {
		return status
	}
	return "available"
}

func (h *Hub) handlePresenceUpdate(userID string, payload map[string]interface{}) {
	status, _ := payload["status"].(string)
	if status == "" {
		return
	}
	switch status {
	case "available", "offline", "dnd":
	default:
		return
	}

	if h.authSvc != nil {
		if _, err := h.authSvc.UpdatePresenceStatus(userID, status); err != nil {
			log.Printf("Failed to update presence for %s: %v", userID, err)
			return
		}
	}

	h.mu.Lock()
	h.presence[userID] = status
	h.mu.Unlock()

	h.broadcastLegacyPresence(userID, status)
	if status == "offline" {
		h.emitPresenceOffline(userID)
	} else {
		h.emitPresenceOnline(userID)
	}
}

func (h *Hub) broadcastLegacyPresence(userID, status string) {
	payload := map[string]interface{}{
		"user_id": userID,
		"status":  status,
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		h.sendToClient(c, "presence", payload)
	}
}

func (h *Hub) emitPresenceOnline(userID string) {
	status := h.getPresenceForUser(userID)
	if status == "offline" {
		return
	}
	payload := map[string]interface{}{"user_id": userID}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.UserID != userID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range clients {
		h.sendToClient(c, "presence.online", payload)
	}
}

func (h *Hub) emitPresenceOffline(userID string) {
	payload := map[string]interface{}{"user_id": userID}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.UserID != userID {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range clients {
		h.sendToClient(c, "presence.offline", payload)
	}
}

func (h *Hub) onlineUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]string, 0, len(h.clients))
	for id := range h.clients {
		status := h.presence[id]
		if status == "" || status != "offline" {
			result = append(result, id)
		}
	}
	return result
}

func (h *Hub) BroadcastPresence(userID, status string) {
	h.mu.Lock()
	h.presence[userID] = status
	h.mu.Unlock()
	h.broadcastLegacyPresence(userID, status)
}

// WSOutgoingMessage kept for legacy welcome handler compatibility.
type WSOutgoingMessage struct {
	Type      string                 `json:"type"`
	Op        string                 `json:"op,omitempty"`
	Seq       int64                  `json:"seq,omitempty"`
	Timestamp time.Time              `json:"timestamp,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
}

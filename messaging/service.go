package messaging

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/cocina/server-mvp/types"
)

// MessageService handles message operations
type MessageService struct {
	db *sql.DB
}

// NewMessageService creates a new message service
func NewMessageService(db *sql.DB) *MessageService {
	return &MessageService{db: db}
}

// SendMessage sends a message to another user or channel
func (s *MessageService) SendMessage(senderID, receiverID, channelID, content, contentType string) (*types.Message, error) {
	messageID := generateMessageID()
	now := time.Now().UTC()

	_, err := s.db.Exec(
		"INSERT INTO messages (id, sender_id, receiver_id, channel_id, content, content_type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		messageID, senderID, receiverID, channelID, content, contentType, now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	msg := &types.Message{
		ID:          messageID,
		SenderID:    senderID,
		ReceiverID:  receiverID,
		ChannelID:   channelID,
		Content:     content,
		ContentType: contentType,
		CreatedAt:   now,
	}

	return msg, nil
}

// GetMessageHistory retrieves message history for a user or channel
func (s *MessageService) GetMessageHistory(userID string, limit int, beforeID string) ([]*types.Message, error) {
	query := `SELECT id, sender_id, receiver_id, channel_id, content, content_type, created_at 
	          FROM messages WHERE (receiver_id = ? OR channel_id IN (SELECT channel_id FROM message_participants WHERE user_id = ?))`

	args := []interface{}{userID, userID}

	if beforeID != "" {
		query += " AND id < ?"
		args = append(args, beforeID)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []*types.Message
	for rows.Next() {
		var msg types.Message
		var createdAtStr string
		err := rows.Scan(&msg.ID, &msg.SenderID, &msg.ReceiverID, &msg.ChannelID, &msg.Content, &msg.ContentType, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		messages = append(messages, &msg)
	}

	return messages, nil
}

// GetChannelMessages retrieves messages for a specific channel
func (s *MessageService) GetChannelMessages(channelID string, limit int, beforeID string) ([]*types.Message, error) {
	query := `SELECT id, sender_id, receiver_id, channel_id, content, content_type, created_at 
	          FROM messages WHERE channel_id = ?`

	args := []interface{}{channelID}

	if beforeID != "" {
		query += " AND id < ?"
		args = append(args, beforeID)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []*types.Message
	for rows.Next() {
		var msg types.Message
		var createdAtStr string
		err := rows.Scan(&msg.ID, &msg.SenderID, &msg.ReceiverID, &msg.ChannelID, &msg.Content, &msg.ContentType, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		messages = append(messages, &msg)
	}

	return messages, nil
}

func generateMessageID() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("msg_%d", now)
}

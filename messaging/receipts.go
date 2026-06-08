package messaging

import (
	"database/sql"
	"fmt"
	"time"
)

// MarkDelivered records that a message was delivered to a recipient.
func (s *MessageService) MarkDelivered(messageID, recipientID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO message_receipts (message_id, user_id, delivered_at)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id, user_id) DO UPDATE SET delivered_at = excluded.delivered_at`,
		messageID, recipientID, now,
	)
	return err
}

// MarkChannelRead marks messages as read for a user in a channel up to lastMessageID.
func (s *MessageService) MarkChannelRead(readerID, channelID, lastMessageID string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.Exec(`
		UPDATE messages
		SET is_read = 1
		WHERE channel_id = ?
		  AND sender_id != ?
		  AND (receiver_id = ? OR receiver_id IS NULL OR receiver_id = '')
		  AND created_at <= (SELECT created_at FROM messages WHERE id = ?)
		  AND is_read = 0`,
		channelID, readerID, readerID, lastMessageID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to mark messages read: %w", err)
	}

	count, _ := res.RowsAffected()

	_, err = s.db.Exec(`
		INSERT INTO message_receipts (message_id, user_id, read_at)
		SELECT id, ?, ?
		FROM messages
		WHERE channel_id = ?
		  AND sender_id != ?
		  AND created_at <= (SELECT created_at FROM messages WHERE id = ?)
		ON CONFLICT(message_id, user_id) DO UPDATE SET read_at = excluded.read_at`,
		readerID, now, channelID, readerID, lastMessageID,
	)
	if err != nil {
		return count, err
	}

	return count, nil
}

// IsDMChannelType checks if channel is a DM by querying the channels table.
func (s *MessageService) IsDMChannel(channelID string) (bool, error) {
	var chType string
	err := s.db.QueryRow(`SELECT type FROM channels WHERE id = ?`, channelID).Scan(&chType)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return chType == "dm", nil
}

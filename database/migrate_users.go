package database

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/cocina/server-mvp/ids"
)

func migrateUserIDsToUUID(db *sql.DB) error {
	rows, err := db.Query(`SELECT id FROM users WHERE id LIKE 'user_%'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var legacyIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		legacyIDs = append(legacyIDs, id)
	}
	if len(legacyIDs) == 0 {
		return nil
	}

	log.Printf("Migrating %d user IDs to UUID format", len(legacyIDs))

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}

	for _, oldID := range legacyIDs {
		newID := ids.NewUUID()
		if err := remapUserID(tx, oldID, newID); err != nil {
			return fmt.Errorf("migrate user %s: %w", oldID, err)
		}
	}

	if _, err := tx.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}

	return tx.Commit()
}

func remapUserID(tx *sql.Tx, oldID, newID string) error {
	updates := []struct {
		query string
		args  []interface{}
	}{
		{`UPDATE users SET id = ? WHERE id = ?`, []interface{}{newID, oldID}},
		{`UPDATE messages SET sender_id = ? WHERE sender_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE messages SET receiver_id = ? WHERE receiver_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE sessions SET user_id = ? WHERE user_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE org_members SET user_id = ? WHERE user_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE workspace_members SET user_id = ? WHERE user_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE channel_members SET user_id = ? WHERE user_id = ?`, []interface{}{newID, oldID}},
		{`UPDATE channels SET created_by = ? WHERE created_by = ?`, []interface{}{newID, oldID}},
		{`UPDATE dm_participants SET user_a = ? WHERE user_a = ?`, []interface{}{newID, oldID}},
		{`UPDATE dm_participants SET user_b = ? WHERE user_b = ?`, []interface{}{newID, oldID}},
	}

	for _, u := range updates {
		if _, err := tx.Exec(u.query, u.args...); err != nil {
			return err
		}
	}
	return nil
}

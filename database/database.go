package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Database wraps the SQL connection with convenience methods
type Database struct {
	conn *sql.DB
}

// New creates a new database instance and initializes the schema
func New(dbPath string) (*Database, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	_, err = conn.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		log.Printf("Warning: could not enable WAL mode: %v", err)
	}

	db := &Database{conn: conn}

	// Initialize schema
	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Println("Database initialized successfully")
	return db, nil
}

func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name TEXT,
		presence_status TEXT DEFAULT 'available',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		sender_id TEXT NOT NULL REFERENCES users(id),
		receiver_id TEXT REFERENCES users(id),
		channel_id TEXT,
		content TEXT NOT NULL,
		content_type TEXT DEFAULT 'text',
		is_read INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_id);
	CREATE INDEX IF NOT EXISTS idx_messages_receiver ON messages(receiver_id);
	CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id);
	CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at DESC);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id),
		token TEXT UNIQUE NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

	CREATE TABLE IF NOT EXISTS organizations (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		deployment_mode TEXT DEFAULT 'saas',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS org_members (
		user_id TEXT NOT NULL REFERENCES users(id),
		org_id TEXT NOT NULL REFERENCES organizations(id),
		role TEXT NOT NULL DEFAULT 'member',
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, org_id)
	);

	CREATE TABLE IF NOT EXISTS workspaces (
		id TEXT PRIMARY KEY,
		org_id TEXT NOT NULL REFERENCES organizations(id),
		name TEXT NOT NULL,
		slug TEXT NOT NULL,
		description TEXT,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(org_id, slug)
	);

	CREATE TABLE IF NOT EXISTS workspace_members (
		user_id TEXT NOT NULL REFERENCES users(id),
		workspace_id TEXT NOT NULL REFERENCES workspaces(id),
		role TEXT NOT NULL DEFAULT 'member',
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, workspace_id)
	);

	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL REFERENCES workspaces(id),
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'public',
		description TEXT,
		created_by TEXT REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_channels_workspace ON channels(workspace_id);

	CREATE TABLE IF NOT EXISTS channel_members (
		channel_id TEXT NOT NULL REFERENCES channels(id),
		user_id TEXT NOT NULL REFERENCES users(id),
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (channel_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS dm_participants (
		channel_id TEXT PRIMARY KEY REFERENCES channels(id),
		workspace_id TEXT NOT NULL REFERENCES workspaces(id),
		user_a TEXT NOT NULL,
		user_b TEXT NOT NULL,
		UNIQUE(workspace_id, user_a, user_b)
	);

	CREATE TABLE IF NOT EXISTS message_receipts (
		message_id TEXT NOT NULL REFERENCES messages(id),
		user_id TEXT NOT NULL REFERENCES users(id),
		delivered_at DATETIME,
		read_at DATETIME,
		PRIMARY KEY (message_id, user_id)
	);
	`

	_, err := d.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Migrate existing databases
	_, _ = d.conn.Exec(`ALTER TABLE users ADD COLUMN presence_status TEXT DEFAULT 'available'`)

	if err := d.seedDefaultOrg(); err != nil {
		return err
	}

	// Migrate legacy channel ids
	_, _ = d.conn.Exec(`UPDATE messages SET channel_id = 'ch_general' WHERE channel_id = 'general'`)

	if err := migrateUserIDsToUUID(d.conn); err != nil {
		log.Printf("Warning: user UUID migration failed: %v", err)
	}

	return nil
}

func (d *Database) seedDefaultOrg() error {
	var count int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := "datetime('now')"
	queries := []string{
		`INSERT INTO organizations (id, name, slug, deployment_mode) VALUES ('org_cocina_default', 'Cocina', 'cocina', 'saas')`,
		`INSERT INTO workspaces (id, org_id, name, slug, description, is_default) VALUES ('ws_cocina_default', 'org_cocina_default', 'General', 'general', 'Workspace principal', 1)`,
		`INSERT INTO channels (id, workspace_id, name, type, description) VALUES ('ch_general', 'ws_cocina_default', 'general', 'public', 'Canal principal de la organización')`,
	}
	for _, q := range queries {
		if _, err := d.conn.Exec(q); err != nil {
			return fmt.Errorf("seed failed: %w", err)
		}
	}
	_ = now

	// Attach existing users to default org/workspace
	_, _ = d.conn.Exec(`
		INSERT OR IGNORE INTO org_members (user_id, org_id, role, joined_at)
		SELECT id, 'org_cocina_default', 'member', CURRENT_TIMESTAMP FROM users`)
	_, _ = d.conn.Exec(`
		INSERT OR IGNORE INTO workspace_members (user_id, workspace_id, role, joined_at)
		SELECT id, 'ws_cocina_default', 'member', CURRENT_TIMESTAMP FROM users`)
	_, _ = d.conn.Exec(`
		INSERT OR IGNORE INTO channel_members (channel_id, user_id, joined_at)
		SELECT 'ch_general', id, CURRENT_TIMESTAMP FROM users`)

	return nil
}

// GetConn returns the underlying database connection
func (d *Database) GetConn() *sql.DB {
	return d.conn
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.conn.Close()
}

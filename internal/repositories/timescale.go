package repositories

import (
	"log"
	"strings"

	"gorm.io/gorm"
)

// InitTimescale enables the TimescaleDB extension and converts the chat_messages table
// into a hypertable partitioned by created_at with 7-day chunk intervals.
// The chat_messages table must already exist (created via GORM AutoMigrate).
func InitTimescale(db *gorm.DB) error {
	// Enable TimescaleDB extension.
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS timescaledb").Error; err != nil {
		return err
	}

	// Check if the chat_messages table exists before converting to hypertable.
	var exists bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_name = 'chat_messages'
		)
	`).Scan(&exists).Error
	if err != nil {
		return err
	}

	if !exists {
		log.Println("[Infra] chat_messages table does not exist yet, skipping hypertable creation")
		return nil
	}

	// Convert chat_messages to a TimescaleDB hypertable.
	if err := db.Exec(`
		SELECT create_hypertable(
			'chat_messages',
			'created_at',
			chunk_time_interval => INTERVAL '7 days',
			if_not_exists => TRUE,
			migrate_data => TRUE
		)
	`).Error; err != nil {
		// Ignore TS103 error — table may already be a hypertable or have conflicting unique indexes
		if !strings.Contains(err.Error(), "TS103") {
			return err
		}
		log.Printf("[Infra] TimescaleDB hypertable note: %v (continuing...)", err)
	}

	// Create composite index for room-based time-ordered queries.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_messages_room_time
		ON chat_messages (room_id, created_at DESC)
	`).Error; err != nil {
		return err
	}

	// Create partial index for thread replies.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_messages_parent
		ON chat_messages (parent_id, created_at ASC)
		WHERE parent_id IS NOT NULL
	`).Error; err != nil {
		return err
	}

	log.Println("[Infra] TimescaleDB hypertable initialized for chat_messages")
	return nil
}

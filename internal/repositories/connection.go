// Package repositories provides PostgreSQL connection initialization and schema migration.
package repositories

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// NewConnectionWithoutMigrate initializes a PostgreSQL connection using GORM v2
// with connection pooling but does NOT run AutoMigrate.
// Use this for CLI tools (e.g. cmd/migrate) where migration is controlled explicitly.
func NewConnectionWithoutMigrate(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=UTC",
		cfg.DBHost,
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBName,
		cfg.DBPort,
		cfg.DBSslMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to extract sql.DB from gorm: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(1 * time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	log.Info().Msg("[Infra] PostgreSQL GORM v2 connection pool established")

	return db, nil
}

// NewConnection initializes PostgreSQL connection using GORM v2 and configures connection pooling.
// WARNING: This function runs AutoMigrate on every call. In production, prefer
// NewConnectionWithoutMigrate + the cmd/migrate CLI tool for controlled migrations.
func NewConnection(cfg *config.Config) (*gorm.DB, error) {
	log.Warn().Msg("[Infra] NewConnection runs AutoMigrate on startup. Use 'cmd/migrate' CLI for production deployments.")

	db, err := NewConnectionWithoutMigrate(cfg)
	if err != nil {
		return nil, err
	}

	if err := RunMigrations(db); err != nil {
		return nil, fmt.Errorf("failed schema auto-migration: %w", err)
	}

	return db, nil
}

// RunMigrations automatically synchronizes all defined GORM entity tables into PostgreSQL schema.
// This is the single source of truth for all database models — call from cmd/migrate or NewConnection.
func RunMigrations(db *gorm.DB) error {
	log.Info().Msg("[Infra] Running GORM AutoMigrate...")
	err := db.AutoMigrate(
		&domain.Tenant{},
		&domain.User{},
		&domain.UserSettings{},
		&domain.Session{},
		&domain.Room{},
		&domain.RoomMember{},
		&domain.ReadPosition{},
		&domain.AuditLog{},
		&domain.File{},
		// TimescaleDB message storage (replaces ScyllaDB)
		&domain.ChatMessage{},
		&domain.ThreadCounter{},
	)
	if err != nil {
		return err
	}

	log.Info().Msg("[Infra] GORM AutoMigrate completed")

	// Backfill slugs for existing rooms without one
	backfillRoomSlugs(db)

	// Initialize TimescaleDB hypertable for chat_messages
	if err := InitTimescale(db); err != nil {
		log.Warn().Err(err).Msg("[Infra] TimescaleDB init skipped (extension may not be installed)")
		// Non-fatal — PostgreSQL works fine without TimescaleDB, just without hypertable optimization
	}

	return nil
}

// backfillRoomSlugs generates URL-friendly slugs for rooms that don't have one yet.
func backfillRoomSlugs(db *gorm.DB) {
	var rooms []domain.Room
	if err := db.Where("slug = '' OR slug IS NULL").Find(&rooms).Error; err != nil {
		log.Warn().Err(err).Msg("[Infra] Could not query rooms for slug backfill")
		return
	}
	if len(rooms) == 0 {
		return
	}

	log.Info().Int("count", len(rooms)).Msg("[Infra] Backfilling room slugs...")

	usedSlugs := make(map[string]bool)
	// Pre-load existing slugs
	var existing []struct{ Slug string }
	db.Model(&domain.Room{}).Where("slug != '' AND slug IS NOT NULL").Select("slug").Find(&existing)
	for _, e := range existing {
		usedSlugs[e.Slug] = true
	}

	for _, r := range rooms {
		base := domain.Slugify(r.Name)
		if base == "" {
			base = "dm-" + r.ID[:8] // DM rooms have no name, use short UUID
		}
		slug := base
		for i := 2; usedSlugs[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		usedSlugs[slug] = true
		db.Model(&domain.Room{}).Where("id = ?", r.ID).Update("slug", slug)
	}

	log.Info().Int("count", len(rooms)).Msg("[Infra] ✓ Room slugs backfilled")
}

// Migrate is a deprecated alias for RunMigrations. Use RunMigrations instead.
func Migrate(db *gorm.DB) error {
	return RunMigrations(db)
}

// PrintMigrationStatus lists all tables in the public schema with their row counts.
func PrintMigrationStatus(db *gorm.DB) {
	type tableInfo struct {
		TableName string
		RowCount  int64
	}

	var tables []string
	db.Raw(`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`).Scan(&tables)

	if len(tables) == 0 {
		fmt.Println("No tables found in public schema.")
		return
	}

	fmt.Printf("\n%-40s %s\n", "TABLE", "ROWS")
	fmt.Println(strings.Repeat("─", 52))

	for _, t := range tables {
		var count int64
		db.Raw(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, t)).Scan(&count)
		fmt.Printf("%-40s %d\n", t, count)
	}

	fmt.Println(strings.Repeat("─", 52))
	fmt.Printf("Total tables: %d\n\n", len(tables))
}

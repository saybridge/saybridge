package main

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/config"
	"github.com/saybridge/saybridge/pkg/crypto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Seed creates a default tenant and super admin user.
// Usage: go run cmd/seed/main.go
func main() {
	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=UTC",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, cfg.DBSslMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("[Seed] Connected to PostgreSQL")

	// ── 1. Ensure default tenant ────────────────────────────────────────────
	tenantID := uuid.New().String()
	var existingTenant domain.Tenant
	result := db.Where("domain = ?", "default.local").First(&existingTenant)
	if result.Error == nil {
		tenantID = existingTenant.ID
		log.Printf("[Seed] Default tenant already exists (id=%s), skipping creation", tenantID)
	} else {
		tenant := domain.Tenant{
			BaseModel: domain.BaseModel{
				ID:        tenantID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Name:     "Default Organization",
			Domain:   "default.local",
			Status:   "active",
			MaxUsers: 10000,
		}
		if err := db.Create(&tenant).Error; err != nil {
			log.Fatalf("[Seed] Failed to create default tenant: %v", err)
		}
		log.Printf("[Seed] Created default tenant: %s (id=%s)", tenant.Name, tenantID)
	}

	// ── 2. Create super admin user ──────────────────────────────────────────
	adminEmail := "admin@saybridge.io"

	var existingUser domain.User
	result = db.Where("email = ? AND tenant_id = ?", adminEmail, tenantID).First(&existingUser)
	if result.Error == nil {
		// User exists — update password hash to ensure it uses Argon2id
		passwordHash, err := crypto.HashPassword("Admin@123456")
		if err != nil {
			log.Fatalf("[Seed] Failed to hash password: %v", err)
		}
		db.Model(&existingUser).Update("password_hash", passwordHash)
		log.Printf("[Seed] Super admin already exists (id=%s), password hash updated to Argon2id", existingUser.ID)
		log.Println("[Seed] Done!")
		return
	}

	passwordHash, err := crypto.HashPassword("Admin@123456")
	if err != nil {
		log.Fatalf("[Seed] Failed to hash password: %v", err)
	}

	userID := uuid.New().String()
	now := time.Now()

	admin := domain.User{
		BaseModel: domain.BaseModel{
			ID:        userID,
			CreatedAt: now,
			UpdatedAt: now,
		},
		TenantID:     tenantID,
		Username:     "superadmin",
		Email:        adminEmail,
		PasswordHash: passwordHash,
		DisplayName:  "Super Admin",
		SystemRole:   "super_admin",
		Presence:     "offline",
		IsActive:     true,
	}

	if err := db.Create(&admin).Error; err != nil {
		log.Fatalf("[Seed] Failed to create super admin user: %v", err)
	}

	// ── 3. Create default settings for admin ────────────────────────────────
	settings := domain.UserSettings{
		UserID:               userID,
		Language:             "en",
		Theme:                "dark",
		Timezone:             "Asia/Ho_Chi_Minh",
		NotificationsEnabled: true,
		NotificationSound:    "default",
		DesktopNotifications: true,
		MobilePushEnabled:    true,
		EmailNotifications:   "all",
		MessagePreviewInPush: true,
		UpdatedAt:            now,
	}

	if err := db.Create(&settings).Error; err != nil {
		log.Fatalf("[Seed] Failed to create admin settings: %v", err)
	}

	log.Println("══════════════════════════════════════════════════════")
	log.Println("  ✅ Super Admin seeded successfully!")
	log.Println("══════════════════════════════════════════════════════")
	log.Printf("  Email:     %s", adminEmail)
	log.Printf("  Password:  Admin@123456")
	log.Printf("  Username:  superadmin")
	log.Printf("  Role:      super_admin")
	log.Printf("  Tenant:    default.local (id=%s)", tenantID)
	log.Println("══════════════════════════════════════════════════════")
	log.Println("[Seed] Done!")
}

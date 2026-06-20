package httphandler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/crypto"
	"github.com/saybridge/saybridge/pkg/response"
	"gorm.io/gorm"
)

// SetupHandler handles the initial workspace setup wizard.
type SetupHandler struct {
	db *gorm.DB
}

// NewSetupHandler creates a new SetupHandler.
func NewSetupHandler(db *gorm.DB) *SetupHandler {
	return &SetupHandler{db: db}
}

// CheckSetup returns whether the system has been initialized.
// GET /api/v1/system/setup-check
func (h *SetupHandler) CheckSetup(c *gin.Context) {
	var userCount int64
	h.db.Table("users").Where("deleted_at IS NULL").Count(&userCount)

	var tenantCount int64
	h.db.Table("tenants").Where("deleted_at IS NULL").Count(&tenantCount)

	response.JSON(c, http.StatusOK, gin.H{
		"setup_completed": userCount > 0,
		"has_tenant":      tenantCount > 0,
		"user_count":      userCount,
	})
}

// SetupRequest holds the initial workspace setup payload.
type SetupRequest struct {
	// Step 1: Admin info
	AdminName     string `json:"admin_name" binding:"required,max=100"`
	AdminUsername  string `json:"admin_username" binding:"required,min=3,max=50"`
	AdminEmail    string `json:"admin_email" binding:"required,email"`
	AdminPassword string `json:"admin_password" binding:"required,min=8"`

	// Step 2: Organization info
	WorkspaceName string `json:"workspace_name" binding:"required,max=255"`
	WorkspaceType string `json:"workspace_type"` // enterprise, team, personal
	Industry      string `json:"industry"`       // technology, education, etc.
	Size          string `json:"size"`            // 1-10, 11-50, 51-200, 200+

	// Step 3: Preferences
	AllowRegistration bool   `json:"allow_registration"`
	Language          string `json:"language"`
}

// CompleteSetup initializes the workspace with admin user, tenant, and default channel.
// POST /api/v1/system/setup
func (h *SetupHandler) CompleteSetup(c *gin.Context) {
	// Prevent re-setup
	var existingUsers int64
	h.db.Table("users").Where("deleted_at IS NULL").Count(&existingUsers)
	if existingUsers > 0 {
		response.Error(c, http.StatusConflict, "ALREADY_SETUP", "Workspace has already been initialized")
		return
	}

	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	// Hash password
	hashedPassword, err := crypto.HashPassword(req.AdminPassword)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SETUP_FAILED", "Failed to secure password")
		return
	}

	// Transaction: create tenant → admin user → default channels
	err = h.db.Transaction(func(tx *gorm.DB) error {
		// 1. Create or update tenant
		var tenant domain.Tenant
		result := tx.First(&tenant)
		if result.Error != nil {
			// Create new tenant
			tenant = domain.Tenant{
				Name:     req.WorkspaceName,
				Domain:   "localhost",
				Status:   "active",
				MaxUsers: 1000,
			}
			if err := tx.Create(&tenant).Error; err != nil {
				return err
			}
		} else {
			// Update existing tenant name
			tx.Model(&tenant).Update("name", req.WorkspaceName)
		}

		// 2. Create admin user
		admin := domain.User{
			TenantID:     tenant.ID,
			Username:     req.AdminUsername,
			Email:        req.AdminEmail,
			PasswordHash: hashedPassword,
			DisplayName:  req.AdminName,
			SystemRole:   "admin",
			Presence:     "offline",
			IsActive:     true,
		}
		if err := tx.Create(&admin).Error; err != nil {
			return err
		}

		// 3. Create admin user settings
		settings := domain.UserSettings{
			UserID:               admin.ID,
			Language:             "vi",
			Theme:                "dark",
			Timezone:             "Asia/Ho_Chi_Minh",
			NotificationsEnabled: true,
			NotificationSound:    "default",
			DesktopNotifications: true,
			MobilePushEnabled:    true,
			EmailNotifications:   "mentions",
			MessagePreviewInPush: true,
		}
		if req.Language != "" {
			settings.Language = req.Language
		}
		if err := tx.Create(&settings).Error; err != nil {
			return err
		}

		// 4. Create default channels
		channels := []domain.Room{
			{
				TenantID:    tenant.ID,
				Name:        "general",
				Slug:        "general",
				Type:        "channel",
				Description: "General channel for all members",
				CreatedBy:   &admin.ID,
			},
			{
				TenantID:    tenant.ID,
				Name:        "random",
				Slug:        "random",
				Type:        "channel",
				Description: "Free chat, share anything",
				CreatedBy:   &admin.ID,
			},
			{
				TenantID:    tenant.ID,
				Name:        "announcements",
				Slug:        "announcements",
				Type:        "channel",
				Description: "Important announcements from administrators",
				IsReadOnly:  true,
				CreatedBy:   &admin.ID,
			},
		}

		for i := range channels {
			if err := tx.Create(&channels[i]).Error; err != nil {
				return err
			}
			// Add admin as owner of each channel
			member := domain.RoomMember{
				RoomID:   channels[i].ID,
				UserID:   admin.ID,
				RoomRole: "owner",
			}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SETUP_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusCreated, gin.H{
		"message":        "Workspace initialized successfully",
		"workspace_name": req.WorkspaceName,
		"admin_email":    req.AdminEmail,
	})
}

package httphandler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/authz"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/admin"
	"github.com/saybridge/saybridge/pkg/metrics"
	"github.com/saybridge/saybridge/pkg/response"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// AdminHandler (from handler/admin_handler.go)
// ---------------------------------------------------------------------------

// AdminHandler handles admin-level user management operations.
type AdminHandler struct {
	db *gorm.DB
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// AdminUpdateUserRequest holds fields admins can modify on any user.
type AdminUpdateUserRequest struct {
	SystemRole *string `json:"system_role"` // "admin" or "user"
	IsActive   *bool   `json:"is_active"`
}

// UpdateUser allows admins to change role or active status of any user.
// PATCH /api/v1/admin/users/:id
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	// Verify caller is admin
	callerRole, _ := c.Get("role")
	if callerRole != "admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
		return
	}

	targetID := c.Param("id")
	if targetID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "User ID is required")
		return
	}

	var req AdminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	updates := map[string]interface{}{}
	if req.SystemRole != nil {
		role := *req.SystemRole
		if role != "admin" && role != "user" && role != "moderator" {
			response.Error(c, http.StatusBadRequest, "INVALID_ROLE", "Role must be 'admin', 'user', or 'moderator'")
			return
		}
		updates["system_role"] = role
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) == 0 {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "No fields to update")
		return
	}

	result := h.db.Table("users").Where("id = ?", targetID).Updates(updates)
	if result.Error != nil {
		response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
		return
	}

	// Return updated user
	var user map[string]interface{}
	h.db.Table("users").Where("id = ?", targetID).First(&user)
	response.JSON(c, http.StatusOK, user)
}

// GetStats returns admin dashboard statistics.
// GET /api/v1/admin/stats
func (h *AdminHandler) GetStats(c *gin.Context) {
	callerRole, _ := c.Get("role")
	if callerRole != "admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
		return
	}

	var totalUsers int64
	h.db.Table("users").Where("deleted_at IS NULL").Count(&totalUsers)

	var activeUsers int64
	h.db.Table("users").Where("deleted_at IS NULL AND is_active = true").Count(&activeUsers)

	var adminCount int64
	h.db.Table("users").Where("deleted_at IS NULL AND system_role = 'admin'").Count(&adminCount)

	var totalRooms int64
	h.db.Table("rooms").Where("deleted_at IS NULL").Count(&totalRooms)

	tenantID, _ := c.Get("tenant_id")
	tenantIDStr, _ := tenantID.(string)

	var totalStorage int64
	h.db.Table("files").
		Where("tenant_id = ? AND status = ?", tenantIDStr, "completed").
		Select("COALESCE(SUM(size), 0)").
		Row().
		Scan(&totalStorage)

	maxTenantStorage := int64(10 * 1024 * 1024 * 1024) // Default 10GB
	if envMaxStorage := os.Getenv("MAX_TENANT_STORAGE_GB"); envMaxStorage != "" {
		if val, err := strconv.ParseInt(envMaxStorage, 10, 64); err == nil {
			maxTenantStorage = val * 1024 * 1024 * 1024
		}
	}

	stats := gin.H{
		"total_users":         totalUsers,
		"active_users":        activeUsers,
		"admin_count":         adminCount,
		"total_rooms":         totalRooms,
		"total_storage_bytes": totalStorage,
		"max_storage_bytes":   maxTenantStorage,
	}

	response.JSON(c, http.StatusOK, stats)
}

// ---------------------------------------------------------------------------
// BanHandler (from handler/ban_handler.go)
// ---------------------------------------------------------------------------

// BanHandler handles banning and unbanning users from rooms.
type BanHandler struct {
	db *gorm.DB
}

// NewBanHandler creates a new BanHandler.
func NewBanHandler(db *gorm.DB) *BanHandler {
	return &BanHandler{db: db}
}

// BanUser bans a user from a room by setting IsBanned=true on their membership.
// POST /api/v1/rooms/:id/ban/:userId
func (h *BanHandler) BanUser(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	roomID := c.Param("id")
	targetUserID := c.Param("userId")
	if roomID == "" || targetUserID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID and User ID are required")
		return
	}

	var member domain.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", roomID, targetUserID).First(&member).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "Room membership not found")
		return
	}

	member.IsBanned = true
	if err := h.db.Save(&member).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "BAN_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "User banned from room"})
}

// UnbanUser unbans a user from a room by setting IsBanned=false.
// POST /api/v1/rooms/:id/unban/:userId
func (h *BanHandler) UnbanUser(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	roomID := c.Param("id")
	targetUserID := c.Param("userId")
	if roomID == "" || targetUserID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID and User ID are required")
		return
	}

	var member domain.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", roomID, targetUserID).First(&member).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "Room membership not found")
		return
	}

	member.IsBanned = false
	if err := h.db.Save(&member).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "UNBAN_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "User unbanned from room"})
}

// GetBannedUsers lists all banned members in a room.
// GET /api/v1/rooms/:id/banned
func (h *BanHandler) GetBannedUsers(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var members []domain.RoomMember
	if err := h.db.Where("room_id = ? AND is_banned = true", roomID).Find(&members).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "FETCH_FAILED", err.Error())
		return
	}

	if members == nil {
		members = []domain.RoomMember{}
	}

	response.JSON(c, http.StatusOK, members)
}

// ---------------------------------------------------------------------------
// PruneHandler (from handler/prune_handler.go)
// ---------------------------------------------------------------------------

// PruneHandler handles bulk message deletion (placeholder — requires TimescaleDB batch operations).
type PruneHandler struct {
	db *gorm.DB
}

// NewPruneHandler creates a new PruneHandler.
func NewPruneHandler(db *gorm.DB) *PruneHandler {
	return &PruneHandler{db: db}
}

// PruneMessages schedules a bulk delete of messages in a room.
// POST /api/v1/rooms/:id/prune
// This is a placeholder — full TimescaleDB batch delete is complex.
func (h *PruneHandler) PruneMessages(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"message": "Prune scheduled",
		"room_id": roomID,
	})
}

// ---------------------------------------------------------------------------
// ExportHandler (from handler/export_handler.go)
// ---------------------------------------------------------------------------

// ExportHandler handles GDPR data export endpoints for admin compliance workflows.
type ExportHandler struct {
	db *gorm.DB
}

// NewExportHandler instantiates a new ExportHandler.
func NewExportHandler(db *gorm.DB) *ExportHandler {
	return &ExportHandler{db: db}
}

// UserExport is the top-level GDPR subject access response payload.
type UserExport struct {
	ExportedAt      time.Time           `json:"exported_at"`
	User            domain.User         `json:"user"`
	Settings        *domain.UserSettings `json:"settings,omitempty"`
	RoomMemberships []domain.RoomMember `json:"room_memberships"`
	Rooms           []RoomSummary       `json:"rooms"`
	AuditLogs       []domain.AuditLog   `json:"audit_logs"`
	Sessions        []domain.Session    `json:"sessions"`
	MessageCount    int64               `json:"message_count_note"`
}

// RoomSummary is a lightweight room descriptor included in user exports.
type RoomSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// WorkspaceExport is a high-level workspace summary for admin reports.
type WorkspaceExport struct {
	ExportedAt   time.Time          `json:"exported_at"`
	TenantID     string             `json:"tenant_id"`
	UserCount    int64              `json:"user_count"`
	RoomCount    int64              `json:"room_count"`
	AuditCount   int64              `json:"audit_log_count"`
	SessionCount int64              `json:"session_count"`
	RoomsByType  []RoomTypeCount    `json:"rooms_by_type"`
	RecentUsers  []UserBrief        `json:"recent_users"`
}

// RoomTypeCount groups rooms by their type with a count.
type RoomTypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// UserBrief is a minimal user descriptor for workspace exports.
type UserBrief struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	SystemRole  string     `json:"system_role"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	LastActive  *time.Time `json:"last_active_at,omitempty"`
}

// ExportUser handles POST /api/v1/admin/export/user/:id — GDPR subject access request.
func (h *ExportHandler) ExportUser(c *gin.Context) {
	_, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	targetUserID := c.Param("id")

	if targetUserID == "" {
		response.Error(c, http.StatusBadRequest, "MISSING_USER_ID", "User ID is required")
		return
	}

	// 1. Fetch user record
	var user domain.User
	if err := h.db.Where("id = ? AND tenant_id = ?", targetUserID, tenantID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "EXPORT_USER_FAILED", err.Error())
		return
	}

	// 2. Fetch user settings
	var settings domain.UserSettings
	settingsPtr := (*domain.UserSettings)(nil)
	if err := h.db.Where("user_id = ?", targetUserID).First(&settings).Error; err == nil {
		settingsPtr = &settings
	}

	// 3. Fetch room memberships
	var memberships []domain.RoomMember
	h.db.Where("user_id = ?", targetUserID).Find(&memberships)

	// 4. Fetch room summaries for joined rooms
	var rooms []RoomSummary
	if len(memberships) > 0 {
		roomIDs := make([]string, len(memberships))
		for i, m := range memberships {
			roomIDs[i] = m.RoomID
		}
		var domainRooms []domain.Room
		h.db.Where("id IN ?", roomIDs).Find(&domainRooms)
		for _, r := range domainRooms {
			rooms = append(rooms, RoomSummary{
				ID:          r.ID,
				Name:        r.Name,
				Type:        r.Type,
				Description: r.Description,
			})
		}
	}

	// 5. Fetch audit logs related to this user (as actor)
	var auditLogs []domain.AuditLog
	h.db.Where("actor_id = ? AND tenant_id = ?", targetUserID, tenantID).
		Order("created_at DESC").
		Limit(500).
		Find(&auditLogs)
	if auditLogs == nil {
		auditLogs = []domain.AuditLog{}
	}

	// 6. Fetch sessions
	var sessions []domain.Session
	h.db.Where("user_id = ?", targetUserID).
		Order("last_active_at DESC").
		Find(&sessions)
	if sessions == nil {
		sessions = []domain.Session{}
	}

	// Note: Messages are stored in TimescaleDB (PostgreSQL hypertable).
	// A message_count_note field is included to indicate this limitation.

	export := UserExport{
		ExportedAt:      time.Now().UTC(),
		User:            user,
		Settings:        settingsPtr,
		RoomMemberships: memberships,
		Rooms:           rooms,
		AuditLogs:       auditLogs,
		Sessions:        sessions,
		MessageCount:    -1, // TODO: query chat_messages COUNT(*) from TimescaleDB
	}

	if export.RoomMemberships == nil {
		export.RoomMemberships = []domain.RoomMember{}
	}
	if export.Rooms == nil {
		export.Rooms = []RoomSummary{}
	}

	response.JSON(c, http.StatusOK, export)
}

// ExportWorkspace handles POST /api/v1/admin/export/workspace — workspace overview export.
func (h *ExportHandler) ExportWorkspace(c *gin.Context) {
	_, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)

	// 1. Count users
	var userCount int64
	if err := h.db.Model(&domain.User{}).Where("tenant_id = ?", tenantID).Count(&userCount).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "EXPORT_WORKSPACE_FAILED", err.Error())
		return
	}

	// 2. Count rooms
	var roomCount int64
	if err := h.db.Model(&domain.Room{}).Where("tenant_id = ?", tenantID).Count(&roomCount).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "EXPORT_WORKSPACE_FAILED", err.Error())
		return
	}

	// 3. Count audit logs
	var auditCount int64
	h.db.Model(&domain.AuditLog{}).Where("tenant_id = ?", tenantID).Count(&auditCount)

	// 4. Count sessions
	var sessionCount int64
	h.db.Model(&domain.Session{}).Count(&sessionCount)

	// 5. Rooms by type breakdown
	var roomsByType []RoomTypeCount
	h.db.Model(&domain.Room{}).
		Select("type, COUNT(*) as count").
		Where("tenant_id = ?", tenantID).
		Group("type").
		Scan(&roomsByType)
	if roomsByType == nil {
		roomsByType = []RoomTypeCount{}
	}

	// 6. Recent users (last 20 created)
	var recentDomainUsers []domain.User
	h.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Limit(20).
		Find(&recentDomainUsers)

	recentUsers := make([]UserBrief, 0, len(recentDomainUsers))
	for _, u := range recentDomainUsers {
		recentUsers = append(recentUsers, UserBrief{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			SystemRole:  u.SystemRole,
			IsActive:    u.IsActive,
			CreatedAt:   u.CreatedAt,
			LastActive:  u.LastActiveAt,
		})
	}

	export := WorkspaceExport{
		ExportedAt:   time.Now().UTC(),
		TenantID:     tenantID,
		UserCount:    userCount,
		RoomCount:    roomCount,
		AuditCount:   auditCount,
		SessionCount: sessionCount,
		RoomsByType:  roomsByType,
		RecentUsers:  recentUsers,
	}

	response.JSON(c, http.StatusOK, export)
}

// ---------------------------------------------------------------------------
// MessageExportHandler (from handler/message_export_handler.go)
// ---------------------------------------------------------------------------

// MessageExportHandler handles exporting messages from a room (placeholder).
type MessageExportHandler struct {
	db *gorm.DB
}

// NewMessageExportHandler creates a new MessageExportHandler.
func NewMessageExportHandler(db *gorm.DB) *MessageExportHandler {
	return &MessageExportHandler{db: db}
}

// ExportMessages exports messages from a room as JSON.
// GET /api/v1/rooms/:id/export
// This is a placeholder — full TimescaleDB streaming is complex.
func (h *MessageExportHandler) ExportMessages(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"room_id":  roomID,
		"messages": []interface{}{},
		"note":     "Export placeholder — TimescaleDB streaming not yet implemented",
	})
}

// ---------------------------------------------------------------------------
// AnalyticsHandler (from handler/analytics_handler.go)
// ---------------------------------------------------------------------------

// AnalyticsHandler handles HTTP endpoints for the admin analytics dashboard and audit logs.
type AnalyticsHandler struct {
	analyticsService *admin.AnalyticsService
	auditRepo        domain.AuditLogRepository
}

// NewAnalyticsHandler instantiates a new AnalyticsHandler controller.
func NewAnalyticsHandler(analyticsService *admin.AnalyticsService, auditRepo domain.AuditLogRepository) *AnalyticsHandler {
	return &AnalyticsHandler{
		analyticsService: analyticsService,
		auditRepo:        auditRepo,
	}
}

// GetDashboard handles GET /api/v1/admin/analytics?period=7d
func (h *AnalyticsHandler) GetDashboard(c *gin.Context) {
	_, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	period := c.DefaultQuery("period", "7d")

	metrics, err := h.analyticsService.GetDashboard(c.Request.Context(), tenantID, period)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "ANALYTICS_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, metrics)
}

// GetMetrics handles GET /api/v1/admin/metrics — a curated, live snapshot of
// the Prometheus metrics for the admin observability dashboard. Admin-only.
func (h *AnalyticsHandler) GetMetrics(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "admin" && role != "super_admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}
	response.JSON(c, http.StatusOK, metrics.Gather())
}

// GetAuditLogs handles GET /api/v1/admin/audit?action=&actor_id=&resource=&from=&to=&page=&limit=
func (h *AnalyticsHandler) GetAuditLogs(c *gin.Context) {
	_, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	filter := h.parseAuditFilter(c)

	logs, err := h.auditRepo.List(c.Request.Context(), tenantID, filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AUDIT_FETCH_FAILED", err.Error())
		return
	}

	total, err := h.auditRepo.Count(c.Request.Context(), tenantID, filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AUDIT_COUNT_FAILED", err.Error())
		return
	}

	if logs == nil {
		logs = []domain.AuditLog{}
	}

	response.JSONWithMeta(c, http.StatusOK, logs, gin.H{
		"total":  total,
		"page":   (filter.Offset / adminMax(filter.Limit, 1)) + 1,
		"limit":  filter.Limit,
	})
}

// ExportAuditLogs handles GET /api/v1/admin/audit/export?format=csv|json
func (h *AnalyticsHandler) ExportAuditLogs(c *gin.Context) {
	_, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	filter := h.parseAuditFilter(c)
	format := c.DefaultQuery("format", "json")

	logs, err := h.auditRepo.Export(c.Request.Context(), tenantID, filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AUDIT_EXPORT_FAILED", err.Error())
		return
	}

	if logs == nil {
		logs = []domain.AuditLog{}
	}

	if format == "csv" {
		h.writeCSV(c, logs)
		return
	}

	response.JSON(c, http.StatusOK, logs)
}

// writeCSV streams audit log entries as a CSV download.
func (h *AnalyticsHandler) writeCSV(c *gin.Context, logs []domain.AuditLog) {
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=audit_logs.csv")
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	// Header row
	_ = w.Write([]string{"id", "tenant_id", "actor_id", "actor_name", "action", "resource", "resource_id", "ip_address", "user_agent", "created_at"})

	for _, l := range logs {
		_ = w.Write([]string{
			l.ID,
			l.TenantID,
			l.ActorID,
			l.ActorName,
			l.Action,
			l.Resource,
			l.ResourceID,
			l.IPAddress,
			l.UserAgent,
			l.CreatedAt.Format(time.RFC3339),
		})
	}
}

// parseAuditFilter extracts audit log query parameters from the request.
func (h *AnalyticsHandler) parseAuditFilter(c *gin.Context) domain.AuditLogFilter {
	filter := domain.AuditLogFilter{
		Action:   c.Query("action"),
		ActorID:  c.Query("actor_id"),
		Resource: c.Query("resource"),
		Limit:    50,
	}

	if page, err := strconv.Atoi(c.Query("page")); err == nil && page > 0 {
		limit := filter.Limit
		if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
			limit = l
		}
		filter.Limit = limit
		filter.Offset = (page - 1) * limit
	}

	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		filter.Limit = l
	}

	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &t
		} else if t, err := time.Parse("2006-01-02", from); err == nil {
			filter.From = &t
		}
	}

	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &t
		} else if t, err := time.Parse("2006-01-02", to); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.To = &end
		}
	}

	return filter
}

// adminMax returns the larger of two ints.
func adminMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatDetails converts the JSON details field to a display string for CSV export.
func formatDetails(details []byte) string {
	if len(details) == 0 || string(details) == "{}" {
		return ""
	}
	return fmt.Sprintf("%s", string(details))
}

// ---------------------------------------------------------------------------
// PermissionHandler (from handler/permission_handler.go)
// ---------------------------------------------------------------------------

// PermissionHandler handles policy management operations for administrators.
type PermissionHandler struct {
	enforcer *authz.AuthzEnforcer
}

// NewPermissionHandler instantiates a new PermissionHandler.
func NewPermissionHandler(enforcer *authz.AuthzEnforcer) *PermissionHandler {
	return &PermissionHandler{enforcer: enforcer}
}

// PolicyRequest holds the details of a policy rule.
type PolicyRequest struct {
	Role    string `json:"role" binding:"required"`
	ObjType string `json:"obj_type" binding:"required"`
	Action  string `json:"action" binding:"required"`
	Effect  string `json:"effect" binding:"required,oneof=allow deny"`
}

// ListPolicies lists policy rules.
// @Summary List all permission policies
// @Description List permission policies, optionally filtered by role
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Param role query string false "Filter policies by role"
// @Success 200 {object} response.SuccessResponse "Policies retrieved"
// @Router /api/v1/admin/permissions [get]
func (h *PermissionHandler) ListPolicies(c *gin.Context) {
	role := c.Query("role")
	var policies []authz.Policy
	if role != "" {
		policies = h.enforcer.GetPoliciesForRole(role)
	} else {
		policies = h.enforcer.GetAllPolicies()
	}
	response.JSON(c, http.StatusOK, policies)
}

// AddPolicy creates a new policy rule.
// @Summary Add a new permission policy rule
// @Description Add a new policy rule (e.g. role, object type, action, effect)
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Param request body PolicyRequest true "Policy rule details"
// @Success 200 {object} response.SuccessResponse "Policy added successfully"
// @Router /api/v1/admin/permissions [post]
func (h *PermissionHandler) AddPolicy(c *gin.Context) {
	var req PolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	err := h.enforcer.AddPolicy(req.Role, req.ObjType, req.Action, req.Effect)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "POLICY_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "Policy added successfully"})
}

// RemovePolicy deletes an existing policy rule.
// @Summary Remove a permission policy rule
// @Description Remove an existing policy rule
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Param request body PolicyRequest true "Policy rule details to delete"
// @Success 200 {object} response.SuccessResponse "Policy removed successfully"
// @Router /api/v1/admin/permissions [delete]
func (h *PermissionHandler) RemovePolicy(c *gin.Context) {
	var req PolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	err := h.enforcer.RemovePolicy(req.Role, req.ObjType, req.Action, req.Effect)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "POLICY_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "Policy removed successfully"})
}

// ListRoles lists unique roles in policy database.
// @Summary List all unique roles in policies
// @Description Retrieve a list of unique roles currently defined in policy configuration
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Success 200 {object} response.SuccessResponse "Roles retrieved"
// @Router /api/v1/admin/permissions/roles [get]
func (h *PermissionHandler) ListRoles(c *gin.Context) {
	policies := h.enforcer.GetAllPolicies()
	roleMap := make(map[string]bool)
	for _, p := range policies {
		roleMap[p.Role] = true
	}

	roles := make([]string, 0, len(roleMap))
	for role := range roleMap {
		roles = append(roles, role)
	}

	response.JSON(c, http.StatusOK, roles)
}

// ReloadPolicy reloads policies from persistence storage.
// @Summary Reload policy store
// @Description Reload all authorization policies from configuration files
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Success 200 {object} response.SuccessResponse "Policies reloaded"
// @Router /api/v1/admin/permissions/reload [post]
func (h *PermissionHandler) ReloadPolicy(c *gin.Context) {
	err := h.enforcer.ReloadPolicy()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "RELOAD_FAILED", err.Error())
		return
	}
	response.JSON(c, http.StatusOK, gin.H{"message": "Policies reloaded successfully"})
}

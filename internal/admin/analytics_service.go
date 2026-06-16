package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// AnalyticsService computes dashboard metrics by querying PostgreSQL directly.
// Message-related metrics are stored in TimescaleDB (PostgreSQL hypertable) and
// are queryable via SQL/GORM. This service computes metrics for users, rooms,
// sessions, and message counts.
type AnalyticsService struct {
	db *gorm.DB
}

// NewAnalyticsService creates a new AnalyticsService.
func NewAnalyticsService(db *gorm.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

// DashboardMetrics is imported from the domain package, but we use a local
// alias via the import. We return domain types directly.

// GetDashboard computes all dashboard metrics for a given tenant and time period.
// Period supports formats like "7d", "30d", "90d" (days).
func (s *AnalyticsService) GetDashboard(ctx context.Context, tenantID, period string) (*dashboardResult, error) {
	days := parsePeriodDays(period)
	since := time.Now().AddDate(0, 0, -days)

	dau, err := s.countActiveUsers(ctx, tenantID, 1)
	if err != nil {
		return nil, fmt.Errorf("dau: %w", err)
	}

	mau, err := s.countActiveUsers(ctx, tenantID, 30)
	if err != nil {
		return nil, fmt.Errorf("mau: %w", err)
	}

	totalUsers, err := s.countTotalUsers(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("total_users: %w", err)
	}

	totalRooms, err := s.countTotalRooms(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("total_rooms: %w", err)
	}

	// Session-based activity timeseries (users active per day based on sessions)
	activityTimeseries, err := s.activeUsersTimeseries(ctx, tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("activity_timeseries: %w", err)
	}

	topRooms, err := s.topRoomsByMembership(ctx, tenantID, 10)
	if err != nil {
		return nil, fmt.Errorf("top_rooms: %w", err)
	}

	storage, err := s.getStorageUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	health := s.getHealthMetrics()

	return &dashboardResult{
		DAU:            dau,
		MAU:            mau,
		TotalUsers:     totalUsers,
		TotalRooms:     totalRooms,
		TotalMessages:  0, // TODO: query chat_messages COUNT(*) via GORM
		MessagesPerDay: activityTimeseries,
		TopRooms:       topRooms,
		Storage:        storage,
		Health:         health,
	}, nil
}

// dashboardResult mirrors domain.DashboardMetrics to avoid circular import concerns.
type dashboardResult struct {
	DAU            int64              `json:"dau"`
	MAU            int64              `json:"mau"`
	TotalUsers     int64              `json:"total_users"`
	TotalRooms     int64              `json:"total_rooms"`
	TotalMessages  int64              `json:"total_messages"`
	MessagesPerDay []timeseriesPoint  `json:"messages_per_day"`
	TopRooms       []roomActivity     `json:"top_rooms"`
	Storage        storageMetrics     `json:"storage"`
	Health         healthMetrics      `json:"health"`
}

type timeseriesPoint struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
}

type roomActivity struct {
	RoomID      string `json:"room_id"`
	RoomName    string `json:"room_name"`
	MemberCount int64  `json:"member_count"`
	ActiveUsers int64  `json:"active_users"`
}

type storageMetrics struct {
	FilesBytes    int64 `json:"files_bytes"`
	DatabaseBytes int64 `json:"database_bytes"`
	LogsBytes     int64 `json:"logs_bytes"`
	TotalBytes    int64 `json:"total_bytes"`
	MaxBytes      int64 `json:"max_bytes"`
}

type healthMetrics struct {
	WSConnections  int64   `json:"ws_connections"`
	ErrorRate      float64 `json:"error_rate"`
	APILatencyP50  float64 `json:"api_latency_p50"`
	APILatencyP95  float64 `json:"api_latency_p95"`
	APILatencyP99  float64 `json:"api_latency_p99"`
	PluginsRunning int     `json:"plugins_running"`
	PluginsTotal   int     `json:"plugins_total"`
}

// countActiveUsers counts distinct users with a session active within the last N days.
func (s *AnalyticsService) countActiveUsers(ctx context.Context, tenantID string, days int) (int64, error) {
	since := time.Now().AddDate(0, 0, -days)
	var count int64
	err := s.db.WithContext(ctx).
		Table("users").
		Where("tenant_id = ? AND last_active_at > ? AND is_active = true", tenantID, since).
		Count(&count).Error
	return count, err
}

// countTotalUsers counts all active users for a tenant.
func (s *AnalyticsService) countTotalUsers(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Table("users").
		Where("tenant_id = ? AND is_active = true AND deleted_at IS NULL", tenantID).
		Count(&count).Error
	return count, err
}

// countTotalRooms counts all non-deleted rooms for a tenant.
func (s *AnalyticsService) countTotalRooms(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Table("rooms").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Count(&count).Error
	return count, err
}

// activeUsersTimeseries returns daily active user counts based on last_active_at.
func (s *AnalyticsService) activeUsersTimeseries(ctx context.Context, tenantID string, since time.Time) ([]timeseriesPoint, error) {
	type row struct {
		Date  string
		Value int64
	}
	var rows []row

	err := s.db.WithContext(ctx).
		Table("users").
		Select("DATE(last_active_at) as date, COUNT(*) as value").
		Where("tenant_id = ? AND last_active_at >= ? AND is_active = true", tenantID, since).
		Group("DATE(last_active_at)").
		Order("date ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]timeseriesPoint, len(rows))
	for i, r := range rows {
		result[i] = timeseriesPoint{Date: r.Date, Value: r.Value}
	}
	return result, nil
}

// topRoomsByMembership returns the top rooms by member count.
func (s *AnalyticsService) topRoomsByMembership(ctx context.Context, tenantID string, limit int) ([]roomActivity, error) {
	type row struct {
		RoomID      string
		RoomName    string
		MemberCount int64
	}
	var rows []row

	err := s.db.WithContext(ctx).
		Table("rooms").
		Select("rooms.id as room_id, rooms.name as room_name, COUNT(rm.user_id) as member_count").
		Joins("LEFT JOIN room_members rm ON rm.room_id = rooms.id").
		Where("rooms.tenant_id = ? AND rooms.deleted_at IS NULL", tenantID).
		Group("rooms.id, rooms.name").
		Order("member_count DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]roomActivity, len(rows))
	for i, r := range rows {
		result[i] = roomActivity{
			RoomID:      r.RoomID,
			RoomName:    r.RoomName,
			MemberCount: r.MemberCount,
		}
	}
	return result, nil
}

// getStorageUsage retrieves PostgreSQL database size using pg_database_size().
func (s *AnalyticsService) getStorageUsage(ctx context.Context) (storageMetrics, error) {
	var dbBytes int64
	err := s.db.WithContext(ctx).
		Raw("SELECT pg_database_size(current_database())").
		Scan(&dbBytes).Error
	if err != nil {
		// Non-fatal: return zeros if we can't query storage
		return storageMetrics{MaxBytes: 10 * 1024 * 1024 * 1024}, nil
	}

	return storageMetrics{
		DatabaseBytes: dbBytes,
		TotalBytes:    dbBytes,
		MaxBytes:      10 * 1024 * 1024 * 1024, // 10 GB default limit
	}, nil
}

// getHealthMetrics returns basic system health indicators.
// In production these would be pulled from Prometheus / internal metrics.
func (s *AnalyticsService) getHealthMetrics() healthMetrics {
	return healthMetrics{
		WSConnections:  0,
		ErrorRate:      0.0,
		APILatencyP50:  0.0,
		APILatencyP95:  0.0,
		APILatencyP99:  0.0,
		PluginsRunning: 0,
		PluginsTotal:   0,
	}
}

// parsePeriodDays converts a period string like "7d", "30d" to an integer number of days.
func parsePeriodDays(period string) int {
	period = strings.TrimSpace(strings.ToLower(period))
	if strings.HasSuffix(period, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(period, "d")); err == nil && n > 0 {
			return n
		}
	}
	return 7 // default to 7 days
}

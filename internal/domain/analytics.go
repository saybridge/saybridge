package domain

// DashboardMetrics aggregates all analytics data for the admin dashboard.
type DashboardMetrics struct {
	DAU             int64             `json:"dau"`
	MAU             int64             `json:"mau"`
	TotalUsers      int64             `json:"total_users"`
	TotalRooms      int64             `json:"total_rooms"`
	TotalMessages   int64             `json:"total_messages"`
	MessagesPerDay  []TimeseriesPoint `json:"messages_per_day"`
	TopRooms        []RoomActivity    `json:"top_rooms"`
	Storage         StorageMetrics    `json:"storage"`
	Health          HealthMetrics     `json:"health"`
}

// TimeseriesPoint represents a single date→value data point for charts.
type TimeseriesPoint struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
}

// RoomActivity summarises message activity and active user counts for a single room.
type RoomActivity struct {
	RoomID       string `json:"room_id"`
	RoomName     string `json:"room_name"`
	MessageCount int64  `json:"message_count"`
	ActiveUsers  int64  `json:"active_users"`
}

// StorageMetrics tracks disk-level storage consumption estimates.
type StorageMetrics struct {
	FilesBytes    int64 `json:"files_bytes"`
	DatabaseBytes int64 `json:"database_bytes"`
	LogsBytes     int64 `json:"logs_bytes"`
	TotalBytes    int64 `json:"total_bytes"`
	MaxBytes      int64 `json:"max_bytes"`
}

// HealthMetrics captures real-time system health indicators.
type HealthMetrics struct {
	WSConnections int64   `json:"ws_connections"`
	ErrorRate     float64 `json:"error_rate"`
	APILatencyP50 float64 `json:"api_latency_p50"`
	APILatencyP95 float64 `json:"api_latency_p95"`
	APILatencyP99 float64 `json:"api_latency_p99"`
	PluginsRunning int    `json:"plugins_running"`
	PluginsTotal   int    `json:"plugins_total"`
}

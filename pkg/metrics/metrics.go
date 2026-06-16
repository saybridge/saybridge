// Package metrics provides Prometheus instrumentation for the application.
// Exposes counters, histograms, and gauges for HTTP requests, WebSocket connections,
// message throughput, and plugin execution latency.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ── HTTP Metrics ────────────────────────────────────────────────────────────

var (
	// HTTPRequestsTotal counts total HTTP requests by method, path, and status.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "saybridge",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed.",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration measures HTTP request latency in seconds.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "saybridge",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution in seconds.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"method", "path"})
)

// ── WebSocket Metrics ───────────────────────────────────────────────────────

var (
	// WSConnectionsActive tracks current active WebSocket connections.
	WSConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "saybridge",
		Name:      "ws_connections_active",
		Help:      "Current number of active WebSocket connections.",
	})

	// WSMessagesTotal counts WebSocket messages sent and received.
	WSMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "saybridge",
		Name:      "ws_messages_total",
		Help:      "Total WebSocket messages processed.",
	}, []string{"direction"}) // "inbound" or "outbound"
)

// ── Message Metrics ─────────────────────────────────────────────────────────

var (
	// MessagesTotal counts total messages by type (text, file, thread_reply, etc.)
	MessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "saybridge",
		Name:      "messages_total",
		Help:      "Total messages processed.",
	}, []string{"msg_type"})
)

// ── Plugin Metrics ──────────────────────────────────────────────────────────

var (
	// PluginHookDuration measures hook execution latency by event and plugin name.
	PluginHookDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "saybridge",
		Name:      "plugin_hook_duration_seconds",
		Help:      "Plugin hook execution latency in seconds.",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	}, []string{"event", "plugin"})

	// PluginHookErrors counts plugin hook execution errors.
	PluginHookErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "saybridge",
		Name:      "plugin_hook_errors_total",
		Help:      "Total plugin hook execution errors.",
	}, []string{"event", "plugin"})
)

// ── Auth Metrics ────────────────────────────────────────────────────────────

var (
	// AuthAttemptsTotal counts login attempts by result.
	AuthAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "saybridge",
		Name:      "auth_attempts_total",
		Help:      "Total authentication attempts.",
	}, []string{"result"}) // "success", "failure", "external"
)

// ── Gin Middleware ──────────────────────────────────────────────────────────

// PrometheusMiddleware returns a Gin middleware that records HTTP metrics.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

// MetricsHandler returns a Gin handler that serves Prometheus metrics.
// Register as: r.GET("/metrics", metrics.MetricsHandler())
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// Package metrics provides Prometheus instrumentation for the application.
// It exposes counters, histograms, and gauges for HTTP requests, WebSocket
// connections, message throughput, plugin execution, authentication, AI
// usage, and notifications, plus Go runtime / process / DB-pool collectors.
//
// All metrics use the "saybridge" namespace and are registered on the default
// Prometheus registry, which also carries the standard go_* and process_*
// collectors. Scrape them at GET /metrics.
package metrics

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "saybridge"

// ── HTTP Metrics ────────────────────────────────────────────────────────────

var (
	// HTTPRequestsTotal counts total HTTP requests by method, path, and status.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed.",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration measures HTTP request latency in seconds.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution in seconds.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path"})

	// HTTPRequestsInFlight tracks the number of HTTP requests currently being served.
	HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "http_requests_in_flight",
		Help:      "Number of HTTP requests currently being served.",
	})
)

// ── WebSocket Metrics ───────────────────────────────────────────────────────

var (
	// WSConnectionsActive tracks current active WebSocket connections.
	WSConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ws_connections_active",
		Help:      "Current number of active WebSocket connections.",
	})

	// WSMessagesTotal counts WebSocket messages sent and received.
	WSMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ws_messages_total",
		Help:      "Total WebSocket messages processed.",
	}, []string{"direction"}) // "inbound" or "outbound"
)

// ── Message Metrics ─────────────────────────────────────────────────────────

var (
	// MessagesTotal counts total messages by type (text, file, thread_reply, etc.)
	MessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "messages_total",
		Help:      "Total chat messages processed.",
	}, []string{"msg_type"})
)

// ── Plugin Metrics ──────────────────────────────────────────────────────────

var (
	// PluginHookDuration measures hook execution latency by event and plugin name.
	PluginHookDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "plugin_hook_duration_seconds",
		Help:      "Plugin hook execution latency in seconds.",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"event", "plugin"})

	// PluginHookErrors counts plugin hook failures by kind ("error" or "panic").
	PluginHookErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "plugin_hook_errors_total",
		Help:      "Total plugin hook failures.",
	}, []string{"event", "plugin", "kind"})

	// PluginsLoaded reports the number of WASM plugins currently loaded.
	PluginsLoaded = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "plugins_loaded",
		Help:      "Number of WASM plugins currently loaded.",
	})
)

// ── Auth Metrics ────────────────────────────────────────────────────────────

var (
	// AuthAttemptsTotal counts authentication attempts by result.
	AuthAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "auth_attempts_total",
		Help:      "Total authentication attempts.",
	}, []string{"result"}) // "success", "failure", "external", "register", "refresh"
)

// ── Notification Metrics ────────────────────────────────────────────────────

var (
	// NotificationsTotal counts notification dispatch outcomes.
	NotificationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "notifications_total",
		Help:      "Total notifications by type, transport, and outcome.",
	}, []string{"type", "transport", "outcome"}) // outcome: "sent", "suppressed", "failed"
)

// ── AI / Copilot Metrics ────────────────────────────────────────────────────

var (
	// AIRequestsTotal counts AI provider calls by provider, model, and outcome.
	AIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ai_requests_total",
		Help:      "Total AI provider requests.",
	}, []string{"provider", "model", "outcome"}) // outcome: "success", "error"

	// AIRequestDuration measures AI provider call latency in seconds.
	AIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "ai_request_duration_seconds",
		Help:      "AI provider request latency in seconds.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60},
	}, []string{"provider"})

	// AITokensTotal counts AI tokens consumed by provider and kind.
	AITokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ai_tokens_total",
		Help:      "Total AI tokens consumed.",
	}, []string{"provider", "kind"}) // kind: "input", "output"
)

// ── Ergonomic helpers ───────────────────────────────────────────────────────

// IncWSConnection / DecWSConnection track the active connection gauge.
func IncWSConnection() { WSConnectionsActive.Inc() }
func DecWSConnection() { WSConnectionsActive.Dec() }

// IncWSMessage records a WebSocket message in the given direction ("inbound"/"outbound").
func IncWSMessage(direction string) { WSMessagesTotal.WithLabelValues(direction).Inc() }

// IncMessage records a processed chat message of the given type.
func IncMessage(msgType string) {
	if msgType == "" {
		msgType = "text"
	}
	MessagesTotal.WithLabelValues(msgType).Inc()
}

// ObservePluginHook records the latency and outcome of a single plugin hook call.
func ObservePluginHook(event, plugin string, d time.Duration, err error, panicked bool) {
	PluginHookDuration.WithLabelValues(event, plugin).Observe(d.Seconds())
	if panicked {
		PluginHookErrors.WithLabelValues(event, plugin, "panic").Inc()
	} else if err != nil {
		PluginHookErrors.WithLabelValues(event, plugin, "error").Inc()
	}
}

// IncAuth records an authentication attempt result.
func IncAuth(result string) { AuthAttemptsTotal.WithLabelValues(result).Inc() }

// IncNotification records a notification dispatch outcome.
func IncNotification(typ, transport, outcome string) {
	if typ == "" {
		typ = "unknown"
	}
	NotificationsTotal.WithLabelValues(typ, transport, outcome).Inc()
}

// ObserveAI records an AI provider request: latency, outcome, and token usage.
func ObserveAI(provider, model, outcome string, d time.Duration, inputTokens, outputTokens int) {
	AIRequestsTotal.WithLabelValues(provider, model, outcome).Inc()
	AIRequestDuration.WithLabelValues(provider).Observe(d.Seconds())
	if inputTokens > 0 {
		AITokensTotal.WithLabelValues(provider, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		AITokensTotal.WithLabelValues(provider, "output").Add(float64(outputTokens))
	}
}

// SetPluginsLoaded updates the loaded-plugins gauge.
func SetPluginsLoaded(n int) { PluginsLoaded.Set(float64(n)) }

var (
	dbCollectorOnce sync.Once
	registeredDB    *sql.DB // retained so Snapshot can read live pool stats
)

// RegisterDBCollector registers a Prometheus collector exposing the database
// connection-pool stats (open/idle/in-use connections, wait counts). Safe to
// call once; subsequent calls are no-ops.
func RegisterDBCollector(db *sql.DB, name string) {
	if db == nil {
		return
	}
	dbCollectorOnce.Do(func() {
		registeredDB = db
		prometheus.MustRegister(collectors.NewDBStatsCollector(db, name))
	})
}

// ── Gin Middleware & Handler ────────────────────────────────────────────────

// PrometheusMiddleware returns a Gin middleware that records HTTP request
// counts, latency, and in-flight gauge. It skips the /metrics endpoint itself.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}
		if path == "/metrics" {
			c.Next()
			return
		}

		HTTPRequestsInFlight.Inc()
		start := time.Now()

		c.Next()

		HTTPRequestsInFlight.Dec()
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

// MetricsHandler returns a Gin handler that serves Prometheus metrics.
// If the METRICS_TOKEN env var is set, requests must present it via an
// "Authorization: Bearer <token>" header or "?token=<token>" query param;
// otherwise the endpoint is open (intended to be firewalled to the scraper).
func MetricsHandler() gin.HandlerFunc {
	token := os.Getenv("METRICS_TOKEN")
	h := promhttp.Handler()
	return func(c *gin.Context) {
		if token != "" && !metricsTokenOK(c, token) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func metricsTokenOK(c *gin.Context, token string) bool {
	if c.Query("token") == token {
		return true
	}
	return c.GetHeader("Authorization") == "Bearer "+token
}

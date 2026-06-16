package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	natspkg "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// ManifestHandler serves plugin UI manifests to clients.
// Supports enable/disable via Redis keys: plugin:disabled:{slug}
type ManifestHandler struct {
	mu        sync.RWMutex
	manifests []*UIManifest
	rdb       *redis.Client
	nc        *natspkg.Conn // Use core NATS (not JetStream) for system broadcasts
}

// NewManifestHandler creates a new ManifestHandler.
func NewManifestHandler(rdb *redis.Client, nc *natspkg.Conn) *ManifestHandler {
	return &ManifestHandler{rdb: rdb, nc: nc}
}

// RegisterManifest adds a UI manifest to the handler's registry.
// Respects persisted toggle state in Redis — if a plugin was disabled,
// it stays disabled after server restart.
func (h *ManifestHandler) RegisterManifest(m *UIManifest) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check Redis for persisted disabled state
	m.Enabled = true
	if h.rdb != nil {
		disabled, err := h.rdb.Get(context.Background(), "plugin:disabled:"+m.ID).Result()
		if err == nil && disabled == "1" {
			m.Enabled = false
		}
	}

	// Update in-place if manifest with same ID already exists to prevent duplicates on hot-reload
	for i, existing := range h.manifests {
		if existing.ID == m.ID {
			h.manifests[i] = m
			return
		}
	}

	h.manifests = append(h.manifests, m)
}

// GetManifests returns all UI manifests from registered plugins.
// Checks Redis for disabled status per plugin.
//
//	GET /api/v1/plugins/manifest
//	Response: { "success": true, "data": [ ...UIManifest[] ] }
func (h *ManifestHandler) GetManifests(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*UIManifest, len(h.manifests))
	for i, m := range h.manifests {
		copied := *m
		copied.Enabled = true // Default to enabled
		if h.rdb != nil {
			key := "plugin:disabled:" + m.ID
			disabled, err := h.rdb.Get(context.Background(), key).Result()
			if err == nil && disabled == "1" {
				copied.Enabled = false
				log.Debug().Msgf("[ManifestHandler] Plugin %s is DISABLED (key=%s)", m.ID, key)
			}
		} else {
			log.Warn().Msg("[ManifestHandler] Redis client is nil — cannot check plugin state")
		}
		result[i] = &copied
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// TogglePlugin enables or disables a plugin by its slug.
// Broadcasts a system event via core NATS so all connected WS clients update in real-time.
//
//	POST /api/v1/plugins/:slug/toggle
//	Body: { "enabled": true/false }
func (h *ManifestHandler) TogglePlugin(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "slug is required"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if h.rdb != nil {
		key := "plugin:disabled:" + slug
		if req.Enabled {
			err := h.rdb.Del(context.Background(), key).Err()
			log.Info().Msgf("[ManifestHandler] TogglePlugin: DEL %s (err=%v)", key, err)
		} else {
			err := h.rdb.Set(context.Background(), key, "1", 0).Err()
			log.Info().Msgf("[ManifestHandler] TogglePlugin: SET %s=1 (err=%v)", key, err)
		}
	} else {
		log.Error().Msg("[ManifestHandler] TogglePlugin: Redis client is nil — toggle NOT persisted!")
	}

	// Broadcast system event via core NATS (not JetStream) to match hub subscriptions
	if h.nc != nil {
		payload := map[string]interface{}{
			"event": "system:plugin_toggled",
			"data": map[string]interface{}{
				"slug":    slug,
				"enabled": req.Enabled,
			},
		}
		payloadBytes, err := json.Marshal(payload)
		if err == nil {
			subject := "tenant.default.system"
			if err := h.nc.Publish(subject, payloadBytes); err != nil {
				log.Error().Err(err).Msg("[ManifestHandler] Failed to broadcast plugin toggle event")
			} else {
				log.Info().Msgf("[ManifestHandler] Broadcast plugin toggle: %s enabled=%v", slug, req.Enabled)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"slug":    slug,
			"enabled": req.Enabled,
		},
	})
}

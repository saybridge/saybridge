package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/redis/go-redis/v9"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 4096
)

// RateLimiter implements a simple thread-safe token bucket algorithm for WebSocket connections.
type RateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter instantiates a new Client-level Rate Limiter.
func NewRateLimiter(refillRate float64, maxTokens float64) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow evaluates if a request falls within permitted rate constraints.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now

	rl.tokens += elapsed * rl.refillRate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}

	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		return true
	}
	return false
}

// Client represents an active single client session connected over WebSockets.
type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	userID     string
	username   string
	tenantID   string
	deviceID   string
	role       string
	limiter    *RateLimiter
	msgUseCase domain.MessageUseCase
	rdb        *redis.Client
	userRepo   domain.UserRepository
	js         nats.JetStreamContext
	hooks      *plugin.HookRegistry
}

// WSFrame defines the generic envelope structure for client-server frame contracts.
type WSFrame struct {
	Event  string          `json:"event"`
	RoomID string          `json:"room_id,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// ReadPump handles inbound frames from connection, applies rate limits, and routes frames to Hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		c.setUserPresence(context.Background(), "offline")
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[Client] Unexpected connection close: %v", err)
			}
			break
		}

		// Enforce Client-level Rate Limiting: 30 messages per minute (0.5 tokens/sec)
		if !c.limiter.Allow() {
			log.Printf("[Client] Rate limit exceeded for user [%s]", c.userID)
			errFrame := map[string]interface{}{
				"event": "error",
				"data":  map[string]string{"message": "rate limit exceeded: max 30 messages per minute"},
			}
			errBytes, _ := json.Marshal(errFrame)
			c.send <- errBytes
			continue
		}

		// Decode frame payload
		var frame WSFrame
		if err := json.Unmarshal(message, &frame); err != nil {
			log.Printf("[Client] Failed to unmarshal message frame: %v", err)
			continue
		}

		// Dispatch core router events
		switch frame.Event {
		case "room:join":
			if frame.RoomID != "" {
				c.hub.JoinRoom(frame.RoomID, c)
				ack := map[string]string{"event": "room:joined", "room_id": frame.RoomID}
				ackBytes, _ := json.Marshal(ack)
				c.send <- ackBytes
			}
		case "room:leave":
			if frame.RoomID != "" {
				c.hub.LeaveRoom(frame.RoomID, c)
				ack := map[string]string{"event": "room:left", "room_id": frame.RoomID}
				ackBytes, _ := json.Marshal(ack)
				c.send <- ackBytes
			}
		case "msg:send":
			var msgData struct {
				LocalID   string `json:"local_id"`
				Content   string `json:"content"`
				MsgType   string `json:"msg_type"`
				ParentID  string `json:"parent_id"`
				ReplyToID string `json:"reply_to_id"`
			}
			if err := json.Unmarshal(frame.Data, &msgData); err != nil {
				log.Printf("[Client] Failed to unmarshal msg:send payload: %v", err)
				continue
			}

			msgType := "text"
			if msgData.MsgType != "" {
				msgType = msgData.MsgType
			}

			msg, err := c.msgUseCase.SendMessage(context.Background(), c.tenantID, c.userID, frame.RoomID, msgData.Content, msgType, msgData.ParentID, msgData.ReplyToID)
			if err != nil {
				log.Printf("[Client] Failed to process SendMessage: %v", err)
				errFrame := map[string]interface{}{
					"event":   "msg:error",
					"room_id": frame.RoomID,
					"data":    map[string]string{"local_id": msgData.LocalID, "message": err.Error()},
				}
				errBytes, _ := json.Marshal(errFrame)
				c.send <- errBytes
				continue
			}

			ackFrame := map[string]interface{}{
				"event":   "msg:ack",
				"room_id": frame.RoomID,
				"data":    map[string]string{"local_id": msgData.LocalID, "message_id": msg.MessageID},
			}
			ackBytes, _ := json.Marshal(ackFrame)
			c.send <- ackBytes

		case "msg:edit":
			var editData struct {
				MessageID  string `json:"message_id"`
				TimeBucket int    `json:"time_bucket"`
				Content    string `json:"content"`
			}
			if err := json.Unmarshal(frame.Data, &editData); err != nil {
				log.Printf("[Client] Failed to unmarshal msg:edit payload: %v", err)
				continue
			}

			_, err := c.msgUseCase.EditMessage(context.Background(), c.tenantID, c.userID, frame.RoomID, editData.MessageID, editData.TimeBucket, editData.Content)
			if err != nil {
				log.Printf("[Client] Failed to edit message: %v", err)
				continue
			}

		case "msg:delete":
			var deleteData struct {
				MessageID  string `json:"message_id"`
				TimeBucket int    `json:"time_bucket"`
			}
			if err := json.Unmarshal(frame.Data, &deleteData); err != nil {
				log.Printf("[Client] Failed to unmarshal msg:delete payload: %v", err)
				continue
			}

			_, err := c.msgUseCase.DeleteMessage(context.Background(), c.tenantID, c.userID, frame.RoomID, deleteData.MessageID, deleteData.TimeBucket)
			if err != nil {
				log.Printf("[Client] Failed to delete message: %v", err)
				continue
			}

		case "msg:reaction":
			var reactionData struct {
				MessageID  string `json:"message_id"`
				TimeBucket int    `json:"time_bucket"`
				Emoji      string `json:"emoji"`
			}
			if err := json.Unmarshal(frame.Data, &reactionData); err != nil {
				log.Printf("[Client] Failed to unmarshal msg:reaction payload: %v", err)
				continue
			}

			_, err := c.msgUseCase.ToggleReaction(context.Background(), c.tenantID, c.userID, frame.RoomID, reactionData.MessageID, reactionData.TimeBucket, reactionData.Emoji)
			if err != nil {
				log.Printf("[Client] Failed to toggle reaction: %v", err)
				continue
			}

			// Emit OnReactionToggled hook
			c.hooks.EmitAsync(context.Background(), plugin.OnReactionToggled, map[string]interface{}{
				"user_id":     c.userID,
				"room_id":     frame.RoomID,
				"message_id":  reactionData.MessageID,
				"time_bucket": reactionData.TimeBucket,
				"emoji":       reactionData.Emoji,
			})

		case "user:typing":
			var typingData struct {
				Typing bool `json:"typing"`
			}
			if err := json.Unmarshal(frame.Data, &typingData); err != nil {
				continue
			}

			typingFrame := map[string]interface{}{
				"event":   "user:typing",
				"room_id": frame.RoomID,
				"data": map[string]interface{}{
					"user_id":  c.userID,
					"username": c.username,
					"typing":   typingData.Typing,
				},
			}
			tfBytes, err := json.Marshal(typingFrame)
			if err == nil {
				subject := fmt.Sprintf("tenant.%s.room.%s", c.tenantID, frame.RoomID)
				_ = c.hub.nc.Publish(subject, tfBytes)
			}

		case "plugin:action":
			var actionData map[string]interface{}
			if err := json.Unmarshal(frame.Data, &actionData); err != nil {
				log.Printf("[Client] Failed to unmarshal plugin:action payload: %v", err)
				continue
			}
			actionData["user_id"] = c.userID
			actionData["tenant_id"] = c.tenantID

			c.hooks.EmitAsync(context.Background(), plugin.OnPluginAction, actionData)
		}
	}
}

// WritePump pushes buffered frames out to active peer, managing ping/pong intervals.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Drain buffered messages to optimize network writing
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) setUserPresence(ctx context.Context, status string) {
	if c.rdb == nil || c.userRepo == nil {
		return
	}

	// 1. Cache presence status in Redis
	key := fmt.Sprintf("user:presence:%s", c.userID)
	_ = c.rdb.Set(ctx, key, status, 15*time.Minute).Err()

	// 2. Persist status in DB
	user, err := c.userRepo.GetUserByID(ctx, c.userID)
	if err == nil {
		user.Presence = status
		_ = c.userRepo.UpdateUser(ctx, user)
	}

	// 3. Broadcast status change across cluster
	subject := fmt.Sprintf("tenant.%s.presence", c.tenantID)
	payload := map[string]interface{}{
		"event":   "user:presence:changed",
		"user_id": c.userID,
		"status":  status,
	}
	payloadBytes, err := json.Marshal(payload)
	if err == nil {
		_, _ = c.js.Publish(subject, payloadBytes)
	}
}

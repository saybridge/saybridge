package copilot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/redis/go-redis/v9"
)

// ── Catch-up Digest Service ───────────────────────────────────────────────────

// DigestService generates daily catch-up digests for users with unread messages.
// It runs as a background goroutine registered via OnServerStart hook.
type DigestService struct {
	gateway     *Gateway
	messageRepo domain.MessageRepository
	rdb         *redis.Client
	// deliverFn delivers the digest text to a user (as a DM from the Sai bot).
	deliverFn  func(ctx context.Context, userID, content string) error
	digestHour int // hour of day to send digest (default: 8 = 08:00)
}

// DigestConfig holds configuration for the digest service.
type DigestConfig struct {
	Enabled    bool
	DigestHour int // 0-23
}

func init() {
	// Register catch-up digest on server start
	plugin.Registry.On(plugin.OnServerStart, plugin.HookHandler{
		Name:     "ai-agent:digest",
		Priority: 20,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			if DefaultAgentConfig == nil || !DefaultAgentConfig.Enabled {
				log.Printf("[Digest] AI not enabled, skipping digest service")
				return nil, nil
			}

			rdb, _ := payload["rdb"].(*redis.Client)
			messageRepo, _ := payload["message_repo"].(domain.MessageRepository)
			dmFn, _ := payload["dm_message_fn"].(func(ctx context.Context, fromUserID, toUserID, content string) (string, error))

			if rdb == nil || messageRepo == nil || DefaultGateway == nil {
				log.Printf("[Digest] Missing dependencies, skipping digest service")
				return nil, nil
			}
			if dmFn == nil {
				log.Printf("[Digest] No DM delivery function available, skipping digest service")
				return nil, nil
			}

			ds := &DigestService{
				gateway:     DefaultGateway,
				messageRepo: messageRepo,
				rdb:         rdb,
				deliverFn: func(ctx context.Context, userID, content string) error {
					// Digests are delivered as a DM from the Sai assistant bot.
					_, err := dmFn(ctx, saiBotID, userID, content)
					return err
				},
				digestHour: 8, // 08:00 default
			}

			go ds.Start(context.Background())
			log.Printf("[Digest] ✓ Catch-up digest service started (runs daily at %02d:00)", ds.digestHour)
			return nil, nil
		},
	})
}

// Start begins the digest scheduler. It checks every minute if it's time to run.
func (ds *DigestService) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if now.Hour() == ds.digestHour && now.Minute() == 0 {
				ds.runDigestCycle(ctx)
			}
		}
	}
}

// runDigestCycle processes digests for all users with pending unread messages.
func (ds *DigestService) runDigestCycle(ctx context.Context) {
	log.Printf("[Digest] Starting daily digest cycle")

	// Get all users with unread notifications from Redis
	// Pattern: digest:pending:{user_id} = comma-separated room IDs
	keys, err := ds.rdb.Keys(ctx, "digest:pending:*").Result()
	if err != nil {
		log.Printf("[Digest] Failed to scan pending digests: %v", err)
		return
	}

	if len(keys) == 0 {
		log.Printf("[Digest] No pending digests to process")
		return
	}

	processed := 0
	for _, key := range keys {
		userID := strings.TrimPrefix(key, "digest:pending:")

		// Check if we already sent a digest today
		lastRunKey := fmt.Sprintf("digest:last_run:%s", userID)
		lastRun, _ := ds.rdb.Get(ctx, lastRunKey).Result()
		today := time.Now().Format("2006-01-02")
		if lastRun == today {
			continue
		}

		// Get pending room IDs
		roomIDsStr, err := ds.rdb.Get(ctx, key).Result()
		if err != nil || roomIDsStr == "" {
			continue
		}
		roomIDs := strings.Split(roomIDsStr, ",")

		// Generate digest for this user
		digest, err := ds.generateUserDigest(ctx, userID, roomIDs)
		if err != nil {
			log.Printf("[Digest] Failed to generate digest for user %s: %v", userID, err)
			continue
		}

		if digest == "" {
			continue
		}

		// Deliver the digest as a DM from the Sai assistant bot to the user.
		if err := ds.deliverFn(ctx, userID, digest); err != nil {
			log.Printf("[Digest] Failed to send digest to user %s: %v", userID, err)
			continue
		}

		// Mark as sent today
		ds.rdb.Set(ctx, lastRunKey, today, 24*time.Hour)
		// Clear pending
		ds.rdb.Del(ctx, key)
		processed++
	}

	log.Printf("[Digest] Processed %d digests", processed)
}

// generateUserDigest creates a summary of unread activity for a user.
func (ds *DigestService) generateUserDigest(ctx context.Context, userID string, roomIDs []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("## 📬 Your Daily Catch-up Digest\n\n")

	hasContent := false

	for _, roomID := range roomIDs {
		if roomID == "" {
			continue
		}

		// Get recent messages from this room
		messages, err := ds.messageRepo.GetMessageHistory(ctx, roomID, 30, "")
		if err != nil {
			log.Printf("[Digest] Failed to get history for room %s: %v", roomID, err)
			continue
		}

		if len(messages) == 0 {
			continue
		}

		// Collect mentions and important messages
		var mentions []string
		var recentMsgs []string

		for _, msg := range messages {
			if msg.IsDeleted {
				continue
			}
			line := fmt.Sprintf("%s: %s", msg.SenderName, msg.Content)
			recentMsgs = append(recentMsgs, line)

			// Check if this message mentions the user
			if strings.Contains(msg.Content, userID) || strings.Contains(msg.Content, "@"+userID) {
				mentions = append(mentions, line)
			}
		}

		if len(recentMsgs) == 0 {
			continue
		}

		hasContent = true

		// If AI is available, generate a summary
		if DefaultAgentConfig.Enabled && ds.gateway != nil {
			conversationText := strings.Join(recentMsgs, "\n")
			chatReq := &ChatRequest{
				SystemPrompt: "You are an assistant that creates brief catch-up summaries. Summarize the key points from this chat room conversation in 2-3 bullet points. Focus on decisions, action items, and mentions. Keep it very concise.",
				Messages: []ChatMessage{
					{Role: "user", Content: conversationText},
				},
			}

			resp, err := ds.gateway.Query(ctx, chatReq)
			if err == nil && resp.Content != "" {
				sb.WriteString(fmt.Sprintf("### Room: %s\n", roomID))
				sb.WriteString(resp.Content)
				sb.WriteString("\n\n")
			}
		} else {
			// Fallback: just list message count and mentions
			sb.WriteString(fmt.Sprintf("### Room: %s\n", roomID))
			sb.WriteString(fmt.Sprintf("- %d new messages\n", len(recentMsgs)))
			if len(mentions) > 0 {
				sb.WriteString(fmt.Sprintf("- **%d mentions of you**\n", len(mentions)))
				for _, m := range mentions {
					if len(m) > 100 {
						m = m[:100] + "..."
					}
					sb.WriteString(fmt.Sprintf("  - %s\n", m))
				}
			}
			sb.WriteString("\n")
		}
	}

	if !hasContent {
		return "", nil
	}

	sb.WriteString("---\n*Generated at " + time.Now().Format("2006-01-02 15:04 MST") + "*")
	return sb.String(), nil
}

// TrackUnread records a room with unread messages for a user, to be included in next digest.
// Called from AfterSendMessage hook to track unread rooms for offline users.
func TrackUnreadForDigest(ctx context.Context, rdb *redis.Client, userID, roomID string) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("digest:pending:%s", userID)

	// Append room ID if not already present
	existing, _ := rdb.Get(ctx, key).Result()
	if existing == "" {
		rdb.Set(ctx, key, roomID, 48*time.Hour)
	} else if !strings.Contains(existing, roomID) {
		rdb.Set(ctx, key, existing+","+roomID, 48*time.Hour)
	}
}

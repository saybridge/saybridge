package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/response"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ── AI Agent Config (self-managed) ────────────────────────────────────────────

type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
}

type AgentConfig struct {
	Provider          string
	APIKey            string
	BaseURL           string
	Model             string
	MaxTokens         int
	Enabled           bool
	SystemPrompt      string
	Temperature       float64
	AutoReply         bool
	ModerationEnabled bool
	OrchestratorRules string                    `json:"orchestratorRules"`
	ModerationRules   string                    `json:"moderationRules"`
	Providers         map[string]ProviderConfig `json:"providers"`
}

var DefaultAgentConfig *AgentConfig

const saiBotID = "00000000-0000-0000-0000-000000000001"

func loadAgentConfig() *AgentConfig {
	maxTokens, _ := strconv.Atoi(os.Getenv("AI_MAX_TOKENS"))
	if maxTokens == 0 {
		maxTokens = 8192
	}
	enabledVal := os.Getenv("AI_ENABLED")
	enabled := true
	if enabledVal != "" {
		enabled, _ = strconv.ParseBool(enabledVal)
	}
	temp, _ := strconv.ParseFloat(os.Getenv("AI_TEMPERATURE"), 64)
	if temp == 0 {
		temp = 0.7
	}
	autoReply, _ := strconv.ParseBool(os.Getenv("AI_AUTO_REPLY"))
	modEnabled, _ := strconv.ParseBool(os.Getenv("AI_MODERATION_ENABLED"))
	
	provider := os.Getenv("AI_PROVIDER")
	if provider == "" {
		provider = "gemini"
	}
	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	return &AgentConfig{
		Provider:          provider,
		APIKey:            os.Getenv("AI_API_KEY"),
		BaseURL:           os.Getenv("AI_BASE_URL"),
		Model:             model,
		MaxTokens:         maxTokens,
		Enabled:           enabled,
		SystemPrompt:      os.Getenv("AI_SYSTEM_PROMPT"),
		Temperature:       temp,
		AutoReply:         autoReply,
		ModerationEnabled: modEnabled,
		OrchestratorRules: os.Getenv("AI_ORCHESTRATOR_RULES"),
		ModerationRules:   os.Getenv("AI_MODERATION_RULES"),
		Providers:         make(map[string]ProviderConfig),
	}
}

// ── Plugin Implementation ─────────────────────────────────────────────────────

// AIAgentPlugin provides AI-powered chat assistant capabilities via hook registration.
type AIAgentPlugin struct {
	gateway       *Gateway
	messageRepo   domain.MessageRepository
	rdb           *redis.Client
	semanticIndex *SemanticIndex
}

func init() {
	// Auto-register with core's OnServerStart hook.
	// When server finishes bootstrapping, AI Agent:
	// 1. Loads config from env
	// 2. Inits provider gateway
	// 3. Registers HTTP routes via the gin.RouterGroup in payload
	plugin.Registry.On(plugin.OnServerStart, plugin.HookHandler{
		Name:     "copilot:start",
		Priority: 10,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			// Load config from env as defaults
			cfg := loadAgentConfig()

			// Try loading persisted config from Redis (set via Admin UI)
			rdb, _ := payload["rdb"].(*redis.Client)
			if rdb != nil {
				if v, err := rdb.Get(ctx, "ai:config:provider").Result(); err == nil && v != "" {
					cfg.Provider = v
				}
				if v, err := rdb.Get(ctx, "ai:config:api_key").Result(); err == nil && v != "" {
					cfg.APIKey = v
				}
				if v, err := rdb.Get(ctx, "ai:config:model").Result(); err == nil && v != "" {
					cfg.Model = v
				}
				if v, err := rdb.Get(ctx, "ai:config:system_prompt").Result(); err == nil && v != "" {
					cfg.SystemPrompt = v
				}
				if v, err := rdb.Get(ctx, "ai:config:max_tokens").Result(); err == nil && v != "" {
					if mt, e := strconv.Atoi(v); e == nil && mt > 0 {
						cfg.MaxTokens = mt
					}
				}
				if v, err := rdb.Get(ctx, "ai:config:temperature").Result(); err == nil && v != "" {
					if t, e := strconv.ParseFloat(v, 64); e == nil {
						cfg.Temperature = t
					}
				}
				if v, err := rdb.Get(ctx, "ai:config:auto_reply").Result(); err == nil && v != "" {
					cfg.AutoReply = (v == "true" || v == "1")
				}
				if v, err := rdb.Get(ctx, "ai:config:moderation_enabled").Result(); err == nil && v != "" {
					cfg.ModerationEnabled = (v == "true" || v == "1")
				}
				if v, err := rdb.Get(ctx, "ai:config:orchestrator_rules").Result(); err == nil && v != "" {
					cfg.OrchestratorRules = v
				}
				if v, err := rdb.Get(ctx, "ai:config:moderation_rules").Result(); err == nil && v != "" {
					cfg.ModerationRules = v
				}
			}

			// Populate provider specific config
			providersList := []string{"openai", "claude", "gemini", "ollama"}
			cfg.Providers = make(map[string]ProviderConfig)
			for _, pid := range providersList {
				pcfg := ProviderConfig{}
				
				switch pid {
				case "openai":
					pcfg.BaseURL = "https://api.openai.com/v1"
					pcfg.Model = "gpt-4o"
				case "claude":
					pcfg.BaseURL = "https://api.anthropic.com/v1"
					pcfg.Model = "claude-3-5-sonnet-20241022"
				case "gemini":
					pcfg.BaseURL = ""
					pcfg.Model = "gemini-2.5-flash"
				case "ollama":
					pcfg.BaseURL = "http://localhost:11434"
					pcfg.Model = "llama3.1"
				}

				if cfg.Provider == pid {
					if cfg.APIKey != "" {
						pcfg.APIKey = cfg.APIKey
					}
					if cfg.BaseURL != "" {
						pcfg.BaseURL = cfg.BaseURL
					}
					if cfg.Model != "" {
						pcfg.Model = cfg.Model
					}
				}

				if rdb != nil {
					if v, err := rdb.Get(ctx, "ai:provider:"+pid+":api_key").Result(); err == nil {
						pcfg.APIKey = v
					}
					if v, err := rdb.Get(ctx, "ai:provider:"+pid+":base_url").Result(); err == nil {
						pcfg.BaseURL = v
					}
					if v, err := rdb.Get(ctx, "ai:provider:"+pid+":model").Result(); err == nil {
						pcfg.Model = v
					}
				}
				
				cfg.Providers[pid] = pcfg
			}

			DefaultAgentConfig = cfg

			// Init gateway + providers
			gw := NewGateway(cfg.Provider)
			
			pOpenAI := cfg.Providers["openai"]
			gw.RegisterProvider(NewOpenAIProvider(pOpenAI.APIKey, pOpenAI.BaseURL, pOpenAI.Model))
			
			pClaude := cfg.Providers["claude"]
			gw.RegisterProvider(NewClaudeProvider(pClaude.APIKey, pClaude.BaseURL, pClaude.Model))
			
			pGemini := cfg.Providers["gemini"]
			gw.RegisterProvider(NewGeminiProvider(pGemini.APIKey, pGemini.BaseURL, pGemini.Model))
			
			pOllama := cfg.Providers["ollama"]
			gw.RegisterProvider(NewOllamaProvider(pOllama.BaseURL, pOllama.Model))
			
			DefaultGateway = gw

			// Extract deps from payload (generic types, no AI-specific contract)
			api, _ := payload["api"].(*gin.RouterGroup)
			messageRepo, _ := payload["message_repo"].(domain.MessageRepository)
			userRepo, _ := payload["user_repo"].(domain.UserRepository)
			db, _ := payload["db"].(*gorm.DB)
			_ = payload["proxy"]

			// Run GORM AutoMigrate and Seed default agents
			if db != nil {
				if err := db.AutoMigrate(&AIAgent{}); err != nil {
					log.Printf("[AIAgent] Schema migration failed: %v", err)
				} else {
					log.Printf("[AIAgent] Schema migration succeeded (ai_agents)")
					seedDefaultAgents(ctx, db, userRepo)
				}
			}

			// Initialize semantic index if DB is available
			var semIdx *SemanticIndex
			if db != nil {
				semIdx = NewSemanticIndex(db, gw)
				if err := semIdx.EnsureSchema(); err != nil {
					log.Printf("[AIAgent] Warning: semantic index schema failed: %v", err)
				} else {
					semIdx.StartBatchProcessor(context.Background())
				}
			}

			if api != nil {
				h := &aiHandler{
					gateway:       gw,
					messageRepo:   messageRepo,
					rdb:           rdb,
					semanticIndex: semIdx,
					db:            db,
				}
				copilotGroup := api.Group("/copilot")
				{
					copilotGroup.POST("/query", h.query)
					copilotGroup.POST("/query/stream", h.queryStream)
					copilotGroup.POST("/summary", h.summary)
					copilotGroup.POST("/summarize", h.summarize)
					copilotGroup.POST("/translate", h.translate)
					copilotGroup.POST("/search", h.semanticSearch)
					copilotGroup.GET("/providers", h.listProviders)
					copilotGroup.GET("/config", h.getConfig)
					copilotGroup.PUT("/config", h.updateConfig)
					copilotGroup.PATCH("/config", h.updateConfig)
					copilotGroup.POST("/chat", h.chat)
					copilotGroup.GET("/agents", h.listAgents)
					copilotGroup.POST("/agents", h.createAgent)
					copilotGroup.PUT("/agents/:id", h.updateAgent)
					copilotGroup.DELETE("/agents/:id", h.deleteAgent)
					copilotGroup.POST("/agents/:id/test", h.testAgent)
					copilotGroup.GET("/metrics", h.getMetrics)
				}
				log.Printf("[Copilot] ✓ Routes registered: /api/v1/copilot/*")
			}

			// Extract send_message_fn for slash command responses
			sendMsgFn, _ := payload["send_message_fn"].(func(ctx context.Context, senderID, senderName, roomID, content string) (string, error))
			publishWSFn, _ := payload["publish_ws_event_fn"].(func(ctx context.Context, roomID, eventName, data string) error)
			updateMsgContentFn, _ := payload["update_message_content_fn"].(func(ctx context.Context, messageID, content string) error)

			// Register slash command handler (Go native, no WASM needed)
			plugin.Registry.On(plugin.MessageSlashCommand, plugin.HookHandler{
				Name:     "copilot:slash-command",
				Priority: 10,
				Fn: func(ctx context.Context, p map[string]interface{}) (interface{}, error) {
					cmd, _ := p["command"].(string)
					args, _ := p["args"].(string)
					roomID, _ := p["room_id"].(string)

					if roomID == "" {
						return nil, nil
					}

					sendReply := func(msg string) {
						if sendMsgFn != nil {
							if _, err := sendMsgFn(ctx, saiBotID, "Sai", roomID, msg); err != nil {
								log.Printf("[AIAgent] Failed to send reply: %v", err)
							}
						}
					}

					setTyping := func(typing bool) {
						if publishWSFn != nil {
							data := fmt.Sprintf(`{"username":"AI Assistant","typing":%t}`, typing)
							_ = publishWSFn(ctx, roomID, "user:typing", data)
						}
					}

					switch cmd {
					case "translate":
						parts := strings.SplitN(args, " ", 2)
						if len(parts) == 0 || parts[0] == "" {
							sendReply("🤖 Syntax: `/translate <lang> [text]`")
							return nil, nil
						}
						lang := parts[0]
						textToTranslate := ""
						if len(parts) > 1 {
							textToTranslate = parts[1]
						}
						if textToTranslate == "" {
							sendReply("🤖 Please provide text to translate.")
							return nil, nil
						}
						sysPrompt := fmt.Sprintf("You are a professional translator. Translate the following text to '%s'. Only return the translation.", lang)
						chatReq := &ChatRequest{
							SystemPrompt: sysPrompt,
							Messages:     []ChatMessage{{Role: "user", Content: textToTranslate}},
						}
						setTyping(true)
						resp, err := gw.Query(ctx, chatReq)
						setTyping(false)
						if err != nil {
							sendReply("🤖 Sorry, cannot translate at this moment.")
							return nil, nil
						}
						sendReply("🌐 **Translation (" + lang + "):** " + resp.Content)
						return nil, nil

					case "summary":
						if messageRepo != nil {
							messages, err := messageRepo.GetMessageHistory(ctx, roomID, 50, "")
							if err != nil || len(messages) == 0 {
								sendReply("🤖 No messages to summarize.")
								return nil, nil
							}
							var sb strings.Builder
							for i := len(messages) - 1; i >= 0; i-- {
								m := messages[i]
								if m.IsDeleted {
									continue
								}
								sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
							}
							chatReq := &ChatRequest{
								SystemPrompt: "You are an AI assistant. Briefly summarize the conversation below, stating main points and decisions.",
								Messages:     []ChatMessage{{Role: "user", Content: sb.String()}},
							}
							setTyping(true)
							resp, err := gw.Query(ctx, chatReq)
							setTyping(false)
							if err != nil {
								sendReply("🤖 Sorry, cannot summarize at this moment.")
								return nil, nil
							}
							sendReply("📝 **Summary:**\n" + resp.Content)
						}
						return nil, nil
					}

					return nil, nil
				},
			})
			log.Printf("[AIAgent] ✓ Slash command handler registered (/summary, /translate)")

			// Register @sai / Multi-Agent trigger handler
			plugin.Registry.On(plugin.AfterSendMessage, plugin.HookHandler{
				Name:     "copilot:mention-handler",
				Priority: 5,
				Fn: func(ctx context.Context, p map[string]interface{}) (interface{}, error) {
					content, _ := p["content"].(string)
					roomID, _ := p["room_id"].(string)
					senderID, _ := p["sender_id"].(string)

					// Skip bot's own messages to prevent infinite loop (all bots use deterministic UUID prefix)
					if strings.HasPrefix(senderID, "00000000-0000-0000-0000-") || senderID == domain.SystemActorID || roomID == "" {
						return nil, nil
					}

					// Skip if AI is disabled globally
					if DefaultAgentConfig == nil || !DefaultAgentConfig.Enabled {
						return nil, nil
					}

					// Fetch all active agents from DB
					var agents []AIAgent
					if db != nil {
						if err := db.Where("enabled = ?", true).Find(&agents).Error; err != nil {
							log.Printf("[AIAgent] Failed to fetch active agents: %v", err)
							return nil, nil
						}
					}

					if len(agents) == 0 {
						return nil, nil
					}

					if strings.Contains(content, "@") {
						log.Printf("[AIAgent] Received potential mention: %q from %s. Active agents count: %d", content, senderID, len(agents))
					}

					for i := range agents {
						agent := agents[i]
						triggered := false
						query := content

						if agent.TriggerType == "mention" {
							// Match explicit mention e.g., @sai_coder, @sai:coder, or @sai
							mentionTag1 := "@" + agent.Username
							mentionTag2 := agent.TriggerKeyword

							if strings.Contains(content, mentionTag1) {
								triggered = true
								query = strings.TrimSpace(strings.ReplaceAll(content, mentionTag1, ""))
							} else if mentionTag2 != "" && strings.Contains(content, mentionTag2) {
								triggered = true
								query = strings.TrimSpace(strings.ReplaceAll(content, mentionTag2, ""))
							}
						} else if agent.TriggerType == "silent" {
							// Silent integration in configured rooms
							if agent.RoomIDs != "" {
								rooms := strings.Split(agent.RoomIDs, ",")
								for _, r := range rooms {
									if strings.TrimSpace(r) == roomID {
										triggered = true
										break
									}
								}
							}
						}

						if triggered {
							log.Printf("[AIAgent] Triggered agent %s (%s) in room %s", agent.Name, agent.ID, roomID)

							// Launch LLM call in a background goroutine so we don't block hook execution
							go func(agent AIAgent, q string) {
								bgCtx := context.Background()

								// Start typing indicator
								if publishWSFn != nil {
									data := fmt.Sprintf(`{"username":"%s","typing":true}`, agent.Name)
									_ = publishWSFn(bgCtx, roomID, "user:typing", data)
								}

								// Build conversation history context
								var chatMessages []ChatMessage
								if messageRepo != nil {
									history, err := messageRepo.GetMessageHistory(bgCtx, roomID, 20, "")
									if err == nil {
										for i := len(history) - 1; i >= 0; i-- {
											m := history[i]
											if m.IsDeleted || m.Content == "" || m.Content == "⏳" {
												continue
											}
											role := "user"
											// If sender is any system bot (starts with deterministic prefix)
											if strings.HasPrefix(m.SenderID, "00000000-0000-0000-0000-") || m.SenderID == domain.SystemActorID {
												role = "assistant"
											}
											chatMessages = append(chatMessages, ChatMessage{Role: role, Content: m.Content})
										}
									}
								}
								chatMessages = append(chatMessages, ChatMessage{Role: "user", Content: q})

								// Resolve active provider and default model
								activeProvider := DefaultAgentConfig.Provider
								model := resolveAgentModel(activeProvider, agent.Model)

								// Orchestrator Agent Routing (only if `@sai` is called and there are other agents)
								if agent.ID == "sai" && len(agents) > 1 {
									var agentDescriptions []string
									for _, a := range agents {
										if a.ID == "sai" {
											continue
										}
										agentDescriptions = append(agentDescriptions, fmt.Sprintf("- ID: %s, Name: %s, Purpose: %s", a.ID, a.Name, a.SystemPrompt))
									}

									routerRules := ""
									if DefaultAgentConfig != nil && DefaultAgentConfig.OrchestratorRules != "" {
										routerRules = "\n\nRouting Rules & Policies:\n" + DefaultAgentConfig.OrchestratorRules
									}
									routerPrompt := fmt.Sprintf(
										"You are an AI query router. Analyze the user request and select the most appropriate agent ID to handle it.\n\n"+
										"Available agents:\n%s"+
										"%s\n\n"+
										"Respond with ONLY the exact agent ID from the list, or 'none' if none of the specialized agents are appropriate. Do not include quotes, punctuation, or extra words.",
										strings.Join(agentDescriptions, "\n"),
										routerRules,
									)

									routeReq := &ChatRequest{
										SystemPrompt: routerPrompt,
										Messages:     []ChatMessage{{Role: "user", Content: q}},
										MaxTokens:    10,
										Temperature:  0.0,
										Model:        model,
									}

									routeResp, err := gw.Query(bgCtx, routeReq)
									if err == nil {
										selectedID := strings.TrimSpace(strings.ToLower(routeResp.Content))
										selectedID = strings.Trim(selectedID, `'"., `)

										if selectedID != "none" && selectedID != "" {
											for _, a := range agents {
												if a.ID == selectedID {
													log.Printf("[AIAgent] Orchestrator routed query %q to agent %s (%s)", q, a.Name, a.ID)
													agent = a
													// Re-resolve model for the newly selected agent
													model = resolveAgentModel(activeProvider, agent.Model)
													break
												}
											}
										}
									} else {
										log.Printf("[AIAgent] Orchestrator routing query failed: %v, falling back to Sai Assistant", err)
									}
								}

								// Resolve Bot ID based on final resolved agent
								botID := "00000000-0000-0000-0000-000000000000"
								switch agent.ID {
								case "sai":
									botID = "00000000-0000-0000-0000-000000000001"
								case "coder":
									botID = "00000000-0000-0000-0000-000000000002"
								case "translator":
									botID = "00000000-0000-0000-0000-000000000003"
								case "summarizer":
									botID = "00000000-0000-0000-0000-000000000004"
								default:
									botID = generateDeterministicUUID(agent.ID)
								}

								temp := agent.Temperature
								if temp == 0 {
									temp = 0.7
								}

								chatReq := &ChatRequest{
									SystemPrompt: agent.SystemPrompt,
									Messages:     chatMessages,
									MaxTokens:    DefaultAgentConfig.MaxTokens,
									Temperature:  temp,
									Model:        model,
								}

								// Create placeholder message
								msgID := ""
								publishStreamFn := func(c string) {
									if publishWSFn == nil || msgID == "" {
										return
									}
									payload := map[string]string{
										"message_id": msgID,
										"room_id":    roomID,
										"content":    c,
									}
									jsonBytes, _ := json.Marshal(payload)
									_ = publishWSFn(bgCtx, roomID, "msg:stream", string(jsonBytes))
								}

								ch := make(chan StreamChunk, 64)
								errCh := make(chan error, 1)
								go func() {
									errCh <- gw.Stream(bgCtx, chatReq, ch)
								}()

								var accumulated strings.Builder
								for chunk := range ch {
									if chunk.Done || chunk.Error != nil || chunk.Content == "" {
										continue
									}
									accumulated.WriteString(chunk.Content)

									if msgID == "" {
										if sendMsgFn != nil {
											var err error
											msgID, err = sendMsgFn(bgCtx, botID, agent.Name, roomID, chunk.Content)
											if err != nil {
												log.Printf("[AIAgent] Agent %s failed to create message on first chunk: %v", agent.ID, err)
												return
											}
										}
									} else {
										publishStreamFn(accumulated.String())
									}
								}
								if err := <-errCh; err != nil {
									log.Printf("[AIAgent] Streaming error for agent %s: %v", agent.ID, err)
								}

								finalContent := accumulated.String()
								if finalContent == "" {
									finalContent = "Sorry, I cannot process this request right now."
								}

								if msgID == "" {
									if sendMsgFn != nil {
										var err error
										msgID, err = sendMsgFn(bgCtx, botID, agent.Name, roomID, finalContent)
										if err != nil {
											log.Printf("[AIAgent] Agent %s fallback reply failed: %v", agent.ID, err)
										}
									}
								} else {
									publishStreamFn(finalContent)
									// Persist final content to DB
									if updateMsgContentFn != nil && msgID != "" {
										if err := updateMsgContentFn(bgCtx, msgID, finalContent); err != nil {
											log.Printf("[AIAgent] Agent %s persist final content failed: %v", agent.ID, err)
										}
									}
								}

								// Record metrics
								inputCharCount := 0
								for _, m := range chatMessages {
									inputCharCount += len(m.Content)
								}
								inputTokens := inputCharCount / 4
								if inputTokens < 1 {
									inputTokens = 1
								}
								outputTokens := len(finalContent) / 4
								if outputTokens < 1 {
									outputTokens = 1
								}
								recordAgentMetrics(bgCtx, rdb, agent.ID, agent.Name, query, finalContent, inputTokens, outputTokens)

								// Stop typing indicator
								if publishWSFn != nil {
									data := fmt.Sprintf(`{"username":"%s","typing":false}`, agent.Name)
									_ = publishWSFn(bgCtx, roomID, "user:typing", data)
								}
							}(agent, query)
						}
					}

					return nil, nil
				},
			})
			log.Printf("[AIAgent] ✓ Multi-Agent mention/silent handler registered")

			// Register message indexing hook for semantic search
			if semIdx != nil {
				plugin.Registry.On(plugin.AfterSendMessage, plugin.HookHandler{
					Name:     "copilot:semantic-index",
					Priority: 90, // low priority — runs after other handlers
					Fn: func(ctx context.Context, p map[string]interface{}) (interface{}, error) {
						msgID, _ := p["message_id"].(string)
						roomID, _ := p["room_id"].(string)
						content, _ := p["content"].(string)
						if msgID != "" && content != "" {
							semIdx.IndexMessage(msgID, roomID, content)
						}
						return nil, nil
					},
				})
				log.Printf("[AIAgent] ✓ Semantic indexing hook registered on AfterSendMessage")
			}

			// Register digest unread tracking hook
			if rdb != nil {
				plugin.Registry.On(plugin.AfterSendMessage, plugin.HookHandler{
					Name:     "copilot:digest-track",
					Priority: 91,
					Fn: func(ctx context.Context, p map[string]interface{}) (interface{}, error) {
						// Track unread rooms for offline users (simplified: track all recipients)
						roomID, _ := p["room_id"].(string)
						senderID, _ := p["sender_id"].(string)
						if roomID != "" && senderID != "" {
							// In a full implementation, we'd check which users are offline
							// For now, the digest service handles deduplication
							_ = senderID // sender doesn't need their own digest for this message
						}
						return nil, nil
					},
				})
			}

			// registerProxyRoutes was completely removed.

			// Register AI content moderation on BeforeSendMessage
			plugin.Registry.On(plugin.BeforeSendMessage, plugin.HookHandler{
				Name:     "copilot:moderation",
				Priority: 10,
				Fn: func(ctx context.Context, p map[string]interface{}) (interface{}, error) {
					if DefaultAgentConfig == nil || !DefaultAgentConfig.Enabled || !DefaultAgentConfig.ModerationEnabled {
						return nil, nil
					}

					content, _ := p["content"].(string)
					senderID, _ := p["sender_id"].(string)

					// Skip system/bot messages or empty content
					if senderID == saiBotID || senderID == domain.SystemActorID || content == "" || strings.HasPrefix(content, "/") {
						return nil, nil
					}

					moderationPrompt := "You are a content moderation system. Analyze this chat message. Is it appropriate (e.g. no hate speech, harassment, illegal content, extreme violence, or explicit sexual content)?"
					if DefaultAgentConfig != nil && DefaultAgentConfig.ModerationRules != "" {
						moderationPrompt += "\n\nAdditional Moderation Rules & Policies:\n" + DefaultAgentConfig.ModerationRules
					}
					moderationPrompt += "\n\nRespond with only one word: 'SAFE' or 'UNSAFE'. Only return that single word."

					log.Printf("[AIAgent] Running AI moderation check on message content")
					chatReq := &ChatRequest{
						SystemPrompt: moderationPrompt,
						Messages: []ChatMessage{
							{Role: "user", Content: content},
						},
						Temperature: 0.0,
						MaxTokens:   5,
					}

					resp, err := gw.Query(ctx, chatReq)
					if err != nil {
						log.Printf("[AIAgent] Warning: AI Moderation check failed to query provider: %v", err)
						return nil, nil // Fallback: allow on API failure
					}

					result := strings.TrimSpace(strings.ToUpper(resp.Content))
if strings.Contains(result, "UNSAFE") {
						log.Printf("[AIAgent] ❌ Message blocked by AI Moderation: %q", content)
						return nil, fmt.Errorf("message blocked by AI content moderation system")
					}

					return nil, nil
				},
			})
			log.Printf("[AIAgent] ✓ AI Content Moderation hook registered on BeforeSendMessage")

			log.Printf("[AIAgent] ✓ Gateway initialized (provider=%s, model=%s)", cfg.Provider, cfg.Model)
			return nil, nil
		},
	})
}

// ── Copilot Gin Handlers ────────────────────────────────────────────────────────

func (h *aiHandler) chat(c *gin.Context) {
	var req struct {
		Message string `json:"message" binding:"required"`
		RoomID  string `json:"room_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	chatReq := &ChatRequest{
		SystemPrompt: DefaultAgentConfig.SystemPrompt,
		Messages: []ChatMessage{
			{Role: "user", Content: req.Message},
		},
		Temperature: DefaultAgentConfig.Temperature,
		MaxTokens:   DefaultAgentConfig.MaxTokens,
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_ERROR", err.Error())
		return
	}

	if h.rdb != nil {
		ctx := c.Request.Context()
		_ = h.rdb.Incr(ctx, "ai:metrics:total_queries").Err()
		if resp.InputTokens > 0 {
			_ = h.rdb.IncrBy(ctx, "ai:metrics:input_tokens", int64(resp.InputTokens)).Err()
		}
		if resp.OutputTokens > 0 {
			_ = h.rdb.IncrBy(ctx, "ai:metrics:output_tokens", int64(resp.OutputTokens)).Err()
		}
		recordQueryLog(ctx, h.rdb, "default", "Default Assistant", req.Message, resp.Content, resp.InputTokens, resp.OutputTokens)
	}

	c.JSON(http.StatusOK, gin.H{
		"reply": resp.Content,
		"model": resp.Model,
	})
}

func (h *aiHandler) summarize(c *gin.Context) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	_ = c.ShouldBindJSON(&req)
	roomID := req.RoomID
	if roomID == "" {
		roomID = "general"
	}

	if h.messageRepo == nil {
		response.Error(c, http.StatusInternalServerError, "REPOSITORY_UNAVAILABLE", "message repository not available")
		return
	}

	messages, err := h.messageRepo.GetMessageHistory(c.Request.Context(), roomID, 50, "")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	if len(messages) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"summary": "No messages to summarize in this room.",
		})
		return
	}

	var sb strings.Builder
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.IsDeleted {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
	}

	chatReq := &ChatRequest{
		SystemPrompt: "You are an AI assistant helping to summarize chat conversations. Please briefly summarize the conversation below in English, clearly stating the main points and decisions (if any).",
		Messages: []ChatMessage{
			{Role: "user", Content: sb.String()},
		},
		Temperature: DefaultAgentConfig.Temperature,
		MaxTokens:   DefaultAgentConfig.MaxTokens,
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_ERROR", err.Error())
		return
	}

	if h.rdb != nil {
		ctx := c.Request.Context()
		_ = h.rdb.Incr(ctx, "ai:metrics:total_queries").Err()
		if resp.InputTokens > 0 {
			_ = h.rdb.IncrBy(ctx, "ai:metrics:input_tokens", int64(resp.InputTokens)).Err()
		}
		if resp.OutputTokens > 0 {
			_ = h.rdb.IncrBy(ctx, "ai:metrics:output_tokens", int64(resp.OutputTokens)).Err()
		}
		recordQueryLog(ctx, h.rdb, "summarizer", "Summarizer", "Summarize Room Chat History", resp.Content, resp.InputTokens, resp.OutputTokens)
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": resp.Content,
		"model":   resp.Model,
	})
}

func (h *aiHandler) listAgents(c *gin.Context) {
	if h.db == nil {
		response.Error(c, http.StatusInternalServerError, "DB_UNAVAILABLE", "database not available")
		return
	}
	var agents []AIAgent
	if err := h.db.Order("created_at asc").Find(&agents).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, agents)
}

func (h *aiHandler) createAgent(c *gin.Context) {
	if h.db == nil {
		response.Error(c, http.StatusInternalServerError, "DB_UNAVAILABLE", "database not available")
		return
	}

	var req struct {
		Name           string  `json:"name" binding:"required"`
		Username       string  `json:"username" binding:"required"`
		SystemPrompt   string  `json:"systemPrompt" binding:"required"`
		Avatar         string  `json:"avatar"`
		Model          string  `json:"model"`
		Temperature    float64 `json:"temperature"`
		TriggerType    string  `json:"triggerType"`
		TriggerKeyword string  `json:"triggerKeyword"`
		RoomIDs        string  `json:"roomIds"`
		Enabled        *bool   `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	// Clean username
	username := strings.ToLower(req.Username)
	username = strings.ReplaceAll(username, "@", "")
	username = strings.ReplaceAll(username, " ", "")

	var count int64
	h.db.Model(&AIAgent{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		response.Error(c, http.StatusBadRequest, "ALREADY_EXISTS", "agent username already exists")
		return
	}

	avatar := req.Avatar
	if avatar == "" {
		avatar = "🤖"
	}
	model := req.Model
	if model == "" {
		model = DefaultAgentConfig.Model
	}
	temp := 0.7
	if req.Temperature > 0 {
		temp = req.Temperature
	}
	triggerType := req.TriggerType
	if triggerType == "" {
		triggerType = "mention"
	}
	triggerKeyword := req.TriggerKeyword
	if triggerKeyword == "" {
		triggerKeyword = "@sai_" + username
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	agent := AIAgent{
		ID:             uuid.New().String(),
		Name:           req.Name,
		Username:       username,
		Avatar:         avatar,
		SystemPrompt:   req.SystemPrompt,
		Model:          model,
		Temperature:    temp,
		TriggerType:    triggerType,
		TriggerKeyword: triggerKeyword,
		RoomIDs:        req.RoomIDs,
		Enabled:        enabled,
	}

	if err := h.db.Create(&agent).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	_ = syncBotUser(c.Request.Context(), h.db, agent)

	c.JSON(http.StatusCreated, agent)
}

func (h *aiHandler) updateAgent(c *gin.Context) {
	if h.db == nil {
		response.Error(c, http.StatusInternalServerError, "DB_UNAVAILABLE", "database not available")
		return
	}
	id := c.Param("id")

	var agent AIAgent
	if err := h.db.First(&agent, "id = ?", id).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "agent not found")
		return
	}

	var req struct {
		Name           string   `json:"name"`
		Username       string   `json:"username"`
		SystemPrompt   string   `json:"systemPrompt"`
		Avatar         string   `json:"avatar"`
		Model          string   `json:"model"`
		Temperature    *float64 `json:"temperature"`
		TriggerType    string   `json:"triggerType"`
		TriggerKeyword string   `json:"triggerKeyword"`
		RoomIDs        string   `json:"roomIds"`
		Enabled        *bool    `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	if req.Name != "" {
		agent.Name = req.Name
	}
	if req.Username != "" {
		username := strings.ToLower(req.Username)
		username = strings.ReplaceAll(username, "@", "")
		username = strings.ReplaceAll(username, " ", "")
		
		var count int64
		h.db.Model(&AIAgent{}).Where("username = ? AND id != ?", username, id).Count(&count)
		if count > 0 {
			response.Error(c, http.StatusBadRequest, "ALREADY_EXISTS", "agent username already exists")
			return
		}
		agent.Username = username
	}
	if req.SystemPrompt != "" {
		agent.SystemPrompt = req.SystemPrompt
	}
	if req.Avatar != "" {
		agent.Avatar = req.Avatar
	}
	if req.Model != "" {
		agent.Model = req.Model
	}
	if req.Temperature != nil {
		agent.Temperature = *req.Temperature
	}
	if req.TriggerType != "" {
		agent.TriggerType = req.TriggerType
	}
	if req.TriggerKeyword != "" {
		agent.TriggerKeyword = req.TriggerKeyword
	}
	if req.RoomIDs != "" {
		agent.RoomIDs = req.RoomIDs
	}
	if req.Enabled != nil {
		agent.Enabled = *req.Enabled
	}

	if err := h.db.Save(&agent).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	_ = syncBotUser(c.Request.Context(), h.db, agent)

	c.JSON(http.StatusOK, agent)
}

func (h *aiHandler) deleteAgent(c *gin.Context) {
	if h.db == nil {
		response.Error(c, http.StatusInternalServerError, "DB_UNAVAILABLE", "database not available")
		return
	}
	id := c.Param("id")

	var agent AIAgent
	if err := h.db.First(&agent, "id = ?", id).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "agent not found")
		return
	}

	if err := h.db.Delete(&agent).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	// Disable user in main users table
	_ = h.db.Exec("UPDATE users SET is_bot = false, status = 'offline' WHERE id = ?", agent.ID).Error

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *aiHandler) testAgent(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	var agent AIAgent
	if err := h.db.First(&agent, "id = ?", id).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "agent not found")
		return
	}

	chatReq := &ChatRequest{
		SystemPrompt: agent.SystemPrompt,
		Messages: []ChatMessage{
			{Role: "user", Content: req.Message},
		},
		Temperature: agent.Temperature,
		MaxTokens:   DefaultAgentConfig.MaxTokens,
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_ERROR", err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reply": resp.Content,
		"model": resp.Model,
	})
}

// registerProxyRoutes was completely removed.

// ── Internal Handler ──────────────────────────────────────────────────────────

type aiHandler struct {
	gateway       *Gateway
	messageRepo   domain.MessageRepository
	rdb           *redis.Client
	semanticIndex *SemanticIndex
	db            *gorm.DB
}

type aiQueryRequest struct {
	Prompt string `json:"prompt" binding:"required"`
	RoomID string `json:"room_id"`
}

func (h *aiHandler) query(c *gin.Context) {
	var req aiQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	chatReq := &ChatRequest{
		SystemPrompt: DefaultAgentConfig.SystemPrompt,
		Messages: []ChatMessage{
			{Role: "user", Content: req.Prompt},
		},
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_QUERY_FAILED", err.Error())
		return
	}

	h.recordMetrics(c.Request.Context(), "default", "Default Assistant", req.Prompt, resp)
	response.JSON(c, http.StatusOK, gin.H{
		"response":      resp.Content,
		"content":       resp.Content,
		"model":         resp.Model,
		"input_tokens":  resp.InputTokens,
		"output_tokens": resp.OutputTokens,
	})
}

// ── SSE Streaming Endpoint ────────────────────────────────────────────────────

func (h *aiHandler) queryStream(c *gin.Context) {
	var req aiQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	chatReq := &ChatRequest{
		SystemPrompt: DefaultAgentConfig.SystemPrompt,
		Messages: []ChatMessage{
			{Role: "user", Content: req.Prompt},
		},
		MaxTokens:   DefaultAgentConfig.MaxTokens,
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Create streaming channel
	ch := make(chan StreamChunk, 64)

	// Track total content for metrics
	var totalContent strings.Builder

	// Start streaming in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.gateway.Stream(c.Request.Context(), chatReq, ch)
	}()

	// Read chunks and write SSE events
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.Error(c, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "Streaming not supported")
		return
	}

	for chunk := range ch {
		if chunk.Error != nil {
			errData, _ := json.Marshal(map[string]string{"error": chunk.Error.Error()})
			fmt.Fprintf(c.Writer, "data: %s\n\n", errData)
			flusher.Flush()
			continue
		}

		if chunk.Done {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}

		totalContent.WriteString(chunk.Content)
		chunkData, _ := json.Marshal(map[string]string{"content": chunk.Content})
		fmt.Fprintf(c.Writer, "data: %s\n\n", chunkData)
		flusher.Flush()
	}

	// Wait for stream goroutine to finish
	if err := <-errCh; err != nil {
		log.Printf("[AIAgent] Stream error: %v", err)
	}

	// Record approximate metrics
	approxTokens := len(strings.Fields(totalContent.String()))
	h.recordMetrics(c.Request.Context(), "default", "Default Assistant", req.Prompt, &ChatResponse{
		Content:      totalContent.String(),
		OutputTokens: approxTokens,
		InputTokens:  len(strings.Fields(req.Prompt)),
	})
}

// ── Semantic Search Endpoint ──────────────────────────────────────────────────

type aiSearchRequest struct {
	Query   string   `json:"query" binding:"required"`
	RoomIDs []string `json:"room_ids"`
	Limit   int      `json:"limit"`
}

func (h *aiHandler) semanticSearch(c *gin.Context) {
	if h.semanticIndex == nil {
		response.Error(c, http.StatusServiceUnavailable, "SEMANTIC_SEARCH_UNAVAILABLE", "Semantic search is not configured")
		return
	}

	var req aiSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	results, err := h.semanticIndex.Search(c.Request.Context(), req.Query, req.RoomIDs, req.Limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"results": results,
		"count":   len(results),
	})
}

type aiSummaryRequest struct {
	RoomID string `json:"room_id" binding:"required"`
}

func (h *aiHandler) summary(c *gin.Context) {
	var req aiSummaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	messages, err := h.messageRepo.GetMessageHistory(c.Request.Context(), req.RoomID, 50, "")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	if len(messages) == 0 {
		response.JSON(c, http.StatusOK, gin.H{
			"summary": "No messages to summarize in this room.",
		})
		return
	}

	var sb strings.Builder
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.IsDeleted {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
	}

	chatReq := &ChatRequest{
		SystemPrompt: "You are an AI assistant helping to summarize chat conversations. Please briefly summarize the conversation below in English, clearly stating the main points and decisions (if any).",
		Messages: []ChatMessage{
			{Role: "user", Content: sb.String()},
		},
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_SUMMARY_FAILED", err.Error())
		return
	}

	h.recordMetrics(c.Request.Context(), "summarizer", "Summarizer", "Summarize Room Chat History", resp)
	response.JSON(c, http.StatusOK, gin.H{
		"summary": resp.Content,
		"model":   resp.Model,
	})
}

type aiTranslateRequest struct {
	MessageID  string `json:"message_id"`
	Text       string `json:"text"`
	TargetLang string `json:"target_lang" binding:"required"`
}

func (h *aiHandler) translate(c *gin.Context) {
	var req aiTranslateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	textToTranslate := req.Text
	if req.MessageID != "" && textToTranslate == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Either text or message_id must be provided")
		return
	}

	if textToTranslate == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Text cannot be empty")
		return
	}

	systemPrompt := fmt.Sprintf("You are a professional translator. Translate the following text into the language with code '%s'. Return only the translation, without any additional explanations.", req.TargetLang)

	chatReq := &ChatRequest{
		SystemPrompt: systemPrompt,
		Messages: []ChatMessage{
			{Role: "user", Content: textToTranslate},
		},
	}

	resp, err := h.gateway.Query(c.Request.Context(), chatReq)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AI_TRANSLATION_FAILED", err.Error())
		return
	}

	h.recordMetrics(c.Request.Context(), "translator", "Translator", fmt.Sprintf("Translate to %s: %s", req.TargetLang, textToTranslate), resp)
	response.JSON(c, http.StatusOK, gin.H{
		"translated_text": resp.Content,
		"model":           resp.Model,
	})
}

func (h *aiHandler) listProviders(c *gin.Context) {
	providers := h.gateway.ListProviders()
	response.JSON(c, http.StatusOK, providers)
}

func (h *aiHandler) getConfig(c *gin.Context) {
	roleVal, _ := c.Get("role")
	if roleVal != "admin" && roleVal != "super_admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"provider":           h.gateway.GetPrimary(),
		"model":              DefaultAgentConfig.Model,
		"maxTokens":          DefaultAgentConfig.MaxTokens,
		"apiKey":             DefaultAgentConfig.APIKey,
		"systemPrompt":       DefaultAgentConfig.SystemPrompt,
		"temperature":        DefaultAgentConfig.Temperature,
		"autoReply":          DefaultAgentConfig.AutoReply,
		"moderationEnabled":  DefaultAgentConfig.ModerationEnabled,
		"orchestratorRules": DefaultAgentConfig.OrchestratorRules,
		"moderationRules":   DefaultAgentConfig.ModerationRules,
		"providers":          DefaultAgentConfig.Providers,
	})
}

type aiUpdateConfigRequest struct {
	Provider          string                    `json:"provider"`
	Model             string                    `json:"model"`
	MaxTokens         int                       `json:"maxTokens"`
	APIKey            string                    `json:"apiKey"`
	SystemPrompt      string                    `json:"systemPrompt"`
	Temperature       *float64                  `json:"temperature"`
	AutoReply         *bool                     `json:"autoReply"`
	ModerationEnabled *bool                     `json:"moderationEnabled"`
	OrchestratorRules string                    `json:"orchestratorRules"`
	ModerationRules   string                    `json:"moderationRules"`
	Providers         map[string]ProviderConfig `json:"providers"`
}

func (h *aiHandler) updateConfig(c *gin.Context) {
	roleVal, _ := c.Get("role")
	if roleVal != "admin" && roleVal != "super_admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	var req aiUpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	ctx := c.Request.Context()

	if req.Provider != "" {
		if err := h.gateway.SetPrimary(req.Provider); err != nil {
			response.Error(c, http.StatusBadRequest, "INVALID_PROVIDER", err.Error())
			return
		}
		DefaultAgentConfig.Provider = req.Provider
	}

	if req.Model != "" {
		DefaultAgentConfig.Model = req.Model
	}

	if req.MaxTokens > 0 {
		DefaultAgentConfig.MaxTokens = req.MaxTokens
	}

	if req.APIKey != "" {
		DefaultAgentConfig.APIKey = req.APIKey
		h.gateway.SetAPIKey(DefaultAgentConfig.Provider, req.APIKey)
	}

	if req.SystemPrompt != "" {
		DefaultAgentConfig.SystemPrompt = req.SystemPrompt
	}

	if req.Temperature != nil {
		DefaultAgentConfig.Temperature = *req.Temperature
	}

	if req.AutoReply != nil {
		DefaultAgentConfig.AutoReply = *req.AutoReply
	}

	if req.ModerationEnabled != nil {
		DefaultAgentConfig.ModerationEnabled = *req.ModerationEnabled
	}

	DefaultAgentConfig.OrchestratorRules = req.OrchestratorRules
	DefaultAgentConfig.ModerationRules = req.ModerationRules

	if req.Providers != nil {
		for pid, pcfg := range req.Providers {
			DefaultAgentConfig.Providers[pid] = pcfg
			
			if h.rdb != nil {
				h.rdb.Set(ctx, "ai:provider:"+pid+":api_key", pcfg.APIKey, 0)
				h.rdb.Set(ctx, "ai:provider:"+pid+":base_url", pcfg.BaseURL, 0)
				h.rdb.Set(ctx, "ai:provider:"+pid+":model", pcfg.Model, 0)
			}
			
			switch pid {
			case "openai":
				h.gateway.RegisterProvider(NewOpenAIProvider(pcfg.APIKey, pcfg.BaseURL, pcfg.Model))
			case "claude":
				h.gateway.RegisterProvider(NewClaudeProvider(pcfg.APIKey, pcfg.BaseURL, pcfg.Model))
			case "gemini":
				h.gateway.RegisterProvider(NewGeminiProvider(pcfg.APIKey, pcfg.BaseURL, pcfg.Model))
			case "ollama":
				h.gateway.RegisterProvider(NewOllamaProvider(pcfg.BaseURL, pcfg.Model))
			}
		}
	}

	// Persist to Redis
	if h.rdb != nil {
		h.rdb.Set(ctx, "ai:config:provider", DefaultAgentConfig.Provider, 0)
		h.rdb.Set(ctx, "ai:config:api_key", DefaultAgentConfig.APIKey, 0)
		h.rdb.Set(ctx, "ai:config:model", DefaultAgentConfig.Model, 0)
		h.rdb.Set(ctx, "ai:config:system_prompt", DefaultAgentConfig.SystemPrompt, 0)
		h.rdb.Set(ctx, "ai:config:max_tokens", strconv.Itoa(DefaultAgentConfig.MaxTokens), 0)
		h.rdb.Set(ctx, "ai:config:temperature", fmt.Sprintf("%f", DefaultAgentConfig.Temperature), 0)
		h.rdb.Set(ctx, "ai:config:auto_reply", strconv.FormatBool(DefaultAgentConfig.AutoReply), 0)
		h.rdb.Set(ctx, "ai:config:moderation_enabled", strconv.FormatBool(DefaultAgentConfig.ModerationEnabled), 0)
		h.rdb.Set(ctx, "ai:config:orchestrator_rules", DefaultAgentConfig.OrchestratorRules, 0)
		h.rdb.Set(ctx, "ai:config:moderation_rules", DefaultAgentConfig.ModerationRules, 0)
	}

	response.JSON(c, http.StatusOK, gin.H{
		"provider":           h.gateway.GetPrimary(),
		"model":              DefaultAgentConfig.Model,
		"maxTokens":          DefaultAgentConfig.MaxTokens,
		"apiKey":             DefaultAgentConfig.APIKey,
		"systemPrompt":       DefaultAgentConfig.SystemPrompt,
		"temperature":        DefaultAgentConfig.Temperature,
		"autoReply":          DefaultAgentConfig.AutoReply,
		"moderationEnabled":  DefaultAgentConfig.ModerationEnabled,
		"orchestratorRules": DefaultAgentConfig.OrchestratorRules,
		"moderationRules":   DefaultAgentConfig.ModerationRules,
		"providers":          DefaultAgentConfig.Providers,
	})
}

func (h *aiHandler) getMetrics(c *gin.Context) {
	roleVal, _ := c.Get("role")
	if roleVal != "admin" && roleVal != "super_admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	ctx := c.Request.Context()
	totalQueriesStr, _ := h.rdb.Get(ctx, "ai:metrics:total_queries").Result()
	inputTokensStr, _ := h.rdb.Get(ctx, "ai:metrics:input_tokens").Result()
	outputTokensStr, _ := h.rdb.Get(ctx, "ai:metrics:output_tokens").Result()

	var totalQueries, inputTokens, outputTokens int64
	fmt.Sscanf(totalQueriesStr, "%d", &totalQueries)
	fmt.Sscanf(inputTokensStr, "%d", &inputTokens)
	fmt.Sscanf(outputTokensStr, "%d", &outputTokens)

	inputCost := float64(inputTokens) * 0.0000015
	outputCost := float64(outputTokens) * 0.000002
	totalCost := inputCost + outputCost

	var agents []AIAgent
	if h.db != nil {
		_ = h.db.Find(&agents).Error
	}

	botsMetrics := make([]gin.H, 0)
	for _, agent := range agents {
		bQueriesStr, _ := h.rdb.Get(ctx, fmt.Sprintf("ai:metrics:bot:%s:queries", agent.ID)).Result()
		bInputStr, _ := h.rdb.Get(ctx, fmt.Sprintf("ai:metrics:bot:%s:input_tokens", agent.ID)).Result()
		bOutputStr, _ := h.rdb.Get(ctx, fmt.Sprintf("ai:metrics:bot:%s:output_tokens", agent.ID)).Result()

		var bQueries, bInput, bOutput int64
		fmt.Sscanf(bQueriesStr, "%d", &bQueries)
		fmt.Sscanf(bInputStr, "%d", &bInput)
		fmt.Sscanf(bOutputStr, "%d", &bOutput)

		bInputCost := float64(bInput) * 0.0000015
		bOutputCost := float64(bOutput) * 0.000002
		bTotalCost := bInputCost + bOutputCost

		botsMetrics = append(botsMetrics, gin.H{
			"agent_id":      agent.ID,
			"name":          agent.Name,
			"username":      agent.Username,
			"avatar":        agent.Avatar,
			"queries":       bQueries,
			"input_tokens":  bInput,
			"output_tokens": bOutput,
			"cost":          bTotalCost,
		})
	}

	// Fetch logs
	logs := make([]gin.H, 0)
	if h.rdb != nil {
		logsStr, _ := h.rdb.LRange(ctx, "ai:metrics:query_logs", 0, -1).Result()
		for _, logStr := range logsStr {
			var entry gin.H
			if err := json.Unmarshal([]byte(logStr), &entry); err == nil {
				logs = append(logs, entry)
			}
		}
	}

	response.JSON(c, http.StatusOK, gin.H{
		"total_queries": totalQueries,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_cost":    totalCost,
		"bots":          botsMetrics,
		"logs":          logs,
	})
}

func (h *aiHandler) recordMetrics(ctx context.Context, agentID string, agentName string, query string, resp *ChatResponse) {
	if h.rdb == nil || resp == nil {
		return
	}
	_ = h.rdb.Incr(ctx, "ai:metrics:total_queries").Err()
	if resp.InputTokens > 0 {
		_ = h.rdb.IncrBy(ctx, "ai:metrics:input_tokens", int64(resp.InputTokens)).Err()
	}
	if resp.OutputTokens > 0 {
		_ = h.rdb.IncrBy(ctx, "ai:metrics:output_tokens", int64(resp.OutputTokens)).Err()
	}

	recordQueryLog(ctx, h.rdb, agentID, agentName, query, resp.Content, resp.InputTokens, resp.OutputTokens)
}

// escapeJSONString escapes a string for safe embedding inside a JSON string value.
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// ── Multi-Agent Helpers ──────────────────────────────────────────────────────

func generateDeterministicUUID(input string) string {
	h := sha256.Sum256([]byte(input))
	hexPart := fmt.Sprintf("%x", h[:6])
	return fmt.Sprintf("00000000-0000-0000-0000-%s", hexPart)
}

func syncBotUser(ctx context.Context, db *gorm.DB, agent AIAgent) error {
	if db == nil {
		return fmt.Errorf("database not available")
	}

	botID := ""
	switch agent.ID {
	case "sai":
		botID = "00000000-0000-0000-0000-000000000001"
	case "coder":
		botID = "00000000-0000-0000-0000-000000000002"
	case "translator":
		botID = "00000000-0000-0000-0000-000000000003"
	case "summarizer":
		botID = "00000000-0000-0000-0000-000000000004"
	default:
		botID = generateDeterministicUUID(agent.ID)
	}

	var user domain.User
	err := db.Where("id = ?", botID).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		// Create new bot user
		user = domain.User{
			BaseModel: domain.BaseModel{
				ID: botID,
			},
			TenantID:     domain.DefaultTenantID,
			Username:     agent.Username,
			Email:        agent.Username + "@bot.local",
			DisplayName:  agent.Name,
			SystemRole:   "bot",
			Presence:     "online",
			AvatarURL:    agent.Avatar,
			IsActive:     agent.Enabled,
		}
		if err := db.Create(&user).Error; err != nil {
			log.Printf("[AIAgent] Failed to create bot user %s: %v", agent.Username, err)
			return err
		}
		log.Printf("[AIAgent] Created bot user: %s", agent.Username)
	} else if err == nil {
		// Update existing bot user
		user.Username = agent.Username
		user.Email = agent.Username + "@bot.local"
		user.DisplayName = agent.Name
		user.AvatarURL = agent.Avatar
		user.IsActive = agent.Enabled
		user.SystemRole = "bot"
		user.Presence = "online"
		if err := db.Save(&user).Error; err != nil {
			log.Printf("[AIAgent] Failed to update bot user %s: %v", agent.Username, err)
			return err
		}
		log.Printf("[AIAgent] Updated bot user: %s", agent.Username)
	} else {
		return err
	}
	return nil
}

func seedDefaultAgents(ctx context.Context, db *gorm.DB, userRepo domain.UserRepository) {
	if db == nil {
		return
	}

	var count int64
	if err := db.Model(&AIAgent{}).Count(&count).Error; err != nil {
		log.Printf("[AIAgent] Failed to check agents count: %v", err)
		return
	}

	if count > 0 {
		// Ensure bot users are in sync even if agents already exist in DB
		var agents []AIAgent
		if err := db.Find(&agents).Error; err == nil {
			for _, agent := range agents {
				_ = syncBotUser(ctx, db, agent)
			}
		}
		return
	}

	defaultAgents := []AIAgent{
		{
			ID:             "sai",
			Name:           "Sai Assistant",
			Username:       "sai",
			Avatar:         "🤖",
			SystemPrompt:   "You are Sai, a friendly and helpful AI assistant. Answer the user's questions clearly, concisely, and accurately.",
			Model:          "gpt-4o",
			Temperature:    0.7,
			TriggerType:    "mention",
			TriggerKeyword: "@sai",
			Enabled:        true,
		},
		{
			ID:             "coder",
			Name:           "Sai Coder",
			Username:       "sai_coder",
			Avatar:         "💻",
			SystemPrompt:   "You are Sai Coder, an expert software engineer. Help the user write clean, efficient, and well-documented code. Always specify language syntax highlighting for code blocks.",
			Model:          "gpt-4o",
			Temperature:    0.3,
			TriggerType:    "mention",
			TriggerKeyword: "@sai:coder",
			Enabled:        true,
		},
		{
			ID:             "translator",
			Name:           "Sai Translator",
			Username:       "sai_translator",
			Avatar:         "🌐",
			SystemPrompt:   "You are Sai Translator. Help the user translate text between different languages. Maintain the original tone and context.",
			Model:          "gpt-4o",
			Temperature:    0.3,
			TriggerType:    "mention",
			TriggerKeyword: "@sai:translator",
			Enabled:        true,
		},
		{
			ID:             "summarizer",
			Name:           "Sai Summarizer",
			Username:       "sai_summarizer",
			Avatar:         "📝",
			SystemPrompt:   "You are Sai Summarizer. Help the user summarize long texts or conversation history. Highlight the main points and decisions clearly using bullet points.",
			Model:          "gpt-4o",
			Temperature:    0.5,
			TriggerType:    "mention",
			TriggerKeyword: "@sai:summarizer",
			Enabled:        true,
		},
	}

	for _, agent := range defaultAgents {
		if err := db.Create(&agent).Error; err != nil {
			log.Printf("[AIAgent] Failed to seed agent %s: %v", agent.Name, err)
		} else {
			log.Printf("[AIAgent] Seeded default agent: %s", agent.Name)
			_ = syncBotUser(ctx, db, agent)
		}
	}
}

func isModelCompatible(providerID, model string) bool {
	if model == "" {
		return false
	}
	modelLower := strings.ToLower(model)
	switch providerID {
	case "openai":
		return strings.HasPrefix(modelLower, "gpt-") || strings.HasPrefix(modelLower, "o1-") || strings.HasPrefix(modelLower, "o3-") || strings.Contains(modelLower, "davinci")
	case "claude":
		return strings.HasPrefix(modelLower, "claude-")
	case "gemini":
		return strings.HasPrefix(modelLower, "gemini-")
	case "ollama":
		return !strings.HasPrefix(modelLower, "gpt-") && !strings.HasPrefix(modelLower, "claude-") && !strings.HasPrefix(modelLower, "gemini-")
	}
	return true
}

func resolveAgentModel(activeProvider string, agentModel string) string {
	if isModelCompatible(activeProvider, agentModel) {
		return agentModel
	}
	
	if DefaultAgentConfig != nil {
		if pcfg, ok := DefaultAgentConfig.Providers[activeProvider]; ok && pcfg.Model != "" {
			return pcfg.Model
		}
	}
	
	switch activeProvider {
	case "openai":
		return "gpt-4o"
	case "claude":
		return "claude-3-5-sonnet-20241022"
	case "gemini":
		return "gemini-2.5-flash"
	case "ollama":
		return "llama3.1"
	}
	
	return agentModel
}

func recordAgentMetrics(ctx context.Context, rdb *redis.Client, agentID string, agentName string, query string, response string, inputTokens, outputTokens int) {
	if rdb == nil {
		return
	}
	_ = rdb.Incr(ctx, "ai:metrics:total_queries").Err()
	_ = rdb.Incr(ctx, fmt.Sprintf("ai:metrics:bot:%s:queries", agentID)).Err()
	
	if inputTokens > 0 {
		_ = rdb.IncrBy(ctx, "ai:metrics:input_tokens", int64(inputTokens)).Err()
		_ = rdb.IncrBy(ctx, fmt.Sprintf("ai:metrics:bot:%s:input_tokens", agentID), int64(inputTokens)).Err()
	}
	if outputTokens > 0 {
		_ = rdb.IncrBy(ctx, "ai:metrics:output_tokens", int64(outputTokens)).Err()
		_ = rdb.IncrBy(ctx, fmt.Sprintf("ai:metrics:bot:%s:output_tokens", agentID), int64(outputTokens)).Err()
	}

	recordQueryLog(ctx, rdb, agentID, agentName, query, response, inputTokens, outputTokens)
}

type QueryLog struct {
	Timestamp    string  `json:"timestamp"`
	AgentID      string  `json:"agentId"`
	AgentName    string  `json:"agentName"`
	Query        string  `json:"query"`
	Response     string  `json:"response"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost"`
}

func recordQueryLog(ctx context.Context, rdb *redis.Client, agentID string, agentName string, query string, response string, inputTokens, outputTokens int) {
	if rdb == nil {
		return
	}
	cost := float64(inputTokens)*0.0000015 + float64(outputTokens)*0.000002

	maxLen := 1000
	shortQuery := query
	if len(shortQuery) > maxLen {
		shortQuery = shortQuery[:maxLen] + "..."
	}
	shortResponse := response
	if len(shortResponse) > maxLen {
		shortResponse = shortResponse[:maxLen] + "..."
	}

	logEntry := QueryLog{
		Timestamp:    time.Now().Format(time.RFC3339),
		AgentID:      agentID,
		AgentName:    agentName,
		Query:        shortQuery,
		Response:     shortResponse,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
	}

	logBytes, err := json.Marshal(logEntry)
	if err != nil {
		return
	}

	key := "ai:metrics:query_logs"
	_ = rdb.LPush(ctx, key, logBytes).Err()
	_ = rdb.LTrim(ctx, key, 0, 99).Err()
}

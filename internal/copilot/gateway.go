package copilot

import (
	"context"
	"fmt"
	"sync"
)

// DefaultGateway is the global AI Gateway instance initialized at startup.
var DefaultGateway *Gateway

// Gateway manages the registered providers and routes queries to the primary provider.
type Gateway struct {
	mu        sync.RWMutex
	providers map[string]Provider
	primary   string // active provider ID from config
	embedder  string // preferred provider ID for embeddings (optional)
}

// NewGateway creates a new Gateway instance.
func NewGateway(primary string) *Gateway {
	return &Gateway{
		providers: make(map[string]Provider),
		primary:   primary,
	}
}

// RegisterProvider registers a new AI provider.
func (g *Gateway) RegisterProvider(p Provider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[p.ID()] = p
}

// Query routes a chat completion request to the active provider.
func (g *Gateway) Query(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	g.mu.RLock()
	primary := g.primary
	p, ok := g.providers[primary]
	g.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("active AI provider %q is not registered", primary)
	}
	return p.Chat(ctx, req)
}

// Stream routes a streaming chat completion request to the active provider.
func (g *Gateway) Stream(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	g.mu.RLock()
	primary := g.primary
	p, ok := g.providers[primary]
	g.mu.RUnlock()

	if !ok {
		close(ch)
		return fmt.Errorf("active AI provider %q is not registered", primary)
	}
	return p.ChatStream(ctx, req, ch)
}

// Embed routes an embedding generation request to a provider that supports
// embeddings. The chat provider (g.primary) may be embeddings-only incapable
// (e.g. Claude); in that case the request is routed to the configured embedder,
// then to any registered provider that supports embeddings.
func (g *Gateway) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	p, err := g.resolveEmbedder()
	if err != nil {
		return nil, err
	}
	return p.Embeddings(ctx, texts)
}

// resolveEmbedder picks the provider used for embeddings. Preference order:
// the explicitly configured embedder, then the primary chat provider, then any
// other registered provider that supports embeddings (openai, gemini, ollama).
func (g *Gateway) resolveEmbedder() (Provider, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	candidates := []string{g.embedder, g.primary, "openai", "gemini", "ollama"}
	for _, id := range candidates {
		if id == "" {
			continue
		}
		if p, ok := g.providers[id]; ok && p.SupportsEmbeddings() {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no registered AI provider supports embeddings")
}

// SetEmbedder sets the preferred provider for embeddings. Empty string falls
// back to automatic resolution.
func (g *Gateway) SetEmbedder(providerID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.embedder = providerID
}

// GetEmbedder returns the resolved provider ID currently used for embeddings,
// or "" if none can serve embeddings.
func (g *Gateway) GetEmbedder() string {
	p, err := g.resolveEmbedder()
	if err != nil {
		return ""
	}
	return p.ID()
}

// SetPrimary changes the active provider.
func (g *Gateway) SetPrimary(providerID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.primary = providerID
	return nil
}

// SetAPIKey updates the API Key of the specified provider dynamically.
func (g *Gateway) SetAPIKey(providerID string, apiKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.providers[providerID]
	if ok {
		if settable, ok := p.(interface{ SetAPIKey(string) }); ok {
			settable.SetAPIKey(apiKey)
		}
	}
}

// GetPrimary returns the active provider ID.
func (g *Gateway) GetPrimary() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.primary
}

// ListProviders returns a list of all registered provider IDs.
func (g *Gateway) ListProviders() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	list := make([]string, 0, len(g.providers))
	for id := range g.providers {
		list = append(list, id)
	}
	return list
}

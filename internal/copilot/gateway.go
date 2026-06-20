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

// Embed routes an embedding generation request to the active provider.
func (g *Gateway) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	g.mu.RLock()
	primary := g.primary
	p, ok := g.providers[primary]
	g.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("active AI provider %q is not registered", primary)
	}
	return p.Embeddings(ctx, texts)
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

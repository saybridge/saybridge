package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// ──────────────────────────────────────────────────────────────────────────────
// Enhanced Hook System — WillBe/HasBeen Mutation + Propagation Control
// ──────────────────────────────────────────────────────────────────────────────

// HookResult is the return type from typed hook handlers.
// Supports propagation control: handlers can stop the chain or cancel the operation.
type HookResult struct {
	// StopPropagation prevents subsequent handlers from running.
	// The operation itself still proceeds.
	StopPropagation bool

	// Cancel aborts the entire operation (only for Before*/WillBe hooks).
	// If Cancel is true, the core will NOT persist the change.
	Cancel bool

	// CancelReason provides a human-readable reason when Cancel is true.
	CancelReason string

	// Data holds any return data from the handler (e.g., modified payload).
	Data interface{}
}

// TypedHookHandler is a handler that works with typed payloads.
// The payload is passed as interface{} and handlers type-assert to the expected type.
type TypedHookHandler struct {
	Name     string
	Priority int
	Runtime  HookRuntime
	Fn       func(ctx context.Context, payload interface{}) (*HookResult, error)
}

// EmitTyped fires typed handlers in priority order with mutation + propagation support.
// Before* hooks: handlers can mutate the payload (WillBe pattern) or cancel the operation.
// After* hooks: handlers receive the final payload (HasBeen pattern), cannot cancel.
//
// Returns:
//   - The (potentially mutated) payload
//   - Whether the operation was cancelled
//   - Any error from a handler
func (r *HookRegistry) EmitTyped(ctx context.Context, event HookEvent, payload interface{}) (interface{}, bool, error) {
	r.mu.RLock()
	handlers := r.typedHandlers[event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		return payload, false, nil
	}

	for _, h := range handlers {
		result, err := h.Fn(ctx, payload)
		if err != nil {
			log.Error().Err(err).Msgf("[Hook Registry] Typed handler '%s' for event '%s' returned error", h.Name, event)
			return payload, false, fmt.Errorf("handler '%s': %w", h.Name, err)
		}

		if result != nil {
			// Check cancellation (Before* hooks only)
			if result.Cancel {
				log.Warn().Msgf("[Hook Registry] Handler '%s' cancelled event '%s': %s", h.Name, event, result.CancelReason)
				return payload, true, nil
			}

			// Check propagation stop
			if result.StopPropagation {
				log.Info().Msgf("[Hook Registry] Handler '%s' stopped propagation for event '%s'", h.Name, event)
				break
			}

			// Apply mutation if handler returned data
			if result.Data != nil {
				payload = result.Data
			}
		}
	}

	return payload, false, nil
}

// EmitTypedAsync fires typed handlers asynchronously with a WaitGroup.
// Used for After* hooks where errors are logged but don't block.
// Optionally collects errors for monitoring.
func (r *HookRegistry) EmitTypedAsync(ctx context.Context, event HookEvent, payload interface{}) <-chan error {
	errCh := make(chan error, 1)

	r.mu.RLock()
	handlers := r.typedHandlers[event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		close(errCh)
		return errCh
	}

	go func() {
		defer close(errCh)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var errs []error

		for _, h := range handlers {
			wg.Add(1)
			go func(handler TypedHookHandler) {
				defer wg.Done()
				_, err := handler.Fn(ctx, payload)
				if err != nil {
					log.Error().Err(err).Msgf("[Hook Registry] Async typed handler '%s' for event '%s' returned error",
						handler.Name, event)
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}(h)
		}

		wg.Wait()
		if len(errs) > 0 {
			errCh <- fmt.Errorf("%d handler(s) failed for event '%s'", len(errs), event)
		}
	}()

	return errCh
}

// OnTyped registers a typed hook handler for a specific event.
// Handlers are sorted by priority after registration.
func (r *HookRegistry) OnTyped(event HookEvent, handler TypedHookHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.typedHandlers == nil {
		r.typedHandlers = make(map[HookEvent][]TypedHookHandler)
	}

	r.typedHandlers[event] = append(r.typedHandlers[event], handler)

	// Sort by priority (ascending)
	handlers := r.typedHandlers[event]
	for i := len(handlers) - 1; i > 0; i-- {
		if handlers[i].Priority < handlers[i-1].Priority {
			handlers[i], handlers[i-1] = handlers[i-1], handlers[i]
		}
	}

	log.Info().Msgf("[Hook Registry] Registered typed handler '%s' for event '%s' (priority: %d)", handler.Name, event, handler.Priority)
}

// RemoveHandler removes all handlers with a given name from an event.
// Useful for plugin uninstallation.
func (r *HookRegistry) RemoveHandler(event HookEvent, handlerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove from untyped handlers
	if handlers, ok := r.handlers[event]; ok {
		filtered := make([]HookHandler, 0, len(handlers))
		for _, h := range handlers {
			if h.Name != handlerName {
				filtered = append(filtered, h)
			}
		}
		r.handlers[event] = filtered
	}

	// Remove from typed handlers
	if handlers, ok := r.typedHandlers[event]; ok {
		filtered := make([]TypedHookHandler, 0, len(handlers))
		for _, h := range handlers {
			if h.Name != handlerName {
				filtered = append(filtered, h)
			}
		}
		r.typedHandlers[event] = filtered
	}
}

// RemoveAllHandlers removes all handlers for a given plugin name across all events.
// Used during plugin uninstallation cleanup.
func (r *HookRegistry) RemoveAllHandlers(pluginName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for event, handlers := range r.handlers {
		filtered := make([]HookHandler, 0, len(handlers))
		for _, h := range handlers {
			if h.Name != pluginName {
				filtered = append(filtered, h)
			}
		}
		r.handlers[event] = filtered
	}

	for event, handlers := range r.typedHandlers {
		filtered := make([]TypedHookHandler, 0, len(handlers))
		for _, h := range handlers {
			if h.Name != pluginName {
				filtered = append(filtered, h)
			}
		}
		r.typedHandlers[event] = filtered
	}

	log.Info().Msgf("[Hook Registry] Removed all handlers for plugin '%s'", pluginName)
}

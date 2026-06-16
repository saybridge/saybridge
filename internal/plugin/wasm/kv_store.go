package wasm

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	// MaxKVStoragePerApp is the maximum storage size per app (10MB).
	MaxKVStoragePerApp = 10 * 1024 * 1024
)

// kvEntry stores a key-value pair with optional expiration.
type kvEntry struct {
	Value     json.RawMessage
	ExpiresAt *time.Time
}

// KVStore provides per-app key-value storage (in-memory).
// TODO: back with DB for persistence.
type KVStore struct {
	mu    sync.RWMutex
	store map[string]map[string]*kvEntry // appSlug → key → entry
}

// NewKVStore creates a new in-memory KV store.
func NewKVStore() *KVStore {
	return &KVStore{
		store: make(map[string]map[string]*kvEntry),
	}
}

// Get retrieves a value by key.
func (kv *KVStore) Get(appSlug, key string) (json.RawMessage, error) {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	appStore, ok := kv.store[appSlug]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found", key)
	}

	entry, ok := appStore[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found", key)
	}

	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		return nil, fmt.Errorf("key '%s' expired", key)
	}

	return entry.Value, nil
}

// Set stores a key-value pair with an optional TTL.
func (kv *KVStore) Set(appSlug, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if _, ok := kv.store[appSlug]; !ok {
		kv.store[appSlug] = make(map[string]*kvEntry)
	}

	// Check storage limit
	totalSize := int64(0)
	for _, e := range kv.store[appSlug] {
		totalSize += int64(len(e.Value))
	}
	if totalSize+int64(len(data)) > MaxKVStoragePerApp {
		return fmt.Errorf("storage limit exceeded for app '%s'", appSlug)
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}

	kv.store[appSlug][key] = &kvEntry{Value: data, ExpiresAt: expiresAt}
	return nil
}

// Delete removes a key.
func (kv *KVStore) Delete(appSlug, key string) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if appStore, ok := kv.store[appSlug]; ok {
		delete(appStore, key)
	}
	return nil
}

// ListKeys returns all non-expired keys for an app.
func (kv *KVStore) ListKeys(appSlug string) ([]string, error) {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	appStore, ok := kv.store[appSlug]
	if !ok {
		return nil, nil
	}

	now := time.Now()
	var keys []string
	for k, e := range appStore {
		if e.ExpiresAt == nil || now.Before(*e.ExpiresAt) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

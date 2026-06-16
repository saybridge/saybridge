// Package wasm manages marketplace app lifecycle.
// This file provides filesystem-based plugin discovery via manifest.json scanning.
package wasm

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PluginManifest represents a marketplace plugin's manifest.json.
// Loaded from disk at startup — no DB or Go code needed.
type PluginManifest struct {
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	ShortDescription string   `json:"short_description"`
	Description      string   `json:"description"`
	Icon             string   `json:"icon"`
	Category         string   `json:"category"`
	Tags             []string `json:"tags"`
	Developer        string   `json:"developer"`
	IsOfficial       bool     `json:"is_official"`
	IsFeatured       bool     `json:"is_featured"`
	Permissions      []string `json:"permissions"`
	Hooks            []string `json:"hooks"`
	WasmFile         string   `json:"wasm_file"`
	Exports          []string `json:"exports"`

	// Internal — set by scanner, not from JSON
	Dir      string `json:"-"` // absolute path to plugin directory
	WasmPath string `json:"-"` // absolute path to .wasm file
}

// PluginRegistry discovers and holds all known marketplace plugins from disk.
var Registry = &PluginRegistryStore{
	entries: make(map[string]*PluginManifest),
}

// PluginRegistryStore holds all scanned plugin manifests.
type PluginRegistryStore struct {
	mu      sync.RWMutex
	entries map[string]*PluginManifest // slug → manifest
}

// ScanPluginsDir scans a directory for plugin subdirectories containing manifest.json.
// Each valid subdirectory becomes a registered marketplace plugin.
func (r *PluginRegistryStore) ScanPluginsDir(baseDir string) error {
	absDir, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		log.Printf("[AppRuntime] Warning: cannot read plugins directory '%s': %v", absDir, err)
		return nil // non-fatal: no plugins directory is ok
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(absDir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "manifest.json")

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			// No manifest.json — skip silently (might be old Go plugin dir)
			continue
		}

		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			log.Printf("[AppRuntime] Warning: invalid manifest.json in '%s': %v", pluginDir, err)
			continue
		}

		if manifest.Slug == "" {
			log.Printf("[AppRuntime] Warning: manifest.json in '%s' has no slug, skipping", pluginDir)
			continue
		}

		// Set internal paths
		manifest.Dir = pluginDir
		if manifest.WasmFile != "" {
			manifest.WasmPath = filepath.Join(pluginDir, manifest.WasmFile)
		}

		r.entries[manifest.Slug] = &manifest
		loaded++
		log.Printf("[AppRuntime] Discovered plugin: %s v%s (%s)", manifest.Name, manifest.Version, manifest.Slug)
	}

	log.Printf("[AppRuntime] Scanned '%s': found %d plugins", absDir, loaded)
	return nil
}

// RegisterFromManifest registers a plugin from a ManifestJSON struct (used by scanner.go).
func (r *PluginRegistryStore) RegisterFromManifest(mj *ManifestJSON, dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	absDir, _ := filepath.Abs(dir)
	manifest := &PluginManifest{
		Slug:             mj.Slug,
		Name:             mj.Name,
		Version:          mj.Version,
		ShortDescription: mj.ShortDescription,
		Description:      mj.Description,
		Icon:             mj.Icon,
		Category:         mj.Category,
		Tags:             mj.Tags,
		Developer:        mj.Developer,
		IsOfficial:       mj.IsOfficial,
		IsFeatured:       mj.IsFeatured,
		Permissions:      mj.Permissions,
		Hooks:            mj.Hooks,
		WasmFile:         mj.WasmFile,
		Exports:          mj.Exports,
		Dir:              absDir,
	}
	if mj.WasmFile != "" {
		manifest.WasmPath = filepath.Join(absDir, mj.WasmFile)
	}
	r.entries[mj.Slug] = manifest
}

// AllApps returns all registered marketplace app manifests.
func (r *PluginRegistryStore) AllApps() []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*PluginManifest, 0, len(r.entries))
	for _, m := range r.entries {
		result = append(result, m)
	}
	return result
}

// GetBySlug returns a single app by slug.
func (r *PluginRegistryStore) GetBySlug(slug string) *PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[slug]
}

// Featured returns featured apps up to limit.
func (r *PluginRegistryStore) Featured(limit int) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*PluginManifest
	for _, m := range r.entries {
		if m.IsFeatured {
			result = append(result, m)
			if len(result) >= limit {
				break
			}
		}
	}
	return result
}

// FilterByCategory returns apps matching a category.
func (r *PluginRegistryStore) FilterByCategory(category string) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*PluginManifest
	for _, m := range r.entries {
		if m.Category == category {
			result = append(result, m)
		}
	}
	return result
}

// Search finds apps by name/description/tags.
func (r *PluginRegistryStore) Search(query string, limit int) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q := strings.ToLower(query)
	var result []*PluginManifest
	for _, m := range r.entries {
		if strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(strings.ToLower(m.ShortDescription), q) ||
			containsTag(m.Tags, q) {
			result = append(result, m)
			if len(result) >= limit {
				break
			}
		}
	}
	return result
}

// Count returns total registered plugins.
func (r *PluginRegistryStore) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// HasWasm checks if a plugin has a WASM file available on disk.
func (r *PluginRegistryStore) HasWasm(slug string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.entries[slug]
	if !ok || m.WasmPath == "" {
		return false
	}
	_, err := os.Stat(m.WasmPath)
	return err == nil
}

func containsTag(tags []string, query string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

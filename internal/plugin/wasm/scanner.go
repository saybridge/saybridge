package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
)

// ManifestJSON represents the manifest.json file on disk.
// Extended with optional admin_menu and admin_screens for SDUI.
type ManifestJSON struct {
	Slug             string            `json:"slug"`
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	ShortDescription string            `json:"short_description"`
	Description      string            `json:"description"`
	Icon             string            `json:"icon"`
	Category         string            `json:"category"`
	Tags             []string          `json:"tags"`
	Developer        string            `json:"developer"`
	IsOfficial       bool              `json:"is_official"`
	IsFeatured       bool              `json:"is_featured"`
	License          string            `json:"license"`
	Permissions      []string          `json:"permissions"`
	Hooks            []string          `json:"hooks"`
	WasmFile         string            `json:"wasm_file"`
	Exports          []string          `json:"exports"`
	AdminMenu        *AdminMenuJSON    `json:"admin_menu,omitempty"`
	AdminScreens     []AdminScreenJSON `json:"admin_screens,omitempty"`
	ComposerExtensions []ComposerExtensionJSON `json:"composer_extensions,omitempty"`
	UIExtensions     *plugin.UIExtensions  `json:"ui_extensions,omitempty"`
}

// AdminMenuJSON mirrors plugin.AdminMenu for JSON loading.
type AdminMenuJSON struct {
	Label    string `json:"label"`
	Icon     string `json:"icon"`
	Priority int    `json:"priority"`
}

// AdminScreenJSON represents a UI screen in manifest.json.
type AdminScreenJSON struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	Components []map[string]interface{} `json:"components,omitempty"`
	IframeSrc  string                   `json:"iframe_src,omitempty"`
}

// ComposerExtensionJSON represents a composer extension in manifest.json.
type ComposerExtensionJSON struct {
	Autocomplete *struct {
		Trigger  string `json:"trigger"`
		MinChars int    `json:"min_chars"`
		Endpoint string `json:"endpoint"`
	} `json:"autocomplete,omitempty"`
	ActionButton *struct {
		Icon     string `json:"icon"`
		Label    string `json:"label"`
		Shortcut string `json:"shortcut,omitempty"`
	} `json:"action_button,omitempty"`
	PickerPanel *struct {
		Endpoint   string `json:"endpoint"`
		RenderType string `json:"render_type"`
	} `json:"picker_panel,omitempty"`
}

// ScanAndRegister scans the given directory for plugin subdirectories,
// reads each manifest.json, and registers the plugin with the manifest handler.
// This replaces the old Go init()-based plugin registration.
func ScanAndRegister(pluginsDir string, deps *plugin.Dependencies, manifestHandler *plugin.ManifestHandler) {
	// Initialize HostAPI and wire callbacks
	hostAPI := NewHostAPI()
	hostAPI.OnSendMessage = func(ctx context.Context, roomID, content, senderName string) error {
		if deps.SendMessageFn != nil {
			return deps.SendMessageFn(ctx, roomID, content, senderName)
		}
		return nil
	}
	hostAPI.OnKVGet = func(ctx context.Context, appID, key string) (string, error) {
		redisKey := fmt.Sprintf("plugin_kv:%s:%s", appID, key)
		return deps.RDB.Get(ctx, redisKey).Result()
	}
	hostAPI.OnKVSet = func(ctx context.Context, appID, key, value string) error {
		redisKey := fmt.Sprintf("plugin_kv:%s:%s", appID, key)
		return deps.RDB.Set(ctx, redisKey, value, 0).Err()
	}

	hostAPI.OnDeleteMessage = func(ctx context.Context, roomID, messageID string) error {
		if deps.DeleteMessageFn != nil {
			// TimescaleDB doesn't need timeBucket — pass 0
			return deps.DeleteMessageFn(ctx, roomID, 0, messageID)
		}
		return nil
	}

	hostAPI.OnEditMessage = func(ctx context.Context, roomID, messageID, newContent string) error {
		if deps.EditMessageFn != nil {
			return deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, newContent)
		}
		return nil
	}

	hostAPI.OnCreateRoom = func(ctx context.Context, name, roomType string) (string, error) {
		if deps.CreateRoomFn != nil {
			return deps.CreateRoomFn(ctx, name, roomType)
		}
		return "", nil
	}

	hostAPI.OnGetUser = func(ctx context.Context, userID string) (string, error) {
		if deps.GetUserFn != nil {
			return deps.GetUserFn(ctx, userID)
		}
		return "", nil
	}

	hostAPI.OnGetRoomMembers = func(ctx context.Context, roomID string) (string, error) {
		if deps.GetRoomMembersFn != nil {
			return deps.GetRoomMembersFn(ctx, roomID)
		}
		return "", nil
	}

	hostAPI.OnAddReaction = func(ctx context.Context, roomID, messageID, emoji string) error {
		if deps.AddReactionFn != nil {
			return deps.AddReactionFn(ctx, roomID, messageID, emoji)
		}
		return nil
	}

	hostAPI.OnRegisterSlashCommand = func(ctx context.Context, command, description string) error {
		if deps.RegisterSlashCommandFn != nil {
			return deps.RegisterSlashCommandFn(ctx, command, description)
		}
		return nil
	}

	hostAPI.OnScheduleTimer = func(ctx context.Context, delayMs int32, hookEvent string, payload string) error {
		if deps.ScheduleTimerFn != nil {
			return deps.ScheduleTimerFn(ctx, delayMs, hookEvent, payload)
		}
		return nil
	}

	hostAPI.OnRegisterRoomType = func(ctx context.Context, typeJSON string) error {
		var roomTypeDef domain.RoomTypeDefinition
		if err := json.Unmarshal([]byte(typeJSON), &roomTypeDef); err != nil {
			return err
		}
		roomTypeDef.IsPlugin = true
		domain.RegisterRoomType(roomTypeDef)
		return nil
	}
	hostAPI.OnHTTPRequest = func(ctx context.Context, appID, method, url, body string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return string(respBytes), nil
	}
	hostAPI.OnGetRoomHistory = func(ctx context.Context, roomID string) (string, error) {
		if deps.GetRoomHistoryFn != nil {
			return deps.GetRoomHistoryFn(ctx, roomID)
		}
		return "", nil
	}
	hostAPI.OnPublishWSEvent = func(ctx context.Context, roomID, eventName, data string) error {
		if deps.PublishWSEventFn != nil {
			return deps.PublishWSEventFn(ctx, roomID, eventName, data)
		}
		return nil
	}
	hostAPI.OnGetUserStatus = func(ctx context.Context, userID string) (string, error) {
		var user struct {
			Presence     string
			CustomStatus string
		}
		err := deps.DB.WithContext(ctx).Table("users").Select("presence, custom_status").Where("id = ?", userID).Scan(&user).Error
		if err != nil {
			return "", err
		}

		var settings struct {
			NotificationsEnabled bool
		}
		err = deps.DB.WithContext(ctx).Table("user_settings").Select("notifications_enabled").Where("user_id = ?", userID).Scan(&settings).Error
		if err != nil {
			// If settings don't exist, default to true
			settings.NotificationsEnabled = true
		}

		resMap := map[string]interface{}{
			"presence":              user.Presence,
			"custom_status":         user.CustomStatus,
			"notifications_enabled": settings.NotificationsEnabled,
		}
		resBytes, _ := json.Marshal(resMap)
		return string(resBytes), nil
	}

	// Initialize WasmRuntime
	wasmRT, err := NewWasmRuntime(hostAPI)
	if err != nil {
		log.Printf("[AppRuntime] Error creating WASM runtime: %v", err)
		return
	}

	// Initialize Engine
	engine := NewEngine(deps.Hooks, wasmRT, func(ctx context.Context, roomID string, timeBucket int, messageID string) error {
		if deps.DeleteMessageFn != nil {
			return deps.DeleteMessageFn(ctx, roomID, timeBucket, messageID)
		}
		return nil
	})

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		// Fallback to "backend/plugins" if running from the monorepo root directory
		fallbackDir := filepath.Join("backend", pluginsDir)
		if entries2, err2 := os.ReadDir(fallbackDir); err2 == nil {
			pluginsDir = fallbackDir
			entries = entries2
		} else {
			log.Printf("[AppRuntime] Warning: cannot read plugins dir %s: %v", pluginsDir, err)
			return
		}
	}

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip scaffold/template directories (e.g. "_template") — they are
		// developer examples, not installable plugins.
		if strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		manifestPath := filepath.Join(pluginsDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // Not a plugin directory
		}

		var mj ManifestJSON
		if err := json.Unmarshal(data, &mj); err != nil {
			log.Printf("[AppRuntime] Warning: invalid manifest in %s: %v", entry.Name(), err)
			continue
		}

		// Enterprise license gate
		if mj.License == "enterprise" && !deps.Cfg.EnterpriseEnabled {
			log.Printf("[AppRuntime] ⏭ Skipping enterprise plugin: %s (ENABLE_ENTERPRISE=false)", mj.Name)
			continue
		}

		// Convert manifest.json to plugin.UIManifest for SDUI
		uiManifest := convertToUIManifest(&mj)

		// Register manifest for SDUI
		manifestHandler.RegisterManifest(uiManifest)

		// Also register in the WASM registry for runtime execution
		Registry.RegisterFromManifest(&mj, filepath.Join(pluginsDir, entry.Name()))

		// Install WASM app at runtime startup so hooks get registered
		if err := engine.InstallWasmApp(context.Background(), mj.Slug); err != nil {
			log.Printf("[AppRuntime] Error installing WASM app '%s': %v", mj.Slug, err)
		}

		// Register marketplace's before_uninstall guard
		if mj.Slug == "marketplace" {
			registerMarketplaceUninstallGuard(deps.Hooks)
		}

		// custom-emoji routes are now registered via plugins/custom-emoji init()

		adminInfo := ""
		if uiManifest.AdminMenu != nil {
			adminInfo = " + admin tab: " + uiManifest.AdminMenu.Label
		}
		screenCount := len(uiManifest.AdminScreens)
		log.Printf("[AppRuntime] ✓ %s v%s [%s] loaded — %d UI screens%s",
			mj.Name, mj.Version, mj.Category, screenCount, adminInfo)
		loaded++
	}

	log.Printf("[AppRuntime] Loaded %d plugins from %s/", loaded, pluginsDir)

	// Start PluginWatcher to hot-reload plugin changes on disk
	watcher, err := NewPluginWatcher(pluginsDir, func(eventType string, manifest *PluginManifest) {
		log.Printf("[AppRuntime] Hot-reload triggered for plugin '%s' (%s)", manifest.Slug, eventType)

		// Reload manifest.json for UI extensions
		manifestPath := filepath.Join(manifest.Dir, "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err == nil {
			var mj ManifestJSON
			if err := json.Unmarshal(data, &mj); err == nil {
				uiManifest := convertToUIManifest(&mj)
				manifestHandler.RegisterManifest(uiManifest)
			}
		}

		if engine.IsAppLoaded(manifest.Slug) {
			_ = engine.UninstallWasmApp(context.Background(), manifest.Slug)
		}

		if err := engine.InstallWasmApp(context.Background(), manifest.Slug); err != nil {
			log.Printf("[AppRuntime] Error installing WASM app during hot-reload '%s': %v", manifest.Slug, err)
		}
	})
	if err == nil {
		if err := watcher.Start(); err != nil {
			log.Printf("[AppRuntime] Error starting plugin watcher: %v", err)
		}
	} else {
		log.Printf("[AppRuntime] Error creating plugin watcher: %v", err)
	}
}

// convertToUIManifest converts a ManifestJSON to plugin.UIManifest for SDUI.
// registerMarketplaceUninstallGuard adds a before_uninstall hook that prevents
// removing the marketplace plugin while any marketplace-downloaded plugins are active.
func registerMarketplaceUninstallGuard(hooks *plugin.HookRegistry) {
	hooks.On(plugin.BeforeUninstallPlugin, plugin.HookHandler{
		Name:     "marketplace:guard_uninstall",
		Priority: 10,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			slug, _ := payload["slug"].(string)
			if slug != "marketplace" {
				return nil, nil // Not about marketplace — skip
			}

			// Check if any non-core plugins from marketplace are still active
			allPlugins := Registry.AllApps()
			var activeFromMarketplace []string

			for _, p := range allPlugins {
				if p.Slug == "marketplace" {
					continue
				}
				// Marketplace-sourced = has "marketplace" in tags or is_official=false
				isFromMarketplace := false
				for _, tag := range p.Tags {
					if strings.EqualFold(tag, "marketplace") {
						isFromMarketplace = true
						break
					}
				}
				// Also consider non-official plugins as marketplace-sourced
				if !p.IsOfficial {
					isFromMarketplace = true
				}
				if isFromMarketplace {
					activeFromMarketplace = append(activeFromMarketplace, p.Name)
				}
			}

			if len(activeFromMarketplace) > 0 {
				names := strings.Join(activeFromMarketplace, ", ")
				log.Printf("[Marketplace] Blocked uninstall: %d active plugins from marketplace: %s",
					len(activeFromMarketplace), names)
				return nil, fmt.Errorf(
					"cannot uninstall Marketplace while %d plugins are still active: %s. Please uninstall them first",
					len(activeFromMarketplace), names)
			}

			return nil, nil // OK to uninstall
		},
	})
	log.Printf("[Marketplace] Registered before_uninstall guard")
}

func convertToUIManifest(mj *ManifestJSON) *plugin.UIManifest {
	um := &plugin.UIManifest{
		ID:          mj.Slug,
		Name:        mj.Name,
		Icon:        mj.Icon,
		Version:     mj.Version,
		Category:    mj.Category,
		Description: mj.ShortDescription,
		Developer:   mj.Developer,
		License:     mj.License,
	}

	if mj.AdminMenu != nil {
		um.AdminMenu = &plugin.AdminMenu{
			Label:    mj.AdminMenu.Label,
			Icon:     mj.AdminMenu.Icon,
			Priority: mj.AdminMenu.Priority,
		}
	}

	for _, screen := range mj.AdminScreens {
		uiScreen := plugin.UIScreen{
			ID:        screen.ID,
			Title:     screen.Title,
			Placement: "admin",
			IframeSrc: screen.IframeSrc,
		}
		for _, comp := range screen.Components {
			compType, _ := comp["type"].(string)
			props, _ := comp["props"].(map[string]interface{})
			if compType != "" {
				uiScreen.Components = append(uiScreen.Components, plugin.UIComponent{
					Type:  compType,
					Props: props,
				})
			}
		}
		um.AdminScreens = append(um.AdminScreens, uiScreen)
	}

	// Convert composer extensions
	for _, ext := range mj.ComposerExtensions {
		ce := plugin.ComposerExtension{}
		if ext.Autocomplete != nil {
			ce.Autocomplete = &plugin.ComposerAutocomplete{
				Trigger:  ext.Autocomplete.Trigger,
				MinChars: ext.Autocomplete.MinChars,
				Endpoint: ext.Autocomplete.Endpoint,
			}
		}
		if ext.ActionButton != nil {
			ce.ActionButton = &plugin.ComposerActionButton{
				Icon:     ext.ActionButton.Icon,
				Label:    ext.ActionButton.Label,
				Shortcut: ext.ActionButton.Shortcut,
			}
		}
		if ext.PickerPanel != nil {
			ce.PickerPanel = &plugin.ComposerPickerPanel{
				Endpoint:   ext.PickerPanel.Endpoint,
				RenderType: ext.PickerPanel.RenderType,
			}
		}
		um.ComposerExtensions = append(um.ComposerExtensions, ce)
	}

	if mj.UIExtensions != nil {
		um.UIExtensions = mj.UIExtensions
	}

	return um
}

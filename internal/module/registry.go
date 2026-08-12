package module

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"maps6/internal/config"
)

// Registry manages the lifecycle of all system modules.
type Registry struct {
	modules    map[string]*managedModule
	mu         sync.RWMutex
	cfg        *config.Config
	configPath string
}

type managedModule struct {
	mod     Module
	enabled bool
	cancel  context.CancelFunc
}

// NewRegistry creates a new module registry.
func NewRegistry(cfg *config.Config, configPath string) *Registry {
	return &Registry{
		modules:    make(map[string]*managedModule),
		cfg:        cfg,
		configPath: configPath,
	}
}

// Register adds a new module to the registry and checks config for its enabled state.
func (r *Registry) Register(name string, mod Module) {
	r.mu.Lock()
	defer r.mu.Unlock()

	enabled := r.isModuleEnabled(name)
	r.modules[name] = &managedModule{
		mod:     mod,
		enabled: enabled,
		cancel:  nil,
	}
	slog.Info("Module registered", "name", name, "enabled", enabled)
}

// isModuleEnabled checks the config to see if a module is enabled by default.
func (r *Registry) isModuleEnabled(name string) bool {
	// Use reflection or a switch to check the config
	switch name {
	case "wifi", "WiFi":
		return r.cfg.Modules.WiFi
	case "lass", "LASS":
		return r.cfg.Modules.LASS
	case "mqtt", "MQTT":
		return r.cfg.Modules.MQTT
	case "oled", "OLED":
		return r.cfg.Modules.OLED
	case "storage_local", "StorageLocal":
		return r.cfg.Modules.StorageLocal
	case "storage_ext", "StorageExt":
		return r.cfg.Modules.StorageExt
	case "ota", "OTA":
		return r.cfg.Modules.OTA
	case "lte", "LTE":
		return r.cfg.Modules.LTE
	case "gps", "GPS":
		return r.cfg.Modules.GPS
	default:
		return false // Default disabled if unknown
	}
}

// updateConfigEnabledState updates the internal config struct boolean for the module.
func (r *Registry) updateConfigEnabledState(name string, enabled bool) {
	switch name {
	case "wifi", "WiFi":
		r.cfg.Modules.WiFi = enabled
	case "lass", "LASS":
		r.cfg.Modules.LASS = enabled
	case "mqtt", "MQTT":
		r.cfg.Modules.MQTT = enabled
	case "oled", "OLED":
		r.cfg.Modules.OLED = enabled
	case "storage_local", "StorageLocal":
		r.cfg.Modules.StorageLocal = enabled
	case "storage_ext", "StorageExt":
		r.cfg.Modules.StorageExt = enabled
	case "ota", "OTA":
		r.cfg.Modules.OTA = enabled
	case "lte", "LTE":
		r.cfg.Modules.LTE = enabled
	case "gps", "GPS":
		r.cfg.Modules.GPS = enabled
	}
}

// StartEnabled starts all modules that are currently marked as enabled.
func (r *Registry) StartEnabled(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, m := range r.modules {
		if m.enabled && m.cancel == nil {
			r.startModule(ctx, name, m)
		}
	}
}

// startModule is an internal helper that starts a module. Must be called with lock held.
func (r *Registry) startModule(parentCtx context.Context, name string, m *managedModule) {
	ctx, cancel := context.WithCancel(parentCtx)
	m.cancel = cancel

	go func() {
		if err := m.mod.Start(ctx); err != nil {
			slog.Error("Module stopped with error", "name", name, "error", err)
		} else {
			slog.Info("Module stopped cleanly", "name", name)
		}
	}()
	slog.Info("Module started", "name", name)
}

// Enable marks a module as enabled, starts it, and saves the config.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.modules[name]
	if !ok {
		return fmt.Errorf("module not found: %s", name)
	}

	if m.enabled {
		return nil // Already enabled
	}

	m.enabled = true
	r.updateConfigEnabledState(name, true)
	if err := r.cfg.Save(r.configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if m.cancel == nil {
		r.startModule(context.Background(), name, m)
	}

	return nil
}

// Disable marks a module as disabled, stops it, and saves the config.
func (r *Registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.modules[name]
	if !ok {
		return fmt.Errorf("module not found: %s", name)
	}

	if !m.enabled {
		return nil // Already disabled
	}

	m.enabled = false
	r.updateConfigEnabledState(name, false)
	if err := r.cfg.Save(r.configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if m.cancel != nil {
		m.cancel()
		_ = m.mod.Stop()
		m.cancel = nil
		slog.Info("Module disabled and stopped", "name", name)
	}

	return nil
}

// StopAll stops all currently running modules.
func (r *Registry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, m := range r.modules {
		if m.cancel != nil {
			m.cancel()
			_ = m.mod.Stop()
			m.cancel = nil
			slog.Info("Module stopped", "name", name)
		}
	}
}

// StatusAll returns the status of all registered modules.
func (r *Registry) StatusAll() []ModuleStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var statuses []ModuleStatus
	for name, m := range r.modules {
		status := m.mod.Status()
		status.Name = name
		status.Enabled = m.enabled
		status.Running = m.cancel != nil
		statuses = append(statuses, status)
	}
	return statuses
}

// Get retrieves a module by name.
func (r *Registry) Get(name string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m, ok := r.modules[name]
	if !ok {
		return nil, false
	}
	return m.mod, true
}

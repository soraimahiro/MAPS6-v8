package storage

import (
	"context"
	"fmt"
	"log/slog"
	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"os"
	"sync"
	"time"
)

// ExternalModule handles external CSV storage of sensor data (e.g. SD card).
type ExternalModule struct {
	cfg      *config.Config
	bus      *bus.SensorBus
	deviceID string
	writer   *CSVWriter

	subName string
	ch      <-chan mcu.SensorData

	mu        sync.RWMutex
	running   bool
	lastError error
}

// NewExternalModule creates a new ExternalModule.
func NewExternalModule(cfg *config.Config, bus *bus.SensorBus, deviceID string) *ExternalModule {
	return &ExternalModule{
		cfg:      cfg,
		bus:      bus,
		deviceID: deviceID,
		writer:   NewCSVWriter(cfg.Storage.External.Path),
		subName:  "storage_ext",
	}
}

// Name returns the module name.
func (m *ExternalModule) Name() string {
	return "storage_ext"
}

// Start subscribes to the sensor bus and starts the writing loop.
func (m *ExternalModule) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.ch = m.bus.Subscribe(m.subName, 10)
	m.running = true
	m.lastError = nil
	m.mu.Unlock()

	go m.run(ctx)
	return nil
}

// Stop unsubscribes from the bus and marks the module as stopped.
func (m *ExternalModule) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}
	m.bus.Unsubscribe(m.subName)
	m.running = false
	return nil
}

// Status returns the module's running status.
func (m *ExternalModule) Status() module.ModuleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errStr string
	if m.lastError != nil {
		errStr = m.lastError.Error()
	}

	return module.ModuleStatus{
		Name:      m.Name(),
		Running:   m.running,
		LastError: errStr,
	}
}

func (m *ExternalModule) run(ctx context.Context) {
	interval := time.Duration(m.cfg.Storage.External.Interval)
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Stop()
			return
		case <-ticker.C:
			// Check if external path is accessible
			if _, err := os.Stat(m.cfg.Storage.External.Path); os.IsNotExist(err) {
				slog.Warn("External storage path is not accessible", "path", m.cfg.Storage.External.Path)
				m.mu.Lock()
				m.lastError = fmt.Errorf("external storage path %s not accessible", m.cfg.Storage.External.Path)
				m.mu.Unlock()
				continue
			}

			data := m.bus.Latest()
			err := m.writer.WriteRecord(m.deviceID, data, 0.0, 0.0)
			
			m.mu.Lock()
			m.lastError = err
			m.mu.Unlock()
		}
	}
}

package storage

import (
	"context"
	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"sync"
	"time"
)

// LocalModule handles local CSV storage of sensor data.
type LocalModule struct {
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

// NewLocalModule creates a new LocalModule.
func NewLocalModule(cfg *config.Config, bus *bus.SensorBus, deviceID string) *LocalModule {
	return &LocalModule{
		cfg:      cfg,
		bus:      bus,
		deviceID: deviceID,
		writer:   NewCSVWriter(cfg.Storage.Local.Path),
		subName:  "storage_local",
	}
}

// Name returns the module name.
func (m *LocalModule) Name() string {
	return "storage_local"
}

// Start subscribes to the sensor bus and starts the writing loop.
func (m *LocalModule) Start(ctx context.Context) error {
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
func (m *LocalModule) Stop() error {
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
func (m *LocalModule) Status() module.ModuleStatus {
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

func (m *LocalModule) run(ctx context.Context) {
	interval := time.Duration(m.cfg.Storage.Local.Interval)
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
			data := m.bus.Latest()
			err := m.writer.WriteRecord(m.deviceID, data, 0.0, 0.0)
			
			m.mu.Lock()
			m.lastError = err
			m.mu.Unlock()
		}
	}
}

package display

import (
	"context"
	"log/slog"
	"time"

	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/input"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"maps6/internal/network"
)

type noopModule struct{}

func (n *noopModule) Name() string { return "oled" }
func (n *noopModule) Start(ctx context.Context) error { return nil }
func (n *noopModule) Stop() error { return nil }
func (n *noopModule) Status() module.ModuleStatus {
	return module.ModuleStatus{
		Name:    n.Name(),
		Enabled: true,
		Running: false,
	}
}

func NewModule(
	cfg *config.Config,
	bus *bus.SensorBus,
	registry *module.Registry,
	networkMgr *network.Manager,
	mega *mcu.Mega2560,
	deviceID, version string,
) module.Module {
	fontPath := "/home/pi/maps6/fonts/NotoSans-Regular.ttf"
	oled, err := NewOLEDDisplay(fontPath)
	if err != nil {
		slog.Error("Failed to initialize OLED display, using no-op stub", "error", err)
		return &noopModule{}
	}

	keyboard := input.NewKeyboardReader()

	timeout := 30 * time.Second
	if cfg != nil && time.Duration(cfg.Display.MenuTimeout) > 0 {
		timeout = time.Duration(cfg.Display.MenuTimeout)
	}

	return NewMenuController(oled, keyboard, bus, registry, networkMgr, mega, deviceID, version, timeout)
}

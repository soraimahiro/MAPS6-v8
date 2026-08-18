package display

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"maps6/internal/bus"
	"maps6/internal/input"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"maps6/internal/network"
)

type MenuState int

const (
	StateStatus MenuState = iota
	StateMainMenu
	StateSensor
	StateModules
	StateWiFi
	StateMaintenance
	StateSystemInfo
	StateOTA
	StateConfirm
)

var mainMenuItems = []string{
	"Sensor Data",
	"Module Control",
	"WiFi Setup",
	"Maintenance",
	"System Info",
	"OTA Update",
}

type MenuController struct {
	oled         *OLEDDisplay
	keyboard     *input.KeyboardReader
	bus          *bus.SensorBus
	registry     *module.Registry
	networkMgr   *network.Manager
	mega         *mcu.Mega2560
	state        MenuState
	cursor       int
	timeout      time.Duration
	confirmAction func() error
	deviceID     string
	version      string
	logger       *slog.Logger
	running      bool
	mu           sync.Mutex
	done         chan struct{}
}

func NewMenuController(
	oled *OLEDDisplay,
	keyboard *input.KeyboardReader,
	bus *bus.SensorBus,
	registry *module.Registry,
	networkMgr *network.Manager,
	mega *mcu.Mega2560,
	deviceID, version string,
	timeout time.Duration,
) *MenuController {
	return &MenuController{
		oled:       oled,
		keyboard:   keyboard,
		bus:        bus,
		registry:   registry,
		networkMgr: networkMgr,
		mega:       mega,
		deviceID:   deviceID,
		version:    version,
		timeout:    timeout,
		logger:     slog.Default().With("component", "menu"),
	}
}

func (m *MenuController) Name() string {
	return "oled"
}

func (m *MenuController) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.done = make(chan struct{})
	m.mu.Unlock()

	if err := m.keyboard.Start(); err != nil {
		m.logger.Warn("Failed to start keyboard", "error", err)
	}

	go m.run(ctx)
	return nil
}

func (m *MenuController) run(ctx context.Context) {
	defer m.keyboard.Stop()
	if m.oled != nil {
		defer m.oled.Close()
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	idleTimer := time.NewTimer(m.timeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		case ev := <-m.keyboard.Events():
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(m.timeout)
			m.handleKey(ev)
		case <-ticker.C:
			if m.state == StateStatus {
				m.renderStatus()
			}
		case <-idleTimer.C:
			m.mu.Lock()
			m.state = StateStatus
			m.mu.Unlock()
			m.renderStatus()
		}
	}
}

func (m *MenuController) handleKey(ev input.KeyEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state {
	case StateStatus:
		m.state = StateMainMenu
		m.cursor = 0
		m.renderMainMenu()
	case StateMainMenu:
		switch ev {
		case input.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case input.KeyDown:
			if m.cursor < len(mainMenuItems)-1 {
				m.cursor++
			}
		case input.KeyEnter:
			m.executeMainMenu()
		case input.KeyEsc:
			m.state = StateStatus
			m.renderStatus()
		}
		if m.state == StateMainMenu {
			m.renderMainMenu()
		}
	case StateMaintenance:
		switch ev {
		case input.KeyEnter:
			m.state = StateConfirm
			m.confirmAction = func() error {
				m.logger.Info("Executing maintenance action")
				return nil
			}
			m.oled.RenderConfirm("Execute Action?")
		case input.KeyEsc:
			m.state = StateMainMenu
			m.renderMainMenu()
		}
	case StateConfirm:
		switch ev {
		case input.KeyEnter:
			if m.confirmAction != nil {
				_ = m.confirmAction()
			}
			m.state = StateMainMenu
			m.renderMainMenu()
		case input.KeyEsc:
			m.state = StateMainMenu
			m.renderMainMenu()
		}
	case StateSystemInfo, StateSensor, StateModules, StateWiFi:
		if ev == input.KeyEsc {
			m.state = StateMainMenu
			m.renderMainMenu()
		}
	default:
		if ev == input.KeyEsc {
			m.state = StateMainMenu
			m.renderMainMenu()
		}
	}
}

func (m *MenuController) executeMainMenu() {
	switch m.cursor {
	case 0:
		m.state = StateSensor
		m.oled.RenderText("Sensors", []string{"All sensors OK"})
	case 1:
		m.state = StateModules
		m.oled.RenderText("Modules", []string{"All modules OK"})
	case 2:
		m.state = StateWiFi
		m.oled.RenderText("WiFi Setup", []string{"Connect via App"})
	case 3:
		m.state = StateMaintenance
		m.oled.RenderText("Maintenance", []string{"CO2 Cal", "PMS Reset", "Fan Toggle"})
	case 4:
		m.state = StateSystemInfo
		m.oled.RenderText("System Info", []string{m.version, "Uptime: 10h"})
	default:
		m.state = StateStatus
		m.renderStatus()
	}
}

func (m *MenuController) renderStatus() {
	if m.oled == nil {
		return
	}
	var netState = "Disconnected"
	var ip = ""
	if m.networkMgr != nil {
		state, nip, _ := m.networkMgr.GetState()
		netState = fmt.Sprint(state)
		ip = nip
	}
	m.oled.RenderStatus(mcu.SensorData{}, netState, ip, m.version)
}

func (m *MenuController) renderMainMenu() {
	if m.oled == nil {
		return
	}
	m.oled.RenderMenu("== MAPS6 Menu ==", mainMenuItems, m.cursor)
}

func (m *MenuController) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return nil
	}
	close(m.done)
	m.running = false
	return nil
}

func (m *MenuController) Status() module.ModuleStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return module.ModuleStatus{
		Name:    m.Name(),
		Enabled: true,
		Running: m.running,
	}
}

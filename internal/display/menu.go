package display

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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
	StateModules
	StateWiFi
	StateMaintenance
	StateSystemInfo
	StateConfirm
)

var mainMenuItems = []string{
	"Module Control",
	"WiFi Status",
	"Maintenance",
	"System Info",
}

type MenuController struct {
	oled          *OLEDDisplay
	keyboard      *input.KeyboardReader
	bus           *bus.SensorBus
	registry      *module.Registry
	networkMgr    *network.Manager
	mega          *mcu.Mega2560
	state         MenuState
	cursor        int
	moduleCursor  int
	maintCursor   int
	timeout       time.Duration
	confirmAction func() error
	deviceID      string
	version       string
	logger        *slog.Logger
	running       atomic.Bool
	mu            sync.Mutex
	done          chan struct{}
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
	if m.running.Swap(true) {
		return nil
	}
	m.done = make(chan struct{})

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

	ticker := time.NewTicker(500 * time.Millisecond)
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
			m.mu.Lock()
			if m.state == StateStatus {
				m.renderStatus()
			}
			m.mu.Unlock()
		case <-idleTimer.C:
			m.mu.Lock()
			m.state = StateStatus
			m.renderStatus()
			m.mu.Unlock()
		}
	}
}

func (m *MenuController) handleKey(ev input.KeyEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state {
	case StateStatus:
		// Any key press from home screen enters main menu
		m.state = StateMainMenu
		m.cursor = 0
		m.renderMainMenu()

	case StateMainMenu:
		switch ev {
		case input.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(mainMenuItems) - 1
			}
			m.renderMainMenu()
		case input.KeyDown:
			if m.cursor < len(mainMenuItems)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
			m.renderMainMenu()
		case input.KeyEnter, input.KeyRight:
			m.executeMainMenu()
		case input.KeyEsc:
			m.state = StateStatus
			m.renderStatus()
		default:
			m.renderMainMenu()
		}

	case StateModules:
		modules := m.getModuleList()
		switch ev {
		case input.KeyUp:
			if m.moduleCursor > 0 {
				m.moduleCursor--
			} else if len(modules) > 0 {
				m.moduleCursor = len(modules) - 1
			}
			m.renderModulesMenu()
		case input.KeyDown:
			if m.moduleCursor < len(modules)-1 {
				m.moduleCursor++
			} else {
				m.moduleCursor = 0
			}
			m.renderModulesMenu()
		case input.KeyEnter:
			if m.moduleCursor >= 0 && m.moduleCursor < len(modules) {
				mod := modules[m.moduleCursor]
				// Avoid deadlock when toggling oled itself or blocking on lock
				go func(modName string, isEnabled bool) {
					if isEnabled {
						_ = m.registry.Disable(modName)
					} else {
						_ = m.registry.Enable(modName)
					}
				}(mod.Name, mod.Enabled)
			}
			time.Sleep(50 * time.Millisecond)
			m.renderModulesMenu()
		case input.KeyEsc, input.KeyLeft:
			m.state = StateMainMenu
			m.renderMainMenu()
		}

	case StateWiFi:
		if ev == input.KeyEsc || ev == input.KeyEnter || ev == input.KeyLeft {
			m.state = StateMainMenu
			m.renderMainMenu()
		}

	case StateMaintenance:
		maintItems := []string{"CO2 Cal (400ppm)", "PMS Reset", "Fan Toggle"}
		switch ev {
		case input.KeyUp:
			if m.maintCursor > 0 {
				m.maintCursor--
			} else {
				m.maintCursor = len(maintItems) - 1
			}
			m.renderMaintenanceMenu()
		case input.KeyDown:
			if m.maintCursor < len(maintItems)-1 {
				m.maintCursor++
			} else {
				m.maintCursor = 0
			}
			m.renderMaintenanceMenu()
		case input.KeyEnter:
			m.state = StateConfirm
			actionName := maintItems[m.maintCursor]
			switch m.maintCursor {
			case 0:
				m.confirmAction = func() error { return m.mega.SetCO2Calibration() }
			case 1:
				m.confirmAction = func() error { return m.mega.SetPMSReset() }
			case 2:
				m.confirmAction = func() error { return m.mega.SetFan(true) }
			}
			m.oled.RenderConfirm(fmt.Sprintf("Run: %s?", actionName))
		case input.KeyEsc, input.KeyLeft:
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
		case input.KeyEsc, input.KeyLeft:
			m.state = StateMaintenance
			m.renderMaintenanceMenu()
		}

	case StateSystemInfo:
		if ev == input.KeyEsc || ev == input.KeyEnter || ev == input.KeyLeft {
			m.state = StateMainMenu
			m.renderMainMenu()
		}

	default:
		m.state = StateStatus
		m.renderStatus()
	}
}

func (m *MenuController) executeMainMenu() {
	switch m.cursor {
	case 0: // Module Control
		m.state = StateModules
		m.moduleCursor = 0
		m.renderModulesMenu()
	case 1: // WiFi Status
		m.state = StateWiFi
		netState := "None"
		ip := "-"
		ssid := "-"
		if m.networkMgr != nil {
			st, nip, nssid := m.networkMgr.GetState()
			netState = st.String()
			if nip != "" {
				ip = nip
			}
			if nssid != "" {
				ssid = nssid
			}
		}
		m.oled.RenderText("WiFi Status", []string{
			fmt.Sprintf("State: %s", netState),
			fmt.Sprintf("IP: %s", ip),
			fmt.Sprintf("SSID: %s", ssid),
			"Press Esc to back",
		})
	case 2: // Maintenance
		m.state = StateMaintenance
		m.maintCursor = 0
		m.renderMaintenanceMenu()
	case 3: // System Info
		m.state = StateSystemInfo
		m.oled.RenderText("System Info", []string{
			fmt.Sprintf("ID: %s", m.deviceID),
			fmt.Sprintf("Ver: %s", m.version),
			"maps6d active",
			"Press Esc to back",
		})
	default:
		m.state = StateStatus
		m.renderStatus()
	}
}

func (m *MenuController) getModuleList() []module.ModuleStatus {
	if m.registry == nil {
		return nil
	}
	return m.registry.StatusAll()
}

func (m *MenuController) renderModulesMenu() {
	if m.oled == nil {
		return
	}
	modules := m.getModuleList()
	var items []string
	for _, mod := range modules {
		state := "[OFF]"
		if mod.Enabled {
			state = "[ON ]"
		}
		items = append(items, fmt.Sprintf("%-12s %s", mod.Name, state))
	}
	if len(items) == 0 {
		items = []string{"No modules"}
	}
	m.oled.RenderMenu("Module Control", items, m.moduleCursor)
}

func (m *MenuController) renderMaintenanceMenu() {
	if m.oled == nil {
		return
	}
	items := []string{
		"CO2 Cal (400ppm)",
		"PMS Sensor Reset",
		"Fan Toggle ON",
	}
	m.oled.RenderMenu("Maintenance", items, m.maintCursor)
}

func (m *MenuController) renderStatus() {
	if m.oled == nil {
		return
	}
	var netState = "None"
	var ip = ""
	if m.networkMgr != nil {
		state, nip, _ := m.networkMgr.GetState()
		netState = state.String()
		ip = nip
	}

	data := mcu.SensorData{}
	if m.bus != nil {
		data = m.bus.Latest()
	}

	m.oled.RenderStatus(m.deviceID, data, netState, ip, m.version, "")
}

func (m *MenuController) renderMainMenu() {
	if m.oled == nil {
		return
	}
	m.oled.RenderMenu("== MAPS6 Menu ==", mainMenuItems, m.cursor)
}

func (m *MenuController) Stop() error {
	if !m.running.Swap(false) {
		return nil
	}
	if m.done != nil {
		close(m.done)
	}
	return nil
}

func (m *MenuController) Status() module.ModuleStatus {
	return module.ModuleStatus{
		Name:    m.Name(),
		Enabled: true,
		Running: m.running.Load(),
	}
}

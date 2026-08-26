package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"maps6/internal/ipc"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"maps6/internal/network"
	"maps6/internal/ota"
)

const (
	ViewMain = iota
	ViewSensor
	ViewModules
	ViewWiFi
	ViewMaintenance
	ViewSysInfo
	ViewOTA
)

type tickMsg time.Time
type wifiScanMsg []network.WiFiNetwork

type Model struct {
	ipcClient     *ipc.Client
	currentView   int
	sensorData    mcu.SensorData
	moduleStatus  []module.ModuleStatus
	systemInfo    ipc.SystemInfo
	otaUpdateInfo *ota.UpdateInfo
	wifiNetworks  []network.WiFiNetwork
	selectedIdx   int
	statusMsg     string
	scanningWiFi  bool
	err           error
	width         int
	height        int
}

func NewModel(client *ipc.Client) Model {
	m := Model{
		ipcClient: client,
	}
	m.fetchData()
	return m
}

func (m *Model) fetchData() {
	if resp, err := m.ipcClient.Call(ipc.MethodGetSystemInfo, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.systemInfo)
	}
	if resp, err := m.ipcClient.Call(ipc.MethodGetSensorData, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.sensorData)
	}
	if resp, err := m.ipcClient.Call(ipc.MethodGetModuleStatus, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.moduleStatus)
	}
}

func (m Model) scanWiFiCmd() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.ipcClient.Call(ipc.MethodGetWiFiNetworks, nil)
		if err != nil || !resp.Success {
			return wifiScanMsg(nil)
		}
		var nets []network.WiFiNetwork
		_ = json.Unmarshal(resp.Data, &nets)
		return wifiScanMsg(nets)
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case wifiScanMsg:
		m.scanningWiFi = false
		m.wifiNetworks = []network.WiFiNetwork(msg)
		if len(m.wifiNetworks) == 0 {
			m.statusMsg = "No WiFi networks found (or scan error)"
		} else {
			m.statusMsg = fmt.Sprintf("Found %d networks", len(m.wifiNetworks))
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1":
			m.currentView = ViewSensor
			m.selectedIdx = 0
			m.statusMsg = ""
		case "2":
			m.currentView = ViewModules
			m.selectedIdx = 0
			m.statusMsg = ""
		case "3":
			m.currentView = ViewWiFi
			m.selectedIdx = 0
			m.scanningWiFi = true
			m.statusMsg = "Scanning WiFi networks..."
			return m, m.scanWiFiCmd()
		case "4":
			m.currentView = ViewMaintenance
			m.selectedIdx = 0
			m.statusMsg = ""
		case "5":
			m.currentView = ViewSysInfo
			m.selectedIdx = 0
			m.statusMsg = ""
		case "6":
			m.currentView = ViewOTA
			m.selectedIdx = 0
			m.statusMsg = ""
		case "esc":
			m.currentView = ViewMain
			m.selectedIdx = 0
			m.statusMsg = ""
		case "r":
			if m.currentView == ViewWiFi {
				m.scanningWiFi = true
				m.statusMsg = "Scanning WiFi networks..."
				return m, m.scanWiFiCmd()
			}
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "down", "j":
			if m.currentView == ViewModules && m.selectedIdx < len(m.moduleStatus)-1 {
				m.selectedIdx++
			} else if m.currentView == ViewWiFi && m.selectedIdx < len(m.wifiNetworks)-1 {
				m.selectedIdx++
			} else if m.currentView == ViewMaintenance && m.selectedIdx < 1 {
				m.selectedIdx++
			}
		case "enter":
			if m.currentView == ViewModules {
				// toggle module
				if m.selectedIdx >= 0 && m.selectedIdx < len(m.moduleStatus) {
					mod := m.moduleStatus[m.selectedIdx]
					m.ipcClient.Call(ipc.MethodSetModuleEnabled, ipc.SetModuleEnabledParams{
						Name:    mod.Name,
						Enabled: !mod.Enabled,
					})
					m.fetchData()
				}
			} else if m.currentView == ViewMaintenance {
				// maintenance
				if m.selectedIdx == 0 {
					if resp, err := m.ipcClient.Call(ipc.MethodTriggerCO2Cal, nil); err == nil && resp.Success {
						m.statusMsg = "CO2 Calibration Triggered (400ppm)"
					} else {
						m.statusMsg = "Failed to trigger CO2 Calibration"
					}
				} else if m.selectedIdx == 1 {
					if resp, err := m.ipcClient.Call(ipc.MethodTriggerPMSReset, nil); err == nil && resp.Success {
						m.statusMsg = "PMS Reset Triggered"
					} else {
						m.statusMsg = "Failed to trigger PMS Reset"
					}
				}
			} else if m.currentView == ViewOTA {
				// OTA check
				m.statusMsg = "Checking for updates..."
				if resp, err := m.ipcClient.Call(ipc.MethodTriggerOTACheck, nil); err == nil && resp.Success {
					if len(resp.Data) > 0 && string(resp.Data) != "null" {
						var info ota.UpdateInfo
						if err := json.Unmarshal(resp.Data, &info); err == nil {
							m.otaUpdateInfo = &info
							if info.Available {
								m.statusMsg = fmt.Sprintf("Update available: %s. Press 'u' to update.", info.Version)
							} else {
								m.statusMsg = "No updates available."
							}
						}
					} else {
						m.statusMsg = "No updates available."
					}
				} else {
					m.statusMsg = "Failed to check OTA."
				}
			}
		case "u":
			if m.currentView == ViewOTA && m.otaUpdateInfo != nil && m.otaUpdateInfo.Available {
				m.statusMsg = "Applying update..."
				m.ipcClient.Call(ipc.MethodTriggerOTAUpdate, nil)
				m.statusMsg = "Update triggered."
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		m.fetchData()
		return m, tickCmd()
	}
	return m, nil
}

func (m Model) View() string {
	switch m.currentView {
	case ViewSensor:
		return renderSensorView(m)
	case ViewModules:
		return renderModuleView(m)
	case ViewWiFi:
		return renderWiFiView(m)
	case ViewMaintenance:
		return renderMaintenanceView(m)
	case ViewSysInfo:
		return renderSystemInfoView(m)
	case ViewOTA:
		return renderOTAView(m)
	default:
		return renderMainView(m)
	}
}

func Run(client *ipc.Client) {
	p := tea.NewProgram(NewModel(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

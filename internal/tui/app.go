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
	"maps6/internal/ota"
)

type tickMsg time.Time

type Model struct {
	ipcClient     *ipc.Client
	currentView   int // 0=main, 1=sensor, 2=modules, 3=maintenance, 4=sysinfo, 5=ota
	sensorData    mcu.SensorData
	moduleStatus  []module.ModuleStatus
	systemInfo    ipc.SystemInfo
	otaUpdateInfo *ota.UpdateInfo
	selectedIdx   int
	statusMsg     string
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
	// fetch system info
	if resp, err := m.ipcClient.Call(ipc.MethodGetSystemInfo, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.systemInfo)
	}
	// fetch sensor data
	if resp, err := m.ipcClient.Call(ipc.MethodGetSensorData, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.sensorData)
	}
	// fetch module status
	if resp, err := m.ipcClient.Call(ipc.MethodGetModuleStatus, nil); err == nil && resp.Success {
		json.Unmarshal(resp.Data, &m.moduleStatus)
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
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1":
			m.currentView = 1
			m.selectedIdx = 0
			m.statusMsg = ""
		case "2":
			m.currentView = 2
			m.selectedIdx = 0
			m.statusMsg = ""
		case "3":
			// WiFi setup
			m.currentView = 0 
		case "4":
			m.currentView = 3
			m.selectedIdx = 0
			m.statusMsg = ""
		case "5":
			m.currentView = 4
			m.selectedIdx = 0
			m.statusMsg = ""
		case "6":
			m.currentView = 5
			m.selectedIdx = 0
			m.statusMsg = ""
		case "esc":
			m.currentView = 0
			m.selectedIdx = 0
			m.statusMsg = ""
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "down", "j":
			if m.currentView == 2 && m.selectedIdx < len(m.moduleStatus)-1 {
				m.selectedIdx++
			} else if m.currentView == 3 && m.selectedIdx < 1 {
				m.selectedIdx++
			}
		case "enter":
			if m.currentView == 2 {
				// toggle module
				if m.selectedIdx >= 0 && m.selectedIdx < len(m.moduleStatus) {
					mod := m.moduleStatus[m.selectedIdx]
					m.ipcClient.Call(ipc.MethodSetModuleEnabled, ipc.SetModuleEnabledParams{
						Name:    mod.Name,
						Enabled: !mod.Enabled,
					})
					m.fetchData()
				}
			} else if m.currentView == 3 {
				// maintenance
				if m.selectedIdx == 0 {
					if resp, err := m.ipcClient.Call(ipc.MethodTriggerCO2Cal, nil); err == nil && resp.Success {
						m.statusMsg = "CO2 Calibration Triggered"
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
			} else if m.currentView == 5 {
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
			if m.currentView == 5 && m.otaUpdateInfo != nil && m.otaUpdateInfo.Available {
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
	case 1:
		return renderSensorView(m)
	case 2:
		return renderModuleView(m)
	case 3:
		return renderMaintenanceView(m)
	case 4:
		return renderSystemInfoView(m)
	case 5:
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

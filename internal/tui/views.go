package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	goodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	selStyle   = lipgloss.NewStyle().Reverse(true)
)

func renderMainView(m Model) string {
	header := fmt.Sprintf("MAPS 8.0 Control Panel         v%s\nDevice: %s\nNetwork: %s (%s)",
		m.systemInfo.Version, m.systemInfo.DeviceID, m.systemInfo.NetworkState, m.systemInfo.IP)

	sensors := fmt.Sprintf("Temp: %.1f°C   Humi: %.1f%%\nCO2: %d ppm   TVOC: %d ppb\nPM2.5: %d µg/m³  Lux: %d",
		m.sensorData.Temp, m.sensorData.Humi, m.sensorData.CO2, m.sensorData.TVOC, m.sensorData.PM25_AE, m.sensorData.Illuminance)

	menu := "[1] Sensor Detail  [4] Maintenance\n[2] Module Control [5] System Info\n[3] WiFi Setup     [6] OTA Update\n                   [q] Quit"

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(header),
		"",
		sensors,
		"",
		menu,
	)

	return boxStyle.Render(content)
}

func renderSensorView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("Sensor Detail View") + "\n\n")

	s := m.sensorData
	sb.WriteString(fmt.Sprintf("Temp: %.2f °C\n", s.Temp))
	sb.WriteString(fmt.Sprintf("Humi: %.2f %%\n", s.Humi))
	sb.WriteString(fmt.Sprintf("CO2: %d ppm (Ave: %d)\n", s.CO2, s.AveCO2))
	sb.WriteString(fmt.Sprintf("TVOC: %d ppb (Base: %d)\n", s.TVOC, s.BaselineTVOC))
	sb.WriteString(fmt.Sprintf("eCO2: %d ppm (Base: %d)\n", s.ECO2, s.BaselineECO2))
	sb.WriteString(fmt.Sprintf("S_H2: %d   S_Ethanol: %d\n", s.SH2, s.SEthanol))
	sb.WriteString(fmt.Sprintf("Lux: %d   Color Temp: %d K\n", s.Illuminance, s.ColorTemp))
	sb.WriteString(fmt.Sprintf("RGB(C): %d, %d, %d (%d)\n", s.ChR, s.ChG, s.ChB, s.ChC))
	sb.WriteString(fmt.Sprintf("PM1.0 (AE): %d µg/m³   (SP): %d µg/m³\n", s.PM1_AE, s.PM1_SP))
	sb.WriteString(fmt.Sprintf("PM2.5 (AE): %d µg/m³   (SP): %d µg/m³\n", s.PM25_AE, s.PM25_SP))
	sb.WriteString(fmt.Sprintf("PM10  (AE): %d µg/m³   (SP): %d µg/m³\n", s.PM10_AE, s.PM10_SP))

	sb.WriteString("\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

func renderModuleView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("Module Control View") + "\n\n")

	for i, mod := range m.moduleStatus {
		cursor := " "
		style := lipgloss.NewStyle()
		if i == m.selectedIdx {
			cursor = ">"
			style = selStyle
		}

		state := "[OFF]"
		if mod.Enabled {
			state = "[ON ]"
		}

		running := "stopped"
		if mod.Running {
			running = "running"
		}

		line := fmt.Sprintf("%s %s %-15s (%s)", cursor, state, mod.Name, running)
		if mod.LastError != "" {
			line += " " + errStyle.Render("ERR: "+mod.LastError)
		}

		sb.WriteString(style.Render(line) + "\n")
	}

	sb.WriteString("\nUse j/k to select, Enter to toggle\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

func renderWiFiView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("WiFi Setup View") + "\n\n")

	sb.WriteString(fmt.Sprintf("Current: %s (IP: %s, SSID: %s)\n\n", m.systemInfo.NetworkState, m.systemInfo.IP, m.systemInfo.SSID))

	if m.scanningWiFi {
		sb.WriteString("Scanning available WiFi networks...\n")
	} else if len(m.wifiNetworks) == 0 {
		sb.WriteString("No networks found or wireless interface unavailable.\nPress 'r' to rescan.\n")
	} else {
		sb.WriteString("Available Networks:\n")
		for i, net := range m.wifiNetworks {
			cursor := " "
			style := lipgloss.NewStyle()
			if i == m.selectedIdx {
				cursor = ">"
				style = selStyle
			}

			sec := "Open"
			if net.Security != "" && net.Security != "--" {
				sec = "Secured"
			}
			line := fmt.Sprintf("%s %-20s (Signal: %d%%, %s)", cursor, net.SSID, net.Signal, sec)
			sb.WriteString(style.Render(line) + "\n")
		}
	}

	if m.statusMsg != "" {
		sb.WriteString("\n" + warnStyle.Render(m.statusMsg) + "\n")
	}

	sb.WriteString("\nPress 'r' to rescan\nTo connect from CLI: maps6ctl wifi connect <SSID> <PASSWORD>\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

func renderMaintenanceView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("Maintenance View") + "\n\n")

	items := []string{"Trigger CO2 Calibration (400ppm)", "Trigger PMS Reset"}
	for i, item := range items {
		cursor := " "
		style := lipgloss.NewStyle()
		if i == m.selectedIdx {
			cursor = ">"
			style = selStyle
		}
		sb.WriteString(style.Render(fmt.Sprintf("%s %s", cursor, item)) + "\n")
	}

	if m.statusMsg != "" {
		sb.WriteString("\n" + warnStyle.Render(m.statusMsg) + "\n")
	}

	sb.WriteString("\nUse j/k to select, Enter to execute\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

func renderSystemInfoView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("System Info View") + "\n\n")

	sb.WriteString(fmt.Sprintf("Device ID: %s\n", m.systemInfo.DeviceID))
	sb.WriteString(fmt.Sprintf("Version: %s\n", m.systemInfo.Version))
	sb.WriteString(fmt.Sprintf("Uptime: %d sec\n", m.systemInfo.UptimeSec))
	sb.WriteString(fmt.Sprintf("MCU Firmware: %d\n", m.systemInfo.MCUFirmware))
	sb.WriteString(fmt.Sprintf("Network State: %s\n", m.systemInfo.NetworkState))
	sb.WriteString(fmt.Sprintf("IP Address: %s\n", m.systemInfo.IP))
	sb.WriteString(fmt.Sprintf("SSID: %s\n", m.systemInfo.SSID))

	sb.WriteString("\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

func renderOTAView(m Model) string {
	sb := strings.Builder{}
	sb.WriteString(titleStyle.Render("OTA Update View") + "\n\n")

	if m.statusMsg != "" {
		sb.WriteString(m.statusMsg + "\n\n")
	} else {
		sb.WriteString("Press Enter to check for updates.\n\n")
	}

	if m.otaUpdateInfo != nil {
		info := m.otaUpdateInfo
		sb.WriteString(fmt.Sprintf("Available: %v\n", info.Available))
		sb.WriteString(fmt.Sprintf("Version: %s\n", info.Version))
		sb.WriteString(fmt.Sprintf("Release Notes: %s\n", info.ReleaseNotes))
		sb.WriteString(fmt.Sprintf("Size: %d bytes\n", info.SizeBytes))
		sb.WriteString(fmt.Sprintf("Mandatory: %v\n", info.Mandatory))
	}

	sb.WriteString("\nPress [esc] to return")
	return boxStyle.Render(sb.String())
}

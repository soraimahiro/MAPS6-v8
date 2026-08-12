package network

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type WiFiNetwork struct {
	SSID     string
	Signal   int
	Security string
}

func ScanWiFi() ([]WiFiNetwork, error) {
	cmd := exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "dev", "wifi", "list")
	out, err := cmd.Output()
	if err != nil {
		return scanWiFiFallback()
	}

	var networks []WiFiNetwork
	lines := strings.Split(string(bytes.TrimSpace(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			security := parts[len(parts)-1]
			signalStr := parts[len(parts)-2]
			ssid := strings.Join(parts[:len(parts)-2], ":")
			
			// nmcli -t escapes ':' as '\:'
			ssid = strings.ReplaceAll(ssid, "\\:", ":")
			
			signal, _ := strconv.Atoi(signalStr)
			if ssid != "" {
				networks = append(networks, WiFiNetwork{
					SSID:     ssid,
					Signal:   signal,
					Security: security,
				})
			}
		}
	}
	return networks, nil
}

func scanWiFiFallback() ([]WiFiNetwork, error) {
	cmd := exec.Command("iwlist", "wlan0", "scan")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	var networks []WiFiNetwork
	var current WiFiNetwork
	
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Cell ") {
			if current.SSID != "" {
				networks = append(networks, current)
			}
			current = WiFiNetwork{}
		} else if strings.HasPrefix(line, "ESSID:") {
			current.SSID = strings.Trim(strings.TrimPrefix(line, "ESSID:"), "\"")
		} else if strings.Contains(line, "Signal level=") {
			parts := strings.Split(line, "Signal level=")
			if len(parts) > 1 {
				sigPart := strings.Split(parts[1], " ")[0]
				sigPart = strings.TrimSuffix(sigPart, "dBm")
				sigPart = strings.TrimSuffix(sigPart, "/100")
				if val, err := strconv.Atoi(sigPart); err == nil {
					current.Signal = val
				}
			}
		} else if strings.Contains(line, "Encryption key:on") {
			current.Security = "encrypted"
		}
	}
	if current.SSID != "" {
		networks = append(networks, current)
	}
	
	return networks, nil
}

func ConnectWiFi(ssid, password string) error {
	cmd := exec.Command("nmcli", "dev", "wifi", "connect", ssid, "password", password)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to connect wifi: %w, out: %s", err, string(out))
	}
	return nil
}

package network

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"maps6/internal/config"
	"maps6/internal/module"
)

type ConnectionState int

const (
	StateNone ConnectionState = iota
	StateWiFi
	StateEthernet
)

func (c ConnectionState) String() string {
	switch c {
	case StateWiFi:
		return "WiFi"
	case StateEthernet:
		return "Ethernet"
	default:
		return "None"
	}
}

type Manager struct {
	state ConnectionState
	ip    string
	ssid  string
	mu    sync.RWMutex

	cfg    *config.Config
	logger *slog.Logger
	cancel context.CancelFunc
}

func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		state:  StateNone,
		cfg:    cfg,
		logger: slog.Default().With("module", "network"),
	}
}

func (m *Manager) CheckConnection() ConnectionState {
	target := m.cfg.Network.PingTarget
	if target == "" {
		target = "8.8.8.8"
	}
	
	cmd := exec.Command("ping", "-c", "1", "-W", "2", target)
	if err := cmd.Run(); err != nil {
		m.mu.Lock()
		m.state = StateNone
		m.ip = ""
		m.ssid = ""
		m.mu.Unlock()
		return StateNone
	}

	var newState ConnectionState = StateEthernet
	// Check if WiFi is operating
	if b, err := os.ReadFile("/sys/class/net/wlan0/operstate"); err == nil {
		if strings.TrimSpace(string(b)) == "up" {
			newState = StateWiFi
		}
	}

	ip := ""
	if cmdIP := exec.Command("hostname", "-I"); cmdIP != nil {
		if out, err := cmdIP.Output(); err == nil {
			fields := strings.Fields(string(out))
			if len(fields) > 0 {
				ip = fields[0]
			}
		}
	} else {
		// Fallback for getting IP using `ip` command could be added, but hostname -I is fairly standard on Linux boards.
	}

	ssid := ""
	if newState == StateWiFi {
		if cmdSSID := exec.Command("iwgetid", "-r"); cmdSSID != nil {
			if out, err := cmdSSID.Output(); err == nil {
				ssid = strings.TrimSpace(string(out))
			}
		}
	}

	m.mu.Lock()
	m.state = newState
	m.ip = ip
	m.ssid = ssid
	m.mu.Unlock()

	return newState
}

func (m *Manager) GetState() (ConnectionState, string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.ip, m.ssid
}

func (m *Manager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state != StateNone
}

func (m *Manager) Name() string {
	return "wifi"
}

func (m *Manager) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	interval := time.Duration(m.cfg.Network.CheckInterval)
	if interval <= 0 {
		interval = 10 * time.Second
	}

	go func() {
		m.CheckConnection() // Initial check
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.CheckConnection()
			}
		}
	}()

	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Status() module.ModuleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := fmt.Sprintf("State: %s, IP: %s", m.state.String(), m.ip)
	if m.state == StateWiFi && m.ssid != "" {
		info += fmt.Sprintf(", SSID: %s", m.ssid)
	}

	return module.ModuleStatus{
		Name:      m.Name(),
		Enabled:   true,
		Running:   m.cancel != nil,
		LastError: info,
	}
}

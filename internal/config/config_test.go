package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Device.AppID != "MAPS6" {
		t.Errorf("expected AppID to be MAPS6, got %s", cfg.Device.AppID)
	}
	if cfg.Device.Version != "8.0.0" {
		t.Errorf("expected Version to be 8.0.0, got %s", cfg.Device.Version)
	}
	if cfg.Modules.WiFi != true {
		t.Errorf("expected WiFi module to be true")
	}
	if cfg.Sensor.PollInterval != Duration(5*time.Second) {
		t.Errorf("expected PollInterval to be 5s, got %v", cfg.Sensor.PollInterval)
	}
}

func TestLoadConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	yamlContent := `
device:
  app_id: "TEST_APP"
  version: "1.0.0"
modules:
  wifi: false
sensor:
  poll_interval: 10s
`
	if _, err := tmpFile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Device.AppID != "TEST_APP" {
		t.Errorf("expected AppID TEST_APP, got %s", cfg.Device.AppID)
	}
	if cfg.Modules.WiFi != false {
		t.Errorf("expected WiFi module to be false")
	}
	if cfg.Sensor.PollInterval != Duration(10*time.Second) {
		t.Errorf("expected PollInterval 10s, got %v", cfg.Sensor.PollInterval)
	}
	// Verify other defaults were kept
	if cfg.Serial.BaudRate != 115200 {
		t.Errorf("expected default BaudRate 115200, got %d", cfg.Serial.BaudRate)
	}
}

func TestSaveConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config_save*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close() // Close so Save can write

	cfg := DefaultConfig()
	cfg.Device.AppID = "SAVE_TEST"
	cfg.Modules.LTE = true

	if err := cfg.Save(tmpFile.Name()); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load after save returned error: %v", err)
	}

	if loaded.Device.AppID != "SAVE_TEST" {
		t.Errorf("expected loaded AppID to be SAVE_TEST, got %s", loaded.Device.AppID)
	}
	if loaded.Modules.LTE != true {
		t.Errorf("expected LTE to be true")
	}
}

func TestLoadShippedConfig(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "maps6.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("shipped config not found")
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("failed to load shipped config: %v", err)
	}

	if cfg.Sensor.PollInterval != Duration(5*time.Second) {
		t.Errorf("expected PollInterval 5s, got %v", cfg.Sensor.PollInterval)
	}
	if cfg.OTA.CheckInterval != Duration(24*time.Hour) {
		t.Errorf("expected OTA CheckInterval 24h, got %v", cfg.OTA.CheckInterval)
	}
	if !cfg.Modules.StorageExt {
		t.Errorf("expected storage_ext module enabled")
	}
}

func TestDurationUnmarshal(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5s", 5 * time.Second},
		{"300ms", 300 * time.Millisecond},
		{"24h", 24 * time.Hour},
		{"1m30s", 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			yamlStr := []byte(tt.input)
			var node yaml.Node
			node.SetString(tt.input) // Simple hack for testing

			yamlStr = []byte("val: " + tt.input)
			var wrapper struct {
				Val Duration `yaml:"val"`
			}
			if err := yaml.Unmarshal(yamlStr, &wrapper); err != nil {
				t.Fatalf("failed to unmarshal duration %s: %v", tt.input, err)
			}
			if time.Duration(wrapper.Val) != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, wrapper.Val)
			}
		})
	}
}

package upload

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
)

func TestMQTTConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Upload.MQTT.GetInterval() != 60*time.Second {
		t.Errorf("expected default sensor interval 60s, got %v", cfg.Upload.MQTT.GetInterval())
	}
	if cfg.Upload.MQTT.GetStatusInterval() != 300*time.Second {
		t.Errorf("expected default status interval 300s, got %v", cfg.Upload.MQTT.GetStatusInterval())
	}

	cfg.Upload.MQTT.Broker = "192.168.1.100"
	if cfg.Upload.MQTT.GetServerURL() != "http://192.168.1.100:3000" {
		t.Errorf("expected derived server url http://192.168.1.100:3000, got %s", cfg.Upload.MQTT.GetServerURL())
	}
}

func TestMQTTModuleStatusPayload(t *testing.T) {
	cfg := config.DefaultConfig()
	b := bus.NewSensorBus()
	mod := NewMQTTModule(cfg, b, "TEST_DEVICE", func() bool { return true })

	mod.SetNetworkStateFn(func() (string, string, string) {
		return "WiFi", "192.168.1.50", "Lab-AP"
	})

	payload := mod.buildStatusPayload()

	if payload.DeviceID != "TEST_DEVICE" {
		t.Errorf("expected device ID TEST_DEVICE, got %s", payload.DeviceID)
	}
	if payload.Network.IP != "192.168.1.50" {
		t.Errorf("expected IP 192.168.1.50, got %s", payload.Network.IP)
	}
	if payload.Network.SSID != "Lab-AP" {
		t.Errorf("expected SSID Lab-AP, got %s", payload.Network.SSID)
	}
	if payload.Network.Type != "wifi" {
		t.Errorf("expected type wifi, got %s", payload.Network.Type)
	}
}

func TestMQTTCommandSetConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	b := bus.NewSensorBus()
	mod := NewMQTTModule(cfg, b, "TEST_DEVICE", func() bool { return true })

	cmdJSON := []byte(`{
		"action": "set_config",
		"timestamp": "2026-09-13T12:00:00Z",
		"params": {
			"config": {
				"sensor_interval": 30,
				"status_interval": 120
			}
		}
	}`)

	mod.handleCommand(cmdJSON)

	if mod.getSensorInterval() != 30*time.Second {
		t.Errorf("expected updated sensor interval 30s, got %v", mod.getSensorInterval())
	}
	if mod.getStatusInterval() != 120*time.Second {
		t.Errorf("expected updated status interval 120s, got %v", mod.getStatusInterval())
	}
}

func TestParseCSVForBackfill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "backfill_test*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	csvContent := `Device ID,Date,Time,Temperature,Humidity,PM2.5_AE,PM1.0_AE,PM10.0_AE,Illuminance,CO2,TVOC,longitude,latitude
TEST_DEV,2026-09-13,12:00:00,26.50,60.00,15,8,22,350,450,80,121.500000,25.000000
TEST_DEV,2026-09-13,12:01:00,26.60,59.80,16,9,24,360,455,82,121.500000,25.000000
`
	filePath := filepath.Join(tmpDir, "2026-09-13.csv")
	if err := os.WriteFile(filePath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write test csv: %v", err)
	}

	records, err := parseCSVForBackfill(filePath, "2026-09-13")
	if err != nil {
		t.Fatalf("parseCSVForBackfill error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Timestamp != "2026-09-13T12:00:00+08:00" {
		t.Errorf("expected timestamp 2026-09-13T12:00:00+08:00, got %s", records[0].Timestamp)
	}
	if records[0].Sensor["temp"] != 26.5 {
		t.Errorf("expected temp 26.5, got %v", records[0].Sensor["temp"])
	}
	if records[0].Sensor["co2"] != 450 {
		t.Errorf("expected co2 450, got %v", records[0].Sensor["co2"])
	}
	if records[1].Sensor["pm25_ae"] != 16 {
		t.Errorf("expected pm25 16, got %v", records[1].Sensor["pm25_ae"])
	}

	count, err := countCSVDataRows(filePath)
	if err != nil {
		t.Fatalf("countCSVDataRows error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestMQTTModuleLiveServerIntegration(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Upload.MQTT.Broker = "127.0.0.1"
	cfg.Upload.MQTT.Port = 1883
	cfg.Upload.MQTT.Username = "maps"
	cfg.Upload.MQTT.Password = "raspberry"
	cfg.Upload.MQTT.UseTLS = false
	cfg.Upload.MQTT.Interval = config.Duration(1 * time.Second)

	sensorBus := bus.NewSensorBus()
	mod := NewMQTTModule(cfg, sensorBus, "V8_TEST_BOX", func() bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mod.Start(ctx); err != nil {
		t.Skipf("skipping live test, cannot connect to local broker: %v", err)
	}
	defer mod.Stop()

	if !mod.IsConnected() {
		t.Errorf("expected module to be connected to local broker")
	}

	// Publish test sensor data via bus
	sensorBus.Publish(mcu.SensorData{
		Temp:    28.5,
		Humi:    62.0,
		CO2:     510,
		PM25_AE: 18,
	})

	time.Sleep(1200 * time.Millisecond)

	st := mod.GetMQTTStatus()
	if !st.Connected {
		t.Errorf("expected GetMQTTStatus().Connected to be true")
	}
	if st.SensorPublishCount == 0 {
		t.Errorf("expected at least 1 sensor publish")
	}
}


package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/display"
	"maps6/internal/ipc"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"maps6/internal/network"
	"maps6/internal/ota"
	"maps6/internal/serial"
	"maps6/internal/storage"
	"maps6/internal/upload"
)

var Version = "dev"

// StubModule represents a module that is not yet fully implemented
type StubModule struct {
	name   string
	logger *slog.Logger
}

func NewStubModule(name string) *StubModule {
	return &StubModule{
		name:   name,
		logger: slog.With("module", name),
	}
}

func (s *StubModule) Name() string { return s.name }
func (s *StubModule) Start(ctx context.Context) error {
	s.logger.Info("Stub module started")
	return nil
}
func (s *StubModule) Stop() error {
	s.logger.Info("Stub module stopped")
	return nil
}
func (s *StubModule) Status() module.ModuleStatus {
	return module.ModuleStatus{Name: s.name, Enabled: true}
}

func getDeviceID() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "unknown"
	}
	for _, i := range interfaces {
		if i.Name == "wlan0" || i.Name == "eth0" {
			mac := i.HardwareAddr.String()
			return strings.ToUpper(strings.ReplaceAll(mac, ":", ""))
		}
	}
	return "unknown"
}

func main() {
	configPath := flag.String("config", "/home/pi/maps6/maps6.yaml", "Path to config file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting MAPS6 Daemon", "version", Version)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load config, using defaults", "err", err)
		cfg = config.DefaultConfig()
	}

	deviceID := getDeviceID()
	slog.Info("Device ID", "id", deviceID)

	port, err := serial.Open(cfg.Serial.Port, cfg.Serial.BaudRate)
	if err != nil {
		slog.Warn("Failed to open primary serial port, trying fallback", "err", err)
		port, err = serial.Open(cfg.Serial.FallbackPort, cfg.Serial.BaudRate)
		if err != nil {
			slog.Error("Failed to open fallback serial port", "err", err)
			os.Exit(1)
		}
	}

	// Wait for Mega2560 bootloader reset after opening serial port (DTR reset pulse)
	slog.Info("Waiting for Mega2560 MCU bootloader to initialize...")
	time.Sleep(2 * time.Second)

	mega := mcu.NewMega2560(port)
	if err := mega.SetSensorPolling(true, true, true, true, true, true); err != nil {
		slog.Error("Failed to set sensor polling", "err", err)
	} else {
		slog.Info("Mega2560 sensor polling configured successfully")
	}
	_ = mega.SetFan(true)
	_ = mega.SetPinLEDAll(true)
	_ = mega.SetStatusLED(1)
	_ = mega.SetRTCDatetime(time.Now())

	var mcuVersion string
	if fw, err := mega.GetFirmwareVersion(); err == nil {
		mcuVersion = strconv.Itoa(fw)
		slog.Info("MCU Firmware", "version", mcuVersion)
	} else {
		slog.Warn("Failed to read MCU firmware version", "err", err)
	}

	sensorBus := bus.NewSensorBus()
	registry := module.NewRegistry(cfg, *configPath)

	otaUpdater := ota.NewUpdater(&cfg.OTA, Version)
	netMgr := network.NewManager(cfg)

	networkStateFn := func() (string, string, string) {
		state, ip, ssid := netMgr.GetState()
		return state.String(), ip, ssid
	}

	mqttModule := upload.NewMQTTModule(cfg, sensorBus, deviceID, netMgr.IsConnected)
	mqttModule.SetNetworkStateFn(networkStateFn)
	mqttModule.SetMCU(mega, mcuVersion)
	mqttModule.SetRegistry(registry)

	registry.Register("wifi", netMgr)
	registry.Register("lass", upload.NewLASSModule(cfg, sensorBus, deviceID, netMgr))
	registry.Register("mqtt", mqttModule)
	registry.Register("oled", display.NewModule(cfg, sensorBus, registry, netMgr, mega, deviceID, Version))
	registry.Register("storage_local", storage.NewLocalModule(cfg, sensorBus, deviceID))
	registry.Register("storage_ext", storage.NewExternalModule(cfg, sensorBus, deviceID))
	registry.Register("ota", otaUpdater)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry.StartEnabled(ctx)

	ipcServer := ipc.NewServer(sensorBus, registry, mega, networkStateFn, otaUpdater, deviceID, Version, cfg)
	if err := ipcServer.Start(ctx); err != nil {
		slog.Error("Failed to start IPC server", "err", err)
	}

	pollInterval := 10 * time.Second
	if cfg.Sensor.PollInterval > 0 {
		pollInterval = time.Duration(cfg.Sensor.PollInterval)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("MAPS6 Daemon fully started")

	for {
		select {
		case <-ticker.C:
			data, err := mega.GetSensorAll()
			if err != nil {
				slog.Error("Failed to get sensor data", "err", err)
				continue
			}
			sensorBus.Publish(data)
		case <-sigCh:
			slog.Info("Shutting down...")
			registry.StopAll()
			ipcServer.Stop()
			port.Close()
			return
		}
	}
}

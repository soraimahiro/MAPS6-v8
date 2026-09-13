package upload

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
	"maps6/internal/module"
)

// NetworkStateFunc returns current network state, IP, and SSID.
type NetworkStateFunc func() (state string, ip string, ssid string)

// MQTTModule manages bidirectional MQTT communication with Central Server.
type MQTTModule struct {
	cfg         *config.Config
	bus         *bus.SensorBus
	deviceID    string
	isConnected func() bool
	client      mqtt.Client

	// Optional system integrations
	networkStateFn NetworkStateFunc
	mcuVersion     string
	mega           *mcu.Mega2560
	registry       *module.Registry

	sensorInterval time.Duration
	statusInterval time.Duration
	intervalMu     sync.RWMutex

	startTime time.Time
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu      sync.RWMutex
	running bool
	err     error
}

// NewMQTTModule creates a new MQTTModule.
func NewMQTTModule(cfg *config.Config, bus *bus.SensorBus, deviceID string, isConnected func() bool) *MQTTModule {
	return &MQTTModule{
		cfg:            cfg,
		bus:            bus,
		deviceID:       deviceID,
		isConnected:    isConnected,
		sensorInterval: cfg.Upload.MQTT.GetInterval(),
		statusInterval: cfg.Upload.MQTT.GetStatusInterval(),
	}
}

// SetNetworkStateFn injects network state resolver.
func (m *MQTTModule) SetNetworkStateFn(fn NetworkStateFunc) {
	m.networkStateFn = fn
}

// SetMCU injects Mega2560 reference and firmware version.
func (m *MQTTModule) SetMCU(mega *mcu.Mega2560, version string) {
	m.mega = mega
	m.mcuVersion = version
}

// SetRegistry injects module registry reference for dynamic module control.
func (m *MQTTModule) SetRegistry(reg *module.Registry) {
	m.registry = reg
}

func (m *MQTTModule) Name() string {
	return "mqtt"
}

func (m *MQTTModule) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("mqtt module already running")
	}
	m.running = true
	m.err = nil
	m.startTime = time.Now()
	m.mu.Unlock()

	if m.cfg.Upload.MQTT.Broker == "" {
		slog.Warn("mqtt broker not configured, skipping start")
		return nil
	}

	opts := mqtt.NewClientOptions()
	scheme := "tcp"
	if m.cfg.Upload.MQTT.UseTLS {
		scheme = "ssl"
	}
	port := m.cfg.Upload.MQTT.Port
	if port <= 0 {
		if m.cfg.Upload.MQTT.UseTLS {
			port = 8883
		} else {
			port = 1883
		}
	}
	brokerURL := fmt.Sprintf("%s://%s:%d", scheme, m.cfg.Upload.MQTT.Broker, port)
	opts.AddBroker(brokerURL)
	opts.SetClientID(m.deviceID)
	if m.cfg.Upload.MQTT.Username != "" {
		opts.SetUsername(m.cfg.Upload.MQTT.Username)
	}
	if m.cfg.Upload.MQTT.Password != "" {
		opts.SetPassword(m.cfg.Upload.MQTT.Password)
	}
	keepalive := m.cfg.Upload.MQTT.Keepalive
	if keepalive <= 0 {
		keepalive = config.Duration(60 * time.Second)
	}
	opts.SetKeepAlive(time.Duration(keepalive))
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	// 1. Setup LWT (Last Will and Testament)
	onlineTopic := fmt.Sprintf("%s/%s/online", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
	offlinePayload := fmt.Sprintf(`{"online":false,"timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
	opts.SetWill(onlineTopic, offlinePayload, 1, true)

	// 2. OnConnect: publish online = true & subscribe to command topic
	opts.OnConnect = func(c mqtt.Client) {
		slog.Info("mqtt connected to broker", "broker", brokerURL)

		// Publish online status (Retain: true)
		onlinePayload := fmt.Sprintf(`{"online":true,"timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
		c.Publish(onlineTopic, 1, true, []byte(onlinePayload))

		// Subscribe to remote command topic
		cmdTopic := fmt.Sprintf("%s/%s/command", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
		c.Subscribe(cmdTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
			m.handleCommand(msg.Payload())
		})

		// Trigger background backfill check
		go m.checkAndRunBackfill()
	}

	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		slog.Error("mqtt connection lost", "error", err)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		m.mu.Lock()
		m.running = false
		m.err = token.Error()
		m.mu.Unlock()
		return fmt.Errorf("failed to connect mqtt: %w", token.Error())
	}
	m.client = client

	subCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	ch := m.bus.Subscribe("mqtt_upload", 10)

	m.wg.Add(2)
	go m.publishSensorTask(subCtx, ch)
	go m.publishStatusTask(subCtx)

	slog.Info("mqtt module started with server integration", "broker", brokerURL)
	return nil
}

func (m *MQTTModule) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	if m.bus != nil {
		m.bus.Unsubscribe("mqtt_upload")
	}

	if m.client != nil && m.client.IsConnected() {
		// Publish graceful offline status
		onlineTopic := fmt.Sprintf("%s/%s/online", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
		offlinePayload := fmt.Sprintf(`{"online":false,"timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
		m.client.Publish(onlineTopic, 1, true, []byte(offlinePayload)).WaitTimeout(1 * time.Second)
		m.client.Disconnect(250)
	}

	m.running = false
	slog.Info("mqtt module stopped")
	return nil
}

func (m *MQTTModule) Status() module.ModuleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errStr string
	if m.err != nil {
		errStr = m.err.Error()
	}

	return module.ModuleStatus{
		Name:      m.Name(),
		Enabled:   m.cfg.Modules.MQTT,
		Running:   m.running,
		LastError: errStr,
	}
}

func (m *MQTTModule) IsConnected() bool {
	if m.client == nil {
		return false
	}
	return m.client.IsConnected()
}

func readCPUTemp() *float64 {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(data))
	millideg, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	temp := millideg / 1000.0
	return &temp
}

type sensorFullData struct {
	mcu.SensorData
	CPUTemp *float64 `json:"cpu_temp,omitempty"`
}

type sensorPayload struct {
	DeviceID  string         `json:"device_id"`
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Timestamp string         `json:"timestamp"`
	Sensor    sensorFullData `json:"sensor"`
}

func (m *MQTTModule) getSensorInterval() time.Duration {
	m.intervalMu.RLock()
	defer m.intervalMu.RUnlock()
	if m.sensorInterval <= 0 {
		return 60 * time.Second
	}
	return m.sensorInterval
}

func (m *MQTTModule) getStatusInterval() time.Duration {
	m.intervalMu.RLock()
	defer m.intervalMu.RUnlock()
	if m.statusInterval <= 0 {
		return 300 * time.Second
	}
	return m.statusInterval
}

func (m *MQTTModule) setSensorInterval(d time.Duration) {
	m.intervalMu.Lock()
	defer m.intervalMu.Unlock()
	if d >= 5*time.Second && d <= 600*time.Second {
		m.sensorInterval = d
	}
}

func (m *MQTTModule) setStatusInterval(d time.Duration) {
	m.intervalMu.Lock()
	defer m.intervalMu.Unlock()
	if d >= 10*time.Second && d <= 1800*time.Second {
		m.statusInterval = d
	}
}

func (m *MQTTModule) publishSensorTask(ctx context.Context, ch <-chan mcu.SensorData) {
	defer m.wg.Done()

	interval := m.getSensorInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var latestData mcu.SensorData
	hasData := false

	topic := fmt.Sprintf("%s/%s/sensor", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
	qos := byte(m.cfg.Upload.MQTT.QoS)

	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if ok {
				latestData = data
				hasData = true
			}
		case <-ticker.C:
			// Check if interval changed dynamically
			currentInterval := m.getSensorInterval()
			if currentInterval != interval {
				interval = currentInterval
				ticker.Reset(interval)
			}

			if !hasData {
				continue
			}
			if m.isConnected != nil && !m.isConnected() {
				continue
			}
			if !m.IsConnected() {
				continue
			}

			payload := sensorPayload{
				DeviceID:  m.deviceID,
				App:       m.cfg.Device.AppID,
				Version:   m.cfg.Device.Version,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Sensor: sensorFullData{
					SensorData: latestData,
					CPUTemp:    readCPUTemp(),
				},
			}

			dataBytes, err := json.Marshal(payload)
			if err != nil {
				slog.Error("mqtt failed to marshal sensor payload", "error", err)
				continue
			}

			token := m.client.Publish(topic, qos, false, dataBytes)
			go func(t mqtt.Token) {
				<-t.Done()
				if t.Error() != nil {
					slog.Error("mqtt failed to publish sensor data", "error", t.Error())
				}
			}(token)
		}
	}
}

type networkInfo struct {
	IP   string `json:"ip,omitempty"`
	SSID string `json:"ssid,omitempty"`
	Type string `json:"type,omitempty"`
}

type statusPayload struct {
	DeviceID    string          `json:"device_id"`
	App         string          `json:"app"`
	Version     string          `json:"version"`
	Timestamp   string          `json:"timestamp"`
	UptimeSec   int64           `json:"uptime_sec"`
	Network     networkInfo     `json:"network"`
	MCUFirmware string          `json:"mcu_firmware,omitempty"`
	Modules     map[string]bool `json:"modules,omitempty"`
}

func (m *MQTTModule) buildStatusPayload() statusPayload {
	uptime := int64(time.Since(m.startTime).Seconds())

	net := networkInfo{Type: "unknown"}
	if m.networkStateFn != nil {
		state, ip, ssid := m.networkStateFn()
		net = networkInfo{
			Type: strings.ToLower(state),
			IP:   ip,
			SSID: ssid,
		}
	}

	modulesMap := make(map[string]bool)
	if m.registry != nil {
		statuses := m.registry.StatusAll()
		for _, s := range statuses {
			modulesMap[s.Name] = s.Running
		}
	} else {
		modulesMap["wifi"] = m.cfg.Modules.WiFi
		modulesMap["lass"] = m.cfg.Modules.LASS
		modulesMap["mqtt"] = m.cfg.Modules.MQTT
		modulesMap["oled"] = m.cfg.Modules.OLED
		modulesMap["storage_local"] = m.cfg.Modules.StorageLocal
		modulesMap["storage_ext"] = m.cfg.Modules.StorageExt
		modulesMap["ota"] = m.cfg.Modules.OTA
	}

	return statusPayload{
		DeviceID:    m.deviceID,
		App:         m.cfg.Device.AppID,
		Version:     m.cfg.Device.Version,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		UptimeSec:   uptime,
		Network:     net,
		MCUFirmware: m.mcuVersion,
		Modules:     modulesMap,
	}
}

func (m *MQTTModule) publishStatusNow() {
	if !m.IsConnected() {
		return
	}
	payload := m.buildStatusPayload()
	dataBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("mqtt failed to marshal status payload", "error", err)
		return
	}
	topic := fmt.Sprintf("%s/%s/status", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
	qos := byte(m.cfg.Upload.MQTT.QoS)
	m.client.Publish(topic, qos, true, dataBytes)
}

func (m *MQTTModule) publishStatusTask(ctx context.Context) {
	defer m.wg.Done()

	interval := m.getStatusInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentInterval := m.getStatusInterval()
			if currentInterval != interval {
				interval = currentInterval
				ticker.Reset(interval)
			}

			if m.isConnected != nil && !m.isConnected() {
				continue
			}
			m.publishStatusNow()
		}
	}
}

type commandPayload struct {
	Action    string                 `json:"action"`
	Timestamp string                 `json:"timestamp"`
	Params    map[string]interface{} `json:"params"`
}

func (m *MQTTModule) handleCommand(raw []byte) {
	var cmd commandPayload
	if err := json.Unmarshal(raw, &cmd); err != nil {
		slog.Error("mqtt command unmarshal error", "error", err)
		return
	}

	slog.Info("mqtt command received", "action", cmd.Action)

	switch cmd.Action {
	case "set_config":
		if cfgMap, ok := cmd.Params["config"].(map[string]interface{}); ok {
			if sInt, ok := cfgMap["sensor_interval"].(float64); ok && sInt > 0 {
				m.setSensorInterval(time.Duration(sInt) * time.Second)
				slog.Info("mqtt updated sensor_interval", "interval", m.getSensorInterval())
			}
			if stInt, ok := cfgMap["status_interval"].(float64); ok && stInt > 0 {
				m.setStatusInterval(time.Duration(stInt) * time.Second)
				slog.Info("mqtt updated status_interval", "interval", m.getStatusInterval())
			}
			// Respond with updated status report
			m.publishStatusNow()
		}

	case "trigger_co2_cal":
		if m.mega != nil {
			slog.Info("executing co2 calibration from mqtt command")
			if err := m.mega.SetCO2Calibration(); err != nil {
				slog.Error("co2 calibration failed", "error", err)
			} else {
				slog.Info("co2 calibration triggered successfully")
			}
		}

	case "trigger_pms_reset":
		if m.mega != nil {
			slog.Info("executing pms reset from mqtt command")
			if err := m.mega.SetPMSReset(); err != nil {
				slog.Error("pms reset failed", "error", err)
			} else {
				slog.Info("pms reset triggered successfully")
			}
		}

	case "trigger_backfill":
		var dates []string
		if dList, ok := cmd.Params["dates"].([]interface{}); ok {
			for _, d := range dList {
				if str, ok := d.(string); ok {
					dates = append(dates, str)
				}
			}
		}
		go m.runBackfill(dates)

	case "reboot":
		slog.Warn("reboot command received, scheduling system reboot")
		go func() {
			time.Sleep(1 * time.Second)
			_ = exec.Command("reboot").Run()
		}()

	case "set_module":
		name, _ := cmd.Params["name"].(string)
		enabled, _ := cmd.Params["enabled"].(bool)
		if name != "" && m.registry != nil {
			if enabled {
				_ = m.registry.Enable(name)
			} else {
				_ = m.registry.Disable(name)
			}
			m.publishStatusNow()
		}
	}
}

// checkAndRunBackfill runs initial backfill comparison against central server.
func (m *MQTTModule) checkAndRunBackfill() {
	serverURL := m.cfg.Upload.MQTT.GetServerURL()
	if serverURL == "" {
		return
	}
	m.runBackfill(nil)
}

// runBackfill uploads missing CSV historical records to Central Server HTTP API.
func (m *MQTTModule) runBackfill(specificDates []string) {
	serverURL := m.cfg.Upload.MQTT.GetServerURL()
	if serverURL == "" {
		return
	}

	storagePath := m.cfg.Storage.Local.Path
	if storagePath == "" {
		storagePath = "/home/pi/maps6/data"
	}

	datesToUpload := specificDates
	if len(datesToUpload) == 0 {
		// Query server's available dates
		datesURL := fmt.Sprintf("%s/api/v1/devices/%s/dates", serverURL, m.deviceID)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(datesURL)
		if err != nil {
			slog.Debug("backfill date query failed", "error", err)
			return
		}
		defer resp.Body.Close()

		var dateRes struct {
			Success bool     `json:"success"`
			Dates   []string `json:"dates"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&dateRes); err != nil {
			return
		}

		serverDates := make(map[string]bool)
		for _, d := range dateRes.Dates {
			serverDates[d] = true
		}

		// Scan local CSV files
		entries, err := os.ReadDir(storagePath)
		if err != nil {
			return
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".csv") {
				continue
			}
			date := strings.TrimSuffix(e.Name(), ".csv")
			if !serverDates[date] {
				datesToUpload = append(datesToUpload, date)
			}
		}
	}

	for _, date := range datesToUpload {
		filePath := filepath.Join(storagePath, fmt.Sprintf("%s.csv", date))
		records, err := parseCSVForBackfill(filePath, date)
		if err != nil || len(records) == 0 {
			continue
		}

		payload := map[string]interface{}{
			"date":    date,
			"records": records,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		uploadURL := fmt.Sprintf("%s/api/v1/devices/%s/backfill", serverURL, m.deviceID)
		req, err := http.NewRequest("POST", uploadURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		uploadResp, err := client.Do(req)
		if err == nil {
			uploadResp.Body.Close()
			slog.Info("mqtt historical backfill uploaded", "device", m.deviceID, "date", date, "records", len(records))
		}

		// Rate limit backfill requests
		time.Sleep(500 * time.Millisecond)
	}
}

type backfillRecord struct {
	Timestamp string                 `json:"timestamp"`
	Sensor    map[string]interface{} `json:"sensor"`
}

// parseCSVForBackfill parses a day's CSV file into backfillRecord structures.
func parseCSVForBackfill(filePath, date string) ([]backfillRecord, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}

	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}

	var records []backfillRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) < len(header) {
			continue
		}

		timeStr := row[colIdx["Time"]]
		timestamp := fmt.Sprintf("%sT%s+08:00", date, timeStr)

		sensor := make(map[string]interface{})
		parseFloatCol := func(colNames []string, targetKey string) {
			for _, name := range colNames {
				if idx, ok := colIdx[name]; ok && idx < len(row) {
					if v, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); err == nil {
						sensor[targetKey] = v
						return
					}
				}
			}
		}
		parseIntCol := func(colNames []string, targetKey string) {
			for _, name := range colNames {
				if idx, ok := colIdx[name]; ok && idx < len(row) {
					if v, err := strconv.Atoi(strings.TrimSpace(row[idx])); err == nil {
						sensor[targetKey] = v
						return
					}
				}
			}
		}

		parseFloatCol([]string{"Temperature", "temp"}, "temp")
		parseFloatCol([]string{"Humidity", "humi"}, "humi")
		parseFloatCol([]string{"CPU_Temp", "cpu_temp"}, "cpu_temp")
		parseIntCol([]string{"CO2", "co2"}, "co2")
		parseIntCol([]string{"AveCO2", "ave_co2"}, "ave_co2")
		parseIntCol([]string{"TVOC", "tvoc"}, "tvoc")
		parseIntCol([]string{"eCO2", "eco2"}, "eco2")
		parseIntCol([]string{"s_h2", "SH2", "RawH2"}, "s_h2")
		parseIntCol([]string{"s_ethanol", "SEthanol", "RawEthanol"}, "s_ethanol")
		parseIntCol([]string{"baseline_tvoc", "BaselineTVOC"}, "baseline_tvoc")
		parseIntCol([]string{"baseline_eco2", "BaselineECO2"}, "baseline_eco2")
		parseIntCol([]string{"Illuminance", "illuminance"}, "illuminance")
		parseIntCol([]string{"ColorTemp", "color_temp"}, "color_temp")
		parseIntCol([]string{"ch_r", "RawRed"}, "ch_r")
		parseIntCol([]string{"ch_g", "RawGreen"}, "ch_g")
		parseIntCol([]string{"ch_b", "RawBlue"}, "ch_b")
		parseIntCol([]string{"ch_c", "RawClear"}, "ch_c")
		parseIntCol([]string{"PM1.0_AE", "PM1_AE", "pm1_ae"}, "pm1_ae")
		parseIntCol([]string{"PM2.5_AE", "PM25_AE", "pm25_ae"}, "pm25_ae")
		parseIntCol([]string{"PM10.0_AE", "PM10_AE", "pm10_ae"}, "pm10_ae")
		parseIntCol([]string{"PM1.0_SP", "PM1_SP", "pm1_sp"}, "pm1_sp")
		parseIntCol([]string{"PM2.5_SP", "PM25_SP", "pm25_sp"}, "pm25_sp")
		parseIntCol([]string{"PM10.0_SP", "PM10_SP", "pm10_sp"}, "pm10_sp")

		records = append(records, backfillRecord{
			Timestamp: timestamp,
			Sensor:    sensor,
		})
	}

	return records, nil
}

package upload

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"maps6/internal/ipc"
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

	// Metrics & telemetry
	statsMu             sync.RWMutex
	sensorPublishCount  int64
	sensorPublishErrors int64
	lastSensorPublish   time.Time
	statusPublishCount  int64
	statusPublishErrors int64
	lastStatusPublish   time.Time

	// Backfill telemetry
	backfillMu          sync.RWMutex
	backfillRunning     bool
	lastBackfillTime    time.Time
	lastBackfillResult  string
	backfillUploaded    int64
	backfillErrors      int64
	backfillCurrentDate string
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
			m.mu.Lock()
			m.err = nil
			m.mu.Unlock()

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
			m.mu.Lock()
			m.err = err
			m.mu.Unlock()
			slog.Error("mqtt connection lost", "error", err)
		}

		client := mqtt.NewClient(opts)
		m.client = client

		subCtx, cancel := context.WithCancel(ctx)
		m.cancel = cancel

		ch := m.bus.Subscribe("mqtt_upload", 10)

		m.wg.Add(3)
		go m.publishSensorTask(subCtx, ch)
		go m.publishStatusTask(subCtx)
		go m.backfillScheduleTask(subCtx)

		// Attempt initial connection
		token := client.Connect()
		if token.Wait() && token.Error() != nil {
			m.mu.Lock()
			m.err = token.Error()
			m.mu.Unlock()
			slog.Warn("mqtt initial connect failed, retrying in background", "error", token.Error())
			m.wg.Add(1)
			go m.reconnectLoop(subCtx)
		}

		slog.Info("mqtt module started with server integration", "broker", brokerURL)
		return nil
}

func (m *MQTTModule) reconnectLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.client == nil {
				return
			}
			if m.client.IsConnected() {
				return
			}
			token := m.client.Connect()
			if token.Wait() && token.Error() == nil {
				m.mu.Lock()
				m.err = nil
				m.mu.Unlock()
				slog.Info("mqtt successfully connected to broker in background")
				return
			} else if token.Error() != nil {
				m.mu.Lock()
				m.err = token.Error()
				m.mu.Unlock()
			}
		}
	}
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
	running := m.running
	var errStr string
	if m.err != nil {
		errStr = m.err.Error()
	} else if running && !m.IsConnected() {
		errStr = "disconnected from broker"
	}
	m.mu.RUnlock()

	return module.ModuleStatus{
		Name:      m.Name(),
		Enabled:   m.cfg.Modules.MQTT,
		Running:   running,
		LastError: errStr,
	}
}

// GetMQTTStatus returns detailed MQTT telemetry and connection metrics.
func (m *MQTTModule) GetMQTTStatus() ipc.MQTTStatus {
	m.mu.RLock()
	running := m.running
	var lastErr string
	if m.err != nil {
		lastErr = m.err.Error()
	} else if running && !m.IsConnected() {
		lastErr = "disconnected from broker"
	}
	m.mu.RUnlock()

	m.statsMu.RLock()
	sCount := m.sensorPublishCount
	sErrors := m.sensorPublishErrors
	var sLast string
	if !m.lastSensorPublish.IsZero() {
		sLast = m.lastSensorPublish.Local().Format("2006-01-02 15:04:05")
	}
	stCount := m.statusPublishCount
	stErrors := m.statusPublishErrors
	var stLast string
	if !m.lastStatusPublish.IsZero() {
		stLast = m.lastStatusPublish.Local().Format("2006-01-02 15:04:05")
	}
	m.statsMu.RUnlock()

	port := m.cfg.Upload.MQTT.Port
	if port <= 0 {
		if m.cfg.Upload.MQTT.UseTLS {
			port = 8883
		} else {
			port = 1883
		}
	}

	bf := m.GetBackfillStatus()

	return ipc.MQTTStatus{
		Enabled:             m.cfg.Modules.MQTT,
		Running:             running,
		Connected:           m.IsConnected(),
		Broker:              m.cfg.Upload.MQTT.Broker,
		Port:                port,
		UseTLS:              m.cfg.Upload.MQTT.UseTLS,
		ClientID:            m.deviceID,
		TopicPrefix:         m.cfg.Upload.MQTT.TopicPrefix,
		SensorIntervalSec:   int(m.getSensorInterval().Seconds()),
		StatusIntervalSec:   int(m.getStatusInterval().Seconds()),
		SensorPublishCount:  sCount,
		SensorPublishErrors: sErrors,
		LastSensorPublish:   sLast,
		StatusPublishCount:  stCount,
		StatusPublishErrors: stErrors,
		LastStatusPublish:   stLast,
		LastError:           lastErr,
		Backfill:            &bf,
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
				m.statsMu.Lock()
				defer m.statsMu.Unlock()
				if t.Error() != nil {
					m.sensorPublishErrors++
					m.mu.Lock()
					m.err = t.Error()
					m.mu.Unlock()
					slog.Error("mqtt failed to publish sensor data", "error", t.Error())
				} else {
					m.sensorPublishCount++
					m.lastSensorPublish = time.Now()
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
	token := m.client.Publish(topic, qos, true, dataBytes)
	go func(t mqtt.Token) {
		<-t.Done()
		m.statsMu.Lock()
		defer m.statsMu.Unlock()
		if t.Error() != nil {
			m.statusPublishErrors++
			m.mu.Lock()
			m.err = t.Error()
			m.mu.Unlock()
			slog.Error("mqtt failed to publish status payload", "error", t.Error())
		} else {
			m.statusPublishCount++
			m.lastStatusPublish = time.Now()
		}
	}(token)
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

func (m *MQTTModule) getBasicAuthHeader() string {
	user := m.cfg.Upload.MQTT.Username
	pass := m.cfg.Upload.MQTT.Password
	if user == "" {
		user = "maps"
	}
	if pass == "" {
		pass = "raspberry"
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// GetBackfillStatus returns current backfill metrics and status.
func (m *MQTTModule) GetBackfillStatus() ipc.BackfillStatus {
	m.backfillMu.RLock()
	defer m.backfillMu.RUnlock()

	var lastTime string
	if !m.lastBackfillTime.IsZero() {
		lastTime = m.lastBackfillTime.Local().Format("2006-01-02 15:04:05")
	}
	res := m.lastBackfillResult
	if res == "" {
		res = "idle"
	}

	return ipc.BackfillStatus{
		Running:       m.backfillRunning,
		LastRunTime:   lastTime,
		LastResult:    res,
		TotalUploaded: m.backfillUploaded,
		TotalErrors:   m.backfillErrors,
		CurrentDate:   m.backfillCurrentDate,
	}
}

// TriggerBackfill triggers a backfill on-demand in a background goroutine.
func (m *MQTTModule) TriggerBackfill(dates []string) error {
	m.backfillMu.RLock()
	running := m.backfillRunning
	m.backfillMu.RUnlock()
	if running {
		return fmt.Errorf("backfill task is already in progress")
	}
	go m.runBackfill(dates)
	return nil
}

func (m *MQTTModule) setBackfillResult(res string) {
	m.backfillMu.Lock()
	m.lastBackfillResult = res
	m.backfillMu.Unlock()
}

func (m *MQTTModule) backfillScheduleTask(ctx context.Context) {
	defer m.wg.Done()

	// Initial check 10 seconds after daemon starts
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
		m.checkAndRunBackfill()
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.IsConnected() {
				m.checkAndRunBackfill()
			}
		}
	}
}

func countCSVDataRows(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	if _, err := reader.Read(); err != nil {
		return 0, err
	}
	count := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// checkAndRunBackfill runs initial backfill comparison against central server.
func (m *MQTTModule) checkAndRunBackfill() {
	serverURL := m.cfg.Upload.MQTT.GetServerURL()
	if serverURL == "" {
		return
	}
	m.runBackfill(nil)
}

type serverDateSummaryItem struct {
	Date    string `json:"date"`
	Count   int    `json:"count"`
	MinTime string `json:"min_time"`
	MaxTime string `json:"max_time"`
}

// runBackfill uploads missing CSV historical records to Central Server HTTP API.
func (m *MQTTModule) runBackfill(specificDates []string) {
	serverURL := m.cfg.Upload.MQTT.GetServerURL()
	if serverURL == "" {
		return
	}

	m.backfillMu.Lock()
	if m.backfillRunning {
		m.backfillMu.Unlock()
		slog.Warn("mqtt backfill already running, skipping")
		return
	}
	m.backfillRunning = true
	m.backfillCurrentDate = "querying server"
	m.backfillMu.Unlock()

	defer func() {
		m.backfillMu.Lock()
		m.backfillRunning = false
		m.backfillCurrentDate = ""
		m.lastBackfillTime = time.Now()
		m.backfillMu.Unlock()
	}()

	storagePath := m.cfg.Storage.Local.Path
	if storagePath == "" {
		storagePath = "/home/pi/maps6/data"
	}

	// 1. Query server for stored date summaries
	summaryURL := fmt.Sprintf("%s/api/v1/devices/%s/date-summary", serverURL, m.deviceID)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", summaryURL, nil)
	if err != nil {
		m.setBackfillResult("failed to create summary request: " + err.Error())
		return
	}
	req.Header.Set("Authorization", m.getBasicAuthHeader())

	serverMap := make(map[string]serverDateSummaryItem)
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		var res struct {
			Success   bool                    `json:"success"`
			Summaries []serverDateSummaryItem `json:"summaries"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			for _, s := range res.Summaries {
				serverMap[s.Date] = s
			}
		}
		resp.Body.Close()
	} else {
		if resp != nil {
			resp.Body.Close()
		}
		// Fallback: try querying /dates if /date-summary is not available
		datesURL := fmt.Sprintf("%s/api/v1/devices/%s/dates", serverURL, m.deviceID)
		if dReq, err := http.NewRequest("GET", datesURL, nil); err == nil {
			dReq.Header.Set("Authorization", m.getBasicAuthHeader())
			if dResp, err := client.Do(dReq); err == nil && dResp.StatusCode == 200 {
				var dRes struct {
					Success bool     `json:"success"`
					Dates   []string `json:"dates"`
				}
				if err := json.NewDecoder(dResp.Body).Decode(&dRes); err == nil {
					for _, d := range dRes.Dates {
						serverMap[d] = serverDateSummaryItem{Date: d, Count: 999999}
					}
				}
				dResp.Body.Close()
			}
		}
	}

	// 2. Scan local CSV files
	entries, err := os.ReadDir(storagePath)
	if err != nil {
		m.setBackfillResult("failed to read local storage directory: " + err.Error())
		return
	}

	specificSet := make(map[string]bool)
	for _, d := range specificDates {
		specificSet[d] = true
	}

	type dateTarget struct {
		date       string
		localCount int
	}
	var datesToUpload []dateTarget

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".csv") {
			continue
		}
		date := strings.TrimSuffix(e.Name(), ".csv")
		if len(specificSet) > 0 && !specificSet[date] {
			continue
		}

		filePath := filepath.Join(storagePath, e.Name())
		localCount, err := countCSVDataRows(filePath)
		if err != nil || localCount == 0 {
			continue
		}

		if serverItem, exists := serverMap[date]; exists {
			if localCount > serverItem.Count {
				datesToUpload = append(datesToUpload, dateTarget{date: date, localCount: localCount})
				slog.Info("backfill detected missing rows", "date", date, "server", serverItem.Count, "local", localCount)
			}
		} else {
			datesToUpload = append(datesToUpload, dateTarget{date: date, localCount: localCount})
			slog.Info("backfill detected missing date", "date", date, "localRows", localCount)
		}
	}

	if len(datesToUpload) == 0 {
		m.setBackfillResult("all records in sync")
		slog.Info("mqtt backfill check complete: all records in sync")
		return
	}

	// 3. Upload chunked records (500 per chunk, 200ms rate limit)
	uploadURL := fmt.Sprintf("%s/api/v1/devices/%s/backfill", serverURL, m.deviceID)
	uploadClient := &http.Client{Timeout: 30 * time.Second}
	totalUploaded := 0
	totalErrors := 0

	for _, target := range datesToUpload {
		m.backfillMu.Lock()
		m.backfillCurrentDate = target.date
		m.backfillMu.Unlock()

		filePath := filepath.Join(storagePath, fmt.Sprintf("%s.csv", target.date))
		records, err := parseCSVForBackfill(filePath, target.date)
		if err != nil || len(records) == 0 {
			continue
		}

		const chunkSize = 500
		for i := 0; i < len(records); i += chunkSize {
			end := i + chunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[i:end]

			payload := map[string]interface{}{
				"date":    target.date,
				"records": chunk,
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				continue
			}

			postReq, err := http.NewRequest("POST", uploadURL, bytes.NewBuffer(payloadBytes))
			if err != nil {
				continue
			}
			postReq.Header.Set("Content-Type", "application/json")
			postReq.Header.Set("Authorization", m.getBasicAuthHeader())

			postResp, err := uploadClient.Do(postReq)
			if err != nil {
				totalErrors++
				m.backfillMu.Lock()
				m.backfillErrors++
				m.backfillMu.Unlock()
				slog.Error("mqtt backfill chunk failed", "date", target.date, "error", err)
			} else {
				postResp.Body.Close()
				totalUploaded += len(chunk)
				m.backfillMu.Lock()
				m.backfillUploaded += int64(len(chunk))
				m.backfillMu.Unlock()
			}

			// Rate limit between chunks
			time.Sleep(200 * time.Millisecond)
		}
	}

	resStr := fmt.Sprintf("completed: %d records uploaded, %d chunk errors", totalUploaded, totalErrors)
	m.setBackfillResult(resStr)
	slog.Info("mqtt backfill job completed", "result", resStr)
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

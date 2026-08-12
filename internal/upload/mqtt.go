package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
	"maps6/internal/module"
)

type MQTTModule struct {
	cfg         *config.Config
	bus         *bus.SensorBus
	deviceID    string
	isConnected func() bool
	client      mqtt.Client

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	running bool
	err     error
}

func NewMQTTModule(cfg *config.Config, bus *bus.SensorBus, deviceID string, isConnected func() bool) *MQTTModule {
	return &MQTTModule{
		cfg:         cfg,
		bus:         bus,
		deviceID:    deviceID,
		isConnected: isConnected,
	}
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
	brokerURL := fmt.Sprintf("%s://%s:%d", scheme, m.cfg.Upload.MQTT.Broker, m.cfg.Upload.MQTT.Port)
	opts.AddBroker(brokerURL)
	opts.SetClientID(m.deviceID)
	if m.cfg.Upload.MQTT.Username != "" {
		opts.SetUsername(m.cfg.Upload.MQTT.Username)
	}
	if m.cfg.Upload.MQTT.Password != "" {
		opts.SetPassword(m.cfg.Upload.MQTT.Password)
	}
	opts.SetKeepAlive(time.Duration(m.cfg.Upload.MQTT.Keepalive))
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)
	
	opts.OnConnect = func(c mqtt.Client) {
		slog.Info("mqtt connected to broker", "broker", brokerURL)
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

	ch := m.bus.Subscribe("mqtt_upload", 5)

	m.wg.Add(2)
	go m.publishSensorTask(subCtx, ch)
	go m.publishStatusTask(subCtx)

	slog.Info("mqtt module started")
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

	if m.client != nil {
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

type sensorPayload struct {
	DeviceID  string         `json:"device_id"`
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Timestamp string         `json:"timestamp"`
	Sensor    mcu.SensorData `json:"sensor"`
}

func (m *MQTTModule) publishSensorTask(ctx context.Context, ch <-chan mcu.SensorData) {
	defer m.wg.Done()

	ticker := time.NewTicker(time.Duration(m.cfg.Upload.LASS.Interval))
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
			if !hasData {
				continue
			}
			if m.isConnected != nil && !m.isConnected() {
				slog.Debug("network not connected, skipping mqtt sensor publish")
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
				Sensor:    latestData,
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

type statusPayload struct {
	DeviceID  string `json:"device_id"`
	App       string `json:"app"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	UptimeSec int64  `json:"uptime_sec"`
}

func (m *MQTTModule) publishStatusTask(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	
	startTime := time.Now()
	topic := fmt.Sprintf("%s/%s/status", m.cfg.Upload.MQTT.TopicPrefix, m.deviceID)
	qos := byte(m.cfg.Upload.MQTT.QoS)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.isConnected != nil && !m.isConnected() {
				slog.Debug("network not connected, skipping mqtt status publish")
				continue
			}
			if !m.IsConnected() {
				continue
			}

			uptime := int64(time.Since(startTime).Seconds())
			payload := statusPayload{
				DeviceID:  m.deviceID,
				App:       m.cfg.Device.AppID,
				Version:   m.cfg.Device.Version,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				UptimeSec: uptime,
			}

			dataBytes, err := json.Marshal(payload)
			if err != nil {
				slog.Error("mqtt failed to marshal status payload", "error", err)
				continue
			}

			token := m.client.Publish(topic, qos, false, dataBytes)
			go func(t mqtt.Token) {
				<-t.Done()
				if t.Error() != nil {
					slog.Error("mqtt failed to publish status data", "error", t.Error())
				}
			}(token)
		}
	}
}

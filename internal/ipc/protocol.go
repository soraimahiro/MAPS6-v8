package ipc

import (
	"encoding/json"
)

// SocketPath is the default path for the UNIX domain socket used for IPC.
const SocketPath = "/var/run/maps6d.sock"

// IPC Method Definitions
const (
	MethodGetSensorData    = "getSensorData"
	MethodGetModuleStatus  = "getModuleStatus"
	MethodSetModuleEnabled = "setModuleEnabled"
	MethodGetSystemInfo    = "getSystemInfo"
	MethodGetWiFiNetworks  = "getWiFiNetworks"
	MethodConnectWiFi      = "connectWiFi"
	MethodTriggerCO2Cal    = "triggerCO2Cal"
	MethodTriggerPMSReset  = "triggerPMSReset"
	MethodTriggerOTACheck  = "triggerOTACheck"
	MethodTriggerOTAUpdate = "triggerOTAUpdate"
	MethodGetMQTTStatus    = "getMQTTStatus"
)

// Request defines the structure of an IPC request.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response defines the structure of an IPC response.
type Response struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// SetModuleEnabledParams defines the parameters for MethodSetModuleEnabled.
type SetModuleEnabledParams struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ConnectWiFiParams defines the parameters for MethodConnectWiFi.
type ConnectWiFiParams struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// SystemInfo defines the payload for MethodGetSystemInfo.
type SystemInfo struct {
	DeviceID     string          `json:"device_id"`
	Version      string          `json:"version"`
	UptimeSec    int64           `json:"uptime_sec"`
	MCUFirmware  int             `json:"mcu_firmware"`
	NetworkState string          `json:"network_state"`
	IP           string          `json:"ip"`
	SSID         string          `json:"ssid"`
	Modules      map[string]bool `json:"modules"`
	MQTT         *MQTTStatus     `json:"mqtt,omitempty"`
}

// MQTTStatus defines the payload for MethodGetMQTTStatus and MQTT telemetry details.
type MQTTStatus struct {
	Enabled             bool   `json:"enabled"`
	Running             bool   `json:"running"`
	Connected           bool   `json:"connected"`
	Broker              string `json:"broker"`
	Port                int    `json:"port"`
	UseTLS              bool   `json:"use_tls"`
	ClientID            string `json:"client_id"`
	TopicPrefix         string `json:"topic_prefix"`
	SensorIntervalSec   int    `json:"sensor_interval_sec"`
	StatusIntervalSec   int    `json:"status_interval_sec"`
	SensorPublishCount  int64  `json:"sensor_publish_count"`
	SensorPublishErrors int64  `json:"sensor_publish_errors"`
	LastSensorPublish   string `json:"last_sensor_publish,omitempty"`
	StatusPublishCount  int64  `json:"status_publish_count"`
	StatusPublishErrors int64  `json:"status_publish_errors"`
	LastStatusPublish   string `json:"last_status_publish,omitempty"`
	LastError           string `json:"last_error,omitempty"`
}

// NewSuccessResponse creates a successful Response with encoded data.
func NewSuccessResponse(data interface{}) Response {
	var raw json.RawMessage
	if data != nil {
		b, _ := json.Marshal(data)
		raw = b
	}
	return Response{
		Success: true,
		Data:    raw,
	}
}

// NewErrorResponse creates an error Response.
func NewErrorResponse(err string) Response {
	return Response{
		Success: false,
		Error:   err,
	}
}

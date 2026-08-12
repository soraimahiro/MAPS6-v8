package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a custom wrapper around time.Duration for YAML marshaling.
type Duration time.Duration

// UnmarshalYAML implements the yaml.Unmarshaler interface for Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	v, err := time.ParseDuration(value.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

type DeviceConfig struct {
	AppID   string `yaml:"app_id"`
	Version string `yaml:"version"`
}

type SerialConfig struct {
	Port         string `yaml:"port"`
	FallbackPort string `yaml:"fallback_port"`
	BaudRate     int    `yaml:"baud_rate"`
}

type ModulesConfig struct {
	WiFi         bool `yaml:"wifi"`
	LASS         bool `yaml:"lass"`
	MQTT         bool `yaml:"mqtt"`
	OLED         bool `yaml:"oled"`
	StorageLocal bool `yaml:"storage_local"`
	StorageExt   bool `yaml:"storage_ext"`
	OTA          bool `yaml:"ota"`
	LTE          bool `yaml:"lte"`
	GPS          bool `yaml:"gps"`
}

type SensorConfig struct {
	PollInterval Duration `yaml:"poll_interval"`
	PollTemp     bool     `yaml:"poll_temp"`
	PollCO2      bool     `yaml:"poll_co2"`
	PollTVOC     bool     `yaml:"poll_tvoc"`
	PollLight    bool     `yaml:"poll_light"`
	PollPMS      bool     `yaml:"poll_pms"`
	PollRTC      bool     `yaml:"poll_rtc"`
}

type NetworkConfig struct {
	CheckInterval Duration `yaml:"check_interval"`
	PingTarget    string   `yaml:"ping_target"`
}

type UploadConfig struct {
	LASS LASSConfig `yaml:"lass"`
	MQTT MQTTConfig `yaml:"mqtt"`
}

type LASSConfig struct {
	URL           string   `yaml:"url"`
	Interval      Duration `yaml:"interval"`
	RetryInterval Duration `yaml:"retry_interval"`
}

type MQTTConfig struct {
	Broker      string   `yaml:"broker"`
	Port        int      `yaml:"port"`
	Username    string   `yaml:"username"`
	Password    string   `yaml:"password"`
	TopicPrefix string   `yaml:"topic_prefix"`
	Keepalive   Duration `yaml:"keepalive"`
	UseTLS      bool     `yaml:"use_tls"`
	QoS         int      `yaml:"qos"`
}

type StorageConfig struct {
	Local    StoragePathConfig `yaml:"local"`
	External StoragePathConfig `yaml:"external"`
}

type StoragePathConfig struct {
	Path     string   `yaml:"path"`
	Interval Duration `yaml:"interval"`
}

type DisplayConfig struct {
	RefreshInterval Duration `yaml:"refresh_interval"`
	MenuTimeout     Duration `yaml:"menu_timeout"`
}

type OTAConfig struct {
	ServerURL     string   `yaml:"server_url"`
	CheckInterval Duration `yaml:"check_interval"`
	AutoUpdate    bool     `yaml:"auto_update"`
}

type Config struct {
	Device  DeviceConfig  `yaml:"device"`
	Serial  SerialConfig  `yaml:"serial"`
	Modules ModulesConfig `yaml:"modules"`
	Sensor  SensorConfig  `yaml:"sensor"`
	Network NetworkConfig `yaml:"network"`
	Upload  UploadConfig  `yaml:"upload"`
	Storage StorageConfig `yaml:"storage"`
	Display DisplayConfig `yaml:"display"`
	OTA     OTAConfig     `yaml:"ota"`
}

// DefaultConfig returns a configuration with default values populated.
func DefaultConfig() *Config {
	return &Config{
		Device: DeviceConfig{
			AppID:   "MAPS6",
			Version: "8.0.0",
		},
		Serial: SerialConfig{
			Port:         "/dev/ttyS0",
			FallbackPort: "/dev/ttyAMA0",
			BaudRate:     115200,
		},
		Modules: ModulesConfig{
			WiFi:         true,
			LASS:         true,
			MQTT:         true,
			OLED:         true,
			StorageLocal: true,
			StorageExt:   true,
			OTA:          true,
			LTE:          false,
			GPS:          false,
		},
		Sensor: SensorConfig{
			PollInterval: Duration(5 * time.Second),
			PollTemp:     true,
			PollCO2:      true,
			PollTVOC:     true,
			PollLight:    true,
			PollPMS:      true,
			PollRTC:      true,
		},
		Network: NetworkConfig{
			CheckInterval: Duration(10 * time.Second),
			PingTarget:    "www.google.com",
		},
		Upload: UploadConfig{
			LASS: LASSConfig{
				URL:           "https://data.lass-net.org/Upload/MAPS-secure.php",
				Interval:      Duration(300 * time.Second),
				RetryInterval: Duration(10 * time.Second),
			},
			MQTT: MQTTConfig{
				Port:        8883,
				TopicPrefix: "MAPS",
				Keepalive:   Duration(270 * time.Second),
				UseTLS:      true,
				QoS:         1,
			},
		},
		Storage: StorageConfig{
			Local: StoragePathConfig{
				Path:     "/home/pi/maps6/data",
				Interval: Duration(60 * time.Second),
			},
			External: StoragePathConfig{
				Path:     "/mnt/SD",
				Interval: Duration(60 * time.Second),
			},
		},
		Display: DisplayConfig{
			RefreshInterval: Duration(300 * time.Millisecond),
			MenuTimeout:     Duration(30 * time.Second),
		},
		OTA: OTAConfig{
			CheckInterval: Duration(24 * time.Hour),
			AutoUpdate:    true,
		},
	}
}

// Load reads the YAML configuration file and applies defaults for missing values.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// Save writes the configuration to a YAML file.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

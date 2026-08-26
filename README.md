# MAPS 8.0 — Air Quality Monitor System

> Go native rewrite of the MAPS 6.0 air quality monitoring box.  
> Single ARM binary, modular architecture, zero Docker dependency.

## Overview

MAPS 8.0 runs on a **Raspberry Pi** equipped with a custom expansion board (Mega2560 MCU + sensors). It collects air quality data (temperature, humidity, CO₂, TVOC, PM2.5, illuminance) and uploads via **LASS HTTP** and/or **self-hosted MQTT**.

### What Changed from Python/Docker

| | Python + Docker (old) | Go v8.0.0 (new) |
|:---|:---|:---|
| Runtime | Docker container (~190 MB) | Single ARM binary (~10-15 MB) |
| Service | `docker run --privileged` | `systemctl start maps6d` |
| Modules | Hard-coded, always on | Toggle on/off at runtime, persisted |
| Management | Flask Web Dashboard | `maps6ctl` CLI + OLED keyboard menu |
| Upload | WiFi HTTP or NB-IoT (coupled) | LASS HTTP + MQTT (independent) |
| Central Monitoring | None | MQTT → Central Dashboard |
| Update | Manual Docker tar swap | OTA (auto version check + apply) |
| LTE/GPS | Partial | Reserved interface, not implemented |

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                  Raspberry Pi (maps6d)                       │
│                                                              │
│  ┌───────────┐   ┌──────────────┐   ┌────────────────────┐   │
│  │  Serial   │──▶│  Mega2560    │──▶│     SensorBus      │   │
│  │  Port     │   │  MCU Driver  │   │     (pub/sub)      │   │
│  └───────────┘   └──────────────┘   └─────┬──┬──┬──┬─────┘   │
│                                           │  │  │  │         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐ │  │  │  │         │
│  │ WiFi Mgr │  │LASS HTTP │  │   MQTT   │◀┘  │  │  │         │
│  └──────────┘  │ Upload   │  │  Upload  │    │  │  │         │
│                └──────────┘  └──────────┘    │  │  │         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐    │  │  │         │
│  │Local CSV │  │Ext SD CSV│  │   OTA    │◀───┘  │  │         │
│  │ Storage  │  │ Storage  │  │ Updater  │       │  │         │
│  └──────────┘  └──────────┘  └──────────┘       │  │         │
│  ┌──────────────────────────────────┐           │  │         │
│  │   OLED Display + USB Keyboard    │◀──────────┘  │         │
│  │   (SSD1306 128×64 + evdev)       │              │         │
│  └──────────────────────────────────┘              │         │
│  ┌──────────────────────────────────┐              │         │
│  │     IPC Server (Unix Socket)     │◀─────────────┘         │
│  └──────────────┬───────────────────┘                        │
└─────────────────┼────────────────────────────────────────────┘
                  │
         ┌────────┴────────┐
         │  maps6ctl CLI   │
         │  (+ TUI panel)  │
         └─────────────────┘
```

### Data Flow

```
Sensors → Mega2560 MCU → UART → maps6d → SensorBus ─┬→ LASS HTTP → LASS Server
                                                     ├→ MQTT      → Broker → Central Dashboard
                                                     ├→ Local CSV → /home/pi/maps6/data/
                                                     ├→ Ext CSV   → /mnt/SD/
                                                     └→ OLED      → 128×64 Screen
```

### MQTT Topic Structure

| Topic | Direction | Description | QoS |
|:---|:---|:---|:---|
| `MAPS/{DEVICE_ID}/sensor` | Box → Broker | Sensor data (per upload interval) | 1 |
| `MAPS/{DEVICE_ID}/status` | Box → Broker | System status (every 10 min) | 1 |
| `MAPS/{DEVICE_ID}/command` | Broker → Box | Remote commands (reserved) | 1 |

---

## Project Structure

```
system_v8/
├── cmd/
│   ├── maps6d/main.go           # Daemon entry point
│   └── maps6ctl/main.go         # CLI tool entry point
├── internal/
│   ├── serial/port.go           # Thread-safe serial port (mutex)
│   ├── mcu/                     # Mega2560 MCU driver
│   │   ├── protocol.go          #   UART packet framing & checksum
│   │   ├── sensor.go            #   SensorData struct (22 fields)
│   │   ├── mega2560.go          #   Command interface
│   │   └── protocol_test.go     #   Unit tests
│   ├── config/                  # YAML configuration
│   │   ├── config.go            #   Struct definitions & load/save
│   │   └── config_test.go       #   Unit tests
│   ├── module/                  # Module lifecycle
│   │   ├── module.go            #   Module interface
│   │   └── registry.go          #   Registry (enable/disable/persist)
│   ├── bus/sensor_bus.go        # Sensor data pub/sub
│   ├── network/                 # Network management
│   │   ├── manager.go           #   WiFi/Ethernet detection
│   │   └── wifi.go              #   Scan & connect (nmcli)
│   ├── upload/                  # Data upload
│   │   ├── lass.go              #   LASS REST HTTP
│   │   └── mqtt.go              #   Self-hosted MQTT
│   ├── storage/                 # CSV storage
│   │   ├── csv.go               #   Shared writer (daily rotation)
│   │   ├── local.go             #   Local /home/pi/maps6/data/
│   │   ├── external.go          #   External SD /mnt/SD/
│   │   └── csv_test.go          #   Unit tests
│   ├── display/                 # OLED + Keyboard
│   │   ├── oled.go              #   SSD1306 renderer (NotoSans)
│   │   ├── menu.go              #   Menu state machine
│   │   └── module.go            #   Display module factory
│   ├── input/keyboard.go        # USB keyboard evdev reader
│   ├── tui/                     # Terminal UI (maps6ctl tui)
│   │   ├── app.go               #   bubbletea application
│   │   └── views.go             #   lipgloss styled views
│   ├── ipc/                     # Daemon ↔ CLI communication
│   │   ├── protocol.go          #   JSON-RPC definitions
│   │   ├── server.go            #   Unix socket server
│   │   └── client.go            #   Unix socket client
│   └── ota/updater.go           # OTA update check & apply
├── configs/
│   ├── maps6.yaml               # Default configuration
│   └── maps6d.service           # systemd unit file
├── fonts/
│   └── NotoSans-Regular.ttf     # OLED display font (614 KB)
├── scripts/
│   └── install.sh               # Raspberry Pi installer
├── Makefile                     # Build / test / release
├── go.mod
└── go.sum
```

---

## Modules

All modules can be toggled on/off at runtime. Changes are persisted to the YAML config.

| Module | Default | Description |
|:---|:---|:---|
| `wifi` | ON | Network connectivity monitoring |
| `lass` | ON | LASS REST HTTP upload (every 5 min) |
| `mqtt` | ON | Self-hosted MQTT publish |
| `oled` | ON | OLED display + USB keyboard menu |
| `storage_local` | ON | Local CSV storage (daily rotation) |
| `storage_ext` | ON | External SD card CSV storage |
| `ota` | ON | OTA version check (every 24 h) |
| `lte` | OFF | Reserved for future LTE/NB-IoT |
| `gps` | OFF | Reserved for future GPS |

---

## Hardware Requirements

- **Raspberry Pi 3B+** (ARMv7, Raspbian/Raspberry Pi OS)
- **MAPS 6.0 Expansion Board** with Mega2560 MCU
- **Sensors** (via MCU): SHT3x, Senseair S8, SGP30, TCS34725, PMS7003
- **OLED**: SSD1306 128×64 (I2C, address 0x3C)
- **Serial**: `/dev/ttyS0` @ 115200 8N1 (GPIO UART)
- **Optional**: USB keyboard (for OLED menu), SPI SD card slot

---

## Build & Deploy

### Prerequisites

- Go 1.21+ installed on development machine (Mac/Linux)
- Network access to Raspberry Pi (for file transfer)

### Stage 1: Build on Local Machine (Mac)

```bash
# Clone and enter project
cd system_v8/

# Run tests
make test

# Build for local development (native architecture)
make build

# Cross-compile + package for Raspberry Pi
make release
# Output: release/maps6-8.0.0-arm.tar.gz
```

The release tarball contains:
```
maps6-8.0.0-arm/
├── maps6d              # ARM daemon binary
├── maps6ctl            # ARM CLI tool binary
├── maps6.yaml          # Default config
├── maps6d.service      # systemd unit
├── install.sh          # Installation script
└── fonts/
    └── NotoSans-Regular.ttf
```

### Stage 2: Install on Raspberry Pi

```bash
# Transfer (choose any method: scp, USB drive, etc.)
scp release/maps6-8.0.0-arm.tar.gz pi@192.168.1.100:~/

# SSH into Raspberry Pi
ssh pi@192.168.1.100

# Extract and install
tar xzf maps6-8.0.0-arm.tar.gz
cd maps6-8.0.0-arm/
sudo ./install.sh
```

The installer will:
1. Stop existing service (if running)
2. Create `/home/pi/maps6/` directory structure
3. Install binaries and symlink `maps6ctl` to PATH
4. Preserve existing `maps6.yaml` (new defaults saved as `.yaml.new`)
5. Install systemd service and start `maps6d`

### Updating

Repeat the same steps. The installer automatically:
- Stops the old service
- Replaces binaries
- Preserves your existing `maps6.yaml`
- Restarts the service

---

## Usage

### Service Management

```bash
# Check service status
sudo systemctl status maps6d

# View live logs
journalctl -u maps6d -f

# Restart / stop
sudo systemctl restart maps6d
sudo systemctl stop maps6d
```

### CLI Tool (maps6ctl)

```bash
# Interactive TUI dashboard
maps6ctl tui

# Quick status check
maps6ctl status

# View sensor data
maps6ctl sensor

# Module management
maps6ctl module list
maps6ctl module enable mqtt
maps6ctl module disable ota

# WiFi management
maps6ctl wifi scan
maps6ctl wifi connect "MyNetwork" "password123"

# Hardware maintenance
maps6ctl co2-cal          # Trigger CO2 calibration (requires confirmation)
maps6ctl pms-reset        # Reset PMS7003

# OTA updates
maps6ctl ota check
maps6ctl ota update
```

### TUI Dashboard

```
┌──────────────────────────────────────────────┐
│  MAPS 8.0 Control Panel             v8.0.0  │
│  Device: B827EB52FDBC                        │
│  Network: WiFi (192.168.1.100)               │
├──────────────────────────────────────────────┤
│  Temp: 25.5°C   Humi: 60.0%                 │
│  CO2: 450 ppm   TVOC: 120 ppb               │
│  PM2.5: 15 µg/m³  Lux: 350                  │
├──────────────────────────────────────────────┤
│  [1] Sensor Data    [4] Maintenance          │
│  [2] Module Control [5] System Info          │
│  [3] WiFi Setup     [6] OTA Update          │
│                     [q] Quit                 │
└──────────────────────────────────────────────┘
```

Access via SSH: `ssh pi@<box-ip>` → `maps6ctl tui`

### OLED + USB Keyboard Menu

When a USB keyboard is connected, press any key on the status screen to enter the menu:

| Key | Action |
|:---|:---|
| ↑ / ↓ | Navigate menu items |
| Enter | Select / Confirm |
| Esc | Back / Cancel |
| Any key (idle) | Enter main menu |
| 30s idle | Auto-return to status screen |

---

## Configuration

Config file: `/home/pi/maps6/maps6.yaml`

```yaml
device:
  app_id: MAPS6
  version: 8.0.0

serial:
  port: /dev/ttyS0
  fallback: /dev/ttyAMA0
  baud_rate: 115200

sensor:
  poll_interval: 5s          # seconds

modules:
  wifi: true
  lass: true
  mqtt: true
  oled: true
  storage_local: true
  storage_ext: true
  ota: true
  lte: false                 # reserved
  gps: false                 # reserved

upload:
  lass:
    url: "https://data.lass-net.org/Upload/MAPS-secure.php"
    interval: 300s           # seconds (5 min)
    retry_interval: 10s      # seconds
  mqtt:
    broker: ""               # e.g. "mqtt.example.com"
    port: 8883
    username: ""
    password: ""
    topic_prefix: "MAPS"
    keepalive: 270s
    use_tls: true
    qos: 1

storage:
  local:
    path: /home/pi/maps6/data
    interval: 60s
  external:
    path: /mnt/SD
    interval: 60s

display:
  refresh_interval: 300ms
  menu_timeout: 30s          # seconds

ota:
  server_url: ""             # OTA server base URL
  check_interval: 24h        # seconds (24 h)
  auto_update: false
```

Module changes via `maps6ctl module enable/disable` are automatically saved back to this file.

---

## Sensor Data

The system reads 22 sensor fields from the Mega2560 MCU:

| Sensor | Fields | Unit |
|:---|:---|:---|
| SHT3x | Temperature, Humidity | °C, %RH |
| Senseair S8 | CO2, Average CO2 | ppm |
| SGP30 | TVOC, eCO2, H2, Ethanol, Baselines | ppb, ppm, Raw |
| TCS34725 | Illuminance, Color Temp, R/G/B/C | Lux, °K, Raw |
| PMS7003 | PM1.0/2.5/10 (AE + SP) | µg/m³ |

### CSV Storage Format

Files are rotated daily: `YYYY-MM-DD.csv` (Asia/Taipei timezone)

```csv
Device ID,Date,Time,Temperature,Humidity,PM2.5_AE,PM1.0_AE,PM10.0_AE,Illuminance,CO2,TVOC,longitude,latitude
B827EB52FDBC,2026-08-23,00:05:00,25.50,60.00,15,8,22,350,450,120,0.000000,0.000000
```

---

## Dependencies

| Package | Purpose |
|:---|:---|
| `go.bug.st/serial` | Serial port communication |
| `gopkg.in/yaml.v3` | YAML config parsing |
| `github.com/eclipse/paho.mqtt.golang` | MQTT client |
| `periph.io/x/conn/v3` | I2C hardware interface |
| `periph.io/x/devices/v3` | SSD1306 OLED driver |
| `periph.io/x/host/v3` | Hardware host init |
| `github.com/golang/freetype` | TrueType font rendering |
| `golang.org/x/image` | Image/font processing |
| `github.com/charmbracelet/bubbletea` | Terminal UI framework |
| `github.com/charmbracelet/lipgloss` | Terminal UI styling |

---

## License

Internal project — MAPS 6.0 Air Quality Monitoring System.

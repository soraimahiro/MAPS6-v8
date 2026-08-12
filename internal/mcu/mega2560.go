package mcu

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"maps6/internal/serial"
)

// Mega2560 represents the MCU driver.
type Mega2560 struct {
	port     *serial.Port
	lastData SensorData
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewMega2560 creates a new Mega2560 instance.
func NewMega2560(port *serial.Port) *Mega2560 {
	return &Mega2560{
		port:   port,
		logger: slog.Default(),
	}
}

// waitEchoCmd waits for the leading byte and echo command.
func (m *Mega2560) waitEchoCmd(echoCmd byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1)
	foundLeading := false

	for time.Now().Before(deadline) {
		m.port.SetReadTimeout(deadline.Sub(time.Now()))
		n, err := m.port.Read(buf)
		if err != nil || n == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if !foundLeading {
			if buf[0] == LeadingCmd {
				foundLeading = true
			}
		} else {
			if buf[0] == echoCmd {
				return []byte{LeadingCmd, echoCmd}, nil
			}
			if buf[0] != LeadingCmd {
				foundLeading = false
			}
		}
	}
	return nil, fmt.Errorf("timeout waiting for echo cmd %X", echoCmd)
}

// GetSensorAll retrieves all sensor data from the MCU.
func (m *Mega2560) GetSensorAll() (SensorData, error) {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	packet := BuildPacket(CmdGetSensorAll, nil)
	if _, err := m.port.Write(packet); err != nil {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastData, err
	}

	header, err := m.waitEchoCmd(CmdGetSensorAll, 2*time.Second)
	if err != nil {
		m.port.Flush()
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastData, err
	}

	payload := make([]byte, 44)
	if _, err := m.port.ReadFull(payload, 2*time.Second); err != nil {
		m.port.Flush()
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastData, err
	}

	csBytes := make([]byte, 2)
	if _, err := m.port.ReadFull(csBytes, 1*time.Second); err != nil {
		m.port.Flush()
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastData, err
	}

	fullPayload := append(header, payload...)
	calcCs := CalcChecksum(fullPayload)
	if calcCs != csBytes[0] || Not(calcCs) != csBytes[1] {
		m.port.Flush()
		m.logger.Warn("checksum mismatch", "expected", calcCs, "got", csBytes[0])
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastData, fmt.Errorf("checksum mismatch")
	}

	data := DecodeSensorPayload(fullPayload)

	m.mu.Lock()
	m.lastData = data
	m.mu.Unlock()

	return data, nil
}

// SetSensorPolling sets the active sensors.
func (m *Mega2560) SetSensorPolling(temp, co2, tvoc, light, pms, rtc bool) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	b := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	payload := []byte{b(temp), b(co2), b(tvoc), b(light), b(pms), b(rtc)}
	packet := BuildPacket(CmdSetPolling, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}

	_, err := m.waitEchoCmd(CmdSetPolling, 1*time.Second)
	if err != nil {
		return err
	}

	resp := make([]byte, 2)
	if _, err := m.port.ReadFull(resp, 1*time.Second); err != nil {
		return err
	}

	return nil
}

// SetFan enables or disables the fan.
func (m *Mega2560) SetFan(enable bool) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	b := byte(0)
	if enable {
		b = 1
	}
	payload := append([]byte(KeyFan), b)
	packet := BuildPacket(CmdSetFan, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetFan, 1*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetStatusLED sets the status LED state.
func (m *Mega2560) SetStatusLED(state uint16) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	payload := []byte{byte(state >> 8), byte(state & 0xFF)}
	packet := BuildPacket(CmdSetStatusLED, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetStatusLED, 1*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetPinLEDAll enables or disables all LED pins.
func (m *Mega2560) SetPinLEDAll(enable bool) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	b := byte(0)
	if enable {
		b = 1
	}
	payload := append([]byte(KeyLED), b)
	packet := BuildPacket(CmdSetLEDAll, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetLEDAll, 1*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetCO2Calibration triggers CO2 calibration.
func (m *Mega2560) SetCO2Calibration() error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	payload := append([]byte(KeyCO2), 1)
	packet := BuildPacket(CmdSetCO2Cal, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetCO2Cal, 2*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetPMSReset triggers PMS reset.
func (m *Mega2560) SetPMSReset() error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	payload := append([]byte(KeyPMS), 1)
	packet := BuildPacket(CmdSetPMSReset, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetPMSReset, 2*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetPMSSleep controls PMS sleep mode.
func (m *Mega2560) SetPMSSleep(sleep bool) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	b := byte(1)
	if sleep {
		b = 0
	}
	payload := append([]byte(KeyPMSSet), b)
	packet := BuildPacket(CmdSetPMSSleep, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetPMSSleep, 2*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// SetRTCDatetime sets the RTC datetime.
func (m *Mega2560) SetRTCDatetime(t time.Time) error {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	payload := []byte{
		byte(t.Year() % 100),
		byte(t.Month()),
		byte(t.Day()),
		byte(t.Hour()),
		byte(t.Minute()),
		byte(t.Second()),
	}
	packet := BuildPacket(CmdSetRTCDatetime, payload)

	if _, err := m.port.Write(packet); err != nil {
		return err
	}
	_, err := m.waitEchoCmd(CmdSetRTCDatetime, 1*time.Second)
	if err == nil {
		resp := make([]byte, 2)
		m.port.ReadFull(resp, 1*time.Second)
	}
	return err
}

// GetFirmwareVersion retrieves the firmware version.
func (m *Mega2560) GetFirmwareVersion() (int, error) {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	packet := BuildPacket(CmdGetInfoVersion, nil)
	if _, err := m.port.Write(packet); err != nil {
		return 0, err
	}

	_, err := m.waitEchoCmd(CmdGetInfoVersion, 1*time.Second)
	if err != nil {
		return 0, err
	}

	payload := make([]byte, 2)
	if _, err := m.port.ReadFull(payload, 1*time.Second); err != nil {
		return 0, err
	}

	csBytes := make([]byte, 2)
	m.port.ReadFull(csBytes, 1*time.Second)

	return int(payload[0])<<8 | int(payload[1]), nil
}

// GetRuntime retrieves the runtime of the MCU.
func (m *Mega2560) GetRuntime() (days, hours, mins, secs int, err error) {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	packet := BuildPacket(CmdGetInfoRuntime, nil)
	if _, err := m.port.Write(packet); err != nil {
		return 0, 0, 0, 0, err
	}

	_, err = m.waitEchoCmd(CmdGetInfoRuntime, 1*time.Second)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	payload := make([]byte, 4)
	if _, err := m.port.ReadFull(payload, 1*time.Second); err != nil {
		return 0, 0, 0, 0, err
	}
	csBytes := make([]byte, 2)
	m.port.ReadFull(csBytes, 1*time.Second)

	return int(payload[0]), int(payload[1]), int(payload[2]), int(payload[3]), nil
}

// GetErrorLog retrieves the error log.
func (m *Mega2560) GetErrorLog() (map[string]int, error) {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	packet := BuildPacket(CmdGetInfoError, nil)
	if _, err := m.port.Write(packet); err != nil {
		return nil, err
	}

	_, err := m.waitEchoCmd(CmdGetInfoError, 1*time.Second)
	if err != nil {
		return nil, err
	}

	payload := make([]byte, 4)
	if _, err := m.port.ReadFull(payload, 1*time.Second); err != nil {
		return nil, err
	}
	csBytes := make([]byte, 2)
	m.port.ReadFull(csBytes, 1*time.Second)

	return map[string]int{
		"err1": int(payload[0]),
		"err2": int(payload[1]),
	}, nil
}

// GetPinState retrieves the pin states.
func (m *Mega2560) GetPinState() (map[string]bool, error) {
	m.port.Lock()
	defer m.port.Unlock()
	m.port.Flush()

	packet := BuildPacket(CmdGetPinState, nil)
	if _, err := m.port.Write(packet); err != nil {
		return nil, err
	}

	_, err := m.waitEchoCmd(CmdGetPinState, 1*time.Second)
	if err != nil {
		return nil, err
	}

	payload := make([]byte, 2)
	if _, err := m.port.ReadFull(payload, 1*time.Second); err != nil {
		return nil, err
	}
	csBytes := make([]byte, 2)
	m.port.ReadFull(csBytes, 1*time.Second)

	return map[string]bool{
		"pin1": payload[0] > 0,
	}, nil
}

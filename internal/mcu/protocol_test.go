package mcu

import (
	"encoding/binary"
	"testing"
)

func TestCalcChecksum(t *testing.T) {
	packet := []byte{0xAA, 0x55, 0xB5, 0x4A}
	// Checksum implementation: for each byte data[i], compute data[i] XOR ((i+1) % 256), sum all results, mask to 0xFF.
	// 0xAA ^ 1 = 170 ^ 1 = 171
	// 0x55 ^ 2 = 85 ^ 2 = 87
	// 0xB5 ^ 3 = 181 ^ 3 = 182
	// 0x4A ^ 4 = 74 ^ 4 = 78
	// 171 + 87 + 182 + 78 = 518. 518 & 0xFF = 6
	cs := CalcChecksum(packet)
	if cs != 6 {
		t.Errorf("expected 6, got %d", cs)
	}
}

func TestNot(t *testing.T) {
	if Not(0xAA) != 0x55 {
		t.Errorf("expected 0x55, got %X", Not(0xAA))
	}
}

func TestBuildPacket(t *testing.T) {
	packet := BuildPacket(0xB5, nil)
	if len(packet) != 6 {
		t.Errorf("expected len 6, got %d", len(packet))
	}
	if packet[0] != 0xAA || packet[1] != 0x55 || packet[2] != 0xB5 || packet[3] != 0x4A {
		t.Errorf("invalid header")
	}
	cs := packet[4]
	if packet[5] != Not(cs) {
		t.Errorf("invalid checksum complement")
	}
}

func TestBuildPacket_WithKey(t *testing.T) {
	payload := append([]byte("FANc"), 1)
	packet := BuildPacket(CmdSetFan, payload)
	if len(packet) != 11 {
		t.Errorf("expected len 11, got %d", len(packet))
	}
}

func TestDecodeSensorPayload(t *testing.T) {
	payload := make([]byte, 46)
	payload[0] = 0xAA
	payload[1] = 0xB5

	binary.LittleEndian.PutUint16(payload[2:4], 2500) // 25.00
	binary.LittleEndian.PutUint16(payload[4:6], 5000) // 50.00
	binary.LittleEndian.PutUint16(payload[6:8], 400)  // 400

	sd := DecodeSensorPayload(payload)
	if sd.Temp != 25.0 {
		t.Errorf("expected 25.0, got %f", sd.Temp)
	}
	if sd.Humi != 50.0 {
		t.Errorf("expected 50.0, got %f", sd.Humi)
	}
	if sd.CO2 != 400 {
		t.Errorf("expected 400, got %d", sd.CO2)
	}
}

func TestDecodeSensorPayload_NegativeTemp(t *testing.T) {
	payload := make([]byte, 46)
	payload[0] = 0xAA
	payload[1] = 0xB5

	// -5.00°C = -500 = 0xFE0C in little-endian = [0x0C, 0xFE]
	payload[2] = 0x0C
	payload[3] = 0xFE

	sd := DecodeSensorPayload(payload)
	if sd.Temp != -5.0 {
		t.Errorf("expected -5.0, got %f", sd.Temp)
	}
}

func TestDecodeSensorPayload_CO2Warmup(t *testing.T) {
	payload := make([]byte, 46)
	payload[0] = 0xAA
	payload[1] = 0xB5

	binary.LittleEndian.PutUint16(payload[6:8], 65535)

	sd := DecodeSensorPayload(payload)
	if sd.CO2 != -1 {
		t.Errorf("expected -1, got %d", sd.CO2)
	}
}

func TestDecodeRuntimePayload(t *testing.T) {
	// RT_DAY = 1337 (0x0539 little-endian), followed by 07:06:05
	payload := []byte{0x39, 0x05, 7, 6, 5}
	days, hours, mins, secs := DecodeRuntimePayload(payload)
	if days != 1337 || hours != 7 || mins != 6 || secs != 5 {
		t.Errorf("expected 1337/7/6/5, got %d/%d/%d/%d", days, hours, mins, secs)
	}
}

func TestDecodeRuntimePayload_Short(t *testing.T) {
	days, hours, mins, secs := DecodeRuntimePayload([]byte{1, 2})
	if days != 0 || hours != 0 || mins != 0 || secs != 0 {
		t.Errorf("expected zeros for short payload, got %d/%d/%d/%d", days, hours, mins, secs)
	}
}

func TestDecodeErrorLogPayload(t *testing.T) {
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint16(payload[0:2], 3)   // temp_hum
	binary.LittleEndian.PutUint16(payload[2:4], 300) // co2
	binary.LittleEndian.PutUint16(payload[10:12], 9) // rtc

	log := DecodeErrorLogPayload(payload)
	if len(log) != 6 {
		t.Errorf("expected 6 counters, got %d", len(log))
	}
	if log["error_temp_hum"] != 3 {
		t.Errorf("expected error_temp_hum 3, got %d", log["error_temp_hum"])
	}
	if log["error_co2"] != 300 {
		t.Errorf("expected error_co2 300, got %d", log["error_co2"])
	}
	if log["error_tvoc"] != 0 || log["error_light"] != 0 || log["error_pms"] != 0 {
		t.Errorf("expected zero counters, got %v", log)
	}
	if log["error_rtc"] != 9 {
		t.Errorf("expected error_rtc 9, got %d", log["error_rtc"])
	}
}

func TestDecodeErrorLogPayload_Short(t *testing.T) {
	log := DecodeErrorLogPayload([]byte{5})
	if log["error_temp_hum"] != 0 {
		t.Errorf("expected zero counter for short payload, got %d", log["error_temp_hum"])
	}
}

func TestDecodePinStatePayload(t *testing.T) {
	payload := []byte{1, 0, 1, 0, 0, 1, 1}
	pins := DecodePinStatePayload(payload)
	if len(pins) != 7 {
		t.Errorf("expected 7 pins, got %d", len(pins))
	}
	if !pins["pin_co2_cal"] {
		t.Errorf("expected pin_co2_cal true")
	}
	if pins["pin_pms_reset"] {
		t.Errorf("expected pin_pms_reset false")
	}
	if !pins["pin_pms_set"] {
		t.Errorf("expected pin_pms_set true")
	}
	if pins["pin_nbiot_pwrkey"] || pins["pin_nbiot_sleep"] {
		t.Errorf("expected nbiot pins false")
	}
	if !pins["pin_led_ctrl"] || !pins["pin_fan_ctrl"] {
		t.Errorf("expected led/fan pins true")
	}
}

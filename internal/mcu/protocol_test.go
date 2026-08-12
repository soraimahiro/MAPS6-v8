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

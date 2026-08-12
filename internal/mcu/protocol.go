package mcu

// LeadingCmd is the start byte for all packets.
const LeadingCmd byte = 0xAA

// Command bytes
const (
	CmdGetSensorAll    byte = 0xB5
	CmdGetInfoVersion  byte = 0xB6
	CmdGetInfoRuntime  byte = 0xB7
	CmdGetInfoError    byte = 0xB8
	CmdGetPinState     byte = 0xBB
	CmdSetStatusLED    byte = 0xBC
	CmdSetCO2Cal       byte = 0xC0
	CmdSetPMSReset     byte = 0xC1
	CmdSetPMSSleep     byte = 0xC2
	CmdSetLEDAll       byte = 0xC5
	CmdSetPolling      byte = 0xC6
	CmdSetRTCDatetime  byte = 0xC7
	CmdSetFan          byte = 0xC8
)

// Security keys
const (
	KeyFan    = "FANc"
	KeyLED    = "SLED"
	KeyCO2    = "S8LP"
	KeyPMS    = "PMS3"
	KeyPMSSet = "3003"
)

// CalcChecksum calculates the checksum for a packet.
// For each byte data[i], compute data[i] XOR ((i+1) % 256), sum all results, mask to 0xFF.
func CalcChecksum(data []byte) byte {
	var sum uint16
	for i, b := range data {
		sum += uint16(b ^ byte((i+1)%256))
	}
	return byte(sum & 0xFF)
}

// Not returns the bitwise complement of b.
func Not(b byte) byte {
	return ^b
}

// BuildPacket builds [0xAA, 0x55, CMD, ~CMD, payload..., CS, ~CS]
func BuildPacket(cmd byte, payload []byte) []byte {
	packet := []byte{LeadingCmd, Not(LeadingCmd), cmd, Not(cmd)}
	packet = append(packet, payload...)
	cs := CalcChecksum(packet)
	packet = append(packet, cs, Not(cs))
	return packet
}

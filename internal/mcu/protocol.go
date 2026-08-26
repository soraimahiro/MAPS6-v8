package mcu

// LeadingCmd is the start byte for all packets.
const LeadingCmd byte = 0xAA

// Command bytes - Sensors & Info Query
const (
	CmdGetTempHum      byte = 0xB0 // SHT3x Temperature & Humidity
	CmdGetCO2          byte = 0xB1 // Senseair S8 CO2
	CmdGetTVOC         byte = 0xB2 // SGP30 TVOC / eCO2
	CmdGetLight        byte = 0xB3 // TCS34725 / AS7262 Light / Color Temp
	CmdGetPMS          byte = 0xB4 // PMS7003 Particulate Matter
	CmdGetSensorAll    byte = 0xB5 // Batch query all 22 fields
	CmdGetInfoVersion  byte = 0xB6 // MCU Firmware Version
	CmdGetInfoRuntime  byte = 0xB7 // MCU Continuous Runtime (Days, Hours, Mins, Secs)
	CmdGetInfoError    byte = 0xB8 // Communication error counters
	CmdGetInfoSensorPor byte = 0xB9 // Sensor POR history & polling status
	CmdGetRTCDateTime  byte = 0xBA // MCU Onboard RTC Datetime
	CmdGetPinState     byte = 0xBB // GPIO pin states
)

// Command bytes - Hardware Control
const (
	CmdSetStatusLED   byte = 0xBC
	CmdSetCO2Cal      byte = 0xC0
	CmdSetPMSReset    byte = 0xC1
	CmdSetPMSSleep    byte = 0xC2
	CmdSetNBIoTPwrKey byte = 0xC3 // Reserved for LTE: Key "NB-I"
	CmdSetNBIoTSleep  byte = 0xC4 // Reserved for LTE: Key "-IOT"
	CmdSetLEDAll      byte = 0xC5
	CmdSetPolling     byte = 0xC6
	CmdSetRTCDatetime byte = 0xC7
	CmdSetFan         byte = 0xC8
)

// Command bytes - UART Transparent Pass-through (Reserved for LTE)
const (
	CmdUARTBegin      byte = 0xCC
	CmdUARTTxRx       byte = 0xCD
	CmdUARTActiveRX   byte = 0xCF
	CmdEchoActiveRX   byte = 0xD0
)

// Security keys
const (
	KeyFan      = "FANc"
	KeyLED      = "SLED"
	KeyCO2      = "S8LP"
	KeyPMS      = "PMS3"
	KeyPMSSet   = "3003"
	KeyNBIoTPwr = "NB-I"
	KeyNBIoTSlp = "-IOT"
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

// BuildCommandPacket builds a 4-byte query packet: [0xAA, 0x55, CMD, ~CMD]
// Used for query commands without payload (0xB0~0xB9, 0xBA, 0xBB).
func BuildCommandPacket(cmd byte) []byte {
	return []byte{LeadingCmd, Not(LeadingCmd), cmd, Not(cmd)}
}

// BuildPacket builds a full packet with payload and checksum:
// [0xAA, 0x55, CMD, ~CMD, payload..., CS, ~CS]
func BuildPacket(cmd byte, payload []byte) []byte {
	packet := []byte{LeadingCmd, Not(LeadingCmd), cmd, Not(cmd)}
	packet = append(packet, payload...)
	cs := CalcChecksum(packet)
	packet = append(packet, cs, Not(cs))
	return packet
}

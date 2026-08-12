package mcu

import "encoding/binary"

// SensorData represents the sensor data payload from the MCU.
type SensorData struct {
	Temp         float64 `json:"temp"`
	Humi         float64 `json:"humi"`
	CO2          int     `json:"co2"`
	AveCO2       int     `json:"ave_co2"`
	TVOC         int     `json:"tvoc"`
	ECO2         int     `json:"eco2"`
	SH2          int     `json:"s_h2"`
	SEthanol     int     `json:"s_ethanol"`
	BaselineTVOC int     `json:"baseline_tvoc"`
	BaselineECO2 int     `json:"baseline_eco2"`
	Illuminance  int     `json:"illuminance"`
	ColorTemp    int     `json:"color_temp"`
	ChR          int     `json:"ch_r"`
	ChG          int     `json:"ch_g"`
	ChB          int     `json:"ch_b"`
	ChC          int     `json:"ch_c"`
	PM1_AE       int     `json:"pm1_ae"`
	PM25_AE      int     `json:"pm25_ae"`
	PM10_AE      int     `json:"pm10_ae"`
	PM1_SP       int     `json:"pm1_sp"`
	PM25_SP      int     `json:"pm25_sp"`
	PM10_SP      int     `json:"pm10_sp"`
}

// DecodeSensorPayload decodes the 46-byte raw payload into SensorData.
func DecodeSensorPayload(data []byte) SensorData {
	if len(data) < 46 {
		return SensorData{}
	}

	tempRaw := int16(binary.LittleEndian.Uint16(data[2:4]))
	humiRaw := int16(binary.LittleEndian.Uint16(data[4:6]))
	co2Raw := binary.LittleEndian.Uint16(data[6:8])

	co2 := int(co2Raw)
	if co2Raw == 65535 {
		co2 = -1
	}

	return SensorData{
		Temp:         float64(tempRaw) / 100.0,
		Humi:         float64(humiRaw) / 100.0,
		CO2:          co2,
		AveCO2:       int(binary.LittleEndian.Uint16(data[8:10])),
		TVOC:         int(binary.LittleEndian.Uint16(data[10:12])),
		ECO2:         int(binary.LittleEndian.Uint16(data[12:14])),
		SH2:          int(binary.LittleEndian.Uint16(data[14:16])),
		SEthanol:     int(binary.LittleEndian.Uint16(data[16:18])),
		BaselineTVOC: int(binary.LittleEndian.Uint16(data[18:20])),
		BaselineECO2: int(binary.LittleEndian.Uint16(data[20:22])),
		Illuminance:  int(binary.LittleEndian.Uint16(data[22:24])),
		ColorTemp:    int(binary.LittleEndian.Uint16(data[24:26])),
		ChR:          int(binary.LittleEndian.Uint16(data[26:28])),
		ChG:          int(binary.LittleEndian.Uint16(data[28:30])),
		ChB:          int(binary.LittleEndian.Uint16(data[30:32])),
		ChC:          int(binary.LittleEndian.Uint16(data[32:34])),
		PM1_AE:       int(binary.LittleEndian.Uint16(data[34:36])),
		PM25_AE:      int(binary.LittleEndian.Uint16(data[36:38])),
		PM10_AE:      int(binary.LittleEndian.Uint16(data[38:40])),
		PM1_SP:       int(binary.LittleEndian.Uint16(data[40:42])),
		PM25_SP:      int(binary.LittleEndian.Uint16(data[42:44])),
		PM10_SP:      int(binary.LittleEndian.Uint16(data[44:46])),
	}
}

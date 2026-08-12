package storage

import (
	"encoding/csv"
	"maps6/internal/mcu"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCSVWriter_WriteRecord(t *testing.T) {
	tempDir := t.TempDir()
	writer := NewCSVWriter(tempDir)

	data := mcu.SensorData{
		Temp:        25.5,
		Humi:        60.0,
		PM25_AE:     10,
		PM1_AE:      5,
		PM10_AE:     12,
		Illuminance: 500,
		CO2:         400,
		TVOC:        100,
	}

	err := writer.WriteRecord("test-device", data, 25.0, 121.0)
	if err != nil {
		t.Fatalf("failed to write record: %v", err)
	}

	// Verify file exists
	loc, _ := time.LoadLocation("Asia/Taipei")
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	dateStr := time.Now().In(loc).Format("2006-01-02")
	filename := filepath.Join(tempDir, dateStr+".csv")

	f, err := os.Open(filename)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 lines (header + 1 data), got %d", len(records))
	}

	header := records[0]
	if len(header) < 13 || header[0] != "Device ID" {
		t.Errorf("unexpected header: %v", header)
	}

	dataRow := records[1]
	if dataRow[0] != "test-device" {
		t.Errorf("expected test-device, got %v", dataRow[0])
	}
}

func TestCSVWriter_DailyRotation(t *testing.T) {
	tempDir := t.TempDir()
	writer := NewCSVWriter(tempDir)

	err := writer.WriteRecord("dev1", mcu.SensorData{}, 0, 0)
	if err != nil {
		t.Fatalf("failed to write record: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Taipei")
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	dateStr := time.Now().In(loc).Format("2006-01-02")
	filename := filepath.Join(tempDir, dateStr+".csv")

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", filename)
	}
}

func TestCSVWriter_HeaderOnlyOnce(t *testing.T) {
	tempDir := t.TempDir()
	writer := NewCSVWriter(tempDir)

	err := writer.WriteRecord("dev1", mcu.SensorData{}, 0, 0)
	if err != nil {
		t.Fatalf("failed to write record 1: %v", err)
	}

	err = writer.WriteRecord("dev1", mcu.SensorData{}, 0, 0)
	if err != nil {
		t.Fatalf("failed to write record 2: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Taipei")
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	dateStr := time.Now().In(loc).Format("2006-01-02")
	filename := filepath.Join(tempDir, dateStr+".csv")

	f, err := os.Open(filename)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 lines (1 header + 2 data), got %d", len(records))
	}

	if records[0][0] != "Device ID" {
		t.Errorf("first line should be header")
	}
	if records[1][0] == "Device ID" || records[2][0] == "Device ID" {
		t.Errorf("header should only appear once")
	}
}

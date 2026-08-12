package storage

import (
	"encoding/csv"
	"fmt"
	"maps6/internal/mcu"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CSVWriter handles concurrent writes to CSV files.
type CSVWriter struct {
	basePath string
	mu       sync.Mutex
}

// NewCSVWriter creates a new CSVWriter with the given base path.
func NewCSVWriter(basePath string) *CSVWriter {
	return &CSVWriter{
		basePath: basePath,
	}
}

// WriteRecord writes a single sensor data record to the current day's CSV file.
func (w *CSVWriter) WriteRecord(deviceID string, data mcu.SensorData, lat, lon float64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	now := time.Now().In(loc)
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")

	if err := os.MkdirAll(w.basePath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := filepath.Join(w.basePath, fmt.Sprintf("%s.csv", dateStr))

	writeHeader := false
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		writeHeader = true
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	if writeHeader {
		header := []string{
			"Device ID", "Date", "Time", "Temperature", "Humidity",
			"PM2.5_AE", "PM1.0_AE", "PM10.0_AE", "Illuminance", "CO2", "TVOC",
			"longitude", "latitude",
		}
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	}

	record := []string{
		deviceID,
		dateStr,
		timeStr,
		fmt.Sprintf("%.2f", data.Temp),
		fmt.Sprintf("%.2f", data.Humi),
		fmt.Sprintf("%d", data.PM25_AE),
		fmt.Sprintf("%d", data.PM1_AE),
		fmt.Sprintf("%d", data.PM10_AE),
		fmt.Sprintf("%d", data.Illuminance),
		fmt.Sprintf("%d", data.CO2),
		fmt.Sprintf("%d", data.TVOC),
		fmt.Sprintf("%f", lon),
		fmt.Sprintf("%f", lat),
	}

	if err := writer.Write(record); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	return nil
}

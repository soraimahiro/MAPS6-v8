package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"time"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/devices/v3/ssd1306"
	"periph.io/x/host/v3"

	"maps6/internal/mcu"
)

type OLEDDisplay struct {
	dev    *ssd1306.Dev
	font9  font.Face
	font14 font.Face
	buf    *image.Gray
	width  int
	height int
}

func NewOLEDDisplay(fontPath string) (*OLEDDisplay, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph host: %w", err)
	}

	// Try default I2C bus (on Raspberry Pi typically /dev/i2c-1)
	b, err := i2creg.Open("")
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus: %w", err)
	}

	dev, err := ssd1306.NewI2C(b, &ssd1306.DefaultOpts)
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("failed to initialize ssd1306 on I2C: %w", err)
	}

	var fontBytes []byte
	if fontPath != "" {
		fontBytes, _ = os.ReadFile(fontPath)
	}
	if len(fontBytes) == 0 {
		fontBytes = defaultFontBytes
	}

	if len(fontBytes) == 0 {
		return nil, fmt.Errorf("no font available (both file and embedded font are empty)")
	}

	ttf, err := truetype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse font: %w", err)
	}

	font9 := truetype.NewFace(ttf, &truetype.Options{Size: 9, DPI: 72})
	font14 := truetype.NewFace(ttf, &truetype.Options{Size: 14, DPI: 72})

	disp := &OLEDDisplay{
		dev:    dev,
		font9:  font9,
		font14: font14,
		buf:    image.NewGray(image.Rect(0, 0, 128, 64)),
		width:  128,
		height: 64,
	}
	disp.Clear()
	_ = disp.Flush()

	return disp, nil
}

func (d *OLEDDisplay) Clear() {
	draw.Draw(d.buf, d.buf.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
}

func (d *OLEDDisplay) DrawText(x, y int, text string, face font.Face) {
	drawer := &font.Drawer{
		Dst:  d.buf,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(x, y<<6),
	}
	drawer.DrawString(text)
}

func (d *OLEDDisplay) Flush() error {
	if d.dev == nil {
		return nil
	}
	return d.dev.Draw(d.buf.Bounds(), d.buf, image.Point{})
}

func (d *OLEDDisplay) Close() error {
	return nil
}

// RenderStatus renders the default 7-line air quality monitoring layout matching system specification
func (d *OLEDDisplay) RenderStatus(deviceID string, data mcu.SensorData, netState, ip, version, csq string) {
	d.Clear()

	now := time.Now().UTC()
	dateStr := now.Format("2006-01-02 15:04:05")

	// Line 1: ID: B827EB52FDBC (14pt)
	d.DrawText(0, 14, fmt.Sprintf("ID: %s", deviceID), d.font14)

	// Line 2: Date: 2026-08-26 15:00:00 (9pt)
	d.DrawText(0, 24, fmt.Sprintf("Date: %s", dateStr), d.font9)

	// Line 3: Temp: 25.5 / RH: 60.0 (9pt)
	d.DrawText(0, 34, fmt.Sprintf("Temp: %.1f / RH: %.1f", data.Temp, data.Humi), d.font9)

	// Line 4: PM2.5: 15 ug/m3 (9pt)
	d.DrawText(0, 44, fmt.Sprintf("PM2.5: %d ug/m3", data.PM25_AE), d.font9)

	// Line 5: TVOC: 120 ppb (9pt)
	d.DrawText(0, 54, fmt.Sprintf("TVOC: %d ppb", data.TVOC), d.font9)

	// Line 6: CO2: 450 ppm (9pt)
	co2Str := fmt.Sprintf("%d", data.CO2)
	if data.CO2 < 0 {
		co2Str = "Init"
	}
	d.DrawText(0, 64, fmt.Sprintf("CO2: %s ppm", co2Str), d.font9)

	// Line 7 Right Bottom: Network status and version
	netIcon := "-"
	if netState == "wifi" || netState == "WiFi" || netState == "1" {
		netIcon = "W"
	} else if netState == "nbiot" || netState == "NBIOT" || netState == "2" {
		netIcon = "N"
	}

	if csq == "" {
		csq = "-"
	}
	d.DrawText(75, 54, fmt.Sprintf("csq: %s", csq), d.font9)
	d.DrawText(75, 64, fmt.Sprintf("V%s %s", version, netIcon), d.font9)

	_ = d.Flush()
}

func (d *OLEDDisplay) RenderMenu(title string, items []string, cursor int) {
	d.Clear()
	d.DrawText(0, 14, title, d.font14)
	y := 24
	for i, item := range items {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		d.DrawText(0, y, prefix+item, d.font9)
		y += 10
	}
	_ = d.Flush()
}

func (d *OLEDDisplay) RenderText(title string, lines []string) {
	d.Clear()
	d.DrawText(0, 14, title, d.font14)
	y := 24
	for _, line := range lines {
		d.DrawText(0, y, line, d.font9)
		y += 10
	}
	_ = d.Flush()
}

func (d *OLEDDisplay) RenderConfirm(message string) {
	d.Clear()
	d.DrawText(0, 24, message, d.font9)
	d.DrawText(0, 44, "Enter=Yes Esc=No", d.font9)
	_ = d.Flush()
}

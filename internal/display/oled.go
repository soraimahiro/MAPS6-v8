package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"

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
		return nil, fmt.Errorf("failed to initialize periph: %w", err)
	}

	b, err := i2creg.Open("")
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C: %w", err)
	}

	dev, err := ssd1306.NewI2C(b, &ssd1306.DefaultOpts)
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("failed to initialize ssd1306: %w", err)
	}

	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read font: %w", err)
	}

	ttf, err := truetype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse font: %w", err)
	}

	font9 := truetype.NewFace(ttf, &truetype.Options{Size: 9, DPI: 72})
	font14 := truetype.NewFace(ttf, &truetype.Options{Size: 14, DPI: 72})

	return &OLEDDisplay{
		dev:    dev,
		font9:  font9,
		font14: font14,
		buf:    image.NewGray(image.Rect(0, 0, 128, 64)),
		width:  128,
		height: 64,
	}, nil
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
	return d.dev.Draw(d.buf.Bounds(), d.buf, image.Point{})
}

func (d *OLEDDisplay) Close() error {
	return nil
}

func (d *OLEDDisplay) RenderStatus(data mcu.SensorData, netState, ip, version string) {
	d.Clear()
	d.DrawText(0, 14, "ID: B827EB52FDBC", d.font14)
	d.DrawText(0, 26, "2026-07-30 14:55:00", d.font9)
	d.DrawText(0, 36, fmt.Sprintf("Temp:25.5  RH:60.0"), d.font9)
	d.DrawText(0, 46, fmt.Sprintf("PM2.5: %d ug/m3", 15), d.font9)
	d.DrawText(0, 56, fmt.Sprintf("CO2:%d  TVOC:%d", 450, 120), d.font9)
	d.DrawText(0, 64, fmt.Sprintf("%s    %s", version, netState), d.font9)
	d.Flush()
}

func (d *OLEDDisplay) RenderMenu(title string, items []string, cursor int) {
	d.Clear()
	d.DrawText(0, 14, title, d.font14)
	y := 26
	for i, item := range items {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		d.DrawText(0, y, prefix+item, d.font9)
		y += 10
	}
	d.Flush()
}

func (d *OLEDDisplay) RenderText(title string, lines []string) {
	d.Clear()
	d.DrawText(0, 14, title, d.font14)
	y := 26
	for _, line := range lines {
		d.DrawText(0, y, line, d.font9)
		y += 10
	}
	d.Flush()
}

func (d *OLEDDisplay) RenderConfirm(message string) {
	d.Clear()
	d.DrawText(0, 24, message, d.font9)
	d.DrawText(0, 44, "Enter=Yes Esc=No", d.font9)
	d.Flush()
}

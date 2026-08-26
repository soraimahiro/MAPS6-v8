package display

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"syscall"
	"time"

	"tinygo.org/x/tinyfont"
	"tinygo.org/x/tinyfont/proggy"

	"maps6/internal/mcu"
)

const (
	OLEDWidth   = 128
	OLEDHeight  = 64
	I2CSlave    = 0x0703
	SSD1306Addr = 0x3C
)

// OLEDDisplay represents an SSD1306 OLED display using native I2C or fallback.
type OLEDDisplay struct {
	file   *os.File
	buf    [1024]byte // 128 x 64 / 8 bytes
	logger *slog.Logger
}

// Size implements tinyfont.Displayer
func (d *OLEDDisplay) Size() (x, y int16) {
	return OLEDWidth, OLEDHeight
}

// SetPixel implements tinyfont.Displayer
func (d *OLEDDisplay) SetPixel(x, y int16, c color.RGBA) {
	if x < 0 || x >= OLEDWidth || y < 0 || y >= OLEDHeight {
		return
	}
	idx := int(x) + int(y/8)*OLEDWidth
	if c.R > 64 || c.G > 64 || c.B > 64 || c.A > 64 {
		d.buf[idx] |= (1 << (y % 8))
	} else {
		d.buf[idx] &= ^(1 << (y % 8))
	}
}

// Display implements tinyfont.Displayer
func (d *OLEDDisplay) Display() error {
	return d.Flush()
}

// NewOLEDDisplay initializes SSD1306 over /dev/i2c-1 (or /dev/i2c-0).
func NewOLEDDisplay(fontPath string) (*OLEDDisplay, error) {
	logger := slog.Default().With("component", "oled")

	// Try opening Linux I2C device (/dev/i2c-1 is standard on Raspberry Pi)
	busPaths := []string{"/dev/i2c-1", "/dev/i2c-0"}
	var f *os.File
	var openErr error

	for _, p := range busPaths {
		f, openErr = os.OpenFile(p, os.O_RDWR, 0600)
		if openErr == nil {
			logger.Info("Opened I2C device", "path", p)
			break
		}
	}

	if f == nil {
		return nil, fmt.Errorf("cannot open I2C bus: %w", openErr)
	}

	// Set I2C slave address 0x3C
	if err := ioctl(f.Fd(), I2CSlave, uintptr(SSD1306Addr)); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to set I2C slave address 0x3C: %w", err)
	}

	disp := &OLEDDisplay{
		file:   f,
		logger: logger,
	}

	// Initialize SSD1306 hardware
	if err := disp.initSSD1306(); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to init SSD1306 hardware: %w", err)
	}

	disp.Clear()
	_ = disp.Flush()
	logger.Info("SSD1306 OLED initialized successfully with tinyfont proggy")

	return disp, nil
}

func ioctl(fd, cmd, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, cmd, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func (d *OLEDDisplay) writeCommand(cmd byte) error {
	packet := []byte{0x00, cmd} // Co = 0, D/C# = 0
	_, err := d.file.Write(packet)
	return err
}

func (d *OLEDDisplay) writeCommands(cmds ...byte) error {
	packet := make([]byte, len(cmds)+1)
	packet[0] = 0x00
	copy(packet[1:], cmds)
	_, err := d.file.Write(packet)
	return err
}

func (d *OLEDDisplay) initSSD1306() error {
	// Standard SSD1306 128x64 initialization sequence
	cmds := []byte{
		0xAE,       // Display off
		0xD5, 0x80, // Set display clock divide ratio / oscillator frequency
		0xA8, 0x3F, // Set multiplex ratio: 64 lines (0x3F)
		0xD3, 0x00, // Set display offset = 0
		0x40,       // Set start line = 0
		0x8D, 0x14, // Enable charge pump regulator
		0x20, 0x00, // Set Memory Addressing Mode: Horizontal
		0xA1,       // Set Segment Re-map (column 127 mapped to SEG0)
		0xC8,       // Set COM Output Scan Direction (remapped)
		0xDA, 0x12, // Set COM Pins Hardware Config: alternative, disable left/right remap
		0x81, 0xCF, // Set Contrast Control: 0xCF
		0xD9, 0xF1, // Set Pre-charge Period
		0xDB, 0x40, // Set VCOMH Deselect Level
		0xA4, // Entire Display ON (output follows RAM content)
		0xA6, // Set Normal (non-inverted) Display
		0xAF, // Turn Display ON
	}
	return d.writeCommands(cmds...)
}

// Clear clears the display buffer
func (d *OLEDDisplay) Clear() {
	for i := range d.buf {
		d.buf[i] = 0
	}
}

// Flush sends the 1024-byte buffer to the SSD1306 display
func (d *OLEDDisplay) Flush() error {
	if d.file == nil {
		return nil
	}

	// Set column and page address to full range (128 columns, 8 pages)
	_ = d.writeCommands(
		0x21, 0x00, 0x7F, // Column address: 0 to 127
		0x22, 0x00, 0x07, // Page address: 0 to 7
	)

	// Write buffer in chunks (Data mode: prefix byte 0x40)
	chunkSize := 64
	dataPacket := make([]byte, chunkSize+1)
	dataPacket[0] = 0x40 // Co = 0, D/C# = 1 (Data)

	for i := 0; i < len(d.buf); i += chunkSize {
		end := i + chunkSize
		if end > len(d.buf) {
			end = len(d.buf)
		}
		packetLen := (end - i) + 1
		copy(dataPacket[1:packetLen], d.buf[i:end])
		if _, err := d.file.Write(dataPacket[:packetLen]); err != nil {
			return err
		}
	}

	return nil
}

// Close closes the I2C file descriptor
func (d *OLEDDisplay) Close() error {
	if d.file != nil {
		_ = d.writeCommand(0xAE) // Display off
		err := d.file.Close()
		d.file = nil
		return err
	}
	return nil
}

// RenderStatus renders the 7-line layout using tinyfont proggy
func (d *OLEDDisplay) RenderStatus(deviceID string, data mcu.SensorData, netState, ip, version, csq string) {
	d.Clear()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	curY := int16(7)

	// Line 1: ID
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY+1, fmt.Sprintf("ID:%s", deviceID), white)
	curY += 9

	// Line 2: Date
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, fmt.Sprintf("Date:%s", nowStr), white)
	curY += 9

	// Line 3: Temp & RH
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, fmt.Sprintf("Temp:%.1f / RH:%.1f", data.Temp, data.Humi), white)
	curY += 9

	// Line 4: PM2.5
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, fmt.Sprintf("PM2.5:%dug/m3", data.PM25_AE), white)
	curY += 9

	// Line 5: TVOC
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, fmt.Sprintf("TVOC:%dppb", data.TVOC), white)
	curY += 9

	// Line 6: CO2
	co2Str := fmt.Sprintf("%d", data.CO2)
	if data.CO2 < 0 {
		co2Str = "Init"
	}
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, fmt.Sprintf("CO2:%sppm", co2Str), white)

	// Line 7 Right Bottom: CSQ, Version, Network Icon
	netIcon := "-"
	if netState == "wifi" || netState == "WiFi" || netState == "1" {
		netIcon = "Wifi"
	} else if netState == "nbiot" || netState == "NBIOT" || netState == "2" {
		netIcon = "Nbiot"
	}

	if csq == "" {
		csq = "-"
	}
	// tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 80, 43, fmt.Sprintf("csq: %s", csq), white)
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 80, 43, fmt.Sprintf("V%s", version), white)
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 80, 52, netIcon, white)

	_ = d.Flush()
}

// RenderMenu renders the interactive menu with scrolling support
func (d *OLEDDisplay) RenderMenu(title string, items []string, cursor int) {
	d.Clear()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, 8, title, white)

	maxVisible := 5
	startIdx := 0
	if cursor >= maxVisible {
		startIdx = cursor - maxVisible + 1
	}

	curY := int16(19)
	for i := startIdx; i < len(items) && i < startIdx+maxVisible; i++ {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, prefix+items[i], white)
		curY += 9
	}
	_ = d.Flush()
}

// RenderText renders multiple lines of text
func (d *OLEDDisplay) RenderText(title string, lines []string) {
	d.Clear()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, 8, title, white)
	curY := int16(18)

	for _, line := range lines {
		tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, curY, line, white)
		curY += 9
	}
	_ = d.Flush()
}

// RenderConfirm renders confirmation dialog
func (d *OLEDDisplay) RenderConfirm(message string) {
	d.Clear()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, 20, message, white)
	tinyfont.WriteLine(d, &proggy.TinySZ8pt7b, 0, 40, "Enter=Yes Esc=No", white)
	_ = d.Flush()
}

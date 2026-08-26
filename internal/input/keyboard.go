package input

import (
	"encoding/binary"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type KeyEvent int

const (
	KeyUp KeyEvent = iota
	KeyDown
	KeyLeft
	KeyRight
	KeyEnter
	KeyEsc
	KeyOther
)

const (
	evKey    uint16 = 0x01
	keyEsc   uint16 = 1
	keyEnter uint16 = 28
	keyUp    uint16 = 103
	keyDown  uint16 = 108
	keyLeft  uint16 = 105
	keyRight uint16 = 106
)

type KeyboardReader struct {
	events       chan KeyEvent
	done         chan struct{}
	logger       *slog.Logger
	mu           sync.Mutex
	activeDevs   map[string]bool
	running      bool
}

func NewKeyboardReader() *KeyboardReader {
	return &KeyboardReader{
		events:     make(chan KeyEvent, 32),
		done:       make(chan struct{}),
		logger:     slog.Default().With("component", "keyboard"),
		activeDevs: make(map[string]bool),
	}
}

// findKeyboardDevices scans /proc/bus/input/devices and /dev/input to find all keyboard devices
func (k *KeyboardReader) findKeyboardDevices() []string {
	var results []string
	seen := make(map[string]bool)

	// 1. Scan /proc/bus/input/devices for Handlers containing "kbd"
	if data, err := os.ReadFile("/proc/bus/input/devices"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "H: Handlers=") && strings.Contains(line, "kbd") {
				fields := strings.Fields(line)
				for _, f := range fields {
					if strings.HasPrefix(f, "event") {
						p := "/dev/input/" + f
						if !seen[p] {
							seen[p] = true
							results = append(results, p)
						}
					}
				}
			}
		}
	}

	// 2. Scan /dev/input/by-id/*kbd*
	if matches, err := filepath.Glob("/dev/input/by-id/*kbd*"); err == nil {
		for _, m := range matches {
			realPath, err := filepath.EvalSymlinks(m)
			if err == nil && !seen[realPath] {
				seen[realPath] = true
				results = append(results, realPath)
			}
		}
	}

	// 3. Fallback: if no specific kbd found, check all /dev/input/event*
	if len(results) == 0 {
		if matches, err := filepath.Glob("/dev/input/event*"); err == nil {
			for _, m := range matches {
				if !seen[m] {
					seen[m] = true
					results = append(results, m)
				}
			}
		}
	}

	return results
}

func (k *KeyboardReader) Start() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.running {
		return nil
	}

	k.running = true
	k.done = make(chan struct{})

	// Start device scanner & hotplug manager goroutine
	go k.scanLoop()

	k.logger.Info("Keyboard reader started with hotplug monitoring")
	return nil
}

func (k *KeyboardReader) scanLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial scan
	k.scanAndAttach()

	for {
		select {
		case <-k.done:
			return
		case <-ticker.C:
			k.scanAndAttach()
		}
	}
}

func (k *KeyboardReader) scanAndAttach() {
	devices := k.findKeyboardDevices()
	for _, devPath := range devices {
		k.mu.Lock()
		if k.activeDevs[devPath] {
			k.mu.Unlock()
			continue
		}
		k.activeDevs[devPath] = true
		k.mu.Unlock()

		go k.readDevice(devPath)
	}
}

func (k *KeyboardReader) readDevice(devPath string) {
	defer func() {
		k.mu.Lock()
		delete(k.activeDevs, devPath)
		k.mu.Unlock()
	}()

	f, err := os.Open(devPath)
	if err != nil {
		return
	}
	defer f.Close()

	k.logger.Info("Listening on keyboard device", "path", devPath)

	buf := make([]byte, 48) // Buffer capable of holding 32-bit (16B) or 64-bit (24B) input_event

	for {
		select {
		case <-k.done:
			return
		default:
		}

		n, err := f.Read(buf)
		if err != nil {
			k.logger.Debug("Device disconnected", "path", devPath, "err", err)
			return
		}

		// Process stream of events in buffer
		offset := 0
		for offset < n {
			remaining := buf[offset:n]
			typ, code, val, bytesRead := parseInputEvent(remaining)
			if bytesRead == 0 {
				break
			}
			offset += bytesRead

			if typ == evKey && val == 1 { // Key press down
				var keyEvent KeyEvent
				switch code {
				case keyUp:
					keyEvent = KeyUp
				case keyDown:
					keyEvent = KeyDown
				case keyLeft:
					keyEvent = KeyLeft
				case keyRight:
					keyEvent = KeyRight
				case keyEnter:
					keyEvent = KeyEnter
				case keyEsc:
					keyEvent = KeyEsc
				default:
					keyEvent = KeyOther
				}

				k.logger.Debug("Key pressed", "code", code, "event", keyEvent)

				select {
				case k.events <- keyEvent:
				default:
					// Drop if full
				}
			}
		}
	}
}

// parseInputEvent dynamically handles 32-bit (16 bytes) and 64-bit (24 bytes) struct input_event
func parseInputEvent(b []byte) (typ uint16, code uint16, val int32, bytesRead int) {
	// Try 64-bit Linux input_event (24 bytes: timeval 16B + type 2B + code 2B + value 4B)
	if len(b) >= 24 {
		t := binary.LittleEndian.Uint16(b[16:18])
		c := binary.LittleEndian.Uint16(b[18:20])
		v := int32(binary.LittleEndian.Uint32(b[20:24]))
		// Sanity check: valid EV_KEY is 0x01, EV_SYN is 0x00, EV_MSC is 0x04
		if t <= 0x1F {
			return t, c, v, 24
		}
	}

	// Try 32-bit Linux input_event (16 bytes: timeval 8B + type 2B + code 2B + value 4B)
	if len(b) >= 16 {
		t := binary.LittleEndian.Uint16(b[8:10])
		c := binary.LittleEndian.Uint16(b[10:12])
		v := int32(binary.LittleEndian.Uint32(b[12:16]))
		if t <= 0x1F {
			return t, c, v, 16
		}
	}

	return 0, 0, 0, 0
}

func (k *KeyboardReader) Events() <-chan KeyEvent {
	return k.events
}

func (k *KeyboardReader) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.running {
		return
	}

	close(k.done)
	k.running = false
}

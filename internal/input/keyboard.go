package input

import (
	"encoding/binary"
	"errors"
	"fmt"
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

type inputEvent struct {
	TimeSec  uint64 // 8 bytes
	TimeUsec uint64 // 8 bytes
	Type     uint16 // 2 bytes
	Code     uint16 // 2 bytes
	Value    int32  // 4 bytes
} // Total: 24 bytes

type KeyboardReader struct {
	events   chan KeyEvent
	done     chan struct{}
	logger   *slog.Logger
	deviceFn string
	mu       sync.Mutex
	running  bool
}

func NewKeyboardReader() *KeyboardReader {
	return &KeyboardReader{
		events: make(chan KeyEvent, 10),
		done:   make(chan struct{}),
		logger: slog.Default().With("component", "input"),
	}
}

func (k *KeyboardReader) findDevice() string {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return ""
	}
	for _, m := range matches {
		if strings.Contains(m, "event") {
			return m
		}
	}
	return ""
}

func (k *KeyboardReader) Start() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.running {
		return errors.New("keyboard reader already running")
	}

	devPath := k.findDevice()
	if devPath == "" {
		return errors.New("no keyboard device found")
	}

	f, err := os.Open(devPath)
	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}

	k.deviceFn = devPath
	k.running = true
	k.done = make(chan struct{})

	go k.readLoop(f)

	return nil
}

func (k *KeyboardReader) readLoop(f *os.File) {
	defer f.Close()
	defer func() {
		k.mu.Lock()
		k.running = false
		k.mu.Unlock()
	}()

	var event inputEvent
	for {
		select {
		case <-k.done:
			return
		default:
		}

		err := binary.Read(f, binary.LittleEndian, &event)
		if err != nil {
			k.logger.Error("failed to read event", "error", err)
			time.Sleep(time.Second) // Prevent tight loop on error
			continue
		}

		if event.Type == evKey && event.Value == 1 { // Key down only
			var keyEvent KeyEvent
			switch event.Code {
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

			select {
			case k.events <- keyEvent:
			default:
				// Channel full, drop event
			}
		}
	}
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
}

package serial

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Port represents a serial port with mutex protection.
type Port struct {
	mu   sync.Mutex
	port serial.Port
}

// Open opens a serial port with the given name and baud rate (8N1).
func Open(portName string, baudRate int) (*Port, error) {
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return &Port{port: p}, nil
}

// Lock acquires the lock for exclusive access.
func (p *Port) Lock() {
	p.mu.Lock()
}

// Unlock releases the lock.
func (p *Port) Unlock() {
	p.mu.Unlock()
}

// Write writes data to the port.
func (p *Port) Write(b []byte) (int, error) {
	return p.port.Write(b)
}

// Read reads data from the port.
func (p *Port) Read(b []byte) (int, error) {
	return p.port.Read(b)
}

// ReadFull reads exactly len(b) bytes or times out.
func (p *Port) ReadFull(b []byte, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	total := 0
	for total < len(b) {
		now := time.Now()
		if now.After(deadline) {
			return total, fmt.Errorf("timeout reading from serial port")
		}
		p.port.SetReadTimeout(deadline.Sub(now))
		n, err := p.port.Read(b[total:])
		if err != nil {
			return total, err
		}
		if n == 0 {
			// Sleep a bit to prevent tight loop if nothing read yet
			time.Sleep(10 * time.Millisecond)
		}
		total += n
	}
	return total, nil
}

// SetReadTimeout sets the read timeout for the port.
func (p *Port) SetReadTimeout(t time.Duration) error {
	return p.port.SetReadTimeout(t)
}

// Flush clears the input buffer.
func (p *Port) Flush() error {
	return p.port.ResetInputBuffer()
}

// Close closes the serial port.
func (p *Port) Close() error {
	return p.port.Close()
}

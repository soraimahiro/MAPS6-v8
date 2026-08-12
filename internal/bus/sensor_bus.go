package bus

import (
	"sync"
	
	"maps6/internal/mcu"
)

// SensorBus implements a publish/subscribe bus for sensor data.
type SensorBus struct {
	subscribers map[string]chan mcu.SensorData
	latest      mcu.SensorData
	mu          sync.RWMutex
}

// NewSensorBus creates a new SensorBus.
func NewSensorBus() *SensorBus {
	return &SensorBus{
		subscribers: make(map[string]chan mcu.SensorData),
	}
}

// Subscribe adds a new subscriber and returns a channel to receive data on.
func (b *SensorBus) Subscribe(name string, bufSize int) <-chan mcu.SensorData {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan mcu.SensorData, bufSize)
	b.subscribers[name] = ch
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *SensorBus) Unsubscribe(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subscribers[name]; ok {
		delete(b.subscribers, name)
		close(ch)
	}
}

// Publish distributes the sensor data to all active subscribers.
func (b *SensorBus) Publish(data mcu.SensorData) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latest = data
	for name, ch := range b.subscribers {
		select {
		case ch <- data:
			// Sent successfully
		default:
			// Skip slow consumers to avoid blocking the bus
			_ = name // Silently drop or log if desired
		}
	}
}

// Latest returns a copy of the most recently published sensor data.
func (b *SensorBus) Latest() mcu.SensorData {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.latest
}

package module

import "context"

// Module defines the interface for all system modules.
type Module interface {
	// Name returns the unique identifier for the module.
	Name() string
	// Start starts the module's background routines.
	Start(ctx context.Context) error
	// Stop stops the module gracefully.
	Stop() error
	// Status returns the current status of the module.
	Status() ModuleStatus
}

// ModuleStatus represents the current state of a module.
type ModuleStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	LastError string `json:"last_error,omitempty"`
}

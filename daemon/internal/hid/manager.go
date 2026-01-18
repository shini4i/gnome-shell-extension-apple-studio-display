// SPDX-License-Identifier: GPL-3.0-only

package hid

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// Manager handles the lifecycle of multiple Apple Studio Displays.
type Manager struct {
	displays   map[string]*Display // serial -> display
	mu         sync.RWMutex
	enumerator func() ([]DeviceInfo, error)
	opener     func(serial string) (Device, error)
}

// ManagerOption is a functional option for configuring a Manager.
type ManagerOption func(*Manager)

// WithEnumerator sets a custom device enumerator for testing.
func WithEnumerator(fn func() ([]DeviceInfo, error)) ManagerOption {
	return func(m *Manager) {
		m.enumerator = fn
	}
}

// WithOpener sets a custom device opener for testing.
func WithOpener(fn func(serial string) (Device, error)) ManagerOption {
	return func(m *Manager) {
		m.opener = fn
	}
}

// NewManager creates a new display manager.
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		displays:   make(map[string]*Display),
		enumerator: EnumerateDisplays,
		opener:     defaultOpener,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// defaultOpener wraps OpenDisplay to match the expected signature.
func defaultOpener(serial string) (Device, error) {
	return OpenDisplay(serial)
}

// ListDisplays returns information about all connected displays.
func (m *Manager) ListDisplays() []DeviceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]DeviceInfo, 0, len(m.displays))
	for _, d := range m.displays {
		infos = append(infos, d.device.Info())
	}
	return infos
}

// GetDisplay returns a display by serial number.
func (m *Manager) GetDisplay(serial string) (*Display, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	display, ok := m.displays[serial]
	if !ok {
		return nil, fmt.Errorf("display with serial %s not found", serial)
	}
	return display, nil
}

// RefreshDisplays re-enumerates connected displays and updates the internal state.
// It opens new displays and closes disconnected ones.
func (m *Manager) RefreshDisplays() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Enumerate current displays
	currentDevices, err := m.enumerator()
	if err != nil {
		return fmt.Errorf("failed to enumerate displays: %w", err)
	}

	currentSerials := make(map[string]DeviceInfo)
	for _, info := range currentDevices {
		currentSerials[info.Serial] = info
	}

	// Find and close disconnected displays
	for serial, display := range m.displays {
		if _, exists := currentSerials[serial]; !exists {
			log.Info().Str("serial", serial).Msg("Display disconnected")
			if err := display.Close(); err != nil {
				log.Warn().Err(err).Str("serial", serial).Msg("Failed to close disconnected display")
			}
			delete(m.displays, serial)
		}
	}

	// Open new displays
	for serial, info := range currentSerials {
		if _, exists := m.displays[serial]; !exists {
			device, err := m.opener(serial)
			if err != nil {
				log.Error().Err(err).Str("serial", serial).Msg("Failed to open display")
				continue
			}
			m.displays[serial] = NewDisplay(device)
			log.Info().Str("serial", serial).Str("product", info.Product).Msg("Display connected")
		}
	}

	return nil
}

// Close closes all open displays.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for serial, display := range m.displays {
		if err := display.Close(); err != nil {
			log.Error().Err(err).Str("serial", serial).Msg("Failed to close display")
		}
		delete(m.displays, serial)
	}
	return nil
}

// Count returns the number of connected displays.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.displays)
}

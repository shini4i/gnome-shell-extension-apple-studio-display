// Package udev provides hot-plug detection for Apple Studio Displays via netlink/udev events.
package udev

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"

	"github.com/pilebones/go-udev/netlink"
	"github.com/rs/zerolog/log"
)

const (
	// netlinkBufferSize is the receive buffer size for the netlink socket.
	// A larger buffer prevents ENOBUFS errors during USB hot-plug events.
	// USB hot-plug generates many netlink messages rapidly; 2MB handles typical scenarios.
	netlinkBufferSize = 2 * 1024 * 1024 // 2 MB
)

const (
	// AppleVendorID is the USB vendor ID for Apple devices (udev format, no leading zero).
	AppleVendorID = "5ac"

	// StudioDisplayProductID is the USB product ID for Apple Studio Display.
	StudioDisplayProductID = "1114"
)

// EventType represents the type of device event.
type EventType int

const (
	// EventAdd indicates a device was connected.
	EventAdd EventType = iota
	// EventRemove indicates a device was disconnected.
	EventRemove
)

// Event represents a device hot-plug event.
type Event struct {
	Type EventType
}

// EventHandler is called when a device event occurs.
type EventHandler func(event Event)

// RecoveryHandler is called when the monitor recovers from an error condition
// (e.g., netlink buffer overflow) and needs to trigger a refresh.
type RecoveryHandler func()

// Monitor watches for Apple Studio Display connect/disconnect events.
type Monitor struct {
	conn            *netlink.UEventConn
	handler         EventHandler
	recoveryHandler RecoveryHandler
	quit            chan struct{}
	stopped         bool
	mu              sync.Mutex
}

// NewMonitor creates a new udev monitor with the given event handler.
func NewMonitor(handler EventHandler) *Monitor {
	return &Monitor{
		handler: handler,
	}
}

// SetRecoveryHandler sets the handler called when the monitor recovers from errors.
// This should trigger a display refresh to recover from potentially missed events.
func (m *Monitor) SetRecoveryHandler(handler RecoveryHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoveryHandler = handler
}

// Start begins monitoring for device events.
// This method is non-blocking; events are processed in a background goroutine.
func (m *Monitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn != nil {
		return fmt.Errorf("monitor already started")
	}

	m.conn = &netlink.UEventConn{}
	if err := m.conn.Connect(netlink.UdevEvent); err != nil {
		m.conn = nil
		return fmt.Errorf("failed to connect to netlink: %w", err)
	}

	// Increase socket receive buffer to prevent ENOBUFS during rapid USB hot-plug events
	if err := setSocketBufferSize(m.conn.Fd, netlinkBufferSize); err != nil {
		log.Warn().Err(err).Int("size", netlinkBufferSize).Msg("Failed to set netlink buffer size")
		// Continue anyway - the default buffer may still work for most cases
	} else {
		log.Debug().Int("size", netlinkBufferSize).Msg("Netlink socket buffer size configured")
	}

	queue := make(chan netlink.UEvent)
	errs := make(chan error)

	// Create matcher for Apple Studio Display USB events
	matcher := m.createMatcher()

	m.quit = m.conn.Monitor(queue, errs, matcher)
	m.stopped = false

	go m.processEvents(queue, errs)

	log.Info().Msg("udev monitor started")
	return nil
}

// Stop stops the monitor and releases resources.
func (m *Monitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil || m.stopped {
		return nil
	}

	m.stopped = true

	// Signal the monitor goroutine to stop
	select {
	case m.quit <- struct{}{}:
	default:
	}

	if err := m.conn.Close(); err != nil {
		return fmt.Errorf("failed to close netlink connection: %w", err)
	}

	m.conn = nil
	log.Info().Msg("udev monitor stopped")
	return nil
}

// createMatcher creates a matcher for Apple Studio Display events.
func (m *Monitor) createMatcher() *netlink.RuleDefinitions {
	rules := &netlink.RuleDefinitions{}

	// Match add/remove actions for USB devices with Apple vendor ID and Studio Display product ID.
	// The PRODUCT env var format is "vendorId/productId/bcdDevice" (e.g., "5ac/1114/157").
	// We use anchored regex to prevent false positives (e.g., "5ac/11149" should not match).
	addAction := "add"
	removeAction := "remove"

	// Pattern matches exactly: vendorId/productId/anything (anchored)
	productPattern := fmt.Sprintf("^%s/%s/[^/]+$", AppleVendorID, StudioDisplayProductID)

	// Match USB subsystem events for Apple Studio Display
	rules.AddRule(netlink.RuleDefinition{
		Action: &addAction,
		Env: map[string]string{
			"SUBSYSTEM": "^usb$",
			"PRODUCT":   productPattern,
		},
	})

	rules.AddRule(netlink.RuleDefinition{
		Action: &removeAction,
		Env: map[string]string{
			"SUBSYSTEM": "^usb$",
			"PRODUCT":   productPattern,
		},
	})

	return rules
}

// processEvents handles incoming udev events.
func (m *Monitor) processEvents(queue chan netlink.UEvent, errs chan error) {
	for {
		select {
		case event, ok := <-queue:
			if !ok {
				return
			}
			m.handleEvent(event)
		case err, ok := <-errs:
			if !ok {
				return
			}
			// Check if we're stopping
			m.mu.Lock()
			stopped := m.stopped
			recoveryHandler := m.recoveryHandler
			m.mu.Unlock()
			if stopped {
				return
			}

			// Handle netlink buffer overflow (ENOBUFS) gracefully.
			// When this occurs, events may have been dropped, so we trigger
			// a recovery refresh to re-enumerate displays.
			if isBufferOverflowError(err) {
				log.Warn().Msg("Netlink buffer overflow detected, triggering recovery refresh")
				if recoveryHandler != nil {
					go recoveryHandler()
				}
				continue
			}

			log.Error().Err(err).Msg("udev monitor error")
		}
	}
}

// setSocketBufferSize sets the receive buffer size for a socket.
// It first tries SO_RCVBUFFORCE (requires CAP_NET_ADMIN), then falls back to SO_RCVBUF.
func setSocketBufferSize(fd int, size int) error {
	// Try SO_RCVBUFFORCE first - bypasses rmem_max limit (requires CAP_NET_ADMIN)
	err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUFFORCE, size)
	if err == nil {
		return nil
	}

	// Fall back to SO_RCVBUF - limited by net.core.rmem_max sysctl
	// The kernel will cap the value at rmem_max and double it internally
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, size)
}

// isBufferOverflowError checks if the error is a netlink buffer overflow (ENOBUFS).
func isBufferOverflowError(err error) bool {
	if err == nil {
		return false
	}
	// Check for ENOBUFS using errors.Is for wrapped error support
	if errors.Is(err, syscall.ENOBUFS) {
		return true
	}
	// Fallback: check error message for non-wrapped cases from the udev library
	// Use case-insensitive matching for robustness
	return strings.Contains(strings.ToLower(err.Error()), "no buffer space available")
}

// handleEvent processes a single udev event.
func (m *Monitor) handleEvent(uevent netlink.UEvent) {
	// Filter for usb_device type only (not usb_interface) on ADD events.
	// For REMOVE events, DEVTYPE may not be present since the device is already gone,
	// so we skip this check. The matcher already ensures we only receive Studio Display events.
	devtype := uevent.Env["DEVTYPE"]
	if uevent.Action == netlink.ADD && devtype != "usb_device" {
		return
	}

	log.Debug().
		Str("action", string(uevent.Action)).
		Str("devpath", uevent.KObj).
		Str("product", uevent.Env["PRODUCT"]).
		Msg("USB device event")

	var eventType EventType
	switch uevent.Action {
	case netlink.ADD:
		eventType = EventAdd
		log.Info().Str("product", uevent.Env["PRODUCT"]).Msg("Apple Studio Display connected")
	case netlink.REMOVE:
		eventType = EventRemove
		log.Info().Str("product", uevent.Env["PRODUCT"]).Msg("Apple Studio Display disconnected")
	default:
		return
	}

	if m.handler != nil {
		m.handler(Event{Type: eventType})
	}
}

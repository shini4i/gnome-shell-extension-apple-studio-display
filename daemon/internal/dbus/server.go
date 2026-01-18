// SPDX-License-Identifier: GPL-3.0-only

// Package dbus provides the D-Bus service implementation for Apple Studio Display brightness control.
package dbus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/asd-brightness-daemon/internal/hid"
	"golang.org/x/time/rate"
)

// ErrEmptySerial is returned when an empty serial number is provided.
var ErrEmptySerial = errors.New("serial cannot be empty")

// ErrRateLimitExceeded is returned when brightness change requests exceed the rate limit.
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// ErrInvalidStep is returned when an invalid brightness step value is provided.
var ErrInvalidStep = errors.New("step must be between 1 and 100")

const (
	// rateLimitPerSecond is the maximum number of brightness changes per second.
	rateLimitPerSecond = 20

	// rateLimitBurst is the maximum burst size for brightness changes.
	rateLimitBurst = 5
)

const (
	// ServiceName is the D-Bus service name.
	ServiceName = "io.github.shini4i.AsdBrightness"

	// ObjectPath is the D-Bus object path.
	ObjectPath = "/io/github/shini4i/AsdBrightness"

	// InterfaceName is the D-Bus interface name.
	InterfaceName = "io.github.shini4i.AsdBrightness"
)

// IntrospectXML is the D-Bus introspection XML for the service.
const IntrospectXML = `
<node name="` + ObjectPath + `">
  <interface name="` + InterfaceName + `">
    <method name="ListDisplays">
      <arg name="displays" type="a(ss)" direction="out"/>
    </method>
    <method name="GetBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="brightness" type="u" direction="out"/>
    </method>
    <method name="SetBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="brightness" type="u" direction="in"/>
    </method>
    <method name="IncreaseBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="step" type="u" direction="in"/>
    </method>
    <method name="DecreaseBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="step" type="u" direction="in"/>
    </method>
    <method name="SetAllBrightness">
      <arg name="brightness" type="u" direction="in"/>
    </method>
    <signal name="DisplayAdded">
      <arg name="serial" type="s"/>
      <arg name="productName" type="s"/>
    </signal>
    <signal name="DisplayRemoved">
      <arg name="serial" type="s"/>
    </signal>
    <signal name="BrightnessChanged">
      <arg name="serial" type="s"/>
      <arg name="brightness" type="u"/>
    </signal>
  </interface>
  ` + introspect.IntrospectDataString + `
</node>
`

// DisplayManager is an interface for managing displays.
// This allows for mocking in tests.
type DisplayManager interface {
	// ListDisplays returns information about all connected displays.
	ListDisplays() []hid.DeviceInfo

	// GetDisplay returns a display by serial number.
	GetDisplay(serial string) (*hid.Display, error)

	// RefreshDisplays re-enumerates connected displays.
	RefreshDisplays() error
}

// DeviceErrorHandler is called when a device error (e.g., device disconnected) is detected.
// This allows the caller to trigger recovery actions like re-enumerating displays.
type DeviceErrorHandler func(serial string, err error)

// DisplayInfo represents display information returned via D-Bus.
// Serializes to D-Bus type (ss) - a struct containing serial and product name.
type DisplayInfo struct {
	Serial      string
	ProductName string
}

// Server implements the D-Bus service for brightness control.
//
// Thread safety:
//   - The underlying Manager and Display types are individually thread-safe.
//   - The connMu mutex protects the D-Bus connection field for signal emission.
//   - The handlerMu mutex protects the deviceErrorHandler field.
//   - Note: IncreaseBrightness and DecreaseBrightness perform non-atomic
//     read-modify-write operations. Concurrent calls may result in missed
//     increments. This is acceptable for typical keyboard shortcut usage.
type Server struct {
	conn               *dbus.Conn
	connMu             sync.RWMutex // Protects conn field only
	manager            DisplayManager
	rateLimiter        *rate.Limiter
	handlerMu          sync.RWMutex // Protects deviceErrorHandler
	deviceErrorHandler DeviceErrorHandler
}

// NewServer creates a new D-Bus server with the given display manager.
func NewServer(manager DisplayManager) *Server {
	return &Server{
		manager:     manager,
		rateLimiter: rate.NewLimiter(rateLimitPerSecond, rateLimitBurst),
	}
}

// Start connects to the session bus and exports the service.
func (s *Server) Start() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("failed to connect to session bus: %w", err)
	}

	// Ensure connection is closed if setup fails
	success := false
	defer func() {
		if !success {
			if closeErr := conn.Close(); closeErr != nil {
				log.Error().Err(closeErr).Msg("Failed to close D-Bus connection during cleanup")
			}
		}
	}()

	// Export the server object
	err = conn.Export(s, ObjectPath, InterfaceName)
	if err != nil {
		return fmt.Errorf("failed to export server: %w", err)
	}

	// Export introspectable interface
	err = conn.Export(introspect.Introspectable(IntrospectXML), ObjectPath, "org.freedesktop.DBus.Introspectable")
	if err != nil {
		return fmt.Errorf("failed to export introspectable: %w", err)
	}

	// Request the service name
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("failed to request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("name %s already taken", ServiceName)
	}

	// Store connection with mutex protection
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()

	success = true
	log.Info().Str("service", ServiceName).Msg("D-Bus service started")
	return nil
}

// Stop disconnects from the session bus.
func (s *Server) Stop() error {
	s.connMu.Lock()
	conn := s.conn
	s.conn = nil
	s.connMu.Unlock()

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// SetDeviceErrorHandler sets the callback invoked when device errors are detected.
// This is typically used to trigger recovery actions like re-enumerating displays
// when a device is found to be disconnected during brightness operations.
//
// This method is thread-safe and can be called at any time.
func (s *Server) SetDeviceErrorHandler(handler DeviceErrorHandler) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.deviceErrorHandler = handler
}

// handleDeviceError checks if the error indicates a disconnected device and triggers recovery.
// Returns true if the error was a device error and recovery was triggered.
func (s *Server) handleDeviceError(serial string, err error) bool {
	if err == nil || !hid.IsDeviceGoneError(err) {
		return false
	}

	log.Warn().
		Err(err).
		Str("serial", serial).
		Msg("Device error detected, triggering recovery")

	s.handlerMu.RLock()
	handler := s.deviceErrorHandler
	s.handlerMu.RUnlock()

	if handler != nil {
		// Run recovery asynchronously to not block the D-Bus response
		go handler(serial, err)
	}

	return true
}

// ListDisplays returns a list of all connected displays.
// Returns an array of structs: [{Serial, ProductName}, ...]
func (s *Server) ListDisplays() ([]DisplayInfo, *dbus.Error) {
	displays := s.manager.ListDisplays()
	result := make([]DisplayInfo, len(displays))
	for i, d := range displays {
		result[i] = DisplayInfo{Serial: d.Serial, ProductName: d.Product}
	}

	log.Debug().Int("count", len(result)).Msg("Listed displays")
	return result, nil
}

// GetBrightness returns the brightness of a display as a percentage (0-100).
func (s *Server) GetBrightness(serial string) (uint32, *dbus.Error) {
	if serial == "" {
		return 0, dbus.MakeFailedError(ErrEmptySerial)
	}

	display, err := s.manager.GetDisplay(serial)
	if err != nil {
		log.Error().Err(err).Str("serial", serial).Msg("Failed to get display")
		return 0, dbus.MakeFailedError(err)
	}

	brightness, err := display.GetBrightness()
	if err != nil {
		s.handleDeviceError(serial, err)
		log.Error().Err(err).Str("serial", serial).Msg("Failed to get brightness")
		return 0, dbus.MakeFailedError(err)
	}

	log.Debug().Str("serial", serial).Uint8("brightness", brightness).Msg("Got brightness")
	return uint32(brightness), nil
}

// SetBrightness sets the brightness of a display to a percentage (0-100).
func (s *Server) SetBrightness(serial string, brightness uint32) *dbus.Error {
	if !s.rateLimiter.Allow() {
		log.Warn().Msg("Rate limit exceeded for SetBrightness")
		return dbus.MakeFailedError(ErrRateLimitExceeded)
	}

	if serial == "" {
		return dbus.MakeFailedError(ErrEmptySerial)
	}

	display, err := s.manager.GetDisplay(serial)
	if err != nil {
		log.Error().Err(err).Str("serial", serial).Msg("Failed to get display")
		return dbus.MakeFailedError(err)
	}

	if brightness > 100 {
		brightness = 100
	}

	// #nosec G115 -- brightness is clamped to 0-100, safe for uint8
	err = display.SetBrightness(uint8(brightness))
	if err != nil {
		s.handleDeviceError(serial, err)
		log.Error().Err(err).Str("serial", serial).Msg("Failed to set brightness")
		return dbus.MakeFailedError(err)
	}

	log.Debug().Str("serial", serial).Uint32("brightness", brightness).Msg("Set brightness")

	// Emit signal
	s.emitBrightnessChanged(serial, brightness)

	return nil
}

// IncreaseBrightness increases the brightness of a display by a step.
// The step parameter must be between 1 and 100.
func (s *Server) IncreaseBrightness(serial string, step uint32) *dbus.Error {
	if !s.rateLimiter.Allow() {
		log.Warn().Msg("Rate limit exceeded for IncreaseBrightness")
		return dbus.MakeFailedError(ErrRateLimitExceeded)
	}

	if serial == "" {
		return dbus.MakeFailedError(ErrEmptySerial)
	}

	if step == 0 || step > 100 {
		return dbus.MakeFailedError(ErrInvalidStep)
	}

	display, err := s.manager.GetDisplay(serial)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	current, err := display.GetBrightness()
	if err != nil {
		s.handleDeviceError(serial, err)
		return dbus.MakeFailedError(err)
	}

	newBrightness := uint32(current) + step
	if newBrightness > 100 {
		newBrightness = 100
	}

	// #nosec G115 -- newBrightness is clamped to 0-100, safe for uint8
	err = display.SetBrightness(uint8(newBrightness))
	if err != nil {
		s.handleDeviceError(serial, err)
		return dbus.MakeFailedError(err)
	}

	log.Debug().Str("serial", serial).Uint32("step", step).Uint32("new", newBrightness).Msg("Increased brightness")
	s.emitBrightnessChanged(serial, newBrightness)

	return nil
}

// DecreaseBrightness decreases the brightness of a display by a step.
// The step parameter must be between 1 and 100.
func (s *Server) DecreaseBrightness(serial string, step uint32) *dbus.Error {
	if !s.rateLimiter.Allow() {
		log.Warn().Msg("Rate limit exceeded for DecreaseBrightness")
		return dbus.MakeFailedError(ErrRateLimitExceeded)
	}

	if serial == "" {
		return dbus.MakeFailedError(ErrEmptySerial)
	}

	if step == 0 || step > 100 {
		return dbus.MakeFailedError(ErrInvalidStep)
	}

	display, err := s.manager.GetDisplay(serial)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	current, err := display.GetBrightness()
	if err != nil {
		s.handleDeviceError(serial, err)
		return dbus.MakeFailedError(err)
	}

	var newBrightness uint32
	if uint32(current) > step {
		newBrightness = uint32(current) - step
	} else {
		newBrightness = 0
	}

	// #nosec G115 -- newBrightness is clamped to 0-100, safe for uint8
	err = display.SetBrightness(uint8(newBrightness))
	if err != nil {
		s.handleDeviceError(serial, err)
		return dbus.MakeFailedError(err)
	}

	log.Debug().Str("serial", serial).Uint32("step", step).Uint32("new", newBrightness).Msg("Decreased brightness")
	s.emitBrightnessChanged(serial, newBrightness)

	return nil
}

// SetAllBrightness sets the brightness of all displays to a percentage (0-100).
func (s *Server) SetAllBrightness(brightness uint32) *dbus.Error {
	if !s.rateLimiter.Allow() {
		log.Warn().Msg("Rate limit exceeded for SetAllBrightness")
		return dbus.MakeFailedError(ErrRateLimitExceeded)
	}

	if brightness > 100 {
		brightness = 100
	}

	displays := s.manager.ListDisplays()
	for _, info := range displays {
		display, err := s.manager.GetDisplay(info.Serial)
		if err != nil {
			log.Error().Err(err).Str("serial", info.Serial).Msg("Failed to get display")
			continue
		}

		// #nosec G115 -- brightness is clamped to 0-100, safe for uint8
		err = display.SetBrightness(uint8(brightness))
		if err != nil {
			s.handleDeviceError(info.Serial, err)
			log.Error().Err(err).Str("serial", info.Serial).Msg("Failed to set brightness")
			continue
		}

		s.emitBrightnessChanged(info.Serial, brightness)
	}

	log.Debug().Uint32("brightness", brightness).Int("count", len(displays)).Msg("Set all brightness")
	return nil
}

// emitBrightnessChanged emits the BrightnessChanged signal.
func (s *Server) emitBrightnessChanged(serial string, brightness uint32) {
	s.connMu.RLock()
	conn := s.conn
	s.connMu.RUnlock()

	if conn == nil {
		return
	}

	err := conn.Emit(ObjectPath, InterfaceName+".BrightnessChanged", serial, brightness)
	if err != nil {
		log.Error().Err(err).Msg("Failed to emit BrightnessChanged signal")
	}
}

// EmitDisplayAdded emits the DisplayAdded signal.
func (s *Server) EmitDisplayAdded(serial, productName string) {
	s.connMu.RLock()
	conn := s.conn
	s.connMu.RUnlock()

	if conn == nil {
		return
	}

	err := conn.Emit(ObjectPath, InterfaceName+".DisplayAdded", serial, productName)
	if err != nil {
		log.Error().Err(err).Msg("Failed to emit DisplayAdded signal")
	}
	log.Info().Str("serial", serial).Str("product", productName).Msg("Display added")
}

// EmitDisplayRemoved emits the DisplayRemoved signal.
func (s *Server) EmitDisplayRemoved(serial string) {
	s.connMu.RLock()
	conn := s.conn
	s.connMu.RUnlock()

	if conn == nil {
		return
	}

	err := conn.Emit(ObjectPath, InterfaceName+".DisplayRemoved", serial)
	if err != nil {
		log.Error().Err(err).Msg("Failed to emit DisplayRemoved signal")
	}
	log.Info().Str("serial", serial).Msg("Display removed")
}

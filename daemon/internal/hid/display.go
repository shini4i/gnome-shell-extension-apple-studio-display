package hid

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/shini4i/asd-brightness-daemon/internal/brightness"
)

const (
	// ReportID is the HID report ID for brightness control.
	ReportID byte = 0x01

	// ReportSize is the size of the HID feature report in bytes.
	ReportSize = 7

	// AppleVendorID is the USB vendor ID for Apple.
	AppleVendorID uint16 = 0x05ac

	// StudioDisplayProductID is the USB product ID for Apple Studio Display.
	StudioDisplayProductID uint16 = 0x1114

	// BrightnessInterface is the USB interface number for brightness control.
	BrightnessInterface = 0x07
)

// Display represents an Apple Studio Display with brightness control capabilities.
// All methods are thread-safe and can be called concurrently.
type Display struct {
	device Device
	mu     sync.Mutex
	closed bool
}

// NewDisplay creates a new Display instance wrapping the given HID device.
func NewDisplay(device Device) *Display {
	return &Display{device: device}
}

// ErrDisplayClosed is returned when an operation is attempted on a closed display.
var ErrDisplayClosed = fmt.Errorf("display is closed")

// GetBrightness reads the current brightness from the display and returns it as a percentage (0-100).
func (d *Display) GetBrightness() (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return 0, ErrDisplayClosed
	}

	data := make([]byte, ReportSize)
	data[0] = ReportID

	_, err := d.device.GetFeatureReport(data)
	if err != nil {
		return 0, fmt.Errorf("failed to get feature report: %w", err)
	}

	// Parse brightness value from little-endian bytes
	nits := binary.LittleEndian.Uint32(data[1:5])
	percent := brightness.NitsToPercent(nits)

	return percent, nil
}

// SetBrightness sets the display brightness to the specified percentage (0-100).
func (d *Display) SetBrightness(percent uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return ErrDisplayClosed
	}

	nits := brightness.PercentToNits(percent)

	data := make([]byte, ReportSize)
	data[0] = ReportID
	binary.LittleEndian.PutUint32(data[1:5], nits)

	_, err := d.device.SendFeatureReport(data)
	if err != nil {
		return fmt.Errorf("failed to send feature report: %w", err)
	}

	return nil
}

// Serial returns the serial number of the display.
// This method does not require locking as device info is immutable.
func (d *Display) Serial() string {
	return d.device.Info().Serial
}

// ProductName returns the product name of the display.
// This method does not require locking as device info is immutable.
func (d *Display) ProductName() string {
	return d.device.Info().Product
}

// Close closes the underlying HID device.
func (d *Display) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil // Already closed
	}

	d.closed = true
	return d.device.Close()
}

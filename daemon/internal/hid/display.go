package hid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"

	"github.com/shini4i/asd-brightness-daemon/internal/brightness"
)

const (
	// ReportID is the HID report ID for brightness control.
	ReportID byte = 0x01

	// ReportSize is the size of the HID feature report in bytes.
	ReportSize = 7

	// ReportOffsetNits is the byte offset where the nits value starts in the HID report.
	ReportOffsetNits = 1

	// ReportLenNits is the length in bytes of the nits value in the HID report.
	ReportLenNits = 4

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
	nits := binary.LittleEndian.Uint32(data[ReportOffsetNits : ReportOffsetNits+ReportLenNits])
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
	binary.LittleEndian.PutUint32(data[ReportOffsetNits:ReportOffsetNits+ReportLenNits], nits)

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

// IsDeviceGoneError checks if an error indicates that the HID device is no longer available.
// This typically happens when a USB device is physically disconnected.
// Common causes:
//   - ENODEV (errno 19): Device has been removed
//   - ENOENT (errno 2): Device node removed from /dev
//   - EIO (errno 5): I/O error during device communication (often mid-disconnect)
//   - "No such device": Device path no longer exists
//   - "No such file or directory": Device node removed from /dev
func IsDeviceGoneError(err error) bool {
	if err == nil {
		return false
	}

	// Check for ENODEV syscall error (device removed)
	if errors.Is(err, syscall.ENODEV) {
		return true
	}

	// Check for ENOENT (file/device node removed)
	if errors.Is(err, syscall.ENOENT) {
		return true
	}

	// Check for EIO (I/O error - common during device disconnect mid-operation)
	if errors.Is(err, syscall.EIO) {
		return true
	}

	// Fallback: check error message for common device-gone patterns
	errMsg := strings.ToLower(err.Error())
	deviceGonePatterns := []string{
		"no such device",
		"no such file or directory",
		"device not configured",
		"bad file descriptor",
	}

	for _, pattern := range deviceGonePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

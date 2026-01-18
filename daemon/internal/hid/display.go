// SPDX-License-Identifier: GPL-3.0-only

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

// HID Feature Report Structure for Apple Studio Display Brightness Control
//
// The brightness is controlled via a 7-byte HID feature report with the following layout:
//
//	Byte 0:     Report ID (0x01)
//	Bytes 1-4:  Brightness value in nits (little-endian uint32)
//	Bytes 5-6:  Reserved/unused
//
// The brightness value is stored as an internal brightness unit (not a percentage).
// Valid range is MinBrightness to MaxBrightness (see brightness package constants).
// The daemon converts between internal units and percentage (0-100) for the D-Bus API.
const (
	// ReportID is the HID report ID for brightness control (always 0x01).
	ReportID byte = 0x01

	// ReportSize is the total size of the HID feature report in bytes.
	// Layout: [ReportID(1)] [Nits(4)] [Reserved(2)] = 7 bytes
	ReportSize = 7

	// ReportOffsetNits is the byte offset where the nits value starts in the HID report.
	ReportOffsetNits = 1

	// ReportLenNits is the length in bytes of the nits value (little-endian uint32).
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
var ErrDisplayClosed = errors.New("display is closed")

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

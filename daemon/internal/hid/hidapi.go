// SPDX-License-Identifier: GPL-3.0-only

// hidapi.go provides the hidraw-based HID device implementation for Linux.
package hid

import (
	"errors"
	"fmt"

	hid "github.com/sstallion/go-hid"
)

// errFound is a sentinel error used to stop enumeration early.
var errFound = errors.New("found")

// HIDAPIDevice wraps a sstallion/go-hid device to implement the Device interface.
type HIDAPIDevice struct {
	device *hid.Device
	info   DeviceInfo
}

// Verify HIDAPIDevice implements Device interface.
var _ Device = (*HIDAPIDevice)(nil)

// NewHIDAPIDevice creates a new HIDAPIDevice from an open hid.Device.
func NewHIDAPIDevice(device *hid.Device, info DeviceInfo) *HIDAPIDevice {
	return &HIDAPIDevice{
		device: device,
		info:   info,
	}
}

// GetFeatureReport reads a feature report from the device.
func (d *HIDAPIDevice) GetFeatureReport(data []byte) (int, error) {
	return d.device.GetFeatureReport(data)
}

// SendFeatureReport writes a feature report to the device.
func (d *HIDAPIDevice) SendFeatureReport(data []byte) (int, error) {
	return d.device.SendFeatureReport(data)
}

// Close closes the device handle.
func (d *HIDAPIDevice) Close() error {
	return d.device.Close()
}

// Info returns information about the device.
func (d *HIDAPIDevice) Info() DeviceInfo {
	return d.info
}

// EnumerateDisplays returns a list of all connected Apple Studio Displays.
// Returns an error if device enumeration fails.
// Note: Devices with empty serial numbers are skipped as they may be in a transitional
// state during connect/disconnect and cannot be reliably identified or opened.
func EnumerateDisplays() ([]DeviceInfo, error) {
	var displays []DeviceInfo

	err := hid.Enumerate(AppleVendorID, StudioDisplayProductID, func(info *hid.DeviceInfo) error {
		// Skip devices that don't match the brightness interface
		if info.InterfaceNbr != BrightnessInterface {
			return nil
		}

		// Skip devices with empty serial numbers - these are in a transitional state
		// during connect/disconnect and cannot be reliably used
		if info.SerialNbr == "" {
			return nil
		}

		displays = append(displays, DeviceInfo{
			Path:         info.Path,
			VendorID:     info.VendorID,
			ProductID:    info.ProductID,
			Serial:       info.SerialNbr,
			Manufacturer: info.MfrStr,
			Product:      info.ProductStr,
			Interface:    info.InterfaceNbr,
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to enumerate HID devices: %w", err)
	}

	return displays, nil
}

// OpenDisplay opens a connection to an Apple Studio Display by serial number.
// If serial is empty, opens the first available display.
func OpenDisplay(serial string) (*HIDAPIDevice, error) {
	var targetInfo *DeviceInfo

	err := hid.Enumerate(AppleVendorID, StudioDisplayProductID, func(info *hid.DeviceInfo) error {
		if info.InterfaceNbr != BrightnessInterface {
			return nil
		}

		// Skip devices with empty serial numbers - these are in a transitional state
		// during connect/disconnect and cannot be reliably used
		if info.SerialNbr == "" {
			return nil
		}

		if serial != "" && info.SerialNbr != serial {
			return nil
		}

		targetInfo = &DeviceInfo{
			Path:         info.Path,
			VendorID:     info.VendorID,
			ProductID:    info.ProductID,
			Serial:       info.SerialNbr,
			Manufacturer: info.MfrStr,
			Product:      info.ProductStr,
			Interface:    info.InterfaceNbr,
		}
		return errFound // Stop enumeration
	})

	// Check for real errors (not our sentinel)
	if err != nil && !errors.Is(err, errFound) {
		return nil, fmt.Errorf("failed to enumerate devices: %w", err)
	}

	if targetInfo == nil {
		if serial != "" {
			return nil, fmt.Errorf("display with serial %s not found", serial)
		}
		return nil, fmt.Errorf("no Apple Studio Display found")
	}

	// Open by path (sstallion/go-hid way)
	device, err := hid.OpenPath(targetInfo.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open display %s: %w", targetInfo.Serial, err)
	}

	return NewHIDAPIDevice(device, *targetInfo), nil
}

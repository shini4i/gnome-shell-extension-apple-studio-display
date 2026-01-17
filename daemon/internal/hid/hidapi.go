package hid

import (
	"fmt"

	karalabehid "github.com/karalabe/hid"
)

// HIDAPIDevice wraps a karalabe/hid device to implement the Device interface.
type HIDAPIDevice struct {
	device karalabehid.Device // karalabe/hid.Device is an interface
	info   DeviceInfo
}

// Verify HIDAPIDevice implements Device interface.
var _ Device = (*HIDAPIDevice)(nil)

// NewHIDAPIDevice creates a new HIDAPIDevice from an open hid.Device.
func NewHIDAPIDevice(device karalabehid.Device, info DeviceInfo) *HIDAPIDevice {
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
func EnumerateDisplays() ([]DeviceInfo, error) {
	var displays []DeviceInfo

	devices, err := karalabehid.Enumerate(AppleVendorID, StudioDisplayProductID)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate HID devices: %w", err)
	}

	for _, device := range devices {
		if device.Interface == BrightnessInterface {
			displays = append(displays, DeviceInfo{
				Path:         device.Path,
				VendorID:     device.VendorID,
				ProductID:    device.ProductID,
				Serial:       device.Serial,
				Manufacturer: device.Manufacturer,
				Product:      device.Product,
				Interface:    device.Interface,
			})
		}
	}

	return displays, nil
}

// OpenDisplay opens a connection to an Apple Studio Display by serial number.
// If serial is empty, opens the first available display.
func OpenDisplay(serial string) (*HIDAPIDevice, error) {
	devices, err := karalabehid.Enumerate(AppleVendorID, StudioDisplayProductID)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate devices: %w", err)
	}

	for _, deviceInfo := range devices {
		if deviceInfo.Interface != BrightnessInterface {
			continue
		}

		if serial != "" && deviceInfo.Serial != serial {
			continue
		}

		device, err := deviceInfo.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open display %s: %w", deviceInfo.Serial, err)
		}

		info := DeviceInfo{
			Path:         deviceInfo.Path,
			VendorID:     deviceInfo.VendorID,
			ProductID:    deviceInfo.ProductID,
			Serial:       deviceInfo.Serial,
			Manufacturer: deviceInfo.Manufacturer,
			Product:      deviceInfo.Product,
			Interface:    deviceInfo.Interface,
		}

		return NewHIDAPIDevice(device, info), nil
	}

	if serial != "" {
		return nil, fmt.Errorf("display with serial %s not found", serial)
	}
	return nil, fmt.Errorf("no Apple Studio Display found")
}

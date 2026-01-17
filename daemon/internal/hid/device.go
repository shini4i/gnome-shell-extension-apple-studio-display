// Package hid provides abstractions for interacting with Apple Studio Display hardware.
package hid

//go:generate mockgen -source=device.go -destination=mocks/device_mock.go -package=mocks

// DeviceInfo contains information about a HID device.
type DeviceInfo struct {
	Path         string
	VendorID     uint16
	ProductID    uint16
	Serial       string
	Manufacturer string
	Product      string
	Interface    int
}

// Device represents an interface for HID device operations.
// This interface allows for mocking in tests.
type Device interface {
	// GetFeatureReport reads a feature report from the device.
	// The first byte is the report ID.
	GetFeatureReport(data []byte) (int, error)

	// SendFeatureReport writes a feature report to the device.
	// The first byte is the report ID.
	SendFeatureReport(data []byte) (int, error)

	// Close closes the device handle.
	Close() error

	// Info returns information about the device.
	Info() DeviceInfo
}

// DeviceOpener is a function type that opens a HID device.
type DeviceOpener func(vendorID, productID uint16, serial string) (Device, error)

// DeviceEnumerator is a function type that enumerates HID devices.
type DeviceEnumerator func(vendorID, productID uint16) []DeviceInfo

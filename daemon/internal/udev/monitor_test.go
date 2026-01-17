package udev

import (
	"errors"
	"sync"
	"syscall"
	"testing"

	"github.com/pilebones/go-udev/netlink"
	"github.com/stretchr/testify/assert"
)

func TestIsStudioDisplayProduct(t *testing.T) {
	tests := []struct {
		name     string
		product  string
		expected bool
	}{
		{
			name:     "valid Apple Studio Display product string",
			product:  "5ac/1114/157",
			expected: true,
		},
		{
			name:     "valid with uppercase vendor ID",
			product:  "5AC/1114/100",
			expected: true,
		},
		{
			name:     "different Apple product",
			product:  "5ac/8286/100",
			expected: false,
		},
		{
			name:     "non-Apple vendor",
			product:  "1234/1114/100",
			expected: false,
		},
		{
			name:     "empty string",
			product:  "",
			expected: false,
		},
		{
			name:     "malformed string",
			product:  "5ac",
			expected: false,
		},
		{
			name:     "only vendor/product",
			product:  "5ac/1114",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStudioDisplayProduct(tt.product)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewMonitor(t *testing.T) {
	handlerCalled := false
	handler := func(event Event) {
		handlerCalled = true
	}

	monitor := NewMonitor(handler)
	assert.NotNil(t, monitor)
	assert.NotNil(t, monitor.handler)

	// Verify handler is stored correctly
	monitor.handler(Event{Type: EventAdd})
	assert.True(t, handlerCalled)
}

func TestNewMonitor_NilHandler(t *testing.T) {
	monitor := NewMonitor(nil)
	assert.NotNil(t, monitor)
	assert.Nil(t, monitor.handler)
}

func TestEventType(t *testing.T) {
	// Verify event type constants
	assert.Equal(t, EventType(0), EventAdd)
	assert.Equal(t, EventType(1), EventRemove)
}

func TestMonitor_StopWithoutStart(t *testing.T) {
	monitor := NewMonitor(nil)
	// Stop should be safe to call even if not started
	err := monitor.Stop()
	assert.NoError(t, err)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "5ac", AppleVendorID)
	assert.Equal(t, "1114", StudioDisplayProductID)
}

func TestMonitor_HandleEvent(t *testing.T) {
	tests := []struct {
		name          string
		uevent        netlink.UEvent
		expectHandler bool
		expectedType  EventType
	}{
		{
			name: "add event for usb_device triggers handler",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"DEVTYPE": "usb_device",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: true,
			expectedType:  EventAdd,
		},
		{
			name: "remove event for usb_device triggers handler",
			uevent: netlink.UEvent{
				Action: netlink.REMOVE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"DEVTYPE": "usb_device",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: true,
			expectedType:  EventRemove,
		},
		{
			name: "usb_interface events are ignored",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1/1-1:1.0",
				Env: map[string]string{
					"DEVTYPE": "usb_interface",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: false,
		},
		{
			name: "change action is ignored",
			uevent: netlink.UEvent{
				Action: netlink.CHANGE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"DEVTYPE": "usb_device",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: false,
		},
		{
			name: "bind action is ignored",
			uevent: netlink.UEvent{
				Action: netlink.BIND,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"DEVTYPE": "usb_device",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: false,
		},
		{
			name: "missing DEVTYPE is ignored",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			handlerCalled := false
			var receivedEvent Event

			handler := func(event Event) {
				mu.Lock()
				defer mu.Unlock()
				handlerCalled = true
				receivedEvent = event
			}

			monitor := NewMonitor(handler)
			monitor.handleEvent(tt.uevent)

			mu.Lock()
			defer mu.Unlock()

			if tt.expectHandler {
				assert.True(t, handlerCalled, "handler should have been called")
				assert.Equal(t, tt.expectedType, receivedEvent.Type)
			} else {
				assert.False(t, handlerCalled, "handler should not have been called")
			}
		})
	}
}

func TestMonitor_HandleEvent_NilHandler(t *testing.T) {
	// Should not panic with nil handler
	monitor := NewMonitor(nil)
	uevent := netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "/devices/pci0000:00/usb1/1-1",
		Env: map[string]string{
			"DEVTYPE": "usb_device",
			"PRODUCT": "5ac/1114/157",
		},
	}

	// This should not panic
	assert.NotPanics(t, func() {
		monitor.handleEvent(uevent)
	})
}

func TestMonitor_CreateMatcher(t *testing.T) {
	monitor := NewMonitor(nil)
	matcher := monitor.createMatcher()

	assert.NotNil(t, matcher)
	assert.Len(t, matcher.Rules, 2) // add and remove rules

	// Test that the matcher compiles without error
	err := matcher.Compile()
	assert.NoError(t, err)

	// Test matching behavior
	tests := []struct {
		name     string
		uevent   netlink.UEvent
		expected bool
	}{
		{
			name: "matches add event for Studio Display",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5ac/1114/157",
				},
			},
			expected: true,
		},
		{
			name: "matches remove event for Studio Display",
			uevent: netlink.UEvent{
				Action: netlink.REMOVE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5ac/1114/157",
				},
			},
			expected: true,
		},
		{
			name: "does not match different product",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5ac/8286/100",
				},
			},
			expected: false,
		},
		{
			name: "does not match different vendor",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "1234/1114/100",
				},
			},
			expected: false,
		},
		{
			name: "does not match change action",
			uevent: netlink.UEvent{
				Action: netlink.CHANGE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5ac/1114/157",
				},
			},
			expected: false,
		},
		{
			name: "does not match different subsystem",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "block",
					"PRODUCT":   "5ac/1114/157",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.Evaluate(tt.uevent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMonitor_SetRecoveryHandler(t *testing.T) {
	monitor := NewMonitor(nil)
	assert.Nil(t, monitor.recoveryHandler)

	handlerCalled := false
	handler := func() {
		handlerCalled = true
	}

	monitor.SetRecoveryHandler(handler)
	assert.NotNil(t, monitor.recoveryHandler)

	// Verify handler is stored correctly
	monitor.recoveryHandler()
	assert.True(t, handlerCalled)
}

func TestIsBufferOverflowError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error returns false",
			err:      nil,
			expected: false,
		},
		{
			name:     "ENOBUFS syscall error returns true",
			err:      syscall.ENOBUFS,
			expected: true,
		},
		{
			name:     "error message with 'no buffer space available' returns true",
			err:      errors.New("unable to check available uevent, err: no buffer space available"),
			expected: true,
		},
		{
			name:     "generic error returns false",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "different syscall error returns false",
			err:      syscall.EINVAL,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBufferOverflowError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

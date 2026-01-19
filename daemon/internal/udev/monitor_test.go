// SPDX-License-Identifier: GPL-3.0-only

package udev

import (
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pilebones/go-udev/netlink"
	"github.com/stretchr/testify/assert"
)

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
	assert.Equal(t, "0?5[aA][cC]", AppleVendorIDPattern)
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
			name: "missing DEVTYPE is ignored for add events",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: false,
		},
		{
			name: "remove event without DEVTYPE triggers handler",
			uevent: netlink.UEvent{
				Action: netlink.REMOVE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: true,
			expectedType:  EventRemove,
		},
		{
			name: "remove event with empty DEVTYPE triggers handler",
			uevent: netlink.UEvent{
				Action: netlink.REMOVE,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"DEVTYPE": "",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: true,
			expectedType:  EventRemove,
		},
		{
			name: "remove event for usb_interface also triggers handler",
			uevent: netlink.UEvent{
				Action: netlink.REMOVE,
				KObj:   "/devices/pci0000:00/usb1/1-1/1-1:1.0",
				Env: map[string]string{
					"DEVTYPE": "usb_interface",
					"PRODUCT": "5ac/1114/157",
				},
			},
			expectHandler: true,
			expectedType:  EventRemove,
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
		// Vendor ID pattern variants - ensures cross-kernel compatibility
		{
			name: "matches vendor ID with leading zero (05ac)",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "05ac/1114/157",
				},
			},
			expected: true,
		},
		{
			name: "matches vendor ID uppercase (5AC)",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5AC/1114/157",
				},
			},
			expected: true,
		},
		{
			name: "matches vendor ID uppercase with leading zero (05AC)",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "05AC/1114/157",
				},
			},
			expected: true,
		},
		{
			name: "matches vendor ID mixed case (5Ac)",
			uevent: netlink.UEvent{
				Action: netlink.ADD,
				KObj:   "/devices/pci0000:00/usb1/1-1",
				Env: map[string]string{
					"SUBSYSTEM": "usb",
					"PRODUCT":   "5Ac/1114/157",
				},
			},
			expected: true,
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

func TestMonitor_RemoveEventDebouncing(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	handler := func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
	}

	monitor := NewMonitor(handler)
	product := "5ac/1114/157"

	// First REMOVE event should trigger handler
	uevent := netlink.UEvent{
		Action: netlink.REMOVE,
		KObj:   "/devices/pci0000:00/usb1/1-1",
		Env: map[string]string{
			"PRODUCT": product,
		},
	}
	monitor.handleEvent(uevent)

	mu.Lock()
	assert.Equal(t, 1, callCount, "first REMOVE should trigger handler")
	mu.Unlock()

	// Second REMOVE with same PRODUCT within debounce window should be ignored
	uevent.KObj = "/devices/pci0000:00/usb1/1-1/1-1:1.0" // Different interface
	monitor.handleEvent(uevent)

	mu.Lock()
	assert.Equal(t, 1, callCount, "second REMOVE within debounce window should be ignored")
	mu.Unlock()

	// Third REMOVE with same PRODUCT within debounce window should also be ignored
	uevent.KObj = "/devices/pci0000:00/usb1/1-1/1-1:1.1"
	monitor.handleEvent(uevent)

	mu.Lock()
	assert.Equal(t, 1, callCount, "third REMOVE within debounce window should be ignored")
	mu.Unlock()
}

func TestMonitor_RemoveEventDebouncing_DifferentProducts(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	handler := func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
	}

	monitor := NewMonitor(handler)

	// First REMOVE for product A
	uevent1 := netlink.UEvent{
		Action: netlink.REMOVE,
		KObj:   "/devices/pci0000:00/usb1/1-1",
		Env: map[string]string{
			"PRODUCT": "5ac/1114/157",
		},
	}
	monitor.handleEvent(uevent1)

	// First REMOVE for product B (different device, e.g., second monitor)
	uevent2 := netlink.UEvent{
		Action: netlink.REMOVE,
		KObj:   "/devices/pci0000:00/usb1/1-2",
		Env: map[string]string{
			"PRODUCT": "5ac/1114/201", // Different bcdDevice
		},
	}
	monitor.handleEvent(uevent2)

	mu.Lock()
	assert.Equal(t, 2, callCount, "REMOVE events for different products should both trigger handler")
	mu.Unlock()
}

func TestMonitor_ShouldDebounceRemove(t *testing.T) {
	monitor := NewMonitor(nil)
	product := "5ac/1114/157"

	// First call should not debounce
	shouldDebounce := monitor.shouldDebounceRemove(product)
	assert.False(t, shouldDebounce, "first call should not debounce")

	// Immediate second call should debounce
	shouldDebounce = monitor.shouldDebounceRemove(product)
	assert.True(t, shouldDebounce, "immediate second call should debounce")

	// Verify the timestamp was recorded
	monitor.mu.Lock()
	_, exists := monitor.lastRemoveTime[product]
	monitor.mu.Unlock()
	assert.True(t, exists, "product should be in lastRemoveTime map")
}

func TestMonitor_ShouldDebounceRemove_Cleanup(t *testing.T) {
	monitor := NewMonitor(nil)

	// Add an old entry manually
	oldProduct := "old/product/1"
	monitor.mu.Lock()
	monitor.lastRemoveTime[oldProduct] = time.Now().Add(-2 * time.Minute) // 2 minutes ago
	monitor.mu.Unlock()

	// Process a new product - this should trigger cleanup of the old entry
	newProduct := "new/product/1"
	monitor.shouldDebounceRemove(newProduct)

	// Verify old entry was cleaned up
	monitor.mu.Lock()
	_, oldExists := monitor.lastRemoveTime[oldProduct]
	_, newExists := monitor.lastRemoveTime[newProduct]
	monitor.mu.Unlock()

	assert.False(t, oldExists, "old entry should be cleaned up")
	assert.True(t, newExists, "new entry should exist")
}

func TestMonitor_AddEventsNotDebounced(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	handler := func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
	}

	monitor := NewMonitor(handler)

	// Multiple ADD events should all trigger handler (no debouncing for ADD)
	uevent := netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "/devices/pci0000:00/usb1/1-1",
		Env: map[string]string{
			"DEVTYPE": "usb_device",
			"PRODUCT": "5ac/1114/157",
		},
	}

	monitor.handleEvent(uevent)
	monitor.handleEvent(uevent)
	monitor.handleEvent(uevent)

	mu.Lock()
	assert.Equal(t, 3, callCount, "ADD events should not be debounced")
	mu.Unlock()
}

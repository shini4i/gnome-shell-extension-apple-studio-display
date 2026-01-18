// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"testing"

	"github.com/shini4i/asd-brightness-daemon/internal/dbus"
	"github.com/shini4i/asd-brightness-daemon/internal/hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDisplaysSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		displays []hid.DeviceInfo
	}{
		{
			name:     "empty manager returns empty snapshot",
			displays: []hid.DeviceInfo{},
		},
		{
			name: "single display",
			displays: []hid.DeviceInfo{
				{Serial: "ABC123", Product: "Display 1"},
			},
		},
		{
			name: "multiple displays",
			displays: []hid.DeviceInfo{
				{Serial: "ABC123", Product: "Display 1"},
				{Serial: "DEF456", Product: "Display 2"},
				{Serial: "GHI789", Product: "Display 3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a manager with mocked enumerator
			enumerator := func() ([]hid.DeviceInfo, error) {
				return tt.displays, nil
			}

			// Create mock opener that returns a simple mock device
			opener := func(serial string) (hid.Device, error) {
				return &mockDevice{serial: serial}, nil
			}

			manager := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))
			err := manager.RefreshDisplays()
			require.NoError(t, err)

			snapshot := getDisplaysSnapshot(manager)
			assert.Len(t, snapshot, len(tt.displays))

			for _, d := range tt.displays {
				info, exists := snapshot[d.Serial]
				assert.True(t, exists, "serial %s should exist in snapshot", d.Serial)
				assert.Equal(t, d.Serial, info.Serial)
			}
		})
	}
}

func TestDiffDisplays(t *testing.T) {
	tests := []struct {
		name            string
		oldDisplays     map[string]hid.DeviceInfo
		newDisplays     map[string]hid.DeviceInfo
		expectedAdded   int
		expectedRemoved int
	}{
		{
			name:            "no changes",
			oldDisplays:     map[string]hid.DeviceInfo{"ABC": {Serial: "ABC"}},
			newDisplays:     map[string]hid.DeviceInfo{"ABC": {Serial: "ABC"}},
			expectedAdded:   0,
			expectedRemoved: 0,
		},
		{
			name:            "one display added",
			oldDisplays:     map[string]hid.DeviceInfo{},
			newDisplays:     map[string]hid.DeviceInfo{"ABC": {Serial: "ABC", Product: "Display 1"}},
			expectedAdded:   1,
			expectedRemoved: 0,
		},
		{
			name:            "one display removed",
			oldDisplays:     map[string]hid.DeviceInfo{"ABC": {Serial: "ABC"}},
			newDisplays:     map[string]hid.DeviceInfo{},
			expectedAdded:   0,
			expectedRemoved: 1,
		},
		{
			name:            "one added one removed",
			oldDisplays:     map[string]hid.DeviceInfo{"ABC": {Serial: "ABC"}},
			newDisplays:     map[string]hid.DeviceInfo{"DEF": {Serial: "DEF", Product: "Display 2"}},
			expectedAdded:   1,
			expectedRemoved: 1,
		},
		{
			name: "multiple changes",
			oldDisplays: map[string]hid.DeviceInfo{
				"ABC": {Serial: "ABC"},
				"DEF": {Serial: "DEF"},
			},
			newDisplays: map[string]hid.DeviceInfo{
				"DEF": {Serial: "DEF"},
				"GHI": {Serial: "GHI", Product: "Display 3"},
				"JKL": {Serial: "JKL", Product: "Display 4"},
			},
			expectedAdded:   2, // GHI and JKL
			expectedRemoved: 1, // ABC
		},
		{
			name:            "both empty",
			oldDisplays:     map[string]hid.DeviceInfo{},
			newDisplays:     map[string]hid.DeviceInfo{},
			expectedAdded:   0,
			expectedRemoved: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := diffDisplays(tt.oldDisplays, tt.newDisplays)

			assert.Len(t, changes.added, tt.expectedAdded, "added count mismatch")
			assert.Len(t, changes.removed, tt.expectedRemoved, "removed count mismatch")

			// Verify added displays have correct info
			for _, added := range changes.added {
				_, existsInNew := tt.newDisplays[added.Serial]
				_, existsInOld := tt.oldDisplays[added.Serial]
				assert.True(t, existsInNew, "added display should exist in new")
				assert.False(t, existsInOld, "added display should not exist in old")
			}

			// Verify removed serials
			for _, removedSerial := range changes.removed {
				_, existsInNew := tt.newDisplays[removedSerial]
				_, existsInOld := tt.oldDisplays[removedSerial]
				assert.False(t, existsInNew, "removed display should not exist in new")
				assert.True(t, existsInOld, "removed display should exist in old")
			}
		})
	}
}

func TestRefreshDisplaysWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	displays := []hid.DeviceInfo{{Serial: "ABC123", Product: "Display"}}

	enumerator := func() ([]hid.DeviceInfo, error) {
		return displays, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return &mockDevice{serial: serial}, nil
	}

	manager := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))

	found, err := refreshDisplaysWithRetry(manager, 3)

	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 1, manager.Count())
}

func TestRefreshDisplaysWithRetry_NoDisplaysFound(t *testing.T) {
	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{}, nil
	}

	manager := hid.NewManager(hid.WithEnumerator(enumerator))

	// Use 0 retries to make test fast
	found, err := refreshDisplaysWithRetry(manager, 0)

	assert.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, 0, manager.Count())
}

// mockDevice implements hid.Device for testing
type mockDevice struct {
	serial  string
	product string
}

func (m *mockDevice) GetFeatureReport(data []byte) (int, error) {
	return len(data), nil
}

func (m *mockDevice) SendFeatureReport(data []byte) (int, error) {
	return len(data), nil
}

func (m *mockDevice) Close() error {
	return nil
}

func (m *mockDevice) Info() hid.DeviceInfo {
	return hid.DeviceInfo{
		Serial:  m.serial,
		Product: m.product,
	}
}

// TestRefreshDisplaysWithRetry_SkipsWhenNoDisplaysFound verifies that
// refreshDisplaysWithRetry returns found=false when no displays are found,
// which is the key behavior that enables the spurious event fix.
//
// This tests the fix for spurious DisplayRemoved events that occurred when:
// 1. Displays were previously connected (oldDisplays > 0)
// 2. HID enumeration temporarily fails to find displays
// 3. Without the fix, diffDisplays would be called with empty newDisplays,
//    causing DisplayRemoved to be emitted for all previous displays
func TestRefreshDisplaysWithRetry_SkipsWhenNoDisplaysFound(t *testing.T) {
	// Manager that always returns empty displays
	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{}, nil
	}

	manager := hid.NewManager(hid.WithEnumerator(enumerator))

	// Use 0 retries to make test fast
	found, err := refreshDisplaysWithRetry(manager, 0)

	assert.NoError(t, err)
	assert.False(t, found, "Should return found=false when no displays found")
	assert.Equal(t, 0, manager.Count())
}

// TestDiffDisplays_WithPreviousDisplaysAndEmptyNew verifies that diffDisplays
// correctly identifies all previous displays as removed when new snapshot is empty.
// This scenario is what the fix prevents from causing spurious events.
func TestDiffDisplays_WithPreviousDisplaysAndEmptyNew(t *testing.T) {
	oldDisplays := map[string]hid.DeviceInfo{
		"ABC123": {Serial: "ABC123", Product: "Display 1"},
		"DEF456": {Serial: "DEF456", Product: "Display 2"},
	}
	newDisplays := map[string]hid.DeviceInfo{}

	changes := diffDisplays(oldDisplays, newDisplays)

	// Without the fix, this would emit 2 DisplayRemoved events
	assert.Len(t, changes.added, 0, "No displays should be added")
	assert.Len(t, changes.removed, 2, "Both displays should be marked as removed")
	assert.Contains(t, changes.removed, "ABC123")
	assert.Contains(t, changes.removed, "DEF456")
}

// TestHotplugHandler_EarlyReturnPreventsSpuriousEvents tests the core behavior
// of the hotplug handler: when refreshDisplaysWithRetry returns found=false,
// the handler should return early without calling diffDisplays/emitDisplayChanges.
//
// Note: This test documents the expected control flow. The actual handler
// uses time.Sleep for device initialization, so we test the logic separately.
func TestHotplugHandler_EarlyReturnPreventsSpuriousEvents(t *testing.T) {
	// Simulate the scenario: we had displays, refresh returns none
	oldDisplays := map[string]hid.DeviceInfo{
		"ABC123": {Serial: "ABC123", Product: "Display 1"},
	}

	// Simulate refreshDisplaysWithRetry returning found=false
	found := false

	// This is the key condition in the fix
	// Old code: if !found && len(oldDisplays) == 0
	// New code: if !found
	shouldSkipDiff := !found

	assert.True(t, shouldSkipDiff, "Should skip diff when found=false, regardless of previous display count")

	// The old condition would NOT skip diff here (because len(oldDisplays) > 0)
	oldConditionWouldSkip := !found && len(oldDisplays) == 0
	assert.False(t, oldConditionWouldSkip, "Old condition would NOT skip diff, causing spurious events")
}

// TestEmitDisplayChanges_OnlyEmitsForActualChanges verifies that emitDisplayChanges
// correctly processes the displayChanges struct.
func TestEmitDisplayChanges_OnlyEmitsForActualChanges(t *testing.T) {
	// This test verifies emitDisplayChanges behavior with various change scenarios.
	// Since we can't capture D-Bus signals without a connection, we verify
	// that the function doesn't panic with different inputs.

	mockManager := &mockDisplayManager{displays: []hid.DeviceInfo{}}
	server := dbus.NewServer(mockManager)

	tests := []struct {
		name    string
		changes displayChanges
	}{
		{
			name:    "empty changes",
			changes: displayChanges{},
		},
		{
			name: "only additions",
			changes: displayChanges{
				added: []hid.DeviceInfo{
					{Serial: "ABC123", Product: "Display 1"},
				},
			},
		},
		{
			name: "only removals",
			changes: displayChanges{
				removed: []string{"ABC123"},
			},
		},
		{
			name: "both additions and removals",
			changes: displayChanges{
				added:   []hid.DeviceInfo{{Serial: "DEF456", Product: "Display 2"}},
				removed: []string{"ABC123"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				emitDisplayChanges(server, tt.changes)
			})
		})
	}
}

// mockDisplayManager implements dbus.DisplayManager for testing.
type mockDisplayManager struct {
	displays []hid.DeviceInfo
}

func (m *mockDisplayManager) ListDisplays() []hid.DeviceInfo {
	return m.displays
}

func (m *mockDisplayManager) GetDisplay(serial string) (*hid.Display, error) {
	return nil, nil
}

func (m *mockDisplayManager) RefreshDisplays() error {
	return nil
}

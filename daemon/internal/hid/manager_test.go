package hid_test

import (
	"errors"
	"testing"

	"github.com/shini4i/asd-brightness-daemon/internal/hid"
	"github.com/shini4i/asd-brightness-daemon/internal/hid/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestManager_ListDisplays_Empty(t *testing.T) {
	m := hid.NewManager()
	displays := m.ListDisplays()
	assert.Empty(t, displays)
}

func TestManager_GetDisplay_NotFound(t *testing.T) {
	m := hid.NewManager()
	display, err := m.GetDisplay("NONEXISTENT")
	assert.Nil(t, display)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_RefreshDisplays_AddsNewDisplays(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{
		Serial:  "ABC123",
		Product: "Apple Studio Display",
	}).AnyTimes()

	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{
			{Serial: "ABC123", Product: "Apple Studio Display"},
		}, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return mockDevice, nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))
	assert.Equal(t, 0, m.Count())

	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())

	// Verify display is accessible
	display, err := m.GetDisplay("ABC123")
	require.NoError(t, err)
	assert.NotNil(t, display)

	// Verify ListDisplays returns the device info
	displays := m.ListDisplays()
	require.Len(t, displays, 1)
	assert.Equal(t, "ABC123", displays[0].Serial)
	assert.Equal(t, "Apple Studio Display", displays[0].Product)
}

func TestManager_RefreshDisplays_RemovesDisconnectedDisplays(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().Close().Return(nil).Times(1)

	// First enumeration returns display, second returns empty
	callCount := 0
	enumerator := func() ([]hid.DeviceInfo, error) {
		callCount++
		if callCount == 1 {
			return []hid.DeviceInfo{{Serial: "ABC123"}}, nil
		}
		return []hid.DeviceInfo{}, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return mockDevice, nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))

	// First refresh adds the display
	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())

	// Second refresh removes the display
	err = m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 0, m.Count())
}

func TestManager_RefreshDisplays_EnumerationError(t *testing.T) {
	enumerator := func() ([]hid.DeviceInfo, error) {
		return nil, errors.New("enumeration failed")
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator))
	err := m.RefreshDisplays()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enumerate")
}

func TestManager_RefreshDisplays_OpenerError(t *testing.T) {
	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{{Serial: "ABC123"}}, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return nil, errors.New("failed to open device")
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))
	err := m.RefreshDisplays()
	// Should not return error, just log and continue
	require.NoError(t, err)
	assert.Equal(t, 0, m.Count())
}

func TestManager_RefreshDisplays_MultipleDisplays(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice1 := mocks.NewMockDevice(ctrl)
	mockDevice1.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123", Product: "Display 1"}).AnyTimes()

	mockDevice2 := mocks.NewMockDevice(ctrl)
	mockDevice2.EXPECT().Info().Return(hid.DeviceInfo{Serial: "DEF456", Product: "Display 2"}).AnyTimes()

	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{
			{Serial: "ABC123", Product: "Display 1"},
			{Serial: "DEF456", Product: "Display 2"},
		}, nil
	}

	deviceMap := map[string]hid.Device{
		"ABC123": mockDevice1,
		"DEF456": mockDevice2,
	}

	opener := func(serial string) (hid.Device, error) {
		return deviceMap[serial], nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))

	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 2, m.Count())

	displays := m.ListDisplays()
	assert.Len(t, displays, 2)
}

func TestManager_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().Close().Return(nil).Times(1)

	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{{Serial: "ABC123"}}, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return mockDevice, nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))

	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())

	err = m.Close()
	require.NoError(t, err)
	assert.Equal(t, 0, m.Count())
}

func TestManager_Count(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice1 := mocks.NewMockDevice(ctrl)
	mockDevice1.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()

	mockDevice2 := mocks.NewMockDevice(ctrl)
	mockDevice2.EXPECT().Info().Return(hid.DeviceInfo{Serial: "DEF456"}).AnyTimes()

	callCount := 0
	enumerator := func() ([]hid.DeviceInfo, error) {
		callCount++
		if callCount == 1 {
			return []hid.DeviceInfo{{Serial: "ABC123"}}, nil
		}
		return []hid.DeviceInfo{
			{Serial: "ABC123"},
			{Serial: "DEF456"},
		}, nil
	}

	deviceMap := map[string]hid.Device{
		"ABC123": mockDevice1,
		"DEF456": mockDevice2,
	}

	opener := func(serial string) (hid.Device, error) {
		return deviceMap[serial], nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))
	assert.Equal(t, 0, m.Count())

	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())

	err = m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 2, m.Count())
}

func TestManager_RefreshDisplays_KeepsExistingDisplays(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Close should NOT be called since display stays connected

	enumerator := func() ([]hid.DeviceInfo, error) {
		return []hid.DeviceInfo{{Serial: "ABC123"}}, nil
	}

	opener := func(serial string) (hid.Device, error) {
		return mockDevice, nil
	}

	m := hid.NewManager(hid.WithEnumerator(enumerator), hid.WithOpener(opener))

	// First refresh
	err := m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())

	// Second refresh - display should still be there without reopening
	err = m.RefreshDisplays()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Count())
}

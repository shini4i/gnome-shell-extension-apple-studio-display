package dbus

import (
	"errors"
	"testing"

	"github.com/shini4i/asd-brightness-daemon/internal/hid"
	"github.com/shini4i/asd-brightness-daemon/internal/hid/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// mockDisplayManager implements DisplayManager for testing.
type mockDisplayManager struct {
	displays    []hid.DeviceInfo
	displayMap  map[string]*hid.Display
	refreshErr  error
	getErr      error
}

func (m *mockDisplayManager) ListDisplays() []hid.DeviceInfo {
	return m.displays
}

func (m *mockDisplayManager) GetDisplay(serial string) (*hid.Display, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	display, ok := m.displayMap[serial]
	if !ok {
		return nil, errors.New("display not found")
	}
	return display, nil
}

func (m *mockDisplayManager) RefreshDisplays() error {
	return m.refreshErr
}

func TestNewServer(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)
	assert.NotNil(t, server)
	assert.Equal(t, manager, server.manager)
}

func TestServer_ListDisplays(t *testing.T) {
	manager := &mockDisplayManager{
		displays: []hid.DeviceInfo{
			{Serial: "ABC123", Product: "Apple Studio Display"},
			{Serial: "DEF456", Product: "Apple Studio Display"},
		},
	}
	server := NewServer(manager)

	result, err := server.ListDisplays()
	require.Nil(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "ABC123", result[0].Serial)
	assert.Equal(t, "Apple Studio Display", result[0].ProductName)
	assert.Equal(t, "DEF456", result[1].Serial)
	assert.Equal(t, "Apple Studio Display", result[1].ProductName)
}

func TestServer_ListDisplays_Empty(t *testing.T) {
	manager := &mockDisplayManager{displays: []hid.DeviceInfo{}}
	server := NewServer(manager)

	result, err := server.ListDisplays()
	require.Nil(t, err)
	assert.Empty(t, result)
}

func TestServer_GetBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Return 50% brightness (midpoint nits value)
	mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		// Set midpoint brightness value (30200 nits = 50%)
		data[1] = 0xF8
		data[2] = 0x75
		data[3] = 0x00
		data[4] = 0x00
		return 7, nil
	})

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	brightness, err := server.GetBrightness("ABC123")
	require.Nil(t, err)
	assert.Equal(t, uint32(50), brightness)
}

func TestServer_GetBrightness_EmptySerial(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	brightness, err := server.GetBrightness("")
	assert.NotNil(t, err)
	assert.Equal(t, uint32(0), brightness)
}

func TestServer_GetBrightness_DisplayNotFound(t *testing.T) {
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{},
	}
	server := NewServer(manager)

	brightness, err := server.GetBrightness("NONEXISTENT")
	assert.NotNil(t, err)
	assert.Equal(t, uint32(0), brightness)
}

func TestServer_SetBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	err := server.SetBrightness("ABC123", 75)
	assert.Nil(t, err)
}

func TestServer_SetBrightness_EmptySerial(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	err := server.SetBrightness("", 50)
	assert.NotNil(t, err)
}

func TestServer_SetBrightness_ClampsOver100(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	// Should clamp to 100
	err := server.SetBrightness("ABC123", 150)
	assert.Nil(t, err)
}

func TestServer_IncreaseBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Current brightness is 50%
	mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		data[1] = 0xF8
		data[2] = 0x75
		data[3] = 0x00
		data[4] = 0x00
		return 7, nil
	})
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	err := server.IncreaseBrightness("ABC123", 10)
	assert.Nil(t, err)
}

func TestServer_IncreaseBrightness_EmptySerial(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	err := server.IncreaseBrightness("", 10)
	assert.NotNil(t, err)
}

func TestServer_IncreaseBrightness_ClampsAt100(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Current brightness is 95%
	mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		// 95% = 57020 nits
		data[1] = 0xCC
		data[2] = 0xDE
		data[3] = 0x00
		data[4] = 0x00
		return 7, nil
	})
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	// Increase by 10 should clamp at 100
	err := server.IncreaseBrightness("ABC123", 10)
	assert.Nil(t, err)
}

func TestServer_DecreaseBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Current brightness is 50%
	mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		data[1] = 0xF8
		data[2] = 0x75
		data[3] = 0x00
		data[4] = 0x00
		return 7, nil
	})
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	err := server.DecreaseBrightness("ABC123", 10)
	assert.Nil(t, err)
}

func TestServer_DecreaseBrightness_EmptySerial(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	err := server.DecreaseBrightness("", 10)
	assert.NotNil(t, err)
}

func TestServer_DecreaseBrightness_ClampsAt0(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	// Current brightness is 5%
	mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		// 5% = 3380 nits
		data[1] = 0x34
		data[2] = 0x0D
		data[3] = 0x00
		data[4] = 0x00
		return 7, nil
	})
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	// Decrease by 10 should clamp at 0
	err := server.DecreaseBrightness("ABC123", 10)
	assert.Nil(t, err)
}

func TestServer_SetAllBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice1 := mocks.NewMockDevice(ctrl)
	mockDevice1.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice1.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	mockDevice2 := mocks.NewMockDevice(ctrl)
	mockDevice2.EXPECT().Info().Return(hid.DeviceInfo{Serial: "DEF456"}).AnyTimes()
	mockDevice2.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display1 := hid.NewDisplay(mockDevice1)
	display2 := hid.NewDisplay(mockDevice2)
	manager := &mockDisplayManager{
		displays: []hid.DeviceInfo{
			{Serial: "ABC123"},
			{Serial: "DEF456"},
		},
		displayMap: map[string]*hid.Display{
			"ABC123": display1,
			"DEF456": display2,
		},
	}
	server := NewServer(manager)

	err := server.SetAllBrightness(75)
	assert.Nil(t, err)
}

func TestServer_SetAllBrightness_ClampsOver100(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil)

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displays:   []hid.DeviceInfo{{Serial: "ABC123"}},
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	err := server.SetAllBrightness(150)
	assert.Nil(t, err)
}

func TestServer_Constants(t *testing.T) {
	assert.Equal(t, "io.github.shini4i.AsdBrightness", ServiceName)
	assert.Equal(t, "/io/github/shini4i/AsdBrightness", ObjectPath)
	assert.Equal(t, "io.github.shini4i.AsdBrightness", InterfaceName)
}

func TestServer_RateLimiting(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock device that allows unlimited calls
	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{Serial: "ABC123"}).AnyTimes()
	mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(7, nil).AnyTimes()

	display := hid.NewDisplay(mockDevice)
	manager := &mockDisplayManager{
		displayMap: map[string]*hid.Display{"ABC123": display},
	}
	server := NewServer(manager)

	// Exhaust the burst limit (rateLimitBurst = 5)
	var rateLimitHit bool
	for i := 0; i < 20; i++ {
		err := server.SetBrightness("ABC123", 50)
		if err != nil {
			rateLimitHit = true
			assert.Contains(t, err.Error(), "rate limit exceeded")
			break
		}
	}

	assert.True(t, rateLimitHit, "Rate limiter should have been triggered")
}

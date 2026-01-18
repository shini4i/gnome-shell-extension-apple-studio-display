package dbus

import (
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestServer_IncreaseBrightness_InvalidStep(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	// Step of 0 should be rejected
	err := server.IncreaseBrightness("ABC123", 0)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "step must be between 1 and 100")

	// Step over 100 should be rejected
	err = server.IncreaseBrightness("ABC123", 101)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "step must be between 1 and 100")
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

func TestServer_DecreaseBrightness_InvalidStep(t *testing.T) {
	server := NewServer(&mockDisplayManager{})

	// Step of 0 should be rejected
	err := server.DecreaseBrightness("ABC123", 0)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "step must be between 1 and 100")

	// Step over 100 should be rejected
	err = server.DecreaseBrightness("ABC123", 101)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "step must be between 1 and 100")
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

func TestServer_SetDeviceErrorHandler(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	// Initially nil
	assert.Nil(t, server.deviceErrorHandler)

	// Set handler
	var handlerCalled bool
	server.SetDeviceErrorHandler(func(serial string, err error) {
		handlerCalled = true
	})

	assert.NotNil(t, server.deviceErrorHandler)

	// Verify handler is stored correctly by calling it directly
	server.deviceErrorHandler("test", errors.New("test error"))
	assert.True(t, handlerCalled)
}

func TestServer_handleDeviceError_NilError(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	handlerCalled := false
	server.SetDeviceErrorHandler(func(serial string, err error) {
		handlerCalled = true
	})

	// Nil error should return false and not call handler
	triggered := server.handleDeviceError("ABC123", nil)
	assert.False(t, triggered)

	// Give async handler time to run (if it were called)
	time.Sleep(10 * time.Millisecond)
	assert.False(t, handlerCalled)
}

func TestServer_handleDeviceError_NonDeviceError(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	handlerCalled := false
	server.SetDeviceErrorHandler(func(serial string, err error) {
		handlerCalled = true
	})

	// Generic error should return false and not call handler
	triggered := server.handleDeviceError("ABC123", errors.New("random error"))
	assert.False(t, triggered)

	// Give async handler time to run (if it were called)
	time.Sleep(10 * time.Millisecond)
	assert.False(t, handlerCalled)
}

func TestServer_handleDeviceError_TriggersRecovery(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	var mu sync.Mutex
	var receivedSerial string
	var receivedErr error
	handlerCalled := make(chan struct{}, 1)

	server.SetDeviceErrorHandler(func(serial string, err error) {
		mu.Lock()
		receivedSerial = serial
		receivedErr = err
		mu.Unlock()
		handlerCalled <- struct{}{}
	})

	// ENODEV error should trigger handler
	triggered := server.handleDeviceError("ABC123", syscall.ENODEV)
	assert.True(t, triggered)

	// Wait for async handler
	select {
	case <-handlerCalled:
		mu.Lock()
		assert.Equal(t, "ABC123", receivedSerial)
		assert.Equal(t, syscall.ENODEV, receivedErr)
		mu.Unlock()
	case <-time.After(100 * time.Millisecond):
		t.Fatal("handler was not called within timeout")
	}
}

func TestServer_handleDeviceError_TriggersRecoveryForEIO(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	handlerCalled := make(chan struct{}, 1)
	server.SetDeviceErrorHandler(func(serial string, err error) {
		handlerCalled <- struct{}{}
	})

	// EIO error should trigger handler
	triggered := server.handleDeviceError("ABC123", syscall.EIO)
	assert.True(t, triggered)

	// Wait for async handler
	select {
	case <-handlerCalled:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("handler was not called within timeout")
	}
}

func TestServer_handleDeviceError_TriggersRecoveryForNoSuchDevice(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	handlerCalled := make(chan struct{}, 1)
	server.SetDeviceErrorHandler(func(serial string, err error) {
		handlerCalled <- struct{}{}
	})

	// "No such device" error message should trigger handler
	triggered := server.handleDeviceError("ABC123", errors.New("ioctl: No such device"))
	assert.True(t, triggered)

	// Wait for async handler
	select {
	case <-handlerCalled:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("handler was not called within timeout")
	}
}

func TestServer_handleDeviceError_NilHandler(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)
	// Don't set a handler - deviceErrorHandler is nil

	// Should return true (error detected) but not panic
	triggered := server.handleDeviceError("ABC123", syscall.ENODEV)
	assert.True(t, triggered)
}

// TestServer_ConcurrentSetDeviceErrorHandler tests that SetDeviceErrorHandler
// is thread-safe when called concurrently with handleDeviceError.
func TestServer_ConcurrentSetDeviceErrorHandler(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)

	var wg sync.WaitGroup
	const numGoroutines = 100

	// Start goroutines that set the handler
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			server.SetDeviceErrorHandler(func(serial string, err error) {
				// Handler body doesn't matter for this test
			})
		}(i)
	}

	// Concurrently call handleDeviceError
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.handleDeviceError("ABC123", syscall.ENODEV)
		}()
	}

	wg.Wait()
	// If we get here without a race detector complaint, the test passes
}

// TestServer_ConcurrentStopAndEmit tests that Stop and signal emission
// methods don't race when called concurrently.
func TestServer_ConcurrentStopAndEmit(t *testing.T) {
	manager := &mockDisplayManager{}
	server := NewServer(manager)
	// Note: conn is nil, but we're testing mutex protection, not actual D-Bus calls

	var wg sync.WaitGroup
	const numGoroutines = 50

	// Start goroutines that emit signals (conn is nil, so they return early)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.EmitDisplayAdded("ABC123", "Test Display")
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.EmitDisplayRemoved("ABC123")
		}()
	}

	// Concurrently call Stop
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = server.Stop()
		}()
	}

	wg.Wait()
	// If we get here without a race detector complaint, the test passes
}

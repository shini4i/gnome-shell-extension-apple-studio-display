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

func TestDisplay_GetBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)

	tests := []struct {
		name            string
		setupMock       func()
		expectedPercent uint8
		expectedError   bool
	}{
		{
			name: "successfully reads minimum brightness",
			setupMock: func() {
				mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Return 400 nits (0x190) in little-endian
						data[0] = 0x01 // report ID
						data[1] = 0x90 // lo byte
						data[2] = 0x01 // mid_lo byte
						data[3] = 0x00 // mid_hi byte
						data[4] = 0x00 // hi byte
						return 7, nil
					},
				)
			},
			expectedPercent: 0,
			expectedError:   false,
		},
		{
			name: "successfully reads maximum brightness",
			setupMock: func() {
				mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Return 60000 nits (0xEA60) in little-endian
						data[0] = 0x01 // report ID
						data[1] = 0x60 // lo byte
						data[2] = 0xEA // mid_lo byte
						data[3] = 0x00 // mid_hi byte
						data[4] = 0x00 // hi byte
						return 7, nil
					},
				)
			},
			expectedPercent: 100,
			expectedError:   false,
		},
		{
			name: "successfully reads 50% brightness",
			setupMock: func() {
				mockDevice.EXPECT().GetFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Return 30200 nits (0x75F8) in little-endian
						data[0] = 0x01 // report ID
						data[1] = 0xF8 // lo byte
						data[2] = 0x75 // mid_lo byte
						data[3] = 0x00 // mid_hi byte
						data[4] = 0x00 // hi byte
						return 7, nil
					},
				)
			},
			expectedPercent: 50,
			expectedError:   false,
		},
		{
			name: "returns error when device fails",
			setupMock: func() {
				mockDevice.EXPECT().GetFeatureReport(gomock.Any()).Return(0, errors.New("device error"))
			},
			expectedPercent: 0,
			expectedError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			display := hid.NewDisplay(mockDevice)

			percent, err := display.GetBrightness()

			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPercent, percent)
			}
		})
	}
}

func TestDisplay_SetBrightness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)

	tests := []struct {
		name          string
		percent       uint8
		setupMock     func()
		expectedError bool
	}{
		{
			name:    "successfully sets minimum brightness",
			percent: 0,
			setupMock: func() {
				mockDevice.EXPECT().SendFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Verify the data is correct for 400 nits (0x190)
						assert.Equal(t, byte(0x01), data[0], "report ID should be 0x01")
						assert.Equal(t, byte(0x90), data[1], "lo byte should be 0x90")
						assert.Equal(t, byte(0x01), data[2], "mid_lo byte should be 0x01")
						return 7, nil
					},
				)
			},
			expectedError: false,
		},
		{
			name:    "successfully sets maximum brightness",
			percent: 100,
			setupMock: func() {
				mockDevice.EXPECT().SendFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Verify the data is correct for 60000 nits (0xEA60)
						assert.Equal(t, byte(0x01), data[0], "report ID should be 0x01")
						assert.Equal(t, byte(0x60), data[1], "lo byte should be 0x60")
						assert.Equal(t, byte(0xEA), data[2], "mid_lo byte should be 0xEA")
						return 7, nil
					},
				)
			},
			expectedError: false,
		},
		{
			name:    "successfully sets 50% brightness",
			percent: 50,
			setupMock: func() {
				mockDevice.EXPECT().SendFeatureReport(gomock.Any()).DoAndReturn(
					func(data []byte) (int, error) {
						// Verify the data is correct for 30200 nits (0x75F8)
						assert.Equal(t, byte(0x01), data[0], "report ID should be 0x01")
						assert.Equal(t, byte(0xF8), data[1], "lo byte should be 0xF8")
						assert.Equal(t, byte(0x75), data[2], "mid_lo byte should be 0x75")
						return 7, nil
					},
				)
			},
			expectedError: false,
		},
		{
			name:    "returns error when device fails",
			percent: 50,
			setupMock: func() {
				mockDevice.EXPECT().SendFeatureReport(gomock.Any()).Return(0, errors.New("device error"))
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			display := hid.NewDisplay(mockDevice)

			err := display.SetBrightness(tt.percent)

			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDisplay_Serial(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{
		Serial:  "C02ABC123",
		Product: "Studio Display",
	})

	display := hid.NewDisplay(mockDevice)
	assert.Equal(t, "C02ABC123", display.Serial())
}

func TestDisplay_ProductName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Info().Return(hid.DeviceInfo{
		Serial:  "C02ABC123",
		Product: "Studio Display",
	})

	display := hid.NewDisplay(mockDevice)
	assert.Equal(t, "Studio Display", display.ProductName())
}

func TestDisplay_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close().Return(nil)

	display := hid.NewDisplay(mockDevice)
	err := display.Close()
	require.NoError(t, err)
}

func TestDisplay_GetBrightness_AfterClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close().Return(nil)

	display := hid.NewDisplay(mockDevice)
	err := display.Close()
	require.NoError(t, err)

	_, err = display.GetBrightness()
	require.Error(t, err)
	assert.ErrorIs(t, err, hid.ErrDisplayClosed)
}

func TestDisplay_SetBrightness_AfterClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close().Return(nil)

	display := hid.NewDisplay(mockDevice)
	err := display.Close()
	require.NoError(t, err)

	err = display.SetBrightness(50)
	require.Error(t, err)
	assert.ErrorIs(t, err, hid.ErrDisplayClosed)
}

func TestDisplay_Close_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := mocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close().Return(nil).Times(1) // Only called once

	display := hid.NewDisplay(mockDevice)
	err := display.Close()
	require.NoError(t, err)

	// Second close should be no-op
	err = display.Close()
	require.NoError(t, err)
}

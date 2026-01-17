package brightness_test

import (
	"testing"

	"github.com/shini4i/asd-brightness-daemon/internal/brightness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNitsToPercent(t *testing.T) {
	tests := []struct {
		name     string
		nits     uint32
		expected uint8
	}{
		{
			name:     "minimum brightness (400 nits) returns 0%",
			nits:     400,
			expected: 0,
		},
		{
			name:     "maximum brightness (60000 nits) returns 100%",
			nits:     60000,
			expected: 100,
		},
		{
			name:     "midpoint brightness returns ~50%",
			nits:     30200, // (60000 + 400) / 2 = 30200
			expected: 50,
		},
		{
			name:     "quarter brightness returns ~25%",
			nits:     15300, // 400 + (59600 * 0.25) = 15300
			expected: 25,
		},
		{
			name:     "three-quarter brightness returns ~75%",
			nits:     45100, // 400 + (59600 * 0.75) = 45100
			expected: 75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := brightness.NitsToPercent(tt.nits)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPercentToNits(t *testing.T) {
	tests := []struct {
		name     string
		percent  uint8
		expected uint32
	}{
		{
			name:     "0% returns minimum brightness (400 nits)",
			percent:  0,
			expected: 400,
		},
		{
			name:     "100% returns maximum brightness (60000 nits)",
			percent:  100,
			expected: 60000,
		},
		{
			name:     "50% returns midpoint brightness",
			percent:  50,
			expected: 30200, // 400 + (59600 * 0.5) = 30200
		},
		{
			name:     "25% returns quarter brightness",
			percent:  25,
			expected: 15300, // 400 + (59600 * 0.25) = 15300
		},
		{
			name:     "75% returns three-quarter brightness",
			percent:  75,
			expected: 45100, // 400 + (59600 * 0.75) = 45100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := brightness.PercentToNits(tt.percent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClampNits(t *testing.T) {
	tests := []struct {
		name     string
		nits     uint32
		expected uint32
	}{
		{
			name:     "value below minimum is clamped to minimum",
			nits:     100,
			expected: 400,
		},
		{
			name:     "value above maximum is clamped to maximum",
			nits:     70000,
			expected: 60000,
		},
		{
			name:     "value within range is unchanged",
			nits:     30000,
			expected: 30000,
		},
		{
			name:     "minimum value is unchanged",
			nits:     400,
			expected: 400,
		},
		{
			name:     "maximum value is unchanged",
			nits:     60000,
			expected: 60000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := brightness.ClampNits(tt.nits)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that converting percent -> nits -> percent gives back the same value
	for percent := uint8(0); percent <= 100; percent++ {
		nits := brightness.PercentToNits(percent)
		result := brightness.NitsToPercent(nits)
		assert.Equal(t, percent, result, "round-trip failed for %d%%", percent)
	}
}

func TestConstants(t *testing.T) {
	require.Equal(t, uint32(400), brightness.MinBrightness, "MinBrightness should be 400 nits")
	require.Equal(t, uint32(60000), brightness.MaxBrightness, "MaxBrightness should be 60000 nits")
	require.Equal(t, uint32(59600), brightness.BrightnessRange, "BrightnessRange should be 59600 nits")
}

// SPDX-License-Identifier: GPL-3.0-only

// Package brightness provides utilities for converting between Apple Studio Display
// brightness values (in nits) and user-friendly percentages.
package brightness

import "math"

const (
	// MinBrightness is the minimum brightness value in nits supported by the Apple Studio Display.
	MinBrightness uint32 = 400

	// MaxBrightness is the maximum brightness value in nits supported by the Apple Studio Display.
	MaxBrightness uint32 = 60000

	// BrightnessRange is the difference between maximum and minimum brightness.
	BrightnessRange uint32 = MaxBrightness - MinBrightness
)

// NitsToPercent converts a brightness value in nits to a percentage (0-100).
// Values outside the valid range are clamped before conversion.
// Uses rounding to ensure round-trip consistency with PercentToNits.
func NitsToPercent(nits uint32) uint8 {
	nits = ClampNits(nits)
	percent := float64(nits-MinBrightness) / float64(BrightnessRange) * 100
	return uint8(math.Round(percent))
}

// PercentToNits converts a percentage (0-100) to a brightness value in nits.
// Percentages above 100 are treated as 100%.
func PercentToNits(percent uint8) uint32 {
	if percent > 100 {
		percent = 100
	}
	nits := uint32(float64(percent)*float64(BrightnessRange)/100) + MinBrightness
	return ClampNits(nits)
}

// ClampNits ensures the brightness value is within the valid range.
func ClampNits(nits uint32) uint32 {
	if nits < MinBrightness {
		return MinBrightness
	}
	if nits > MaxBrightness {
		return MaxBrightness
	}
	return nits
}

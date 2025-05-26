package redfish

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestDellMEStorageCalculation(t *testing.T) {
	// Test values provided by user
	// AllocatedBytes = 15350213115904 (15.35TB)
	// ConsumedBytes = 10017816576 (10GB)
	// RemainingCapacityPercent = 66
	// Expected used capacity is around 5.22TB (34% of 15.35TB)
	
	totalCapacityBytes := float32(15350213115904)
	remainingPercent := float32(66)
	
	// Calculate used percentage
	usedPercent := 100.0 - remainingPercent
	
	// Print the calculation steps for debugging
	t.Logf("Total capacity: %.2f TB", totalCapacityBytes/1099511627776)
	t.Logf("Used percent: %.2f%%", usedPercent)
	
	// Calculate used bytes
	usedBytes := totalCapacityBytes * (usedPercent / 100.0)
	t.Logf("Used bytes (raw): %.2f bytes", usedBytes)
	t.Logf("Used capacity (raw): %.4f TB", usedBytes/1099511627776)
	
	// Round to two decimals
	usedBytesRounded := roundToTwoDecimals(usedBytes)
	t.Logf("Used bytes (rounded): %.2f bytes", usedBytesRounded)
	
	// For decimal TB calculation (1TB = 10^12 bytes)
	decimalTB := usedBytesRounded / 1000000000000
	
	// For binary TiB calculation (1TiB = 2^40 bytes) - just for comparison
	binaryTiB := usedBytesRounded / 1099511627776
	t.Logf("Used capacity (decimal TB): %.4f TB", decimalTB)
	t.Logf("Used capacity (binary TiB): %.4f TiB", binaryTiB)
	
	// Check that the calculation is correct (34% of 15.35TB is ~5.22TB)
	assert.InDelta(t, 5.22, float64(decimalTB), 0.5, "Used capacity should be close to 5.22TB")
	
	// Check that the calculated value matches 34% of total capacity
	expectedBytes := totalCapacityBytes * 0.34
	assert.InDelta(t, expectedBytes, usedBytesRounded, float64(expectedBytes)*0.01, "Used capacity should be 34% of total capacity")
	
	// The Dell ME interface reports capacity in decimal TB (10^12 bytes)
	// rather than binary TiB (2^40 bytes), which explains the differences in values.
	//
	// For example: 15.35TB (decimal) * 34% = 5.22TB, which is the expected value
}

func TestDellMEFullPoolCalculation(t *testing.T) {
	// Test case for a full pool (0% remaining)
	totalCapacityBytes := float32(1000000000000) // 1TB
	remainingPercent := float32(0) // Pool is full
	
	// Calculate used percentage
	usedPercent := 100.0 - remainingPercent
	
	// Calculate used bytes
	usedBytes := totalCapacityBytes * (usedPercent / 100.0)
	usedBytesRounded := roundToTwoDecimals(usedBytes)
	
	t.Logf("Full pool test - Used bytes: %.2f, Expected: %.2f", usedBytesRounded, totalCapacityBytes)
	
	// When pool is full, used capacity should equal total capacity
	assert.Equal(t, totalCapacityBytes, usedBytesRounded, "Full pool should show 100% usage")
	assert.Equal(t, float32(100.0), usedPercent, "Used percent should be 100%")
}
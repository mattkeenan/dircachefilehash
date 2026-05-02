package dircachefilehash

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
)

// ParseHumanSize parses human-readable size strings (e.g., "2M", "512k", "1G")
func ParseHumanSize(sizeStr string) (int, error) {
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Convert to uppercase for consistent parsing
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	// Extract numeric part and suffix
	var numPart strings.Builder
	var suffix string
	for i, char := range sizeStr {
		if char >= '0' && char <= '9' || char == '.' {
			numPart.WriteString(string(char))
		} else {
			suffix = sizeStr[i:]
			break
		}
	}

	if numPart.String() == "" {
		return 0, fmt.Errorf("no numeric part in size string: %s", sizeStr)
	}

	// Parse the numeric part
	num, err := strconv.ParseFloat(numPart.String(), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric part in size string %s: %w", sizeStr, err)
	}

	// Apply multiplier based on suffix
	var multiplier int64
	switch suffix {
	case "", "B":
		multiplier = 1
	case "K", "KB":
		multiplier = 1024
	case "M", "MB":
		multiplier = 1024 * 1024
	case "G", "GB":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size suffix: %s", suffix)
	}

	result := int64(num * float64(multiplier))
	if result <= 0 {
		return 0, fmt.Errorf("size must be positive: %s", sizeStr)
	}
	if result > int64(^uint(0)>>1) { // Check for int overflow
		return 0, fmt.Errorf("size too large: %s", sizeStr)
	}

	return int(result), nil
}

// FormatHumanSize formats bytes into human-readable format using bit operations
func FormatHumanSize(bytes int64) string {
	if bytes < 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}

	// Find the highest bit position to determine the unit
	// bits.Len64 returns the bit length (position of highest 1 bit + 1)
	bitLen := bits.Len64(uint64(bytes))

	// Each unit is 10 bits apart (2^10 = 1024)
	// So we divide by 10 to get the unit index
	unitIndex := (bitLen - 1) / 10

	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	if unitIndex >= len(units) {
		unitIndex = len(units) - 1
	}

	// Calculate the divisor as 1 << (unitIndex * 10)
	divisor := float64(int64(1) << (unitIndex * 10))
	value := float64(bytes) / divisor

	// Use integer format for bytes, decimal format for larger units
	if unitIndex == 0 {
		return fmt.Sprintf("%d %s", bytes, units[unitIndex])
	}
	return fmt.Sprintf("%.1f %s", value, units[unitIndex])
}

// FormatHumanRate formats bytes per second into human-readable format
func FormatHumanRate(bytesPerSec float64) string {
	return FormatHumanSize(int64(bytesPerSec)) + "/s"
}

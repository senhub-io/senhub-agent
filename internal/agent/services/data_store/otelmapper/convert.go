package otelmapper

import "strings"

// convertValue applies unit-based scaling to the raw cache value so that
// it matches the OTel unit declared in the YAML.
//
// Conversions applied (based on (sourceUnit, otelUnit) pair):
//   - Percent → ratio (sourceUnit ∈ {%, percent}, otelUnit == "1"): ÷100
//   - Kilobytes → bytes (sourceUnit ∈ {KB, kb, ...}, otelUnit == "By"): ×1024
//   - Megabytes → bytes: ×1048576
//   - Gigabytes → bytes: ×1073741824
//   - Milliseconds → seconds: ÷1000
//   - Microseconds → seconds: ÷1e6
//   - Nanoseconds → seconds: ÷1e9
//   - Megabits per second → bits per second: ×1e6
//   - Gigabits per second → bits per second: ×1e9
//   - Hours → seconds: ×3600
//   - Days → seconds: ×86400
//
// Also applies the explicit ValueScale from the YAML if present (takes
// precedence over unit-based conversions — used for probe-specific
// scalings not derivable from units alone).
func convertValue(raw float64, sourceUnit, otelUnit string, valueScale float64) float64 {
	if valueScale != 0 {
		return raw * valueScale
	}

	src := strings.ToLower(strings.TrimSpace(sourceUnit))
	dst := strings.TrimSpace(otelUnit)

	if dst == "1" && (src == "%" || src == "percent") {
		return raw / 100.0
	}

	if dst == "By" {
		switch src {
		case "kb", "kib", "kibibyte", "kilobyte":
			return raw * 1024.0
		case "mb", "mib", "mebibyte", "megabyte":
			return raw * 1048576.0
		case "gb", "gib", "gibibyte", "gigabyte":
			return raw * 1073741824.0
		}
	}

	if dst == "s" {
		switch src {
		case "ms", "millisecond", "milliseconds":
			return raw / 1000.0
		case "us", "μs", "microsecond", "microseconds":
			return raw / 1.0e6
		case "ns", "nanosecond", "nanoseconds":
			return raw / 1.0e9
		case "h", "hour", "hours":
			return raw * 3600.0
		case "d", "day", "days":
			return raw * 86400.0
		}
	}

	if dst == "bit/s" {
		switch src {
		case "mbits/s", "mbps", "megabits/s":
			return raw * 1.0e6
		case "gbits/s", "gbps", "gigabits/s", "gbit/s":
			return raw * 1.0e9
		}
	}

	return raw
}

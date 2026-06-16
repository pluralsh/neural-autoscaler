package forecast

import (
	"fmt"
	"strings"
	"time"
)

// ValueUnit describes how forecast point values should be labeled.
type ValueUnit int

const (
	// UnitMillicores formats CPU usage (metrics-server aggregates pod CPU in millicores).
	UnitMillicores ValueUnit = iota
	// UnitBytes formats memory usage (metrics-server aggregates pod memory in bytes).
	UnitBytes
	// UnitGeneric formats values with fixed decimal precision and no unit suffix.
	UnitGeneric
)

// FormatPoints renders forecast points as offset/value pairs, e.g. "+1m: 250m, +2m: 1.50 cores".
func FormatPoints(step time.Duration, points []float64, unit ValueUnit) string {
	if len(points) == 0 {
		return ""
	}
	parts := make([]string, len(points))
	for i, v := range points {
		parts[i] = fmt.Sprintf("%s: %s", formatStepOffset(step, i), FormatValue(v, unit))
	}
	return strings.Join(parts, ", ")
}

// FormatQuantiles renders quantile forecast series with the same point formatting.
func FormatQuantiles(step time.Duration, quantiles []QuantileSeries, unit ValueUnit) string {
	if len(quantiles) == 0 {
		return ""
	}
	parts := make([]string, len(quantiles))
	for i, q := range quantiles {
		parts[i] = fmt.Sprintf("q%.2f (%s)", q.Level, FormatPoints(step, q.Values, unit))
	}
	return strings.Join(parts, "; ")
}

// FormatValue renders a single metric value with an appropriate unit label.
func FormatValue(v float64, unit ValueUnit) string {
	switch unit {
	case UnitMillicores:
		return formatMillicores(v)
	case UnitBytes:
		return formatBytes(v)
	default:
		return formatGeneric(v)
	}
}

func formatStepOffset(step time.Duration, index int) string {
	d := step * time.Duration(index+1)
	if d <= 0 {
		return "+0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("+%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("+%dm", d/time.Minute)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("+%ds", d/time.Second)
	}
	return "+" + d.String()
}

func formatMillicores(v float64) string {
	if v >= 1000 {
		cores := v / 1000
		if cores >= 10 {
			return fmt.Sprintf("%.1f cores", cores)
		}
		return fmt.Sprintf("%.2f cores", cores)
	}
	return fmt.Sprintf("%.0fm", v)
}

func formatBytes(v float64) string {
	const unit = 1024.0
	if v < unit {
		return fmt.Sprintf("%.0f B", v)
	}
	div, exp := unit, 0
	suffixes := []string{"KiB", "MiB", "GiB", "TiB"}
	for n := v / unit; n >= unit && exp < len(suffixes)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %s", v/div, suffixes[exp])
}

func formatGeneric(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

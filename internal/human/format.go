// Package human formats byte counts, durations, and text for display in the TUI
// and in maintenance command output.
package human

import (
	"fmt"
	"time"
)

// FormatBytes renders a byte count using binary (1024) units.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1fTiB", value/unit)
}

// ShortDuration renders a duration with a coarse precision suitable for logs
// and status lines: milliseconds below a second, tenths of a second above.
func ShortDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

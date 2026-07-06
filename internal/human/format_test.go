package human

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{name: "bytes", n: 512, want: "512B"},
		{name: "kibibytes", n: 1024, want: "1.0KiB"},
		{name: "mebibytes", n: 1024 * 1024, want: "1.0MiB"},
		{name: "gibibytes", n: 1024 * 1024 * 1024, want: "1.0GiB"},
		{name: "tebibytes", n: 1024 * 1024 * 1024 * 1024, want: "1.0TiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.n); got != tt.want {
				t.Fatalf("FormatBytes(%d)=%q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "milliseconds", d: 250 * time.Millisecond, want: "250ms"},
		{name: "round subsecond", d: 1500 * time.Microsecond, want: "2ms"},
		{name: "tenths above second", d: 1234 * time.Millisecond, want: "1.2s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortDuration(tt.d); got != tt.want {
				t.Fatalf("ShortDuration(%s)=%q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

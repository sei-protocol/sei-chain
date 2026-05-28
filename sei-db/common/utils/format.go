package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Int64Commas formats n with commas as thousands separators (e.g., 1000000 -> "1,000,000").
func Int64Commas(n int64) string {
	abs := n
	if n < 0 {
		abs = -n
	}
	s := strconv.FormatInt(abs, 10)
	if len(s) <= 3 {
		if n < 0 {
			return "-" + s
		}
		return s
	}
	firstGroupLen := len(s) % 3
	if firstGroupLen == 0 {
		firstGroupLen = 3
	}
	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
	}
	b.WriteString(s[:firstGroupLen])
	for i := firstGroupLen; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// FormatNumberFloat64 formats f with commas in the integer part and the given number of decimal places.
// Special values (NaN, Inf) are formatted as strconv.FormatFloat would.
func FormatNumberFloat64(f float64, decimals int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	format := fmt.Sprintf("%%.%df", decimals)
	s := fmt.Sprintf(format, f)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	parts := strings.SplitN(s, ".", 2)
	integerPart, _ := strconv.ParseInt(parts[0], 10, 64)
	if neg {
		integerPart = -integerPart
	}
	var b strings.Builder
	if neg && integerPart == 0 {
		b.WriteString("-")
	}
	b.WriteString(Int64Commas(integerPart))
	if len(parts) == 2 {
		b.WriteByte('.')
		b.WriteString(parts[1])
	}
	return b.String()
}

// FormatBytes formats a byte count using the most appropriate binary unit
// (e.g. 1536 -> "1.50 KiB", 2097152 -> "2.00 MiB").
func FormatBytes(b int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	abs := b
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= tib:
		return fmt.Sprintf("%.2f TiB", float64(b)/float64(tib))
	case abs >= gib:
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(gib))
	case abs >= mib:
		return fmt.Sprintf("%.2f MiB", float64(b)/float64(mib))
	case abs >= kib:
		return fmt.Sprintf("%.2f KiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatDuration formats d using the most appropriate unit (days, hours, minutes, seconds, ms, us, ns).
func FormatDuration(d time.Duration, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	format := fmt.Sprintf("%%.%df%%s", decimals)
	ns := d.Nanoseconds()
	abs := ns
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= int64(24*time.Hour):
		return fmt.Sprintf(format, float64(ns)/float64(time.Hour)/24, "d")
	case abs >= int64(time.Hour):
		return fmt.Sprintf(format, float64(ns)/float64(time.Hour), "h")
	case abs >= int64(time.Minute):
		return fmt.Sprintf(format, float64(ns)/float64(time.Minute), "m")
	case abs >= int64(time.Second):
		return fmt.Sprintf(format, float64(ns)/float64(time.Second), "s")
	case abs >= int64(time.Millisecond):
		return fmt.Sprintf(format, float64(ns)/float64(time.Millisecond), "ms")
	case abs >= int64(time.Microsecond):
		return fmt.Sprintf(format, float64(ns)/float64(time.Microsecond), "µs")
	default:
		return fmt.Sprintf(format, float64(ns), "ns")
	}
}

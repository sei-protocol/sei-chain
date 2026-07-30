package cryptosim

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// BytesToHex returns a lowercase hex string with 0x prefix, suitable for printing binary keys or addresses.
func BytesToHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// Get the key for the account ID counter in the database.
// Uses EVMKeyCode with padded keyBytes; EVMKeyNonce requires 20-byte addresses and
// non-standard lengths are routed to EVMKeyMisc which FlatKV ignores.
func AccountIDCounterKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, paddedCounterKey(accountIdCounterKey))
}

// Get the key for the ERC20 contract ID counter in the database.
func Erc20IDCounterKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, paddedCounterKey(erc20IdCounterKey))
}

// Get the key for the block number counter in the database.
func BlockNumberCounterKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, paddedCounterKey(blockNumberCounterKey))
}

// paddedCounterKey pads the string to AddressLen bytes for use with EVM key builders.
func paddedCounterKey(s string) []byte {
	b := make([]byte, keys.AddressLen)
	copy(b, s)
	return b
}

// int64Commas formats n with commas as thousands separators (e.g., 1000000 -> "1,000,000").
func int64Commas(n int64) string {
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

// formatNumberFloat64 formats f with commas in the integer part and the given number of decimal places.
// Special values (NaN, Inf) are formatted as strconv.FormatFloat would.
func formatNumberFloat64(f float64, decimals int) string {
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
	// Handle negative sign - we'll need to format the abs and prepend minus
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
	b.WriteString(int64Commas(integerPart))
	if len(parts) == 2 {
		b.WriteByte('.')
		b.WriteString(parts[1])
	}
	return b.String()
}

// formatDuration formats d using the most appropriate unit (days, hours, minutes, seconds, ms, µs, ns).
func formatDuration(d time.Duration, decimals int) string {
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

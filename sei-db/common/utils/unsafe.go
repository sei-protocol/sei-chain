package utils

import "unsafe"

// UnsafeBytesToString converts a byte slice to a string without copying the data.
// Note that once converted in this way, it is not safe to modify the byte slice for any reason.
func UnsafeBytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b)) //nolint:gosec // documented zero-copy conversion
}

// UnsafeStringToBytes returns the bytes of a string without copying them. The result must never be written
// to.
func UnsafeStringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s)) //nolint:gosec // documented zero-copy conversion
}

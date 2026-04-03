package common

import "time"

// PrefixEnvVar returns the environment variable name with the given prefix and suffix
func PrefixEnvVar(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "_" + suffix
}

// PrefixFlag returns the flag name with the given prefix and suffix
func PrefixFlag(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "." + suffix
}

// ToMilliseconds converts the given duration to milliseconds. Unlike duration.Milliseconds(), this function returns
// a float64 with nanosecond precision (at least, as much precision as floating point numbers allow).
func ToMilliseconds(duration time.Duration) float64 {
	return float64(duration.Nanoseconds()) / float64(time.Millisecond)
}

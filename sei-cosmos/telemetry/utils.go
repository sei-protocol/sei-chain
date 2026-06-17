package telemetry

import "strings"

// DenomClass buckets a denom into a cardinality-bounded class.
// Returns "usei", "ibc", "factory", or "other".
func DenomClass(denom string) string {
	switch {
	case denom == "usei":
		return "usei"
	case strings.HasPrefix(denom, "ibc/"):
		return "ibc"
	case strings.HasPrefix(denom, "factory/"):
		return "factory"
	default:
		return "other"
	}
}

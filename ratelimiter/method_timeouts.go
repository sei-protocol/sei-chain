package ratelimiter

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cast"
)

// ParseMethodTimeouts parses "method=duration" entries (for example "eth_call=60s") into an
// exact-match map and a prefix-match map, for DeadlineConfig.Overrides and
// DeadlineConfig.PrefixOverrides. An entry whose method name ends in "*" (for example
// "debug_trace*=0") matches every method starting with the text before "*" and is returned in
// prefixes rather than exact. Returns an error naming the malformed entry.
func ParseMethodTimeouts(entries []string) (exact map[string]time.Duration, prefixes map[string]time.Duration, err error) {
	exact = make(map[string]time.Duration, len(entries))
	prefixes = make(map[string]time.Duration)
	for _, entry := range entries {
		method, raw, ok := strings.Cut(entry, "=")
		if !ok || method == "" {
			return nil, nil, fmt.Errorf("%q must be formatted as method=duration, for example eth_estimateGasAfterCalls=5m", entry)
		}
		d, err := cast.ToDurationE(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%q: %w", entry, err)
		}
		if d < 0 {
			return nil, nil, fmt.Errorf("%q duration must be >= 0 (0 disables the method deadline), got %s", entry, d)
		}
		if prefix, isWildcard := strings.CutSuffix(method, "*"); isWildcard {
			if prefix == "" || strings.Contains(prefix, "*") {
				return nil, nil, fmt.Errorf("%q: wildcard entry must have a single non-empty prefix before a trailing \"*\", for example debug_trace*", entry)
			}
			prefixes[prefix] = d
			continue
		}
		if strings.Contains(method, "*") {
			return nil, nil, fmt.Errorf("%q: \"*\" is only supported as a trailing wildcard, for example debug_trace*", entry)
		}
		exact[method] = d
	}
	return exact, prefixes, nil
}

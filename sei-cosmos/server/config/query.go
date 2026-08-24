package config

import (
	"fmt"
	"net"
)

const (
	DefaultQueryMaxLimit      = uint64(1_000)
	DefaultQueryMaxOffset     = uint64(10_000)
	DefaultQueryMaxIterations = DefaultQueryMaxOffset + DefaultQueryMaxLimit
)

// QueryConfig holds node-local ABCI/gRPC pagination limits. Runtime reads go
// through baseapp.readQueryConfig; this type supplies template defaults and the
// GetConfig absent-key baseline only.
type QueryConfig struct {
	// DisableLimits turns off all pagination limits on the query path.
	DisableLimits bool `mapstructure:"disable-limits"`

	// TrustedCIDRs is a CIDR allowlist whose callers receive unlimited pagination.
	// Matching uses the direct TCP peer, not forwarded client headers.
	TrustedCIDRs []string `mapstructure:"trusted-cidrs"`

	// MaxLimit is the maximum page size for untrusted query origins.
	MaxLimit uint64 `mapstructure:"max-limit"`

	// MaxOffset is the maximum offset for untrusted query origins.
	MaxOffset uint64 `mapstructure:"max-offset"`

	// MaxIterations is the maximum store entries a single untrusted query may scan.
	MaxIterations uint64 `mapstructure:"max-iterations"`
}

// DefaultQueryConfig returns the default query configuration.
func DefaultQueryConfig() QueryConfig {
	return QueryConfig{
		MaxLimit:      DefaultQueryMaxLimit,
		MaxOffset:     DefaultQueryMaxOffset,
		MaxIterations: DefaultQueryMaxIterations,
	}
}

// ApplyQueryDefaults replaces zero max caps with their package defaults.
func ApplyQueryDefaults(cfg QueryConfig) QueryConfig {
	defaults := DefaultQueryConfig()
	if cfg.MaxLimit == 0 {
		cfg.MaxLimit = defaults.MaxLimit
	}
	if cfg.MaxOffset == 0 {
		cfg.MaxOffset = defaults.MaxOffset
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = defaults.MaxIterations
	}
	return cfg
}

// ValidateQueryConfig checks trusted CIDR entries for unsafe patterns.
func ValidateQueryConfig(cfg QueryConfig) []string {
	var warnings []string
	for _, cidr := range cfg.TrustedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("query.trusted-cidrs contains invalid CIDR %q: %v", cidr, err))
			continue
		}
		if ones, _ := network.Mask.Size(); ones == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"query.trusted-cidrs contains overly broad entry %q; public RPC callers will bypass pagination limits",
				cidr,
			))
		}
	}
	return warnings
}

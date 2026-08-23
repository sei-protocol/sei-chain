package config

import (
	"fmt"
	"net"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

const (
	DefaultQueryMaxLimit      = uint64(1_000)
	DefaultQueryMaxOffset     = uint64(10_000)
	DefaultQueryMaxIterations = uint64(10_000)
)

// QueryConfig holds node-local ABCI/gRPC pagination limits.
type QueryConfig struct {
	// DisableLimits turns off all pagination limits on the query path.
	DisableLimits bool `mapstructure:"disable-limits"`

	// TrustedCIDRs is a CIDR allowlist whose callers receive unlimited pagination.
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

// ParseQueryConfig reads the [query] section from v.
func ParseQueryConfig(v *viper.Viper) (QueryConfig, error) {
	cfg := DefaultQueryConfig()
	if v == nil {
		return cfg, nil
	}

	if v.IsSet("query.disable-limits") {
		cfg.DisableLimits = v.GetBool("query.disable-limits")
	}

	if v.IsSet("query.trusted-cidrs") {
		cidrs, err := cast.ToStringSliceE(v.Get("query.trusted-cidrs"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.trusted-cidrs: %w", err)
		}
		cfg.TrustedCIDRs = cidrs
	}

	if v.IsSet("query.max-limit") {
		limit, err := cast.ToUint64E(v.Get("query.max-limit"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.max-limit: %w", err)
		}
		cfg.MaxLimit = limit
	}

	if v.IsSet("query.max-offset") {
		offset, err := cast.ToUint64E(v.Get("query.max-offset"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.max-offset: %w", err)
		}
		cfg.MaxOffset = offset
	}

	if v.IsSet("query.max-iterations") {
		iterations, err := cast.ToUint64E(v.Get("query.max-iterations"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.max-iterations: %w", err)
		}
		cfg.MaxIterations = iterations
	}

	return cfg, nil
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

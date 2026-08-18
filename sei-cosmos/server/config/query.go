package config

import (
	"fmt"
	"net"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

const (
	DefaultTrustedScanLimit = uint64(100_000)
)

// QueryConfig holds node-local query pagination settings.
type QueryConfig struct {
	// TrustedCIDRs is a CIDR allowlist for relaxed scan limits. Empty means fail closed.
	TrustedCIDRs []string `mapstructure:"trusted-cidrs"`

	// TrustedScanLimit is the max store entries a paginator may scan for trusted origins.
	// Zero means unlimited.
	TrustedScanLimit uint64 `mapstructure:"trusted-scan-limit"`
}

// DefaultQueryConfig returns the default query configuration.
func DefaultQueryConfig() QueryConfig {
	return QueryConfig{
		TrustedCIDRs:     nil,
		TrustedScanLimit: DefaultTrustedScanLimit,
	}
}

// ParseQueryConfig reads the [query] section from v.
func ParseQueryConfig(v *viper.Viper) (QueryConfig, error) {
	cfg := DefaultQueryConfig()
	if v == nil {
		return cfg, nil
	}

	if v.IsSet("query.trusted-cidrs") {
		cidrs, err := cast.ToStringSliceE(v.Get("query.trusted-cidrs"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.trusted-cidrs: %w", err)
		}
		cfg.TrustedCIDRs = cidrs
	}

	if v.IsSet("query.trusted-scan-limit") {
		limit, err := cast.ToUint64E(v.Get("query.trusted-scan-limit"))
		if err != nil {
			return cfg, fmt.Errorf("invalid query.trusted-scan-limit: %w", err)
		}
		cfg.TrustedScanLimit = limit
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
		if cidr == "0.0.0.0/0" || cidr == "::/0" {
			warnings = append(warnings, fmt.Sprintf(
				"query.trusted-cidrs contains overly broad entry %q; public RPC callers will receive relaxed scan limits",
				cidr,
			))
			continue
		}
		if ones, _ := network.Mask.Size(); ones == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"query.trusted-cidrs contains overly broad entry %q; public RPC callers will receive relaxed scan limits",
				cidr,
			))
		}
	}
	return warnings
}

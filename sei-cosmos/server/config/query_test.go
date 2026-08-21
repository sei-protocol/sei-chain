package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestParseQueryConfigDefaults(t *testing.T) {
	cfg, err := ParseQueryConfig(viper.New())
	require.NoError(t, err)
	require.Equal(t, DefaultQueryConfig(), cfg)
}

func TestParseQueryConfigOverrides(t *testing.T) {
	v := viper.New()
	v.Set("query.trusted-cidrs", []string{"127.0.0.1/32"})
	v.Set("query.trusted-scan-limit", 250_000)

	cfg, err := ParseQueryConfig(v)
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1/32"}, cfg.TrustedCIDRs)
	require.Equal(t, uint64(250_000), cfg.TrustedScanLimit)
}

func TestValidateQueryConfigRejectsInvalidCIDR(t *testing.T) {
	warnings := ValidateQueryConfig(QueryConfig{TrustedCIDRs: []string{"not-a-cidr"}})
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "invalid CIDR")
}

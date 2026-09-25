package cosmosmetrics_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/cosmosmetrics"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [cosmos_metrics] section means the
// gauges are off.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := cosmosmetrics.ReadConfig(configtest.AppOpts{})
	require.NoError(t, err, "an absent [cosmos_metrics] section must read cleanly")
	require.Equal(t, cosmosmetrics.DefaultConfig, cfg)
}

func TestReadConfigReadsEveryKey(t *testing.T) {
	wallet := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	token := "0x3894085Ef7Ff0f0aeDf52E2A2704928d1Ec074F1"
	cfg, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
		"cosmos_metrics.enabled":                 "true",
		"cosmos_metrics.refresh_interval":        "30s",
		"cosmos_metrics.denom_exponent":          "18",
		"cosmos_metrics.wallet_addresses":        []string{wallet},
		"cosmos_metrics.bank_transfer_threshold": "42",
		"cosmos_metrics.erc20_tokens":            []string{token},
	})
	require.NoError(t, err)
	want := cosmosmetrics.Config{
		Enabled:               true,
		RefreshInterval:       30 * time.Second,
		DenomExponent:         18,
		WalletAddresses:       []string{wallet},
		BankTransferThreshold: 42,
		ERC20Tokens:           []string{token},
	}
	require.Equal(t, want, cfg)
}

// TestReadConfigValidatesOnlyWhenEnabled keeps a stale wallet list in app.toml from stopping a node that
// does not serve the metrics.
func TestReadConfigValidatesOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		_, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
			"cosmos_metrics.enabled":          enabled,
			"cosmos_metrics.wallet_addresses": []string{"not-an-address"},
		})
		if enabled {
			require.Error(t, err, "an invalid wallet address must be rejected when enabled")
		} else {
			require.NoError(t, err, "wallet addresses must not be validated while disabled")
		}
	}
	_, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
		"cosmos_metrics.enabled":          true,
		"cosmos_metrics.refresh_interval": "-1s",
	})
	require.Error(t, err, "a negative refresh interval must be rejected when enabled")
	_, err = cosmosmetrics.ReadConfig(configtest.AppOpts{
		"cosmos_metrics.enabled":      true,
		"cosmos_metrics.erc20_tokens": []string{"not-an-address"},
	})
	require.Error(t, err, "an invalid token address must be rejected when enabled")
}

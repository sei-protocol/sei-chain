package cosmosmetrics_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/cosmosmetrics"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [cosmos_metrics] section means the
// collectors are off.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := cosmosmetrics.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [cosmos_metrics] section must read cleanly, got %v", err)
	}
	if !reflect.DeepEqual(cfg, cosmosmetrics.DefaultConfig) {
		t.Fatalf("an absent [cosmos_metrics] section resolved to %+v, want the declared defaults %+v",
			cfg, cosmosmetrics.DefaultConfig)
	}
}

func TestReadConfigReadsEveryKey(t *testing.T) {
	wallet := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	cfg, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
		"cosmos_metrics.enabled":                 "true",
		"cosmos_metrics.refresh_interval":        "30s",
		"cosmos_metrics.denom_exponent":          "18",
		"cosmos_metrics.wallet_addresses":        []string{wallet},
		"cosmos_metrics.bank_transfer_threshold": "42",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := cosmosmetrics.Config{
		Enabled:               true,
		RefreshInterval:       30 * time.Second,
		DenomExponent:         18,
		WalletAddresses:       []string{wallet},
		BankTransferThreshold: 42,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

// TestReadConfigValidatesOnlyWhenEnabled keeps a stale wallet list in app.toml from stopping a node that
// does not serve the metrics.
func TestReadConfigValidatesOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		_, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
			"cosmos_metrics.enabled":          enabled,
			"cosmos_metrics.wallet_addresses": []string{"not-an-address"},
		})
		if enabled && err == nil {
			t.Fatal("an invalid wallet address must be rejected when enabled")
		}
		if !enabled && err != nil {
			t.Fatalf("wallet addresses must not be validated while disabled, got %v", err)
		}
	}
	_, err := cosmosmetrics.ReadConfig(configtest.AppOpts{
		"cosmos_metrics.enabled":          true,
		"cosmos_metrics.refresh_interval": "-1s",
	})
	if err == nil {
		t.Fatal("a negative refresh interval must be rejected when enabled")
	}
}

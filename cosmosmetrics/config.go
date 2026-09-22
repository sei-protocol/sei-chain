package cosmosmetrics

import (
	"fmt"
	"time"

	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/spf13/cast"
)

// The keys this package's reader resolves.
const (
	flagEnabled               = "cosmos_metrics.enabled"
	flagRefreshInterval       = "cosmos_metrics.refresh_interval"
	flagDenomExponent         = "cosmos_metrics.denom_exponent"
	flagWalletAddresses       = "cosmos_metrics.wallet_addresses"
	flagBankTransferThreshold = "cosmos_metrics.bank_transfer_threshold"
)

// Config defines configuration for the in-process cosmos_* Prometheus metrics.
type Config struct {
	// Enabled controls whether the cosmos_* collectors register on the node's Prometheus registry.
	Enabled bool `mapstructure:"enabled"`
	// RefreshInterval is the minimum time between two reads of chain state; scrapes in between are
	// served from the previous read.
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	// DenomExponent is the power of ten bond-denom amounts are divided by before being reported.
	DenomExponent uint32 `mapstructure:"denom_exponent"`
	// WalletAddresses are the bech32 accounts whose balances, delegations and rewards are reported.
	WalletAddresses []string `mapstructure:"wallet_addresses"`
	// BankTransferThreshold is the base-unit amount a transfer must reach to be reported as a
	// cosmos_bank_transfer_amount sample.
	BankTransferThreshold uint64 `mapstructure:"bank_transfer_threshold"`
}

var DefaultConfig = Config{
	Enabled:               false,
	RefreshInterval:       15 * time.Second,
	DenomExponent:         6,
	WalletAddresses:       nil,
	BankTransferThreshold: 1_000_000_000_000,
}

// ReadConfig reads the cosmos_metrics section from app options.
func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig
	var err error
	if v := opts.Get(flagEnabled); v != nil {
		if cfg.Enabled, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", flagEnabled, err)
		}
	}
	if v := opts.Get(flagRefreshInterval); v != nil {
		if cfg.RefreshInterval, err = cast.ToDurationE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", flagRefreshInterval, err)
		}
	}
	if v := opts.Get(flagDenomExponent); v != nil {
		if cfg.DenomExponent, err = cast.ToUint32E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", flagDenomExponent, err)
		}
	}
	if v := opts.Get(flagWalletAddresses); v != nil {
		if cfg.WalletAddresses, err = cast.ToStringSliceE(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", flagWalletAddresses, err)
		}
	}
	if v := opts.Get(flagBankTransferThreshold); v != nil {
		if cfg.BankTransferThreshold, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("%s: %w", flagBankTransferThreshold, err)
		}
	}
	if cfg.Enabled {
		if err := cfg.validate(); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.RefreshInterval < 0 {
		return fmt.Errorf("%s: must not be negative, got %s", flagRefreshInterval, c.RefreshInterval)
	}
	for _, addr := range c.WalletAddresses {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return fmt.Errorf("%s: %q: %w", flagWalletAddresses, addr, err)
		}
	}
	return nil
}

// ConfigTemplate is the TOML template for the [cosmos_metrics] section of app.toml.
const ConfigTemplate = `
###############################################################################
###                  Cosmos Metrics Configuration (Auto-managed)            ###
###############################################################################

[cosmos_metrics]

# Serve cosmos_params_*, cosmos_general_*, cosmos_validators_*, cosmos_wallet_*,
# cosmos_oracle_* and cosmos_bank_* gauges on the Tendermint Prometheus endpoint
# ([instrumentation] in config.toml). Off by default.
enabled = {{ .CosmosMetrics.Enabled }}

# Minimum time between two reads of committed state; scrapes in between are served
# from the previous read.
refresh_interval = "{{ .CosmosMetrics.RefreshInterval }}"

# Bond-denom amounts are divided by 10^denom_exponent before being reported.
denom_exponent = {{ .CosmosMetrics.DenomExponent }}

# Bech32 accounts whose balances, delegations, unbondings, redelegations and
# rewards are reported under cosmos_wallet_*.
wallet_addresses = [{{ range $i, $a := .CosmosMetrics.WalletAddresses }}{{ if $i }}, {{ end }}"{{ $a }}"{{ end }}]

# Bank transfers of at least this many base units are reported for five minutes
# under cosmos_bank_transfer_amount.
bank_transfer_threshold = {{ .CosmosMetrics.BankTransferThreshold }}
`

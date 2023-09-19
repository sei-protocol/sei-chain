package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/go-playground/validator/v10"
)

const (
	DenomUSD = "USD"

	defaultListenAddr      = "0.0.0.0:7171"
	defaultSrvWriteTimeout = 15 * time.Second
	defaultSrvReadTimeout  = 15 * time.Second
	defaultProviderTimeout = 100 * time.Millisecond

	// API sources for Sei native oracle price feed - examples include price of BTC, ETH - that applications on Sei can
	// use
	ProviderKraken   = "kraken"
	ProviderBinance  = "binance"
	ProviderCrypto   = "crypto"
	ProviderMexc     = "mexc"
	ProviderHuobi    = "huobi"
	ProviderOkx      = "okx"
	ProviderGate     = "gate"
	ProviderCoinbase = "coinbase"
	ProviderMock     = "mock"
)

var (
	validate = validator.New()

	// ErrEmptyConfigPath defines a sentinel error for an empty config path.
	ErrEmptyConfigPath = errors.New("empty configuration file path")

	// SupportedProviders is a mapping of all API sources for Sei native oracle price feed - examples include price of
	// BTC, ETH - that applications on Sei can use
	SupportedProviders = map[string]struct{}{
		ProviderKraken:   {},
		ProviderBinance:  {},
		ProviderCrypto:   {},
		ProviderMexc:     {},
		ProviderOkx:      {},
		ProviderHuobi:    {},
		ProviderGate:     {},
		ProviderCoinbase: {},
		ProviderMock:     {},
	}

	// maxDeviationThreshold is the maxmimum allowed amount of standard
	// deviations which validators are able to set for a given asset.
	maxDeviationThreshold = sdk.MustNewDecFromStr("3.0")

	// SupportedQuotes defines a lookup table for which assets we support
	// using as quotes.
	SupportedQuotes = map[string]struct{}{
		DenomUSD:  {},
		"AXLUSDC": {},
		"USDC":    {},
		"USDT":    {},
		"DAI":     {},
		"BTC":     {},
		"ETH":     {},
		"ATOM":    {},
	}
)

type (
	// Config defines all necessary price-feeder configuration parameters.
	Config struct {
		Server            Server             `toml:"server"`
		CurrencyPairs     []CurrencyPair     `toml:"currency_pairs" validate:"required,gt=0,dive,required"`
		Deviations        []Deviation        `toml:"deviation_thresholds"`
		Account           Account            `toml:"account" validate:"required,gt=0,dive,required"`
		Keyring           Keyring            `toml:"keyring" validate:"required,gt=0,dive,required"`
		RPC               RPC                `toml:"rpc" validate:"required,gt=0,dive,required"`
		Telemetry         Telemetry          `toml:"telemetry"`
		GasAdjustment     float64            `toml:"gas_adjustment" validate:"required"`
		GasPrices         string             `toml:"gas_prices" validate:"required"`
		ProviderTimeout   string             `toml:"provider_timeout"`
		ProviderEndpoints []ProviderEndpoint `toml:"provider_endpoints" validate:"dive"`
		EnableServer      bool               `toml:"enable_server"`
		EnableVoter       bool               `toml:"enable_voter"`
		Healthchecks      []Healthchecks     `toml:"healthchecks" validate:"dive"`
	}

	// Server defines the API server configuration.
	Server struct {
		ListenAddr     string   `toml:"listen_addr"`
		WriteTimeout   string   `toml:"write_timeout"`
		ReadTimeout    string   `toml:"read_timeout"`
		VerboseCORS    bool     `toml:"verbose_cors"`
		AllowedOrigins []string `toml:"allowed_origins"`
	}

	// CurrencyPair defines a price quote of the exchange rate for two different
	// currencies and the supported providers for getting the exchange rate.
	CurrencyPair struct {
		Base       string   `toml:"base" validate:"required"`
		ChainDenom string   `toml:"chain_denom" validate:"required"`
		Quote      string   `toml:"quote" validate:"required"`
		Providers  []string `toml:"providers" validate:"required,gt=0,dive,required"`
	}

	// Deviation defines a maximum amount of standard deviations that a given asset can
	// be from the median without being filtered out before voting.
	Deviation struct {
		Base      string `toml:"base" validate:"required"`
		Threshold string `toml:"threshold" validate:"required"`
	}

	// Account defines account related configuration that is related to the
	// network and transaction signing functionality.
	Account struct {
		ChainID    string `toml:"chain_id" validate:"required"`
		Address    string `toml:"address" validate:"required"`
		Validator  string `toml:"validator" validate:"required"`
		FeeGranter string `toml:"fee_granter"`
		Prefix     string `toml:"prefix" validate:"required"`
	}

	// Keyring defines the required keyring configuration.
	Keyring struct {
		Backend string `toml:"backend" validate:"required"`
		Dir     string `toml:"dir" validate:"required"`
	}

	// RPC defines RPC configuration of both the gRPC and Tendermint nodes.
	RPC struct {
		TMRPCEndpoint string `toml:"tmrpc_endpoint" validate:"required"`
		GRPCEndpoint  string `toml:"grpc_endpoint" validate:"required"`
		RPCTimeout    string `toml:"rpc_timeout" validate:"required"`
	}

	// Telemetry defines the configuration options for application telemetry.
	Telemetry struct {
		// Prefixed with keys to separate services
		ServiceName string `toml:"service_name" mapstructure:"service-name"`

		// Enabled enables the application telemetry functionality. When enabled,
		// an in-memory sink is also enabled by default. Operators may also enabled
		// other sinks such as Prometheus.
		Enabled bool `toml:"enabled" mapstructure:"enabled"`

		// Enable prefixing gauge values with hostname
		EnableHostname bool `toml:"enable_hostname" mapstructure:"enable-hostname"`

		// Enable adding hostname to labels
		EnableHostnameLabel bool `toml:"enable_hostname_label" mapstructure:"enable-hostname-label"`

		// Enable adding service to labels
		EnableServiceLabel bool `toml:"enable_service_label" mapstructure:"enable-service-label"`

		// GlobalLabels defines a global set of name/value label tuples applied to all
		// metrics emitted using the wrapper functions defined in telemetry package.
		//
		// Example:
		// [["chain_id", "cosmoshub-1"]]
		GlobalLabels [][]string `toml:"global_labels" mapstructure:"global-labels"`

		// PrometheusRetentionTime, when positive, enables a Prometheus metrics sink.
		// It defines the retention duration in seconds.
		PrometheusRetentionTime int64 `toml:"prometheus_retention" mapstructure:"prometheus-retention-time"`
	}

	// ProviderEndpoint defines an override setting in our config for the
	// hardcoded rest and websocket api endpoints.
	ProviderEndpoint struct {
		// Name of the provider, ex. "binance"
		Name string `toml:"name"`

		// Rest endpoint for the provider, ex. "https://api1.binance.com"
		Rest string `toml:"rest"`

		// Websocket endpoint for the provider, ex. "stream.binance.com:9443"
		Websocket string `toml:"websocket"`
	}

	Healthchecks struct {
		URL     string `toml:"url" validate:"required"`
		Timeout string `toml:"timeout" validate:"required"`
	}
)

// telemetryValidation is custom validation for the Telemetry struct.
func telemetryValidation(sl validator.StructLevel) {
	tel := sl.Current().Interface().(Telemetry)

	if tel.Enabled && (len(tel.GlobalLabels) == 0 || len(tel.ServiceName) == 0) {
		sl.ReportError(tel.Enabled, "enabled", "Enabled", "enabledNoOptions", "")
	}
}

// endpointValidation is custom validation for the ProviderEndpoint struct.
func endpointValidation(sl validator.StructLevel) {
	endpoint := sl.Current().Interface().(ProviderEndpoint)

	if len(endpoint.Name) < 1 || len(endpoint.Rest) < 1 || len(endpoint.Websocket) < 1 {
		sl.ReportError(endpoint, "endpoint", "Endpoint", "unsupportedEndpointType", "")
	}
	if _, ok := SupportedProviders[endpoint.Name]; !ok {
		sl.ReportError(endpoint.Name, "name", "Name", "unsupportedEndpointProvider", "")
	}
}

// Validate returns an error if the Config object is invalid.
func (c Config) Validate() error {
	validate.RegisterStructValidation(telemetryValidation, Telemetry{})
	validate.RegisterStructValidation(endpointValidation, ProviderEndpoint{})
	return validate.Struct(c)
}

// ParseConfig attempts to read and parse configuration from the given file path.
// An error is returned if reading or parsing the config fails.
func ParseConfig(configPath string) (Config, error) {
	var cfg Config

	if configPath == "" {
		return cfg, ErrEmptyConfigPath
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return cfg, fmt.Errorf("failed to read config: %w", err)
	}

	if _, err := toml.Decode(string(configData), &cfg); err != nil {
		return cfg, fmt.Errorf("failed to decode config: %w", err)
	}

	if cfg.Server.ListenAddr == "" {
		cfg.Server.ListenAddr = defaultListenAddr
	}
	if len(cfg.Server.WriteTimeout) == 0 {
		cfg.Server.WriteTimeout = defaultSrvWriteTimeout.String()
	}
	if len(cfg.Server.ReadTimeout) == 0 {
		cfg.Server.ReadTimeout = defaultSrvReadTimeout.String()
	}
	if len(cfg.ProviderTimeout) == 0 {
		cfg.ProviderTimeout = defaultProviderTimeout.String()
	}

	pairs := make(map[string]map[string]struct{})
	coinQuotes := make(map[string]struct{})
	for _, cp := range cfg.CurrencyPairs {
		if _, ok := pairs[cp.Base]; !ok {
			pairs[cp.Base] = make(map[string]struct{})
		}
		if strings.ToUpper(cp.Quote) != DenomUSD {
			coinQuotes[cp.Quote] = struct{}{}
		}
		if _, ok := SupportedQuotes[strings.ToUpper(cp.Quote)]; !ok {
			return cfg, fmt.Errorf("unsupported quote: %s", cp.Quote)
		}

		for _, provider := range cp.Providers {
			if _, ok := SupportedProviders[provider]; !ok {
				return cfg, fmt.Errorf("unsupported provider: %s", provider)
			}
			pairs[cp.Base][provider] = struct{}{}
		}
	}

	// Use coinQuotes to ensure that any quotes can be converted to USD.
	for quote := range coinQuotes {
		for index, pair := range cfg.CurrencyPairs {
			if pair.Base == quote && pair.Quote == DenomUSD {
				break
			}
			if index == len(cfg.CurrencyPairs)-1 {
				return cfg, fmt.Errorf("all non-usd quotes require a conversion rate feed")
			}
		}
	}

	for base, providers := range pairs {
		if _, ok := pairs[base]["mock"]; !ok && len(providers) < 3 {
			return cfg, fmt.Errorf("must have at least three providers for %s", base)
		}
	}

	for _, deviation := range cfg.Deviations {
		threshold, err := sdk.NewDecFromStr(deviation.Threshold)
		if err != nil {
			return cfg, fmt.Errorf("deviation thresholds must be numeric: %w", err)
		}

		if threshold.GT(maxDeviationThreshold) {
			return cfg, fmt.Errorf("deviation thresholds must not exceed 3.0")
		}
	}

	return cfg, cfg.Validate()
}

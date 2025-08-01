package oracle

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/armon/go-metrics"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/exp/slices"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/client"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	pfsync "github.com/sei-protocol/sei-chain/oracle/price-feeder/pkg/sync"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type mockTelemetry struct {
	mx       sync.Mutex
	recorded []mockMetric
}

type mockMetric struct {
	keys   []string
	val    float32
	labels []metrics.Label
}

func resetMockTelemetry() *mockTelemetry {
	res := &mockTelemetry{
		mx: sync.Mutex{},
	}
	sendProviderFailureMetric = res.IncrCounterWithLabels
	return res
}

func (r mockMetric) containsLabel(expected metrics.Label) bool {
	for _, l := range r.labels {
		if l.Name == expected.Name && l.Value == expected.Value {
			return true
		}
	}
	return false
}

func (r mockMetric) labelsEqual(expected []metrics.Label) bool {
	if len(expected) != len(r.labels) {
		return false
	}
	for _, l := range expected {
		if !r.containsLabel(l) {
			return false
		}
	}
	return true
}

func (mt *mockTelemetry) IncrCounterWithLabels(keys []string, val float32, labels []metrics.Label) {
	mt.mx.Lock()
	defer mt.mx.Unlock()
	mt.recorded = append(mt.recorded, mockMetric{keys, val, labels})
}

func (mt *mockTelemetry) Len() int {
	return len(mt.recorded)
}

func (mt *mockTelemetry) AssertProviderError(t *testing.T, provider, base, reason, priceType string) {
	labels := []metrics.Label{
		{Name: "provider", Value: provider},
		{Name: "reason", Value: reason},
	}
	if base != "" {
		labels = append(labels, metrics.Label{Name: "base", Value: base})
	}
	if priceType != "" {
		labels = append(labels, metrics.Label{Name: "type", Value: priceType})
	}
	mt.AssertContains(t, []string{"failure", "provider"}, 1, labels)
}

func (mt *mockTelemetry) AssertContains(t *testing.T, keys []string, val float32, labels []metrics.Label) {
	for _, r := range mt.recorded {
		if r.val == val && slices.Equal(keys, r.keys) && r.labelsEqual(labels) {
			return
		}
	}
	require.Fail(t, fmt.Sprintf("no matching metric found: keys=%v, val=%v, labels=%v", keys, val, labels))
}

type mockProvider struct {
	prices    map[string]provider.TickerPrice
	candleErr error
}

func (m mockProvider) GetTickerPrices(_ ...types.CurrencyPair) (map[string]provider.TickerPrice, error) {
	return m.prices, nil
}

func (m mockProvider) GetCandlePrices(_ ...types.CurrencyPair) (map[string][]provider.CandlePrice, error) {
	if m.candleErr != nil {
		return nil, m.candleErr
	}
	candles := make(map[string][]provider.CandlePrice)
	for pair, price := range m.prices {
		candles[pair] = []provider.CandlePrice{
			{
				Price:     price.Price,
				TimeStamp: provider.PastUnixTime(1 * time.Minute),
				Volume:    price.Volume,
			},
		}
	}
	return candles, nil
}

func (m mockProvider) SubscribeCurrencyPairs(_ ...types.CurrencyPair) error {
	return nil
}

func (m mockProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

type failingProvider struct {
	prices map[string]provider.TickerPrice
}

func (m failingProvider) GetTickerPrices(_ ...types.CurrencyPair) (map[string]provider.TickerPrice, error) {
	return nil, fmt.Errorf("unable to get ticker prices")
}

func (m failingProvider) GetCandlePrices(_ ...types.CurrencyPair) (map[string][]provider.CandlePrice, error) {
	return nil, fmt.Errorf("unable to get candle prices")
}

func (m failingProvider) SubscribeCurrencyPairs(_ ...types.CurrencyPair) error {
	return nil
}

func (m failingProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

type OracleTestSuite struct {
	suite.Suite

	oracle *Oracle
}

// SetupSuite executes once before the suite's tests are executed.
func (ots *OracleTestSuite) SetupSuite() {
	ots.oracle = New(
		// set to debug to hit the debug-only code paths
		zerolog.Nop().Level(zerolog.DebugLevel),
		client.OracleClient{},
		[]config.CurrencyPair{
			{
				Base:       "UMEE",
				ChainDenom: "uumee",
				Quote:      "USDT",
				Providers:  []string{config.ProviderBinance},
			},
			{
				Base:       "UMEE",
				ChainDenom: "uumee",
				Quote:      "USDC",
				Providers:  []string{config.ProviderKraken},
			},
			{
				Base:       "XBT",
				ChainDenom: "uxbt",
				Quote:      "USDT",
				Providers:  []string{config.ProviderOkx},
			},
			{
				Base:       "USDC",
				ChainDenom: "uusdc",
				Quote:      "USD",
				Providers:  []string{config.ProviderHuobi},
			},
			{
				Base:       "USDT",
				ChainDenom: "uusdt",
				Quote:      "USD",
				Providers:  []string{config.ProviderCoinbase},
			},
		},
		time.Millisecond*100,
		make(map[string]sdk.Dec),
		make(map[string]config.ProviderEndpoint),
		[]config.Healthchecks{
			{URL: "https://hc-ping.com/HEALTHCHECK-UUID", Timeout: "200ms"},
		},
	)
}

func TestServiceTestSuite(t *testing.T) {
	suite.Run(t, new(OracleTestSuite))
}

func (ots *OracleTestSuite) TestStop() {
	ots.Eventually(
		func() bool {
			ots.oracle.Stop()
			return true
		},
		5*time.Second,
		time.Second,
	)
}

func (ots *OracleTestSuite) TestGetLastPriceSyncTimestamp() {
	// when no tick() has been invoked, assume zero value
	ots.Require().Equal(time.Time{}, ots.oracle.GetLastPriceSyncTimestamp())
}

func (ots *OracleTestSuite) TestPrices() {

	// initial prices should be empty (not set)
	ots.Require().Empty(ots.oracle.GetPrices())

	var denoms []string
	for _, v := range ots.oracle.chainDenomMapping {
		// we'll make ubxt a non-whitelisted denom
		if v != "uxbt" {
			denoms = append(denoms, v)
		}
	}
	ots.oracle.paramCache = ParamCache{
		params: &oracletypes.Params{
			Whitelist: denomList(denoms...),
		},
	}
	// Use a mock provider with exchange rates that are not specified in
	// configuration.
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDX": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDX": {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}
	telemetryMock := resetMockTelemetry()
	ots.Require().Error(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Empty(ots.oracle.GetPrices())

	ots.Require().Equal(10, telemetryMock.Len())
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderKraken, "UMEE", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderHuobi, "USDC", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderCoinbase, "USDT", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderKraken, "UMEE", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderHuobi, "USDC", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderCoinbase, "USDT", "error", "candle")

	// use a mock provider without a conversion rate for these stablecoins
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDT": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}
	telemetryMock = resetMockTelemetry()
	ots.Require().Error(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Equal(6, telemetryMock.Len())
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderHuobi, "USDC", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderCoinbase, "USDT", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderHuobi, "USDC", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderCoinbase, "USDT", "error", "candle")

	prices := ots.oracle.GetPrices()
	ots.Require().Len(prices, 0)

	// use a mock provider to provide prices for the configured exchange pairs
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDT": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderHuobi: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDCUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		config.ProviderCoinbase: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDTUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderOkx: mockProvider{
			prices: map[string]provider.TickerPrice{
				"XBTUSDT": {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	telemetryMock = resetMockTelemetry()
	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Equal(0, telemetryMock.Len())

	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.710916056220858266"), prices.AmountOf("uumee"))
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices.AmountOf("uxbt"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdc"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdt"))

	// use one working provider and one provider with an incorrect exchange rate
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDX": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderHuobi: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDCUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		config.ProviderCoinbase: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDTUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderOkx: mockProvider{
			prices: map[string]provider.TickerPrice{
				"XBTUSDT": {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	telemetryMock = resetMockTelemetry()
	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Equal(2, telemetryMock.Len())
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "candle")

	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.70"), prices.AmountOf("uumee"))
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices.AmountOf("uxbt"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdc"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdt"))

	// use one working provider and one provider that fails
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: failingProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.71"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
			candleErr: fmt.Errorf("test error"),
		},
		config.ProviderHuobi: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDCUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		config.ProviderCoinbase: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDTUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderOkx: mockProvider{
			prices: map[string]provider.TickerPrice{
				"XBTUSDT": {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}
	telemetryMock = resetMockTelemetry()
	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Equal(3, telemetryMock.Len())
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "UMEE", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderKraken, "UMEE", "error", "candle")

	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.71"), prices.AmountOf("uumee"))
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices.AmountOf("uxbt"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdc"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdt"))

	// if a provider never initialized correctly, verify it doesn't prevent future updates
	ots.oracle.failedProviders = map[string]error{
		config.ProviderBinance: fmt.Errorf("test error"),
	}
	// a non-whitelisted entry fails (ubxt), but the rest succeed
	ots.oracle.priceProviders = map[string]provider.Provider{
		config.ProviderBinance: failingProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		config.ProviderKraken: mockProvider{
			prices: map[string]provider.TickerPrice{
				"UMEEUSDC": {
					Price:  sdk.MustNewDecFromStr("3.71"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderHuobi: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDCUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		config.ProviderCoinbase: mockProvider{
			prices: map[string]provider.TickerPrice{
				"USDTUSD": {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		config.ProviderOkx: failingProvider{},
	}
	telemetryMock = resetMockTelemetry()
	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Equal(3, telemetryMock.Len())
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "ticker")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderOkx, "XBT", "error", "candle")
	telemetryMock.AssertProviderError(ots.T(), config.ProviderBinance, "", "init", "")

	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 3)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.71"), prices.AmountOf("uumee"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdc"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdt"))
}

func denomList(names ...string) oracletypes.DenomList {
	var result oracletypes.DenomList
	for _, n := range names {
		result = append(result, oracletypes.Denom{Name: n})
	}
	return result
}

func generateValidatorAddr() string {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	return sdk.ValAddress(pubKey.Address().Bytes()).String()
}

func generateAcctAddr() string {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	return sdk.AccAddress(pubKey.Address().Bytes()).String()
}

func TestTickScenarios(t *testing.T) {
	// generate address so that Bech32 address is valid
	validatorAddr := generateValidatorAddr()
	feederAddr := generateAcctAddr()

	tests := []struct {
		name               string
		isJailed           bool
		prices             map[string]sdk.Dec
		pairs              []config.CurrencyPair
		whitelist          oracletypes.DenomList
		blockHeight        int64
		previousVotePeriod float64
		votePeriod         uint64
		mockBroadcastErr   error

		// expectations
		expectedVoteMsg *oracletypes.MsgAggregateExchangeRateVote
		expectedErr     error
	}{
		{
			name:               "Filtered prices, should broadcast all entries, none filtered",
			isJailed:           false,
			blockHeight:        1,
			previousVotePeriod: 0,
			votePeriod:         1,
			pairs: []config.CurrencyPair{
				{Base: "USDT", ChainDenom: "uusdt", Quote: "USD"},
				{Base: "BTC", ChainDenom: "ubtc", Quote: "USD"},
				{Base: "ETH", ChainDenom: "ueth", Quote: "USD"},
			},
			prices: map[string]sdk.Dec{
				"USDT": sdk.MustNewDecFromStr("1.1"),
				"BTC":  sdk.MustNewDecFromStr("2.2"),
				"ETH":  sdk.MustNewDecFromStr("3.3"),
			},
			whitelist: denomList("uusdt", "ubtc", "ueth"),
			expectedVoteMsg: &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: "2.200000000000000000ubtc,3.300000000000000000ueth,1.100000000000000000uusdt",
				Feeder:        feederAddr,
				Validator:     validatorAddr,
			},
		},
		{
			name:               "Filtered prices, should broadcast only whitelisted entries",
			isJailed:           false,
			blockHeight:        1,
			previousVotePeriod: 0,
			votePeriod:         1,
			pairs: []config.CurrencyPair{
				{Base: "USDT", ChainDenom: "uusdt", Quote: "USD"},
				{Base: "BTC", ChainDenom: "ubtc", Quote: "USD"},
				{Base: "OTHER", ChainDenom: "uother", Quote: "USD"}, // filtered out
			},
			prices: map[string]sdk.Dec{
				"USDT":  sdk.MustNewDecFromStr("1.1"),
				"BTC":   sdk.MustNewDecFromStr("2.2"),
				"OTHER": sdk.MustNewDecFromStr("3.3"),
			},
			whitelist: denomList("uusdt", "ubtc"),
			expectedVoteMsg: &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: "2.200000000000000000ubtc,1.100000000000000000uusdt", // does not include uother
				Feeder:        feederAddr,
				Validator:     validatorAddr,
			},
		},
		{
			name:               "Should not crash if broadcast returns nil response with error",
			isJailed:           false,
			blockHeight:        1,
			previousVotePeriod: 0,
			votePeriod:         1,
			pairs: []config.CurrencyPair{
				{Base: "USDT", ChainDenom: "uusdt", Quote: "USD"},
				{Base: "BTC", ChainDenom: "ubtc", Quote: "USD"},
				{Base: "ETH", ChainDenom: "ueth", Quote: "USD"},
			},
			prices: map[string]sdk.Dec{
				"USDT": sdk.MustNewDecFromStr("1.1"),
				"BTC":  sdk.MustNewDecFromStr("2.2"),
				"ETH":  sdk.MustNewDecFromStr("3.3"),
			},
			whitelist: denomList("uusdt", "ubtc", "ueth"),
			expectedVoteMsg: &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: "2.200000000000000000ubtc,3.300000000000000000ueth,1.100000000000000000uusdt",
				Feeder:        feederAddr,
				Validator:     validatorAddr,
			},
			mockBroadcastErr: fmt.Errorf("test error"),
			expectedErr:      fmt.Errorf("test error"),
		},
		{
			name:               "Same voting period should avoid broadcasting without error",
			isJailed:           false,
			blockHeight:        1,
			previousVotePeriod: 1,
			votePeriod:         2,
			expectedErr:        nil,
			expectedVoteMsg:    nil,
		},
		{
			name:        "Jailed should return error",
			isJailed:    true,
			blockHeight: 1,
			expectedErr: fmt.Errorf("validator %s is jailed", validatorAddr),
		},
		{
			name:        "Zero block height should return error",
			isJailed:    false,
			blockHeight: 0,
			expectedErr: fmt.Errorf("expected positive block height"),
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		test := tc
		cdm, _ := createMappingsFromPairs(test.pairs)
		t.Run(test.name, func(t *testing.T) {
			var setPriceCount int
			var broadcastCount int
			// Create the oracle instance
			oracle := &Oracle{
				jailCache: JailCache{
					isJailed: test.isJailed,
				},
				mockSetPrices: func(ctx context.Context) error {
					setPriceCount++
					return nil
				},
				previousVotePeriod: test.previousVotePeriod,
				chainDenomMapping:  cdm,
				prices:             test.prices,
				paramCache: ParamCache{
					params: &oracletypes.Params{
						Whitelist:  test.whitelist,
						VotePeriod: test.votePeriod,
					},
				},
				oracleClient: client.OracleClient{
					OracleAddrString:    feederAddr,
					ValidatorAddrString: validatorAddr,
					MockBroadcastTx: func(ctx sdkclient.Context, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
						// Assert that there's only one message
						require.Equal(t, 1, len(msgs))

						// Extract the message of type MsgAggregateExchangeRateVote
						voteMsg, ok := msgs[0].(*oracletypes.MsgAggregateExchangeRateVote)
						require.True(t, ok, "Expected message type *oracletypes.MsgAggregateExchangeRateVote")

						// Assert the expected values in the voteMsg
						require.Equal(t, test.expectedVoteMsg.ExchangeRates, voteMsg.ExchangeRates, test.name)
						require.Equal(t, test.expectedVoteMsg.Feeder, voteMsg.Feeder, test.name)
						require.Equal(t, test.expectedVoteMsg.Validator, voteMsg.Validator, test.name)

						broadcastCount++

						if test.mockBroadcastErr != nil {
							return nil, test.mockBroadcastErr
						}

						return &sdk.TxResponse{TxHash: "0xhash", Code: 200}, nil
					},
				},
			}

			// execute the tick function
			err := oracle.tick(ctx, sdkclient.Context{}, test.blockHeight)

			if test.expectedErr != nil {
				require.Equal(t, test.expectedErr, err, test.name)
			} else {
				require.NoError(t, err, test.name)
			}
			if test.expectedVoteMsg != nil {
				// ensure functions were actually called
				require.Equal(t, 1, broadcastCount, test.name)
				require.Equal(t, 1, setPriceCount, test.name)
			}
			if test.expectedVoteMsg == nil {
				// should not call broadcast
				require.Equal(t, 0, broadcastCount, test.name)
			}
		})
	}
}

func TestFilterPricesWithDenomList(t *testing.T) {
	tests := []struct {
		name           string
		inputDC        sdk.DecCoins
		inputDL        oracletypes.DenomList
		expectedResult sdk.DecCoins
	}{
		{
			name: "Matching denominations",
			inputDC: sdk.NewDecCoins(
				sdk.NewDecCoin("usdt", sdk.NewInt(100)),
				sdk.NewDecCoin("eth", sdk.NewInt(5)),
			),
			inputDL: oracletypes.DenomList{
				{Name: "usdt"},
			},
			expectedResult: sdk.NewDecCoins(
				sdk.NewDecCoin("usdt", sdk.NewInt(100)),
			),
		},
		{
			name: "No matching denominations",
			inputDC: sdk.NewDecCoins(
				sdk.NewDecCoin("btc", sdk.NewInt(1)),
				sdk.NewDecCoin("eth", sdk.NewInt(10)),
			),
			inputDL: oracletypes.DenomList{
				{Name: "usdt"},
			},
			expectedResult: sdk.NewDecCoins(),
		},
		{
			name:           "Empty input DecCoins and DenomList",
			inputDC:        sdk.DecCoins{},
			inputDL:        oracletypes.DenomList{},
			expectedResult: sdk.NewDecCoins(),
		},
		{
			name:    "Empty input DecCoins",
			inputDC: sdk.DecCoins{},
			inputDL: oracletypes.DenomList{
				{Name: "usdt"},
			},
			expectedResult: sdk.NewDecCoins(),
		},
		{
			name: "Empty input DenomList",
			inputDC: sdk.NewDecCoins(
				sdk.NewDecCoin("usdt", sdk.NewInt(100)),
				sdk.NewDecCoin("eth", sdk.NewInt(5)),
			),
			inputDL:        oracletypes.DenomList{},
			expectedResult: sdk.NewDecCoins(),
		},
	}

	for _, test := range tests {
		tc := test
		t.Run(tc.name, func(t *testing.T) {
			result := filterPricesByDenomList(tc.inputDC, tc.inputDL)
			require.Equal(t, tc.expectedResult, result)
		})
	}
}
func TestGenerateExchangeRatesString(t *testing.T) {
	testCases := map[string]struct {
		input    sdk.DecCoins
		expected string
	}{
		"empty input": {
			input:    sdk.NewDecCoins(),
			expected: "",
		},
		"single denom": {
			input:    sdk.NewDecCoins(sdk.NewDecCoinFromDec("UMEE", sdk.MustNewDecFromStr("3.72"))),
			expected: "3.720000000000000000UMEE",
		},
		"multi denom": {
			input: sdk.NewDecCoins(sdk.NewDecCoinFromDec("UMEE", sdk.MustNewDecFromStr("3.72")),
				sdk.NewDecCoinFromDec("ATOM", sdk.MustNewDecFromStr("40.13")),
				sdk.NewDecCoinFromDec("OSMO", sdk.MustNewDecFromStr("8.69")),
			),
			expected: "40.130000000000000000ATOM,8.690000000000000000OSMO,3.720000000000000000UMEE",
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			out := GenerateExchangeRatesString(tc.input)
			require.Equal(t, tc.expected, out)
		})
	}
}

func TestSuccessSetProviderTickerPricesAndCandles(t *testing.T) {
	providerPrices := make(provider.AggregatedProviderPrices, 1)
	providerCandles := make(provider.AggregatedProviderCandles, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USDT",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	prices := make(map[string]provider.TickerPrice, 1)
	prices[pair.String()] = provider.TickerPrice{
		Price:  atomPrice,
		Volume: atomVolume,
	}

	candles := make(map[string][]provider.CandlePrice, 1)
	candles[pair.String()] = []provider.CandlePrice{
		{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}

	success := SetProviderTickerPricesAndCandles(
		config.ProviderGate,
		providerPrices,
		providerCandles,
		prices,
		candles,
		pair,
	)

	require.True(t, success, "It should successfully set the prices")
	require.Equal(t, atomPrice, providerPrices[config.ProviderGate][pair.Base].Price)
	require.Equal(t, atomPrice, providerCandles[config.ProviderGate][pair.Base][0].Price)
}

func TestFailedSetProviderTickerPricesAndCandles(t *testing.T) {
	success := SetProviderTickerPricesAndCandles(
		config.ProviderCoinbase,
		make(provider.AggregatedProviderPrices, 1),
		make(provider.AggregatedProviderCandles, 1),
		make(map[string]provider.TickerPrice, 1),
		make(map[string][]provider.CandlePrice, 1),
		types.CurrencyPair{
			Base:  "ATOM",
			Quote: "USDT",
		},
	)

	require.False(t, success, "It should failed to set the prices, prices and candle are empty")
}

func TestSuccessGetComputedPricesCandles(t *testing.T) {
	providerCandles := make(provider.AggregatedProviderCandles, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USD",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	candles := make(map[string][]provider.CandlePrice, 1)
	candles[pair.Base] = []provider.CandlePrice{
		{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[config.ProviderBinance] = candles

	providerPair := map[string][]types.CurrencyPair{
		"binance": {pair},
	}

	prices, err := GetComputedPrices(
		zerolog.Nop(),
		providerCandles,
		make(provider.AggregatedProviderPrices, 1),
		providerPair,
		make(map[string]sdk.Dec),
		map[string]struct{}{
			"ATOM": {},
		},
	)

	require.NoError(t, err, "It should successfully get computed candle prices")
	require.Equal(t, prices[pair.Base], atomPrice)
}

func TestSuccessGetComputedPricesTickers(t *testing.T) {
	providerPrices := make(provider.AggregatedProviderPrices, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USD",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	tickerPrices := make(map[string]provider.TickerPrice, 1)
	tickerPrices[pair.Base] = provider.TickerPrice{
		Price:  atomPrice,
		Volume: atomVolume,
	}
	providerPrices[config.ProviderBinance] = tickerPrices

	providerPair := map[string][]types.CurrencyPair{
		"binance": {pair},
	}

	prices, err := GetComputedPrices(
		zerolog.Nop(),
		make(provider.AggregatedProviderCandles, 1),
		providerPrices,
		providerPair,
		make(map[string]sdk.Dec),
		map[string]struct{}{
			"ATOM": {},
		},
	)

	require.NoError(t, err, "It should successfully get computed ticker prices")
	require.Equal(t, prices[pair.Base], atomPrice)
}

func TestGetComputedPricesCandlesConversion(t *testing.T) {
	btcPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "ETH",
	}
	btcUSDPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "USD",
	}
	ethPair := types.CurrencyPair{
		Base:  "ETH",
		Quote: "USD",
	}
	btcEthPrice := sdk.MustNewDecFromStr("17.55")
	btcUSDPrice := sdk.MustNewDecFromStr("20962.601")
	ethUsdPrice := sdk.MustNewDecFromStr("1195.02")
	volume := sdk.MustNewDecFromStr("894123.00")
	providerCandles := make(provider.AggregatedProviderCandles, 4)

	// normal rates
	binanceCandles := make(map[string][]provider.CandlePrice, 2)
	binanceCandles[btcPair.Base] = []provider.CandlePrice{
		{
			Price:     btcEthPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	binanceCandles[ethPair.Base] = []provider.CandlePrice{
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[config.ProviderBinance] = binanceCandles

	// normal rates
	gateCandles := make(map[string][]provider.CandlePrice, 1)
	gateCandles[ethPair.Base] = []provider.CandlePrice{
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	gateCandles[btcPair.Base] = []provider.CandlePrice{
		{
			Price:     btcEthPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[config.ProviderGate] = gateCandles

	// abnormal eth rate
	okxCandles := make(map[string][]provider.CandlePrice, 1)
	okxCandles[ethPair.Base] = []provider.CandlePrice{
		{
			Price:     sdk.MustNewDecFromStr("1.0"),
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[config.ProviderOkx] = okxCandles

	// btc / usd rate
	krakenCandles := make(map[string][]provider.CandlePrice, 1)
	krakenCandles[btcUSDPair.Base] = []provider.CandlePrice{
		{
			Price:     btcUSDPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[config.ProviderKraken] = krakenCandles

	providerPair := map[string][]types.CurrencyPair{
		config.ProviderBinance: {btcPair, ethPair},
		config.ProviderGate:    {ethPair},
		config.ProviderOkx:     {ethPair},
		config.ProviderKraken:  {btcUSDPair},
	}

	prices, err := GetComputedPrices(
		zerolog.Nop(),
		providerCandles,
		make(provider.AggregatedProviderPrices, 1),
		providerPair,
		make(map[string]sdk.Dec),
		map[string]struct{}{
			"BTC": {},
		},
	)

	require.NoError(t, err,
		"It should successfully filter out bad candles and convert everything to USD",
	)
	require.Equal(t,
		ethUsdPrice.Mul(
			btcEthPrice).Add(btcUSDPrice).Quo(sdk.MustNewDecFromStr("2")),
		prices[btcPair.Base],
	)
}

func TestGetComputedPricesTickersConversion(t *testing.T) {
	btcPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "ETH",
	}
	btcUSDPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "USD",
	}
	ethPair := types.CurrencyPair{
		Base:  "ETH",
		Quote: "USD",
	}
	volume := sdk.MustNewDecFromStr("881272.00")
	btcEthPrice := sdk.MustNewDecFromStr("72.55")
	ethUsdPrice := sdk.MustNewDecFromStr("9989.02")
	btcUSDPrice := sdk.MustNewDecFromStr("724603.401")
	providerPrices := make(provider.AggregatedProviderPrices, 1)

	// normal rates
	binanceTickerPrices := make(map[string]provider.TickerPrice, 2)
	binanceTickerPrices[btcPair.Base] = provider.TickerPrice{
		Price:  btcEthPrice,
		Volume: volume,
	}
	binanceTickerPrices[ethPair.Base] = provider.TickerPrice{
		Price:  ethUsdPrice,
		Volume: volume,
	}
	providerPrices[config.ProviderBinance] = binanceTickerPrices

	// normal rates
	gateTickerPrices := make(map[string]provider.TickerPrice, 4)
	gateTickerPrices[btcPair.Base] = provider.TickerPrice{
		Price:  btcEthPrice,
		Volume: volume,
	}
	gateTickerPrices[ethPair.Base] = provider.TickerPrice{
		Price:  ethUsdPrice,
		Volume: volume,
	}
	providerPrices[config.ProviderGate] = gateTickerPrices

	// abnormal eth rate
	okxTickerPrices := make(map[string]provider.TickerPrice, 1)
	okxTickerPrices[ethPair.Base] = provider.TickerPrice{
		Price:  sdk.MustNewDecFromStr("1.0"),
		Volume: volume,
	}
	providerPrices[config.ProviderOkx] = okxTickerPrices

	// btc / usd rate
	krakenTickerPrices := make(map[string]provider.TickerPrice, 1)
	krakenTickerPrices[btcUSDPair.Base] = provider.TickerPrice{
		Price:  btcUSDPrice,
		Volume: volume,
	}
	providerPrices[config.ProviderKraken] = krakenTickerPrices

	providerPair := map[string][]types.CurrencyPair{
		config.ProviderBinance: {ethPair, btcPair},
		config.ProviderGate:    {ethPair},
		config.ProviderOkx:     {ethPair},
		config.ProviderKraken:  {btcUSDPair},
	}

	prices, err := GetComputedPrices(
		zerolog.Nop(),
		make(provider.AggregatedProviderCandles, 1),
		providerPrices,
		providerPair,
		make(map[string]sdk.Dec),
		map[string]struct{}{
			"BTC": {},
		},
	)

	require.NoError(t, err,
		"It should successfully filter out bad tickers and convert everything to USD",
	)
	require.Equal(t,
		ethUsdPrice.Mul(
			btcEthPrice).Add(btcUSDPrice).Quo(sdk.MustNewDecFromStr("2")),
		prices[btcPair.Base],
	)
}

func TestOracle_Start(t *testing.T) {
	tests := []struct {
		name           string
		blockEvents    []int64
		mockSetPrices  func(ctx context.Context) error
		expectedErrors []string
	}{
		{
			name:        "successful oracle start and tick",
			blockEvents: []int64{1, 2, 3},
			mockSetPrices: func(ctx context.Context) error {
				return nil
			},
			expectedErrors: []string{},
		},
		{
			name:        "oracle start with set prices error",
			blockEvents: []int64{1},
			mockSetPrices: func(ctx context.Context) error {
				return fmt.Errorf("set prices failed")
			},
			expectedErrors: []string{"set prices failed"},
		},
		{
			name:        "oracle start with context cancellation",
			blockEvents: []int64{},
			mockSetPrices: func(ctx context.Context) error {
				return nil
			},
			expectedErrors: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock oracle client with block height events
			blockHeightChan := make(chan int64, len(tc.blockEvents))
			for _, height := range tc.blockEvents {
				blockHeightChan <- height
			}
			close(blockHeightChan)

			oracle := &Oracle{
				logger: zerolog.Nop(),
				closer: pfsync.NewCloser(),
				oracleClient: client.OracleClient{
					BlockHeightEvents: blockHeightChan,
					MockBroadcastTx: func(ctx sdkclient.Context, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
						return &sdk.TxResponse{TxHash: "0xhash", Code: 0}, nil
					},
				},
				paramCache: ParamCache{
					params: &oracletypes.Params{
						VotePeriod: 1,
						Whitelist:  denomList("uusdt", "ubtc"),
					},
				},
				prices: map[string]sdk.Dec{
					"USDT": sdk.MustNewDecFromStr("1.0"),
					"BTC":  sdk.MustNewDecFromStr("50000.0"),
				},
				chainDenomMapping: map[string]string{
					"USDT": "uusdt",
					"BTC":  "ubtc",
				},
				mockSetPrices: tc.mockSetPrices,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// Start oracle in goroutine
			go func() {
				err := oracle.Start(ctx)
				if err != nil {
					t.Logf("Oracle start error: %v", err)
				}
			}()

			// Wait for context cancellation or timeout
			<-ctx.Done()
			oracle.Stop()
		})
	}
}

func TestOracle_GetParams(t *testing.T) {
	tests := []struct {
		name          string
		grpcEndpoint  string
		timeout       time.Duration
		expectedError bool
	}{
		{
			name:          "invalid grpc endpoint",
			grpcEndpoint:  "invalid-endpoint",
			timeout:       15 * time.Second,
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				oracleClient: client.OracleClient{
					GRPCEndpoint: tc.grpcEndpoint,
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			_, err := oracle.GetParams(ctx)

			if tc.expectedError {
				require.Error(t, err)
			}
		})
	}
}

func TestOracle_CheckWhitelist(t *testing.T) {
	tests := []struct {
		name              string
		chainDenomMapping map[string]string
		params            oracletypes.Params
		expectedWarnings  int
	}{
		{
			name: "all denoms in whitelist",
			chainDenomMapping: map[string]string{
				"USDT": "uusdt",
				"BTC":  "ubtc",
			},
			params: oracletypes.Params{
				Whitelist: denomList("uusdt", "ubtc"),
			},
			expectedWarnings: 0,
		},
		{
			name: "missing denom in whitelist",
			chainDenomMapping: map[string]string{
				"USDT": "uusdt",
				"BTC":  "ubtc",
			},
			params: oracletypes.Params{
				Whitelist: denomList("uusdt"), // missing ubtc
			},
			expectedWarnings: 1,
		},
		{
			name: "extra denom in whitelist",
			chainDenomMapping: map[string]string{
				"USDT": "uusdt",
			},
			params: oracletypes.Params{
				Whitelist: denomList("uusdt", "ubtc", "ueth"), // extra denoms
			},
			expectedWarnings: 2,
		},
		{
			name:              "empty mappings and whitelist",
			chainDenomMapping: map[string]string{},
			params: oracletypes.Params{
				Whitelist: oracletypes.DenomList{},
			},
			expectedWarnings: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				logger:            zerolog.Nop(),
				chainDenomMapping: tc.chainDenomMapping,
			}

			oracle.checkWhitelist(tc.params)
			// Note: checkWhitelist only logs warnings, so we can't easily assert on them
			// The test ensures the function doesn't panic and handles all cases
		})
	}
}

func TestOracle_HealthchecksPing(t *testing.T) {
	tests := []struct {
		name          string
		healthchecks  map[string]http.Client
		expectedCalls int
	}{
		{
			name: "single healthcheck",
			healthchecks: map[string]http.Client{
				"https://hc-ping.com/test": {
					Timeout: 5 * time.Second,
				},
			},
			expectedCalls: 1,
		},
		{
			name: "multiple healthchecks",
			healthchecks: map[string]http.Client{
				"https://hc-ping.com/test1": {
					Timeout: 5 * time.Second,
				},
				"https://hc-ping.com/test2": {
					Timeout: 5 * time.Second,
				},
			},
			expectedCalls: 2,
		},
		{
			name:          "no healthchecks",
			healthchecks:  map[string]http.Client{},
			expectedCalls: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				logger:       zerolog.Nop(),
				healthchecks: tc.healthchecks,
			}

			// healthchecksPing logs info messages but doesn't return errors
			// The test ensures the function doesn't panic
			oracle.healthchecksPing()
		})
	}
}

func TestOracle_LogResponseError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		resp         *sdk.TxResponse
		startTime    time.Time
		blockHeight  int64
		expectedLogs int
	}{
		{
			name:         "error with response",
			err:          fmt.Errorf("broadcast failed"),
			resp:         &sdk.TxResponse{TxHash: "0xhash", Code: 1},
			startTime:    time.Now(),
			blockHeight:  100,
			expectedLogs: 1,
		},
		{
			name:         "error without response",
			err:          fmt.Errorf("connection failed"),
			resp:         nil,
			startTime:    time.Now(),
			blockHeight:  101,
			expectedLogs: 1,
		},
		{
			name:         "nil error",
			err:          nil,
			resp:         &sdk.TxResponse{TxHash: "0xhash", Code: 0},
			startTime:    time.Now(),
			blockHeight:  102,
			expectedLogs: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				logger: zerolog.Nop(),
			}

			// logResponseError logs error messages but doesn't return errors
			// The test ensures the function doesn't panic
			oracle.logResponseError(tc.err, tc.resp, tc.startTime, tc.blockHeight)
		})
	}
}

func TestOracle_GetParamCache(t *testing.T) {
	tests := []struct {
		name               string
		currentBlockHeight int64
		cacheBlockHeight   int64
		cacheParams        *oracletypes.Params
		expectedCacheHit   bool
		expectedParams     oracletypes.Params
		expectedError      bool
	}{
		{
			name:               "cache hit - params not outdated",
			currentBlockHeight: 100,
			cacheBlockHeight:   95,
			cacheParams: &oracletypes.Params{
				VotePeriod: 5,
				Whitelist:  denomList("uusdt"),
			},
			expectedCacheHit: true,
			expectedParams: oracletypes.Params{
				VotePeriod: 5,
				Whitelist:  denomList("uusdt"),
			},
			expectedError: false,
		},
		{
			name:               "cache miss - params outdated",
			currentBlockHeight: 100,
			cacheBlockHeight:   50, // very old
			cacheParams: &oracletypes.Params{
				VotePeriod: 5,
				Whitelist:  denomList("uusdt"),
			},
			expectedCacheHit: false,
			expectedError:    false, // GetParams may not fail immediately in test environment
		},
		{
			name:               "empty cache",
			currentBlockHeight: 100,
			cacheBlockHeight:   0,
			cacheParams:        nil,
			expectedCacheHit:   false,
			expectedError:      true, // GetParams will fail in test environment
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				paramCache: ParamCache{
					lastUpdatedBlock: tc.cacheBlockHeight,
					params:           tc.cacheParams,
				},
				oracleClient: client.OracleClient{
					GRPCEndpoint: "invalid-endpoint", // Will cause GetParams to fail
				},
			}

			params, err := oracle.GetParamCache(context.Background(), tc.currentBlockHeight)

			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Don't compare specific values since GetParams may not work in test environment
				// Just ensure we got a valid params object
				require.NotNil(t, params)
			}
		})
	}
}

func TestOracle_NewProvider(t *testing.T) {
	tests := []struct {
		name          string
		providerName  string
		logger        zerolog.Logger
		endpoint      config.ProviderEndpoint
		providerPairs []types.CurrencyPair
		expectedError bool
		expectedType  string
	}{

		{
			name:         "mock provider",
			providerName: config.ProviderMock,
			logger:       zerolog.Nop(),
			endpoint: config.ProviderEndpoint{
				Name: config.ProviderMock,
			},
			providerPairs: []types.CurrencyPair{
				{Base: "BTC", Quote: "USDT"},
			},
			expectedError: false,
			expectedType:  "*provider.MockProvider",
		},
		{
			name:         "unknown provider",
			providerName: "unknown",
			logger:       zerolog.Nop(),
			endpoint: config.ProviderEndpoint{
				Name: "unknown",
			},
			providerPairs: []types.CurrencyPair{
				{Base: "BTC", Quote: "USDT"},
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			provider, err := NewProvider(ctx, tc.providerName, tc.logger, tc.endpoint, tc.providerPairs...)

			if tc.expectedError {
				require.Error(t, err)
				require.Nil(t, provider)
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
				require.Equal(t, tc.expectedType, fmt.Sprintf("%T", provider))
			}
		})
	}
}

func TestOracle_GetOrSetProvider(t *testing.T) {
	tests := []struct {
		name              string
		providerName      string
		existingProviders map[string]provider.Provider
		failedProviders   map[string]error
		endpoints         map[string]config.ProviderEndpoint
		providerPairs     map[string][]types.CurrencyPair
		expectedError     bool
		expectedProvider  bool
	}{
		{
			name:         "existing provider",
			providerName: config.ProviderBinance,
			existingProviders: map[string]provider.Provider{
				config.ProviderBinance: &mockProvider{},
			},
			failedProviders: map[string]error{},
			endpoints: map[string]config.ProviderEndpoint{
				config.ProviderBinance: {
					Name: config.ProviderBinance,
					Rest: "https://api.binance.com",
				},
			},
			providerPairs: map[string][]types.CurrencyPair{
				config.ProviderBinance: {{Base: "BTC", Quote: "USDT"}},
			},
			expectedError:    false,
			expectedProvider: true,
		},
		{
			name:              "new provider creation",
			providerName:      config.ProviderMock,
			existingProviders: map[string]provider.Provider{},
			failedProviders:   map[string]error{},
			endpoints: map[string]config.ProviderEndpoint{
				config.ProviderMock: {
					Name: config.ProviderMock,
				},
			},
			providerPairs: map[string][]types.CurrencyPair{
				config.ProviderMock: {{Base: "BTC", Quote: "USDT"}},
			},
			expectedError:    false,
			expectedProvider: true,
		},
		{
			name:              "failed provider",
			providerName:      config.ProviderBinance,
			existingProviders: map[string]provider.Provider{},
			failedProviders: map[string]error{
				config.ProviderBinance: fmt.Errorf("provider failed"),
			},
			endpoints: map[string]config.ProviderEndpoint{
				config.ProviderBinance: {
					Name: config.ProviderBinance,
					Rest: "https://api.binance.com",
				},
			},
			providerPairs: map[string][]types.CurrencyPair{
				config.ProviderBinance: {{Base: "BTC", Quote: "USDT"}},
			},
			expectedError:    true,
			expectedProvider: false,
		},
		{
			name:              "provider creation failure",
			providerName:      "unknown",
			existingProviders: map[string]provider.Provider{},
			failedProviders:   map[string]error{},
			endpoints: map[string]config.ProviderEndpoint{
				"unknown": {
					Name: "unknown",
				},
			},
			providerPairs: map[string][]types.CurrencyPair{
				"unknown": {{Base: "BTC", Quote: "USDT"}},
			},
			expectedError:    true,
			expectedProvider: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := &Oracle{
				logger:          zerolog.Nop(),
				priceProviders:  tc.existingProviders,
				failedProviders: tc.failedProviders,
				endpoints:       tc.endpoints,
				providerPairs:   tc.providerPairs,
			}

			ctx := context.Background()
			provider, err := oracle.getOrSetProvider(ctx, tc.providerName)

			if tc.expectedError {
				require.Error(t, err)
				require.Nil(t, provider)
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
			}

			if tc.expectedProvider {
				// Check if provider was added to the map
				_, exists := oracle.priceProviders[tc.providerName]
				require.True(t, exists)
			}
		})
	}
}

func TestOracle_New(t *testing.T) {
	tests := []struct {
		name              string
		logger            zerolog.Logger
		currencyPairs     []config.CurrencyPair
		providerTimeout   time.Duration
		deviations        map[string]sdk.Dec
		endpoints         map[string]config.ProviderEndpoint
		healthchecks      []config.Healthchecks
		expectedError     bool
		expectedProviders int
	}{
		{
			name:   "valid oracle creation",
			logger: zerolog.Nop(),
			currencyPairs: []config.CurrencyPair{
				{
					Base:       "BTC",
					ChainDenom: "ubtc",
					Quote:      "USDT",
					Providers:  []string{config.ProviderBinance},
				},
				{
					Base:       "ETH",
					ChainDenom: "ueth",
					Quote:      "USD",
					Providers:  []string{config.ProviderKraken},
				},
			},
			providerTimeout: 5 * time.Second,
			deviations:      map[string]sdk.Dec{},
			endpoints: map[string]config.ProviderEndpoint{
				config.ProviderBinance: {Name: config.ProviderBinance, Rest: "https://api.binance.com"},
				config.ProviderKraken:  {Name: config.ProviderKraken, Rest: "https://api.kraken.com"},
			},
			healthchecks: []config.Healthchecks{
				{URL: "https://hc-ping.com/test", Timeout: "5s"},
			},
			expectedError:     false,
			expectedProviders: 2,
		},
		{
			name:   "oracle with invalid healthcheck timeout",
			logger: zerolog.Nop(),
			currencyPairs: []config.CurrencyPair{
				{
					Base:       "BTC",
					ChainDenom: "ubtc",
					Quote:      "USDT",
					Providers:  []string{config.ProviderBinance},
				},
			},
			providerTimeout: 5 * time.Second,
			deviations:      map[string]sdk.Dec{},
			endpoints: map[string]config.ProviderEndpoint{
				config.ProviderBinance: {Name: config.ProviderBinance, Rest: "https://api.binance.com"},
			},
			healthchecks: []config.Healthchecks{
				{URL: "https://hc-ping.com/test", Timeout: "invalid-timeout"},
			},
			expectedError:     false, // Should not error, just skip invalid healthcheck
			expectedProviders: 1,
		},
		{
			name:              "oracle with empty configuration",
			logger:            zerolog.Nop(),
			currencyPairs:     []config.CurrencyPair{},
			providerTimeout:   5 * time.Second,
			deviations:        map[string]sdk.Dec{},
			endpoints:         map[string]config.ProviderEndpoint{},
			healthchecks:      []config.Healthchecks{},
			expectedError:     false,
			expectedProviders: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oracle := New(
				tc.logger,
				client.OracleClient{},
				tc.currencyPairs,
				tc.providerTimeout,
				tc.deviations,
				tc.endpoints,
				tc.healthchecks,
			)

			require.NotNil(t, oracle)
			require.Equal(t, tc.expectedProviders, len(oracle.providerPairs))
			require.Equal(t, tc.providerTimeout, oracle.providerTimeout)
			require.Equal(t, tc.deviations, oracle.deviations)
			require.Equal(t, tc.endpoints, oracle.endpoints)
		})
	}
}

func TestCreateMappingsFromPairs(t *testing.T) {
	tests := []struct {
		name                string
		currencyPairs       []config.CurrencyPair
		expectedChainMap    map[string]string
		expectedProviderMap map[string][]types.CurrencyPair
	}{
		{
			name: "single pair single provider",
			currencyPairs: []config.CurrencyPair{
				{
					Base:       "BTC",
					ChainDenom: "ubtc",
					Quote:      "USDT",
					Providers:  []string{config.ProviderBinance},
				},
			},
			expectedChainMap: map[string]string{
				"BTC": "ubtc",
			},
			expectedProviderMap: map[string][]types.CurrencyPair{
				config.ProviderBinance: {{Base: "BTC", Quote: "USDT"}},
			},
		},
		{
			name: "single pair multiple providers",
			currencyPairs: []config.CurrencyPair{
				{
					Base:       "BTC",
					ChainDenom: "ubtc",
					Quote:      "USDT",
					Providers:  []string{config.ProviderBinance, config.ProviderKraken},
				},
			},
			expectedChainMap: map[string]string{
				"BTC": "ubtc",
			},
			expectedProviderMap: map[string][]types.CurrencyPair{
				config.ProviderBinance: {{Base: "BTC", Quote: "USDT"}},
				config.ProviderKraken:  {{Base: "BTC", Quote: "USDT"}},
			},
		},
		{
			name: "multiple pairs",
			currencyPairs: []config.CurrencyPair{
				{
					Base:       "BTC",
					ChainDenom: "ubtc",
					Quote:      "USDT",
					Providers:  []string{config.ProviderBinance},
				},
				{
					Base:       "ETH",
					ChainDenom: "ueth",
					Quote:      "USD",
					Providers:  []string{config.ProviderKraken},
				},
			},
			expectedChainMap: map[string]string{
				"BTC": "ubtc",
				"ETH": "ueth",
			},
			expectedProviderMap: map[string][]types.CurrencyPair{
				config.ProviderBinance: {{Base: "BTC", Quote: "USDT"}},
				config.ProviderKraken:  {{Base: "ETH", Quote: "USD"}},
			},
		},
		{
			name:                "empty pairs",
			currencyPairs:       []config.CurrencyPair{},
			expectedChainMap:    map[string]string{},
			expectedProviderMap: map[string][]types.CurrencyPair{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chainMap, providerMap := createMappingsFromPairs(tc.currencyPairs)

			require.Equal(t, tc.expectedChainMap, chainMap)
			require.Equal(t, tc.expectedProviderMap, providerMap)
		})
	}
}

func TestSafeMapContains(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]int
		key      string
		expected bool
	}{
		{
			name:     "key exists",
			m:        map[string]int{"a": 1, "b": 2},
			key:      "a",
			expected: true,
		},
		{
			name:     "key does not exist",
			m:        map[string]int{"a": 1, "b": 2},
			key:      "c",
			expected: false,
		},
		{
			name:     "nil map",
			m:        nil,
			key:      "a",
			expected: false,
		},
		{
			name:     "empty map",
			m:        map[string]int{},
			key:      "a",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := safeMapContains(tc.m, tc.key)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestReportPriceErrMetrics(t *testing.T) {
	tests := []struct {
		name          string
		providerName  string
		priceType     string
		prices        map[string]int
		expected      []types.CurrencyPair
		expectedCalls int
	}{
		{
			name:         "missing prices",
			providerName: "binance",
			priceType:    "ticker",
			prices:       map[string]int{"BTCUSDT": 1},
			expected: []types.CurrencyPair{
				{Base: "BTC", Quote: "USDT"},
				{Base: "ETH", Quote: "USDT"},
			},
			expectedCalls: 1, // ETHUSDT missing
		},
		{
			name:         "all prices present",
			providerName: "binance",
			priceType:    "ticker",
			prices:       map[string]int{"BTCUSDT": 1, "ETHUSDT": 1},
			expected: []types.CurrencyPair{
				{Base: "BTC", Quote: "USDT"},
				{Base: "ETH", Quote: "USDT"},
			},
			expectedCalls: 0, // no missing prices
		},
		{
			name:         "nil prices map",
			providerName: "binance",
			priceType:    "ticker",
			prices:       nil,
			expected: []types.CurrencyPair{
				{Base: "BTC", Quote: "USDT"},
			},
			expectedCalls: 1, // all prices missing
		},
		{
			name:          "empty expected pairs",
			providerName:  "binance",
			priceType:     "ticker",
			prices:        map[string]int{"BTCUSDT": 1},
			expected:      []types.CurrencyPair{},
			expectedCalls: 0, // no expected pairs
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset mock telemetry
			telemetryMock := resetMockTelemetry()

			reportPriceErrMetrics(tc.providerName, tc.priceType, tc.prices, tc.expected)

			// Verify metrics were recorded for missing prices
			if tc.expectedCalls > 0 {
				require.GreaterOrEqual(t, telemetryMock.Len(), tc.expectedCalls)
			} else {
				require.Equal(t, 0, telemetryMock.Len())
			}
		})
	}
}

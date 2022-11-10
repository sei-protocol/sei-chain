package oracle

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/client"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
)

type mockProvider struct {
	prices map[string]provider.TickerPrice
}

func (m mockProvider) GetTickerPrices(_ ...types.CurrencyPair) (map[string]provider.TickerPrice, error) {
	return m.prices, nil
}

func (m mockProvider) GetCandlePrices(_ ...types.CurrencyPair) (map[string][]provider.CandlePrice, error) {
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
		zerolog.Nop(),
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

	ots.Require().Error(ots.oracle.SetPrices(context.TODO()))
	ots.Require().Empty(ots.oracle.GetPrices())

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

	ots.Require().Error(ots.oracle.SetPrices(context.TODO()))

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

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))

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

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
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

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.71"), prices.AmountOf("uumee"))
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices.AmountOf("uxbt"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdc"))
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices.AmountOf("uusdt"))
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

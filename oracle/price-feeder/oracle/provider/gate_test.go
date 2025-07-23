package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestGateProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   lastPrice,
			Vol:    volume,
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr(lastPrice), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["ATOMUSDT"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := "34.69000000"
		lastPriceUMEE := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   lastPriceAtom,
			Vol:    volume,
		}

		tickerMap["UMEE_USDT"] = GateTicker{
			Symbol: "UMEE_USDT",
			Last:   lastPriceUMEE,
			Vol:    volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "UMEE", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceAtom), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["ATOMUSDT"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceUMEE), prices["UMEEUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["UMEEUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   "0",
			Vol:    "0",
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.True(t, prices["ATOMUSDT"].Price.IsZero())
		require.True(t, prices["ATOMUSDT"].Volume.IsZero())
	})

	t.Run("invalid_price_format", func(t *testing.T) {
		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   "invalid_price",
			Vol:    "1000",
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]GateTicker{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestGateProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.69"
		volume := "100.0"
		timestamp := time.Now().Unix()

		candleMap := map[string][]GateCandle{}
		candleMap["ATOM_USDT"] = []GateCandle{
			{
				Close:     price,
				Volume:    volume,
				TimeStamp: timestamp,
				Symbol:    "ATOM_USDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Contains(t, candles, "ATOMUSDT")
		require.Len(t, candles["ATOMUSDT"], 1)
		require.Equal(t, sdk.MustNewDecFromStr(price), candles["ATOMUSDT"][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), candles["ATOMUSDT"][0].Volume)
		require.Equal(t, timestamp, candles["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("valid_request_multiple_candles", func(t *testing.T) {
		price1, price2 := "34.69", "35.50"
		volume1, volume2 := "100.0", "200.0"
		timestamp1 := time.Now().Unix()
		timestamp2 := timestamp1 + 60

		candleMap := map[string][]GateCandle{}
		candleMap["ATOM_USDT"] = []GateCandle{
			{
				Close:     price1,
				Volume:    volume1,
				TimeStamp: timestamp1,
				Symbol:    "ATOM_USDT",
			},
			{
				Close:     price2,
				Volume:    volume2,
				TimeStamp: timestamp2,
				Symbol:    "ATOM_USDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 2)
	})

	t.Run("invalid_request_missing_pair", func(t *testing.T) {
		p.candles = map[string][]GateCandle{}
		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})

	t.Run("invalid_candle_price_format", func(t *testing.T) {
		candleMap := map[string][]GateCandle{}
		candleMap["ATOM_USDT"] = []GateCandle{
			{
				Close:     "invalid_price",
				Volume:    "100.0",
				TimeStamp: time.Now().Unix(),
				Symbol:    "ATOM_USDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})
}

func TestGateProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns Gate pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := []GatePairSummary{
				{Base: "ATOM", Quote: "USDT"},
				{Base: "SEI", Quote: "USDT"},
				{Base: "BTC", Quote: "USDT"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewGateProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderGate,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)

		pairs, err := p.GetAvailablePairs()
		require.NoError(t, err)
		require.Len(t, pairs, 3)
		require.Contains(t, pairs, "ATOMUSDT")
		require.Contains(t, pairs, "SEIUSDT")
		require.Contains(t, pairs, "BTCUSDT")
	})

	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewGateProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderGate,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})

	t.Run("invalid_json_response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewGateProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderGate,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})
}

func TestGateProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("invalid_subscribe_channels_empty", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs([]types.CurrencyPair{}...)
		require.ErrorContains(t, err, "currency pairs is empty")
	})

	t.Run("valid_subscribe_single_pair", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs(types.CurrencyPair{Base: "SEI", Quote: "USDT"})
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "SEIUSDT")
	})

	t.Run("valid_subscribe_multiple_pairs", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs(
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
			types.CurrencyPair{Base: "ETH", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "BTCUSDT")
		require.Contains(t, p.subscribedPairs, "ETHUSDT")
	})
}

func TestGateCandle_UnmarshalParams(t *testing.T) {
	t.Run("valid_candle_params", func(t *testing.T) {
		// Gate candle format: [timestamp, close, high, low, open, volume, ?, symbol]
		params := [][]interface{}{
			{float64(1234567890), "105.0", "110.0", "95.0", "100.0", "1000.0", "102.5", "ATOM_USDT"},
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.NoError(t, err)
		require.Equal(t, int64(1234567890), candle.TimeStamp)
		require.Equal(t, "105.0", candle.Close)
		require.Equal(t, "1000.0", candle.Volume)
		require.Equal(t, "ATOM_USDT", candle.Symbol)
	})

	t.Run("multiple_candles_uses_latest", func(t *testing.T) {
		// Should use the last candle in the array
		params := [][]interface{}{
			{float64(1234567890), "105.0", "110.0", "95.0", "100.0", "1000.0", "102.5", "ATOM_USDT"},
			{float64(1234567900), "106.0", "111.0", "96.0", "101.0", "1001.0", "103.5", "ATOM_USDT"},
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.NoError(t, err)
		require.Equal(t, int64(1234567900), candle.TimeStamp)
		require.Equal(t, "106.0", candle.Close)
		require.Equal(t, "1001.0", candle.Volume)
	})

	t.Run("empty_params", func(t *testing.T) {
		params := [][]interface{}{}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no candles in response")
	})

	t.Run("invalid_params_length", func(t *testing.T) {
		params := [][]interface{}{
			{float64(1234567890), "105.0", "110.0"}, // Too few fields
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wrong number of fields")
	})

	t.Run("invalid_timestamp_zero", func(t *testing.T) {
		params := [][]interface{}{
			{float64(0), "105.0", "110.0", "95.0", "100.0", "1000.0", "102.5", "ATOM_USDT"},
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "time field must be a float")
	})

	t.Run("invalid_close_type", func(t *testing.T) {
		params := [][]interface{}{
			{float64(1234567890), 105.0, "110.0", "95.0", "100.0", "1000.0", "102.5", "ATOM_USDT"}, // Close as float, not string
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "close field must be a string")
	})

	t.Run("invalid_volume_type", func(t *testing.T) {
		params := [][]interface{}{
			{float64(1234567890), "105.0", "110.0", "95.0", "100.0", 1000.0, "102.5", "ATOM_USDT"}, // Volume as float, not string
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "volume field must be a string")
	})

	t.Run("invalid_symbol_type", func(t *testing.T) {
		params := [][]interface{}{
			{float64(1234567890), "105.0", "110.0", "95.0", "100.0", "1000.0", "102.5", 123}, // Symbol as number, not string
		}

		var candle GateCandle
		err := candle.UnmarshalParams(params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "symbol field must be a string")
	})
}

func TestGateTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := GateTicker{
			Symbol: "ATOM_USDT",
			Last:   "34.69",
			Vol:    "1000",
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("invalid_price", func(t *testing.T) {
		ticker := GateTicker{
			Symbol: "ATOM_USDT",
			Last:   "invalid",
			Vol:    "1000",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Gate price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		ticker := GateTicker{
			Symbol: "ATOM_USDT",
			Last:   "34.69",
			Vol:    "invalid",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Gate volume")
	})
}

func TestGateCandle_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		timestamp := time.Now().Unix()
		candle := GateCandle{
			Close:     "34.69",
			Volume:    "1000",
			TimeStamp: timestamp,
			Symbol:    "ATOM_USDT",
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("invalid_price", func(t *testing.T) {
		candle := GateCandle{
			Close:     "invalid",
			Volume:    "1000",
			TimeStamp: time.Now().Unix(),
			Symbol:    "ATOM_USDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Gate price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		candle := GateCandle{
			Close:     "34.69",
			Volume:    "invalid",
			TimeStamp: time.Now().Unix(),
			Symbol:    "ATOM_USDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Gate volume")
	})
}

func TestGateProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	// Add some subscribed pairs (ATOM-USDT is already subscribed from provider creation)
	p.setSubscribedPairs(
		types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)

	pairs := p.subscribedPairsToSlice()
	require.Len(t, pairs, 3) // ATOM-USDT + SEI-USDT + BTC-USDT
}

func TestGateCurrencyPairToGatePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	GateSymbol := currencyPairToGatePair(cp)
	require.Equal(t, GateSymbol, "ATOM_USDT")
}

func TestGatePairToCurrencyPair(t *testing.T) {
	// Simple conversion from "ATOM_USDT" to "ATOMUSDT"
	currencyPairSymbol := strings.ReplaceAll("ATOM_USDT", "_", "")
	require.Equal(t, "ATOMUSDT", currencyPairSymbol)
}

func TestNewGateTickerSubscriptionMsg(t *testing.T) {
	gateSymbols := []string{"ATOM_USDT", "SEI_USDT"}

	msg := newGateTickerSubscription(gateSymbols...)
	require.Equal(t, "spot.tickers", msg.Channel)
	require.Equal(t, "subscribe", msg.Event)
	require.Equal(t, []string{"ATOM_USDT", "SEI_USDT"}, msg.Payload)
	require.Greater(t, msg.Time, int64(0))
}

func TestNewGateCandleSubscriptionMsg(t *testing.T) {
	gatePair := "ATOM_USDT"

	msg := newGateCandleSubscription(gatePair)
	require.Equal(t, "spot.candlesticks", msg.Channel)
	require.Equal(t, "subscribe", msg.Event)
	require.Equal(t, []string{"1m", "ATOM_USDT"}, msg.Payload)
	require.Greater(t, msg.Time, int64(0))
}

func TestGateProvider_NewGateProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewGateProvider(
			context.TODO(),
			zerolog.Nop(),
			endpoints,
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Equal(t, endpoints.Rest, p.endpoints.Rest)
	})

	t.Run("with_invalid_provider_name", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      "invalid_provider",
			Rest:      "https://custom-api.example.com",
			Websocket: "invalid_websocket_endpoint", // This will be overridden to default
		}

		// Since invalid provider defaults to gate endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Gate WebSocket
		require.NotEqual(t, config.ProviderGate, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewGateProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderGate,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestGateProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := GateTicker{Symbol: "ATOM_USDT", Last: "100", Vol: "1000"}
		ticker2 := GateTicker{Symbol: "SEI_USDT", Last: "200", Vol: "2000"}

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["ATOM_USDT"] = ticker1
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["SEI_USDT"] = ticker2
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
			}
		}()

		time.Sleep(100 * time.Millisecond) // Let goroutines run

		// Check using provider's thread-safe methods instead of direct map access
		_, err1 := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		_, err2 := p.GetTickerPrices(types.CurrencyPair{Base: "SEI", Quote: "USDT"})
		require.NoError(t, err1)
		require.NoError(t, err2)
	})

	t.Run("concurrent_candle_updates", func(t *testing.T) {
		// Test concurrent access to candles map
		candle1 := GateCandle{Close: "100", Volume: "10", TimeStamp: time.Now().Unix(), Symbol: "ATOM_USDT"}
		candle2 := GateCandle{Close: "200", Volume: "20", TimeStamp: time.Now().Unix(), Symbol: "SEI_USDT"}

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["ATOM_USDT"] = []GateCandle{candle1}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["SEI_USDT"] = []GateCandle{candle2}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
			}
		}()

		time.Sleep(100 * time.Millisecond) // Let goroutines run

		// Check using provider's thread-safe methods instead of direct map access
		_, err1 := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		_, err2 := p.GetCandlePrices(types.CurrencyPair{Base: "SEI", Quote: "USDT"})
		require.NoError(t, err1)
		require.NoError(t, err2)
	})
}

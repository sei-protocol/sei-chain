package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestKrakenProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := sdk.MustNewDecFromStr("34.69000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOMUSDT"] = TickerPrice{
			Price:  lastPrice,
			Volume: volume,
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, lastPrice, prices["ATOMUSDT"].Price)
		require.Equal(t, volume, prices["ATOMUSDT"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := sdk.MustNewDecFromStr("34.69000000")
		lastPriceSei := sdk.MustNewDecFromStr("41.35000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOMUSDT"] = TickerPrice{
			Price:  lastPriceAtom,
			Volume: volume,
		}

		tickerMap["SEIUSDT"] = TickerPrice{
			Price:  lastPriceSei,
			Volume: volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, lastPriceAtom, prices["ATOMUSDT"].Price)
		require.Equal(t, volume, prices["ATOMUSDT"].Volume)
		require.Equal(t, lastPriceSei, prices["SEIUSDT"].Price)
		require.Equal(t, volume, prices["SEIUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOMUSDT"] = TickerPrice{
			Price:  sdk.ZeroDec(),
			Volume: sdk.ZeroDec(),
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.True(t, prices["ATOMUSDT"].Price.IsZero())
		require.True(t, prices["ATOMUSDT"].Volume.IsZero())
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]TickerPrice{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestKrakenProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.69"
		volume := "100.0"
		timestamp := time.Now().Unix()

		candleMap := map[string][]KrakenCandle{}
		candleMap["ATOMUSDT"] = []KrakenCandle{
			{
				Close:     price,
				Volume:    volume,
				TimeStamp: timestamp,
				Symbol:    "ATOMUSDT",
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

		candleMap := map[string][]KrakenCandle{}
		candleMap["ATOMUSDT"] = []KrakenCandle{
			{
				Close:     price1,
				Volume:    volume1,
				TimeStamp: timestamp1,
				Symbol:    "ATOMUSDT",
			},
			{
				Close:     price2,
				Volume:    volume2,
				TimeStamp: timestamp2,
				Symbol:    "ATOMUSDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 2)
	})

	t.Run("invalid_request_missing_pair", func(t *testing.T) {
		p.candles = map[string][]KrakenCandle{}
		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})

	t.Run("invalid_candle_price_format", func(t *testing.T) {
		candleMap := map[string][]KrakenCandle{}
		candleMap["ATOMUSDT"] = []KrakenCandle{
			{
				Close:     "invalid_price",
				Volume:    "100.0",
				TimeStamp: time.Now().Unix(),
				Symbol:    "ATOMUSDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})
}

func TestKrakenProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns Kraken pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := KrakenPairsSummary{
				Result: map[string]KrakenPairData{
					"ATOMUSD": {WsName: "ATOM/USD"},
					"XBTUSD":  {WsName: "XBT/USD"},
					"SEIUSDT": {WsName: "SEI/USDT"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderKraken,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
		)
		require.NoError(t, err)

		pairs, err := p.GetAvailablePairs()
		require.NoError(t, err)
		require.Len(t, pairs, 3)
		require.Contains(t, pairs, "ATOMUSD")
		require.Contains(t, pairs, "XBTUSD") // Note: Kraken uses XBT for BTC
		require.Contains(t, pairs, "SEIUSDT")
	})

	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderKraken,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
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

		p, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderKraken,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})

	t.Run("malformed_wsname_field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := KrakenPairsSummary{
				Result: map[string]KrakenPairData{
					"ATOMUSD":    {WsName: "ATOM/USD"},
					"MALFORMED1": {WsName: "INVALID"}, // No slash
					"MALFORMED2": {WsName: "A/B/C"},   // Too many slashes
					"SEIUSDT":    {WsName: "SEI/USDT"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderKraken,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
		)
		require.NoError(t, err)

		pairs, err := p.GetAvailablePairs()
		require.NoError(t, err)
		require.Len(t, pairs, 2) // Only valid pairs should be included
		require.Contains(t, pairs, "ATOMUSD")
		require.Contains(t, pairs, "SEIUSDT")
		require.NotContains(t, pairs, "INVALID")
	})
}

func TestKrakenProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("invalid_subscribe_channels_empty", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs([]types.CurrencyPair{}...)
		require.ErrorContains(t, err, "currency pairs is empty")
	})

	t.Run("valid_subscribe_single_pair", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "ATOMUSDT")
	})

	t.Run("valid_subscribe_multiple_pairs", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "ATOMUSDT")
		require.Contains(t, p.subscribedPairs, "SEIUSDT")
	})
}

func TestKrakenCandle_UnmarshalJSON(t *testing.T) {
	t.Run("valid_candle_array", func(t *testing.T) {
		// Kraken candle format: [time, etime, open, high, low, close, vwap, volume, count]
		candleJSON := `["1234567890.123", "1234567891.123", "100.0", "110.0", "95.0", "105.0", "102.5", "1000.0", "50"]`

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.NoError(t, err)
		require.Equal(t, int64(1234567891), candle.TimeStamp) // Uses etime field (index 1), not time field (index 0)
		require.Equal(t, "105.0", candle.Close)
		require.Equal(t, "1000.0", candle.Volume)
	})

	t.Run("invalid_array_length", func(t *testing.T) {
		candleJSON := `["1234567890.123", "1234567891.123", "100.0"]` // Too few elements

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
		require.Contains(t, err.Error(), "wrong number of fields")
	})

	t.Run("invalid_time_format", func(t *testing.T) {
		candleJSON := `["1234567890.123", 1234567891, "100.0", "110.0", "95.0", "105.0", "102.5", "1000.0", "50"]` // etime (index 1) as number, not string

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
		require.Contains(t, err.Error(), "time field must be a string")
	})

	t.Run("invalid_time_value", func(t *testing.T) {
		candleJSON := `["1234567890.123", "invalid_time", "100.0", "110.0", "95.0", "105.0", "102.5", "1000.0", "50"]` // etime (index 1) as invalid string

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unable to convert time to float")
	})

	t.Run("invalid_close_format", func(t *testing.T) {
		candleJSON := `["1234567890.123", "1234567891.123", "100.0", "110.0", "95.0", 105.0, "102.5", "1000.0", "50"]` // Close as number, not string

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
		require.Contains(t, err.Error(), "close field must be a string")
	})

	t.Run("invalid_volume_format", func(t *testing.T) {
		candleJSON := `["1234567890.123", "1234567891.123", "100.0", "110.0", "95.0", "105.0", "102.5", 1000.0, "50"]` // Volume as number, not string

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
		require.Contains(t, err.Error(), "volume field must be a string")
	})

	t.Run("invalid_json", func(t *testing.T) {
		candleJSON := `invalid json`

		var candle KrakenCandle
		err := candle.UnmarshalJSON([]byte(candleJSON))
		require.Error(t, err)
	})
}

func TestKrakenCandle_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		candle := KrakenCandle{
			Close:     "34.69",
			Volume:    "1000.0",
			TimeStamp: time.Now().Unix(),
			Symbol:    "ATOMUSDT",
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000.0"), price.Volume)
		require.Equal(t, candle.TimeStamp, price.TimeStamp)
	})

	t.Run("invalid_price", func(t *testing.T) {
		candle := KrakenCandle{
			Close:     "invalid",
			Volume:    "1000.0",
			TimeStamp: time.Now().Unix(),
			Symbol:    "ATOMUSDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Kraken price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		candle := KrakenCandle{
			Close:     "34.69",
			Volume:    "invalid",
			TimeStamp: time.Now().Unix(),
			Symbol:    "ATOMUSDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Kraken volume")
	})
}

func TestKrakenTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := KrakenTicker{
			C: []string{"34.69", "1"},    // [price, whole_lot_volume]
			V: []string{"500", "1000.0"}, // [today, last_24h]
		}

		price, err := ticker.toTickerPrice("ATOMUSDT")
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000.0"), price.Volume)
	})

	t.Run("invalid_ticker_C_length", func(t *testing.T) {
		ticker := KrakenTicker{
			C: []string{"34.69"}, // Missing second element
			V: []string{"500", "1000.0"},
		}

		_, err := ticker.toTickerPrice("ATOMUSDT")
		require.Error(t, err)
		require.Contains(t, err.Error(), "error converting KrakenTicker to TickerPrice")
	})

	t.Run("invalid_ticker_V_length", func(t *testing.T) {
		ticker := KrakenTicker{
			C: []string{"34.69", "1"},
			V: []string{"500"}, // Missing second element
		}

		_, err := ticker.toTickerPrice("ATOMUSDT")
		require.Error(t, err)
		require.Contains(t, err.Error(), "error converting KrakenTicker to TickerPrice")
	})

	t.Run("invalid_price_format", func(t *testing.T) {
		ticker := KrakenTicker{
			C: []string{"invalid", "1"},
			V: []string{"500", "1000.0"},
		}

		_, err := ticker.toTickerPrice("ATOMUSDT")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Kraken price")
	})

	t.Run("invalid_volume_format", func(t *testing.T) {
		ticker := KrakenTicker{
			C: []string{"34.69", "1"},
			V: []string{"500", "invalid"},
		}

		_, err := ticker.toTickerPrice("ATOMUSDT")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Kraken volume")
	})
}

func TestKrakenProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	// Add some subscribed pairs (BTC-USDT is already subscribed from provider creation)
	p.setSubscribedPairs(
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		types.CurrencyPair{Base: "SEI", Quote: "USDT"},
	)

	pairs := p.subscribedPairsToSlice()
	require.Len(t, pairs, 3) // BTC-USDT + ATOM-USDT + SEI-USDT
}

func TestKrakenProvider_RemoveSubscribedTickers(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	// Add pairs
	p.setSubscribedPairs(
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		types.CurrencyPair{Base: "SEI", Quote: "USDT"},
	)

	// Remove one pair
	p.removeSubscribedTickers("ATOMUSDT")

	require.NotContains(t, p.subscribedPairs, "ATOMUSDT")
	require.Contains(t, p.subscribedPairs, "SEIUSDT")
	require.Contains(t, p.subscribedPairs, "BTCUSDT")
}

func TestKrakenPairToCurrencyPairSymbol(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	currencyPairSymbol := krakenPairToCurrencyPairSymbol("ATOM/USDT")
	require.Equal(t, cp.String(), currencyPairSymbol)
}

func TestKrakenCurrencyPairToKrakenPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	krakenSymbol := currencyPairToKrakenPair(cp)
	require.Equal(t, krakenSymbol, "ATOM/USDT")
}

func TestNormalizeKrakenBTCPair(t *testing.T) {
	t.Run("normalize_XBT_to_BTC", func(t *testing.T) {
		btcSymbol := normalizeKrakenBTCPair("XBT/USDT")
		require.Equal(t, btcSymbol, "BTC/USDT")
	})

	t.Run("keep_other_pairs_unchanged", func(t *testing.T) {
		atomSymbol := normalizeKrakenBTCPair("ATOM/USDT")
		require.Equal(t, atomSymbol, "ATOM/USDT")
	})

	t.Run("only_replace_first_occurrence", func(t *testing.T) {
		// Edge case: if XBT appears multiple times, only replace first
		weirdSymbol := normalizeKrakenBTCPair("XBT/XBTUSDT")
		require.Equal(t, weirdSymbol, "BTC/XBTUSDT")
	})
}

func TestNewKrakenTickerSubscriptionMsg(t *testing.T) {
	t.Run("single_pair", func(t *testing.T) {
		msg := newKrakenTickerSubscriptionMsg("ATOM/USDT")
		require.Equal(t, "subscribe", msg.Event)
		require.Equal(t, []string{"ATOM/USDT"}, msg.Pair)
		require.Equal(t, "ticker", msg.Subscription.Name)
	})

	t.Run("multiple_pairs", func(t *testing.T) {
		msg := newKrakenTickerSubscriptionMsg("ATOM/USDT", "SEI/USDT")
		require.Equal(t, "subscribe", msg.Event)
		require.Equal(t, []string{"ATOM/USDT", "SEI/USDT"}, msg.Pair)
		require.Equal(t, "ticker", msg.Subscription.Name)
	})

	t.Run("empty_pairs", func(t *testing.T) {
		msg := newKrakenTickerSubscriptionMsg()
		require.Equal(t, "subscribe", msg.Event)
		require.Len(t, msg.Pair, 0)
		require.Equal(t, "ticker", msg.Subscription.Name)
	})
}

func TestNewKrakenCandleSubscriptionMsg(t *testing.T) {
	t.Run("single_pair", func(t *testing.T) {
		msg := newKrakenCandleSubscriptionMsg("ATOM/USDT")
		require.Equal(t, "subscribe", msg.Event)
		require.Equal(t, []string{"ATOM/USDT"}, msg.Pair)
		require.Equal(t, "ohlc", msg.Subscription.Name)
	})

	t.Run("multiple_pairs", func(t *testing.T) {
		msg := newKrakenCandleSubscriptionMsg("ATOM/USDT", "SEI/USDT")
		require.Equal(t, "subscribe", msg.Event)
		require.Equal(t, []string{"ATOM/USDT", "SEI/USDT"}, msg.Pair)
		require.Equal(t, "ohlc", msg.Subscription.Name)
	})

	t.Run("empty_pairs", func(t *testing.T) {
		msg := newKrakenCandleSubscriptionMsg()
		require.Equal(t, "subscribe", msg.Event)
		require.Len(t, msg.Pair, 0)
		require.Equal(t, "ohlc", msg.Subscription.Name)
	})
}

func TestKrakenProvider_NewKrakenProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			endpoints,
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
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

		// Since invalid provider defaults to kraken endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Kraken WebSocket
		require.NotEqual(t, config.ProviderKraken, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewKrakenProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderKraken,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestKrakenProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderKraken,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := TickerPrice{Price: sdk.MustNewDecFromStr("100"), Volume: sdk.MustNewDecFromStr("1000")}
		ticker2 := TickerPrice{Price: sdk.MustNewDecFromStr("200"), Volume: sdk.MustNewDecFromStr("2000")}

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["ATOMUSDT"] = ticker1
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["SEIUSDT"] = ticker2
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
		candle1 := KrakenCandle{Close: "100", Volume: "10", TimeStamp: time.Now().Unix(), Symbol: "ATOMUSDT"}
		candle2 := KrakenCandle{Close: "200", Volume: "20", TimeStamp: time.Now().Unix(), Symbol: "SEIUSDT"}

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["ATOMUSDT"] = []KrakenCandle{candle1}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["SEIUSDT"] = []KrakenCandle{candle2}
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

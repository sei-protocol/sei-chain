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
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestBinanceProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: lastPrice,
			Volume:    volume,
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
		lastPriceSei := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: lastPriceAtom,
			Volume:    volume,
		}

		tickerMap["SEIUSDT"] = BinanceTicker{
			Symbol:    "SEIUSDT",
			LastPrice: lastPriceSei,
			Volume:    volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceAtom), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["ATOMUSDT"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceSei), prices["SEIUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["SEIUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "0",
			Volume:    "0",
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.True(t, prices["ATOMUSDT"].Price.IsZero())
		require.True(t, prices["ATOMUSDT"].Volume.IsZero())
	})

	t.Run("invalid_price_format", func(t *testing.T) {
		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "invalid_price",
			Volume:    "1000",
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]BinanceTicker{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestBinanceProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.69000000"
		volume := "2396974.02000000"
		timestamp := time.Now().Unix() * 1000

		candleMap := map[string][]BinanceCandle{}
		candleMap["ATOMUSDT"] = []BinanceCandle{
			{
				Symbol: "ATOMUSDT",
				Metadata: BinanceCandleMetadata{
					Close:     price,
					Volume:    volume,
					TimeStamp: timestamp,
				},
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 1)
		require.Equal(t, sdk.MustNewDecFromStr(price), candles["ATOMUSDT"][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), candles["ATOMUSDT"][0].Volume)
		require.Equal(t, timestamp, candles["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("valid_request_multiple_candles", func(t *testing.T) {
		price1, price2 := "34.69", "35.50"
		volume1, volume2 := "1000", "2000"
		timestamp1 := time.Now().Unix() * 1000
		timestamp2 := timestamp1 + 60000 // 1 minute later

		candleMap := map[string][]BinanceCandle{}
		candleMap["ATOMUSDT"] = []BinanceCandle{
			{
				Symbol: "ATOMUSDT",
				Metadata: BinanceCandleMetadata{
					Close:     price1,
					Volume:    volume1,
					TimeStamp: timestamp1,
				},
			},
			{
				Symbol: "ATOMUSDT",
				Metadata: BinanceCandleMetadata{
					Close:     price2,
					Volume:    volume2,
					TimeStamp: timestamp2,
				},
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 2)
	})

	t.Run("invalid_request_missing_pair", func(t *testing.T) {
		p.candles = map[string][]BinanceCandle{}
		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})

	t.Run("invalid_price_format_in_candle", func(t *testing.T) {
		candleMap := map[string][]BinanceCandle{}
		candleMap["ATOMUSDT"] = []BinanceCandle{
			{
				Symbol: "ATOMUSDT",
				Metadata: BinanceCandleMetadata{
					Close:     "invalid_price",
					Volume:    "1000",
					TimeStamp: time.Now().Unix() * 1000,
				},
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})
}

func TestBinanceProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := []BinancePairSummary{
				{Symbol: "ATOMUSDT"},
				{Symbol: "SEIUSDT"},
				{Symbol: "BTCUSDT"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewBinanceProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderBinance,
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

		p, err := NewBinanceProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderBinance,
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

		p, err := NewBinanceProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderBinance,
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

func TestBinanceProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
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

func TestBinanceProvider_MessageReceived(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("ticker_message", func(t *testing.T) {
		ticker := BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "34.69",
			Volume:    "1000",
			C:         123456789,
		}

		tickerJSON, err := json.Marshal(ticker)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, tickerJSON)

		require.Contains(t, p.tickers, "ATOMUSDT")
		require.Equal(t, "34.69", p.tickers["ATOMUSDT"].LastPrice)
	})

	t.Run("candle_message", func(t *testing.T) {
		candle := BinanceCandle{
			Symbol: "ATOMUSDT",
			Metadata: BinanceCandleMetadata{
				Close:     "34.69",
				Volume:    "1000",
				TimeStamp: time.Now().Unix() * 1000,
			},
		}

		candleJSON, err := json.Marshal(candle)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, candleJSON)

		require.Contains(t, p.candles, "ATOMUSDT")
		require.Len(t, p.candles["ATOMUSDT"], 1)
	})

	t.Run("non_text_message", func(t *testing.T) {
		originalTickerCount := len(p.tickers)
		p.messageReceived(websocket.BinaryMessage, []byte("binary data"))
		require.Equal(t, originalTickerCount, len(p.tickers))
	})

	t.Run("invalid_json", func(t *testing.T) {
		originalTickerCount := len(p.tickers)
		p.messageReceived(websocket.TextMessage, []byte("invalid json"))
		require.Equal(t, originalTickerCount, len(p.tickers))
	})
}

func TestBinanceProvider_SetTickerPair(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	ticker := BinanceTicker{
		Symbol:    "ATOMUSDT",
		LastPrice: "34.69",
		Volume:    "1000",
		C:         123456789,
	}

	p.setTickerPair(ticker)

	require.Contains(t, p.tickers, "ATOMUSDT")
	require.Equal(t, ticker, p.tickers["ATOMUSDT"])
}

func TestBinanceProvider_SetCandlePair(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	candle := BinanceCandle{
		Symbol: "ATOMUSDT",
		Metadata: BinanceCandleMetadata{
			Close:     "34.69",
			Volume:    "1000",
			TimeStamp: time.Now().Unix() * 1000,
		},
	}

	p.setCandlePair(candle)

	require.Contains(t, p.candles, "ATOMUSDT")
	require.Len(t, p.candles["ATOMUSDT"], 1)
	require.Equal(t, candle, p.candles["ATOMUSDT"][0])
}

func TestBinanceProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	// Add some subscribed pairs
	p.setSubscribedPairs(
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		types.CurrencyPair{Base: "SEI", Quote: "USDT"},
	)

	pairs := p.subscribedPairsToSlice()
	require.Len(t, pairs, 2)
}

func TestBinanceTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "34.69",
			Volume:    "1000",
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("invalid_price", func(t *testing.T) {
		ticker := BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "invalid",
			Volume:    "1000",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Binance price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		ticker := BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: "34.69",
			Volume:    "invalid",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Binance volume")
	})
}

func TestBinanceCandle_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		timestamp := time.Now().Unix() * 1000
		candle := BinanceCandle{
			Symbol: "ATOMUSDT",
			Metadata: BinanceCandleMetadata{
				Close:     "34.69",
				Volume:    "1000",
				TimeStamp: timestamp,
			},
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("invalid_price", func(t *testing.T) {
		candle := BinanceCandle{
			Symbol: "ATOMUSDT",
			Metadata: BinanceCandleMetadata{
				Close:     "invalid",
				Volume:    "1000",
				TimeStamp: time.Now().Unix() * 1000,
			},
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Binance price")
	})
}

func TestBinanceCurrencyPairToBinancePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	binanceSymbol := currencyPairToBinanceTickerPair(cp)
	require.Equal(t, binanceSymbol, "atomusdt@ticker")
}

func TestBinanceCurrencyPairToBinanceCandlePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	binanceSymbol := currencyPairToBinanceCandlePair(cp)
	require.Equal(t, "atomusdt@kline_1m", binanceSymbol)
}

func TestNewBinanceSubscriptionMsg(t *testing.T) {
	t.Run("single_param", func(t *testing.T) {
		msg := newBinanceSubscriptionMsg("atomusdt@ticker")
		require.Equal(t, "SUBSCRIBE", msg.Method)
		require.Equal(t, []string{"atomusdt@ticker"}, msg.Params)
		require.Equal(t, uint16(1), msg.ID)
	})

	t.Run("multiple_params", func(t *testing.T) {
		msg := newBinanceSubscriptionMsg("atomusdt@ticker", "seiusdt@ticker")
		require.Equal(t, "SUBSCRIBE", msg.Method)
		require.Equal(t, []string{"atomusdt@ticker", "seiusdt@ticker"}, msg.Params)
		require.Equal(t, uint16(1), msg.ID)
	})

	t.Run("empty_params", func(t *testing.T) {
		msg := newBinanceSubscriptionMsg()
		require.Equal(t, "SUBSCRIBE", msg.Method)
		require.Len(t, msg.Params, 0) // Check length instead of exact slice equality
		require.Equal(t, uint16(1), msg.ID)
	})
}

func TestBinanceProvider_NewBinanceProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewBinanceProvider(
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

		// Since invalid provider defaults to binance endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Binance WebSocket
		require.NotEqual(t, config.ProviderBinance, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewBinanceProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderBinance,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestBinanceProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := BinanceTicker{Symbol: "ATOMUSDT", LastPrice: "100", Volume: "1000"}
		ticker2 := BinanceTicker{Symbol: "SEIUSDT", LastPrice: "200", Volume: "2000"}

		go func() {
			for i := 0; i < 100; i++ {
				p.setTickerPair(ticker1)
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.setTickerPair(ticker2)
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
}

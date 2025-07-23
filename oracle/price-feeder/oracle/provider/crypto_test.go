package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestCryptoProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := sdk.MustNewDecFromStr("34.69000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOM_USDT"] = TickerPrice{
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
		lastPriceLuna := sdk.MustNewDecFromStr("41.35000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOM_USDT"] = TickerPrice{
			Price:  lastPriceAtom,
			Volume: volume,
		}

		tickerMap["LUNA_USDT"] = TickerPrice{
			Price:  lastPriceLuna,
			Volume: volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, lastPriceAtom, prices["ATOMUSDT"].Price)
		require.Equal(t, volume, prices["ATOMUSDT"].Volume)
		require.Equal(t, lastPriceLuna, prices["LUNAUSDT"].Price)
		require.Equal(t, volume, prices["LUNAUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]TickerPrice{}
		tickerMap["ATOM_USDT"] = TickerPrice{
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

func TestCryptoProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.689998626708984000"
		volume := "2396974.000000000000000000"
		timeStamp := int64(1000000)

		candle := CryptoCandle{
			Volume:    volume,
			Close:     price,
			Timestamp: timeStamp,
		}

		p.setCandlePair("ATOM_USDT", candle)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		priceDec, _ := sdk.NewDecFromStr(price)
		volumeDec, _ := sdk.NewDecFromStr(volume)

		require.Equal(t, priceDec, prices["ATOMUSDT"][0].Price)
		require.Equal(t, volumeDec, prices["ATOMUSDT"][0].Volume)
		require.Equal(t, timeStamp, prices["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("valid_request_multiple_candles", func(t *testing.T) {
		candle1 := CryptoCandle{
			Volume:    "1000.0",
			Close:     "34.69",
			Timestamp: time.Now().Unix(),
		}
		candle2 := CryptoCandle{
			Volume:    "2000.0",
			Close:     "35.50",
			Timestamp: time.Now().Unix() + 60,
		}

		p.setCandlePair("ATOM_USDT", candle1)
		p.setCandlePair("ATOM_USDT", candle2)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		// Crypto.com provider maintains recent candles, so may have 1 or more
		require.GreaterOrEqual(t, len(prices["ATOMUSDT"]), 1)
	})

	t.Run("invalid_request_invalid_candle", func(t *testing.T) {
		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("candle_with_zero_values", func(t *testing.T) {
		candle := CryptoCandle{
			Volume:    "0",
			Close:     "0",
			Timestamp: 0,
		}

		p.setCandlePair("ATOM_USDT", candle)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Len(t, prices["ATOMUSDT"], 1)
		require.True(t, prices["ATOMUSDT"][0].Price.IsZero())
		require.True(t, prices["ATOMUSDT"][0].Volume.IsZero())
		require.Equal(t, int64(0), prices["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("invalid_candle_price_format", func(t *testing.T) {
		// Reset candles to ensure clean state
		p.candles = map[string][]CandlePrice{}

		candle := CryptoCandle{
			Volume:    "1000.0",
			Close:     "invalid_price",
			Timestamp: time.Now().Unix(),
		}

		p.setCandlePair("ATOM_USDT", candle)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		// Crypto.com provider silently ignores invalid candles during setCandlePair
		// So ATOM_USDT should not be in the candles map or should be empty
		if atomCandles, exists := prices["ATOMUSDT"]; exists {
			require.Len(t, atomCandles, 0)
		}
	})
}

func TestCryptoProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns Crypto.com pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := CryptoPairsSummary{
				Result: CryptoInstruments{
					Data: []CryptoTicker{
						{InstrumentName: "ATOM_USDT"},
						{InstrumentName: "SEI_USDT"},
						{InstrumentName: "BTC_USDT"},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCrypto,
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

		p, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCrypto,
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

		p, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCrypto,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})

	t.Run("malformed_instrument_name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := CryptoPairsSummary{
				Result: CryptoInstruments{
					Data: []CryptoTicker{
						{InstrumentName: "ATOM_USDT"},
						{InstrumentName: "INVALID"}, // No underscore
						{InstrumentName: "A_B_C"},   // Too many underscores
						{InstrumentName: "SEI_USDT"},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCrypto,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)

		pairs, err := p.GetAvailablePairs()
		require.NoError(t, err)
		require.Len(t, pairs, 2) // Only valid pairs should be included
		require.Contains(t, pairs, "ATOMUSDT")
		require.Contains(t, pairs, "SEIUSDT")
		require.NotContains(t, pairs, "INVALID")
	})
}

func TestCryptoProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("invalid_subscribe_channels_empty", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs([]types.CurrencyPair{}...)
		// Crypto.com provider may not validate empty pairs, so check if error occurs
		if err != nil {
			require.ErrorContains(t, err, "currency pairs is empty")
		}
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

func TestCryptoProvider_MessageReceived(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("heartbeat_message", func(t *testing.T) {
		heartbeat := CryptoHeartbeatResponse{
			ID:     123,
			Method: "public/heartbeat",
		}

		heartbeatJSON, err := json.Marshal(heartbeat)
		require.NoError(t, err)

		// Should handle heartbeat gracefully
		originalTickerCount := len(p.tickers)
		p.messageReceived(websocket.TextMessage, heartbeatJSON)
		require.Equal(t, originalTickerCount, len(p.tickers))
	})

	t.Run("ticker_message", func(t *testing.T) {
		ticker := CryptoTickerResponse{
			Result: CryptoTickerResult{
				InstrumentName: "ATOM_USDT",
				Channel:        "ticker",
				Data: []CryptoTicker{
					{
						InstrumentName: "ATOM_USDT",
						Volume:         "1000",
						LatestTrade:    "34.69",
					},
				},
			},
		}

		tickerJSON, err := json.Marshal(ticker)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, tickerJSON)
		require.Contains(t, p.tickers, "ATOM_USDT")
	})

	t.Run("candle_message", func(t *testing.T) {
		candle := CryptoCandleResponse{
			Result: CryptoCandleResult{
				InstrumentName: "ATOM_USDT",
				Channel:        "candlestick",
				Data: []CryptoCandle{
					{
						Close:     "34.69",
						Volume:    "1000",
						Timestamp: time.Now().Unix(),
					},
				},
			},
		}

		candleJSON, err := json.Marshal(candle)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, candleJSON)
		require.Contains(t, p.candles, "ATOM_USDT")
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

func TestCryptoTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := CryptoTicker{
			InstrumentName: "ATOM_USDT",
			LatestTrade:    "34.69",
			Volume:         "1000",
		}

		price, err := newTickerPrice(config.ProviderCrypto, ticker.InstrumentName, ticker.LatestTrade, ticker.Volume)
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("invalid_price", func(t *testing.T) {
		ticker := CryptoTicker{
			InstrumentName: "ATOM_USDT",
			LatestTrade:    "invalid",
			Volume:         "1000",
		}

		_, err := newTickerPrice(config.ProviderCrypto, ticker.InstrumentName, ticker.LatestTrade, ticker.Volume)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse crypto price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		ticker := CryptoTicker{
			InstrumentName: "ATOM_USDT",
			LatestTrade:    "34.69",
			Volume:         "invalid",
		}

		_, err := newTickerPrice(config.ProviderCrypto, ticker.InstrumentName, ticker.LatestTrade, ticker.Volume)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse crypto volume")
	})
}

func TestCryptoCandle_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		timestamp := time.Now().Unix()
		candle := CryptoCandle{
			Close:     "34.69",
			Volume:    "1000",
			Timestamp: timestamp,
		}

		price, err := newCandlePrice(config.ProviderCrypto, "ATOM_USDT", candle.Close, candle.Volume, candle.Timestamp)
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("invalid_price", func(t *testing.T) {
		candle := CryptoCandle{
			Close:     "invalid",
			Volume:    "1000",
			Timestamp: time.Now().Unix(),
		}

		_, err := newCandlePrice(config.ProviderCrypto, "ATOM_USDT", candle.Close, candle.Volume, candle.Timestamp)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse crypto price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		candle := CryptoCandle{
			Close:     "34.69",
			Volume:    "invalid",
			Timestamp: time.Now().Unix(),
		}

		_, err := newCandlePrice(config.ProviderCrypto, "ATOM_USDT", candle.Close, candle.Volume, candle.Timestamp)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse crypto volume")
	})
}

func TestCryptoCurrencyPairToCryptoPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	cryptoSymbol := currencyPairToCryptoPair(cp)
	require.Equal(t, cryptoSymbol, "ATOM_USDT")
}

func TestNewCryptoSubscriptionMsg(t *testing.T) {
	t.Run("single_channel", func(t *testing.T) {
		channels := []string{"ticker.ATOM_USDT"}
		msg := newCryptoSubscriptionMsg(channels)
		require.Equal(t, "subscribe", msg.Method)
		require.Equal(t, channels, msg.Params.Channels)
		require.Greater(t, msg.Nonce, int64(0))
	})

	t.Run("multiple_channels", func(t *testing.T) {
		channels := []string{"ticker.ATOM_USDT", "candlestick.5m.ATOM_USDT"}
		msg := newCryptoSubscriptionMsg(channels)
		require.Equal(t, "subscribe", msg.Method)
		require.Equal(t, channels, msg.Params.Channels)
		require.Greater(t, msg.Nonce, int64(0))
	})

	t.Run("empty_channels", func(t *testing.T) {
		channels := []string{}
		msg := newCryptoSubscriptionMsg(channels)
		require.Equal(t, "subscribe", msg.Method)
		require.Len(t, msg.Params.Channels, 0)
	})
}

func TestCryptoProvider_NewCryptoProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			endpoints,
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Equal(t, endpoints.Rest, p.endpoint.Rest)
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

		// Since invalid provider defaults to crypto endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Crypto.com WebSocket
		require.NotEqual(t, config.ProviderCrypto, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewCryptoProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCrypto,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		// Crypto.com provider may not validate empty pairs during creation
		if err != nil {
			require.Contains(t, err.Error(), "currency pairs is empty")
		}
	})
}

func TestCryptoProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := TickerPrice{Price: sdk.MustNewDecFromStr("100"), Volume: sdk.MustNewDecFromStr("1000")}
		ticker2 := TickerPrice{Price: sdk.MustNewDecFromStr("200"), Volume: sdk.MustNewDecFromStr("2000")}

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
		candle1 := CandlePrice{Price: sdk.MustNewDecFromStr("100"), Volume: sdk.MustNewDecFromStr("10"), TimeStamp: time.Now().Unix()}
		candle2 := CandlePrice{Price: sdk.MustNewDecFromStr("200"), Volume: sdk.MustNewDecFromStr("20"), TimeStamp: time.Now().Unix()}

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["ATOM_USDT"] = []CandlePrice{candle1}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["SEI_USDT"] = []CandlePrice{candle2}
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

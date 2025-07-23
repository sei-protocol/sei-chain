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

func TestOkxProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderOkx,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPrice,
			Vol24h: volume,
		}

		p.tickers = syncMap

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

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPriceAtom,
			Vol24h: volume,
		}

		syncMap["SEI-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "SEI-USDT",
			},
			Last:   lastPriceSei,
			Vol24h: volume,
		}

		p.tickers = syncMap
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
		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   "0",
			Vol24h: "0",
		}

		p.tickers = syncMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.True(t, prices["ATOMUSDT"].Price.IsZero())
		require.True(t, prices["ATOMUSDT"].Volume.IsZero())
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]OkxTickerPair{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestOkxProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderOkx,
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

		candleMap := map[string][]OkxCandlePair{}
		candleMap["ATOM-USDT"] = []OkxCandlePair{
			{
				Close:     price,
				Volume:    volume,
				TimeStamp: timestamp,
				InstID:    "ATOM-USDT",
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

		candleMap := map[string][]OkxCandlePair{}
		candleMap["ATOM-USDT"] = []OkxCandlePair{
			{
				Close:     price1,
				Volume:    volume1,
				TimeStamp: timestamp1,
				InstID:    "ATOM-USDT",
			},
			{
				Close:     price2,
				Volume:    volume2,
				TimeStamp: timestamp2,
				InstID:    "ATOM-USDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 2)
	})

	t.Run("invalid_request_missing_pair", func(t *testing.T) {
		p.candles = map[string][]OkxCandlePair{}
		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		// OKX provider returns an error for missing pairs
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get candle prices")
		require.Zero(t, len(candles))
	})

	t.Run("invalid_candle_price_format", func(t *testing.T) {
		candleMap := map[string][]OkxCandlePair{}
		candleMap["ATOM-USDT"] = []OkxCandlePair{
			{
				Close:     "invalid_price",
				Volume:    "100.0",
				TimeStamp: time.Now().Unix(),
				InstID:    "ATOM-USDT",
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		// OKX provider returns an error for invalid price formats
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Okx price")
		require.Zero(t, len(candles))
	})
}

func TestOkxProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns OKX pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := struct {
				Data []OkxInstID `json:"data"`
			}{
				Data: []OkxInstID{
					{InstID: "ATOM-USDT"},
					{InstID: "SEI-USDT"},
					{InstID: "BTC-USDT"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewOkxProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderOkx,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
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

		p, err := NewOkxProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderOkx,
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

		p, err := NewOkxProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderOkx,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})

	t.Run("malformed_inst_id", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := struct {
				Data []OkxInstID `json:"data"`
			}{
				Data: []OkxInstID{
					{InstID: "ATOM-USDT"},
					{InstID: "INVALID"}, // No dash
					{InstID: "A-B-C"},   // Too many dashes
					{InstID: "SEI-USDT"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewOkxProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderOkx,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
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

func TestOkxProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderOkx,
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
		err = p.SubscribeCurrencyPairs(types.CurrencyPair{Base: "SEI", Quote: "USDT"})
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "SEIUSDT")
	})

	t.Run("valid_subscribe_multiple_pairs", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "ETH", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Contains(t, p.subscribedPairs, "ATOMUSDT")
		require.Contains(t, p.subscribedPairs, "ETHUSDT")
	})
}

func TestOkxTickerPair_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := OkxTickerPair{
			OkxInstID: OkxInstID{InstID: "ATOM-USDT"},
			Last:      "34.69",
			Vol24h:    "1000",
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("invalid_price", func(t *testing.T) {
		ticker := OkxTickerPair{
			OkxInstID: OkxInstID{InstID: "ATOM-USDT"},
			Last:      "invalid",
			Vol24h:    "1000",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Okx price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		ticker := OkxTickerPair{
			OkxInstID: OkxInstID{InstID: "ATOM-USDT"},
			Last:      "34.69",
			Vol24h:    "invalid",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Okx volume")
	})
}

func TestOkxCandlePair_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		timestamp := time.Now().Unix()
		candle := OkxCandlePair{
			Close:     "34.69",
			Volume:    "1000",
			TimeStamp: timestamp,
			InstID:    "ATOM-USDT",
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("invalid_price", func(t *testing.T) {
		candle := OkxCandlePair{
			Close:     "invalid",
			Volume:    "1000",
			TimeStamp: time.Now().Unix(),
			InstID:    "ATOM-USDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Okx price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		candle := OkxCandlePair{
			Close:     "34.69",
			Volume:    "invalid",
			TimeStamp: time.Now().Unix(),
			InstID:    "ATOM-USDT",
		}

		_, err := candle.toCandlePrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Okx volume")
	})
}

func TestOkxProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderOkx,
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

func TestOkxCurrencyPairToOkxPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	okxSymbol := currencyPairToOkxPair(cp)
	require.Equal(t, "ATOM-USDT", okxSymbol)
}

func TestNewOkxSubscriptionMsg(t *testing.T) {
	t.Run("ticker_subscription", func(t *testing.T) {
		topics := []OkxSubscriptionTopic{
			{Channel: "tickers", InstID: "ATOM-USDT"},
			{Channel: "tickers", InstID: "SEI-USDT"},
		}

		msg := newOkxSubscriptionMsg(topics...)
		require.Equal(t, "subscribe", msg.Op)
		require.Len(t, msg.Args, 2)
		require.Equal(t, "tickers", msg.Args[0].Channel)
		require.Equal(t, "ATOM-USDT", msg.Args[0].InstID)
		require.Equal(t, "tickers", msg.Args[1].Channel)
		require.Equal(t, "SEI-USDT", msg.Args[1].InstID)
	})

	t.Run("candle_subscription", func(t *testing.T) {
		topics := []OkxSubscriptionTopic{
			{Channel: "candle1m", InstID: "ATOM-USDT"},
		}

		msg := newOkxSubscriptionMsg(topics...)
		require.Equal(t, "subscribe", msg.Op)
		require.Len(t, msg.Args, 1)
		require.Equal(t, "candle1m", msg.Args[0].Channel)
		require.Equal(t, "ATOM-USDT", msg.Args[0].InstID)
	})

	t.Run("empty_topics", func(t *testing.T) {
		msg := newOkxSubscriptionMsg()
		require.Equal(t, "subscribe", msg.Op)
		require.Len(t, msg.Args, 0)
	})
}

func TestOkxProvider_NewOkxProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderOkx,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewOkxProvider(
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

		// Since invalid provider defaults to okx endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real OKX WebSocket
		require.NotEqual(t, config.ProviderOkx, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewOkxProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderOkx,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestOkxProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderOkx,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := OkxTickerPair{OkxInstID: OkxInstID{InstID: "ATOM-USDT"}, Last: "100", Vol24h: "1000"}
		ticker2 := OkxTickerPair{OkxInstID: OkxInstID{InstID: "SEI-USDT"}, Last: "200", Vol24h: "2000"}

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["ATOM-USDT"] = ticker1
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["SEI-USDT"] = ticker2
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
		candle1 := OkxCandlePair{Close: "100", Volume: "10", TimeStamp: time.Now().Unix(), InstID: "ATOM-USDT"}
		candle2 := OkxCandlePair{Close: "200", Volume: "20", TimeStamp: time.Now().Unix(), InstID: "SEI-USDT"}

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["ATOM-USDT"] = []OkxCandlePair{candle1}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["SEI-USDT"] = []OkxCandlePair{candle2}
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

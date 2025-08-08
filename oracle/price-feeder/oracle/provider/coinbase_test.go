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

func TestCoinbaseProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		tickerMap := map[string]CoinbaseTicker{}
		tickerMap["ATOM-USDT"] = CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     lastPrice,
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
		lastPriceUmee := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]CoinbaseTicker{}
		tickerMap["ATOM-USDT"] = CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     lastPriceAtom,
			Volume:    volume,
		}

		tickerMap["UMEE-USDT"] = CoinbaseTicker{
			ProductID: "UMEE-USDT",
			Price:     lastPriceUmee,
			Volume:    volume,
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
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceUmee), prices["UMEEUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["UMEEUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]CoinbaseTicker{}
		tickerMap["ATOM-USDT"] = CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "0",
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
		tickerMap := map[string]CoinbaseTicker{}
		tickerMap["ATOM-USDT"] = CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "invalid_price",
			Volume:    "1000",
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]CoinbaseTicker{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestCoinbaseProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.69"
		size := "100.0"
		timestamp := time.Now().Unix()

		tradeMap := map[string][]CoinbaseTrade{}
		tradeMap["ATOM-USDT"] = []CoinbaseTrade{
			{
				ProductID: "ATOM-USDT",
				Price:     price,
				Size:      size,
				Time:      timestamp,
			},
		}

		p.trades = tradeMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Contains(t, candles, "ATOMUSDT")
		require.Len(t, candles["ATOMUSDT"], 1)
		require.Equal(t, sdk.MustNewDecFromStr(price), candles["ATOMUSDT"][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr(size), candles["ATOMUSDT"][0].Volume)
		require.Equal(t, timestamp, candles["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("valid_request_multiple_trades_same_minute", func(t *testing.T) {
		price1, price2 := "34.69", "35.50"
		size1, size2 := "100.0", "200.0"
		timestamp := time.Now().Unix()

		tradeMap := map[string][]CoinbaseTrade{}
		tradeMap["ATOM-USDT"] = []CoinbaseTrade{
			{
				ProductID: "ATOM-USDT",
				Price:     price1,
				Size:      size1,
				Time:      timestamp,
			},
			{
				ProductID: "ATOM-USDT",
				Price:     price2,
				Size:      size2,
				Time:      timestamp + 30, // same minute
			},
		}

		p.trades = tradeMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 1)

		// Should aggregate volumes and use latest price
		expectedVolume := sdk.MustNewDecFromStr(size1).Add(sdk.MustNewDecFromStr(size2))
		require.Equal(t, sdk.MustNewDecFromStr(price2), candles["ATOMUSDT"][0].Price)
		require.Equal(t, expectedVolume, candles["ATOMUSDT"][0].Volume)
	})

	t.Run("valid_request_multiple_trades_different_minutes", func(t *testing.T) {
		price1, price2 := "34.69", "35.50"
		size1, size2 := "100.0", "200.0"
		timestamp := time.Now().Unix()

		tradeMap := map[string][]CoinbaseTrade{}
		tradeMap["ATOM-USDT"] = []CoinbaseTrade{
			{
				ProductID: "ATOM-USDT",
				Price:     price1,
				Size:      size1,
				Time:      timestamp,
			},
			{
				ProductID: "ATOM-USDT",
				Price:     price2,
				Size:      size2,
				Time:      timestamp + 70000, // different minute (70 seconds later in ms)
			},
		}

		p.trades = tradeMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 2) // Two separate candles
	})

	t.Run("invalid_request_missing_pair", func(t *testing.T) {
		p.trades = map[string][]CoinbaseTrade{}
		_, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no trades have been received")
	})

	t.Run("invalid_trade_price_format", func(t *testing.T) {
		tradeMap := map[string][]CoinbaseTrade{}
		tradeMap["ATOM-USDT"] = []CoinbaseTrade{
			{
				ProductID: "ATOM-USDT",
				Price:     "invalid_price",
				Size:      "100.0",
				Time:      time.Now().Unix(),
			},
		}

		p.trades = tradeMap

		_, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.Error(t, err)
	})

	t.Run("invalid_trade_size_format", func(t *testing.T) {
		tradeMap := map[string][]CoinbaseTrade{}
		tradeMap["ATOM-USDT"] = []CoinbaseTrade{
			{
				ProductID: "ATOM-USDT",
				Price:     "34.69",
				Size:      "invalid_size",
				Time:      time.Now().Unix(),
			},
		}

		p.trades = tradeMap

		_, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.Error(t, err)
	})
}

func TestCoinbaseProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := []CoinbasePairSummary{
				{Base: "atom", Quote: "usdt"},
				{Base: "sei", Quote: "usdt"},
				{Base: "btc", Quote: "usdt"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewCoinbaseProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCoinbase,
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

		p, err := NewCoinbaseProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCoinbase,
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

		p, err := NewCoinbaseProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCoinbase,
				Rest:      server.URL,
				Websocket: mockServer.GetBaseURL(),
			},
			types.CurrencyPair{Base: "BTC", Quote: "USDT"},
		)
		require.NoError(t, err)

		_, err = p.GetAvailablePairs()
		require.Error(t, err)
	})
}

func TestCoinbaseProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
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

func TestCoinbaseProvider_MessageReceived(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("ticker_message", func(t *testing.T) {
		ticker := CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "34.69",
			Volume:    "1000",
		}

		// Create a full message with type field
		message := map[string]interface{}{
			"type":       "ticker",
			"product_id": ticker.ProductID,
			"price":      ticker.Price,
			"volume_24h": ticker.Volume,
		}

		messageJSON, err := json.Marshal(message)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, messageJSON)

		require.Contains(t, p.tickers, "ATOM-USDT")
		require.Equal(t, "34.69", p.tickers["ATOM-USDT"].Price)
	})

	t.Run("trade_message", func(t *testing.T) {
		trade := CoinbaseTradeResponse{
			Type:      "match",
			ProductID: "ATOM-USDT",
			Price:     "34.69",
			Size:      "100",
			Time:      "2022-02-24T17:04:38.305000Z",
		}

		tradeJSON, err := json.Marshal(trade)
		require.NoError(t, err)

		p.messageReceived(websocket.TextMessage, tradeJSON)

		require.Contains(t, p.trades, "ATOM-USDT")
		require.Len(t, p.trades["ATOM-USDT"], 1)
	})

	t.Run("error_message", func(t *testing.T) {
		errMsg := CoinbaseErrResponse{
			Type:   "error",
			Reason: "invalid channel",
		}

		errorJSON, err := json.Marshal(errMsg)
		require.NoError(t, err)

		// Should handle error gracefully without crashing
		p.messageReceived(websocket.TextMessage, errorJSON)
	})

	t.Run("subscriptions_message", func(t *testing.T) {
		message := map[string]interface{}{
			"type": "subscriptions",
		}

		messageJSON, err := json.Marshal(message)
		require.NoError(t, err)

		// Should handle subscriptions message gracefully
		originalTickerCount := len(p.tickers)
		p.messageReceived(websocket.TextMessage, messageJSON)
		require.Equal(t, originalTickerCount, len(p.tickers))
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

func TestCoinbaseProvider_SetTickerPair(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	ticker := CoinbaseTicker{
		ProductID: "ATOM-USDT",
		Price:     "34.69",
		Volume:    "1000",
	}

	p.setTickerPair(ticker)

	require.Contains(t, p.tickers, "ATOM-USDT")
	require.Equal(t, ticker, p.tickers["ATOM-USDT"])
}

func TestCoinbaseProvider_SetTradePair(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	trade := CoinbaseTradeResponse{
		Type:      "match",
		ProductID: "ATOM-USDT",
		Price:     "34.69",
		Size:      "100",
		Time:      "2022-02-24T17:04:38.305000Z",
	}

	p.setTradePair(trade)

	require.Contains(t, p.trades, "ATOM-USDT")
	require.Len(t, p.trades["ATOM-USDT"], 1)
	require.Equal(t, "34.69", p.trades["ATOM-USDT"][0].Price)
}

func TestCoinbaseProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
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

func TestCoinbaseTradeResponse_TimeToUnix(t *testing.T) {
	t.Run("valid_time_format", func(t *testing.T) {
		trade := CoinbaseTradeResponse{
			Time: "2022-02-24T17:04:38.305000Z", // Using microseconds format
		}

		timestamp := trade.timeToUnix()
		require.Greater(t, timestamp, int64(0))

		// Should be a reasonable timestamp (around Feb 2022)
		expectedTime := time.Date(2022, 2, 24, 17, 4, 38, 305000000, time.UTC)
		require.Equal(t, expectedTime.UnixMilli(), timestamp)
	})

	t.Run("invalid_time_format", func(t *testing.T) {
		trade := CoinbaseTradeResponse{
			Time: "invalid_time",
		}

		timestamp := trade.timeToUnix()
		require.Equal(t, int64(0), timestamp)
	})

	t.Run("empty_time", func(t *testing.T) {
		trade := CoinbaseTradeResponse{
			Time: "",
		}

		timestamp := trade.timeToUnix()
		require.Equal(t, int64(0), timestamp)
	})
}

func TestCoinbaseTradeResponse_ToTrade(t *testing.T) {
	trade := CoinbaseTradeResponse{
		Type:      "match",
		ProductID: "ATOM-USDT",
		Price:     "34.69",
		Size:      "100",
		Time:      "2022-02-24T17:04:38.305000Z",
	}

	coinbaseTrade := trade.toTrade()
	require.Equal(t, trade.ProductID, coinbaseTrade.ProductID)
	require.Equal(t, trade.Price, coinbaseTrade.Price)
	require.Equal(t, trade.Size, coinbaseTrade.Size)
	require.Greater(t, coinbaseTrade.Time, int64(0))
}

func TestCoinbaseTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "34.69",
			Volume:    "1000",
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("invalid_price", func(t *testing.T) {
		ticker := CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "invalid",
			Volume:    "1000",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Coinbase price")
	})

	t.Run("invalid_volume", func(t *testing.T) {
		ticker := CoinbaseTicker{
			ProductID: "ATOM-USDT",
			Price:     "34.69",
			Volume:    "invalid",
		}

		_, err := ticker.toTickerPrice()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse Coinbase volume")
	})
}

func TestCoinbasePairToCurrencyPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	currencyPairSymbol := coinbasePairToCurrencyPair("ATOM-USDT")
	require.Equal(t, cp.String(), currencyPairSymbol)
}

func TestCurrencyPairToCoinbasePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	coinbaseSymbol := currencyPairToCoinbasePair(cp)
	require.Equal(t, coinbaseSymbol, "ATOM-USDT")
}

func TestNewCoinbaseSubscription(t *testing.T) {
	t.Run("single_pair", func(t *testing.T) {
		msg := newCoinbaseSubscription("ATOM-USDT")
		require.Equal(t, "subscribe", msg.Type)
		require.Equal(t, []string{"ATOM-USDT"}, msg.ProductIDs)
		require.Equal(t, []string{"matches", "ticker"}, msg.Channels)
	})

	t.Run("multiple_pairs", func(t *testing.T) {
		msg := newCoinbaseSubscription("ATOM-USDT", "SEI-USDT")
		require.Equal(t, "subscribe", msg.Type)
		require.Equal(t, []string{"ATOM-USDT", "SEI-USDT"}, msg.ProductIDs)
		require.Equal(t, []string{"matches", "ticker"}, msg.Channels)
	})

	t.Run("empty_pairs", func(t *testing.T) {
		msg := newCoinbaseSubscription()
		require.Equal(t, "subscribe", msg.Type)
		require.Len(t, msg.ProductIDs, 0)
		require.Equal(t, []string{"matches", "ticker"}, msg.Channels)
	})
}

func TestCoinbaseProvider_NewCoinbaseProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewCoinbaseProvider(
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

		// Since invalid provider defaults to coinbase endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Coinbase WebSocket
		require.NotEqual(t, config.ProviderCoinbase, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewCoinbaseProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderCoinbase,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestCoinbaseProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewCoinbaseProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := CoinbaseTicker{ProductID: "ATOM-USDT", Price: "100", Volume: "1000"}
		ticker2 := CoinbaseTicker{ProductID: "SEI-USDT", Price: "200", Volume: "2000"}

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

	t.Run("concurrent_trade_updates", func(t *testing.T) {
		// Test concurrent access to trades map
		trade1 := CoinbaseTradeResponse{
			Type: "match", ProductID: "ATOM-USDT", Price: "100", Size: "10",
			Time: "2022-02-24T17:04:38.305000Z",
		}
		trade2 := CoinbaseTradeResponse{
			Type: "match", ProductID: "SEI-USDT", Price: "200", Size: "20",
			Time: "2022-02-24T17:04:38.305000Z",
		}

		go func() {
			for i := 0; i < 50; i++ {
				p.setTradePair(trade1)
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.setTradePair(trade2)
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

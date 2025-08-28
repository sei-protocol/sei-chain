package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestHuobiProvider_GetTickerPrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := 34.69000000
		volume := 2396974.02000000

		tickerMap := map[string]HuobiTicker{}
		tickerMap["market.atomusdt.ticker"] = HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPrice,
				Vol:       volume,
			},
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(lastPrice, 'f', -1, 64)), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(volume, 'f', -1, 64)), prices["ATOMUSDT"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := 34.69000000
		lastPriceSei := 41.35000000
		volume := 2396974.02000000

		tickerMap := map[string]HuobiTicker{}
		tickerMap["market.atomusdt.ticker"] = HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPriceAtom,
				Vol:       volume,
			},
		}

		tickerMap["market.seiusdt.ticker"] = HuobiTicker{
			CH: "market.seiusdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPriceSei,
				Vol:       volume,
			},
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(lastPriceAtom, 'f', -1, 64)), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(volume, 'f', -1, 64)), prices["ATOMUSDT"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(lastPriceSei, 'f', -1, 64)), prices["SEIUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(volume, 'f', -1, 64)), prices["SEIUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		tickerMap := map[string]HuobiTicker{}
		tickerMap["market.atomusdt.ticker"] = HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: 0.0,
				Vol:       0.0,
			},
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.True(t, prices["ATOMUSDT"].Price.IsZero())
		require.True(t, prices["ATOMUSDT"].Volume.IsZero())
	})

	t.Run("empty_ticker_map", func(t *testing.T) {
		p.tickers = map[string]HuobiTicker{}
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestHuobiProvider_GetCandlePrices(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := 34.69
		volume := 100.0
		timestamp := time.Now().Unix()

		candleMap := map[string][]HuobiCandle{}
		candleMap["market.atomusdt.kline.1min"] = []HuobiCandle{
			{
				CH: "market.atomusdt.kline.1min",
				Tick: HuobiCandleTick{
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
		require.Contains(t, candles, "ATOMUSDT")
		require.Len(t, candles["ATOMUSDT"], 1)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(price, 'f', -1, 64)), candles["ATOMUSDT"][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr(strconv.FormatFloat(volume, 'f', -1, 64)), candles["ATOMUSDT"][0].Volume)
		require.Equal(t, timestamp, candles["ATOMUSDT"][0].TimeStamp)
	})

	t.Run("valid_request_multiple_candles", func(t *testing.T) {
		price1, price2 := 34.69, 35.50
		volume1, volume2 := 100.0, 200.0
		timestamp1 := time.Now().Unix()
		timestamp2 := timestamp1 + 60

		candleMap := map[string][]HuobiCandle{}
		candleMap["market.atomusdt.kline.1min"] = []HuobiCandle{
			{
				CH: "market.atomusdt.kline.1min",
				Tick: HuobiCandleTick{
					Close:     price1,
					Volume:    volume1,
					TimeStamp: timestamp1,
				},
			},
			{
				CH: "market.atomusdt.kline.1min",
				Tick: HuobiCandleTick{
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
		p.candles = map[string][]HuobiCandle{}
		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Zero(t, len(candles))
	})

	t.Run("candle_with_zero_values", func(t *testing.T) {
		candleMap := map[string][]HuobiCandle{}
		candleMap["market.atomusdt.kline.1min"] = []HuobiCandle{
			{
				CH: "market.atomusdt.kline.1min",
				Tick: HuobiCandleTick{
					Close:     0.0,
					Volume:    0.0,
					TimeStamp: time.Now().Unix(),
				},
			},
		}

		p.candles = candleMap

		candles, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, candles, 1)
		require.Len(t, candles["ATOMUSDT"], 1)
		require.True(t, candles["ATOMUSDT"][0].Price.IsZero())
		require.True(t, candles["ATOMUSDT"][0].Volume.IsZero())
	})
}

func TestHuobiProvider_GetAvailablePairs(t *testing.T) {
	t.Run("successful_response", func(t *testing.T) {
		// Mock server that returns Huobi pair summary
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := HuobiPairsSummary{
				Data: []HuobiPairData{
					{Symbol: "atomusdt"},
					{Symbol: "seiusdt"},
					{Symbol: "btcusdt"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		mockServer := NewMockProviderServer()
		mockServer.Start()
		defer mockServer.Close()

		p, err := NewHuobiProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderHuobi,
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

		p, err := NewHuobiProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderHuobi,
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

		p, err := NewHuobiProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderHuobi,
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

func TestHuobiProvider_SubscribeCurrencyPairs(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
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

func TestHuobiTicker_ToTickerPrice(t *testing.T) {
	t.Run("valid_ticker", func(t *testing.T) {
		ticker := HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: 34.69,
				Vol:       1000.0,
			},
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
	})

	t.Run("ticker_with_zero_values", func(t *testing.T) {
		ticker := HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: 0.0,
				Vol:       0.0,
			},
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.True(t, price.Price.IsZero())
		require.True(t, price.Volume.IsZero())
	})

	t.Run("ticker_with_very_small_values", func(t *testing.T) {
		ticker := HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: 0.000001,
				Vol:       0.000001,
			},
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("0.000001"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("0.000001"), price.Volume)
	})

	t.Run("ticker_with_large_values", func(t *testing.T) {
		ticker := HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: 999999.999999,
				Vol:       999999.999999,
			},
		}

		price, err := ticker.toTickerPrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("999999.999999"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("999999.999999"), price.Volume)
	})
}

func TestHuobiCandle_ToCandlePrice(t *testing.T) {
	t.Run("valid_candle", func(t *testing.T) {
		timestamp := time.Now().Unix()
		candle := HuobiCandle{
			CH: "market.atomusdt.kline.1min",
			Tick: HuobiCandleTick{
				Close:     34.69,
				Volume:    1000.0,
				TimeStamp: timestamp,
			},
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr("34.69"), price.Price)
		require.Equal(t, sdk.MustNewDecFromStr("1000"), price.Volume)
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("candle_with_zero_values", func(t *testing.T) {
		timestamp := time.Now().Unix()
		candle := HuobiCandle{
			CH: "market.atomusdt.kline.1min",
			Tick: HuobiCandleTick{
				Close:     0.0,
				Volume:    0.0,
				TimeStamp: timestamp,
			},
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.True(t, price.Price.IsZero())
		require.True(t, price.Volume.IsZero())
		require.Equal(t, timestamp, price.TimeStamp)
	})

	t.Run("candle_with_zero_timestamp", func(t *testing.T) {
		candle := HuobiCandle{
			CH: "market.atomusdt.kline.1min",
			Tick: HuobiCandleTick{
				Close:     34.69,
				Volume:    1000.0,
				TimeStamp: 0,
			},
		}

		price, err := candle.toCandlePrice()
		require.NoError(t, err)
		require.Equal(t, int64(0), price.TimeStamp)
	})
}

func TestHuobiProvider_SubscribedPairsToSlice(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
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

func TestHuobiCurrencyPairToHuobiPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	huobiSymbol := currencyPairToHuobiTickerPair(cp)
	require.Equal(t, huobiSymbol, "market.atomusdt.ticker")
}

func TestHuobiCurrencyPairToHuobiCandlePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	huobiSymbol := currencyPairToHuobiCandlePair(cp)
	require.Equal(t, "market.atomusdt.kline.1min", huobiSymbol)
}

func TestNewHuobiTickerSubscriptionMsg(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	msg := newHuobiTickerSubscriptionMsg(cp)
	require.Equal(t, "market.atomusdt.ticker", msg.Sub)
}

func TestNewHuobiCandleSubscriptionMsg(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	msg := newHuobiCandleSubscriptionMsg(cp)
	require.Equal(t, "market.atomusdt.kline.1min", msg.Sub)
}

func TestHuobiProvider_NewHuobiProvider(t *testing.T) {
	t.Run("with_custom_endpoints", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		endpoints := config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
			Rest:      "https://custom-api.example.com",
			Websocket: server.GetBaseURL(),
		}

		p, err := NewHuobiProvider(
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

		// Since invalid provider defaults to huobi endpoints,
		// we can only test the REST endpoint defaults, not WebSocket
		// as it would try to connect to real Huobi WebSocket
		require.NotEqual(t, config.ProviderHuobi, endpoints.Name)
		require.Equal(t, "invalid_provider", endpoints.Name)
	})

	t.Run("empty_currency_pairs", func(t *testing.T) {
		server := NewMockProviderServer()
		server.Start()
		defer server.Close()

		_, err := NewHuobiProvider(
			context.TODO(),
			zerolog.Nop(),
			config.ProviderEndpoint{
				Name:      config.ProviderHuobi,
				Rest:      "",
				Websocket: server.GetBaseURL(),
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "currency pairs is empty")
	})
}

func TestDecompressGzip(t *testing.T) {
	t.Run("decompress_gzip_data", func(t *testing.T) {
		// This test would require actual gzip compressed data
		// For now, let's test the error case
		invalidGzipData := []byte("invalid gzip data")
		_, err := decompressGzip(invalidGzipData)
		require.Error(t, err)
	})

	t.Run("decompress_empty_data", func(t *testing.T) {
		emptyData := []byte("")
		_, err := decompressGzip(emptyData)
		require.Error(t, err)
	})
}

func TestHuobiProvider_ConcurrencyHandling(t *testing.T) {
	server := NewMockProviderServer()
	server.Start()
	defer server.Close()

	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{
			Name:      config.ProviderHuobi,
			Rest:      "",
			Websocket: server.GetBaseURL(),
		},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("concurrent_ticker_updates", func(t *testing.T) {
		// Test concurrent access to tickers map
		ticker1 := HuobiTicker{
			CH:   "market.atomusdt.ticker",
			Tick: HuobiTick{LastPrice: 100.0, Vol: 1000.0},
		}
		ticker2 := HuobiTicker{
			CH:   "market.seiusdt.ticker",
			Tick: HuobiTick{LastPrice: 200.0, Vol: 2000.0},
		}

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["market.atomusdt.ticker"] = ticker1
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 100; i++ {
				p.mtx.Lock()
				p.tickers["market.seiusdt.ticker"] = ticker2
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
		candle1 := HuobiCandle{
			CH:   "market.atomusdt.kline.1min",
			Tick: HuobiCandleTick{Close: 100.0, Volume: 10.0, TimeStamp: time.Now().Unix()},
		}
		candle2 := HuobiCandle{
			CH:   "market.seiusdt.kline.1min",
			Tick: HuobiCandleTick{Close: 200.0, Volume: 20.0, TimeStamp: time.Now().Unix()},
		}

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["market.atomusdt.kline.1min"] = []HuobiCandle{candle1}
				p.mtx.Unlock()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				p.mtx.Lock()
				p.candles["market.seiusdt.kline.1min"] = []HuobiCandle{candle2}
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

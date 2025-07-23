package provider

import (
	"net/http"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestNewTickerPrice(t *testing.T) {
	t.Run("valid_ticker_price", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "2396974.02"

		tickerPrice, err := newTickerPrice(provider, symbol, lastPrice, volume)
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr(lastPrice), tickerPrice.Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), tickerPrice.Volume)
	})

	t.Run("invalid_price_format", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "invalid_price"
		volume := "2396974.02"

		_, err := newTickerPrice(provider, symbol, lastPrice, volume)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse binance price")
	})

	t.Run("invalid_volume_format", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "invalid_volume"

		_, err := newTickerPrice(provider, symbol, lastPrice, volume)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse binance volume")
	})

	t.Run("zero_values", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "0"
		volume := "0"

		tickerPrice, err := newTickerPrice(provider, symbol, lastPrice, volume)
		require.NoError(t, err)
		require.True(t, tickerPrice.Price.IsZero())
		require.True(t, tickerPrice.Volume.IsZero())
	})

	t.Run("empty_strings", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := ""
		volume := ""

		_, err := newTickerPrice(provider, symbol, lastPrice, volume)
		require.Error(t, err)
	})
}

func TestNewCandlePrice(t *testing.T) {
	timestamp := time.Now().Unix()

	t.Run("valid_candle_price", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "2396974.02"

		candlePrice, err := newCandlePrice(provider, symbol, lastPrice, volume, timestamp)
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr(lastPrice), candlePrice.Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), candlePrice.Volume)
		require.Equal(t, timestamp, candlePrice.TimeStamp)
	})

	t.Run("invalid_price_format", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "invalid_price"
		volume := "2396974.02"

		_, err := newCandlePrice(provider, symbol, lastPrice, volume, timestamp)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse binance price")
	})

	t.Run("invalid_volume_format", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "invalid_volume"

		_, err := newCandlePrice(provider, symbol, lastPrice, volume, timestamp)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse binance volume")
	})

	t.Run("zero_timestamp", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "2396974.02"

		candlePrice, err := newCandlePrice(provider, symbol, lastPrice, volume, 0)
		require.NoError(t, err)
		require.Equal(t, int64(0), candlePrice.TimeStamp)
	})

	t.Run("negative_timestamp", func(t *testing.T) {
		provider := "binance"
		symbol := "ATOMUSDT"
		lastPrice := "34.69"
		volume := "2396974.02"

		candlePrice, err := newCandlePrice(provider, symbol, lastPrice, volume, -1)
		require.NoError(t, err)
		require.Equal(t, int64(-1), candlePrice.TimeStamp)
	})
}

func TestPastUnixTime(t *testing.T) {
	t.Run("one_minute_ago", func(t *testing.T) {
		duration := time.Minute
		result := PastUnixTime(duration)
		expected := time.Now().Add(-duration).Unix() * int64(time.Second/time.Millisecond)

		// Allow for small timing differences (within 1 second)
		require.InDelta(t, expected, result, 1000)
	})

	t.Run("one_hour_ago", func(t *testing.T) {
		duration := time.Hour
		result := PastUnixTime(duration)
		expected := time.Now().Add(-duration).Unix() * int64(time.Second/time.Millisecond)

		// Allow for small timing differences (within 1 second)
		require.InDelta(t, expected, result, 1000)
	})

	t.Run("zero_duration", func(t *testing.T) {
		duration := time.Duration(0)
		result := PastUnixTime(duration)
		expected := time.Now().Unix() * int64(time.Second/time.Millisecond)

		// Allow for small timing differences (within 1 second)
		require.InDelta(t, expected, result, 1000)
	})

	t.Run("negative_duration", func(t *testing.T) {
		duration := -time.Hour
		result := PastUnixTime(duration)
		expected := time.Now().Add(time.Hour).Unix() * int64(time.Second/time.Millisecond)

		// Allow for small timing differences (within 1 second)
		require.InDelta(t, expected, result, 1000)
	})
}

func TestStrToDec(t *testing.T) {
	t.Run("valid_integer", func(t *testing.T) {
		result := strToDec("123")
		expected := sdk.MustNewDecFromStr("123")
		require.True(t, result.Equal(expected))
	})

	t.Run("valid_decimal", func(t *testing.T) {
		result := strToDec("123.456")
		expected := sdk.MustNewDecFromStr("123.456")
		require.True(t, result.Equal(expected))
	})

	t.Run("decimal_with_18_precision", func(t *testing.T) {
		input := "123.123456789012345678"
		result := strToDec(input)
		expected := sdk.MustNewDecFromStr("123.123456789012345678")
		require.True(t, result.Equal(expected))
	})

	t.Run("decimal_with_more_than_18_precision", func(t *testing.T) {
		input := "123.1234567890123456789012345"
		result := strToDec(input)
		// Should be truncated to 18 decimal places
		expected := sdk.MustNewDecFromStr("123.123456789012345678")
		require.True(t, result.Equal(expected))
	})

	t.Run("zero_value", func(t *testing.T) {
		result := strToDec("0")
		expected := sdk.MustNewDecFromStr("0")
		require.True(t, result.Equal(expected))
	})

	t.Run("negative_value", func(t *testing.T) {
		result := strToDec("-123.456")
		expected := sdk.MustNewDecFromStr("-123.456")
		require.True(t, result.Equal(expected))
	})

	t.Run("very_small_decimal", func(t *testing.T) {
		result := strToDec("0.000000000000000001")
		expected := sdk.MustNewDecFromStr("0.000000000000000001")
		require.True(t, result.Equal(expected))
	})
}

func TestNewDefaultHTTPClient(t *testing.T) {
	t.Run("creates_client_with_default_timeout", func(t *testing.T) {
		client := newDefaultHTTPClient()
		require.NotNil(t, client)
		require.Equal(t, defaultTimeout, client.Timeout)
		require.NotNil(t, client.CheckRedirect)
	})
}

func TestNewHTTPClientWithTimeout(t *testing.T) {
	t.Run("creates_client_with_custom_timeout", func(t *testing.T) {
		timeout := 30 * time.Second
		client := newHTTPClientWithTimeout(timeout)
		require.NotNil(t, client)
		require.Equal(t, timeout, client.Timeout)
		require.NotNil(t, client.CheckRedirect)
	})

	t.Run("creates_client_with_zero_timeout", func(t *testing.T) {
		timeout := time.Duration(0)
		client := newHTTPClientWithTimeout(timeout)
		require.NotNil(t, client)
		require.Equal(t, timeout, client.Timeout)
	})
}

func TestPreventRedirect(t *testing.T) {
	t.Run("returns_use_last_response_error", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://example.com", nil)
		require.NoError(t, err)

		var via []*http.Request
		err = preventRedirect(req, via)
		require.Equal(t, http.ErrUseLastResponse, err)
	})

	t.Run("handles_nil_request", func(t *testing.T) {
		var via []*http.Request
		err := preventRedirect(nil, via)
		require.Equal(t, http.ErrUseLastResponse, err)
	})

	t.Run("handles_nil_via", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://example.com", nil)
		require.NoError(t, err)

		err = preventRedirect(req, nil)
		require.Equal(t, http.ErrUseLastResponse, err)
	})
}

func TestTickerPrice(t *testing.T) {
	t.Run("ticker_price_struct", func(t *testing.T) {
		price := sdk.MustNewDecFromStr("100.50")
		volume := sdk.MustNewDecFromStr("1000.25")

		ticker := TickerPrice{
			Price:  price,
			Volume: volume,
		}

		require.True(t, ticker.Price.Equal(price))
		require.True(t, ticker.Volume.Equal(volume))
	})
}

func TestCandlePrice(t *testing.T) {
	t.Run("candle_price_struct", func(t *testing.T) {
		price := sdk.MustNewDecFromStr("100.50")
		volume := sdk.MustNewDecFromStr("1000.25")
		timestamp := time.Now().Unix()

		candle := CandlePrice{
			Price:     price,
			Volume:    volume,
			TimeStamp: timestamp,
		}

		require.True(t, candle.Price.Equal(price))
		require.True(t, candle.Volume.Equal(volume))
		require.Equal(t, timestamp, candle.TimeStamp)
	})
}

func TestAggregatedProviderPrices(t *testing.T) {
	t.Run("aggregated_provider_prices_type", func(t *testing.T) {
		prices := make(AggregatedProviderPrices)

		binancePrices := make(map[string]TickerPrice)
		binancePrices["ATOMUSDT"] = TickerPrice{
			Price:  sdk.MustNewDecFromStr("100.50"),
			Volume: sdk.MustNewDecFromStr("1000.25"),
		}

		prices["binance"] = binancePrices

		require.Len(t, prices, 1)
		require.Contains(t, prices, "binance")
		require.Len(t, prices["binance"], 1)
		require.Contains(t, prices["binance"], "ATOMUSDT")
	})
}

func TestAggregatedProviderCandles(t *testing.T) {
	t.Run("aggregated_provider_candles_type", func(t *testing.T) {
		candles := make(AggregatedProviderCandles)

		binanceCandles := make(map[string][]CandlePrice)
		binanceCandles["ATOMUSDT"] = []CandlePrice{
			{
				Price:     sdk.MustNewDecFromStr("100.50"),
				Volume:    sdk.MustNewDecFromStr("1000.25"),
				TimeStamp: time.Now().Unix(),
			},
		}

		candles["binance"] = binanceCandles

		require.Len(t, candles, 1)
		require.Contains(t, candles, "binance")
		require.Len(t, candles["binance"], 1)
		require.Contains(t, candles["binance"], "ATOMUSDT")
		require.Len(t, candles["binance"]["ATOMUSDT"], 1)
	})
}

func TestConstants(t *testing.T) {
	t.Run("constants_have_expected_values", func(t *testing.T) {
		require.Equal(t, 10*time.Second, defaultTimeout)
		require.Equal(t, 20*time.Minute, defaultReconnectTime)
		require.Equal(t, 3, maxReconnectionTries)
		require.Equal(t, 10*time.Minute, providerCandlePeriod)
		require.Equal(t, []byte("ping"), ping)
	})
}

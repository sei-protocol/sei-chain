package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	defaultTimeout           = 10 * time.Second
	defaultReadNewWSMessage  = 50 * time.Millisecond
	defaultMaxConnectionTime = time.Hour * 23 // should be < 24h
	defaultReconnectTime     = time.Minute * 20
	maxReconnectionTries     = 3
	providerCandlePeriod     = 10 * time.Minute
)

var ping = []byte("ping")

// Provider defines an interface an exchange price provider must implement.
type Provider interface {
	// GetTickerPrices returns the tickerPrices based on the provided pairs.
	GetTickerPrices(...types.CurrencyPair) (map[string]TickerPrice, error)

	// GetCandlePrices returns the candlePrices based on the provided pairs.
	GetCandlePrices(...types.CurrencyPair) (map[string][]CandlePrice, error)

	// GetAvailablePairs return all available pairs symbol to susbscribe.
	GetAvailablePairs() (map[string]struct{}, error)

	// SubscribeCurrencyPairs subscribe to ticker and candle channels for all pairs.
	SubscribeCurrencyPairs(...types.CurrencyPair) error
}

// TickerPrice defines price and volume information for a symbol or ticker
// exchange rate.
type TickerPrice struct {
	Price  sdk.Dec // last trade price
	Volume sdk.Dec // 24h volume
}

// AggregatedProviderPrices defines a type alias for a map
// of provider -> asset -> TickerPrice
type AggregatedProviderPrices map[string]map[string]TickerPrice

// CandlePrice defines price, volume, and time information for an
// exchange rate.
type CandlePrice struct {
	Price     sdk.Dec // last trade price
	Volume    sdk.Dec // volume
	TimeStamp int64   // timestamp
}

// AggregatedProviderCandles defines a type alias for a map
// of provider -> asset -> []CandlePrice
type AggregatedProviderCandles map[string]map[string][]CandlePrice

// preventRedirect avoid any redirect in the http.Client the request call
// will not return an error, but a valid response with redirect response code.
//
//nolint:unused
func preventRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

//nolint:unused,deadcode
func newDefaultHTTPClient() *http.Client {
	return newHTTPClientWithTimeout(defaultTimeout)
}

//nolint:unused
func newHTTPClientWithTimeout(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: preventRedirect,
	}
}

func newTickerPrice(provider, symbol, lastPrice, volume string) (TickerPrice, error) {
	price, err := sdk.NewDecFromStr(lastPrice)
	if err != nil {
		return TickerPrice{}, fmt.Errorf("failed to parse %s price (%s) for %s", provider, lastPrice, symbol)
	}

	volumeDec, err := sdk.NewDecFromStr(volume)
	if err != nil {
		return TickerPrice{}, fmt.Errorf("failed to parse %s volume (%s) for %s", provider, volume, symbol)
	}

	return TickerPrice{Price: price, Volume: volumeDec}, nil
}

func newCandlePrice(provider, symbol, lastPrice, volume string, timeStamp int64) (CandlePrice, error) {
	price, err := sdk.NewDecFromStr(lastPrice)
	if err != nil {
		return CandlePrice{}, fmt.Errorf("failed to parse %s price (%s) for %s", provider, lastPrice, symbol)
	}

	volumeDec, err := sdk.NewDecFromStr(volume)
	if err != nil {
		return CandlePrice{}, fmt.Errorf("failed to parse %s volume (%s) for %s", provider, volume, symbol)
	}

	return CandlePrice{Price: price, Volume: volumeDec, TimeStamp: timeStamp}, nil
}

// PastUnixTime returns a millisecond timestamp that represents the unix time
// minus t.
func PastUnixTime(t time.Duration) int64 {
	return time.Now().Add(t*-1).Unix() * int64(time.Second/time.Millisecond)
}

//nolint:unused,deadcode
func strToDec(str string) sdk.Dec {
	if strings.Contains(str, ".") {
		split := strings.Split(str, ".")
		if len(split[1]) > 18 {
			// sdk.MustNewDecFromStr will panic if decimal precision is greater than 18
			str = split[0] + "." + split[1][0:18]
		}
	}
	return sdk.MustNewDecFromStr(str)
}

package provider

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
)

const (
	finRestURL               = "https://api.kujira.app"
	finPairsEndpoint         = "/api/coingecko/pairs"
	finTickersEndpoint       = "/api/coingecko/tickers"
	finCandlesEndpoint       = "/api/trades/candles"
	finCandleBinSizeMinutes  = 5
	finCandleWindowSizeHours = 240
)

var _ Provider = (*FinProvider)(nil)

type (
	FinProvider struct {
		baseURL string
		client  *http.Client
	}

	FinTickers struct {
		Tickers []FinTicker `json:"tickers"`
	}

	FinTicker struct {
		Base   string `json:"base_currency"`
		Target string `json:"target_currency"`
		Symbol string `json:"ticker_id"`
		Price  string `json:"last_price"`
		Volume string `json:"base_volume"`
	}

	FinCandles struct {
		Candles []FinCandle `json:"candles"`
	}

	FinCandle struct {
		Bin    string `json:"bin"`
		Close  string `json:"close"`
		Volume string `json:"volume"`
	}

	FinPairs struct {
		Pairs []FinPair `json:"pairs"`
	}

	FinPair struct {
		Base    string `json:"base"`
		Target  string `json:"target"`
		Symbol  string `json:"ticker_id"`
		Address string `json:"pool_id"`
	}
)

func NewFinProvider(endpoint config.ProviderEndpoint) *FinProvider {
	if endpoint.Name == config.ProviderFin {
		return &FinProvider{
			baseURL: endpoint.Rest,
			client:  newDefaultHTTPClient(),
		}
	}
	return &FinProvider{
		baseURL: finRestURL,
		client:  newDefaultHTTPClient(),
	}
}

func (p FinProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
	path := fmt.Sprintf("%s%s", p.baseURL, finTickersEndpoint)
	tickerResponse, err := p.client.Get(path)
	if err != nil {
		return nil, fmt.Errorf("FIN tickers request failed: %w", err)
	}
	defer tickerResponse.Body.Close()
	tickerContent, err := ioutil.ReadAll(tickerResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("FIN tickers response read failed: %w", err)
	}
	var tickers FinTickers
	err = json.Unmarshal(tickerContent, &tickers)
	if err != nil {
		return nil, fmt.Errorf("FIN tickers response unmarshal failed: %w", err)
	}
	tickerSymbolPairs := make(map[string]types.CurrencyPair, len(pairs))
	for _, pair := range pairs {
		tickerSymbolPairs[pair.Base+"_"+pair.Quote] = pair
	}
	tickerPrices := make(map[string]TickerPrice, len(pairs))
	for _, ticker := range tickers.Tickers {
		pair, ok := tickerSymbolPairs[strings.ToUpper(ticker.Symbol)]
		if !ok {
			// skip tokens that are not requested
			continue
		}
		_, ok = tickerPrices[pair.String()]
		if ok {
			return nil, fmt.Errorf("FIN tickers response contained duplicate: %s", ticker.Symbol)
		}
		tickerPrices[pair.String()] = TickerPrice{
			Price:  strToDec(ticker.Price),
			Volume: strToDec(ticker.Volume),
		}
	}
	for _, pair := range pairs {
		_, ok := tickerPrices[pair.String()]
		if !ok {
			return nil, fmt.Errorf("FIN ticker price missing for pair: %s", pair.String())
		}
	}
	return tickerPrices, nil
}

func (p FinProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	pairAddresses, err := p.getFinPairAddresses()
	if err != nil {
		return nil, fmt.Errorf("FIN pair addresses lookup failed: %w", err)
	}
	candlePricesPairs := make(map[string][]CandlePrice)
	for _, pair := range pairs {
		address, ok := pairAddresses[pair.String()]
		if !ok {
			return nil, fmt.Errorf("FIN contract address lookup failed for pair: %s", pair.String())
		}
		candlePricesPairs[pair.String()] = []CandlePrice{}
		windowEndTime := time.Now()
		windowStartTime := windowEndTime.Add(-finCandleWindowSizeHours * time.Hour)
		path := fmt.Sprintf("%s%s?contract=%s&precision=%d&from=%s&to=%s",
			p.baseURL,
			finCandlesEndpoint,
			address,
			finCandleBinSizeMinutes,
			windowStartTime.Format(time.RFC3339),
			windowEndTime.Format(time.RFC3339),
		)
		candlesResponse, err := p.client.Get(path)
		if err != nil {
			return nil, fmt.Errorf("FIN candles request failed: %w", err)
		}
		defer candlesResponse.Body.Close()
		candlesContent, err := ioutil.ReadAll(candlesResponse.Body)
		if err != nil {
			return nil, fmt.Errorf("FIN candles response read failed: %w", err)
		}
		var candles FinCandles
		err = json.Unmarshal(candlesContent, &candles)
		if err != nil {
			return nil, fmt.Errorf("FIN candles response unmarshal failed: %w", err)
		}
		candlePrices := []CandlePrice{}
		for _, candle := range candles.Candles {
			timeStamp, err := binToTimeStamp(candle.Bin)
			if err != nil {
				return nil, fmt.Errorf("FIN candle timestamp failed to parse: %s", candle.Bin)
			}
			candlePrices = append(candlePrices, CandlePrice{
				Price:     strToDec(candle.Close),
				Volume:    strToDec(candle.Volume),
				TimeStamp: timeStamp,
			})
		}
		candlePricesPairs[pair.String()] = candlePrices
	}
	return candlePricesPairs, nil
}

func (p FinProvider) GetAvailablePairs() (map[string]struct{}, error) {
	finPairs, err := p.getFinPairs()
	if err != nil {
		return nil, err
	}
	availablePairs := make(map[string]struct{}, len(finPairs.Pairs))
	for _, pair := range finPairs.Pairs {
		pair := types.CurrencyPair{
			Base:  strings.ToUpper(pair.Base),
			Quote: strings.ToUpper(pair.Target),
		}
		availablePairs[pair.String()] = struct{}{}
	}
	return availablePairs, nil
}

func (p FinProvider) getFinPairs() (FinPairs, error) {
	path := fmt.Sprintf("%s%s", p.baseURL, finPairsEndpoint)
	pairsResponse, err := p.client.Get(path)
	if err != nil {
		return FinPairs{}, err
	}
	defer pairsResponse.Body.Close()
	var pairs FinPairs
	err = json.NewDecoder(pairsResponse.Body).Decode(&pairs)
	if err != nil {
		return FinPairs{}, err
	}
	return pairs, nil
}

func (p FinProvider) getFinPairAddresses() (map[string]string, error) {
	finPairs, err := p.getFinPairs()
	if err != nil {
		return nil, err
	}
	pairAddresses := make(map[string]string, len(finPairs.Pairs))
	for _, pair := range finPairs.Pairs {
		pairAddresses[strings.ToUpper(pair.Base+pair.Target)] = pair.Address
	}
	return pairAddresses, nil
}

// SubscribeCurrencyPairs performs a no-op since fin does not use websockets
func (p FinProvider) SubscribeCurrencyPairs(pairs ...types.CurrencyPair) error {
	return nil
}

func binToTimeStamp(bin string) (int64, error) {
	timeParsed, err := time.Parse(time.RFC3339, bin)
	if err != nil {
		return -1, err
	}
	return timeParsed.Unix(), nil
}

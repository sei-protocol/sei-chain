package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

const (
	binanceWSHost   = "stream.binance.com:9443"
	binanceWSPath   = "/ws/umeestream"
	binanceRestHost = "https://api1.binance.com"
	binanceRestPath = "/api/v3/ticker/price"
)

var _ Provider = (*BinanceProvider)(nil)

type (
	// BinanceProvider defines an Oracle provider implemented by the Binance public
	// API.
	//
	// REF: https://binance-docs.github.io/apidocs/spot/en/#individual-symbol-mini-ticker-stream
	// REF: https://binance-docs.github.io/apidocs/spot/en/#kline-candlestick-streams
	BinanceProvider struct {
		wsURL           url.URL
		wsClient        *websocket.Conn
		logger          zerolog.Logger
		mtx             sync.RWMutex
		endpoints       config.ProviderEndpoint
		tickers         map[string]BinanceTicker      // Symbol => BinanceTicker
		candles         map[string][]BinanceCandle    // Symbol => BinanceCandle
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	// BinanceTicker ticker price response. https://pkg.go.dev/encoding/json#Unmarshal
	// Unmarshal matches incoming object keys to the keys used by Marshal (either the
	// struct field name or its tag), preferring an exact match but also accepting a
	// case-insensitive match. C field which is Statistics close time is not used, but
	// it avoids to implement specific UnmarshalJSON.
	BinanceTicker struct {
		Symbol    string `json:"s"` // Symbol ex.: BTCUSDT
		LastPrice string `json:"c"` // Last price ex.: 0.0025
		Volume    string `json:"v"` // Total traded base asset volume ex.: 1000
		C         uint64 `json:"C"` // Statistics close time
	}

	// BinanceCandleMetadata candle metadata used to compute tvwap price.
	BinanceCandleMetadata struct {
		Close     string `json:"c"` // Price at close
		TimeStamp int64  `json:"T"` // Close time in unix epoch ex.: 1645756200000
		Volume    string `json:"v"` // Volume during period
	}

	// BinanceCandle candle binance websocket channel "kline_1m" response.
	BinanceCandle struct {
		Symbol   string                `json:"s"` // Symbol ex.: BTCUSDT
		Metadata BinanceCandleMetadata `json:"k"` // Metadata for candle
	}

	// BinanceSubscribeMsg Msg to subscribe all the tickers channels.
	BinanceSubscriptionMsg struct {
		Method string   `json:"method"` // SUBSCRIBE/UNSUBSCRIBE
		Params []string `json:"params"` // streams to subscribe ex.: usdtatom@ticker
		ID     uint16   `json:"id"`     // identify messages going back and forth
	}

	// BinancePairSummary defines the response structure for a Binance pair
	// summary.
	BinancePairSummary struct {
		Symbol string `json:"symbol"`
	}
)

func NewBinanceProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*BinanceProvider, error) {
	if (endpoints.Name) != config.ProviderBinance {
		endpoints = config.ProviderEndpoint{
			Name:      config.ProviderBinance,
			Rest:      binanceRestHost,
			Websocket: binanceWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   binanceWSPath,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Binance websocket: %w, %v", err, response)
	}

	provider := &BinanceProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "binance").Logger(),
		endpoints:       endpoints,
		tickers:         map[string]BinanceTicker{},
		candles:         map[string][]BinanceCandle{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}

	if err := provider.SubscribeCurrencyPairs(pairs...); err != nil {
		return nil, err
	}

	go provider.handleWebSocketMsgs(ctx)

	return provider, nil
}

// GetTickerPrices returns the tickerPrices based on the provided pairs.
func (p *BinanceProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
	tickerPrices := make(map[string]TickerPrice, len(pairs))

	for _, cp := range pairs {
		key := cp.String()
		price, err := p.getTickerPrice(key)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch tickers for pair ", cp))
			continue
		}
		tickerPrices[key] = price
	}

	return tickerPrices, nil
}

// GetCandlePrices returns the candlePrices based on the provided pairs.
func (p *BinanceProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	candlePrices := make(map[string][]CandlePrice, len(pairs))

	for _, cp := range pairs {
		key := cp.String()
		prices, err := p.getCandlePrices(key)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch candles for pair ", cp))
			continue
		}
		candlePrices[key] = prices
	}

	return candlePrices, nil
}

// SubscribeCurrencyPairs subscribe all currency pairs into ticker and candle channels.
func (p *BinanceProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	if len(cps) == 0 {
		return fmt.Errorf("currency pairs is empty")
	}

	if err := p.subscribeChannels(cps...); err != nil {
		return err
	}

	p.setSubscribedPairs(cps...)
	return nil
}

// subscribeChannels subscribe to the ticker and candle channels for all currency pairs.
func (p *BinanceProvider) subscribeChannels(cps ...types.CurrencyPair) error {
	if err := p.subscribeTickers(cps...); err != nil {
		return err
	}

	return p.subscribeCandles(cps...)
}

// subscribeTickers subscribe to the ticker channel for all currency pairs.
func (p *BinanceProvider) subscribeTickers(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = currencyPairToBinanceTickerPair(cp)
	}

	return p.subscribePairs(pairs...)
}

// subscribeCandles subscribe to the candle channel for all currency pairs.
func (p *BinanceProvider) subscribeCandles(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = currencyPairToBinanceCandlePair(cp)
	}

	return p.subscribePairs(pairs...)
}

// subscribedPairsToSlice returns the map of subscribed pairs as a slice.
func (p *BinanceProvider) subscribedPairsToSlice() []types.CurrencyPair {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return types.MapPairsToSlice(p.subscribedPairs)
}

func (p *BinanceProvider) getTickerPrice(key string) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	ticker, ok := p.tickers[key]
	if !ok {
		return TickerPrice{}, fmt.Errorf("binance provider failed to get ticker price for %s", key)
	}

	return ticker.toTickerPrice()
}

func (p *BinanceProvider) getCandlePrices(key string) ([]CandlePrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	candles, ok := p.candles[key]
	if !ok {
		return []CandlePrice{}, fmt.Errorf("failed to get candle prices for %s", key)
	}

	candleList := []CandlePrice{}
	for _, candle := range candles {
		cp, err := candle.toCandlePrice()
		if err != nil {
			return []CandlePrice{}, err
		}
		candleList = append(candleList, cp)
	}
	return candleList, nil
}

func (p *BinanceProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		tickerResp BinanceTicker
		tickerErr  error
		candleResp BinanceCandle
		candleErr  error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)
	if len(tickerResp.LastPrice) != 0 {
		p.setTickerPair(tickerResp)
		metrics.SafeTelemetryIncrCounter(
			1,
			"websocket",
			"message",
			"type",
			"ticker",
			"provider",
			config.ProviderBinance,
		)
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if len(candleResp.Metadata.Close) != 0 {
		p.setCandlePair(candleResp)
		metrics.SafeTelemetryIncrCounter(
			1,
			"websocket",
			"message",
			"type",
			"candle",
			"provider",
			config.ProviderBinance,
		)
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		Msg("Error on receive message")
}

func (p *BinanceProvider) setTickerPair(ticker BinanceTicker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.tickers[ticker.Symbol] = ticker
}

func (p *BinanceProvider) setCandlePair(candle BinanceCandle) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []BinanceCandle{}
	candleList = append(candleList, candle)

	for _, c := range p.candles[candle.Symbol] {
		if staleTime < c.Metadata.TimeStamp {
			candleList = append(candleList, c)
		}
	}
	p.candles[candle.Symbol] = candleList
}

func (ticker BinanceTicker) toTickerPrice() (TickerPrice, error) {
	return newTickerPrice("Binance", ticker.Symbol, ticker.LastPrice, ticker.Volume)
}

func (candle BinanceCandle) toCandlePrice() (CandlePrice, error) {
	return newCandlePrice("Binance", candle.Symbol, candle.Metadata.Close, candle.Metadata.Volume,
		candle.Metadata.TimeStamp)
}

func (p *BinanceProvider) handleWebSocketMsgs(ctx context.Context) {
	reconnectTicker := time.NewTicker(defaultMaxConnectionTime)
	defer reconnectTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(defaultReadNewWSMessage):
			messageType, bz, err := p.wsClient.ReadMessage()
			if err != nil {
				// if some error occurs continue to try to read the next message.
				p.logger.Err(err).Msg("could not read message")
				continue
			}

			if len(bz) == 0 {
				continue
			}

			p.messageReceived(messageType, bz)

		case <-reconnectTicker.C:
			if err := p.reconnect(); err != nil {
				p.logger.Err(err).Msg("error reconnecting")
				p.keepReconnecting()
			}
		}
	}
}

// reconnect closes the last WS connection then create a new one and subscribe to
// all subscribed pairs in the ticker and candle pais. A single connection to
// stream.binance.com is only valid for 24 hours; expect to be disconnected at the
// 24 hour mark. The websocket server will send a ping frame every 3 minutes. If
// the websocket server does not receive a pong frame back from the connection
// within a 10 minute period, the connection will be disconnected.
func (p *BinanceProvider) reconnect() error {
	p.wsClient.Close()

	p.logger.Debug().Msg("reconnecting websocket")
	wsConn, response, err := websocket.DefaultDialer.Dial(p.wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("error reconnect to binance websocket: %w", err)
	}
	p.wsClient = wsConn

	currencyPairs := p.subscribedPairsToSlice()

	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"reconnect",
		"provider",
		config.ProviderBinance,
	)
	return p.subscribeChannels(currencyPairs...)
}

// keepReconnecting keeps trying to reconnect if an error occurs in reconnect.
func (p *BinanceProvider) keepReconnecting() {
	reconnectTicker := time.NewTicker(defaultReconnectTime)
	defer reconnectTicker.Stop()
	connectionTries := 1

	for time := range reconnectTicker.C {
		if err := p.reconnect(); err != nil {
			p.logger.Err(err).Msgf("attempted to reconnect %d times at %s", connectionTries, time.String())
			connectionTries++
			continue
		}

		if connectionTries > maxReconnectionTries {
			p.logger.Warn().Msgf("failed to reconnect %d times", connectionTries)
		}
		return
	}
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *BinanceProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// subscribePairs write the subscription msg to the provider.
func (p *BinanceProvider) subscribePairs(pairs ...string) error {
	subsMsg := newBinanceSubscriptionMsg(pairs...)
	return p.wsClient.WriteJSON(subsMsg)
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "UMEEUSDC" => {}].
func (p *BinanceProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + binanceRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []BinancePairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToBinanceTickerPair receives a currency pair and return binance
// ticker symbol atomusdt@ticker.
func currencyPairToBinanceTickerPair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@ticker")
}

// currencyPairToBinanceCandlePair receives a currency pair and return binance
// candle symbol atomusdt@kline_1m.
func currencyPairToBinanceCandlePair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@kline_1m")
}

// newBinanceSubscriptionMsg returns a new subscription Msg.
func newBinanceSubscriptionMsg(params ...string) BinanceSubscriptionMsg {
	return BinanceSubscriptionMsg{
		Method: "SUBSCRIBE",
		Params: params,
		ID:     1,
	}
}

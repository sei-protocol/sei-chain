package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	mexcWSHost   = "wbs.mexc.com"
	mexcWSPath   = "/raw/ws"
	mexcRestHost = "https://www.mexc.com"
	mexcRestPath = "/open/api/v2/market/ticker"
)

var _ Provider = (*MexcProvider)(nil)

type (
	// MexcProvider defines an Oracle provider implemented by the Mexc public
	// API.
	//
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#ticker-information
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#k-line
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#overview
	MexcProvider struct {
		wsURL           url.URL
		wsClient        *websocket.Conn
		logger          zerolog.Logger
		mtx             sync.RWMutex
		endpoints       config.ProviderEndpoint
		tickers         map[string]MexcTicker         // Symbol => MexcTicker
		candles         map[string][]MexcCandle       // Symbol => MexcCandle
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	// MexcTicker ticker price response. https://pkg.go.dev/encoding/json#Unmarshal
	// Unmarshal matches incoming object keys to the keys used by Marshal (either the
	// struct field name or its tag), preferring an exact match but also accepting a
	// case-insensitive match. C field which is Statistics close time is not used, but
	// it avoids to implement specific UnmarshalJSON.

	MexcTicker struct {
		Symbol    string `json:"symbol"` // Symbol ex.: ATOM_USDT
		LastPrice string `json:"p"`      // Last price ex.: 0.0025
		Volume    string `json:"v"`      // Total traded base asset volume ex.: 1000
		C         uint64 `json:"C"`      // Statistics close time
	}

	MexcTickerData struct {
		LastPrice float64 `json:"p"` // Last price ex.: 0.0025
		Volume    float64 `json:"v"` // Total traded base asset volume ex.: 1000
	}

	MexcTickerResult struct {
		Channel string                    `json:"channel"` // expect "push.overview"
		Symbol  map[string]MexcTickerData `json:"data"`    // this key is the Symbol ex.: ATOM_USDT
	}

	// MexcCandleMetadata candle metadata used to compute tvwap price.
	MexcCandleMetadata struct {
		Close     float64 `json:"c"` // Price at close
		TimeStamp int64   `json:"t"` // Close time in unix epoch ex.: 1645756200000
		Volume    float64 `json:"v"` // Volume during period
	}

	// MexcCandle candle Mexc websocket channel "kline_1m" response.
	MexcCandle struct {
		// Channel  string             `json:"channel"` // expect "push.kline"
		Symbol   string             `json:"symbol"` // Symbol ex.: ATOM_USDT
		Metadata MexcCandleMetadata `json:"data"`   // Metadata for candle
	}

	// MexcSubscribeMsg Msg to subscribe all the tickers channels.
	MexcCandleSubscriptionMsg struct {
		OP       string `json:"op"`       // kline
		Symbol   string `json:"symbol"`   // streams to subscribe ex.: atom_usdt
		Interval string `json:"interval"` // Min1、Min5、Min15、Min30
	}

	// MexcSubscribeMsg Msg to subscribe all the tickers channels.
	MexcTickerSubscriptionMsg struct {
		OP string `json:"op"` // kline
	}

	// MexcPairSummary defines the response structure for a Mexc pair
	// summary.
	MexcPairSummary struct {
		Symbol string `json:"symbol"`
	}
)

func NewMexcProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*MexcProvider, error) {
	if (endpoints.Name) != config.ProviderMexc {
		endpoints = config.ProviderEndpoint{
			Name:      config.ProviderMexc,
			Rest:      mexcRestHost,
			Websocket: mexcWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   mexcWSPath,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to mexc websocket: %w", err)
	}

	provider := &MexcProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "mexc").Logger(),
		endpoints:       endpoints,
		tickers:         map[string]MexcTicker{},
		candles:         map[string][]MexcCandle{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}

	if err := provider.SubscribeCurrencyPairs(pairs...); err != nil {
		return nil, err
	}

	go provider.handleWebSocketMsgs(ctx)

	return provider, nil
}

// GetTickerPrices returns the tickerPrices based on the provided pairs.
func (p *MexcProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
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
func (p *MexcProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
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
func (p *MexcProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
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
func (p *MexcProvider) subscribeChannels(cps ...types.CurrencyPair) error {
	if err := p.subscribeTickers(cps...); err != nil {
		return err
	}

	return p.subscribeCandles(cps...)
}

// subscribeTickers subscribe to the ticker channel for all currency pairs.
func (p *MexcProvider) subscribeTickers(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = currencyPairToMexcPair(cp)
	}

	return p.subscribePairs(pairs...)
}

// subscribeCandles subscribe to the candle channel for all currency pairs.
func (p *MexcProvider) subscribeCandles(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = currencyPairToMexcPair(cp)
	}

	return p.subscribePairs(pairs...)
}

// subscribedPairsToSlice returns the map of subscribed pairs as a slice.
func (p *MexcProvider) subscribedPairsToSlice() []types.CurrencyPair {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return types.MapPairsToSlice(p.subscribedPairs)
}

func (p *MexcProvider) getTickerPrice(key string) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	ticker, ok := p.tickers[key]
	if !ok {
		return TickerPrice{}, fmt.Errorf("mexc provider failed to get ticker price for %s", key)
	}

	return ticker.toTickerPrice()
}

func (p *MexcProvider) getCandlePrices(key string) ([]CandlePrice, error) {
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

func (p *MexcProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		tickerResp MexcTickerResult
		tickerErr  error
		candleResp MexcCandle
		candleErr  error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)
	// subscribed_pairs := make([]string, 0, len(p.subscribedPairs))
	for _, cp := range p.subscribedPairs {
		if tickerResp.Symbol[currencyPairToMexcPair(cp)].LastPrice != 0 {
			p.setTickerPair(cp.String(), tickerResp.Symbol[currencyPairToMexcPair(cp)])
			metrics.SafeTelemetryIncrCounter(
				1,
				"websocket",
				"message",
				"type",
				"ticker",
				"provider",
				config.ProviderMexc,
			)
			return
		}
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if candleResp.Metadata.Close != 0 {
		p.setCandlePair(candleResp)
		metrics.SafeTelemetryIncrCounter(
			1,
			"websocket",
			"message",
			"type",
			"candle",
			"provider",
			config.ProviderMexc,
		)
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		Msg("mexc: Error on receive message")
}

func (p *MexcProvider) setTickerPair(symbol string, ticker MexcTickerData) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	var mt MexcTicker
	mt.Symbol = symbol
	mt.LastPrice = strconv.FormatFloat(ticker.LastPrice, 'f', 5, 64)
	mt.Volume = strconv.FormatFloat(ticker.Volume, 'f', 5, 64)
	// Uncomment below two lines to log retrieved ticker prices
	// msg := mt.Symbol + " - $" + mt.LastPrice + " - V: " + mt.Volume
	// p.logger.Warn().Msgf("mexc got price: %d", msg)
	p.tickers[symbol] = mt
}

func (p *MexcProvider) setCandlePair(candle MexcCandle) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []MexcCandle{}
	candleList = append(candleList, candle)

	for _, c := range p.candles[candle.Symbol] {
		if staleTime < c.Metadata.TimeStamp {
			candleList = append(candleList, c)
		}
	}
	// Uncomment below two lines to log retrieved candle prices
	// msg := strconv.FormatInt(candle.Metadata.TimeStamp, 10) + " - " + candle.Symbol + " - C: $" + strconv.FormatFloat(candle.Metadata.Close, 'f', 5, 64) + " - V: " + strconv.FormatFloat(candle.Metadata.Volume, 'f', 5, 64)
	// p.logger.Warn().Msgf("mexc got candle: %d", msg)
	p.candles[candle.Symbol] = candleList
}

func (ticker MexcTicker) toTickerPrice() (TickerPrice, error) {
	return newTickerPrice("Mexc", ticker.Symbol, ticker.LastPrice, ticker.Volume)
}

func (candle MexcCandle) toCandlePrice() (CandlePrice, error) {
	return newCandlePrice("Mexc", candle.Symbol, strconv.FormatFloat(candle.Metadata.Close, 'f', 5, 64), strconv.FormatFloat(candle.Metadata.Volume, 'f', 5, 64),
		candle.Metadata.TimeStamp)
}

func (p *MexcProvider) handleWebSocketMsgs(ctx context.Context) {
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
				p.logger.Err(err).Msg("mexc: could not read message")
				continue
			}

			if len(bz) == 0 {
				continue
			}

			p.messageReceived(messageType, bz)

		case <-reconnectTicker.C:
			if err := p.reconnect(); err != nil {
				p.logger.Err(err).Msg("mexc: error reconnecting")
				p.keepReconnecting()
			}
		}
	}
}

// reconnect closes the last WS connection then create a new one and subscribe to
// all subscribed pairs in the ticker and candle pairs. If no ping is received
// within 1 minute, the connection will be disconnected. It is recommended to
// send a ping for 10-20 seconds
func (p *MexcProvider) reconnect() error {
	p.wsClient.Close()

	p.logger.Debug().Msg("mexc: reconnecting websocket")
	wsConn, response, err := websocket.DefaultDialer.Dial(p.wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("mexc: error reconnect to mexc websocket: %w", err)
	}
	p.wsClient = wsConn

	currencyPairs := p.subscribedPairsToSlice()

	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"reconnect",
		"provider",
		config.ProviderMexc,
	)
	return p.subscribeChannels(currencyPairs...)
}

// keepReconnecting keeps trying to reconnect if an error occurs in reconnect.
func (p *MexcProvider) keepReconnecting() {
	reconnectTicker := time.NewTicker(defaultReconnectTime)
	defer reconnectTicker.Stop()
	connectionTries := 1

	for time := range reconnectTicker.C {
		if err := p.reconnect(); err != nil {
			p.logger.Err(err).Msgf("mexc: attempted to reconnect %d times at %s", connectionTries, time.String())
			connectionTries++
			continue
		}

		if connectionTries > maxReconnectionTries {
			p.logger.Warn().Msgf("mexc: failed to reconnect %d times", connectionTries)
		}
		return
	}
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *MexcProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// subscribePairs write the subscription msg to the provider.
func (p *MexcProvider) subscribePairs(pairs ...string) error {
	for _, cp := range pairs {
		subsMsg := newMexcCandleSubscriptionMsg(cp)
		err := p.wsClient.WriteJSON(subsMsg)
		if err != nil {
			return err
		}
	}
	subsMsg := newMexcTickerSubscriptionMsg()
	return p.wsClient.WriteJSON(subsMsg)
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "UMEEUSDC" => {}].
func (p *MexcProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + mexcRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []MexcPairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToMexcPair receives a currency pair and return mexc
// ticker symbol atomusdt@ticker.
func currencyPairToMexcPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + "_" + cp.Quote)
}

// newMexcCandleSubscriptionMsg returns a new candle subscription Msg.
func newMexcCandleSubscriptionMsg(param string) MexcCandleSubscriptionMsg {
	return MexcCandleSubscriptionMsg{
		OP:       "sub.kline",
		Symbol:   param,
		Interval: "Min1",
	}
}

// newMexcTickerSubscriptionMsg returns a new ticker subscription Msg.
func newMexcTickerSubscriptionMsg() MexcTickerSubscriptionMsg {
	return MexcTickerSubscriptionMsg{
		OP: "sub.overview",
	}
}

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

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
)

const (
	okxWSHost    = "ws.okx.com:8443"
	okxWSPath    = "/ws/v5/public"
	okxPingCheck = time.Second * 28 // should be < 30
	okxRestHost  = "https://www.okx.com"
	okxRestPath  = "/api/v5/market/tickers?instType=SPOT"
)

var _ Provider = (*OkxProvider)(nil)

type (
	// OkxProvider defines an Oracle provider implemented by the Okx public
	// API.
	//
	// REF: https://www.okx.com/docs-v5/en/#websocket-api-public-channel-tickers-channel
	OkxProvider struct {
		wsURL           url.URL
		wsClient        *websocket.Conn
		logger          zerolog.Logger
		reconnectTimer  *time.Ticker
		mtx             sync.RWMutex
		endpoints       config.ProviderEndpoint
		tickers         map[string]OkxTickerPair      // InstId => OkxTickerPair
		candles         map[string][]OkxCandlePair    // InstId => 0kxCandlePair
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	// OkxInstId defines the id Symbol of an pair.
	OkxInstID struct {
		InstID string `json:"instId"` // Instrument ID ex.: BTC-USDT
	}

	// OkxTickerPair defines a ticker pair of Okx.
	OkxTickerPair struct {
		OkxInstID
		Last   string `json:"last"`   // Last traded price ex.: 43508.9
		Vol24h string `json:"vol24h"` // 24h trading volume ex.: 11159.87127845
	}

	// OkxInst defines the structure containing ID information for the OkxResponses.
	OkxID struct {
		OkxInstID
		Channel string `json:"channel"`
	}

	// OkxTickerResponse defines the response structure of a Okx ticker request.
	OkxTickerResponse struct {
		Data []OkxTickerPair `json:"data"`
		ID   OkxID           `json:"arg"`
	}

	// OkxCandlePair defines a candle for Okx.
	OkxCandlePair struct {
		Close     string `json:"c"`      // Close price for this time period
		TimeStamp int64  `json:"ts"`     // Linux epoch timestamp
		Volume    string `json:"vol"`    // Volume for this time period
		InstID    string `json:"instId"` // Instrument ID ex.: BTC-USDT
	}

	// OkxCandleResponse defines the response structure of a Okx candle request.
	OkxCandleResponse struct {
		Data [][]string `json:"data"`
		ID   OkxID      `json:"arg"`
	}

	// OkxSubscriptionTopic Topic with the ticker to be subscribed/unsubscribed.
	OkxSubscriptionTopic struct {
		Channel string `json:"channel"` // Channel name ex.: tickers
		InstID  string `json:"instId"`  // Instrument ID ex.: BTC-USDT
	}

	// OkxSubscriptionMsg Message to subscribe/unsubscribe with N Topics.
	OkxSubscriptionMsg struct {
		Op   string                 `json:"op"` // Operation ex.: subscribe
		Args []OkxSubscriptionTopic `json:"args"`
	}

	// OkxPairsSummary defines the response structure for an Okx pairs summary.
	OkxPairsSummary struct {
		Data []OkxInstID `json:"data"`
	}
)

// NewOkxProvider creates a new OkxProvider.
func NewOkxProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*OkxProvider, error) {
	if endpoints.Name != config.ProviderOkx {
		endpoints = config.ProviderEndpoint{
			Name:      config.ProviderOkx,
			Rest:      okxRestHost,
			Websocket: okxWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   okxWSPath,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Okx websocket: %w", err)
	}

	provider := &OkxProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "okx").Logger(),
		reconnectTimer:  time.NewTicker(okxPingCheck),
		endpoints:       endpoints,
		tickers:         map[string]OkxTickerPair{},
		candles:         map[string][]OkxCandlePair{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}
	provider.wsClient.SetPongHandler(provider.pongHandler)

	if err := provider.SubscribeCurrencyPairs(pairs...); err != nil {
		return nil, err
	}

	go provider.handleReceivedTickers(ctx)

	return provider, nil
}

// GetTickerPrices returns the tickerPrices based on the saved map.
func (p *OkxProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
	tickerPrices := make(map[string]TickerPrice, len(pairs))

	for _, currencyPair := range pairs {
		price, err := p.getTickerPrice(currencyPair)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch tickers for pair ", currencyPair))
			continue
		}

		tickerPrices[currencyPair.String()] = price
	}

	return tickerPrices, nil
}

// GetCandlePrices returns the candlePrices based on the saved map
func (p *OkxProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	candlePrices := make(map[string][]CandlePrice, len(pairs))

	for _, currencyPair := range pairs {
		candles, err := p.getCandlePrices(currencyPair)
		if err != nil {
			// CONTEXT: we are ok erroring here because we have disabled the candles subscriptions
			return nil, err
		}

		candlePrices[currencyPair.String()] = candles
	}

	return candlePrices, nil
}

// SubscribeCurrencyPairs subscribe all currency pairs into ticker and candle channels.
func (p *OkxProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	if len(cps) == 0 {
		return fmt.Errorf("currency pairs is empty")
	}

	if err := p.subscribeChannels(cps...); err != nil {
		return err
	}

	p.setSubscribedPairs(cps...)
	metrics.SafeTelemetryIncrCounter(
		float32(len(cps)),
		"websocket",
		"subscribe",
		"currency_pairs",
		"provider",
		config.ProviderOkx,
	)
	return nil
}

// subscribeChannels subscribe all currency pairs into ticker and candle channels.
func (p *OkxProvider) subscribeChannels(cps ...types.CurrencyPair) error {

	return p.subscribeTickers(cps...)

	// CONTEXT: we want to no-op the candles subscription because its using a different path and the price feeding provides more instantaneous data using ticker pricing anyways
	// if err := p.subscribeTickers(cps...); err != nil {
	// 	return err
	// }

	// return p.subscribeCandles(cps...)
}

// subscribeTickers subscribe all currency pairs into ticker channel.
func (p *OkxProvider) subscribeTickers(cps ...types.CurrencyPair) error {
	topics := make([]OkxSubscriptionTopic, len(cps))

	for i, cp := range cps {
		topics[i] = newOkxTickerSubscriptionTopic(currencyPairToOkxPair(cp))
	}

	return p.subscribePairs(topics...)
}

// CONTEXT: commented out because okx candles are currently unused
// // subscribeCandles subscribe all currency pairs into candle channel.
// func (p *OkxProvider) subscribeCandles(cps ...types.CurrencyPair) error {
// 	topics := make([]OkxSubscriptionTopic, len(cps))

// 	for i, cp := range cps {
// 		topics[i] = newOkxCandleSubscriptionTopic(currencyPairToOkxPair(cp))
// 	}

// 	return p.subscribePairs(topics...)
// }

// subscribedPairsToSlice returns the map of subscribed pairs as slice
func (p *OkxProvider) subscribedPairsToSlice() []types.CurrencyPair {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return types.MapPairsToSlice(p.subscribedPairs)
}

func (p *OkxProvider) getTickerPrice(cp types.CurrencyPair) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	instrumentID := currencyPairToOkxPair(cp)
	tickerPair, ok := p.tickers[instrumentID]
	if !ok {
		return TickerPrice{}, fmt.Errorf("okx provider failed to get ticker price for %s", instrumentID)
	}

	return tickerPair.toTickerPrice()
}

func (p *OkxProvider) getCandlePrices(cp types.CurrencyPair) ([]CandlePrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	instrumentID := currencyPairToOkxPair(cp)
	candles, ok := p.candles[instrumentID]
	if !ok {
		return []CandlePrice{}, fmt.Errorf("failed to get candle prices for %s", instrumentID)
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

func (p *OkxProvider) handleReceivedTickers(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(defaultReadNewWSMessage):
			messageType, bz, err := p.wsClient.ReadMessage()
			if err != nil {
				// if some error occurs continue to try to read the next message.
				p.logger.Err(err).Msg("could not read message")
				if err := p.ping(); err != nil {
					p.logger.Err(err).Msg("could not send ping")
				}
				continue
			}

			if len(bz) == 0 {
				continue
			}

			p.resetReconnectTimer()
			p.messageReceived(messageType, bz)

		case <-p.reconnectTimer.C: // reset by the pongHandler.
			if err := p.reconnect(); err != nil {
				p.logger.Err(err).Msg("error reconnecting")
			}
		}
	}
}

func (p *OkxProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		tickerResp OkxTickerResponse
		tickerErr  error
		candleResp OkxCandleResponse
		candleErr  error
	)

	// sometimes the message received is not a ticker or a candle response.
	tickerErr = json.Unmarshal(bz, &tickerResp)
	if tickerResp.ID.Channel == "tickers" {
		for _, tickerPair := range tickerResp.Data {
			p.setTickerPair(tickerPair)
			metrics.SafeTelemetryIncrCounter(
				1,
				"websocket",
				"message",
				"type",
				"ticker",
				"provider",
				config.ProviderOkx,
			)
		}
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if candleResp.ID.Channel == "candle1m" {
		for _, candlePair := range candleResp.Data {
			p.setCandlePair(candlePair, candleResp.ID.InstID)
			metrics.SafeTelemetryIncrCounter(
				1,
				"websocket",
				"message",
				"type",
				"candle",
				"provider",
				config.ProviderOkx,
			)
		}
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		Msg("Error on receive message")
}

func (p *OkxProvider) setTickerPair(tickerPair OkxTickerPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.tickers[tickerPair.InstID] = tickerPair
}

// subscribePairs write the subscription msg to the provider.
func (p *OkxProvider) subscribePairs(pairs ...OkxSubscriptionTopic) error {
	subsMsg := newOkxSubscriptionMsg(pairs...)
	return p.wsClient.WriteJSON(subsMsg)
}

func (p *OkxProvider) setCandlePair(pairData []string, instID string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	ts, err := strconv.ParseInt(pairData[0], 10, 64)
	if err != nil {
		return
	}
	// the candlesticks channel uses an array of strings.
	candle := OkxCandlePair{
		Close:     pairData[4],
		InstID:    instID,
		Volume:    pairData[5],
		TimeStamp: ts,
	}
	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []OkxCandlePair{}

	candleList = append(candleList, candle)
	for _, c := range p.candles[instID] {
		if staleTime < c.TimeStamp {
			candleList = append(candleList, c)
		}
	}
	p.candles[instID] = candleList
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *OkxProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

func (p *OkxProvider) resetReconnectTimer() {
	p.reconnectTimer.Reset(okxPingCheck)
}

// reconnect closes the last WS connection and creates a new one. If there’s a
// network problem, the system will automatically disable the connection. The
// connection will break automatically if the subscription is not established or
// data has not been pushed for more than 30 seconds. To keep the connection stable:
// 1. Set a timer of N seconds whenever a response message is received, where N is
// less than 30.
// 2. If the timer is triggered, which means that no new message is received within
// N seconds, send the String 'ping'.
// 3. Expect a 'pong' as a response. If the response message is not received within
// N seconds, please raise an error or reconnect.
func (p *OkxProvider) reconnect() error {
	p.wsClient.Close()

	p.logger.Debug().Msg("reconnecting websocket")
	wsConn, response, err := websocket.DefaultDialer.Dial(p.wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("error reconnecting to Okx websocket: %w", err)
	}
	wsConn.SetPongHandler(p.pongHandler)
	p.wsClient = wsConn

	currencyPairs := p.subscribedPairsToSlice()

	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"reconnect",
		"provider",
		config.ProviderOkx,
	)
	return p.subscribeChannels(currencyPairs...)
}

// ping to check websocket connection.
func (p *OkxProvider) ping() error {
	return p.wsClient.WriteMessage(websocket.PingMessage, ping)
}

func (p *OkxProvider) pongHandler(_ string) error {
	p.resetReconnectTimer()
	return nil
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p *OkxProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + okxRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary struct {
		Data []OkxInstID `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary.Data))
	for _, pair := range pairsSummary.Data {
		splitInstID := strings.Split(pair.InstID, "-")
		if len(splitInstID) != 2 {
			continue
		}

		cp := types.CurrencyPair{
			Base:  strings.ToUpper(splitInstID[0]),
			Quote: strings.ToUpper(splitInstID[1]),
		}
		availablePairs[cp.String()] = struct{}{}
	}

	return availablePairs, nil
}

func (ticker OkxTickerPair) toTickerPrice() (TickerPrice, error) {
	return newTickerPrice("Okx", ticker.InstID, ticker.Last, ticker.Vol24h)
}

func (candle OkxCandlePair) toCandlePrice() (CandlePrice, error) {
	return newCandlePrice("Okx", candle.InstID, candle.Close, candle.Volume, candle.TimeStamp)
}

// currencyPairToOkxPair returns the expected pair instrument ID for Okx
// ex.: "BTC-USDT".
func currencyPairToOkxPair(pair types.CurrencyPair) string {
	return pair.Base + "-" + pair.Quote
}

// newOkxTickerSubscriptionTopic returns a new subscription topic.
func newOkxTickerSubscriptionTopic(instID string) OkxSubscriptionTopic {
	return OkxSubscriptionTopic{
		Channel: "tickers",
		InstID:  instID,
	}
}

// CONTEXT: commented out because okx candles are unused
// // newOkxSubscriptionTopic returns a new subscription topic.
// func newOkxCandleSubscriptionTopic(instID string) OkxSubscriptionTopic {
// 	return OkxSubscriptionTopic{
// 		Channel: "candle1m",
// 		InstID:  instID,
// 	}
// }

// newOkxSubscriptionMsg returns a new subscription Msg for Okx.
func newOkxSubscriptionMsg(args ...OkxSubscriptionTopic) OkxSubscriptionMsg {
	return OkxSubscriptionMsg{
		Op:   "subscribe",
		Args: args,
	}
}

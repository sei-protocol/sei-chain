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
	gateWSHost    = "api.gateio.ws"
	gateWSPath    = "/ws/v4/"
	gatePingCheck = time.Second * 28 // should be < 30
	gateRestHost  = "https://api.gateio.ws"
	gateRestPath  = "/api/v4/spot/currency_pairs"
)

var _ Provider = (*GateProvider)(nil)

type (
	// GateProvider defines an Oracle provider implemented by the Gate public
	// API.
	//
	// REF: https://www.gate.io/docs/websocket/index.html
	GateProvider struct {
		wsURL           url.URL
		wsClient        *websocket.Conn
		logger          zerolog.Logger
		reconnectTimer  *time.Ticker
		mtx             sync.RWMutex
		endpoints       config.ProviderEndpoint
		tickers         map[string]GateTicker         // Symbol => GateTicker
		candles         map[string][]GateCandle       // Symbol => GateCandle
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	GateTicker struct {
		Last   string `json:"last"`       // Last traded price ex.: 43508.9
		Vol    string `json:"baseVolume"` // Trading volume ex.: 11159.87127845
		Symbol string `json:"symbol"`     // Symbol ex.: ATOM_UDST
	}

	GateCandle struct {
		Close     string // Closing price
		TimeStamp int64  // Unix timestamp
		Volume    string // Total candle volume
		Symbol    string // Total symbol
	}

	// GateTickerSubscriptionMsg Msg to subscribe all the tickers channels.
	GateTickerSubscriptionMsg struct {
		Time int64 `json:"time"`
		// Method string   `json:"method"` // ticker.subscribe
		Channel string `json:"channel"` // spot.tickers
		Event   string `json:"event"`   // subscribe
		// Params  []string `json:"params"`  // streams to subscribe ex.: BOT_USDT
		Payload []string `json:"payload"` // streams to subscribe ex.: BOT_USDT
		ID      uint16   `json:"id"`      // identify messages going back and forth
	}

	// GateCandleSubscriptionMsg Msg to subscribe to a candle channel.
	GateCandleSubscriptionMsg struct {
		Time    int64  `json:"time"`
		Channel string `json:"channel"` // spot.candlesticks
		Event   string `json:"event"`   // subscribe
		// Params  []string `json:"params"`  // streams to subscribe ex.: BOT_USDT
		Payload []string `json:"payload"` // streams to subscribe ex.: BOT_USDT
		ID      uint16   `json:"id"`      // identify messages going back and forth
	}

	// GateTickerResponse defines the response body for gate tickers.
	GateTickerResponse struct {
		Time    int64            `json:"time"`
		TimeMS  int64            `json:"time_ms"`
		Channel string           `json:"channel"`
		Event   string           `json:"event"`
		Result  GateTickerResult `json:"result"`
	}

	// GateTickerResult defines the response body for gate tickers result data.
	GateTickerResult struct {
		CurrencyPair     string `json:"currency_pair"`
		Last             string `json:"last"`
		LowestAsk        string `json:"lowest_ask"`
		HighestBid       string `json:"highest_bid"`
		ChangePercentage string `json:"change_percentage"`
		BaseVolume       string `json:"base_volume"`
		QuoteVolume      string `json:"quote_volume"`
		High24h          string `json:"high_24h"`
		Low24h           string `json:"low_24h"`
	}

	// GateTickerResponse defines the response body for gate tickers.
	// The Params response is a 2D slice of multiple candles and their data.
	//
	// REF: https://www.gate.io/docs/websocket/index.html
	GateCandleResponse struct {
		Time    int64            `json:"time"`
		TimeMS  int64            `json:"time_ms"`
		Channel string           `json:"channel"`
		Event   string           `json:"event"`
		Result  GateCandleResult `json:"result"`
	}

	// GateCandleResult defines the response body for gate candle result data.
	GateCandleResult struct {
		Timestamp         string `json:"t"`
		Volume            string `json:"v"`
		ClosePrice        string `json:"c"`
		HighestPrice      string `json:"h"`
		LowestPrice       string `json:"l"`
		OpenPrice         string `json:"o"`
		SubscriptionName  string `json:"n"`
		BaseTradingAmount string `json:"a"`
	}

	// GateEvent defines the response body for gate subscription statuses.
	GateEvent struct {
		ID     int             `json:"id"`     // subscription id, ex.: 123
		Result GateEventResult `json:"result"` // event result body
	}
	// GateEventResult defines the Result body for the GateEvent response.
	GateEventResult struct {
		Status string `json:"status"` // ex. "successful"
	}

	// GatePairSummary defines the response structure for a Gate pair summary.
	GatePairSummary struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

// NewGateProvider creates a new GateProvider.
func NewGateProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*GateProvider, error) {
	if endpoints.Name != config.ProviderGate {
		endpoints = config.ProviderEndpoint{
			Name:      config.ProviderGate,
			Rest:      gateRestHost,
			Websocket: gateWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   gateWSPath,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Gate websocket: %w", err)
	}

	provider := &GateProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "gate").Logger(),
		reconnectTimer:  time.NewTicker(gatePingCheck),
		endpoints:       endpoints,
		tickers:         map[string]GateTicker{},
		candles:         map[string][]GateCandle{},
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
func (p *GateProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
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
func (p *GateProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	candlePrices := make(map[string][]CandlePrice, len(pairs))
	for _, currencyPair := range pairs {
		gp := currencyPairToGatePair(currencyPair)
		price, err := p.getCandlePrices(gp)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch candles for pair ", currencyPair))
			continue
		}

		candlePrices[currencyPair.String()] = price
	}

	return candlePrices, nil
}

func (p *GateProvider) getCandlePrices(key string) ([]CandlePrice, error) {
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

// SubscribeCurrencyPairs subscribe to ticker and candle channels for all pairs.
func (p *GateProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	if len(cps) == 0 {
		return fmt.Errorf("currency pairs is empty")
	}

	if err := p.subscribeTickers(cps...); err != nil {
		return err
	}
	if err := p.subscribeCandles(cps...); err != nil {
		return err
	}
	p.setSubscribedPairs(cps...)
	metrics.SafeTelemetryIncrCounter(
		float32(len(cps)),
		"websocket",
		"subscribe",
		"currency_pairs",
		"provider",
		config.ProviderGate,
	)
	return nil
}

// subscribeTickers subscribes to the ticker channels for all pairs at once.
func (p *GateProvider) subscribeTickers(cps ...types.CurrencyPair) error {
	topics := []string{}

	for _, cp := range cps {
		topics = append(topics, currencyPairToGatePair(cp))
	}

	tickerMsg := newGateTickerSubscription(topics...)
	return p.subscribeTickerPairs(tickerMsg)
}

// subscribeCandles subscribes to the candle channels for all pairs one-by-one.
// The gate API currently only supports subscribing to one kline market at a time.
//
// REF: https://www.gate.io/docs/websocket/index.html
func (p *GateProvider) subscribeCandles(cps ...types.CurrencyPair) error {
	gatePairs := make([]string, len(cps))

	iterator := 0
	for _, cp := range cps {
		gatePairs[iterator] = currencyPairToGatePair(cp)
		iterator++
	}

	for _, pair := range gatePairs {
		msg := newGateCandleSubscription(pair)
		if err := p.subscribeCandlePair(msg); err != nil {
			return err
		}
	}

	return nil
}

func (p *GateProvider) subscribedPairsToSlice() []types.CurrencyPair {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return types.MapPairsToSlice(p.subscribedPairs)
}

func (p *GateProvider) getTickerPrice(cp types.CurrencyPair) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	gp := currencyPairToGatePair(cp)
	if tickerPair, ok := p.tickers[gp]; ok {
		return tickerPair.toTickerPrice()
	}

	return TickerPrice{}, fmt.Errorf("gate provider failed to get ticker price for %s", gp)
}

func (p *GateProvider) handleReceivedTickers(ctx context.Context) {
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

func (p *GateProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		gateEvent GateEvent
		gateErr   error
		tickerErr error
		candleErr error
	)

	gateErr = json.Unmarshal(bz, &gateEvent)
	if gateErr == nil {
		switch gateEvent.Result.Status {
		case "success":
			return
		case "":
			break
		default:
			err := p.reconnect()
			if err != nil {
				p.logger.Error().
					AnErr("ticker", tickerErr).
					AnErr("candle", candleErr).
					AnErr("event", err).
					Msg("Error on reconnecting")
			}
			return
		}
	}

	tickerErr = p.messageReceivedTickerPrice(bz)
	if tickerErr == nil {
		return
	}

	candleErr = p.messageReceivedCandle(bz)
	if candleErr == nil {
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		AnErr("event", gateErr).
		Msg("Error on receive message")
}

// messageReceivedTickerPrice handles the ticker price msg.
// The provider response is a slice with different types at each index.
//
// REF: https://www.gate.io/docs/websocket/index.html
func (p *GateProvider) messageReceivedTickerPrice(bz []byte) error {
	var tickerMessage GateTickerResponse
	if err := json.Unmarshal(bz, &tickerMessage); err != nil {
		return err
	}

	if tickerMessage.Channel != "spot.tickers" || tickerMessage.Event != "update" {
		return fmt.Errorf("message is not a ticker update")
	}

	gateTicker := GateTicker{
		Last:   tickerMessage.Result.Last,
		Vol:    tickerMessage.Result.BaseVolume,
		Symbol: tickerMessage.Result.CurrencyPair,
	}

	p.setTickerPair(gateTicker)
	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"message",
		"type",
		"ticker",
		"provider",
		config.ProviderGate,
	)
	return nil
}

// UnmarshalParams is a helper function which unmarshals the 2d slice of interfaces
// from a GateCandleResponse into the GateCandle.
func (candle *GateCandle) UnmarshalParams(params [][]interface{}) error {
	var tmp []interface{}

	if len(params) == 0 {
		return fmt.Errorf("no candles in response")
	}

	// use the most recent candle
	tmp = params[len(params)-1]
	if len(tmp) != 8 {
		return fmt.Errorf("wrong number of fields in candle")
	}

	time := int64(tmp[0].(float64))
	if time == 0 {
		return fmt.Errorf("time field must be a float")
	}
	candle.TimeStamp = time

	closeStr, ok := tmp[1].(string)
	if !ok {
		return fmt.Errorf("close field must be a string")
	}
	candle.Close = closeStr

	volume, ok := tmp[5].(string)
	if !ok {
		return fmt.Errorf("volume field must be a string")
	}
	candle.Volume = volume

	symbol, ok := tmp[7].(string)
	if !ok {
		return fmt.Errorf("symbol field must be a string")
	}
	candle.Symbol = symbol

	return nil
}

// messageReceivedCandle handles the candle price msg.
// The provider response is a slice with different types at each index.
//
// REF: https://www.gate.io/docs/websocket/index.html
func (p *GateProvider) messageReceivedCandle(bz []byte) error {
	var candleMessage GateCandleResponse
	if err := json.Unmarshal(bz, &candleMessage); err != nil {
		return err
	}
	if candleMessage.Channel != "spot.candlesticks" || candleMessage.Event != "update" {
		return fmt.Errorf("message is not a candle update")
	}

	timeStamp, err := strconv.ParseInt(candleMessage.Result.Timestamp, 10, 64)
	if err != nil {
		return err
	}

	symbolArr := strings.SplitN(candleMessage.Result.SubscriptionName, "_", 2)

	gateCandle := GateCandle{
		Close:     candleMessage.Result.ClosePrice,
		TimeStamp: timeStamp,
		Volume:    candleMessage.Result.BaseTradingAmount,
		Symbol:    symbolArr[1],
	}

	p.setCandlePair(gateCandle)
	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"message",
		"type",
		"candle",
		"provider",
		config.ProviderGate,
	)
	return nil
}

func (p *GateProvider) setTickerPair(ticker GateTicker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.tickers[ticker.Symbol] = ticker
}

func (p *GateProvider) setCandlePair(candle GateCandle) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	// convert gate timestamp seconds -> milliseconds
	candle.TimeStamp *= int64(time.Second / time.Millisecond)
	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []GateCandle{}

	candleList = append(candleList, candle)
	for _, c := range p.candles[candle.Symbol] {
		if staleTime < c.TimeStamp {
			candleList = append(candleList, c)
		}
	}
	p.candles[candle.Symbol] = candleList
}

// subscribeTickerPairs write the subscription msg to the provider.
func (p *GateProvider) subscribeTickerPairs(msg GateTickerSubscriptionMsg) error {
	return p.wsClient.WriteJSON(msg)
}

// subscribeCandlePair write the subscription msg to the provider.
func (p *GateProvider) subscribeCandlePair(msg GateCandleSubscriptionMsg) error {
	return p.wsClient.WriteJSON(msg)
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *GateProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

func (p *GateProvider) resetReconnectTimer() {
	p.reconnectTimer.Reset(gatePingCheck)
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
func (p *GateProvider) reconnect() error {
	p.wsClient.Close()

	p.logger.Debug().Msg("reconnecting websocket")
	wsConn, response, err := websocket.DefaultDialer.Dial(p.wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("error reconnecting to Gate websocket: %w", err)
	}
	wsConn.SetPongHandler(p.pongHandler)
	p.wsClient = wsConn

	currencyPairs := p.subscribedPairsToSlice()

	metrics.SafeTelemetryIncrCounter(
		1,
		"websocket",
		"reconnect",
		"provider",
		config.ProviderGate,
	)
	return p.SubscribeCurrencyPairs(currencyPairs...)
}

// ping to check websocket connection.
func (p *GateProvider) ping() error {
	return p.wsClient.WriteMessage(websocket.PingMessage, ping)
}

func (p *GateProvider) pongHandler(_ string) error {
	p.resetReconnectTimer()
	return nil
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
func (p *GateProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + gateRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []GatePairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pair := range pairsSummary {
		cp := types.CurrencyPair{
			Base:  strings.ToUpper(pair.Base),
			Quote: strings.ToUpper(pair.Quote),
		}
		availablePairs[cp.String()] = struct{}{}
	}

	return availablePairs, nil
}

func (ticker GateTicker) toTickerPrice() (TickerPrice, error) {
	return newTickerPrice("Gate", ticker.Symbol, ticker.Last, ticker.Vol)
}

func (candle GateCandle) toCandlePrice() (CandlePrice, error) {
	return newCandlePrice(
		"Gate",
		candle.Symbol,
		candle.Close,
		candle.Volume,
		candle.TimeStamp,
	)
}

// currencyPairToGatePair returns the expected pair for Gate
// ex.: "ATOM_USDT".
func currencyPairToGatePair(pair types.CurrencyPair) string {
	return pair.Base + "_" + pair.Quote
}

// newGateTickerSubscription returns a new subscription topic for tickers.
func newGateTickerSubscription(cp ...string) GateTickerSubscriptionMsg {
	timeSecs := time.Now().Unix()
	return GateTickerSubscriptionMsg{
		Time:    timeSecs,
		Channel: "spot.tickers",
		Event:   "subscribe",
		Payload: cp,
		ID:      1,
	}
}

// newGateCandleSubscription returns a new subscription topic for candles.
func newGateCandleSubscription(gatePair string) GateCandleSubscriptionMsg {
	pair := []string{"1m", gatePair}
	timeSecs := time.Now().Unix()
	return GateCandleSubscriptionMsg{
		Time:    timeSecs,
		Channel: "spot.candlesticks",
		Event:   "subscribe",
		Payload: pair,
		ID:      2,
	}
}

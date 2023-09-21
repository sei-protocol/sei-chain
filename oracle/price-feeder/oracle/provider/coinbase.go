package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	coinbaseWSHost    = "ws-feed.exchange.coinbase.com"
	coinbasePingCheck = time.Second * 28 // should be < 30
	coinbaseRestHost  = "https://api.exchange.coinbase.com"
	coinbaseRestPath  = "/products"
	timeLayout        = "2006-01-02T15:04:05.000000Z"
	unixMinute        = 60000
)

var _ Provider = (*CoinbaseProvider)(nil)

type (
	// CoinbaseProvider defines an Oracle provider implemented by the Coinbase public
	// API.
	//
	// REF: https://www.coinbase.io/docs/websocket/index.html
	CoinbaseProvider struct {
		wsURL           url.URL
		wsClient        *websocket.Conn
		logger          zerolog.Logger
		reconnectTimer  *time.Ticker
		mtx             sync.RWMutex
		endpoints       config.ProviderEndpoint
		trades          map[string][]CoinbaseTrade    // Symbol => []CoinbaseTrade
		tickers         map[string]CoinbaseTicker     // Symbol => CoinbaseTicker
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	// CoinbaseSubscriptionMsg Msg to subscribe to all channels.
	CoinbaseSubscriptionMsg struct {
		Type       string   `json:"type"`        // ex. "subscribe"
		ProductIDs []string `json:"product_ids"` // streams to subscribe ex.: ["BOT-USDT", ...]
		Channels   []string `json:"channels"`    // channels to subscribe to ex.: "ticker"
	}

	// CoinbaseMatchResponse defines the response body for coinbase trades.
	CoinbaseTradeResponse struct {
		Type      string `json:"type"`       // "last_match" or "match"
		ProductID string `json:"product_id"` // ex.: ATOM-USDT
		Time      string `json:"time"`       // Time in format 2006-01-02T15:04:05.000000Z
		Size      string `json:"size"`       // Size of the trade ex.: 10.41
		Price     string `json:"price"`      // ex.: 14.02
	}

	// CoinbaseTrade defines the trade info we'd like to save.
	CoinbaseTrade struct {
		ProductID string // ex.: ATOM-USDT
		Time      int64  // Time in unix epoch ex.: 164732388700
		Size      string // Size of the trade ex.: 10.41
		Price     string // ex.: 14.02
	}

	// CoinbaseTicker defines the ticker info we'd like to save.
	CoinbaseTicker struct {
		ProductID string `json:"product_id"` // ex.: ATOM-USDT
		Price     string `json:"price"`      // ex.: 523.0
		Volume    string `json:"volume_24h"` // 24-hour volume
	}

	// CoinbaseErrResponse defines the response body for errors.
	CoinbaseErrResponse struct {
		Type   string `json:"type"`   // should be "error"
		Reason string `json:"reason"` // ex.: "tickers" is not a valid channel
	}

	// CoinbasePairSummary defines the response structure for a Coinbase pair summary.
	CoinbasePairSummary struct {
		Base  string `json:"base_currency"`
		Quote string `json:"quote_currency"`
	}
)

// NewCoinbaseProvider creates a new CoinbaseProvider.
func NewCoinbaseProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*CoinbaseProvider, error) {
	if endpoints.Name != config.ProviderCoinbase {
		endpoints = config.ProviderEndpoint{
			Name:      config.ProviderCoinbase,
			Rest:      coinbaseRestHost,
			Websocket: coinbaseWSHost,
		}
	}
	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Coinbase websocket: %w", err)
	}

	provider := &CoinbaseProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "coinbase").Logger(),
		reconnectTimer:  time.NewTicker(coinbasePingCheck),
		endpoints:       endpoints,
		trades:          map[string][]CoinbaseTrade{},
		tickers:         map[string]CoinbaseTicker{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}
	provider.wsClient.SetPongHandler(provider.pongHandler)

	if err := provider.SubscribeCurrencyPairs(pairs...); err != nil {
		return nil, err
	}

	go provider.handleReceivedMessages(ctx)

	return provider, nil
}

// GetTickerPrices returns the tickerPrices based on the saved map.
func (p *CoinbaseProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
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

// GetCandlePrices returns candles based off of the saved trades map.
// Candles need to be cut up into one-minute intervals.
func (p *CoinbaseProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	tradeMap := make(map[string][]CoinbaseTrade, len(pairs))

	for _, cp := range pairs {
		key := currencyPairToCoinbasePair(cp)
		tradeSet, err := p.getTradePrices(key)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch candles for pair ", cp))
			continue
		}
		tradeMap[key] = tradeSet
	}
	if len(tradeMap) == 0 {
		return nil, fmt.Errorf("no trades have been received")
	}

	candles := make(map[string][]CandlePrice)

	for cp := range tradeMap {
		trades := tradeMap[cp]
		// sort oldest -> newest
		sort.Slice(trades, func(i, j int) bool {
			return time.Unix(trades[i].Time, 0).Before(time.Unix(trades[j].Time, 0))
		})

		candleSlice := []CandlePrice{
			{
				Price:  sdk.ZeroDec(),
				Volume: sdk.ZeroDec(),
			},
		}
		startTime := trades[0].Time
		index := 0

		// divide into chunks by minute
		for _, trade := range trades {
			// every minute, reset the time period
			if trade.Time-startTime > unixMinute {
				index++
				startTime = trade.Time
				candleSlice = append(candleSlice, CandlePrice{
					Price:  sdk.ZeroDec(),
					Volume: sdk.ZeroDec(),
				})
			}

			size, err := sdk.NewDecFromStr(trade.Size)
			if err != nil {
				return nil, err
			}
			price, err := sdk.NewDecFromStr(trade.Price)
			if err != nil {
				return nil, err
			}

			volume := candleSlice[index].Volume.Add(size)
			candleSlice[index] = CandlePrice{
				Volume:    volume,     // aggregate size
				Price:     price,      // most recent price
				TimeStamp: trade.Time, // most recent timestamp
			}
		}

		candles[coinbasePairToCurrencyPair(cp)] = candleSlice
	}

	return candles, nil
}

// SubscribeCurrencyPairs subscribes to websockets for all currency pairs.
func (p *CoinbaseProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	if len(cps) == 0 {
		return fmt.Errorf("currency pairs is empty")
	}

	if err := p.subscribe(cps...); err != nil {
		return err
	}

	p.setSubscribedPairs(cps...)
	telemetry.IncrCounter(
		float32(len(cps)),
		"websocket",
		"subscribe",
		"currency_pairs",
		"provider",
		config.ProviderCoinbase,
	)
	return nil
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
func (p *CoinbaseProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + coinbaseRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []CoinbasePairSummary
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

// subscribe subscribes to the coinbase "ticker" and "match" websockets.
func (p *CoinbaseProvider) subscribe(cps ...types.CurrencyPair) error {
	topics := make([]string, len(cps))
	index := 0

	for _, cp := range cps {
		topics[index] = currencyPairToCoinbasePair(cp)
		index++
	}

	tickerMsg := newCoinbaseSubscription(topics...)
	return p.subscribePairs(tickerMsg)
}

// subscribedPairsToSlice returns the map of subscribed pairs as a slice.
func (p *CoinbaseProvider) subscribedPairsToSlice() []types.CurrencyPair {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return types.MapPairsToSlice(p.subscribedPairs)
}

func (p *CoinbaseProvider) getTickerPrice(cp types.CurrencyPair) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	gp := currencyPairToCoinbasePair(cp)
	if tickerPair, ok := p.tickers[gp]; ok {
		return tickerPair.toTickerPrice()
	}

	return TickerPrice{}, fmt.Errorf("failed to get ticker price for %s", gp)
}

func (p *CoinbaseProvider) getTradePrices(key string) ([]CoinbaseTrade, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	trades, ok := p.trades[key]
	if !ok {
		return []CoinbaseTrade{}, fmt.Errorf("failed to get trades for %s", key)
	}

	return trades, nil
}

func (p *CoinbaseProvider) handleReceivedMessages(ctx context.Context) {
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

func (p *CoinbaseProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var coinbaseTrade CoinbaseTradeResponse
	if err := json.Unmarshal(bz, &coinbaseTrade); err != nil {
		p.logger.Error().Err(err).Msg("unable to unmarshal response")
		return
	}

	if coinbaseTrade.Type == "error" {
		var coinbaseErr CoinbaseErrResponse
		if err := json.Unmarshal(bz, &coinbaseErr); err != nil {
			p.logger.Debug().Err(err).Msg("unable to unmarshal error response")
		}
		p.logger.Error().Msg(coinbaseErr.Reason)
		return
	}

	if coinbaseTrade.Type == "subscriptions" { // successful subscription message
		return
	}

	if coinbaseTrade.Type == "ticker" {
		var coinbaseTicker CoinbaseTicker
		if err := json.Unmarshal(bz, &coinbaseTicker); err != nil {
			p.logger.Error().Err(err).Msg("unable to unmarshal response")
			return
		}

		p.setTickerPair(coinbaseTicker)
		telemetry.IncrCounter(
			1,
			"websocket",
			"message",
			"type",
			"ticker",
			"provider",
			config.ProviderCoinbase,
		)
		return
	}
	telemetry.IncrCounter(
		1,
		"websocket",
		"message",
		"type",
		"trade",
		"provider",
		config.ProviderCoinbase,
	)
	p.setTradePair(coinbaseTrade)
}

// timeToUnix converts a Time in format "2006-01-02T15:04:05.000000Z" to unix
func (tr CoinbaseTradeResponse) timeToUnix() int64 {
	t, err := time.Parse(timeLayout, tr.Time)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func (tr CoinbaseTradeResponse) toTrade() CoinbaseTrade {
	return CoinbaseTrade{
		Time:      tr.timeToUnix(),
		Price:     tr.Price,
		ProductID: tr.ProductID,
		Size:      tr.Size,
	}
}

func (p *CoinbaseProvider) setTickerPair(ticker CoinbaseTicker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.tickers[ticker.ProductID] = ticker
}

// setTradePair takes a CoinbaseTradeResponse, converts its date into unix epoch,
// and then will add it to a copy of the trade slice. Then it filters out any
// "stale" trades, and sets the trade slice in memory to the copy.
func (p *CoinbaseProvider) setTradePair(tradeResponse CoinbaseTradeResponse) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	staleTime := PastUnixTime(providerCandlePeriod)
	tradeList := []CoinbaseTrade{
		tradeResponse.toTrade(),
	}

	for _, t := range p.trades[tradeResponse.ProductID] {
		if staleTime < t.Time {
			tradeList = append(tradeList, t)
		}
	}
	p.trades[tradeResponse.ProductID] = tradeList
}

// subscribePairs write the subscription msg to the provider.
func (p *CoinbaseProvider) subscribePairs(msg CoinbaseSubscriptionMsg) error {
	return p.wsClient.WriteJSON(msg)
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *CoinbaseProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

func (p *CoinbaseProvider) resetReconnectTimer() {
	p.reconnectTimer.Reset(coinbasePingCheck)
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
func (p *CoinbaseProvider) reconnect() error {
	p.wsClient.Close()

	p.logger.Debug().Msg("reconnecting websocket")
	wsConn, response, err := websocket.DefaultDialer.Dial(p.wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("error reconnecting to Coinbase websocket: %w", err)
	}
	wsConn.SetPongHandler(p.pongHandler)
	p.wsClient = wsConn

	currencyPairs := p.subscribedPairsToSlice()

	telemetry.IncrCounter(
		1,
		"websocket",
		"reconnect",
		"provider",
		config.ProviderCoinbase,
	)
	return p.SubscribeCurrencyPairs(currencyPairs...)
}

// ping to check websocket connection.
func (p *CoinbaseProvider) ping() error {
	return p.wsClient.WriteMessage(websocket.PingMessage, ping)
}

func (p *CoinbaseProvider) pongHandler(_ string) error {
	p.resetReconnectTimer()
	return nil
}

func (ticker CoinbaseTicker) toTickerPrice() (TickerPrice, error) {
	return newTickerPrice(
		"Coinbase",
		coinbasePairToCurrencyPair(ticker.ProductID),
		ticker.Price,
		ticker.Volume,
	)
}

// currencyPairToCoinbasePair returns the expected pair for Coinbase
// ex.: "ATOM-USDT".
func currencyPairToCoinbasePair(pair types.CurrencyPair) string {
	return pair.Base + "-" + pair.Quote
}

// coinbasePairToCurrencyPair returns the currency pair string
// ex.: "ATOMUSDT".
func coinbasePairToCurrencyPair(coinbasePair string) string {
	return strings.ReplaceAll(coinbasePair, "-", "")
}

// newCoinbaseSubscription returns a new subscription topic for matches/tickers.
func newCoinbaseSubscription(cp ...string) CoinbaseSubscriptionMsg {
	return CoinbaseSubscriptionMsg{
		Type:       "subscribe",
		ProductIDs: cp,
		Channels:   []string{"matches", "ticker"},
	}
}

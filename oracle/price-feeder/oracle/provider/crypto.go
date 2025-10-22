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

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
)

const (
	cryptoWSHost             = "stream.crypto.com"
	cryptoWSPath             = "/v2/market"
	cryptoRestHost           = "https://api.crypto.com"
	cryptoRestPath           = "/v2/public/get-ticker"
	cryptoTickerChannel      = "ticker"
	cryptoCandleChannel      = "candlestick"
	cryptoHeartbeatMethod    = "public/heartbeat"
	cryptoHeartbeatReqMethod = "public/respond-heartbeat"
	cryptoTickerMsgPrefix    = "ticker."
	cryptoCandleMsgPrefix    = "candlestick.5m."
)

var _ Provider = (*CryptoProvider)(nil)

type (
	// CryptoProvider defines an Oracle provider implemented by the Crypto.com public
	// API.
	//
	// REF: https://exchange-docs.crypto.com/spot/index.html#introduction
	CryptoProvider struct {
		wsc             *WebsocketController
		logger          zerolog.Logger
		mtx             sync.RWMutex
		endpoint        config.ProviderEndpoint
		tickers         map[string]TickerPrice        // Symbol => TickerPrice
		candles         map[string][]CandlePrice      // Symbol => CandlePrice
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	CryptoTickerResponse struct {
		Result CryptoTickerResult `json:"result"`
	}
	CryptoTickerResult struct {
		InstrumentName string         `json:"instrument_name"` // ex.: ATOM_USDT
		Channel        string         `json:"channel"`         // ex.: ticker
		Data           []CryptoTicker `json:"data"`            // ticker data
	}
	CryptoTicker struct {
		InstrumentName string `json:"i"` // Instrument Name, e.g. BTC_USDT, ETH_CRO, etc.
		Volume         string `json:"v"` // The total 24h traded volume
		LatestTrade    string `json:"a"` // The price of the latest trade, null if there weren't any trades
	}

	CryptoCandleResponse struct {
		Result CryptoCandleResult `json:"result"`
	}
	CryptoCandleResult struct {
		InstrumentName string         `json:"instrument_name"` // ex.: ATOM_USDT
		Channel        string         `json:"channel"`         // ex.: candlestick
		Data           []CryptoCandle `json:"data"`            // candlestick data
	}
	CryptoCandle struct {
		Close     string `json:"c"` // Price at close
		Volume    string `json:"v"` // Volume during interval
		Timestamp int64  `json:"t"` // End time of candlestick (Unix timestamp)
	}

	CryptoSubscriptionMsg struct {
		ID     int64                    `json:"id"`
		Method string                   `json:"method"` // subscribe, unsubscribe
		Params CryptoSubscriptionParams `json:"params"`
		Nonce  int64                    `json:"nonce"` // Current timestamp (milliseconds since the Unix epoch)
	}
	CryptoSubscriptionParams struct {
		Channels []string `json:"channels"` // Channels to be subscribed ex. ticker.ATOM_USDT
	}

	CryptoPairsSummary struct {
		Result CryptoInstruments `json:"result"`
	}
	CryptoInstruments struct {
		Data []CryptoTicker `json:"data"`
	}

	CryptoHeartbeatResponse struct {
		ID     int64  `json:"id"`
		Method string `json:"method"` // public/heartbeat
	}
	CryptoHeartbeatRequest struct {
		ID     int64  `json:"id"`
		Method string `json:"method"` // public/respond-heartbeat
	}
)

func NewCryptoProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoint config.ProviderEndpoint,
	pairs ...types.CurrencyPair,
) (*CryptoProvider, error) {
	if endpoint.Name != config.ProviderCrypto {
		endpoint = config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      cryptoRestHost,
			Websocket: cryptoWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoint.Websocket,
		Path:   cryptoWSPath,
	}

	provider := &CryptoProvider{
		logger:          logger.With().Str("provider", "crypto").Logger(),
		endpoint:        endpoint,
		tickers:         map[string]TickerPrice{},
		candles:         map[string][]CandlePrice{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}

	provider.setSubscribedPairs(pairs...)

	provider.wsc = NewWebsocketController(
		ctx,
		config.ProviderCrypto,
		wsURL,
		provider.getSubscriptionMsgs(pairs...),
		provider.messageReceived,
		disabledPingDuration,
		websocket.PingMessage,
		provider.logger,
	)

	go provider.wsc.Start()

	return provider, nil
}

func (p *CryptoProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(cps)*2)
	for _, cp := range cps {
		cryptoPair := currencyPairToCryptoPair(cp)
		channel := cryptoTickerMsgPrefix + cryptoPair
		msg := newCryptoSubscriptionMsg([]string{channel})
		subscriptionMsgs = append(subscriptionMsgs, msg)

		cryptoPair = currencyPairToCryptoPair(cp)
		channel = cryptoCandleMsgPrefix + cryptoPair
		msg = newCryptoSubscriptionMsg([]string{channel})
		subscriptionMsgs = append(subscriptionMsgs, msg)
	}
	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *CryptoProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := p.subscribedPairs[cp.String()]; !ok {
			newPairs = append(newPairs, cp)
		}
	}

	newSubscriptionMsgs := p.getSubscriptionMsgs(newPairs...)
	if err := p.wsc.AddSubscriptionMsgs(newSubscriptionMsgs); err != nil {
		return err
	}

	p.setSubscribedPairs(newPairs...)
	return nil
}

// GetTickerPrices returns the tickerPrices based on the saved map.
func (p *CryptoProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]TickerPrice, error) {
	tickerPrices := make(map[string]TickerPrice, len(pairs))

	for _, cp := range pairs {
		key := currencyPairToCryptoPair(cp)
		price, err := p.getTickerPrice(key)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch tickers for pair ", cp))
			continue
		}
		tickerPrices[cp.String()] = price
	}

	return tickerPrices, nil
}

// GetCandlePrices returns the candlePrices based on the saved map
func (p *CryptoProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]CandlePrice, error) {
	candlePrices := make(map[string][]CandlePrice, len(pairs))

	for _, cp := range pairs {
		key := currencyPairToCryptoPair(cp)
		prices, err := p.getCandlePrices(key)
		if err != nil {
			p.logger.Debug().AnErr("err", err).Msg(fmt.Sprint("failed to fetch candles for pair ", cp))
			continue
		}
		candlePrices[cp.String()] = prices
	}

	return candlePrices, nil
}

func (p *CryptoProvider) getTickerPrice(key string) (TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	ticker, ok := p.tickers[key]
	if !ok {
		return TickerPrice{}, fmt.Errorf("%s ticker not found for %s", config.ProviderCrypto, key)
	}

	return ticker, nil
}

func (p *CryptoProvider) getCandlePrices(key string) ([]CandlePrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	candles, ok := p.candles[key]
	if !ok {
		return []CandlePrice{}, fmt.Errorf("%s candle not found for %s", config.ProviderCrypto, key)
	}

	candleList := []CandlePrice{}
	candleList = append(candleList, candles...)

	return candleList, nil
}

func (p *CryptoProvider) messageReceived(messageType int, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		heartbeatResp CryptoHeartbeatResponse
		heartbeatErr  error

		tickerResp CryptoTickerResponse
		tickerErr  error

		candleResp CryptoCandleResponse
		candleErr  error
	)

	// sometimes the message received is not a ticker or a candle response.
	heartbeatErr = json.Unmarshal(bz, &heartbeatResp)
	if heartbeatResp.Method == cryptoHeartbeatMethod {
		p.pong(heartbeatResp)
		return
	}

	tickerErr = json.Unmarshal(bz, &tickerResp)
	if tickerResp.Result.Channel == cryptoTickerChannel {
		for _, tickerPair := range tickerResp.Result.Data {
			p.setTickerPair(tickerResp.Result.InstrumentName, tickerPair)
			metrics.SafeTelemetryIncrCounter(
				1,
				"websocket",
				"message",
				"type",
				"ticker",
				"provider",
				config.ProviderCrypto,
			)
		}
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if candleResp.Result.Channel == cryptoCandleChannel {
		for _, candlePair := range candleResp.Result.Data {
			p.setCandlePair(candleResp.Result.InstrumentName, candlePair)
			metrics.SafeTelemetryIncrCounter(
				1,
				"websocket",
				"message",
				"type",
				"candle",
				"provider",
				config.ProviderCrypto,
			)
		}
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("heartbeat", heartbeatErr).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		Msg("Error on receive message")
}

// pong return a heartbeat message when a "ping" is received and reset the
// reconnect ticker because the connection is alive. After connected to crypto.com's
// Websocket server, the server will send heartbeat periodically (30s interval).
// When client receives an heartbeat message, it must respond back with the
// public/respond-heartbeat method, using the same matching id,
// within 5 seconds, or the connection will break.
func (p *CryptoProvider) pong(heartbeatResp CryptoHeartbeatResponse) {
	heartbeatReq := CryptoHeartbeatRequest{
		ID:     heartbeatResp.ID,
		Method: cryptoHeartbeatReqMethod,
	}

	if err := p.wsc.SendJSON(heartbeatReq); err != nil {
		p.logger.Err(err).Msg("could not send pong message back")
	}
}

func (p *CryptoProvider) setTickerPair(symbol string, tickerPair CryptoTicker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	tickerPrice, err := newTickerPrice(
		config.ProviderCrypto,
		symbol,
		tickerPair.LatestTrade,
		tickerPair.Volume,
	)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crypto: failed to parse ticker")
		return
	}

	p.tickers[symbol] = tickerPrice
}

func (p *CryptoProvider) setCandlePair(symbol string, candlePair CryptoCandle) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	candle, err := newCandlePrice(
		config.ProviderCrypto,
		symbol,
		candlePair.Close,
		candlePair.Volume,
		candlePair.Timestamp,
	)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crypto: failed to parse candle")
		return
	}

	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []CandlePrice{}
	candleList = append(candleList, candle)

	for _, c := range p.candles[symbol] {
		if staleTime < c.TimeStamp {
			candleList = append(candleList, c)
		}
	}

	p.candles[symbol] = candleList
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *CryptoProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "UMEEUSDC" => {}].
func (p *CryptoProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoint.Rest + cryptoRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary CryptoPairsSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary.Result.Data))
	for _, pair := range pairsSummary.Result.Data {
		splitInstName := strings.Split(pair.InstrumentName, "_")
		if len(splitInstName) != 2 {
			continue
		}

		cp := types.CurrencyPair{
			Base:  strings.ToUpper(splitInstName[0]),
			Quote: strings.ToUpper(splitInstName[1]),
		}

		availablePairs[cp.String()] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToCryptoPair receives a currency pair and return crypto
// ticker symbol atomusdt@ticker.
func currencyPairToCryptoPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + "_" + cp.Quote)
}

// newCryptoSubscriptionMsg returns a new subscription Msg.
func newCryptoSubscriptionMsg(channels []string) CryptoSubscriptionMsg {
	return CryptoSubscriptionMsg{
		ID:     1,
		Method: "subscribe",
		Params: CryptoSubscriptionParams{
			Channels: channels,
		},
		Nonce: time.Now().UnixMilli(),
	}
}

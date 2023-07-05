package provider

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
)

const (
	cryptoRestHost = "https://api.crypto.com"
	cryptoRestPath = "/exchange/v1"
	cryptoWsHost   = "wss://stream.crypto.com"
	cryptoWsPath   = "/exchange/v1/market"
)

var _ Provider = (*Provider)(nil)

type (
	// CryptoProvider defines an oracle provider implemented by the crypto.com
	// public API.
	//
	// REF: https://exchange-docs.crypto.com/exchange/v1/rest-ws/index.html
	CryptoProvider struct {
		ctx      context.Context
		logger   zerolog.Logger
		endpoint config.ProviderEndpoint
		wsURL    url.URL
		wsClient *websocket.Conn

		mtx             sync.RWMutex
		tickers         map[string]CryptoTicker
		candles         map[string][]CryptoCandle
		subscribedPairs map[string]types.CurrencyPair // Symbol => types.CurrencyPair
	}

	CryptoTickersResponse struct {
		Code   int64                     `json:"code"`
		Result CryptoTickersResponseData `json:"result"`
	}

	CryptoTickersResponseData struct {
		Data []CryptoTicker `json:"data"`
	}

	CryptoTicker struct {
		Symbol string `json:"i"` // Symbol ex.: BTC_USDT
		Price  string `json:"a"` // Last price ex.: 0.0025
		Volume string `json:"v"` // Total traded base asset volume ex.: 1000
		Time   int64  `json:"t"` // Timestamp ex.: 1675246930699
	}

	CryptoCandleResponse struct {
		Code   int64                    `json:"code"`
		Result CryptoCandleResponseData `json:"result"`
	}

	CryptoCandleResponseData struct {
		Data           []CryptoCandle `json:"data"`
		InstrumentName string         `json:"instrument_name"`
	}

	CryptoCandle struct {
		Open   string `json:"o"` // Open price
		High   string `json:"h"` // High price
		Low    string `json:"l"` // Low price
		Close  string `json:"c"` // Close price
		Volume string `json:"v"` // Volume
		Start  int64  `json:"t"` // Start time
	}

	CryptoSubscriptionMsg struct {
		Method   string   `json:"method"`   // subscribe/unsubscribe
		Channels []string `json:"channels"` // Channels to be subscribed
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
			Websocket: cryptoWsHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoint.Websocket,
		Path:   cryptoWsPath,
	}

	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	defer func() {
		if response != nil {
			response.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("error connecting to crypto.com websocket: %w", err)
	}

	provider := &CryptoProvider{
		wsURL:           wsURL,
		wsClient:        wsConn,
		logger:          logger.With().Str("provider", "okx").Logger(),
		endpoint:        endpoint,
		tickers:         map[string]CryptoTicker{},
		candles:         map[string][]CryptoCandle{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}

	if err := provider.SubscribeCurrencyPairs(pairs...); err != nil {
		return nil, err
	}

	// go provider.handleWebSocketMsgs(ctx)

	return provider, nil
}

func (p *CryptoProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) error {
	if len(cps) == 0 {
		return fmt.Errorf("currency pairs is empty")
	}

	if err := p.subscribeChannels(cps...); err != nil {
		return err
	}

	p.setSubscribedPairs(cps...)
	return nil
}

func (p *CryptoProvider) subscribeChannels(cps ...types.CurrencyPair) error {
	if err := p.subscribeTickers(cps...); err != nil {
		return err
	}

	return p.subscribeCandles(cps...)
}

func (p *CryptoProvider) subscribeTickers(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = fmt.Sprintf("ticker.%s_%s", cp.Base, cp.Quote)
	}

	return p.subscribePairs(pairs...)
}

func (p *CryptoProvider) subscribeCandles(cps ...types.CurrencyPair) error {
	pairs := make([]string, len(cps))

	for i, cp := range cps {
		pairs[i] = fmt.Sprintf("candlestick.1m.%s_%s", cp.Base, cp.Quote)
	}

	return p.subscribePairs(pairs...)
}

func (p *CryptoProvider) subscribePairs(channels ...string) error {
	subsMsg := CryptoSubscriptionMsg{
		Method:   "subscribe",
		Channels: channels,
	}

	return p.wsClient.WriteJSON(subsMsg)
}

func (p *CryptoProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// func (p *BinanceProvider) GetAvailablePairs() (map[string]struct{}, error) {
// 	resp, err := http.Get(p.endpoints.Rest + binanceRestPath)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer resp.Body.Close()

// 	var pairsSummary []BinancePairSummary
// 	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
// 		return nil, err
// 	}

// 	availablePairs := make(map[string]struct{}, len(pairsSummary))
// 	for _, pairName := range pairsSummary {
// 		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
// 	}

// 	return availablePairs, nil
// }

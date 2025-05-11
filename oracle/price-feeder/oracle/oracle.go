package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	gometrics "github.com/armon/go-metrics"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/client"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	pfsync "github.com/sei-protocol/sei-chain/oracle/price-feeder/pkg/sync"
	seimetrics "github.com/sei-protocol/sei-chain/utils/metrics"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

// Oracle implements the core component responsible for fetching exchange rates
// for a given set of currency pairs and determining the correct exchange rates
// to submit to the on-chain price oracle adhering the oracle specification.
type Oracle struct {
	logger zerolog.Logger
	closer *pfsync.Closer

	providerTimeout    time.Duration
	providerPairs      map[string][]types.CurrencyPair
	chainDenomMapping  map[string]string
	previousVotePeriod float64
	priceProviders     map[string]provider.Provider
	failedProviders    map[string]error
	oracleClient       client.OracleClient
	deviations         map[string]sdk.Dec
	endpoints          map[string]config.ProviderEndpoint

	mtx             sync.RWMutex
	lastPriceSyncTS time.Time
	prices          map[string]sdk.Dec
	paramCache      ParamCache
	jailCache       JailCache
	healthchecks    map[string]http.Client
	mockSetPrices   func(ctx context.Context) error
}

// createMappingsFromPairs is a helper function to initialize maps from currencyPairs
// this is used to by test cases to initialize the oracle client
func createMappingsFromPairs(currencyPairs []config.CurrencyPair) (
	map[string]string,
	map[string][]types.CurrencyPair) {
	chainDenomMapping := make(map[string]string)
	providerPairs := make(map[string][]types.CurrencyPair)

	for _, pair := range currencyPairs {
		for _, p := range pair.Providers {
			providerPairs[p] = append(providerPairs[p], types.CurrencyPair{
				Base:  pair.Base,
				Quote: pair.Quote,
			})
		}
		chainDenomMapping[pair.Base] = pair.ChainDenom
	}
	return chainDenomMapping, providerPairs
}

func New(
	logger zerolog.Logger,
	oc client.OracleClient,
	currencyPairs []config.CurrencyPair,
	providerTimeout time.Duration,
	deviations map[string]sdk.Dec,
	endpoints map[string]config.ProviderEndpoint,
	healthchecksConfig []config.Healthchecks,
) *Oracle {

	chainDenomMapping, providerPairs := createMappingsFromPairs(currencyPairs)

	healthchecks := make(map[string]http.Client)
	for _, healthcheck := range healthchecksConfig {
		timeout, err := time.ParseDuration(healthcheck.Timeout)
		if err != nil {
			logger.Warn().
				Str("timeout", healthcheck.Timeout).
				Msg("failed to parse healthcheck timeout, skipping configuration")
		} else {
			healthchecks[healthcheck.URL] = http.Client{
				Timeout: timeout,
			}
		}
	}

	return &Oracle{
		logger:            logger.With().Str("module", "oracle").Logger(),
		closer:            pfsync.NewCloser(),
		oracleClient:      oc,
		providerPairs:     providerPairs,
		chainDenomMapping: chainDenomMapping,
		priceProviders:    make(map[string]provider.Provider),
		providerTimeout:   providerTimeout,
		deviations:        deviations,
		paramCache:        ParamCache{},
		jailCache:         JailCache{},
		failedProviders:   make(map[string]error),
		endpoints:         endpoints,
		healthchecks:      healthchecks,
	}
}

// Start starts the oracle process in a blocking fashion.
func (o *Oracle) Start(ctx context.Context) error {

	clientCtx, err := o.oracleClient.CreateClientContext()
	if err != nil {
		return err
	}
	var previousBlockHeight int64

	for {
		select {
		case <-ctx.Done():
			o.closer.Close()

		default:
			o.logger.Debug().Msg("starting oracle tick")

			// Wait for next block height to be available in the channel
			currBlockHeight := <-o.oracleClient.BlockHeightEvents

			startTime := time.Now()
			err = o.tick(ctx, clientCtx, currBlockHeight)
			if err != nil {
				seimetrics.SafeTelemetryIncrCounter(1, "failure", "tick")
				o.logger.Warn().Msg(fmt.Sprintf("Oracle tick failed for height %d, err: %s", currBlockHeight, err.Error()))
			} else {
				seimetrics.SafeTelemetryIncrCounter(1, "success", "tick")
			}
			telemetry.MeasureSince(startTime, "latency", "tick")
			seimetrics.SafeTelemetryIncrCounter(1, "num_ticks", "tick")

			// Catch any missing blocks
			if currBlockHeight > (previousBlockHeight+1) && previousBlockHeight > 0 {
				missedBlocks := currBlockHeight - (previousBlockHeight + 1)
				seimetrics.SafeTelemetryIncrCounter(float32(missedBlocks), "skipped_blocks", "tick")
			}
			previousBlockHeight = currBlockHeight
		}
	}
}

// Stop stops the oracle process and waits for it to gracefully exit.
func (o *Oracle) Stop() {
	o.closer.Close()
	<-o.closer.Done()
}

// GetLastPriceSyncTimestamp returns the latest timestamp at which prices where
// fetched from the oracle's set of exchange rate providers.
func (o *Oracle) GetLastPriceSyncTimestamp() time.Time {
	o.mtx.RLock()
	defer o.mtx.RUnlock()

	return o.lastPriceSyncTS
}

// GetPrices returns a copy of the current prices fetched from the oracle's
// set of exchange rate providers.
func (o *Oracle) GetPrices() sdk.DecCoins {
	o.mtx.RLock()
	defer o.mtx.RUnlock()
	// Creates a new array for the prices in the oracle
	prices := sdk.NewDecCoins()
	for k, v := range o.prices {
		chainDenom := o.chainDenomMapping[k]
		// Fills in the prices with each value in the oracle
		prices = prices.Add(sdk.NewDecCoinFromDec(chainDenom, v))
	}

	return prices
}

// sendProviderFailureMetric function is overridden by unit tests
var sendProviderFailureMetric = telemetry.IncrCounterWithLabels

// safeMapContains handles a nil check if the map is nil
func safeMapContains[V any](m map[string]V, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// reportPriceErrMetrics sends metrics to telemetry for missing prices
func reportPriceErrMetrics[V any](providerName string, priceType string, prices map[string]V, expected []types.CurrencyPair) {
	for _, pair := range expected {
		if !safeMapContains(prices, pair.String()) {
			sendProviderFailureMetric([]string{"failure", "provider"}, 1, []gometrics.Label{
				{Name: "type", Value: priceType},
				{Name: "reason", Value: "error"},
				{Name: "provider", Value: providerName},
				{Name: "base", Value: pair.Base},
			})
		}
	}
}

// SetPrices retrieves all the prices and candles from our set of providers as
// determined in the config. If candles are available, uses TVWAP in order
// to determine prices. If candles are not available, uses the most recent prices
// with VWAP. Warns the user of any missing prices, and filters out any faulty
// providers which do not report prices or candles within 2𝜎 of the others.
func (o *Oracle) SetPrices(ctx context.Context) error {
	if o.mockSetPrices != nil {
		return o.mockSetPrices(ctx)
	}
	g := new(errgroup.Group)
	mtx := new(sync.Mutex)
	providerPrices := make(provider.AggregatedProviderPrices)
	providerCandles := make(provider.AggregatedProviderCandles)
	requiredRates := make(map[string]struct{})

	for providerName, currencyPairs := range o.providerPairs {
		providerName := providerName
		currencyPairs := currencyPairs

		priceProvider, err := o.getOrSetProvider(ctx, providerName)
		if err != nil {
			sendProviderFailureMetric([]string{"failure", "provider"}, 1, []gometrics.Label{
				{Name: "reason", Value: "init"},
				{Name: "provider", Value: providerName},
			})
			o.logger.Debug().AnErr("err", err).Msgf("Failed to get or set provider %s", providerName)
			continue // don't block everything on one provider having an issue
		}

		for _, pair := range currencyPairs {
			if _, ok := requiredRates[pair.Base]; !ok {
				if o.paramCache.params.Whitelist.Contains(o.chainDenomMapping[pair.Base]) {
					requiredRates[pair.Base] = struct{}{}
				}
			}
		}

		g.Go(func() error {
			prices := make(map[string]provider.TickerPrice, 0)
			candles := make(map[string][]provider.CandlePrice, 0)
			ch := make(chan struct{})

			go func() {
				defer close(ch)
				prices, err = priceProvider.GetTickerPrices(currencyPairs...)
				if err != nil {
					o.logger.Debug().Err(err).Msg("failed to get ticker prices from provider")
				}
				reportPriceErrMetrics(providerName, "ticker", prices, currencyPairs)

				candles, err = priceProvider.GetCandlePrices(currencyPairs...)
				if err != nil {
					o.logger.Debug().Err(err).Msg("failed to get candle prices from provider")
				}
				reportPriceErrMetrics(providerName, "candle", candles, currencyPairs)
			}()

			select {
			case <-ch:
				break
			case <-time.After(o.providerTimeout):
				seimetrics.SafeTelemetryIncrCounterWithLabels([]string{"failure", "provider"}, 1, []gometrics.Label{
					{Name: "reason", Value: "timeout"},
					{Name: "provider", Value: providerName},
				})
				o.logger.Error().Msgf("provider timed out: %s", providerName)
				// returning nil to avoid canceling other providers that might succeed
				return nil
			}

			// flatten and collect prices based on the base currency per provider
			//
			// e.g.: {ProviderKraken: {"ATOM": <price, volume>, ...}}
			mtx.Lock()
			for _, pair := range currencyPairs {
				success := SetProviderTickerPricesAndCandles(providerName, providerPrices, providerCandles, prices, candles, pair)
				if !success {
					mtx.Unlock()
					seimetrics.SafeTelemetryIncrCounterWithLabels([]string{"failure", "provider"}, 1, []gometrics.Label{
						{Name: "reason", Value: "set-prices"},
						{Name: "provider", Value: providerName},
					})
					o.logger.Error().Msgf("failed to set prices for provider %s", providerName)
					// returning nil to avoid canceling other providers that might succeed
					return nil
				}
			}

			mtx.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// this should not be possible because there are no errors returned from the tasks
		o.logger.Error().Err(err).Msg("set-prices errgroup returned an error")
	}

	computedPrices, err := GetComputedPrices(
		o.logger,
		providerCandles,
		providerPrices,
		o.providerPairs,
		o.deviations,
		requiredRates,
	)
	if err != nil {
		return err
	}

	for base := range requiredRates {
		if _, ok := computedPrices[base]; !ok {
			return fmt.Errorf("reported prices were not equal to required rates, missed: %s", base)
		}
	}

	o.prices = computedPrices
	return nil
}

// GetComputedPrices gets the candle and ticker prices and computes it.
// It returns candles' TVWAP if possible, if not possible (not available
// or due to some staleness) it will use the most recent ticker prices
// and the VWAP formula instead.
func GetComputedPrices(
	logger zerolog.Logger,
	providerCandles provider.AggregatedProviderCandles,
	providerPrices provider.AggregatedProviderPrices,
	providerPairs map[string][]types.CurrencyPair,
	deviations map[string]sdk.Dec,
	requiredRates map[string]struct{},
) (prices map[string]sdk.Dec, err error) {
	// only do asset provider map logic is log level is debug
	if logger.GetLevel() == zerolog.DebugLevel {
		assetProviderMap := make(map[string][]string)
		for provider, val := range providerPrices {
			for asset := range val {
				assetProviderMap[asset] = append(assetProviderMap[asset], provider)
			}
		}
		assetProviderJSON, err := json.Marshal(assetProviderMap)
		if err != nil {
			return nil, err
		}
		logger.Debug().Msg(fmt.Sprintf("Asset Provider Coverage Map: %s", string(assetProviderJSON)))

		candleProviderMap := make(map[string][]string)
		for provider, val := range providerCandles {
			for asset := range val {
				candleProviderMap[asset] = append(candleProviderMap[asset], provider)
			}
		}
		candleProviderJSON, err := json.Marshal(candleProviderMap)
		if err != nil {
			return nil, err
		}
		logger.Debug().Msg(fmt.Sprintf("Candle Provider Coverage Map: %s", string(candleProviderJSON)))
	}
	// convert any non-USD denominated candles into USD
	convertedCandles, err := convertCandlesToUSD(
		logger,
		providerCandles,
		providerPairs,
		deviations,
	)
	if err != nil {
		return nil, err
	}

	// filter out any erroneous candles
	filteredCandles, err := FilterCandleDeviations(
		logger,
		convertedCandles,
		deviations,
	)
	if err != nil {
		return nil, err
	}

	// attempt to use candles for TVWAP calculations
	computedPrices, err := ComputeTVWAP(filteredCandles)
	if err != nil {
		return nil, err
	}

	candleAssets := []string{}
	tickerAssets := []string{}
	for base := range computedPrices {
		candleAssets = append(candleAssets, base)
	}
	allRequiredAssetsPresent := true
	for asset := range requiredRates {
		if _, ok := computedPrices[asset]; !ok {
			allRequiredAssetsPresent = false
		}
	}
	// If we're missing some assets, calculate tickers too to fill the gaps
	// use most recent prices & VWAP instead.
	if !allRequiredAssetsPresent {
		logger.Debug().Msg("Evaluating tickers because some required rates were not provided via candles")
		convertedTickers, err := convertTickersToUSD(
			logger,
			providerPrices,
			providerPairs,
			deviations,
		)
		if err != nil {
			return nil, err
		}

		filteredProviderPrices, err := FilterTickerDeviations(
			logger,
			convertedTickers,
			deviations,
		)
		if err != nil {
			return nil, err
		}

		vwapPrices, err := ComputeVWAP(filteredProviderPrices)
		if err != nil {
			return nil, err
		}

		for asset, price := range vwapPrices {
			if _, ok := computedPrices[asset]; !ok {
				tickerAssets = append(tickerAssets, asset)
				computedPrices[asset] = price
			}
		}
	}
	logger.Debug().Msg(fmt.Sprint("Assets using Candle TVWAP: ", candleAssets, " Assets using Ticker VWAP: ", tickerAssets))
	return computedPrices, nil
}

// SetProviderTickerPricesAndCandles flattens and collects prices for
// candles and tickers based on the base currency per provider.
// Returns true if at least one of price or candle exists.
func SetProviderTickerPricesAndCandles(
	providerName string,
	providerPrices provider.AggregatedProviderPrices,
	providerCandles provider.AggregatedProviderCandles,
	prices map[string]provider.TickerPrice,
	candles map[string][]provider.CandlePrice,
	pair types.CurrencyPair,
) (success bool) {
	if _, ok := providerPrices[providerName]; !ok {
		providerPrices[providerName] = make(map[string]provider.TickerPrice)
	}
	if _, ok := providerCandles[providerName]; !ok {
		providerCandles[providerName] = make(map[string][]provider.CandlePrice)
	}

	tp, pricesOk := prices[pair.String()]
	cp, candlesOk := candles[pair.String()]

	if pricesOk {
		providerPrices[providerName][pair.Base] = tp
	}
	if candlesOk {
		providerCandles[providerName][pair.Base] = cp
	}

	return pricesOk || candlesOk
}

// GetParamCache returns the last updated parameters of the x/oracle module
// if the current ParamCache is outdated, we will query it again.
func (o *Oracle) GetParamCache(ctx context.Context, currentBlockHeight int64) (oracletypes.Params, error) {
	if !o.paramCache.IsOutdated(currentBlockHeight) {
		return *o.paramCache.params, nil
	}

	params, err := o.GetParams(ctx)
	if err != nil {
		return oracletypes.Params{}, err
	}

	o.checkWhitelist(params)
	o.paramCache.Update(currentBlockHeight, params)
	return params, nil
}

// GetParams returns the current on-chain parameters of the x/oracle module.
func (o *Oracle) GetParams(ctx context.Context) (oracletypes.Params, error) {
	grpcConn, err := grpc.Dial(
		o.oracleClient.GRPCEndpoint,
		// the Cosmos SDK doesn't support any transport security mechanism
		grpc.WithInsecure(),
		grpc.WithContextDialer(dialerFunc),
	)
	if err != nil {
		return oracletypes.Params{}, fmt.Errorf("failed to dial Cosmos gRPC service: %w", err)
	}

	defer grpcConn.Close()
	queryClient := oracletypes.NewQueryClient(grpcConn)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	queryResponse, err := queryClient.Params(ctx, &oracletypes.QueryParamsRequest{})
	if err != nil {
		return oracletypes.Params{}, fmt.Errorf("failed to get x/oracle params: %w", err)
	}

	return queryResponse.Params, nil
}

func (o *Oracle) getOrSetProvider(ctx context.Context, providerName string) (provider.Provider, error) {
	var (
		priceProvider provider.Provider
		ok            bool
	)

	//TODO: replace with a exponential backoff mechanism
	if err, ok := o.failedProviders[providerName]; ok {
		return nil, errors.Wrap(err, "failed at first init (skipping provider)")
	}

	priceProvider, ok = o.priceProviders[providerName]
	if !ok {
		newProvider, err := NewProvider(
			ctx,
			providerName,
			o.logger,
			o.endpoints[providerName],
			o.providerPairs[providerName]...,
		)
		if err != nil {
			o.failedProviders[providerName] = err
			return nil, err
		}
		priceProvider = newProvider

		o.priceProviders[providerName] = priceProvider
	}

	return priceProvider, nil
}

// Create various providers to pull priace data for oracle price feeds
func NewProvider(
	ctx context.Context,
	providerName string,
	logger zerolog.Logger,
	endpoint config.ProviderEndpoint,
	providerPairs ...types.CurrencyPair,
) (provider.Provider, error) {
	switch providerName {
	case config.ProviderBinance:
		return provider.NewBinanceProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderKraken:
		return provider.NewKrakenProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderCrypto:
		return provider.NewCryptoProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderMexc:
		return provider.NewMexcProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderHuobi:
		return provider.NewHuobiProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderCoinbase:
		return provider.NewCoinbaseProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderOkx:
		return provider.NewOkxProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderGate:
		return provider.NewGateProvider(ctx, logger, endpoint, providerPairs...)

	case config.ProviderMock:
		return provider.NewMockProvider(), nil
	}

	return nil, fmt.Errorf("provider %s not found", providerName)
}

func (o *Oracle) checkWhitelist(params oracletypes.Params) {
	chainDenomSet := make(map[string]struct{})
	for _, v := range o.chainDenomMapping {
		chainDenomSet[v] = struct{}{}
	}
	for _, denom := range params.Whitelist {
		if _, ok := chainDenomSet[denom.Name]; !ok {
			o.logger.Warn().Str("denom", denom.Name).Msg("price missing for required denom")
		}
	}
}

// filterPricesByDenomList takes a list of DecCoins and filters out any
// coins that are not in the provided DenomList.
func filterPricesByDenomList(coinPrices sdk.DecCoins, denomList oracletypes.DenomList) sdk.DecCoins {
	result := sdk.NewDecCoins()
	for _, c := range coinPrices {
		for _, d := range denomList {
			if d.Name == c.Denom {
				result = result.Add(c)
			}
		}
	}
	return result
}

func (o *Oracle) tick(
	ctx context.Context,
	clientCtx sdkclient.Context,
	blockHeight int64) error {

	startTime := time.Now().UTC()

	o.logger.Debug().Msg(fmt.Sprintf("executing oracle tick for height %d", blockHeight))

	if blockHeight < 1 {
		return fmt.Errorf("expected positive block height")
	}

	isJailed, err := o.GetCachedJailedState(ctx, blockHeight)
	if err != nil {
		return err
	}
	if isJailed {
		return fmt.Errorf("validator %s is jailed", o.oracleClient.ValidatorAddrString)
	}

	oracleParams, err := o.GetParamCache(ctx, blockHeight)
	if err != nil {
		return err
	}

	if err = o.SetPrices(ctx); err != nil {
		return err
	}
	o.lastPriceSyncTS = time.Now()

	// Get oracle vote period, next block height, current vote period, and index
	// in the vote period.
	oracleVotePeriod := int64(oracleParams.VotePeriod)
	nextBlockHeight := blockHeight + 1
	currentVotePeriod := math.Floor(float64(nextBlockHeight) / float64(oracleVotePeriod))

	// Skip until new voting period. Specifically, skip when:
	// index [0, oracleVotePeriod - 1] > oracleVotePeriod - 2 OR index is 0
	if currentVotePeriod == o.previousVotePeriod {
		o.logger.Info().
			Int64("vote_period", oracleVotePeriod).
			Float64("previous", o.previousVotePeriod).
			Float64("current", currentVotePeriod).
			Int64("tick_duration", time.Since(startTime).Milliseconds()).
			Msg("skipping until next voting period")
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(o.oracleClient.ValidatorAddrString)
	if err != nil {
		return err
	}

	prices := o.GetPrices()

	// filter for whitelisted denominations so that extra oracle prices are not penalized
	filteredPrices := filterPricesByDenomList(prices, oracleParams.Whitelist)
	exchangeRatesStr := GenerateExchangeRatesString(filteredPrices)

	// otherwise, we're in the next voting period and thus we vote
	voteMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: exchangeRatesStr,
		Feeder:        o.oracleClient.OracleAddrString,
		Validator:     valAddr.String(),
	}

	o.logger.Debug().
		Str("exchange_rates", GenerateExchangeRatesString(prices)).
		Msg("pre-filtered prices")

	o.logger.Info().
		Str("exchange_rates", voteMsg.ExchangeRates).
		Str("validator", voteMsg.Validator).
		Str("feeder", voteMsg.Feeder).
		Float64("vote_period", currentVotePeriod).
		Int64("tick_duration", time.Since(startTime).Milliseconds()).
		Msg("Going to broadcast vote")

	resp, err := o.oracleClient.BroadcastTx(clientCtx, voteMsg)
	if err != nil {
		o.logResponseError(err, resp, startTime, blockHeight)
		seimetrics.SafeTelemetryIncrCounter(1, "failure", "broadcast")
		return err
	}

	o.logger.Info().
		Str("status", "success").
		Uint32("response_code", resp.Code).
		Str("tx_hash", resp.TxHash).
		Int64("tick_duration", time.Since(startTime).Milliseconds()).
		Msg(fmt.Sprintf("broadcasted for height %d", blockHeight))
	seimetrics.SafeTelemetryIncrCounter(1, "success", "broadcast")

	o.previousVotePeriod = currentVotePeriod
	o.healthchecksPing()

	return nil
}

func (o *Oracle) logResponseError(err error, resp *sdk.TxResponse, startTime time.Time, blockHeight int64) {
	responseCode := -1 // success is 0
	var txHash string

	if resp != nil {
		responseCode = int(resp.Code)
		txHash = resp.TxHash
	}

	o.logger.Error().Err(err).
		Str("status", "failure").
		Int("response_code", responseCode).
		Str("tx_hash", txHash).
		Int64("tick_duration", time.Since(startTime).Milliseconds()).
		Msg(fmt.Sprintf("broadcasted for height %d", blockHeight))
}

func (o *Oracle) healthchecksPing() {
	for url, client := range o.healthchecks {
		o.logger.Info().Msg("updating healthcheck status")
		response, err := client.Get(url)
		if err != nil {
			o.logger.Warn().Msg("healthcheck ping failed")
		}
		response.Body.Close()
	}
}

// GenerateExchangeRatesString generates a canonical string representation of
// the aggregated exchange rates.
func GenerateExchangeRatesString(prices sdk.DecCoins) string {
	prices.Sort()
	return prices.String()
}

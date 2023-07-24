package oracle

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/client"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	pfsync "github.com/sei-protocol/sei-chain/oracle/price-feeder/pkg/sync"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
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
	oracleClient       client.OracleClient
	deviations         map[string]sdk.Dec
	endpoints          map[string]config.ProviderEndpoint

	mtx             sync.RWMutex
	lastPriceSyncTS time.Time
	prices          map[string]sdk.Dec
	paramCache      ParamCache
	jailCache       JailCache
	healthchecks    map[string]http.Client
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
	providerPairs := make(map[string][]types.CurrencyPair)
	chainDenomMapping := make(map[string]string)

	for _, pair := range currencyPairs {
		for _, provider := range pair.Providers {
			providerPairs[provider] = append(providerPairs[provider], types.CurrencyPair{
				Base:  pair.Base,
				Quote: pair.Quote,
			})
		}
		chainDenomMapping[pair.Base] = pair.ChainDenom
	}

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
				telemetry.IncrCounter(1, "failure", "tick")
				o.logger.Warn().Msg(fmt.Sprintf("Oracle tick failed for height %d, err: %s", currBlockHeight, err.Error()))
			} else {
				telemetry.IncrCounter(1, "success", "tick")
			}
			telemetry.MeasureSince(startTime, "latency", "tick")
			telemetry.IncrCounter(1, "num_ticks", "tick")

			// Catch any missing blocks
			if currBlockHeight > (previousBlockHeight+1) && previousBlockHeight > 0 {
				missedBlocks := currBlockHeight - (previousBlockHeight + 1)
				telemetry.IncrCounter(float32(missedBlocks), "skipped_blocks", "tick")
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

// SetPrices retrieves all the prices and candles from our set of providers as
// determined in the config. If candles are available, uses TVWAP in order
// to determine prices. If candles are not available, uses the most recent prices
// with VWAP. Warns the user of any missing prices, and filters out any faulty
// providers which do not report prices or candles within 2𝜎 of the others.
func (o *Oracle) SetPrices(ctx context.Context) error {
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
			return err
		}

		for _, pair := range currencyPairs {
			if _, ok := requiredRates[pair.Base]; !ok {
				requiredRates[pair.Base] = struct{}{}
			}
		}

		g.Go(func() error {
			prices := make(map[string]provider.TickerPrice, 0)
			candles := make(map[string][]provider.CandlePrice, 0)
			ch := make(chan struct{})
			errCh := make(chan error, 1)

			go func() {
				defer close(ch)
				prices, err = priceProvider.GetTickerPrices(currencyPairs...)
				if err != nil {
					telemetry.IncrCounter(1, "failure", "provider", "type", "ticker")
					errCh <- err
				}

				candles, err = priceProvider.GetCandlePrices(currencyPairs...)
				if err != nil {
					telemetry.IncrCounter(1, "failure", "provider", "type", "candle")
					errCh <- err
				}
			}()

			select {
			case <-ch:
				break
			case err := <-errCh:
				return err
			case <-time.After(o.providerTimeout):
				telemetry.IncrCounter(1, "failure", "provider", "type", "timeout")
				return fmt.Errorf("provider timed out: %s", providerName)
			}

			// flatten and collect prices based on the base currency per provider
			//
			// e.g.: {ProviderKraken: {"ATOM": <price, volume>, ...}}
			mtx.Lock()
			for _, pair := range currencyPairs {
				success := SetProviderTickerPricesAndCandles(providerName, providerPrices, providerCandles, prices, candles, pair)
				if !success {
					mtx.Unlock()
					return fmt.Errorf("failed to find any exchange rates in provider responses")
				}
			}

			mtx.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		o.logger.Debug().Err(err).Msg("failed to get ticker prices from provider")
	}

	computedPrices, err := GetComputedPrices(
		o.logger,
		providerCandles,
		providerPrices,
		o.providerPairs,
		o.deviations,
	)
	if err != nil {
		return err
	}

	// TODO: make this more lenient to allow assigning prices even when unable to retrieve all
	if len(computedPrices) != len(requiredRates) {
		return fmt.Errorf("unable to get prices for all exchange candles")
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
) (prices map[string]sdk.Dec, err error) {
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
	tvwapPrices, err := ComputeTVWAP(filteredCandles)
	if err != nil {
		return nil, err
	}

	// If TVWAP candles are not available or were filtered out due to staleness,
	// use most recent prices & VWAP instead.
	if len(tvwapPrices) == 0 {
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

		return vwapPrices, nil
	}

	return tvwapPrices, nil
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

func (o *Oracle) tick(
	ctx context.Context,
	clientCtx sdkclient.Context,
	blockHeight int64) error {

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
			Msg("skipping until next voting period")
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(o.oracleClient.ValidatorAddrString)
	if err != nil {
		return err
	}

	exchangeRatesStr := GenerateExchangeRatesString(o.GetPrices())

	// otherwise, we're in the next voting period and thus we vote
	voteMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: exchangeRatesStr,
		Feeder:        o.oracleClient.OracleAddrString,
		Validator:     valAddr.String(),
	}

	o.logger.Info().
		Str("exchange_rates", voteMsg.ExchangeRates).
		Str("validator", voteMsg.Validator).
		Str("feeder", voteMsg.Feeder).
		Float64("vote_period", currentVotePeriod).
		Msg("Going to broadcast vote")

	resp, err := o.oracleClient.BroadcastTx(clientCtx, voteMsg)
	if err != nil {
		telemetry.IncrCounter(1, "failure", "broadcast")
		return err
	}
	o.logger.Info().
		Uint32("response_code", resp.Code).
		Str("tx_hash", resp.TxHash).
		Msg(fmt.Sprintf("Successfully broadcasted for height %d", blockHeight))
	telemetry.IncrCounter(1, "success", "broadcast")

	o.previousVotePeriod = currentVotePeriod
	o.healthchecksPing()

	return nil
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

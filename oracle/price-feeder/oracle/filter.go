package oracle

import (
	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"
)

// defaultDeviationThreshold defines how many 𝜎 a provider can be away
// from the mean without being considered faulty. This can be overridden
// in the config.
var defaultDeviationThreshold = sdk.MustNewDecFromStr("1.0")

// FilterTickerDeviations finds the standard deviations of the prices of
// all assets, and filters out any providers that are not within 2𝜎 of the mean.
func FilterTickerDeviations(
	logger zerolog.Logger,
	prices provider.AggregatedProviderPrices,
	deviationThresholds map[string]sdk.Dec,
) (provider.AggregatedProviderPrices, error) {
	var (
		filteredPrices = make(provider.AggregatedProviderPrices)
		priceMap       = make(map[string]map[string]sdk.Dec)
	)

	for providerName, priceTickers := range prices {
		p, ok := priceMap[providerName]
		if !ok {
			p = map[string]sdk.Dec{}
			priceMap[providerName] = p
		}
		for base, tp := range priceTickers {
			p[base] = tp.Price
		}
	}

	deviations, means, err := StandardDeviation(priceMap)
	if err != nil {
		return nil, err
	}

	// We accept any prices that are within (2 * T)𝜎, or for which we couldn't get 𝜎.
	// T is defined as the deviation threshold, either set by the config
	// or defaulted to 1.
	for providerName, priceTickers := range prices {
		for base, tp := range priceTickers {
			t := defaultDeviationThreshold
			if _, ok := deviationThresholds[base]; ok {
				t = deviationThresholds[base]
			}

			if d, ok := deviations[base]; !ok || isBetween(tp.Price, means[base], d.Mul(t)) {
				p, ok := filteredPrices[providerName]
				if !ok {
					p = map[string]provider.TickerPrice{}
					filteredPrices[providerName] = p
				}
				p[base] = tp
			} else {
				telemetry.IncrCounterWithLabels([]string{"failure", "provider"}, 1, []metrics.Label{
					{Name: "type", Value: "ticker"},
					{Name: "reason", Value: "deviation"},
					{Name: "base", Value: base},
					{Name: "provider", Value: providerName},
				})
				logger.Warn().
					Str("base", base).
					Str("provider", providerName).
					Str("price", tp.Price.String()).
					Msg("provider deviating from other prices")
			}
		}
	}

	return filteredPrices, nil
}

// FilterCandleDeviations finds the standard deviations of the tvwaps of
// all assets, and filters out any providers that are not within 2𝜎 of the mean.
func FilterCandleDeviations(
	logger zerolog.Logger,
	candles provider.AggregatedProviderCandles,
	deviationThresholds map[string]sdk.Dec,
) (provider.AggregatedProviderCandles, error) {
	var (
		filteredCandles = make(provider.AggregatedProviderCandles)
		tvwaps          = make(map[string]map[string]sdk.Dec)
	)

	for providerName, priceCandles := range candles {
		candlePrices := make(provider.AggregatedProviderCandles)

		for base, cp := range priceCandles {
			p, ok := candlePrices[providerName]
			if !ok {
				p = map[string][]provider.CandlePrice{}
				candlePrices[providerName] = p
			}
			p[base] = cp
		}

		tvwap, err := ComputeTVWAP(candlePrices)
		if err != nil {
			return nil, err
		}

		for base, asset := range tvwap {
			if _, ok := tvwaps[providerName]; !ok {
				tvwaps[providerName] = make(map[string]sdk.Dec)
			}

			tvwaps[providerName][base] = asset
		}
	}

	deviations, means, err := StandardDeviation(tvwaps)
	if err != nil {
		return nil, err
	}

	// We accept any prices that are within (2 * T)𝜎, or for which we couldn't get 𝜎.
	// T is defined as the deviation threshold, either set by the config
	// or defaulted to 1.
	for providerName, priceMap := range tvwaps {
		for base, price := range priceMap {
			t := defaultDeviationThreshold
			if _, ok := deviationThresholds[base]; ok {
				t = deviationThresholds[base]
			}

			if d, ok := deviations[base]; !ok || isBetween(price, means[base], d.Mul(t)) {
				p, ok := filteredCandles[providerName]
				if !ok {
					p = map[string][]provider.CandlePrice{}
					filteredCandles[providerName] = p
				}
				p[base] = candles[providerName][base]
			} else {
				telemetry.IncrCounterWithLabels([]string{"failure", "provider"}, 1, []metrics.Label{
					{Name: "type", Value: "candle"},
					{Name: "reason", Value: "deviation"},
					{Name: "base", Value: base},
					{Name: "provider", Value: providerName},
				})
				logger.Warn().
					Str("base", base).
					Str("provider", providerName).
					Str("price", price.String()).
					Msg("provider deviating from other candles")
			}
		}
	}

	return filteredCandles, nil
}

func isBetween(p, mean, margin sdk.Dec) bool {
	return p.GTE(mean.Sub(margin)) &&
		p.LTE(mean.Add(margin))
}

package oracle

import (
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var minimumTimeWeight = sdk.MustNewDecFromStr("0.2")

// this lets us mock now for tests
var mockNow int64

const (
	// tvwapCandlePeriod represents the time period we use for tvwap in minutes
	tvwapCandlePeriod = 5 * time.Minute
)

// compute VWAP for each base by dividing the Σ {P * V} by Σ {V}
func vwap(weightedPrices, volumeSum map[string]sdk.Dec) (map[string]sdk.Dec, error) {
	vwap := make(map[string]sdk.Dec)

	for base, p := range weightedPrices {
		if !volumeSum[base].Equal(sdk.ZeroDec()) {
			if _, ok := vwap[base]; !ok {
				vwap[base] = sdk.ZeroDec()
			}

			vwap[base] = p.Quo(volumeSum[base])
		}
	}

	return vwap, nil
}

// ComputeVWAP computes the volume weighted average price for all price points
// for each ticker/exchange pair. The provided prices argument reflects a mapping
// of provider => {<base> => <TickerPrice>, ...}.
//
// Ref: https://en.wikipedia.org/wiki/Volume-weighted_average_price
func ComputeVWAP(prices provider.AggregatedProviderPrices) (map[string]sdk.Dec, error) {
	var (
		weightedPrices = make(map[string]sdk.Dec)
		volumeSum      = make(map[string]sdk.Dec)
	)

	for _, providerPrices := range prices {
		for base, tp := range providerPrices {
			if _, ok := weightedPrices[base]; !ok {
				weightedPrices[base] = sdk.ZeroDec()
			}
			if _, ok := volumeSum[base]; !ok {
				volumeSum[base] = sdk.ZeroDec()
			}

			// weightedPrices[base] = Σ {P * V} for all TickerPrice
			weightedPrices[base] = weightedPrices[base].Add(tp.Price.Mul(tp.Volume))

			// track total volume for each base
			volumeSum[base] = volumeSum[base].Add(tp.Volume)
		}
	}

	return vwap(weightedPrices, volumeSum)
}

// ComputeTVWAP computes the time volume weighted average price for all points
// for each exchange pair. Filters out any candles that did not occur within
// timePeriod. The provided prices argument reflects a mapping of
// provider => {<base> => <TickerPrice>, ...}.
//
// Ref : https://en.wikipedia.org/wiki/Time-weighted_average_price
func ComputeTVWAP(prices provider.AggregatedProviderCandles) (map[string]sdk.Dec, error) {
	var (
		weightedPrices = make(map[string]sdk.Dec)
		volumeSum      = make(map[string]sdk.Dec)
		now            = provider.PastUnixTime(0)
		timePeriod     = provider.PastUnixTime(tvwapCandlePeriod)
	)

	// this lets us mock now for tests
	if mockNow > 0 {
		now = mockNow
	}

	for _, providerPrices := range prices {
		for base := range providerPrices {
			cp := providerPrices[base]

			if _, ok := weightedPrices[base]; !ok {
				weightedPrices[base] = sdk.ZeroDec()
			}
			if _, ok := volumeSum[base]; !ok {
				volumeSum[base] = sdk.ZeroDec()
			}

			// Sort by timestamp old -> new
			sort.SliceStable(cp, func(i, j int) bool {
				return cp[i].TimeStamp < cp[j].TimeStamp
			})

			period := sdk.NewDec(now - cp[0].TimeStamp)

			// weight unit is one, then decreased proportionately by candle age
			weightUnit := sdk.OneDec().Sub(minimumTimeWeight)

			// if zero, it would divide by zero
			if !period.Equal(sdk.ZeroDec()) {
				weightUnit = weightUnit.Quo(period)
			}

			// get weighted prices, and sum of volumes
			for _, candle := range cp {
				// we only want candles within the last timePeriod
				if timePeriod < candle.TimeStamp {
					// timeDiff = now - candle.TimeStamp
					timeDiff := sdk.NewDec(now - candle.TimeStamp)
					// volume = candle.Volume * (weightUnit * (period - timeDiff) + minimumTimeWeight)
					volume := candle.Volume.Mul(
						weightUnit.Mul(period.Sub(timeDiff).Add(minimumTimeWeight)),
					)
					volumeSum[base] = volumeSum[base].Add(volume)
					weightedPrices[base] = weightedPrices[base].Add(candle.Price.Mul(volume))
				}
			}

		}
	}

	return vwap(weightedPrices, volumeSum)
}

// StandardDeviation returns maps of the standard deviations and means of assets.
// Will skip calculating for an asset if there are less than 3 prices.
func StandardDeviation(
	prices map[string]map[string]sdk.Dec,
) (map[string]sdk.Dec, map[string]sdk.Dec, error) {
	var (
		deviations = make(map[string]sdk.Dec)
		means      = make(map[string]sdk.Dec)
		priceSlice = make(map[string][]sdk.Dec)
		priceSums  = make(map[string]sdk.Dec)
	)

	for _, providerPrices := range prices {
		for base, p := range providerPrices {
			if _, ok := priceSums[base]; !ok {
				priceSums[base] = sdk.ZeroDec()
			}
			if _, ok := priceSlice[base]; !ok {
				priceSlice[base] = []sdk.Dec{}
			}

			priceSums[base] = priceSums[base].Add(p)
			priceSlice[base] = append(priceSlice[base], p)
		}
	}

	for base, sum := range priceSums {
		// Skip if standard deviation would not be meaningful
		if len(priceSlice[base]) < 3 {
			continue
		}
		if _, ok := deviations[base]; !ok {
			deviations[base] = sdk.ZeroDec()
		}
		if _, ok := means[base]; !ok {
			means[base] = sdk.ZeroDec()
		}

		numPrices := int64(len(priceSlice[base]))
		means[base] = sum.QuoInt64(numPrices)
		varianceSum := sdk.ZeroDec()

		for _, price := range priceSlice[base] {
			deviation := price.Sub(means[base])
			varianceSum = varianceSum.Add(deviation.Mul(deviation))
		}

		variance := varianceSum.QuoInt64(numPrices)

		standardDeviation, err := variance.ApproxSqrt()
		if err != nil {
			return make(map[string]sdk.Dec), make(map[string]sdk.Dec), err
		}

		deviations[base] = standardDeviation
	}

	return deviations, means, nil
}

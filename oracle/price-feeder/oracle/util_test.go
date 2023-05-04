package oracle_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/provider"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestComputeVWAP(t *testing.T) {
	testCases := map[string]struct {
		prices   map[string]map[string]provider.TickerPrice
		expected map[string]sdk.Dec
	}{
		"empty prices": {
			prices:   make(map[string]map[string]provider.TickerPrice),
			expected: make(map[string]sdk.Dec),
		},
		"nil prices": {
			prices:   nil,
			expected: make(map[string]sdk.Dec),
		},
		"non empty prices": {
			prices: map[string]map[string]provider.TickerPrice{
				config.ProviderBinance: {
					"ATOM": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("28.21000000"),
						Volume: sdk.MustNewDecFromStr("2749102.78000000"),
					},
					"UMEE": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("1.13000000"),
						Volume: sdk.MustNewDecFromStr("249102.38000000"),
					},
					"SEI": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("64.87000000"),
						Volume: sdk.MustNewDecFromStr("7854934.69000000"),
					},
				},
				config.ProviderKraken: {
					"ATOM": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("28.268700"),
						Volume: sdk.MustNewDecFromStr("178277.53314385"),
					},
					"SEI": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("64.87853000"),
						Volume: sdk.MustNewDecFromStr("458917.46353577"),
					},
				},
				"FOO": {
					"ATOM": provider.TickerPrice{
						Price:  sdk.MustNewDecFromStr("28.168700"),
						Volume: sdk.MustNewDecFromStr("4749102.53314385"),
					},
				},
			},
			expected: map[string]sdk.Dec{
				"ATOM": sdk.MustNewDecFromStr("28.185812745610043621"),
				"UMEE": sdk.MustNewDecFromStr("1.13000000"),
				"SEI":  sdk.MustNewDecFromStr("64.870470848638112395"),
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			vwap, err := oracle.ComputeVWAP(tc.prices)
			require.NoError(t, err)
			require.Len(t, vwap, len(tc.expected))

			for k, v := range tc.expected {
				require.Equalf(t, v, vwap[k], "unexpected VWAP for %s", k)
			}
		})
	}
}

func TestStandardDeviation(t *testing.T) {
	type deviation struct {
		mean      sdk.Dec
		deviation sdk.Dec
	}
	testCases := map[string]struct {
		prices   map[string]map[string]sdk.Dec
		expected map[string]deviation
	}{
		"empty prices": {
			prices:   make(map[string]map[string]sdk.Dec),
			expected: map[string]deviation{},
		},
		"nil prices": {
			prices:   nil,
			expected: map[string]deviation{},
		},
		"not enough prices": {
			prices: map[string]map[string]sdk.Dec{
				config.ProviderBinance: {
					"ATOM": sdk.MustNewDecFromStr("28.21000000"),
					"UMEE": sdk.MustNewDecFromStr("1.13000000"),
					"SEI":  sdk.MustNewDecFromStr("64.87000000"),
				},
				config.ProviderKraken: {
					"ATOM": sdk.MustNewDecFromStr("28.23000000"),
					"UMEE": sdk.MustNewDecFromStr("1.13050000"),
					"SEI":  sdk.MustNewDecFromStr("64.85000000"),
				},
			},
			expected: map[string]deviation{},
		},
		"some prices": {
			prices: map[string]map[string]sdk.Dec{
				config.ProviderBinance: {
					"ATOM": sdk.MustNewDecFromStr("28.21000000"),
					"UMEE": sdk.MustNewDecFromStr("1.13000000"),
					"SEI":  sdk.MustNewDecFromStr("64.87000000"),
				},
				config.ProviderKraken: {
					"ATOM": sdk.MustNewDecFromStr("28.23000000"),
					"UMEE": sdk.MustNewDecFromStr("1.13050000"),
				},
				config.ProviderCoinbase: {
					"ATOM": sdk.MustNewDecFromStr("28.40000000"),
					"UMEE": sdk.MustNewDecFromStr("1.14000000"),
					"SEI":  sdk.MustNewDecFromStr("64.10000000"),
				},
			},
			expected: map[string]deviation{
				"ATOM": {
					mean:      sdk.MustNewDecFromStr("28.28"),
					deviation: sdk.MustNewDecFromStr("0.085244745683629475"),
				},
				"UMEE": {
					mean:      sdk.MustNewDecFromStr("1.1335"),
					deviation: sdk.MustNewDecFromStr("0.004600724580614015"),
				},
			},
		},

		"non empty prices": {
			prices: map[string]map[string]sdk.Dec{
				config.ProviderBinance: {
					"ATOM": sdk.MustNewDecFromStr("28.21000000"),

					"UMEE": sdk.MustNewDecFromStr("1.13000000"),
					"SEI":  sdk.MustNewDecFromStr("64.87000000"),
				},
				config.ProviderKraken: {
					"ATOM": sdk.MustNewDecFromStr("28.23000000"),
					"UMEE": sdk.MustNewDecFromStr("1.13050000"),
					"SEI":  sdk.MustNewDecFromStr("64.85000000"),
				},
				config.ProviderCoinbase: {
					"ATOM": sdk.MustNewDecFromStr("28.40000000"),
					"UMEE": sdk.MustNewDecFromStr("1.14000000"),
					"SEI":  sdk.MustNewDecFromStr("64.10000000"),
				},
			},
			expected: map[string]deviation{
				"ATOM": {
					mean:      sdk.MustNewDecFromStr("28.28"),
					deviation: sdk.MustNewDecFromStr("0.085244745683629475"),
				},
				"UMEE": {
					mean:      sdk.MustNewDecFromStr("1.1335"),
					deviation: sdk.MustNewDecFromStr("0.004600724580614015"),
				},
				"SEI": {
					mean:      sdk.MustNewDecFromStr("64.606666666666666666"),
					deviation: sdk.MustNewDecFromStr("0.358360464089193609"),
				},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			deviation, mean, err := oracle.StandardDeviation(tc.prices)
			require.NoError(t, err)
			require.Len(t, deviation, len(tc.expected))
			require.Len(t, mean, len(tc.expected))

			for k, v := range tc.expected {
				require.Equalf(t, v.deviation, deviation[k], "unexpected deviation for %s", k)
				require.Equalf(t, v.mean, mean[k], "unexpected mean for %s", k)
			}
		})
	}
}

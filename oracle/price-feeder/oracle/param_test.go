package oracle

import (
	"testing"

	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestParamCacheIsOutdated(t *testing.T) {
	testCases := map[string]struct {
		paramCache         ParamCache
		currentBlockHeight int64
		expected           bool
	}{
		"Params Nil": {
			paramCache: ParamCache{
				params:           nil,
				lastUpdatedBlock: 0,
			},
			currentBlockHeight: 10,
			expected:           true,
		},
		"currentBlockHeight < cacheOnChainBlockQuantity": {
			paramCache: ParamCache{
				params:           &oracletypes.Params{},
				lastUpdatedBlock: 0,
			},
			currentBlockHeight: 199,
			expected:           false,
		},
		"currentBlockHeight < lastUpdatedBlock": {
			paramCache: ParamCache{
				params:           &oracletypes.Params{},
				lastUpdatedBlock: 205,
			},
			currentBlockHeight: 203,
			expected:           true,
		},
		"Outdated": {
			paramCache: ParamCache{
				params:           &oracletypes.Params{},
				lastUpdatedBlock: 200,
			},
			currentBlockHeight: 401,
			expected:           true,
		},
		"Limit to keep in cache": {
			paramCache: ParamCache{
				params:           &oracletypes.Params{},
				lastUpdatedBlock: 200,
			},
			currentBlockHeight: 400,
			expected:           false,
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.paramCache.IsOutdated(tc.currentBlockHeight))
		})
	}
}

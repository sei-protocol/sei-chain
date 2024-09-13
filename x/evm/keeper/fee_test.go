package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestBaseFeePerGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetDynamicBaseFeePerGas(ctx))
	originalbf := k.GetDynamicBaseFeePerGas(ctx)
	k.SetDynamicBaseFeePerGas(ctx, sdk.OneDec())
	require.Equal(t, sdk.NewDecFromInt(sdk.NewInt(1)), k.GetDynamicBaseFeePerGas(ctx))
	k.SetDynamicBaseFeePerGas(ctx, originalbf)
}

func TestAdjustBaseFeePerGas(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	testCases := []struct {
		name            string
		currentBaseFee  float64
		minimumFee      float64
		blockGasUsed    uint64
		blockGasLimit   uint64
		expectedBaseFee uint64
	}{
		{
			name:            "Block gas usage exactly half of limit, no fee change",
			currentBaseFee:  100,
			minimumFee:      10,
			blockGasUsed:    500000,
			blockGasLimit:   1000000,
			expectedBaseFee: 100,
		},
		{
			name:            "Block gas usage 75%, base fee increases",
			currentBaseFee:  10000,
			minimumFee:      10,
			blockGasUsed:    750000,
			blockGasLimit:   1000000,
			expectedBaseFee: 10250,
		},
		{
			name:            "Block gas usage 25%, base fee decreases",
			currentBaseFee:  10000,
			minimumFee:      10,
			blockGasUsed:    250000,
			blockGasLimit:   1000000,
			expectedBaseFee: 9950,
		},
		{
			name:            "Block gas usage low, new base fee below minimum, set to minimum",
			currentBaseFee:  100,
			minimumFee:      99,
			blockGasUsed:    0,
			blockGasLimit:   1000000,
			expectedBaseFee: 99, // Should not go below the minimum fee
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx = ctx.WithConsensusParams(&tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{MaxGas: int64(tc.blockGasLimit)},
			})
			k.SetDynamicBaseFeePerGas(ctx, sdk.NewDecFromInt(sdk.NewInt(int64(tc.currentBaseFee))))
			p := k.GetParams(ctx)
			p.MinimumFeePerGas = sdk.NewDec(int64(tc.minimumFee))
			k.SetParams(ctx, p)
			k.AdjustDynamicBaseFeePerGas(ctx, tc.blockGasUsed)
			expected := sdk.NewDecFromInt(sdk.NewInt(int64(tc.expectedBaseFee)))
			height := ctx.BlockHeight()
			require.Equal(t, expected, k.GetDynamicBaseFeePerGas(ctx.WithBlockHeight(height+1)), "base fee did not match expected value")
		})
	}
}

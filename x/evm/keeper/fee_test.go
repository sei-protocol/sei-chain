package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestBaseFeePerGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetDynamicBaseFeePerGas(ctx))
	k.SetDynamicBaseFeePerGas(ctx, 1)
	require.Equal(t, sdk.NewDecFromInt(sdk.NewInt(1)), k.GetDynamicBaseFeePerGas(ctx))
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
			expectedBaseFee: 10000 + 10000*(keeper.MaxBaseFeeChange/2), // 6.25% increase
		},
		{
			name:            "Block gas usage 25%, base fee decreases",
			currentBaseFee:  10000,
			minimumFee:      10,
			blockGasUsed:    250000,
			blockGasLimit:   1000000,
			expectedBaseFee: 10000 - 10000*(keeper.MaxBaseFeeChange/2), // 6.25% decrease
		},
		{
			name:            "Block gas usage low, new base fee below minimum, set to minimum",
			currentBaseFee:  100,
			minimumFee:      90,
			blockGasUsed:    100000,
			blockGasLimit:   1000000,
			expectedBaseFee: 90, // Should not go below the minimum fee
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx = ctx.WithConsensusParams(&tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{MaxGas: int64(tc.blockGasLimit)},
			})
			k.SetDynamicBaseFeePerGas(ctx, uint64(tc.currentBaseFee))
			p := k.GetParams(ctx)
			p.MinimumFeePerGas = sdk.NewDec(int64(tc.minimumFee))
			k.SetParams(ctx, p)
			k.AdjustDynamicBaseFeePerGas(ctx, tc.blockGasUsed)
			expected := sdk.NewDecFromInt(sdk.NewInt(int64(tc.expectedBaseFee)))
			require.Equal(t, expected, k.GetDynamicBaseFeePerGas(ctx), "base fee did not match expected value")
		})
	}
}

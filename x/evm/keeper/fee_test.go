package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestBaseFeePerGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetNextBaseFeePerGas(ctx))
	require.True(t, k.GetNextBaseFeePerGas(ctx).LTE(k.GetMaximumFeePerGas(ctx)))
	originalbf := k.GetNextBaseFeePerGas(ctx)
	k.SetNextBaseFeePerGas(ctx, sdk.OneDec())
	require.Equal(t, sdk.NewDecFromInt(sdk.NewInt(1)), k.GetNextBaseFeePerGas(ctx))
	k.SetNextBaseFeePerGas(ctx, originalbf)
}

func TestAdjustBaseFeePerGas(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	testCases := []struct {
		name            string
		currentBaseFee  float64
		minimumFee      float64
		maximumFee      float64
		blockGasUsed    uint64
		blockGasLimit   uint64
		upwardAdj       sdk.Dec
		downwardAdj     sdk.Dec
		targetGasUsed   uint64
		expectedBaseFee uint64
	}{
		{
			name:            "Block gas usage exactly half of limit, 0% up, 0% down, no fee change",
			currentBaseFee:  100,
			minimumFee:      10,
			maximumFee:      1000,
			blockGasUsed:    500000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDec(0),
			downwardAdj:     sdk.NewDec(0),
			targetGasUsed:   500000,
			expectedBaseFee: 100,
		},
		{
			name:            "Block gas usage 50%, 50% up, 50% down, no fee change",
			currentBaseFee:  100,
			minimumFee:      10,
			maximumFee:      1000,
			blockGasUsed:    500000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 1),
			downwardAdj:     sdk.NewDecWithPrec(5, 1),
			targetGasUsed:   500000,
			expectedBaseFee: 100,
		},
		{
			name:            "Block gas usage 75%, 0% up, 0% down, base fee stays the same",
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      100000,
			blockGasUsed:    750000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDec(0),
			downwardAdj:     sdk.NewDec(0),
			targetGasUsed:   500000,
			expectedBaseFee: 10000,
		},
		{
			name:            "Block gas usage 25%, 0% up, 0% down, base fee stays the same",
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      100000,
			blockGasUsed:    250000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDec(0),
			downwardAdj:     sdk.NewDec(0),
			targetGasUsed:   500000,
			expectedBaseFee: 10000,
		},
		{
			name:            "Block gas usage 75%, 50% up, 0% down, base fee increases by 25%",
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      100000,
			blockGasUsed:    750000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 1),
			downwardAdj:     sdk.NewDec(0),
			targetGasUsed:   500000,
			expectedBaseFee: 12500,
		},
		{
			name:            "Block gas usage 25%, 0% up, 50% down, base fee decreases by 25%",
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      100000,
			blockGasUsed:    250000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDec(0),
			downwardAdj:     sdk.NewDecWithPrec(5, 1),
			targetGasUsed:   500000,
			expectedBaseFee: 7500,
		},
		{
			name:            "Block gas usage low, new base fee below minimum, set to minimum",
			currentBaseFee:  100,
			minimumFee:      99,
			maximumFee:      1000,
			blockGasUsed:    0,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 2),
			downwardAdj:     sdk.NewDecWithPrec(5, 2),
			targetGasUsed:   500000,
			expectedBaseFee: 99, // Should not go below the minimum fee
		},
		{
			name:            "Block gas usage high, new base fee above maximum, set to maximum",
			currentBaseFee:  999,
			minimumFee:      10,
			maximumFee:      1000,
			blockGasUsed:    1000000, // completely full block
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 1),
			downwardAdj:     sdk.NewDecWithPrec(5, 1),
			targetGasUsed:   500000,
			expectedBaseFee: 1000, // Should not go above the maximum fee
		},
		{
			name:            "target gas used is 0",
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      1000,
			blockGasUsed:    0,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 1),
			downwardAdj:     sdk.NewDecWithPrec(5, 1),
			targetGasUsed:   0,
			expectedBaseFee: 10000,
		},
		{
			name: "cap block gas used to block gas limit",
			// block gas used is 1.5x block gas limit
			currentBaseFee:  10000,
			minimumFee:      10,
			maximumFee:      100000,
			blockGasUsed:    1500000,
			blockGasLimit:   1000000,
			upwardAdj:       sdk.NewDecWithPrec(5, 1),
			downwardAdj:     sdk.NewDecWithPrec(5, 1),
			targetGasUsed:   500000,
			expectedBaseFee: 15000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx = ctx.WithConsensusParams(&tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{MaxGas: int64(tc.blockGasLimit)},
			})
			k.SetNextBaseFeePerGas(ctx, sdk.NewDecFromInt(sdk.NewInt(int64(tc.currentBaseFee))))
			p := k.GetParams(ctx)
			p.MinimumFeePerGas = sdk.NewDec(int64(tc.minimumFee))
			p.MaximumFeePerGas = sdk.NewDec(int64(tc.maximumFee))
			p.MaxDynamicBaseFeeUpwardAdjustment = tc.upwardAdj
			p.MaxDynamicBaseFeeDownwardAdjustment = tc.downwardAdj
			p.TargetGasUsedPerBlock = tc.targetGasUsed
			k.SetParams(ctx, p)
			k.AdjustDynamicBaseFeePerGas(ctx, tc.blockGasUsed)
			expected := sdk.NewDecFromInt(sdk.NewInt(int64(tc.expectedBaseFee)))
			gotNextBaseFee := k.GetNextBaseFeePerGas(ctx)
			require.Equal(t, expected, gotNextBaseFee, 0.001, "next block base fee did not match expected value")
		})
	}
}

func TestAdjustBaseFeePerGasWithUnlimitedMaxGas(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	ctx = ctx.WithConsensusParams(&tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{MaxGas: 0},
	})

	params := k.GetParams(ctx)
	params.MinimumFeePerGas = sdk.OneDec()
	params.MaximumFeePerGas = sdk.NewDec(1_000_000_000)
	params.MaxDynamicBaseFeeUpwardAdjustment = sdk.NewDecWithPrec(5, 1)
	params.MaxDynamicBaseFeeDownwardAdjustment = sdk.NewDecWithPrec(5, 1)
	params.TargetGasUsedPerBlock = 100
	k.SetParams(ctx, params)

	baseFee := sdk.NewDec(100)
	k.SetNextBaseFeePerGas(ctx, baseFee)

	blockGasUsed := uint64(105)
	newBaseFee := k.AdjustDynamicBaseFeePerGas(ctx, blockGasUsed)
	require.NotNil(t, newBaseFee)

	expectedUpward := sdk.MustNewDecFromStr("102.380952380952380952")
	require.Equal(t, expectedUpward, *newBaseFee)

	k.SetNextBaseFeePerGas(ctx, baseFee)
	lowerGasUsed := uint64(60)
	k.AdjustDynamicBaseFeePerGas(ctx, lowerGasUsed)
	expectedDownward := sdk.NewDec(80)
	require.Equal(t, expectedDownward, k.GetNextBaseFeePerGas(ctx))
}

func TestGetDynamicBaseFeePerGasWithNilMinFee(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()

	// Test case 1: When dynamic base fee doesn't exist and minimum fee is nil
	store := ctx.KVStore(k.GetStoreKey())
	store.Delete(types.BaseFeePerGasPrefix)

	// Clear the dynamic base fee from store
	fee := k.GetNextBaseFeePerGas(ctx)
	require.Equal(t, types.DefaultParams().MinimumFeePerGas, fee)
	require.False(t, fee.IsNil())

	// Test case 2: When dynamic base fee exists
	expectedFee := sdk.NewDec(100)
	k.SetNextBaseFeePerGas(ctx, expectedFee)

	fee = k.GetNextBaseFeePerGas(ctx)
	require.Equal(t, expectedFee, fee)
	require.False(t, fee.IsNil())
}

func TestGetPrevBlockBaseFeePerGasWithNilMinFee(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()

	// Test case 1: When dynamic base fee doesn't exist and minimum fee is nil
	store := ctx.KVStore(k.GetStoreKey())
	store.Delete(types.BaseFeePerGasPrefix)

	// Clear the dynamic base fee from store
	fee := k.GetNextBaseFeePerGas(ctx)
	require.Equal(t, types.DefaultParams().MinimumFeePerGas, fee)
	require.False(t, fee.IsNil())
}

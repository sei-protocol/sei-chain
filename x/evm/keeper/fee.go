package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var half = sdk.NewDec(1).Quo(sdk.NewDec(2)) // 0.5 as sdk.Dec

// eip-1559 adjustment using sdk.Dec
func (k *Keeper) AdjustDynamicBaseFeePerGas(ctx sdk.Context, blockGasUsed uint64) {
	if ctx.ConsensusParams() == nil || ctx.ConsensusParams().Block == nil {
		return
	}
	currentBaseFee := k.GetDynamicBaseFeePerGas(ctx)
	minimumFeePerGas := k.GetParams(ctx).MinimumFeePerGas
	blockGasLimit := sdk.NewDec(ctx.ConsensusParams().Block.MaxGas)
	blockGasUsedDec := sdk.NewDec(int64(blockGasUsed))

	blockFullness := blockGasUsedDec.Quo(blockGasLimit)

	var newBaseFee sdk.Dec
	if blockFullness.GT(half) {
		// upward adjustment
		adjustmentFactor := k.GetMaxDynamicBaseFeeUpwardAdjustment(ctx).Mul(blockFullness.Sub(half)).Quo(half)
		newBaseFee = currentBaseFee.Mul(sdk.NewDec(1).Add(adjustmentFactor))
	} else {
		// downward adjustment
		adjustmentFactor := k.GetMaxDynamicBaseFeeDownwardAdjustment(ctx).Mul(half.Sub(blockFullness)).Quo(half)
		newBaseFee = currentBaseFee.Mul(sdk.NewDec(1).Sub(adjustmentFactor))
	}

	// Ensure the new base fee is not lower than the minimum fee
	if newBaseFee.LT(minimumFeePerGas) {
		newBaseFee = minimumFeePerGas
	}

	// Set the new base fee for the next height
	k.SetDynamicBaseFeePerGas(ctx.WithBlockHeight(ctx.BlockHeight()+1), newBaseFee) // Convert sdk.Dec to uint64 using RoundInt64()
}

func (k *Keeper) GetDynamicBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	h := make([]byte, 8)
	binary.BigEndian.PutUint64(h, uint64(ctx.BlockHeight()))
	bz := k.PrefixStore(ctx, types.BaseFeePerGasPrefix).Get(h)
	if bz == nil {
		return k.GetMinimumFeePerGas(ctx)
	}

	d := sdk.Dec{}
	err := d.UnmarshalJSON(bz)
	if err != nil {
		panic(err)
	}
	return d
}

func (k *Keeper) SetDynamicBaseFeePerGas(ctx sdk.Context, baseFeePerGas sdk.Dec) {
	h := make([]byte, 8)
	binary.BigEndian.PutUint64(h, uint64(ctx.BlockHeight()))
	bz, err := baseFeePerGas.MarshalJSON()
	if err != nil {
		panic(err)
	}
	k.PrefixStore(ctx, types.BaseFeePerGasPrefix).Set(h, bz)
}

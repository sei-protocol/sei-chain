package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// modified eip-1559 adjustment using target gas used
func (k *Keeper) AdjustDynamicBaseFeePerGas(ctx sdk.Context, blockGasUsed uint64) *sdk.Dec {
	if ctx.ConsensusParams() == nil || ctx.ConsensusParams().Block == nil {
		return nil
	}
	prevBaseFee := k.GetPrevBlockBaseFeePerGas(ctx)
	// set the resulting base fee for block n-1 on block n
	k.SetDynamicBaseFeePerGas(ctx, prevBaseFee)
	targetGasUsed := sdk.NewDec(int64(k.GetTargetGasUsedPerBlock(ctx)))
	if targetGasUsed.IsZero() { // avoid division by zero
		return &prevBaseFee // return the previous base fee as is
	}
	minimumFeePerGas := k.GetParams(ctx).MinimumFeePerGas
	maximumFeePerGas := k.GetParams(ctx).MaximumFeePerGas
	blockGasLimit := sdk.NewDec(ctx.ConsensusParams().Block.MaxGas)
	blockGasUsedDec := sdk.NewDec(int64(blockGasUsed))

	// cap block gas used to block gas limit
	if blockGasUsedDec.GT(blockGasLimit) {
		blockGasUsedDec = blockGasLimit
	}

	var newBaseFee sdk.Dec
	if blockGasUsedDec.GT(targetGasUsed) {
		// upward adjustment
		numerator := blockGasUsedDec.Sub(targetGasUsed)
		denominator := blockGasLimit.Sub(targetGasUsed)
		percentageFull := numerator.Quo(denominator)
		adjustmentFactor := k.GetMaxDynamicBaseFeeUpwardAdjustment(ctx).Mul(percentageFull)
		newBaseFee = prevBaseFee.Mul(sdk.NewDec(1).Add(adjustmentFactor))
	} else {
		// downward adjustment
		numerator := targetGasUsed.Sub(blockGasUsedDec)
		denominator := targetGasUsed
		percentageEmpty := numerator.Quo(denominator)
		adjustmentFactor := k.GetMaxDynamicBaseFeeDownwardAdjustment(ctx).Mul(percentageEmpty)
		newBaseFee = prevBaseFee.Mul(sdk.NewDec(1).Sub(adjustmentFactor))
	}

	// Ensure the new base fee is not lower than the minimum fee
	if newBaseFee.LT(minimumFeePerGas) {
		newBaseFee = minimumFeePerGas
	}

	// Ensure the new base fee is not higher than the maximum fee
	if newBaseFee.GT(maximumFeePerGas) {
		newBaseFee = maximumFeePerGas
	}

	// Set the new base fee for the next height
	k.SetPrevBlockBaseFeePerGas(ctx, newBaseFee)

	return &newBaseFee
}

// dont have height be a prefix, just store the current base fee directly
func (k *Keeper) GetDynamicBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.BaseFeePerGasPrefix)
	if bz == nil {
		minFeePerGas := k.GetMinimumFeePerGas(ctx)
		if minFeePerGas.IsNil() {
			minFeePerGas = types.DefaultParams().MinimumFeePerGas
		}
		return minFeePerGas
	}
	d := sdk.Dec{}
	err := d.UnmarshalJSON(bz)
	if err != nil {
		panic(err)
	}
	return d
}

func (k *Keeper) SetDynamicBaseFeePerGas(ctx sdk.Context, baseFeePerGas sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	bz, err := baseFeePerGas.MarshalJSON()
	if err != nil {
		panic(err)
	}
	store.Set(types.BaseFeePerGasPrefix, bz)
}

func (k *Keeper) SetPrevBlockBaseFeePerGas(ctx sdk.Context, baseFeePerGas sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	bz, err := baseFeePerGas.MarshalJSON()
	if err != nil {
		panic(err)
	}
	store.Set(types.PrevBlockBaseFeePerGasPrefix, bz)
}

func (k *Keeper) GetPrevBlockBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.PrevBlockBaseFeePerGasPrefix)
	if bz == nil {
		minFeePerGas := k.GetMinimumFeePerGas(ctx)
		if minFeePerGas.IsNil() {
			minFeePerGas = types.DefaultParams().MinimumFeePerGas
		}
		return minFeePerGas
	}
	d := sdk.Dec{}
	err := d.UnmarshalJSON(bz)
	if err != nil {
		panic(err)
	}
	return d
}

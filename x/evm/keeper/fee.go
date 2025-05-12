package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// modified eip-1559 adjustment using target gas used
func (k *Keeper) AdjustDynamicBaseFeePerGas(ctx sdk.Context, blockGasUsed uint64) *sdk.Dec {
	if ctx.ConsensusParams() == nil || ctx.ConsensusParams().Block == nil {
		return nil
	}
	prevBaseFee := k.GetNextBaseFeePerGas(ctx)
	fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, prevBaseFee", prevBaseFee)
	k.SetCurrBaseFeePerGas(ctx, prevBaseFee)
	targetGasUsed := sdk.NewDec(int64(k.GetTargetGasUsedPerBlock(ctx)))
	if targetGasUsed.IsZero() { // avoid division by zero
		return &prevBaseFee // return the previous base fee as is
	}
	minimumFeePerGas := k.GetParams(ctx).MinimumFeePerGas
	maximumFeePerGas := k.GetParams(ctx).MaximumFeePerGas
	blockGasLimit := sdk.NewDec(ctx.ConsensusParams().Block.MaxGas)
	blockGasUsedDec := sdk.NewDec(int64(blockGasUsed))

	fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, targetGasUsed", targetGasUsed)
	fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, blockGasUsedDec", blockGasUsedDec)

	// cap block gas used to block gas limit
	if blockGasUsedDec.GT(blockGasLimit) {
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, blockGasUsedDec > blockGasLimit")
		blockGasUsedDec = blockGasLimit
	}

	var newBaseFee sdk.Dec
	if blockGasUsedDec.GT(targetGasUsed) {
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, blockGasUsedDec > targetGasUsed, in upward adjustment")
		// upward adjustment
		numerator := blockGasUsedDec.Sub(targetGasUsed)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, numerator", numerator)
		denominator := blockGasLimit.Sub(targetGasUsed)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, denominator", denominator)
		percentageFull := numerator.Quo(denominator)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, percentageFull", percentageFull)
		adjustmentFactor := k.GetMaxDynamicBaseFeeUpwardAdjustment(ctx).Mul(percentageFull)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, adjustmentFactor", adjustmentFactor)
		newBaseFee = prevBaseFee.Mul(sdk.NewDec(1).Add(adjustmentFactor))
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, newBaseFee", newBaseFee)
	} else {
		fmt.Println("[DEBUG] blockGasUsedDec <= targetGasUsed, in downward adjustment")
		// downward adjustment
		numerator := targetGasUsed.Sub(blockGasUsedDec)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, numerator", numerator)
		denominator := targetGasUsed
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, denominator", denominator)
		percentageEmpty := numerator.Quo(denominator)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, percentageEmpty", percentageEmpty)
		adjustmentFactor := k.GetMaxDynamicBaseFeeDownwardAdjustment(ctx).Mul(percentageEmpty)
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, adjustmentFactor", adjustmentFactor)
		newBaseFee = prevBaseFee.Mul(sdk.NewDec(1).Sub(adjustmentFactor))
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, newBaseFee", newBaseFee)
	}

	// Ensure the new base fee is not lower than the minimum fee
	if newBaseFee.LT(minimumFeePerGas) {
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, newBaseFee < minimumFeePerGas, setting to minimumFeePerGas")
		newBaseFee = minimumFeePerGas
	}

	// Ensure the new base fee is not higher than the maximum fee
	if newBaseFee.GT(maximumFeePerGas) {
		fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, newBaseFee > maximumFeePerGas, setting to maximumFeePerGas")
		newBaseFee = maximumFeePerGas
	}

	fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, setting newBaseFee", newBaseFee)
	// Set the new base fee for the next height
	k.SetNextBaseFeePerGas(ctx, newBaseFee)

	fmt.Println("[DEBUG] In AdjustDynamicBaseFeePerGas, returning newBaseFee", newBaseFee)
	return &newBaseFee
}

// dont have height be a prefix, just store the current base fee directly
func (k *Keeper) GetCurrBaseFeePerGas(ctx sdk.Context) sdk.Dec {
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

func (k *Keeper) SetCurrBaseFeePerGas(ctx sdk.Context, baseFeePerGas sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	bz, err := baseFeePerGas.MarshalJSON()
	if err != nil {
		panic(err)
	}
	store.Set(types.BaseFeePerGasPrefix, bz)
}

func (k *Keeper) SetNextBaseFeePerGas(ctx sdk.Context, baseFeePerGas sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	bz, err := baseFeePerGas.MarshalJSON()
	if err != nil {
		panic(err)
	}
	store.Set(types.NextBaseFeePerGasPrefix, bz)
}

func (k *Keeper) GetNextBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.NextBaseFeePerGasPrefix)
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

package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// TODO: make this a param
// const MaxBaseFeeChange = 0.125
// var MaxBaseFeeChange = sdk.NewIntWithDecimal(125, -3)
var MaxBaseFeeChange = sdk.NewDecWithPrec(125, 3) // 12.5%

// eip-1559 adjustment using sdk.Dec
func (k *Keeper) AdjustDynamicBaseFeePerGas(ctx sdk.Context, blockGasUsed uint64) {
	if ctx.ConsensusParams() == nil {
		return
	}

	// Use sdk.Dec for base fee and minimum fee
	currentBaseFee := k.GetDynamicBaseFeePerGas(ctx)      // Returns sdk.Dec
	minimumFeePerGas := k.GetParams(ctx).MinimumFeePerGas // Returns sdk.Dec

	// Convert block gas limit and gas used to sdk.Dec
	blockGasLimit := sdk.NewDec(ctx.ConsensusParams().Block.MaxGas)
	blockGasUsedDec := sdk.NewDec(int64(blockGasUsed))

	// Calculate block fullness as sdk.Dec
	blockFullness := blockGasUsedDec.Quo(blockGasLimit) // blockGasUsed / blockGasLimit

	// Calculate adjustment factor as sdk.Dec
	// adjustmentFactor := sdk.NewDecWithPrec(int64(MaxBaseFeeChange), 2) // MaxBaseFeeChange (e.g., 0.125) as Dec
	half := sdk.NewDec(1).Quo(sdk.NewDec(2)) // 0.5 as sdk.Dec
	adjustmentFactor := MaxBaseFeeChange.Mul(blockFullness.Sub(half)).Quo(half)

	// Calculate the new base fee
	newBaseFee := currentBaseFee.Mul(sdk.NewDec(1).Add(adjustmentFactor)) // currentBaseFee * (1 + adjustmentFactor)

	// Ensure the new base fee is not lower than the minimum fee
	if newBaseFee.LT(minimumFeePerGas) {
		newBaseFee = minimumFeePerGas
	}

	// Set the new base fee
	k.SetDynamicBaseFeePerGas(ctx, newBaseFee) // Convert sdk.Dec to uint64 using RoundInt64()
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

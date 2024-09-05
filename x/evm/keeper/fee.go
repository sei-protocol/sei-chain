package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// TODO: make this a param
const MaxBaseFeeChange = 0.125

// eip-1559 adjustment
func (k *Keeper) AdjustBaseFeePerGas(ctx sdk.Context, blockGasUsed uint64) {
	currentBaseFee := k.GetBaseFeePerGas(ctx).MustFloat64()
	minimumFeePerGas := k.GetParams(ctx).MinimumFeePerGas.MustFloat64()
	blockGasLimit := ctx.ConsensusParams().Block.MaxGas
	blockFullness := float64(blockGasUsed) / float64(blockGasLimit)
	adjustmentFactor := MaxBaseFeeChange * (blockFullness - 0.5) / 0.5 // range between -12.5% to 12.5%
	newBaseFee := float64(currentBaseFee) * (1 + adjustmentFactor)
	if newBaseFee < minimumFeePerGas {
		newBaseFee = minimumFeePerGas
	}
	k.SetBaseFeePerGas(ctx, uint64(newBaseFee))
}

func (k *Keeper) GetBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	h := make([]byte, 8)
	binary.BigEndian.PutUint64(h, uint64(ctx.BlockHeight()))
	bz := k.PrefixStore(ctx, types.BaseFeePerGasPrefix).Get(h)
	if bz == nil {
		return k.GetMinimumFeePerGas(ctx)
	}

	return sdk.NewDecFromInt(sdk.NewInt(int64(binary.BigEndian.Uint64(bz))))
}

func (k *Keeper) SetBaseFeePerGas(ctx sdk.Context, baseFeePerGas uint64) {
	h := make([]byte, 8)
	binary.BigEndian.PutUint64(h, uint64(ctx.BlockHeight()))
	fee := make([]byte, 8)
	binary.BigEndian.PutUint64(fee, baseFeePerGas)
	k.PrefixStore(ctx, types.BaseFeePerGasPrefix).Set(h, fee)
}

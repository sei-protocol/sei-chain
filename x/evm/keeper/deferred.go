package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetAllEVMTxDeferredInfo(ctx sdk.Context) (res []*types.DeferredInfo) {
	store := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.DeferredInfoPrefix)
	for txIdx, msg := range k.msgs {
		txRes := k.txResults[txIdx]
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(txIdx))
		val := store.Get(key)
		if val == nil {
			if msg == nil {
				continue
			}
			// this means the transaction got reverted during execution, either in ante handler
			// or due to a panic in msg server
			etx, _ := msg.AsTransaction()
			if etx == nil {
				panic("etx is nil for EVM DeferredInfo msg.AsTransaction(). This should never happen.")
			}
			if txRes.Code == 0 {
				ctx.Logger().Error(fmt.Sprintf("transaction %s has code 0 but no deferred info", etx.Hash().Hex()))
			}
			res = append(res, &types.DeferredInfo{
				TxIndex: uint32(txIdx),
				TxHash:  etx.Hash().Bytes(),
				Error:   txRes.Log,
			})
		} else {
			info := &types.DeferredInfo{}
			if err := info.Unmarshal(val); err != nil {
				// unable to unmarshal deferred info is serious, because it could cause
				// balance surplus to be mishandled and thus affect total supply
				panic(err)
			}
			res = append(res, info)
		}
	}
	return
}

func (k *Keeper) AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash, surplus sdk.Int) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(ctx.TxIndex()))
	val := &types.DeferredInfo{
		TxIndex: uint32(ctx.TxIndex()),
		TxBloom: bloom[:],
		TxHash:  txHash[:],
		Surplus: surplus,
	}
	bz, err := val.Marshal()
	if err != nil {
		// unable to marshal deferred info is serious, because it could cause
		// balance surplus to be mishandled and thus affect total supply
		panic(err)
	}
	prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.DeferredInfoPrefix).Set(key, bz)
}

func (k *Keeper) GetEVMTxDeferredInfo(ctx sdk.Context) (*types.DeferredInfo, bool) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(ctx.TxIndex()))
	val := &types.DeferredInfo{}
	bz := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.DeferredInfoPrefix).Get(key)
	if bz == nil {
		return nil, false
	}
	if err := val.Unmarshal(bz); err != nil {
		ctx.Logger().Error(fmt.Sprintf("failed to unmarshal EVM deferred info: %s", err))
		return nil, false
	}
	return val, true
}

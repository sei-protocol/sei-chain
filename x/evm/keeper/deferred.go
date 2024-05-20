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

func (k *Keeper) GetEVMTxDeferredInfo(ctx sdk.Context) (res []*types.DeferredInfo) {
	store := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.DeferredInfoPrefix)
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		key := binary.BigEndian.Uint64(iter.Key())
		if key >= uint64(len(k.txResults)) {
			ctx.Logger().Error(fmt.Sprintf("getting invalid tx index in EVM deferred info: %d, num of txs: %d", key, len(k.txResults)))
			continue
		}
		val := &types.DeferredInfo{}
		if err := val.Unmarshal(iter.Value()); err != nil {
			// unable to unmarshal deferred info is serious, because it could cause
			// balance surplus to be mishandled and thus affect total supply
			panic(err)
		}
		// cast is safe here because of the check above
		if k.txResults[int(key)].Code == 0 || val.Error != "" {
			res = append(res, val)
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

func (k *Keeper) AppendErrorToEvmTxDeferredInfo(ctx sdk.Context, txHash common.Hash, err string) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(ctx.TxIndex()))
	val := &types.DeferredInfo{
		TxIndex: uint32(ctx.TxIndex()),
		TxHash:  txHash[:],
		Error:   err,
	}
	bz, e := val.Marshal()
	if e != nil {
		// unable to marshal deferred info is serious, because it could cause
		// balance surplus to be mishandled and thus affect total supply
		panic(e)
	}
	prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.DeferredInfoPrefix).Set(key, bz)
}

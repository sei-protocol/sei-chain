package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetCode(ctx sdk.Context, addr common.Address) []byte {
	code := k.PrefixStore(ctx, types.CodeKeyPrefix).Get(addr[:])
	if len(code) == 0 {
		return nil
	}
	return code
}

func (k *Keeper) SetCode(ctx sdk.Context, addr common.Address, code []byte) {
	if code == nil {
		code = []byte{}
	}
	k.PrefixStore(ctx, types.CodeKeyPrefix).Set(addr[:], code)
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(code)))
	k.PrefixStore(ctx, types.CodeSizeKeyPrefix).Set(addr[:], length)
	h := crypto.Keccak256Hash(code)
	k.PrefixStore(ctx, types.CodeHashKeyPrefix).Set(addr[:], h[:])
	// no need to set address mapping anymore because direct casting will always be used
	k.InitAccount(ctx, addr)
}

func (k *Keeper) GetCodeHash(ctx sdk.Context, addr common.Address) common.Hash {
	store := k.PrefixStore(ctx, types.CodeHashKeyPrefix)
	bz := store.Get(addr[:])
	if bz == nil {
		return ethtypes.EmptyCodeHash
	}
	return common.BytesToHash(bz)
}

func (k *Keeper) GetCodeSize(ctx sdk.Context, addr common.Address) int {
	bz := k.PrefixStore(ctx, types.CodeSizeKeyPrefix).Get(addr[:])
	if bz == nil {
		return 0
	}
	return int(binary.BigEndian.Uint64(bz))
}

func (k *Keeper) IterateAllCode(ctx sdk.Context, cb func(addr common.Address, code []byte) bool) {
	iter := prefix.NewStore(ctx.KVStore(k.storeKey), types.CodeKeyPrefix).Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		evmAddr := common.BytesToAddress(iter.Key())
		if cb(evmAddr, iter.Value()) {
			break
		}
	}
}

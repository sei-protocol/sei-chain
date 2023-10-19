package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	val := k.PrefixStore(ctx, types.StateKey(addr)).Get(hash[:])
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (k *Keeper) SetState(ctx sdk.Context, addr common.Address, key common.Hash, val common.Hash) {
	k.PrefixStore(ctx, types.StateKey(addr)).Set(key[:], val[:])
}

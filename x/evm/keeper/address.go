package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) SetAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.EVMAddressToSeiAddressKey(evmAddress), seiAddress)
	store.Set(types.SeiAddressToEVMAddressKey(seiAddress), evmAddress[:])
}

func (k *Keeper) GetEVMAddress(ctx sdk.Context, seiAddress sdk.AccAddress) (string, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.SeiAddressToEVMAddressKey(seiAddress))
	if bz == nil {
		return "", false
	}
	return string(bz), true
}

func (k *Keeper) GetSeiAddress(ctx sdk.Context, evmAddress common.Address) (sdk.AccAddress, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.EVMAddressToSeiAddressKey(evmAddress))
	if bz == nil {
		return []byte{}, false
	}
	return bz, true
}

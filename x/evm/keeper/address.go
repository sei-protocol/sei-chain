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

func (k *Keeper) DeleteAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.EVMAddressToSeiAddressKey(evmAddress))
	store.Delete(types.SeiAddressToEVMAddressKey(seiAddress))
}

func (k *Keeper) GetEVMAddress(ctx sdk.Context, seiAddress sdk.AccAddress) (common.Address, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.SeiAddressToEVMAddressKey(seiAddress))
	addr := common.Address{}
	if bz == nil {
		return addr, false
	}
	copy(addr[:], bz)
	return addr, true
}

func (k *Keeper) GetSeiAddress(ctx sdk.Context, evmAddress common.Address) (sdk.AccAddress, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.EVMAddressToSeiAddressKey(evmAddress))
	if bz == nil {
		return []byte{}, false
	}
	return bz, true
}

package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) SetAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.EVMAddressToSeiAddressKey(evmAddress), seiAddress)
	store.Set(types.SeiAddressToEVMAddressKey(seiAddress), evmAddress[:])
	if !k.accountKeeper.HasAccount(ctx, seiAddress) {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, seiAddress))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAddressAssociated,
		sdk.NewAttribute(types.AttributeKeySeiAddress, seiAddress.String()),
		sdk.NewAttribute(types.AttributeKeyEvmAddress, evmAddress.Hex()),
	))
}

func (k *Keeper) DeleteAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.EVMAddressToSeiAddressKey(evmAddress))
	store.Delete(types.SeiAddressToEVMAddressKey(seiAddress))
}

func (k *Keeper) InitAccount(ctx sdk.Context, addr common.Address) {
	seiAddress := k.GetSeiAddress(ctx, addr)
	if !k.accountKeeper.HasAccount(ctx, seiAddress) {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, seiAddress))
	}
}

func (k *Keeper) GetEVMAddress(ctx sdk.Context, seiAddress sdk.AccAddress) common.Address {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.SeiAddressToEVMAddressKey(seiAddress))
	addr := common.Address{}
	if bz == nil {
		return common.BytesToAddress(seiAddress)
	}
	copy(addr[:], bz)
	return addr
}

func (k *Keeper) GetSeiAddress(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.EVMAddressToSeiAddressKey(evmAddress))
	if bz == nil {
		return evmAddress.Bytes()
	}
	return bz
}

func (k *Keeper) IterateSeiAddressMapping(ctx sdk.Context, cb func(evmAddr common.Address, seiAddr sdk.AccAddress) bool) {
	iter := prefix.NewStore(ctx.KVStore(k.storeKey), types.EVMAddressToSeiAddressKeyPrefix).Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		evmAddr := common.BytesToAddress(iter.Key())
		seiAddr := sdk.AccAddress(iter.Value())
		if cb(evmAddr, seiAddr) {
			break
		}
	}
}

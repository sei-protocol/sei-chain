package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) SetERC20NativePointer(ctx sdk.Context, token string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC20NativeKey(token), addr, native.CurrentVersion)
}

func (k *Keeper) GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool) {
	return k.GetPointerInfo(ctx, types.PointerERC20NativeKey(token))
}

func (k *Keeper) SetERC20CW20Pointer(ctx sdk.Context, cw20Address string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), addr, cw20.CurrentVersion)
}

func (k *Keeper) GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool) {
	return k.GetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address))
}

func (k *Keeper) SetERC721CW721Pointer(ctx sdk.Context, cw721Address string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), addr, cw20.CurrentVersion)
}

func (k *Keeper) GetERC721CW721Pointer(ctx sdk.Context, cw721Address string) (addr common.Address, version uint16, exists bool) {
	return k.GetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address))
}

func (k *Keeper) GetPointerInfo(ctx sdk.Context, pref []byte) (addr common.Address, version uint16, exists bool) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	iter := store.ReverseIterator(nil, nil)
	defer iter.Close()
	exists = iter.Valid()
	if !exists {
		return
	}
	version = binary.BigEndian.Uint16(iter.Key())
	addr = common.BytesToAddress(iter.Value())
	return
}

func (k *Keeper) SetPointerInfo(ctx sdk.Context, pref []byte, addr common.Address, version uint16) error {
	existingAddr, existingVersion, exists := k.GetPointerInfo(ctx, pref)
	if exists && existingVersion >= version {
		return fmt.Errorf("pointer at %s with version %d exists when trying to set pointer for version %d", existingAddr.Hex(), existingVersion, version)
	}
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	versionBz := make([]byte, 2)
	binary.BigEndian.PutUint16(versionBz, version)
	store.Set(versionBz, addr[:])
	return nil
}

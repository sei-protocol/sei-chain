package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) SetERC20NativePointer(ctx sdk.Context, token string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC20NativeKey(token), addr[:], native.CurrentVersion)
}

func (k *Keeper) SetERC20NativePointerWithVersion(ctx sdk.Context, token string, addr common.Address, version uint16) error {
	return k.SetPointerInfo(ctx, types.PointerERC20NativeKey(token), addr[:], version)
}

func (k *Keeper) GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC20NativeKey(token))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

func (k *Keeper) DeleteERC20NativePointer(ctx sdk.Context, token string, version uint16) {
	k.DeletePointerInfo(ctx, types.PointerERC20NativeKey(token), version)
}

func (k *Keeper) SetERC20CW20Pointer(ctx sdk.Context, cw20Address string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), addr[:], cw20.CurrentVersion)
}

func (k *Keeper) SetERC20CW20PointerWithVersion(ctx sdk.Context, cw20Address string, addr common.Address, version uint16) error {
	return k.SetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), addr[:], version)
}

func (k *Keeper) GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

func (k *Keeper) DeleteERC20CW20Pointer(ctx sdk.Context, cw20Address string, version uint16) {
	k.DeletePointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), version)
}

func (k *Keeper) SetERC721CW721Pointer(ctx sdk.Context, cw721Address string, addr common.Address) error {
	return k.SetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), addr[:], cw721.CurrentVersion)
}

func (k *Keeper) SetERC721CW721PointerWithVersion(ctx sdk.Context, cw721Address string, addr common.Address, version uint16) error {
	return k.SetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), addr[:], version)
}

func (k *Keeper) GetERC721CW721Pointer(ctx sdk.Context, cw721Address string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

func (k *Keeper) DeleteERC721CW721Pointer(ctx sdk.Context, cw721Address string, version uint16) {
	k.DeletePointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), version)
}

func (k *Keeper) SetCW20ERC20Pointer(ctx sdk.Context, erc20Address common.Address, addr string) error {
	return k.SetPointerInfo(ctx, types.PointerCW20ERC20Key(erc20Address), []byte(addr), erc20.CurrentVersion)
}

func (k *Keeper) GetCW20ERC20Pointer(ctx sdk.Context, erc20Address common.Address) (addr sdk.AccAddress, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerCW20ERC20Key(erc20Address))
	if exists {
		addr = sdk.MustAccAddressFromBech32(string(addrBz))
	}
	return
}

func (k *Keeper) SetCW721ERC721Pointer(ctx sdk.Context, erc721Address common.Address, addr string) error {
	return k.SetPointerInfo(ctx, types.PointerCW721ERC721Key(erc721Address), []byte(addr), erc721.CurrentVersion)
}

func (k *Keeper) GetCW721ERC721Pointer(ctx sdk.Context, erc721Address common.Address) (addr sdk.AccAddress, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerCW721ERC721Key(erc721Address))
	if exists {
		addr = sdk.MustAccAddressFromBech32(string(addrBz))
	}
	return
}

func (k *Keeper) GetPointerInfo(ctx sdk.Context, pref []byte) (addr []byte, version uint16, exists bool) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	iter := store.ReverseIterator(nil, nil)
	defer iter.Close()
	exists = iter.Valid()
	if !exists {
		return
	}
	version = binary.BigEndian.Uint16(iter.Key())
	addr = iter.Value()
	return
}

func (k *Keeper) SetPointerInfo(ctx sdk.Context, pref []byte, addr []byte, version uint16) error {
	existingAddr, existingVersion, exists := k.GetPointerInfo(ctx, pref)
	if exists && existingVersion >= version {
		return fmt.Errorf("pointer at %s with version %d exists when trying to set pointer for version %d", string(existingAddr), existingVersion, version)
	}
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	versionBz := make([]byte, 2)
	binary.BigEndian.PutUint16(versionBz, version)
	store.Set(versionBz, addr)
	return nil
}

func (k *Keeper) DeletePointerInfo(ctx sdk.Context, pref []byte, version uint16) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	versionBz := make([]byte, 2)
	binary.BigEndian.PutUint16(versionBz, version)
	store.Delete(versionBz)
}

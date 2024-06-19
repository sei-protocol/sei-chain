package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	artifactsutils "github.com/sei-protocol/sei-chain/x/evm/artifacts/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var ErrorPointerToPointerNotAllowed = sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "cannot create a pointer to a pointer")

// ERC20 -> Native Token
func (k *Keeper) SetERC20NativePointer(ctx sdk.Context, token string, addr common.Address) error {
	return k.SetERC20NativePointerWithVersion(ctx, token, addr, native.CurrentVersion)
}

// ERC20 -> Native Token
func (k *Keeper) SetERC20NativePointerWithVersion(ctx sdk.Context, token string, addr common.Address, version uint16) error {
	if k.cwAddressIsPointer(ctx, token) {
		return ErrorPointerToPointerNotAllowed
	}
	err := k.setPointerInfo(ctx, types.PointerERC20NativeKey(token), addr[:], version)
	if err != nil {
		return err
	}
	return k.setPointerInfo(ctx, types.PointerReverseRegistryKey(addr), []byte(token), version)
}

// ERC20 -> Native Token
func (k *Keeper) GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC20NativeKey(token))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

// ERC20 -> Native Token
func (k *Keeper) DeleteERC20NativePointer(ctx sdk.Context, token string, version uint16) {
	addr, _, exists := k.GetERC20NativePointer(ctx, token)
	if exists {
		k.deletePointerInfo(ctx, types.PointerERC20NativeKey(token), version)
		k.deletePointerInfo(ctx, types.PointerReverseRegistryKey(addr), version)
	}
}

// ERC20 -> CW20
func (k *Keeper) SetERC20CW20Pointer(ctx sdk.Context, cw20Address string, addr common.Address) error {
	return k.SetERC20CW20PointerWithVersion(ctx, cw20Address, addr, cw20.CurrentVersion(ctx))
}

// ERC20 -> CW20
func (k *Keeper) SetERC20CW20PointerWithVersion(ctx sdk.Context, cw20Address string, addr common.Address, version uint16) error {
	if k.cwAddressIsPointer(ctx, cw20Address) {
		return ErrorPointerToPointerNotAllowed
	}
	err := k.setPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), addr[:], version)
	if err != nil {
		return err
	}
	return k.setPointerInfo(ctx, types.PointerReverseRegistryKey(addr), []byte(cw20Address), version)
}

// ERC20 -> CW20
func (k *Keeper) GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC20CW20Key(cw20Address))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

// ERC20 -> CW20
func (k *Keeper) DeleteERC20CW20Pointer(ctx sdk.Context, cw20Address string, version uint16) {
	addr, _, exists := k.GetERC20CW20Pointer(ctx, cw20Address)
	if exists {
		k.deletePointerInfo(ctx, types.PointerERC20CW20Key(cw20Address), version)
		k.deletePointerInfo(ctx, types.PointerReverseRegistryKey(addr), version)
	}
}

// ERC721 -> CW721
func (k *Keeper) SetERC721CW721Pointer(ctx sdk.Context, cw721Address string, addr common.Address) error {
	return k.SetERC721CW721PointerWithVersion(ctx, cw721Address, addr, cw721.CurrentVersion)
}

// ERC721 -> CW721
func (k *Keeper) SetERC721CW721PointerWithVersion(ctx sdk.Context, cw721Address string, addr common.Address, version uint16) error {
	if k.cwAddressIsPointer(ctx, cw721Address) {
		return ErrorPointerToPointerNotAllowed
	}
	err := k.setPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), addr[:], version)
	if err != nil {
		return err
	}
	return k.setPointerInfo(ctx, types.PointerReverseRegistryKey(addr), []byte(cw721Address), version)
}

// ERC721 -> CW721
func (k *Keeper) GetERC721CW721Pointer(ctx sdk.Context, cw721Address string) (addr common.Address, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerERC721CW721Key(cw721Address))
	if exists {
		addr = common.BytesToAddress(addrBz)
	}
	return
}

// ERC721 -> CW721
func (k *Keeper) DeleteERC721CW721Pointer(ctx sdk.Context, cw721Address string, version uint16) {
	addr, _, exists := k.GetERC721CW721Pointer(ctx, cw721Address)
	if exists {
		k.deletePointerInfo(ctx, types.PointerERC721CW721Key(cw721Address), version)
		k.deletePointerInfo(ctx, types.PointerReverseRegistryKey(addr), version)
	}
}

// CW20 -> ERC20
func (k *Keeper) SetCW20ERC20Pointer(ctx sdk.Context, erc20Address common.Address, addr string) error {
	return k.SetCW20ERC20PointerWithVersion(ctx, erc20Address, addr, erc20.CurrentVersion)
}

// CW20 -> ERC20
func (k *Keeper) SetCW20ERC20PointerWithVersion(ctx sdk.Context, erc20Address common.Address, addr string, version uint16) error {
	if k.evmAddressIsPointer(ctx, erc20Address) {
		return ErrorPointerToPointerNotAllowed
	}
	err := k.setPointerInfo(ctx, types.PointerCW20ERC20Key(erc20Address), []byte(addr), version)
	if err != nil {
		return err
	}
	return k.setPointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(addr))), erc20Address[:], version)
}

// CW20 -> ERC20
func (k *Keeper) GetCW20ERC20Pointer(ctx sdk.Context, erc20Address common.Address) (addr sdk.AccAddress, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerCW20ERC20Key(erc20Address))
	if exists {
		addr = sdk.MustAccAddressFromBech32(string(addrBz))
	}
	return
}

// CW20 -> ERC20
func (k *Keeper) DeleteCW20ERC20Pointer(ctx sdk.Context, erc20Address common.Address, version uint16) {
	addr, _, exists := k.GetCW20ERC20Pointer(ctx, erc20Address)
	if exists {
		k.deletePointerInfo(ctx, types.PointerCW20ERC20Key(erc20Address), version)
		k.deletePointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(addr.String()))), version)
	}
}

func (k *Keeper) evmAddressIsPointer(ctx sdk.Context, addr common.Address) bool {
	_, _, exists := k.GetPointerInfo(ctx, types.PointerReverseRegistryKey(addr))
	return exists
}

func (k *Keeper) cwAddressIsPointer(ctx sdk.Context, addr string) bool {
	_, _, exists := k.GetPointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(addr))))
	return exists
}

// CW721 -> ERC721
func (k *Keeper) SetCW721ERC721Pointer(ctx sdk.Context, erc721Address common.Address, addr string) error {
	return k.SetCW721ERC721PointerWithVersion(ctx, erc721Address, addr, erc721.CurrentVersion)
}

// CW721 -> ERC721
func (k *Keeper) SetCW721ERC721PointerWithVersion(ctx sdk.Context, erc721Address common.Address, addr string, version uint16) error {
	if k.evmAddressIsPointer(ctx, erc721Address) {
		return ErrorPointerToPointerNotAllowed
	}
	err := k.setPointerInfo(ctx, types.PointerCW721ERC721Key(erc721Address), []byte(addr), version)
	if err != nil {
		return err
	}
	return k.setPointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(addr))), erc721Address[:], version)
}

// CW721 -> ERC721
func (k *Keeper) GetCW721ERC721Pointer(ctx sdk.Context, erc721Address common.Address) (addr sdk.AccAddress, version uint16, exists bool) {
	addrBz, version, exists := k.GetPointerInfo(ctx, types.PointerCW721ERC721Key(erc721Address))
	if exists {
		addr = sdk.MustAccAddressFromBech32(string(addrBz))
	}
	return
}

// CW721 -> ERC721
func (k *Keeper) DeleteCW721ERC721Pointer(ctx sdk.Context, erc721Address common.Address, version uint16) {
	addr, _, exists := k.GetCW721ERC721Pointer(ctx, erc721Address)
	if exists {
		k.deletePointerInfo(ctx, types.PointerCW721ERC721Key(erc721Address), version)
		k.deletePointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(addr.String()))), version)
	}
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

func (k *Keeper) setPointerInfo(ctx sdk.Context, pref []byte, addr []byte, version uint16) error {
	existingAddr, existingVersion, exists := k.GetPointerInfo(ctx, pref)
	if exists && existingVersion >= version {
		return fmt.Errorf("pointer at %X with version %d exists when trying to set pointer for version %d", string(existingAddr), existingVersion, version)
	}
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	versionBz := make([]byte, 2)
	binary.BigEndian.PutUint16(versionBz, version)
	store.Set(versionBz, addr)
	return nil
}

func (k *Keeper) deletePointerInfo(ctx sdk.Context, pref []byte, version uint16) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), pref)
	versionBz := make([]byte, 2)
	binary.BigEndian.PutUint16(versionBz, version)
	store.Delete(versionBz)
}

func (k *Keeper) GetStoredPointerCodeID(ctx sdk.Context, pointerType types.PointerType) uint64 {
	store := k.PrefixStore(ctx, types.PointerCWCodePrefix)
	var versionBz []byte
	switch pointerType {
	case types.PointerType_ERC20:
		store = prefix.NewStore(store, types.PointerCW20ERC20Prefix)
		versionBz = artifactsutils.GetVersionBz(erc20.CurrentVersion)
	case types.PointerType_ERC721:
		store = prefix.NewStore(store, types.PointerCW721ERC721Prefix)
		versionBz = artifactsutils.GetVersionBz(erc721.CurrentVersion)
	default:
		return 0
	}
	bz := store.Get(versionBz)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

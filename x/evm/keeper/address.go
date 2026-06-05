package keeper

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// SetAddressMapping binds a Sei address to an EVM address bidirectionally.
//
// The mapping is maintained as two keys (forward: sei→evm, reverse: evm→sei).
// When either side is being rebound to a new partner, we must also clear the
// OLD partner's stale half of the mapping — otherwise the old address would
// continue to resolve to its former partner via one direction while the new
// binding wins in the other, leaving the index permanently inconsistent.
//
// The defensive `bytes.Equal` / `Equals` checks before deletion guard against
// already-inconsistent state: we only delete the stale half if it still
// points back to the address we're rebinding away from.
func (k *Keeper) SetAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	evmKey := types.EVMAddressToSeiAddressKey(evmAddress)
	seiKey := types.SeiAddressToEVMAddressKey(seiAddress)

	// If evmAddress was previously bound to a different Sei address,
	// clear that Sei address's stale forward mapping.
	if prevSei := store.Get(evmKey); prevSei != nil && !sdk.AccAddress(prevSei).Equals(seiAddress) {
		prevSeiKey := types.SeiAddressToEVMAddressKey(prevSei)
		if bytes.Equal(store.Get(prevSeiKey), evmAddress[:]) {
			store.Delete(prevSeiKey)
		}
	}
	// If seiAddress was previously bound to a different EVM address,
	// clear that EVM address's stale reverse mapping.
	if prevEvm := store.Get(seiKey); prevEvm != nil && !bytes.Equal(prevEvm, evmAddress[:]) {
		prevEvmKey := types.EVMAddressToSeiAddressKey(common.BytesToAddress(prevEvm))
		if prevReverseSei := store.Get(prevEvmKey); prevReverseSei != nil && sdk.AccAddress(prevReverseSei).Equals(seiAddress) {
			store.Delete(prevEvmKey)
		}
	}

	store.Set(evmKey, seiAddress)
	store.Set(seiKey, evmAddress[:])
	if !k.accountKeeper.HasAccount(ctx, seiAddress) {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, seiAddress))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAddressAssociated,
		sdk.NewAttribute(types.AttributeKeySeiAddress, seiAddress.String()),
		sdk.NewAttribute(types.AttributeKeyEvmAddress, evmAddress.Hex()),
	))
}

// DeleteAddressMapping removes both directions of the binding.
// The caller is responsible for passing a (sei, evm) pair that was actually
// bound together — passing a mismatched pair will corrupt the indexes.
func (k *Keeper) DeleteAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.EVMAddressToSeiAddressKey(evmAddress))
	store.Delete(types.SeiAddressToEVMAddressKey(seiAddress))
}

func (k *Keeper) GetEVMAddress(ctx sdk.Context, seiAddress sdk.AccAddress) (common.Address, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.SeiAddressToEVMAddressKey(seiAddress))
	if bz == nil {
		return common.Address{}, false
	}
	var addr common.Address
	copy(addr[:], bz)
	return addr, true
}

func (k *Keeper) GetEVMAddressOrDefault(ctx sdk.Context, seiAddress sdk.AccAddress) common.Address {
	if addr, ok := k.GetEVMAddress(ctx, seiAddress); ok {
		return addr
	}
	return common.BytesToAddress(seiAddress)
}

func (k *Keeper) GetSeiAddress(ctx sdk.Context, evmAddress common.Address) (sdk.AccAddress, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.EVMAddressToSeiAddressKey(evmAddress))
	if bz == nil {
		return nil, false
	}
	return bz, true
}

func (k *Keeper) GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress {
	if addr, ok := k.GetSeiAddress(ctx, evmAddress); ok {
		return addr
	}
	return sdk.AccAddress(evmAddress[:])
}

// IterateSeiAddressMapping walks every (evm, sei) pair in the address index.
// The callback returns `stop=true` to halt iteration early.
func (k *Keeper) IterateSeiAddressMapping(ctx sdk.Context, cb func(evmAddr common.Address, seiAddr sdk.AccAddress) (stop bool)) {
	iter := prefix.NewStore(ctx.KVStore(k.storeKey), types.EVMAddressToSeiAddressKeyPrefix).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		evmAddr := common.BytesToAddress(iter.Key())
		seiAddr := sdk.AccAddress(iter.Value())
		if cb(evmAddr, seiAddr) {
			return
		}
	}
}

// CanAddressReceive reports whether bank may credit `addr`.
//
// EVM and Sei addresses are both 20 bytes, so any Sei address can be
// interpreted as the byte-cast of an EVM address. That cast is permitted
// as a recipient EXCEPT when the underlying EVM address has already been
// associated with a real, pubkey-derived Sei address — in that case, funds
// sent to the cast form would be stranded outside the real account, so we
// reject it. The carve-out for `associatedAddr.Equals(addr)` permits EVM
// contracts and other cases where the cast IS the canonical Sei side.
func (k *Keeper) CanAddressReceive(ctx sdk.Context, addr sdk.AccAddress) bool {
	directCast := common.BytesToAddress(addr)
	associatedAddr, isAssociated := k.GetSeiAddress(ctx, directCast)
	if !isAssociated {
		return true // not a cast form, or a cast that hasn't been associated yet
	}
	return associatedAddr.Equals(addr)
}

type EvmAddressHandler struct {
	evmKeeper *Keeper
}

func NewEvmAddressHandler(evmKeeper *Keeper) EvmAddressHandler {
	return EvmAddressHandler{evmKeeper: evmKeeper}
}

// GetSeiAddressFromString resolves a string-encoded address (either 0x-hex
// or bech32) to its Sei address form. Hex inputs go through the keeper's
// associated/cast resolution; bech32 inputs are returned as-is.
func (h EvmAddressHandler) GetSeiAddressFromString(ctx sdk.Context, address string) (sdk.AccAddress, error) {
	if common.IsHexAddress(address) {
		return h.evmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(address)), nil
	}
	return sdk.AccAddressFromBech32(address)
}

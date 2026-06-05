package keeper

import (
	"encoding/hex"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// sto558Trace toggles verbose tracing of EVM↔Sei address-mapping reads/writes.
// Enable via env var STO558_TRACE=1 on the running node; everything is logged
// at the keeper's standard logger (INFO level) so kubectl logs can pick it up.
// Default off so production runs are unaffected.
var sto558Trace = os.Getenv("STO558_TRACE") == "1"

func (k *Keeper) SetAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	store := ctx.KVStore(k.storeKey)
	evmToSeiKey := types.EVMAddressToSeiAddressKey(evmAddress)
	seiToEvmKey := types.SeiAddressToEVMAddressKey(seiAddress)
	store.Set(evmToSeiKey, seiAddress)
	store.Set(seiToEvmKey, evmAddress[:])
	created := false
	if !k.accountKeeper.HasAccount(ctx, seiAddress) {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, seiAddress))
		created = true
	}
	if sto558Trace {
		logger.Info("STO558 SetAddressMapping",
			"height", ctx.BlockHeight(),
			"seiAddr", seiAddress.String(),
			"evmAddr", evmAddress.Hex(),
			"sei2evmKey", hex.EncodeToString(seiToEvmKey),
			"evm2seiKey", hex.EncodeToString(evmToSeiKey),
			"createdAccount", created,
		)
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

func (k *Keeper) GetEVMAddress(ctx sdk.Context, seiAddress sdk.AccAddress) (common.Address, bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.SeiAddressToEVMAddressKey(seiAddress)
	bz := store.Get(key)
	addr := common.Address{}
	if bz == nil {
		if sto558Trace {
			logger.Info("STO558 GetEVMAddress miss",
				"height", ctx.BlockHeight(),
				"seiAddr", seiAddress.String(),
				"sei2evmKey", hex.EncodeToString(key),
			)
		}
		return addr, false
	}
	copy(addr[:], bz)
	if sto558Trace {
		logger.Info("STO558 GetEVMAddress hit",
			"height", ctx.BlockHeight(),
			"seiAddr", seiAddress.String(),
			"sei2evmKey", hex.EncodeToString(key),
			"returnedEvmAddr", addr.Hex(),
			"returnedBzLen", len(bz),
			"returnedBzHex", hex.EncodeToString(bz),
		)
	}
	return addr, true
}

func (k *Keeper) GetEVMAddressOrDefault(ctx sdk.Context, seiAddress sdk.AccAddress) common.Address {
	addr, ok := k.GetEVMAddress(ctx, seiAddress)
	if ok {
		return addr
	}
	return common.BytesToAddress(seiAddress)
}

func (k *Keeper) GetSeiAddress(ctx sdk.Context, evmAddress common.Address) (sdk.AccAddress, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.EVMAddressToSeiAddressKey(evmAddress))
	if bz == nil {
		return []byte{}, false
	}
	return bz, true
}

func (k *Keeper) GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress {
	addr, ok := k.GetSeiAddress(ctx, evmAddress)
	if ok {
		return addr
	}
	return sdk.AccAddress(evmAddress[:])
}

func (k *Keeper) IterateSeiAddressMapping(ctx sdk.Context, cb func(evmAddr common.Address, seiAddr sdk.AccAddress) bool) {
	iter := prefix.NewStore(ctx.KVStore(k.storeKey), types.EVMAddressToSeiAddressKeyPrefix).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		evmAddr := common.BytesToAddress(iter.Key())
		seiAddr := sdk.AccAddress(iter.Value())
		if cb(evmAddr, seiAddr) {
			break
		}
	}
}

// A sdk.AccAddress may not receive funds from bank if it's the result of direct-casting
// from an EVM address AND the originating EVM address has already been associated with
// a true (i.e. derived from the same pubkey) sdk.AccAddress.
func (k *Keeper) CanAddressReceive(ctx sdk.Context, addr sdk.AccAddress) bool {
	directCast := common.BytesToAddress(addr) // casting goes both directions since both address formats have 20 bytes
	associatedAddr, isAssociated := k.GetSeiAddress(ctx, directCast)
	// if the associated address is the cast address itself, allow the address to receive (e.g. EVM contract addresses)
	return associatedAddr.Equals(addr) || !isAssociated // this means it's either a cast address that's not associated yet, or not a cast address at all.
}

type EvmAddressHandler struct {
	evmKeeper *Keeper
}

func NewEvmAddressHandler(evmKeeper *Keeper) EvmAddressHandler {
	return EvmAddressHandler{evmKeeper: evmKeeper}
}

func (h EvmAddressHandler) GetSeiAddressFromString(ctx sdk.Context, address string) (sdk.AccAddress, error) {
	if common.IsHexAddress(address) {
		parsedAddress := common.HexToAddress(address)
		return h.evmKeeper.GetSeiAddressOrDefault(ctx, parsedAddress), nil
	}
	parsedAddress, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return nil, err
	}
	return parsedAddress, nil
}

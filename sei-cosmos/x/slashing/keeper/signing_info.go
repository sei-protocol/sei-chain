package keeper

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
)

const UINT_64_NUM_BITS = 64

// GetValidatorSigningInfo retruns the ValidatorSigningInfo for a specific validator
// ConsAddress
func (k Keeper) GetValidatorSigningInfo(ctx sdk.Context, address sdk.ConsAddress) (info types.ValidatorSigningInfo, found bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ValidatorSigningInfoKey(address))
	if bz == nil {
		found = false
		return
	}
	k.cdc.MustUnmarshal(bz, &info)
	found = true
	return
}

// HasValidatorSigningInfo returns if a given validator has signing information
// persited.
func (k Keeper) HasValidatorSigningInfo(ctx sdk.Context, consAddr sdk.ConsAddress) bool {
	_, ok := k.GetValidatorSigningInfo(ctx, consAddr)
	return ok
}

// SetValidatorSigningInfo sets the validator signing info to a consensus address key
func (k Keeper) SetValidatorSigningInfo(ctx sdk.Context, address sdk.ConsAddress, info types.ValidatorSigningInfo) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&info)
	store.Set(types.ValidatorSigningInfoKey(address), bz)
}

// IterateValidatorSigningInfos iterates over the stored ValidatorSigningInfo
func (k Keeper) IterateValidatorSigningInfos(ctx sdk.Context,
	handler func(address sdk.ConsAddress, info types.ValidatorSigningInfo) (stop bool)) {

	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.ValidatorSigningInfoKeyPrefix)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		address := types.ValidatorSigningInfoAddress(iter.Key())
		var info types.ValidatorSigningInfo
		k.cdc.MustUnmarshal(iter.Value(), &info)
		if handler(address, info) {
			break
		}
	}
}

// GetValidatorMissedBlockArray gets the missed blocks array
func (k Keeper) GetValidatorMissedBlocks(ctx sdk.Context, address sdk.ConsAddress) (missedInfo types.ValidatorMissedBlockArray, found bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ValidatorMissedBlockBitArrayKey(address))
	if bz == nil {
		found = false
		return
	}
	k.cdc.MustUnmarshal(bz, &missedInfo)
	found = true
	return
}

// SetValidatorMissedBlockArray sets the missed blocks array
func (k Keeper) SetValidatorMissedBlocks(ctx sdk.Context, address sdk.ConsAddress, missedInfo types.ValidatorMissedBlockArray) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&missedInfo)
	store.Set(types.ValidatorMissedBlockBitArrayKey(address), bz)
}

// Get a boolean representing whether a validator missed a block with a specific index offset
func (k Keeper) GetBooleanFromBitGroups(bitGroupArray []uint64, index int64) bool {
	// convert the index into indexKey + indexShift
	indexKey := index / UINT_64_NUM_BITS
	indexShift := index % UINT_64_NUM_BITS
	if indexKey >= int64(len(bitGroupArray)) {
		return false
	}
	// shift 1 by the indexShift value to generate bit mask (to index into the bitGroup)
	indexMask := uint64(1) << indexShift
	// apply the mask and if the value at that `indexShift` is 1 (indicating miss), then the value would be non-zero
	missed := (bitGroupArray[indexKey] & indexMask) != 0
	return missed
}

// Set the missed value for whether a validator missed a block
func (k Keeper) SetBooleanInBitGroups(bitGroupArray []uint64, index int64, missed bool) []uint64 {
	// convert the index into indexKey + indexShift
	indexKey := index / UINT_64_NUM_BITS
	indexShift := index % UINT_64_NUM_BITS
	indexMask := uint64(1) << indexShift
	if missed {
		// set bit to 1 by ORing with the specific position as 1
		bitGroupArray[indexKey] |= indexMask
	} else {
		// set bit to 0 by AND NOTing with the specific position as 1
		bitGroupArray[indexKey] &^= indexMask
	}
	// set after updating the position
	return bitGroupArray
}

func (k Keeper) ParseBitGroupsToBoolArray(bitGroups []uint64, window int64) []bool {
	boolArray := make([]bool, window)

	for i := int64(0); i < window; i++ {
		boolArray[i] = k.GetBooleanFromBitGroups(bitGroups, i)
	}
	return boolArray
}

func (k Keeper) ParseBoolArrayToBitGroups(boolArray []bool) []uint64 {
	arrLen := (len(boolArray) + UINT_64_NUM_BITS - 1) / UINT_64_NUM_BITS
	bitGroups := make([]uint64, arrLen)

	for index, boolVal := range boolArray {
		bitGroups = k.SetBooleanInBitGroups(bitGroups, int64(index), boolVal)
	}

	return bitGroups
}

// JailUntil attempts to set a validator's JailedUntil attribute in its signing
// info. It will panic if the signing info does not exist for the validator.
func (k Keeper) JailUntil(ctx sdk.Context, consAddr sdk.ConsAddress, jailTime time.Time) {
	signInfo, ok := k.GetValidatorSigningInfo(ctx, consAddr)
	if !ok {
		panic("cannot jail validator that does not have any signing information")
	}

	signInfo.JailedUntil = jailTime
	k.SetValidatorSigningInfo(ctx, consAddr, signInfo)
}

// Tombstone attempts to tombstone a validator. It will panic if signing info for
// the given validator does not exist.
func (k Keeper) Tombstone(ctx sdk.Context, consAddr sdk.ConsAddress) {
	signInfo, ok := k.GetValidatorSigningInfo(ctx, consAddr)
	if !ok {
		panic("cannot tombstone validator that does not have any signing information")
	}

	if signInfo.Tombstoned {
		panic("cannot tombstone validator that is already tombstoned")
	}

	signInfo.Tombstoned = true
	k.SetValidatorSigningInfo(ctx, consAddr, signInfo)
}

// IsTombstoned returns if a given validator by consensus address is tombstoned.
func (k Keeper) IsTombstoned(ctx sdk.Context, consAddr sdk.ConsAddress) bool {
	signInfo, ok := k.GetValidatorSigningInfo(ctx, consAddr)
	if !ok {
		return false
	}

	return signInfo.Tombstoned
}

// clearValidatorMissedBlockBitArray deletes every instance of ValidatorMissedBlockBitArray in the store
func (k Keeper) ClearValidatorMissedBlockBitArray(ctx sdk.Context, address sdk.ConsAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.ValidatorMissedBlockBitArrayKey(address))
}

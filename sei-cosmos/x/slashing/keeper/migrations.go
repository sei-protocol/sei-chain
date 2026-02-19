package keeper

import (
	"encoding/binary"
	"fmt"
	"sort"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"

	gogotypes "github.com/gogo/protobuf/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	return nil
}

// Migrate2to3 migrates from version 2 to 3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)
	valMissedMap := make(map[string]types.ValidatorMissedBlockArrayLegacyMissedHeights)

	ctx.Logger().Info("Migrating Signing Info")
	signInfoIter := sdk.KVStorePrefixIterator(store, types.ValidatorSigningInfoKeyPrefix)
	var newSignInfoKeys [][]byte
	var newSignInfoVals []types.ValidatorSigningInfoLegacyMissedHeights

	for ; signInfoIter.Valid(); signInfoIter.Next() {
		ctx.Logger().Info(fmt.Sprintf("Migrating Signing Info for key: %v\n", signInfoIter.Key()))
		var oldInfo types.ValidatorSigningInfo
		m.keeper.cdc.MustUnmarshal(signInfoIter.Value(), &oldInfo)

		newInfo := types.ValidatorSigningInfoLegacyMissedHeights{
			Address:             oldInfo.Address,
			StartHeight:         oldInfo.StartHeight,
			JailedUntil:         oldInfo.JailedUntil,
			Tombstoned:          oldInfo.Tombstoned,
			MissedBlocksCounter: oldInfo.MissedBlocksCounter,
		}
		newSignInfoKeys = append(newSignInfoKeys, signInfoIter.Key())
		newSignInfoVals = append(newSignInfoVals, newInfo)
	}
	if err := signInfoIter.Close(); err != nil {
		return err
	}

	if len(newSignInfoKeys) != len(newSignInfoVals) {
		return fmt.Errorf("new sign info data length doesn't match up")
	}
	ctx.Logger().Info("Writing New Signing Info")
	for i := range newSignInfoKeys {
		bz := m.keeper.cdc.MustMarshal(&newSignInfoVals[i])
		store.Set(newSignInfoKeys[i], bz)
	}

	ctx.Logger().Info("Migrating Missed Block Bit Array")
	keysToDelete := [][]byte{}
	iter := sdk.KVStorePrefixIterator(store, types.ValidatorMissedBlockBitArrayKeyPrefix)
	// Note that we close the iterator twice. 2 iterators cannot be open at the same time due to mutex on the storage
	// This close within defer is a safety net, while the close() after iteration is to close the iterator before opening
	// a new one.
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		// need to use the key to extract validator cons addr
		// last 8 bytes are the index
		// remove the store prefix + length prefix
		key := iter.Key()
		if len(key) < 10 {
			return fmt.Errorf("invalid missed block bit array key: too short (%d bytes)", len(key))
		}
		consAddrBytes, indexBytes := key[2:len(key)-8], key[len(key)-8:]

		consAddr := sdk.ConsAddress(consAddrBytes)
		index := int64(binary.LittleEndian.Uint64(indexBytes)) //nolint:gosec // index represents a block index, stored as uint64
		// load legacy signing info type
		var signInfo types.ValidatorSigningInfoLegacyMissedHeights
		signInfoKey := types.ValidatorSigningInfoKey(consAddr)
		bz := store.Get(signInfoKey)
		if bz == nil {
			return fmt.Errorf("signing info not found for validator %s", consAddr.String())
		}

		m.keeper.cdc.MustUnmarshal(bz, &signInfo)
		arr, ok := valMissedMap[consAddr.String()]
		if !ok {
			ctx.Logger().Info(fmt.Sprintf("Migrating for next validator with consAddr: %s\n", consAddr.String()))
			arr = types.ValidatorMissedBlockArrayLegacyMissedHeights{
				Address:       consAddr.String(),
				MissedHeights: make([]int64, 0),
			}
		}
		var missed gogotypes.BoolValue
		m.keeper.cdc.MustUnmarshal(iter.Value(), &missed)
		if missed.Value {
			arr.MissedHeights = append(arr.MissedHeights, index+signInfo.StartHeight)
		}

		valMissedMap[consAddr.String()] = arr
		keysToDelete = append(keysToDelete, iter.Key())
	}

	if err := iter.Close(); err != nil {
		return err
	}

	ctx.Logger().Info(fmt.Sprintf("Starting deletion of missed bit array keys (total %d)", len(keysToDelete)))
	interval := len(keysToDelete) / 50
	if interval == 0 {
		interval = 1
	}
	for i, key := range keysToDelete {
		store.Delete(key)
		if i%interval == 0 {
			ctx.Logger().Info(fmt.Sprintf("Processing index %d", i))
		}
	}

	ctx.Logger().Info("Writing new validator missed heights")
	var valKeys []string
	for key := range valMissedMap {
		valKeys = append(valKeys, key)
	}
	sort.Strings(valKeys)
	for _, key := range valKeys {
		missedBlockArray := valMissedMap[key]
		consAddrKey, err := sdk.ConsAddressFromBech32(key)
		if err != nil {
			return err
		}
		ctx.Logger().Info(fmt.Sprintf("Writing missed heights for validator: %s\n", consAddrKey.String()))
		bz := m.keeper.cdc.MustMarshal(&missedBlockArray)
		store.Set(types.ValidatorMissedBlockBitArrayKey(consAddrKey), bz)
	}
	ctx.Logger().Info("Done migrating")
	return nil
}

// Migrate3to4 migrates from version 3 to 4.
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	ctx.Logger().Info("Migrating 3 -> 4")
	store := ctx.KVStore(m.keeper.storeKey)
	valMissedMap := make(map[string]types.ValidatorMissedBlockArray)
	ctx.Logger().Info("Migrating Signing Info")
	signInfoIter := sdk.KVStorePrefixIterator(store, types.ValidatorSigningInfoKeyPrefix)
	var newSignInfoKeys [][]byte
	var newSignInfoVals []types.ValidatorSigningInfo
	// use previous height to calculate index offset
	window := m.keeper.SignedBlocksWindow(ctx)
	index := window - 1

	for ; signInfoIter.Valid(); signInfoIter.Next() {
		ctx.Logger().Info(fmt.Sprintf("Migrating Signing Info for key: %v\n", signInfoIter.Key()))
		var oldInfo types.ValidatorSigningInfoLegacyMissedHeights
		m.keeper.cdc.MustUnmarshal(signInfoIter.Value(), &oldInfo)

		newInfo := types.ValidatorSigningInfo{
			Address:             oldInfo.Address,
			StartHeight:         oldInfo.StartHeight,
			IndexOffset:         index,
			JailedUntil:         oldInfo.JailedUntil,
			Tombstoned:          oldInfo.Tombstoned,
			MissedBlocksCounter: oldInfo.MissedBlocksCounter,
		}
		newSignInfoKeys = append(newSignInfoKeys, signInfoIter.Key())
		newSignInfoVals = append(newSignInfoVals, newInfo)
	}
	if err := signInfoIter.Close(); err != nil {
		return err
	}

	if len(newSignInfoKeys) != len(newSignInfoVals) {
		return fmt.Errorf("new sign info data length doesn't match up")
	}
	ctx.Logger().Info("Writing New Signing Info")
	for i := range newSignInfoKeys {
		bz := m.keeper.cdc.MustMarshal(&newSignInfoVals[i])
		store.Set(newSignInfoKeys[i], bz)
	}

	// need to turn this into a bool array
	ctx.Logger().Info("Migrating Missed Block Bit Array")
	startWindowHeight := ctx.BlockHeight() - window
	iter := sdk.KVStorePrefixIterator(store, types.ValidatorMissedBlockBitArrayKeyPrefix)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		var missedInfo types.ValidatorMissedBlockArrayLegacyMissedHeights
		key := iter.Key()
		if len(key) < 3 {
			return fmt.Errorf("invalid missed block bit array key: too short (%d bytes)", len(key))
		}
		consAddrBytes := key[2:]

		consAddr := sdk.ConsAddress(consAddrBytes)
		ctx.Logger().Info(fmt.Sprintf("Migrating for next validator with consAddr: %s\n", consAddr.String()))

		if window <= 0 {
			return fmt.Errorf("invalid signed blocks window: %d", window)
		}
		newBoolArray := make([]bool, window)
		m.keeper.cdc.MustUnmarshal(iter.Value(), &missedInfo)
		heights := missedInfo.MissedHeights
		for _, height := range heights {
			if height < startWindowHeight {
				continue
			}
			idx := height - startWindowHeight
			if idx < 0 || idx >= window {
				continue
			}
			newBoolArray[idx] = true
		}

		valMissedMap[consAddr.String()] = types.ValidatorMissedBlockArray{
			Address:      missedInfo.Address,
			MissedBlocks: m.keeper.ParseBoolArrayToBitGroups(newBoolArray),
			WindowSize:   window,
		}
	}

	ctx.Logger().Info("Writing new validator missed blocks infos")
	var valKeys []string
	for key := range valMissedMap {
		valKeys = append(valKeys, key)
	}
	sort.Strings(valKeys)
	for _, key := range valKeys {
		missedBlockArray := valMissedMap[key]
		consAddr, err := sdk.ConsAddressFromBech32(key)
		if err != nil {
			return err
		}
		ctx.Logger().Info(fmt.Sprintf("Writing missed heights for validator: %s\n", consAddr.String()))
		m.keeper.SetValidatorMissedBlocks(ctx, consAddr, missedBlockArray)
	}
	ctx.Logger().Info("Done migrating")
	return nil
}

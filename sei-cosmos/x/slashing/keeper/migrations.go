package keeper

import (
	"encoding/binary"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v043 "github.com/cosmos/cosmos-sdk/x/slashing/legacy/v043"

	"github.com/cosmos/cosmos-sdk/x/slashing/types"

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
	return v043.MigrateStore(ctx, m.keeper.storeKey)
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)
	valMissedMap := make(map[string]types.ValidatorMissedBlockArray)

	ctx.Logger().Info("Migrating Signing Info")
	signInfoIter := sdk.KVStorePrefixIterator(store, types.ValidatorSigningInfoKeyPrefix)
	newSignInfoKeys := [][]byte{}
	newSignInfoVals := []types.ValidatorSigningInfo{}
	defer signInfoIter.Close()
	for ; signInfoIter.Valid(); signInfoIter.Next() {
		ctx.Logger().Info(fmt.Sprintf("Migrating Signing Info for key: %v\n", signInfoIter.Key()))
		var oldInfo types.ValidatorSigningInfoLegacyV43
		m.keeper.cdc.MustUnmarshal(signInfoIter.Value(), &oldInfo)

		newInfo := types.ValidatorSigningInfo{
			Address:             oldInfo.Address,
			StartHeight:         oldInfo.StartHeight,
			JailedUntil:         oldInfo.JailedUntil,
			Tombstoned:          oldInfo.Tombstoned,
			MissedBlocksCounter: oldInfo.MissedBlocksCounter,
		}
		newSignInfoKeys = append(newSignInfoKeys, signInfoIter.Key())
		newSignInfoVals = append(newSignInfoVals, newInfo)
	}
	signInfoIter.Close()

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
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		// need to use the key to extract validator cons addr
		// last 8 bytes are the index
		// remove the store prefix + length prefix
		key := iter.Key()
		consAddrBytes, indexBytes := key[2:len(key)-8], key[len(key)-8:]

		consAddr := sdk.ConsAddress(consAddrBytes)
		index := int64(binary.LittleEndian.Uint64(indexBytes))

		signInfo, found := m.keeper.GetValidatorSigningInfo(ctx, consAddr)
		if !found {
			return fmt.Errorf("signing info not found")
		}
		arr, ok := valMissedMap[consAddr.String()]
		if !ok {
			ctx.Logger().Info(fmt.Sprintf("Migrating for next validator with consAddr: %s\n", consAddr.String()))
			arr = types.ValidatorMissedBlockArray{
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
	for key, missedBlockArray := range valMissedMap {
		consAddrKey, err := sdk.ConsAddressFromBech32(key)
		ctx.Logger().Info(fmt.Sprintf("Writing missed heights for validator: %s\n", consAddrKey.String()))
		if err != nil {
			return err
		}
		m.keeper.SetValidatorMissedBlocks(ctx, consAddrKey, missedBlockArray)
	}
	ctx.Logger().Info("Done migrating")
	return nil
}

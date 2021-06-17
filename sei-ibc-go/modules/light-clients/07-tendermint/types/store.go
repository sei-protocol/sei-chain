package types

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
)

/*
This file contains the logic for storage and iteration over `IterationKey` metadata that is stored
for each consensus state. The consensus state key specified in ICS-24 and expected by counterparty chains
stores the consensus state under the key: `consensusStates/{revision_number}-{revision_height}`, with each number
represented as a string.
While this works fine for IBC proof verification, it makes efficient iteration difficult since the lexicographic order
of the consensus state keys do not match the height order of consensus states. This makes consensus state pruning and
monotonic time enforcement difficult since it is inefficient to find the earliest consensus state or to find the neigboring
consensus states given a consensus state height.
Changing the ICS-24 representation will be a major breaking change that requires counterparty chains to accept a new key format.
Thus to avoid breaking IBC, we can store a lookup from a more efficiently formatted key: `iterationKey` to the consensus state key which
stores the underlying consensus state. This efficient iteration key will be formatted like so: `iterateConsensusStates{BigEndianRevisionBytes}{BigEndianHeightBytes}`.
This ensures that the lexicographic order of iteration keys match the height order of the consensus states. Thus, we can use the SDK store's
Iterators to iterate over the consensus states in ascending/descending order by providing a mapping from `iterationKey -> consensusStateKey -> ConsensusState`.
A future version of IBC may choose to replace the ICS24 ConsensusState path with the more efficient format and make this indirection unnecessary.
*/

const KeyIterateConsensusStatePrefix = "iterateConsensusStates"

var (
	// KeyProcessedTime is appended to consensus state key to store the processed time
	KeyProcessedTime = []byte("/processedTime")
	// KeyProcessedHeight is appended to consensus state key to store the processed height
	KeyProcessedHeight = []byte("/processedHeight")
	// KeyIteration stores the key mapping to consensus state key for efficient iteration
	KeyIteration = []byte("/iterationKey")
)

// SetConsensusState stores the consensus state at the given height.
func SetConsensusState(clientStore sdk.KVStore, cdc codec.BinaryCodec, consensusState *ConsensusState, height exported.Height) {
	key := host.ConsensusStateKey(height)
	val := clienttypes.MustMarshalConsensusState(cdc, consensusState)
	clientStore.Set(key, val)
}

// GetConsensusState retrieves the consensus state from the client prefixed
// store. An error is returned if the consensus state does not exist.
func GetConsensusState(store sdk.KVStore, cdc codec.BinaryCodec, height exported.Height) (*ConsensusState, error) {
	bz := store.Get(host.ConsensusStateKey(height))
	if bz == nil {
		return nil, sdkerrors.Wrapf(
			clienttypes.ErrConsensusStateNotFound,
			"consensus state does not exist for height %s", height,
		)
	}

	consensusStateI, err := clienttypes.UnmarshalConsensusState(cdc, bz)
	if err != nil {
		return nil, sdkerrors.Wrapf(clienttypes.ErrInvalidConsensus, "unmarshal error: %v", err)
	}

	consensusState, ok := consensusStateI.(*ConsensusState)
	if !ok {
		return nil, sdkerrors.Wrapf(
			clienttypes.ErrInvalidConsensus,
			"invalid consensus type %T, expected %T", consensusState, &ConsensusState{},
		)
	}

	return consensusState, nil
}

// deleteConsensusState deletes the consensus state at the given height
func deleteConsensusState(clientStore sdk.KVStore, height exported.Height) {
	key := host.ConsensusStateKey(height)
	clientStore.Delete(key)
}

// IterateConsensusMetadata iterates through the prefix store and applies the callback.
// If the cb returns true, then iterator will close and stop.
func IterateConsensusMetadata(store sdk.KVStore, cb func(key, val []byte) bool) {
	iterator := sdk.KVStorePrefixIterator(store, []byte(host.KeyConsensusStatePrefix))

	// iterate over processed time and processed height
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		keySplit := strings.Split(string(iterator.Key()), "/")
		// processed time key in prefix store has format: "consensusState/<height>/processedTime"
		if len(keySplit) != 3 {
			// ignore all consensus state keys
			continue

		}

		if keySplit[2] != "processedTime" && keySplit[2] != "processedHeight" {
			// only perform callback on consensus metadata
			continue
		}

		if cb(iterator.Key(), iterator.Value()) {
			break
		}
	}

	// iterate over iteration keys
	iterator = sdk.KVStorePrefixIterator(store, []byte(KeyIterateConsensusStatePrefix))

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		if cb(iterator.Key(), iterator.Value()) {
			break
		}
	}
}

// ProcessedTimeKey returns the key under which the processed time will be stored in the client store.
func ProcessedTimeKey(height exported.Height) []byte {
	return append(host.ConsensusStateKey(height), KeyProcessedTime...)
}

// SetProcessedTime stores the time at which a header was processed and the corresponding consensus state was created.
// This is useful when validating whether a packet has reached the time specified delay period in the tendermint client's
// verification functions
func SetProcessedTime(clientStore sdk.KVStore, height exported.Height, timeNs uint64) {
	key := ProcessedTimeKey(height)
	val := sdk.Uint64ToBigEndian(timeNs)
	clientStore.Set(key, val)
}

// GetProcessedTime gets the time (in nanoseconds) at which this chain received and processed a tendermint header.
// This is used to validate that a received packet has passed the time delay period.
func GetProcessedTime(clientStore sdk.KVStore, height exported.Height) (uint64, bool) {
	key := ProcessedTimeKey(height)
	bz := clientStore.Get(key)
	if bz == nil {
		return 0, false
	}
	return sdk.BigEndianToUint64(bz), true
}

// deleteProcessedTime deletes the processedTime for a given height
func deleteProcessedTime(clientStore sdk.KVStore, height exported.Height) {
	key := ProcessedTimeKey(height)
	clientStore.Delete(key)
}

// ProcessedHeightKey returns the key under which the processed height will be stored in the client store.
func ProcessedHeightKey(height exported.Height) []byte {
	return append(host.ConsensusStateKey(height), KeyProcessedHeight...)
}

// SetProcessedHeight stores the height at which a header was processed and the corresponding consensus state was created.
// This is useful when validating whether a packet has reached the specified block delay period in the tendermint client's
// verification functions
func SetProcessedHeight(clientStore sdk.KVStore, consHeight, processedHeight exported.Height) {
	key := ProcessedHeightKey(consHeight)
	val := []byte(processedHeight.String())
	clientStore.Set(key, val)
}

// GetProcessedHeight gets the height at which this chain received and processed a tendermint header.
// This is used to validate that a received packet has passed the block delay period.
func GetProcessedHeight(clientStore sdk.KVStore, height exported.Height) (exported.Height, bool) {
	key := ProcessedHeightKey(height)
	bz := clientStore.Get(key)
	if bz == nil {
		return nil, false
	}
	processedHeight, err := clienttypes.ParseHeight(string(bz))
	if err != nil {
		return nil, false
	}
	return processedHeight, true
}

// deleteProcessedHeight deletes the processedHeight for a given height
func deleteProcessedHeight(clientStore sdk.KVStore, height exported.Height) {
	key := ProcessedHeightKey(height)
	clientStore.Delete(key)
}

// IterationKey returns the key under which the consensus state key will be stored.
// The iteration key is a BigEndian representation of the consensus state key to support efficient iteration.
func IterationKey(height exported.Height) []byte {
	heightBytes := bigEndianHeightBytes(height)
	return append([]byte(KeyIterateConsensusStatePrefix), heightBytes...)
}

// SetIterationKey stores the consensus state key under a key that is more efficient for ordered iteration
func SetIterationKey(clientStore sdk.KVStore, height exported.Height) {
	key := IterationKey(height)
	val := host.ConsensusStateKey(height)
	clientStore.Set(key, val)
}

// GetIterationKey returns the consensus state key stored under the efficient iteration key.
// NOTE: This function is currently only used for testing purposes
func GetIterationKey(clientStore sdk.KVStore, height exported.Height) []byte {
	key := IterationKey(height)
	return clientStore.Get(key)
}

// deleteIterationKey deletes the iteration key for a given height
func deleteIterationKey(clientStore sdk.KVStore, height exported.Height) {
	key := IterationKey(height)
	clientStore.Delete(key)
}

// GetHeightFromIterationKey takes an iteration key and returns the height that it references
func GetHeightFromIterationKey(iterKey []byte) exported.Height {
	bigEndianBytes := iterKey[len([]byte(KeyIterateConsensusStatePrefix)):]
	revisionBytes := bigEndianBytes[0:8]
	heightBytes := bigEndianBytes[8:]
	revision := binary.BigEndian.Uint64(revisionBytes)
	height := binary.BigEndian.Uint64(heightBytes)
	return clienttypes.NewHeight(revision, height)
}

// IterateConsensusStateAscending iterates through the consensus states in ascending order. It calls the provided
// callback on each height, until stop=true is returned.
func IterateConsensusStateAscending(clientStore sdk.KVStore, cb func(height exported.Height) (stop bool)) error {
	iterator := sdk.KVStorePrefixIterator(clientStore, []byte(KeyIterateConsensusStatePrefix))
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		iterKey := iterator.Key()
		height := GetHeightFromIterationKey(iterKey)
		if cb(height) {
			return nil
		}
	}
	return nil
}

// GetNextConsensusState returns the lowest consensus state that is larger than the given height.
// The Iterator returns a storetypes.Iterator which iterates from start (inclusive) to end (exclusive).
// If the starting height exists in store, we need to call iterator.Next() to get the next consenus state.
// Otherwise, the iterator is already at the next consensus state so we can call iterator.Value() immediately.
func GetNextConsensusState(clientStore sdk.KVStore, cdc codec.BinaryCodec, height exported.Height) (*ConsensusState, bool) {
	iterateStore := prefix.NewStore(clientStore, []byte(KeyIterateConsensusStatePrefix))
	iterator := iterateStore.Iterator(bigEndianHeightBytes(height), nil)
	defer iterator.Close()
	if !iterator.Valid() {
		return nil, false
	}

	// if iterator is at current height, ignore the consensus state at current height and get next height
	// if iterator value is not at current height, it is already at next height.
	if bytes.Equal(iterator.Value(), host.ConsensusStateKey(height)) {
		iterator.Next()
		if !iterator.Valid() {
			return nil, false
		}
	}

	csKey := iterator.Value()

	return getTmConsensusState(clientStore, cdc, csKey)
}

// GetPreviousConsensusState returns the highest consensus state that is lower than the given height.
// The Iterator returns a storetypes.Iterator which iterates from the end (exclusive) to start (inclusive).
// Thus to get previous consensus state we call iterator.Value() immediately.
func GetPreviousConsensusState(clientStore sdk.KVStore, cdc codec.BinaryCodec, height exported.Height) (*ConsensusState, bool) {
	iterateStore := prefix.NewStore(clientStore, []byte(KeyIterateConsensusStatePrefix))
	iterator := iterateStore.ReverseIterator(nil, bigEndianHeightBytes(height))
	defer iterator.Close()

	if !iterator.Valid() {
		return nil, false
	}

	csKey := iterator.Value()

	return getTmConsensusState(clientStore, cdc, csKey)
}

// PruneAllExpiredConsensusStates iterates over all consensus states for a given
// client store. If a consensus state is expired, it is deleted and its metadata
// is deleted.
func PruneAllExpiredConsensusStates(
	ctx sdk.Context, clientStore sdk.KVStore,
	cdc codec.BinaryCodec, clientState *ClientState,
) (err error) {
	var heights []exported.Height

	pruneCb := func(height exported.Height) bool {
		consState, err := GetConsensusState(clientStore, cdc, height)
		// this error should never occur
		if err != nil {
			return true
		}

		if clientState.IsExpired(consState.Timestamp, ctx.BlockTime()) {
			heights = append(heights, height)
		}

		return false
	}

	IterateConsensusStateAscending(clientStore, pruneCb)
	if err != nil {
		return err
	}

	for _, height := range heights {
		deleteConsensusState(clientStore, height)
		deleteConsensusMetadata(clientStore, height)
	}

	return nil
}

// Helper function for GetNextConsensusState and GetPreviousConsensusState
func getTmConsensusState(clientStore sdk.KVStore, cdc codec.BinaryCodec, key []byte) (*ConsensusState, bool) {
	bz := clientStore.Get(key)
	if bz == nil {
		return nil, false
	}

	consensusStateI, err := clienttypes.UnmarshalConsensusState(cdc, bz)
	if err != nil {
		return nil, false
	}

	consensusState, ok := consensusStateI.(*ConsensusState)
	if !ok {
		return nil, false
	}
	return consensusState, true
}

func bigEndianHeightBytes(height exported.Height) []byte {
	heightBytes := make([]byte, 16)
	binary.BigEndian.PutUint64(heightBytes, height.GetRevisionNumber())
	binary.BigEndian.PutUint64(heightBytes[8:], height.GetRevisionHeight())
	return heightBytes
}

// setConsensusMetadata sets context time as processed time and set context height as processed height
// as this is internal tendermint light client logic.
// client state and consensus state will be set by client keeper
// set iteration key to provide ability for efficient ordered iteration of consensus states.
func setConsensusMetadata(ctx sdk.Context, clientStore sdk.KVStore, height exported.Height) {
	setConsensusMetadataWithValues(clientStore, height, clienttypes.GetSelfHeight(ctx), uint64(ctx.BlockTime().UnixNano()))
}

// setConsensusMetadataWithValues sets the consensus metadata with the provided values
func setConsensusMetadataWithValues(
	clientStore sdk.KVStore, height,
	processedHeight exported.Height,
	processedTime uint64,
) {
	SetProcessedTime(clientStore, height, processedTime)
	SetProcessedHeight(clientStore, height, processedHeight)
	SetIterationKey(clientStore, height)
}

// deleteConsensusMetadata deletes the metadata stored for a particular consensus state.
func deleteConsensusMetadata(clientStore sdk.KVStore, height exported.Height) {
	deleteProcessedTime(clientStore, height)
	deleteProcessedHeight(clientStore, height)
	deleteIterationKey(clientStore, height)
}

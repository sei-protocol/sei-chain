package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// budTombstoneHorizon is the tombstone horizon, as the BUD spec defines it, of every block: the not-modified-since
// height of a budlet whose key had no entry in the state before its block.
//
// FlatKV MUST start storing tombstones before Autobahn. The true tombstone horizon is the first block at which FlatKV
// stores tombstones, but it is simpler to set it to the giga upgrade height. It must become that global chain
// parameter before the mainnet app hash uses the BUD hash.
const budTombstoneHorizon uint64 = 0

// hashSerializedBlock returns the BUD hash of a serialized block. It returns an error if the block cannot be
// deserialized.
func hashSerializedBlock(
	// The serialized block.
	serialized []byte,
) ([32]byte, error) {
	mutations, err := deserializeBlock(serialized)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to deserialize block: %w", err)
	}
	tree, err := budTreeFromMutations(mutations)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to build BUD tree: %w", err)
	}
	return tree.BUDHash(), nil
}

// budTreeFromMutations returns the BUD tree over one block's mutations. Each mutation must meet the requirements of
// budletFromMutation().
func budTreeFromMutations(
	// The block's mutations, strictly sorted by key.
	mutations []mutation.Mutation,
) (*bud.BUDTree, error) {
	budlets, err := budletsFromMutations(mutations)
	if err != nil {
		return nil, err
	}
	tree, err := bud.NewBUDTree(budlets)
	if err != nil {
		return nil, fmt.Errorf("failed to create BUD tree: %w", err)
	}
	return tree, nil
}

// budletsFromMutations returns the budlet of each mutation, in order.
func budletsFromMutations(mutations []mutation.Mutation) ([]*bud.Budlet, error) {
	budlets := make([]*bud.Budlet, len(mutations))
	errs := make([]error, len(mutations))
	forEachMutationChunk(len(mutations), func(start int, end int) {
		for i := start; i < end; i++ {
			budlet, err := budletFromMutation(&mutations[i])
			if err != nil {
				errs[i] = fmt.Errorf("failed to convert mutation %d: %w", i, err)
				return
			}
			budlets[i] = budlet
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return budlets, nil
}

// budletFromMutation returns the budlet for one key a block modified.
//
// The mutation's key must not be empty, and each of its values must be nil or a serialized FlatKV VType. A nil value
// converts to the absent payload, the empty byte string, which budPayload() never returns.
func budletFromMutation(
	// The mutation to convert. The budlet retains its key.
	m *mutation.Mutation,
) (*bud.Budlet, error) {
	var value []byte
	if m.NewValue != nil {
		var err error
		value, _, err = budPayload(m.NewValue)
		if err != nil {
			return nil, fmt.Errorf("invalid new value of key %x: %w", m.Key, err)
		}
	}

	var previousValue []byte
	notModifiedSince := budTombstoneHorizon
	if m.PreviousValue != nil {
		var err error
		previousValue, notModifiedSince, err = budPayload(m.PreviousValue)
		if err != nil {
			return nil, fmt.Errorf("invalid previous value of key %x: %w", m.Key, err)
		}
	}

	budlet, err := bud.NewBudlet(m.Key, value, previousValue, notModifiedSince)
	if err != nil {
		return nil, fmt.Errorf("invalid mutation of key %x: %w", m.Key, err)
	}
	return budlet, nil
}

// budPayload returns the BUD payload of a serialized FlatKV VType, its serialization version followed by its data,
// and the height of the block that wrote it.
func budPayload(
	// The serialized VType.
	value []byte,
) ([]byte, uint64, error) {
	version, blockHeight, data, err := vtype.SplitValue(value)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to split value: %w", err)
	}
	payload := make([]byte, 0, 1+len(data))
	payload = append(payload, version)
	payload = append(payload, data...)
	return payload, blockHeight, nil
}

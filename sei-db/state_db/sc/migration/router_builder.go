package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	dbm "github.com/tendermint/tm-db"
)

// Build a function capable of reading data from memiavl.
func buildMemIAVLReader(memIAVL *memiavl.CommitStore) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, false, fmt.Errorf("store not found: %s", store)
		}
		value := childStore.Get(key)
		return value, value != nil, nil
	}
}

// Build a function capable of writing data to memiavl.
func buildMemIAVLWriter(memIAVL *memiavl.CommitStore) DBWriter {
	return func(changesets []*proto.NamedChangeSet) error {
		err := memIAVL.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a function capable of getting an iterator over a range of keys in a memiavl store.
func buildMemIAVLIteratorBuilder(memIAVL *memiavl.CommitStore) DBIteratorBuilder {
	return func(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.Iterator(start, end, ascending), nil
	}
}

// Build a function capable of building a proof of the value for a key in a memiavl store.
func buildMemIAVLProofBuilder(memIAVL *memiavl.CommitStore) DBProofBuilder {
	return func(store string, key []byte) (*ics23.CommitmentProof, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.GetProof(key), nil
	}
}

// Build a function capable of reading data from flatkv.
func buildFlatKVReader(flatKV *flatkv.CommitStore) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		value, found := flatKV.Get(store, key)
		return value, found, nil
	}
}

// Build a function capable of writing data to flatkv.
func buildFlatKVWriter(flatKV *flatkv.CommitStore) DBWriter {
	return func(changesets []*proto.NamedChangeSet) error {
		err := flatKV.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a route to a memiavl store for the given module names.
func routeToMemIAVL(memIAVL *memiavl.CommitStore, moduleNames ...string) (*Route, error) {
	return NewRoute(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		moduleNames...,
	)
}

// Build a route to a flatkv store for the given module names.
func routeToFlatKV(flatKV *flatkv.CommitStore, moduleNames ...string) (*Route, error) {
	return NewRoute(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // iteration not supported
		nil, // proof building not supported
		moduleNames...,
	)
}

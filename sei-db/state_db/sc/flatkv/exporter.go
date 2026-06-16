package flatkv

import (
	"bytes"
	"fmt"

	dbm "github.com/tendermint/tm-db"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Exporter = (*KVExporter)(nil)

// KVExporter exports all committed data from a read-only FlatKV store as raw
// physical key/value pairs. It uses RawGlobalIterator to walk every data DB
// in global lexicographic order and emits each row as a single
// SnapshotNode without any parsing or conversion.
//
// The stream is self-describing: the first item is the keys.FlatKVStoreKey
// module header (a string), mirroring the memiavl MultiTreeExporter which
// emits each tree name ahead of its nodes. Emitting the header here — rather
// than having the composite layer inject it — keeps the FlatKV module section
// valid no matter how the exporter is consumed (bare in flatkv_only, or
// appended after the cosmos modules in a mixed/migrated export). The restore
// side relies on this header to register the module and route nodes through
// the FlatKV import path.
//
// All emitted SnapshotNodes carry the export version and Height=0 (leaf).
//
// The caller must Close the exporter when done.
type KVExporter struct {
	store      *CommitStore
	version    int64
	iter       dbm.Iterator
	headerSent bool
}

func NewKVExporter(store *CommitStore, version int64) *KVExporter {
	return &KVExporter{
		store:   store,
		version: version,
	}
}

func (e *KVExporter) Next() (interface{}, error) {
	// Emit the module header as the first item, before any node, so the
	// FlatKV section is self-describing and registers correctly on restore
	// even when there is no preceding cosmos module section. This holds for
	// an empty store too: the header is emitted, then ErrorExportDone.
	if !e.headerSent {
		e.headerSent = true
		return keys.FlatKVStoreKey, nil
	}

	if e.iter == nil {
		var err error
		e.iter, err = e.store.RawGlobalIterator()
		if err != nil {
			return nil, fmt.Errorf("raw global iterator: %w", err)
		}
		if !e.iter.Valid() {
			if err := e.iter.Error(); err != nil {
				return nil, fmt.Errorf("iterator error: %w", err)
			}
			return nil, errorutils.ErrorExportDone
		}
	}

	if !e.iter.Valid() {
		if err := e.iter.Error(); err != nil {
			return nil, fmt.Errorf("iterator error: %w", err)
		}
		return nil, errorutils.ErrorExportDone
	}

	node := &types.SnapshotNode{
		Key:     bytes.Clone(e.iter.Key()),
		Value:   bytes.Clone(e.iter.Value()),
		Version: e.version,
		Height:  0,
	}
	e.iter.Next()
	return node, nil
}

func (e *KVExporter) Close() error {
	if e.iter != nil {
		_ = e.iter.Close()
		e.iter = nil
	}
	if e.store != nil {
		err := e.store.Close()
		e.store = nil
		return err
	}
	return nil
}

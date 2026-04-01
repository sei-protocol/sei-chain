package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Exporter = (*KVExporter)(nil)

type exportDBKind int

const (
	exportDBAccount exportDBKind = iota
	exportDBCode
	exportDBStorage
	exportDBLegacy
	exportDBDone
)

// KVExporter exports all committed EVM data from a read-only FlatKV store
// as SnapshotNode items. Keys are emitted in memiavl EVM format so the
// importer can feed them through ApplyChangeSets unchanged.
//
// All emitted SnapshotNodes carry the export version and Height=0 (leaf).
// This intentionally flattens version history: state sync only transfers the
// latest state at a given height, not the full edit history.
//
// The caller must Close the exporter when done.
type KVExporter struct {
	store   *CommitStore
	version int64

	currentDB   exportDBKind
	currentIter dbtypes.KeyValueDBIterator

	// accountDB entries decompose into multiple snapshot nodes (nonce + codehash).
	pendingNodes []*types.SnapshotNode
}

func NewKVExporter(store *CommitStore, version int64) *KVExporter {
	return &KVExporter{
		store:   store,
		version: version,
	}
}

func (e *KVExporter) Next() (interface{}, error) {
	if len(e.pendingNodes) > 0 {
		node := e.pendingNodes[0]
		e.pendingNodes = e.pendingNodes[1:]
		return node, nil
	}

	for e.currentDB < exportDBDone {
		if e.currentIter == nil {
			iter, err := e.openIterForDB(e.currentDB)
			if err != nil {
				return nil, fmt.Errorf("open iterator for db %d: %w", e.currentDB, err)
			}
			if iter == nil {
				e.currentDB++
				continue
			}
			if !iter.First() {
				err := iter.Error()
				_ = iter.Close()
				if err != nil {
					return nil, fmt.Errorf("iterator seek error for db %d: %w", e.currentDB, err)
				}
				e.currentDB++
				continue
			}
			e.currentIter = iter
		}

		if !e.currentIter.Valid() {
			if err := e.currentIter.Error(); err != nil {
				return nil, fmt.Errorf("iterator error: %w", err)
			}
			_ = e.currentIter.Close()
			e.currentIter = nil
			e.currentDB++
			continue
		}

		if isMetaKey(e.currentIter.Key()) {
			e.currentIter.Next()
			continue
		}
		key := bytes.Clone(e.currentIter.Key())
		value := bytes.Clone(e.currentIter.Value())
		e.currentIter.Next()

		nodes, err := e.convertToNodes(e.currentDB, key, value)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			continue
		}

		if len(nodes) > 1 {
			e.pendingNodes = nodes[1:]
		}
		return nodes[0], nil
	}

	return nil, errorutils.ErrorExportDone
}

func (e *KVExporter) Close() error {
	if e.currentIter != nil {
		_ = e.currentIter.Close()
		e.currentIter = nil
	}
	if e.store != nil {
		err := e.store.Close()
		e.store = nil
		return err
	}
	return nil
}

// openIterForDB returns an iterator over all user data in the given DB.
// Metadata keys are filtered out by isMetaKey() in the iteration loop.
func (e *KVExporter) openIterForDB(db exportDBKind) (dbtypes.KeyValueDBIterator, error) {
	var kvDB dbtypes.KeyValueDB
	switch db {
	case exportDBAccount:
		kvDB = e.store.accountDB
	case exportDBCode:
		kvDB = e.store.codeDB
	case exportDBStorage:
		kvDB = e.store.storageDB
	case exportDBLegacy:
		kvDB = e.store.legacyDB
	default:
		return nil, nil
	}
	if kvDB == nil {
		return nil, nil
	}
	return kvDB.NewIter(&dbtypes.IterOptions{})
}

func (e *KVExporter) convertToNodes(db exportDBKind, key, value []byte) ([]*types.SnapshotNode, error) {
	switch db {
	case exportDBAccount:
		return e.accountToNodes(key, value)
	case exportDBCode:
		return e.codeToNodes(key, value)
	case exportDBStorage:
		return e.storageToNodes(key, value)
	case exportDBLegacy:
		return e.legacyToNodes(key, value)
	default:
		return nil, nil
	}
}

func (e *KVExporter) accountToNodes(key, value []byte) ([]*types.SnapshotNode, error) {
	ad, err := vtype.DeserializeAccountData(value)
	if err != nil {
		return nil, fmt.Errorf("corrupt account entry key=%x: %w", key, err)
	}

	var nodes []*types.SnapshotNode

	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, key)
	nonceValue := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonceValue, ad.GetNonce())
	nodes = append(nodes, &types.SnapshotNode{
		Key:     nonceKey,
		Value:   nonceValue,
		Version: e.version,
		Height:  0,
	})

	codeHash := ad.GetCodeHash()
	var zeroHash vtype.CodeHash
	if codeHash != nil && *codeHash != zeroHash {
		codeHashKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, key)
		codeHashValue := make([]byte, vtype.CodeHashLen)
		copy(codeHashValue, codeHash[:])
		nodes = append(nodes, &types.SnapshotNode{
			Key:     codeHashKey,
			Value:   codeHashValue,
			Version: e.version,
			Height:  0,
		})
	}

	return nodes, nil
}

func (e *KVExporter) codeToNodes(key, value []byte) ([]*types.SnapshotNode, error) {
	codeData, err := vtype.DeserializeCodeData(value)
	if err != nil {
		return nil, fmt.Errorf("corrupt code entry key=%x: %w", key, err)
	}
	memiavlKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, key)
	return []*types.SnapshotNode{{
		Key:     memiavlKey,
		Value:   codeData.GetBytecode(),
		Version: e.version,
		Height:  0,
	}}, nil
}

func (e *KVExporter) storageToNodes(key, value []byte) ([]*types.SnapshotNode, error) {
	storageData, err := vtype.DeserializeStorageData(value)
	if err != nil {
		return nil, fmt.Errorf("corrupt storage entry key=%x: %w", key, err)
	}
	memiavlKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, key)
	return []*types.SnapshotNode{{
		Key:     memiavlKey,
		Value:   storageData.GetValue()[:],
		Version: e.version,
		Height:  0,
	}}, nil
}

func (e *KVExporter) legacyToNodes(key, value []byte) ([]*types.SnapshotNode, error) {
	legacyData, err := vtype.DeserializeLegacyData(value)
	if err != nil {
		return nil, fmt.Errorf("corrupt legacy entry key=%x: %w", key, err)
	}
	return []*types.SnapshotNode{{
		Key:     key,
		Value:   legacyData.GetValue(),
		Version: e.version,
		Height:  0,
	}}, nil
}

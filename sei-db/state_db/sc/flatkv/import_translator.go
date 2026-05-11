package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// PhysicalKVPair is a (physical_key, serialized_value) pair already encoded
// in FlatKV's on-disk layout, ready for direct insertion into KVImporter
// (e.g. via types.SnapshotNode).
type PhysicalKVPair struct {
	Key   []byte
	Value []byte
}

// ImportTranslator converts raw EVM/non-EVM changesets into physically-encoded
// pairs ready for FlatKV bulk import.
//
// It applies the same translation logic that CommitStore.ApplyChangeSets uses
// (classifyAndPrefix + processStorageChanges + processCodeChanges +
// processLegacyChanges + mergeAccountUpdates), but assumes the import target
// is empty so it does not merge with prior DB values.
//
// Storage / code / legacy / non-EVM pairs are emitted directly from each
// Translate call. Account-related entries (nonce, codehash) are buffered
// across all Translate calls so that each address is written exactly once
// with its fully-merged AccountData; flush them by calling Finalize.
//
// Deletes are dropped: importing into a fresh store has no prior values to
// remove.
//
// ImportTranslator is not safe for concurrent use.
type ImportTranslator struct {
	blockHeight  int64
	pendingAccts map[string]*vtype.PendingAccountWrite
}

// NewImportTranslator creates a translator that stamps blockHeight onto every
// emitted value. blockHeight should match the memiavl version that the import
// is sourced from.
func NewImportTranslator(blockHeight int64) *ImportTranslator {
	return &ImportTranslator{
		blockHeight:  blockHeight,
		pendingAccts: make(map[string]*vtype.PendingAccountWrite),
	}
}

// Translate returns the storage / code / legacy / non-EVM physical pairs
// encoded from cs. Account fragments (nonce, codehash) are buffered
// internally; flush them via Finalize after all changesets have been fed in.
//
// nil or empty changesets return (nil, nil).
func (t *ImportTranslator) Translate(cs *proto.NamedChangeSet) ([]PhysicalKVPair, error) {
	if cs == nil || len(cs.Changeset.Pairs) == 0 {
		return nil, nil
	}

	// Drop deletes up front: import targets an empty store, so deleting a
	// non-existent key is a no-op. This also keeps mergeAccountUpdates
	// from interpreting nil values as "set field to zero".
	filteredPairs := make([]*proto.KVPair, 0, len(cs.Changeset.Pairs))
	for _, p := range cs.Changeset.Pairs {
		if p == nil || p.Delete {
			continue
		}
		filteredPairs = append(filteredPairs, p)
	}
	if len(filteredPairs) == 0 {
		return nil, nil
	}
	filteredCS := &proto.NamedChangeSet{
		Name:      cs.Name,
		Changeset: proto.ChangeSet{Pairs: filteredPairs},
	}

	changesByType, err := classifyAndPrefix([]*proto.NamedChangeSet{filteredCS})
	if err != nil {
		return nil, err
	}

	out := make([]PhysicalKVPair, 0, len(filteredPairs))

	storageChanges, err := processStorageChanges(changesByType[keys.EVMKeyStorage], t.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to process storage changes: %w", err)
	}
	for k, v := range storageChanges {
		if v.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(k), Value: v.Serialize()})
	}

	codeChanges, err := processCodeChanges(changesByType[keys.EVMKeyCode], t.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to process code changes: %w", err)
	}
	for k, v := range codeChanges {
		if v.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(k), Value: v.Serialize()})
	}

	legacyChanges, err := processLegacyChanges(changesByType[keys.EVMKeyLegacy], t.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to process legacy changes: %w", err)
	}
	for k, v := range legacyChanges {
		if v.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(k), Value: v.Serialize()})
	}

	// Accumulate nonce + codeHash entries from this batch into the
	// translator-level pending account map. Multiple Translate calls
	// naturally fold updates for the same address together: the SetXxx
	// methods on PendingAccountWrite mutate the pointer in place when the
	// receiver is non-nil.
	batchAccts, err := mergeAccountUpdates(
		changesByType[keys.EVMKeyNonce],
		changesByType[keys.EVMKeyCodeHash],
		nil, // TODO: balance, when balance key kind is introduced
	)
	if err != nil {
		return nil, fmt.Errorf("failed to merge account changes: %w", err)
	}
	for addr, batchUpdate := range batchAccts {
		existing, ok := t.pendingAccts[addr]
		if !ok || existing == nil {
			t.pendingAccts[addr] = batchUpdate
			continue
		}
		if batchUpdate.IsNonceSet() {
			existing.SetNonce(batchUpdate.GetNonce())
		}
		if batchUpdate.IsCodeHashSet() {
			existing.SetCodeHash(batchUpdate.GetCodeHash())
		}
		if batchUpdate.IsBalanceSet() {
			existing.SetBalance(batchUpdate.GetBalance())
		}
	}

	return out, nil
}

// Finalize flushes the buffered account writes as physically-encoded pairs.
// Each accumulated address is merged into a fresh AccountData (no base, since
// the import target is empty) and serialized.
//
// Call once after all Translate calls. Translate must not be called after
// Finalize.
func (t *ImportTranslator) Finalize() []PhysicalKVPair {
	out := make([]PhysicalKVPair, 0, len(t.pendingAccts))
	for addr, pending := range t.pendingAccts {
		merged := pending.Merge(nil, t.blockHeight)
		if merged.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(addr), Value: merged.Serialize()})
	}
	t.pendingAccts = nil
	return out
}

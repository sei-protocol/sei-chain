package flatkv

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// ErrImportTranslatorFinalized is returned by ImportTranslator.Translate
// when called after Finalize has flushed the pending-account buffer. The
// translator is single-shot by contract; this error makes the violation
// explicit (and recoverable for the caller) instead of panicking on a
// nil map write inside the account-merge path.
var ErrImportTranslatorFinalized = errors.New("flatkv: ImportTranslator.Translate called after Finalize")

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
// processMiscChanges + mergeAccountUpdates), but assumes the import target
// is empty so it does not merge with prior DB values.
//
// Storage / code / misc / non-EVM pairs are emitted directly from each
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

// Translate returns the storage / code / misc / non-EVM physical pairs
// encoded from cs. Account fragments (nonce, codehash) are buffered
// internally; flush them via Finalize after all changesets have been fed in.
//
// nil or empty changesets return (nil, nil).
func (t *ImportTranslator) Translate(cs *proto.NamedChangeSet) ([]PhysicalKVPair, error) {
	if t.pendingAccts == nil {
		return nil, ErrImportTranslatorFinalized
	}
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
	out = appendNonDeletes(out, storageChanges)

	codeChanges, err := processCodeChanges(changesByType[keys.EVMKeyCode], t.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to process code changes: %w", err)
	}
	out = appendNonDeletes(out, codeChanges)

	miscChanges, err := processMiscChanges(changesByType[keys.EVMKeyMisc], t.blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to process misc changes: %w", err)
	}
	out = appendNonDeletes(out, miscChanges)

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
	merged := make(map[string]*vtype.AccountData, len(t.pendingAccts))
	for addr, pending := range t.pendingAccts {
		merged[addr] = pending.Merge(nil, t.blockHeight)
	}
	t.pendingAccts = nil
	return appendNonDeletes(make([]PhysicalKVPair, 0, len(merged)), merged)
}

// appendNonDeletes serializes every non-delete entry in m and appends the
// resulting (physical_key, serialized_value) pair to out. Hoisted out of
// the three processStorage/Code/Misc branches in Translate (and reused
// by Finalize) so that the "drop tombstones, serialize to PhysicalKVPair"
// contract lives in one place; mirrors gatherPairs's generic use
// of vtype.VType in store_apply.go.
func appendNonDeletes[T vtype.VType](out []PhysicalKVPair, m map[string]T) []PhysicalKVPair {
	for k, v := range m {
		if v.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(k), Value: v.Serialize()})
	}
	return out
}

package flatkv

import (
	"errors"
	"fmt"

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
// It produces the same rows a committed block would, but assumes the import
// target is empty so it does not merge with prior DB values.
//
// Storage / code / misc / non-EVM pairs are emitted directly from each
// Translate call. Account-related entries (nonce, codehash, balance) are buffered
// across all Translate calls so that each address is written exactly once
// with its fully-merged AccountData; flush them by calling Finalize.
//
// Deletes are dropped: importing into a fresh store has no prior values to
// remove.
//
// ImportTranslator is not safe for concurrent use.
type ImportTranslator struct {
	blockHeight int64

	// The account fields written so far to each account row, keyed by string(physical key).
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
// encoded from cs. Account fragments (nonce, codehash, balance) are buffered
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

	var out []PhysicalKVPair
	for _, pair := range cs.Changeset.Pairs {
		if pair == nil || pair.Delete {
			continue
		}
		kind, key, err := changeSetEntryKey(cs.Name, pair.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse module %q key %x: %w", cs.Name, pair.Key, err)
		}
		if isAccountKind(kind) {
			fields, ok := t.pendingAccts[string(key)]
			if !ok {
				fields = vtype.NewPendingAccountWrite()
				t.pendingAccts[string(key)] = fields
			}
			if err := fields.SetField(kind, pair.Value, false); err != nil {
				return nil, fmt.Errorf("failed to set account field of key %x: %w", key, err)
			}
			continue
		}
		//nolint:gosec // block heights are non-negative
		value, err := rowValue(kind, pair, uint64(t.blockHeight))
		if err != nil {
			return nil, fmt.Errorf("failed to encode module %q key %x: %w", cs.Name, pair.Key, err)
		}
		if value == nil {
			// The write empties the row, which an empty import target does not have.
			continue
		}
		out = append(out, PhysicalKVPair{Key: key, Value: value})
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
	for key, fields := range t.pendingAccts {
		account := fields.Merge(nil, t.blockHeight)
		if account.IsDelete() {
			continue
		}
		out = append(out, PhysicalKVPair{Key: []byte(key), Value: account.Serialize()})
	}
	t.pendingAccts = nil
	return out
}

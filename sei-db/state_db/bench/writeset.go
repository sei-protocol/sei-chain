package bench

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// This file implements the write-set replay adapter: it parses a captured
// write-set file (typically derived from a debug_traceCall prestateTracer
// diff) into per-block changesets and replays them through a storage-engine
// wrapper, timing ApplyChangeSets and Commit separately.
//
// v1 scope: EVM-module keys only (storage/code/nonce/codehash/raw). Bank
// (balance) changesets require the bank store layout and are deliberately
// out of scope; see the gas-repricing storage doc.

// WriteSetEntryKind enumerates the supported key kinds in a write-set file.
const (
	WriteKindStorage  = "storage"  // requires address + slot
	WriteKindCode     = "code"     // requires address
	WriteKindNonce    = "nonce"    // requires address
	WriteKindCodeHash = "codehash" // requires address
	WriteKindRaw      = "raw"      // requires key (full store key, hex)
)

// WriteSetEntry is one captured write. Hex fields accept an optional 0x prefix.
type WriteSetEntry struct {
	// Kind is one of the WriteKind* constants.
	Kind string `json:"kind"`
	// Address is the 20-byte EVM address (storage/code/nonce/codehash kinds).
	Address string `json:"address,omitempty"`
	// Slot is the 32-byte storage slot (storage kind only).
	Slot string `json:"slot,omitempty"`
	// Key is the full raw store key (raw kind only).
	Key string `json:"key,omitempty"`
	// Value is the new value. Ignored when Delete is true.
	Value string `json:"value,omitempty"`
	// Delete marks a deletion instead of a write.
	Delete bool `json:"delete,omitempty"`
}

// WriteSetBlock groups the writes that commit together as one block.
type WriteSetBlock struct {
	Writes []WriteSetEntry `json:"writes"`
}

// WriteSet is the top-level write-set file format.
type WriteSet struct {
	// Module is the store the writes belong to. Only "evm" is supported in v1;
	// empty defaults to "evm".
	Module string          `json:"module,omitempty"`
	Blocks []WriteSetBlock `json:"blocks"`
}

// LoadWriteSet reads and validates a write-set file.
func LoadWriteSet(path string) (*WriteSet, error) {
	data, err := os.ReadFile(path) //nolint:gosec // benchmark input path supplied by the operator
	if err != nil {
		return nil, fmt.Errorf("read write-set file: %w", err)
	}
	var ws WriteSet
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse write-set file: %w", err)
	}
	if err := ws.Validate(); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Validate checks module support and per-entry field consistency.
func (ws *WriteSet) Validate() error {
	if ws.Module != "" && ws.Module != keys.EVMStoreKey {
		return fmt.Errorf("unsupported module %q: v1 replay supports only %q", ws.Module, keys.EVMStoreKey)
	}
	if len(ws.Blocks) == 0 {
		return fmt.Errorf("write set has no blocks")
	}
	for bi, block := range ws.Blocks {
		for wi, w := range block.Writes {
			if _, err := buildEntryKey(w); err != nil {
				return fmt.Errorf("block %d write %d: %w", bi, wi, err)
			}
			if !w.Delete {
				if _, err := decodeEntryValue(w); err != nil {
					return fmt.Errorf("block %d write %d: %w", bi, wi, err)
				}
			}
		}
	}
	return nil
}

// TotalKeys returns the total number of writes across all blocks.
func (ws *WriteSet) TotalKeys() int {
	total := 0
	for _, b := range ws.Blocks {
		total += len(b.Writes)
	}
	return total
}

// BlockChangesets converts one block into the NamedChangeSet slice consumed by
// DBWrapper.ApplyChangeSets.
func (ws *WriteSet) BlockChangesets(blockIdx int) ([]*proto.NamedChangeSet, error) {
	block := ws.Blocks[blockIdx]
	pairs := make([]*proto.KVPair, 0, len(block.Writes))
	for wi, w := range block.Writes {
		key, err := buildEntryKey(w)
		if err != nil {
			return nil, fmt.Errorf("block %d write %d: %w", blockIdx, wi, err)
		}
		pair := &proto.KVPair{Key: key, Delete: w.Delete}
		if !w.Delete {
			value, err := decodeEntryValue(w)
			if err != nil {
				return nil, fmt.Errorf("block %d write %d: %w", blockIdx, wi, err)
			}
			pair.Value = value
		}
		pairs = append(pairs, pair)
	}
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}, nil
}

// buildEntryKey builds the raw store key for a write-set entry.
func buildEntryKey(w WriteSetEntry) ([]byte, error) {
	switch w.Kind {
	case WriteKindStorage:
		addr, err := decodeHexField("address", w.Address, keys.AddressLen)
		if err != nil {
			return nil, err
		}
		slot, err := decodeHexField("slot", w.Slot, 32)
		if err != nil {
			return nil, err
		}
		return keys.BuildEVMKey(keys.EVMKeyStorage, append(addr, slot...)), nil
	case WriteKindCode, WriteKindNonce, WriteKindCodeHash:
		addr, err := decodeHexField("address", w.Address, keys.AddressLen)
		if err != nil {
			return nil, err
		}
		kind := map[string]keys.EVMKeyKind{
			WriteKindCode:     keys.EVMKeyCode,
			WriteKindNonce:    keys.EVMKeyNonce,
			WriteKindCodeHash: keys.EVMKeyCodeHash,
		}[w.Kind]
		return keys.BuildEVMKey(kind, addr), nil
	case WriteKindRaw:
		key, err := decodeHexField("key", w.Key, 0)
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, fmt.Errorf("raw write has empty key")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unknown write kind %q", w.Kind)
	}
}

// valueLenForKind returns the exact byte length a kind's value must have, or 0
// when the length is unconstrained (code and raw). The fixed widths mirror the
// FlatKV apply path (vtype.ParseNonce/ParseCodeHash/ParseStorageValue), so a
// wrong-length value in a hand-authored write-set file is rejected up front by
// Validate rather than only failing later inside ApplyChangeSets on FlatKV
// (memiavl stores raw bytes and would silently accept it, breaking the
// same-write-set-across-backends premise of the benchmark).
//
// Raw entries are the escape hatch this check cannot cover: their keys are
// opaque here, so hand-authored raw entries must target key families FlatKV
// does not width-check (legacy prefixes such as 0x09 codesize). A raw key
// aliasing an optimized family (e.g. 0x0a nonce) with a wrong-width value
// passes Validate, replays on memiavl, and hard-fails on FlatKV.
func valueLenForKind(kind string) int {
	switch kind {
	case WriteKindNonce:
		return 8
	case WriteKindStorage, WriteKindCodeHash:
		return 32
	default: // WriteKindCode, WriteKindRaw: unconstrained
		return 0
	}
}

// decodeEntryValue decodes a write entry's value, enforcing the fixed width its
// kind requires (see valueLenForKind).
func decodeEntryValue(w WriteSetEntry) ([]byte, error) {
	return decodeHexField("value", w.Value, valueLenForKind(w.Kind))
}

// decodeHexField decodes a hex field, tolerating a 0x prefix. wantLen of 0
// disables the length check. An empty string decodes to nil.
func decodeHexField(name, value string, wantLen int) ([]byte, error) {
	trimmed := strings.TrimPrefix(value, "0x")
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("field %s: invalid hex %q: %w", name, value, err)
	}
	if wantLen > 0 && len(decoded) != wantLen {
		return nil, fmt.Errorf("field %s: expected %d bytes, got %d", name, wantLen, len(decoded))
	}
	return decoded, nil
}

// OpenReplayWrapper opens a fresh DBWrapper for a replay run, supplying the
// explicit default config that the FlatKV wrapper factory requires.
//
// memiavl is opened with AsyncCommitBuffer=0 (synchronous WAL write) rather
// than the shared bench default of 10. With the async buffer, memiavl's
// Commit() returns once the WAL entry is enqueued, while FlatKV's Commit()
// waits for its WAL write — the reported commit_ns/key would compare enqueue
// latency against write latency. Neither backend fsyncs, so with a
// synchronous WAL write on both sides the durability semantics match.
func OpenReplayWrapper(ctx context.Context, backend wrappers.DBType, dbDir string) (wrappers.DBWrapper, error) {
	var dbConfig any
	switch backend {
	case wrappers.FlatKV:
		dbConfig = flatkvConfig.DefaultConfig()
	case wrappers.MemIAVL:
		cfg := wrappers.DefaultBenchMemIAVLConfig()
		cfg.AsyncCommitBuffer = 0
		dbConfig = &cfg
	}
	return wrappers.NewDBImpl(ctx, backend, dbDir, dbConfig)
}

// ReplayResult reports a replay run with apply and commit timed separately.
type ReplayResult struct {
	Blocks         int
	Keys           int
	ApplyDuration  time.Duration
	CommitDuration time.Duration
}

// ReplayWriteSet replays the write set through the wrapper, one block per
// version, timing ApplyChangeSets and Commit separately. The wrapper must be
// freshly opened (or snapshot-loaded); replay starts at wrapper.Version()+1.
func ReplayWriteSet(wrapper wrappers.DBWrapper, ws *WriteSet) (ReplayResult, error) {
	result := ReplayResult{Blocks: len(ws.Blocks), Keys: ws.TotalKeys()}
	baseVersion := wrapper.Version()
	for i := range ws.Blocks {
		changesets, err := ws.BlockChangesets(i)
		if err != nil {
			return result, err
		}
		entry := &proto.ChangelogEntry{
			Version:    baseVersion + int64(i) + 1,
			Changesets: changesets,
		}

		applyStart := time.Now()
		if err := wrapper.ApplyChangeSets(entry); err != nil {
			return result, fmt.Errorf("apply block %d: %w", i, err)
		}
		result.ApplyDuration += time.Since(applyStart)

		commitStart := time.Now()
		if _, err := wrapper.Commit(); err != nil {
			return result, fmt.Errorf("commit block %d: %w", i, err)
		}
		result.CommitDuration += time.Since(commitStart)
	}
	return result, nil
}

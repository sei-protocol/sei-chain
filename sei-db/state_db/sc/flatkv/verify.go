package flatkv

import (
	"bytes"
	"fmt"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// VerifyLtHash full-scans all four data DBs and checks the recomputed state
// against the store's maintained metadata. In addition to the global
// committedLtHash, it validates the per-DB, per-module decomposition that FlatKV
// now persists (per-module LtHashes and per-module key/byte stats), catching
// drift in that bookkeeping even when the global root still matches. Read-write
// stores with uncommitted ApplyChangeSets writes are rejected (the on-disk scan
// cannot see them).
//
// Buffers one DB's worth of KVs in memory at a time and is not cancellable.
// Intended for tests and offline maintenance / migration checks; not suitable
// for online verification of production-sized state.
func VerifyLtHash(s Store) error {
	cs, ok := s.(*CommitStore)
	if !ok {
		return fmt.Errorf("VerifyLtHash: unsupported store type %T", s)
	}
	return verifyLtHashInternal(cs)
}

func verifyLtHashInternal(cs *CommitStore) error {
	// A read-write store between ApplyChangeSets and Commit has
	// workingLtHash != committedLtHash. The full scan below reads only
	// persisted DB contents, so there is no way to validate the in-memory
	// pending state against disk here. Fail loudly rather than masquerade
	// a pending-writes situation as an integrity error.
	if !cs.readOnly && !cs.workingLtHash.Equal(cs.committedLtHash) {
		return fmt.Errorf(
			"VerifyLtHash: store has uncommitted writes at version %d; "+
				"commit or reopen readonly before verifying",
			cs.committedVersion,
		)
	}

	// Recompute each DB's per-module hashes and stats from disk, validate the
	// maintained per-module metadata against them, and accumulate the global
	// root as the homomorphic sum of the derived per-DB roots.
	global := lthash.New()
	for _, ndb := range cs.namedDataDBs() {
		if ndb.db == nil {
			continue
		}
		scanHash, scanStats, err := scanDBByModule(ndb.db)
		if err != nil {
			return fmt.Errorf("VerifyLtHash: scan %s: %w", ndb.dir, err)
		}
		dbRoot, err := cs.verifyDBModuleMetadata(ndb.dir, scanHash, scanStats)
		if err != nil {
			return err
		}
		global.MixIn(dbRoot)
	}

	// The full scan reflects on-disk (committed) state, so the only correct
	// reference is committedLtHash. workingLtHash may include uncommitted
	// ApplyChangeSets updates that have not yet been persisted.
	if gc, cc := global.Checksum(), cs.committedLtHash.Checksum(); gc != cc {
		return fmt.Errorf(
			"VerifyLtHash: global mismatch at version %d\n  committed: %x\n  full-scan: %x",
			cs.committedVersion, cc, gc,
		)
	}
	return nil
}

// scanDBByModule full-scans one data DB and returns, per module, the LtHash of
// its keys and their key-count / byte footprint. Meta keys are skipped. Module
// membership uses the same physical-key routing the write path uses, so the
// result is directly comparable to the maintained per-module metadata.
func scanDBByModule(db seidbtypes.KeyValueDB) (map[string]*lthash.LtHash, map[string]lthash.ModuleStats, error) {
	iter, err := db.NewIter(&seidbtypes.IterOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("open iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	byModule := make(map[string][]lthash.KVPairWithLastValue)
	stats := make(map[string]lthash.ModuleStats)
	for ; iter.Valid(); iter.Next() {
		if ktype.IsMetaKey(iter.Key()) {
			continue
		}
		module, err := moduleOfKey(iter.Key())
		if err != nil {
			return nil, nil, fmt.Errorf("route key %x: %w", iter.Key(), err)
		}
		byModule[module] = append(byModule[module], lthash.KVPairWithLastValue{
			Key:   bytes.Clone(iter.Key()),
			Value: bytes.Clone(iter.Value()),
		})
		st := stats[module]
		st.KeyCount++
		st.Bytes += int64(len(iter.Key())) + int64(len(iter.Value()))
		stats[module] = st
	}
	if err := iter.Error(); err != nil {
		return nil, nil, fmt.Errorf("iterator error: %w", err)
	}

	hashes := make(map[string]*lthash.LtHash, len(byModule))
	for module, pairs := range byModule {
		h, _ := lthash.ComputeLtHash(nil, pairs)
		if h == nil {
			h = lthash.New()
		}
		hashes[module] = h
	}
	return hashes, stats, nil
}

// verifyDBModuleMetadata checks the maintained per-module hashes and stats for
// one DB against a fresh scan, verifies they homomorphically sum to the
// maintained per-DB root, and returns that (scan-derived) per-DB root for the
// global accumulation. It fails on: a scanned module missing/mismatched in the
// maintained map, a maintained module absent from disk yet not zeroed, a
// stats mismatch, or the per-module sum not equaling the per-DB root.
func (cs *CommitStore) verifyDBModuleMetadata(
	dir string,
	scanHash map[string]*lthash.LtHash,
	scanStats map[string]lthash.ModuleStats,
) (*lthash.LtHash, error) {
	workingHash := cs.perDBModuleWorkingLtHash[dir]
	workingStats := cs.perDBModuleWorkingStats[dir]

	// Every module on disk must match the maintained hash and stats.
	for module, h := range scanHash {
		wh := workingHash[module]
		if wh == nil || !wh.Equal(h) {
			return nil, fmt.Errorf(
				"VerifyLtHash: per-module hash mismatch for %s/%s at version %d\n  maintained: %s\n  full-scan:  %x",
				dir, module, cs.committedVersion, checksumOrNil(wh), h.Checksum(),
			)
		}
		if ws := workingStats[module]; ws != scanStats[module] {
			return nil, fmt.Errorf(
				"VerifyLtHash: per-module stats mismatch for %s/%s at version %d\n  maintained: %+v\n  full-scan:  %+v",
				dir, module, cs.committedVersion, ws, scanStats[module],
			)
		}
	}

	// A maintained module with no keys on disk is allowed only if it has been
	// zeroed out (identity hash, empty stats) — the residue of deleting every
	// key of a module.
	for module, wh := range workingHash {
		if _, ok := scanHash[module]; ok {
			continue
		}
		if wh != nil && !wh.Equal(lthash.New()) {
			return nil, fmt.Errorf(
				"VerifyLtHash: maintained per-module hash for %s/%s is non-zero but the module has no keys on disk (version %d)",
				dir, module, cs.committedVersion,
			)
		}
		if ws := workingStats[module]; ws != (lthash.ModuleStats{}) {
			return nil, fmt.Errorf(
				"VerifyLtHash: maintained per-module stats for %s/%s are non-zero but the module has no keys on disk (version %d): %+v",
				dir, module, cs.committedVersion, ws,
			)
		}
	}

	// The maintained per-module hashes must homomorphically sum to the
	// maintained per-DB root, and that root must equal the scan.
	root := cs.perDBWorkingLtHash[dir]
	sum := lthash.SumModuleHashes(workingHash)
	if root == nil || !root.Equal(sum) {
		return nil, fmt.Errorf(
			"VerifyLtHash: per-module hashes do not sum to the per-DB root for %s at version %d\n  root: %s\n  sum:  %x",
			dir, cs.committedVersion, checksumOrNil(root), sum.Checksum(),
		)
	}
	scanRoot := lthash.SumModuleHashes(scanHash)
	if !root.Equal(scanRoot) {
		return nil, fmt.Errorf(
			"VerifyLtHash: per-DB root mismatch for %s at version %d\n  maintained: %x\n  full-scan:  %x",
			dir, cs.committedVersion, root.Checksum(), scanRoot.Checksum(),
		)
	}
	return scanRoot, nil
}

// checksumOrNil renders an LtHash checksum for error messages, tolerating nil.
func checksumOrNil(h *lthash.LtHash) string {
	if h == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%x", h.Checksum())
}

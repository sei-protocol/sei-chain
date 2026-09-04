package flatkv

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// VerifyLtHash scans all four data stores and checks the recomputed state against the store's maintained
// metadata. Beyond the global root it validates the per-DB, per-module decomposition — per-module hashes
// and per-module key/byte totals — catching drift in that bookkeeping even when the global root matches.
// A store with a staged block is rejected: the scan sees that block's rows and the maintained hashes do
// not.
//
// Buffers one store's worth of KVs in memory at a time and is not cancellable.
// Intended for tests and offline maintenance / migration checks; not suitable
// for online verification of production-sized state.
func VerifyLtHash(s gigatypes.LiveStateStore) error {
	cs, ok := s.(*CommitStore)
	if !ok {
		return fmt.Errorf("VerifyLtHash: unsupported store type %T", s)
	}
	return verifyLtHashInternal(cs)
}

func verifyLtHashInternal(cs *CommitStore) error {
	// The scan walks the stores, which merge a staged block's rows, while the hashes it is compared
	// against do not account for that block until it is sealed. Refuse rather than report a healthy
	// store as corrupt.
	if cs.pendingBlockHeight != 0 {
		return fmt.Errorf(
			"VerifyLtHash: store has uncommitted writes: block %d is staged; "+
				"commit or reopen readonly before verifying",
			cs.pendingBlockHeight,
		)
	}

	// verifyPersistedDBMetadata reads the databases rather than the stores, so whatever the view managers have
	// staged has to reach pebble before it can see it.
	if err := cs.flushLatestVersion(); err != nil {
		return fmt.Errorf("VerifyLtHash: flush before reading persisted metadata: %w", err)
	}

	// Recompute each DB's per-module hashes and stats from disk, validate the
	// maintained per-module metadata against them, and accumulate the global
	// root as the homomorphic sum of the derived per-DB roots.
	global := lthash.New()
	for _, store := range cs.stores {
		scanHash, scanStats, err := scanStoreByModule(store)
		if err != nil {
			return fmt.Errorf("VerifyLtHash: scan %s: %w", store.Name(), err)
		}
		dbRoot, err := cs.verifyDBModuleMetadata(store.Name(), scanHash, scanStats)
		if err != nil {
			return err
		}
		if err := cs.verifyPersistedDBMetadata(store.Name(), dbRoot); err != nil {
			return err
		}
		global.MixIn(dbRoot)
	}

	// The scan reflects committed state, so committedLtHash is the reference.
	if gc, cc := global.Checksum(), cs.committedLtHash.Checksum(); gc != cc {
		return fmt.Errorf(
			"VerifyLtHash: global mismatch at version %d\n  committed: %x\n  full-scan: %x",
			cs.committedVersion, cc, gc,
		)
	}
	return nil
}

// scanStoreByModule full-scans one data store and returns, per module, the
// LtHash of its keys and their key-count / byte footprint. Only rows with a
// non-empty key and non-empty value are counted — the same membership predicate
// foldChunk / serializeKV use for LtHash MixIn — so the scan is directly
// comparable to the maintained per-module metadata. Module membership uses the
// same physical-key routing the write path uses.
func scanStoreByModule(
	store view.ViewManager,
) (map[string]*lthash.LtHash, map[string]lthash.ModuleStats, error) {
	iter, err := store.Iterator(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	byModule := make(map[string][]lthash.KVPairWithLastValue)
	stats := make(map[string]lthash.ModuleStats)
	for ; iter.Valid(); iter.Next() {
		// Match foldChunk / serializeKV: empty key or empty value is not a
		// hash-set member and must not appear in stats. Reserved metadata keys are filtered by the
		// store, so there is nothing to skip for them here.
		if len(iter.Key()) == 0 || len(iter.Value()) == 0 {
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

// verifyPersistedDBMetadata reads the named DB's LocalMeta off disk and checks its version against the
// store's committed version and its root against scanRoot.
func (cs *CommitStore) verifyPersistedDBMetadata(dir string, scanRoot *lthash.LtHash) error {
	meta, err := loadLocalMeta(cs.rawDBFor(dir))
	if err != nil {
		return fmt.Errorf("VerifyLtHash: read %s persisted metadata: %w", dir, err)
	}
	if meta.CommittedVersion != cs.committedVersion {
		return fmt.Errorf(
			"VerifyLtHash: %s is persisted at version %d but the store is at %d",
			dir, meta.CommittedVersion, cs.committedVersion,
		)
	}
	// A DB that has never been written records no root at all, which reads as
	// the identity — the same value a scan of its empty keyspace produces.
	persistedRoot := meta.LtHash
	if persistedRoot == nil {
		persistedRoot = lthash.New()
	}
	if !persistedRoot.Equal(scanRoot) {
		return fmt.Errorf(
			"VerifyLtHash: persisted per-DB root mismatch for %s at version %d"+
				"\n  persisted: %x\n  full-scan: %x",
			dir, cs.committedVersion, persistedRoot.Checksum(), scanRoot.Checksum(),
		)
	}
	return nil
}

// verifyDBModuleMetadata checks the maintained per-module hashes and stats for
// one DB against a fresh scan, verifies they homomorphically sum to the
// maintained per-DB root, and returns that (scan-derived) per-DB root for the
// global accumulation. It fails on: a scanned module missing/mismatched in the
// maintained maps, a maintained hash/stats entry for a module absent from disk
// that is not zeroed, or the per-module sum not equaling the per-DB root.
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

	// A maintained hash for a module with no keys on disk is allowed only if
	// it has been zeroed (identity) — the residue of deleting every key of a
	// module.
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
	}

	// Same residue rule for stats. Iterate workingStats on its own so an
	// orphan entry (stats present, no hash entry, no on-disk keys) cannot
	// slip past the hash-keyed loop above.
	for module, ws := range workingStats {
		if _, ok := scanHash[module]; ok {
			continue
		}
		if ws != (lthash.ModuleStats{}) {
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

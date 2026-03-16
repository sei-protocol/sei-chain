package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// loadLocalMeta loads the local metadata from a DB, or returns default if not present.
func loadLocalMeta(db types.KeyValueDB) (*LocalMeta, error) {
	val, err := db.Get(DBLocalMetaKey)
	if err != nil && !errorutils.IsNotFound(err) {
		return nil, fmt.Errorf("could not get DBLocalMetaKey: %w", err)
	}
	if errorutils.IsNotFound(err) || val == nil {
		return &LocalMeta{CommittedVersion: 0}, nil
	}
	return UnmarshalLocalMeta(val)
}

// loadGlobalVersion reads the global committed version from metadata DB.
// Returns 0 if not found (fresh start).
func (s *CommitStore) loadGlobalVersion() (int64, error) {
	data, err := s.metadataDB.Get([]byte(MetaGlobalVersion))
	if errorutils.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read global version: %w", err)
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid global version length: got %d, want 8", len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("global version overflow: %d exceeds max int64", v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
}

// loadGlobalLtHash reads the global committed LtHash from metadata DB.
// Returns nil if not found (fresh start).
func (s *CommitStore) loadGlobalLtHash() (*lthash.LtHash, error) {
	data, err := s.metadataDB.Get([]byte(MetaGlobalLtHash))
	if errorutils.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	return lthash.Unmarshal(data)
}

// commitGlobalMetadata atomically commits global version, global LtHash,
// and per-DB LtHashes to metadata DB.
//
// When needsPerDBBackfill is true (upgrade in progress), per-DB keys are
// omitted to prevent persisting wrong zero-based hashes before the full-scan
// backfill completes.
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	batch := s.metadataDB.NewBatch()
	defer func() { _ = batch.Close() }()

	versionBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version)) //nolint:gosec // version is always non-negative

	if err := batch.Set([]byte(MetaGlobalVersion), versionBuf); err != nil {
		return fmt.Errorf("failed to set global version: %w", err)
	}

	lthashBytes := hash.Marshal()
	if err := batch.Set([]byte(MetaGlobalLtHash), lthashBytes); err != nil {
		return fmt.Errorf("failed to set global lthash: %w", err)
	}

	if !s.needsPerDBBackfill {
		for dbDir, metaKey := range perDBLtHashKeys {
			if h := s.perDBCommittedLtHash[dbDir]; h != nil {
				if err := batch.Set([]byte(metaKey), h.Marshal()); err != nil {
					return fmt.Errorf("failed to set %s lthash: %w", dbDir, err)
				}
			}
		}
	}

	return batch.Commit(types.WriteOptions{Sync: s.config.Fsync})
}

// loadPerDBLtHashes reads per-DB LtHashes from metadataDB.
// If any key is missing, sets needsPerDBBackfill and initializes all to zero.
func (s *CommitStore) loadPerDBLtHashes() error {
	loaded := make(map[string]*lthash.LtHash, len(perDBLtHashKeys))
	var missing []string

	for dbDir, metaKey := range perDBLtHashKeys {
		data, err := s.metadataDB.Get([]byte(metaKey))
		if errorutils.IsNotFound(err) {
			missing = append(missing, dbDir)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to read %s lthash: %w", dbDir, err)
		}
		h, err := lthash.Unmarshal(data)
		if err != nil {
			return fmt.Errorf("failed to unmarshal %s lthash: %w", dbDir, err)
		}
		loaded[dbDir] = h
	}

	if len(missing) > 0 {
		s.needsPerDBBackfill = true
		for dbDir := range perDBLtHashKeys {
			s.perDBCommittedLtHash[dbDir] = lthash.New()
			s.perDBWorkingLtHash[dbDir] = lthash.New()
		}
		logger.Info("per-DB LtHash keys missing from metadataDB, will backfill after catchup",
			"missing", missing, "found", len(loaded))
		return nil
	}

	for dbDir, h := range loaded {
		s.perDBCommittedLtHash[dbDir] = h
		s.perDBWorkingLtHash[dbDir] = h.Clone()
	}
	return nil
}

// fullScanDBLtHash computes the LtHash of a single data DB by iterating
// all KV pairs (excluding the LocalMeta key at 0x00).
func fullScanDBLtHash(db types.KeyValueDB) (*lthash.LtHash, error) {
	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: metaKeyLowerBound(),
	})
	if err != nil {
		return nil, fmt.Errorf("fullScanDBLtHash: new iter: %w", err)
	}
	defer iter.Close()

	var pairs []lthash.KVPairWithLastValue
	for iter.First(); iter.Valid(); iter.Next() {
		key := bytes.Clone(iter.Key())
		value := bytes.Clone(iter.Value())
		pairs = append(pairs, lthash.KVPairWithLastValue{
			Key:   key,
			Value: value,
		})
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("fullScanDBLtHash: iter error: %w", err)
	}

	result, _ := lthash.ComputeLtHash(nil, pairs)
	return result, nil
}

// backfillPerDBLtHashes computes per-DB LtHashes via full scan of each data DB
// and optionally persists them to metadataDB and each DB's LocalMeta.
// Must be called AFTER catchup when all DBs are at a consistent version.
func (s *CommitStore) backfillPerDBLtHashes(persist bool) error {
	dataDBs := map[string]types.KeyValueDB{
		accountDBDir: s.accountDB,
		codeDBDir:    s.codeDB,
		storageDBDir: s.storageDB,
		legacyDBDir:  s.legacyDB,
	}

	for dbDir, db := range dataDBs {
		h, err := fullScanDBLtHash(db)
		if err != nil {
			return fmt.Errorf("backfill %s: %w", dbDir, err)
		}
		s.perDBCommittedLtHash[dbDir] = h
		s.perDBWorkingLtHash[dbDir] = h.Clone()
	}

	if persist {
		batch := s.metadataDB.NewBatch()
		defer func() { _ = batch.Close() }()
		for dbDir, metaKey := range perDBLtHashKeys {
			if err := batch.Set([]byte(metaKey), s.perDBCommittedLtHash[dbDir].Marshal()); err != nil {
				return fmt.Errorf("backfill persist %s to metadataDB: %w", dbDir, err)
			}
		}
		if err := batch.Commit(types.WriteOptions{Sync: s.config.Fsync}); err != nil {
			return fmt.Errorf("backfill metadataDB commit: %w", err)
		}

		for dbDir, db := range dataDBs {
			meta := &LocalMeta{
				CommittedVersion: s.localMeta[dbDir].CommittedVersion,
				LtHash:           s.perDBCommittedLtHash[dbDir],
			}
			if err := db.Set(DBLocalMetaKey, MarshalLocalMeta(meta), types.WriteOptions{Sync: s.config.Fsync}); err != nil {
				return fmt.Errorf("backfill persist %s LocalMeta: %w", dbDir, err)
			}
			s.localMeta[dbDir] = meta
		}
	}

	s.needsPerDBBackfill = false
	logger.Info("per-DB LtHash backfill complete", "persist", persist, "version", s.committedVersion)
	return nil
}

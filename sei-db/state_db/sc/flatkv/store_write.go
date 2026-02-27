package flatkv

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// ApplyChangeSets buffers EVM changesets and updates LtHash.
//
// LtHash is computed based on actual storage format (internal keys):
// - storageDB: key=addr||slot, value=storage_value
// - accountDB: key=addr, value=AccountValue (balance(32)||nonce(8)||codehash(32)
// - codeDB: key=addr, value=bytecode
// - legacyDB: key=full original key (with prefix), value=raw value
func (s *CommitStore) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	// Save original changesets for changelog
	s.pendingChangeSets = append(s.pendingChangeSets, cs...)

	// Collect LtHash pairs per DB (using internal key format)
	var storagePairs []lthash.KVPairWithLastValue
	var codePairs []lthash.KVPairWithLastValue
	var legacyPairs []lthash.KVPairWithLastValue
	// Account pairs are collected at the end after all account changes are processed

	// Track which accounts were modified (for LtHash computation)
	modifiedAccounts := make(map[string]bool)

	for _, namedCS := range cs {
		if namedCS.Changeset.Pairs == nil {
			continue
		}

		for _, pair := range namedCS.Changeset.Pairs {
			// Parse memiavl key to determine type
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			if kind == evm.EVMKeyUnknown {
				// Skip non-EVM keys silently
				continue
			}

			// Route to appropriate DB based on key type
			switch kind {
			case evm.EVMKeyStorage:
				// Get old value for LtHash
				oldValue, err := s.getStorageValue(keyBytes)
				if err != nil {
					return fmt.Errorf("failed to get storage value: %w", err)
				}

				// Storage: keyBytes = addr(20) || slot(32)
				keyStr := string(keyBytes)
				if pair.Delete {
					s.storageWrites[keyStr] = &pendingKVWrite{
						key:      keyBytes,
						isDelete: true,
					}
				} else {
					s.storageWrites[keyStr] = &pendingKVWrite{
						key:   keyBytes,
						value: pair.Value,
					}
				}

				// LtHash pair: internal key directly
				storagePairs = append(storagePairs, lthash.KVPairWithLastValue{
					Key:       keyBytes,
					Value:     pair.Value,
					LastValue: oldValue,
					Delete:    pair.Delete,
				})

			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				// Account data: keyBytes = addr(20)
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					return fmt.Errorf("invalid address length %d for key kind %d", len(keyBytes), kind)
				}
				addrStr := string(addr[:])

				// Track this account as modified for LtHash
				modifiedAccounts[addrStr] = true
				// Get or create pending account write
				paw := s.accountWrites[addrStr]
				if paw == nil {
					// Load existing value from DB
					existingValue, err := s.getAccountValue(addr)
					if err != nil {
						return fmt.Errorf("failed to load existing account value: %w", err)
					}
					paw = &pendingAccountWrite{
						addr:  addr,
						value: existingValue,
					}
					s.accountWrites[addrStr] = paw
				}

				if pair.Delete {
					if kind == evm.EVMKeyNonce {
						paw.value.Nonce = 0
					} else {
						paw.value.CodeHash = CodeHash{}
					}
				} else {
					if kind == evm.EVMKeyNonce {
						if len(pair.Value) != NonceLen {
							return fmt.Errorf("invalid nonce value length: got %d, expected %d", len(pair.Value), NonceLen)
						}
						paw.value.Nonce = binary.BigEndian.Uint64(pair.Value)
					} else {
						if len(pair.Value) != CodeHashLen {
							return fmt.Errorf("invalid codehash value length: got %d, expected %d", len(pair.Value), CodeHashLen)
						}
						copy(paw.value.CodeHash[:], pair.Value)
					}
				}

			case evm.EVMKeyCode:
				// Get old value for LtHash
				oldValue, err := s.getCodeValue(keyBytes)
				if err != nil {
					return fmt.Errorf("failed to get code value: %w", err)
				}

				// Code: keyBytes = addr(20) - per x/evm/types/keys.go
				keyStr := string(keyBytes)
				if pair.Delete {
					s.codeWrites[keyStr] = &pendingKVWrite{
						key:      keyBytes,
						isDelete: true,
					}
				} else {
					s.codeWrites[keyStr] = &pendingKVWrite{
						key:   keyBytes,
						value: pair.Value,
					}
				}

				// LtHash pair: internal key directly
				codePairs = append(codePairs, lthash.KVPairWithLastValue{
					Key:       keyBytes,
					Value:     pair.Value,
					LastValue: oldValue,
					Delete:    pair.Delete,
				})

			case evm.EVMKeyLegacy:
				oldValue, err := s.getLegacyValue(keyBytes)
				if err != nil {
					return fmt.Errorf("failed to get legacy value: %w", err)
				}

				keyStr := string(keyBytes)
				if pair.Delete {
					s.legacyWrites[keyStr] = &pendingKVWrite{
						key:      keyBytes,
						isDelete: true,
					}
				} else {
					s.legacyWrites[keyStr] = &pendingKVWrite{
						key:   keyBytes,
						value: pair.Value,
					}
				}

				legacyPairs = append(legacyPairs, lthash.KVPairWithLastValue{
					Key:       keyBytes,
					Value:     pair.Value,
					LastValue: oldValue,
					Delete:    pair.Delete,
				})
			}
		}
	}

	// Build account LtHash pairs based on full AccountValue changes
	accountPairs := make([]lthash.KVPairWithLastValue, 0, len(modifiedAccounts))
	for addrStr := range modifiedAccounts {
		addr, ok := AddressFromBytes([]byte(addrStr))
		if !ok {
			return fmt.Errorf("invalid address in modifiedAccounts: %x", addrStr)
		}

		// Get old AccountValue from DB (committed state)
		oldAV, err := s.getAccountValueFromDB(addr)
		if err != nil {
			return fmt.Errorf("failed to get old account value for addr %x: %w", addr, err)
		}
		oldValue := oldAV.Encode()

		// Get new AccountValue (from pending writes or DB)
		var newValue []byte
		var isDelete bool
		if paw, ok := s.accountWrites[addrStr]; ok {
			newValue = paw.value.Encode()
			isDelete = paw.isDelete
		} else {
			// No pending write means no change (shouldn't happen, but be safe)
			continue
		}

		accountPairs = append(accountPairs, lthash.KVPairWithLastValue{
			Key:       AccountKey(addr),
			Value:     newValue,
			LastValue: oldValue,
			Delete:    isDelete,
		})
	}

	// Combine all pairs and update working LtHash
	allPairs := append(storagePairs, accountPairs...)
	allPairs = append(allPairs, codePairs...)
	allPairs = append(allPairs, legacyPairs...)

	if len(allPairs) > 0 {
		newLtHash, _ := lthash.ComputeLtHash(s.workingLtHash, allPairs)
		s.workingLtHash = newLtHash
	}

	return nil
}

// Commit persists buffered writes and advances the version.
// Protocol: WAL → per-DB batch (with LocalMeta) → flush → update metaDB.
// On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit() (int64, error) {
	// Auto-increment version
	version := s.committedVersion + 1

	// Step 1: Write Changelog (WAL) - source of truth (always sync)
	changelogEntry := proto.ChangelogEntry{
		Version:    version,
		Changesets: s.pendingChangeSets,
	}
	if err := s.changelog.Write(changelogEntry); err != nil {
		return 0, fmt.Errorf("changelog write: %w", err)
	}

	// Step 2: Commit to each DB (data + LocalMeta.CommittedVersion atomically)
	if err := s.commitBatches(version); err != nil {
		return 0, fmt.Errorf("db commit: %w", err)
	}

	// Step 3: Update in-memory committed state
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()

	// Step 4: Persist global metadata to metadata DB (always every block)
	if err := s.commitGlobalMetadata(version, s.committedLtHash); err != nil {
		return 0, fmt.Errorf("metadata DB commit: %w", err)
	}

	// Step 5: Clear pending buffers
	s.clearPendingWrites()

	s.log.Info("Committed version", "version", version)
	return version, nil
}

// clearPendingWrites clears all pending write buffers
func (s *CommitStore) clearPendingWrites() {
	s.accountWrites = make(map[string]*pendingAccountWrite)
	s.codeWrites = make(map[string]*pendingKVWrite)
	s.storageWrites = make(map[string]*pendingKVWrite)
	s.legacyWrites = make(map[string]*pendingKVWrite)
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0)
}

// commitBatches commits pending writes to their respective DBs atomically.
// Each DB batch includes LocalMeta update for crash recovery.
// Also called by catchup to replay WAL without re-writing changelog.
func (s *CommitStore) commitBatches(version int64) error {
	syncOpt := types.WriteOptions{Sync: s.config.Fsync}

	// Commit to accountDB
	// accountDB uses AccountValue structure: key=addr(20), value=balance(32)||nonce(8)||codehash(32)
	if len(s.accountWrites) > 0 || version > s.localMeta[accountDBDir].CommittedVersion {
		batch := s.accountDB.NewBatch()
		defer func() { _ = batch.Close() }()

		for _, paw := range s.accountWrites {
			key := AccountKey(paw.addr)
			if paw.isDelete {
				if err := batch.Delete(key); err != nil {
					return fmt.Errorf("accountDB delete: %w", err)
				}
			} else {
				// Encode AccountValue and store with addr as key
				encoded := EncodeAccountValue(paw.value)
				if err := batch.Set(key, encoded); err != nil {
					return fmt.Errorf("accountDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("accountDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("accountDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.localMeta[accountDBDir] = newLocalMeta
	}

	// Commit to codeDB
	if len(s.codeWrites) > 0 || version > s.localMeta[codeDBDir].CommittedVersion {
		batch := s.codeDB.NewBatch()
		defer func() { _ = batch.Close() }()

		for _, pw := range s.codeWrites {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("codeDB delete: %w", err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("codeDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("codeDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("codeDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.localMeta[codeDBDir] = newLocalMeta
	}

	// Commit to storageDB
	if len(s.storageWrites) > 0 || version > s.localMeta[storageDBDir].CommittedVersion {
		batch := s.storageDB.NewBatch()
		defer func() { _ = batch.Close() }()

		for _, pw := range s.storageWrites {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("storageDB delete: %w", err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("storageDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("storageDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("storageDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.localMeta[storageDBDir] = newLocalMeta
	}

	// Commit to legacyDB
	if len(s.legacyWrites) > 0 || version > s.localMeta[legacyDBDir].CommittedVersion {
		batch := s.legacyDB.NewBatch()
		defer func() { _ = batch.Close() }()

		for _, pw := range s.legacyWrites {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("legacyDB delete: %w", err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("legacyDB set: %w", err)
				}
			}
		}

		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("legacyDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("legacyDB commit: %w", err)
		}

		s.localMeta[legacyDBDir] = newLocalMeta
	}

	return nil
}

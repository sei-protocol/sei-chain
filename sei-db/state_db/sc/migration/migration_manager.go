package migration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "migration")

// MigrationManager handles migration from one database to another,
// routing reads and writes during the course of the migration.
//
// MigrationManager supports concurrent Read calls. ApplyChangeSets must not
// run concurrently with Read or with itself.
//
// If any method returns an error, the manager is left in an undefined state
// and the process is expected to tear itself down; a fresh manager
// constructed against the same WAL and databases will recover any in-flight
// batch on startup.
type MigrationManager struct {

	// For reading values out of the old database.
	oldDBReader DBReader

	// For writing values to the old database.
	oldDBWriter DBWriter

	// For reading values out of the new database.
	newDBReader DBReader

	// For writing values to the new database.
	newDBWriter DBWriter

	// For iterating through key-value pairs to migrate in the old database.
	iterator MigrationIterator

	// The boundary of the migration. All keys to the left of (or equal to) the boundary
	// are considered migrated.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int

	// Required to make writes across databases atomic.
	wal *MigrationWAL

	// The next migration batch to write to the WAL. The first batch ID is 1, and increases monotonically afterwards.
	nextBatchID uint64
}

// Create a new MigrationManager. Channeling reads/writes through this migration manager will cause the migration
// to progress. Feeding reads and writes through a migration manager after a migration has completed is equivalent
// to simply operating on the new database directly.
func NewMigrationManager(
	// The path to the WAL directory.
	walPath string,
	// The number of key-value pairs to migrate after each write operation. Must be > 0.
	migrationBatchSize int,
	// For reading values out of the old database.
	oldDBReader DBReader,
	// For writing values to the old database.
	oldDBWriter DBWriter,
	// For reading values out of the new database.
	newDBReader DBReader,
	// For writing values to the new database.
	newDBWriter DBWriter,
	// For iterating through key-value pairs to migrate in the old database.
	iterator MigrationIterator,
) (*MigrationManager, error) {

	switch {
	case oldDBReader == nil:
		return nil, errors.New("oldDBReader must not be nil")
	case oldDBWriter == nil:
		return nil, errors.New("oldDBWriter must not be nil")
	case newDBReader == nil:
		return nil, errors.New("newDBReader must not be nil")
	case newDBWriter == nil:
		return nil, errors.New("newDBWriter must not be nil")
	case iterator == nil:
		return nil, errors.New("iterator must not be nil")
	}
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migration batch size must be positive, got %d", migrationBatchSize)
	}

	wal, err := OpenMigrationWAL(walPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	// Recover any in-flight batch from the WAL before reading the boundary:
	// a replay to the new DB updates the stored boundary, and we want the
	// post-recovery value.
	nextBatchID, err := recoverFromWAL(wal, oldDBReader, oldDBWriter, newDBReader, newDBWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	boundary, err := readMigrationBoundary(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration boundary: %w", err)
	}
	iterator.SetBoundary(boundary)

	logger.Info("initialized migration manager",
		"boundary", boundary.String(), "nextBatchID", nextBatchID)

	return &MigrationManager{
		oldDBReader:        oldDBReader,
		oldDBWriter:        oldDBWriter,
		newDBReader:        newDBReader,
		newDBWriter:        newDBWriter,
		iterator:           iterator,
		boundary:           boundary,
		migrationBatchSize: migrationBatchSize,
		wal:                wal,
		nextBatchID:        nextBatchID,
	}, nil
}

// readMigrationBoundary reads the current migration boundary from the new
// database, or returns MigrationBoundaryNotStarted if none is stored yet.
func readMigrationBoundary(newDBReader DBReader) (MigrationBoundary, error) {
	boundaryBytes, ok, err := newDBReader(MigrationStore, []byte(MigrationBoundaryKey))
	if err != nil {
		return MigrationBoundary{}, fmt.Errorf("failed to get migration boundary: %w", err)
	}
	if !ok {
		return MigrationBoundaryNotStarted, nil
	}
	boundary, err := DeserializeMigrationBoundary(boundaryBytes)
	if err != nil {
		return MigrationBoundary{}, fmt.Errorf("failed to deserialize migration boundary: %w", err)
	}
	return boundary, nil
}

// recoverFromWAL brings the old and new databases back in sync with the
// WAL and returns the batch ID to use for the next Append.
//
// Two regimes:
//
//   - Empty WAL: either a fresh start (both DB counters zero) or a state
//     sync that delivered both DBs at some post-commit point N without
//     carrying the source WAL. Without a WAL there is no in-flight batch
//     to reconcile, so we trust the DBs iff their counters agree and
//     return counter+1. A disagreement is unrecoverable.
//
//   - Non-empty WAL: crash-recovery path. Each DB's counter must equal
//     the WAL's latest batch ID or be exactly one behind; anything else
//     is corruption. If either DB lags, the WAL payload is decoded and
//     the missing writes are replayed.
func recoverFromWAL(
	wal *MigrationWAL,
	oldDBReader DBReader,
	oldDBWriter DBWriter,
	newDBReader DBReader,
	newDBWriter DBWriter,
) (uint64, error) {
	walBatchID, payload, err := wal.Latest()
	if err != nil {
		return 0, fmt.Errorf("failed to read latest WAL record: %w", err)
	}
	oldBatchID, err := readDBBatchID(oldDBReader, OldDBBatchIDKey)
	if err != nil {
		return 0, fmt.Errorf("failed to read old DB batch ID: %w", err)
	}
	newBatchID, err := readDBBatchID(newDBReader, NewDBBatchIDKey)
	if err != nil {
		return 0, fmt.Errorf("failed to read new DB batch ID: %w", err)
	}

	// Empty WAL: fresh start (both zero) or post-state-sync (both equal
	// some N). Without a WAL we cannot reconcile a disagreement between
	// the two DBs, so a mismatch is fatal.
	if walBatchID == 0 {
		if oldBatchID != newBatchID {
			return 0, fmt.Errorf(
				"WAL is empty but DB batch IDs disagree (old=%d, new=%d); unrecoverable without a WAL",
				oldBatchID, newBatchID)
		}
		return oldBatchID + 1, nil
	}

	if walBatchID != oldBatchID && walBatchID != oldBatchID+1 {
		return 0, fmt.Errorf(
			"unexpected batch ID found in old DB, possible data corruption. Found %d, expected %d or %d",
			oldBatchID, walBatchID, walBatchID-1)
	}
	if walBatchID != newBatchID && walBatchID != newBatchID+1 {
		return 0, fmt.Errorf(
			"unexpected batch ID found in new DB, possible data corruption. Found %d, expected %d or %d",
			newBatchID, walBatchID, walBatchID-1)
	}

	needOldReplay := walBatchID != oldBatchID
	needNewReplay := walBatchID != newBatchID
	if !needOldReplay && !needNewReplay {
		return walBatchID + 1, nil
	}

	oldDBChangeSets, newDBChangeSets, err := decodeWALRecord(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to decode WAL record for replay: %w", err)
	}
	if needOldReplay {
		logger.Info("migration manager replaying changes to old DB", "batchID", walBatchID)
		if err := oldDBWriter(oldDBChangeSets); err != nil {
			return 0, fmt.Errorf("failed to replay batch %d to old DB: %w", walBatchID, err)
		}
	}
	if needNewReplay {
		logger.Info("migration manager replaying changes to new DB", "batchID", walBatchID)
		if err := newDBWriter(newDBChangeSets); err != nil {
			return 0, fmt.Errorf("failed to replay batch %d to new DB: %w", walBatchID, err)
		}
	}
	return walBatchID + 1, nil
}

// readDBBatchID reads a batch counter from a database's MigrationStore,
// returning 0 if no value is stored.
func readDBBatchID(reader DBReader, key string) (uint64, error) {
	data, ok, err := reader(MigrationStore, []byte(key))
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("expected 8-byte batch ID at %q, got %d bytes", key, len(data))
	}
	return binary.BigEndian.Uint64(data), nil
}

// Read a value from the database. If the requested value is migrated, read it from the new database.
// Otherwise, read it from the old database.
//
// Reads from MigrationStore always route to the new database.
//
// Not safe to call concurrently with ApplyChangeSets.
func (m *MigrationManager) Read(store string, key []byte) ([]byte, bool, error) {
	if store == MigrationStore {
		return m.newDBReader(store, key)
	}
	if m.boundary.IsMigrated(store, key) {
		return m.newDBReader(store, key)
	}
	return m.oldDBReader(store, key)
}

// Apply a batch of change sets to the database. Depending on the progress of the migration,
// writes are routed to either the new or old database.
//
// This method will also migrate some keys from the old database to the new database. Although this migration operation
// will change the hash of the databases, it will not change the overall state of the databases (i.e. the values
// returned by reads will be the same).
//
// Writes targeting MigrationStore are rejected with an error.
//
// If ctx is cancelled while ApplyChangeSets is waiting on the DB writers,
// it returns ctx.Err(). Note that the underlying DB writers are not
// themselves context-aware, so a cancel releases this call but does not
// abort in-flight writes.
//
// Not safe to call concurrently with Read or itself.
func (m *MigrationManager) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	if changesets == nil {
		changesets = make([]*proto.NamedChangeSet, 0)
	}
	for _, cs := range changesets {
		if cs.Name == MigrationStore {
			return fmt.Errorf("writes to internal migration store %q are not permitted", MigrationStore)
		}
	}

	// Once migration is complete, forward directly to the new DB.
	if m.boundary.Status() == MigrationComplete {
		if err := m.newDBWriter(changesets); err != nil {
			return fmt.Errorf("failed to apply changes to new database: %w", err)
		}
		return nil
	}

	// Pairs destined for each DB, grouped by store name and keyed by KVPair.Key.
	// Later writes to the same (store, key) overwrite earlier ones.
	oldDBPairsByStore := make(map[string]map[string]*proto.KVPair)
	newDBPairsByStore := make(map[string]map[string]*proto.KVPair)

	// Get a batch of keys to migrate
	valuesToMigrate, newBoundary, err := m.iterator.NextBatch(m.migrationBatchSize)
	if err != nil {
		return fmt.Errorf("failed to get next batch: %w", err)
	}
	m.boundary = newBoundary

	// Delete the keys from the old database and write them to the new database
	for _, value := range valuesToMigrate {
		putPair(oldDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Delete: true})
		putPair(newDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Value: value.Value})
	}

	// For each pair in the original change sets, route to the appropriate database.
	for _, changeSet := range changesets {
		for _, pair := range changeSet.Changeset.Pairs {
			if m.boundary.IsMigrated(changeSet.Name, pair.Key) {
				putPair(newDBPairsByStore, changeSet.Name, pair)
			} else {
				putPair(oldDBPairsByStore, changeSet.Name, pair)
			}
		}
	}

	oldDBChangeSet := flattenPairsByStore(oldDBPairsByStore)
	newDBChangeSets := flattenPairsByStore(newDBPairsByStore)

	migrationBatchIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(migrationBatchIDBytes, m.nextBatchID)

	// Write the new boundary into the new DB so we have the proper boundary if we restart/sync.
	// Write the migration batch to both DBs.
	newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
		Name: MigrationStore,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: []byte(MigrationBoundaryKey), Value: newBoundary.Serialize()},
				{Key: []byte(NewDBBatchIDKey), Value: migrationBatchIDBytes},
			},
		},
	})
	oldDBChangeSet = append(oldDBChangeSet, &proto.NamedChangeSet{
		Name: MigrationStore,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: []byte(OldDBBatchIDKey), Value: migrationBatchIDBytes},
			},
		},
	})

	walBytes, err := encodeWALRecord(oldDBChangeSet, newDBChangeSets)
	if err != nil {
		return fmt.Errorf("failed to encode WAL record: %w", err)
	}

	// Before writing to the databases, flush the batch to the WAL. This is
	// what makes the subsequent cross-DB writes atomic: if we crash after
	// the WAL append but before (or part way through) the DB writes, the
	// next boot will replay whichever side is missing.
	if err := m.wal.Append(m.nextBatchID, walBytes); err != nil {
		return fmt.Errorf("failed to append changes to WAL: %w", err)
	}
	m.nextBatchID++

	// Apply changes to each database in parallel.
	oldDBErr := make(chan error, 1)
	newDBErr := make(chan error, 1)
	go func() {
		err := m.oldDBWriter(oldDBChangeSet)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to old database: %w", err)
		}
		oldDBErr <- err
	}()
	go func() {
		err := m.newDBWriter(newDBChangeSets)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to new database: %w", err)
		}
		newDBErr <- err
	}()

	// Wait for both writers to finish.
	var oldErr, newErr error
	oldDone, newDone := false, false
	for !oldDone || !newDone {
		select {
		case e := <-oldDBErr:
			oldErr = e
			oldDone = true
		case e := <-newDBErr:
			newErr = e
			newDone = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := errors.Join(oldErr, newErr); err != nil {
		return fmt.Errorf("failed to apply changes to databases: %w", err)
	}

	return nil
}

// putPair inserts pair into dest under (store, pair.Key), creating the inner
// map on demand. Later writes to the same (store, key) overwrite earlier ones.
func putPair(dest map[string]map[string]*proto.KVPair, store string, pair *proto.KVPair) {
	byKey, ok := dest[store]
	if !ok {
		byKey = make(map[string]*proto.KVPair)
		dest[store] = byKey
	}
	byKey[string(pair.Key)] = pair
}

// flattenPairsByStore collapses a store-keyed map of (key -> KVPair) into one
// NamedChangeSet per store, with stores and pairs emitted in sorted order for
// deterministic downstream writes.
func flattenPairsByStore(pairsByStore map[string]map[string]*proto.KVPair) []*proto.NamedChangeSet {
	storeNames := make([]string, 0, len(pairsByStore))
	for name := range pairsByStore {
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)

	changeSets := make([]*proto.NamedChangeSet, 0, len(storeNames))
	for _, name := range storeNames {
		byKey := pairsByStore[name]
		pairs := make([]*proto.KVPair, 0, len(byKey))
		for _, pair := range byKey {
			pairs = append(pairs, pair)
		}
		sort.Slice(pairs, func(i, j int) bool {
			return string(pairs[i].Key) < string(pairs[j].Key)
		})
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: pairs},
		})
	}
	return changeSets
}

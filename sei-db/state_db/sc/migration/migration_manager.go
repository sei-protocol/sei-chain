package migration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
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
//
// Lifecycle. A manager is always in one of three states; ApplyChangeSets
// drives the transitions in the listed order:
//
//  1. Migrating: active migration. Reads split across old/new DBs by
//     boundary; writes go to both DBs atomically through the WAL.
//
//  2. BumpBlock: one-shot transition triggered by the first ApplyChangeSets
//     call after the iterator reports MigrationComplete. In a single
//     atomic new-DB changeset the manager writes MigrationVersionKey =
//     destVersion, deletes the now-obsolete MigrationBoundaryKey and
//     NewDBBatchIDKey, and applies the caller's writes. It then
//     best-effort removes the WAL directory and invokes
//     finalizeMigration. Old-DB metadata is left alone — finalizeMigration
//     is expected to delete the old DB wholesale.
//
//  3. Passthrough: versionBumped=true. All reads/writes forwarded directly
//     to the new DB. No WAL, no boundary, no iterator. This is also the
//     state a freshly constructed manager enters when the new DB already
//     reports destVersion (e.g. post-reboot after the bump block).
type MigrationManager struct {

	// For reading values out of the old database. May be nil once the
	// manager is in the passthrough state (post-bump or constructed at
	// destVersion).
	oldDBReader DBReader

	// For writing values to the old database. May be nil in passthrough
	// (see oldDBReader).
	oldDBWriter DBWriter

	// For reading values out of the new database.
	newDBReader DBReader

	// For writing values to the new database.
	newDBWriter DBWriter

	// For iterating through key-value pairs to migrate in the old
	// database. May be nil in passthrough.
	iterator MigrationIterator

	// The boundary of the migration. All keys to the left of (or equal to) the boundary
	// are considered migrated. In passthrough this is pinned to
	// MigrationBoundaryComplete so any stray Read call still routes to
	// the new DB.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int

	// Required to make writes across databases atomic. May be nil in
	// passthrough.
	wal *MigrationWAL

	// Cached so the bump block and the passthrough constructor path can
	// os.RemoveAll it.
	walPath string

	// The next migration batch to write to the WAL. The first batch ID is 1, and increases monotonically afterwards.
	nextBatchID uint64

	// The version we want to migrate to.
	targetVersion uint64

	// User-supplied cleanup hook invoked on the bump block and on every
	// subsequent boot while the DB reports destVersion. Must be
	// idempotent and never nil.
	finalizeMigration func()

	// True once MigrationVersionKey=destVersion has been durably written
	// to the new DB (either by a prior boot or by this manager's bump
	// block). When true, ApplyChangeSets becomes a pure passthrough.
	versionBumped bool
}

// NewMigrationManager constructs a MigrationManager.
//
// The manager transitions the stored migration version from startVersion
// to targetVersion. Callers supply the expected boundaries; the manager
// looks up the currently stored version (new DB first, then old DB; absent
// = 0) and decides how to proceed:
//
//   - current == destVersion: the migration is already done. Returns a
//     passthrough manager. The constructor best-effort removes the WAL
//     directory and invokes finalizeMigration before returning. In this
//     state the old-DB handles and the iterator may be nil — a prior boot
//     may already have deleted the old DB entirely.
//
//   - current == startVersion (or absent at startVersion=0): a migration
//     is either not started or in progress. All of oldDBReader,
//     oldDBWriter, newDBReader, newDBWriter, iterator must be non-nil.
//     The WAL is opened, any in-flight batch replayed, and the persisted
//     boundary adopted.
//
//   - anything else: unexpected. Returns an error; the process should
//     treat this as a fatal configuration mismatch.
//
// finalizeMigration must be non-nil and MUST be idempotent. It is invoked
// exactly once on the bump block (from ApplyChangeSets) and once per
// subsequent boot (from NewMigrationManager) for as long as the new DB
// reports destVersion. Any mix of "fully run before", "crashed partway",
// and "first invocation" has to converge to the same post-state. A
// typical implementation might close the old-DB handle and delete its
// storage directory, tolerating already-closed / already-removed state.
//
// finalizeMigration does not return an error; internal failures must be
// logged by the implementation. A transient failure (disk hiccup, stuck
// handle) will be retried on the next boot.
func NewMigrationManager(
	// The path to the WAL directory.
	walPath string,
	// The number of key-value pairs to migrate after each write operation. Must be > 0.
	migrationBatchSize int,
	// The migration version the stored data is expected to be at on entry.
	startVersion uint64,
	// The migration version the manager will transition to.
	// Must be strictly greater than startVersion.
	targetVersion uint64,
	// Idempotent cleanup callback invoked on the bump block and on every
	// boot while the DB reports targetVersion.
	finalizeMigration func(),
	// For reading values out of the old database. May be nil iff the new
	// DB already reports targetVersion.
	oldDBReader DBReader,
	// For writing values to the old database. May be nil iff the new DB
	// already reports targetVersion.
	oldDBWriter DBWriter,
	// For reading values out of the new database.
	newDBReader DBReader,
	// For writing values to the new database.
	newDBWriter DBWriter,
	// For iterating through key-value pairs to migrate in the old
	// database. May be nil iff the new DB already reports targetVersion.
	iterator MigrationIterator,
) (*MigrationManager, error) {

	// Always-required handles and parameters.
	switch {
	case newDBReader == nil:
		return nil, errors.New("newDBReader must not be nil")
	case newDBWriter == nil:
		return nil, errors.New("newDBWriter must not be nil")
	case finalizeMigration == nil:
		return nil, errors.New("finalizeMigration must not be nil")
	}
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migration batch size must be positive, got %d", migrationBatchSize)
	}
	if startVersion >= targetVersion {
		return nil, fmt.Errorf("startVersion (%d) must be strictly less than destVersion (%d)", startVersion, targetVersion)
	}

	// Look up the version from the new DB first. If it's already at
	// destVersion the migration has completed on a prior boot and we
	// don't need the old DB for anything.
	newDBVersion, newDBVersionPresent, err := readVersionFromDB(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version from new DB: %w", err)
	}

	if newDBVersionPresent {
		if newDBVersion != targetVersion {
			return nil, fmt.Errorf(
				"unexpected migration version in new DB: expected %d, got %d",
				targetVersion, newDBVersion)
		}
		// Passthrough path. Retry the cleanup that the bump block
		// attempted; both steps are idempotent so transient crashes
		// are harmless.
		if err := os.RemoveAll(walPath); err != nil {
			logger.Warn("failed to remove migration WAL directory; will retry on next boot",
				"walPath", walPath, "err", err)
		}
		finalizeMigration()
		logger.Info("migration manager constructed in passthrough mode",
			"destVersion", targetVersion)
		return &MigrationManager{
			newDBReader:        newDBReader,
			newDBWriter:        newDBWriter,
			boundary:           MigrationBoundaryComplete,
			migrationBatchSize: migrationBatchSize,
			walPath:            walPath,
			targetVersion:      targetVersion,
			finalizeMigration:  finalizeMigration,
			versionBumped:      true,
		}, nil
	}

	// Key was absent from the new DB; we're expecting to find
	// startVersion (possibly absent = 0) in the old DB. Old-DB handles
	// are required from here on.
	switch {
	case oldDBReader == nil:
		return nil, errors.New("oldDBReader must not be nil when new DB is not at destVersion")
	case oldDBWriter == nil:
		return nil, errors.New("oldDBWriter must not be nil when new DB is not at destVersion")
	case iterator == nil:
		return nil, errors.New("iterator must not be nil when new DB is not at destVersion")
	}

	oldDBVersion, _, err := readVersionFromDB(oldDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version from old DB: %w", err)
	}
	if oldDBVersion != startVersion {
		return nil, fmt.Errorf(
			"unexpected migration version: expected %d (start) or %d (dest), got %d",
			startVersion, targetVersion, oldDBVersion)
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
		"startVersion", startVersion, "destVersion", targetVersion,
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
		walPath:            walPath,
		nextBatchID:        nextBatchID,
		targetVersion:      targetVersion,
		finalizeMigration:  finalizeMigration,
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

// readVersionFromDB reads MigrationVersionKey from the given DB's
// MigrationStore, returning (version, present, error). An absent key is
// reported as (0, false, nil) so the caller can distinguish "not set"
// from "explicitly zero".
//
// This helper deliberately talks to a raw DBReader rather than going
// through MigrationManager.Read, whose MigrationStore rule is "always new
// DB" and would miss a value still sitting in the old DB after a prior
// migration.
func readVersionFromDB(reader DBReader) (uint64, bool, error) {
	data, ok, err := reader(MigrationStore, []byte(MigrationVersionKey))
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	if len(data) != 8 {
		return 0, false, fmt.Errorf(
			"expected 8-byte migration version, got %d bytes", len(data))
	}
	return binary.BigEndian.Uint64(data), true, nil
}

// IsAtVersion reports whether the DB reached by reader is currently at the
// given migration version. An absent MigrationVersionKey is interpreted as
// version 0.
//
// Intended for callers that need to decide, before constructing a
// MigrationManager, whether to bother opening the legacy/old DB at all:
//
//	atDest, err := migration.IsAtVersion(newReader, destVersion)
//	if err != nil { /* handle */ }
//	if atDest {
//	    // Skip opening the old DB; just go straight to the new one.
//	}
//
// This is a pure lookup; it does not mutate state or call any finalizer.
func IsAtVersion(reader DBReader, version uint64) (bool, error) {
	v, _, err := readVersionFromDB(reader)
	if err != nil {
		return false, err
	}
	return v == version, nil
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
// In passthrough (versionBumped=true), all reads route to the new DB.
//
// Not safe to call concurrently with ApplyChangeSets.
func (m *MigrationManager) Read(store string, key []byte) ([]byte, bool, error) {
	if m.versionBumped {
		return m.newDBReader(store, key)
	}
	if store == MigrationStore {
		return m.newDBReader(store, key)
	}
	if m.boundary.IsMigrated(store, key) {
		return m.newDBReader(store, key)
	}
	return m.oldDBReader(store, key)
}

// ApplyChangeSets applies a batch of change sets to the database.
//
// Three states, exercised in this order over a single migration's
// lifetime (see MigrationManager's type doc for the full story):
//
//  1. Passthrough (versionBumped=true): changesets are forwarded verbatim
//     to the new DB. The old DB, WAL, iterator, and boundary are not
//     touched.
//
//  2. Bump block (boundary == MigrationComplete but not yet
//     versionBumped): the first ApplyChangeSets call that observes a
//     Complete boundary. In a single atomic new-DB changeset the
//     manager appends a MigrationStore entry that writes
//     MigrationVersionKey = destVersion, deletes MigrationBoundaryKey,
//     and deletes NewDBBatchIDKey alongside the caller's pairs. On
//     success, it best-effort removes the WAL directory and invokes
//     finalizeMigration(); transient failures here are non-fatal
//     because the constructor retries on every subsequent boot. The
//     old DB is not touched by the bump block: finalizeMigration is
//     expected to remove the old DB entirely.
//
//  3. Migrating (default): migrates up to migrationBatchSize keys from
//     the old DB to the new DB, routes the caller's writes across the
//     boundary, appends a WAL record to make the cross-DB writes
//     atomic, and advances both DB counters.
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

	// Passthrough: migration is complete AND post-migration cleanup has
	// already been attempted at least once (either by a prior bump block
	// on this process or by the constructor on a later boot).
	if m.versionBumped {
		if err := m.newDBWriter(changesets); err != nil {
			return fmt.Errorf("failed to apply changes to new database: %w", err)
		}
		return nil
	}

	// Bump block: first ApplyChangeSets call since the iterator reported
	// the migration complete. Write the version + purge obsolete
	// metadata in the same atomic new-DB changeset as the caller's
	// pairs, then best-effort clean up.
	if m.boundary.Status() == MigrationComplete {
		versionBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(versionBytes, m.targetVersion)
		// Build a fresh slice so we don't mutate the caller's backing
		// array with the MigrationStore entry.
		bumpCS := make([]*proto.NamedChangeSet, 0, len(changesets)+1)
		bumpCS = append(bumpCS, changesets...)
		bumpCS = append(bumpCS, &proto.NamedChangeSet{
			Name: MigrationStore,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(MigrationVersionKey), Value: versionBytes},
				{Key: []byte(MigrationBoundaryKey), Delete: true},
				{Key: []byte(NewDBBatchIDKey), Delete: true},
			}},
		})
		if err := m.newDBWriter(bumpCS); err != nil {
			return fmt.Errorf("failed to write version bump to new database: %w", err)
		}
		// Best-effort cleanup after the atomic write. The constructor
		// retries both on every subsequent boot, so a transient failure
		// here is non-fatal.
		if m.walPath != "" {
			if err := os.RemoveAll(m.walPath); err != nil {
				logger.Warn("failed to remove migration WAL directory after bump; will retry on next boot",
					"walPath", m.walPath, "err", err)
			}
		}
		m.finalizeMigration()
		m.versionBumped = true
		logger.Info("migration completed; manager switched to passthrough",
			"destVersion", m.targetVersion)
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

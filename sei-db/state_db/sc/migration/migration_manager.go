package migration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
	db "github.com/tendermint/tm-db"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "migration")

var _ Router = (*MigrationManager)(nil)

// MigrationManager handles migration from one database to another,
// routing reads and writes during the course of the migration.
//
// MigrationManager is NOT safe for concurrent use; wrap it with
// NewThreadSafeRouter to serialize external callers. BuildRouter wraps
// each Router it returns automatically.
//
// A MigrationManager always represents an in-progress migration. Reads
// split across old/new DBs by boundary; writes are routed across the
// boundary and applied to both DBs sequentially (old DB first, then
// new DB). Each block, up to migrationBatchSize keys are deleted from
// the old DB and written to the new DB. The boundary advances to
// MigrationBoundaryComplete on the final block of the migration; once
// that happens the manager is not expected to be reused on subsequent
// blocks - the layer above is expected to detect completion via
// IsAtVersion and switch to a steady-state router.
//
// On any returned error from ApplyChangeSets the manager's in-memory
// state (boundary, iterator cursor, metrics) may have advanced past the
// state durably persisted to the underlying DBs. The manager must not
// be reused after such an error: callers are required to treat any
// ApplyChangeSets error as fatal, shut the process down, and run
// cross-DB recovery on next boot.
type MigrationManager struct {
	// For reading values out of the old database.
	oldDBReader DBReader

	// For writing values to the old database.
	oldDBWriter DBWriter

	// For reading values out of the new database.
	newDBReader DBReader

	// For writing values to the new database.
	newDBWriter DBWriter

	// For iterating through key-value pairs to migrate in the old
	// database.
	iterator MigrationIterator

	// The boundary of the migration. All keys to the left of (or equal
	// to) the boundary are considered migrated. Reaches
	// MigrationBoundaryComplete on the final block of the migration.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int

	// The version we want to migrate to.
	targetVersion uint64

	// Optional metrics sink. May be nil; all calls on this field go
	// through nil-safe methods on *MigrationMetrics.
	metrics *MigrationMetrics
}

// Handles the migration of data from one database to another.
//
// All five DB/iterator handles are unconditionally required. If the new
// DB is already at targetVersion the migration is over and the caller
// must construct the next migration mode's router (steady-state) rather
// than a MigrationManager; passing such a configuration here returns an
// error.
func NewMigrationManager(
	// The number of key-value pairs to migrate after each write operation. Must be > 0.
	migrationBatchSize int,
	// The migration version the stored data is expected to be at on entry. If no prior migration
	// version is stored in the DB, startVersion should be 0.
	startVersion uint64,
	// The migration version after the migration is complete.
	// Must be strictly greater than startVersion.
	targetVersion uint64,
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
	// Optional metrics sink. Pass nil to disable metric emission.
	metrics *MigrationMetrics,
) (*MigrationManager, error) {

	if oldDBReader == nil {
		return nil, errors.New("oldDBReader must not be nil")
	}
	if oldDBWriter == nil {
		return nil, errors.New("oldDBWriter must not be nil")
	}
	if newDBReader == nil {
		return nil, errors.New("newDBReader must not be nil")
	}
	if newDBWriter == nil {
		return nil, errors.New("newDBWriter must not be nil")
	}
	if iterator == nil {
		return nil, errors.New("iterator must not be nil")
	}
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migration batch size must be positive, got %d", migrationBatchSize)
	}
	if startVersion >= targetVersion {
		return nil, fmt.Errorf("startVersion (%d) must be strictly less than targetVersion (%d)",
			startVersion, targetVersion)
	}

	// Look up the version from the new DB first. If it's already at
	// targetVersion the migration has completed on a prior boot; the
	// caller should not be constructing a MigrationManager in that case.
	currentMigrationVersion, versionKnown, err := readVersionFromDB(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version from new DB: %w", err)
	}

	if versionKnown {
		if currentMigrationVersion == targetVersion {
			return nil, fmt.Errorf(
				"new DB already at targetVersion (%d); construct the next migration mode's router instead of a MigrationManager",
				targetVersion)
		}
		if currentMigrationVersion != startVersion {
			return nil, fmt.Errorf(
				"unexpected migration version in new DB: expected %d (start) or %d (target), got %d",
				startVersion, targetVersion, currentMigrationVersion)
		}
	}

	if !versionKnown {
		// The version wasn't in the new DB, so read it from the old DB.
		currentMigrationVersion, _, err = readVersionFromDB(oldDBReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration version from old DB: %w", err)
		}
		if currentMigrationVersion != startVersion {
			return nil, fmt.Errorf(
				"unexpected migration version in old DB: expected %d, got %d", startVersion, currentMigrationVersion)
		}
	}

	boundary, err := readMigrationBoundary(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration boundary: %w", err)
	}
	iterator.SetBoundary(boundary)

	logger.Info("initialized migration manager",
		"startVersion", startVersion,
		"targetVersion", targetVersion,
		"boundary", boundary.String())

	metrics.SetVersion(currentMigrationVersion)
	metrics.SetBoundary(boundary)

	return &MigrationManager{
		oldDBReader:        oldDBReader,
		oldDBWriter:        oldDBWriter,
		newDBReader:        newDBReader,
		newDBWriter:        newDBWriter,
		iterator:           iterator,
		boundary:           boundary,
		migrationBatchSize: migrationBatchSize,
		targetVersion:      targetVersion,
		metrics:            metrics,
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
// This helper takes a raw DBReader rather than going through
// MigrationManager.Read because the MigrationStore is reserved for
// internal use and MigrationManager.Read rejects reads against it.
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
//	atTarget, err := migration.IsAtVersion(newReader, targetVersion)
//	if err != nil { /* handle */ }
//	if atTarget {
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

// Read a value from the database. If the requested value is migrated,
// read it from the new database. Otherwise, read it from the old
// database. After the boundary has reached MigrationBoundaryComplete on
// the final block of the migration, IsMigrated returns true for every
// key, so all reads route to the new DB.
//
// Reads targeting MigrationStore are rejected with an error: that store
// is reserved for the manager's own bookkeeping.
//
// Not safe for concurrent use; wrap with NewThreadSafeRouter.
func (m *MigrationManager) Read(store string, key []byte) ([]byte, bool, error) {
	if store == MigrationStore {
		// The migration module is reserved for internal use, do not permit outer scope reads from it.
		return nil, false, fmt.Errorf("reads from the 'migration' module are not permitted")
	}
	if m.boundary.IsMigrated(store, key) {
		// This key has already been migrated, read it from the new DB.
		return m.newDBReader(store, key)
	}
	// This key has not been migrated, read it from the old DB.
	return m.oldDBReader(store, key)
}

// ApplyChangeSets applies a batch of change sets to the database.
//
// Writes are dispatched sequentially: the old DB first, then the new DB.
// If the old-DB write fails the new-DB write is not attempted. Between
// writes the context is checked and a cancellation short-circuits the
// new-DB write. Sequential is a deliberate correctness/simplicity
// choice at the current scale (migration windows are small; per-block
// work is small); revisit if migration-window throughput becomes a
// bottleneck.
//
// On any returned error, the manager's in-memory state may have
// advanced past the durable state (the boundary, iterator cursor, and
// metrics are updated before the writes are dispatched). The manager
// must not be reused after a returned error: callers are required to
// treat such errors as fatal, shut the process down, and run cross-DB
// recovery on next boot.
//
// Not safe for concurrent use; wrap with NewThreadSafeRouter.
func (m *MigrationManager) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	start := time.Now()
	defer func() {
		m.metrics.RecordApplyDuration(time.Since(start))
	}()

	if changesets == nil {
		changesets = make([]*proto.NamedChangeSet, 0)
	}
	for _, cs := range changesets {
		if cs.Name == MigrationStore {
			// The migration module is reserved for internal use, do not permit outer scope writes to it.
			return fmt.Errorf("writes to internal migration store %q are not permitted", MigrationStore)
		}
	}

	if m.boundary.Equals(MigrationBoundaryComplete) {
		// Migration is complete; forward the caller's writes to the new DB only.
		if err := m.newDBWriter(ctx, changesets); err != nil {
			return fmt.Errorf("failed to apply changes to new database: %w", err)
		}
		return nil
	}

	// Get the next batch of keys to migrate.
	valuesToMigrate, newBoundary, err := m.iterator.NextBatch(m.migrationBatchSize)
	if err != nil {
		return fmt.Errorf("failed to get next batch: %w", err)
	}
	m.boundary = newBoundary
	m.metrics.SetBoundary(newBoundary)

	// Pairs destined for each DB, grouped by store name and keyed by KVPair.Key.
	oldDBPairsByStore := make(map[string]map[string]*proto.KVPair)
	newDBPairsByStore := make(map[string]map[string]*proto.KVPair)

	// Create change sets that move the values to migrate from the old DB to the new DB.
	var keyBytesThisBatch, valueBytesThisBatch int64
	for _, value := range valuesToMigrate {
		keyBytesThisBatch += int64(len(value.Key))
		valueBytesThisBatch += int64(len(value.Value))
		// Write the value to the new DB.
		putPair(newDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Value: value.Value})
		// Delete the value from the old DB.
		putPair(oldDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Delete: true})
	}
	m.metrics.ReportKeysMigrated(int64(len(valuesToMigrate)), keyBytesThisBatch, valueBytesThisBatch)

	// For each pair in the original change sets, route to the appropriate database.
	// These must overwrite migrated values, so it's important to do this after we've collected
	// the change set for the migrated values.
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

	if m.boundary.Equals(MigrationBoundaryComplete) {
		// On the final block of the migration, update the migration version and delete the boundary.
		versionBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(versionBytes, m.targetVersion)
		newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
			Name: MigrationStore,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(MigrationVersionKey), Value: versionBytes},
				{Key: []byte(MigrationBoundaryKey), Delete: true},
			}},
		})
		// Mirror the on-disk version bump in the in-memory metric so the
		// version gauge and the boundary-snapshot loop see the
		// completion at the same moment the DB does.
		m.metrics.SetVersion(m.targetVersion)
	} else {
		// On every other block of the migration, update the boundary.
		newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
			Name: MigrationStore,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(MigrationBoundaryKey), Value: newBoundary.Serialize()},
			}},
		})
	}

	// Write to the old DB first, then the new DB. If the old-DB write
	// fails we do not touch the new DB; this gives the caller a clean
	// "old DB diverged from new DB" recovery point. Between writes we
	// honor context cancellation so a long-running new-DB write is not
	// dispatched after a cancelled context.
	if err := m.oldDBWriter(ctx, oldDBChangeSet); err != nil {
		return fmt.Errorf("failed to apply changes to old database: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.newDBWriter(ctx, newDBChangeSets); err != nil {
		return fmt.Errorf("failed to apply changes to new database: %w", err)
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

// GetProof implements [Router].
func (m *MigrationManager) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	// We won't be able to serve state proofs for flatKV until we implement BUD proofs.
	return nil, fmt.Errorf("state proofs not supported for store %q", store)
}

// Iterator implements [Router].
func (m *MigrationManager) Iterator(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	// Eventually we will implement iteration for some modules within FlatKV, but never for the evm/ module.
	// Since we're migrating the evm/ module first, implementing iteration for FlatKV is not a blocker.
	return nil, fmt.Errorf("iteration not supported for store %q", store)
}

// BuildRoute returns a Route that dispatches the given module names to
// this MigrationManager. Reads, writes, iteration and proof requests
// for those modules will all flow through this migration manager.
//
// Module names must be unique; NewRoute's validation rules apply. The
// returned Route may be passed to NewModuleRouter alongside other
// Routes to compose multi-database setups.
func (m *MigrationManager) BuildRoute(moduleNames ...string) (*Route, error) {
	return NewRoute(m.Read, m.ApplyChangeSets, m.Iterator, m.GetProof, moduleNames...)
}

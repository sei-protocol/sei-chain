package migration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "migration")

var _ Router = (*MigrationManager)(nil)

// MigrationManager handles migration from one database to another,
// routing reads and writes during the course of the migration.
//
// MigrationManager supports concurrent Read calls. ApplyChangeSets must not
// run concurrently with Read or with itself.
//
// A migration manager has two states: "migrating" and "passthrough".
//
//  1. Migrating: active migration. Reads split across old/new DBs by
//     boundary, writes are routed across the boundary and applied to
//     both DBs in parallel. Each block, N keys are deleted from the old DB
//     and written to the new DB.
//  2. Passthrough: All reads/writes forwarded directly
//     to the new DB. No boundary, no iterator.
type MigrationManager struct {

	// For reading values out of the old database. May be nil once the
	// manager is in the passthrough state (post-finalization or
	// constructed at targetVersion).
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

	// The boundary of the migration. All keys to the left of (or equal
	// to) the boundary are considered migrated. In passthrough this is
	// pinned to MigrationBoundaryComplete, though Read short-circuits
	// via migrationFinished before consulting the boundary.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int

	// The version we want to migrate to.
	targetVersion uint64

	// If true, then the migration has been fully completed.
	migrationFinished bool

	// Optional metrics sink. May be nil; all calls on this field go
	// through nil-safe methods on *MigrationMetrics.
	metrics *MigrationMetrics
}

// Handles the migration of data from one database to another.
func NewMigrationManager(
	// The number of key-value pairs to migrate after each write operation. Must be > 0.
	migrationBatchSize int,
	// The migration version the stored data is expected to be at on entry. If no prior migration
	// version is stored in the DB, startVersion should be 0.
	startVersion uint64,
	// The migration version after the migration is complete.
	// Must be strictly greater than startVersion.
	targetVersion uint64,
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
	// Optional metrics sink. Pass nil to disable metric emission.
	metrics *MigrationMetrics,
) (*MigrationManager, error) {

	// Always-required handles and parameters.
	if newDBReader == nil {
		return nil, errors.New("newDBReader must not be nil")
	}
	if newDBWriter == nil {
		return nil, errors.New("newDBWriter must not be nil")
	}
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migration batch size must be positive, got %d", migrationBatchSize)
	}
	if startVersion >= targetVersion {
		return nil, fmt.Errorf("startVersion (%d) must be strictly less than targetVersion (%d)",
			startVersion, targetVersion)
	}

	// Look up the version from the new DB first. If it's already at
	// targetVersion the migration has completed on a prior boot and we
	// don't need the old DB for anything.
	currentMigrationVersion, versionKnown, err := readVersionFromDB(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version from new DB: %w", err)
	}

	if versionKnown {
		if currentMigrationVersion == targetVersion {
			// Passthrough path, migration already complete.
			logger.Info("migration manager constructed in passthrough mode", "targetVersion", targetVersion)
			metrics.SetVersion(targetVersion)
			metrics.SetBoundary(MigrationBoundaryComplete)
			return &MigrationManager{
				newDBReader:        newDBReader,
				newDBWriter:        newDBWriter,
				boundary:           MigrationBoundaryComplete,
				migrationBatchSize: migrationBatchSize,
				targetVersion:      targetVersion,
				migrationFinished:  true,
				metrics:            metrics,
			}, nil
		}
		if currentMigrationVersion != startVersion {
			return nil, fmt.Errorf(
				"unexpected migration version in new DB: expected %d (start) or %d (target), got %d",
				startVersion, targetVersion, currentMigrationVersion)
		}
	}

	// Migration is not complete, so we can't tolerate nil old DB accessors.
	if oldDBReader == nil {
		return nil, errors.New("oldDBReader must not be nil when new DB is not at targetVersion")
	}
	if oldDBWriter == nil {
		return nil, errors.New("oldDBWriter must not be nil when new DB is not at targetVersion")
	}
	if iterator == nil {
		return nil, errors.New("iterator must not be nil when new DB is not at targetVersion")
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

// Read a value from the database. If the requested value is migrated, read it from the new database.
// Otherwise, read it from the old database.
//
// Reads targeting MigrationStore are rejected with an error: that store
// is reserved for the manager's own bookkeeping.
//
// In passthrough (migrationFinished=true), all reads route to the new DB.
//
// Not safe to call concurrently with ApplyChangeSets.
func (m *MigrationManager) Read(store string, key []byte) ([]byte, bool, error) {
	if store == MigrationStore {
		// The migration module is reserved for internal use, do not permit outer scope reads from it.
		return nil, false, fmt.Errorf("reads from the 'migration' module are not permitted")
	}
	if m.migrationFinished {
		// We've finished the migration, all reads should go to the new DB.
		return m.newDBReader(store, key)
	}
	if m.boundary.IsMigrated(store, key) {
		// We are mid-migration and this key has already been migrated, read it from the new DB.
		return m.newDBReader(store, key)
	}
	// We are mid-migration and this key has not been migrated, read it from the old DB.
	return m.oldDBReader(store, key)
}

// ApplyChangeSets applies a batch of change sets to the database.
//
// Not safe to call concurrently with Read or itself.
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

	if m.migrationFinished {
		// Passthrough: migration is complete.
		if err := m.newDBWriter(changesets); err != nil {
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
	m.migrationFinished = newBoundary.Status() == MigrationComplete
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

	if m.migrationFinished {
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

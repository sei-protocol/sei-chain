package migration

import (
	"bytes"
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
type MigrationManager struct {
	// For reading values out of the old database.
	oldDBReader DBReader

	// For writing values to the old database.
	oldDBWriter DBWriter

	// For reading values out of the new database.
	newDBReader DBReader

	// For writing values to the new database.
	newDBWriter DBWriter

	// For preserving legacy key iteration while a module is migrating.
	oldDBIteratorBuilder DBIteratorBuilder

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

	// In-memory counters for operator-visible completion logs. These are not
	// persisted; after a restart they summarize the resumed segment.
	stats migrationRunStats
}

type migrationRunStats struct {
	startedAt time.Time

	batches                  int64
	keysMigrated             int64
	keyBytesMigrated         int64
	valueBytesMigrated       int64
	originalPairsRoutedOldDB int64
	originalPairsRoutedNewDB int64
	oldDBPairsWritten        int64
	newDBPairsWritten        int64
}

type migrationBatchStats struct {
	keysMigrated             int64
	keyBytesMigrated         int64
	valueBytesMigrated       int64
	originalPairsRoutedOldDB int64
	originalPairsRoutedNewDB int64
	oldDBPairsWritten        int64
	newDBPairsWritten        int64
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

	// Migration metadata is owned exclusively by the new DB (flatkv).
	currentMigrationVersion, versionKnown, err := readVersionFromDB(newDBReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version from new DB: %w", err)
	}

	atTargetVersion := versionKnown && currentMigrationVersion == targetVersion

	if versionKnown && !atTargetVersion && currentMigrationVersion != startVersion {
		return nil, fmt.Errorf(
			"unexpected migration version in new DB: expected %d (start) or %d (target), got %d",
			startVersion, targetVersion, currentMigrationVersion)
	}

	if !versionKnown {
		currentMigrationVersion = startVersion
	}

	var boundary MigrationBoundary
	if atTargetVersion {
		// The final block of the migration wrote MigrationVersionKey =
		// targetVersion and deleted MigrationBoundaryKey atomically, so
		// there is no boundary on disk to read. Come up in passthrough:
		// every read routes to the new DB via IsMigrated, every write
		// takes the post-completion early-return in ApplyChangeSets,
		// and the iterator's Complete short-circuit keeps it inert.
		boundary = MigrationBoundaryComplete
	} else {
		boundary, err = readMigrationBoundary(newDBReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration boundary: %w", err)
		}
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
		stats: migrationRunStats{
			startedAt: time.Now(),
		},
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
	// This key has not been migrated, so existing source data still lives in
	// the old DB. Brand-new writes created after migration starts are routed to
	// the new DB to avoid chasing an ever-growing key tail, so fall back there
	// if the old DB misses.
	value, found, err := m.oldDBReader(store, key)
	if err != nil || found {
		return value, found, err
	}
	return m.newDBReader(store, key)
}

// ApplyChangeSets applies a batch of change sets to the database.
//
// Not safe for concurrent use; wrap with NewThreadSafeRouter.
func (m *MigrationManager) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
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
	m.metrics.SetBoundary(newBoundary)

	// Pairs destined for each DB, grouped by store name and keyed by KVPair.Key.
	oldDBPairsByStore := make(map[string]map[string]*proto.KVPair)
	newDBPairsByStore := make(map[string]map[string]*proto.KVPair)

	// Create change sets that move the values to migrate from the old DB to the new DB.
	batchStats := migrationBatchStats{
		keysMigrated: int64(len(valuesToMigrate)),
	}
	for _, value := range valuesToMigrate {
		batchStats.keyBytesMigrated += int64(len(value.Key))
		batchStats.valueBytesMigrated += int64(len(value.Value))
		// Write the value to the new DB.
		putPair(newDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Value: value.Value})
		// Delete the value from the old DB.
		putPair(oldDBPairsByStore, value.ModuleName, &proto.KVPair{Key: value.Key, Delete: true})
	}
	m.metrics.ReportKeysMigrated(batchStats.keysMigrated, batchStats.keyBytesMigrated, batchStats.valueBytesMigrated)

	// For each pair in the original change sets, route to the appropriate database.
	// These must overwrite migrated values, so it's important to do this after we've collected
	// the change set for the migrated values.
	for _, changeSet := range changesets {
		for _, pair := range changeSet.Changeset.Pairs {
			writeNew, err := m.shouldWriteOriginalPairToNewDB(changeSet.Name, pair.Key)
			if err != nil {
				return err
			}
			if writeNew {
				putPair(newDBPairsByStore, changeSet.Name, pair)
				batchStats.originalPairsRoutedNewDB++
			} else {
				putPair(oldDBPairsByStore, changeSet.Name, pair)
				batchStats.originalPairsRoutedOldDB++
			}
		}
	}

	oldDBChangeSet, oldDBPairsWritten := flattenPairsByStore(oldDBPairsByStore)
	newDBChangeSets, newDBPairsWritten := flattenPairsByStore(newDBPairsByStore)
	batchStats.oldDBPairsWritten = oldDBPairsWritten
	migrationComplete := m.boundary.Equals(MigrationBoundaryComplete)
	metadataPairsWritten := int64(1)

	if migrationComplete {
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
		metadataPairsWritten = 2
	} else {
		// On every other block of the migration, update the boundary.
		newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
			Name: MigrationStore,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(MigrationBoundaryKey), Value: newBoundary.Serialize()},
			}},
		})
	}
	batchStats.newDBPairsWritten = newDBPairsWritten + metadataPairsWritten

	if err := m.oldDBWriter(oldDBChangeSet); err != nil {
		return fmt.Errorf("failed to apply changes to old database: %w", err)
	}
	if err := m.newDBWriter(newDBChangeSets); err != nil {
		return fmt.Errorf("failed to apply changes to new database: %w", err)
	}

	m.recordMigrationBatch(batchStats)
	if migrationComplete {
		m.logMigrationCompleteSummary()
	}

	return nil
}

func (m *MigrationManager) recordMigrationBatch(batch migrationBatchStats) {
	m.stats.batches++
	m.stats.keysMigrated += batch.keysMigrated
	m.stats.keyBytesMigrated += batch.keyBytesMigrated
	m.stats.valueBytesMigrated += batch.valueBytesMigrated
	m.stats.originalPairsRoutedOldDB += batch.originalPairsRoutedOldDB
	m.stats.originalPairsRoutedNewDB += batch.originalPairsRoutedNewDB
	m.stats.oldDBPairsWritten += batch.oldDBPairsWritten
	m.stats.newDBPairsWritten += batch.newDBPairsWritten
}

func (m *MigrationManager) logMigrationCompleteSummary() {
	elapsed := time.Since(m.stats.startedAt)
	logger.Info("migration complete",
		"targetVersion", m.targetVersion,
		"batches", m.stats.batches,
		"keysMigrated", m.stats.keysMigrated,
		"keyBytesMigrated", m.stats.keyBytesMigrated,
		"valueBytesMigrated", m.stats.valueBytesMigrated,
		"originalPairsRoutedOldDB", m.stats.originalPairsRoutedOldDB,
		"originalPairsRoutedNewDB", m.stats.originalPairsRoutedNewDB,
		"oldDBPairsWritten", m.stats.oldDBPairsWritten,
		"newDBPairsWritten", m.stats.newDBPairsWritten,
		"elapsed", elapsed)
}

func (m *MigrationManager) shouldWriteOriginalPairToNewDB(store string, key []byte) (bool, error) {
	if m.boundary.IsMigrated(store, key) {
		return true, nil
	}
	_, foundInOld, err := m.oldDBReader(store, key)
	if err != nil {
		return false, fmt.Errorf("failed to check old database for store %q key %x: %w", store, key, err)
	}
	// Existing not-yet-migrated keys stay in old DB so their latest value is
	// picked up when the iterator reaches them. Brand-new keys go straight to
	// new DB; otherwise continuously-created monotonically increasing keys can
	// keep the migration from ever reaching completion.
	return !foundInOld, nil
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
func flattenPairsByStore(pairsByStore map[string]map[string]*proto.KVPair) ([]*proto.NamedChangeSet, int64) {
	storeNames := make([]string, 0, len(pairsByStore))
	for name := range pairsByStore {
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)

	changeSets := make([]*proto.NamedChangeSet, 0, len(storeNames))
	var pairCount int64
	for _, name := range storeNames {
		byKey := pairsByStore[name]
		pairs := make([]*proto.KVPair, 0, len(byKey))
		for _, pair := range byKey {
			pairs = append(pairs, pair)
		}
		sort.Slice(pairs, func(i, j int) bool {
			return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0
		})
		pairCount += int64(len(pairs))
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: pairs},
		})
	}
	return changeSets, pairCount
}

// GetProof implements [Router].
func (m *MigrationManager) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	// We won't be able to serve state proofs for flatKV until we implement BUD proofs.
	return nil, fmt.Errorf("state proofs not supported for store %q", store)
}

// Iterator implements [Router].
func (m *MigrationManager) Iterator(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	if store == MigrationStore {
		return nil, fmt.Errorf("iteration from the 'migration' module is not permitted")
	}
	if m.boundary.Equals(MigrationBoundaryComplete) {
		return nil, fmt.Errorf("iteration not supported after migration completion for store %q", store)
	}
	if m.oldDBIteratorBuilder == nil {
		return nil, fmt.Errorf("iteration not supported for store %q", store)
	}

	// Preserve the legacy memiavl iterator path during the migration window.
	// This keeps existing EndBlock cleanup code that still does prefix scans
	// from panicking before the first migration batch commits, without adding a
	// per-block full FlatKV scan on large stores.
	return m.oldDBIteratorBuilder(store, start, end, ascending)
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

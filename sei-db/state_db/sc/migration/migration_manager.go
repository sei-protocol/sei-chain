package migration

import (
	"context"
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
// MigrationManager is NOT safe for concurrent concurrent reads, but not thread safe when writes are in flight.
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

	// The boundary of the migration. All keys to the left of the boundary (are considered migrated.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int
}

// Create a new MigrationManager. Channeling reads/writes through this migration manager will cause the migration
// to progress. Feeding reads and writes through a migration manager after a migration has completed is equivalent
// to simply operating on the new database directly.
func NewMigrationManager(
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
	// The number of key-value pairs to migrate after each write operation. Must be > 0.
	migrationBatchSize int,
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

	boundaryBytes, ok, err := newDBReader(MigrationStore, []byte(FlatKVMigrationBoundaryKey))
	if err != nil {
		return nil, fmt.Errorf("failed to get migration boundary: %w", err)
	}

	var boundary MigrationBoundary
	if ok {
		boundary, err = DeserializeMigrationBoundary(boundaryBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize migration boundary: %w", err)
		}
	} else {
		boundary = MigrationBoundaryNotStarted
	}
	iterator.SetBoundary(boundary)

	logger.Info("initialized migration manager", "boundary", boundary.String())

	return &MigrationManager{
		oldDBReader:        oldDBReader,
		oldDBWriter:        oldDBWriter,
		newDBReader:        newDBReader,
		newDBWriter:        newDBWriter,
		iterator:           iterator,
		boundary:           boundary,
		migrationBatchSize: migrationBatchSize,
	}, nil
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

	// Write the new boundary into the new DB so we have the proper boundary if we restart/sync.
	newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
		Name: MigrationStore,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: []byte(FlatKVMigrationBoundaryKey), Value: newBoundary.Serialize()},
			},
		},
	})

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

	// Wait for both writers, or bail out on ctx cancellation. Writer
	// goroutines send on buffered channels, so an early return does not
	// leak.
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
